package fetch

import (
	"context"
	"sync"
	"time"
)

// recoveryStreak is how many consecutive clean successes are required
// before the adaptive limiter grows concurrency/rate back up by one step.
const recoveryStreak = 20

// pollInterval is how often acquireSlot re-checks for a free concurrency
// slot. Cheap relative to request latency (hundreds of ms to seconds) even
// at the max supported concurrency (32).
const pollInterval = 5 * time.Millisecond

// Limiter combines a token-bucket rate cap with an AIMD-adaptive
// concurrency gate. It starts optimistic (full configured concurrency and
// rate) and backs off multiplicatively on 429/503 responses, recovering
// additively after a streak of clean successes — fast to back off, gentle
// to recover, which preserves speed on healthy stores while staying polite
// on rate-limited ones.
type Limiter struct {
	mu sync.Mutex

	// token bucket (steady-state request rate)
	tokens     float64
	rate       float64
	maxRate    float64
	minRate    float64
	burst      float64
	lastRefill time.Time

	// adaptive concurrency gate
	inFlight int
	curLimit int
	maxLimit int
	minLimit int
	okStreak int
}

// NewLimiter creates a limiter starting at maxConcurrency in-flight
// requests and maxRatePerSec steady-state throughput.
func NewLimiter(maxConcurrency int, maxRatePerSec float64) *Limiter {
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	if maxRatePerSec < 1 {
		maxRatePerSec = 1
	}
	return &Limiter{
		rate:       maxRatePerSec,
		maxRate:    maxRatePerSec,
		minRate:    1,
		burst:      maxRatePerSec + 2,
		tokens:     maxRatePerSec + 2,
		lastRefill: time.Now(),
		curLimit:   maxConcurrency,
		maxLimit:   maxConcurrency,
		minLimit:   1,
	}
}

// Acquire blocks until both a concurrency slot and a rate-limit token are
// available, or ctx is cancelled. Every successful Acquire must be paired
// with exactly one Release.
func (l *Limiter) Acquire(ctx context.Context) error {
	if err := l.acquireSlot(ctx); err != nil {
		return err
	}
	if err := l.acquireToken(ctx); err != nil {
		l.Release()
		return err
	}
	return nil
}

// Release frees the concurrency slot acquired by Acquire.
func (l *Limiter) Release() {
	l.mu.Lock()
	l.inFlight--
	l.mu.Unlock()
}

func (l *Limiter) acquireSlot(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		l.mu.Lock()
		if l.inFlight < l.curLimit {
			l.inFlight++
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l *Limiter) acquireToken(ctx context.Context) error {
	for {
		l.mu.Lock()
		l.refillLocked()
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		deficit := 1 - l.tokens
		rate := l.rate
		l.mu.Unlock()

		wait := time.Duration(deficit / rate * float64(time.Second))
		if wait <= 0 {
			wait = time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *Limiter) refillLocked() {
	now := time.Now()
	elapsed := now.Sub(l.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	l.tokens += elapsed * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.lastRefill = now
}

// OnSuccess records a clean (200) response, growing concurrency/rate by one
// step after recoveryStreak consecutive successes.
func (l *Limiter) OnSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.okStreak++
	if l.okStreak < recoveryStreak {
		return
	}
	l.okStreak = 0
	if l.curLimit < l.maxLimit {
		l.curLimit++
	}
	if l.rate < l.maxRate {
		l.rate++
		if l.rate > l.maxRate {
			l.rate = l.maxRate
		}
	}
}

// OnThrottle records a 429/503 response, halving concurrency and rate
// (floored) and resetting the recovery streak.
func (l *Limiter) OnThrottle() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.okStreak = 0

	newLimit := l.curLimit / 2
	if newLimit < l.minLimit {
		newLimit = l.minLimit
	}
	l.curLimit = newLimit

	newRate := l.rate / 2
	if newRate < l.minRate {
		newRate = l.minRate
	}
	l.rate = newRate
}

// Snapshot returns the current adaptive state, useful for verbose logging.
func (l *Limiter) Snapshot() (curLimit int, rate float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.curLimit, l.rate
}
