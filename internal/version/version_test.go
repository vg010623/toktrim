package version

import (
	"strings"
	"testing"
)

func TestGetAlwaysReportsAVersion(t *testing.T) {
	i := Get()
	if i.Version == "" {
		t.Error("Version must never be empty")
	}
	if i.GoVersion == "" || i.Platform == "" {
		t.Errorf("incomplete build info: %+v", i)
	}
}

func TestStringAndLongMentionTheVersion(t *testing.T) {
	i := Get()
	if !strings.Contains(i.String(), i.Version) {
		t.Errorf("String() = %q, should contain %q", i.String(), i.Version)
	}
	if !strings.Contains(i.Long(), i.Version) {
		t.Errorf("Long() = %q, should contain %q", i.Long(), i.Version)
	}
	if !strings.HasPrefix(i.String(), "toktrim ") {
		t.Errorf("String() = %q, should start with the program name", i.String())
	}
}

func TestCommitIsAbbreviated(t *testing.T) {
	i := Info{Version: "v1.0.0", Commit: "0123456789abcdef0123456789abcdef01234567", Platform: "linux/amd64", GoVersion: "go1.22"}
	if s := i.String(); !strings.Contains(s, "0123456789ab)") {
		t.Errorf("String() = %q, want a 12-character commit", s)
	}
}
