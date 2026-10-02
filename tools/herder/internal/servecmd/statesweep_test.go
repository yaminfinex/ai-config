package servecmd

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/webstate"
)

func markerRow(key string, updated time.Time) webstate.Row {
	return webstate.Row{Key: key, Value: json.RawMessage(`{"turn":1}`), Updated: updated.UnixMilli(), WriteID: "w"}
}

func TestStateSweepPolicyDropsOnlyLongAbsentAgentsReadMarkers(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)
	proj := agentstore.NewProjection()
	proj.Apply(agentstore.Event{ID: "1", At: old, Kind: agentstore.KindSessionObserved, Name: "stale"}, 0)
	proj.Apply(agentstore.Event{ID: "2", At: old.Add(time.Hour), Kind: agentstore.KindCulled, Name: "stale"}, 0)
	proj.Apply(agentstore.Event{ID: "3", At: old, Kind: agentstore.KindSessionObserved, Name: "fresh-closed"}, 0)
	proj.Apply(agentstore.Event{ID: "4", At: recent, Kind: agentstore.KindCulled, Name: "fresh-closed"}, 0)
	proj.Apply(agentstore.Event{ID: "5", At: recent, Kind: agentstore.KindSessionObserved, Name: "fresh-seen"}, 0)
	roster := []hcomidentity.Row{{Name: "live"}, {Name: "impl-base", BaseName: "base"}}
	policy := stateSweepPolicy(now, roster, proj)

	cases := []struct {
		namespace string
		row       webstate.Row
		want      bool
		why       string
	}{
		{"read.markers", markerRow("stale", old), true, "off the roster, history closed >7d ago, row >7d old"},
		{"read.markers", markerRow("unknown", old), true, "no history at all and off the roster"},
		{"read.markers", markerRow("live", old), false, "on the roster"},
		{"read.markers", markerRow("base", old), false, "on the roster by base name"},
		{"read.markers", markerRow("fresh-closed", old), false, "closed only 2 days ago"},
		{"read.markers", markerRow("fresh-seen", old), false, "seen 2 days ago"},
		{"read.markers", markerRow("stale", recent), false, "the row itself was written recently"},
		{"notes", markerRow("stale", old), false, "notes are keyed by note id, not agent"},
		{"spaces.members", markerRow("stale", old), false, "keyed by space id"},
	}
	for _, c := range cases {
		if got := policy.Absent(c.namespace, c.row); got != c.want {
			t.Errorf("%s/%s: absent=%v want %v (%s)", c.namespace, c.row.Key, got, c.want, c.why)
		}
	}
	if policy.TombstonesBefore != now.Add(-30*24*time.Hour).UnixMilli() {
		t.Fatalf("tombstones purge before %d", policy.TombstonesBefore)
	}
}

func TestSweepStateOncePublishesEachChangedNamespaceAndNeedsARosterForAbsence(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour)
	store, err := webstate.NewFileStore(t.TempDir(), webstate.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	tombstone := func(key string) webstate.Row {
		return webstate.Row{Key: key, Value: json.RawMessage(`null`), Updated: old.UnixMilli(), WriteID: "w", Deleted: true}
	}
	if _, _, err := store.Upsert("web-owner", "read.markers", []webstate.Row{markerRow("gone", old), tombstone("culled")}); err != nil {
		t.Fatal(err)
	}

	deps := fixtureDeps()
	deps.now = func() time.Time { return now }
	deps.projection = &projectionCache{}
	deps.projection.set(agentstore.NewProjection(), 0)
	rosterErr := errRosterPending
	deps.roster = func() ([]hcomidentity.Row, error) { return nil, rosterErr }
	var published []stateChange
	publish := func(change stateChange) { published = append(published, change) }

	sweepStateOnce(deps, store, publish, nil)
	rows, _, _ := store.Since("web-owner", "read.markers", 0)
	if len(published) != 1 || published[0].Namespace != "read.markers" || len(rows) != 1 || rows[0].Key != "gone" {
		t.Fatalf("without a roster only the old tombstone goes; an unreadable roster is not evidence of absence: published=%+v rows=%v", published, rows)
	}

	rosterErr = nil
	published = nil
	sweepStateOnce(deps, store, publish, nil)
	rows, rev, _ := store.Since("web-owner", "read.markers", 0)
	if len(rows) != 0 || len(published) != 1 || published[0].Namespace != "read.markers" || published[0].Rev != rev {
		t.Fatalf("with a roster the absent agent's row goes and is published: rows=%v published=%+v rev=%d", rows, published, rev)
	}
}

