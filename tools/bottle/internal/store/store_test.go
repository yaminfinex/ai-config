package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-config/tools/bottle/internal/refs"
)

// newStore opens a fresh store under a temp dir, with warnings captured.
func newStore(t *testing.T) (*Store, *bytes.Buffer) {
	t.Helper()
	warn := &bytes.Buffer{}
	s, err := Open(filepath.Join(t.TempDir(), "bottles"), WithWarnWriter(warn))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, warn
}

func mkCreate(name, session string) CreateRequest {
	return CreateRequest{
		Name:       name,
		Transcript: []byte(`{"sessionId":"` + session + `"}` + "\n"),
		Source: Source{
			SessionID:  session,
			Harness:    "claude",
			CWD:        "/tmp/proj",
			GitBranch:  "main",
			GitSHA:     "abc1234",
			CutTurn:    3,
			TotalTurns: 5,
		},
		Note: "a note",
	}
}

func TestCreateAndResolve(t *testing.T) {
	s, _ := newStore(t)

	b1, err := s.Create(mkCreate("auth-expert", "sess-1"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-z]{8}$`).MatchString(b1.ID) {
		t.Errorf("bottle id %q is not 8-char base36", b1.ID)
	}
	if b1.Meta.Version != 1 {
		t.Errorf("first version = %d, want 1", b1.Meta.Version)
	}

	// Same-name create bumps the version.
	b2, err := s.Create(mkCreate("auth-expert", "sess-2"))
	if err != nil {
		t.Fatalf("Create #2: %v", err)
	}
	if b2.Meta.Version != 2 {
		t.Errorf("bumped version = %d, want 2", b2.Meta.Version)
	}
	if b2.ID == b1.ID {
		t.Errorf("bottle ids collide: %q", b1.ID)
	}

	// Unpinned ref resolves to the latest version.
	latest, err := s.Resolve(refs.Ref{Name: "auth-expert"})
	if err != nil {
		t.Fatalf("Resolve latest: %v", err)
	}
	if latest.ID != b2.ID {
		t.Errorf("latest = %s, want %s", latest.ID, b2.ID)
	}

	// Pinned ref resolves to that exact version.
	pinned, err := s.Resolve(refs.Ref{Name: "auth-expert", Version: 1})
	if err != nil {
		t.Fatalf("Resolve pinned: %v", err)
	}
	if pinned.ID != b1.ID {
		t.Errorf("pinned @1 = %s, want %s", pinned.ID, b1.ID)
	}

	// Transcript round-trips.
	tr, err := s.ReadTranscript(b1.ID)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if want := `{"sessionId":"sess-1"}` + "\n"; string(tr) != want {
		t.Errorf("transcript = %q, want %q", tr, want)
	}
}

func TestResolveErrors(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Resolve(refs.Ref{Name: "ghost"}); err == nil {
		t.Error("Resolve missing name: expected error")
	}
	if _, err := s.Create(mkCreate("real", "sess-1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Resolve(refs.Ref{Name: "real", Version: 9}); err == nil {
		t.Error("Resolve missing version: expected error")
	}
}

func TestCreateRejectsInvalidName(t *testing.T) {
	s, _ := newStore(t)
	for _, name := range []string{"Foo", "-x", "a@b"} {
		_, err := s.Create(mkCreate(name, "sess-1"))
		if err == nil {
			t.Errorf("Create(%q): expected error", name)
			continue
		}
		if !strings.Contains(err.Error(), refs.NamePattern) {
			t.Errorf("Create(%q) error %q does not contain the name regex", name, err)
		}
	}
}

// TestMetaFieldNames pins the on-disk meta.json schema to the origin spec:
// name, version, created, note, source{session_id, harness, cwd, git_branch,
// git_sha, cut_turn, total_turns}, parent{bottle_id, decant_session_id},
// inherited_lines + compaction annotations.
func TestMetaFieldNames(t *testing.T) {
	s, _ := newStore(t)
	parentBottle, err := s.Create(mkCreate("parent", "sess-p"))
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	req := mkCreate("child", "sess-c")
	req.Parent = &Parent{BottleID: parentBottle.ID, DecantSessionID: "decant-sess"}
	req.InheritedLines = 42
	req.Compacted = true
	req.CompactionReachesInherited = true
	req.RewoundIntoParent = true
	child, err := s.Create(req)
	if err != nil {
		t.Fatalf("Create child: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(s.Root(), "store", child.ID, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("meta.json not valid JSON: %v", err)
	}
	for _, key := range []string{"name", "version", "created", "note", "source", "parent",
		"inherited_lines", "compacted", "compaction_reaches_inherited", "rewound_into_parent"} {
		if _, ok := m[key]; !ok {
			t.Errorf("meta.json missing key %q", key)
		}
	}
	src, _ := m["source"].(map[string]any)
	for _, key := range []string{"session_id", "harness", "cwd", "git_branch", "git_sha", "cut_turn", "total_turns"} {
		if _, ok := src[key]; !ok {
			t.Errorf("meta.json source missing key %q", key)
		}
	}
	par, _ := m["parent"].(map[string]any)
	for _, key := range []string{"bottle_id", "decant_session_id"} {
		if _, ok := par[key]; !ok {
			t.Errorf("meta.json parent missing key %q", key)
		}
	}
}

// TestRegistryFieldNames pins registry.json to the spec schema:
// names: name -> [{version, bottle_id}], plus a decants map.
func TestRegistryFieldNames(t *testing.T) {
	s, _ := newStore(t)
	b, err := s.Create(mkCreate("auth-expert", "sess-1"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Root(), "registry.json"))
	if err != nil {
		t.Fatalf("read registry.json: %v", err)
	}
	var reg struct {
		Names map[string][]struct {
			Version  int    `json:"version"`
			BottleID string `json:"bottle_id"`
		} `json:"names"`
		Decants map[string]any `json:"decants"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatalf("registry.json not valid JSON: %v", err)
	}
	entries := reg.Names["auth-expert"]
	if len(entries) != 1 || entries[0].Version != 1 || entries[0].BottleID != b.ID {
		t.Errorf("registry names entry = %+v, want [{1 %s}]", entries, b.ID)
	}
	if reg.Decants == nil {
		t.Error("registry.json missing decants map")
	}
}

