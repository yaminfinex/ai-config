// Package seatcompletion owns the one path from live seat facts to a durable
// seated registry snapshot.
package seatcompletion

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

type Origin string

const (
	OriginSpawn        Origin = "herder spawn"
	OriginEnroll       Origin = "herder enroll"
	OriginEnrollRepair Origin = "herder enroll"
	OriginAdopt        Origin = "herder adopt"
	OriginReclaim      Origin = "hcom start --as"
	OriginResume       Origin = "herder resume"
	OriginReconcile    Origin = "herder reconcile --apply"
	OriginRecognition  Origin = "seat recognition"
	OriginRepair       Origin = "herder repair"
)

const (
	SeatHerdr   = "herdr"
	SeatProcess = "process"

	RefusalSeatMissing    = "live_seat_missing"
	RefusalProcessMissing = "live_process_missing"
	RefusalBusUnavailable = "bus_roster_unavailable"
	RefusalBusRowMissing  = "joined_bus_row_missing"
	RefusalBusAmbiguous   = "bus_identity_ambiguous"
	RefusalAttestation    = "attested_binding_invalid"
	RefusalRegistryWrite  = "registry_write_refused"
)

type SeatClaim struct {
	Kind       string
	PaneID     string
	TerminalID string
	PID        int
}

type LivePane struct {
	PaneID     string
	TerminalID string
}

type AttestedBinding struct {
	Operation string
	Field     string
	Value     string
}

// NarrowFallback carries the already-established facts used by the two
// empty-launch-context relief valves. Complete rechecks the full predicate.
type NarrowFallback struct {
	Current v2.SessionRecord
}

type Request struct {
	Origin         Origin
	RegistryPath   string
	Candidate      v2.SessionRecord
	Seat           SeatClaim
	ObservedPane   *LivePane
	ObservedBus    *hcomidentity.Result
	Namespace      string
	Evidence       hcomidentity.Evidence
	RequireBus     bool
	Attested       *AttestedBinding
	Fallback       *NarrowFallback
	BuildLocked    func(registry.LockedUpdate, v2.Seat) (v2.SessionRecord, []v2.SessionRecord, []v2.SessionRecord, error)
	Event          string
	FinalizeLocked func(registry.LockedUpdate, *v2.SessionRecord, *v2.SessionRecord, string) error
}

type MissingFact struct {
	Fact         string
	SupplierVerb string
	Remedy       string
}

type Refusal struct {
	Code          string
	Cause         string
	Missing       []MissingFact
	LaunchContext *hcomidentity.LaunchContextRepair
}

func (r *Refusal) Error() string {
	if r == nil {
		return ""
	}
	return r.Cause
}

type Result struct {
	Outcome  registry.WriteOutcome
	Outcomes []registry.WriteOutcome
	Status   registry.WriteStatus
	Row      []byte
	Refusal  *Refusal
}

type Engine struct {
	HerdrPane           func(context.Context, string) (LivePane, error)
	ListBus             func(context.Context, string) ([]hcomidentity.Row, error)
	RepairLaunchContext func(string, string, string) hcomidentity.LaunchContextRepair
	ProcessAlive        func(int) bool
	Now                 func() time.Time
	NewBindingID        func() (string, error)
	UpdateRegistry      func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error)
}

func DefaultEngine() Engine {
	return Engine{
		HerdrPane: func(_ context.Context, paneID string) (LivePane, error) {
			out, err := (&herdrcli.Client{}).Output("pane", "get", paneID)
			if err != nil {
				return LivePane{}, err
			}
			pane, err := herdrcli.ParsePaneGet(out)
			if err != nil {
				return LivePane{}, err
			}
			return LivePane{PaneID: pane.PaneID, TerminalID: pane.TerminalID}, nil
		},
		ListBus:             hcomidentity.ListContext,
		RepairLaunchContext: hcomidentity.RepairLaunchContext,
		ProcessAlive: func(pid int) bool {
			if pid <= 0 {
				return false
			}
			process, err := os.FindProcess(pid)
			return err == nil && process.Signal(syscall.Signal(0)) == nil
		},
		Now:            time.Now,
		NewBindingID:   registry.NewGUID,
		UpdateRegistry: registry.UpdateLocked,
	}
}

