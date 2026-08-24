package observercmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/occupant"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestCacheStampCollapsesRecognitionAndTurnover(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	proj := projection(t,
		seatRow("guid-worker", "worker", "pane-1", "old-name", "", "2026-08-24T09:59:00Z"),
	)
	got := buildCacheCandidates(proj, livePane("pane-1", "sid-1", "new-name", "active"), now, time.Minute)
	if len(got) != 1 {
		t.Fatalf("candidates = %+v, want one cache stamp", got)
	}
	row := got[0].row
	if got[0].kind != "stamp" || row.Event != "observed" || row.GUID != "guid-worker" {
		t.Fatalf("candidate = %+v, want same-guid observed stamp", got[0])
	}
	if row.Cache == nil || row.Cache.PaneID != "pane-1" || row.Cache.OccupantKind != "codex" || row.Cache.SessionID != "sid-1" || row.Cache.HcomName != "new-name" || row.Cache.Label != "worker" || row.Cache.Liveness != "active" || row.Cache.ObservedAt != now.Format(time.RFC3339) {
		t.Fatalf("cache stamp = %+v", row.Cache)
	}
	if row.Seat == nil || row.Seat.PaneID != "pane-1" || row.Seat.HcomName != "new-name" || latestSID(row) != "sid-1" {
		t.Fatalf("interim verb-compatible row = %+v", row)
	}
	if row.Event == "recognised" || row.Event == "turnover" || row.Lineage.ClearedFrom != "" || row.Lineage.DisplacedBy != "" {
		t.Fatalf("authority transition leaked into cache stamp: %+v", row)
	}
}

func TestCacheStampDedupeKeepsProbeCorroboratedRow(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	proj := projection(t,
		seatRow("guid-live", "live", "pane-1", "live-name", "sid-live", "2026-08-24T09:00:00Z"),
		seatRow("guid-stale", "stale", "pane-1", "stale-name", "sid-stale", "2026-08-24T09:59:00Z"),
	)
	got := buildCacheCandidates(proj, livePane("pane-1", "sid-live", "live-name", "listening"), now, time.Minute)
	if len(got) != 2 || got[0].kind != "retire-duplicate" || got[0].guid != "guid-stale" || got[1].kind != "stamp" || got[1].guid != "guid-live" {
		t.Fatalf("candidates = %+v, want stale retirement then corroborated stamp", got)
	}
	if got[0].row.State != v2.StateRetired || got[0].row.Cache == nil || got[0].row.Cache.Liveness != "duplicate" {
		t.Fatalf("duplicate retirement = %+v", got[0].row)
	}
}

func TestCacheStampRetiresDeadRowsAfterGrace(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	proj := projection(t, seatRow("guid-dead", "dead", "pane-gone", "dead-name", "sid-dead", "2026-08-24T09:59:00Z"))
	got := buildCacheCandidates(proj, map[string]paneObservation{
		"pane-gone": {Occupant: occupant.Observation{Status: occupant.PaneGone}},
	}, now, time.Minute)
	if len(got) != 1 || got[0].kind != "dead" || got[0].row.Event != "observed_dead" || got[0].row.State != v2.StateUnseated || got[0].row.Cache == nil || got[0].row.Cache.Liveness != "dead" {
		t.Fatalf("dead candidates = %+v", got)
	}

	dead := got[0].row
	dead.RecordedAt = now.Add(-30 * time.Second).Format(time.RFC3339)
	dead.Cache.ObservedAt = dead.RecordedAt
	got = buildCacheCandidates(projection(t, dead), nil, now, time.Minute)
	if len(got) != 0 {
		t.Fatalf("within-grace candidates = %+v, want dead row retained", got)
	}

	dead.RecordedAt = now.Add(-2 * time.Minute).Format(time.RFC3339)
	dead.Cache.ObservedAt = dead.RecordedAt
	archivedProjection := projection(t, dead)
	got = buildCacheCandidates(archivedProjection, nil, now, time.Minute)
	if len(got) != 1 || got[0].kind != "archive-dead" || got[0].row.Event != "observation_archived" || got[0].row.State != v2.StateRetired {
		t.Fatalf("archive candidates = %+v", got)
	}
}

func TestCacheStampMarksRecordedOccupantMismatchDead(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	proj := projection(t, seatRow("guid-old", "old", "pane-1", "old-name", "sid-old", "2026-08-24T09:59:00Z"))
	got := buildCacheCandidates(proj, livePane("pane-1", "sid-new", "new-name", "active"), now, time.Minute)
	if len(got) != 1 || got[0].kind != "dead" || got[0].row.Cache == nil || got[0].row.Cache.Liveness != "dead" {
		t.Fatalf("mismatch candidates = %+v, want recorded occupant dead", got)
	}
}

