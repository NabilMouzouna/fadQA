package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetPage_RetriesOnQuota429ThenSucceeds(t *testing.T) {
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		if n <= 2 {
			// Genuine Shopify quota throttle: Retry-After, no CF challenge body.
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	limiter := NewLimiter(4, 100)
	result, err := GetPage(context.Background(), NewClient(), limiter, srv.URL)
	if err != nil {
		t.Fatalf("expected eventual success, got error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.StatusCode)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (2 throttled + 1 success), got %d", calls)
	}
}

func TestGetPage_ExhaustsAfterQuota429Storm(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: waits through the retry budget")
	}
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	limiter := NewLimiter(4, 100)
	_, err := GetPage(context.Background(), NewClient(), limiter, srv.URL)
	if err == nil {
		t.Fatalf("expected an error after exhausting the retry budget")
	}
	if got := atomic.LoadInt64(&calls); got != int64(maxAttempts) {
		t.Fatalf("expected exactly %d attempts, got %d", maxAttempts, got)
	}
}

func TestGetPage_CloudflareChallengeGivesUp(t *testing.T) {
	// Every response is a Cloudflare challenge. With shrunken cooldowns the
	// limiter should escalate through its episode budget and give up with
	// ErrBlocked, rather than retrying forever.
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("<html><body>Verifying your connection...</body></html>"))
	}))
	defer srv.Close()

	limiter := NewLimiter(4, 100)
	limiter.mu.Lock()
	limiter.baseCooldown = 5 * time.Millisecond
	limiter.maxCooldown = 20 * time.Millisecond
	limiter.mu.Unlock()

	_, err := GetPage(context.Background(), NewClient(), limiter, srv.URL)
	if err == nil {
		t.Fatalf("expected an error when the store keeps challenging")
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
	if limiter.TotalChallenges() == 0 {
		t.Fatalf("expected challenge episodes to be counted")
	}
}

func TestGetPage_RecoversAfterChallengeClears(t *testing.T) {
	// Challenge the first couple of requests, then serve normally — the
	// crawl should ride out the (shrunken) cooldown and succeed.
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		if n <= 2 {
			w.Header().Set("cf-mitigated", "challenge")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Verifying your connection..."))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	limiter := NewLimiter(4, 100)
	limiter.mu.Lock()
	limiter.baseCooldown = 5 * time.Millisecond
	limiter.maxCooldown = 20 * time.Millisecond
	limiter.mu.Unlock()

	res, err := GetPage(context.Background(), NewClient(), limiter, srv.URL)
	if err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after recovery, got %d", res.StatusCode)
	}
}

func TestIsCloudflareChallenge(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("cf-mitigated", "challenge")
	if !isCloudflareChallenge(429, hdr, nil) {
		t.Fatalf("cf-mitigated header should signal a challenge")
	}
	if !isCloudflareChallenge(429, http.Header{}, []byte("<title>Just a moment...</title>")) {
		t.Fatalf("challenge body on a 429 should be detected")
	}
	if isCloudflareChallenge(200, http.Header{}, []byte("Just a moment while we load")) {
		t.Fatalf("a 200 body should not be treated as a challenge here")
	}
	if isCloudflareChallenge(429, http.Header{}, []byte(`{"error":"throttled"}`)) {
		t.Fatalf("a plain quota 429 without markers is not a challenge")
	}
}

func TestIsChallengeBody(t *testing.T) {
	if !IsChallengeBody([]byte(`cf-mitigated ... challenge-platform`)) {
		t.Fatalf("expected two-marker challenge body to be detected")
	}
	if IsChallengeBody([]byte(`Just a moment, your order is processing`)) {
		t.Fatalf("a single incidental phrase should not trip the guard")
	}
}
