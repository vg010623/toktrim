package filter

import "strings"

// splitLines splits input into lines without inventing a trailing empty line.
//
// strings.Split("a\n", "\n") yields ["a", ""], and filters that treat that
// final element as a real line emit a stray blank line. splitLines reports the
// trailing newline separately instead.
func splitLines(input string) (lines []string, trailingNewline bool) {
	if input == "" {
		return nil, false
	}
	if strings.HasSuffix(input, "\n") {
		return strings.Split(strings.TrimSuffix(input, "\n"), "\n"), true
	}
	return strings.Split(input, "\n"), false
}

// joinLines is the inverse of splitLines.
func joinLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		return ""
	}
	s := strings.Join(lines, "\n")
	if trailingNewline {
		s += "\n"
	}
	return s
}