func TestCacheStampMakesBlockedStateVisible(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	proj := projection(t, seatRow("guid-worker", "worker", "pane-1", "worker", "sid-1", "2026-08-24T09:59:00Z"))
	got := buildCacheCandidates(proj, livePane("pane-1", "sid-1", "worker", "blocked"), now, time.Minute)
	if len(got) != 1 || got[0].row.Cache == nil || got[0].row.Cache.Liveness != "blocked" {
		t.Fatalf("blocked candidates = %+v", got)
	}
}

func TestCacheStampBootRaceIsLastWriteWinsWithoutIdentityEvent(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	proj := projection(t, seatRow("guid-birth", "worker", "pane-1", "birth-name", "", "2026-08-24T10:00:01Z"))
	first := buildCacheCandidates(proj, livePane("pane-1", "sid-1", "fresh-name", "active"), now, time.Minute)
	if len(first) != 1 || first[0].row.GUID != "guid-birth" || first[0].row.Event != "observed" || first[0].row.Seat.HcomName != "fresh-name" {
		t.Fatalf("first sweep = %+v", first)
	}
	secondProj := projection(t, first[0].row)
	second := buildCacheCandidates(secondProj, livePane("pane-1", "sid-1", "forked-name", "listening"), now.Add(time.Second), time.Minute)
	if len(second) != 1 || second[0].row.GUID != "guid-birth" || second[0].row.Event != "observed" || second[0].row.Seat.HcomName != "forked-name" || latestSID(second[0].row) != "sid-1" {
		t.Fatalf("name-churn sweep = %+v", second)
	}
}

func TestCacheStampBootingPaneWaitsForCorroboration(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	proj := projection(t, seatRow("guid-birth", "worker", "pane-1", "birth-name", "", "2026-08-24T10:00:01Z"))
	got := buildCacheCandidates(proj, map[string]paneObservation{
		"pane-1": {Occupant: occupant.Observation{Status: occupant.Unprobeable}},
	}, now, time.Minute)
	if len(got) != 0 {
		t.Fatalf("booting pane candidates = %+v, want birth stamp left for next sweep", got)
	}
}

func TestCacheStampRelocatesLiveIdentityBeforeDeath(t *testing.T) {
	rec := seatRow("guid-live", "worker", "pane-old", "worker-name", "sid-live", "2026-08-24T09:59:00Z")
	bus := busState{available: true, roster: []hcomidentity.Row{{Name: "worker-name", SessionID: "sid-live", Status: "listening"}}}
	aliasCalls := 0
	obs, ok := relocateRows([]v2.SessionRecord{rec}, herdrState{}, bus,
		func(id string) occupant.Observation {
			aliasCalls++
			if id != "pane-old" {
				t.Fatalf("alias probe id = %q", id)
			}
			return occupant.Observation{Status: occupant.Occupied, Tool: "codex", SID: "sid-live", Pane: herdrcli.Pane{PaneID: "pane-new", TerminalID: "term-new"}}
		},
		func(string) occupant.Observation { return occupant.Observation{Status: occupant.PaneGone} },
	)
	if !ok || aliasCalls != 1 || obs.Pane.PaneID != "pane-new" || obs.SID != "sid-live" {
		t.Fatalf("relocation = (%+v, %t), calls=%d", obs, ok, aliasCalls)
	}
	got := buildCacheCandidates(projection(t, rec), map[string]paneObservation{
		"pane-old": {Occupant: obs, Bus: hcomidentity.Result{Name: "worker-name", SessionID: "sid-live", PaneID: "pane-new", Verified: true}, BusStatus: "listening"},
	}, time.Now().UTC(), time.Minute)
	if len(got) != 1 || got[0].kind != "stamp" || got[0].row.State != v2.StateSeated || got[0].row.Seat.PaneID != "pane-new" {
		t.Fatalf("relocated stamp = %+v", got)
	}
}

