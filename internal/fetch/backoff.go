package fetch

import (
	"context"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// Retry/backoff tuning constants live in get.go alongside the retry loop
// that consumes them (maxAttempts, maxBackoff, baseBackoff, maxRetryAfter).

// backoffDuration returns an exponential backoff with full jitter for the
// given zero-based attempt number, capped at `cap`.
func backoffDuration(attempt int, cap time.Duration) time.Duration {
	exp := float64(baseBackoff) * math.Pow(2, float64(attempt))
	if exp > float64(cap) {
		exp = float64(cap)
	}
	jittered := exp * (0.5 + rand.Float64()*0.5)
	return time.Duration(jittered)
}

// retryAfter parses a Retry-After header (seconds or HTTP-date form),
// capped at maxRetryAfter. Returns 0 if absent, unparseable, or negative.
func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		d := time.Duration(secs) * time.Second
		return capDuration(d, maxRetryAfter)
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return capDuration(d, maxRetryAfter)
	}
	return 0
}

func capDuration(d, max time.Duration) time.Duration {
	if d > max {
		return max
	}
	if d < 0 {
		return 0
	}
	return d
}

// sleepCtx sleeps for d or returns ctx.Err() if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
