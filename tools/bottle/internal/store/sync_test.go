package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-config/tools/bottle/internal/refs"
)

var syncEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// syncBottle builds a projection-input fixture: id, name@version, a created
// offset from syncEpoch, and an optional parent bottle id.
func syncBottle(id, name string, version int, createdOffset time.Duration, parentID string) Bottle {
	m := Meta{Name: name, Version: version, Created: syncEpoch.Add(createdOffset)}
	if parentID != "" {
		m.Parent = &Parent{BottleID: parentID, DecantSessionID: "decant-" + parentID}
	}
	return Bottle{ID: id, Meta: m}
}

// syncFixtures is every projection scenario, shared with the determinism
// test so the convergence property is asserted across all of them.
var syncFixtures = map[string][]Bottle{
	"empty": nil,
	"disjoint": {
		syncBottle("aaaa0001", "alpha", 1, 0, ""),
		syncBottle("bbbb0001", "beta", 1, time.Minute, ""),
	},
	"already-synced": {
		syncBottle("id000001", "auth-expert", 1, 0, ""),
		syncBottle("id000002", "auth-expert", 2, time.Hour, "id000001"),
	},
	"independent-create": {
		syncBottle("aaaa1111", "auth-expert", 1, 0, ""),
		syncBottle("bbbb1111", "auth-expert", 1, time.Hour, ""),
	},
	"divergent-rebottle": {
		syncBottle("shared01", "auth-expert", 1, 0, ""),
		syncBottle("shared02", "auth-expert", 2, time.Hour, "shared01"),
		syncBottle("aaaa3333", "auth-expert", 3, 2*time.Hour, "shared02"),
		syncBottle("bbbb3333", "auth-expert", 3, 3*time.Hour, "shared02"),
		syncBottle("bbbb4444", "auth-expert", 4, 4*time.Hour, "bbbb3333"),
	},
	"created-tie": {
		syncBottle("aaaa1111", "auth-expert", 1, 0, ""),
		syncBottle("bbbb1111", "auth-expert", 1, 0, ""),
	},
	"suffix-exhaustion": {
		syncBottle("aaaa1111", "auth-expert", 1, 0, ""),
		syncBottle("bbbb1111", "auth-expert", 1, time.Hour, ""),
		syncBottle("cccc1111", "auth-expert-2", 1, 0, ""),
	},
	"rm-vs-rebottle": {
		syncBottle("shared01", "auth-expert", 1, 0, ""),
		syncBottle("aaaa3333", "auth-expert", 3, 2*time.Hour, "deadbeef"),
	},
	"stacked-both-sides": {
		syncBottle("aaaa3333", "auth-expert", 3, 0, ""),
		syncBottle("aaaa4444", "auth-expert", 4, time.Hour, "aaaa3333"),
		syncBottle("bbbb3333", "auth-expert", 3, 2*time.Hour, ""),
		syncBottle("bbbb4444", "auth-expert", 4, 3*time.Hour, "bbbb3333"),
	},
}

func mustProject(t *testing.T, bottles []Bottle) (map[string][]versionEntry, []SyncRename) {
	t.Helper()
	names, renames, err := projectNames(bottles)
	if err != nil {
		t.Fatalf("projectNames: %v", err)
	}
	return names, renames
}

func TestProjectDisjointUnion(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["disjoint"])
	want := map[string][]versionEntry{
		"alpha": {{Version: 1, BottleID: "aaaa0001"}},
		"beta":  {{Version: 1, BottleID: "bbbb0001"}},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if len(renames) != 0 {
		t.Errorf("renames = %v, want none", renames)
	}
}

func TestProjectAlreadySynced(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["already-synced"])
	want := map[string][]versionEntry{
		"auth-expert": {{Version: 1, BottleID: "id000001"}, {Version: 2, BottleID: "id000002"}},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if len(renames) != 0 {
		t.Errorf("renames = %v, want none", renames)
	}
}

func TestProjectIndependentCreateCollision(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["independent-create"])
	want := map[string][]versionEntry{
		"auth-expert":   {{Version: 1, BottleID: "aaaa1111"}},
		"auth-expert-2": {{Version: 1, BottleID: "bbbb1111"}},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	wantRenames := []SyncRename{{BottleID: "bbbb1111", OldName: "auth-expert", NewName: "auth-expert-2"}}
	if !reflect.DeepEqual(renames, wantRenames) {
		t.Errorf("renames = %v, want %v", renames, wantRenames)
	}
}

