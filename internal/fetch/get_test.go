package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGetPage_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		if n <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	limiter := NewLimiter(4, 100)
	result, err := GetPage(context.Background(), NewClient(), limiter, srv.URL)
	if err != nil {
		t.Fatalf("expected eventual success, got error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.StatusCode)
	}
	if calls != 3 {
		t.Fatalf("expected exactly 3 calls (2 throttled + 1 success), got %d", calls)
	}
}

func TestGetPage_ExhaustsAfter429Storm(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: waits through the full 429 retry budget")
	}

	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	limiter := NewLimiter(4, 100)
	_, err := GetPage(context.Background(), NewClient(), limiter, srv.URL)
	if err == nil {
		t.Fatalf("expected an error after exhausting the 429 retry budget")
	}
	if got := atomic.LoadInt64(&calls); got != int64(max429Attempts) {
		t.Fatalf("expected exactly %d attempts, got %d", max429Attempts, got)
	}
}

func TestGetPage_ThrottleTriggersSharedCooldown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	limiter := NewLimiter(4, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A single 429 should place a shared cooldown on the limiter, blocking
	// an unrelated Acquire from a different caller immediately afterward.
	_, _ = doOnceAndThrottle(ctx, limiter, srv.URL)

	blocked := make(chan struct{})
	go func() {
		_ = limiter.Acquire(context.Background())
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatalf("expected the shared cooldown to block a concurrent Acquire")
	default:
	}
}

// doOnceAndThrottle issues one request and applies the same
// OnThrottle+Cooldown bookkeeping getPage does, without running the full
// retry loop — isolates the "one 429 pauses everyone" behavior for testing.
func doOnceAndThrottle(ctx context.Context, limiter *Limiter, url string) (int, error) {
	if err := limiter.Acquire(ctx); err != nil {
		return 0, err
	}
	_, status, header, _, _, err := doOnce(ctx, NewClient(), url, defaultBodyCap)
	limiter.Release()
	if err != nil {
		return 0, err
	}
	if status == http.StatusTooManyRequests {
		d := retryAfter(header)
		limiter.OnThrottle()
		limiter.Cooldown(d)
	}
	return status, nil
}
