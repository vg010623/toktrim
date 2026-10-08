package pipeline

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vg010623/toktrim/internal/filter"
)

// run feeds input through a processor in chunks of the given size and returns
// the filtered output.
func run(t *testing.T, input string, chunk int, opts Options) (string, *Processor) {
	t.Helper()
	if opts.RawLogDir == "" {
		opts.RawLogDir = t.TempDir()
	}
	var out bytes.Buffer
	p := New(&out, opts)
	b := []byte(input)
	if chunk <= 0 {
		chunk = len(b)
	}
	for i := 0; i < len(b); i += chunk {
		end := i + chunk
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
	return out.String(), p
}

func TestSmallOutputIsByteForByteIdentical(t *testing.T) {
	inputs := []string{
		"",
		"hi\n",
		"no trailing newline",
		"a\nb\nc\n",
		// Runs that the dedup filter would otherwise collapse.
		strings.Repeat("same line\n", 20),
		// CRLF must survive untouched.
		"one\r\ntwo\r\n",
		// Blank lines, tabs, ANSI codes: none of it is rewritten.
		"\x1b[31mred\x1b[0m\n\n\tindented\n",
	}
	for _, in := range inputs {
		for _, chunk := range []int{1, 3, 0} {
			got, p := run(t, in, chunk, Options{Filters: []filter.LineFilter{filter.NewGuard(), filter.NewDedup()}})
			if got != in {
				t.Errorf("chunk=%d: output not identical\n got %q\nwant %q", chunk, got, in)
			}
			if p.RawLogPath() != "" {
				t.Errorf("chunk=%d: passthrough should not leave a raw log", chunk)
			}
		}
	}
}

func TestLargeOutputIsFilteredAndLogged(t *testing.T) {
	// 100 identical lines: over the line threshold, and highly compressible.
	in := strings.Repeat("progress...\n", 100)
	got, p := run(t, in, 7, Options{Filters: []filter.LineFilter{filter.NewGuard(), filter.NewDedup()}})

	if strings.Count(got, "\n") >= 100 {
		t.Errorf("output was not compressed: %d lines", strings.Count(got, "\n"))
	}
	// The run collapses to its first line kept verbatim, plus a marker.
	if !strings.Contains(got, "progress...\n[toktrim] \u00d7100 similar lines collapsed") {
		t.Errorf("expected a dedup summary, got:\n%s", got)
	}

	path := p.RawLogPath()
	if path == "" {
		t.Fatal("filtered output must leave a raw log")
	}
	if !strings.Contains(got, path) {
		t.Errorf("footer must name the raw log path; got:\n%s", got)
	}
	if !strings.Contains(got, "lines elided") {
		t.Errorf("footer must report how many lines went missing; got:\n%s", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading raw log: %v", err)
	}
	if string(raw) != in {
		t.Errorf("raw log must hold the complete output: got %d bytes, want %d", len(raw), len(in))
	}
}

func TestFailureLinesSurviveCompression(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString("noise\n")
	}
	b.WriteString("npm ERR! code ELIFECYCLE\n")
	b.WriteString("npm ERR! errno 1\n")
	for i := 0; i < 60; i++ {
		b.WriteString("noise\n")
	}
	b.WriteString("Error: connect ECONNREFUSED 127.0.0.1:5432\n")
	b.WriteString("    at TCPConnectWrap.afterConnect (net.js:1146:16)\n")
	in := b.String()

	got, _ := run(t, in, 13, Options{Filters: []filter.LineFilter{filter.NewGuard(), filter.NewDedup()}})

	for _, must := range []string{
		"npm ERR! code ELIFECYCLE",
		"npm ERR! errno 1",
		"Error: connect ECONNREFUSED 127.0.0.1:5432",
		"at TCPConnectWrap.afterConnect (net.js:1146:16)",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("failure information was dropped: %q missing from:\n%s", must, got)
		}
	}
}

func TestChunkBoundariesDoNotChangeResult(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "line %d\n", i%5)
	}
	in := b.String()

	var want string
	for _, chunk := range []int{1, 2, 7, 64, 4096, 0} {
		got, _ := run(t, in, chunk, Options{
			Filters:  []filter.LineFilter{filter.NewGuard(), filter.NewDedup()},
			NoFooter: true,
		})
		if want == "" {
			want = got
			continue
		}
		if got != want {
			t.Errorf("chunk=%d changed the result\n got %q\nwant %q", chunk, got, want)
		}
	}
}