func TestProjectDivergentRebottleCollision(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["divergent-rebottle"])
	want := map[string][]versionEntry{
		"auth-expert": {
			{Version: 1, BottleID: "shared01"},
			{Version: 2, BottleID: "shared02"},
			{Version: 3, BottleID: "aaaa3333"},
		},
		"auth-expert-2": {
			{Version: 3, BottleID: "bbbb3333"},
			{Version: 4, BottleID: "bbbb4444"},
		},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	wantRenames := []SyncRename{
		{BottleID: "bbbb3333", OldName: "auth-expert", NewName: "auth-expert-2"},
		{BottleID: "bbbb4444", OldName: "auth-expert", NewName: "auth-expert-2"},
	}
	if !reflect.DeepEqual(renames, wantRenames) {
		t.Errorf("renames = %v, want %v", renames, wantRenames)
	}
}

func TestProjectCreatedTieSmallerIDWins(t *testing.T) {
	names, _ := mustProject(t, syncFixtures["created-tie"])
	if got := names["auth-expert"]; len(got) != 1 || got[0].BottleID != "aaaa1111" {
		t.Errorf("auth-expert = %v, want kept by smaller id aaaa1111", got)
	}
	if got := names["auth-expert-2"]; len(got) != 1 || got[0].BottleID != "bbbb1111" {
		t.Errorf("auth-expert-2 = %v, want loser bbbb1111", got)
	}
}

func TestProjectSuffixExhaustion(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["suffix-exhaustion"])
	if got := names["auth-expert-3"]; len(got) != 1 || got[0].BottleID != "bbbb1111" {
		t.Errorf("auth-expert-3 = %v, want loser bbbb1111 (auth-expert-2 is taken)", got)
	}
	if got := names["auth-expert-2"]; len(got) != 1 || got[0].BottleID != "cccc1111" {
		t.Errorf("auth-expert-2 = %v, want the pre-existing cccc1111 untouched", got)
	}
	wantRenames := []SyncRename{{BottleID: "bbbb1111", OldName: "auth-expert", NewName: "auth-expert-3"}}
	if !reflect.DeepEqual(renames, wantRenames) {
		t.Errorf("renames = %v, want %v", renames, wantRenames)
	}
}

func TestProjectDanglingParent(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["rm-vs-rebottle"])
	want := map[string][]versionEntry{
		"auth-expert": {{Version: 1, BottleID: "shared01"}, {Version: 3, BottleID: "aaaa3333"}},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v (dangling parent must not drop the child)", names, want)
	}
	if len(renames) != 0 {
		t.Errorf("renames = %v, want none", renames)
	}
}

// TestProjectStackedBothSides: a collision at @3 whose resolution moves the
// loser's stacked @4 too, dissolving the @4 collision in the same pass.
func TestProjectStackedBothSides(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["stacked-both-sides"])
	want := map[string][]versionEntry{
		"auth-expert": {
			{Version: 3, BottleID: "aaaa3333"},
			{Version: 4, BottleID: "aaaa4444"},
		},
		"auth-expert-2": {
			{Version: 3, BottleID: "bbbb3333"},
			{Version: 4, BottleID: "bbbb4444"},
		},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	wantRenames := []SyncRename{
		{BottleID: "bbbb3333", OldName: "auth-expert", NewName: "auth-expert-2"},
		{BottleID: "bbbb4444", OldName: "auth-expert", NewName: "auth-expert-2"},
	}
	if !reflect.DeepEqual(renames, wantRenames) {
		t.Errorf("renames = %v, want %v", renames, wantRenames)
	}
}

func TestProjectEmpty(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["empty"])
	if len(names) != 0 {
		t.Errorf("names = %v, want empty", names)
	}
	if len(renames) != 0 {
		t.Errorf("renames = %v, want none", renames)
	}
}