func TestCacheStampRevivesArchivedRowWhenLiveIdentityRelocates(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	rec := seatRow("guid-live", "", "pane-old", "worker-name", "sid-live", "2026-08-24T09:55:00Z")
	rec.State = v2.StateRetired
	rec.Seat = nil
	rec.Cache = &v2.CacheObservation{PaneID: "pane-old", TerminalID: "term-old", SessionID: "sid-live", HcomName: "worker-name", Label: "worker", Liveness: "dead", ObservedAt: rec.RecordedAt}
	bus := busState{available: true, roster: []hcomidentity.Row{{Name: "worker-name", SessionID: "sid-live", Status: "listening"}}}
	if !anyLiveBusRow([]v2.SessionRecord{rec}, bus) {
		t.Fatal("archived cache identity did not match its live bus row")
	}
	wantObs := occupant.Observation{Status: occupant.Occupied, Tool: "claude", SID: "sid-live", Pane: herdrcli.Pane{PaneID: "pane-new", TerminalID: "term-new"}}
	obs, ok := relocateRows([]v2.SessionRecord{rec}, herdrState{}, bus,
		func(id string) occupant.Observation {
			if id != "pane-old" {
				t.Fatalf("alias probe id = %q", id)
			}
			return wantObs
		},
		func(string) occupant.Observation { return occupant.Observation{Status: occupant.PaneGone} },
	)
	if !ok || obs.Pane.PaneID != "pane-new" {
		t.Fatalf("archived relocation = (%+v, %t)", obs, ok)
	}
	got := buildCacheCandidates(projection(t, rec), map[string]paneObservation{
		"pane-old": {Occupant: obs, Bus: hcomidentity.Result{Name: "worker-name", SessionID: "sid-live", PaneID: "pane-new", Verified: true}, BusStatus: "listening"},
	}, now, time.Minute)
	if len(got) != 1 || got[0].kind != "stamp" || got[0].row.State != v2.StateSeated || got[0].row.Seat == nil || got[0].row.Seat.PaneID != "pane-new" || got[0].row.Label != "worker" || got[0].row.Cache.Liveness != "listening" {
		t.Fatalf("revival stamp = %+v", got)
	}
}

func TestCacheStampRelocatesByAliasWhenTranscriptProbeIsAmbiguous(t *testing.T) {
	rec := seatRow("guid-live", "worker", "pane-old", "worker-name", "sid-live", "2026-08-24T09:59:00Z")
	bus := busState{available: true, roster: []hcomidentity.Row{{Name: "worker-name", SessionID: "sid-live", Status: "listening"}}}
	obs, ok := relocateRows([]v2.SessionRecord{rec}, herdrState{}, bus,
		func(string) occupant.Observation {
			return occupant.Observation{Status: occupant.Unprobeable, Pane: herdrcli.Pane{PaneID: "pane-new", TerminalID: "term-new"}}
		},
		func(string) occupant.Observation { return occupant.Observation{Status: occupant.PaneGone} },
	)
	if !ok || obs.Status != occupant.Occupied || obs.Pane.PaneID != "pane-new" || obs.SID != "sid-live" || obs.Tool != "codex" {
		t.Fatalf("alias relocation = (%+v, %t)", obs, ok)
	}
}

func TestCacheStampLiveBusWithoutRelocationNeverDies(t *testing.T) {
	rec := seatRow("guid-live", "worker", "pane-old", "worker-name", "sid-live", "2026-08-24T09:59:00Z")
	bus := busState{available: true, roster: []hcomidentity.Row{{Name: "worker-name", SessionID: "sid-live", Status: "blocked"}}}
	if !anyLiveBusRow([]v2.SessionRecord{rec}, bus) {
		t.Fatal("live blocked hcom row was not recognized as stronger liveness evidence")
	}
	got := buildCacheCandidates(projection(t, rec), map[string]paneObservation{
		"pane-old": {Occupant: occupant.Observation{Status: occupant.Unprobeable}},
	}, time.Now().UTC(), time.Minute)
	if len(got) != 0 {
		t.Fatalf("live-but-unrelocated candidates = %+v, want retry without death", got)
	}
}

func TestObservePanesEnforcesAllChannelsBeforeDeath(t *testing.T) {
	rec := seatRow("guid-live", "worker", "pane-old", "worker-name", "sid-live", "2026-08-24T09:59:00Z")
	proj := projection(t, rec)
	row := hcomidentity.Row{Name: "worker-name", SessionID: "sid-live", Status: "listening"}
	bus := busState{available: true, rows: map[string]hcomidentity.Row{"worker-name": row}, roster: []hcomidentity.Row{row}}
	hd := herdrState{available: true, byTerm: map[string]herdrcli.Pane{}, procs: map[string]herdrcli.ProcessInfo{}}

	t.Run("live bus and failed relocation suppress death", func(t *testing.T) {
		observed := observePanesWithAliasProbe(proj, hd, bus, func(string) occupant.Observation {
			return occupant.Observation{Status: occupant.PaneGone}
		})
		got := buildCacheCandidates(proj, observed, time.Now().UTC(), time.Minute)
		if len(got) != 0 {
			t.Fatalf("candidates = %+v, want live row left untouched", got)
		}
	})

	t.Run("alias relocation stamps current pane", func(t *testing.T) {
		observed := observePanesWithAliasProbe(proj, hd, bus, func(string) occupant.Observation {
			return occupant.Observation{Status: occupant.Occupied, Tool: "codex", SID: "sid-live", Pane: herdrcli.Pane{PaneID: "pane-new", TerminalID: "term-new"}}
		})
		got := buildCacheCandidates(proj, observed, time.Now().UTC(), time.Minute)
		if len(got) != 1 || got[0].kind != "stamp" || got[0].row.Seat == nil || got[0].row.Seat.PaneID != "pane-new" {
			t.Fatalf("relocated candidates = %+v", got)
		}
	})
}

