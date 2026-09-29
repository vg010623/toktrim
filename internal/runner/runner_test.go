package runner

import (
	"errors"
	"os/exec"
	"testing"
)

func TestExitCodeOf(t *testing.T) {
	if got := exitCodeOf(nil); got != 0 {
		t.Errorf("exitCodeOf(nil) = %d, want 0", got)
	}

	// A real non-zero exit must be reported verbatim.
	err := exec.Command("sh", "-c", "exit 3").Run()
	if got := exitCodeOf(err); got != 3 {
		t.Errorf("exitCodeOf(exit 3) = %d, want 3", got)
	}

	err = exec.Command("sh", "-c", "exit 42").Run()
	if got := exitCodeOf(err); got != 42 {
		t.Errorf("exitCodeOf(exit 42) = %d, want 42", got)
	}

	// Anything that is not an ExitError is a toktrim-level failure.
	if got := exitCodeOf(errors.New("boom")); got != 1 {
		t.Errorf("exitCodeOf(other) = %d, want 1", got)
	}
}
