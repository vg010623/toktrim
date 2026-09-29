package filter

import (
	"regexp"
	"strings"
)

// ansiEscape matches the escape sequences that appear in terminal output:
// CSI sequences (colour, cursor movement), OSC sequences (window titles,
// hyperlinks) and the short two-byte escapes.
var ansiEscape = regexp.MustCompile(
	"\x1b\\[[0-9;:?]*[ -/]*[@-~]" + // CSI
		"|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)" + // OSC
		"|\x1b[PX^_][^\x1b]*\x1b\\\\" + // DCS/SOS/PM/APC
		"|\x1b[@-Z\\\\-_]", // two-byte escapes
)

// StripANSI removes terminal escape sequences from s.
func StripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	return ansiEscape.ReplaceAllString(s, "")
}

// ResolveCarriageReturns reduces a line that was redrawn in place to the text
// that was left on screen.
//
// A progress bar writes "10%\r50%\r100%" as one line; only the last state
// matters. Trailing empty segments are ignored, because tools often finish by
// returning the cursor to the start of the line without writing anything more.
func ResolveCarriageReturns(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	segments := strings.Split(s, "\r")
	for i := len(segments) - 1; i >= 0; i-- {
		if strings.TrimSpace(segments[i]) != "" {
			return segments[i]
		}
	}
	return ""
}

// progressShaped matches a line whose only content is transient progress.
var progressShaped = regexp.MustCompile(
	`^[\s\pS\pC]*[-\\|/*.oO°⠁-⣿◐◓◑◒▁-█░▒▓■□▪▫=#>·⋅]+[\s\pS\pC]*$` + // bars and spinners
		`|\b\d{1,3}(\.\d+)?%` + // any percentage readout
		`|\bETA[: ]` +
		`|\b\d+(\.\d+)?\s?[kKmMgG]?i?[bB]\s*/\s*\d+(\.\d+)?\s?[kKmMgG]?i?[bB]\b`, // 1.2MB / 5.0MB
)

// IsProgressLine reports whether a line is a transient progress readout.
func IsProgressLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	return progressShaped.MatchString(t)
}

// ANSI strips escape sequences, resolves in-place redraws, and collapses a run
// of progress readouts down to the last one.
//
// It runs ahead of Guard so that later filters match on clean text rather than
// on text with colour codes spliced through it. It never drops a line that
// carries failure information, and it never drops the last line of a progress
// run, so the final state of a download or build bar is always visible.
type ANSI struct {
	tracker

	pendingProgress *Line
}

// NewANSI returns an ANSI filter.
func NewANSI() *ANSI { return &ANSI{} }

// Name implements LineFilter.
func (a *ANSI) Name() string { return "ansi" }

// Process implements LineFilter.
func (a *ANSI) Process(l Line, emit Emit) {
	clean := ResolveCarriageReturns(StripANSI(l.Text))
	if clean != l.Text {
		a.markChanged()
		l.Text = clean
	}

	if !l.Protected && IsProgressLine(l.Text) {
		// Hold it: if another progress line follows, this one was overwritten.
		if a.pendingProgress != nil {
			a.markChanged()
		}
		held := l
		a.pendingProgress = &held
		return
	}

	a.flushProgress(emit)
	emit(l)
}

// Flush implements LineFilter.
func (a *ANSI) Flush(emit Emit) { a.flushProgress(emit) }

func (a *ANSI) flushProgress(emit Emit) {
	if a.pendingProgress == nil {
		return
	}
	l := *a.pendingProgress
	a.pendingProgress = nil
	if strings.TrimSpace(l.Text) != "" {
		emit(l)
	} else {
		a.markChanged()
	}
}
