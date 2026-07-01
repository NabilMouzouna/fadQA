package ui

import (
	"fmt"
	"math"
	"time"
)

// EstimateDuration gives a rough, upfront estimate of how long testing
// `count` products will take at the configured steady-state rate. It's
// necessarily approximate — actual throughput depends on the store's
// response times and whether the adaptive limiter has to back off — the
// live progress bar's own ETA (which adapts to observed throughput) is the
// more accurate figure once a run is underway.
func EstimateDuration(count int, ratePerSec float64) time.Duration {
	if ratePerSec <= 0 {
		ratePerSec = 1
	}
	seconds := math.Ceil(float64(count) / ratePerSec)
	return time.Duration(seconds) * time.Second
}

// FormatDuration renders a duration as a short human string, e.g. "1h 5m",
// "1m 30s", or "45s".
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
