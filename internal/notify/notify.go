// Package notify handles the optional, cross-platform "done" signals
// (sound, desktop notification) and best-effort keep-awake during a run.
// Every function here is best-effort: a failure (e.g. no notification
// daemon on a headless box) must never fail the run itself.
package notify

import (
	"fmt"

	"github.com/gen2brain/beeep"
)

// Done plays a sound and/or shows a desktop notification announcing that a
// run finished. Errors are swallowed; sound falls back to a terminal bell.
func Done(title, message string, sound, desktop bool) {
	if desktop {
		_ = beeep.Notify(title, message, nil)
	}
	if sound {
		if err := beeep.Beep(beeep.DefaultFreq, beeep.DefaultDuration); err != nil {
			fmt.Print("\a")
		}
	}
}