// TestProjectDeterminism: project(A ∪ B) == project(B ∪ A), byte-identical
// after JSON marshal, across every fixture.
func TestProjectDeterminism(t *testing.T) {
	for label, bottles := range syncFixtures {
		t.Run(label, func(t *testing.T) {
			forward := append([]Bottle(nil), bottles...)
			reversed := make([]Bottle, len(bottles))
			for i, b := range bottles {
				reversed[len(bottles)-1-i] = b
			}
			fNames, fRenames := mustProject(t, forward)
			rNames, rRenames := mustProject(t, reversed)

			fJSON, err := json.Marshal(fNames)
			if err != nil {
				t.Fatal(err)
			}
			rJSON, err := json.Marshal(rNames)
			if err != nil {
				t.Fatal(err)
			}
			if string(fJSON) != string(rJSON) {
				t.Errorf("projection depends on input order:\nforward:  %s\nreversed: %s", fJSON, rJSON)
			}
			if !reflect.DeepEqual(fRenames, rRenames) {
				t.Errorf("renames depend on input order:\nforward:  %v\nreversed: %v", fRenames, rRenames)
			}
		})
	}
}

// TestScanProjectApply exercises the store-side path: a real bottle plus a
// hand-planted colliding bottle dir (as a git merge union would leave), then
// scan → project → apply, asserting the rewritten meta and on-disk registry.
func TestScanProjectApply(t *testing.T) {
	s, _ := newStore(t)
	winner, err := s.Create(mkCreate("auth-expert", "sess-1"))
	if err != nil {
		t.Fatal(err)
	}

	// Plant a colliding auth-expert@1 with a newer Created and an id that
	// sorts after the real one regardless of its random value.
	loserID := "zzzzzzzz"
	loserMeta := Meta{
		Name:    "auth-expert",
		Version: 1,
		Created: winner.Meta.Created.Add(time.Hour),
		Source:  Source{SessionID: "sess-2", Harness: "claude", CWD: "/tmp/proj"},
		Parent:  &Parent{BottleID: winner.ID, DecantSessionID: "decant-x"},
	}
	if err := s.writeMeta(loserID, loserMeta); err != nil {
		t.Fatal(err)
	}
	if err := s.backend.Write(bottlePath(loserID, "transcript.jsonl"), []byte("{}\n")); err != nil {
		t.Fatal(err)
	}

	bottles, err := s.scanMetas()
	if err != nil {
		t.Fatalf("scanMetas: %v", err)
	}
	if len(bottles) != 2 {
		t.Fatalf("scanMetas found %d bottles, want 2", len(bottles))
	}
	names, renames, err := projectNames(bottles)
	if err != nil {
		t.Fatalf("projectNames: %v", err)
	}

	unlock, err := s.lockExclusive()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := s.readRegistry()
	if err != nil {
		unlock()
		t.Fatal(err)
	}
	err = s.applyProjection(reg, names, renames)
	unlock()
	if err != nil {
		t.Fatalf("applyProjection: %v", err)
	}

	// Loser meta rewritten via the rename idiom; parent pointer untouched.
	loser, err := s.Get(loserID)
	if err != nil {
		t.Fatal(err)
	}
	if loser.Meta.Name != "auth-expert-2" {
		t.Errorf("loser name = %q, want auth-expert-2", loser.Meta.Name)
	}
	if len(loser.Meta.PreviousNames) != 1 || loser.Meta.PreviousNames[0] != "auth-expert" {
		t.Errorf("loser previous names = %v, want [auth-expert]", loser.Meta.PreviousNames)
	}
	if loser.Meta.Parent == nil || loser.Meta.Parent.BottleID != winner.ID {
		t.Errorf("loser parent = %+v, want untouched pointer to %s", loser.Meta.Parent, winner.ID)
	}

	// Winner meta untouched.
	w, err := s.Get(winner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Meta.Name != "auth-expert" || len(w.Meta.PreviousNames) != 0 {
		t.Errorf("winner meta changed: name=%q previous=%v", w.Meta.Name, w.Meta.PreviousNames)
	}

	// On-disk registry reflects the projection.
	raw, err := os.ReadFile(filepath.Join(s.Root(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Names map[string][]struct {
			Version  int    `json:"version"`
			BottleID string `json:"bottle_id"`
		} `json:"names"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if e := got.Names["auth-expert"]; len(e) != 1 || e[0].BottleID != winner.ID || e[0].Version != 1 {
		t.Errorf("registry auth-expert = %+v, want [{1 %s}]", e, winner.ID)
	}
	if e := got.Names["auth-expert-2"]; len(e) != 1 || e[0].BottleID != loserID || e[0].Version != 1 {
		t.Errorf("registry auth-expert-2 = %+v, want [{1 %s}]", e, loserID)
	}
}

// ---------------------------------------------------------------------------
// Store.Sync integration tests: two temp stores converging through a bare
// remote. Everything below requires git on PATH except the scrubbed-PATH and
// substrate-disabled error tests.
// ---------------------------------------------------------------------------

// hermeticGit blanks the developer's global and system git config for the
// duration of a test, so sync behavior cannot be skewed by settings like
// merge.ff=only, fetch.prune, or a hooks path. Also skips when git is absent.
func hermeticGit(t *testing.T) {
	t.Helper()
	requireGit(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// newBareRemote creates a bare git repo to act as the sync remote.
func newBareRemote(t *testing.T) string {
	t.Helper()
	hermeticGit(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "remote.git")
	if out, err := gitOut(t, parent, "init", "--bare", "-q", dir); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	return dir
}

// storeWithClock opens a store whose Create timestamps are pinned, so
// collision winners are deterministic across the two stores.
func storeWithClock(t *testing.T, fixed time.Time) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "bottles"),
		WithWarnWriter(&bytes.Buffer{}), WithClock(func() time.Time { return fixed }))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func registryBytes(t *testing.T, s *Store) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.Root(), "registry.json"))
	if err != nil {
		t.Fatalf("read registry.json: %v", err)
	}
	return raw
}

// assertClean fails when the store worktree has uncommitted or unmerged
// state — every sync outcome, success or failure, must leave it clean.
func assertClean(t *testing.T, root string) {
	t.Helper()
	status, err := gitOut(t, root, "status", "--porcelain")
	if err != nil {
		t.Fatalf("git status: %v: %s", err, status)
	}
	if status != "" {
		t.Errorf("worktree dirty:\n%s", status)
	}
}

func mustSync(t *testing.T, s *Store, remote string) SyncReport {
	t.Helper()
	rep, err := s.Sync(remote)
	if err != nil {
		t.Fatalf("Sync(%q): %v", remote, err)
	}
	return rep
}

func mustCreate(t *testing.T, s *Store, name, session string) *Bottle {
	t.Helper()
	b, err := s.Create(mkCreate(name, session))
	if err != nil {
		t.Fatalf("Create(%s): %v", name, err)
	}
	return b
}

func TestSyncPushThenReceive(t *testing.T) {
	remote := newBareRemote(t)
	a, _ := newStore(t)
	mustCreate(t, a, "alpha", "sa1")
	mustCreate(t, a, "beta", "sa2")

	repA := mustSync(t, a, remote)
	if repA.Sent != 2 || repA.Received != 0 || len(repA.Renames) != 0 {
		t.Errorf("A report = %+v, want 2 sent, 0 received, no renames", repA)
	}
	if !repA.RemoteConfigured || repA.RemoteURL != remote {
		t.Errorf("A report remote = (%q, configured=%v), want (%q, true)", repA.RemoteURL, repA.RemoteConfigured, remote)
	}

	b, _ := newStore(t)
	repB := mustSync(t, b, remote)
	if repB.Received != 2 || repB.Sent != 0 {
		t.Errorf("B report = %+v, want 2 received, 0 sent", repB)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := b.Resolve(refs.Ref{Name: name}); err != nil {
			t.Errorf("B cannot resolve %s after sync: %v", name, err)
		}
	}
	if !bytes.Equal(registryBytes(t, a), registryBytes(t, b)) {
		t.Errorf("registries differ after sync:\nA: %s\nB: %s", registryBytes(t, a), registryBytes(t, b))
	}
	assertClean(t, a.Root())
	assertClean(t, b.Root())
}

// TestSyncConvergesCollisions: both stores independently create
// auth-expert@1 plus a non-colliding bottle; after A→B→A the registries are
// byte-identical and the younger bottle moved to auth-expert-2.
func TestSyncConvergesCollisions(t *testing.T) {
	remote := newBareRemote(t)
	a := storeWithClock(t, syncEpoch)
	b := storeWithClock(t, syncEpoch.Add(time.Hour)) // younger Created loses
	aWin := mustCreate(t, a, "auth-expert", "sa1")
	mustCreate(t, a, "alpha", "sa2")
	bLose := mustCreate(t, b, "auth-expert", "sb1")
	mustCreate(t, b, "beta", "sb2")

	mustSync(t, a, remote)
	repB := mustSync(t, b, remote) // the merge happens here
	if len(repB.Renames) != 1 || repB.Renames[0].OldName != "auth-expert" || repB.Renames[0].NewName != "auth-expert-2" {
		t.Fatalf("B renames = %+v, want auth-expert → auth-expert-2", repB.Renames)
	}
	repA2 := mustSync(t, a, remote)
	if repA2.Received != 2 {
		t.Errorf("A second sync received = %d, want 2 (B's two bottles)", repA2.Received)
	}

	if !bytes.Equal(registryBytes(t, a), registryBytes(t, b)) {
		t.Errorf("registries did not converge:\nA: %s\nB: %s", registryBytes(t, a), registryBytes(t, b))
	}
	for _, s := range []*Store{a, b} {
		win, err := s.Resolve(refs.Ref{Name: "auth-expert"})
		if err != nil || win.ID != aWin.ID {
			t.Errorf("auth-expert = %v (err=%v), want winner %s", win, err, aWin.ID)
		}
		lose, err := s.Resolve(refs.Ref{Name: "auth-expert-2"})
		if err != nil || lose.ID != bLose.ID {
			t.Errorf("auth-expert-2 = %v (err=%v), want loser %s", lose, err, bLose.ID)
		}
	}
	// Loser meta rewritten via the rename idiom on both stores.
	loser, err := b.Get(bLose.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loser.Meta.Name != "auth-expert-2" || len(loser.Meta.PreviousNames) != 1 || loser.Meta.PreviousNames[0] != "auth-expert" {
		t.Errorf("loser meta = name %q previous %v, want auth-expert-2 / [auth-expert]", loser.Meta.Name, loser.Meta.PreviousNames)
	}
	assertClean(t, a.Root())
	assertClean(t, b.Root())
}

// TestSyncRebottlePropagates: a new version on A reaches B.
func TestSyncRebottlePropagates(t *testing.T) {
	remote := newBareRemote(t)
	a, _ := newStore(t)
	b, _ := newStore(t)
	mustCreate(t, a, "x", "s1")
	mustSync(t, a, remote)
	mustSync(t, b, remote)

	v2 := mustCreate(t, a, "x", "s2") // version bump, as rebottle does
	mustSync(t, a, "")
	mustSync(t, b, "")
	got, err := b.Resolve(refs.Ref{Name: "x", Version: 2})
	if err != nil || got.ID != v2.ID {
		t.Errorf("B x@2 = %v (err=%v), want %s", got, err, v2.ID)
	}
	if !bytes.Equal(registryBytes(t, a), registryBytes(t, b)) {
		t.Error("registries differ after rebottle sync")
	}
}

// TestSyncRmPropagates: rm on A merges into B as a deletion; regeneration
// drops the registry entry and the bottle dir is gone.
func TestSyncRmPropagates(t *testing.T) {
	remote := newBareRemote(t)
	a, _ := newStore(t)
	b, _ := newStore(t)
	doomed := mustCreate(t, a, "x", "s1")
	mustCreate(t, a, "y", "s2")
	mustSync(t, a, remote)
	mustSync(t, b, remote)

	if err := a.Remove(refs.Ref{Name: "x"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	mustSync(t, a, "")
	mustSync(t, b, "")

	if _, err := b.Resolve(refs.Ref{Name: "x"}); err == nil {
		t.Error("B still resolves the removed name after sync")
	}
	if _, err := os.Stat(filepath.Join(b.Root(), "store", doomed.ID)); !os.IsNotExist(err) {
		t.Errorf("removed bottle dir survived on B (err=%v)", err)
	}
	if _, err := b.Resolve(refs.Ref{Name: "y"}); err != nil {
		t.Errorf("B lost the surviving bottle: %v", err)
	}
	assertClean(t, b.Root())
}

func TestSyncGitAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s, _ := newStore(t)
	_, err := s.Sync("/tmp/anywhere.git")
	if err == nil || !strings.Contains(err.Error(), "git") {
		t.Fatalf("Sync without git = %v, want error mentioning git", err)
	}
	if _, statErr := os.Stat(filepath.Join(s.Root(), ".git")); !os.IsNotExist(statErr) {
		t.Error("sync touched the store: a .git appeared although git is absent")
	}
}

func TestSyncSubstrateDisabled(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bottles")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"git_auto_commit": false}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root, WithWarnWriter(&bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Sync("/tmp/anywhere.git")
	if err == nil || !strings.Contains(err.Error(), "git_auto_commit") {
		t.Fatalf("Sync with substrate disabled = %v, want error naming git_auto_commit", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(statErr) {
		t.Error("sync created a .git although the substrate is disabled")
	}
}

func TestSyncNoRemoteConfigured(t *testing.T) {
	hermeticGit(t)
	s, _ := newStore(t)
	mustCreate(t, s, "x", "s1")
	_, err := s.Sync("")
	if err == nil || !strings.Contains(err.Error(), "--remote") {
		t.Fatalf("Sync without a remote = %v, want error suggesting --remote", err)
	}
}

func TestSyncUnreachableRemote(t *testing.T) {
	hermeticGit(t)
	s, _ := newStore(t)
	mustCreate(t, s, "x", "s1")
	before := registryBytes(t, s)

	_, err := s.Sync(filepath.Join(t.TempDir(), "missing.git"))
	if err == nil {
		t.Fatal("Sync against a missing remote: expected error")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("error is not single-line: %q", err)
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Errorf("error %q does not mention the failed fetch", err)
	}
	if !bytes.Equal(before, registryBytes(t, s)) {
		t.Error("registry changed by a failed sync")
	}
	assertClean(t, s.Root())
}

// TestSyncConflictAborts: the same bottle id carrying different transcript
// content on both machines aborts the merge, names the id, and leaves the
// store untouched.
func TestSyncConflictAborts(t *testing.T) {
	remote := newBareRemote(t)
	a, _ := newStore(t)
	b, _ := newStore(t)
	shared := mustCreate(t, a, "x", "s1")
	mustSync(t, a, remote)
	mustSync(t, b, remote)

	// Hand-craft divergent content for the same id on both sides; each
	// store's next sync sweeps its version into a commit.
	transcript := filepath.Join("store", shared.ID, "transcript.jsonl")
	if err := os.WriteFile(filepath.Join(a.Root(), transcript), []byte("ours\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSync(t, a, "")
	if err := os.WriteFile(filepath.Join(b.Root(), transcript), []byte("theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := registryBytes(t, b)
	_, err := b.Sync("")
	if err == nil || !strings.Contains(err.Error(), shared.ID) {
		t.Fatalf("conflicting sync = %v, want error naming bottle %s", err, shared.ID)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("error is not single-line: %q", err)
	}
	assertClean(t, b.Root())
	if !bytes.Equal(before, registryBytes(t, b)) {
		t.Error("registry changed by an aborted sync")
	}
	got, err := b.ReadTranscript(shared.ID)
	if err != nil || string(got) != "theirs\n" {
		t.Errorf("B transcript after abort = %q (err=%v), want its own content back", got, err)
	}
}

// TestSyncInterruptedPushSelfHeals: a push rejected after the merge commit
// leaves a committed merge; the next sync pushes it without re-merging.
func TestSyncInterruptedPushSelfHeals(t *testing.T) {
	remote := newBareRemote(t)
	a, _ := newStore(t)
	b, _ := newStore(t)
	mustCreate(t, a, "from-a", "s1")
	mustSync(t, a, remote)
	mustCreate(t, b, "from-b", "s2")

	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n[ -f reject ] && { echo rejected by test hook >&2; exit 1; }\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	reject := filepath.Join(remote, "reject")
	if err := os.WriteFile(reject, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := b.Sync(remote)
	if err == nil || !strings.Contains(err.Error(), "push") {
		t.Fatalf("sync with rejected push = %v, want push error", err)
	}
	assertClean(t, b.Root()) // merge committed, only the push failed
	merges, _ := gitOut(t, b.Root(), "rev-list", "--count", "--merges", "HEAD")
	if merges != "1" {
		t.Fatalf("merge commits after interrupted sync = %s, want 1", merges)
	}

	if err := os.Remove(reject); err != nil {
		t.Fatal(err)
	}
	rep := mustSync(t, b, "")
	if rep.Received != 0 || rep.Sent != 1 {
		t.Errorf("self-heal report = %+v, want 0 received, 1 sent", rep)
	}
	if merges, _ := gitOut(t, b.Root(), "rev-list", "--count", "--merges", "HEAD"); merges != "1" {
		t.Errorf("self-heal re-merged: %s merge commits, want still 1", merges)
	}
	remoteHead, err := gitOut(t, remote, "rev-parse", "main")
	if err != nil {
		t.Fatalf("remote rev-parse: %v: %s", err, remoteHead)
	}
	localHead, _ := gitOut(t, b.Root(), "rev-parse", "HEAD")
	if remoteHead != localHead {
		t.Errorf("remote head %s != local head %s after self-heal", remoteHead, localHead)
	}
	// A receives B's bottle through the healed remote.
	mustSync(t, a, "")
	if _, err := a.Resolve(refs.Ref{Name: "from-b"}); err != nil {
		t.Errorf("A cannot resolve from-b after heal: %v", err)
	}
}

// TestSyncDecantsUnion: merged registries carry both sides' decants; an
// overlapping session id keeps the merging side's (ours-wins).
func TestSyncDecantsUnion(t *testing.T) {
	remote := newBareRemote(t)
	a, _ := newStore(t)
	b, _ := newStore(t)
	aB := mustCreate(t, a, "one", "s1")
	bB := mustCreate(t, b, "two", "s2")
	for sess, id := range map[string]string{"shared-sess": aB.ID, "only-a": aB.ID} {
		if err := a.RecordDecant(sess, id); err != nil {
			t.Fatal(err)
		}
	}
	for sess, id := range map[string]string{"shared-sess": bB.ID, "only-b": bB.ID} {
		if err := b.RecordDecant(sess, id); err != nil {
			t.Fatal(err)
		}
	}

	mustSync(t, a, remote)
	mustSync(t, b, remote) // merge on B: B is "ours"

	decants, err := b.Decants()
	if err != nil {
		t.Fatal(err)
	}
	if len(decants) != 3 {
		t.Fatalf("merged decants = %v, want 3 entries", decants)
	}
	if got := decants["only-a"].BottleID; got != aB.ID {
		t.Errorf("only-a → %s, want %s", got, aB.ID)
	}
	if got := decants["only-b"].BottleID; got != bB.ID {
		t.Errorf("only-b → %s, want %s", got, bB.ID)
	}
	if got := decants["shared-sess"].BottleID; got != bB.ID {
		t.Errorf("shared-sess → %s, want ours-wins %s", got, bB.ID)
	}
}

// TestSyncRemoteReplacement: re-running --remote with a new URL replaces
// origin and syncs against it.
func TestSyncRemoteReplacement(t *testing.T) {
	first := newBareRemote(t)
	second := newBareRemote(t)
	s, _ := newStore(t)
	mustCreate(t, s, "x", "s1")
	mustSync(t, s, first)

	rep := mustSync(t, s, second)
	if !rep.RemoteConfigured || rep.RemoteURL != second {
		t.Errorf("report remote = (%q, configured=%v), want (%q, true)", rep.RemoteURL, rep.RemoteConfigured, second)
	}
	// The fetch must prune the first remote's stale tracking refs, or the
	// already-merged fast path counts against the old remote's tree.
	if rep.Sent != 1 || rep.Received != 0 {
		t.Errorf("report = %+v, want 1 sent, 0 received against the fresh remote", rep)
	}
	url, err := gitOut(t, s.Root(), "remote", "get-url", "origin")
	if err != nil || url != second {
		t.Errorf("origin = %q (err=%v), want %q", url, err, second)
	}
	if head, err := gitOut(t, second, "rev-parse", "main"); err != nil {
		t.Errorf("replacement remote has no main branch: %v: %s", err, head)
	}
}

// TestSyncSweepsDirtyState: an untracked file in the store root is committed
// by the sweep before the merge, leaving the worktree clean.
func TestSyncSweepsDirtyState(t *testing.T) {
	remote := newBareRemote(t)
	s, _ := newStore(t)
	mustCreate(t, s, "x", "s1")
	mustSync(t, s, remote)

	if err := os.WriteFile(filepath.Join(s.Root(), "stray-note.txt"), []byte("untracked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSync(t, s, "")
	assertClean(t, s.Root())
	tracked, err := gitOut(t, s.Root(), "ls-files", "stray-note.txt")
	if err != nil || tracked != "stray-note.txt" {
		t.Errorf("stray file not swept into a commit: %q (err=%v)", tracked, err)
	}
}

// ---------------------------------------------------------------------------
// SyncStatus — local-ref ahead/behind for the list hint. Always silent and
// network-free; ok=false collapses every failure to "no hint".
// ---------------------------------------------------------------------------

func wantStatus(t *testing.T, s *Store, ahead, behind int, ok bool) {
	t.Helper()
	a, b, o := s.SyncStatus()
	if a != ahead || b != behind || o != ok {
		t.Errorf("SyncStatus() = (%d, %d, %v), want (%d, %d, %v)", a, b, o, ahead, behind, ok)
	}
}

// TestSyncStatusNoRemote: a store with commits but no origin reads not-ok —
// list must stay byte-identical without a remote.
func TestSyncStatusNoRemote(t *testing.T) {
	hermeticGit(t)
	s, _ := newStore(t)
	mustCreate(t, s, "x", "s1")
	wantStatus(t, s, 0, 0, false)
}

// TestSyncStatusNeverPushed: origin configured but never fetched or pushed —
// no remote-tracking ref — counts every local commit as ahead.
func TestSyncStatusNeverPushed(t *testing.T) {
	hermeticGit(t)
	remote := newBareRemote(t)
	s, _ := newStore(t)
	mustCreate(t, s, "x", "s1")
	mustCreate(t, s, "y", "s2") // two commits, one per create
	if out, err := gitOut(t, s.Root(), "remote", "add", "origin", remote); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}
	wantStatus(t, s, 2, 0, true)
}

// TestSyncStatusSyncedThenAhead: right after a sync the store reads (0,0,ok);
// one more local mutation reads ahead by exactly its auto-commit.
func TestSyncStatusSyncedThenAhead(t *testing.T) {
	remote := newBareRemote(t)
	s, _ := newStore(t)
	mustCreate(t, s, "x", "s1")
	mustSync(t, s, remote)
	wantStatus(t, s, 0, 0, true)

	mustCreate(t, s, "y", "s2")
	wantStatus(t, s, 1, 0, true)
}

// TestSyncStatusBehindAfterFetch: another machine pushes, this one fetches —
// the remote-tracking ref moves ahead of HEAD and behind goes positive with
// no network call from SyncStatus itself. A local commit then mixes in ahead.
func TestSyncStatusBehindAfterFetch(t *testing.T) {
	remote := newBareRemote(t)
	a, _ := newStore(t)
	mustCreate(t, a, "alpha", "s1")
	mustSync(t, a, remote)

	b, _ := newStore(t)
	mustCreate(t, b, "beta", "s2")
	mustSync(t, b, remote) // merges a's history, pushes — a is now behind

	if out, err := gitOut(t, a.Root(), "fetch", "origin"); err != nil {
		t.Fatalf("git fetch: %v: %s", err, out)
	}
	ahead, behind, ok := a.SyncStatus()
	if !ok || ahead != 0 || behind == 0 {
		t.Errorf("SyncStatus() = (%d, %d, %v), want (0, >0, true) after fetching the other machine's push", ahead, behind, ok)
	}

	mustCreate(t, a, "gamma", "s3")
	ahead, behind, ok = a.SyncStatus()
	if !ok || ahead != 1 || behind == 0 {
		t.Errorf("SyncStatus() = (%d, %d, %v), want (1, >0, true) with a local commit on top", ahead, behind, ok)
	}
}

// TestSyncStatusGitAbsent: a previously-configured remote with git gone from
// PATH reads not-ok — never an error, never a warning.
func TestSyncStatusGitAbsent(t *testing.T) {
	remote := newBareRemote(t)
	s, warn := newStore(t)
	mustCreate(t, s, "x", "s1")
	mustSync(t, s, remote)

	t.Setenv("PATH", t.TempDir()) // git unfindable from here on
	wantStatus(t, s, 0, 0, false)
	if warn.Len() != 0 {
		t.Errorf("SyncStatus warned: %s", warn)
	}
}

// TestSyncStatusSubstrateDisabled: git_auto_commit:false stores never report
// a status, even when the directory happens to be a repo with a remote.
func TestSyncStatusSubstrateDisabled(t *testing.T) {
	hermeticGit(t)
	root := filepath.Join(t.TempDir(), "bottles")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"git_auto_commit": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root, WithWarnWriter(&bytes.Buffer{}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	wantStatus(t, s, 0, 0, false)
}
