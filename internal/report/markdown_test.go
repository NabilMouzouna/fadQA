package report

import (
	"strings"
	"testing"
	"time"

	"github.com/realift/fad-qa/internal/verdict"
)

func TestRender_CountsAndSections(t *testing.T) {
	in := Input{
		GeneratedAt:   time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		StoreURL:      "https://example.com",
		CanonicalHost: "https://example.com",
		AppType:       "realfoot",
		Mode:          "full",
		EnumMethod:    "products.json",
		TotalProducts: 3,
		Results: []verdict.ProductResult{
			{Handle: "a", Title: "Running Shoes", URL: "https://example.com/products/a", Verdict: verdict.PASS},
			{Handle: "b", Title: "Wool | Socks", URL: "https://example.com/products/b", Verdict: verdict.FailNotIncluded, Reason: "no keyword", SuggestedFix: "add 'shoe'"},
			{Handle: "c", Title: "Odd Item", URL: "https://example.com/products/c", Verdict: verdict.PASS, Advisory: verdict.WarnUnexpectedShow, SuggestedFix: "review"},
		},
		StoreFindings: []string{"Test finding."},
	}

	out := Render(in)

	if !strings.Contains(out, "| PASS | 2 |") {
		t.Fatalf("expected PASS count of 2 in summary, got:\n%s", out)
	}
	if !strings.Contains(out, "| FAIL (total) | 1 |") {
		t.Fatalf("expected FAIL total of 1, got:\n%s", out)
	}
	if !strings.Contains(out, "Test finding.") {
		t.Fatalf("expected store finding to appear")
	}
	if !strings.Contains(out, "Wool \\| Socks") {
		t.Fatalf("expected pipe character in title to be escaped, got:\n%s", out)
	}
	if !strings.Contains(out, "Visible but worth reviewing (1)") {
		t.Fatalf("expected advisory section with 1 entry")
	}
	if strings.Contains(out, "Skipped") {
		t.Fatalf("did not expect a skipped section with zero skips")
	}
}

func TestRender_NoFailures(t *testing.T) {
	in := Input{
		GeneratedAt: time.Now(),
		Results: []verdict.ProductResult{
			{Handle: "a", Verdict: verdict.PASS},
		},
	}
	out := Render(in)
	if !strings.Contains(out, "Failing products (0)") {
		t.Fatalf("expected zero failing products heading, got:\n%s", out)
	}
	if !strings.Contains(out, "None.") {
		t.Fatalf("expected 'None.' body for empty failing table")
	}
}

func TestFileName_Sanitized(t *testing.T) {
	in := Input{
		GeneratedAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		CanonicalHost: "https://example.myshopify.com",
		AppType:       "realfoot",
	}
	name := fileName(in)
	if strings.ContainsAny(name, ":/") {
		t.Fatalf("expected sanitized filename with no : or /, got %q", name)
	}
	if !strings.HasSuffix(name, "2026-07-01.md") {
		t.Fatalf("expected date suffix, got %q", name)
	}
}
