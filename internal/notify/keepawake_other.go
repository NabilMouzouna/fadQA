//go:build !darwin && !windows

package notify

// StartKeepAwake is a no-op on platforms without a specific implementation.
func StartKeepAwake() func() {
	return func() {}
}
