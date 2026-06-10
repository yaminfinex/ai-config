package cli

import (
	"os/exec"
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/store"
)

func TestListEmptyStoreIsPolite(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newDeps(t, st)

	if code := cmdList(d, nil); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("empty store wrote to stderr: %s", stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "No bottles yet") {
		t.Errorf("empty store output not polite: %q", out)
	}
}

func TestListTable(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "the first one", nil)
	makeBottle(t, st, "beta", "", nil)
	makeBottle(t, st, "beta", "", nil) // beta@2

	d, stdout, stderr := newDeps(t, st)
	if code := cmdList(d, nil); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	out := stdout.String()

	for _, want := range []string{
		"NAME", "LATEST", "VERSIONS", "AGE", "NOTE",
		"alpha", "the first one",
		"beta",
		"2d", // nowAt is two days after createdAt
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}

	// beta is sorted before nothing-after; alpha before beta.
	if i, j := strings.Index(out, "alpha"), strings.Index(out, "beta"); i < 0 || j < 0 || i > j {
		t.Errorf("rows not sorted alpha<beta:\n%s", out)
	}

	// beta has two versions, alpha one — assert beta's row shows latest @2
	// and a version count of 2 (columns: NAME LATEST VERSIONS AGE …).
	var betaLine string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(l, "beta") {
			betaLine = l
		}
	}
	if betaLine == "" {
		t.Fatalf("no beta row in output:\n%s", out)
	}
	fields := strings.Fields(betaLine)
	if len(fields) < 3 || fields[1] != "@2" || fields[2] != "2" {
		t.Errorf("beta row latest/count wrong, got fields %v:\n%s", fields, out)
	}
}

// ---------------------------------------------------------------------------
// Unsynced hint — one quiet line after the table when a remote is configured
// and local refs say there is something to sync; otherwise list is unchanged.
// ---------------------------------------------------------------------------

// listOut runs cmdList and returns stdout, failing on a non-zero exit or any
// stderr — the hint must never make list noisier.
func listOut(t *testing.T, st *store.Store) string {
	t.Helper()
	d, stdout, stderr := newDeps(t, st)
	if code := cmdList(d, nil); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("list wrote to stderr: %s", stderr)
	}
	return stdout.String()
}

// TestListNoRemoteNoHint: without a remote the output carries no hint line —
// byte-identical to a hint-less list (the table is the whole output).
func TestListNoRemoteNoHint(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)
	out := listOut(t, st)
	if strings.Contains(out, "bottle sync") || strings.Contains(out, "·") {
		t.Errorf("remote-less list grew a sync hint:\n%s", out)
	}
}

// TestListHintAhead: one local commit past the last sync renders the singular
// hint as the final line.
func TestListHintAhead(t *testing.T) {
	remote := newBareRemote(t)
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)
	if _, err := st.Sync(remote); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	makeBottle(t, st, "beta", "", nil) // one auto-commit ahead

	out := listOut(t, st)
	if !strings.HasSuffix(out, "· 1 commit unsynced — bottle sync\n") {
		t.Errorf("list missing the singular unsynced hint:\n%s", out)
	}
}

// TestListHintNeverPushed: a configured remote that was never synced counts
// every local commit as unsynced (plural form).
func TestListHintNeverPushed(t *testing.T) {
	hermeticGitCLI(t)
	remote := newBareRemote(t)
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)
	makeBottle(t, st, "beta", "", nil) // two commits, one per create
	cmd := exec.Command("git", "remote", "add", "origin", remote)
	cmd.Dir = st.Root()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}

	out := listOut(t, st)
	if !strings.HasSuffix(out, "· 2 commits unsynced — bottle sync\n") {
		t.Errorf("list missing the never-pushed hint:\n%s", out)
	}
}

// TestListHintBehindAfterFetch: a fetch that moves the remote-tracking ref
// past HEAD renders the to-pull hint.
func TestListHintBehindAfterFetch(t *testing.T) {
	remote := newBareRemote(t)
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)
	if _, err := st.Sync(remote); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	other := syncStoreAt(t, createdAt)
	makeBottle(t, other, "beta", "", nil)
	if _, err := other.Sync(remote); err != nil {
		t.Fatalf("other Sync: %v", err)
	}
	cmd := exec.Command("git", "fetch", "origin")
	cmd.Dir = st.Root()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fetch: %v: %s", err, out)
	}

	out := listOut(t, st)
	if !strings.Contains(out, "to pull — bottle sync") {
		t.Errorf("list missing the behind hint:\n%s", out)
	}
}

// TestListHintSyncedSilent: fully synced → table only, no hint.
func TestListHintSyncedSilent(t *testing.T) {
	remote := newBareRemote(t)
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)
	if _, err := st.Sync(remote); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	out := listOut(t, st)
	if strings.Contains(out, "bottle sync") {
		t.Errorf("synced list still hints:\n%s", out)
	}
}

// TestListHintGitAbsent: remote configured, then git vanishes from PATH —
// list stays exactly as remote-less, with no warning.
func TestListHintGitAbsent(t *testing.T) {
	remote := newBareRemote(t)
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)
	if _, err := st.Sync(remote); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	makeBottle(t, st, "beta", "", nil) // would hint if git were present

	t.Setenv("PATH", t.TempDir())
	out := listOut(t, st)
	if strings.Contains(out, "bottle sync") {
		t.Errorf("git-less list grew a sync hint:\n%s", out)
	}
}

// TestSyncHintWording: the renderer's three phrasings, including both-sides.
func TestSyncHintWording(t *testing.T) {
	for _, tc := range []struct {
		ahead, behind int
		ok            bool
		want          string
	}{
		{0, 0, false, ""},
		{3, 0, false, ""}, // not-ok always wins
		{0, 0, true, ""},
		{1, 0, true, "· 1 commit unsynced — bottle sync"},
		{3, 0, true, "· 3 commits unsynced — bottle sync"},
		{0, 1, true, "· 1 commit to pull — bottle sync"},
		{0, 2, true, "· 2 commits to pull — bottle sync"},
		{1, 2, true, "· 1 commit unsynced, 2 commits to pull — bottle sync"},
	} {
		if got := syncHint(tc.ahead, tc.behind, tc.ok); got != tc.want {
			t.Errorf("syncHint(%d, %d, %v) = %q, want %q", tc.ahead, tc.behind, tc.ok, got, tc.want)
		}
	}
}
