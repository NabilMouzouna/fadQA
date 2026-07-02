package slack

import (
	"fmt"
	"strings"
)

// Slack Block Kit payload types (only the subset we use). Built as structs
// and JSON-marshalled so product titles/reasons with quotes or newlines are
// escaped correctly rather than string-concatenated.
type payload struct {
	Text   string  `json:"text"` // notification/fallback text
	Blocks []block `json:"blocks,omitempty"`
}

type block struct {
	Type     string    `json:"type"`
	Text     *textObj  `json:"text,omitempty"`
	Fields   []textObj `json:"fields,omitempty"`
	Elements []textObj `json:"elements,omitempty"`
}

type textObj struct {
	Type string `json:"type"` // "mrkdwn" or "plain_text"
	Text string `json:"text"`
}

func mrkdwn(s string) *textObj { return &textObj{Type: "mrkdwn", Text: s} }

func buildPayload(r Report) payload {
	title := r.StoreName
	if title == "" {
		title = r.StoreURL
	}

	statusEmoji := ":white_check_mark:"
	if r.Fail > 0 || r.Errored > 0 {
		statusEmoji = ":x:"
	}

	fallback := fmt.Sprintf("Realift QA — %s: %d passed, %d failed, %d skipped, %d error",
		title, r.Pass, r.Fail, r.Skip, r.Errored)

	blocks := []block{
		{Type: "header", Text: &textObj{Type: "plain_text", Text: truncate(fmt.Sprintf("%s Realift QA — %s", emojiForHeader(r), title), 150)}},
		{Type: "section", Fields: []textObj{
			{Type: "mrkdwn", Text: fmt.Sprintf("*Store:*\n<%s|%s>", r.StoreURL, esc(title))},
			{Type: "mrkdwn", Text: fmt.Sprintf("*App:*\n%s", esc(r.AppType))},
			{Type: "mrkdwn", Text: fmt.Sprintf("*Mode:*\n%s", esc(r.Mode))},
			{Type: "mrkdwn", Text: fmt.Sprintf("*Products:*\n%d", r.Products)},
			{Type: "mrkdwn", Text: fmt.Sprintf("*Date:*\n%s", r.Date.Format("2006-01-02 15:04 MST"))},
			{Type: "mrkdwn", Text: fmt.Sprintf("*Status:*\n%s", status(r))},
		}},
		{Type: "section", Text: mrkdwn(fmt.Sprintf(
			"%s *Results:*  :white_check_mark: %d passed   :x: %d failed   :fast_forward: %d skipped   :warning: %d error",
			statusEmoji, r.Pass, r.Fail, r.Skip, r.Errored))},
	}

	if len(r.Findings) > 0 {
		var b strings.Builder
		b.WriteString("*Findings:*\n")
		for _, f := range r.Findings {
			b.WriteString("• " + esc(f) + "\n")
		}
		blocks = append(blocks, block{Type: "section", Text: mrkdwn(truncate(b.String(), 2900))})
	}

	if len(r.Failures) > 0 {
		blocks = append(blocks, block{Type: "divider"})
		var b strings.Builder
		n := len(r.Failures)
		shown := n
		if shown > maxSlackFailures {
			shown = maxSlackFailures
		}
		b.WriteString(fmt.Sprintf("*Failing products (%d):*\n", n))
		for _, f := range r.Failures[:shown] {
			name := f.Title
			if name == "" {
				name = f.URL
			}
			b.WriteString(fmt.Sprintf("• <%s|%s> — `%s`\n", f.URL, esc(truncate(name, 80)), esc(f.Verdict)))
		}
		if n > shown {
			b.WriteString(fmt.Sprintf("_…and %d more — see the full Markdown report._\n", n-shown))
		}
		blocks = append(blocks, block{Type: "section", Text: mrkdwn(truncate(b.String(), 2900))})
	}

	return payload{Text: fallback, Blocks: blocks}
}

func status(r Report) string {
	if r.Fail > 0 {
		return fmt.Sprintf(":x: %d failing", r.Fail)
	}
	if r.Errored > 0 {
		return fmt.Sprintf(":warning: %d errored", r.Errored)
	}
	return ":white_check_mark: all passing"
}

func emojiForHeader(r Report) string {
	if r.Fail > 0 {
		return "🔴"
	}
	if r.Errored > 0 {
		return "🟠"
	}
	return "🟢"
}

// esc neutralizes Slack mrkdwn control characters in user-supplied text.
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
