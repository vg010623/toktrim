package filter

import (
	"strings"

	"github.com/vg010623/toktrim/internal/config"
)

// Kind classifies a command so the right filters can be selected.
type Kind string

// The command families toktrim recognises.
const (
	KindNodeInstall Kind = "node-install"
	KindNodeScript  Kind = "node-script"
	KindDocker      Kind = "docker-build"
	KindDotNet      Kind = "dotnet"
	KindQuery       Kind = "query"
	KindGeneric     Kind = "generic"
)

// Classify works out which family a command belongs to.
//
// argv is the command as it will be run. When toktrim wraps a shell invocation
// (`bash -c "npm ci && npm test"`) the script itself is inspected, because that
// is where the interesting command name is.
func Classify(argv []string) Kind {
	// A query is checked first: its output is the answer, so it must never be
	// truncated no matter what else the command line looks like.
	if IsQueryCommand(argv) {
		return KindQuery
	}
	words := commandWords(argv)
	// npm and friends are checked first: the subcommand decides whether this is
	// an install or a script run.
	if k, ok := classifyNode(words); ok {
		return k
	}
	for _, w := range words {
		switch w {
		case "docker", "podman", "buildx":
			return KindDocker
		case "dotnet", "msbuild":
			return KindDotNet
		}
	}
	return KindGeneric
}

// classifyNode distinguishes an install from a script run.
func classifyNode(words []string) (Kind, bool) {
	for i, w := range words {
		switch w {
		case "npm", "pnpm", "yarn", "bun":
			sub := ""
			for _, next := range words[i+1:] {
				if strings.HasPrefix(next, "-") {
					continue
				}
				sub = next
				break
			}
			switch sub {
			case "install", "i", "ci", "add", "update", "upgrade", "remove", "rm", "uninstall", "dedupe", "prune":
				return KindNodeInstall, true
			case "":
				// Bare `yarn` installs.
				return KindNodeInstall, true
			default:
				return KindNodeScript, true
			}
		}
	}
	return KindGeneric, false
}

// baseName returns the last path element, splitting on both separators.
//
// filepath.Base only understands the host's separator, but toktrim classifies
// Windows command lines (`C:\Program Files\nodejs\npm.cmd`) while its tests and
// CI also run on Linux, so both separators have to be handled everywhere.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	p = strings.ToLower(p)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		p = strings.TrimSuffix(p, ext)
	}
	return p
}

// commandWords reduces argv to the words that could name a command, following
// into a shell -c script when there is one.
func commandWords(argv []string) []string {
	var words []string
	for i, a := range argv {
		base := baseName(a)
		words = append(words, base)
		if (base == "bash" || base == "sh" || base == "zsh" || base == "dash") && i+2 <= len(argv)-1 {
			if argv[i+1] == "-c" {
				words = append(words, scriptWords(argv[i+2])...)
			}
		}
	}
	return words
}

// scriptWords pulls the command names out of a shell script fragment.
func scriptWords(script string) []string {
	repl := strings.NewReplacer("&&", " ", "||", " ", "|", " ", ";", " ", "(", " ", ")", " ", "\n", " ")
	var words []string
	for _, f := range strings.Fields(repl.Replace(script)) {
		if base := baseName(strings.Trim(f, `"'`)); base != "" {
			words = append(words, base)
		}
	}
	return words
}

