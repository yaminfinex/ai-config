package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/harness/claude"
)

// decantSession runs a decant and returns the materialized session id (the
// decants-map key), for the create→decant→rebottle chain.
func decantSession(t *testing.T, d *deps, ref string) string {
	t.Helper()
	before, _ := d.store.Decants()
	if code := cmdDecant(d, []string{ref}); code != 0 {
		t.Fatalf("decant %s: exit %d", ref, code)
	}
	after, err := d.store.Decants()
	if err != nil {
		t.Fatalf("decants: %v", err)
	}
	for id := range after {
		if _, existed := before[id]; !existed {
			return id
		}
	}
	t.Fatalf("no new decant session recorded")
	return ""
}

// TestProvenanceChain is the spec acceptance: create → decant → rebottle, with
// log telling the true story (`@2 ← decant of core@1 (session …)`).
func TestProvenanceChain(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-A", "plain")

	if code := cmdCreate(d, []string{"core", "--session", "sess-A"}); code != 0 {
		t.Fatalf("create: exit %d (stderr: %s)", code, stderr)
	}
	v1 := mustResolve(t, st, "core@1")

	sid := decantSession(t, d, "core")

	// rebottle the decanted session (whole; it isn't the live session here).
	stdout.Reset()
	if code := cmdRebottle(d, []string{"--session", sid}); code != 0 {
		t.Fatalf("rebottle: exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Rebottled core@2") {
		t.Errorf("rebottle confirmation wrong: %s", stdout)
	}

	v2 := mustResolve(t, st, "core")
	if v2.Meta.Version != 2 {
		t.Fatalf("latest version = %d, want 2", v2.Meta.Version)
	}
	if v2.Meta.Parent == nil || v2.Meta.Parent.BottleID != v1.ID {
		t.Fatalf("parent = %+v, want bottle %s", v2.Meta.Parent, v1.ID)
	}
	if v2.Meta.Parent.DecantSessionID != sid {
		t.Errorf("parent decant session = %q, want %q", v2.Meta.Parent.DecantSessionID, sid)
	}

	// log renders the provenance line.
	stdout.Reset()
	if code := cmdLog(d, []string{"core"}); code != 0 {
		t.Fatalf("log: exit %d (stderr: %s)", code, stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "@2 ← decant of core@1") {
		t.Errorf("log missing provenance line:\n%s", out)
	}
	if !strings.Contains(out, "session "+shortSession(sid)) {
		t.Errorf("log missing decant session id:\n%s", out)
	}
}

func TestRebottleNonDecantErrorsWithFallbackHint(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	if code := cmdRebottle(d, []string{"--session", "never-decanted"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero for a non-decant session")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "not a known decant") {
		t.Errorf("stderr missing the documented error: %s", msg)
	}
	if !strings.Contains(msg, "bottle create") {
		t.Errorf("stderr missing the plain-create fallback hint: %s", msg)
	}
}

func TestRebottleNoSessionOutsideHarness(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	d.selfSession = ""
	if code := cmdRebottle(d, nil); code == 0 {
		t.Fatalf("exit = 0, want non-zero with no session")
	}
	if !strings.Contains(stderr.String(), "not inside a Claude session") {
		t.Errorf("stderr missing the no-session guidance: %s", stderr)
	}
}

// TestRebottleLiveSelfTrimsAndSetsParent covers rebottling the live session: it
// trims the in-flight turn (self-bottle) and still records the parent.
func TestRebottleLiveSelfTrimsAndSetsParent(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-A", "plain")
	if code := cmdCreate(d, []string{"core", "--session", "sess-A"}); code != 0 {
		t.Fatalf("create: exit %d (stderr: %s)", code, stderr)
	}
	v1 := mustResolve(t, st, "core@1")

	sid := decantSession(t, d, "core")

	// Make the decanted session look like a live, in-flight one and rebottle it.
	dangling, err := os.ReadFile(filepath.Join("..", "..", "testdata", "dangling-tool-use.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(claude.ProjectDir(d.projectsRoot, d.cwd), sid+".jsonl")
	if err := os.WriteFile(seed, dangling, 0o600); err != nil {
		t.Fatal(err)
	}
	d.selfSession = sid

	if code := cmdRebottle(d, nil); code != 0 {
		t.Fatalf("rebottle: exit %d (stderr: %s)", code, stderr)
	}
	v2 := mustResolve(t, st, "core")
	if v2.Meta.Version != 2 || v2.Meta.Parent == nil || v2.Meta.Parent.BottleID != v1.ID {
		t.Fatalf("parent linkage wrong: %+v", v2.Meta)
	}
	if v2.Meta.Source.CutTurn != 2 {
		t.Errorf("cut turn = %d, want 2 (self-trim of the dangling tail)", v2.Meta.Source.CutTurn)
	}
}
