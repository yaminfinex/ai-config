package cli

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRootSurfaceHasExactlyThreeSubcommands(t *testing.T) {
	root := newRoot(testDeps())
	got := make([]string, 0, len(root.Commands()))
	for _, cmd := range root.Commands() {
		if cmd.Hidden {
			continue
		}
		got = append(got, cmd.Name())
	}
	want := []string{"backlog", "new", "status"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("registered commands = %v, want %v", got, want)
	}
}

func TestUnknownSubcommandExitsUsageAndNamesMishHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"wat"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("Run unknown exit = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unknown command wrote stdout:\n%s", stdout.String())
	}
	err := stderr.String()
	for _, want := range []string{"unknown command", "wat", "mish", "mish --help"} {
		if !strings.Contains(err, want) {
			t.Fatalf("unknown command stderr missing %q:\n%s", want, err)
		}
	}
}

func TestBareMishAndHelpExitOK(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}} {
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("Run(%v) exit = %d, want %d; stderr=%s", args, code, exitOK, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("Run(%v) wrote stderr:\n%s", args, stderr.String())
		}
		out := stdout.String()
		for _, want := range []string{"mish", "new", "backlog", "status"} {
			if !strings.Contains(out, want) {
				t.Fatalf("Run(%v) help output missing %q:\n%s", args, want, out)
			}
		}
	}
}

func TestDepsResolveMissionsRepoOnce(t *testing.T) {
	t.Setenv("MISSIONS_REPO", "/tmp/one")
	d := newDeps(io.Discard, io.Discard)
	t.Setenv("MISSIONS_REPO", "/tmp/two")
	if d.missionsRepo != "/tmp/one" {
		t.Fatalf("missionsRepo = %q, want initial env value", d.missionsRepo)
	}
}

func testDeps() deps {
	return deps{
		env:          func(string) string { return "" },
		cwd:          func() (string, error) { return "", nil },
		exec:         func(string, []string, string, io.Reader, io.Writer, io.Writer) execResult { return execResult{} },
		git:          func([]string, string) ([]byte, error) { return nil, nil },
		clock:        func() time.Time { return time.Unix(0, 0) },
		stdout:       io.Discard,
		stderr:       io.Discard,
		missionsRepo: "",
	}
}
