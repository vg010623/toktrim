package filter

import (
	"fmt"
)

// Truncate defaults.
const (
	DefaultHeadLines = 30
	DefaultTailLines = 60
	// maxProtectedHeld bounds how many middle failure lines are kept, so a
	// command that fails on every line cannot exhaust memory. The raw log still
	// holds everything.
	maxProtectedHeld = 2000
)

// Truncate keeps the first HeadLines and last TailLines of the output, plus any
// line in between that carries failure information.
//
// Order is preserved: held middle lines are tracked by position and emitted
// before the tail window they precede. It is the last filter in a chain, and
// the only one that drops lines purely because of where they are.
type Truncate struct {
	tracker
	HeadLines int
	TailLines int

	n        int       // lines seen
	held     []idxLine // middle lines carrying failure information
	tail     []idxLine // ring of the most recent TailLines lines
	heldFull bool
	skipped  int // non-protected middle lines dropped so far
}

type idxLine struct {
	idx  int
	line Line
}

// NewTruncate returns a Truncate with the given limits; zero selects defaults.
func NewTruncate(head, tail int) *Truncate {
	if head <= 0 {
		head = DefaultHeadLines
	}
	if tail <= 0 {
		tail = DefaultTailLines
	}
	return &Truncate{HeadLines: head, TailLines: tail}
}

// Name implements LineFilter.
func (t *Truncate) Name() string { return "truncate" }

// Process implements LineFilter.
func (t *Truncate) Process(l Line, emit Emit) {
	t.n++
	if t.n <= t.HeadLines {
		emit(l)
		return
	}

	// Everything past the head goes into the tail ring. Protected lines are
	// additionally remembered, so they survive being evicted from it.
	if l.Protected && len(t.held) < maxProtectedHeld {
		t.held = append(t.held, idxLine{t.n, l})
	} else if l.Protected {
		t.heldFull = true
	}

	t.tail = append(t.tail, idxLine{t.n, l})
	if len(t.tail) > t.TailLines {
		evicted := t.tail[0]
		t.tail = t.tail[1:]
		if !evicted.line.Protected {
			t.skipped++
			t.markChanged()
		}
	}
}

// Flush implements LineFilter.
func (t *Truncate) Flush(emit Emit) {
	firstTail := -1
	if len(t.tail) > 0 {
		firstTail = t.tail[0].idx
	}

	// Middle failure lines that fell out of the tail window, in order.
	var middle []idxLine
	for _, h := range t.held {
		if firstTail < 0 || h.idx < firstTail {
			middle = append(middle, h)
		}
	}

	if len(middle) > 0 {
		// The count can be zero when everything evicted was itself protected,
		// so only state a number when there is one to state.
		marker := "[toktrim] ... output elided; failures from the middle follow ..."
		if t.skipped > 0 {
			marker = fmt.Sprintf("[toktrim] ... %d %s elided; failures from the middle follow ...",
				t.skipped, plural(t.skipped, "line"))
		}
		emit(Line{Text: marker})
		for _, m := range middle {
			emit(m.line)
		}
		if t.heldFull {
			emit(Line{Text: "[toktrim] ... more failure lines elided; see the full output log ..."})
		}
		emit(Line{Text: fmt.Sprintf("[toktrim] ... resuming at the last %d %s ...", len(t.tail), plural(len(t.tail), "line"))})
	} else if t.skipped > 0 {
		emit(Line{Text: fmt.Sprintf("[toktrim] ... %d %s elided ...", t.skipped, plural(t.skipped, "line"))})
	}

	for _, e := range t.tail {
		emit(e.line)
	}
	t.tail, t.held = nil, nil
}
