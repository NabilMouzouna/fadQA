package ui

import (
	"fmt"
	"time"

	"github.com/schollz/progressbar/v3"
)

// ProductBar is a live progress bar for the per-product testing phase. Its
// description shows a running pass/fail/skip/error tally so the operator
// gets a sense of the outcome mix without waiting for the final report.
type ProductBar struct {
	bar                        *progressbar.ProgressBar
	pass, fail, skip, errCount int
}

// NewProductBar creates a bar for `total` products. Colors follow the same
// NO_COLOR / non-TTY detection as the rest of the package.
func NewProductBar(total int) *ProductBar {
	bar := progressbar.NewOptions(total,
		progressbar.OptionSetWidth(30),
		progressbar.OptionShowCount(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionEnableColorCodes(colorEnabled),
		progressbar.OptionThrottle(65*time.Millisecond),
	)
	return &ProductBar{bar: bar}
}

// Add records one completed product's outcome and redraws the bar.
func (p *ProductBar) Add(kind string) {
	switch kind {
	case "pass":
		p.pass++
	case "fail":
		p.fail++
	case "skip":
		p.skip++
	case "error":
		p.errCount++
	}
	p.bar.Describe(fmt.Sprintf("%s %s %s %s",
		Green(fmt.Sprintf("%d pass", p.pass)),
		Red(fmt.Sprintf("%d fail", p.fail)),
		Yellow(fmt.Sprintf("%d skip", p.skip)),
		Gray(fmt.Sprintf("%d err", p.errCount)),
	))
	_ = p.bar.Add(1)
}

// Finish completes the bar and clears its line.
func (p *ProductBar) Finish() {
	_ = p.bar.Finish()
}
