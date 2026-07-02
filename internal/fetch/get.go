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
	netAttempts, throttleAttempts := 0, 0

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
			// for the episode; the next Acquire waits it out, then this request
			// retries. Retrying harder is futile — only the crawl-wide slowdown
			// helps. The limiter is the sole give-up authority: once it has
			// ridden out enough consecutive escalating episodes with no
			// recovery, OnChallenge returns false and we stop (ErrBlocked),
			// which also latches so every other in-flight request bails fast.
			if !limiter.OnChallenge() {
				return Result{StatusCode: res.status}, fmt.Errorf("fetch %s: %w", url, ErrBlocked)
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
