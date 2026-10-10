package filter

import (
	"regexp"
	"strconv"
	"strings"
)

// Dedup defaults.
const (
	// DefaultMinRun is the shortest run worth collapsing.
	DefaultMinRun = 3
	// DefaultSimilarWindow is how far back a repeat is still a repeat rather
	// than new information.
	DefaultSimilarWindow = 50
	// maxSampleLen bounds the sample quoted in a cross-gap marker.
	maxSampleLen = 70
	// minSuppressibleLen is the shortest line worth suppressing across a gap.
	// A marker costs roughly this much, so collapsing shorter lines churns the
	// output without shrinking it.
	minSuppressibleLen = 24
)

// Normalisers applied in order to build a line's shape. The order matters: a
// UUID and a git hash are both made of digits, so they have to be recognised
// before bare numbers are blanked.
var (
	reUUID      = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reISOTime   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)
	reHexNumber = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	reHexHash   = regexp.MustCompile(`(?i)\b(sha256:)?[0-9a-f]{7,}\b`)
	reDQuoted   = regexp.MustCompile(`"[^"]*"`)
	reSQuoted   = regexp.MustCompile(`'[^']*'`)
	reNumber    = regexp.MustCompile(`\d+(\.\d+)?`)
)

// logNoisePattern matches a line whose varying parts are measurements rather
// than identifiers, which is the only place matching by shape is safe.
//
// `info: [db] SELECT ... {"ms":3}` and `...{"ms":41}` say the same thing; the
// duration is incidental. `unique line 7` and `[  7/400] CC module_007.o` do
// not: there the number *is* the content, and collapsing them would throw away
// which line or which file it was. Rather than guess from the shape of the
// number, shape matching is restricted to lines that announce themselves as
// logging.
var logNoisePattern = regexp.MustCompile(
	`(?i)^\s*\[?(info|warn|warning|debug|trace|verbose|silly|notice|log)\]?\s*[:|\]]` +
		`|(?i)^\s*deprecation warning\b` +
		`|(?i)^\s*\[deprecation\]` +
		`|(?i)^\s*(npm|pnpm|yarn)\s+(warn|info|http|timing|verb|verbose|silly)\b` +
		`|(?i)^\s*<[wie]>\s`, // some build tools prefix severity like <w>
)

// NormalizableNoise reports whether a line may be matched by shape rather than
// by bytes.
func NormalizableNoise(s string) bool { return logNoisePattern.MatchString(s) }

// SimilarityKey reduces a line to the shape it shares with its near-identical
// neighbours.
func SimilarityKey(s string) string {
	s = strings.TrimRight(s, " \t")
	s = reISOTime.ReplaceAllString(s, "<ts>")
	s = reUUID.ReplaceAllString(s, "<uuid>")
	s = reHexNumber.ReplaceAllString(s, "<hex>")
	s = reHexHash.ReplaceAllString(s, "<hash>")
	s = reDQuoted.ReplaceAllString(s, `"<s>"`)
	s = reSQuoted.ReplaceAllString(s, `'<s>'`)
	s = reNumber.ReplaceAllString(s, "<n>")
	return s
}

// Dedup collapses repeated lines, by two rules.
//
// An *adjacent* run collapses to its first line plus a marker placed exactly
// where the run ended. A line repeating an earlier one *across a gap*, within
// SimilarWindow lines, is dropped once that shape has already appeared a few
// times, and the total is reported once at the end.
//
// Both rules compare by shape for log-shaped lines and byte for byte for
// everything else. The asymmetry is the point. An application log alternates
// between shapes -- a db line, then an http line, then a db line -- so its
// repeats are never adjacent and the run rule alone would never fire. But
// matching *any* line by shape is unsafe: "Suite1 Controller" and "Suite7
// Controller" differ only in a digit, and collapsing them would throw away
// which suites ran. Restricting shape matching to lines that announce
// themselves as logging gets the compression without that risk.
//
// Nothing inside a failure block is ever touched: an assertion diff is full of
// lines that differ only in a number, and that number is the whole point.
type Dedup struct {
	tracker
	MinRun        int
	SimilarWindow int
	Normalize     bool

	// The adjacent run currently being held.
	prev    Line
	prevKey string
	held    bool
	count   int

	// Cross-gap bookkeeping, keyed by shape.
	lineNo    int
	lastSeen  map[string]int
	seenCount map[string]int
	extra     map[string]int
	sample    map[string]string
	order     []string
}

// NewDedup returns a Dedup using the default run length and window.
func NewDedup() *Dedup {
	return &Dedup{
		MinRun:        DefaultMinRun,
		SimilarWindow: DefaultSimilarWindow,
		Normalize:     true,
		lastSeen:      map[string]int{},
		seenCount:     map[string]int{},
		extra:         map[string]int{},
		sample:        map[string]string{},
	}
}

