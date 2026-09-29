//go:build windows

package runner

import (
	"os"
	"syscall"
)

// interruptSignals lists the signals toktrim relays to the child.
//
// Windows has no SIGBREAK in the syscall package. The Go runtime's console
// control handler maps both CTRL_C_EVENT and CTRL_BREAK_EVENT onto SIGINT, so
// os.Interrupt is what a Ctrl-C and a Ctrl-Break both arrive as, and listening
// for it covers the cases the spec asks about.
func interruptSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// forwardSignal passes s on to the child process.
//
// Windows cannot deliver a signal to another process: os.Process.Signal only
// supports Kill. A console process group already receives Ctrl-C and Ctrl-Break
// directly, so the child normally sees the interrupt before toktrim does; this
// is the backstop for when it does not. Everything toktrim has processed has
// already been flushed by the time this runs, so killing the child is the
// honest equivalent of forwarding a terminate.
func forwardSignal(p *os.Process, s os.Signal) {
	_ = p.Kill()
}
