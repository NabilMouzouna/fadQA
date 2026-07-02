package fetch

import (
	"bytes"
	"net/http"
)

// challengeMarkers are strings that appear in a Cloudflare interstitial /
// managed-challenge page body. Confirmed against the live response: Shopify
// storefronts front by Cloudflare return an HTTP 429 whose body is a
// "Verifying your connection..." HTML page carrying a `cf-mitigated:
// challenge` header — NOT Shopify's JSON API rate-limit response.
var challengeMarkers = [][]byte{
	[]byte("cf-mitigated"),
	[]byte("Verifying your connection"),
	[]byte("Just a moment"),
	[]byte("challenge-platform"),
	[]byte("cf_chl_opt"),
	[]byte("_cf_chl"),
}

// isCloudflareChallenge reports whether a response is a Cloudflare bot
// challenge rather than a genuine Shopify quota throttle. The distinction
// matters: a real Shopify 429 carries a Retry-After and clears on its own
// timer, so retrying that one request works; a Cloudflare challenge is a
// per-IP reputation gate with no Retry-After that a plain HTTP client can
// never solve by retrying — the only remedy is to slow the whole crawl and
// let the score decay. Callers handle the two cases very differently.
func isCloudflareChallenge(status int, header http.Header, body []byte) bool {
	if header != nil && header.Get("cf-mitigated") != "" {
		return true
	}
	// Only treat these statuses as challenge candidates; a normal 200 product
	// page could legitimately contain a substring like "Just a moment" in its
	// own copy, so we don't scan successful bodies here (the verdict layer has
	// its own defensive guard for a challenge served with a 200).
	switch status {
	case http.StatusTooManyRequests, http.StatusForbidden, http.StatusServiceUnavailable:
		for _, m := range challengeMarkers {
			if bytes.Contains(body, m) {
				return true
			}
		}
	}
	return false
}

// IsChallengeBody is a defensive check for the verdict layer: if a page came
// back 200 but its body is actually a Cloudflare challenge (rare, but
// possible depending on CF config), we must not misread the absence of the
// realift tags as "SDK disabled". Exported for use from the verdict package.
func IsChallengeBody(body []byte) bool {
	// Require two independent markers to avoid false positives on real
	// product copy that happens to contain one phrase.
	hits := 0
	for _, m := range challengeMarkers {
		if bytes.Contains(body, m) {
			hits++
			if hits >= 2 {
				return true
			}
		}
	}
	return false
}
