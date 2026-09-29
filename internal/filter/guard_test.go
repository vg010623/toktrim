package filter

import (
	"strings"
	"testing"
)

// collect runs lines through a filter and returns what came out.
func collect(f LineFilter, lines []string) []Line {
	var out []Line
	emit := func(l Line) { out = append(out, l) }
	for _, s := range lines {
		f.Process(Line{Text: s}, emit)
	}
	f.Flush(emit)
	return out
}

func texts(ls []Line) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Text
	}
	return out
}

func TestIsFailureLine(t *testing.T) {
	keep := []string{
		"npm ERR! code ELIFECYCLE",
		"Error: cannot find module 'x'",
		"error[E0308]: mismatched types",
		"FAILED tests/test_api.py::test_login",
		"  1 failing",
		"panic: runtime error: index out of range",
		"thread 'main' panicked at src/lib.rs:4:5",
		"Traceback (most recent call last):",
		`  File "app.py", line 12, in <module>`,
		"    at Object.<anonymous> (test/bar.test.js:10:22)",
		"Unhandled exception. System.InvalidOperationException: nope",
		"Program.cs(12,9): error CS1002: ; expected",
		"fatal: not a git repository",
		"AssertionError: expected 1 to equal 2",
		"--- FAIL: TestFoo (0.00s)",
		"Segmentation fault",
		"#3  0x00007ffff7a3b1c7 in abort ()",
	}
	for _, s := range keep {
		if !IsFailureLine(s) {
			t.Errorf("IsFailureLine(%q) = false, want true", s)
		}
	}

	drop := []string{
		"added 214 packages in 6s",
		"npm WARN deprecated request@2.88.2: no longer maintained",
		"Compiling error_handling v0.1.0",     // word-boundary: not a failure
		"reify:lodash: timing reifyNode:... ", // npm progress
		"#5 [2/6] RUN npm ci",
		"  ✓ renders without crashing (23 ms)",
		"Restored /src/App/App.csproj (in 1.2 sec).",
	}
	for _, s := range drop {
		if IsFailureLine(s) {
			t.Errorf("IsFailureLine(%q) = true, want false", s)
		}
	}
}

func TestGuardProtectsContextAroundFailures(t *testing.T) {
	in := []string{
		"a1", "a2", "a3", "a4", "a5", "a6",
		"Error: boom",
		"b1", "b2", "b3", "b4", "b5",
	}
	g := &Guard{ContextBefore: 2, ContextAfter: 2}
	got := collect(g, in)

	if len(got) != len(in) {
		t.Fatalf("Guard dropped lines: got %d, want %d", len(got), len(in))
	}
	protected := map[string]bool{}
	for _, l := range got {
		if l.Protected {
			protected[l.Text] = true
		}
	}
	for _, want := range []string{"a5", "a6", "Error: boom", "b1", "b2"} {
		if !protected[want] {
			t.Errorf("%q should be protected", want)
		}
	}
	for _, notWant := range []string{"a1", "a2", "a3", "a4", "b3", "b4", "b5"} {
		if protected[notWant] {
			t.Errorf("%q should not be protected", notWant)
		}
	}
}

func TestGuardPreservesOrder(t *testing.T) {
	in := []string{"1", "2", "Error: x", "3", "4", "5", "6", "7"}
	got := texts(collect(NewGuard(), in))
	if strings.Join(got, ",") != strings.Join(in, ",") {
		t.Errorf("order changed: got %v, want %v", got, in)
	}
}

func TestDedupLeavesProtectedRunsAlone(t *testing.T) {
	g := NewGuard()
	d := NewDedup()
	// Route guard output into dedup, as the pipeline does.
	var out []Line
	emit := func(l Line) { out = append(out, l) }
	feed := func(s string) {
		g.Process(Line{Text: s}, func(l Line) { d.Process(l, emit) })
	}
	for i := 0; i < 5; i++ {
		feed("npm ERR! repeated failure detail")
	}
	g.Flush(func(l Line) { d.Process(l, emit) })
	d.Flush(emit)

	if len(out) != 5 {
		t.Fatalf("protected lines must not be collapsed: got %d lines\n%v", len(out), texts(out))
	}
}

func TestDedupCollapsesUnprotectedRuns(t *testing.T) {
	d := NewDedup()
	got := texts(collect(d, []string{"x", "noise", "noise", "noise", "noise", "y"}))
	want := []string{"x", "[Repeated 4 times: noise]", "y"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
	if !d.Changed() {
		t.Error("Dedup should report that it changed the stream")
	}
}

func TestDedupLeavesShortRunsAndBlanksAlone(t *testing.T) {
	d := NewDedup()
	in := []string{"a", "a", "", "", "", "", "b"}
	got := texts(collect(d, in))
	if strings.Join(got, "|") != strings.Join(in, "|") {
		t.Errorf("got %v, want %v", got, in)
	}
	if d.Changed() {
		t.Error("Dedup should not report a change when it collapsed nothing")
	}
}
