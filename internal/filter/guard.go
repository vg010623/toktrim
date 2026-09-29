package filter

import "regexp"

// DefaultContextBefore and DefaultContextAfter are how many neighbouring lines
// are protected alongside a line that carries failure information.
const (
	DefaultContextBefore = 3
	DefaultContextAfter  = 3
)

// failurePattern matches a line that may carry failure information.
//
// Word boundaries keep ordinary filenames from matching: a build that compiles
// error_handling.go should still compress. Everything that does match is kept
// verbatim, so this pattern errs towards matching -- a false positive costs a
// few tokens, a false negative hides the reason a command failed.
var failurePattern = regexp.MustCompile(
	`(?i)` +
		// Anchored on the suffix so CamelCase compounds match too:
		// AssertionError, TypeError, NullPointerException, TestFailure.
		`error(s)?\b` +
		`|exception(s)?\b` +
		`|failed\b|failure(s)?\b` +
		`|panic(ked|king)?\b` +
		// Anchored on both sides where the bare word is ambiguous.
		`|\berr\b|ERR!` +
		`|\bfail(s|ing)?\b` +
		`|\btraceback\b` +
		`|\bfatal\b` +
		`|\bassert\b` +
		`|\bunhandled\b` +
		`|\bsegfault\b|\bsegmentation fault\b` +
		`|\bE\d{3,}\b` + // pnpm/yarn error codes
		`|\bMSB\d+\b|\bCS\d{4}\b|\bNETSDK\d+\b`, // msbuild / roslyn / dotnet sdk
)

// stackFramePattern matches a line that is part of a stack trace or traceback.
var stackFramePattern = regexp.MustCompile(
	`^\s*at\s+\S` + // JS/JVM: "at Object.<anonymous> (a.js:1:2)"
		`|^\s*File "[^"]+", line \d+` + // Python traceback
		`|^\s*from [\w./\\-]+:\d+` + // Ruby
		`|^\s*#\d+\s+0x[0-9a-fA-F]+` + // gdb / native
		`|^\s*[\w./\\-]+\.(go|rs|py|js|ts|tsx|jsx|cs|java|rb|php|c|cc|cpp|h):\d+` +
		`|^\s*--- FAIL` + // go test
		`|^\s*thread '.*' panicked`, // rust
)

// selfContained matches a line that must be kept but does not need its
// neighbours to make sense.
//
// A compiler diagnostic names its own file, line and code, so surrounding it
// with three lines of restore chatter adds noise without adding information. A
// panic or a traceback is the opposite: the lines around it are the story.
var selfContained = regexp.MustCompile(
	`(?i)^\s*\d+\s+(warning|error)\(s\)` + // msbuild counts
		`|(?i)\bwarn(ing)?\b.*\b[A-Z]+\d{3,}:` + // warning CS0168:
		`|(?i)^\s*npm\s+warn\b`,
)

// urlToken matches a whitespace-delimited token containing a scheme.
//
// Package names are full of failure words -- error-ex, makeerror, es-errors,
// json-parse-even-better-errors -- and a registry URL mentioning one says
// nothing about anything failing. Without this, every line of npm's registry
// chatter looks like a failure and nothing compresses.
var urlToken = regexp.MustCompile(`\S*://\S*`)

// IsFailureLine reports whether a line carries failure information and must be
// preserved verbatim.
func IsFailureLine(s string) bool {
	if stackFramePattern.MatchString(s) {
		return true
	}
	return failurePattern.MatchString(urlToken.ReplaceAllString(s, " "))
}

// needsContext reports whether a failure line is worth showing neighbours for.
func needsContext(s string) bool {
	return IsFailureLine(s) && !selfContained.MatchString(s)
}

// Guard marks lines that carry failure information, together with a few lines
// of context on each side, so that downstream filters leave them alone.
//
// Protecting context before a failure means holding that many lines back, so
// Guard delays the stream by ContextBefore lines. It never drops anything.
type Guard struct {
	tracker
	ContextBefore int
	ContextAfter  int

	held      []Line
	afterLeft int
}

// NewGuard returns a Guard with the default context window.
func NewGuard() *Guard {
	return &Guard{ContextBefore: DefaultContextBefore, ContextAfter: DefaultContextAfter}
}

// Name implements LineFilter.
func (g *Guard) Name() string { return "guard" }

// Process implements LineFilter.
func (g *Guard) Process(l Line, emit Emit) {
	switch {
	case needsContext(l.Text):
		// Retroactively protect the lines still held back.
		for i := range g.held {
			g.held[i].Protected = true
		}
		l.Protected = true
		g.afterLeft = g.ContextAfter
	case IsFailureLine(l.Text):
		// Kept, but it does not drag its neighbours in with it.
		l.Protected = true
	case g.afterLeft > 0:
		l.Protected = true
		g.afterLeft--
	}

	g.held = append(g.held, l)
	for len(g.held) > g.ContextBefore {
		emit(g.held[0])
		g.held = g.held[1:]
	}
}

// Flush implements LineFilter.
func (g *Guard) Flush(emit Emit) {
	for _, l := range g.held {
		emit(l)
	}
	g.held = nil
}
