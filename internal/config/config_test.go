package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTOMLSubset(t *testing.T) {
	in := `
# a comment
passthrough_lines = 80
raw_log_dir = "/var/log/toktrim"   # trailing comment
enabled = true

[filters]
dedup = false
docker-build = true

[truncate]
head_lines = 10
tail_lines = 20
`
	got, err := parseTOML(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parseTOML: %v", err)
	}
	want := map[string]string{
		"passthrough_lines":    "80",
		"raw_log_dir":          "/var/log/toktrim",
		"enabled":              "true",
		"filters.dedup":        "false",
		"filters.docker-build": "true",
		"truncate.head_lines":  "10",
		"truncate.tail_lines":  "20",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys, want %d: %v", len(got), len(want), got)
	}
}

func TestParseTOMLRejectsNonsense(t *testing.T) {
	for _, in := range []string{
		"passthrough_lines",           // no '='
		"[unterminated",               // bad table
		"[]",                          // empty table
		"= 4",                         // no key
		"head_lines = [1, 2]",         // unsupported type
		`raw_log_dir = "unterminated`, // bad string
	} {
		if _, err := parseTOML(strings.NewReader(in)); err == nil {
			t.Errorf("parseTOML(%q) succeeded, want an error", in)
		}
	}
}

func TestApplyRejectsUnknownSettings(t *testing.T) {
	cfg := Default()
	if err := cfg.apply(map[string]string{"colour_scheme": "dark"}); err == nil {
		t.Error("an unknown setting should be reported, not silently ignored")
	}
}

func TestLoadPrefersProjectOverUser(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	proj := filepath.Join(root, "work", "app")
	if err := os.MkdirAll(filepath.Join(home, ".config", "toktrim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".config", "toktrim", FileName), "head_lines = 5\ntail_lines = 7\n")
	write(filepath.Join(root, "work", FileName), "tail_lines = 11\n")

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg, err := Load(proj)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// From the user file, not overridden anywhere.
	if cfg.HeadLines != 5 {
		t.Errorf("HeadLines = %d, want 5 (from the user config)", cfg.HeadLines)
	}
	// The nearer project file wins.
	if cfg.TailLines != 11 {
		t.Errorf("TailLines = %d, want 11 (the project config should win)", cfg.TailLines)
	}
}

func TestLoadIsNotFatalOnBadConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("this is not toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg, err := Load(dir)
	if err == nil {
		t.Error("a malformed config should be reported")
	}
	if cfg == nil {
		t.Fatal("Load must still return a usable config")
	}
	if cfg.HeadLines != DefaultHeadLines {
		t.Errorf("HeadLines = %d, want the default %d", cfg.HeadLines, DefaultHeadLines)
	}
}

func TestFilterEnabled(t *testing.T) {
	cfg := Default()
	if !cfg.FilterEnabled("dedup") {
		t.Error("filters should be on by default")
	}
	cfg.Filters = map[string]bool{"dedup": false}
	if cfg.FilterEnabled("dedup") {
		t.Error("dedup was switched off in config")
	}
	if !cfg.FilterEnabled("ansi") {
		t.Error("switching one filter off must not affect the others")
	}
	cfg.Enabled = false
	if cfg.FilterEnabled("ansi") {
		t.Error("nothing should run when toktrim is disabled")
	}
}

func TestDisableViaEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("TOKTRIM_DISABLE", "1")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Enabled {
		t.Error("TOKTRIM_DISABLE=1 should disable filtering")
	}
}
