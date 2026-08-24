package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestAttestationWritersAreRemoved(t *testing.T) {
	_, _, err := normalizeSessionAppend(bindingProjection(t), attestedCorrectionPatch("binding-bus", v2.BindingFieldHcomName))
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("attested append err = %v, want removed-writer refusal", err)
	}
}

func TestTornAttestedAppendLeavesPriorRowAuthoritative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-torn", Event: "registered", State: v2.StateUnseated, Label: "stable", Tool: "codex"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"kind":"session","guid":"guid-torn","event":"attested_binding","attestations":[`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current := V2ByGUID(projection, "guid-torn")
	if current == nil || current.Event != "registered" || current.Label != "stable" {
		t.Fatalf("current after torn append = %+v", current)
	}
}

func attestedCorrectionPatch(bindingID, field string) v2.SessionRecord {
	verified := true
	return v2.SessionRecord{
		GUID: "guid-binding", Event: v2.EventAttestedBinding, State: v2.StateSeated,
		Seat: &v2.Seat{Kind: "herdr", Node: testNodeA, TerminalID: "terminal-live", PaneID: "pane-old", HcomName: "bus-repaired", HcomVerified: &verified, Namespace: "/bus"},
		Bindings: []v2.BindingFact{
			seatBinding("binding-seat", "pane-old"), busBinding("binding-bus", "bus-live"),
			{ID: "binding-correction", Field: v2.BindingFieldHcomName, Value: "bus-repaired", EvidenceClass: v2.EvidenceAttested, ObservedAt: "2026-07-17T00:01:00Z", AttestationID: "attestation-one"},
		},
		Attestations:      []v2.Attestation{{ID: "attestation-one", Operation: v2.AttestationRebind, GUID: "guid-binding", Field: v2.BindingFieldHcomName, Value: "bus-repaired", Statement: "explicit statement", PaneID: "pane-old", TerminalID: "terminal-live", ObservedAt: "2026-07-17T00:01:00Z"}},
		BindingTombstones: []v2.BindingTombstone{{BindingID: bindingID, Field: field, CorrectionBindingID: "binding-correction", AttestationID: "attestation-one", TombstonedAt: "2026-07-17T00:01:00Z"}},
	}
}

func TestBindingHistoryCarriesAcrossNonBindingAndLifecycleEvents(t *testing.T) {
	projection := bindingProjection(t)

	labelled, ok, err := normalizeSessionAppend(projection, v2.SessionRecord{
		GUID:  "guid-binding",
		Event: "labelled",
		Label: "renamed",
	})
	if err != nil || !ok {
		t.Fatalf("labelled normalize = ok %v err %v", ok, err)
	}
	assertBindingIDs(t, labelled.Bindings, "binding-seat", "binding-bus")

	unseated, ok, err := normalizeSessionAppend(projection, v2.SessionRecord{
		GUID:  "guid-binding",
		Event: "unseated",
		State: v2.StateUnseated,
	})
	if err != nil || !ok {
		t.Fatalf("unseated normalize = ok %v err %v", ok, err)
	}
	if unseated.Seat != nil {
		t.Fatalf("unseated seat = %+v, want nil", unseated.Seat)
	}
	assertBindingIDs(t, unseated.Bindings, "binding-seat", "binding-bus")
}

func TestAttestationAndTombstoneHistoriesCarryAcrossLifecycleEvents(t *testing.T) {
	corrected := attestedCorrectionPatch("binding-bus", v2.BindingFieldHcomName)
	encoded, err := json.Marshal(corrected)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := v2.Load(strings.NewReader(string(encoded)+"\n"), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		row  v2.SessionRecord
	}{
		{name: "label", row: v2.SessionRecord{GUID: corrected.GUID, Event: "labelled", Label: "renamed"}},
		{name: "mission", row: v2.SessionRecord{GUID: corrected.GUID, Event: "mission_joined", Mission: &v2.Mission{Slug: "repair-audit", Source: "explicit"}}},
		{name: "unseat", row: v2.SessionRecord{GUID: corrected.GUID, Event: "unseated", State: v2.StateUnseated}},
		{name: "retire", row: v2.SessionRecord{GUID: corrected.GUID, Event: "retired", State: v2.StateRetired}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, ok, err := normalizeSessionAppend(projection, tt.row)
			if err != nil || !ok {
				t.Fatalf("normalize = ok %v err %v", ok, err)
			}
			if !reflect.DeepEqual(next.Attestations, corrected.Attestations) {
				t.Fatalf("attestations changed across %s: got %+v want %+v", tt.row.Event, next.Attestations, corrected.Attestations)
			}
			if !reflect.DeepEqual(next.BindingTombstones, corrected.BindingTombstones) {
				t.Fatalf("tombstones changed across %s: got %+v want %+v", tt.row.Event, next.BindingTombstones, corrected.BindingTombstones)
			}
		})
	}
}

