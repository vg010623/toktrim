package filter

import (
	"strconv"
	"strings"
)

// DedupFilter collapses 3 or more identical consecutive lines into a summary line.
// Example: three lines of "foo bar" become "[Repeated 3 times: foo bar]".
// In Go, we use a simple state machine over lines.
// Compared to TypeScript: Go's for loop over slices is idiomatic; we avoid mutation of input slice.
type DedupFilter struct{}

// Apply processes input string and returns deduplicated output.
func (d DedupFilter) Apply(input string) string {
	lines, trailing := splitLines(input)
	if len(lines) == 0 {
		return input
	}
	var out []string
	i := 0
	for i < len(lines) {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		out = append(out, collapseRun(lines[i], j-i)...)
		i = j
	}
	return joinLines(out, trailing)
}

// collapseRun turns a run of count identical lines into the lines to emit.
// Runs of three or more collapse to a single summary line; shorter runs are
// passed through so that small outputs are never rewritten.
func collapseRun(line string, count int) []string {
	if count < 3 || strings.TrimSpace(line) == "" {
		out := make([]string, count)
		for i := range out {
			out[i] = line
		}
		return out
	}
	return []string{"[Repeated " + strconv.Itoa(count) + " times: " + line + "]"}
}