func TestNewNameLineageRecordsParent(t *testing.T) {
	s, _ := newStore(t)
	alpha, err := s.Create(mkCreate("alpha", "sess-a"))
	if err != nil {
		t.Fatalf("Create alpha: %v", err)
	}
	req := mkCreate("beta", "sess-b")
	req.Parent = &Parent{BottleID: alpha.ID, DecantSessionID: "decant-1"}
	if _, err := s.Create(req); err != nil {
		t.Fatalf("Create beta: %v", err)
	}

	log, err := s.Log("beta")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(log) != 1 {
		t.Fatalf("Log returned %d entries, want 1", len(log))
	}
	p := log[0].Parent
	if p == nil {
		t.Fatal("Log entry has no parent")
	}
	if p.BottleID != alpha.ID || p.DecantSessionID != "decant-1" {
		t.Errorf("parent = %+v, want bottle %s via decant-1", p, alpha.ID)
	}
	if p.Display() != "alpha@1" {
		t.Errorf("parent display = %q, want %q", p.Display(), "alpha@1")
	}
}

func TestLogOrdersNewestFirst(t *testing.T) {
	s, _ := newStore(t)
	for _, sess := range []string{"s1", "s2", "s3"} {
		if _, err := s.Create(mkCreate("x", sess)); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	log, err := s.Log("x")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(log) != 3 || log[0].Version != 3 || log[2].Version != 1 {
		t.Errorf("Log versions = %v, want [3 2 1]", []int{log[0].Version, log[1].Version, log[2].Version})
	}
}

func TestRenameMovesAllVersions(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Create(mkCreate("x", "s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(mkCreate("x", "s2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("x", "y"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	for v := 1; v <= 2; v++ {
		b, err := s.Resolve(refs.Ref{Name: "y", Version: v})
		if err != nil {
			t.Fatalf("Resolve y@%d after rename: %v", v, err)
		}
		if b.Meta.Name != "y" {
			t.Errorf("y@%d meta name = %q, want %q", v, b.Meta.Name, "y")
		}
		if len(b.Meta.PreviousNames) != 1 || b.Meta.PreviousNames[0] != "x" {
			t.Errorf("y@%d previous names = %v, want [x]", v, b.Meta.PreviousNames)
		}
	}
	if _, err := s.Resolve(refs.Ref{Name: "x"}); err == nil {
		t.Error("old name still resolves after rename")
	}
}

func TestRenameRefusals(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Create(mkCreate("x", "s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(mkCreate("taken", "s2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("x", "taken"); err == nil {
		t.Error("Rename onto an existing name: expected refusal")
	}
	if err := s.Rename("ghost", "fresh"); err == nil {
		t.Error("Rename of a missing name: expected error")
	}
	if err := s.Rename("x", "Bad Name"); err == nil || !strings.Contains(err.Error(), refs.NamePattern) {
		t.Errorf("Rename to invalid name: error %v should contain the name regex", err)
	}
}

func TestRenameKeepsLineage(t *testing.T) {
	s, _ := newStore(t)
	alpha, err := s.Create(mkCreate("alpha", "sess-a"))
	if err != nil {
		t.Fatal(err)
	}
	req := mkCreate("beta", "sess-b")
	req.Parent = &Parent{BottleID: alpha.ID, DecantSessionID: "decant-1"}
	if _, err := s.Create(req); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("alpha", "gamma"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	log, err := s.Log("beta")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if got := log[0].Parent.Display(); got != "gamma@1" {
		t.Errorf("parent display after rename = %q, want %q (lineage must survive renames)", got, "gamma@1")
	}
}

func TestSetNote(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Create(mkCreate("x", "s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(mkCreate("x", "s2")); err != nil {
		t.Fatal(err)
	}
	// Unpinned note edits the latest version.
	if err := s.SetNote(refs.Ref{Name: "x"}, "new note"); err != nil {
		t.Fatalf("SetNote: %v", err)
	}
	latest, _ := s.Resolve(refs.Ref{Name: "x"})
	if latest.Meta.Note != "new note" {
		t.Errorf("latest note = %q, want %q", latest.Meta.Note, "new note")
	}
	v1, _ := s.Resolve(refs.Ref{Name: "x", Version: 1})
	if v1.Meta.Note != "a note" {
		t.Errorf("v1 note = %q, want untouched %q", v1.Meta.Note, "a note")
	}
	// Pinned note edits that version.
	if err := s.SetNote(refs.Ref{Name: "x", Version: 1}, "old one"); err != nil {
		t.Fatalf("SetNote pinned: %v", err)
	}
	v1, _ = s.Resolve(refs.Ref{Name: "x", Version: 1})
	if v1.Meta.Note != "old one" {
		t.Errorf("v1 note = %q, want %q", v1.Meta.Note, "old one")
	}
	if err := s.SetNote(refs.Ref{Name: "ghost"}, "x"); err == nil {
		t.Error("SetNote on missing name: expected error")
	}
}

func TestDecants(t *testing.T) {
	fixed := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	warn := &bytes.Buffer{}
	s, err := Open(filepath.Join(t.TempDir(), "bottles"),
		WithWarnWriter(warn), WithClock(func() time.Time { return fixed }))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(mkCreate("x", "s1"))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RecordDecant("decant-sess-1", b.ID); err != nil {
		t.Fatalf("RecordDecant: %v", err)
	}
	d, ok, err := s.LookupDecant("decant-sess-1")
	if err != nil || !ok {
		t.Fatalf("LookupDecant: ok=%v err=%v", ok, err)
	}
	if d.BottleID != b.ID {
		t.Errorf("decant bottle = %q, want %q", d.BottleID, b.ID)
	}
	if !d.Created.Equal(fixed) {
		t.Errorf("decant timestamp = %v, want %v (decant entries carry timestamps)", d.Created, fixed)
	}

	// The on-disk decants entry carries the spec field names + timestamp.
	raw, err := os.ReadFile(filepath.Join(s.Root(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		Decants map[string]map[string]any `json:"decants"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	entry := reg.Decants["decant-sess-1"]
	if entry == nil {
		t.Fatal("registry.json decants missing the recorded session")
	}
	for _, key := range []string{"bottle_id", "created"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("decants entry missing key %q", key)
		}
	}

	if _, ok, _ := s.LookupDecant("never-recorded"); ok {
		t.Error("LookupDecant on unknown session: expected ok=false")
	}

	all, err := s.Decants()
	if err != nil || len(all) != 1 {
		t.Fatalf("Decants() = %v entries, err=%v; want 1", len(all), err)
	}
	if err := s.RemoveDecants("decant-sess-1"); err != nil {
		t.Fatalf("RemoveDecants: %v", err)
	}
	if _, ok, _ := s.LookupDecant("decant-sess-1"); ok {
		t.Error("decant entry survived RemoveDecants")
	}
}

func TestRemoveVersionAndName(t *testing.T) {
	s, _ := newStore(t)
	b1, err := s.Create(mkCreate("x", "s1"))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := s.Create(mkCreate("x", "s2"))
	if err != nil {
		t.Fatal(err)
	}

	// Remove one pinned version.
	if err := s.Remove(refs.Ref{Name: "x", Version: 1}); err != nil {
		t.Fatalf("Remove @1: %v", err)
	}
	if _, err := s.Resolve(refs.Ref{Name: "x", Version: 1}); err == nil {
		t.Error("x@1 still resolves after Remove")
	}
	if _, err := s.Resolve(refs.Ref{Name: "x", Version: 2}); err != nil {
		t.Errorf("x@2 broken by Remove of @1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "store", b1.ID)); !os.IsNotExist(err) {
		t.Errorf("bottle dir for removed @1 still exists (err=%v)", err)
	}

	// Remove the whole name.
	if err := s.Remove(refs.Ref{Name: "x"}); err != nil {
		t.Fatalf("Remove whole name: %v", err)
	}
	if _, err := s.Resolve(refs.Ref{Name: "x"}); err == nil {
		t.Error("name still resolves after whole-name Remove")
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "store", b2.ID)); !os.IsNotExist(err) {
		t.Errorf("bottle dir for removed name still exists (err=%v)", err)
	}

	if err := s.Remove(refs.Ref{Name: "ghost"}); err == nil {
		t.Error("Remove of missing name: expected error")
	}
}

// TestDeletedParentRendersDeleted: rm of a version held as a parent is
// allowed; the child's log shows "(deleted)".
func TestDeletedParentRendersDeleted(t *testing.T) {
	s, _ := newStore(t)
	alpha, err := s.Create(mkCreate("alpha", "sess-a"))
	if err != nil {
		t.Fatal(err)
	}
	req := mkCreate("beta", "sess-b")
	req.Parent = &Parent{BottleID: alpha.ID, DecantSessionID: "decant-1"}
	if _, err := s.Create(req); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(refs.Ref{Name: "alpha", Version: 1}); err != nil {
		t.Fatalf("Remove parent: %v", err)
	}
	log, err := s.Log("beta")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	p := log[0].Parent
	if p == nil || !p.Deleted {
		t.Fatalf("parent = %+v, want Deleted=true", p)
	}
	if p.Display() != "(deleted)" {
		t.Errorf("parent display = %q, want %q", p.Display(), "(deleted)")
	}
}

func TestList(t *testing.T) {
	s, _ := newStore(t)
	infos, err := s.List()
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("empty store List = %v, want empty", infos)
	}

	if _, err := s.Create(mkCreate("bbb", "s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(mkCreate("aaa", "s2")); err != nil {
		t.Fatal(err)
	}
	req := mkCreate("aaa", "s3")
	req.Note = "latest note"
	if _, err := s.Create(req); err != nil {
		t.Fatal(err)
	}

	infos, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 || infos[0].Name != "aaa" || infos[1].Name != "bbb" {
		t.Fatalf("List = %+v, want [aaa bbb] sorted", infos)
	}
	if infos[0].Latest != 2 || infos[0].Versions != 2 {
		t.Errorf("aaa = latest %d / %d versions, want 2/2", infos[0].Latest, infos[0].Versions)
	}
	if infos[0].Note != "latest note" {
		t.Errorf("aaa note = %q, want the latest version's note", infos[0].Note)
	}
	if infos[0].Created.IsZero() {
		t.Error("aaa Created is zero; list needs it for the age column")
	}
}

func TestArtifacts(t *testing.T) {
	s, _ := newStore(t)
	b, err := s.Create(mkCreate("x", "s1"))
	if err != nil {
		t.Fatal(err)
	}

	names, err := s.Artifacts(b.ID)
	if err != nil || len(names) != 0 {
		t.Fatalf("Artifacts on bottle without any = %v, err=%v; want empty", names, err)
	}

	if err := s.AddArtifact(b.ID, "report.md", []byte("hi")); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if err := s.AddArtifact(b.ID, "sub/dir/data.txt", []byte("nested")); err != nil {
		t.Fatalf("AddArtifact nested: %v", err)
	}

	names, err = s.Artifacts(b.ID)
	if err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	want := []string{"report.md", "sub/dir/data.txt"}
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("Artifacts = %v, want %v", names, want)
	}
	data, err := s.ReadArtifact(b.ID, "sub/dir/data.txt")
	if err != nil || string(data) != "nested" {
		t.Errorf("ReadArtifact = %q, err=%v; want %q", data, err, "nested")
	}

	// Path escapes are refused.
	for _, evil := range []string{"../evil", "/abs/path", "a/../../evil"} {
		if err := s.AddArtifact(b.ID, evil, []byte("x")); err == nil {
			t.Errorf("AddArtifact(%q): expected refusal", evil)
		}
	}
}

// TestConcurrentMutationsSerialized: concurrent creates and decant records
// are serialized by the flock — every update lands, versions stay unique.
func TestConcurrentMutationsSerialized(t *testing.T) {
	s, _ := newStore(t)
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := s.Create(mkCreate("race", "sess"))
			errs <- err
		}()
		go func(i int) {
			defer wg.Done()
			errs <- s.RecordDecant(fmt.Sprintf("decant-sess-%d", i), "abcd1234")
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent mutation failed: %v", err)
		}
	}

	log, err := s.Log("race")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(log) != n {
		t.Fatalf("got %d versions, want %d (lost updates)", len(log), n)
	}
	seen := map[int]bool{}
	for _, e := range log {
		if seen[e.Version] {
			t.Errorf("duplicate version %d", e.Version)
		}
		seen[e.Version] = true
	}
	decants, err := s.Decants()
	if err != nil {
		t.Fatal(err)
	}
	if len(decants) != n {
		t.Errorf("got %d decant entries, want %d (lost updates)", len(decants), n)
	}
}

func gitOut(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// requireGit skips tests that assert git-present behavior, so the whole
// suite stays green on machines without git (the brief's no-git run).
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping git-present substrate test")
	}
}

// TestGitLazyInitAndAutoCommit: no repo until the first mutation; after a
// mutation the repo exists and is clean (everything committed).
func TestGitLazyInitAndAutoCommit(t *testing.T) {
	requireGit(t)
	s, warn := newStore(t)
	if _, err := os.Stat(filepath.Join(s.Root(), ".git")); !os.IsNotExist(err) {
		t.Fatal("git repo exists before any mutation; init must be lazy")
	}
	if _, err := s.Create(mkCreate("x", "s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), ".git")); err != nil {
		t.Fatalf("no git repo after first mutation: %v", err)
	}
	status, err := gitOut(t, s.Root(), "status", "--porcelain")
	if err != nil {
		t.Fatalf("git status: %v: %s", err, status)
	}
	if status != "" {
		t.Errorf("working tree dirty after auto-commit:\n%s", status)
	}
	if warn.Len() != 0 {
		t.Errorf("unexpected warnings with git present: %q", warn)
	}
}

// TestGitStateMatchesRegistry: after a create/rm sequence the committed
// registry matches the live one.
func TestGitStateMatchesRegistry(t *testing.T) {
	requireGit(t)
	s, _ := newStore(t)
	if _, err := s.Create(mkCreate("keep", "s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(mkCreate("gone", "s2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(refs.Ref{Name: "gone"}); err != nil {
		t.Fatal(err)
	}

	committed, err := gitOut(t, s.Root(), "show", "HEAD:registry.json")
	if err != nil {
		t.Fatalf("git show: %v: %s", err, committed)
	}
	live, err := os.ReadFile(filepath.Join(s.Root(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if committed != strings.TrimSpace(string(live)) {
		t.Errorf("committed registry differs from live registry:\n--- committed ---\n%s\n--- live ---\n%s", committed, live)
	}
	status, _ := gitOut(t, s.Root(), "status", "--porcelain")
	if status != "" {
		t.Errorf("working tree dirty after create/rm sequence:\n%s", status)
	}
}

// TestGitDisabledPerStore: config.json {"git_auto_commit": false} turns the
// substrate off for that store location.
func TestGitDisabledPerStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bottles")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"git_auto_commit": false}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	warn := &bytes.Buffer{}
	s, err := Open(root, WithWarnWriter(warn))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(mkCreate("x", "s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Error("git repo created although the store config disables it")
	}
	if warn.Len() != 0 {
		t.Errorf("unexpected warnings with git disabled: %q", warn)
	}
}

// TestGitAbsent: with git scrubbed off PATH every operation still works and
// exactly one warning is emitted.
func TestGitAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s, warn := newStore(t)

	if _, err := s.Create(mkCreate("x", "s1")); err != nil {
		t.Fatalf("Create without git: %v", err)
	}
	if _, err := s.Create(mkCreate("x", "s2")); err != nil {
		t.Fatalf("Create #2 without git: %v", err)
	}
	if err := s.Rename("x", "y"); err != nil {
		t.Fatalf("Rename without git: %v", err)
	}
	if err := s.Remove(refs.Ref{Name: "y", Version: 1}); err != nil {
		t.Fatalf("Remove without git: %v", err)
	}
	if _, err := s.Resolve(refs.Ref{Name: "y"}); err != nil {
		t.Fatalf("Resolve without git: %v", err)
	}

	if _, err := os.Stat(filepath.Join(s.Root(), ".git")); !os.IsNotExist(err) {
		t.Error("a .git appeared although git is absent")
	}
	warnings := strings.Count(warn.String(), "\n")
	if warnings != 1 {
		t.Errorf("got %d warning lines, want exactly 1:\n%s", warnings, warn)
	}
	if !strings.Contains(warn.String(), "git") {
		t.Errorf("warning does not mention git: %q", warn)
	}
}

// TestGitSweepsUntrackedState: a store mutated while git was absent gets its
// prior state swept into the first commit once git is back.
func TestGitSweepsUntrackedState(t *testing.T) {
	requireGit(t)
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	s, _ := newStore(t)
	early, err := s.Create(mkCreate("early", "s1"))
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", origPath)
	if _, err := s.Create(mkCreate("late", "s2")); err != nil {
		t.Fatal(err)
	}

	tracked, err := gitOut(t, s.Root(), "ls-files")
	if err != nil {
		t.Fatalf("git ls-files: %v: %s", err, tracked)
	}
	wantPath := "store/" + early.ID + "/transcript.jsonl"
	if !strings.Contains(tracked, wantPath) {
		t.Errorf("first commit did not sweep pre-git state: %s not tracked in:\n%s", wantPath, tracked)
	}
	status, _ := gitOut(t, s.Root(), "status", "--porcelain")
	if status != "" {
		t.Errorf("working tree dirty after sweep:\n%s", status)
	}
}

// TestStorePermissions: a store created from scratch is 0700 throughout
// (dirs 0700, files 0600).
func TestStorePermissions(t *testing.T) {
	s, _ := newStore(t)
	req := mkCreate("perm-check", "sess-1")
	if _, err := s.Create(req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := filepath.WalkDir(s.Root(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) ||
			strings.HasSuffix(path, string(filepath.Separator)+".git") {
			return nil // git's own objects have their own modes
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		perm := info.Mode().Perm()
		if d.IsDir() && perm != 0o700 {
			t.Errorf("dir %s has mode %o, want 0700", path, perm)
		}
		if !d.IsDir() && perm != 0o600 {
			t.Errorf("file %s has mode %o, want 0600", path, perm)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
