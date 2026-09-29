package main

import (
	"fmt"
	"os"

	"github.com/vg010623/toktrim/internal/config"
	"github.com/vg010623/toktrim/internal/runner"
)

const usage = `toktrim - trim verbose command output before it reaches an LLM

Usage:
  toktrim run -- <command> [args...]   run a command and filter its output
  toktrim pipe                         filter output arriving on stdin

"exec" is accepted as an alias for "run".

Environment:
  TOKTRIM_STATS=1    report what was trimmed, on stderr
  TOKTRIM_DISABLE=1  pass everything through unchanged
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	cfg := config.Default()

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
		os.Exit(runner.Run(args, cfg))
	case "pipe":
		os.Exit(runner.ProcessStdin(cfg))
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "toktrim: unknown subcommand %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
