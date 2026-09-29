# toktrim

A token-optimizing CLI proxy that reduces verbose developer terminal output by 70-95% before it reaches LLMs and coding agents.

## Features

- **Command Execution Mode**: `toktrim exec -- <command>` runs commands and filters output
- **Pipe/Stdin Mode**: `cat log.txt | toktrim pipe` filters existing output
- **Intelligent Filtering**:
  - Test runner output compression (Jest, Vitest, Go test, Cargo test, PyTest)
  - Run-length deduplication (collapses 3+ identical consecutive lines)
  - Lockfile diff summarizer (package-lock.json, Cargo.lock, etc.)
- **Exact Token Counting**: Uses tiktoken-go with cl100k_base encoding (GPT-4o compatible)
- **Clean Separation**: Filtered output to stdout, telemetry to stderr
- **Zero Configuration**: Works out of the box with sensible defaults

## Installation

⚠️ **Note**: The module path in this repository is a placeholder (`github.com/yourusername/toktrim`).  
Before installing, replace `yourusername` with your actual GitHub username or organization, or build from source as shown below.

```bash
# If you have set up the module path correctly:
go install github.com/yourusername/toktrim@latest

# Or build from source:
git clone https://github.com/yourusername/toktrim.git
cd toktrim
go build -o toktrim ./cmd/toktrim
```

> **Prerequisites**: Go 1.22+ must be installed and `$HOME/go/bin` (or `$GOPATH/bin`) added to your `$PATH`.  
> The token encoding is downloaded from OpenAI’s servers on first use (cl100k_base, GPT‑4o). This provides a close but not exact match to Claude’s internal tokenizer.

## Usage

### Command Execution Mode

```bash
toktrim exec -- npm test
```

### Pipe Mode

```bash
cat test-output.log | toktrim pipe
```

### Example Output

```
[toktrim] Tokens: 12,450 -> 580 (-95.3%) | Saved: ~$0.0338
```

## How It Works

toktrim intercepts command output and applies a pipeline of filters:

1. **Test Runner Filter**: Identifies and compresses verbose test output while preserving failures and stack traces
2. **Deduplication Filter**: Collapses repeated lines (3+ identical consecutive lines) into a summary
3. **Diff Filter**: Summarizes noisy lockfile changes into a single line

Each filter preserves critical error information while removing noise that consumes tokens without adding value for LLMs.

## Integration with Claude Code

Add this to your Claude Code `settings.json` to enable automatic filtering:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "validator": {
          "type": "command",
          "command": "toktrim exec --"
        }
      }
    ]
  }
}
```

> The `validator` field runs the specified command and allows the original Bash tool to proceed only if the command exits with status 0.  
> If your Claude Code version uses `approver` instead of `validator`, replace `validator` with `approver` — both forms are accepted in different versions.

## License

MIT