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
	// crawl must go quiet long enough for the per-IP score to decay. We start
	// at 60s (a light trip often clears in one step, since we also drop to
	// concurrency 1 AND ratchet the speed ceiling down) and escalate per
	// consecutive episode up to 2 minutes.
	baseChallengeCooldown = 60 * time.Second
	maxChallengeCooldown  = 120 * time.Second

	// blockedFromStartStreak is how many consecutive challenge episodes with
	// NO successful request *ever* we tolerate before concluding the store is
	// closed to automated access entirely (e.g. a locked preview domain) and
	// giving up. Crucially this only applies before the first success: once
	// any product has been fetched, the store clearly works and we NEVER give
	// up — we just pace down and grind through every product, however slowly.
	// That's the difference between "blocked" and "merely rate-limited".
	blockedFromStartStreak = 4
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
	hadSuccess      bool // has any request ever succeeded? (gates give-up)
	givenUp         bool // set only if blocked from the very start

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

// OnSuccess records a clean 200. It marks that the store works (which
// permanently disables give-up), clears the challenge streak, and after
// rampStreak consecutive successes nudges concurrency and rate up one step
// toward their (possibly ratcheted-down) ceilings.
func (l *Limiter) OnSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hadSuccess = true
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

// OnChallenge records a Cloudflare bot challenge. It drops to the floor
// (concurrency 1, min rate) AND ratchets the speed *ceiling* down (AIMD:
// multiplicative decrease of maxLimit/maxRate) so the limiter converges on a
// rate the store actually tolerates instead of ramping back up and
// re-tripping. It opens a single shared, escalating cooldown per episode
// (simultaneous detections by other workers coalesce into it).
//
// It only gives up (returns false) if the store has challenged us from the
// very start with no success ever — a store that's simply closed to
// automated access. Once ANY request has succeeded, it never gives up:
// returns true so the caller keeps retrying, and the crawl grinds through
// every product however slowly. That's the intended behavior — better to be
// slow than to abandon most of the catalog untested.
func (l *Limiter) OnChallenge() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.okStreak = 0
	l.curLimit = l.minLimit
	l.rate = l.minRate

	// Already cooling down for this episode (another worker got here first) —
	// don't escalate, re-count, or re-ratchet; just keep waiting it out.
	if time.Now().Before(l.pauseUntil) {
		return !l.givenUp
	}

	l.challengeStreak++
	l.totalChallenges++

	// Multiplicative decrease of the ceiling so we settle at a sustainable
	// pace rather than climbing back to a rate that keeps tripping.
	if l.maxLimit > l.minLimit {
		if l.maxLimit /= 2; l.maxLimit < l.minLimit {
			l.maxLimit = l.minLimit
		}
	}
	if l.maxRate > l.minRate {
		if l.maxRate /= 2; l.maxRate < l.minRate {
			l.maxRate = l.minRate
		}
	}

	// Give up ONLY if the store never let anything through (blocked from the
	// start). If it has worked at all, keep going indefinitely.
	if !l.hadSuccess && l.challengeStreak >= blockedFromStartStreak {
		l.givenUp = true
		return false
	}

	shift := l.challengeStreak - 1
	if shift > 4 {
		shift = 4 // guard against overflow on a very long streak
	}
	d := l.baseCooldown << shift
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
