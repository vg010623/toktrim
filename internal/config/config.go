// Package config holds toktrim's tunable settings.
//
// It deliberately imports nothing from the rest of toktrim so that every other
// package can depend on it.
package config

// Defaults.
const (
	// DefaultPassthroughLines and DefaultPassthroughBytes set the size below
	// which output is emitted byte-for-byte unchanged.
	DefaultPassthroughLines = 40
	DefaultPassthroughBytes = 2048
	// DefaultHeadLines and DefaultTailLines bound the generic fallback filter.
	DefaultHeadLines = 30
	DefaultTailLines = 60
	// DefaultContextBefore and DefaultContextAfter are how many neighbouring
	// lines are kept alongside a line carrying failure information.
	DefaultContextBefore = 3
	DefaultContextAfter  = 3
	// DefaultMinRun is the shortest run of identical lines worth collapsing.
	DefaultMinRun = 3
	// DefaultSimilarWindow is how far back a repeated line is still a repeat.
	DefaultSimilarWindow = 50
	// DefaultRawLogMaxAgeDays is how long a raw output log is kept before
	// `toktrim stats` prunes it.
	DefaultRawLogMaxAgeDays = 7
)

// Config is the effective configuration for one toktrim invocation.
type Config struct {
	// Enabled false makes toktrim a transparent pass-through.
	Enabled bool
	// PassthroughLines and PassthroughBytes set the size below which output is
	// emitted unchanged.
	PassthroughLines int
	PassthroughBytes int
	// HeadLines and TailLines bound the generic truncation filter.
	HeadLines int
	TailLines int
	// ContextBefore and ContextAfter size the window of lines protected around
	// a line carrying failure information.
	ContextBefore int
	ContextAfter  int
	// MinRun is the shortest run of identical lines the dedup filter collapses.
	MinRun int
	// SimilarWindow is how far back the dedup filter looks for a repeat.
	SimilarWindow int
	// RawLogDir is where full-output logs are written. Empty means the system
	// temp directory.
	RawLogDir string
	// RawLogMaxAgeDays is how long a raw log is kept. Zero disables pruning.
	RawLogMaxAgeDays int
	// Filters holds per-filter on/off switches from the [filters] table.
	Filters map[string]bool
	// Source is the path of the last config file applied, for diagnostics.
	Source string
}

// Default returns the configuration used when nothing is set.
func Default() *Config {
	return &Config{
		Enabled:          true,
		PassthroughLines: DefaultPassthroughLines,
		PassthroughBytes: DefaultPassthroughBytes,
		HeadLines:        DefaultHeadLines,
		TailLines:        DefaultTailLines,
		ContextBefore:    DefaultContextBefore,
		ContextAfter:     DefaultContextAfter,
		MinRun:           DefaultMinRun,
		SimilarWindow:    DefaultSimilarWindow,
		RawLogMaxAgeDays: DefaultRawLogMaxAgeDays,
	}
}
