package registry

import (
	"path/filepath"
	"strings"
	"testing"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestLockedWriteRefusesInvalidDurableMission(t *testing.T) {
	tests := []struct {
		name        string
		mission     v2.Mission
		wantErrText string
	}{
		{"cwd source", v2.Mission{Slug: "alpha", Source: "cwd"}, "invalid durable mission source"},
		{"marker source", v2.Mission{Slug: "alpha", Source: "marker"}, "invalid durable mission source"},
		{"empty slug", v2.Mission{Source: "explicit"}, "mission membership without a slug"},
		{"invalid slug", v2.Mission{Slug: "bad--slug", Source: "explicit"}, "invalid mission membership"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcomes, err := UpdateLocked(filepath.Join(t.TempDir(), "registry.jsonl"), func(LockedUpdate) ([]v2.SessionRecord, error) {
				return []v2.SessionRecord{{GUID: "guid-invalid-mission", Event: "registered", State: v2.StateSeated, Mission: &tt.mission}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := SingleOutcome(outcomes)
			if err != nil || outcome.Status != WriteRefused || outcome.Err() == nil {
				t.Fatalf("outcome=%+v err=%v, want refused", outcome, err)
			}
			if !strings.Contains(outcome.Err().Error(), tt.wantErrText) {
				t.Fatalf("outcome error = %q, want text %q", outcome.Err(), tt.wantErrText)
			}
		})
	}
}

func TestMissionCarriesAcrossSameGUIDSuccessors(t *testing.T) {
	base := missionProjection(t, `{"kind":"session","guid":"guid-live","event":"registered","recorded_at":"2026-07-15T00:00:00Z","state":"seated","label":"worker","role":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"term-live"},"mission":{"slug":"alpha","source":"explicit"}}`)
	current := V2ByGUID(base, "guid-live")
	if current == nil {
		t.Fatal("missing seed row")
	}
	tests := []struct {
		name  string
		event string
		edit  func(*v2.SessionRecord)
	}{
		{name: "spawn or bus registration enrichment", event: "registered"},
		{name: "seat refresh", event: "seated"},
		{name: "recognition", event: "recognised"},
		{name: "reconciliation", event: "reconciled"},
		{name: "cull or dead-seat observation", event: "unseated", edit: func(row *v2.SessionRecord) { row.State, row.Seat = v2.StateUnseated, nil }},
		{name: "rename", event: "labelled", edit: func(row *v2.SessionRecord) { row.Label = "renamed" }},
		{name: "atomic label transfer", event: "label_transferred", edit: func(row *v2.SessionRecord) { row.Label = "transferred" }},
		{name: "retire", event: "retired", edit: func(row *v2.SessionRecord) { row.State, row.Label, row.Seat = v2.StateRetired, "", nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := *current
			row.Event, row.RecordedAt, row.Mission = tt.event, "", nil
			if tt.edit != nil {
				tt.edit(&row)
			}
			normalized, _, err := normalizeSessionAppend(base, row)
			if err != nil {
				t.Fatal(err)
			}
			assertMission(t, normalized.Mission, "alpha")
		})
	}
}

func TestMissionCarriesAcrossReopenAndResume(t *testing.T) {
	t.Run("reopen", func(t *testing.T) {
		projection := missionProjection(t, `{"kind":"session","guid":"guid-retired","event":"retired","recorded_at":"2026-07-15T00:00:00Z","state":"retired","role":"worker","tool":"codex","mission":{"slug":"alpha","source":"explicit"}}`)
		row := *V2ByGUID(projection, "guid-retired")
		row.Event, row.RecordedAt, row.State, row.Mission = "reopened", "", v2.StateUnseated, nil
		normalized, _, err := normalizeSessionAppend(projection, row)
		if err != nil {
			t.Fatal(err)
		}
		assertMission(t, normalized.Mission, "alpha")
	})

	t.Run("resume registration", func(t *testing.T) {
		projection := missionProjection(t, `{"kind":"session","guid":"guid-resume","event":"unseated","recorded_at":"2026-07-15T00:00:00Z","state":"unseated","label":"worker","role":"worker","tool":"codex","mission":{"slug":"alpha","source":"explicit"}}`)
		row := *V2ByGUID(projection, "guid-resume")
		row.Event, row.RecordedAt, row.State, row.Mission = "registered", "", v2.StateSeated, nil
		row.Seat = &v2.Seat{Kind: "herdr", TerminalID: "term-resumed"}
		normalized, _, err := normalizeSessionAppend(projection, row)
		if err != nil {
			t.Fatal(err)
		}
		assertMission(t, normalized.Mission, "alpha")
	})
}

func TestMissionDoesNotFollowForkOrAdoption(t *testing.T) {
	projection := missionProjection(t, `{"kind":"session","guid":"guid-parent","event":"registered","recorded_at":"2026-07-15T00:00:00Z","state":"seated","label":"worker","role":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"term-parent"},"mission":{"slug":"alpha","source":"explicit"}}`)

	t.Run("fork child", func(t *testing.T) {
		row := v2.SessionRecord{GUID: "guid-fork", Event: "registered", State: v2.StateSeated, Lineage: v2.Lineage{ForkedFrom: "guid-parent"}}
		normalized, _, err := normalizeSessionAppend(projection, row)
		if err != nil {
			t.Fatal(err)
		}
		if normalized.Mission != nil {
			t.Fatalf("fork inherited mission: %+v", normalized.Mission)
		}
	})

	t.Run("adoption replacement", func(t *testing.T) {
		row := v2.SessionRecord{GUID: "guid-replacement", Event: "registered", State: v2.StateSeated}
		normalized, _, err := normalizeSessionAppend(projection, row)
		if err != nil {
			t.Fatal(err)
		}
		if normalized.Mission != nil {
			t.Fatalf("replacement inherited mission: %+v", normalized.Mission)
		}
	})

	t.Run("adoption source release", func(t *testing.T) {
		row := *V2ByGUID(projection, "guid-parent")
		row.Event, row.RecordedAt, row.State, row.Label, row.Seat = "adoption_source_released", "", v2.StateUnseated, "", nil
		row.CloseResult = "adopted"
		row.CloseReason = "seat superseded by replacement process in the same pane"
		normalized, _, err := normalizeSessionAppend(projection, row)
		if err != nil {
			t.Fatal(err)
		}
		if normalized.Mission != nil {
			t.Fatalf("adoption source retained mission: %+v", normalized.Mission)
		}
	})
}

func TestMissionTransfersAcrossObserverProvenTurnover(t *testing.T) {
	projection := missionProjection(t, `{"kind":"session","guid":"guid-old","event":"registered","recorded_at":"2026-07-15T00:00:00Z","state":"seated","label":"worker","role":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"term-shared"},"mission":{"slug":"alpha","source":"explicit"}}`)

	child := v2.SessionRecord{GUID: "guid-child", Event: "registered", State: v2.StateSeated, Lineage: v2.Lineage{ClearedFrom: "guid-old"}}
	normalizedChild, _, err := normalizeSessionAppend(projection, child)
	if err != nil {
		t.Fatal(err)
	}
	assertMission(t, normalizedChild.Mission, "alpha")

	old := *V2ByGUID(projection, "guid-old")
	old.Event, old.RecordedAt, old.State, old.Seat = "unseated", "", v2.StateUnseated, nil
	old.Lineage.DisplacedBy = "guid-child"
	normalizedOld, _, err := normalizeSessionAppend(projection, old)
	if err != nil {
		t.Fatal(err)
	}
	if normalizedOld.Mission != nil {
		t.Fatalf("displaced row retained mission: %+v", normalizedOld.Mission)
	}
}

func TestOrdinarySuccessorCannotSilentlyChangeMission(t *testing.T) {
	projection := missionProjection(t, `{"kind":"session","guid":"guid-live","event":"registered","recorded_at":"2026-07-15T00:00:00Z","state":"seated","label":"worker","role":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"term-live"},"mission":{"slug":"alpha","source":"explicit"}}`)
	row := *V2ByGUID(projection, "guid-live")
	row.Event = "labelled"
	row.Label = "renamed"
	row.Mission = &v2.Mission{Slug: "beta", Source: "explicit"}
	if _, _, err := normalizeSessionAppend(projection, row); err == nil || !strings.Contains(err.Error(), "cannot change explicit mission membership") {
		t.Fatalf("err = %v, want protected mission mutation refusal", err)
	}
}

func missionProjection(t *testing.T, rows ...string) *v2.Projection {
	t.Helper()
	projection, err := v2.Load(strings.NewReader(strings.Join(rows, "\n")+"\n"), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func assertMission(t *testing.T, mission *v2.Mission, slug string) {
	t.Helper()
	if mission == nil || mission.Slug != slug || mission.Source != "explicit" {
		t.Fatalf("mission = %+v, want slug=%q source=explicit", mission, slug)
	}
}
