package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBacklogDeniedSubcommandsNameAllowlist(t *testing.T) {
	for _, subcommand := range []string{"init", "config", "agents", "browser", "future"} {
		t.Run(subcommand, func(t *testing.T) {
			h := newBacklogHarness(t)
			h.writeMission("alpha", true)

			code := h.run("backlog", "--mission", "alpha", subcommand)
			if code != exitRefuse {
				t.Fatalf("exit = %d, want %d; stderr=%s", code, exitRefuse, h.stderr.String())
			}
			if h.lookedPath || len(h.execs) != 0 {
				t.Fatalf("denied subcommand reached lookup=%v execs=%d", h.lookedPath, len(h.execs))
			}
			err := h.stderr.String()
			for _, want := range []string{subcommand, "not allowed", "task", "tasks", "cleanup"} {
				if !strings.Contains(err, want) {
					t.Fatalf("stderr missing %q:\n%s", want, err)
				}
			}
		})
	}
}

func TestBacklogAllowedSubcommandsExecWithPinnedCWDAndVerbatimArgs(t *testing.T) {
	for _, subcommand := range sortedBacklogAllowlistForTests() {
		t.Run(subcommand, func(t *testing.T) {
			h := newBacklogHarness(t)
			missionDir := h.writeMission("alpha", true)

			code := h.run("backlog", "--mission", "alpha", subcommand, "show", "--help")
			if code != exitOK {
				t.Fatalf("exit = %d, want %d; stderr=%s", code, exitOK, h.stderr.String())
			}
			call := h.singleExec(t)
			if call.name != backlogBinary {
				t.Fatalf("exec name = %q, want %q", call.name, backlogBinary)
			}
			if call.dir != missionDir {
				t.Fatalf("exec dir = %q, want %q", call.dir, missionDir)
			}
			wantArgs := []string{subcommand, "show", "--help"}
			if !slices.Equal(call.args, wantArgs) {
				t.Fatalf("exec args = %v, want %v", call.args, wantArgs)
			}
		})
	}
}

func TestBacklogMissingBoardRefusesBeforeLookupOrExec(t *testing.T) {
	h := newBacklogHarness(t)
	h.writeMission("alpha", false)

	code := h.run("backlog", "--mission", "alpha", "task", "list")
	if code != exitRefuse {
		t.Fatalf("exit = %d, want %d", code, exitRefuse)
	}
	if h.lookedPath || len(h.execs) != 0 {
		t.Fatalf("missing board reached lookup=%v execs=%d", h.lookedPath, len(h.execs))
	}
	err := h.stderr.String()
	for _, want := range []string{"board missing", "scaffold damaged or wrong mission"} {
		if !strings.Contains(err, want) {
			t.Fatalf("stderr missing %q:\n%s", want, err)
		}
	}
}

func TestBacklogPassthroughExitCodesAreReturnedVerbatim(t *testing.T) {
	for _, exitCode := range []int{0, 1, 7} {
		t.Run(string(rune('0'+exitCode)), func(t *testing.T) {
			h := newBacklogHarness(t)
			h.writeMission("alpha", true)
			h.execExitCode = exitCode

			code := h.run("backlog", "--mission", "alpha", "task", "list")
			if code != exitCode {
				t.Fatalf("exit = %d, want passthrough %d; stderr=%s", code, exitCode, h.stderr.String())
			}
			h.singleExec(t)
		})
	}
}

func TestBacklogFlagShapedTailArgsForwardUnmodified(t *testing.T) {
	h := newBacklogHarness(t)
	h.writeMission("alpha", true)

	code := h.run("backlog", "--mission", "alpha", "task", "edit", "TASK-1", "--ref", "repo@abc123", "-s", "In Progress")
	if code != exitOK {
		t.Fatalf("exit = %d, want %d; stderr=%s", code, exitOK, h.stderr.String())
	}
	call := h.singleExec(t)
	want := []string{"task", "edit", "TASK-1", "--ref", "repo@abc123", "-s", "In Progress"}
	if !slices.Equal(call.args, want) {
		t.Fatalf("exec args = %v, want %v", call.args, want)
	}
}

func TestBacklogMissionFlagOnlyBeforeSubcommand(t *testing.T) {
	h := newBacklogHarness(t)
	h.writeMission("alpha", true)
	markerDir := h.writeWorkMarker("alpha")

	code := h.runFrom(markerDir, "backlog", "--mission", "alpha", "task", "list")
	if code != exitOK {
		t.Fatalf("pre-subcommand --mission exit = %d; stderr=%s", code, h.stderr.String())
	}
	call := h.singleExec(t)
	if !slices.Equal(call.args, []string{"task", "list"}) {
		t.Fatalf("pre-subcommand args = %v", call.args)
	}

	h.resetOutputAndExecs()
	code = h.runFrom(markerDir, "backlog", "task", "--mission", "beta")
	if code != exitOK {
		t.Fatalf("post-subcommand --mission exit = %d; stderr=%s", code, h.stderr.String())
	}
	call = h.singleExec(t)
	if !slices.Equal(call.args, []string{"task", "--mission", "beta"}) {
		t.Fatalf("post-subcommand args = %v", call.args)
	}
}

