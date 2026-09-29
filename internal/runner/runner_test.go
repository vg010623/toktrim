package runner

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// exitWith runs a trivial child that exits with the given code, in a way that
// works on both a POSIX shell and Windows without one.
func exitWith(t *testing.T, code int) error {
	t.Helper()
	// `go run` is always available wherever the tests are running, but it is
	// slow; prefer a shell when there is one.
	if sh, err := exec.LookPath("sh"); err == nil {
		return exec.Command(sh, "-c", "exit "+itoa(code)).Run()
	}
	if runtime.GOOS == "windows" {
		if cmdExe, err := exec.LookPath("cmd"); err == nil {
			return exec.Command(cmdExe, "/c", "exit "+itoa(code)).Run()
		}
	}
	t.Skip("no shell available to produce an exit code")
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestExitCodeOf(t *testing.T) {
	if got := exitCodeOf(nil); got != 0 {
		t.Errorf("exitCodeOf(nil) = %d, want 0", got)
	}

	// A real non-zero exit must be reported verbatim.
	for _, want := range []int{1, 3, 42} {
		err := exitWith(t, want)
		if got := exitCodeOf(err); got != want {
			t.Errorf("exitCodeOf(exit %d) = %d, want %d", want, got, want)
		}
	}

	// Anything that is not an ExitError is a toktrim-level failure.
	if got := exitCodeOf(errors.New("boom")); got != 1 {
		t.Errorf("exitCodeOf(other) = %d, want 1", got)
	}
}

func TestRunReportsCommandNotFound(t *testing.T) {
	// A command that cannot be started exits 127, the shell convention.
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()

	stderr := os.Stderr
	os.Stderr = devnull
	defer func() { os.Stderr = stderr }()

	if got := Run([]string{"toktrim-no-such-binary-xyz123"}, nil); got != ExitCommandNotFound {
		t.Errorf("Run(missing binary) = %d, want %d", got, ExitCommandNotFound)
	}
	if got := Run(nil, nil); got != ExitCommandNotFound {
		t.Errorf("Run(no args) = %d, want %d", got, ExitCommandNotFound)
	}
}
