package runner

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/vg010623/toktrim/internal/filter"
	"github.com/vg010623/toktrim/internal/tokenizer"
)

// ExitCommandNotFound is the conventional shell exit code for a command that
// could not be started at all.
const ExitCommandNotFound = 127

// ExecuteCommand runs the given command with stdout and stderr merged into a
// single ordered stream, prints the filtered output, and then exits with the
// child's exit code.
//
// The output is always printed before the process exits: a failing command must
// still show why it failed.
func ExecuteCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "toktrim: no command provided")
		os.Exit(ExitCommandNotFound)
	}

	// A single OS pipe shared by stdout and stderr keeps the two interleaved in
	// the order the child actually wrote them. os/exec passes the same file
	// descriptor to both when Stdout and Stderr are the same *os.File.
	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "toktrim: cannot create pipe: %v\n", err)
		os.Exit(ExitCommandNotFound)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		fmt.Fprintf(os.Stderr, "toktrim: cannot start %q: %v\n", args[0], err)
		os.Exit(ExitCommandNotFound)
	}

	// The parent's write end must be closed or the read below never sees EOF.
	pw.Close()

	raw, readErr := io.ReadAll(pr)
	pr.Close()
	if readErr != nil {
		fmt.Fprintf(os.Stderr, "toktrim: error reading command output: %v\n", readErr)
	}

	waitErr := cmd.Wait()

	filtered := applyFilters(string(raw))
	io.WriteString(os.Stdout, filtered)
	printTokenStats(string(raw), filtered)

	os.Exit(exitCodeOf(waitErr))
}

// exitCodeOf maps the error returned by Cmd.Wait to a process exit code.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if ok := asExitError(err, &exitErr); ok {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
		// Killed by a signal: report it the way a shell does.
		return 128
	}
	return 1
}

func asExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}

// ProcessStdin reads stdin, applies filters and writes the result to stdout.
func ProcessStdin() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "toktrim: error reading stdin: %v\n", err)
		os.Exit(1)
	}
	filtered := applyFilters(string(raw))
	io.WriteString(os.Stdout, filtered)
	printTokenStats(string(raw), filtered)
}

// applyFilters runs the filter pipeline and returns the filtered string.
func applyFilters(input string) string {
	filters := []filter.Filter{
		&filter.TestRunnerFilter{},
		&filter.DedupFilter{},
		&filter.DiffFilter{},
	}
	current := input
	for _, f := range filters {
		current = f.Apply(current)
	}
	return current
}

// printTokenStats reports the estimated saving, but only when the user opts in.
//
// Claude Code forwards the Bash tool's stderr to the model, so unconditional
// telemetry on stderr would itself cost tokens on every command.
func printTokenStats(raw, filtered string) {
	if os.Getenv("TOKTRIM_STATS") != "1" {
		return
	}
	rawTokens := tokenizer.Estimate(raw)
	filteredTokens := tokenizer.Estimate(filtered)
	if rawTokens == 0 {
		return
	}
	pct := float64(rawTokens-filteredTokens) / float64(rawTokens) * 100
	fmt.Fprintf(os.Stderr, "[toktrim] ~%d -> ~%d est. tokens (-%.1f%%)\n",
		rawTokens, filteredTokens, pct)
}
