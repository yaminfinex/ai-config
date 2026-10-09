package webstate

import (
	"sort"
	"strings"
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
	if _, err := store.Upsert("web-owner", "read.markers", []Row{
		row("gone", 10, "w", map[string]any{"turn": 1}, false),
		row("here", 10, "w", map[string]any{"turn": 1}, false),
		row("old-delete", 10, "w", nil, true),
		row("new-delete", 900, "w", nil, true),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert("web-owner", "notes", []Row{row("note-1", 10, "w", map[string]any{"text": "x"}, false), row("note-2", 10, "w", nil, true)}); err != nil {
		t.Fatal(err)
	}
	_, before, _ := since(store, "web-owner", "read.markers", 0)

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

	rows, rev, err := since(store, "web-owner", "read.markers", 0)
	if err != nil || rev != results[0].Rev {
		t.Fatalf("since: rev=%d err=%v", rev, err)
	}
	if got := keys(rows); len(got) != 2 || got[0] != "here" || got[1] != "new-delete" {
		t.Fatalf("rows after sweep = %v; want the present row and the recent tombstone, nothing left for the absent row", got)
	}
	notes, _, _ := since(store, "web-owner", "notes", 0)
	if got := keys(notes); len(got) != 2 || got[0] != "note-1" || got[1] != "note-2" {
		t.Fatalf("notes after sweep = %v; a namespace the policy does not list keeps every row and tombstone", got)
	}

	again, err := store.Sweep(SweepPolicy{Namespaces: map[string]bool{"read.markers": true}, TombstonesBefore: 500, Absent: func(string, Row) bool { return false }})
	if err != nil || len(again) != 0 {
		t.Fatalf("a sweep with nothing to remove changes nothing: %+v %v", again, err)
	}
	_, still, _ := since(store, "web-owner", "read.markers", 0)
	if still != rev {
		t.Fatalf("an idle sweep bumped the revision %d -> %d", rev, still)
	}
}

func TestSweepAnswersAnOlderCursorWithEveryCurrentRow(t *testing.T) {
	store := sweepFixture(t)
	if _, err := store.Upsert("web-owner", "read.markers", []Row{row("a", 10, "w", map[string]any{"turn": 1}, false), row("b", 10, "w", map[string]any{"turn": 1}, false)}); err != nil {
		t.Fatal(err)
	}
	_, cursor, _ := since(store, "web-owner", "read.markers", 0)
	if _, err := store.Sweep(SweepPolicy{Namespaces: map[string]bool{"read.markers": true}, Absent: func(_ string, row Row) bool { return row.Key == "b" }}); err != nil {
		t.Fatal(err)
	}
	rows, rev, err := since(store, "web-owner", "read.markers", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(rows); len(got) != 1 || got[0] != "a" || rev != cursor+1 {
		t.Fatalf("older cursor got %v at rev %d; want the full current snapshot [a] at %d", got, rev, cursor+1)
	}
	rows, _, _ = since(store, "web-owner", "read.markers", rev)
	if len(rows) != 0 {
		t.Fatalf("a cursor at the sweep revision gets nothing new, got %v", keys(rows))
	}

	// A name reused later starts fresh: its first write is a plain new row.
	if _, err := store.Upsert("web-owner", "read.markers", []Row{row("b", 20, "w2", map[string]any{"turn": 0}, false)}); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = since(store, "web-owner", "read.markers", rev)
	if got := keys(rows); len(got) != 1 || got[0] != "b" {
		t.Fatalf("after the floor, deltas resume: %v", got)
	}

	reopened, err := NewFileStore(store.root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ = since(reopened, "web-owner", "read.markers", cursor)
	if got := keys(rows); len(got) != 2 {
		t.Fatalf("the floor survives a restart: older cursor got %v", got)
	}
}

func horizonPolicy(before int64) SweepPolicy {
	return SweepPolicy{Namespaces: map[string]bool{"notes": true, "read.markers": true}, Horizon: map[string]bool{"notes": true}, TombstonesBefore: before}
}

func TestSweepPurgesGuardedTombstonesAndRaisesTheHorizon(t *testing.T) {
	store := sweepFixture(t)
	if _, err := store.Upsert("web-owner", "notes", []Row{
		row("live-old", 10, "w", map[string]any{"text": "kept"}, false),
		row("deleted-early", 100, "w", nil, true),
		row("deleted-late", 300, "w", nil, true),
		row("deleted-recent", 900, "w", nil, true),
	}); err != nil {
		t.Fatal(err)
	}
	results, err := store.Sweep(horizonPolicy(500))
	if err != nil || len(results) != 1 || results[0].Removed != 2 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	snapshot, err := store.Since("web-owner", "notes", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(snapshot.Rows); len(got) != 2 || got[0] != "deleted-recent" || got[1] != "live-old" {
		t.Fatalf("rows after sweep = %v; want the live row and the recent tombstone", got)
	}
	if snapshot.Horizon != 300 {
		t.Fatalf("horizon = %d; want the newest purged tombstone, 300", snapshot.Horizon)
	}

	// A later sweep that purges nothing older leaves the horizon where it is.
	if _, err := store.Sweep(horizonPolicy(500)); err != nil {
		t.Fatal(err)
	}
	if snapshot, _ = store.Since("web-owner", "notes", 0); snapshot.Horizon != 300 {
		t.Fatalf("an idle sweep moved the horizon to %d", snapshot.Horizon)
	}

	reopened, err := NewFileStore(store.root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, _ = reopened.Since("web-owner", "notes", 0); snapshot.Horizon != 300 {
		t.Fatalf("the horizon survives a restart: got %d", snapshot.Horizon)
	}
	result, err := reopened.Upsert("web-owner", "notes", []Row{row("deleted-late", 200, "stale-device", map[string]any{"text": "cached"}, false)})
	if err != nil || len(result.Accepted) != 0 || len(result.Stale) != 1 {
		t.Fatalf("after a restart a stale replay is still refused: %+v %v", result, err)
	}
}

// Without a purge there is no horizon: the browser's seeded main space,
// written at updated 0, is a create like any other.
func TestUpsertWithoutAHorizonAcceptsARowAtUpdatedZero(t *testing.T) {
	store := sweepFixture(t)
	result, err := store.Upsert("web-owner", "spaces", []Row{row("main", 0, "seed", map[string]any{"name": "main"}, false)})
	if err != nil || strings.Join(result.Accepted, ",") != "main" || len(result.Stale) != 0 {
		t.Fatalf("seeded row with no horizon: %+v %v", result, err)
	}
}

func TestUpsertAnswersAStaleCreateBelowTheHorizonAndAcceptsEverythingElse(t *testing.T) {
	store := sweepFixture(t)
	if _, err := store.Upsert("web-owner", "notes", []Row{
		row("held", 10, "w", map[string]any{"text": "v1"}, false),
		row("purged", 300, "w", nil, true),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Sweep(horizonPolicy(500)); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Since("web-owner", "notes", 0)

	// A stale device replays its cached live copy of the purged note, and an
	// old tombstone of a note the server never held: both are stale.
	result, err := store.Upsert("web-owner", "notes", []Row{
		row("purged", 200, "stale-device", map[string]any{"text": "cached"}, false),
		row("ancient", 300, "stale-device", nil, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Accepted) != 0 || strings.Join(result.Stale, ",") != "purged,ancient" || result.Rev != before.Rev {
		t.Fatalf("stale creates: %+v; want both stale and the revision unchanged at %d", result, before.Rev)
	}
	after, _ := store.Since("web-owner", "notes", 0)
	if got := keys(after.Rows); len(got) != 1 || got[0] != "held" {
		t.Fatalf("rows after a stale replay = %v", got)
	}

	// An edit of a row the server holds is ordinary last-write-wins, even when
	// its time is below the horizon; a new note above the horizon is a create.
	result, err = store.Upsert("web-owner", "notes", []Row{
		row("held", 20, "w2", map[string]any{"text": "v2"}, false),
		row("fresh", 1000, "w", map[string]any{"text": "new"}, false),
	})
	if err != nil || strings.Join(result.Accepted, ",") != "held,fresh" || len(result.Stale) != 0 {
		t.Fatalf("live edit and fresh create: %+v %v", result, err)
	}
}

func TestReadMarkersPurgeLeavesNoHorizon(t *testing.T) {
	store := sweepFixture(t)
	if _, err := store.Upsert("web-owner", "read.markers", []Row{row("culled", 10, "w", nil, true)}); err != nil {
		t.Fatal(err)
	}
	if results, err := store.Sweep(horizonPolicy(500)); err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	snapshot, _ := store.Since("web-owner", "read.markers", 0)
	if snapshot.Horizon != 0 || len(snapshot.Rows) != 0 {
		t.Fatalf("read.markers after sweep: %+v", snapshot)
	}
	// A stale client re-adding a swept marker is accepted, as before.
	result, err := store.Upsert("web-owner", "read.markers", []Row{row("culled", 5, "stale", map[string]any{"turn": 1}, false)})
	if err != nil || len(result.Accepted) != 1 || len(result.Stale) != 0 {
		t.Fatalf("read.markers re-add: %+v %v", result, err)
	}
}
