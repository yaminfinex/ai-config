package cli

import (
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/store"
)

func TestPruneRemovesOnlyDeadDecants(t *testing.T) {
	st := openTestStore(t)
	b := makeBottle(t, st, "alpha", "", nil)
	const live, dead = "sess-live", "sess-dead"
	if err := st.RecordDecant(live, b.ID); err != nil {
		t.Fatalf("record live decant: %v", err)
	}
	if err := st.RecordDecant(dead, b.ID); err != nil {
		t.Fatalf("record dead decant: %v", err)
	}

	d, stdout, stderr := newDeps(t, st)
	// Seam fake: the live session still exists, the dead one does not.
	d.sessionExists = func(_ *store.Meta, sid string) bool { return sid == live }

	if code := cmdPrune(d, nil); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Pruned 1") {
		t.Errorf("expected to prune 1, got: %s", stdout)
	}

	remaining, err := st.Decants()
	if err != nil {
		t.Fatalf("decants: %v", err)
	}
	if _, ok := remaining[live]; !ok {
		t.Errorf("live decant was pruned")
	}
	if _, ok := remaining[dead]; ok {
		t.Errorf("dead decant was not pruned")
	}
}

func TestPruneOrphanedDecantRemoved(t *testing.T) {
	st := openTestStore(t)
	if err := st.RecordDecant("sess-orphan", "no-such-bottle"); err != nil {
		t.Fatalf("record orphan decant: %v", err)
	}
	d, stdout, _ := newDeps(t, st)
	if code := cmdPrune(d, nil); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Pruned 1") {
		t.Errorf("orphaned decant (missing bottle) not pruned: %s", stdout)
	}
}

func TestPruneEmptyReportsZero(t *testing.T) {
	st := openTestStore(t)
	d, stdout, _ := newDeps(t, st)
	if code := cmdPrune(d, nil); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Pruned 0") {
		t.Errorf("empty prune should report 0: %s", stdout)
	}
}