func TestNoTrailingNewlineIsStillProcessed(t *testing.T) {
	in := strings.Repeat("x\n", 50) + "last line with no newline"
	got, _ := run(t, in, 5, Options{
		Filters:  []filter.LineFilter{filter.NewGuard(), filter.NewDedup()},
		NoFooter: true,
	})
	if !strings.Contains(got, "last line with no newline") {
		t.Errorf("final partial line was dropped:\n%s", got)
	}
}

func TestRawLogRemovedWhenNothingWasTrimmed(t *testing.T) {
	// 50 distinct, incompressible lines: over the threshold, but nothing to cut.
	var b strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "unique line %d\n", i)
	}
	in := b.String()
	got, p := run(t, in, 0, Options{Filters: []filter.LineFilter{filter.NewGuard(), filter.NewDedup()}})

	if got != in {
		t.Errorf("incompressible output should come out unchanged\n got %q\nwant %q", got, in)
	}
	if p.RawLogPath() != "" {
		t.Error("no raw log should be kept when nothing was trimmed")
	}
	if strings.Contains(got, "[toktrim]") {
		t.Error("no footer should be added when nothing was trimmed")
	}
}

// TestFilteredOutputNeverExceedsRaw is item 6 of the v0.3 spec. On output where
// nothing can be dropped -- a linter run where every line is an error -- the
// filters contribute only markers, and v0.2.0's output came out larger than
// its input.
func TestFilteredOutputNeverExceedsRaw(t *testing.T) {
	// Every line is a failure line, so nothing is droppable.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "  %d:%d  error  Unexpected any. Specify a different type.  @typescript-eslint/no-explicit-any\n", i, i%40)
	}
	in := b.String()

	for _, chunk := range []int{1, 64, 4096, 0} {
		got, p := run(t, in, chunk, Options{
			Filters: []filter.LineFilter{filter.NewGuard(), filter.NewDedup(),
				filter.NewTruncate(30, 60)},
		})
		if len(got) > len(in) {
			t.Errorf("chunk=%d: output grew %d -> %d bytes (+%.2f%%)",
				chunk, len(in), len(got), float64(len(got)-len(in))/float64(len(in))*100)
		}
		// Nothing was removed, so there is nothing to recover.
		if p.RawLogPath() != "" {
			t.Errorf("chunk=%d: a raw log was kept although nothing was trimmed", chunk)
		}
	}
}

// TestRawOutputPreferredWhenFilteringDoesNotHelp checks that when the filtered
// form is no smaller, the original bytes are handed back verbatim.
func TestRawOutputPreferredWhenFilteringDoesNotHelp(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "error: distinct failure number %d that cannot be collapsed\n", i)
	}
	in := b.String()
	got, p := run(t, in, 0, Options{
		Filters: []filter.LineFilter{filter.NewGuard(), filter.NewDedup(),
			filter.NewTruncate(30, 60)},
	})
	if got != in {
		t.Errorf("incompressible output should be returned verbatim\n got %d bytes\nwant %d bytes", len(got), len(in))
	}
	if p.RawLogPath() != "" {
		t.Error("no raw log should be kept when the raw output was emitted")
	}
	if s := p.Stats(); s.Filtered {
		t.Error("Stats should report that the output was not filtered")
	}
}

// TestRawLogIsCompleteIncludingPassthroughPrefix guards a bug found while
// adding the decision window: the bytes buffered before the passthrough
// thresholds were passed were fed to the chain but never written to the log,
// so the log was missing the start of the output.
func TestRawLogIsCompleteIncludingPassthroughPrefix(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "line %d of output that must all reach the log\n", i)
	}
	in := b.String()
	// Small chunks, so the thresholds are crossed mid-stream.
	_, p := run(t, in, 37, Options{
		Filters: []filter.LineFilter{filter.NewGuard(), filter.NewDedup(),
			filter.NewTruncate(10, 20)},
	})
	path := p.RawLogPath()
	if path == "" {
		t.Fatal("expected a raw log")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != in {
		t.Errorf("raw log is not the complete output: %d bytes, want %d", len(raw), len(in))
	}
}
