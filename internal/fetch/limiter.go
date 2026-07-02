package fetch

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	// rampStreak is how many consecutive clean successes are required before
	// the limiter nudges concurrency/rate up by one step. Higher than a
	// simple AIMD would use, because tripping Cloudflare's per-IP reputation
	// score is expensive (minutes of recovery), so we probe upward cautiously.
	rampStreak = 30

	// pollInterval is how often acquireSlot re-checks for a free slot.
	pollInterval = 5 * time.Millisecond

	// Challenge cooldowns. When Cloudflare starts challenging us the whole
	// crawl must go quiet long enough for the per-IP score to decay —
	// measured empirically at ~4 minutes after heavy flagging, faster after a
	// light trip. We start at 60s (a light trip often clears in one step,
	// since we also drop to concurrency 1) and double per consecutive episode
	// up to 4 minutes, which covers even a heavy trip's observed recovery.
	baseChallengeCooldown = 60 * time.Second
	maxChallengeCooldown  = 240 * time.Second

	// maxChallengeStreak is how many consecutive escalating challenge episodes
	// (with no successful request in between) we ride out before giving up on
	// the whole run. With the escalation above that's ~60+120+240+240s ≈ 11min
	// of the store continuously blocking us before we conclude it's genuinely
	// closed to automated access — at which point continuing is pointless and
	// rude. Any single success in between resets this to zero.
	maxChallengeStreak = 3
)

// ErrBlocked is returned by Acquire once the limiter has given up because the
// store keeps challenging every request. Callers should treat the affected
// products as ERROR ("blocked") rather than continuing to retry.
var ErrBlocked = errors.New("fetch: store persistently blocking automated requests (Cloudflare)")

// Limiter paces the crawl with two goals: (1) stay under Cloudflare's per-IP
// bot-reputation threshold so we're never challenged in the first place, and
// (2) if we are challenged, back off hard and let the score decay rather than
// hammering (which only keeps the flag alive). It starts deliberately slow
// and ramps up only after long clean streaks, so it self-discovers a safe
// speed for whatever store/network it's run against instead of hardcoding a
// magic rate that varies per Cloudflare config.
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

	// pauseUntil is a hard, shared cooldown: while set in the future EVERY
	// Acquire blocks, not just the caller that hit the limit.
	pauseUntil time.Time

	// challenge tracking
	challengeStreak int  // consecutive challenge episodes with no success since
	totalChallenges int  // total challenge episodes this run (for reporting)
	givenUp         bool // set once challengeStreak exceeds the cap

	// cooldown durations, defaulted from the package consts but held as
	// fields so tests can shrink them to exercise escalation/give-up fast.
	baseCooldown time.Duration
	maxCooldown  time.Duration

	onCooldown func(d time.Duration, episode int)
}

// NewLimiter creates a limiter whose concurrency and rate ramp UP toward
// maxConcurrency / maxRatePerSec from a slow, safe starting point.
func NewLimiter(maxConcurrency int, maxRatePerSec float64) *Limiter {
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	if maxRatePerSec < 1 {
		maxRatePerSec = 1
	}
	startLimit := 2
	if startLimit > maxConcurrency {
		startLimit = maxConcurrency
	}
	startRate := 2.0
	if startRate > maxRatePerSec {
		startRate = maxRatePerSec
	}
	return &Limiter{
		rate:         startRate,
		maxRate:      maxRatePerSec,
		minRate:      1,
		burst:        startRate,
		tokens:       startRate,
		lastRefill:   time.Now(),
		curLimit:     startLimit,
		maxLimit:     maxConcurrency,
		minLimit:     1,
		baseCooldown: baseChallengeCooldown,
		maxCooldown:  maxChallengeCooldown,
	}
}

// SetCooldownHook registers a callback fired once at the start of each new
// challenge cooldown episode (not once per worker). Used to surface a
// "pausing to let the store's rate limit clear" message in the UI.
func (l *Limiter) SetCooldownHook(fn func(d time.Duration, episode int)) {
	l.mu.Lock()
	l.onCooldown = fn
	l.mu.Unlock()
}

// Acquire blocks until any active cooldown has elapsed and both a concurrency
// slot and a rate-limit token are available, or ctx is cancelled. Returns
// ErrBlocked if the limiter has given up. Every successful Acquire must be
// paired with exactly one Release.
func (l *Limiter) Acquire(ctx context.Context) error {
	if err := l.acquirePause(ctx); err != nil {
		return err
	}
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

// Cooldown pauses every Acquire until d elapses (extends, never shortens).
// Used for a genuine Shopify Retry-After throttle.
func (l *Limiter) Cooldown(d time.Duration) {
	if d <= 0 {
		return
	}
	l.mu.Lock()
	if until := time.Now().Add(d); until.After(l.pauseUntil) {
		l.pauseUntil = until
	}
	l.mu.Unlock()
}

func (l *Limiter) acquirePause(ctx context.Context) error {
	for {
		l.mu.Lock()
		if l.givenUp {
			l.mu.Unlock()
			return ErrBlocked
		}
		wait := time.Until(l.pauseUntil)
		l.mu.Unlock()
		if wait <= 0 {
			return nil
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
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

// OnSuccess records a clean 200. It clears any challenge streak (recovery
// worked) and, after rampStreak consecutive successes, nudges concurrency and
// rate up one step toward their ceilings.
func (l *Limiter) OnSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.challengeStreak = 0
	l.okStreak++
	if l.okStreak < rampStreak {
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

// OnThrottle records a genuine Shopify quota 429/503 (one that carried a
// Retry-After): halve concurrency and rate, floored. The Retry-After itself
// is applied via Cooldown by the caller.
func (l *Limiter) OnThrottle() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.okStreak = 0
	if l.curLimit /= 2; l.curLimit < l.minLimit {
		l.curLimit = l.minLimit
	}
	if l.rate /= 2; l.rate < l.minRate {
		l.rate = l.minRate
	}
}

// OnChallenge records a Cloudflare bot challenge. Unlike a quota throttle this
// is a per-IP flag with no server-supplied timer, so the whole crawl must go
// quiet to let the score decay. It drops to the floor (concurrency 1, min
// rate) and opens a single shared cooldown for the episode; simultaneous
// detections by other workers coalesce into that one episode rather than
// stacking. Consecutive episodes escalate the cooldown, and after too many we
// give up. Returns true if the caller should keep retrying the request after
// the cooldown, false if we've given up.
func (l *Limiter) OnChallenge() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.okStreak = 0
	l.curLimit = l.minLimit
	l.rate = l.minRate

	// Already cooling down for this episode (another worker got here first) —
	// don't escalate or re-count; just keep waiting it out.
	if time.Now().Before(l.pauseUntil) {
		return !l.givenUp
	}

	l.challengeStreak++
	l.totalChallenges++
	if l.challengeStreak > maxChallengeStreak {
		l.givenUp = true
		return false
	}

	d := l.baseCooldown << (l.challengeStreak - 1)
	if d > l.maxCooldown || d <= 0 {
		d = l.maxCooldown
	}
	l.pauseUntil = time.Now().Add(d)
	if l.onCooldown != nil {
		l.onCooldown(d, l.totalChallenges)
	}
	return true
}

// TotalChallenges reports how many challenge episodes occurred this run.
func (l *Limiter) TotalChallenges() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.totalChallenges
}

// GivenUp reports whether the limiter concluded the store is persistently
// blocking automated access and stopped.
func (l *Limiter) GivenUp() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.givenUp
}

// Snapshot returns the current adaptive state, useful for verbose logging.
func (l *Limiter) Snapshot() (curLimit int, rate float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.curLimit, l.rate
}
