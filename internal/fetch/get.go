package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultBodyCap = 4 << 20  // 4MB
	largeBodyCap   = 16 << 20 // 16MB — one-shot recovery when truncation is suspected
	requestTimeout = 20 * time.Second

	// maxAttempts bounds retries for network errors and 5xx.
	maxAttempts = 5
	maxBackoff  = 30 * time.Second

	// maxReqChallenges is a per-request backstop: how many Cloudflare
	// cooldowns a SINGLE request rides before it's marked ERROR and the crawl
	// moves on to the next product (that ERROR is picked up by the retry
	// pass). This is not a global give-up — other products are still
	// attempted — it just stops one stubborn URL from looping forever. The
	// limiter's ceiling ratchet means a working store rarely challenges the
	// same request more than once or twice.
	maxReqChallenges = 8

	baseBackoff   = 500 * time.Millisecond
	maxRetryAfter = 120 * time.Second
)

// Result is the outcome of successfully fetching one URL (a definitive HTTP
// status was reached — including 404/401/403, which callers classify
// themselves rather than treating as a fetch failure).
type Result struct {
	Body       []byte
	StatusCode int
	Attempts   int
	Header     http.Header
	FinalURL   string
}

// GetPage fetches url with adaptive pacing, challenge handling, and retry.
// A non-nil error means the retry budget was exhausted, the store is blocking
// us (errors.Is(err, ErrBlocked)), or ctx was cancelled — callers record
// these as an ERROR verdict.
func GetPage(ctx context.Context, client *http.Client, limiter *Limiter, url string) (Result, error) {
	return getPage(ctx, client, limiter, url, defaultBodyCap)
}

// GetPageLarge re-fetches with a larger body cap, used when a script tag
// looked truncated at the default cap.
func GetPageLarge(ctx context.Context, client *http.Client, limiter *Limiter, url string) (Result, error) {
	return getPage(ctx, client, limiter, url, largeBodyCap)
}

func getPage(ctx context.Context, client *http.Client, limiter *Limiter, url string, cap int64) (Result, error) {
	var lastErr error
	var lastStatus int
	netAttempts, throttleAttempts, challengeAttempts := 0, 0, 0

	for {
		if err := limiter.Acquire(ctx); err != nil {
			// ErrBlocked (given up) or ctx cancelled — either way, stop.
			if errors.Is(err, ErrBlocked) {
				return Result{StatusCode: lastStatus}, fmt.Errorf("fetch %s: %w", url, err)
			}
			return Result{}, err
		}
		res := doOnce(ctx, client, url, cap)
		limiter.Release()

		if res.err != nil {
			lastErr = res.err
			netAttempts++
			if netAttempts >= maxAttempts {
				break
			}
			if waitErr := sleepCtx(ctx, backoffDuration(netAttempts-1, maxBackoff)); waitErr != nil {
				return Result{}, waitErr
			}
			continue
		}
		lastStatus = res.status
		lastErr = nil

		switch {
		case res.isChallenge:
			// Cloudflare bot gate. The limiter opens a single shared cooldown
			// for the episode + ratchets the ceiling down; the next Acquire
			// waits it out, then this request retries. OnChallenge returns
			// false ONLY if the store is blocked from the very start (no
			// success ever) — that latches a global give-up. Otherwise we keep
			// going; the per-request cap below just stops one stubborn URL
			// from looping forever (it errors and the crawl moves on).
			if !limiter.OnChallenge() {
				return Result{StatusCode: res.status}, fmt.Errorf("fetch %s: %w", url, ErrBlocked)
			}
			challengeAttempts++
			if challengeAttempts >= maxReqChallenges {
				return Result{StatusCode: res.status}, fmt.Errorf("fetch %s: still challenged after %d cooldowns", url, challengeAttempts)
			}
			continue

		case res.status == http.StatusOK:
			limiter.OnSuccess()
			return res.result(), nil

		case res.status == http.StatusTooManyRequests || res.status == http.StatusServiceUnavailable:
			// Genuine Shopify quota throttle (has Retry-After, not a CF
			// challenge). Respect the server's timer.
			throttleAttempts++
			d := res.retryAfter
			if d == 0 {
				d = backoffDuration(throttleAttempts-1, maxRetryAfter)
			}
			limiter.OnThrottle()
			limiter.Cooldown(d)
			if throttleAttempts >= maxAttempts {
				break
			}
			continue

		case res.status >= 500:
			netAttempts++
			if netAttempts >= maxAttempts {
				break
			}
			if waitErr := sleepCtx(ctx, backoffDuration(netAttempts-1, maxBackoff)); waitErr != nil {
				return Result{}, waitErr
			}
			continue

		default:
			// 404/401/403 (non-challenge): definitive, no retry.
			return res.result(), nil
		}
	}

	if lastErr != nil {
		return Result{StatusCode: lastStatus}, fmt.Errorf("fetch %s: exhausted retries: %w", url, lastErr)
	}
	return Result{StatusCode: lastStatus}, fmt.Errorf("fetch %s: exhausted retries (last status %d)", url, lastStatus)
}

// onceResult bundles everything doOnce observed about a single HTTP attempt.
type onceResult struct {
	body        []byte
	status      int
	header      http.Header
	finalURL    string
	retryAfter  time.Duration
	isChallenge bool
	err         error
}

func (o onceResult) result() Result {
	return Result{Body: o.body, StatusCode: o.status, Header: o.header, FinalURL: o.finalURL, Attempts: 1}
}

func doOnce(ctx context.Context, client *http.Client, url string, cap int64) onceResult {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return onceResult{err: err}
	}
	applyBrowserHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return onceResult{err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	out := onceResult{status: resp.StatusCode, header: resp.Header}
	if resp.Request != nil && resp.Request.URL != nil {
		out.finalURL = resp.Request.URL.String()
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, cap))
	if err != nil {
		out.err = err
		return out
	}
	out.body = body

	out.isChallenge = isCloudflareChallenge(out.status, out.header, out.body)
	if !out.isChallenge && (out.status == http.StatusTooManyRequests || out.status == http.StatusServiceUnavailable) {
		out.retryAfter = retryAfter(resp.Header)
	}
	return out
}
