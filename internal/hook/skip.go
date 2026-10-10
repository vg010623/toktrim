// Package hook implements toktrim's Claude Code PreToolUse hook.
package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/vg010623/toktrim/internal/filter"
)

// knownTools are tools whose output toktrim recognises. The list exists only
// so that `yarn <tool>` can be told apart from `yarn <script>`.
var knownTools = map[string]bool{
	"jest": true, "vitest": true, "mocha": true, "ava": true, "tap": true,
	"pytest": true, "cargo": true, "tsc": true, "eslint": true, "biome": true,
	"prettier": true, "nyc": true, "c8": true, "tsx": true, "esbuild": true,
	"vite": true, "webpack": true, "rollup": true,
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
//
// toktrim is a standalone tool. Its own filters already cover jest, vitest,
// mocha, node:test, pytest, go test and cargo, so nothing here needs another
// tool to be installed. Deference to rtk is opt-in, for people who run both
// and want rtk to keep the commands it is better at.
type Classifier struct {
	// DeferToRTK makes toktrim leave a command alone when rtk would handle it.
	// Off by default: a developer who installs toktrim should get a tool that
	// works, not one whose behaviour depends on what else is on the machine.
	DeferToRTK bool
	// askRTK reports whether rtk claims a command. Replaced in tests.
	askRTK func(command string) bool
}

var (
	defaultOnce       sync.Once
	defaultClassifier *Classifier
)

// Default returns the classifier used by the hook, created once.
func Default() *Classifier {
	defaultOnce.Do(func() {
		defaultClassifier = &Classifier{
			DeferToRTK: deferToRTKFromEnv(),
			askRTK:     rtkClaims,
		}
	})
	return defaultClassifier
}

// deferToRTKFromEnv reads the opt-in switch.
func deferToRTKFromEnv() bool {
	switch os.Getenv("TOKTRIM_DEFER_TO_RTK") {
	case "1", "true", "yes":
		return true
	}
	return false
}

// rtkClaims asks rtk's own hook whether it would rewrite this command.
//
// Asking rtk beats keeping a copy of its allowlist here for two reasons. It is
// accurate for whatever rtk version is installed -- rtk 0.44.2 covers about
// seventy commands, far more than the handful toktrim's list was built from --
// and it answers the right question. Whether the rtk *binary* exists says
// nothing about whether rtk's hook is actually running; whether rtk's hook
// claims a command is exactly what decides if the two would collide.
//
// Any failure means "rtk does not claim it", so a missing or broken rtk can
// never stop toktrim from doing its job.
func rtkClaims(command string) bool {
	path, err := exec.LookPath("rtk")
	if err != nil {
		return false
	}
	payload, err := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": command},
	})
	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), rtkTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "hook", "claude")
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	// rtk prints nothing when it has no opinion, and a hookSpecificOutput with
	// an updatedInput when it wants to rewrite the command.
	var reply struct {
		HookSpecificOutput struct {
			UpdatedInput map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return false
	}
	_, ok := reply.HookSpecificOutput.UpdatedInput["command"]
	return ok
}

// rtkTimeout bounds how long toktrim waits for rtk's opinion. rtk answers in
// about 10 ms; the timeout only matters if it hangs, and a hook must not.
const rtkTimeout = 500 * time.Millisecond

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

	rtkWouldHandle := c.DeferToRTK && c.rtkClaimsAll(segments)
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
	}
	if rtkWouldHandle {
		return Decision{false, "deferring to rtk, which claims this command"}
	}
	return Decision{true, "toktrim handles this"}
}

// rtkClaimsAll reports whether rtk would rewrite every segment of the command.
//
// Every segment, not any: if a chain mixes something rtk handles with something
// it does not, rtk will not rewrite the chain, so deferring would leave it
// unfiltered.
func (c *Classifier) rtkClaimsAll(segments []string) bool {
	ask := c.askRTK
	if ask == nil {
		ask = rtkClaims
	}
	for _, seg := range segments {
		if !ask(seg) {
			return false
		}
	}
	return len(segments) > 0
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

// isKnownTool reports whether a name is a tool whose output toktrim
// recognises, which is what makes unwrapping `yarn <tool>` safe.
func isKnownTool(name string) bool { return knownTools[name] }

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
