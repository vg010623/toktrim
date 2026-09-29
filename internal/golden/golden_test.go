// Package golden holds the fixture-driven tests for the filter chain.
//
// Each directory under ../../testdata is one case:
//
//	cmd         the command line, used to pick the filter chain
//	input.txt   recorded output of that command
//	golden.txt  what toktrim is expected to produce
//	PROVENANCE  where input.txt came from
//
// Run `go test ./internal/golden -update` to rewrite the golden files after an
// intentional change, then read the diff before committing it.
package golden

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vg010623/toktrim/internal/config"
	"github.com/vg010623/toktrim/internal/filter"
	"github.com/vg010623/toktrim/internal/pipeline"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testdata = "../../testdata"

// filterCase runs one fixture through the chain its cmd file selects.
func filterCase(t *testing.T, dir string, cfg *config.Config) (input, got string) {
	t.Helper()

	inputBytes, err := os.ReadFile(filepath.Join(dir, "input.txt"))
	if err != nil {
		t.Fatalf("reading input: %v", err)
	}
	cmdBytes, err := os.ReadFile(filepath.Join(dir, "cmd"))
	if err != nil {
		t.Fatalf("reading cmd: %v", err)
	}
	argv := strings.Fields(strings.TrimSpace(string(cmdBytes)))

	var out bytes.Buffer
	p := pipeline.New(&out, pipeline.Options{
		Filters:          filter.Chain(argv, cfg),
		PassthroughLines: cfg.PassthroughLines,
		PassthroughBytes: cfg.PassthroughBytes,
		// Fixtures exist to pin the filters, so the passthrough rule is off;
		// it has its own tests in internal/pipeline.
		AlwaysFilter: true,
		// The raw log path is a temp file, so it cannot appear in a golden file.
		// Its content is asserted separately, in the pipeline tests.
		NoFooter: true,
	})
	// Feed in small chunks so the fixtures also exercise the streaming path.
	b := inputBytes
	for i := 0; i < len(b); i += 64 {
		end := i + 64
		if end > len(b) {
			end = len(b)
		}
		if _, err := p.Write(b[i:end]); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return string(inputBytes), out.String()
}

func TestGolden(t *testing.T) {
	entries, err := os.ReadDir(testdata)
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}
	cases := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cases++
		t.Run(e.Name(), func(t *testing.T) {
			dir := filepath.Join(testdata, e.Name())
			_, got := filterCase(t, dir, config.Default())

			goldenPath := filepath.Join(dir, "golden.txt")
			if *update {
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("writing golden: %v", err)
				}
				t.Logf("updated %s", goldenPath)
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden (run with -update to create it): %v", err)
			}
			if got != string(want) {
				t.Errorf("output does not match %s\n%s", goldenPath, diff(string(want), got))
			}
		})
	}
	if cases == 0 {
		t.Fatal("no fixtures found")
	}
}

// diff renders the first differing lines, which is enough to see what moved.
func diff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			b.WriteString("line " + itoa(i+1) + ":\n  want: " + w + "\n   got: " + g + "\n")
			if b.Len() > 2000 {
				b.WriteString("  ...\n")
				break
			}
		}
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
