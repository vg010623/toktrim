# toktrim

A fallback output filter for Claude Code, meant to sit alongside **rtk** (Rust
Token Killer). rtk compresses the common commands — git, cargo, pytest, jest,
tsc, eslint, ls, grep — and deliberately skips everything else. toktrim takes the leftovers: npm/pnpm/yarn installs and
scripts, `docker build`, `dotnet`, and pipelines or chained commands.

It is a wrapper, not a rewriter. **The exit code always matches the wrapped
command, output is always printed before toktrim exits, and anything removed is
recoverable from a log file named in the output.**

## What it does

| Command family | What is removed | What is always kept |
| --- | --- | --- |
| `npm`/`pnpm`/`yarn install` | deprecation warnings, registry and dependency-tree chatter, funding notices | `ERR!` blocks, `added N packages`, vulnerability counts |
| `npm run <script>` | the above, plus passing-test noise when the script turns out to be jest or vitest | every failing test's name, assertion and stack frames; the summary |
| `docker build` | each step's layer and download progress | step headers, the failing step's full output, the final image ID |
| `dotnet build`/`test` | per-project restore and build lines | warnings with their codes, errors, the build and test summary |
| anything else | runs of identical lines; the middle of very long output | the first 30 and last 60 lines, and every failure line in between |
| all of the above | ANSI colour codes, `\r` redraws, progress bars collapsed to their final state | — |

### Measured on the recorded fixtures

These are produced by `go test ./internal/golden -v` and asserted as floors by
`TestMeasuredSavings`, so they cannot drift from what the code actually does.
The inputs are in [`testdata/`](./testdata), each with a `PROVENANCE` file
saying where it came from.

| Fixture | Bytes | Lines | Saved |
| --- | --- | --- | --- |
| `npm install` (real capture, 4 deps with deprecated transitives) | 186,289 → 867 | 1871 → 22 | 99.5% |
| `make` (400 lines, failures buried in the middle) | 15,512 → 4,000 | 406 → 102 | 74.2% |
| `cargo` with spinners and a download bar | 846 → 284 | 15 → 8 | 66.4% |
| `docker build` with a failing step | 3,866 → 2,015 | 84 → 52 | 47.9% |
| `dotnet build` with 2 warnings and 2 errors | 1,827 → 1,469 | 23 → 18 | 19.6% |
| `npm test` with a failing jest suite | 1,347 → 1,347 | 45 → 45 | 0% |

That last row is not a bug. A failing test run is almost entirely failure
information, so toktrim leaves it alone. Compression is highest where the output
is noise and lowest where it matters — which is the point.

## What it deliberately skips

toktrim's hook leaves a command alone when:

- it starts with `rtk` or `toktrim`;
- **rtk already covers it**: `git status`/`log`/`diff`/`show`, `cargo`,
  `go test`, `pytest`, `jest`, `vitest`, `tsc`, `eslint`, `ls`, `grep`, `find`.
  Note that `go build` and `git commit` are *not* in that list, so toktrim does
  handle those;
- it is long-running or interactive: a `dev`, `start`, `watch`, `serve` script,
  `--watch`, `-it`, `tail -f`, or `docker run` without `--rm`;
- it already sends all output to a file (`> log 2>&1`, `&> log`).

A chain like `npm ci && npm run build` *is* handled, because rtk does not
rewrite chains — unless every segment is itself an rtk command.

## Install

### From Releases (no Go needed)

