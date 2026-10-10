package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TOKTRIM_STATE_DIR", dir)
	t.Setenv("TOKTRIM_NO_LEDGER", "")

	Append(Entry{Command: "npm run build", Filters: []string{"ansi", "dedup"},
		RawBytes: 1000, OutBytes: 200, ExitCode: 0})
	Append(Entry{Command: "npm test", RawBytes: 500, OutBytes: 500, ExitCode: 1})

	got, err := Read(time.Time{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d entries, want 2", len(got))
	}
	if got[0].Command != "npm run build" || got[0].Saved() != 800 {
		t.Errorf("first entry = %+v", got[0])
	}
	if got[1].ExitCode != 1 || got[1].Saved() != 0 {
		t.Errorf("second entry = %+v", got[1])
	}
	if got[0].Time.IsZero() {
		t.Error("Append should stamp a time")
	}
}

func TestAppendIsSilentOnFailure(t *testing.T) {
	// A path that cannot be created must not panic or report anything.
	t.Setenv("TOKTRIM_STATE_DIR", filepath.Join(os.DevNull, "nope"))
	Append(Entry{Command: "x", RawBytes: 1, OutBytes: 1})
}

func TestNoLedgerEnvDisablesRecording(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TOKTRIM_STATE_DIR", dir)
	t.Setenv("TOKTRIM_NO_LEDGER", "1")
	Append(Entry{Command: "npm test", RawBytes: 10, OutBytes: 1})
	if entries, _ := Read(time.Time{}); len(entries) != 0 {
		t.Errorf("TOKTRIM_NO_LEDGER=1 should record nothing, got %d entries", len(entries))
	}
}

func TestReadSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TOKTRIM_STATE_DIR", dir)
	path := filepath.Join(dir, "ledger.jsonl")
	body := `{"command":"a","raw_bytes":10,"out_bytes":2}
this is not json
{"command":"b","raw_bytes":20,"out_bytes":4}
{"command":"truncated
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(time.Time{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// A half-written line from a concurrent run must not hide the good ones.
	if len(got) != 2 {
		t.Fatalf("read %d entries, want the 2 valid ones", len(got))
	}
}

func TestReadMissingLedgerIsNotAnError(t *testing.T) {
	t.Setenv("TOKTRIM_STATE_DIR", t.TempDir())
	got, err := Read(time.Time{})
	if err != nil {
		t.Errorf("Read on a missing ledger should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want none", len(got))
	}
}

func TestReadHonoursSince(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TOKTRIM_STATE_DIR", dir)
	t.Setenv("TOKTRIM_NO_LEDGER", "")
	old := time.Now().Add(-48 * time.Hour)
	Append(Entry{Time: old, Command: "old", RawBytes: 10, OutBytes: 1})
	Append(Entry{Command: "new", RawBytes: 10, OutBytes: 1})

	got, err := Read(time.Now().Add(-1 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "new" {
		t.Errorf("since should exclude the old entry, got %+v", got)
	}
}

func TestTruncateCommand(t *testing.T) {
	long := strings.Repeat("a", 200)
	if got := TruncateCommand(long); len(got) > maxCommandLen {
		t.Errorf("command not truncated: %d chars", len(got))
	}
	if got := TruncateCommand("  npm   run   build  "); got != "npm run build" {
		t.Errorf("whitespace should be collapsed, got %q", got)
	}
}

func TestParseSince(t *testing.T) {
	for _, in := range []string{"7d", "2w", "12h", "30m", "90s"} {
		if _, err := ParseSince(in); err != nil {
			t.Errorf("ParseSince(%q): %v", in, err)
		}
	}
	if ts, err := ParseSince(""); err != nil || !ts.IsZero() {
		t.Errorf("an empty --since means all time, got %v %v", ts, err)
	}
	for _, in := range []string{"soon", "-3d", "7 days", "d"} {
		if _, err := ParseSince(in); err == nil {
			t.Errorf("ParseSince(%q) should fail", in)
		}
	}
	// 7d really is seven days back.
	ts, err := ParseSince("7d")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(ts); d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Errorf("7d resolved to %v ago", d)
	}
}

func TestCommandKeyGroupsSensibly(t *testing.T) {
	tests := []struct{ in, want string }{
		{"npm install lodash axios", "npm install"},
		{"npm run build --verbose", "npm run build"},
		{"npm run lint", "npm run lint"},
		{"npm test", "npm test"},
		{"docker build -t app .", "docker build"},
		{"dotnet test --no-build", "dotnet test"},
		{"make -j8", "make"},
		// The hook wraps everything in bash -c, so the real command has to be
		// visible through the wrapper or every row would say "bash".
		{"bash -c npm run build", "npm run build"},
		{"bash -c 'npm ci && npm run build'", "npm ci"},
		{"/bin/sh -c make -j8", "make"},
		{"", "(empty)"},
	}
	for _, tt := range tests {
		if got := commandKey(tt.in); got != tt.want {
			t.Errorf("commandKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReportOnEmptyLedger(t *testing.T) {
	t.Setenv("TOKTRIM_STATE_DIR", t.TempDir())
	var b strings.Builder
	if err := Report(&b, time.Time{}, 10); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "No toktrim runs recorded") {
		t.Errorf("unexpected report:\n%s", b.String())
	}
}

func TestReportSummarises(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TOKTRIM_STATE_DIR", dir)
	t.Setenv("TOKTRIM_NO_LEDGER", "")
	Append(Entry{Command: "bash -c npm install", Filters: []string{"node-install"},
		RawBytes: 186289, OutBytes: 867})
	Append(Entry{Command: "bash -c npm test", Filters: []string{"test-runner"},
		RawBytes: 52301, OutBytes: 4684, ExitCode: 1})
	Append(Entry{Command: "bash -c echo hi", RawBytes: 3, OutBytes: 3})

	var b strings.Builder
	if err := Report(&b, time.Time{}, 10); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"runs recorded   3",
		"npm install",
		"npm test",
		"estimated as bytes/4",
		"Ledger:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report is missing %q:\n%s", want, out)
		}
	}
	// The biggest saver is listed first.
	iInstall := strings.Index(out, "npm install")
	iTest := strings.Index(out, "npm test")
	if iInstall < 0 || iTest < 0 || iInstall > iTest {
		t.Errorf("rows should be ordered by bytes saved:\n%s", out)
	}
}

func TestPruneLogsRemovesOnlyStaleOnes(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "toktrim-npm-1.log")
	stale := filepath.Join(dir, "toktrim-npm-2.log")
	other := filepath.Join(dir, "something-else.log")
	for _, p := range []string{fresh, stale, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, old, old); err != nil {
		t.Fatal(err)
	}

	n, _ := PruneLogs(dir, 7*24*time.Hour)
	if n != 1 {
		t.Errorf("pruned %d logs, want 1", n)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a fresh log was removed")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the stale log was not removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("a file that is not a toktrim log was removed")
	}
}
