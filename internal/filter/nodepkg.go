package filter

import (
	"regexp"
	"strconv"
	"strings"
)

// Noise emitted by npm, pnpm and yarn while installing. None of it tells you
// anything about whether the install worked.
var (
	// npm renamed "npm WARN" to "npm warn" in v11, so every npm pattern here is
	// case-insensitive rather than matching whichever release was current.

	// npm warn deprecated foo@1.2.3: use bar instead
	npmDeprecated = regexp.MustCompile(`(?i)^\s*npm\s+warn\s+deprecated\b`)
	// yarn: "warning foo > bar@1.0.0: deprecated"
	yarnDeprecated = regexp.MustCompile(`(?i)^\s*warning\b.*\bdeprecated\b`)
	// pnpm: " WARN  deprecated foo@1.0.0"
	pnpmDeprecated = regexp.MustCompile(`(?i)^\s*warn\s+deprecated\b`)

	// Per-package registry chatter and arborist progress. At --loglevel=info
	// this is the great majority of an install's output, and none of it says
	// whether the install worked.
	npmTreeProgress = regexp.MustCompile(`(?i)^\s*npm\s+(timing|verb|verbose|silly|sill|info|http)\b` +
		`|(?i)^\s*(idealTree|reify|reifyNode|audit)\b`)

	// Funding and update notices.
	npmFunding = regexp.MustCompile(`(?i)packages? (are|is) looking for funding` +
		`|(?i)^\s*run \x60npm fund\x60` +
		`|(?i)^\s*npm\s+notice\b` +
		`|(?i)New (major|minor|patch)? ?version of npm available` +
		`|(?i)Changelog:\s*https://github.com/npm/cli` +
		`|(?i)^\s*To update run:\s*npm install -g npm`)

	// yarn's step counters and lockfile chatter.
	yarnSteps = regexp.MustCompile(`^\s*\[\d+/\d+\]\s+(Resolving|Fetching|Linking|Building)\b` +
		`|^\s*success Saved lockfile\.` +
		`|^\s*info (fsevents|There appears to be trouble)`)

	// pnpm's rolling progress line.
	pnpmProgress = regexp.MustCompile(`^\s*(Progress:\s*resolved|Packages:|Downloading\s)`)

	// The summary lines worth keeping.
	installSummary = regexp.MustCompile(`(?i)^\s*(added|removed|changed|audited)\s+\d+\s+packages?` +
		`|(?i)^\s*up to date\b` +
		`|(?i)^\s*\+?\d+\s+packages?\s+(added|installed)\b` +
		`|(?i)\bfound \d+ vulnerabilit` +
		`|(?i)^\s*\d+ vulnerabilit` +
		`|(?i)^\s*(Done|success Already up-to-date|success Saved \d+)\b` +
		`|(?i)^\s*(Dependencies|devDependencies|Packages):\s*$`)
)

// NodePackageManager compresses the output of an npm, pnpm or yarn install.
//
// It drops deprecation warnings, dependency-tree and reify progress, and
// funding notices, and keeps ERR! blocks -- which Guard has already protected --
// together with the final "added N packages" summary.
type NodePackageManager struct {
	tracker

	droppedDeprecated int
	droppedProgress   int
}

// NewNodePackageManager returns a NodePackageManager filter.
func NewNodePackageManager() *NodePackageManager { return &NodePackageManager{} }

// Name implements LineFilter.
func (n *NodePackageManager) Name() string { return "node-install" }

// Process implements LineFilter.
func (n *NodePackageManager) Process(l Line, emit Emit) {
	if l.Protected || installSummary.MatchString(l.Text) {
		emit(l)
		return
	}

	switch {
	case npmDeprecated.MatchString(l.Text),
		pnpmDeprecated.MatchString(l.Text),
		yarnDeprecated.MatchString(l.Text):
		n.droppedDeprecated++
		n.markChanged()
		return
	case npmTreeProgress.MatchString(l.Text),
		npmFunding.MatchString(l.Text),
		yarnSteps.MatchString(l.Text),
		pnpmProgress.MatchString(l.Text):
		n.droppedProgress++
		n.markChanged()
		return
	}

	emit(l)
}

// Flush implements LineFilter. It reports what was dropped, so the absence of
// deprecation warnings is visible rather than silent.
func (n *NodePackageManager) Flush(emit Emit) {
	var parts []string
	if n.droppedDeprecated > 0 {
		parts = append(parts, strconv.Itoa(n.droppedDeprecated)+" deprecation warnings")
	}
	if n.droppedProgress > 0 {
		parts = append(parts, strconv.Itoa(n.droppedProgress)+" progress/notice lines")
	}
	if len(parts) > 0 {
		emit(Line{Text: "[toktrim] dropped " + strings.Join(parts, " and ")})
	}
	n.droppedDeprecated, n.droppedProgress = 0, 0
}
