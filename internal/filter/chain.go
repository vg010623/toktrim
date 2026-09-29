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
	KindGeneric     Kind = "generic"
)

// Classify works out which family a command belongs to.
//
// argv is the command as it will be run. When toktrim wraps a shell invocation
// (`bash -c "npm ci && npm test"`) the script itself is inspected, because that
// is where the interesting command name is.
func Classify(argv []string) Kind {
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

	switch Classify(argv) {
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
	}

	d := NewDedup()
	d.MinRun = cfg.MinRun
	add("dedup", d)
	add("truncate", NewTruncate(cfg.HeadLines, cfg.TailLines))
	return chain
}
