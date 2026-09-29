package filter

import (
	"strconv"
	"strings"
)

// DefaultMinRun is the shortest run of identical lines worth collapsing.
const DefaultMinRun = 3

// Dedup collapses a run of identical consecutive lines into one summary line.
//
// It holds a single line back so it can tell where a run ends, which is all the
// buffering run-length encoding needs; memory does not grow with the run.
type Dedup struct {
	tracker
	MinRun int

	prev  Line
	held  bool
	count int
}

// NewDedup returns a Dedup using the default minimum run length.
func NewDedup() *Dedup { return &Dedup{MinRun: DefaultMinRun} }

// Name implements LineFilter.
func (d *Dedup) Name() string { return "dedup" }

// Process implements LineFilter.
func (d *Dedup) Process(l Line, emit Emit) {
	if l.Protected {
		d.flushRun(emit)
		emit(l)
		return
	}
	if d.held && l.Text == d.prev.Text {
		d.count++
		return
	}
	d.flushRun(emit)
	d.prev, d.held, d.count = l, true, 1
}

// Flush implements LineFilter.
func (d *Dedup) Flush(emit Emit) { d.flushRun(emit) }

func (d *Dedup) flushRun(emit Emit) {
	if !d.held {
		return
	}
	minRun := d.MinRun
	if minRun < 2 {
		minRun = DefaultMinRun
	}
	// Blank-line runs are left alone: collapsing them saves almost nothing and
	// makes the output harder to read.
	if d.count >= minRun && !blank(d.prev.Text) {
		emit(Line{Text: "[Repeated " + strconv.Itoa(d.count) + " times: " + strings.TrimRight(d.prev.Text, " \t") + "]"})
		d.markChanged()
	} else {
		for i := 0; i < d.count; i++ {
			emit(d.prev)
		}
	}
	d.held, d.count = false, 0
}
