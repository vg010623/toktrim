package filter

import (
	"fmt"
	"regexp"
)

// DefaultContextBefore and DefaultContextAfter are how many neighbouring lines
// are protected alongside a line that carries failure information.
const (
	DefaultContextBefore = 3
	DefaultContextAfter  = 3
	// DefaultMaxBlockLines caps how much of one failure block is kept. Real
	// assertion diffs and tracebacks are far smaller; the cap exists so a
	// runaway block cannot defeat compression entirely. When it bites, a marker
	// is emitted at the point of the cut.
	DefaultMaxBlockLines = 500
)

// failurePattern matches a line that may carry failure information.
//
// The error/exception/failure alternatives are anchored on their suffix so that
// CamelCase compounds match too: AssertionError, TypeError,
// NullPointerException, TestFailure. Everything that matches is kept verbatim,
// so this pattern errs towards matching -- a false positive costs a few tokens,
// a false negative hides the reason a command failed.
var failurePattern = regexp.MustCompile(
	`(?i)` +
		`error(s)?\b` +
		`|exception(s)?\b` +
		`|failed\b|failure(s)?\b` +
		`|panic(ked|king)?\b` +
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

// blockStartPattern matches a line that opens a failure block: everything from
// here until the block ends is failure detail and is kept whole.
var blockStartPattern = regexp.MustCompile(
	`^\s*\d+\)\s` + // mocha: "  1) Controller"
		`|^\s*●` + // jest failure detail
		`|^\s*(FAIL|FAILED)\b` + // jest/vitest/pytest
		`|^\s*--- FAIL:` + // go test
		`|^\s*not ok \d+` + // TAP
		`|AssertionError` +
		`|^\s*[A-Za-z_][\w.]*Error:` + // TypeError:, ReferenceError:, Error:
		`|^\s*Error:` +
		`|^\s*(Traceback \(most recent call last\)|Unhandled exception)` +
		`|^\s*thread '.*' panicked` +
		`|^\s*panic:`,
)

// blockEndPattern matches a summary line, which always closes a block.
var blockEndPattern = regexp.MustCompile(
	`^\s*\d+ (passing|failing|pending)\b` + // mocha
		`|^\s*(Test Suites|Tests|Snapshots|Time|Duration):` + // jest/vitest
		`|^\s*Ran all test suites` +
		`|^\s*test result:` + // cargo
		`|^\s*(PASS|ok)\s+\S+\s` +
		`|^=+ .* =+$` + // pytest summary rule
		`|^\s*#\s+(tests|pass|fail)\s+\d+` + // TAP
		`|^\s*(Build succeeded|Build FAILED|Time Elapsed)\b`,
)

// failureDetailPattern matches a line that is plainly part of a failure's
// detail even when it is not indented past the block header.
//
// Assertion diffs are the reason this exists: mocha prints "+ expected
// - actual" and the diff body at the same indentation as the AssertionError
// line above it, so an indentation rule alone would cut the block in half --
// which is exactly the bug this guard was rewritten to fix.
var failureDetailPattern = regexp.MustCompile(
	`^\s*[-+](\s|$|[^-+>])` + // diff lines: "-  \"total\": 118"
		`|^\s*[-+]{3}\s` + // ---/+++ headers
		`|^\s*[{}\[\]]` + // object/array braces in a diff
		`|^\s*(expected|actual|received|want|got)\b` +
		`|^\s*[+-]\s*expected` +
		`|^\s*\|` + // compiler carets and gutters
		`|^\s*\^+` +
		`|^\s*~+$` +
		`|^\s*\d+\s*\|`, // "  214 |  name = strdupp(...)"
)

// IsFailureLine reports whether a line carries failure information and must be
// preserved verbatim.
func IsFailureLine(s string) bool {
	if stackFramePattern.MatchString(s) {
		return true
	}
	return failurePattern.MatchString(urlToken.ReplaceAllString(s, " "))
}

// urlToken matches a whitespace-delimited token containing a scheme.
//
// Package names are full of failure words -- error-ex, makeerror, es-errors,
// json-parse-even-better-errors -- and a registry URL mentioning one says
// nothing about anything failing. Without this, every line of npm's registry
// chatter looks like a failure and nothing compresses.
var urlToken = regexp.MustCompile(`\S*://\S*`)

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

// needsContext reports whether a failure line is worth showing neighbours for.
func needsContext(s string) bool {
	return IsFailureLine(s) && !selfContained.MatchString(s)
}

// IsBlockStart reports whether a line opens a failure block.
func IsBlockStart(s string) bool { return blockStartPattern.MatchString(s) }

// Guard marks lines that carry failure information so that downstream filters
// leave them alone.
//
// It works two ways. A line that merely mentions a failure is protected along
// with a few lines of context on each side. A line that *opens a failure block*
// -- a mocha numbered failure, a jest ●, a traceback, an AssertionError -- puts
// the guard into block mode, where every line is protected until the block
// closes. Context windows are the wrong tool for a failure block: an assertion
// diff is as long as it needs to be, and keeping a fixed ±3 lines of it drops
// the middle of the diff while keeping its edges, which reads as a complete
// diff that says something false.
//
// Guard never drops a line except when a single block exceeds MaxBlockLines,
// and then it emits a marker at the point of the cut.
type Guard struct {
	tracker
	ContextBefore int
	ContextAfter  int
	MaxBlockLines int

	held      []Line
	afterLeft int

	inBlock     bool
	blockIndent int
	blockLines  int
	blockElided int
	heldBlank   []Line
}

// NewGuard returns a Guard with the default context window and block cap.
func NewGuard() *Guard {
	return &Guard{
		ContextBefore: DefaultContextBefore,
		ContextAfter:  DefaultContextAfter,
		MaxBlockLines: DefaultMaxBlockLines,
	}
}

// Name implements LineFilter.
func (g *Guard) Name() string { return "guard" }

// Process implements LineFilter.
func (g *Guard) Process(l Line, emit Emit) {
	if g.inBlock {
		if g.blockContinues(l) {
			g.blockAppend(l, emit)
			return
		}
		g.closeBlock(emit)
		// Fall through: this line is judged on its own merits.
	}

	if IsBlockStart(l.Text) && !blockEndPattern.MatchString(l.Text) {
		// Context before a failure still applies: flush the held window with
		// those lines protected, then open the block.
		for i := range g.held {
			g.held[i].Protected = true
		}
		g.flushHeld(emit)
		g.openBlock(l, emit)
		return
	}

	switch {
	case needsContext(l.Text):
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

// openBlock begins a failure block at l.
func (g *Guard) openBlock(l Line, emit Emit) {
	g.inBlock = true
	g.blockIndent = indentOf(l.Text)
	g.blockLines = 0
	g.blockElided = 0
	g.afterLeft = 0
	g.blockAppend(l, emit)
}

// blockAppend emits a line as part of the current block, or counts it against
// the cap.
func (g *Guard) blockAppend(l Line, emit Emit) {
	// Blank lines inside a block are held: a block often ends with one, and
	// emitting it as block content would attach a trailing blank to every
	// failure. They are released as soon as the block continues.
	if blank(l.Text) {
		g.heldBlank = append(g.heldBlank, l)
		return
	}
	for _, b := range g.heldBlank {
		g.blockLines++
		emit(Line{Text: b.Text, Protected: true, InFailureBlock: true})
	}
	g.heldBlank = nil

	g.blockLines++
	if g.MaxBlockLines > 0 && g.blockLines > g.MaxBlockLines {
		g.blockElided++
		g.markChanged()
		return
	}
	l.Protected = true
	l.InFailureBlock = true
	emit(l)
}

// closeBlock ends the current block, reporting anything the cap cut.
func (g *Guard) closeBlock(emit Emit) {
	if !g.inBlock {
		return
	}
	if g.blockElided > 0 {
		// The cut is at the end of what was kept, so the marker lands exactly
		// where the lines went missing.
		emit(Line{Text: fmt.Sprintf("[toktrim] %d %s elided from this failure; see the full output log",
			g.blockElided, plural(g.blockElided, "line"))})
	}
	g.inBlock = false
	g.blockElided = 0
	g.blockLines = 0
	// A failure whose detail was not indented -- a bare "Error: ..." line --
	// closes its block immediately. Fall back to the context window so those
	// still keep a few lines of their surroundings.
	g.afterLeft = g.ContextAfter
	// A blank held at the end of a block is the separator that follows it.
	// It is emitted protected so that positional filters downstream cannot
	// drop it and run two failures together.
	for _, b := range g.heldBlank {
		b.Protected = true
		emit(b)
	}
	g.heldBlank = nil
}

// blockContinues reports whether l is still part of the open block.
func (g *Guard) blockContinues(l Line) bool {
	if blank(l.Text) {
		return true
	}
	// A summary line always closes a block.
	if blockEndPattern.MatchString(l.Text) {
		return false
	}
	// Detail indented past the header belongs to the block. This is checked
	// before the block-start test on purpose: mocha nests an AssertionError
	// inside its numbered failure, and treating that as a sibling header would
	// split one failure into two blocks.
	if indentOf(l.Text) > g.blockIndent {
		return true
	}
	// A failure header at or outside the current indentation is a sibling.
	if IsBlockStart(l.Text) {
		return false
	}
	// Assertion diffs sit at the same indentation as their header, so shape
	// decides rather than indentation alone.
	return failureDetailPattern.MatchString(l.Text) || IsFailureLine(l.Text)
}

// flushHeld releases the context window.
func (g *Guard) flushHeld(emit Emit) {
	for _, h := range g.held {
		emit(h)
	}
	g.held = nil
}

// Flush implements LineFilter.
func (g *Guard) Flush(emit Emit) {
	g.closeBlock(emit)
	g.flushHeld(emit)
}
