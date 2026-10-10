package main

import (
	"fmt"
	"os"
	"time"

	"github.com/vg010623/toktrim/internal/config"
	"github.com/vg010623/toktrim/internal/hook"
	"github.com/vg010623/toktrim/internal/ledger"
	"github.com/vg010623/toktrim/internal/runner"
	"github.com/vg010623/toktrim/internal/version"
)

const usage = `toktrim - trim verbose command output before it reaches an LLM

Usage:
  toktrim run -- <command> [args...]   run a command and filter its output
  toktrim pipe                         filter output arriving on stdin
  toktrim hook                         Claude Code PreToolUse hook (see README)
  toktrim stats [--since 7d]           what toktrim has saved
  toktrim version                      print the version and exit

"exec" is accepted as an alias for "run".

Environment:
  TOKTRIM_STATS=1        report what was trimmed, on stderr
  TOKTRIM_DISABLE=1      pass everything through unchanged
  TOKTRIM_RAW_LOG_DIR    where to write full-output logs
  TOKTRIM_NO_LEDGER=1    do not record runs
  TOKTRIM_STATE_DIR      where the ledger lives
  TOKTRIM_HOOK_DEBUG=1   explain why the hook stayed silent

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
	case "stats":
		os.Exit(runStats(os.Args[2:]))
	case "version", "--version", "-V":
		fmt.Print(version.Get().Long())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "toktrim: unknown subcommand %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

// runStats prints the savings report and prunes stale raw logs.
func runStats(args []string) int {
	sinceArg := ""
	topN := 10
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--since":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "toktrim: --since needs a value, e.g. 7d")
				return 2
			}
			sinceArg = args[i+1]
			i++
		case "--all":
			sinceArg = ""
		case "--top":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "toktrim: --top needs a number")
				return 2
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &topN); err != nil {
				fmt.Fprintf(os.Stderr, "toktrim: invalid --top %q\n", args[i+1])
				return 2
			}
			i++
		default:
			// Bare "7d" is accepted as shorthand for --since 7d.
			if sinceArg == "" && args[i] != "" && args[i][0] != '-' {
				sinceArg = args[i]
				continue
			}
			fmt.Fprintf(os.Stderr, "toktrim: unknown flag %q for stats\n", args[i])
			return 2
		}
	}

	since, err := ledger.ParseSince(sinceArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "toktrim: %v\n", err)
		return 2
	}
	if err := ledger.Report(os.Stdout, since, topN); err != nil {
		fmt.Fprintf(os.Stderr, "toktrim: %v\n", err)
		return 1
	}

	// Raw logs are the escape hatch for output that was trimmed, but they are
	// never cleaned up by the runs that create them. Stats is the natural place
	// to do it, since it is the command a user runs when thinking about them.
	cfg := loadConfig()
	age := time.Duration(cfg.RawLogMaxAgeDays) * 24 * time.Hour
	if age > 0 {
		if n, freed := ledger.PruneLogs(cfg.RawLogDir, age); n > 0 {
			fmt.Printf("Pruned %d raw %s older than %d days (%s).\n",
				n, map[bool]string{true: "log", false: "logs"}[n == 1],
				cfg.RawLogMaxAgeDays, humanBytes(freed))
		}
	}
	return 0
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
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
