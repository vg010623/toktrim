package main

import (
	"encoding/json"
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

// runHook pipes a PreToolUse payload through the built binary's hook subcommand.
func runHook(t *testing.T, bin, payload string) string {
	t.Helper()
	cmd := exec.Command(bin, "hook")
	cmd.Stdin = strings.NewReader(payload)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("toktrim hook: %v", err)
	}
	return out.String()
}

// Acceptance tests 8, 9 and 10, through the real binary.
func TestAcceptance_HookThroughBinary(t *testing.T) {
	bin := buildToktrim(t)

	if got := runHook(t, bin, `{"tool_input":{"command":"git status"}}`); got != "" {
		t.Errorf("AT8: git status should print nothing, got %q", got)
	}

	got := runHook(t, bin, `{"tool_input":{"command":"npm run build"}}`)
	want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"toktrim run -- bash -c 'npm run build'"}}}` + "\n"
	if got != want {
		t.Errorf("AT9: wrapped output\n got %q\nwant %q", got, want)
	}

	if got := runHook(t, bin, `{"tool_input":{"command":"npm run dev"}}`); got != "" {
		t.Errorf("AT10: npm run dev should print nothing, got %q", got)
	}
}

// Acceptance test 11, end to end: the command the hook produces is executed and
// compared against the same command run directly.
func TestAcceptance_HookWrappedCommandRunsIdentically(t *testing.T) {
	bin := buildToktrim(t)

	// Put the freshly built binary first on PATH so the wrapper, which names
	// "toktrim", resolves to it rather than to whatever is installed on the
	// machine. On Windows the build is toktrim.exe, which PATH lookup finds.
	binDir := filepath.Dir(bin)

	script := `export NAME=world; echo "hi $NAME" && echo 'it'"'"'s quoted' | tr a-z A-Z`

	payload, err := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": script},
	})
	if err != nil {
		t.Fatal(err)
	}
	reply := runHook(t, bin, string(payload))
	if reply == "" {
		t.Fatal("expected the command to be wrapped")
	}
	var parsed struct {
		HookSpecificOutput struct {
			UpdatedInput struct {
				Command string `json:"command"`
			} `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(reply), &parsed); err != nil {
		t.Fatalf("bad reply: %v", err)
	}
	wrapped := parsed.HookSpecificOutput.UpdatedInput.Command

	direct, errDirect := exec.Command("bash", "-c", script).CombinedOutput()

	wrappedCmd := exec.Command("bash", "-c", wrapped)
	wrappedCmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	viaHook, errHook := wrappedCmd.CombinedOutput()

	if (errDirect == nil) != (errHook == nil) {
		t.Errorf("exit differed: direct=%v, wrapped=%v\nwrapped output: %s", errDirect, errHook, viaHook)
	}
	if string(direct) != string(viaHook) {
		t.Errorf("output differed\n direct: %q\nwrapped: %q\ncommand: %s", direct, viaHook, wrapped)
	}
}
