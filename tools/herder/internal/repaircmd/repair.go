// Package repaircmd owns herder's deliberately narrow, operator-attested
// break-glass identity repair surface.
package repaircmd

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
)

const RateWindow = 10 * time.Minute

type Operation string

const (
	OperationRebind            Operation = "rebind"
	OperationReissueCredential Operation = "reissue-credential"
)

var (
	ErrAttestationRequired = errors.New("explicit tty attestation is required")
	ErrCorroborationFailed = errors.New("seat-control corroboration failed")
)

type Request struct {
	Operation Operation
	GUID      string
	Field     string
	Value     string
}

type Proof struct {
	Statement  string
	PaneID     string
	TerminalID string
}

type Result struct {
	Status        registry.WriteStatus
	UpstreamGated bool
}

type Service struct {
	RegistryPath string
	Now          func() time.Time
	NewID        func() (string, error)
	CollectProof func(context.Context, v2.SessionRecord, Request) (Proof, error)
	Complete     func(context.Context, seatcompletion.Request) (seatcompletion.Result, error)
	ListBus      func(context.Context, string) ([]hcomidentity.Row, error)
	Update       func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error)
}

func DefaultService(stderr io.Writer) Service {
	collector := DefaultProofCollector(stderr)
	return Service{
		RegistryPath: registry.DefaultPath(),
		Now:          time.Now,
		NewID:        registry.NewGUID,
		CollectProof: collector.Collect,
		Complete:     seatcompletion.Complete,
		ListBus:      hcomidentity.ListContext,
		Update:       registry.UpdateLocked,
	}
}

func (s Service) Execute(ctx context.Context, request Request) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	if s.RegistryPath == "" {
		s.RegistryPath = registry.DefaultPath()
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.NewID == nil {
		s.NewID = registry.NewGUID
	}
	if s.CollectProof == nil {
		return Result{}, ErrAttestationRequired
	}
	if s.Complete == nil {
		s.Complete = seatcompletion.Complete
	}
	projection, err := v2.LoadFile(s.RegistryPath, v2.LoadOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("load registry: %w", err)
	}
	current := sessionByGUID(projection, request.GUID)
	if current == nil {
		return Result{}, fmt.Errorf("repair target guid %s does not exist", request.GUID)
	}
	if current.State != v2.StateSeated || current.Seat == nil || current.Seat.Kind != seatcompletion.SeatHerdr || current.Seat.PaneID == "" {
		return Result{}, fmt.Errorf("repair target guid %s must claim one live herdr pane; repair registry seat coordinates through enroll/adopt/reconcile", request.GUID)
	}
	if err := rateLimitError(*current, s.Now()); err != nil {
		return Result{}, err
	}
	if request.Operation == OperationRebind && request.Field != v2.BindingFieldLaunchContext && projectedBindingValue(*current, request.Field) == request.Value {
		return Result{}, fmt.Errorf("repair value %q is already current for field %s", request.Value, request.Field)
	}
	proof, err := s.CollectProof(ctx, *current, request)
	if err != nil {
		return Result{}, err
	}
	if proof.Statement == "" || proof.PaneID != current.Seat.PaneID || (current.Seat.TerminalID != "" && proof.TerminalID != current.Seat.TerminalID) {
		return Result{}, ErrCorroborationFailed
	}

	attestationID, err := s.NewID()
	if err != nil {
		return Result{}, err
	}
	stamp := s.Now().UTC().Format(time.RFC3339)
	attestation := v2.Attestation{
		ID: attestationID, GUID: request.GUID, Field: request.Field, Value: request.Value,
		Statement: proof.Statement, PaneID: proof.PaneID, TerminalID: proof.TerminalID, ObservedAt: stamp,
	}
	if request.Operation == OperationReissueCredential {
		attestation.Operation = v2.AttestationReissueCredential
	} else {
		attestation.Operation = v2.AttestationRebind
	}

	if request.Field == v2.BindingFieldLaunchContext {
		return s.executeLaunchContext(ctx, *current, request, attestation)
	}

	candidate := cloneRecord(*current)
	if request.Field == v2.BindingFieldHcomName {
		candidate.Seat.HcomName = request.Value
		verified := true
		candidate.Seat.HcomVerified = &verified
	}
	if request.Field == v2.BindingFieldSID {
		candidate.SIDs = append(candidate.SIDs, v2.SID{SID: request.Value, ObservedAt: stamp, Source: v2.EvidenceAttested})
		candidate.Continuity = "confirmed"
	}
	completionRequest := seatcompletion.Request{
		Origin: seatcompletion.OriginRepair, RegistryPath: s.RegistryPath, Candidate: candidate,
		Seat:      seatcompletion.SeatClaim{Kind: candidate.Seat.Kind, PaneID: candidate.Seat.PaneID, TerminalID: candidate.Seat.TerminalID},
		Namespace: candidate.Seat.Namespace, Evidence: hcomidentity.Evidence{PaneIDs: []string{candidate.Seat.PaneID}},
		RequireBus: true, Event: v2.EventAttestedBinding,
		Attested: &seatcompletion.AttestedBinding{Operation: string(attestation.Operation), Field: request.Field, Value: request.Value},
	}
	completionRequest.FinalizeLocked = s.finalizer(*current, request, attestation)
	completed, err := s.Complete(ctx, completionRequest)
	if err != nil {
		return Result{}, err
	}
	if completed.Refusal != nil {
		return Result{}, errors.New(completed.Refusal.Cause)
	}
	return Result{Status: completed.Status}, nil
}

