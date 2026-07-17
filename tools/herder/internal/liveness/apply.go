package liveness

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

var (
	ErrNotPositiveDeath = errors.New("liveness applier requires a positive-death verdict")
	ErrSeatChanged      = errors.New("observed seat changed before liveness append")
)

type SeatAnchor struct {
	Kind       string
	Node       string
	TerminalID string
	PaneID     string
	PID        int
	Namespace  string
	HcomName   string
	HerdrEpoch string
}

func Anchor(seat *v2.Seat) SeatAnchor {
	if seat == nil {
		return SeatAnchor{}
	}
	return SeatAnchor{
		Kind: seat.Kind, Node: seat.Node, TerminalID: seat.TerminalID, PaneID: seat.PaneID,
		PID: seat.PID, Namespace: seat.Namespace, HcomName: seat.HcomName, HerdrEpoch: seat.HerdrEpoch,
	}
}

type ApplyResult struct {
	Status registry.WriteStatus
	Row    *v2.SessionRecord
}

func ApplyPositiveDeath(path, guid string, anchor SeatAnchor, verdict Verdict, observedAt time.Time, applier string) (ApplyResult, error) {
	if verdict.Class != VerdictPositiveDeath {
		return ApplyResult{Status: registry.WriteRefused}, ErrNotPositiveDeath
	}
	if guid == "" || strings.TrimSpace(applier) == "" {
		return ApplyResult{Status: registry.WriteRefused}, errors.New("liveness applier requires guid and provenance")
	}
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	var appended *v2.SessionRecord
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		current := registry.V2ByGUID(tx.Projection, guid)
		if current == nil {
			return nil, fmt.Errorf("liveness append target %s not found", guid)
		}
		if current.State != v2.StateSeated || current.Seat == nil {
			return nil, nil
		}
		if Anchor(current.Seat) != anchor {
			return nil, ErrSeatChanged
		}
		next := *current
		next.Event = "unseated"
		next.State = v2.StateUnseated
		next.RecordedAt = observedAt.UTC().Format(time.RFC3339)
		next.Seat = nil
		next.CloseResult = "observed_dead"
		next.CloseReason = formatReason(verdict)
		next.ObservedVia = formatObservedVia(applier, verdict.ObservedVia)
		appended = &next
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		return ApplyResult{Status: registry.WriteRefused}, err
	}
	if len(outcomes) == 0 {
		return ApplyResult{Status: registry.WriteNoop}, nil
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		return ApplyResult{Status: registry.WriteRefused}, err
	}
	if err := outcome.Err(); err != nil {
		return ApplyResult{Status: registry.WriteRefused}, err
	}
	return ApplyResult{Status: outcome.Status, Row: appended}, nil
}

func formatReason(verdict Verdict) string {
	reason := "cause_class=" + string(verdict.Cause)
	if len(verdict.Evidence) > 0 {
		reason += "; evidence=" + strings.Join(verdict.Evidence, ",")
	}
	return reason
}

func formatObservedVia(applier string, sources []string) string {
	all := appendUnique(nil, append([]string{strings.TrimSpace(applier)}, sources...)...)
	return strings.Join(all, "+")
}
