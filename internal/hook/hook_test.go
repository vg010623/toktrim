package hook

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		command string
		wrap    bool
	}{
		// rtk's territory: left alone.
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
		{"ls -la", false},
		{"grep -rn TODO .", false},
		{"find . -name '*.go'", false},
		// Never wrap ourselves.
		{"rtk git status", false},
		{"toktrim run -- ls", false},
		// Long-running or interactive.
		{"npm run dev", false},
		{"npm start", false},
		{"npm run watch", false},
		{"npm run test:watch", false},
		{"pnpm run serve", false},
		{"yarn build --watch", false},
		{"dotnet watch run", false},
		{"docker run -it ubuntu bash", false},
		{"docker run ubuntu", false}, // no --rm
		{"tail -f app.log", false},
		{"docker logs -f app", false},
		// Output already going to a file.
		{"npm run build > out.log 2>&1", false},
		{"npm run build &> out.log", false},
		{"npm ci >build.log 2>&1", false},
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
		{"go build ./...", true},    // rtk only covers `go test`
		{"git commit -m wip", true}, // rtk only covers status/log/diff/show
		// Chains: rtk does not handle these, so toktrim does.
		{"npm ci && npm run build", true},
		{"npm run build | tee build.log", true},
		{"cd app && dotnet test", true},
		// A chain of only rtk commands stays with rtk.
		{"git status && git log", false},
		// Environment assignments do not hide the command.
		{"CI=1 npm run build", true},
		{"NODE_ENV=production npm run dev", false},
		// Nothing to do.
		{"", false},
	}
	for _, tt := range tests {
		got := Classify(tt.command)
		if got.Wrap != tt.wrap {
			t.Errorf("Classify(%q).Wrap = %v, want %v (reason: %s)",
				tt.command, got.Wrap, tt.wrap, got.Reason)
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