// Chain returns the filters to apply to the output of the given command.
//
// The order is fixed: escape sequences are cleaned first so later filters match
// on plain text; Guard then marks failure lines, and everything after it must
// leave those alone; the command-specific filter runs next; dedup and
// truncation act as the generic backstop.
//
// A Guard is always present. Disabling it through config is not offered,
// because every compression step downstream relies on it.
func Chain(argv []string, cfg *config.Config) []LineFilter {
	if cfg == nil {
		cfg = config.Default()
	}
	if !cfg.Enabled {
		return nil
	}

	var chain []LineFilter
	add := func(name string, f LineFilter) {
		if cfg.FilterEnabled(name) {
			chain = append(chain, f)
		}
	}

	add("ansi", NewANSI())

	// Guard is unconditional.
	g := NewGuard()
	g.ContextBefore, g.ContextAfter = cfg.ContextBefore, cfg.ContextAfter
	chain = append(chain, g)

	kind := Classify(argv)
	switch kind {
	case KindNodeInstall:
		add("node-install", NewNodePackageManager())
	case KindNodeScript:
		// A script may be anything; the test filter switches itself on when the
		// output turns out to be jest or vitest.
		add("node-install", NewNodePackageManager())
		add("test-runner", NewAutoTestRunner())
	case KindDocker:
		add("docker-build", NewDockerBuild())
	case KindDotNet:
		add("dotnet", NewDotNet())
	case KindQuery:
		// Nothing command-specific, and -- below -- no truncation.
	}

	d := NewDedup()
	d.MinRun = cfg.MinRun
	d.SimilarWindow = cfg.SimilarWindow
	if kind == KindQuery {
		// In a dependency tree or a config dump, the same line at a different
		// position is still information. Only adjacent repeats go.
		d.SimilarWindow = 0
	}
	add("dedup", d)

	// Truncation is positional: it drops lines for where they are rather than
	// what they say. That is the right trade for a build log and the wrong one
	// for a query, whose middle is exactly what was being looked up.
	if kind != KindQuery {
		add("truncate", NewTruncate(cfg.HeadLines, cfg.TailLines))
	}
	return chain
}

// queryCommands are commands whose output *is* the answer to a question.
//
// Truncating them defeats the purpose: `npm ls --all` is run to look a package
// up, and the middle of the tree is where most packages live. These get the
// cheap, lossless filters -- escape stripping, carriage returns, dedup -- and
// never positional truncation.
var queryCommands = map[string]bool{
	// Print a file or a stream, as asked.
	"cat": true, "bat": true, "glow": true, "jq": true, "yq": true,
	"head": true, "tail": true, "awk": true, "column": true, "gh": true,

	// Print one fact.
	"env": true, "printenv": true, "pwd": true, "which": true, "whereis": true,
	"date": true, "hostname": true, "uname": true, "id": true, "whoami": true,
	"groups": true, "locale": true, "nproc": true, "arch": true, "realpath": true,
	"readlink": true, "basename": true, "dirname": true, "stat": true, "file": true,

	// Print a listing whose middle matters as much as its ends. `ps aux` cut to
	// its head and tail is missing processes, and `df` on a host with many
	// mounts is missing filesystems -- both of which a tester would read as a
	// complete answer.
	"ps": true, "df": true, "du": true, "lsof": true, "netstat": true,
	"ss": true, "mount": true, "lsblk": true, "free": true, "uptime": true,
	"ifconfig": true, "ip": true, "dig": true, "nslookup": true, "host": true,
	"sysctl": true, "tree": true, "shasum": true, "sha256sum": true, "md5sum": true,
}

// readOnlyVerbs are subcommand names that mean "show me something" rather than
// "do something", across almost every CLI: git log, npm ls, docker images,
// kubectl get, pip freeze, cargo tree, go env.
//
// Getting this wrong is deliberately asymmetric. Calling a build log a query
// costs some compression. Calling a query a build log loses the answer -- a
// `git log` cut to its first and last lines is missing commits, and a reader
// has no way to tell. So the list errs towards matching.
var readOnlyVerbs = map[string]bool{
	"list": true, "ls": true, "show": true, "get": true, "describe": true,
	"info": true, "view": true, "inspect": true, "status": true, "diff": true,
	"tree": true, "search": true, "why": true, "explain": true, "outdated": true,
	"freeze": true, "env": true, "version": true, "help": true, "config": true,
	"metadata": true, "graph": true, "blame": true, "history": true, "cat": true,
	"print": true, "dump": true, "export": true, "report": true, "summary": true,
	"stat": true, "stats": true, "versions": true, "where": true, "path": true,
	"images": true, "ps": true, "top": true, "licenses": true, "audit": true,
	"bin": true, "root": true, "prefix": true, "output": true, "whoami": true,

	// "log" is here but "logs" is not, on purpose. `git log` and `hg log` print
	// a record whose middle matters as much as its ends. `docker logs` and
	// `kubectl logs` print application output, which is exactly what toktrim
	// exists to compress -- and the follow forms are caught as long-running
	// before they reach here.
	"log": true,
}

