package fetch

import (
	"net/http"
	"net/http/cookiejar"
	"time"
)

// browserUA presents fad-qa as an ordinary desktop Chrome — the same kind of
// traffic these public product pages already serve to every visitor. Shopify
// storefronts sit behind Cloudflare bot-management, which scores requests on
// how browser-like they look; a User-Agent that announces "crawler" plus
// missing browser headers raises that score. This is not evasion — the tool
// is doing legitimate QA on pages that are public to any browser — it just
// avoids looking gratuitously robotic. Kept reasonably current; a very stale
// version string is itself a mild bot signal.
const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// applyBrowserHeaders sets the headers a real Chrome sends on a top-level
// navigation. Deliberately omits Accept-Encoding: Go's transport adds it and
// transparently gunzips only when it set the header itself, so setting it
// here would hand back undecoded (possibly brotli) bytes.
func applyBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-CH-UA", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"macOS"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
}

// NewClient returns a single, reusable HTTP client. It carries a cookie jar
// so the Shopify/Cloudflare session cookies set on the first request are
// echoed back on later ones — making the crawl look like one visitor
// browsing a session, rather than thousands of cookieless first-time hits
// (another signal bot-management keys on). cookiejar.Jar is safe for
// concurrent use.
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
	jar, _ := cookiejar.New(nil) // never errors with nil options
	return &http.Client{
		Transport: transport,
		Jar:       jar,
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
