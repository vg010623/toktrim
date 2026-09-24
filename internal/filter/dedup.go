package filter

import (
	"strings"
)

// DedupFilter collapses 3 or more identical consecutive lines into a summary line.
// Example: three lines of "foo bar" become "[Repeated 3 times: foo bar]".
// In Go, we use a simple state machine over lines.
// Compared to TypeScript: Go's for loop over slices is idiomatic; we avoid mutation of input slice.
type DedupFilter struct{}

// Apply processes input string and returns deduplicated output.
func (d DedupFilter) Apply(input string) string {
	if input == "" {
		return ""
	}
	var out strings.Builder
	lines := strings.Split(input, "\n")
	var prevLine string
	var count int
	for _, line := range lines {
		if line == prevLine {
			count++
		} else {
			// Emit previous run
			if prevLine != "" {
				d.emitRun(&out, prevLine, count)
			}
			prevLine = line
			count = 1
		}
	}
	// Emit final run
	if prevLine != "" {
		d.emitRun(&out, prevLine, count)
	}
	result := out.String()
	// Remove trailing newline if we added extra (but keep if original had trailing newline)
	if result != "" && result[len(result)-1] == '\n' && !strings.HasSuffix(input, "\n") {
		result = result[:len(result)-1]
	}
	return result
}

// emitRun writes the run to out builder.
func (d DedupFilter) emitRun(out *strings.Builder, line string, count int) {
	if count >= 3 {
		out.WriteString("[Repeated ")
		out.WriteString(dedupIntToString(count))
		out.WriteString(" times: ")
		out.WriteString(line)
		out.WriteString("]\n")
	} else {
		for i := 0; i < count; i++ {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
}

// dedupIntToString converts integer to string without allocations (using Itob).
// In Go, we could use strconv.Itoa but we'll keep simple.
func dedupIntToString(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
