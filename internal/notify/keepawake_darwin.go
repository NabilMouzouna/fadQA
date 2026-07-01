//go:build darwin

package notify

import "os/exec"

// StartKeepAwake spawns `caffeinate -i` for the duration of the run to
// prevent macOS idle sleep from pausing a long crawl. Call the returned
// stop func when the run finishes to let the system sleep normally again.
func StartKeepAwake() func() {
	cmd := exec.Command("caffeinate", "-i")
	if err := cmd.Start(); err != nil {
		return func() {}
	}
	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}
