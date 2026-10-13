package golden

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vg010623/toktrim/internal/config"
	"github.com/vg010623/toktrim/internal/filter"
)

// canonical applies the transforms toktrim is allowed to make to a line
// without that counting as dropped information.
func canonical(s string) string {
	return filter.ResolveCarriageReturns(filter.StripANSI(s))
}

func canonicalAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = canonical(l)
	}
	return out
}

// marker reports whether a line is one of toktrim's own annotations.
func marker(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[toktrim]") }

// TestFailureLinesSurviveEveryFixture is the invariant that matters most: a
// line carrying failure information must reach the output, in the order it was
// printed. toktrim may compress anything else; it may not quietly edit the
// reason a command failed.
func TestFailureLinesSurviveEveryFixture(t *testing.T) {
	forEachFixture(t, func(t *testing.T, name, input, got string) {
		var want []string
		for _, l := range strings.Split(input, "\n") {
			// Compare canonical text: stripping escapes and resolving in-place
			// redraws is a documented transform, not information loss.
			l = canonical(l)
			if filter.IsFailureLine(l) {
				want = append(want, l)
			}
		}
		if len(want) == 0 {
			t.Skip("fixture has no failure lines")
		}

		outLines := canonicalAll(strings.Split(got, "\n"))
		// Walk the output once, in order, matching each expected line.
		at := 0
		var missing []string
		for _, w := range want {
			found := false
			for j := at; j < len(outLines); j++ {
				if outLines[j] == w {
					at = j + 1
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, w)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%d failure %s missing from the output (or out of order); first few:\n  %s",
				len(missing), plural(len(missing), "line"),
				strings.Join(trim(missing, 5), "\n  "))
		}
	})
}

// TestNoSilentGapInsideFailureBlocks checks the other half of the guarantee:
// within a failure block, either every line is present or there is a marker at
// the point where lines went missing. A diff with a hole in it and no marker
// reads as complete and says something false, which was the v0.2.0 bug.
func TestNoSilentGapInsideFailureBlocks(t *testing.T) {
	forEachFixture(t, func(t *testing.T, name, input, got string) {
		blocks := failureBlocks(canonicalAll(strings.Split(input, "\n")))
		if len(blocks) == 0 {
			t.Skip("fixture has no failure blocks")
		}
		outLines := canonicalAll(strings.Split(got, "\n"))

		for _, b := range blocks {
			// Find where this block's first line landed in the output.
			start := indexFrom(outLines, b[0], 0)
			if start < 0 {
				// The whole block is absent. That is a separate failure mode,
				// reported by the test above for its failure lines; a block
				// with no failure line in it is allowed to be dropped whole.
				continue
			}
			// Walk the block and the output together.
			oi := start
			for bi := 0; bi < len(b); bi++ {
				if oi < len(outLines) && outLines[oi] == b[bi] {
					oi++
					continue
				}
				// A mismatch is only acceptable if a marker sits here.
				if oi < len(outLines) && marker(outLines[oi]) {
					oi++
					bi-- // re-test this block line against what follows
					continue
				}
				// Or if the rest of the block was cut with a marker as the last
				// thing emitted for it.
				if oi > 0 && marker(outLines[oi-1]) {
					break
				}
				t.Errorf("block starting %q: line %q is missing with no marker at that point\n"+
					"  output around the gap:\n    %s",
					strings.TrimSpace(b[0]), strings.TrimSpace(b[bi]),
					strings.Join(trim(context(outLines, oi, 3), 7), "\n    "))
				break
			}
		}
	})
}

// TestOutputNeverGrows checks that filtering a fixture cannot produce more
// bytes than it consumed.
func TestOutputNeverGrows(t *testing.T) {
	forEachFixture(t, func(t *testing.T, name, input, got string) {
		if len(got) > len(input) {
			t.Errorf("output grew: %d -> %d bytes (+%.1f%%)",
				len(input), len(got), float64(len(got)-len(input))/float64(len(input))*100)
		}
	})
}

// forEachFixture runs fn over every fixture under testdata.
func forEachFixture(t *testing.T, fn func(t *testing.T, name, input, got string)) {
	t.Helper()
	entries, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			input, got := filterCase(t, filepath.Join(testdata, e.Name()), config.Default())
			fn(t, e.Name(), input, got)
		})
	}
}

