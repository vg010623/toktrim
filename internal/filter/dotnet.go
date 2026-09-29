package filter

import (
	"fmt"
	"regexp"
)

var (
	// "  Restored /src/App/App.csproj (in 1.2 sec)."
	dotnetRestored = regexp.MustCompile(`^\s*Restored\s+.*\((in|took)\s`)
	// "  App -> /src/App/bin/Debug/net8.0/App.dll"
	dotnetBuilt = regexp.MustCompile(`^\s*\S+\s+->\s+\S+\.(dll|exe|pdb)\s*$`)
	// Restore and evaluation chatter.
	dotnetRestoreNoise = regexp.MustCompile(`^\s*(Determining projects to restore|Nothing to do\. None of the projects|All projects are up-to-date for restore|Restore (complete|succeeded)|MSBuild version|Welcome to \.NET|----------|Build started|Using launch settings)\b` +
		`|^\s*Restored\s+\S+\s*$`)
	// Warnings carry a code and must be kept with it.
	dotnetDiagnostic = regexp.MustCompile(`\b(warning|error)\s+[A-Z]+\d+:`)
	// Build and test summaries.
	dotnetSummary = regexp.MustCompile(`^\s*(Build succeeded|Build FAILED|Time Elapsed)\b` +
		`|^\s*\d+\s+(Warning|Error)\(s\)` +
		`|^\s*(Passed!|Failed!|Skipped!)\s` +
		`|^\s*Test Run (Successful|Failed|Aborted)` +
		`|(?i)^\s*(Total tests|Passed|Failed|Skipped|Total time):`)
)

// DotNet compresses `dotnet build` and `dotnet test` output.
//
// Per-project restore and build lines collapse to a count. Warnings keep their
// codes, and errors and the build or test summary are kept verbatim.
type DotNet struct {
	tracker

	restored int
	built    int
	noise    int
}

// NewDotNet returns a DotNet filter.
func NewDotNet() *DotNet { return &DotNet{} }

// Name implements LineFilter.
func (d *DotNet) Name() string { return "dotnet" }

// Process implements LineFilter.
func (d *DotNet) Process(l Line, emit Emit) {
	// A diagnostic with a code is kept even though "warning" is not a failure
	// word, because the code is what makes it actionable.
	if l.Protected || dotnetDiagnostic.MatchString(l.Text) || dotnetSummary.MatchString(l.Text) {
		emit(l)
		return
	}

	switch {
	case dotnetRestored.MatchString(l.Text):
		d.restored++
		d.markChanged()
	case dotnetBuilt.MatchString(l.Text):
		d.built++
		d.markChanged()
	case dotnetRestoreNoise.MatchString(l.Text):
		d.noise++
		d.markChanged()
	default:
		emit(l)
	}
}

// Flush implements LineFilter.
func (d *DotNet) Flush(emit Emit) {
	if d.restored > 0 {
		emit(Line{Text: fmt.Sprintf("[toktrim] restored %d %s", d.restored, plural(d.restored, "project"))})
	}
	if d.built > 0 {
		emit(Line{Text: fmt.Sprintf("[toktrim] built %d %s", d.built, plural(d.built, "project"))})
	}
	d.restored, d.built, d.noise = 0, 0, 0
}
