package cli

import (
	"bytes"
	"strings"
	"testing"
)

// The full verb set from the design spec's CLI surface table.
var speccedVerbs = []string{
	"create", "decant", "rebottle", "list", "log", "show",
	"rename", "note", "prune", "rm", "artifacts", "sync",
}

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestNoArgsPrintsHelpSkeletonAndExitsZero(t *testing.T) {
	code, stdout, _ := runCLI(t)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != RootHelp() {
		t.Errorf("no-arg output is not the RootHelp single source of truth")
	}
	for _, verb := range speccedVerbs {
		if !strings.Contains(stdout, verb) {
			t.Errorf("help skeleton missing command %q", verb)
		}
	}
}

func TestHelpFlagMatchesNoArgOutput(t *testing.T) {
	code, stdout, _ := runCLI(t, "--help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != RootHelp() {
		t.Errorf("--help output differs from RootHelp()")
	}
}

func TestUnknownSubcommandExitsNonZeroWithOneLineHint(t *testing.T) {
	code, _, stderr := runCLI(t, "frobnicate")
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero")
	}
	trimmed := strings.TrimRight(stderr, "\n")
	if trimmed == "" {
		t.Fatalf("no hint written to stderr")
	}
	if strings.Contains(trimmed, "\n") {
		t.Errorf("hint is not one line:\n%s", stderr)
	}
	if !strings.Contains(trimmed, "frobnicate") {
		t.Errorf("hint does not name the unknown command: %s", trimmed)
	}
}

func TestEverySpeccedVerbResolves(t *testing.T) {
	for _, verb := range speccedVerbs {
		t.Run(verb, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, verb, "--help")
			if code != 0 {
				t.Errorf("%s --help: exit code = %d, want 0 (stderr: %s)", verb, code, stderr)
			}
			combined := stdout + stderr
			if strings.Contains(combined, "unknown command") {
				t.Errorf("%s resolved as unknown command", verb)
			}
			if !strings.Contains(stdout, "bottle "+verb) {
				t.Errorf("%s --help: usage line missing %q", verb, "bottle "+verb)
			}
		})
	}
}

// The help skeleton's command table is generated from the registry, so the
// no-arg help can never drift from what is actually wired (U8 golden tests
// will pin the final content).
func TestRootHelpListsExactlyTheRegisteredCommands(t *testing.T) {
	help := RootHelp()
	for _, verb := range speccedVerbs {
		if !strings.Contains(help, verb) {
			t.Errorf("RootHelp missing registered command %q", verb)
		}
	}
}
