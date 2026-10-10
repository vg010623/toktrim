// Package ledger records what each toktrim run saved, as one JSON object per
// line.
//
// Every write is best effort. A ledger is a convenience, so nothing it does
// may interfere with the command the user actually ran: failures to create the
// directory, open the file or write the line are all swallowed.
package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// maxCommandLen bounds how much of a command line is recorded. A ledger should
// not become a transcript of every argument the user ever typed.
const maxCommandLen = 80

// Entry is one line of the ledger.
type Entry struct {
	Time     time.Time `json:"time"`
	Command  string    `json:"command"`
	Filters  []string  `json:"filters,omitempty"`
	RawBytes int       `json:"raw_bytes"`
	OutBytes int       `json:"out_bytes"`
	ExitCode int       `json:"exit_code"`
	RawLog   string    `json:"raw_log,omitempty"`
}

// Saved returns the bytes this entry avoided sending to the model.
func (e Entry) Saved() int {
	if e.RawBytes <= e.OutBytes {
		return 0
	}
	return e.RawBytes - e.OutBytes
}

// Dir returns the directory the ledger lives in.
//
// XDG_STATE_HOME when set, ~/.local/state/toktrim on Unix, and
// %LOCALAPPDATA%\toktrim on Windows.
func Dir() (string, error) {
	if v := os.Getenv("TOKTRIM_STATE_DIR"); v != "" {
		return v, nil
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "toktrim"), nil
		}
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "toktrim"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "toktrim"), nil
}

// Path returns the ledger file's path.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ledger.jsonl"), nil
}

// Append adds one entry to the ledger, silently doing nothing on any failure.
func Append(e Entry) {
	if os.Getenv("TOKTRIM_NO_LEDGER") == "1" {
		return
	}
	path, err := Path()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Command = TruncateCommand(e.Command)

	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	// One Write, so concurrent toktrim runs append whole lines rather than
	// interleaving halves of two entries.
	f.Write(append(line, '\n'))
}

// TruncateCommand shortens a command line for recording.
//
// The result is at most maxCommandLen bytes including the ellipsis, and is cut
// on a rune boundary so a multi-byte character is never split in half.
func TruncateCommand(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxCommandLen {
		return s
	}
	const ellipsis = "…"
	cut := maxCommandLen - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

// Read returns the entries recorded since the given time, oldest first.
//
// A malformed line is skipped rather than failing the read: the ledger is
// append-only from possibly-concurrent processes, and one bad line should not
// hide the rest.
func Read(since time.Time) ([]Entry, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if !since.IsZero() && e.Time.Before(since) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
