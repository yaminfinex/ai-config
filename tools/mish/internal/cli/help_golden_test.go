package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update help golden files")

const (
	goldenDir      = "testdata/golden"
	rootLineBudget = 60
)

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if *updateGolden {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", name, err)
	}
	if got != string(want) {
		t.Errorf("%s help drifted from golden; rerun with -update only after editing the spec-derived help.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestRootHelpGolden(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--help"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("mish --help exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("mish --help wrote stderr:\n%s", stderr.String())
	}
	assertGolden(t, "root.txt", stdout.String())
}

func TestVerbHelpGolden(t *testing.T) {
	for _, verb := range []string{"new", "backlog", "status"} {
		t.Run(verb, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{verb, "--help"}, &stdout, &stderr)
			if code != exitOK {
				t.Fatalf("mish %s --help exit = %d, want 0; stderr=%s", verb, code, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("mish %s --help wrote stderr:\n%s", verb, stderr.String())
			}
			assertGolden(t, verb+".txt", stdout.String())
		})
	}
}

func TestRootHelpUnderLineBudget(t *testing.T) {
	lines := strings.Count(rootHelp(), "\n")
	if lines >= rootLineBudget {
		t.Fatalf("root help is %d lines, must stay under %d", lines, rootLineBudget)
	}
}

func TestRootHelpVerbTableMatchesRegisteredCommands(t *testing.T) {
	root := newRoot(testDeps())
	var registered []string
	for _, cmd := range root.Commands() {
		if cmd.Hidden || cmd.Name() == "help" {
			continue
		}
		registered = append(registered, cmd.Name())
	}
	slices.Sort(registered)
	want := rootHelpVerbNames()
	if !slices.Equal(registered, want) {
		t.Fatalf("root help verbs = %v, registered commands = %v", want, registered)
	}

	help := rootHelp()
	for _, name := range registered {
		if !strings.Contains(help, "\n  "+name+" ") {
			t.Fatalf("root help verb table missing %q:\n%s", name, help)
		}
	}
}

func TestBacklogHelpAdvertisesOnlyAllowedSubcommands(t *testing.T) {
	help := backlogHelp()
	if err := validateBacklogHelpAllowlist(); err != nil {
		t.Fatal(err)
	}
	allowedSection := sectionBetween(t, help, "Allowed subcommands:\n", "\nExcluded rationale:")
	for _, name := range []string{"init", "config", "agents", "browser", "completion", "instructions", "mcp"} {
		if strings.Contains(allowedSection, "\n  "+name+" ") {
			t.Fatalf("excluded subcommand %q appears as available:\n%s", name, allowedSection)
		}
		if !strings.Contains(help, "\n  "+name+" ") {
			t.Fatalf("excluded subcommand %q missing rationale:\n%s", name, help)
		}
	}
}

func TestHelpCarriesDoctrineVocabulary(t *testing.T) {
	combined := rootHelp() + newHelpText + backlogHelp() + statusHelpText
	for _, want := range []string{
		"mission(<slug>): <verb> <summary>",
		"Closeout prose",
		"rename custody commit",
		"markers never nest",
		"Pull before",
		"<repo>@<sha>",
		"replaces the whole references list",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("help missing doctrine phrase %q", want)
		}
	}
}

func sectionBetween(t *testing.T, text, start, end string) string {
	t.Helper()
	_, tail, ok := strings.Cut(text, start)
	if !ok {
		t.Fatalf("missing section start %q", start)
	}
	section, _, ok := strings.Cut(tail, end)
	if !ok {
		t.Fatalf("missing section end %q", end)
	}
	return section
}