func TestBindingHistoryRefusesSeatedCoordinateChangeWithoutFact(t *testing.T) {
	projection := bindingProjection(t)
	verified := true
	_, _, err := normalizeSessionAppend(projection, v2.SessionRecord{
		GUID:  "guid-binding",
		Event: "reconciled",
		State: v2.StateSeated,
		Seat: &v2.Seat{
			Kind:         "herdr",
			Node:         testNodeA,
			TerminalID:   "terminal-live",
			PaneID:       "pane-new",
			HcomName:     "bus-live",
			HcomVerified: &verified,
			Namespace:    "/bus",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "binding fact") {
		t.Fatalf("coordinate mutation err = %v, want binding-fact refusal", err)
	}
}

func TestBindingHistoryAcceptsSeatedCoordinateChangeWithMatchingFact(t *testing.T) {
	projection := bindingProjection(t)
	verified := true
	row, ok, err := normalizeSessionAppend(projection, v2.SessionRecord{
		GUID:  "guid-binding",
		Event: "reconciled",
		State: v2.StateSeated,
		Seat: &v2.Seat{
			Kind:         "herdr",
			Node:         testNodeA,
			TerminalID:   "terminal-live",
			PaneID:       "pane-new",
			HcomName:     "bus-live",
			HcomVerified: &verified,
			Namespace:    "/bus",
		},
		Bindings: []v2.BindingFact{
			seatBinding("binding-seat", "pane-old"),
			busBinding("binding-bus", "bus-live"),
			seatBinding("binding-seat-new", "pane-new"),
		},
	})
	if err != nil || !ok {
		t.Fatalf("coordinate mutation normalize = ok %v err %v", ok, err)
	}
	assertBindingIDs(t, row.Bindings, "binding-seat", "binding-bus", "binding-seat-new")
}

func TestBindingHistoryRefusesRewritingPersistedEntry(t *testing.T) {
	projection := bindingProjection(t)
	mutated := seatBinding("binding-seat", "pane-forged")
	_, _, err := normalizeSessionAppend(projection, v2.SessionRecord{
		GUID:     "guid-binding",
		Event:    "labelled",
		Label:    "renamed",
		Bindings: []v2.BindingFact{mutated, busBinding("binding-bus", "bus-live")},
	})
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("history rewrite err = %v, want append-only refusal", err)
	}
}

func TestBindingHistoryRefusesMalformedAppendShapes(t *testing.T) {
	current := []v2.BindingFact{seatBinding("binding-seat", "pane-old"), busBinding("binding-bus", "bus-live")}
	tests := []struct {
		name  string
		patch []v2.BindingFact
		want  string
	}{
		{name: "shorter", patch: current[:1], want: "append-only"},
		{name: "reordered", patch: []v2.BindingFact{current[1], current[0]}, want: "append-only"},
		{name: "empty id", patch: append(cloneBindings(current), busBinding("", "bus-next")), want: "missing durable id"},
		{name: "duplicate id", patch: append(cloneBindings(current), busBinding("binding-bus", "bus-next")), want: "not unique"},
		{name: "empty evidence class", patch: func() []v2.BindingFact {
			fact := busBinding("binding-next", "bus-next")
			fact.EvidenceClass = ""
			return append(cloneBindings(current), fact)
		}(), want: "invalid evidence class"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := normalizeSessionAppend(bindingProjection(t), v2.SessionRecord{
				GUID:     "guid-binding",
				Event:    "labelled",
				Label:    "renamed",
				Bindings: tt.patch,
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("normalizeSessionAppend() err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBindingHistoryRefusesClearedOrDemotedSeatedBusProjection(t *testing.T) {
	for _, tt := range []struct {
		name     string
		busName  string
		verified *bool
	}{
		{name: "cleared", busName: "", verified: nil},
		{name: "demoted", busName: "bus-live", verified: boolPointer(false)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := normalizeSessionAppend(bindingProjection(t), v2.SessionRecord{
				GUID:  "guid-binding",
				Event: "reconciled",
				State: v2.StateSeated,
				Seat: &v2.Seat{
					Kind:         "herdr",
					Node:         testNodeA,
					TerminalID:   "terminal-live",
					PaneID:       "pane-old",
					HcomName:     tt.busName,
					HcomVerified: tt.verified,
					Namespace:    "/bus",
				},
				Bindings: []v2.BindingFact{seatBinding("binding-seat", "pane-old"), busBinding("binding-bus", "bus-live")},
			})
			if err == nil || !strings.Contains(err.Error(), "bus binding") {
				t.Fatalf("bus projection mutation err = %v, want bus-binding refusal", err)
			}
		})
	}
}

func TestCarrySeatFieldsDoesNotAliasProjectionSeat(t *testing.T) {
	projection := bindingProjection(t)
	row, ok, err := normalizeSessionAppend(projection, v2.SessionRecord{
		GUID:  "guid-binding",
		Event: "labelled",
		Label: "renamed",
	})
	if err != nil || !ok {
		t.Fatalf("labelled normalize = ok %v err %v", ok, err)
	}
	if _, err := stampSessionNode(row, testNodeB); err != nil {
		t.Fatal(err)
	}
	current := V2ByGUID(projection, "guid-binding")
	if current == nil || current.Seat == nil || current.Seat.Node != testNodeA {
		t.Fatalf("projection seat after stamping carried row = %+v, want original node %q", current, testNodeA)
	}
}

func bindingProjection(t *testing.T) *v2.Projection {
	t.Helper()
	row := v2.SessionRecord{
		Kind:       v2.KindSession,
		GUID:       "guid-binding",
		Event:      "seated",
		RecordedAt: "2026-07-17T00:00:00Z",
		Node:       testNodeA,
		State:      v2.StateSeated,
		Label:      "worker",
		Tool:       "codex",
		Seat: &v2.Seat{
			Kind:         "herdr",
			Node:         testNodeA,
			TerminalID:   "terminal-live",
			PaneID:       "pane-old",
			HcomName:     "bus-live",
			HcomVerified: boolPointer(true),
			Namespace:    "/bus",
		},
		Bindings: []v2.BindingFact{
			seatBinding("binding-seat", "pane-old"),
			busBinding("binding-bus", "bus-live"),
		},
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := v2.Load(strings.NewReader(string(encoded)+"\n"), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func seatBinding(id, pane string) v2.BindingFact {
	return v2.BindingFact{
		ID:            id,
		Field:         v2.BindingFieldSeat,
		EvidenceClass: v2.EvidenceLiveVerified,
		ObservedAt:    "2026-07-17T00:00:00Z",
		Seat: &v2.BindingSeat{
			Kind:       "herdr",
			Node:       testNodeA,
			TerminalID: "terminal-live",
			PaneID:     pane,
			Namespace:  "/bus",
		},
	}
}

func busBinding(id, name string) v2.BindingFact {
	return v2.BindingFact{
		ID:            id,
		Field:         v2.BindingFieldHcomName,
		Value:         name,
		EvidenceClass: v2.EvidenceLiveVerified,
		ObservedAt:    "2026-07-17T00:00:00Z",
	}
}

func assertBindingIDs(t *testing.T, facts []v2.BindingFact, want ...string) {
	t.Helper()
	if len(facts) != len(want) {
		t.Fatalf("binding count = %d, want %d: %+v", len(facts), len(want), facts)
	}
	for i := range want {
		if facts[i].ID != want[i] {
			t.Fatalf("binding %d id = %q, want %q", i, facts[i].ID, want[i])
		}
	}
}
