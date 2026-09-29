package main

import (
	"fmt"
	"os"

	"github.com/vg010623/toktrim/internal/config"
	"github.com/vg010623/toktrim/internal/hook"
	"github.com/vg010623/toktrim/internal/runner"
	"github.com/vg010623/toktrim/internal/version"
)

const usage = `toktrim - trim verbose command output before it reaches an LLM

Usage:
  toktrim run -- <command> [args...]   run a command and filter its output
  toktrim pipe                         filter output arriving on stdin
  toktrim hook                         Claude Code PreToolUse hook (see README)
  toktrim version                      print the version and exit

"exec" is accepted as an alias for "run".

Environment:
  TOKTRIM_STATS=1        report what was trimmed, on stderr
  TOKTRIM_DISABLE=1      pass everything through unchanged
  TOKTRIM_RAW_LOG_DIR    where to write full-output logs

Configuration: toktrim.toml in the project root or ~/.config/toktrim/.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "run", "exec":
		args := os.Args[2:]
		// The "--" separator is optional but conventional.
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "toktrim: %s needs a command\n\n%s", os.Args[1], usage)
			os.Exit(2)
		}
		os.Exit(runner.Run(args, loadConfig()))
	case "pipe":
		os.Exit(runner.ProcessStdin(loadConfig()))
	case "hook":
		os.Exit(hook.Run(os.Stdin, os.Stdout))
	case "version", "--version", "-V":
		fmt.Print(version.Get().Long())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "toktrim: unknown subcommand %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

// loadConfig reads toktrim.toml if there is one. A broken config is reported but
// never fatal: toktrim must not be the reason a command fails to run.
func loadConfig() *config.Config {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "toktrim: ignoring config: %v\n", err)
	}
	return cfg
}
