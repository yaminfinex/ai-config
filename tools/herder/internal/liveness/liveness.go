// Package liveness owns the evidence rules used to classify a seated holder.
// It deliberately knows nothing about observer availability: callers provide
// direct observations and every caller receives the same verdict.
package liveness

import (
	"sort"
	"strings"
)

type State string

const (
	StateUnknown State = "unknown"
	StateAlive   State = "alive"
	StateDead    State = "dead"
)

type EpochRelation string

const (
	EpochUnknown EpochRelation = "unknown"
	EpochSame    EpochRelation = "same"
	EpochChanged EpochRelation = "changed"
)

type BusRowState string

const (
	BusUnavailable BusRowState = "unavailable"
	BusAbsent      BusRowState = "absent"
	BusPresent     BusRowState = "present"
)

type KeepaliveState string

const (
	KeepaliveUnknown KeepaliveState = "unknown"
	KeepaliveFresh   KeepaliveState = "fresh"
	KeepaliveStarved KeepaliveState = "starved"
)

type Signal struct {
	State       State
	ObservedVia string
}

type Input struct {
	SeatKind       string
	Holder         Signal
	Pane           Signal
	PaneEpoch      EpochRelation
	Process        Signal
	BusRow         BusRowState
	BusObservedVia string
	Keepalive      KeepaliveState
}

type VerdictClass string

const (
	VerdictAlive          VerdictClass = "alive"
	VerdictPositiveDeath  VerdictClass = "positive_death"
	VerdictObservationGap VerdictClass = "observation_gap"
)

type CauseClass string

const (
	CauseLiveEvidence           CauseClass = "live_evidence"
	CauseHolderExited           CauseClass = "holder_exited"
	CausePaneGoneSameEpoch      CauseClass = "pane_gone_same_epoch"
	CauseDeadPIDStaleBusRow     CauseClass = "dead_pid_stale_bus_row"
	CausePossiblePaneHusk       CauseClass = "possible_pane_husk"
	CauseKeepaliveStarvation    CauseClass = "keepalive_starvation"
	CauseEvidenceConflict       CauseClass = "evidence_conflict"
	CauseInsufficientEvidence   CauseClass = "insufficient_evidence"
	CauseEpochUncertain         CauseClass = "epoch_uncertain"
	CauseObservationUnavailable CauseClass = "observation_unavailable"
)

type Advisory struct {
	Cause       CauseClass
	Detail      string
	ObservedVia []string
}

type Verdict struct {
	Class       VerdictClass
	Cause       CauseClass
	Evidence    []string
	ObservedVia []string
	Advisory    *Advisory
}

func Evaluate(in Input) Verdict {
	deathCause, deathEvidence, deathVia := deathFacts(in)
	liveEvidence, liveVia := liveFacts(in, deathCause != "")

	if len(liveEvidence) > 0 && deathCause != "" {
		return Verdict{
			Class:       VerdictObservationGap,
			Cause:       CauseEvidenceConflict,
			Evidence:    appendUnique(liveEvidence, deathEvidence...),
			ObservedVia: appendUnique(liveVia, deathVia...),
		}
	}
	if deathCause != "" {
		return Verdict{
			Class:       VerdictPositiveDeath,
			Cause:       deathCause,
			Evidence:    deathEvidence,
			ObservedVia: deathVia,
		}
	}
	if in.SeatKind != "process" && in.Pane.State == StateAlive && in.Process.State == StateDead {
		via := appendUnique(nil, in.Pane.ObservedVia, in.Process.ObservedVia)
		return Verdict{
			Class:       VerdictObservationGap,
			Cause:       CausePossiblePaneHusk,
			Evidence:    []string{"pane_present", "foreground_process_snapshot_empty"},
			ObservedVia: via,
			Advisory: &Advisory{
				Cause:       CausePossiblePaneHusk,
				Detail:      "pane is alive but one foreground-process snapshot was empty; possible husk, no automated unseat",
				ObservedVia: via,
			},
		}
	}
	if len(liveEvidence) > 0 {
		verdict := Verdict{
			Class:       VerdictAlive,
			Cause:       CauseLiveEvidence,
			Evidence:    liveEvidence,
			ObservedVia: liveVia,
		}
		if in.Keepalive == KeepaliveStarved {
			via := appendUnique(liveVia, in.BusObservedVia)
			detail := "holder alive; bus keepalive is starved"
			if in.BusRow == BusAbsent {
				detail = "holder alive; expected bus roster row is absent"
			}
			verdict.Advisory = &Advisory{
				Cause:       CauseKeepaliveStarvation,
				Detail:      detail,
				ObservedVia: via,
			}
		}
		return verdict
	}

	cause := CauseInsufficientEvidence
	if in.Pane.State == StateDead && in.PaneEpoch != EpochSame {
		cause = CauseEpochUncertain
	} else if in.BusRow == BusUnavailable {
		cause = CauseObservationUnavailable
	}
	return Verdict{Class: VerdictObservationGap, Cause: cause, Evidence: gapFacts(in), ObservedVia: allVia(in)}
}

