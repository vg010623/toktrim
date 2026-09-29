//go:build !windows

package runner

import (
	"os"
	"syscall"
)

// interruptSignals lists the signals toktrim relays to the child.
func interruptSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}
}

// forwardSignal passes s on to the child process.
func forwardSignal(p *os.Process, s os.Signal) {
	_ = p.Signal(s)
}