Grab the binary for your platform from the
[latest release](https://github.com/vg010623/toktrim/releases/latest):

- **Windows**: `toktrim_windows_amd64.exe` → rename to `toktrim.exe`, put it on
  your `PATH`.
- **macOS**: `toktrim_darwin_arm64` (Apple silicon) or `toktrim_darwin_amd64`.
- **Linux**: `toktrim_linux_amd64` or `toktrim_linux_arm64`.

```bash
chmod +x toktrim_linux_amd64 && sudo mv toktrim_linux_amd64 /usr/local/bin/toktrim
toktrim version
```

Verify your download against `checksums.txt` on the release page.

### With Go

```bash
go install github.com/vg010623/toktrim/cmd/toktrim@latest
```

toktrim has **no dependencies** and makes **no network requests**. CI asserts
both, and builds with `GOPROXY=off` to prove it.

## Use

```bash
toktrim run -- npm install          # run a command, filter its output
toktrim run -- bash -c 'a && b'     # pipes, &&, redirects all work
cat build.log | toktrim pipe        # filter output you already have
toktrim version
```

`exec` is accepted as an alias for `run`.

## Claude Code hook

Add to `~/.claude/settings.json` (or a project `.claude/settings.json`):

```json
{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"toktrim hook"}]}]}}
```

On Windows, Claude Code's Bash tool runs under Git Bash, and `toktrim hook`
needs to be resolvable on the `PATH` that Claude Code sees.

### The hook never approves anything

`toktrim hook` returns `hookSpecificOutput.updatedInput` and **never**
`permissionDecision`. Claude Code's
[hooks reference](https://code.claude.com/docs/en/hooks) documents exit 0 with
no decision as *"the hook has no decision to report, so the tool call continues
through the normal permission flow"*, and lists `permissionDecision` and
`updatedInput` as independent fields. Returning `updatedInput` alone therefore
rewrites the command and leaves your permission prompt exactly where it was.

That is deliberate: toktrim wraps arbitrary commands, so it must never be the
reason one skips your prompt. A test asserts the field is absent from every
reply. Caveat worth stating plainly: the docs' own worked example pairs
`updatedInput` with `permissionDecision: "allow"`, and they do not spell out the
no-decision combination for `updatedInput` specifically. The behaviour above is
what the documented exit-0 rule implies, and what we chose in order to keep the
normal flow.

The permission prompt you see will show the rewritten command:

```
toktrim run -- bash -c 'npm run build'
```

### Debugging the hook

```bash
echo '{"tool_input":{"command":"npm run build"}}' | toktrim hook
# {"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"toktrim run -- bash -c 'npm run build'"}}}

echo '{"tool_input":{"command":"git status"}}' | toktrim hook
# (nothing — rtk's territory)

TOKTRIM_HOOK_DEBUG=1 echo '{"tool_input":{"command":"npm run dev"}}' | toktrim hook
# [toktrim hook] leaving alone: long-running or interactive command
```

The hook always exits 0 and prints nothing on any error, so it can never block a
command.

## Configuration

`toktrim.toml` in your project root, or `~/.config/toktrim/toktrim.toml` for
defaults everywhere. A project file wins over the user file, setting by setting.
See [`toktrim.example.toml`](./toktrim.example.toml) for the annotated version.

```toml
enabled = true            # false, or TOKTRIM_DISABLE=1, for a pass-through
passthrough_lines = 40    # output at or below BOTH limits is untouched
passthrough_bytes = 2048
context_before = 3        # lines kept around a failure
context_after = 3
min_run = 3               # shortest run of identical lines to collapse
raw_log_dir = ""          # empty = system temp dir

[truncate]
head_lines = 30
tail_lines = 60

[filters]                 # every filter is on unless set to false here
ansi = true
node-install = true
test-runner = true
docker-build = true
dotnet = true
dedup = true
truncate = true
```

Only a subset of TOML is supported: comments, `[table]` headers, and
`key = value` for strings, integers and booleans. An unknown key or an
unsupported value type is reported on stderr and the file is ignored — a broken
config never stops a command from running.

### Environment variables

| Variable | Effect |
| --- | --- |
| `TOKTRIM_STATS=1` | report what was trimmed, on stderr |
| `TOKTRIM_DISABLE=1` | pass everything through unchanged |
| `TOKTRIM_RAW_LOG_DIR` | where full-output logs go |
| `TOKTRIM_HOOK_DEBUG=1` | explain why the hook stayed silent |

Stats are off by default on purpose: Claude Code forwards the Bash tool's stderr
to the model, so unconditional telemetry would cost the tokens it claims to save.

## Guarantees

1. **The result is unchanged.** The exit code matches the wrapped command
   exactly. A command that cannot be started exits 127.
2. **Failure information is never dropped.** Any line matching
   error/fail/panic/exception/traceback/`FAILED`/`ERR!`/stack-frame patterns is
   kept verbatim, with three lines of context on each side. Compiler
   diagnostics, which name their own file and code, are kept without context.
3. **There is always a way back.** Whenever anything is removed, the raw output
   is written to a temp file and the filtered output ends with
   `[toktrim] N lines elided — full output: <path>`.
4. **Small output is untouched.** Output at or below 40 lines and 2 KB comes out
   byte-for-byte identical, including CRLF line endings and ANSI codes.
5. **Nothing is buffered whole.** Output is filtered line by line as it arrives,
   so a command killed by Claude Code's two-minute Bash timeout still shows
   everything it printed. `SIGINT`/`SIGTERM` flush what was processed and are
   relayed to the child.

## Known limitations

- **Two hooks rewriting one command race.** Claude Code runs `PreToolUse` hooks
  in parallel and, when more than one returns `updatedInput` for the same tool
  call, the last to finish wins. This is exactly why toktrim skips everything
  rtk handles — the skip list keeps the two tools from fighting. If you add a
  third hook that rewrites Bash commands, make sure its scope does not overlap.
- **Windows cannot forward a signal.** `os.Process.Signal` supports only `Kill`
  there, so a relayed `SIGTERM` kills the child. Everything toktrim processed is
  already flushed by then. In practice a console process group receives Ctrl-C
  and Ctrl-Break directly, so the child usually sees it first.
- **Token counts are estimates.** Tokens are reported as characters ÷ 4 and
  labelled as estimates. A real BPE tokenizer would mean either a large bundled
  vocabulary or a download at runtime, and toktrim has to work offline.
- **`docker build` buffers one step at a time.** Whether a step's output matters
  is only known when it finishes, so each step is held until it reports
  `DONE`/`ERROR`. A single step printing more than 4,000 lines before failing is
  truncated; the raw log still has all of it.
- **`npm run <script>` is detected from its output.** A script may be anything,
  so the test filter stays a pass-through until the output identifies itself as
  jest or vitest. The first few lines of a test run are not compressed.
- **The generic fallback reorders nothing but does interleave markers.** When
  failures are rescued from the middle of long output, they appear between
  `[toktrim] ...` markers in their original order, not in place.
- **`docker`/`dotnet` fixtures are transcripts, not captures.** Neither tool was
  installed on the machine the fixtures were recorded on; both files record
  their provenance.

## Development

```bash
go test ./...                              # everything
go test ./internal/golden -v               # fixture tests, with measured savings
go test ./internal/golden -update          # rewrite goldens after a change
go test -race ./...
GOOS=windows go vet ./...                  # the hook's main target
```

CI runs on `ubuntu-latest` and `windows-latest`: `go vet`, `gofmt`,
`go test -race`, the golden tests, the hook JSON tests, the acceptance tests, a
check that the goldens are not stale, a check that `go.mod` has no requirements,
and a goreleaser snapshot build asserting the Windows `.exe` is in the release.

## Licence

MIT
