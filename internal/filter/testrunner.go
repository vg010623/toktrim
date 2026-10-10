package filter

import (
	"regexp"
	"strconv"
)

var (
	// A failing block starts here. Guard already protects block bodies; this
	// pattern is what stops the test filter from dropping the header itself.
	testFailureStart = regexp.MustCompile(`^\s*(FAIL|✕|×|✗)\b` + // jest/vitest suite or case
		`|^\s*●` + // jest failure detail
		`|^\s*--- FAIL:` + // go test
		`|^(FAILED|ERROR)\b` + // pytest
		`|^\s*\d+\)\s` + // mocha numbered failure
		`|^not ok \d+` + // TAP
		`|^\s*(test result: FAILED|failures:)`)

	// A new suite header ends whatever block was open.
	testBlockEnd = regexp.MustCompile(`^\s*(PASS|RUN|✓|✔|√)\b` +
		`|^\s*(Test Suites|Tests|Snapshots|Time|Duration|Ran all test suites)\b` +
		`|^\s*\d+ (passing|failing|pending)\b`)

	// Passing noise, dropped when no failure block is open.
	// ✔ is mocha's default glyph from v8 on; ✓ is the older one and vitest's.
	testPassLine = regexp.MustCompile(`^\s*(PASS|✓|✔|√)\b` + // jest/vitest/mocha pass
		`|^\s*[✓✔√]\s` + // mocha indents the glyph then the title
		`|^ok \d+` + // TAP
		`|^\s*ok\s+\S+\s+[\d.]+s` + // go test
		`|^\s*---\s+PASS:` +
		`|^\s*RUN\s+v[\d.]+` + // vitest banner
		`|^\s*\S+\s+\.+\s*(ok|PASSED)\s*$` + // pytest dots form
		`|^\s*(Determining|Collecting|collected)\s`)

	// The summary block is always worth keeping.
	testSummary = regexp.MustCompile(`^\s*(Test Suites|Tests|Snapshots|Time|Duration|Start at):` +
		`|^\s*Ran all test suites` +
		`|(?i)^\s*\d+ (passing|pending|failing)\b` +
		`|(?i)^=+ .*(passed|failed|error).* =+$` + // pytest summary rule
		`|^\s*test result:` + // cargo
		`|(?i)^\s*(Passed!|Failed!)\s+-\s+Failed:` + // dotnet test
		`|^\s*Tests? (run|Run):` +
		`|^\s*#\s+(tests|pass|fail|skip)\s+\d+` + // TAP summary
		`|^\s*1\.\.\d+\s*$`) // TAP plan

	// mochaFailingHeader marks the point after which mocha prints nothing but
	// failure detail.
	mochaFailingHeader = regexp.MustCompile(`^\s*\d+ failing\b`)
)

// TestRunner compresses jest, vitest, mocha, node:test/TAP, pytest, go test and
// cargo test output while keeping every failing test intact.
//
// Once mocha prints "N failing", everything after it is failure detail, so the
// filter stops dropping anything at all from that point on. Before it, and for
// the other runners, a failure block keeps every line until the next suite
// header.
type TestRunner struct {
	tracker

	inFailure     bool
	failureDetail bool // mocha: past the "N failing" header
	droppedPass   int

	// TAP attaches an indented YAML diagnostic block to each result. When a
	// passing result is dropped its diagnostics go with it; keeping them would
	// leave orphaned "---" / "duration_ms:" / "..." fragments behind.
	tapSuppress       bool
	tapSuppressIndent int
}

// NewTestRunner returns a TestRunner filter.
func NewTestRunner() *TestRunner { return &TestRunner{} }

// Name implements LineFilter.
func (tr *TestRunner) Name() string { return "test-runner" }

