# toktrim

A token-optimizing CLI proxy that reduces verbose developer terminal output by 70-95% before it reaches LLMs and coding agents.

## Features

- **Command Execution Mode**: `toktrim run -- <command>` runs commands and filters output
- **Pipe/Stdin Mode**: `cat log.txt | toktrim pipe` filters existing output
- **Intelligent Filtering**:
  - Test runner output compression (Jest, Vitest, Go test, Cargo test, PyTest)
  - Run-length deduplication (collapses 3+ identical consecutive lines)
  - Lockfile diff summarizer (package-lock.json, Cargo.lock, etc.)
- **Offline Token Estimate**: tokens are estimated as characters/4; no network access, no vocabulary download
- **Clean Separation**: Filtered output to stdout; stats only when `TOKTRIM_STATS=1`
- **Zero Configuration**: Works out of the box with sensible defaults

## Installation

```bash
go install github.com/vg010623/toktrim/cmd/toktrim@latest

# Or build from source:
git clone https://github.com/vg010623/toktrim.git
cd toktrim
go build -o toktrim ./cmd/toktrim
```

> **Prerequisites**: Go 1.22+ and `$HOME/go/bin` (or `$GOPATH/bin`) on your `$PATH`.
> Prebuilt binaries are published on the Releases page.

## Usage

### Command Execution Mode

```bash
toktrim run -- npm test
```

### Pipe Mode

```bash
cat test-output.log | toktrim pipe
```

### Example Output

```
$ TOKTRIM_STATS=1 toktrim run -- npm install
...
[toktrim] ~12450 -> ~580 est. tokens (-95.3%)
```

## How It Works

toktrim intercepts command output and applies a pipeline of filters:

1. **Test Runner Filter**: Identifies and compresses verbose test output while preserving failures and stack traces
2. **Deduplication Filter**: Collapses repeated lines (3+ identical consecutive lines) into a summary
3. **Diff Filter**: Summarizes noisy lockfile changes into a single line

Each filter preserves critical error information while removing noise that consumes tokens without adding value for LLMs.

## Integration with Claude Code

A `PreToolUse` hook that rewrites Bash commands is added in a later phase
(`toktrim hook`). See the README section added with that phase for the
`settings.json` snippet.

## License

MIT