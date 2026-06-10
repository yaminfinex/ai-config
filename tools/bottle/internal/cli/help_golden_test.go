package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGolden regenerates the testdata/golden/*.txt fixtures from the live
// help output. Run `go test ./internal/cli/ -run Golden -update` after an
// intentional help change, then review the diff.
var updateGolden = flag.Bool("update", false, "update help golden files")

const goldenDir = "testdata/golden"

// rootLineBudget is the hard ceiling on the no-arg skill help: it must stay a
// glanceable skill, not a manual. The spec targets "well under 60 lines".
const rootLineBudget = 60

// assertGolden compares got against testdata/golden/<name>, or rewrites the
// fixture under -update. The golden file IS the contract for the help surface.
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
		t.Errorf("%s help drifted from golden — rerun with -update if intentional.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// TestRootHelpGolden pins the no-arg `bottle` skill help byte-exact.
func TestRootHelpGolden(t *testing.T) {
	assertGolden(t, "root.txt", RootHelp())
}

// TestSubcommandHelpGolden pins every `bottle <verb> --help` surface byte-exact,
// driven through the real Run() dispatch so the goldens reflect what an agent
// actually sees.
func TestSubcommandHelpGolden(t *testing.T) {
	for _, verb := range speccedVerbs {
		t.Run(verb, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := Run([]string{verb, "--help"}, &out, &errOut)
			if code != 0 {
				t.Fatalf("%s --help exit = %d, want 0 (stderr: %s)", verb, code, errOut.String())
			}
			if errOut.Len() != 0 {
				t.Errorf("%s --help wrote to stderr: %s", verb, errOut.String())
			}
			assertGolden(t, verb+".txt", out.String())
		})
	}
}

// TestRootHelpUnderLineBudget keeps the no-arg skill help glanceable.
func TestRootHelpUnderLineBudget(t *testing.T) {
	n := strings.Count(RootHelp(), "\n")
	if n >= rootLineBudget {
		t.Errorf("RootHelp() is %d lines, must stay under %d", n, rootLineBudget)
	}
}

// TestRootHelpTableMatchesRegistry asserts the help/wiring cannot drift: every
// command in the no-arg table exists in the registry, and every registered
// command appears in the table. The table is generated from `commands`, so this
// is belt-and-suspenders over that generation.
func TestRootHelpTableMatchesRegistry(t *testing.T) {
	help := RootHelp()
	registered := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		registered[cmd.name] = true
		if !strings.Contains(help, "\n  "+cmd.name) {
			t.Errorf("registered command %q missing from the no-arg help table", cmd.name)
		}
	}
	// Parse the table region (between "Commands:\n" and the trailing blank line)
	// and confirm no name there is absent from the registry.
	_, table, ok := strings.Cut(help, "Commands:\n")
	if !ok {
		t.Fatal("no Commands: section in RootHelp()")
	}
	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, "  ") {
			break // end of the indented table
		}
		name := strings.Fields(line)[0]
		if !registered[name] {
			t.Errorf("help table names %q, which is not a registered command", name)
		}
	}
}
