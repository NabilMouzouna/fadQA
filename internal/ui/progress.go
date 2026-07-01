package ui

import (
	"fmt"
	"time"

	"github.com/schollz/progressbar/v3"
)

// ProductBar renders two live lines for the testing phase: the bar itself
// (percentage, count, elapsed time) on top, and a spaced-out pass/fail/
// skip/error tally below it. Elapsed time is shown rather than a predicted
// ETA — under real-world rate limiting, throughput can fall off a cliff
// mid-run (a store throttling hard, then a patient retry pass), which
// makes any simple linear ETA prediction actively misleading rather than
// just imprecise.
type ProductBar struct {
	bar                        *progressbar.ProgressBar
	pass, fail, skip, errCount int
	linesPrinted               bool
}

// NewProductBar creates a bar for `total` products. Colors follow the same
// NO_COLOR / non-TTY detection as the rest of the package.
func NewProductBar(total int) *ProductBar {
	bar := progressbar.NewOptions(total,
		progressbar.OptionSetWidth(30),
		progressbar.OptionShowCount(),
		progressbar.OptionSetElapsedTime(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionEnableColorCodes(colorEnabled),
		progressbar.OptionThrottle(65*time.Millisecond),
	)
	return &ProductBar{bar: bar}
}

// Add records one completed product's outcome and redraws both lines.
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

	if p.linesPrinted {
		fmt.Print("\x1b[1A\r") // back up to the bar's line
	}
	_ = p.bar.Add(1) // redraws the bar in place
	fmt.Print("\n\r\x1b[2K")
	fmt.Print(p.statsLine())
	p.linesPrinted = true
}

func (p *ProductBar) statsLine() string {
	return fmt.Sprintf("    %s     %s     %s     %s",
		Green(fmt.Sprintf("%d pass", p.pass)),
		Red(fmt.Sprintf("%d fail", p.fail)),
		Yellow(fmt.Sprintf("%d skip", p.skip)),
		Gray(fmt.Sprintf("%d err", p.errCount)),
	)
}

// Finish renders the bar's final state and leaves both lines in place,
// moving the cursor past them for whatever prints next.
func (p *ProductBar) Finish() {
	if p.linesPrinted {
		fmt.Print("\x1b[1A\r")
	}
	_ = p.bar.Finish()
	fmt.Print("\n\r\n")
}
