package repaircmd

import (
	"context"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestSeasonDuplicateSeatedAftermathUsesExactRepairThenOrdinaryReseat(t *testing.T) {
	service, path := testService(t)
	verified := true
	mustUpdateRegistry(t, path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			GUID: "guid-duplicate", Event: "seated", State: v2.StateSeated, Label: "duplicate", Tool: "codex",
			Seat: &v2.Seat{Kind: "herdr", Node: tx.NodeID, TerminalID: "terminal-live", PaneID: "pane-live", HcomName: "bus-duplicate", HcomVerified: &verified, Namespace: "/bus"},
			Bindings: []v2.BindingFact{
				{ID: "duplicate-seat", Field: v2.BindingFieldSeat, EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: "2026-07-17T00:00:00Z", Seat: &v2.BindingSeat{Kind: "herdr", Node: tx.NodeID, TerminalID: "terminal-live", PaneID: "pane-live", Namespace: "/bus"}},
				{ID: "duplicate-bus", Field: v2.BindingFieldHcomName, Value: "bus-duplicate", EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: "2026-07-17T00:00:00Z"},
			},
		}}, nil
	})
	if _, err := service.Execute(context.Background(), Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldHcomName, Value: "bus-new"}); err != nil {
		t.Fatal(err)
	}
	projection, _ := v2.LoadFile(path, v2.LoadOptions{})
	duplicate := sessionForTest(projection, "guid-duplicate")
	if duplicate == nil || duplicate.Seat == nil || duplicate.Seat.HcomName != "bus-duplicate" {
		t.Fatalf("single-row repair mutated duplicate = %+v", duplicate)
	}
	mustUpdateRegistry(t, path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		current := registry.V2ByGUID(tx.Projection, "guid-duplicate")
		return []v2.SessionRecord{{GUID: current.GUID, Event: "unseated", State: v2.StateUnseated, CloseResult: "reconciled", CloseReason: "ordinary re-seat corridor detached duplicate"}}, nil
	})
	projection, _ = v2.LoadFile(path, v2.LoadOptions{})
	if got := sessionForTest(projection, "guid-duplicate"); got == nil || got.State != v2.StateUnseated {
		t.Fatalf("duplicate after ordinary re-seat = %+v", got)
	}
}

func TestSeasonRetiredRowOwningLiveSIDIsSupersededByAttestedCurrentBinding(t *testing.T) {
	service, path := testService(t)
	mustUpdateRegistry(t, path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-retired", Event: "retired", State: v2.StateRetired, Tool: "codex", SIDs: []v2.SID{{SID: "sid-live", Source: v2.EvidenceHarvest}}}}, nil
	})
	if _, err := service.Execute(context.Background(), Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldSID, Value: "sid-live"}); err != nil {
		t.Fatal(err)
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	repaired := sessionForTest(projection, "guid-repair")
	retired := sessionForTest(projection, "guid-retired")
	if repaired == nil || repaired.State != v2.StateSeated || len(repaired.SIDs) == 0 || repaired.SIDs[len(repaired.SIDs)-1].SID != "sid-live" {
		t.Fatalf("repaired current sid = %+v", repaired)
	}
	if retired == nil || retired.State != v2.StateRetired || len(retired.SIDs) != 1 || retired.SIDs[0].SID != "sid-live" {
		t.Fatalf("retired history = %+v", retired)
	}
	fact, status := registry.LatestSufficientBinding(*repaired, v2.BindingFieldSID, registry.LiveEvidenceAbsent)
	if status != registry.BindingSelected || fact.Value != "sid-live" || fact.EvidenceClass != v2.EvidenceAttested {
		t.Fatalf("repair-scoped sid adjudication = %+v status=%q", fact, status)
	}
}

func sessionForTest(projection *v2.Projection, guid string) *v2.SessionRecord {
	for _, rec := range projection.Sessions() {
		if rec.GUID == guid {
			copy := rec
			return &copy
		}
	}
	return nil
}
