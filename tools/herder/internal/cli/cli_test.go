package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestRootUsageListsEverySubcommand(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}, {"help"}} {
		code, stdout, stderr := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("Run(%v) = %d, want 0 (stderr: %q)", args, code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(%v) wrote to stderr: %q", args, stderr)
		}
		for _, cmd := range commands {
			if !strings.Contains(stdout, "  "+cmd.name) {
				t.Errorf("Run(%v) usage missing subcommand %q:\n%s", args, cmd.name, stdout)
			}
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	code, stdout, stderr := runCLI(t, "bogus")
	if code != 2 {
		t.Fatalf("Run(bogus) = %d, want 2", code)
	}
	if stdout != "" {
		t.Fatalf("Run(bogus) wrote to stdout: %q", stdout)
	}
	if !strings.Contains(stderr, `unknown command "bogus"`) {
		t.Fatalf("Run(bogus) stderr = %q, want unknown-command message", stderr)
	}
}

// Until a subcommand's port lands, its stub must fail loudly and name the
// still-authoritative bash script — a shim flip before the port would
// otherwise silently no-op.
func TestStubsFailLoudAndNameBashScript(t *testing.T) {
	for _, cmd := range commands {
		if cmd.name == "send" {
			continue
		}
		code, stdout, stderr := runCLI(t, cmd.name, "--whatever")
		if code != 1 {
			t.Errorf("Run(%s) = %d, want 1", cmd.name, code)
		}
		if stdout != "" {
			t.Errorf("Run(%s) wrote to stdout: %q", cmd.name, stdout)
		}
		want := "skills/herder/scripts/herder-" + cmd.name
		if !strings.Contains(stderr, want) {
			t.Errorf("Run(%s) stderr = %q, want mention of %q", cmd.name, stderr, want)
		}
	}
}
