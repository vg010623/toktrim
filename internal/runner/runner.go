package runner

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

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

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "error starting command: %v\n", err)
		os.Exit(1)
	}

	var stdoutBuf, stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(&stdoutBuf, stdoutPipe)
	}()
	go func() {
		defer wg.Done()
		io.Copy(&stderrBuf, stderrPipe)
	}()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				os.Exit(status.ExitStatus())
			}
		}
		fmt.Fprintf(os.Stderr, "warning: command exited with error: %v\n", err)
		os.Exit(1)
	}

	raw := stdoutBuf.String() + stderrBuf.String()
	filtered := applyFilters(raw)
	fmt.Print(filtered)
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