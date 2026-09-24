package main

import (
	"fmt"
	"os"

	"github.com/yourusername/toktrim/internal/runner"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [args...]\n", os.Args[0])
		os.Exit(1)
	}
	// The first argument is the subcommand (exec or pipe)
	switch os.Args[1] {
	case "exec":
		if len(os.Args) < 3 || os.Args[2] != "--" {
			fmt.Fprintf(os.Stderr, "Usage: %s exec -- <command> [args...]\n", os.Args[0])
			os.Exit(1)
		}
		runner.ExecuteCommand(os.Args[3:])
	case "pipe":
		runner.ProcessStdin()
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", os.Args[1])
		os.Exit(1)
	}
}
