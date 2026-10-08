// Package pipeline drives command output through the filter chain.
//
// A Processor is an io.Writer, so the runner can copy the child's output into
// it as it arrives. Three guarantees shape the design:
//
//   - Small output is byte-for-byte identical to the input. Until the output
//     grows past the passthrough thresholds the raw bytes are simply held, and
//     if the command ends first they are written out untouched.
//   - Filtering never makes the output bigger. While the output still fits in
//     the decision window, both the raw and the filtered form are held, and
//     whichever is smaller is what gets written.
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

// DefaultDecisionWindow is how much output is held while deciding whether
// filtering actually helped.
//
// Past this, toktrim commits to the filtered form and streams it: a command
// producing megabytes is not one where markers could plausibly outweigh what
// was removed, and holding more would risk losing buffered output if toktrim
// itself were killed.
const DefaultDecisionWindow = 1 << 20 // 1 MiB

// Options configures a Processor.
type Options struct {
	// Filters is the chain applied to each line, in order. Build it with
	// filter.Chain, which guarantees a Guard sits ahead of every filter that
	// drops lines.
	Filters []filter.LineFilter
	// PassthroughLines and PassthroughBytes set the size below which output is
	// emitted unchanged. Zero selects the default.
	PassthroughLines int
	PassthroughBytes int
	// DecisionWindow bounds how much is held while comparing the filtered form
	// against the raw one. Zero selects the default.
	DecisionWindow int
	// RawLogDir is where the full-output log is written. Empty selects the
	// system temp directory.
	RawLogDir string
	// Label is included in the raw log's filename to make it identifiable.
	Label string
	// NoFooter suppresses the "N lines elided" trailer. Used by tests.
	NoFooter bool
	// AlwaysFilter skips the passthrough check, so even small output goes
	// through the chain. Used by the fixture tests.
	AlwaysFilter bool
}

// Processor filters a stream of command output.
type Processor struct {
	out   io.Writer
	opts  Options
	chain []filter.LineFilter

	// raw holds the bytes seen so far, while they are still held.
	raw      bytes.Buffer
	rawLines int
	// filtered collects the chain's output while the decision is open.
	filtered bytes.Buffer

	// filtering becomes true once the passthrough thresholds are passed.
	filtering bool
	// holding becomes false once the decision window is passed and the
	// filtered form has been committed to.
	holding bool

	// partial holds bytes after the last newline seen so far.
	partial []byte
	// handled counts bytes of raw already written to the log and fed to the
	// chain. One offset for both, so the log cannot miss the bytes that were
	// buffered before filtering began -- which is exactly what it did when
	// they were tracked separately.
	handled int

	rawLog     *os.File
	rawLogPath string
	rawLogErr  error

	inLines    int
	outLines   int
	inBytes    int
	outBytes   int
	emittedRaw bool
	closed     bool
}

// New returns a Processor writing filtered output to out.
func New(out io.Writer, opts Options) *Processor {
	if opts.PassthroughLines <= 0 {
		opts.PassthroughLines = config.DefaultPassthroughLines
	}
	if opts.PassthroughBytes <= 0 {
		opts.PassthroughBytes = config.DefaultPassthroughBytes
	}
	if opts.DecisionWindow <= 0 {
		opts.DecisionWindow = DefaultDecisionWindow
	}
	return &Processor{out: out, opts: opts, chain: opts.Filters, holding: true}
}

// dest is where finished lines go: a buffer while the decision is open, the
// real output once it has been made.
func (p *Processor) dest() io.Writer {
	if p.holding {
		return &p.filtered
	}
	return p.out
}

