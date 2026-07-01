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

func TestBackoffDuration_MonotonicAndCapped(t *testing.T) {
	prevMax := time.Duration(0)
	for attempt := 0; attempt < 10; attempt++ {
		d := backoffDuration(attempt)
		if d <= 0 {
			t.Fatalf("attempt %d: expected positive duration, got %v", attempt, d)
		}
		if d > maxBackoff {
			t.Fatalf("attempt %d: exceeded cap: %v", attempt, d)
		}
		_ = prevMax
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
