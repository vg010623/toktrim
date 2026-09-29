// Package hook implements toktrim's Claude Code PreToolUse hook.
package hook

import (
	"strings"
)

// rtkAllowlist is the set of commands rtk already compresses.
//
// toktrim is a fallback, not a competitor: wrapping a command rtk handles would
// mean two PreToolUse hooks rewriting the same tool input, and Claude Code
// resolves that by taking whichever hook finished last -- a race. Leaving
// rtk's commands alone is what keeps the two tools composable.
var rtkAllowlist = map[string]bool{
	"cargo": true, "pytest": true, "jest": true, "vitest": true,
	"tsc": true, "eslint": true, "ls": true, "grep": true, "find": true,
}

// rtkGitSubcommands are the git subcommands rtk covers. Other git subcommands
// (commit, push, rebase) are left to toktrim.
var rtkGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
}

// longRunning marks commands that stream for minutes or expect a terminal.
// Wrapping them would either hang behind toktrim's buffering or break the
// interactive session.
var longRunning = map[string]bool{
	"dev": true, "start": true, "watch": true, "serve": true,
	"develop": true, "preview": true, "repl": true, "console": true,
	"attach": true, "exec": true, "logs": true, "tail": true,
}

// longRunningFlags are flags that mean the same thing.
var longRunningFlags = map[string]bool{
	"--watch": true, "-w": true, "--serve": true, "--dev": true,
	"-it": true, "-ti": true, "--interactive": true, "--tty": true,
	"--follow": true, "-f": true,
}

// Decision explains what the hook chose to do and why.
type Decision struct {
	Wrap   bool
	Reason string
}

// Classify decides whether toktrim should wrap a Bash command.
//
// The default is to leave a command alone: a command toktrim wraps needlessly
// costs the user a rewritten permission prompt, so anything uncertain is
// skipped.
func Classify(command string) Decision {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return Decision{false, "empty command"}
	}

	segments := splitSegments(trimmed)
	if len(segments) == 0 {
		return Decision{false, "nothing to run"}
	}

	// Never wrap ourselves, or rtk.
	switch firstWord(segments[0]) {
	case "rtk", "toktrim":
		return Decision{false, "already handled by rtk or toktrim"}
	}

	if redirectsAllOutput(trimmed) {
		return Decision{false, "output already redirected to a file"}
	}

	allRTK := true
	for _, seg := range segments {
		words := splitWords(seg)
		if len(words) == 0 {
			continue
		}
		if isLongRunning(words) {
			return Decision{false, "long-running or interactive command"}
		}
		if isInteractiveDockerRun(words) {
			return Decision{false, "docker run without --rm"}
		}
		if !coveredByRTK(words) {
			allRTK = false
		}
	}
	if allRTK {
		return Decision{false, "covered by rtk's allowlist"}
	}

	return Decision{true, "not covered by rtk"}
}

// coveredByRTK reports whether rtk already compresses this segment.
func coveredByRTK(words []string) bool {
	cmd := base(words[0])
	if cmd == "git" {
		for _, w := range words[1:] {
			if strings.HasPrefix(w, "-") {
				continue
			}
			return rtkGitSubcommands[w]
		}
		return false
	}
	if cmd == "go" {
		// rtk covers `go test`; other go subcommands are ours.
		for _, w := range words[1:] {
			if strings.HasPrefix(w, "-") {
				continue
			}
			return w == "test"
		}
		return false
	}
	return rtkAllowlist[cmd]
}

// isLongRunning reports whether a segment starts a process that does not end.
func isLongRunning(words []string) bool {
	for _, w := range words[1:] {
		if longRunningFlags[strings.ToLower(w)] {
			return true
		}
	}
	// Subcommands and script names are checked word by word, splitting on the
	// separators script names use, so "test:watch" and "start-dev" both match.
	for _, w := range words[1:] {
		if strings.HasPrefix(w, "-") {
			continue
		}
		for _, part := range strings.FieldsFunc(strings.ToLower(w), func(r rune) bool {
			return r == ':' || r == '-' || r == '.' || r == '_' || r == '/'
		}) {
			if longRunning[part] {
				return true
			}
		}
	}
	return false
}

// isInteractiveDockerRun reports whether this is `docker run` without --rm.
// A run without --rm leaves a container behind and is usually a service.
func isInteractiveDockerRun(words []string) bool {
	cmd := base(words[0])
	if cmd != "docker" && cmd != "podman" {
		return false
	}
	sub := ""
	for _, w := range words[1:] {
		if !strings.HasPrefix(w, "-") {
			sub = w
			break
		}
	}
	if sub != "run" {
		return false
	}
	for _, w := range words[1:] {
		if w == "--rm" {
			return false
		}
	}
	return true
}

// redirectsAllOutput reports whether both streams already go to a file, which
// leaves toktrim nothing to filter.
func redirectsAllOutput(command string) bool {
	if strings.Contains(command, "&>") || strings.Contains(command, ">&") {
		return true
	}
	// "> file 2>&1" sends both streams to the file.
	return strings.Contains(command, ">") && strings.Contains(command, "2>&1")
}

// splitSegments breaks a command line on the operators that separate commands,
// ignoring anything inside quotes.
func splitSegments(command string) []string {
	var segments []string
	var cur strings.Builder
	var quote rune
	runes := []rune(command)

	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			segments = append(segments, s)
		}
		cur.Reset()
	}

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			cur.WriteRune(r)
		case r == ';' || r == '|' || r == '&':
			// Consume a doubled operator as one separator.
			if i+1 < len(runes) && runes[i+1] == r {
				i++
			}
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return segments
}

// splitWords splits a segment into words, dropping environment assignments that
// precede the command name.
func splitWords(segment string) []string {
	fields := strings.Fields(segment)
	for len(fields) > 0 && isEnvAssignment(fields[0]) {
		fields = fields[1:]
	}
	return fields
}

// isEnvAssignment reports whether a word is a leading VAR=value.
func isEnvAssignment(w string) bool {
	eq := strings.Index(w, "=")
	if eq <= 0 {
		return false
	}
	for _, r := range w[:eq] {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// firstWord returns the command name of a segment.
func firstWord(segment string) string {
	words := splitWords(segment)
	if len(words) == 0 {
		return ""
	}
	return base(words[0])
}

// base strips any directory and Windows extension from a command name.
func base(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	p = strings.ToLower(p)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		p = strings.TrimSuffix(p, ext)
	}
	return p
}
