package fetch

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestLimiter_StartsLowAndRampsUp(t *testing.T) {
	l := NewLimiter(8, 100)

	curLimit, rate := l.Snapshot()
	if curLimit != 2 || rate != 2 {
		t.Fatalf("expected slow start (2, 2), got (%d, %v)", curLimit, rate)
	}

	for i := 0; i < rampStreak; i++ {
		l.OnSuccess()
	}
	curLimit, rate = l.Snapshot()
	if curLimit != 3 || rate != 3 {
		t.Fatalf("expected ramp to (3, 3) after a clean streak, got (%d, %v)", curLimit, rate)
	}
}

func TestLimiter_ThrottleHalves(t *testing.T) {
	l := NewLimiter(8, 100)
	// ramp up a few steps first so halving is observable
	for i := 0; i < rampStreak*3; i++ {
		l.OnSuccess()
	}
	before, _ := l.Snapshot()
	l.OnThrottle()
	after, _ := l.Snapshot()
	if after > before/2+1 || after < 1 {
		t.Fatalf("expected concurrency roughly halved from %d, got %d", before, after)
	}
}

func forceFreshEpisode(l *Limiter) {
	l.mu.Lock()
	l.pauseUntil = time.Time{}
	l.mu.Unlock()
}

func TestLimiter_OnChallengeDropsToFloor(t *testing.T) {
	l := NewLimiter(8, 100)
	l.OnSuccess() // avoid give-up
	l.OnChallenge()
	if cur, rate := l.Snapshot(); cur != 1 || rate != 1 {
		t.Fatalf("expected drop to floor (1,1) on challenge, got (%d,%v)", cur, rate)
	}
}

func TestLimiter_GivesUpOnlyWhenBlockedFromStart(t *testing.T) {
	l := NewLimiter(8, 100) // no OnSuccess — simulate a store that blocks from request #1
	gaveUp := false
	for i := 0; i < blockedFromStartStreak+2; i++ {
		forceFreshEpisode(l)
		if !l.OnChallenge() {
			gaveUp = true
			break
		}
	}
	if !gaveUp || !l.GivenUp() {
		t.Fatalf("expected give-up when blocked from the very start")
	}
	if err := l.Acquire(context.Background()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected Acquire to return ErrBlocked once given up, got %v", err)
	}
}

func TestLimiter_NeverGivesUpAfterASuccess(t *testing.T) {
	l := NewLimiter(8, 100)
	l.OnSuccess() // the store has let something through — it works, just slowly
	for i := 0; i < blockedFromStartStreak*3; i++ {
		forceFreshEpisode(l)
		if !l.OnChallenge() {
			t.Fatalf("must never give up once a request has succeeded")
		}
	}
	if l.GivenUp() {
		t.Fatalf("GivenUp must stay false after a success")
	}
}

func TestLimiter_ChallengeRatchetsCeilingDown(t *testing.T) {
	l := NewLimiter(8, 8)
	l.OnSuccess() // avoid give-up
	forceFreshEpisode(l)
	l.OnChallenge() // ceiling 8 -> 4
	forceFreshEpisode(l)
	l.OnChallenge() // ceiling 4 -> 2
	l.mu.Lock()
	ml, mr := l.maxLimit, l.maxRate
	l.mu.Unlock()
	if ml != 2 || mr != 2 {
		t.Fatalf("expected ceiling ratcheted to 2/2, got %d/%v", ml, mr)
	}
}

func TestLimiter_ChallengeCoalescesSimultaneousDetections(t *testing.T) {
	l := NewLimiter(8, 100)
	l.mu.Lock()
	l.baseCooldown = time.Hour // long, so the episode stays "active"
	l.mu.Unlock()

	if !l.OnChallenge() {
		t.Fatalf("first challenge should keep going")
	}
	// Two more workers detecting the SAME episode (pause still active) must not
	// escalate the streak — they coalesce into the one episode.
	l.OnChallenge()
	l.OnChallenge()
	if got := l.TotalChallenges(); got != 1 {
		t.Fatalf("expected simultaneous detections to coalesce into 1 episode, got %d", got)
	}
}

func TestLimiter_SuccessClearsChallengeStreak(t *testing.T) {
	l := NewLimiter(8, 100)
	l.mu.Lock()
	l.baseCooldown = time.Millisecond
	l.mu.Unlock()

	l.OnChallenge()
	l.OnSuccess() // recovery
	l.mu.Lock()
	streak := l.challengeStreak
	l.mu.Unlock()
	if streak != 0 {
		t.Fatalf("expected a success to clear the challenge streak, got %d", streak)
	}
}

func TestLimiter_AcquireRespectsConcurrencyCap(t *testing.T) {
	l := NewLimiter(2, 1000)
	ctx := context.Background()

	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("acquire 2: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		_ = l.Acquire(ctx)
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatalf("third acquire should have blocked while 2 slots are held")
	case <-time.After(50 * time.Millisecond):
	}
	l.Release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatalf("third acquire should have unblocked after a release")
	}
	l.Release()
	l.Release()
}

func TestLimiter_AcquireRespectsContextCancellation(t *testing.T) {
	l := NewLimiter(1, 1000)
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Acquire(cancelCtx); err == nil {
		t.Fatalf("expected error from cancelled context")
	}
}

func TestLimiter_CooldownBlocksAllAcquires(t *testing.T) {
	l := NewLimiter(8, 1000)
	l.Cooldown(80 * time.Millisecond)
	start := time.Now()
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if time.Since(start) < 70*time.Millisecond {
		t.Fatalf("expected Acquire to block for the cooldown")
	}
}

func TestLimiter_CooldownExtendsButNeverShortens(t *testing.T) {
	l := NewLimiter(1, 1000)
	l.Cooldown(200 * time.Millisecond)
	l.Cooldown(50 * time.Millisecond)
	start := time.Now()
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatalf("a shorter Cooldown call shortened the existing pause")
	}
}

func TestBackoffDuration_MonotonicAndCapped(t *testing.T) {
	for attempt := 0; attempt < 12; attempt++ {
		d := backoffDuration(attempt, maxBackoff)
		if d <= 0 {
			t.Fatalf("attempt %d: expected positive duration, got %v", attempt, d)
		}
		if d > maxBackoff {
			t.Fatalf("attempt %d: exceeded cap: %v", attempt, d)
		}
	}
}

func TestRetryAfter_SecondsForm(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")
	if d := retryAfter(h); d != 5*time.Second {
		t.Fatalf("expected 5s, got %v", d)
	}
}

func TestRetryAfter_CapsAtMax(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "99999")
	if d := retryAfter(h); d != maxRetryAfter {
		t.Fatalf("expected capped at %v, got %v", maxRetryAfter, d)
	}
}

func TestRetryAfter_Absent(t *testing.T) {
	if d := retryAfter(http.Header{}); d != 0 {
		t.Fatalf("expected 0 for absent header, got %v", d)
	}
}