func TestObservePanesBusFailureCannotAgreeToDeath(t *testing.T) {
	rec := seatRow("guid-live", "worker", "pane-old", "worker-name", "sid-live", "2026-08-24T09:59:00Z")
	proj := projection(t, rec)
	hd := herdrState{available: true, byTerm: map[string]herdrcli.Pane{}, procs: map[string]herdrcli.ProcessInfo{}}
	observed := observePanesWithAliasProbe(proj, hd, busState{err: errors.New("hcom unavailable")}, func(string) occupant.Observation {
		return occupant.Observation{Status: occupant.PaneGone}
	})
	got := buildCacheCandidatesWithHealth(proj, observed, time.Now().UTC(), time.Minute, false)
	if len(got) != 0 {
		t.Fatalf("candidates = %+v, want no destructive write while bus channel is unavailable", got)
	}
}

func TestCacheStampLiveDedupeLoserUsesRecoverableDeadPath(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	proj := projection(t,
		seatRow("guid-winner", "winner", "pane-1", "winner-name", "sid-winner", "2026-08-24T09:59:00Z"),
		seatRow("guid-live-loser", "loser", "pane-1", "loser-name", "sid-loser", "2026-08-24T09:58:00Z"),
	)
	winnerRow := hcomidentity.Row{Name: "winner-name", SessionID: "sid-winner", Status: "listening"}
	loserRow := hcomidentity.Row{Name: "loser-name", SessionID: "sid-loser", Status: "listening"}
	bus := busState{
		available: true,
		rows:      map[string]hcomidentity.Row{"winner-name": winnerRow, "loser-name": loserRow},
		roster:    []hcomidentity.Row{winnerRow, loserRow},
	}
	hd := herdrState{available: true, byTerm: map[string]herdrcli.Pane{}, procs: map[string]herdrcli.ProcessInfo{}}
	observed := observePanesWithAliasProbe(proj, hd, bus, func(string) occupant.Observation {
		return occupant.Observation{Status: occupant.Occupied, Tool: "codex", SID: "sid-winner", Pane: herdrcli.Pane{PaneID: "pane-new", TerminalID: "term-new"}}
	})
	got := buildCacheCandidates(proj, observed, now, time.Minute)
	if len(got) != 2 || got[0].kind != "dead" || got[0].guid != "guid-live-loser" || got[0].row.State != v2.StateUnseated || got[0].row.Cache == nil || got[0].row.Cache.Liveness != "dead" || got[1].kind != "stamp" {
		t.Fatalf("candidates = %+v, want recoverable dead loser then winner stamp", got)
	}
}

func TestLiveBusRowUsesHcomJoinedClassification(t *testing.T) {
	rec := seatRow("guid-worker", "worker", "pane-1", "worker-name", "sid-worker", "2026-08-24T09:59:00Z")
	joinedFalse := false
	for _, row := range []hcomidentity.Row{
		{Name: "worker-name", SessionID: "sid-worker", Status: "closed"},
		{Name: "worker-name", SessionID: "sid-worker", Status: "DEAD"},
		{Name: "worker-name", SessionID: "sid-worker", Status: "listening", Joined: &joinedFalse},
	} {
		if got, ok := liveBusRow(rec, busState{available: true, roster: []hcomidentity.Row{row}}); ok {
			t.Fatalf("row %+v classified live as %+v", row, got)
		}
	}
}

