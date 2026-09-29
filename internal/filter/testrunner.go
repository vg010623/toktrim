package filter

import (
	"regexp"
	"strconv"
)

var (
	// A failing block starts here and runs until the next suite header.
	testFailureStart = regexp.MustCompile(`^\s*(FAIL|✕|×|✗)\b` + // jest/vitest suite or case
		`|^\s*●` + // jest failure detail
		`|^\s*--- FAIL:` + // go test
		`|^(FAILED|ERROR)\b` + // pytest
		`|^\s*\d+\)\s` + // mocha numbered failure
		`|^\s*(test result: FAILED|failures:)`)

	// A new suite header ends whatever block was open.
	testBlockEnd = regexp.MustCompile(`^\s*(PASS|RUN|✓|√)\b` +
		`|^\s*(Test Suites|Tests|Snapshots|Time|Duration|Ran all test suites)\b`)

	// Passing noise, dropped when no failure block is open.
	testPassLine = regexp.MustCompile(`^\s*(PASS|✓|√)\b` + // jest/vitest pass
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
		`|^\s*Tests? (run|Run):`)
)

// TestRunner compresses jest, vitest, mocha, pytest, go test and cargo test
// output while keeping every failing test intact.
//
// It is block aware: once a failure block opens, every line is kept until the
// next suite header, so a failing test's name, assertion and stack frames all
// survive even when they run past Guard's context window.
type TestRunner struct {
	tracker

	inFailure   bool
	droppedPass int
}

// NewTestRunner returns a TestRunner filter.
func NewTestRunner() *TestRunner { return &TestRunner{} }

// Name implements LineFilter.
func (tr *TestRunner) Name() string { return "test-runner" }

// Process implements LineFilter.
func (tr *TestRunner) Process(l Line, emit Emit) {
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
		return
	}
	emit(l)
}

// Flush implements LineFilter.
func (tr *TestRunner) Flush(emit Emit) {
	if tr.droppedPass > 0 {
		emit(Line{Text: "[toktrim] dropped " + strconv.Itoa(tr.droppedPass) + " passing-test lines"})
	}
	tr.droppedPass = 0
	tr.inFailure = false
}

// LooksLikeTestOutput reports whether a line is a marker of a JS test runner.
// The chain uses it to switch the test filter on for `npm run <script>` when
// the script turns out to be jest or vitest.
func LooksLikeTestOutput(s string) bool {
	return jsTestMarker.MatchString(s)
}

var jsTestMarker = regexp.MustCompile(`^\s*(PASS|FAIL)\s+\S+\.(test|spec)\.` +
	`|^\s*Test Suites:` +
	`|^\s*RUN\s+v[\d.]+` +
	`|^\s*(Tests|Snapshots):\s+\d` +
	`|jest|vitest`)

// AutoTestRunner behaves as a pass-through until the output looks like a JS test
// runner, then hands over to TestRunner.
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
