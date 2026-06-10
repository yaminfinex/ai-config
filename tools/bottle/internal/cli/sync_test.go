package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/bottle/internal/store"
)

// hermeticGitCLI blanks the developer's global and system git config for the
// duration of a test (same idiom as store/sync_test.go's hermeticGit), so sync
// behavior cannot be skewed by settings like merge.ff=only. Skips when git is
// absent.
func hermeticGitCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// newBareRemote creates a bare git repo to act as the sync remote.
func newBareRemote(t *testing.T) string {
	t.Helper()
	hermeticGitCLI(t)
	dir := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	return dir
}

// syncStoreAt opens a temp store whose creation clock is pinned to created, so
// collision winners across two stores are deterministic.
func syncStoreAt(t *testing.T, created time.Time) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "bottles"),
		store.WithClock(func() time.Time { return created }),
		store.WithWarnWriter(io.Discard))
	if err != nil {
		t.Fatalf("open sync test store: %v", err)
	}
	return st
}

// TestSyncFirstRunPrintsRemotePrivacyReminderAndSummary: `bottle sync --remote`
// against a fresh bare remote configures origin, pushes, and prints the remote
// line, the privacy reminder, and the summary.
func TestSyncFirstRunPrintsRemotePrivacyReminderAndSummary(t *testing.T) {
	remote := newBareRemote(t)
	st := openTestStore(t)
	makeBottle(t, st, "auth-expert", "", nil)
	d, stdout, stderr := newDeps(t, st)

	if code := cmdSync(d, []string{"--remote", remote}); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Remote configured: "+remote) {
		t.Errorf("output does not echo the configured remote:\n%s", out)
	}
	if !strings.Contains(out, "keep this remote private") {
		t.Errorf("output is missing the privacy reminder:\n%s", out)
	}
	if !strings.Contains(out, "synced: 0 received, 1 sent") {
		t.Errorf("output is missing the summary line:\n%s", out)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
}

// TestSyncSecondRunAlreadyInSync: a bare `bottle sync` right after a sync
// reports "already in sync" with no remote/privacy lines, exit 0.
func TestSyncSecondRunAlreadyInSync(t *testing.T) {
	remote := newBareRemote(t)
	st := openTestStore(t)
	makeBottle(t, st, "auth-expert", "", nil)
	d, stdout, stderr := newDeps(t, st)
	if code := cmdSync(d, []string{"--remote", remote}); code != 0 {
		t.Fatalf("first sync exit code = %d (stderr: %s)", code, stderr.String())
	}
	stdout.Reset()

	if code := cmdSync(d, nil); code != 0 {
		t.Fatalf("second sync exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if got := stdout.String(); got != "already in sync\n" {
		t.Errorf("second sync output = %q, want %q", got, "already in sync\n")
	}
}

// TestSyncNoRemoteConfigured: bare `bottle sync` on a store with no origin
// fails with a single stderr line suggesting --remote.
func TestSyncNoRemoteConfigured(t *testing.T) {
	hermeticGitCLI(t)
	st := openTestStore(t)
	makeBottle(t, st, "auth-expert", "", nil)
	d, _, stderr := newDeps(t, st)

	if code := cmdSync(d, nil); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	msg := strings.TrimRight(stderr.String(), "\n")
	if msg == "" || strings.Contains(msg, "\n") {
		t.Fatalf("stderr is not a single line: %q", stderr.String())
	}
	if !strings.Contains(msg, "--remote") {
		t.Errorf("stderr does not suggest --remote: %s", msg)
	}
}

// TestSyncGitMissingSingleLineError: with git scrubbed off PATH the store
// error surfaces as one stderr line, exit 1.
func TestSyncGitMissingSingleLineError(t *testing.T) {
	hermeticGitCLI(t)
	st := openTestStore(t) // opened while git is still reachable
	d, _, stderr := newDeps(t, st)
	t.Setenv("PATH", t.TempDir())

	if code := cmdSync(d, nil); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	msg := strings.TrimRight(stderr.String(), "\n")
	if msg == "" || strings.Contains(msg, "\n") {
		t.Fatalf("stderr is not a single line: %q", stderr.String())
	}
	if !strings.Contains(msg, "git") {
		t.Errorf("stderr does not name git: %s", msg)
	}
}

// TestSyncRenamesRenderOnePerPair: two stores both holding auth-expert@1
// converge through the remote; the later-created bottle loses the collision
// and the syncing side prints one rename line naming old → new.
func TestSyncRenamesRenderOnePerPair(t *testing.T) {
	remote := newBareRemote(t)
	older := syncStoreAt(t, createdAt)
	newer := syncStoreAt(t, createdAt.Add(time.Hour))
	makeBottle(t, older, "auth-expert", "", nil)
	makeBottle(t, newer, "auth-expert", "", nil)

	dOlder, _, errOlder := newDeps(t, older)
	if code := cmdSync(dOlder, []string{"--remote", remote}); code != 0 {
		t.Fatalf("older store sync exit code = %d (stderr: %s)", code, errOlder.String())
	}

	dNewer, stdout, stderr := newDeps(t, newer)
	if code := cmdSync(dNewer, []string{"--remote", remote}); code != 0 {
		t.Fatalf("newer store sync exit code = %d (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "synced: 1 received, 1 sent") {
		t.Errorf("output is missing the summary line:\n%s", out)
	}
	want := "renamed: auth-expert → auth-expert-2 (name collision)\n"
	if got := strings.Count(out, "renamed:"); got != 1 {
		t.Fatalf("rename lines = %d, want exactly 1:\n%s", got, out)
	}
	if !strings.Contains(out, want) {
		t.Errorf("output missing rename line %q:\n%s", want, out)
	}
}
