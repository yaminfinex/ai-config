package occupant

import (
	"testing"

	"ai-config/tools/herder/internal/registry/v2"
)

func TestResolveUsesTranscriptLineageNotSeatCoordinates(t *testing.T) {
	rows := []v2.SessionRecord{
		{GUID: "wrong-seat", Seat: &v2.Seat{PaneID: "pane-live"}, SIDs: []v2.SID{{SID: sidB}}},
		{GUID: "right-sid", Seat: &v2.Seat{PaneID: "pane-stale"}, SIDs: []v2.SID{{SID: sidA}}},
	}
	got := Resolve(Observation{Status: Occupied, SID: sidA}, rows, "")
	if got.Outcome.Status != Match || got.Row == nil || got.Row.GUID != "right-sid" {
		t.Fatalf("Resolve = %+v", got)
	}
}

func TestResolveClaimDisagreementIsPositiveMismatch(t *testing.T) {
	rows := []v2.SessionRecord{
		{GUID: "claimed", SIDs: []v2.SID{{SID: sidB}}},
		{GUID: "occupant", SIDs: []v2.SID{{SID: sidA}}},
	}
	got := Resolve(Observation{Status: Occupied, SID: sidA}, rows, "claimed")
	if got.Outcome.Status != PositiveMismatch || got.Row != nil {
		t.Fatalf("Resolve = %+v", got)
	}
}

func TestResolveDuplicateSIDIsAmbiguous(t *testing.T) {
	row := v2.SessionRecord{SIDs: []v2.SID{{SID: sidA}}}
	got := Resolve(Observation{Status: Occupied, SID: sidA}, []v2.SessionRecord{row, row}, "")
	if got.Outcome.Status != OutcomeAmbiguous || got.Matches != 2 {
		t.Fatalf("Resolve = %+v", got)
	}
}

func TestExactEvidenceRejectsCohortOnly(t *testing.T) {
	if ExactEvidence(Observation{Evidence: []Signal{SignalCohort, SignalEnvironGUID}}) {
		t.Fatal("cohort-class observation reported exact")
	}
	if !ExactEvidence(Observation{Evidence: []Signal{SignalCohort, SignalAgentSession}}) {
		t.Fatal("agent_session + artifact observation did not report exact")
	}
}