// Write implements io.Writer. It is safe to call with arbitrary chunk
// boundaries; lines are reassembled internally.
func (p *Processor) Write(b []byte) (int, error) {
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	n := len(b)

	if p.holding {
		p.raw.Write(b)
		p.rawLines += bytes.Count(b, []byte{'\n'})

		if !p.filtering {
			underThresholds := p.rawLines <= p.opts.PassthroughLines &&
				p.raw.Len() <= p.opts.PassthroughBytes
			if underThresholds && !p.opts.AlwaysFilter {
				return n, nil
			}
			p.filtering = true
			p.openRawLog()
		}
		// Hand the chain and the log everything not yet handled. On the write
		// that trips the thresholds this is the whole buffer, including the
		// bytes that arrived while the output might still have passed through.
		all := p.raw.Bytes()
		fresh := all[p.handled:]
		p.writeRawLog(fresh)
		p.consume(fresh)
		p.handled = len(all)
		p.commitIfPastWindow()
		return n, nil
	}

	p.writeRawLog(b)
	p.consume(b)
	return n, nil
}

// commitIfPastWindow gives up on the raw/filtered comparison once too much has
// been held, writes out what the chain has produced so far, and streams from
// then on.
func (p *Processor) commitIfPastWindow() {
	if !p.holding || p.raw.Len() <= p.opts.DecisionWindow {
		return
	}
	p.holding = false
	p.out.Write(p.filtered.Bytes())
	p.filtered.Reset()
	// The raw bytes can no longer be reconsidered; the log still has them.
	p.raw.Reset()
	p.handled = 0
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

// emit writes a finished line to the current destination.
func (p *Processor) emit(l filter.Line) {
	p.outLines++
	p.outBytes += len(l.Text) + 1
	w := p.dest()
	io.WriteString(w, l.Text)
	io.WriteString(w, "\n")
}

// Close finishes the stream.
func (p *Processor) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true

	if !p.filtering {
		// Small output: exactly the bytes we were given.
		p.emittedRaw = true
		_, err := p.out.Write(p.raw.Bytes())
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

	if !p.holding {
		// Already committed and streamed; only the trailer is left.
		p.writeFooter(p.out)
		return nil
	}

	// The whole output fits in the window, so the comparison is exact: write
	// the footer into the buffer and then pick the smaller form.
	p.writeFooter(&p.filtered)
	if p.filtered.Len() >= p.raw.Len() {
		// Filtering did not pay for itself. Hand back the original bytes and
		// keep no log: there is nothing to recover.
		p.emittedRaw = true
		p.discardRawLog()
		_, err := p.out.Write(p.raw.Bytes())
		return err
	}
	_, err := p.out.Write(p.filtered.Bytes())
	return err
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
func (p *Processor) writeFooter(w io.Writer) {
	if p.opts.NoFooter || !p.changed() {
		p.discardRawLog()
		return
	}
	elided := p.inLines - p.outLines
	what := "output rewritten"
	if elided > 0 {
		what = fmt.Sprintf("%d lines elided", elided)
	}
	if p.rawLogPath == "" {
		fmt.Fprintf(w, "[toktrim] %s (raw log unavailable: %v)\n", what, p.rawLogErr)
		return
	}
	fmt.Fprintf(w, "[toktrim] %s — full output: %s\n", what, p.rawLogPath)
}

// Stats describes what the processor did.
type Stats struct {
	LinesIn, LinesOut int
	BytesIn, BytesOut int
	// Filtered is false when the output was emitted unchanged, either because
	// it was small or because filtering did not shrink it.
	Filtered bool
	RawLog   string
	// Filters names the filters that altered the stream.
	Filters []string
}

// Stats reports what the processor did.
func (p *Processor) Stats() Stats {
	s := Stats{
		LinesIn: p.inLines, LinesOut: p.outLines,
		BytesIn: p.inBytes, BytesOut: p.outBytes,
		Filtered: p.filtering && !p.emittedRaw,
		RawLog:   p.rawLogPath,
	}
	if p.emittedRaw {
		n := p.raw.Len()
		s.BytesIn, s.BytesOut = n, n
		s.LinesOut = s.LinesIn
		if !p.filtering {
			s.LinesIn, s.LinesOut = p.rawLines, p.rawLines
		}
		return s
	}
	for _, f := range p.chain {
		if f.Changed() {
			s.Filters = append(s.Filters, f.Name())
		}
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
	f, err := os.CreateTemp(dir, "toktrim-"+sanitizeLabel(p.opts.Label)+"-*.log")
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
