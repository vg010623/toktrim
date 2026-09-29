//go:build windows

package runner

import (
	"os"
	"syscall"
)

// interruptSignals lists the signals toktrim relays to the child.
//
// Windows delivers Ctrl-C as os.Interrupt and Ctrl-Break as SIGBREAK. A console
// process group receives both directly, so the child usually sees them before
// toktrim does; relaying is a backstop for when it does not.
func interruptSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGBREAK}
}

// forwardSignal passes s on to the child process.
//
// Windows has no signal delivery to another process: Process.Signal only
// supports Kill. Since toktrim has already flushed what it processed, killing
// the child is the honest equivalent of forwarding a terminate.
func forwardSignal(p *os.Process, s os.Signal) {
	_ = p.Kill()
}
