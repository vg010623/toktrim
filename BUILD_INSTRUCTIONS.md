# Build and Test Instructions

## Prerequisites
- Go 1.22 or later (https://golang.org/dl/)
- Git

## Building
```bash
# Clone the repository (if you haven't already)
git clone https://github.com/yourusername/toktrim.git
cd toktrim

# Install dependencies
go mod tidy

# Build the binary
go build -o toktrim ./cmd/toktrim

# Or install directly
go install github.com/yourusername/toktrim@latest
```

## Running Tests
```bash
go test ./...
```

## Quick Usage Examples
```bash
# Command execution mode
toktrim exec -- npm test

# Pipe mode
cat test-output.log | toktrim pipe

# With Claude Code (add to ~/.claude/settings.json)
# See README.md for details
```

## Expected Behavior
- Filtered output goes to stdout (clean for LLMs)
- Token analytics go to stderr (does not pollute LLM context)
- Example stderr output: `[toktrim] Tokens: 12,450 -> 580 (-95.3%) | Saved: ~$0.0338`