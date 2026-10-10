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
| `npm test` / `npm run <script>` | the above, plus passing-test noise once the output identifies itself as jest, vitest, mocha, node:test or TAP | every failing test's name, assertion, diff and stack frames, byte-for-byte; the summary |
| any noisy log stream | lines repeated by *shape* — same sentence, different duration, id, hash or quoted value | anything carrying failure information, and every line whose varying part is an identifier |
| query commands (`npm ls`, `gh`, `cat`, `jq`, …) | escape codes and adjacent duplicates only | everything else: a query's output is the answer, so it is never truncated |
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
| `npm install` (real capture, deprecated transitives) | 186,289 → 866 | 1871 → 21 | 99.5% |
| `npm test`, mocha, 400 passing + 6 failures | 52,301 → 4,434 | 967 → 158 | 91.5% |
| `npm start`, interleaved app logs | 11,761 → 1,311 | 154 → 20 | 88.9% |
| `npm test`, mocha, all passing | 11,866 → 1,694 | 221 → 54 | 85.7% |
| `make` (400 lines, failures in the middle) | 15,512 → 4,000 | 406 → 102 | 74.2% |
| `npm test`, node:test / TAP | 2,202 → 592 | 120 → 28 | 73.1% |
| `cargo` with spinners and a download bar | 846 → 284 | 15 → 8 | 66.4% |
| `docker build` with a failing step | 3,866 → 2,015 | 84 → 52 | 47.9% |
| `dotnet build`, 2 warnings + 2 errors | 1,827 → 1,469 | 23 → 18 | 19.6% |
| `npm test`, failing jest suite | 1,347 → 1,347 | 45 → 45 | 0% |
| `npx eslint`, every line an error | 8,106 → 8,106 | 125 → 125 | 0% |
| `npm ls --all` (3145 lines) | 157,075 → 157,075 | 3145 → 3145 | 0% |

The three 0% rows are the point, not a shortfall:

- A **failing jest suite** is almost entirely failure information, so there is
  nothing to drop.
- **eslint output where every line is an error** has nothing droppable either,
  and toktrim now returns the original bytes rather than adding markers. In
  v0.2.0 this case came out **2.89% larger** than its input.
- **`npm ls --all`** is a query: its output *is* the answer, so it is never
  truncated. v0.2.0 cut it to 435 lines and lost every package that only
  appears in the middle of the tree.

Compression is highest where the output is noise and lowest where it matters.

## What it deliberately skips

toktrim's hook leaves a command alone when:

- it starts with `rtk` or `toktrim`;
- **rtk already covers it *and rtk is installed***: `git status`/`log`/`diff`/
  `show`, `cargo`, `go test`, `pytest`, `jest`, `vitest`, `tsc`, `eslint`, `ls`,
  `grep`, `find`. toktrim looks `rtk` up on `PATH` once per process; if it is
  not there, toktrim handles these too, since otherwise nothing would.
  `go build` and `git commit` are *not* on rtk's list, so toktrim always handles
  those. A binary runner is normalised first, so `npx jest`, `bunx vitest`,
  `pnpm exec jest` and `yarn jest` are treated exactly like bare `jest` — in
  v0.2.0 the `npx` spelling was wrapped while the bare one was skipped;
- it is long-running or interactive: a `dev`, `start`, `watch`, `serve` script,
  `--watch`, `-it`, `tail -f`, or `docker run` without `--rm`;
- it already sends all output to a file (`> log 2>&1`, `&> log`);
- **it is a query, whose output is the answer**: `npm`/`pnpm`/`yarn`
  `ls`/`outdated`/`view`/`why`/`info`/`explain`/`search`/`audit`, `gh`, `cat`,
  `jq`, `head`, non-follow `tail`, `sed -n`, `awk`, `git config`/`remote`/
  `rev-parse`/`describe`;
- **the pipeline already selects its own output**: anything ending in `head`,
  `tail`, `grep`, `wc`, `sort`, `uniq`, `cut` or `jq` — whoever wrote that
  pipeline already decided how much they wanted.

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
toktrim stats                      # what toktrim has saved
toktrim stats --since 7d           # and over the last week
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
similar_window = 50       # how far back a repeated line is still a repeat
raw_log_dir = ""          # empty = system temp dir
raw_log_max_age_days = 7  # `toktrim stats` prunes logs older than this

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
| `TOKTRIM_NO_LEDGER=1` | do not record runs in the savings ledger |
| `TOKTRIM_STATE_DIR` | where the ledger lives |

Stats are off by default on purpose: Claude Code forwards the Bash tool's stderr
to the model, so unconditional telemetry would cost the tokens it claims to save.

## Guarantees

1. **The result is unchanged.** The exit code matches the wrapped command
   exactly. A command that cannot be started exits 127.
2. **Failure information is never dropped.** Any line matching
   error/fail/panic/exception/traceback/`FAILED`/`ERR!`/stack-frame patterns is
   kept verbatim, with three lines of context on each side. Compiler
   diagnostics, which name their own file and code, are kept without context.
3. **A failure block is kept whole.** A mocha numbered failure, a jest `●`, a
   traceback, an `AssertionError` or a TAP `not ok` opens a *block*, and every
   line is kept until it closes — however long the assertion diff is. If a
   single block ever exceeds 500 lines it is cut with a marker at the point of
   the cut. Nothing inside a failure is ever dropped silently; a test asserts
   this across every fixture, and another asserts it still catches the v0.2.0
   output that violated it.
4. **There is always a way back.** Whenever anything is removed, the raw output
   is written to a temp file and the filtered output ends with
   `[toktrim] N lines elided — full output: <path>`.
5. **Small output is untouched.** Output at or below 40 lines and 2 KB comes out
   byte-for-byte identical, including CRLF line endings and ANSI codes.
6. **Filtering never makes the output bigger.** While the output still fits in
   the 1 MiB decision window, toktrim holds both forms and emits whichever is
   smaller, footer included. If the raw form wins, the log is deleted too —
   there is nothing to recover when nothing was removed.
7. **Nothing is buffered whole.** Output is filtered line by line as it arrives,
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
- **Cross-gap repeats are summarised at the end, not in place.** A line
  suppressed because the same shape appeared earlier is counted into one
  `[toktrim] ×N further similar lines: …` marker printed after the output.
  There is no single point in the stream where that marker would belong.
- **Shape matching only applies to log-shaped lines.** A line is matched by
  shape — numbers, ids, hashes and quoted values blanked — only when it starts
  with a level like `info:`/`warn:`, a `DEPRECATION WARNING`, or an npm/pnpm/
  yarn log prefix. Everything else must match byte for byte, because
  `[  7/400] CC module_007.o` and `Suite7 Controller` differ from their
  neighbours only in a digit and that digit is the content.
- **`toktrim pipe` cannot tell a query from a build.** Classification needs the
  command line, which `pipe` does not have, so piped output gets the generic
  chain including truncation. Use `toktrim run --` to get the query protection.
- **Beyond 1 MiB, the never-grow guarantee is best effort.** Past the decision
  window toktrim has already streamed what it produced and cannot retract it.
  At that size markers cannot plausibly outweigh what was removed.
- **`docker`/`dotnet` fixtures are transcripts, not captures.** Neither tool was
  installed on the machine the fixtures were recorded on; both files record
  their provenance.
- **Raw logs are not cleaned up.** Every run that trims something leaves a
  `toktrim-*.log` in the temp directory, because deleting it would defeat the
  point of naming it in the output. They are small and the OS clears the temp
  directory eventually; set `raw_log_dir` if you would rather collect them
  somewhere you can prune yourself.

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