func (s Service) finalizer(preflight v2.SessionRecord, request Request, attestation v2.Attestation) func(registry.LockedUpdate, *v2.SessionRecord, *v2.SessionRecord, string) error {
	return func(_ registry.LockedUpdate, current *v2.SessionRecord, next *v2.SessionRecord, stamp string) error {
		if current == nil || !sameRepairAnchor(preflight, *current) {
			return errors.New("repair target changed during attestation; no mutation committed, inspect the current row and retry")
		}
		if remaining, limited := registry.AttestationRateLimit(*current, s.Now(), RateWindow); limited {
			return rateLimitRefusal(current.GUID, remaining)
		}
		next.Attestations = append(next.Attestations, attestation)
		if request.Operation == OperationReissueCredential {
			return nil
		}
		oldValue := projectedBindingValue(*current, request.Field)
		oldID := latestSurvivingBindingID(*current, request.Field)
		correctionID := ""
		if request.Field == v2.BindingFieldHcomName {
			for i := len(next.Bindings) - 1; i >= 0; i-- {
				if next.Bindings[i].Field == request.Field && next.Bindings[i].Value == request.Value && next.Bindings[i].EvidenceClass == v2.EvidenceAttested {
					next.Bindings[i].AttestationID = attestation.ID
					correctionID = next.Bindings[i].ID
					break
				}
			}
		} else {
			var err error
			correctionID, err = s.NewID()
			if err != nil {
				return err
			}
			next.Bindings = append(next.Bindings, v2.BindingFact{ID: correctionID, Field: request.Field, Value: request.Value, EvidenceClass: v2.EvidenceAttested, ObservedAt: stamp, AttestationID: attestation.ID})
		}
		if correctionID == "" {
			return errors.New("seat completion did not append the attested correction binding")
		}
		if oldValue == "" {
			return nil
		}
		if oldID == "" {
			legacyID, err := s.NewID()
			if err != nil {
				return err
			}
			legacy := v2.BindingFact{ID: legacyID, Field: request.Field, Value: oldValue, EvidenceClass: v2.EvidenceCarried, ObservedAt: current.RecordedAt}
			idx := len(next.Bindings) - 1
			next.Bindings = append(next.Bindings, v2.BindingFact{})
			copy(next.Bindings[idx+1:], next.Bindings[idx:])
			next.Bindings[idx] = legacy
			oldID = legacyID
		}
		next.BindingTombstones = append(next.BindingTombstones, v2.BindingTombstone{
			BindingID: oldID, Field: request.Field, CorrectionBindingID: correctionID,
			AttestationID: attestation.ID, TombstonedAt: stamp,
		})
		return nil
	}
}

