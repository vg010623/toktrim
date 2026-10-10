package ledger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vg010623/toktrim/internal/tokenizer"
)

// DefaultPruneAge is how long a raw output log is kept before `toktrim stats`
// removes it.
const DefaultPruneAge = 7 * 24 * time.Hour

// ParseSince turns a duration like "7d", "12h" or "30m" into a cutoff time.
// An empty string means no cutoff.
func ParseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	// Days are not a time.Duration unit, so handle them first.
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil || days < 0 {
			return time.Time{}, fmt.Errorf("invalid --since %q", s)
		}
		return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		weeks, err := strconv.Atoi(n)
		if err != nil || weeks < 0 {
			return time.Time{}, fmt.Errorf("invalid --since %q", s)
		}
		return time.Now().Add(-time.Duration(weeks) * 7 * 24 * time.Hour), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since %q (try 7d, 12h or 30m)", s)
	}
	return time.Now().Add(-d), nil
}

// Report writes a summary of the ledger to w.
func Report(w io.Writer, since time.Time, topN int) error {
	entries, err := Read(since)
	if err != nil {
		return err
	}
	path, _ := Path()
	if len(entries) == 0 {
		fmt.Fprintf(w, "No toktrim runs recorded%s.\n", sinceSuffix(since))
		if path != "" {
			fmt.Fprintf(w, "Ledger: %s\n", path)
		}
		return nil
	}

	var rawTotal, outTotal, filteredRuns int
	byCommand := map[string]*commandStat{}
	for _, e := range entries {
		rawTotal += e.RawBytes
		outTotal += e.OutBytes
		if len(e.Filters) > 0 {
			filteredRuns++
		}
		key := commandKey(e.Command)
		st := byCommand[key]
		if st == nil {
			st = &commandStat{Command: key}
			byCommand[key] = st
		}
		st.Runs++
		st.Raw += e.RawBytes
		st.Out += e.OutBytes
	}

	saved := rawTotal - outTotal
	if saved < 0 {
		saved = 0
	}
	pct := 0.0
	if rawTotal > 0 {
		pct = float64(saved) / float64(rawTotal) * 100
	}

	fmt.Fprintf(w, "toktrim savings%s\n\n", sinceSuffix(since))
	fmt.Fprintf(w, "  runs recorded   %d (%d filtered, %d passed through)\n",
		len(entries), filteredRuns, len(entries)-filteredRuns)
	fmt.Fprintf(w, "  bytes in        %s\n", humanBytes(rawTotal))
	fmt.Fprintf(w, "  bytes out       %s\n", humanBytes(outTotal))
	fmt.Fprintf(w, "  saved           %s (%.1f%%)\n", humanBytes(saved), pct)
	fmt.Fprintf(w, "  est. tokens     ~%s -> ~%s (saved ~%s, estimated as bytes/4)\n",
		thousands(tokenizer.EstimateBytes(rawTotal)),
		thousands(tokenizer.EstimateBytes(outTotal)),
		thousands(tokenizer.EstimateBytes(saved)))

	// Top commands by bytes saved.
	stats := make([]*commandStat, 0, len(byCommand))
	for _, st := range byCommand {
		stats = append(stats, st)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Saved() != stats[j].Saved() {
			return stats[i].Saved() > stats[j].Saved()
		}
		return stats[i].Command < stats[j].Command
	})
	if topN > 0 && len(stats) > topN {
		stats = stats[:topN]
	}

	if len(stats) > 0 {
		fmt.Fprintf(w, "\n  top commands by bytes saved\n")
		width := 0
		for _, st := range stats {
			if len(st.Command) > width {
				width = len(st.Command)
			}
		}
		if width > 40 {
			width = 40
		}
		for _, st := range stats {
			name := st.Command
			if len(name) > width {
				name = name[:width-1] + "…"
			}
			fmt.Fprintf(w, "    %-*s  %4d run%s  %9s saved  (%.0f%%)\n",
				width, name, st.Runs, pluralS(st.Runs), humanBytes(st.Saved()), st.Pct())
		}
	}

	if path != "" {
		fmt.Fprintf(w, "\nLedger: %s\n", path)
	}
	return nil
}

type commandStat struct {
	Command  string
	Runs     int
	Raw, Out int
}

func (c *commandStat) Saved() int {
	if c.Raw <= c.Out {
		return 0
	}
	return c.Raw - c.Out
}

func (c *commandStat) Pct() float64 {
	if c.Raw == 0 {
		return 0
	}
	return float64(c.Saved()) / float64(c.Raw) * 100
}

// commandKey groups runs of the same command, ignoring the arguments past the
// subcommand so that `npm run build` and `npm run lint` are told apart but
// `npm install lodash` and `npm install axios` are counted together.
//
// It looks through a shell wrapper first. The hook wraps everything as
// `bash -c '<original>'`, so without this every hook-driven run would be
// grouped under "bash" and the report would say nothing at all.
func commandKey(cmd string) string {
	fields := strings.Fields(unwrapShell(cmd))
	if len(fields) == 0 {
		return "(empty)"
	}
	keep := 1
	switch baseCommand(fields[0]) {
	case "npm", "pnpm", "yarn", "bun", "dotnet", "docker", "git", "go", "cargo":
		keep = 2
		if len(fields) > 2 && (fields[1] == "run" || fields[1] == "exec") {
			keep = 3
		}
	}
	if keep > len(fields) {
		keep = len(fields)
	}
	return strings.Join(fields[:keep], " ")
}

// unwrapShell returns the script a `sh -c` invocation runs, or cmd unchanged.
func unwrapShell(cmd string) string {
	fields := strings.Fields(cmd)
	for i, f := range fields {
		switch baseCommand(f) {
		case "bash", "sh", "zsh", "dash":
			if i+2 <= len(fields)-1 && fields[i+1] == "-c" {
				// Everything after -c is the script; its first command is what
				// the run was really about.
				script := strings.Join(fields[i+2:], " ")
				return strings.Trim(script, `"'`)
			}
		}
	}
	return cmd
}

// baseCommand strips directories and a Windows extension from a command word.
func baseCommand(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	p = strings.ToLower(p)
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		p = strings.TrimSuffix(p, ext)
	}
	return p
}

// PruneLogs removes raw output logs older than age, returning how many went
// and how many bytes that freed.
func PruneLogs(dir string, age time.Duration) (int, int64) {
	if dir == "" {
		dir = os.TempDir()
	}
	matches, err := filepath.Glob(filepath.Join(dir, "toktrim-*.log"))
	if err != nil {
		return 0, 0
	}
	cutoff := time.Now().Add(-age)
	var n int
	var freed int64
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || info.IsDir() || info.ModTime().After(cutoff) {
			continue
		}
		size := info.Size()
		if os.Remove(m) == nil {
			n++
			freed += size
		}
	}
	return n, freed
}

func sinceSuffix(since time.Time) string {
	if since.IsZero() {
		return " (all time)"
	}
	return " since " + since.Format("2006-01-02 15:04")
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// humanBytes renders a byte count at a readable scale.
func humanBytes(n int) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// thousands groups an integer with separators.
func thousands(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
