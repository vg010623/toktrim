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
	want := []string{"x", "noise", "[toktrim] \u00d74 similar lines collapsed", "y"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
	if !d.Changed() {
		t.Error("Dedup should report that it changed the stream")
	}
}

func TestDedupLeavesShortRunsAlone(t *testing.T) {
	d := NewDedup()
	in := []string{"a", "a", "b"}
	got := texts(collect(d, in))
	if strings.Join(got, "|") != strings.Join(in, "|") {
		t.Errorf("got %v, want %v", got, in)
	}
	if d.Changed() {
		t.Error("Dedup should not report a change when it collapsed nothing")
	}
}

func TestDedupCollapsesBlankRuns(t *testing.T) {
	// Blanks are separators. Once interleaved content has been dropped they
	// pile up, so a run of them collapses to one.
	d := NewDedup()
	got := texts(collect(d, []string{"a", "", "", "", "", "b"}))
	want := []string{"a", "", "b"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDedupNormalizesOnlyLogNoise(t *testing.T) {
	// Log-shaped lines whose numbers are measurements collapse by shape.
	noise := []string{
		`info: [db] SELECT * FROM t WHERE id = 1 {"ms":3}`,
		`info: [db] SELECT * FROM t WHERE id = 2 {"ms":41}`,
		`info: [db] SELECT * FROM t WHERE id = 3 {"ms":7}`,
		`info: [db] SELECT * FROM t WHERE id = 4 {"ms":12}`,
	}
	got := texts(collect(NewDedup(), noise))
	if len(got) != 2 || got[0] != noise[0] || !strings.Contains(got[1], "×4 similar lines") {
		t.Errorf("log noise should collapse by shape, got %v", got)
	}

	// Lines where the number IS the content must survive, even though they
	// differ only in digits. This is the case that makes blanket
	// normalisation unsafe.
	for _, in := range [][]string{
		{"unique line 0", "unique line 1", "unique line 2", "unique line 3"},
		{"[  1/400] CC build/obj/module_001.o", "[  2/400] CC build/obj/module_002.o",
			"[  3/400] CC build/obj/module_003.o", "[  4/400] CC build/obj/module_004.o"},
		{"test/a_1.js:10:3", "test/a_2.js:11:3", "test/a_3.js:12:3", "test/a_4.js:13:3"},
	} {
		got := texts(collect(NewDedup(), in))
		if strings.Join(got, "|") != strings.Join(in, "|") {
			t.Errorf("identifiers must not be collapsed\n got %v\nwant %v", got, in)
		}
	}
}

func TestSimilarityKey(t *testing.T) {
	same := func(a, b string) {
		t.Helper()
		if SimilarityKey(a) != SimilarityKey(b) {
			t.Errorf("%q and %q should share a key (%q vs %q)", a, b, SimilarityKey(a), SimilarityKey(b))
		}
	}
	differ := func(a, b string) {
		t.Helper()
		if SimilarityKey(a) == SimilarityKey(b) {
			t.Errorf("%q and %q should not share a key (both %q)", a, b, SimilarityKey(a))
		}
	}
	same(`took 3ms`, `took 418ms`)
	same(`id 550e8400-e29b-41d4-a716-446655440000 ok`, `id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 ok`)
	same(`layer sha256:4bfd66e1e1f4`, `layer sha256:aa1b2c3d4e5f`)
	same(`loaded "src/a.js"`, `loaded "src/bbbb.js"`)
	same(`2026-01-02T03:04:05Z done`, `2026-09-30T11:22:33Z done`)
	same(`addr 0xdeadbeef`, `addr 0x1234`)
	differ(`SELECT * FROM users`, `SELECT * FROM orders`)
	differ(`compiling parser`, `compiling lexer`)
}
