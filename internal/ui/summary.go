package ui

import (
	"fmt"

	"github.com/realift/fad-qa/internal/report"
	"github.com/realift/fad-qa/internal/verdict"
)

// PrintSummary renders the same pass/fail/skip/error counts that go into
// the Markdown report's Summary table, surfaced immediately in the
// terminal so the operator doesn't have to open the file for the headline
// result.
func PrintSummary(results []verdict.ProductResult) {
	counts := report.Tally(results)
	failTotal := report.FailTotal(counts)

	Section("Summary")
	fmt.Printf("    %-24s %s\n", "Pass", Green(fmt.Sprint(counts[verdict.PASS])))
	fmt.Printf("    %-24s %s\n", "Fail", Red(fmt.Sprint(failTotal)))
	fmt.Printf("    %-24s %s\n", "Skip (out of scope)", Yellow(fmt.Sprint(counts[verdict.SkipNotRelevant])))
	fmt.Printf("    %-24s %s\n", "Error", Gray(fmt.Sprint(counts[verdict.Errored])))
}