func liveFacts(in Input, suppressPane bool) ([]string, []string) {
	var evidence, via []string
	if in.Holder.State == StateAlive {
		evidence = append(evidence, "holder_alive")
		via = append(via, in.Holder.ObservedVia)
	}
	if in.Pane.State == StateAlive && !suppressPane {
		evidence = append(evidence, "pane_present")
		via = append(via, in.Pane.ObservedVia)
	}
	if in.Process.State == StateAlive {
		evidence = append(evidence, "process_alive")
		via = append(via, in.Process.ObservedVia)
	}
	return appendUnique(nil, evidence...), appendUnique(nil, via...)
}

func deathFacts(in Input) (CauseClass, []string, []string) {
	if in.Holder.State == StateDead {
		return CauseHolderExited, []string{"holder_exited"}, appendUnique(nil, in.Holder.ObservedVia)
	}
	if in.Pane.State == StateDead && in.PaneEpoch == EpochSame {
		return CausePaneGoneSameEpoch, []string{"pane_absent", "epoch_unchanged"}, appendUnique(nil, in.Pane.ObservedVia)
	}
	if in.SeatKind == "process" && in.Process.State == StateDead && in.BusRow == BusPresent && in.Keepalive == KeepaliveStarved {
		return CauseDeadPIDStaleBusRow, []string{"pid_dead", "bus_row_present", "keepalive_starved"}, appendUnique(nil, in.Process.ObservedVia, in.BusObservedVia)
	}
	return "", nil, nil
}

func gapFacts(in Input) []string {
	var facts []string
	if in.Holder.State == StateUnknown {
		facts = append(facts, "holder_unknown")
	}
	if in.Pane.State == StateDead {
		facts = append(facts, "pane_absent", "pane_epoch_"+string(in.PaneEpoch))
	} else if in.Pane.State == StateUnknown {
		facts = append(facts, "pane_unknown")
	}
	if in.Process.State == StateDead {
		facts = append(facts, "process_dead_without_corroboration")
	} else if in.Process.State == StateUnknown {
		facts = append(facts, "process_unknown")
	}
	if in.BusRow == BusUnavailable {
		facts = append(facts, "bus_unavailable")
	} else if in.BusRow == BusAbsent {
		facts = append(facts, "bus_row_absent")
	}
	return appendUnique(nil, facts...)
}

func allVia(in Input) []string {
	return appendUnique(nil, in.Holder.ObservedVia, in.Pane.ObservedVia, in.Process.ObservedVia, in.BusObservedVia)
}

func appendUnique(base []string, values ...string) []string {
	seen := make(map[string]bool, len(base)+len(values))
	for _, value := range base {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = true
		}
	}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && !seen[value] {
			base = append(base, value)
			seen[value] = true
		}
	}
	sort.Strings(base)
	return base
}
