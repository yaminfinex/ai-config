package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/bottle/internal/transcript"
)

// fixture returns the path to a read-only testdata fixture.
func fixture(name string) string {
	return filepath.Join("..", "..", "..", "testdata", name)
}

// contains reports whether argv holds v.
func contains(argv []string, v string) bool {
	for _, a := range argv {
		if a == v {
			return true
		}
	}
	return false
}

// flagValue returns the argument following the first occurrence of flag.
func flagValue(argv []string, flag string) (string, bool) {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1], true
		}
	}
	return "", false
}

func TestEncodeProjectDir(t *testing.T) {
	// Expected values are the actual on-disk names observed under
	// ~/.claude/projects/ on this machine (non-alphanumeric byte → '-', runs
	// preserved one-for-one, case preserved).
	cases := []struct{ cwd, want string }{
		{"/home/ubuntu", "-home-ubuntu"},
		{"/home/ubuntu/Coding/ai-config", "-home-ubuntu-Coding-ai-config"},
		{
			"/home/ubuntu/.herdr/worktrees/ai-config/feat-bottle-cli",
			"-home-ubuntu--herdr-worktrees-ai-config-feat-bottle-cli",
		},
		{"plain", "plain"},
		{"a.b_c", "a-b-c"},
	}
	for _, c := range cases {
		if got := EncodeProjectDir(c.cwd); got != c.want {
			t.Errorf("EncodeProjectDir(%q) = %q, want %q", c.cwd, got, c.want)
		}
	}
}

func TestEncodeProjectDirNeverEmptyCollapse(t *testing.T) {
	// "/.herdr" must produce a double dash, not a single one — the scheme does
	// not collapse runs (that is what makes the observed names match).
	if got := EncodeProjectDir("/.herdr"); got != "--herdr" {
		t.Errorf("EncodeProjectDir(%q) = %q, want %q", "/.herdr", got, "--herdr")
	}
}

// writeFixtureCopy copies a read-only fixture's bytes into dir as name and
// stamps its mtime, returning the full path. Used to build temp project dirs.
func writeFixtureCopy(t *testing.T, dir, name, fixtureName string, mod time.Time) string {
	t.Helper()
	data, err := os.ReadFile(fixture(fixtureName))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write copy: %v", err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return path
}

func TestLastSessionNewestWinsWithPreview(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/project"
	dir := ProjectDir(root, cwd)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	older := time.Date(2026, 6, 10, 1, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 10, 2, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 6, 10, 3, 0, 0, 0, time.UTC)

	writeFixtureCopy(t, dir, "11111111-1111-4111-8111-111111111111.jsonl", "plain.jsonl", older)
	winner := "22222222-2222-4222-8222-222222222222"
	writeFixtureCopy(t, dir, winner+".jsonl", "plain.jsonl", newer)
	// A newer agent-* subagent transcript must be ignored.
	writeFixtureCopy(t, dir, "agent-33333333.jsonl", "plain.jsonl", newest)

	got, err := LastSession(root, cwd)
	if err != nil {
		t.Fatalf("LastSession: %v", err)
	}
	if got.SessionID != winner {
		t.Errorf("SessionID = %q, want %q (agent-* must be excluded)", got.SessionID, winner)
	}
	wantPreview := "Remember the codeword ALPHA. Reply with just OK."
	if got.FirstUserText != wantPreview {
		t.Errorf("FirstUserText = %q, want %q", got.FirstUserText, wantPreview)
	}
	if !got.ModTime.Equal(newer) {
		t.Errorf("ModTime = %v, want %v", got.ModTime, newer)
	}
	if age := got.Age(newer.Add(90 * time.Minute)); age != 90*time.Minute {
		t.Errorf("Age = %v, want 90m", age)
	}
}

