package filter

import (
	"strconv"
	"strings"
)

// DiffFilter collapses noisy lockfile diffs into a single summary line.
// Diffs for other files are passed through unchanged.
type DiffFilter struct{}

// minLockfileChanges is the number of changed lines a lockfile diff must have
// before it is worth collapsing.
const minLockfileChanges = 5

// Apply processes input and returns filtered output.
func (d DiffFilter) Apply(input string) string {
	lines, trailing := splitLines(input)
	if len(lines) == 0 {
		return input
	}

	var out []string
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], "diff --git") {
			out = append(out, lines[i])
			i++
			continue
		}

		// Find the extent of this file's diff.
		j := i + 1
		changes := 0
		for j < len(lines) && !strings.HasPrefix(lines[j], "diff --git") {
			if isDiffChange(lines[j]) {
				changes++
			}
			j++
		}

		if name := diffTargetFile(lines[i]); isLockfile(name) && changes >= minLockfileChanges {
			out = append(out, "[Lockfile changed: "+name+" ("+strconv.Itoa(changes)+" lines changed)]")
		} else {
			out = append(out, lines[i:j]...)
		}
		i = j
	}
	return joinLines(out, trailing)
}

// isDiffChange reports whether a diff body line adds or removes content.
// The "+++"/"---" file headers are not content changes.
func isDiffChange(line string) bool {
	if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
		return false
	}
	return strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")
}

// diffTargetFile extracts the b/ path from a "diff --git a/x b/x" header.
func diffTargetFile(header string) string {
	fields := strings.Fields(header)
	if len(fields) < 4 {
		return ""
	}
	return strings.TrimPrefix(fields[3], "b/")
}

// isLockfile reports whether name is a known dependency lockfile.
func isLockfile(name string) bool {
	switch strings.ToLower(name) {
	case "package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml",
		"bun.lockb", "cargo.lock", "poetry.lock", "pipfile.lock", "uv.lock",
		"composer.lock", "gemfile.lock", "go.sum", "packages.lock.json":
		return true
	}
	return false
}
