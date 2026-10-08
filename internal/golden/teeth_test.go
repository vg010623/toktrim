package golden

import (
	"os"
	"strings"
	"testing"
)

// TestInvariantCatchesTheV020Bug runs the gap invariant against output captured
// from the released v0.2.0 binary, which is known to drop lines from the middle
// of a failure block without a marker.
//
// It exists so that the invariant test cannot quietly become a tautology: if
// this stops reporting a gap, the invariant has lost its teeth.
func TestInvariantCatchesTheV020Bug(t *testing.T) {
	const captured = "../../testdata/mocha-multi-fail/v020-output.txt"
	raw, err := os.ReadFile(captured)
	if err != nil {
		t.Skipf("no captured v0.2.0 output at %s", captured)
	}
	input, err := os.ReadFile("../../testdata/mocha-multi-fail/input.txt")
	if err != nil {
		t.Fatal(err)
	}

	blocks := failureBlocks(canonicalAll(strings.Split(string(input), "\n")))
	if len(blocks) == 0 {
		t.Fatal("expected to find failure blocks in the fixture")
	}
	outLines := canonicalAll(strings.Split(string(raw), "\n"))

	gaps := 0
	for _, b := range blocks {
		start := indexFrom(outLines, b[0], 0)
		if start < 0 {
			continue
		}
		oi := start
		for bi := 0; bi < len(b); bi++ {
			if oi < len(outLines) && outLines[oi] == b[bi] {
				oi++
				continue
			}
			if oi < len(outLines) && marker(outLines[oi]) {
				oi++
				bi--
				continue
			}
			if oi > 0 && marker(outLines[oi-1]) {
				break
			}
			gaps++
			t.Logf("gap in block %q at %q", strings.TrimSpace(b[0]), strings.TrimSpace(b[bi]))
			break
		}
	}
	if gaps == 0 {
		t.Error("the invariant found no gap in v0.2.0 output, so it would not catch the bug it exists for")
	} else {
		t.Logf("invariant correctly reports %d silently-truncated failure blocks in v0.2.0 output", gaps)
	}
}