func TestFindSessionPath(t *testing.T) {
	root := t.TempDir()
	when := time.Date(2026, 6, 10, 1, 0, 0, 0, time.UTC)

	// The session was launched at the workspace root and filed under its dir.
	rootCwd := "/work/project"
	rootDir := ProjectDir(root, rootCwd)
	if err := os.MkdirAll(rootDir, 0o700); err != nil {
		t.Fatal(err)
	}
	id := "22222222-2222-4222-8222-222222222222"
	writeFixtureCopy(t, rootDir, id+".jsonl", "plain.jsonl", when)

	// Found when the cwd matches (prefer-cwd branch).
	got, err := FindSessionPath(root, rootCwd, id)
	if err != nil {
		t.Fatalf("FindSessionPath(root cwd): %v", err)
	}
	if got != filepath.Join(rootDir, id+".jsonl") {
		t.Errorf("path = %q, want under %q", got, rootDir)
	}

	// Found even when invoked from a subdirectory whose encoded dir differs and
	// holds no session file — the cross-dir search locates it by id.
	subCwd := "/work/project/tools/bottle"
	if d := ProjectDir(root, subCwd); d == rootDir {
		t.Fatalf("test setup: subdir encodes to the same project dir as root")
	}
	got, err = FindSessionPath(root, subCwd, id)
	if err != nil {
		t.Fatalf("FindSessionPath(subdir cwd): %v", err)
	}
	if got != filepath.Join(rootDir, id+".jsonl") {
		t.Errorf("subdir path = %q, want under %q", got, rootDir)
	}

	// Absent id is ErrNoSessions, not a misleading cwd-relative path.
	if _, err := FindSessionPath(root, subCwd, "00000000-0000-4000-8000-000000000000"); err != ErrNoSessions {
		t.Errorf("missing id: err = %v, want ErrNoSessions", err)
	}
}

func TestLastSessionNoSessions(t *testing.T) {
	root := t.TempDir()
	// Absent project dir.
	if _, err := LastSession(root, "/no/such/cwd"); err != ErrNoSessions {
		t.Errorf("absent dir: err = %v, want ErrNoSessions", err)
	}
	// Present dir with only an agent-* file.
	cwd := "/work/empty"
	dir := ProjectDir(root, cwd)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureCopy(t, dir, "agent-deadbeef.jsonl", "plain.jsonl", time.Now())
	if _, err := LastSession(root, cwd); err != ErrNoSessions {
		t.Errorf("agent-only dir: err = %v, want ErrNoSessions", err)
	}
}

func TestSelfSessionID(t *testing.T) {
	t.Setenv(SessionEnvVar, "")
	if got := SelfSessionID(); got != "" {
		t.Errorf("unset: SelfSessionID() = %q, want empty", got)
	}
	t.Setenv(SessionEnvVar, "abc-123")
	if got := SelfSessionID(); got != "abc-123" {
		t.Errorf("set: SelfSessionID() = %q, want abc-123", got)
	}
}

