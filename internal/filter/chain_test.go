package filter

import (
	"strings"
	"testing"

	"github.com/vg010623/toktrim/internal/config"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		argv []string
		want Kind
	}{
		{[]string{"npm", "install"}, KindNodeInstall},
		{[]string{"npm", "ci"}, KindNodeInstall},
		{[]string{"npm", "i", "--save-dev", "jest"}, KindNodeInstall},
		{[]string{"pnpm", "add", "lodash"}, KindNodeInstall},
		{[]string{"yarn"}, KindNodeInstall},
		{[]string{"npm", "run", "build"}, KindNodeScript},
		{[]string{"npm", "test"}, KindNodeScript},
		{[]string{"pnpm", "run", "lint"}, KindNodeScript},
		{[]string{"docker", "build", "-t", "app", "."}, KindDocker},
		{[]string{"dotnet", "build"}, KindDotNet},
		{[]string{"dotnet", "test"}, KindDotNet},
		{[]string{"make", "-j8"}, KindGeneric},
		// Wrapped in a shell, as the hook does it.
		{[]string{"bash", "-c", "npm ci && npm run build"}, KindNodeInstall},
		{[]string{"bash", "-c", "docker build ."}, KindDocker},
		{[]string{"bash", "-c", "cd app && dotnet test"}, KindDotNet},
		// Windows shims.
		{[]string{"npm.cmd", "install"}, KindNodeInstall},
		{[]string{"C:\\Program Files\\nodejs\\npm.cmd", "run", "build"}, KindNodeScript},
	}
	for _, tt := range tests {
		if got := Classify(tt.argv); got != tt.want {
			t.Errorf("Classify(%v) = %s, want %s", tt.argv, got, tt.want)
		}
	}
}

// A chain that can drop lines must always have a Guard in front of it.
func TestChainAlwaysGuards(t *testing.T) {
	cfg := config.Default()
	for _, argv := range [][]string{
		{"npm", "install"}, {"npm", "test"}, {"docker", "build", "."},
		{"dotnet", "build"}, {"make"}, {"anything-at-all"},
	} {
		chain := Chain(argv, cfg)
		guardAt := -1
		for i, f := range chain {
			if f.Name() == "guard" {
				guardAt = i
			}
		}
		if guardAt < 0 {
			t.Errorf("Chain(%v) has no guard", argv)
			continue
		}
		// Only the ANSI filter may precede it.
		for i := 0; i < guardAt; i++ {
			if chain[i].Name() != "ansi" {
				t.Errorf("Chain(%v): %q runs before the guard", argv, chain[i].Name())
			}
		}
	}
}

func TestChainRespectsConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Filters = map[string]bool{"dedup": false, "truncate": false}
	var names []string
	for _, f := range Chain([]string{"make"}, cfg) {
		names = append(names, f.Name())
	}
	got := strings.Join(names, ",")
	if strings.Contains(got, "dedup") || strings.Contains(got, "truncate") {
		t.Errorf("disabled filters are still in the chain: %s", got)
	}
	if !strings.Contains(got, "guard") {
		t.Errorf("the guard must survive any config: %s", got)
	}
}

func TestChainDisabledEntirely(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = false
	if chain := Chain([]string{"npm", "install"}, cfg); len(chain) != 0 {
		t.Errorf("a disabled config should yield no filters, got %d", len(chain))
	}
}
