package config

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// parseTOML reads the subset of TOML that toktrim's config needs: comments,
// `[table]` headers, and `key = value` where value is a string, integer or
// boolean. Anything else is reported as an error rather than ignored, so a
// typo in a config file is visible instead of silently doing nothing.
//
// Keys are returned flattened as "table.key", or "key" at the top level.
func parseTOML(r io.Reader) (map[string]string, error) {
	values := map[string]string{}
	table := ""

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(stripComment(sc.Text()))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("line %d: unterminated table header %q", lineNo, line)
			}
			table = strings.TrimSpace(line[1 : len(line)-1])
			if table == "" {
				return nil, fmt.Errorf("line %d: empty table header", lineNo)
			}
			continue
		}

		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value, got %q", lineNo, line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNo)
		}
		val, err := parseValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", lineNo, key, err)
		}
		if table != "" {
			key = table + "." + key
		}
		values[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// stripComment removes a trailing # comment that is not inside a quoted string.
func stripComment(s string) string {
	inQuote := false
	for i, r := range s {
		switch r {
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote {
				return s[:i]
			}
		}
	}
	return s
}

// parseValue normalises a TOML scalar to its string form.
func parseValue(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("missing value")
	}
	if strings.HasPrefix(raw, `"`) {
		if len(raw) < 2 || !strings.HasSuffix(raw, `"`) {
			return "", fmt.Errorf("unterminated string %s", raw)
		}
		unquoted, err := strconv.Unquote(raw)
		if err != nil {
			// Fall back to the literal contents for strings with characters Go
			// would reject, such as a bare backslash in a Windows path.
			return raw[1 : len(raw)-1], nil
		}
		return unquoted, nil
	}
	switch raw {
	case "true", "false":
		return raw, nil
	}
	if _, err := strconv.Atoi(raw); err == nil {
		return raw, nil
	}
	return "", fmt.Errorf("unsupported value %q (toktrim.toml supports strings, integers and booleans)", raw)
}
