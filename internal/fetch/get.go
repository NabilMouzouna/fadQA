package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultBodyCap = 4 << 20  // 4MB
	largeBodyCap   = 16 << 20 // 16MB — one-shot recovery when truncation is suspected
	requestTimeout = 20 * time.Second
	userAgent      = "fad-qa/1.0 (Realift SDK QA crawler)"
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

// GetPage fetches url with adaptive rate limiting, retry, and exponential
// backoff. A non-nil error means the retry budget was exhausted or ctx was
// cancelled — callers should record this as an ERROR verdict, distinct from
// any HTTP-status-based classification.
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

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := limiter.Acquire(ctx); err != nil {
			return Result{}, err
		}
		body, status, header, finalURL, retryAfterDur, err := doOnce(ctx, client, url, cap)
		limiter.Release()

		if err != nil {
			lastErr = err
			if waitErr := sleepCtx(ctx, backoffDuration(attempt)); waitErr != nil {
				return Result{}, waitErr
			}
			continue
		}
		lastStatus = status

		switch {
		case status == http.StatusOK:
			limiter.OnSuccess()
			return Result{Body: body, StatusCode: status, Attempts: attempt + 1, Header: header, FinalURL: finalURL}, nil

		case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
			limiter.OnThrottle()
			d := retryAfterDur
			if d == 0 {
				d = backoffDuration(attempt)
			}
			if waitErr := sleepCtx(ctx, d); waitErr != nil {
				return Result{}, waitErr
			}
			continue

		case status >= 500:
			if waitErr := sleepCtx(ctx, backoffDuration(attempt)); waitErr != nil {
				return Result{}, waitErr
			}
			continue

		default:
			// 404/401/403/etc: definitive, no retry — caller decides verdict.
			return Result{Body: body, StatusCode: status, Attempts: attempt + 1, Header: header, FinalURL: finalURL}, nil
		}
	}

	if lastErr != nil {
		return Result{StatusCode: lastStatus}, fmt.Errorf("fetch %s: exhausted retries: %w", url, lastErr)
	}
	return Result{StatusCode: lastStatus}, fmt.Errorf("fetch %s: exhausted retries (last status %d)", url, lastStatus)
}

func doOnce(ctx context.Context, client *http.Client, url string, cap int64) (body []byte, status int, header http.Header, finalURL string, retryAfterDur time.Duration, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, nil, "", 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, "", 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	status = resp.StatusCode
	header = resp.Header
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		retryAfterDur = retryAfter(resp.Header)
	}

	body, err = io.ReadAll(io.LimitReader(resp.Body, cap))
	if err != nil {
		return nil, status, header, finalURL, retryAfterDur, err
	}
	return body, status, header, finalURL, retryAfterDur, nil
}