// testBlockStart is the test's own notion of a failure header, deliberately
// independent of the production pattern. If the test reused
// filter.IsBlockStart, disabling block detection in the filter would also
// disable it here and the test would pass by finding nothing to check.
var testBlockStart = regexp.MustCompile(
	`^\s*\d+\)\s` + // mocha "  1) Controller"
		`|^\s*●` + // jest
		`|^\s*not ok \d+` + // TAP
		`|AssertionError` +
		`|^\s*--- FAIL:` +
		`|^\s*Traceback \(most recent call last\)`)

// failureBlocks extracts the failure blocks from an input: a header plus the
// detail indented beneath it.
func failureBlocks(lines []string) [][]string {
	var blocks [][]string
	for i := 0; i < len(lines); i++ {
		if !testBlockStart.MatchString(lines[i]) {
			continue
		}
		base := indent(lines[i])
		block := []string{lines[i]}
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" {
				// A blank only continues the block if more detail follows.
				if j+1 < len(lines) && indent(lines[j+1]) > base {
					block = append(block, l)
					continue
				}
				break
			}
			if indent(l) > base {
				block = append(block, l)
				continue
			}
			break
		}
		if len(block) > 1 {
			blocks = append(blocks, block)
		}
		i = j - 1
	}
	return blocks
}

func indent(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' && r != '\t' {
			break
		}
		n++
	}
	return n
}

func indexFrom(lines []string, want string, from int) int {
	for i := from; i < len(lines); i++ {
		if lines[i] == want {
			return i
		}
	}
	return -1
}

func context(lines []string, at, n int) []string {
	lo, hi := at-n, at+n
	if lo < 0 {
		lo = 0
	}
	if hi > len(lines) {
		hi = len(lines)
	}
	return lines[lo:hi]
}

func trim(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(s[:n:n], "...")
}

func plural(n int, w string) string {
	if n == 1 {
		return w
	}
	return w + "s"
}

// TestQueryOutputIsNeverTruncated pins the rule that a query's output is the
// answer. A fixture whose cmd is a query must come through with every line.
//
// A tester hit this with `git log` in v0.3.1: it lost 18 of 29 commits, and
// nothing in the output said which ones. Compression is never worth that.
func TestQueryOutputIsNeverTruncated(t *testing.T) {
	entries, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(testdata, e.Name())
		cmdBytes, err := os.ReadFile(filepath.Join(dir, "cmd"))
		if err != nil {
			continue
		}
		argv := strings.Fields(strings.TrimSpace(string(cmdBytes)))
		if !filter.IsQueryCommand(argv) {
			continue
		}
		checked++
		t.Run(e.Name(), func(t *testing.T) {
			input, got := filterCase(t, dir, config.Default())
			in := strings.Split(strings.TrimRight(input, "\n"), "\n")
			out := strings.Split(strings.TrimRight(got, "\n"), "\n")

			// Every input line must appear, in order.
			at := 0
			var missing []string
			for _, l := range in {
				found := false
				for j := at; j < len(out); j++ {
					if out[j] == l {
						at, found = j+1, true
						break
					}
				}
				if !found {
					missing = append(missing, l)
				}
			}
			if len(missing) > 0 {
				t.Errorf("%d of %d lines missing from a query's output; first few:\n  %s",
					len(missing), len(in), strings.Join(trim(missing, 5), "\n  "))
			}
		})
	}
	if checked == 0 {
		t.Fatal("no query fixtures found; this invariant would pass vacuously")
	}
}
