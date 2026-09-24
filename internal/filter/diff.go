package filter

import (
	"strings"
)

// DiffFilter collapses noisy lockfile diffs into a summary line.
// It detects lines that are part of a diff for common lockfiles and replaces
// extensive changes with a concise summary.
type DiffFilter struct{}

// Apply processes input and returns filtered output.
func (d DiffFilter) Apply(input string) string {
	if input == "" {
		return ""
	}
	var out strings.Builder
	lines := strings.Split(input, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git") {
			// Potential lockfile diff
			parts := strings.Split(line, " ")
			var filename string
			if len(parts) >= 4 {
				filename = strings.TrimPrefix(parts[3], "b/")
			}
			if isLockfile(filename) {
				// Skip until next diff header or end
				var changeCount int
				j := i + 1
				for j < len(lines) && !strings.HasPrefix(lines[j], "diff --git") {
					if strings.HasPrefix(lines[j], "+") || strings.HasPrefix(lines[j], "-") {
						if !strings.HasPrefix(lines[j], "@@") {
							changeCount++
						}
					}
					j++
				}
				if changeCount >= 5 {
					out.WriteString("[Lockfile changed: ")
					out.WriteString(filename)
					out.WriteString(" (")
					out.WriteString(diffIntToString(changeCount))
					out.WriteString(" lines changed)]\n")
				} else {
				 // Output the lines as is (we didn't store them, but for simplicity we output original segment)
				 // To keep it simple, we'll just output the original lines (inefficient but ok for demo)
				 for k := i; k < j; k++ {
					 out.WriteString(lines[k])
					 out.WriteByte('\n')
				 }
				}
				i = j
				continue
			}
		}
		// Not a lockfile diff header, output line
		out.WriteString(line)
		out.WriteByte('\n')
		i++
	}
	result := out.String()
	if result != "" && result[len(result)-1] == '\n' && !strings.HasSuffix(input, "\n") {
		result = result[:len(result)-1]
	}
	return result
}

// isLockfile checks if filename is a known lockfile.
func isLockfile(name string) bool {
	name = strings.ToLower(name)
	switch name {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "cargo.lock",
		"poetry.lock", "Pipfile.lock", "go.sum":
		return true
	}
	return false
}

// diffIntToString converts integer to string.
func diffIntToString(i int) string {
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
