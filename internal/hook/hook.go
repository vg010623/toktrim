package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// input is the part of Claude Code's PreToolUse payload that toktrim needs.
type input struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	Cwd       string          `json:"cwd"`
}

// output is the hook's reply.
//
// permissionDecision is deliberately absent. Claude Code documents exit 0 with
// no decision as "the tool call continues through the normal permission flow",
// and toktrim wraps arbitrary commands, so it must never be the reason a
// command skips the user's permission prompt. Returning updatedInput alone
// rewrites the command and leaves the prompt exactly where it was.
type output struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName string          `json:"hookEventName"`
	UpdatedInput  json.RawMessage `json:"updatedInput"`
}

// Run reads a PreToolUse payload from r and writes the hook's reply to w.
//
// It always returns 0. A hook that fails must not stop a command from running,
// so every error path here is silent and lets the original command through
// unchanged. Set TOKTRIM_HOOK_DEBUG=1 to see why nothing happened.
func Run(r io.Reader, w io.Writer) int {
	raw, err := io.ReadAll(r)
	if err != nil {
		debugf("cannot read stdin: %v", err)
		return 0
	}

	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		debugf("cannot parse hook input: %v", err)
		return 0
	}
	if in.ToolName != "" && in.ToolName != "Bash" {
		debugf("not a Bash call (%s)", in.ToolName)
		return 0
	}

	// Keep the rest of the tool input intact: updatedInput replaces the whole
	// object, so dropping description or timeout would change the tool call.
	var toolInput map[string]any
	if len(in.ToolInput) > 0 {
		if err := json.Unmarshal(in.ToolInput, &toolInput); err != nil {
			debugf("cannot parse tool_input: %v", err)
			return 0
		}
	}
	command, _ := toolInput["command"].(string)
	if strings.TrimSpace(command) == "" {
		debugf("no command in tool_input")
		return 0
	}

	decision := Classify(command)
	if !decision.Wrap {
		debugf("leaving alone: %s", decision.Reason)
		return 0
	}

	toolInput["command"] = Wrap(command)
	updated, err := json.Marshal(toolInput)
	if err != nil {
		debugf("cannot marshal updatedInput: %v", err)
		return 0
	}

	reply, err := json.Marshal(output{hookSpecificOutput{
		HookEventName: "PreToolUse",
		UpdatedInput:  updated,
	}})
	if err != nil {
		debugf("cannot marshal reply: %v", err)
		return 0
	}
	fmt.Fprintln(w, string(reply))
	return 0
}

// Wrap turns a command into the toktrim invocation that runs it.
//
// The command is handed to bash rather than split up, so pipes, &&, redirects
// and quoting keep working exactly as they did, and only the final output is
// filtered.
func Wrap(command string) string {
	return "toktrim run -- bash -c " + shellQuote(command)
}

// shellQuote wraps s in single quotes for POSIX sh.
//
// Inside single quotes every character is literal except the quote itself, so
// the only escape needed is to close the quoted run, emit a literal quote, and
// reopen: ' becomes '\”.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// debugf reports why the hook stayed silent, when asked to.
func debugf(format string, args ...any) {
	if os.Getenv("TOKTRIM_HOOK_DEBUG") != "1" {
		return
	}
	fmt.Fprintf(os.Stderr, "[toktrim hook] "+format+"\n", args...)
}
