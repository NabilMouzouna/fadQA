package ui

import "fmt"

// Section prints a bold header marking a distinct block of output (e.g.
// "Configuration", "Summary").
func Section(title string) {
	fmt.Println()
	fmt.Println(Bold(Cyan("=== " + title + " ===")))
}

// Step prints a numbered phase marker (e.g. "Step 1/3: Enumerating store").
func Step(n, total int, title string) {
	fmt.Printf("\n%s %s\n", Bold(Blue(fmt.Sprintf("==> Step %d/%d:", n, total))), title)
}

// KV prints an indented, aligned key/value line under a Section or Step.
func KV(key, value string) {
	fmt.Printf("    %-12s %s\n", key, value)
}

// Info prints a plain indented status line.
func Info(format string, args ...any) {
	fmt.Printf("    %s\n", fmt.Sprintf(format, args...))
}

// Success prints an indented, green-tagged status line.
func Success(format string, args ...any) {
	fmt.Printf("    %s %s\n", Green("[OK]"), fmt.Sprintf(format, args...))
}

// Warn prints an indented, yellow-tagged status line.
func Warn(format string, args ...any) {
	fmt.Printf("    %s %s\n", Yellow("[!]"), fmt.Sprintf(format, args...))
}

// Fail prints an indented, red-tagged status line.
func Fail(format string, args ...any) {
	fmt.Printf("    %s %s\n", Red("[X]"), fmt.Sprintf(format, args...))
}
