package servecmd

import (
	"encoding/json"
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
	tombstone := webstate.Row{Key: "n1", Value: json.RawMessage(`null`), Updated: old.UnixMilli(), WriteID: "w", Deleted: true}
	if _, _, err := store.Upsert("web-owner", "read.markers", []webstate.Row{markerRow("gone", old)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Upsert("web-owner", "notes", []webstate.Row{tombstone}); err != nil {
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
	if len(published) != 1 || published[0].Namespace != "notes" {
		t.Fatalf("without a roster only the old tombstone goes: %+v", published)
	}
	rows, _, _ := store.Since("web-owner", "read.markers", 0)
	if len(rows) != 1 {
		t.Fatalf("an unreadable roster is not evidence of absence: %v", rows)
	}

	rosterErr = nil
	published = nil
	sweepStateOnce(deps, store, publish, nil)
	rows, rev, _ := store.Since("web-owner", "read.markers", 0)
	if len(rows) != 0 || len(published) != 1 || published[0].Namespace != "read.markers" || published[0].Rev != rev {
		t.Fatalf("with a roster the absent agent's row goes and is published: rows=%v published=%+v rev=%d", rows, published, rev)
	}
}
