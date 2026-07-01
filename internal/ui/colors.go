// Package ui renders fad-qa's structured terminal output: phase headers,
// a config summary, a live progress bar during testing, and a final
// colored results panel. Colors and the progress bar are automatically
// disabled when stdout isn't a terminal (piped output, CI logs) or when
// NO_COLOR is set, so redirected output stays clean plain text.
package ui

import (
	"os"

	"golang.org/x/term"
)

var colorEnabled = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTTY()
}

// IsTTY reports whether stdout is an interactive terminal.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
)

func paint(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

func Bold(s string) string   { return paint(ansiBold, s) }
func Red(s string) string    { return paint(ansiRed, s) }
func Green(s string) string  { return paint(ansiGreen, s) }
func Yellow(s string) string { return paint(ansiYellow, s) }
func Blue(s string) string   { return paint(ansiBlue, s) }
func Cyan(s string) string   { return paint(ansiCyan, s) }
func Gray(s string) string   { return paint(ansiGray, s) }
