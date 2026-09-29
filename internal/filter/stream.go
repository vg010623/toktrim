// Package filter contains the streaming line filters toktrim applies to
// command output.
//
// Filters are pull-free: the pipeline pushes one Line at a time into
// Process, and the filter pushes zero or more Lines onward via emit. A filter
// that needs to see a whole block (a test summary, a docker build step) may
// buffer that block, but must never buffer the entire output: toktrim has to
// keep working when Claude Code's two-minute Bash timeout kills the command
// halfway through.
package filter

import "strings"

// Line is one line of command output travelling through the pipeline.
//
// Text never contains the terminating newline. Protected marks a line that
// carries failure information (or sits next to one); no filter may drop or
// rewrite a protected line.
type Line struct {
	Text      string
	Protected bool
}

// Emit receives the lines a filter produces.
type Emit func(Line)

// LineFilter transforms a stream of lines.
type LineFilter interface {
	// Name identifies the filter in diagnostics and config.
	Name() string
	// Process handles one input line.
	Process(l Line, emit Emit)
	// Flush releases anything the filter is holding. Called once, at end of
	// stream, before the pipeline finishes.
	Flush(emit Emit)
	// Changed reports whether the filter dropped or rewrote anything. The
	// pipeline uses this to decide whether a raw log is needed.
	Changed() bool
}

// tracker is embedded by filters to record that they altered the stream.
type tracker struct{ changed bool }

func (t *tracker) markChanged() { t.changed = true }

// Changed implements part of LineFilter.
func (t *tracker) Changed() bool { return t.changed }

// blank reports whether a line has no visible content.
func blank(s string) bool { return strings.TrimSpace(s) == "" }

// plural returns word, pluralised when n is not 1.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
