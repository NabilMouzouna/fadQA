package ui

import "github.com/realift/fad-qa/internal/verdict"

// VerdictKind buckets a Verdict into one of five coarse outcome kinds,
// shared by the live progress bar and the final summary panel so both
// stay in visual sync.
func VerdictKind(v verdict.Verdict) string {
	switch {
	case v == verdict.PASS:
		return "pass"
	case v.IsFail():
		return "fail"
	case v == verdict.SkipNotRelevant:
		return "skip"
	case v == verdict.Errored:
		return "error"
	default:
		return "other"
	}
}

// VerdictLabel renders a short, colored label for a verdict, used in
// verbose per-product output.
func VerdictLabel(v verdict.Verdict) string {
	switch VerdictKind(v) {
	case "pass":
		return Green(string(v))
	case "fail":
		return Red(string(v))
	case "skip":
		return Yellow(string(v))
	case "error":
		return Gray(string(v))
	default:
		return string(v)
	}
}
