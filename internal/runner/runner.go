package runner

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/yourusername/toktrim/internal/filter"
	"github.com/yourusername/toktrim/internal/tokenizer"
)

// ExecuteCommand runs the given command, filters its combined output,
// prints filtered output to stdout and token analytics to stderr.
func ExecuteCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: no command provided")
		os.Exit(1)
	}
	cmd := exec.Command(args[0], args[1:]...)
	// Combine stdout and stderr? The spec says intercepts stdout and stderr.
	// We'll combine them for simplicity; but we could keep separate.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating stdout pipe: %v\n", err)
		os.Exit(1)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating stderr pipe: %v\n", err)
		os.Exit(1)
	}
	// We'll read both pipes concurrently and combine.
	var output strings.Builder
	// Use a channel to signal when both pipes are done.
	done := make(chan struct{})
	go func() {
		io.Copy(&output, stdoutPipe)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(&output, stderrPipe)
		done <- struct{}{}
	}()
	// Wait for both goroutines (two signals)
	<-done
	<-done
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "error starting command: %v\n", err)
		os.Exit(1)
	}
	if err := cmd.Wait(); err != nil {
		// We still want to show output even if command failed.
		// We'll note the exit error in stderr later.
		fmt.Fprintf(os.Stderr, "warning: command exited with error: %v\n", err)
	}
	raw := output.String()
	filtered := applyFilters(raw)
	// Print filtered output to stdout (must be clean data)
	fmt.Print(filtered)
	// Print token analytics to stderr
	printTokenStats(raw, filtered)
}

// ProcessStdin reads from stdin, applies filters, writes to stdout,
// and prints token analytics to stderr.
func ProcessStdin() {
	var input strings.Builder
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input.WriteString(scanner.Text())
		input.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error reading stdin: %v\n", err)
		os.Exit(1)
	}
	raw := input.String()
	filtered := applyFilters(raw)
	fmt.Print(filtered)
	printTokenStats(raw, filtered)
}

// applyFilters runs the filter pipeline and returns filtered string.
func applyFilters(input string) string {
	filters := []filter.Filter{
		&filter.TestRunnerFilter{},
		&filter.DedupFilter{},
		&filter.DiffFilter{},
	}
	var current string = input
	for _, f := range filters {
		current = f.Apply(current)
	}
	return current
}

// printTokenStats computes token counts before and after filtering and prints to stderr.
func printTokenStats(raw, filtered string) {
	tok, err := tokenizer.NewTokenizer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing tokenizer: %v\n", err)
		return
	}
	rawTokens := tok.Count(raw)
	filteredTokens := tok.Count(filtered)
	saved := rawTokens - filteredTokens
	var savedDollar float64
	// Rough estimate: $0.003 per 1k tokens for GPT-4o input (output similar)
	// We'll just compute savings as (saved / 1000) * 0.003 (approx)
	savedDollar = float64(saved) / 1000.0 * 0.003
	fmt.Fprintf(os.Stderr, "[toktrim] Tokens: %d -> %d (%.1f%%) | Saved: ~$%.4f\n",
		rawTokens, filteredTokens, float64(saved)/float64(rawTokens)*100, savedDollar)
}