func TestBacklogBareAndLeadingHelpPrintWrapperAllowlistSummary(t *testing.T) {
	for _, args := range [][]string{
		{"backlog"},
		{"backlog", "help"},
		{"backlog", "-h"},
		{"backlog", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newBacklogHarness(t)
			code := h.run(args...)
			if code != exitOK {
				t.Fatalf("exit = %d, want %d; stderr=%s", code, exitOK, h.stderr.String())
			}
			if h.lookedPath || len(h.execs) != 0 {
				t.Fatalf("help reached lookup=%v execs=%d", h.lookedPath, len(h.execs))
			}
			out := h.stdout.String()
			for _, want := range []string{"Allowed subcommands", "task", "cleanup", "Excluded", "init", "browser"} {
				if !strings.Contains(out, want) {
					t.Fatalf("help missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestBacklogMissingBinaryRefusesWithInstallHint(t *testing.T) {
	h := newBacklogHarness(t)
	h.writeMission("alpha", true)
	h.lookPathErr = errors.New("not found")

	code := h.run("backlog", "--mission", "alpha", "task", "list")
	if code != exitRefuse {
		t.Fatalf("exit = %d, want %d", code, exitRefuse)
	}
	if len(h.execs) != 0 {
		t.Fatalf("missing binary reached exec: %#v", h.execs)
	}
	err := h.stderr.String()
	for _, want := range []string{"Backlog.md CLI not found", "npm:backlog.md", "backlog"} {
		if !strings.Contains(err, want) {
			t.Fatalf("stderr missing %q:\n%s", want, err)
		}
	}
}

type backlogHarness struct {
	t            *testing.T
	repo         string
	cwd          string
	stdin        *strings.Reader
	stdout       bytes.Buffer
	stderr       bytes.Buffer
	lookedPath   bool
	lookPathErr  error
	execExitCode int
	execs        []execCall
}

type execCall struct {
	name  string
	args  []string
	dir   string
	stdin io.Reader
}

func newBacklogHarness(t *testing.T) *backlogHarness {
	t.Helper()
	root := t.TempDir()
	h := &backlogHarness{
		t:     t,
		repo:  filepath.Join(root, "repo"),
		cwd:   filepath.Join(root, "work"),
		stdin: strings.NewReader("input"),
	}
	if err := os.MkdirAll(filepath.Join(h.repo, "missions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *backlogHarness) deps() deps {
	return deps{
		env: func(string) string {
			return ""
		},
		cwd: func() (string, error) {
			return h.cwd, nil
		},
		lookPath: func(name string) (string, error) {
			h.lookedPath = true
			if h.lookPathErr != nil {
				return "", h.lookPathErr
			}
			return filepath.Join("/bin", name), nil
		},
		exec: func(name string, args []string, dir string, stdin io.Reader, stdout, stderr io.Writer) execResult {
			h.execs = append(h.execs, execCall{
				name:  name,
				args:  slices.Clone(args),
				dir:   dir,
				stdin: stdin,
			})
			return execResult{ExitCode: h.execExitCode}
		},
		git: func([]string, string) ([]byte, error) {
			return nil, nil
		},
		clock: func() time.Time {
			return time.Unix(0, 0)
		},
		stdin:        h.stdin,
		stdout:       &h.stdout,
		stderr:       &h.stderr,
		missionsRepo: h.repo,
	}
}

func (h *backlogHarness) run(args ...string) int {
	h.t.Helper()
	return runWithDeps(args, h.deps())
}

func (h *backlogHarness) runFrom(cwd string, args ...string) int {
	h.t.Helper()
	h.cwd = cwd
	return h.run(args...)
}

func (h *backlogHarness) writeMission(slug string, withBoard bool) string {
	h.t.Helper()
	missionDir := filepath.Join(h.repo, "missions", slug)
	if err := os.MkdirAll(missionDir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missionDir, "mission.md"), []byte("mission: "+slug+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
	if withBoard {
		boardDir := filepath.Join(missionDir, "backlog")
		if err := os.MkdirAll(boardDir, 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(boardDir, "config.yml"), []byte("project_name: "+slug+"\n"), 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
	return missionDir
}

func (h *backlogHarness) writeWorkMarker(slug string) string {
	h.t.Helper()
	markerDir := filepath.Join(h.cwd, "sub")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, ".mission"), []byte(slug+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return markerDir
}

func (h *backlogHarness) singleExec(t *testing.T) execCall {
	t.Helper()
	if len(h.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1: %#v", len(h.execs), h.execs)
	}
	call := h.execs[0]
	if call.stdin != h.stdin {
		t.Fatalf("exec stdin = %#v, want harness stdin", call.stdin)
	}
	return call
}

func (h *backlogHarness) resetOutputAndExecs() {
	h.stdout.Reset()
	h.stderr.Reset()
	h.execs = nil
	h.lookedPath = false
}

func sortedBacklogAllowlistForTests() []string {
	names := backlogAllowlistNames()
	slices.Sort(names)
	return names
}