func Complete(ctx context.Context, request Request) (Result, error) {
	return DefaultEngine().Complete(ctx, request)
}

func (e Engine) Complete(ctx context.Context, request Request) (Result, error) {
	if request.RegistryPath == "" || (request.Candidate.GUID == "" && request.BuildLocked == nil) {
		return Result{}, errors.New("seat completion requires registry path and a guid or locked candidate builder")
	}
	if e.Now == nil || e.NewBindingID == nil {
		return Result{}, errors.New("seat completion engine is missing clock or binding-id source")
	}
	if request.Attested != nil && !validAttestedBinding(*request.Attested) {
		return Result{Refusal: &Refusal{Code: RefusalAttestation, Cause: "attested completion requires one supported repair operation"}}, nil
	}

	stamp := e.Now().UTC().Format(time.RFC3339)
	seat, busAttested, refusal := e.resolveSeat(ctx, request, stamp)
	if refusal != nil {
		return Result{Refusal: refusal}, nil
	}

	update := e.UpdateRegistry
	if update == nil {
		update = registry.UpdateLocked
	}
	completedAt := 0
	outcomes, err := update(request.RegistryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		next := request.Candidate
		var before, after []v2.SessionRecord
		if request.BuildLocked != nil {
			var buildErr error
			next, before, after, buildErr = request.BuildLocked(tx, seat)
			if buildErr != nil {
				return nil, buildErr
			}
			completedAt = len(before)
		}
		if next.GUID == "" {
			return nil, errors.New("seat completion locked builder returned an empty guid")
		}
		current := registry.V2ByGUID(tx.Projection, next.GUID)
		if current != nil {
			next.Bindings = cloneBindings(current.Bindings)
			next.Attestations = cloneAttestations(current.Attestations)
			next.BindingTombstones = cloneTombstones(current.BindingTombstones)
		}
		next.Kind = v2.KindSession
		next.Event = request.Event
		if next.Event == "" {
			next.Event = "seated"
		}
		next.RecordedAt = stamp
		next.State = v2.StateSeated
		seat.Node = tx.NodeID
		if next.Seat != nil {
			seat.HooksBound = next.Seat.HooksBound
			seat.TranscriptPath = next.Seat.TranscriptPath
		}
		next.Seat = cloneSeat(&seat)

		if bindingSeatChanged(current, seat) {
			seatID, err := e.NewBindingID()
			if err != nil {
				return nil, err
			}
			next.Bindings = append(next.Bindings, v2.BindingFact{
				ID:            seatID,
				Field:         v2.BindingFieldSeat,
				EvidenceClass: v2.EvidenceLiveVerified,
				ObservedAt:    stamp,
				Seat:          bindingSeat(seat),
			})
		}
		if verifiedBusChanged(current, seat) {
			busID, err := e.NewBindingID()
			if err != nil {
				return nil, err
			}
			busClass := v2.EvidenceLiveVerified
			if busAttested {
				busClass = v2.EvidenceAttested
			}
			next.Bindings = append(next.Bindings, v2.BindingFact{
				ID:            busID,
				Field:         v2.BindingFieldHcomName,
				Value:         seat.HcomName,
				EvidenceClass: busClass,
				ObservedAt:    stamp,
			})
		}
		if request.FinalizeLocked != nil {
			if err := request.FinalizeLocked(tx, current, &next, stamp); err != nil {
				return nil, err
			}
		}
		rows := make([]v2.SessionRecord, 0, len(before)+1+len(after))
		rows = append(rows, before...)
		rows = append(rows, next)
		rows = append(rows, after...)
		return rows, nil
	})
	if err != nil {
		return Result{}, err
	}
	if completedAt >= len(outcomes) {
		return Result{}, fmt.Errorf("seat completion returned %d outcomes", len(outcomes))
	}
	outcome := outcomes[completedAt]
	result := Result{Outcome: outcome, Status: outcome.Status, Row: outcome.Row}
	result.Outcomes = outcomes
	if writeErr := outcome.Err(); writeErr != nil {
		result.Refusal = &Refusal{Code: RefusalRegistryWrite, Cause: writeErr.Error()}
	}
	return result, nil
}

