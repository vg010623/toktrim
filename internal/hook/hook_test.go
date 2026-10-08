package hook

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClassifyWithRTKInstalled(t *testing.T) {
	c := &Classifier{RTKAvailable: true}
	tests := []struct {
		command string
		wrap    bool
	}{
		// rtk's territory, left alone because rtk is there to handle it.
		{"git status", false},
		{"git log --oneline -20", false},
		{"git diff HEAD~1", false},
		{"git show abc123", false},
		{"cargo build --release", false},
		{"cargo test", false},
		{"go test ./...", false},
		{"pytest -q tests/", false},
		{"jest --coverage", false},
		{"vitest run", false},
		{"tsc --noEmit", false},
		{"eslint src/", false},
		// Spelled through a runner, the same command must get the same answer.
		{"npx jest --coverage", false},
		{"npx tsc --noEmit", false},
		{"npx eslint src/", false},
		{"bunx vitest run", false},
		{"pnpm exec jest", false},
		{"yarn jest", false},
		{"yarn tsc", false},
		// Never wrap ourselves.
		{"rtk git status", false},
		{"toktrim run -- ls", false},
		// Long-running or interactive.
		{"npm run dev", false},
		{"npm start", false},
		{"npm run test:watch", false},
		{"pnpm run serve", false},
		{"yarn build --watch", false},
		{"dotnet watch run", false},
		{"docker run -it ubuntu bash", false},
		{"docker run ubuntu", false},
		{"tail -f app.log", false},
		{"docker logs -f app", false},
		// Output already going to a file.
		{"npm run build > out.log 2>&1", false},
		{"npm run build &> out.log", false},
		// Queries: the output is the answer.
		{"npm ls --all", false},
		{"npm outdated", false},
		{"npm view react versions", false},
		{"npm why lodash", false},
		{"pnpm ls --depth 3", false},
		{"yarn list --pattern react", false},
		{"gh pr list", false},
		{"gh run view 123 --log", false},
		{"cat package.json", false},
		{"jq '.scripts' package.json", false},
		{"head -50 big.log", false},
		{"tail -100 big.log", false},
		{"sed -n '10,20p' file.txt", false},
		{"awk '{print $1}' file.txt", false},
		{"git config --list", false},
		{"git remote -v", false},
		// Pipelines that select their own output.
		{"npm run build | tail -40", false},
		{"dotnet build | grep error", false},
		{"npm ls --all | wc -l", false},
		// Ours.
		{"npm install", true},
		{"npm ci", true},
		{"npm i --save-dev jest", true},
		{"pnpm add lodash", true},
		{"yarn", true},
		{"npm run build", true},
		{"npm test", true},
		{"docker build -t app .", true},
		{"docker run --rm app", true},
		{"dotnet build", true},
		{"dotnet test", true},
		{"make -j8", true},
		{"go build ./...", true},
		{"git commit -m wip", true},
		{"npm ci && npm run build", true},
		{"cd app && dotnet test", true},
		{"yarn build", true}, // a script, not a tool: stays wrapped
		{"git status && git log", false},
		{"CI=1 npm run build", true},
		{"NODE_ENV=production npm run dev", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := c.Classify(tt.command); got.Wrap != tt.wrap {
			t.Errorf("Classify(%q).Wrap = %v, want %v (reason: %s)",
				tt.command, got.Wrap, tt.wrap, got.Reason)
		}
	}
}

// Without rtk on PATH nothing else compresses its commands, so toktrim takes
// them on. The test filter already understands jest, pytest, go test and cargo.
func TestClassifyWithoutRTK(t *testing.T) {
	absent := &Classifier{RTKAvailable: false}
	present := &Classifier{RTKAvailable: true}

	nowOurs := []string{
		"jest --coverage", "vitest run", "pytest -q", "cargo test",
		"go test ./...", "tsc --noEmit", "eslint src/", "cargo build",
		"git status", "git log --oneline", "npx jest",
	}
	for _, cmd := range nowOurs {
		if d := present.Classify(cmd); d.Wrap {
			t.Errorf("with rtk, %q should be skipped (got %s)", cmd, d.Reason)
		}
		if d := absent.Classify(cmd); !d.Wrap {
			t.Errorf("without rtk, %q should be handled by toktrim (got %s)", cmd, d.Reason)
		}
	}

	// Everything skipped for a reason other than rtk stays skipped either way.
	stillSkipped := []string{
		"npm run dev", "tail -f app.log", "docker run -it ubuntu bash",
		"npm ls --all", "cat package.json", "rtk git status",
		"toktrim run -- ls", "npm run build > out.log 2>&1",
		"npm run build | tail -40",
	}
	for _, cmd := range stillSkipped {
		if d := absent.Classify(cmd); d.Wrap {
			t.Errorf("without rtk, %q should still be skipped (got %s)", cmd, d.Reason)
		}
	}
}

// Classify through a fake PATH, to check the lookup itself rather than the
// injected flag.
func TestRTKDetectionUsesPATH(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if rtkOnPath() {
		t.Error("rtk should not be found on an empty PATH")
	}

	name := filepath.Join(dir, "rtk")
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(name, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !rtkOnPath() {
		t.Error("rtk should be found once it is on PATH")
	}
}

func TestNormalizeRunner(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"npx", "jest", "--coverage"}, []string{"jest", "--coverage"}},
		{[]string{"bunx", "vitest"}, []string{"vitest"}},
		{[]string{"pnpm", "exec", "eslint", "src/"}, []string{"eslint", "src/"}},
		{[]string{"yarn", "dlx", "prettier"}, []string{"prettier"}},
		{[]string{"yarn", "jest"}, []string{"jest"}},
		// A package script is not a bare binary and must not be unwrapped.
		{[]string{"yarn", "build"}, []string{"yarn", "build"}},
		{[]string{"npm", "run", "build"}, []string{"npm", "run", "build"}},
		{[]string{"pnpm", "run", "lint"}, []string{"pnpm", "run", "lint"}},
		{[]string{"make"}, []string{"make"}},
	}
	for _, tt := range tests {
		got := normalizeRunner(tt.in)
		if strings.Join(got, " ") != strings.Join(tt.want, " ") {
			t.Errorf("normalizeRunner(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// hookOutput runs the hook over a payload and returns its stdout.
func hookOutput(t *testing.T, payload string) string {
	t.Helper()
	var out strings.Builder
	if code := Run(strings.NewReader(payload), &out); code != 0 {
		t.Errorf("Run returned %d; the hook must never block a command", code)
	}
	return out.String()
}

// updatedCommand extracts the rewritten command from the hook's reply.
func updatedCommand(t *testing.T, reply string) string {
	t.Helper()
	var parsed struct {
		HookSpecificOutput struct {
			HookEventName      string         `json:"hookEventName"`
			PermissionDecision *string        `json:"permissionDecision"`
			UpdatedInput       map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(reply), &parsed); err != nil {
		t.Fatalf("reply is not valid JSON: %v\n%s", err, reply)
	}
	if parsed.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", parsed.HookSpecificOutput.HookEventName)
	}
	// The central safety property: toktrim wraps arbitrary commands and must
	// never approve one.
	if parsed.HookSpecificOutput.PermissionDecision != nil {
		t.Errorf("hook returned permissionDecision %q; it must never make a permission decision",
			*parsed.HookSpecificOutput.PermissionDecision)
	}
	cmd, ok := parsed.HookSpecificOutput.UpdatedInput["command"].(string)
	if !ok {
		t.Fatalf("updatedInput has no command: %s", reply)
	}
	return cmd
}

// Acceptance test 8.
func TestAcceptance_GitStatusPrintsNothing(t *testing.T) {
	if got := hookOutput(t, `{"tool_input":{"command":"git status"}}`); got != "" {
		t.Errorf("git status should print nothing (rtk's territory), got:\n%s", got)
	}
}

// Acceptance test 9.
func TestAcceptance_NpmRunBuildIsWrapped(t *testing.T) {
	reply := hookOutput(t, `{"tool_input":{"command":"npm run build"}}`)
	if reply == "" {
		t.Fatal("npm run build should be wrapped")
	}
	if got, want := updatedCommand(t, reply), `toktrim run -- bash -c 'npm run build'`; got != want {
		t.Errorf("wrapped command = %q, want %q", got, want)
	}
}

// Acceptance test 10.
func TestAcceptance_NpmRunDevPrintsNothing(t *testing.T) {
	if got := hookOutput(t, `{"tool_input":{"command":"npm run dev"}}`); got != "" {
		t.Errorf("npm run dev should print nothing, got:\n%s", got)
	}
}

func TestHookNeverBlocksOnBadInput(t *testing.T) {
	for _, payload := range []string{
		"",
		"not json",
		"{}",
		`{"tool_name":"Read","tool_input":{"file_path":"x"}}`,
		`{"tool_input":{}}`,
		`{"tool_input":{"command":""}}`,
		`{"tool_input":{"command":123}}`,
		`{"tool_input":null}`,
	} {
		if got := hookOutput(t, payload); got != "" {
			t.Errorf("payload %q should be ignored, got:\n%s", payload, got)
		}
	}
}

func TestHookPreservesOtherToolInputFields(t *testing.T) {
	reply := hookOutput(t, `{"tool_name":"Bash","tool_input":{
		"command":"npm run build",
		"description":"Build the app",
		"timeout":300000,
		"run_in_background":false}}`)

	var parsed struct {
		HookSpecificOutput struct {
			UpdatedInput map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(reply), &parsed); err != nil {
		t.Fatalf("bad reply: %v", err)
	}
	in := parsed.HookSpecificOutput.UpdatedInput
	if in["description"] != "Build the app" {
		t.Errorf("description was lost: %v", in["description"])
	}
	if in["timeout"] != float64(300000) {
		t.Errorf("timeout was lost: %v", in["timeout"])
	}
	if _, ok := in["run_in_background"]; !ok {
		t.Error("run_in_background was lost")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"npm run build", `'npm run build'`},
		{"echo hi", `'echo hi'`},
		{`echo "double"`, `'echo "double"'`},
		{"echo 'single'", `'echo '\''single'\'''`},
		{"echo $VAR && ls | wc -l", `'echo $VAR && ls | wc -l'`},
		{`it's`, `'it'\''s'`},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Acceptance test 11: the wrapped command must behave exactly like the original.
func TestAcceptance_WrappedCommandBehavesIdentically(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	scripts := []string{
		`echo hi`,
		`export NAME=world; echo "hi $NAME"`,
		`echo 'single quoted'`,
		`echo "it's got an apostrophe"`,
		`echo a && echo b | tr a-z A-Z`,
		`printf '%s|%s\n' "a b" 'c d'`,
		`echo $((2 + 2))`,
		`echo "tab	and \$literal"`,
		`false || echo recovered`,
	}
	for _, script := range scripts {
		// The wrapper passes the script to bash -c; running that inner part is
		// what proves the quoting survived. Running the full toktrim wrapper is
		// covered by the end-to-end tests in cmd/toktrim.
		quoted := shellQuote(script)
		direct, errDirect := exec.Command("bash", "-c", script).CombinedOutput()
		viaQuote, errQuote := exec.Command("bash", "-c", "bash -c "+quoted).CombinedOutput()
		if (errDirect == nil) != (errQuote == nil) {
			t.Errorf("%q: exit differed (direct=%v, wrapped=%v)", script, errDirect, errQuote)
		}
		if string(direct) != string(viaQuote) {
			t.Errorf("%q: output differed\n direct: %q\nwrapped: %q", script, direct, viaQuote)
		}
	}
}
