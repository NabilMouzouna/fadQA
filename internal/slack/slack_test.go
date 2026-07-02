package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDotEnv(t *testing.T) {
	dir := t.TempDir()
	content := "# a comment\n\nSLACK-CHANNEL = \"#qa\"\nexport SLACK-WEBHOOK-TOKEN='T1/B2/secret'\nMALFORMED\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := parseDotEnv(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env["SLACK-CHANNEL"] != "#qa" {
		t.Fatalf("channel: got %q", env["SLACK-CHANNEL"])
	}
	if env["SLACK-WEBHOOK-TOKEN"] != "T1/B2/secret" {
		t.Fatalf("token: got %q", env["SLACK-WEBHOOK-TOKEN"])
	}
}

func TestLoad_AbsentIsNotError(t *testing.T) {
	_, ok, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing .env should not be an error, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false when no .env present")
	}
}

func TestLoad_ReadsWebhook(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SLACK-WEBHOOK-TOKEN=T1/B2/secret\nSLACK-CHANNEL=#qa\n"), 0o600)
	cfg, ok, err := Load(dir)
	if err != nil || !ok {
		t.Fatalf("expected config, ok=%v err=%v", ok, err)
	}
	if cfg.WebhookURL != "https://hooks.slack.com/services/T1/B2/secret" {
		t.Fatalf("unexpected webhook URL: %q", cfg.WebhookURL)
	}
	if cfg.Channel != "#qa" {
		t.Fatalf("unexpected channel: %q", cfg.Channel)
	}
}

func TestWebhookURL(t *testing.T) {
	cases := map[string]string{
		"https://hooks.slack.com/services/T/B/x": "https://hooks.slack.com/services/T/B/x",
		"hooks.slack.com/services/T/B/x":         "https://hooks.slack.com/services/T/B/x",
		"T/B/x":                                  "https://hooks.slack.com/services/T/B/x",
		"/T/B/x":                                 "https://hooks.slack.com/services/T/B/x",
	}
	for in, want := range cases {
		if got := webhookURL(in); got != want {
			t.Errorf("webhookURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestBuildPayload_EscapesAndStructure(t *testing.T) {
	r := Report{
		StoreName: "Acme <Shoes>",
		StoreURL:  "https://acme.example.com",
		AppType:   "realfoot",
		Mode:      "full",
		Date:      time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
		Products:  100,
		Pass:      80, Fail: 15, Skip: 3, Errored: 2,
		Findings: []string{"No include keywords configured"},
		Failures: []Failure{
			{Title: "Sock & Co", URL: "https://acme.example.com/products/x", Verdict: "FAIL_NOT_INCLUDED", Reason: "hidden"},
		},
	}
	p := buildPayload(r)
	if _, err := json.Marshal(p); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(p.Text, "80 passed") {
		t.Fatalf("fallback text should carry counts, got %q", p.Text)
	}
	if len(p.Blocks) == 0 {
		t.Fatalf("expected blocks")
	}
	// The Store field runs user text through esc() (mrkdwn-safe), so the
	// angle brackets in the name must be entity-escaped there.
	var storeField string
	for _, b := range p.Blocks {
		for _, f := range b.Fields {
			if strings.HasPrefix(f.Text, "*Store:*") {
				storeField = f.Text
			}
		}
	}
	if !strings.Contains(storeField, "Acme &lt;Shoes&gt;") {
		t.Fatalf("expected store name esc()-escaped in the Store field, got %q", storeField)
	}
}

func TestBuildPayload_CapsFailures(t *testing.T) {
	var fails []Failure
	for i := 0; i < 40; i++ {
		fails = append(fails, Failure{Title: "p", URL: "https://x/p", Verdict: "FAIL_NOT_INCLUDED"})
	}
	p := buildPayload(Report{Failures: fails, Fail: 40})
	raw, _ := json.Marshal(p)
	if !strings.Contains(string(raw), "and 25 more") {
		t.Fatalf("expected truncation note for >%d failures", maxSlackFailures)
	}
}

func TestPost(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := &Config{WebhookURL: srv.URL}
	err := cfg.Post(context.Background(), srv.Client(), Report{StoreName: "Acme", Pass: 1})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(gotBody) == 0 || !strings.Contains(string(gotBody), "Acme") {
		t.Fatalf("expected posted body to contain store name, got %q", string(gotBody))
	}
}

func TestPost_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	cfg := &Config{WebhookURL: srv.URL}
	if err := cfg.Post(context.Background(), srv.Client(), Report{}); err == nil {
		t.Fatalf("expected error on non-2xx")
	}
}