func TestMaterializeWritesFreshSeed(t *testing.T) {
	root := t.TempDir()
	cwd := "/home/ubuntu/proj"
	res, err := Materialize(MaterializeRequest{
		SourcePath:   fixture("plain.jsonl"),
		ProjectsRoot: root,
		Cwd:          cwd,
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	wantDir := ProjectDir(root, cwd)
	if res.ProjectDir != wantDir {
		t.Errorf("ProjectDir = %q, want %q", res.ProjectDir, wantDir)
	}
	if res.SeedPath != filepath.Join(wantDir, res.SessionID+".jsonl") {
		t.Errorf("SeedPath = %q, unexpected for id %q in %q", res.SeedPath, res.SessionID, wantDir)
	}
	if res.CompactBoundaries != 0 {
		t.Errorf("CompactBoundaries = %d, want 0 for plain.jsonl", res.CompactBoundaries)
	}

	// Seed exists, parses, lints clean, and carries the fresh id on every line.
	info, err := transcript.IndexFile(res.SeedPath)
	if err != nil {
		t.Fatalf("seed does not parse: %v", err)
	}
	rewritten := 0
	for _, e := range info.Entries {
		if e.SessionID == "" {
			continue
		}
		if e.SessionID != res.SessionID {
			t.Fatalf("line %d sessionId = %q, want fresh id %q", e.Line, e.SessionID, res.SessionID)
		}
		rewritten++
	}
	if rewritten == 0 {
		t.Fatal("no sessionId fields were rewritten")
	}

	// The seed id differs from the fixture's fixed fake id.
	if res.SessionID == "00000000-0000-4000-8000-000000000001" {
		t.Error("session id was not freshened")
	}
}

func TestMaterializeCreatesProjectDir0700(t *testing.T) {
	root := t.TempDir()
	cwd := "/fresh/cwd"
	res, err := Materialize(MaterializeRequest{
		SourcePath:   fixture("plain.jsonl"),
		ProjectsRoot: root,
		Cwd:          cwd,
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	fi, err := os.Stat(res.ProjectDir)
	if err != nil {
		t.Fatalf("stat project dir: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Errorf("project dir mode = %o, want 0700", perm)
	}
}

func TestMaterializeReportsCompaction(t *testing.T) {
	root := t.TempDir()
	res, err := Materialize(MaterializeRequest{
		SourcePath:   fixture("multi-compact.jsonl"),
		ProjectsRoot: root,
		Cwd:          "/work/compacted",
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if res.CompactBoundaries != 2 {
		t.Errorf("CompactBoundaries = %d, want 2 for multi-compact.jsonl", res.CompactBoundaries)
	}
}

func TestMaterializeGCdSourceErrorsBeforeWrite(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/gone"
	missing := filepath.Join(t.TempDir(), "vanished.jsonl")

	_, err := Materialize(MaterializeRequest{
		SourcePath:   missing,
		ProjectsRoot: root,
		Cwd:          cwd,
	})
	if err == nil {
		t.Fatal("expected error for GC'd source, got nil")
	}

	// Nothing must have been written — not even the encoded project dir.
	if _, statErr := os.Stat(ProjectDir(root, cwd)); !os.IsNotExist(statErr) {
		t.Errorf("project dir created despite source error: %v", statErr)
	}
}

func TestBuildLaunchInteractive(t *testing.T) {
	cwd := t.TempDir()
	plan, err := BuildLaunch(LaunchRequest{SessionID: "sid-1", Cwd: cwd})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if plan.Pane {
		t.Error("interactive plan should not be a pane")
	}
	if plan.RunCwd != cwd {
		t.Errorf("RunCwd = %q, want %q", plan.RunCwd, cwd)
	}
	want := []string{"claude", "--resume", "sid-1"}
	if strings.Join(plan.Argv, " ") != strings.Join(want, " ") {
		t.Errorf("Argv = %v, want %v", plan.Argv, want)
	}
}

func TestBuildLaunchInteractiveYoloAndPrompt(t *testing.T) {
	cwd := t.TempDir()
	plan, err := BuildLaunch(LaunchRequest{
		SessionID: "sid-1",
		Cwd:       cwd,
		Yolo:      true,
		Prompt:    "pick up where we left off",
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if !contains(plan.Argv, "--dangerously-skip-permissions") {
		t.Errorf("yolo interactive missing skip flag: %v", plan.Argv)
	}
	if plan.Argv[len(plan.Argv)-1] != "pick up where we left off" {
		t.Errorf("prompt not last arg: %v", plan.Argv)
	}
}

func TestBuildLaunchPaneDefaultSafe(t *testing.T) {
	cwd := t.TempDir()
	plan, err := BuildLaunch(LaunchRequest{
		SessionID: "sid-2",
		Cwd:       cwd,
		Pane:      PaneRight,
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if !plan.Pane {
		t.Error("pane plan should report Pane=true")
	}
	if plan.Argv[0] != "herder-spawn" {
		t.Errorf("Argv[0] = %q, want herder-spawn", plan.Argv[0])
	}
	if split, _ := flagValue(plan.Argv, "--split"); split != "right" {
		t.Errorf("--split = %q, want right", split)
	}
	if c, _ := flagValue(plan.Argv, "--cwd"); c != cwd {
		t.Errorf("--cwd = %q, want %q", c, cwd)
	}
	if !contains(plan.Argv, "--safe") {
		t.Errorf("default pane must pass --safe: %v", plan.Argv)
	}
	if contains(plan.Argv, "--dangerously-skip-permissions") {
		t.Errorf("default pane must not skip permissions: %v", plan.Argv)
	}
	// claude flags ride through --extra-arg.
	if !contains(plan.Argv, "--extra-arg") || !contains(plan.Argv, "--resume") || !contains(plan.Argv, "sid-2") {
		t.Errorf("pane argv missing resume wiring: %v", plan.Argv)
	}
}

func TestBuildLaunchPaneBelowMapsToDown(t *testing.T) {
	cwd := t.TempDir()
	plan, err := BuildLaunch(LaunchRequest{SessionID: "sid", Cwd: cwd, Pane: PaneBelow})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if split, _ := flagValue(plan.Argv, "--split"); split != "down" {
		t.Errorf("--split = %q, want down (below→down)", split)
	}
}

func TestBuildLaunchPaneYolo(t *testing.T) {
	cwd := t.TempDir()
	plan, err := BuildLaunch(LaunchRequest{
		SessionID: "sid-3",
		Cwd:       cwd,
		Pane:      PaneRight,
		Yolo:      true,
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if contains(plan.Argv, "--safe") {
		t.Errorf("yolo pane must not pass --safe: %v", plan.Argv)
	}
	if !contains(plan.Argv, "--dangerously-skip-permissions") {
		t.Errorf("yolo pane must pass --dangerously-skip-permissions: %v", plan.Argv)
	}
}

func TestBuildLaunchInteractiveRestoresPermissionMode(t *testing.T) {
	cwd := t.TempDir()
	plan, err := BuildLaunch(LaunchRequest{
		SessionID:      "sid-pm",
		Cwd:            cwd,
		PermissionMode: "acceptEdits",
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	want := []string{"claude", "--resume", "sid-pm", "--permission-mode", "acceptEdits"}
	if strings.Join(plan.Argv, " ") != strings.Join(want, " ") {
		t.Errorf("Argv = %v, want %v", plan.Argv, want)
	}
}

func TestBuildLaunchDefaultModeStaysImplicit(t *testing.T) {
	cwd := t.TempDir()
	// A recorded "default" mode is the launch default already — it must not add
	// a --permission-mode flag, and on the pane path it must keep --safe.
	interactive, err := BuildLaunch(LaunchRequest{SessionID: "sid", Cwd: cwd, PermissionMode: "default"})
	if err != nil {
		t.Fatalf("BuildLaunch interactive: %v", err)
	}
	if contains(interactive.Argv, "--permission-mode") {
		t.Errorf("default mode must not emit --permission-mode: %v", interactive.Argv)
	}
	pane, err := BuildLaunch(LaunchRequest{SessionID: "sid", Cwd: cwd, Pane: PaneRight, PermissionMode: "default"})
	if err != nil {
		t.Fatalf("BuildLaunch pane: %v", err)
	}
	if !contains(pane.Argv, "--safe") {
		t.Errorf("default-mode pane must keep --safe: %v", pane.Argv)
	}
}

func TestBuildLaunchYoloOverridesPermissionMode(t *testing.T) {
	cwd := t.TempDir()
	plan, err := BuildLaunch(LaunchRequest{
		SessionID:      "sid",
		Cwd:            cwd,
		Yolo:           true,
		PermissionMode: "acceptEdits",
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if contains(plan.Argv, "--permission-mode") {
		t.Errorf("yolo must win over restored mode (no --permission-mode): %v", plan.Argv)
	}
	if !contains(plan.Argv, "--dangerously-skip-permissions") {
		t.Errorf("yolo must skip permissions: %v", plan.Argv)
	}
}

func TestBuildLaunchPaneRestoresPermissionMode(t *testing.T) {
	cwd := t.TempDir()
	plan, err := BuildLaunch(LaunchRequest{
		SessionID:      "sid-pm",
		Cwd:            cwd,
		Pane:           PaneRight,
		PermissionMode: "plan",
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	// The mode rides through as --extra-arg, which herder-spawn recognises and
	// so suppresses its autonomous default — making --safe redundant.
	if contains(plan.Argv, "--safe") {
		t.Errorf("restored-mode pane must not pass --safe: %v", plan.Argv)
	}
	joined := strings.Join(plan.Argv, " ")
	if !strings.Contains(joined, "--extra-arg --permission-mode --extra-arg plan") {
		t.Errorf("pane argv missing restored mode wiring: %v", plan.Argv)
	}
}

func TestBuildLaunchRefusesMissingCwd(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	_, err := BuildLaunch(LaunchRequest{SessionID: "sid", Cwd: missing})
	if err == nil {
		t.Fatal("expected refusal for missing cwd")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error must name the path %q: %v", missing, err)
	}
}

func TestBuildLaunchRejectsBadPane(t *testing.T) {
	cwd := t.TempDir()
	if _, err := BuildLaunch(LaunchRequest{SessionID: "sid", Cwd: cwd, Pane: "sideways"}); err == nil {
		t.Error("expected error for unknown pane mode")
	}
}

func TestBuildLaunchRequiresSessionID(t *testing.T) {
	cwd := t.TempDir()
	if _, err := BuildLaunch(LaunchRequest{Cwd: cwd}); err == nil {
		t.Error("expected error for empty session id")
	}
}
