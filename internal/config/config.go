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
)

// Config is the effective configuration for one toktrim invocation.
type Config struct {
	// PassthroughLines and PassthroughBytes set the size below which output is
	// emitted unchanged.
	PassthroughLines int
	PassthroughBytes int
	// RawLogDir is where full-output logs are written. Empty means the system
	// temp directory.
	RawLogDir string
}

// Default returns the configuration used when nothing is set.
func Default() *Config {
	return &Config{
		PassthroughLines: DefaultPassthroughLines,
		PassthroughBytes: DefaultPassthroughBytes,
	}
}