// gitReadOnly are git's read-only subcommands whose names the generic verb list
// does not cover. git is worth spelling out because its output is a record --
// commits, hunks, blame lines -- where dropping the middle is data loss, and
// because almost every one of these was previously left to rtk and so never
// had to be classified here at all. That is how `git log` ended up being
// truncated when toktrim became standalone.
var gitReadOnly = map[string]bool{
	"log": true, "show": true, "diff": true, "status": true, "blame": true,
	"annotate": true, "reflog": true, "shortlog": true, "describe": true,
	"rev-parse": true, "rev-list": true, "ls-files": true, "ls-tree": true,
	"ls-remote": true, "cat-file": true, "show-ref": true, "for-each-ref": true,
	"whatchanged": true, "count-objects": true, "var": true, "grep": true,
	"branch": true, "tag": true, "remote": true, "config": true, "notes": true,
	"stash": true, "worktree": true, "submodule": true, "bisect": true,
	"verify-commit": true, "verify-tag": true, "name-rev": true, "check-ignore": true,
}

// pipelineSinks end a pipeline by selecting or counting, which means the
// author has already decided how much output they want.
var pipelineSinks = map[string]bool{
	"head": true, "tail": true, "grep": true, "wc": true, "jq": true,
	"rg": true, "sort": true, "uniq": true, "cut": true, "column": true,
}

// IsQuerySegment reports whether one command (already split into words) is a
// query whose output must not be truncated.
func IsQuerySegment(words []string) bool {
	if len(words) == 0 {
		return false
	}
	cmd := baseName(words[0])

	// A help or version request is always just the answer.
	for _, w := range words[1:] {
		switch strings.ToLower(w) {
		case "--help", "-h", "--version", "-v", "-V":
			return true
		}
	}

	if queryCommands[cmd] {
		// `tail -f` is a follow, not a query.
		for _, w := range words[1:] {
			if w == "-f" || w == "--follow" || w == "-F" {
				return false
			}
		}
		return true
	}

	switch cmd {
	case "sed":
		// `sed -n ...` prints selected lines; a plain sed transforms a stream.
		for _, w := range words[1:] {
			if w == "-n" || w == "--quiet" || w == "--silent" {
				return true
			}
		}
		return false
	case "git":
		if sub, ok := subcommand(words); ok {
			return gitReadOnly[sub]
		}
		// Bare `git` prints usage.
		return true
	}

	// Everything else: judge the subcommand.
	if sub, ok := subcommand(words); ok {
		return readOnlyVerbs[sub]
	}
	return false
}

// subcommand returns the first non-flag word after the command name.
func subcommand(words []string) (string, bool) {
	for _, w := range words[1:] {
		if strings.HasPrefix(w, "-") {
			continue
		}
		return strings.ToLower(w), true
	}
	return "", false
}

// IsPipelineSink reports whether a command ends a pipeline by selecting or
// counting its input.
func IsPipelineSink(words []string) bool {
	if len(words) == 0 {
		return false
	}
	return pipelineSinks[baseName(words[0])]
}

// IsQueryCommand reports whether argv is a query command.
func IsQueryCommand(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	// Follow into a shell -c script, and treat a pipeline by its last stage.
	for _, seg := range shellSegments(argv) {
		if IsQuerySegment(seg) || IsPipelineSink(seg) {
			return true
		}
	}
	return false
}

// shellSegments returns the command segments argv will run, following into a
// `sh -c` script when there is one.
func shellSegments(argv []string) [][]string {
	for i, a := range argv {
		switch baseName(a) {
		case "bash", "sh", "zsh", "dash":
			if i+2 < len(argv) && argv[i+1] == "-c" {
				var out [][]string
				for _, seg := range splitShellSegments(argv[i+2]) {
					out = append(out, strings.Fields(seg))
				}
				return out
			}
		}
	}
	return [][]string{argv}
}

// splitShellSegments breaks a script on the operators that separate commands.
func splitShellSegments(script string) []string {
	repl := strings.NewReplacer("&&", "\n", "||", "\n", "|", "\n", ";", "\n")
	var out []string
	for _, s := range strings.Split(repl.Replace(script), "\n") {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
