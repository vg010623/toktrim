package golden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vg010623/toktrim/internal/config"
)

// TestMeasuredSavings pins the compression figures quoted in the README to the
// recorded fixtures, so a claim in the docs cannot drift away from what the
// code does. The thresholds are floors, not targets: a filter that improves
// will not fail the test, one that regresses will.
func TestMeasuredSavings(t *testing.T) {
	cases := []struct {
		dir string
		// minByteSaving is the fraction of bytes that must be removed.
		minByteSaving float64
		// mustKeep are strings that have to survive whatever else happens.
		mustKeep []string
	}{
		{
			// Acceptance test 3: an npm install on a medium project must shrink
			// by 60% or more.
			dir:           "npm-install",
			minByteSaving: 0.95,
			mustKeep: []string{
				"added 720 packages",
				"18 vulnerabilities",
			},
		},
		{
			// The v0.3 bug: every diff line of every failure must survive, and
			// each block must be byte-for-byte what mocha printed.
			dir:           "mocha-multi-fail",
			minByteSaving: 0.85,
			mustKeep: []string{
				`-  "total": 118`, // the actual value, dropped by v0.2.0
				`+  "total": 120`, // the expected value
				`"field_1_1": 1`, `"field_6_6": 6`,
				"AssertionError: expected { Object (rows, total) }",
				"at Context.<anonymous> (test/ctrl1.test.js:10:14)",
				"at Context.<anonymous> (test/ctrl6.test.js:60:14)",
				"6 failing",
			},
		},
		{
			// The same output with mocha's older ✓ glyph.
			dir:           "mocha-multi-fail-oldglyph",
			minByteSaving: 0.85,
			mustKeep:      []string{`-  "total": 118`, `+  "total": 120`, "6 failing"},
		},
		{
			// An all-passing mocha run: the ✔ glyph is recognised, passing
			// tests collapse to a count, and every suite name survives.
			dir:           "mocha-pass",
			minByteSaving: 0.70,
			mustKeep: []string{
				"96 passing (4s)",
				"Suite1 Controller", "Suite7 Controller", "Suite12 Controller",
				"dropped 96 passing-test lines",
			},
		},
		{
			// node:test TAP: the failing diagnostic survives whole.
			dir:           "node-test-tap",
			minByteSaving: 0.50,
			mustKeep: []string{
				"not ok 25 - returns correct totals",
				"Expected values to be strictly deep-equal:",
				"+ { rows: 4, total: 118 }",
				"- { rows: 4, total: 120 }",
				"location: 'test/totals.test.js:18:1'",
				"TestContext.<anonymous> (test/totals.test.js:19:3)",
				"# fail 1",
			},
		},
		{
			// Interleaved application logs: noise collapses by shape, the
			// error and its stack do not.
			dir:           "node-app-logs",
			minByteSaving: 0.80,
			mustKeep: []string{
				"Error: ECONNREFUSED connecting to redis://cache:6379",
				"at TCPConnectWrap.afterConnect (node:net:1605:16)",
				"at Socket.emit (node:events:519:28)",
				"warn: [cache] redis miss rate above threshold",
				"info: [boot] listening on 0.0.0.0:8080",
			},
		},
		{
			// A query's output is the answer: nothing is truncated, and the
			// packages that only appear in the middle third are all present.
			dir:           "npm-ls-large",
			minByteSaving: 0.0,
			mustKeep: []string{
				"math-intrinsics@", "axobject-query@", "shebang-regex@", "terminal-link@",
				"dayjs@", "ag-grid-react@", "big.js@",
			},
		},
		{
			// Every line is an error, so there is nothing to drop and the
			// output must come back byte-for-byte rather than gain markers.
			dir:           "eslint-all-errors",
			minByteSaving: 0.0,
			mustKeep: []string{
				"96 problems (96 errors, 0 warnings)",
				"potentially fixable with the `--fix` option",
			},
		},
		{
			// Acceptance test 2: every failing test name, assertion and stack
			// frame survives.
			dir:           "npm-test-jest-fail",
			minByteSaving: 0.0,
			mustKeep: []string{
				"is wrong about big numbers",
				"throws on zero",
				"Expected: 9999",
				"Received: 1001",
				"Expected substring: \"divide by zero\"",
				"at Object.toBe (src/sum.test.js:5:67)",
				"at Object.toThrow (src/sum.test.js:9:58)",
				"Tests:       2 failed, 6 passed, 8 total",
			},
		},
		{
			// Acceptance test 4: the failing step's full output is present.
			dir:           "docker-build-fail",
			minByteSaving: 0.30,
			mustKeep: []string{
				"#10 [6/6] RUN npm run build",
				"src/server.ts(42,17): error TS2345",
				"src/routes/user.ts(18,3): error TS2554",
				"src/db/pool.ts(7,10): error TS2551",
				"Found 3 errors in 3 files.",
				`ERROR: failed to solve: process "/bin/sh -c npm run build"`,
				"#8 [4/6] RUN npm ci",
			},
		},
		{
			dir:           "dotnet-build-error",
			minByteSaving: 0.0,
			mustKeep: []string{
				"warning CS0168", "warning CS4014",
				"error CS1503", "error CS1002",
				"Build FAILED.", "2 Error(s)",
			},
		},
		{
			dir:           "generic-truncate",
			minByteSaving: 0.60,
			mustKeep: []string{
				"src/parser/lexer.c:214:9: error: implicit declaration of function 'strdupp'",
				"src/net/socket.c:88:5: error: too few arguments to function 'connect'",
				"make: *** [Makefile:42: all] Error 1",
				"[  1/400] CC   build/obj/module_001.o",
				"[400/400] CC   build/obj/module_400.o",
			},
		},
		{
			dir:           "ansi-progress",
			minByteSaving: 0.30,
			mustKeep: []string{
				"error[E0308]: mismatched types",
				"could not compile `app`",
				"42/42",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			dir := filepath.Join(testdata, tc.dir)
			input, got := filterCase(t, dir, config.Default())

			for _, must := range tc.mustKeep {
				if !strings.Contains(got, must) {
					t.Errorf("dropped something that must survive: %q", must)
				}
			}

			saving := 1 - float64(len(got))/float64(len(input))
			t.Logf("%s: %d -> %d bytes (%.1f%% saved), %d -> %d lines",
				tc.dir, len(input), len(got), saving*100,
				strings.Count(input, "\n"), strings.Count(got, "\n"))
			if saving < tc.minByteSaving {
				t.Errorf("saved %.1f%% of bytes, want at least %.1f%%", saving*100, tc.minByteSaving*100)
			}
			// No filter may ever grow the output.
			if len(got) > len(input) {
				t.Errorf("output grew: %d -> %d bytes", len(input), len(got))
			}
		})
	}
}

// TestNoANSIEscapesSurvive checks that the escape stripper leaves nothing behind.
func TestNoANSIEscapesSurvive(t *testing.T) {
	_, got := filterCase(t, filepath.Join(testdata, "ansi-progress"), config.Default())
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("an escape sequence survived:\n%q", got)
	}
	if strings.ContainsRune(got, '\r') {
		t.Errorf("a carriage return survived:\n%q", got)
	}
}

// TestFixturesHaveProvenance keeps every fixture accountable for where it came
// from, so a hand-written transcript cannot be mistaken for a real capture.
func TestFixturesHaveProvenance(t *testing.T) {
	entries, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(testdata, e.Name(), "PROVENANCE")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s has no PROVENANCE file", e.Name())
			continue
		}
		if len(strings.TrimSpace(string(b))) < 40 {
			t.Errorf("%s: PROVENANCE should say where the fixture came from", e.Name())
		}
	}
}
