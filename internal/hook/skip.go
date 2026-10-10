// Package hook implements toktrim's Claude Code PreToolUse hook.
package hook

import (
	"os/exec"
	"strings"
	"sync"

	"github.com/vg010623/toktrim/internal/filter"
)

// rtkAllowlist is the set of commands rtk compresses.
//
// toktrim defers to rtk for these, but only when rtk is actually installed.
// Skipping them unconditionally meant that on a machine without rtk nothing
// filtered them at all.
var rtkAllowlist = map[string]bool{
	"cargo": true, "pytest": true, "jest": true, "vitest": true,
	"tsc": true, "eslint": true, "ls": true, "grep": true, "find": true,
}

// rtkGitSubcommands are the git subcommands rtk covers. Others (commit, push,
// rebase) are left to toktrim.
var rtkGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
}

// longRunning marks commands that stream for minutes or expect a terminal.
var longRunning = map[string]bool{
	"dev": true, "start": true, "watch": true, "serve": true,
	"develop": true, "preview": true, "repl": true, "console": true,
	"attach": true, "logs": true, "tail": true,
}

// longRunningFlags are flags that mean the same thing.
var longRunningFlags = map[string]bool{
	"--watch": true, "-w": true, "--serve": true, "--dev": true,
	"-it": true, "-ti": true, "--interactive": true, "--tty": true,
	"--follow": true, "-f": true,
}

// binaryRunners hand the rest of the command line to another program, so the
// interesting command name is the next word rather than the first.
//
// Without this, `npx jest` was wrapped while bare `jest` was skipped -- the
// same command treated two different ways depending on how it was spelled.
var binaryRunners = map[string]bool{
	"npx": true, "bunx": true, "pnpx": true,
}

// Decision explains what the hook chose to do and why.
type Decision struct {
	Wrap   bool
	Reason string
}

// Classifier decides whether toktrim should wrap a command.
type Classifier struct {
	// RTKAvailable reports whether rtk is on PATH. When it is not, toktrim
	// handles the commands it would otherwise leave to rtk.
	RTKAvailable bool
}

var (
	defaultOnce       sync.Once
	defaultClassifier *Classifier
)

// rtkOnPath reports whether rtk is installed, looked up once per process.
func rtkOnPath() bool {
	_, err := exec.LookPath("rtk")
	return err == nil
}

// Default returns the classifier used by the hook, created once.
func Default() *Classifier {
	defaultOnce.Do(func() {
		defaultClassifier = &Classifier{RTKAvailable: rtkOnPath()}
	})
	return defaultClassifier
}

// Classify decides whether toktrim should wrap a Bash command, using the
// process-wide classifier.
func Classify(command string) Decision { return Default().Classify(command) }

// Classify decides whether toktrim should wrap a Bash command.
//
// The default is to leave a command alone: a command wrapped needlessly costs
// the user a rewritten permission prompt, so anything uncertain is skipped.
func (c *Classifier) Classify(command string) Decision {
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

	// A pipeline ending in head, tail, grep, wc or jq has already had its
	// output chosen by whoever wrote it.
	if last := splitWords(segments[len(segments)-1]); filter.IsPipelineSink(normalizeRunner(last)) && len(segments) > 1 {
		return Decision{false, "pipeline already selects its output"}
	}

	allRTK := true
	for _, seg := range segments {
		words := normalizeRunner(splitWords(seg))
		if len(words) == 0 {
			continue
		}
		if isLongRunning(words) {
			return Decision{false, "long-running or interactive command"}
		}
		if isInteractiveDockerRun(words) {
			return Decision{false, "docker run without --rm"}
		}
		if filter.IsQuerySegment(words) {
			return Decision{false, "query command: its output is the answer"}
		}
		if !c.coveredByRTK(words) {
			allRTK = false
		}
	}
	if allRTK && c.RTKAvailable {
		return Decision{false, "covered by rtk's allowlist"}
	}

	if allRTK {
		return Decision{true, "rtk is not installed, so toktrim handles it"}
	}
	return Decision{true, "not covered by rtk"}
}

// normalizeRunner strips a binary runner prefix so that `npx jest` is judged
// as `jest`.
func normalizeRunner(words []string) []string {
	for len(words) > 1 {
		switch base(words[0]) {
		case "npx", "bunx", "pnpx":
			words = words[1:]
			continue
		case "pnpm", "yarn", "bun":
			// `pnpm exec X`, `yarn dlx X`: the runner form.
			if len(words) > 2 && (words[1] == "exec" || words[1] == "dlx" || words[1] == "run") {
				// `run` names a package script, which is not a bare binary.
				if words[1] == "run" {
					return words
				}
				words = words[2:]
				continue
			}
			// `yarn jest` runs the binary, but `yarn build` runs a script, so
			// only a recognised tool name is unwrapped.
			if len(words) > 1 && isKnownTool(base(words[1])) {
				words = words[1:]
				continue
			}
			return words
		}
		return words
	}
	return words
}

// isKnownTool reports whether a name is a tool whose output toktrim or rtk
// recognises, which is what makes unwrapping `yarn <tool>` safe.
func isKnownTool(name string) bool {
	if rtkAllowlist[name] {
		return true
	}
	switch name {
	case "mocha", "ava", "tap", "nyc", "c8", "prettier", "biome", "tsx", "esbuild", "vite", "webpack":
		return true
	}
	return false
}

// coveredByRTK reports whether rtk already compresses this segment.
func (c *Classifier) coveredByRTK(words []string) bool {
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

// splitWords splits a segment into words, dropping environment assignments
// that precede the command name.
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
