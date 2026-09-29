package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// FileName is the config file toktrim looks for.
const FileName = "toktrim.toml"

// Load returns the configuration for a run starting from dir.
//
// It reads, in order of increasing precedence: the user config
// (~/.config/toktrim/toktrim.toml), then toktrim.toml in dir or the nearest
// parent directory containing one. A malformed file is reported and otherwise
// ignored: a bad config must not stop a command from running.
func Load(dir string) (*Config, error) {
	cfg := Default()
	var firstErr error

	for _, path := range candidatePaths(dir) {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		values, err := parseTOML(f)
		f.Close()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", path, err)
			}
			continue
		}
		if err := cfg.apply(values); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", path, err)
		}
		cfg.Source = path
	}

	applyEnv(cfg)
	return cfg, firstErr
}

// candidatePaths lists config files from lowest to highest precedence.
func candidatePaths(dir string) []string {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "toktrim", FileName))
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "toktrim", FileName))
	}
	// Walk up from dir so a project file wins over the user file. Collect
	// parents first, so the closest one is applied last.
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err == nil {
			var up []string
			for {
				up = append(up, filepath.Join(abs, FileName))
				parent := filepath.Dir(abs)
				if parent == abs {
					break
				}
				abs = parent
			}
			for i := len(up) - 1; i >= 0; i-- {
				paths = append(paths, up[i])
			}
		}
	}
	return paths
}

// apply merges parsed values into the config.
func (c *Config) apply(values map[string]string) error {
	for key, raw := range values {
		switch key {
		case "passthrough_lines":
			if err := setInt(&c.PassthroughLines, raw, key); err != nil {
				return err
			}
		case "passthrough_bytes":
			if err := setInt(&c.PassthroughBytes, raw, key); err != nil {
				return err
			}
		case "head_lines", "truncate.head_lines":
			if err := setInt(&c.HeadLines, raw, key); err != nil {
				return err
			}
		case "tail_lines", "truncate.tail_lines":
			if err := setInt(&c.TailLines, raw, key); err != nil {
				return err
			}
		case "context_before":
			if err := setInt(&c.ContextBefore, raw, key); err != nil {
				return err
			}
		case "context_after":
			if err := setInt(&c.ContextAfter, raw, key); err != nil {
				return err
			}
		case "min_run":
			if err := setInt(&c.MinRun, raw, key); err != nil {
				return err
			}
		case "raw_log_dir":
			c.RawLogDir = raw
		case "enabled":
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			c.Enabled = b
		default:
			// [filters] table: per-filter on/off switches.
			if name, ok := trimPrefix(key, "filters."); ok {
				b, err := strconv.ParseBool(raw)
				if err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
				if c.Filters == nil {
					c.Filters = map[string]bool{}
				}
				c.Filters[name] = b
				continue
			}
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	return nil
}

func trimPrefix(s, prefix string) (string, bool) {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}

func setInt(dst *int, raw, key string) error {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if n < 0 {
		return fmt.Errorf("%s: must not be negative", key)
	}
	*dst = n
	return nil
}

// applyEnv lets environment variables override the file, which is handy for
// one-off debugging without editing a config.
func applyEnv(c *Config) {
	if os.Getenv("TOKTRIM_DISABLE") == "1" {
		c.Enabled = false
	}
	if v := os.Getenv("TOKTRIM_RAW_LOG_DIR"); v != "" {
		c.RawLogDir = v
	}
}

// FilterEnabled reports whether the named filter should run.
func (c *Config) FilterEnabled(name string) bool {
	if !c.Enabled {
		return false
	}
	if on, ok := c.Filters[name]; ok {
		return on
	}
	return true
}