// Reviewer demonstration (not part of the unit): a live seat whose recorded
// bus name was recycled to a different joined agent while its recorded sid is
// still live under a new name. The amendment's death rule is disjunctive —
// "no live hcom row for that session id or name" — so death must be vetoed.
func TestReviewConflictingBusCorrelatesStillVetoDeath(t *testing.T) {
	rec := seatRow("guid-w", "worker", "pane-1", "worker-a", "S1", "2026-08-24T09:59:00Z")
	roster := []hcomidentity.Row{
		{Name: "worker-a", SessionID: "S2", Status: "listening"},
		{Name: "worker-b", SessionID: "S1", Status: "listening"},
	}
	bus := busState{available: true, roster: roster}
	if _, live := liveBusRow(rec, bus); !live {
		t.Errorf("liveBusRow: live sid row on the bus did not veto death")
	}
	hd := herdrState{available: true, byTerm: map[string]herdrcli.Pane{}, procs: map[string]herdrcli.ProcessInfo{}}
	observed := observePanesWithAliasProbe(projection(t, rec), hd, bus, func(string) occupant.Observation {
		return occupant.Observation{Status: occupant.PaneGone}
	})
	got := buildCacheCandidates(projection(t, rec), observed, time.Now().UTC(), time.Minute)
	if len(got) != 0 {
		t.Errorf("candidates = %+v, want live seat left untouched despite ambiguous bus correlates", got)
	}
}

func TestOccupiedForeignSIDAliasDoesNotRelocate(t *testing.T) {
	rec := seatRow("guid-live", "worker", "pane-old", "worker-name", "sid-live", "2026-08-24T09:59:00Z")
	row := hcomidentity.Row{Name: "worker-name", SessionID: "sid-live", Status: "listening"}
	bus := busState{available: true, roster: []hcomidentity.Row{row}}
	hd := herdrState{available: true, byTerm: map[string]herdrcli.Pane{}, procs: map[string]herdrcli.ProcessInfo{}}
	proj := projection(t, rec)
	observed := observePanesWithAliasProbe(proj, hd, bus, func(string) occupant.Observation {
		return occupant.Observation{Status: occupant.Occupied, Tool: "codex", SID: "sid-foreign", Pane: herdrcli.Pane{PaneID: "pane-new", TerminalID: "term-new"}}
	})
	got := buildCacheCandidates(proj, observed, time.Now().UTC(), time.Minute)
	if len(got) != 0 {
		t.Fatalf("candidates = %+v, want foreign occupant rejected and live row left untouched", got)
	}
}

func TestObservedStampBypassesFrozenBindingLegality(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	initial := seatRow("guid-worker", "worker", "pane-1", "old-name", "sid-1", "2026-08-24T09:59:00Z")
	initial.Bindings = []v2.BindingFact{
		{ID: "seat-binding", Field: v2.BindingFieldSeat, Seat: &v2.BindingSeat{Kind: "herdr", TerminalID: "term-1", PaneID: "pane-1"}, EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: initial.RecordedAt},
		{ID: "bus-binding", Field: v2.BindingFieldHcomName, Value: "old-name", EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: initial.RecordedAt},
	}
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) { return []v2.SessionRecord{initial}, nil })
	if err != nil || len(outcomes) != 1 || outcomes[0].Status != registry.WriteApplied {
		t.Fatalf("seed write = %+v, %v", outcomes, err)
	}
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cands := buildCacheCandidates(proj, livePane("pane-1", "sid-1", "new-name", "active"), time.Now().UTC(), time.Minute)
	summary := applyCandidates(path, cands, &bytes.Buffer{})
	if summary.Applied != 1 || summary.Refused != 0 {
		t.Fatalf("summary = %+v, want legality-free observed write", summary)
	}
}

func projection(t *testing.T, rows ...v2.SessionRecord) *v2.Projection {
	t.Helper()
	var raw bytes.Buffer
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		raw.Write(encoded)
		raw.WriteByte('\n')
	}
	proj, err := v2.Load(&raw, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return proj
}

func seatRow(guid, label, pane, hcomName, sid, observedAt string) v2.SessionRecord {
	rec := v2.SessionRecord{
		Kind: v2.KindSession, GUID: guid, Event: "seated", RecordedAt: observedAt,
		State: v2.StateSeated, Label: label, Role: "worker", Tool: "codex",
		Seat: &v2.Seat{Kind: "herdr", TerminalID: "term-1", PaneID: pane, HcomName: hcomName},
	}
	if sid != "" {
		rec.SIDs = []v2.SID{{SID: sid, ObservedAt: observedAt, Source: "observer"}}
	}
	return rec
}

func livePane(pane, sid, name, status string) map[string]paneObservation {
	return map[string]paneObservation{
		pane: {
			Occupant:  occupant.Observation{Pane: herdrcli.Pane{PaneID: pane, TerminalID: "term-1"}, Tool: "codex", SID: sid, Status: occupant.Occupied},
			Bus:       hcomidentity.Result{Name: name, SessionID: sid, PaneID: pane, Verified: true},
			BusStatus: status,
		},
	}
}
