//go:build windows

package notify

import "syscall"

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procSetThreadExecState = kernel32.NewProc("SetThreadExecutionState")
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// StartKeepAwake tells Windows to suppress idle/system sleep for the
// duration of the run via SetThreadExecutionState. Call the returned stop
// func when done to restore normal power management.
func StartKeepAwake() func() {
	_, _, _ = procSetThreadExecState.Call(uintptr(esContinuous | esSystemRequired))
	return func() {
		_, _, _ = procSetThreadExecState.Call(uintptr(esContinuous))
	}
}
