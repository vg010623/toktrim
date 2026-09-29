package filter

import "github.com/vg010623/toktrim/internal/config"

// Chain returns the filters to apply to the output of the given command.
//
// The pipeline installs a Guard ahead of whatever Chain returns, so a filter
// here can assume Protected is already set on failure lines.
func Chain(argv []string, cfg *config.Config) []LineFilter {
	return []LineFilter{
		NewDedup(),
	}
}