// Name implements LineFilter.
func (d *Dedup) Name() string { return "dedup" }

// key returns the comparison key for a line.
func (d *Dedup) key(text string) string {
	if d.Normalize && NormalizableNoise(text) {
		return SimilarityKey(text)
	}
	return text
}

// Process implements LineFilter.
func (d *Dedup) Process(l Line, emit Emit) {
	// Anything carrying failure information goes straight through.
	if l.Protected || l.InFailureBlock || IsFailureLine(l.Text) {
		d.flushRun(emit)
		d.lineNo++
		emit(l)
		return
	}

	d.lineNo++
	k := d.key(l.Text)

	// Extend the run being held. The window is deliberately not consulted
	// here: a run of one shape should collapse as a run, which reads better
	// and leaves one marker instead of many.
	if d.held && k == d.prevKey {
		d.count++
		return
	}

	d.flushRun(emit)
	d.prev, d.prevKey, d.held, d.count = l, k, true, 1
}

// Flush implements LineFilter.
func (d *Dedup) Flush(emit Emit) {
	d.flushRun(emit)
	// Cross-gap repeats, reported once each, in first-seen order. There is no
	// single point in the stream where these belong, so they are summarised at
	// the end rather than guessed at.
	for _, k := range d.order {
		n := d.extra[k]
		if n == 0 {
			continue
		}
		emit(Line{Text: "[toktrim] " + times(n) + " further similar " +
			plural(n, "line") + ": " + truncateSample(d.sample[k])})
	}
	d.extra = map[string]int{}
	d.order = nil
}

// flushRun releases the run being held.
func (d *Dedup) flushRun(emit Emit) {
	if !d.held {
		return
	}
	text := d.prev.Text
	count := d.count
	d.held, d.count = false, 0

	// A run of blank lines collapses to one. Blanks are separators, and once
	// interleaved content is dropped they pile up.
	if blank(text) {
		if count > 1 {
			d.markChanged()
		}
		emit(d.prev)
		return
	}

	minRun := d.MinRun
	if minRun < 2 {
		minRun = DefaultMinRun
	}
	k := d.key(text)

	if count >= minRun {
		emit(d.prev)
		emit(Line{Text: "[toktrim] " + times(count) + " similar " +
			plural(count, "line") + " collapsed"})
		d.markChanged()
		d.note(k, text)
		return
	}

	if d.shouldSuppress(k, text) {
		d.extra[k] += count
		d.markChanged()
		return
	}
	for i := 0; i < count; i++ {
		emit(d.prev)
	}
	d.note(k, text)
}

// shouldSuppress reports whether a line repeating an earlier one within the
// window should be dropped.
//
// A shape has to appear keepFirst times before later occurrences go. That
// bound is what makes the single per-shape marker cheaper than the lines it
// stands in for: if anything is suppressed at all, several lines were.
func (d *Dedup) shouldSuppress(key, text string) bool {
	if d.SimilarWindow <= 0 || len(text) < minSuppressibleLen {
		return false
	}
	seen, ok := d.lastSeen[key]
	if !ok || d.lineNo-seen > d.SimilarWindow {
		return false
	}
	keepFirst := d.MinRun - 1
	if keepFirst < 2 {
		keepFirst = 2
	}
	d.lastSeen[key] = d.lineNo
	d.seenCount[key]++
	return d.seenCount[key] > keepFirst
}

// note records that a line was emitted, for the window.
func (d *Dedup) note(key, text string) {
	if d.SimilarWindow <= 0 {
		return
	}
	if _, ok := d.lastSeen[key]; !ok {
		d.order = append(d.order, key)
		d.sample[key] = text
	}
	d.lastSeen[key] = d.lineNo
	d.seenCount[key]++
	d.evict()
}

// evict drops bookkeeping for shapes out of range, so memory does not grow
// with the length of the output.
func (d *Dedup) evict() {
	if len(d.lastSeen) < 4096 {
		return
	}
	for k, seen := range d.lastSeen {
		if d.lineNo-seen > d.SimilarWindow && d.extra[k] == 0 {
			delete(d.lastSeen, k)
			delete(d.seenCount, k)
			delete(d.sample, k)
		}
	}
}

// times renders a repeat count as ×N.
func times(n int) string { return "×" + strconv.Itoa(n) }

// truncateSample shortens a line for quoting inside a marker.
func truncateSample(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxSampleLen {
		return s
	}
	return s[:maxSampleLen] + "…"
}
