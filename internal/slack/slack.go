// Package slack posts a summarized run report to a Slack Incoming Webhook.
// Configuration is read from a .env file sitting next to the binary, so a
// distributed binary can be pointed at a channel without recompiling and
// without the secret ever living in the repo. If no .env / webhook is
// present, Slack reporting is simply skipped.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config holds the resolved Slack Incoming Webhook settings.
type Config struct {
	WebhookURL string
	Channel    string // informational only; incoming webhooks post to a fixed channel
}

// Load reads `.env` from dir (the directory next to the binary) and returns a
// Config if a usable webhook token is present. ok=false means "no Slack
// configured" — a normal, non-error state the caller should treat as "skip
// Slack". A malformed file is reported via err.
func Load(dir string) (cfg *Config, ok bool, err error) {
	env, err := parseDotEnv(filepath.Join(dir, ".env"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	token := firstNonEmpty(env["SLACK-WEBHOOK-TOKEN"], env["SLACK_WEBHOOK_TOKEN"])
	if token == "" {
		return nil, false, nil
	}
	return &Config{
		WebhookURL: webhookURL(token),
		Channel:    firstNonEmpty(env["SLACK-CHANNEL"], env["SLACK_CHANNEL"]),
	}, true, nil
}

// webhookURL normalizes whatever form the token was stored in into a full
// Incoming Webhook URL: a complete https URL is used as-is; a bare
// hooks.slack.com host gets a scheme; anything else is treated as the
// services path token.
func webhookURL(token string) string {
	t := strings.TrimSpace(token)
	switch {
	case strings.HasPrefix(t, "https://"):
		return t
	case strings.HasPrefix(t, "http://"):
		return t
	case strings.HasPrefix(t, "hooks.slack.com"):
		return "https://" + t
	default:
		return "https://hooks.slack.com/services/" + strings.TrimPrefix(t, "/")
	}
}

// Failure is one failing product row included in the Slack summary.
type Failure struct {
	Title   string
	URL     string
	Verdict string
	Reason  string
}

// Report is the summarized run data posted to Slack.
type Report struct {
	StoreName string
	StoreURL  string
	AppType   string
	Mode      string
	Date      time.Time
	Products  int

	Pass    int
	Fail    int
	Skip    int
	Errored int

	Findings []string
	Failures []Failure // already capped by the caller
}

const maxSlackFailures = 15

// Post builds and sends the summary to the configured webhook.
func (c *Config) Post(ctx context.Context, client *http.Client, r Report) error {
	payload := buildPayload(r)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