func validAttestedBinding(binding AttestedBinding) bool {
	switch binding.Operation {
	case "", v2.AttestationRebind:
		return (binding.Field == v2.BindingFieldHcomName || binding.Field == v2.BindingFieldSID || binding.Field == v2.BindingFieldLaunchContext) && binding.Value != ""
	case v2.AttestationReissueCredential:
		return binding.Field == "" && binding.Value == ""
	default:
		return false
	}
}

func (e Engine) resolveSeat(ctx context.Context, request Request, stamp string) (v2.Seat, bool, *Refusal) {
	seat := v2.Seat{Kind: request.Seat.Kind, Namespace: request.Namespace, ConfirmedAt: stamp}
	busAttested := false
	switch request.Seat.Kind {
	case SeatHerdr:
		if request.ObservedPane != nil {
			pane := *request.ObservedPane
			if pane.PaneID == "" || pane.TerminalID == "" || (request.Seat.PaneID != "" && pane.PaneID != request.Seat.PaneID) {
				return v2.Seat{}, false, missingRefusal(RefusalSeatMissing, "observed herdr pane coordinates do not match the claim", "live herdr terminal + pane", string(request.Origin), "retry from the live seat")
			}
			seat.PaneID = pane.PaneID
			seat.TerminalID = pane.TerminalID
		} else if e.HerdrPane == nil {
			return v2.Seat{}, false, &Refusal{Code: RefusalSeatMissing, Cause: "live herdr seat resolver unavailable"}
		} else {
			pane, err := e.HerdrPane(ctx, request.Seat.PaneID)
			if err != nil || pane.PaneID == "" || pane.TerminalID == "" {
				cause := "live herdr pane and terminal are required"
				if err != nil {
					cause += ": " + err.Error()
				}
				return v2.Seat{}, false, missingRefusal(RefusalSeatMissing, cause, "live herdr terminal + pane", string(request.Origin), "retry from the live seat")
			}
			seat.PaneID = pane.PaneID
			seat.TerminalID = pane.TerminalID
		}
	case SeatProcess:
		if e.ProcessAlive == nil || !e.ProcessAlive(request.Seat.PID) {
			return v2.Seat{}, false, missingRefusal(RefusalProcessMissing, "live process pid is required", "live process pid", string(request.Origin), "retry after the headless process is running")
		}
		seat.PID = request.Seat.PID
	default:
		return v2.Seat{}, false, &Refusal{Code: RefusalSeatMissing, Cause: fmt.Sprintf("unsupported seat kind %q", request.Seat.Kind)}
	}

	if !request.RequireBus && !launchcmd.IsHcomCapable(request.Candidate.Tool) {
		return seat, false, nil
	}
	if request.ObservedBus == nil && e.ListBus == nil {
		return v2.Seat{}, false, &Refusal{Code: RefusalBusUnavailable, Cause: "live bus roster resolver unavailable"}
	}
	var rows []hcomidentity.Row
	resolved := hcomidentity.Result{}
	if request.ObservedBus != nil {
		resolved = *request.ObservedBus
	} else {
		var err error
		rows, err = e.ListBus(ctx, request.Namespace)
		if err != nil {
			return v2.Seat{}, false, missingRefusal(RefusalBusUnavailable, "live bus roster unavailable: "+err.Error(), "reachable live bus roster", string(request.Origin), "restore hcom access and retry")
		}
		resolved = hcomidentity.Resolve(rows, request.Evidence)
	}
	if !resolved.Verified && !containsAmbiguity(resolved.Reason) {
		historyResults := make([]hcomidentity.Result, 0, 2)
		if fact, status := registry.LatestSufficientBinding(request.Candidate, v2.BindingFieldHcomName, registry.LiveEvidenceAbsent); status == registry.BindingSelected {
			if row, count := hcomidentity.JoinedNamedCount(rows, fact.Value); count == 1 {
				historyResults = append(historyResults, hcomidentity.Result{Name: row.Name, BaseName: row.BaseName, SessionID: row.SessionID, PaneID: row.LaunchContext.PaneID, Verified: true})
				busAttested = fact.EvidenceClass == v2.EvidenceAttested
			}
		}
		if fact, status := registry.LatestSufficientBinding(request.Candidate, v2.BindingFieldSID, registry.LiveEvidenceAbsent); status == registry.BindingSelected {
			bySID := hcomidentity.Resolve(rows, hcomidentity.Evidence{SessionID: fact.Value})
			if bySID.Verified {
				historyResults = append(historyResults, bySID)
			}
		}
		if len(historyResults) > 0 {
			resolved = historyResults[0]
			for _, candidate := range historyResults[1:] {
				if candidate.Name != resolved.Name {
					return v2.Seat{}, false, &Refusal{Code: RefusalBusAmbiguous, Cause: "surviving binding histories resolve to different joined bus rows"}
				}
			}
		}
	}
	if !resolved.Verified && request.Attested != nil && request.Attested.Field == v2.BindingFieldHcomName {
		row, count := hcomidentity.JoinedNamedCount(rows, request.Attested.Value)
		if count != 1 {
			return v2.Seat{}, false, &Refusal{Code: RefusalBusAmbiguous, Cause: fmt.Sprintf("attested bus name resolves to %d joined rows", count)}
		}
		resolved = hcomidentity.Result{Name: row.Name, BaseName: row.BaseName, SessionID: row.SessionID, PaneID: row.LaunchContext.PaneID, Verified: true}
		busAttested = true
	}
	if !resolved.Verified && request.Attested != nil && request.Attested.Field == v2.BindingFieldLaunchContext && request.Candidate.Seat != nil && request.Candidate.Seat.HcomName != "" {
		row, count := hcomidentity.JoinedNamedCount(rows, request.Candidate.Seat.HcomName)
		if count != 1 {
			return v2.Seat{}, false, &Refusal{Code: RefusalBusAmbiguous, Cause: fmt.Sprintf("stored bus name resolves to %d joined rows", count)}
		}
		resolved = hcomidentity.Result{Name: row.Name, BaseName: row.BaseName, SessionID: row.SessionID, PaneID: row.LaunchContext.PaneID, Verified: true}
	}
	if resolved.Verified && request.Attested != nil && request.Attested.Operation == v2.AttestationRebind && request.Attested.Field == v2.BindingFieldHcomName {
		if resolved.Name != request.Attested.Value {
			return v2.Seat{}, false, &Refusal{Code: RefusalBusAmbiguous, Cause: fmt.Sprintf("conflicting live bus evidence proves @%s, not attested @%s", resolved.Name, request.Attested.Value)}
		}
		busAttested = true
	}
	if !resolved.Verified && request.Fallback != nil {
		resolved = narrowFallback(rows, request.Fallback.Current, seat)
	}
	if !resolved.Verified {
		code := RefusalBusRowMissing
		if containsAmbiguity(resolved.Reason) {
			code = RefusalBusAmbiguous
		}
		return v2.Seat{}, false, missingRefusal(code, resolved.Reason, "one joined bus row", "hcom start", "join the live session to hcom, then retry "+string(request.Origin))
	}
	verified := true
	seat.HcomName = resolved.Name
	seat.HcomVerified = &verified
	if seat.Kind == SeatHerdr {
		if request.ObservedBus != nil && resolved.PaneID == seat.PaneID {
			return seat, busAttested, nil
		}
		joined, count := hcomidentity.JoinedNamedCount(rows, resolved.Name)
		if count == 1 && joined.LaunchContext.PaneID == seat.PaneID {
			return seat, busAttested, nil
		}
		if e.RepairLaunchContext == nil {
			return v2.Seat{}, false, &Refusal{Code: "launch_context_repair_unavailable", Cause: "launch-context repair unavailable"}
		}
		// The roster's tagged display name is the delivery coordinate, while
		// hcom's instances table is keyed by the emitted base name. Repair only
		// the store coordinate hcom supplied; never manufacture one from tag
		// parts. Untagged/legacy rows legitimately use Name as the fallback.
		repairName := resolved.BaseName
		if repairName == "" {
			repairName = resolved.Name
		}
		repair := e.RepairLaunchContext(request.Namespace, repairName, seat.PaneID)
		if repair.Refused() || (repair.Status != "written" && repair.Status != "already-present") {
			return v2.Seat{}, false, &Refusal{Code: repair.Code, Cause: repair.Cause, LaunchContext: &repair}
		}
	}
	return seat, busAttested, nil
}

