// Package pipeline drives command output through the filter chain.
//
// A Processor is an io.Writer, so the runner can copy the child's output into
// it as it arrives. Two guarantees shape the design:
//
//   - Small output is byte-for-byte identical to the input. Until the output
//     grows past the passthrough thresholds the raw bytes are simply held, and
//     if the command ends first they are written out untouched.
//   - Anything removed is recoverable. Once filtering begins the raw stream is
//     also written to a temp file, and the filtered output ends with a pointer
//     to it.
package pipeline

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vg010623/toktrim/internal/config"
	"github.com/vg010623/toktrim/internal/filter"
)

// Options configures a Processor.
type Options struct {
	// Filters is the chain applied to each line, in order. A Guard is always
	// installed ahead of it by New.
	Filters []filter.LineFilter
	// PassthroughLines and PassthroughBytes set the size below which output is
	// passed through unchanged. Zero selects the default.
	PassthroughLines int
	PassthroughBytes int
	// RawLogDir is where the full-output log is written. Empty selects the
	// system temp directory.
	RawLogDir string
	// Label is included in the raw log's filename to make it identifiable.
	Label string
	// NoFooter suppresses the "N lines elided" trailer. Used by tests.
	NoFooter bool
}

// Processor filters a stream of command output.
type Processor struct {
	out   io.Writer
	opts  Options
	chain []filter.LineFilter

	// Buffering phase: raw bytes held while the output might still be small
	// enough to pass through untouched.
	held      bytes.Buffer
	heldLines int
	streaming bool

	// partial holds bytes after the last newline seen so far.
	partial []byte

	passthroughBytes int

	rawLog     *os.File
	rawLogPath string
	rawLogErr  error

	inLines  int
	outLines int
	inBytes  int
	outBytes int
	closed   bool
}

// New returns a Processor writing filtered output to out.
func New(out io.Writer, opts Options) *Processor {
	if opts.PassthroughLines <= 0 {
		opts.PassthroughLines = config.DefaultPassthroughLines
	}
	if opts.PassthroughBytes <= 0 {
		opts.PassthroughBytes = config.DefaultPassthroughBytes
	}
	chain := make([]filter.LineFilter, 0, len(opts.Filters)+1)
	// The guard runs first so every later filter sees Protected already set.
	chain = append(chain, filter.NewGuard())
	chain = append(chain, opts.Filters...)
	return &Processor{out: out, opts: opts, chain: chain}
}

// Write implements io.Writer. It is safe to call with arbitrary chunk
// boundaries; lines are reassembled internally.
func (p *Processor) Write(b []byte) (int, error) {
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	n := len(b)

	if !p.streaming {
		p.held.Write(b)
		p.heldLines += bytes.Count(b, []byte{'\n'})
		if p.heldLines <= p.opts.PassthroughLines && p.held.Len() <= p.opts.PassthroughBytes {
			return n, nil
		}
		// Too big to pass through: start filtering, replaying what was held.
		if err := p.beginStreaming(); err != nil {
			return n, err
		}
		return n, nil
	}

	p.writeRawLog(b)
	p.consume(b)
	return n, nil
}

// beginStreaming switches from holding raw bytes to filtering them.
func (p *Processor) beginStreaming() error {
	p.streaming = true
	buffered := p.held.Bytes()
	p.openRawLog()
	p.writeRawLog(buffered)
	p.consume(buffered)
	p.held.Reset()
	return nil
}

// consume splits b into complete lines and feeds them through the chain.
func (p *Processor) consume(b []byte) {
	p.partial = append(p.partial, b...)
	for {
		i := bytes.IndexByte(p.partial, '\n')
		if i < 0 {
			return
		}
		line := p.partial[:i]
		p.partial = p.partial[i+1:]
		// A CRLF stream leaves a stray CR at the end of every line. Strip the
		// terminator itself; a CR used to redraw a progress line is left for
		// the ANSI filter to resolve.
		line = bytes.TrimSuffix(line, []byte{'\r'})
		p.feed(string(line))
	}
}

// feed pushes one line into the head of the chain.
func (p *Processor) feed(text string) {
	p.inLines++
	p.inBytes += len(text) + 1
	p.push(0, filter.Line{Text: text})
}

// push runs a line through the chain starting at index i.
func (p *Processor) push(i int, l filter.Line) {
	if i == len(p.chain) {
		p.emit(l)
		return
	}
	p.chain[i].Process(l, func(next filter.Line) { p.push(i+1, next) })
}

