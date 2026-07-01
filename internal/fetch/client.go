package fetch

import (
	"net/http"
	"time"
)

// NewClient returns a single, reusable HTTP client configured for high
// keep-alive reuse under bounded concurrency. Callers should construct one
// per run and share it across all workers.
func NewClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		DisableCompression:    false,
	}
	return &http.Client{
		Transport: transport,
		// No client-wide Timeout: each call site supplies a context
		// deadline instead, which distinguishes connect vs. read phases
		// more precisely than a single blunt timeout would.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}
