package fetch

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestLimiter_AIMD_ThrottleThenRecover(t *testing.T) {
	l := NewLimiter(8, 100)

	curLimit, rate := l.Snapshot()
	if curLimit != 8 || rate != 100 {
		t.Fatalf("expected initial state (8, 100), got (%d, %v)", curLimit, rate)
	}

	l.OnThrottle()
	curLimit, rate = l.Snapshot()
	if curLimit != 4 {
		t.Fatalf("expected concurrency halved to 4 after throttle, got %d", curLimit)
	}
	if rate != 50 {
		t.Fatalf("expected rate halved to 50 after throttle, got %v", rate)
	}

	for i := 0; i < recoveryStreak; i++ {
		l.OnSuccess()
	}
	curLimit, rate = l.Snapshot()
	if curLimit != 5 {
		t.Fatalf("expected concurrency to grow by 1 to 5 after recovery streak, got %d", curLimit)
	}
	if rate != 51 {
		t.Fatalf("expected rate to grow by 1 to 51 after recovery streak, got %v", rate)
	}
}

func TestLimiter_NeverExceedsMaxOrFloor(t *testing.T) {
	l := NewLimiter(1, 1)
	l.OnThrottle() // already at floor
	curLimit, rate := l.Snapshot()
	if curLimit != 1 || rate != 1 {
		t.Fatalf("expected floor (1,1), got (%d, %v)", curLimit, rate)
	}

	for i := 0; i < recoveryStreak*5; i++ {
		l.OnSuccess()
	}
	curLimit, rate = l.Snapshot()
	if curLimit != 1 || rate != 1 {
		t.Fatalf("expected to stay at ceiling (1,1) since max==min, got (%d, %v)", curLimit, rate)
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
	ctx := context.Background()
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Acquire(cancelCtx); err == nil {
		t.Fatalf("expected error from cancelled context")
	}
}

func TestLimiter_CooldownBlocksAllAcquires(t *testing.T) {
	l := NewLimiter(8, 1000) // plenty of slots/tokens — only the cooldown should gate us
	l.Cooldown(80 * time.Millisecond)

	start := time.Now()
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 70*time.Millisecond {
		t.Fatalf("expected Acquire to block for the cooldown, only waited %v", elapsed)
	}
}

func TestLimiter_CooldownExtendsButNeverShortens(t *testing.T) {
	l := NewLimiter(1, 1000)
	l.Cooldown(200 * time.Millisecond)
	l.Cooldown(50 * time.Millisecond) // shorter — must not shrink the existing pause

	start := time.Now()
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatalf("a shorter Cooldown call shortened the existing pause")
	}
}

func TestBackoffDuration_MonotonicAndCapped(t *testing.T) {
	for attempt := 0; attempt < 10; attempt++ {
		d := backoffDuration(attempt, maxBackoff)
		if d <= 0 {
			t.Fatalf("attempt %d: expected positive duration, got %v", attempt, d)
		}
		if d > maxBackoff {
			t.Fatalf("attempt %d: exceeded cap: %v", attempt, d)
		}
	}
	for attempt := 0; attempt < 10; attempt++ {
		d := backoffDuration(attempt, max429Backoff)
		if d > max429Backoff {
			t.Fatalf("attempt %d: exceeded 429 cap: %v", attempt, d)
		}
	}
}

func TestRetryAfter_SecondsForm(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")
	d := retryAfter(h)
	if d != 5*time.Second {
		t.Fatalf("expected 5s, got %v", d)
	}
}

func TestRetryAfter_CapsAtMax(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "99999")
	d := retryAfter(h)
	if d != maxRetryAfter {
		t.Fatalf("expected capped at %v, got %v", maxRetryAfter, d)
	}
}

func TestRetryAfter_Absent(t *testing.T) {
	if d := retryAfter(http.Header{}); d != 0 {
		t.Fatalf("expected 0 for absent header, got %v", d)
	}
}
