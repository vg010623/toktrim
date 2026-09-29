// Package version reports how this toktrim binary was built.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// These are set at link time by goreleaser:
//
//	-ldflags "-X github.com/vg010623/toktrim/internal/version.version=v1.2.3 ..."
//
// A binary built by `go install` has none of them, so Info falls back to the
// module version the toolchain embeds instead of reporting "dev".
var (
	version = ""
	commit  = ""
	date    = ""
)

// Info describes the running binary.
type Info struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
	Platform  string
}

// Get returns the build information for this binary.
func Get() Info {
	i := Info{
		Version:   version,
		Commit:    commit,
		Date:      date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		if i.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			i.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if i.Commit == "" {
					i.Commit = s.Value
				}
			case "vcs.time":
				if i.Date == "" {
					i.Date = s.Value
				}
			}
		}
	}
	if i.Version == "" {
		i.Version = "dev"
	}
	return i
}

// String renders the version line.
func (i Info) String() string {
	s := "toktrim " + i.Version
	if i.Commit != "" {
		c := i.Commit
		if len(c) > 12 {
			c = c[:12]
		}
		s += " (" + c + ")"
	}
	return fmt.Sprintf("%s %s %s", s, i.Platform, i.GoVersion)
}

// Long renders the full build information, one field per line.
func (i Info) Long() string {
	s := fmt.Sprintf("toktrim %s\n", i.Version)
	if i.Commit != "" {
		s += fmt.Sprintf("  commit:   %s\n", i.Commit)
	}
	if i.Date != "" {
		s += fmt.Sprintf("  built:    %s\n", i.Date)
	}
	s += fmt.Sprintf("  go:       %s\n", i.GoVersion)
	s += fmt.Sprintf("  platform: %s\n", i.Platform)
	return s
}