func (s Service) executeLaunchContext(ctx context.Context, current v2.SessionRecord, request Request, attestation v2.Attestation) (Result, error) {
	if request.Value != current.Seat.PaneID {
		return Result{}, fmt.Errorf("launch_context value must equal the corroborated live pane %q", current.Seat.PaneID)
	}
	list := s.ListBus
	if list == nil {
		list = hcomidentity.ListContext
	}
	rows, err := list(ctx, current.Seat.Namespace)
	if err != nil {
		return Result{}, fmt.Errorf("live bus roster unavailable: %w", err)
	}
	row, count := hcomidentity.JoinedNamedCount(rows, current.Seat.HcomName)
	if count != 1 {
		return Result{}, fmt.Errorf("stored bus name resolves to %d joined rows", count)
	}
	if row.LaunchContext.PaneID != "" && row.LaunchContext.PaneID != request.Value {
		attestation.Operation = v2.AttestationAuthorizeRecreate
		status, err := s.appendAuthorization(current, attestation)
		if err != nil {
			return Result{}, err
		}
		return Result{Status: status, UpstreamGated: true}, nil
	}
	candidate := cloneRecord(current)
	completionRequest := seatcompletion.Request{
		Origin: seatcompletion.OriginRepair, RegistryPath: s.RegistryPath, Candidate: candidate,
		Seat:      seatcompletion.SeatClaim{Kind: candidate.Seat.Kind, PaneID: candidate.Seat.PaneID, TerminalID: candidate.Seat.TerminalID},
		Namespace: candidate.Seat.Namespace, Evidence: hcomidentity.Evidence{PaneIDs: []string{candidate.Seat.PaneID}}, RequireBus: true,
		Event:    v2.EventAttestedBinding,
		Attested: &seatcompletion.AttestedBinding{Operation: v2.AttestationRebind, Field: v2.BindingFieldLaunchContext, Value: request.Value},
		FinalizeLocked: func(_ registry.LockedUpdate, locked *v2.SessionRecord, next *v2.SessionRecord, _ string) error {
			if locked == nil || !sameRepairAnchor(current, *locked) {
				return errors.New("repair target changed during attestation; no mutation committed")
			}
			if remaining, limited := registry.AttestationRateLimit(*locked, s.Now(), RateWindow); limited {
				return rateLimitRefusal(locked.GUID, remaining)
			}
			next.Attestations = append(next.Attestations, attestation)
			return nil
		},
	}
	completed, err := s.Complete(ctx, completionRequest)
	if err != nil {
		return Result{}, err
	}
	if completed.Refusal != nil {
		return Result{}, errors.New(completed.Refusal.Cause)
	}
	return Result{Status: completed.Status}, nil
}

func (s Service) appendAuthorization(preflight v2.SessionRecord, attestation v2.Attestation) (registry.WriteStatus, error) {
	update := s.Update
	if update == nil {
		update = registry.UpdateLocked
	}
	outcomes, err := update(s.RegistryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		current := registry.V2ByGUID(tx.Projection, preflight.GUID)
		if current == nil || !sameRepairAnchor(preflight, *current) {
			return nil, errors.New("repair target changed during attestation; no mutation committed")
		}
		if remaining, limited := registry.AttestationRateLimit(*current, s.Now(), RateWindow); limited {
			return nil, rateLimitRefusal(current.GUID, remaining)
		}
		next := cloneRecord(*current)
		next.Event = v2.EventAttestedBinding
		next.RecordedAt = s.Now().UTC().Format(time.RFC3339)
		next.Attestations = append(next.Attestations, attestation)
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		return "", err
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		return "", err
	}
	if err := outcome.Err(); err != nil {
		return outcome.Status, err
	}
	return outcome.Status, nil
}

func sameRepairAnchor(a, b v2.SessionRecord) bool {
	return a.GUID == b.GUID && a.RecordedAt == b.RecordedAt && a.Event == b.Event && a.State == b.State &&
		reflect.DeepEqual(a.Seat, b.Seat) && bytes.Equal(a.Raw, b.Raw)
}

func projectedBindingValue(rec v2.SessionRecord, field string) string {
	switch field {
	case v2.BindingFieldHcomName:
		if rec.Seat != nil {
			return rec.Seat.HcomName
		}
	case v2.BindingFieldSID:
		if len(rec.SIDs) > 0 {
			return rec.SIDs[len(rec.SIDs)-1].SID
		}
		return rec.Provenance.ToolSessionID
	}
	return ""
}

func latestSurvivingBindingID(rec v2.SessionRecord, field string) string {
	tombstoned := map[string]bool{}
	for _, marker := range rec.BindingTombstones {
		tombstoned[marker.BindingID] = true
	}
	for i := len(rec.Bindings) - 1; i >= 0; i-- {
		if rec.Bindings[i].Field == field && !tombstoned[rec.Bindings[i].ID] {
			return rec.Bindings[i].ID
		}
	}
	return ""
}