// emit writes a finished line to the output.
func (p *Processor) emit(l filter.Line) {
	p.outLines++
	p.outBytes += len(l.Text) + 1
	io.WriteString(p.out, l.Text)
	io.WriteString(p.out, "\n")
}

// Close finishes the stream: it flushes the chain, writes the passthrough bytes
// if filtering never started, and appends the elision footer.
func (p *Processor) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true

	if !p.streaming {
		// Small output: exactly the bytes we were given.
		b := p.held.Bytes()
		p.passthroughBytes = len(b)
		_, err := p.out.Write(b)
		return err
	}

	// A trailing line with no newline still has to go through the filters.
	if len(p.partial) > 0 {
		p.writeRawLogString("\n")
		p.feed(string(bytes.TrimSuffix(p.partial, []byte{'\r'})))
		p.partial = nil
	}
	for i := range p.chain {
		p.flushFrom(i)
	}

	p.finishRawLog()
	p.writeFooter()
	return nil
}

// flushFrom flushes chain[i], routing what it releases through the rest of the
// chain so later filters still see it.
func (p *Processor) flushFrom(i int) {
	p.chain[i].Flush(func(l filter.Line) { p.push(i+1, l) })
}

// changed reports whether any filter altered the stream.
func (p *Processor) changed() bool {
	if p.inLines != p.outLines {
		return true
	}
	for _, f := range p.chain {
		if f.Changed() {
			return true
		}
	}
	return false
}

// writeFooter appends the pointer back to the full output, when there is
// something to point at.
func (p *Processor) writeFooter() {
	if p.opts.NoFooter || !p.changed() {
		p.discardRawLog()
		return
	}
	elided := p.inLines - p.outLines
	var what string
	switch {
	case elided > 0:
		what = fmt.Sprintf("%d lines elided", elided)
	default:
		what = "output rewritten"
	}
	if p.rawLogPath == "" {
		fmt.Fprintf(p.out, "[toktrim] %s (raw log unavailable: %v)\n", what, p.rawLogErr)
		return
	}
	fmt.Fprintf(p.out, "[toktrim] %s — full output: %s\n", what, p.rawLogPath)
}

// Stats describes what the processor did.
type Stats struct {
	LinesIn, LinesOut int
	BytesIn, BytesOut int
	// Filtered is false when the output was small enough to pass through.
	Filtered bool
	RawLog   string
}

// Stats reports what the processor did.
func (p *Processor) Stats() Stats {
	s := Stats{
		LinesIn: p.inLines, LinesOut: p.outLines,
		BytesIn: p.inBytes, BytesOut: p.outBytes,
		Filtered: p.streaming, RawLog: p.rawLogPath,
	}
	if !p.streaming {
		// Passthrough: input and output are the same bytes.
		n := p.passthroughBytes
		if n == 0 {
			n = p.held.Len()
		}
		s.BytesIn, s.BytesOut = n, n
		s.LinesIn, s.LinesOut = p.heldLines, p.heldLines
	}
	return s
}

// RawLogPath returns the path to the full-output log, or "" if none was kept.
func (p *Processor) RawLogPath() string { return p.rawLogPath }

func (p *Processor) openRawLog() {
	dir := p.opts.RawLogDir
	if dir == "" {
		dir = os.TempDir()
	}
	label := sanitizeLabel(p.opts.Label)
	f, err := os.CreateTemp(dir, "toktrim-"+label+"-*.log")
	if err != nil {
		p.rawLogErr = err
		return
	}
	p.rawLog = f
	p.rawLogPath = f.Name()
}

func (p *Processor) writeRawLog(b []byte) {
	if p.rawLog == nil {
		return
	}
	if _, err := p.rawLog.Write(b); err != nil {
		p.rawLogErr = err
	}
}

func (p *Processor) writeRawLogString(s string) { p.writeRawLog([]byte(s)) }

func (p *Processor) finishRawLog() {
	if p.rawLog == nil {
		return
	}
	if err := p.rawLog.Close(); err != nil && p.rawLogErr == nil {
		p.rawLogErr = err
	}
	p.rawLog = nil
}

// discardRawLog removes the log when nothing was actually removed.
func (p *Processor) discardRawLog() {
	p.finishRawLog()
	if p.rawLogPath != "" {
		os.Remove(p.rawLogPath)
		p.rawLogPath = ""
	}
}

// sanitizeLabel reduces a command to something safe for a filename.
func sanitizeLabel(s string) string {
	s = filepath.Base(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
		if b.Len() >= 24 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "cmd"
	}
	return out
}
