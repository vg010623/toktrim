package filter

import (
	"strings"
)

// TestRunnerFilter compresses verbose test output while preserving failures and stack traces.
// In Go, we use slices and strings.Builder for efficient string manipulation.
// Compared to TypeScript: Go strings are immutable, so we build results with strings.Builder.
// Go's range over strings yields runes (Unicode code points), not bytes.
type TestRunnerFilter struct{}

// Apply processes the input string and returns filtered output.
// Heuristics:
// - Keep lines that look like failures (contain "FAIL", "error", "panic", "stacktrace")
// - Keep lines that are part of a stack trace (indented or starting with "at ")
// - Drop lines ending with "... ok" (passing tests)
// - Drop lines with just checkmarks or dots (progress indicators)
// - Drop download/compilation progress lines (often contain "downloading", "compiling")
// - Also drop lines that indicate a passing test (contain "PASS" but not "FAIL")
func (t TestRunnerFilter) Apply(input string) string {
	if input == "" {
		return ""
	}
	var out strings.Builder
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		if t.shouldKeep(line) {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	// Remove trailing newline if we added one (but keep if original had it)
	result := out.String()
	if result != "" && result[len(result)-1] == '\n' && !strings.HasSuffix(input, "\n") {
		result = result[:len(result)-1]
	}
	return result
}

// shouldKeep returns true if the line should be kept in output.
func (t TestRunnerFilter) shouldKeep(line string) bool {
	// Trim spaces for checks
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		// Keep empty lines for readability? We'll keep them.
		return true
	}
	lower := strings.ToLower(trimmed)

	// Keep failure indicators
	if strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "panic") {
		return true
	}
	// Keep stack trace lines (common patterns)
	if strings.HasPrefix(trimmed, "at ") || strings.HasPrefix(trimmed, "\t") || strings.HasPrefix(trimmed, "    ") {
		return true
	}
	// Keep lines that look like assertions or expectations
	if strings.Contains(lower, "expected") || strings.Contains(lower, "got") || strings.Contains(lower, "want") {
		return true
	}

	// Drop lines ending with "... ok" (test passed)
	if strings.HasSuffix(trimmed, "... ok") {
		return false
	}
	// Drop progress indicators: just dots, checkmarks, or spinners
	if isProgressIndicator(trimmed) {
		return false
	}
	// Drop download/compilation noise
	if strings.Contains(lower, "downloading") || strings.Contains(lower, "installing") ||
		strings.Contains(lower, "compiling") || strings.Contains(lower, "building") {
		return false
	}
	// Drop lines that indicate a passing test (contain "pass" but not "fail")
	if strings.Contains(lower, "pass") && !strings.Contains(lower, "fail") {
		return false
	}
	// Default: keep line
	return true
}

// isProgressIndicator checks for common test progress patterns.
func isProgressIndicator(s string) bool {
	// Empty or just whitespace
	if s == "" {
		return false
	}
	// All dots
	if strings.Trim(s, ".") == "" {
		return true
	}
	// All checkmarks or crosses (unicode or ascii)
	// We'll check if all runes are in a set of progress chars
	progressChars := map[rune]bool{
		'✓': true, '✔': true, '✕': true, '✖': true, '√': true, '×': true,
		'.': true, ' ': true, // space alone handled above
	}
	for _, r := range s {
		if !progressChars[r] {
			return false
		}
	}
	return true
}
