package registry

import (
	"encoding/json"
	"strings"
	"testing"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

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