func narrowFallback(rows []hcomidentity.Row, current v2.SessionRecord, live v2.Seat) hcomidentity.Result {
	if current.State != v2.StateSeated || current.Seat == nil || current.Seat.Kind != SeatHerdr ||
		current.Seat.HcomVerified == nil || !*current.Seat.HcomVerified || current.Seat.HcomName == "" ||
		current.Seat.TerminalID != live.TerminalID || current.Seat.PaneID != live.PaneID {
		return hcomidentity.Result{Reason: "narrow empty-context proof is incomplete"}
	}
	row, count := hcomidentity.JoinedNamedCount(rows, current.Seat.HcomName)
	if count != 1 || !row.LaunchContext.Empty() {
		return hcomidentity.Result{Reason: "narrow empty-context proof requires one joined row with empty launch context"}
	}
	return hcomidentity.Result{Name: row.Name, BaseName: row.BaseName, SessionID: row.SessionID, Verified: true}
}

func missingRefusal(code, cause, fact, supplier, remedy string) *Refusal {
	return &Refusal{Code: code, Cause: cause, Missing: []MissingFact{{Fact: fact, SupplierVerb: supplier, Remedy: remedy}}}
}

func containsAmbiguity(reason string) bool {
	for _, fragment := range []string{"multiple", "different bus rows", "ambiguous"} {
		if strings.Contains(reason, fragment) {
			return true
		}
	}
	return false
}

