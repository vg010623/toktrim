// Package runner executes a command and streams its output through the filter
// pipeline.
package runner

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"

	"github.com/vg010623/toktrim/internal/config"
	"github.com/vg010623/toktrim/internal/filter"
	"github.com/vg010623/toktrim/internal/pipeline"
	"github.com/vg010623/toktrim/internal/tokenizer"
)

// ExitCommandNotFound is the conventional shell exit code for a command that
// could not be started at all.
const ExitCommandNotFound = 127

// Run executes args, filters the output as it arrives, and returns the exit
// code the caller should exit with.
//
// Output is always written before Run returns: a failing command must still
// show why it failed.
func Run(args []string, cfg *config.Config) int {
	if cfg == nil {
		cfg = config.Default()
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "toktrim: no command provided")
		return ExitCommandNotFound
	}

	// One OS pipe shared by stdout and stderr keeps the two interleaved in the
	// order the child wrote them. os/exec hands the same descriptor to both
	// when Stdout and Stderr are the same *os.File.
	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "toktrim: cannot create pipe: %v\n", err)
		return ExitCommandNotFound
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		fmt.Fprintf(os.Stderr, "toktrim: cannot start %q: %v\n", args[0], err)
		return ExitCommandNotFound
	}
	// The parent's write end must be closed or the read below never sees EOF.
	pw.Close()

	stopSignals := forwardSignals(cmd)
	defer stopSignals()

	stdout := bufio.NewWriter(os.Stdout)
	proc := pipeline.New(stdout, pipeline.Options{
		Filters:          filter.Chain(args, cfg),
		PassthroughLines: cfg.PassthroughLines,
		PassthroughBytes: cfg.PassthroughBytes,
		RawLogDir:        cfg.RawLogDir,
		Label:            args[0],
	})

	// Copy through the processor as output arrives. If the command is killed --
	// by Claude Code's Bash timeout, or a Ctrl-C -- everything already read has
	// been processed and is flushed by Close below.
	_, copyErr := io.Copy(proc, pr)
	pr.Close()

	waitErr := cmd.Wait()

	closeErr := proc.Close()
	flushErr := stdout.Flush()

	for _, e := range []error{copyErr, closeErr, flushErr} {
		if e != nil && !errors.Is(e, os.ErrClosed) {
			fmt.Fprintf(os.Stderr, "toktrim: %v\n", e)
		}
	}

	reportStats(proc)
	return exitCodeOf(waitErr)
}

// exitCodeOf maps the error from Cmd.Wait onto a process exit code.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
		// Killed by a signal; report it the way a shell does.
		return 128
	}
	return 1
}

// ProcessStdin filters output arriving on stdin.
func ProcessStdin(cfg *config.Config) int {
	if cfg == nil {
		cfg = config.Default()
	}
	stdout := bufio.NewWriter(os.Stdout)
	proc := pipeline.New(stdout, pipeline.Options{
		Filters:          filter.Chain(nil, cfg),
		PassthroughLines: cfg.PassthroughLines,
		PassthroughBytes: cfg.PassthroughBytes,
		RawLogDir:        cfg.RawLogDir,
		Label:            "pipe",
	})
	if _, err := io.Copy(proc, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "toktrim: error reading stdin: %v\n", err)
		proc.Close()
		stdout.Flush()
		return 1
	}
	proc.Close()
	stdout.Flush()
	reportStats(proc)
	return 0
}

// reportStats prints the estimated saving, but only when the user opts in.
//
// Claude Code forwards the Bash tool's stderr to the model, so unconditional
// telemetry on stderr would cost the tokens it claims to save.
func reportStats(proc *pipeline.Processor) {
	if os.Getenv("TOKTRIM_STATS") != "1" {
		return
	}
	s := proc.Stats()
	if !s.Filtered {
		fmt.Fprintf(os.Stderr, "[toktrim] %d lines passed through unchanged\n", s.LinesIn)
		return
	}
	inTok, outTok := tokenizer.EstimateBytes(s.BytesIn), tokenizer.EstimateBytes(s.BytesOut)
	var pct float64
	if inTok > 0 {
		pct = float64(inTok-outTok) / float64(inTok) * 100
	}
	fmt.Fprintf(os.Stderr, "[toktrim] %d -> %d lines, ~%d -> ~%d est. tokens (-%.1f%%)\n",
		s.LinesIn, s.LinesOut, inTok, outTok, pct)
}

// forwardSignals relays interrupt and terminate signals to the child and
// returns a function that stops relaying.
func forwardSignals(cmd *exec.Cmd) func() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, interruptSignals()...)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-ch:
				if cmd.Process != nil {
					forwardSignal(cmd.Process, s)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
