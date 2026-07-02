package ui

import (
	"fmt"
	"sync"
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
//
// All drawing goes through a mutex because Note (called from a fetch
// goroutine when a Cloudflare cooldown starts) and Add (called from the
// result-collector goroutine) both write to stdout concurrently.
type ProductBar struct {
	mu                         sync.Mutex
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
	p.mu.Lock()
	defer p.mu.Unlock()
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

// Note clears the live bar lines, prints a permanent message (it scrolls
// into scrollback), and lets the next Add redraw the bar fresh below it.
// Used to explain a pause — e.g. a Cloudflare cooldown — so a legitimate
// multi-minute wait doesn't look like a frozen bar.
func (p *ProductBar) Note(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.linesPrinted {
		// cursor is on the stats line; clear it, go up, clear the bar line.
		fmt.Print("\r\x1b[2K\x1b[1A\r\x1b[2K")
		p.linesPrinted = false
	}
	fmt.Println(msg)
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
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.linesPrinted {
		fmt.Print("\x1b[1A\r")
	}
	_ = p.bar.Finish()
	fmt.Print("\n\r\n")
}