func cloneSeat(in *v2.Seat) *v2.Seat {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneBindings(in []v2.BindingFact) []v2.BindingFact {
	if len(in) == 0 {
		return nil
	}
	out := make([]v2.BindingFact, len(in))
	copy(out, in)
	for i := range out {
		if in[i].Seat != nil {
			seat := *in[i].Seat
			out[i].Seat = &seat
		}
	}
	return out
}

func cloneAttestations(in []v2.Attestation) []v2.Attestation {
	if len(in) == 0 {
		return nil
	}
	out := make([]v2.Attestation, len(in))
	copy(out, in)
	return out
}

func cloneTombstones(in []v2.BindingTombstone) []v2.BindingTombstone {
	if len(in) == 0 {
		return nil
	}
	out := make([]v2.BindingTombstone, len(in))
	copy(out, in)
	return out
}

func bindingSeat(seat v2.Seat) *v2.BindingSeat {
	return &v2.BindingSeat{
		Kind:       seat.Kind,
		Node:       seat.Node,
		TerminalID: seat.TerminalID,
		PaneID:     seat.PaneID,
		PID:        seat.PID,
		Namespace:  seat.Namespace,
	}
}

func bindingSeatChanged(current *v2.SessionRecord, seat v2.Seat) bool {
	if current == nil || len(current.Bindings) == 0 || current.State != v2.StateSeated || current.Seat == nil {
		return true
	}
	want := bindingSeat(seat)
	for i := len(current.Bindings) - 1; i >= 0; i-- {
		fact := current.Bindings[i]
		if fact.Field == v2.BindingFieldSeat {
			return fact.Seat == nil || *fact.Seat != *want
		}
	}
	return true
}

func verifiedBusChanged(current *v2.SessionRecord, seat v2.Seat) bool {
	if seat.HcomName == "" || seat.HcomVerified == nil || !*seat.HcomVerified {
		return false
	}
	if current == nil || len(current.Bindings) == 0 {
		return true
	}
	for i := len(current.Bindings) - 1; i >= 0; i-- {
		fact := current.Bindings[i]
		if fact.Field == v2.BindingFieldHcomName {
			return fact.Value != seat.HcomName
		}
	}
	return true
}
