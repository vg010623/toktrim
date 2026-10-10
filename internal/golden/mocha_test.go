package golden

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vg010623/toktrim/internal/config"
)

var (
	diffLine     = regexp.MustCompile(`^\s*[-+]\s*\S`)
	fileLineRef  = regexp.MustCompile(`[\w./-]+\.(js|ts|jsx|tsx|go|py|rs|cs):\d+(:\d+)?`)
	mochaFailHdr = regexp.MustCompile(`^\s*\d+\) `)
)

// TestMochaFailuresSurviveExhaustively is the assertion the v0.3 spec asks
// for: *every* +/- diff line and *every* file:line reference in the input has
// to appear in the output, not a representative sample.
//
// v0.2.0 kept 6 of 6 `+  "total": 120` lines and 3 of 6 `-  "total": 118`
// lines, which a spot check of the expected value alone would have passed.
func TestMochaFailuresSurviveExhaustively(t *testing.T) {
	for _, fixture := range []string{"mocha-multi-fail", "mocha-multi-fail-oldglyph"} {
		t.Run(fixture, func(t *testing.T) {
			input, got := filterCase(t, filepath.Join(testdata, fixture), config.Default())

			var wantDiff, wantRefs []string
			for _, l := range strings.Split(input, "\n") {
				if diffLine.MatchString(l) {
					wantDiff = append(wantDiff, l)
				}
				for _, m := range fileLineRef.FindAllString(l, -1) {
					wantRefs = append(wantRefs, m)
				}
			}
			if len(wantDiff) == 0 || len(wantRefs) == 0 {
				t.Fatalf("fixture has nothing to check: %d diff lines, %d refs",
					len(wantDiff), len(wantRefs))
			}

			// Every diff line, counted: a diff that keeps five of six lines is
			// worse than one that keeps none, because it still looks complete.
			outLines := strings.Split(got, "\n")
			have := map[string]int{}
			for _, l := range outLines {
				have[l]++
			}
			want := map[string]int{}
			for _, l := range wantDiff {
				want[l]++
			}
			for l, n := range want {
				if have[l] < n {
					t.Errorf("diff line %q: %d of %d occurrences survived", strings.TrimSpace(l), have[l], n)
				}
			}

			for _, ref := range wantRefs {
				if !strings.Contains(got, ref) {
					t.Errorf("file:line reference %q is missing from the output", ref)
				}
			}

			// And the headers, so a whole failure cannot vanish.
			var wantHdrs int
			for _, l := range strings.Split(input, "\n") {
				if mochaFailHdr.MatchString(l) {
					wantHdrs++
				}
			}
			var gotHdrs int
			for _, l := range outLines {
				if mochaFailHdr.MatchString(l) {
					gotHdrs++
				}
			}
			if gotHdrs != wantHdrs {
				t.Errorf("%d of %d failure headers survived", gotHdrs, wantHdrs)
			}
		})
	}
}

// TestMochaBlocksAreByteForByte checks that each failure block is reproduced
// exactly, which is what the spec means by "keep all six blocks".
func TestMochaBlocksAreByteForByte(t *testing.T) {
	input, got := filterCase(t, filepath.Join(testdata, "mocha-multi-fail"), config.Default())

	inBlocks := splitMochaBlocks(strings.Split(input, "\n"))
	outBlocks := splitMochaBlocks(strings.Split(got, "\n"))

	if len(inBlocks) != 6 {
		t.Fatalf("expected 6 failure blocks in the fixture, found %d", len(inBlocks))
	}
	if len(outBlocks) != len(inBlocks) {
		t.Fatalf("output has %d failure blocks, want %d", len(outBlocks), len(inBlocks))
	}
	for i := range inBlocks {
		a := trimTrailingBlanks(inBlocks[i])
		b := trimTrailingBlanks(outBlocks[i])
		if strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("block %d differs\n--- input ---\n%s\n--- output ---\n%s",
				i+1, strings.Join(a, "\n"), strings.Join(b, "\n"))
		}
	}
}

// splitMochaBlocks groups lines into mocha's numbered failure blocks.
func splitMochaBlocks(lines []string) [][]string {
	var blocks [][]string
	var cur []string
	for _, l := range lines {
		if mochaFailHdr.MatchString(l) {
			if cur != nil {
				blocks = append(blocks, cur)
			}
			cur = []string{l}
			continue
		}
		if cur == nil {
			continue
		}
		if marker(l) {
			// toktrim's own annotations are not part of what mocha printed.
			continue
		}
		cur = append(cur, l)
	}
	if cur != nil {
		blocks = append(blocks, cur)
	}
	return blocks
}

func trimTrailingBlanks(s []string) []string {
	for len(s) > 0 && strings.TrimSpace(s[len(s)-1]) == "" {
		s = s[:len(s)-1]
	}
	return s
}