func cloneRecord(in v2.SessionRecord) v2.SessionRecord {
	out := in
	out.Raw = nil
	if in.Seat != nil {
		seat := *in.Seat
		out.Seat = &seat
	}
	out.Bindings = append([]v2.BindingFact(nil), in.Bindings...)
	out.Attestations = append([]v2.Attestation(nil), in.Attestations...)
	out.BindingTombstones = append([]v2.BindingTombstone(nil), in.BindingTombstones...)
	out.SIDs = append([]v2.SID(nil), in.SIDs...)
	return out
}

func sessionByGUID(projection *v2.Projection, guid string) *v2.SessionRecord {
	for _, rec := range projection.Sessions() {
		if rec.GUID == guid {
			copy := rec
			return &copy
		}
	}
	return nil
}

func validateRequest(request Request) error {
	if request.GUID == "" {
		return errors.New("repair requires --guid")
	}
	if len(request.GUID) > 128 || containsUnsafeTokenRune(request.GUID) {
		return errors.New("repair guid must be one bounded token without whitespace or control characters")
	}
	switch request.Operation {
	case OperationRebind:
		if request.Value == "" {
			return errors.New("rebind requires one nonempty --value")
		}
		if len(request.Value) > 512 || containsUnsafeTokenRune(request.Value) {
			return errors.New("rebind value must be one bounded token without whitespace or control characters")
		}
		switch request.Field {
		case v2.BindingFieldHcomName, v2.BindingFieldSID, v2.BindingFieldLaunchContext:
			return nil
		default:
			return fmt.Errorf("field %q is outside break-glass vocabulary; allowed: hcom_name, sid, launch_context", request.Field)
		}
	case OperationReissueCredential:
		if request.Field != "" || request.Value != "" {
			return errors.New("reissue-credential does not accept a field or value")
		}
		return nil
	default:
		return fmt.Errorf("unsupported repair operation %q", request.Operation)
	}
}

func containsUnsafeTokenRune(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0
}

func formatRemaining(remaining time.Duration) string {
	return remaining.Round(time.Second).String()
}

func rateLimitError(rec v2.SessionRecord, now time.Time) error {
	remaining, limited := registry.AttestationRateLimit(rec, now, RateWindow)
	if !limited {
		return nil
	}
	return rateLimitRefusal(rec.GUID, remaining)
}

func rateLimitRefusal(guid string, remaining time.Duration) error {
	return fmt.Errorf("rate limit: guid %s permits one committed break-glass operation per 10m; retry in %s", guid, formatRemaining(remaining))
}

func parseArgs(args []string) (Request, error) {
	if len(args) == 0 {
		return Request{}, errors.New("repair requires rebind or reissue-credential")
	}
	request := Request{Operation: Operation(args[0])}
	flags := flag.NewFlagSet("repair", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&request.GUID, "guid", "", "durable target guid")
	flags.StringVar(&request.Field, "field", "", "identity field")
	flags.StringVar(&request.Value, "value", "", "replacement value")
	if err := flags.Parse(args[1:]); err != nil {
		return Request{}, err
	}
	if flags.NArg() != 0 {
		return Request{}, errors.New("repair accepts no positional values")
	}
	if err := validateRequest(request); err != nil {
		return Request{}, err
	}
	return request, nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	return runWithService(args, stdout, stderr, DefaultService(stderr))
}

func runWithService(args []string, stdout, stderr io.Writer, service Service) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, usage)
		return 0
	}
	request, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "herder repair: %v\n", err)
		return 2
	}
	fmt.Fprintf(stderr, "herder repair: BREAK-GLASS attempt for guid %s operation=%s field=%s; deliberate use is logged and rate-limited\n", request.GUID, request.Operation, request.Field)
	result, err := service.Execute(context.Background(), request)
	if err != nil {
		fmt.Fprintf(stderr, "herder repair: refused: %v\n", err)
		return 1
	}
	if result.UpstreamGated {
		fmt.Fprintln(stderr, "herder repair: authorization recorded; launch_context was NOT rewritten. Recreate the vendor row from the verified live pane (leave/stop the wrong row, rejoin under the same name), then run completion. If hcom's reclaim guard refuses the rejoin, this shape is upstream-gated; follow docs/hazards/agent-cli-identity-hijack.md.")
		return 1
	}
	fmt.Fprintf(stderr, "herder repair: committed attested operation for guid %s\n", request.GUID)
	return 0
}

const usage = `usage:
  herder repair rebind --guid GUID --field hcom_name|sid|launch_context --value VALUE
  herder repair reissue-credential --guid GUID

This is an interactive, rate-limited break-glass surface. It requires explicit
attestation on /dev/tty plus nonce corroboration through the target live pane.
`
