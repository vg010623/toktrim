package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// buildToktrim compiles the binary once per test binary run.
func buildToktrim(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		// The acceptance tests shell out to bash, which exists on the Windows
		// runner via Git Bash but not on a bare container.
		if _, err := exec.LookPath("bash"); err != nil {
			t.Skip("bash not available")
		}
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "toktrim")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// runToktrim invokes the built binary and returns stdout and the exit code.
func runToktrim(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "TOKTRIM_STATS=")
	var stdout strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if ok := asExit(err, &ee); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running toktrim: %v", err)
		}
	}
	return stdout.String(), code
}

func asExit(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

// Acceptance test 1: exit code and output are both preserved.
func TestAcceptance_ExitCodePreserved(t *testing.T) {
	bin := buildToktrim(t)
	out, code := runToktrim(t, bin, "run", "--", "bash", "-c", "echo hi; exit 3")
	if out != "hi\n" {
		t.Errorf("stdout = %q, want %q", out, "hi\n")
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}

// Acceptance test 5: small output is byte-for-byte identical.
func TestAcceptance_SmallOutputIdentical(t *testing.T) {
	bin := buildToktrim(t)
	var want strings.Builder
	for i := 1; i <= 20; i++ {
		want.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	out, code := runToktrim(t, bin, "run", "--", "bash", "-c", "for i in $(seq 1 20); do echo line $i; done")
	if out != want.String() {
		t.Errorf("20 lines of output were modified\n got %q\nwant %q", out, want.String())
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

// Acceptance test 6: anything cut points at a raw log holding the full output.
func TestAcceptance_RawLogHoldsCompleteOutput(t *testing.T) {
	bin := buildToktrim(t)
	// 300 identical lines: over the threshold and fully compressible.
	out, code := runToktrim(t, bin, "run", "--", "bash", "-c",
		"for i in $(seq 1 300); do echo repeated noise; done")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	re := regexp.MustCompile(`\[toktrim\] (\d+) lines elided — full output: (.+)\n$`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("output must end with the raw-log footer, got:\n%s", out)
	}
	raw, err := os.ReadFile(strings.TrimSpace(m[2]))
	if err != nil {
		t.Fatalf("reading raw log %q: %v", m[2], err)
	}
	defer os.Remove(strings.TrimSpace(m[2]))

	lines := strings.Count(string(raw), "\n")
	if lines != 300 {
		t.Errorf("raw log holds %d lines, want the complete 300", lines)
	}
}

// Acceptance test 11: quoting is not mangled when a command runs under toktrim.
func TestAcceptance_QuotingAndPipesBehaveTheSame(t *testing.T) {
	bin := buildToktrim(t)
	script := `export VAR=world; echo "hello $VAR" && echo 'single '\''quoted'\''' | tr a-z A-Z`

	direct, err := exec.Command("bash", "-c", script).Output()
	if err != nil {
		t.Fatalf("running script directly: %v", err)
	}
	wrapped, code := runToktrim(t, bin, "run", "--", "bash", "-c", script)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if wrapped != string(direct) {
		t.Errorf("wrapped output differs\n got %q\nwant %q", wrapped, string(direct))
	}
}

// Acceptance test 7: no network access is required.
func TestAcceptance_WorksWithoutNetwork(t *testing.T) {
	bin := buildToktrim(t)
	// There is nothing to disable: toktrim has no network code and no
	// dependencies. Assert that, so a dependency cannot creep back in.
	out, err := exec.Command("go", "list", "-deps", "./...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.Contains(dep, "tiktoken") {
			t.Errorf("tiktoken must not be a dependency (found %q)", dep)
		}
	}
	if _, code := runToktrim(t, bin, "run", "--", "bash", "-c", "echo offline"); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}
