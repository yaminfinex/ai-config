package webstate

import (
	"sort"
	"testing"
)

func sweepFixture(t *testing.T) *FileStore {
	t.Helper()
	store, err := NewFileStore(t.TempDir(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func keys(rows []Row) []string {
	out := []string{}
	for _, row := range rows {
		out = append(out, row.Key)
	}
	sort.Strings(out)
	return out
}

func TestSweepPurgesOldTombstonesAndAbsentRowsWithoutTombstones(t *testing.T) {
	store := sweepFixture(t)
	if _, _, err := store.Upsert("web-owner", "read.markers", []Row{
		row("gone", 10, "w", map[string]any{"turn": 1}, false),
		row("here", 10, "w", map[string]any{"turn": 1}, false),
		row("old-delete", 10, "w", nil, true),
		row("new-delete", 900, "w", nil, true),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Upsert("web-owner", "notes", []Row{row("note-1", 10, "w", map[string]any{"text": "x"}, false), row("note-2", 10, "w", nil, true)}); err != nil {
		t.Fatal(err)
	}
	_, before, _ := store.Since("web-owner", "read.markers", 0)

	results, err := store.Sweep(SweepPolicy{
		Namespaces:       map[string]bool{"read.markers": true},
		TombstonesBefore: 500,
		Absent:           func(namespace string, row Row) bool { return namespace == "read.markers" && row.Key == "gone" },
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Namespace < results[j].Namespace })
	if len(results) != 1 || results[0].Namespace != "read.markers" || results[0].Removed != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Rev != before+1 {
		t.Fatalf("a sweep bumps the revision once: rev %d after %d", results[0].Rev, before)
	}

	rows, rev, err := store.Since("web-owner", "read.markers", 0)
	if err != nil || rev != results[0].Rev {
		t.Fatalf("since: rev=%d err=%v", rev, err)
	}
	if got := keys(rows); len(got) != 2 || got[0] != "here" || got[1] != "new-delete" {
		t.Fatalf("rows after sweep = %v; want the present row and the recent tombstone, nothing left for the absent row", got)
	}
	notes, _, _ := store.Since("web-owner", "notes", 0)
	if got := keys(notes); len(got) != 2 || got[0] != "note-1" || got[1] != "note-2" {
		t.Fatalf("notes after sweep = %v; a namespace the policy does not list keeps every row and tombstone", got)
	}

	again, err := store.Sweep(SweepPolicy{Namespaces: map[string]bool{"read.markers": true}, TombstonesBefore: 500, Absent: func(string, Row) bool { return false }})
	if err != nil || len(again) != 0 {
		t.Fatalf("a sweep with nothing to remove changes nothing: %+v %v", again, err)
	}
	_, still, _ := store.Since("web-owner", "read.markers", 0)
	if still != rev {
		t.Fatalf("an idle sweep bumped the revision %d -> %d", rev, still)
	}
}

func TestSweepAnswersAnOlderCursorWithEveryCurrentRow(t *testing.T) {
	store := sweepFixture(t)
	if _, _, err := store.Upsert("web-owner", "read.markers", []Row{row("a", 10, "w", map[string]any{"turn": 1}, false), row("b", 10, "w", map[string]any{"turn": 1}, false)}); err != nil {
		t.Fatal(err)
	}
	_, cursor, _ := store.Since("web-owner", "read.markers", 0)
	if _, err := store.Sweep(SweepPolicy{Namespaces: map[string]bool{"read.markers": true}, Absent: func(_ string, row Row) bool { return row.Key == "b" }}); err != nil {
		t.Fatal(err)
	}
	rows, rev, err := store.Since("web-owner", "read.markers", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(rows); len(got) != 1 || got[0] != "a" || rev != cursor+1 {
		t.Fatalf("older cursor got %v at rev %d; want the full current snapshot [a] at %d", got, rev, cursor+1)
	}
	rows, _, _ = store.Since("web-owner", "read.markers", rev)
	if len(rows) != 0 {
		t.Fatalf("a cursor at the sweep revision gets nothing new, got %v", keys(rows))
	}

	// A name reused later starts fresh: its first write is a plain new row.
	if _, _, err := store.Upsert("web-owner", "read.markers", []Row{row("b", 20, "w2", map[string]any{"turn": 0}, false)}); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = store.Since("web-owner", "read.markers", rev)
	if got := keys(rows); len(got) != 1 || got[0] != "b" {
		t.Fatalf("after the floor, deltas resume: %v", got)
	}

	reopened, err := NewFileStore(store.root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ = reopened.Since("web-owner", "read.markers", cursor)
	if got := keys(rows); len(got) != 2 {
		t.Fatalf("the floor survives a restart: older cursor got %v", got)
	}
}
