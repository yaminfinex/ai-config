package liveness

import "testing"

func TestEvaluateEvidenceMatrix(t *testing.T) {
	tests := []struct {
		name     string
		input    Input
		class    VerdictClass
		cause    CauseClass
		advisory CauseClass
		advice   string
	}{
		{name: "holder exit", input: Input{Holder: Signal{State: StateDead, ObservedVia: "sidecar_wait"}}, class: VerdictPositiveDeath, cause: CauseHolderExited},
		{name: "pane absent same epoch", input: Input{Pane: Signal{State: StateDead, ObservedVia: "socket_snapshot"}, PaneEpoch: EpochSame}, class: VerdictPositiveDeath, cause: CausePaneGoneSameEpoch},
		{name: "pane absent unknown epoch", input: Input{Pane: Signal{State: StateDead, ObservedVia: "one_shot_snapshot"}, PaneEpoch: EpochUnknown}, class: VerdictObservationGap, cause: CauseEpochUncertain},
		{name: "single empty foreground snapshot", input: Input{Pane: Signal{State: StateAlive, ObservedVia: "pane_snapshot"}, Process: Signal{State: StateDead, ObservedVia: "process_info"}}, class: VerdictObservationGap, cause: CauseClass("possible_pane_husk"), advisory: CauseClass("possible_pane_husk"), advice: "pane is alive but one foreground-process snapshot was empty; possible husk, no automated unseat"},
		{name: "dead pid stale listening row", input: Input{SeatKind: "process", Process: Signal{State: StateDead, ObservedVia: "pid_probe"}, BusRow: BusPresent, BusObservedVia: "bus_roster", Keepalive: KeepaliveStarved}, class: VerdictPositiveDeath, cause: CauseDeadPIDStaleBusRow},
		{name: "dead pid fresh listening row", input: Input{SeatKind: "process", Process: Signal{State: StateDead, ObservedVia: "pid_probe"}, BusRow: BusPresent, BusObservedVia: "bus_roster", Keepalive: KeepaliveFresh}, class: VerdictObservationGap, cause: CauseInsufficientEvidence},
		{name: "dead pid unavailable bus", input: Input{SeatKind: "process", Process: Signal{State: StateDead, ObservedVia: "pid_probe"}, BusRow: BusUnavailable}, class: VerdictObservationGap, cause: CauseObservationUnavailable},
		{name: "foreign pane survives tracker gap", input: Input{Pane: Signal{State: StateAlive, ObservedVia: "pane_snapshot"}, Process: Signal{State: StateAlive, ObservedVia: "process_info"}}, class: VerdictAlive, cause: CauseLiveEvidence},
		{name: "live holder starved keepalive", input: Input{Holder: Signal{State: StateAlive, ObservedVia: "holder_wait"}, BusRow: BusPresent, BusObservedVia: "bus_roster", Keepalive: KeepaliveStarved}, class: VerdictAlive, cause: CauseLiveEvidence, advisory: CauseKeepaliveStarvation, advice: "holder alive; bus keepalive is starved"},
		{name: "live holder missing roster row", input: Input{Holder: Signal{State: StateAlive, ObservedVia: "holder_wait"}, BusRow: BusAbsent, BusObservedVia: "bus_roster", Keepalive: KeepaliveStarved}, class: VerdictAlive, cause: CauseLiveEvidence, advisory: CauseKeepaliveStarvation, advice: "holder alive; expected bus roster row is absent"},
		{name: "absence only", input: Input{Pane: Signal{State: StateUnknown}, Process: Signal{State: StateUnknown}, BusRow: BusAbsent}, class: VerdictObservationGap, cause: CauseInsufficientEvidence},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Evaluate(test.input)
			if got.Class != test.class || got.Cause != test.cause {
				t.Fatalf("verdict = %+v, want class=%s cause=%s", got, test.class, test.cause)
			}
			if test.advisory == "" && got.Advisory != nil {
				t.Fatalf("unexpected advisory: %+v", got.Advisory)
			}
			if test.advisory != "" && (got.Advisory == nil || got.Advisory.Cause != test.advisory || got.Advisory.Detail != test.advice) {
				t.Fatalf("advisory = %+v, want cause=%s", got.Advisory, test.advisory)
			}
		})
	}
}

func TestEvidenceConflictNeverProducesDeath(t *testing.T) {
	got := Evaluate(Input{
		Holder:    Signal{State: StateAlive, ObservedVia: "holder_wait"},
		Pane:      Signal{State: StateDead, ObservedVia: "socket_snapshot"},
		PaneEpoch: EpochSame,
	})
	if got.Class != VerdictObservationGap || got.Cause != CauseEvidenceConflict {
		t.Fatalf("conflicting evidence verdict = %+v; removing the conflict guard must fail this test", got)
	}
}
