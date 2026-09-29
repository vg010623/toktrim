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
	if !strings.Contains(got, "[Repeated 100 times: progress...]") {
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
