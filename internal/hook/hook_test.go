package hook

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// standalone is the default classifier: toktrim handles what it understands,
// regardless of what else is installed.
func standalone() *Classifier { return &Classifier{} }

// deferring opts in to leaving rtk's commands alone, with rtk's answer stubbed
// so the test depends neither on rtk being installed nor on its version. rtk
// 0.44.2 covers roughly seventy commands; an earlier release covered a
// handful, and a test that asked the real rtk would change meaning when rtk
// was upgraded.
func deferring(claims ...string) *Classifier {
	set := map[string]bool{}
	for _, c := range claims {
		set[c] = true
	}
	return &Classifier{
		DeferToRTK: true,
		askRTK:     func(cmd string) bool { return set[strings.TrimSpace(cmd)] },
	}
}

func TestClassifyStandalone(t *testing.T) {
	c := standalone()
	tests := []struct {
		command string
		wrap    bool
	}{
		// Everything toktrim understands, handled on its own. None of this
		// needs rtk: the test-runner filter covers all of these runners.
		{"jest --coverage", true},
		{"vitest run", true},
		{"npx jest", true},
		{"pytest -q tests/", true},
		{"go test ./...", true},
		{"cargo test", true},
		{"npm install", true},
		{"npm ci", true},
		{"npm test", true},
		{"npm run build", true},
		{"docker build -t app .", true},
		{"dotnet build", true},
		{"dotnet test", true},
		{"make -j8", true},
		{"git status", true},
		{"git commit -m wip", true},
		{"npm ci && npm run build", true},
		{"./scripts/deploy.sh", true},
		{"CI=1 npm run build", true},

		// Never wrap ourselves, or rtk.
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
		{"NODE_ENV=production npm run dev", false},

		// Output already going to a file.
		{"npm run build > out.log 2>&1", false},
		{"npm run build &> out.log", false},

		// Queries: the output is the answer.
		{"npm ls --all", false},
		{"npm outdated", false},
		{"npm why lodash", false},
		{"gh pr list", false},
		{"cat package.json", false},
		{"jq '.scripts' package.json", false},
		{"head -50 big.log", false},
		{"sed -n '10,20p' file.txt", false},
		{"git config --list", false},

		// Pipelines that select their own output.
		{"npm run build | tail -40", false},
		{"npm ls --all | wc -l", false},

		{"", false},
	}
	for _, tt := range tests {
		if got := c.Classify(tt.command); got.Wrap != tt.wrap {
			t.Errorf("Classify(%q).Wrap = %v, want %v (reason: %s)",
				tt.command, got.Wrap, tt.wrap, got.Reason)
		}
	}
}

// With deference opted in, a command rtk claims is left to rtk and everything
// else is still toktrim's.
func TestClassifyDeferringToRTK(t *testing.T) {
	c := deferring("git status", "jest --coverage", "cargo build", "npm run build")

	for _, cmd := range []string{"git status", "jest --coverage", "cargo build", "npm run build"} {
		if d := c.Classify(cmd); d.Wrap {
			t.Errorf("%q: should defer to rtk, got wrap (%s)", cmd, d.Reason)
		}
		// The same command standalone is toktrim's.
		if d := standalone().Classify(cmd); !d.Wrap {
			t.Errorf("%q: standalone toktrim should handle this (%s)", cmd, d.Reason)
		}
	}
	for _, cmd := range []string{"npm install", "npm test", "make -j8", "./deploy.sh"} {
		if d := c.Classify(cmd); !d.Wrap {
			t.Errorf("%q: rtk does not claim this, toktrim should handle it (%s)", cmd, d.Reason)
		}
	}
}

// A chain is only left to rtk when rtk claims every part of it. Otherwise rtk
// will not rewrite the chain and deferring would leave it unfiltered.
func TestDeferOnlyWhenRTKClaimsTheWholeChain(t *testing.T) {
	c := deferring("git status", "git log")
	if d := c.Classify("git status && git log"); d.Wrap {
		t.Errorf("a chain rtk claims entirely should be deferred, got wrap (%s)", d.Reason)
	}
	if d := c.Classify("git status && npm install"); !d.Wrap {
		t.Errorf("a mixed chain must stay with toktrim, got skip (%s)", d.Reason)
	}
}

// Deference must never be the reason nothing filters a command: if asking rtk
// fails or it claims nothing, toktrim handles it.
func TestBrokenRTKDoesNotDisableToktrim(t *testing.T) {
	c := &Classifier{DeferToRTK: true, askRTK: func(string) bool { return false }}
	if d := c.Classify("jest --coverage"); !d.Wrap {
		t.Errorf("toktrim should handle the command when rtk claims nothing (%s)", d.Reason)
	}
}

func TestDeferToRTKIsOffByDefault(t *testing.T) {
	t.Setenv("TOKTRIM_DEFER_TO_RTK", "")
	if deferToRTKFromEnv() {
		t.Error("deference must be opt-in")
	}
	for _, v := range []string{"1", "true", "yes"} {
		t.Setenv("TOKTRIM_DEFER_TO_RTK", v)
		if !deferToRTKFromEnv() {
			t.Errorf("TOKTRIM_DEFER_TO_RTK=%s should enable deference", v)
		}
	}
	t.Setenv("TOKTRIM_DEFER_TO_RTK", "0")
	if deferToRTKFromEnv() {
		t.Error("TOKTRIM_DEFER_TO_RTK=0 should not enable deference")
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

// hookOutput runs the hook over a payload with the default (standalone)
// classifier and returns its stdout.
func hookOutput(t *testing.T, payload string) string {
	t.Helper()
	return hookOutputWith(t, standalone(), payload)
}

func hookOutputWith(t *testing.T, c *Classifier, payload string) string {
	t.Helper()
	var out strings.Builder
	if code := RunWith(c, strings.NewReader(payload), &out); code != 0 {
		t.Errorf("RunWith returned %d; the hook must never block a command", code)
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

// Acceptance test 8, with deference opted in: `git status` is rtk's territory.
//
// Standalone -- the default since v0.3.1 -- toktrim handles it instead, which
// the companion test below pins. The original acceptance test assumed rtk was
// always there to take it.
func TestAcceptance_GitStatusPrintsNothing(t *testing.T) {
	c := deferring("git status")
	if got := hookOutputWith(t, c, `{"tool_input":{"command":"git status"}}`); got != "" {
		t.Errorf("with deference on, git status should print nothing, got:\n%s", got)
	}
}

func TestGitStatusIsWrappedByDefault(t *testing.T) {
	got := hookOutput(t, `{"tool_input":{"command":"git status"}}`)
	if got == "" {
		t.Fatal("standalone toktrim should wrap git status so that something filters it")
	}
	if want := `toktrim run -- bash -c 'git status'`; updatedCommand(t, got) != want {
		t.Errorf("wrapped command = %q, want %q", updatedCommand(t, got), want)
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

// rtkClaims must be inert when rtk is not installed, so a machine without rtk
// never pays for the lookup with anything worse than a false.
func TestRTKClaimsIsSafeWithoutRTK(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if rtkClaims("git status") {
		t.Error("rtkClaims should be false when rtk is not on PATH")
	}
}