// Owner content keeps its tombstones: clients replay their caches
// additively, so a purged note or space tombstone would let a stale browser
// re-add the deleted row. Only agent-keyed namespaces are swept.
func TestSweepNeverPurgesOwnerContentTombstones(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	old := now.Add(-31 * 24 * time.Hour)
	store, err := webstate.NewFileStore(t.TempDir(), webstate.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, namespace := range []string{"notes", "spaces", "spaces.members"} {
		rows := []webstate.Row{
			{Key: "deleted", Value: json.RawMessage(`null`), Updated: old.UnixMilli(), WriteID: "w", Deleted: true},
			{Key: "gone", Value: json.RawMessage(`{"x":1}`), Updated: old.UnixMilli(), WriteID: "w"},
		}
		if _, _, err := store.Upsert("web-owner", namespace, rows); err != nil {
			t.Fatal(err)
		}
	}
	deps := fixtureDeps()
	deps.now = func() time.Time { return now }
	deps.projection = &projectionCache{}
	deps.projection.set(agentstore.NewProjection(), 0)
	deps.roster = func() ([]hcomidentity.Row, error) { return nil, nil }
	var published []stateChange
	sweepStateOnce(deps, store, func(change stateChange) { published = append(published, change) }, nil)
	if len(published) != 0 {
		t.Fatalf("owner content namespaces changed: %+v", published)
	}
	for _, namespace := range []string{"notes", "spaces", "spaces.members"} {
		rows, _, err := store.Since("web-owner", namespace, 0)
		if err != nil || len(rows) != 2 {
			t.Fatalf("%s after sweep = %v (%v); want the 31-day tombstone and the old row kept", namespace, rows, err)
		}
	}
}

// A store read that fails after an earlier success leaves the board its
// last projection, but that projection is no evidence an agent is gone.
func TestSweepSkipsAbsenceAfterAFailedProjectionRefresh(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour)
	deps := fixtureDeps()
	deps.now = func() time.Time { return now }
	deps.projection = &projectionCache{}
	stale := agentstore.NewProjection()
	stale.Apply(agentstore.Event{ID: "1", At: old, Kind: agentstore.KindSessionObserved, Name: "gone"}, 0)
	deps.projection.set(stale, 0)
	deps.store = agentstore.Open(t.TempDir(), nil)
	if err := os.MkdirAll(deps.store.EventsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	var audits int
	deps.audit = func(string, ...any) { audits++ }
	refreshProjection(deps)
	if audits == 0 {
		t.Fatal("expected the projection read to fail")
	}
	if deps.projection.get() != stale {
		t.Fatal("the board keeps the last projection")
	}
	deps.roster = func() ([]hcomidentity.Row, error) { return nil, nil }
	store, err := webstate.NewFileStore(t.TempDir(), webstate.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Upsert("web-owner", "read.markers", []webstate.Row{markerRow("gone", old)}); err != nil {
		t.Fatal(err)
	}
	sweepStateOnce(deps, store, func(stateChange) {}, nil)
	if rows, _, _ := store.Since("web-owner", "read.markers", 0); len(rows) != 1 {
		t.Fatalf("marker removed on a stale projection: %v", rows)
	}

	// A later successful read is evidence again.
	if err := os.Remove(deps.store.EventsPath()); err != nil {
		t.Fatal(err)
	}
	refreshProjection(deps)
	if deps.projection.current() == nil {
		t.Fatal("a successful refresh clears the failure")
	}
	sweepStateOnce(deps, store, func(stateChange) {}, nil)
	if rows, _, _ := store.Since("web-owner", "read.markers", 0); len(rows) != 0 {
		t.Fatalf("after a good read the absent marker goes: %v", rows)
	}
}
