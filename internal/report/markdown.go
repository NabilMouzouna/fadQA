// Package report renders a single Markdown QA report from a run's results.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/realift/fad-qa/internal/verdict"
)

// Input bundles everything needed to render one run's report.
type Input struct {
	GeneratedAt   time.Time
	StoreURL      string
	CanonicalHost string
	AppType       string
	Mode          string // "full" | "quick"
	EnumMethod    string
	TotalProducts int
	Results       []verdict.ProductResult
	StoreFindings []string
	NewProducts   int
	GoneProducts  int
}

// Write renders the report and writes it to outDir, returning the path.
func Write(outDir string, in Input) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("report dir: %w", err)
	}
	path := filepath.Join(outDir, fileName(in))
	if err := os.WriteFile(path, []byte(Render(in)), 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	return path, nil
}

func fileName(in Input) string {
	host := sanitize(in.CanonicalHost)
	app := sanitize(strings.ToLower(in.AppType))
	date := in.GeneratedAt.Format("2006-01-02")
	return fmt.Sprintf("%s__%s__%s.md", host, app, date)
}

var fileNameReplacer = strings.NewReplacer("https://", "", "http://", "", "/", "_", ":", "_", " ", "_")

func sanitize(s string) string {
	return fileNameReplacer.Replace(s)
}

// Render builds the full Markdown document as a string.
func Render(in Input) string {
	var b strings.Builder

	counts := Tally(in.Results)
	total := len(in.Results)
	failTotal := FailTotal(counts)

	fmt.Fprintf(&b, "# Realift Button QA Report\n\n")
	fmt.Fprintf(&b, "- **Date:** %s\n", in.GeneratedAt.Format("2006-01-02 15:04 MST"))
	fmt.Fprintf(&b, "- **Store:** %s\n", in.StoreURL)
	if in.CanonicalHost != "" && in.CanonicalHost != in.StoreURL {
		fmt.Fprintf(&b, "- **Canonical host:** %s\n", in.CanonicalHost)
	}
	fmt.Fprintf(&b, "- **App type:** %s\n", in.AppType)
	fmt.Fprintf(&b, "- **Mode:** %s\n", in.Mode)
	fmt.Fprintf(&b, "- **Enumeration method:** %s\n", orDash(in.EnumMethod))
	fmt.Fprintf(&b, "- **Products discovered:** %d\n", in.TotalProducts)
	fmt.Fprintf(&b, "- **Products tested this run:** %d\n\n", total)

	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "| Result | Count |\n|---|---|\n")
	fmt.Fprintf(&b, "| PASS | %d |\n", counts[verdict.PASS])
	fmt.Fprintf(&b, "| FAIL (total) | %d |\n", failTotal)
	fmt.Fprintf(&b, "| &nbsp;&nbsp;— SDK not enabled | %d |\n", counts[verdict.FailSDKOff])
	fmt.Fprintf(&b, "| &nbsp;&nbsp;— button block missing on template | %d |\n", counts[verdict.FailNoButtonBlock])
	fmt.Fprintf(&b, "| &nbsp;&nbsp;— not included (keywords) | %d |\n", counts[verdict.FailNotIncluded])
	fmt.Fprintf(&b, "| &nbsp;&nbsp;— excluded by keyword | %d |\n", counts[verdict.FailExcluded])
	fmt.Fprintf(&b, "| SKIP — correctly out of scope | %d |\n", counts[verdict.SkipNotRelevant])
	fmt.Fprintf(&b, "| ERROR — could not test | %d |\n", counts[verdict.Errored])
	if in.NewProducts > 0 || in.GoneProducts > 0 {
		fmt.Fprintf(&b, "| New products since last run | %d |\n", in.NewProducts)
		fmt.Fprintf(&b, "| Removed products since last run | %d |\n", in.GoneProducts)
	}
	b.WriteString("\n")

	if len(in.StoreFindings) > 0 {
		fmt.Fprintf(&b, "## Store-level findings\n\n")
		for _, f := range in.StoreFindings {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	failing := filterFail(in.Results)
	renderTable(&b, fmt.Sprintf("Failing products (%d)", len(failing)), failing)

	advisory := filterAdvisory(in.Results)
	if len(advisory) > 0 {
		renderTable(&b, fmt.Sprintf("Visible but worth reviewing (%d)", len(advisory)), advisory)
	}

	skipped := filterVerdict(in.Results, verdict.SkipNotRelevant)
	if len(skipped) > 0 {
		renderTable(&b, fmt.Sprintf("Skipped — correctly out of scope (%d)", len(skipped)), skipped)
	}

	errored := filterVerdict(in.Results, verdict.Errored)
	if len(errored) > 0 {
		renderTable(&b, fmt.Sprintf("Errors — could not test (%d)", len(errored)), errored)
	}

	return b.String()
}

// Tally counts results by verdict. Exported so other presentations (the
// terminal summary panel in internal/ui) can share the same counts as the
// Markdown report instead of recomputing them.
func Tally(results []verdict.ProductResult) map[verdict.Verdict]int {
	counts := map[verdict.Verdict]int{}
	for _, r := range results {
		counts[r.Verdict]++
	}
	return counts
}

// FailTotal sums every verdict bucket that counts as a failure, per
// Verdict.IsFail() — kept in one place so a new FAIL_* verdict can't
// silently fall out of the headline count.
func FailTotal(counts map[verdict.Verdict]int) int {
	total := 0
	for v, c := range counts {
		if v.IsFail() {
			total += c
		}
	}
	return total
}

func filterFail(results []verdict.ProductResult) []verdict.ProductResult {
	var out []verdict.ProductResult
	for _, r := range results {
		if r.Verdict.IsFail() {
			out = append(out, r)
		}
	}
	sortByHandle(out)
	return out
}

func filterVerdict(results []verdict.ProductResult, v verdict.Verdict) []verdict.ProductResult {
	var out []verdict.ProductResult
	for _, r := range results {
		if r.Verdict == v {
			out = append(out, r)
		}
	}
	sortByHandle(out)
	return out
}

func filterAdvisory(results []verdict.ProductResult) []verdict.ProductResult {
	var out []verdict.ProductResult
	for _, r := range results {
		if r.Advisory != "" {
			out = append(out, r)
		}
	}
	sortByHandle(out)
	return out
}

func sortByHandle(results []verdict.ProductResult) {
	sort.Slice(results, func(i, j int) bool { return results[i].Handle < results[j].Handle })
}

func renderTable(b *strings.Builder, heading string, rows []verdict.ProductResult) {
	fmt.Fprintf(b, "## %s\n\n", heading)
	if len(rows) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	b.WriteString("| Title | URL | Verdict | Reason | Suggested fix |\n|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
			escapeCell(orDash(r.Title)),
			escapeCell(r.URL),
			string(r.Verdict),
			escapeCell(r.Reason),
			escapeCell(orDash(r.SuggestedFix)),
		)
	}
	b.WriteString("\n")
}

func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
