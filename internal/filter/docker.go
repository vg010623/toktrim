package filter

import (
	"fmt"
	"regexp"
)

var (
	// BuildKit prefixes every line of a step with "#<n> ".
	dockerStepLine = regexp.MustCompile(`^#(\d+)\s+(.*)$`)
	// "#5 [2/6] RUN npm ci" or "#1 [internal] load build definition"
	dockerStepHeader = regexp.MustCompile(`^(\[[^\]]+\]|CACHED)\s`)
	// Layer digests, transfer and extraction progress.
	dockerStepProgress = regexp.MustCompile(`^sha256:[0-9a-f]+` +
		`|^(transferring|extracting|resolve|pulling|download|sending)\b` +
		`|^\.{3,}$`)
	// Terminal status for a step.
	dockerStepDone  = regexp.MustCompile(`^DONE\s`)
	dockerStepError = regexp.MustCompile(`^(ERROR|CANCELED)\b`)
)

// maxDockerStepLines bounds how much of one step toktrim holds. A step is only
// buffered until it reports DONE or ERROR, but a step that prints a million
// lines before failing must not exhaust memory; the raw log still has all of it.
const maxDockerStepLines = 4000

// DockerBuild compresses `docker build` output.
//
// Each step's layer and download progress collapses to a count. Step headers,
// the final image ID and the result lines are kept. A step that fails is
// replayed in full, which is why steps are buffered until they report a status:
// whether a step's output matters is only known once it finishes.
type DockerBuild struct {
	tracker

	steps map[string]*dockerStep
	order []string
}

type dockerStep struct {
	id       string
	header   Line
	hasHead  bool
	body     []Line
	progress int
	overflow int
	failed   bool
}

// NewDockerBuild returns a DockerBuild filter.
func NewDockerBuild() *DockerBuild {
	return &DockerBuild{steps: map[string]*dockerStep{}}
}

// Name implements LineFilter.
func (d *DockerBuild) Name() string { return "docker-build" }

// Process implements LineFilter.
func (d *DockerBuild) Process(l Line, emit Emit) {
	m := dockerStepLine.FindStringSubmatch(l.Text)
	if m == nil {
		// Not step output: blank separators go, result lines stay.
		if blank(l.Text) && !l.Protected {
			d.markChanged()
			return
		}
		emit(l)
		return
	}

	id, rest := m[1], m[2]
	st := d.steps[id]
	if st == nil {
		// Only start buffering once a step announces itself with a header.
		// BuildKit's "#0 building with ..." preamble is not a step, and lines
		// for a step whose header was never seen are passed straight through
		// rather than held back and reordered.
		if !dockerStepHeader.MatchString(rest) {
			emit(l)
			return
		}
		st = &dockerStep{id: id, header: l, hasHead: true}
		d.steps[id] = st
		d.order = append(d.order, id)
		return
	}

	switch {
	case dockerStepDone.MatchString(rest):
		d.finish(st, l, false, emit)
		return
	case dockerStepError.MatchString(rest):
		// The status line is emitted by finish; adding it to the body too would
		// print it twice.
		st.failed = true
		d.finish(st, l, true, emit)
		return
	case l.Protected:
		st.failed = true
		st.body = append(st.body, l)
		return
	case dockerStepProgress.MatchString(rest) || IsProgressLine(rest):
		st.progress++
		d.markChanged()
		return
	}

	if len(st.body) < maxDockerStepLines {
		st.body = append(st.body, l)
	} else {
		st.overflow++
		d.markChanged()
	}
}

// finish emits a completed step: a one-line summary when it succeeded, the full
// buffered output when it failed.
func (d *DockerBuild) finish(st *dockerStep, status Line, failed bool, emit Emit) {
	if st.hasHead {
		emit(st.header)
	}
	if failed || st.failed {
		for _, b := range st.body {
			emit(b)
		}
		if st.overflow > 0 {
			emit(Line{Text: fmt.Sprintf("#%s [toktrim] %d further %s elided; see the full output log", st.id, st.overflow, plural(st.overflow, "line"))})
		}
	} else if n := len(st.body) + st.progress; n > 0 {
		emit(Line{Text: fmt.Sprintf("#%s [toktrim] %d %s of layer/progress output elided", st.id, n, plural(n, "line"))})
		d.markChanged()
	}
	emit(status)
	delete(d.steps, st.id)
}

// Flush implements LineFilter. Steps still open when the build ends -- a build
// killed by a timeout, say -- are emitted rather than lost.
func (d *DockerBuild) Flush(emit Emit) {
	for _, id := range d.order {
		st := d.steps[id]
		if st == nil {
			continue
		}
		if st.hasHead {
			emit(st.header)
		}
		for _, b := range st.body {
			emit(b)
		}
		if st.progress > 0 {
			emit(Line{Text: fmt.Sprintf("#%s [toktrim] %d %s of layer/progress output elided", st.id, st.progress, plural(st.progress, "line"))})
		}
		delete(d.steps, id)
	}
	d.order = nil
}