// Process implements LineFilter.
func (tr *TestRunner) Process(l Line, emit Emit) {
	// Everything after mocha's "N failing" line is failure detail.
	if mochaFailingHeader.MatchString(l.Text) {
		tr.failureDetail = true
		emit(l)
		return
	}
	if tr.failureDetail || l.InFailureBlock {
		tr.tapSuppress = false
		emit(l)
		return
	}

	// Continue swallowing a dropped TAP result's diagnostic block.
	if tr.tapSuppress {
		if !blank(l.Text) && indentOf(l.Text) > tr.tapSuppressIndent &&
			!l.Protected && !IsFailureLine(l.Text) {
			tr.markChanged()
			return
		}
		tr.tapSuppress = false
	}

	switch {
	case testFailureStart.MatchString(l.Text):
		tr.inFailure = true
		emit(l)
		return
	case tr.inFailure && testBlockEnd.MatchString(l.Text):
		tr.inFailure = false
		// Fall through: the header itself is judged on its own merits below.
	case tr.inFailure:
		// Inside a failure block everything is kept, blank lines included.
		emit(l)
		return
	}

	if l.Protected || testSummary.MatchString(l.Text) {
		emit(l)
		return
	}
	if testPassLine.MatchString(l.Text) {
		tr.droppedPass++
		tr.markChanged()
		// A TAP "ok N" line owns the indented diagnostics that follow it.
		if tapResultLine.MatchString(l.Text) {
			tr.tapSuppress = true
			tr.tapSuppressIndent = indentOf(l.Text)
		}
		return
	}
	emit(l)
}

// Flush implements LineFilter.
func (tr *TestRunner) Flush(emit Emit) {
	if tr.droppedPass > 0 {
		emit(Line{Text: "[toktrim] dropped " + strconv.Itoa(tr.droppedPass) + " passing-test " +
			plural(tr.droppedPass, "line")})
	}
	tr.droppedPass = 0
	tr.inFailure = false
	tr.failureDetail = false
	tr.tapSuppress = false
}

// tapResultLine matches a TAP result line, which owns the YAML diagnostic block
// indented beneath it.
var tapResultLine = regexp.MustCompile(`^\s*(not )?ok \d+`)

// LooksLikeTestOutput reports whether a line is a marker of a test runner whose
// output the test filter understands. The chain uses it to switch the filter on
// for `npm test` and `npm run <script>`, where the script could be anything.
func LooksLikeTestOutput(s string) bool {
	return jsTestMarker.MatchString(s)
}

var jsTestMarker = regexp.MustCompile(`^\s*(PASS|FAIL)\s+\S+\.(test|spec)\.` + // jest/vitest
	`|^\s*Test Suites:` +
	`|^\s*RUN\s+v[\d.]+` +
	`|^\s*(Tests|Snapshots):\s+\d` +
	`|\bjest\b|\bvitest\b|\bmocha\b` +
	// mocha's own summary, which is the first reliable mocha marker.
	`|^\s*\d+ (passing|failing|pending)\b` +
	// node:test and anything else speaking TAP.
	`|^ok \d+` +
	`|^not ok \d+` +
	`|^#\s+(tests|pass|fail|skip)\s+\d+` +
	`|^TAP version \d+`)

// AutoTestRunner behaves as a pass-through until the output looks like a test
// runner it understands, then hands over to TestRunner.
//
// `npm run <script>` can be anything, so the filter cannot be chosen from the
// command line alone. Lines seen before the runner identifies itself are passed
// through unchanged, which costs a few tokens at the top of the output and
// avoids compressing output the filter does not understand.
type AutoTestRunner struct {
	inner    *TestRunner
	detected bool
}

// NewAutoTestRunner returns an AutoTestRunner.
func NewAutoTestRunner() *AutoTestRunner { return &AutoTestRunner{inner: NewTestRunner()} }

// Name implements LineFilter.
func (a *AutoTestRunner) Name() string { return "test-runner" }

// Process implements LineFilter.
func (a *AutoTestRunner) Process(l Line, emit Emit) {
	if !a.detected {
		if !LooksLikeTestOutput(l.Text) {
			emit(l)
			return
		}
		a.detected = true
	}
	a.inner.Process(l, emit)
}

// Flush implements LineFilter.
func (a *AutoTestRunner) Flush(emit Emit) { a.inner.Flush(emit) }

// Changed implements LineFilter.
func (a *AutoTestRunner) Changed() bool { return a.inner.Changed() }
