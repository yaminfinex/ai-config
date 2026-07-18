package seatcompletion

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestBuslessHerdrCompletionNeverConsultsBus(t *testing.T) {
	engine := testEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		t.Fatal("busless completion consulted hcom")
		return nil, nil
	}
	engine.RepairLaunchContext = func(string, string, string) hcomidentity.LaunchContextRepair {
		t.Fatal("busless completion attempted launch-context repair")
		return hcomidentity.LaunchContextRepair{}
	}

	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginSpawn,
		RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
		Candidate:    v2.SessionRecord{GUID: "guid-bash", Label: "shell", Tool: "bash"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
	})
	if err != nil || result.Refusal != nil {
		t.Fatalf("Complete() = result %+v err %v", result, err)
	}
	row := decodeCompletedRow(t, result.Row)
	if row.Seat == nil || row.Seat.Kind != SeatHerdr || row.Seat.TerminalID != "terminal-live" || row.Seat.HcomName != "" {
		t.Fatalf("busless seat = %+v", row.Seat)
	}
	if len(row.Bindings) != 1 || row.Bindings[0].Field != v2.BindingFieldSeat {
		t.Fatalf("busless bindings = %+v", row.Bindings)
	}
}

func TestBusCapableCompletionRefusesAbsentRowWithoutRegistryAppend(t *testing.T) {
	engine := testEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) { return nil, nil }
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginEnroll,
		RegistryPath: path,
		Candidate:    v2.SessionRecord{GUID: "guid-agent", Label: "worker", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
		Evidence:     hcomidentity.Evidence{PaneIDs: []string{"pane-live"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Refusal == nil || result.Refusal.Code != RefusalBusRowMissing || len(result.Refusal.Missing) != 1 || result.Refusal.Missing[0].SupplierVerb != "hcom start" {
		t.Fatalf("refusal = %+v", result.Refusal)
	}
	if info, statErr := os.Stat(path); statErr == nil && info.Size() != 0 {
		t.Fatalf("registry size = %d, want unchanged", info.Size())
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}
}

func TestHerdrCompletionBackfillsBeforeAppendingCanonicalBinding(t *testing.T) {
	engine := testEngine(t)
	joined := true
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "bus-live", Joined: &joined, SessionID: "session-live"}}, nil
	}
	repaired := false
	engine.RepairLaunchContext = func(dir, name, pane string) hcomidentity.LaunchContextRepair {
		repaired = true
		if dir != "/bus" || name != "bus-live" || pane != "pane-live" {
			t.Fatalf("repair args = %q %q %q", dir, name, pane)
		}
		return hcomidentity.LaunchContextRepair{Status: "written", PaneID: pane, ProcessID: "process-live"}
	}
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginEnrollRepair,
		RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
		Candidate:    v2.SessionRecord{GUID: "guid-agent", Label: "worker", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
		Namespace:    "/bus",
		Evidence:     hcomidentity.Evidence{SessionID: "session-live"},
	})
	if err != nil || result.Refusal != nil || !repaired {
		t.Fatalf("Complete() = result %+v repaired %v err %v", result, repaired, err)
	}
	row := decodeCompletedRow(t, result.Row)
	if row.Seat == nil || row.Seat.HcomName != "bus-live" || row.Seat.HcomVerified == nil || !*row.Seat.HcomVerified {
		t.Fatalf("completed seat = %+v", row.Seat)
	}
	if len(row.Bindings) != 2 || row.Bindings[0].ID != "binding-1" || row.Bindings[1].ID != "binding-2" {
		t.Fatalf("bindings = %+v", row.Bindings)
	}
}

func TestTaggedJoinedRowWithoutPaneRepairsByAuthoritativeBaseName(t *testing.T) {
	engine := testEngine(t)
	joined := true
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{
			Name:      "worker-live",
			BaseName:  "live",
			Joined:    &joined,
			SessionID: "session-live",
			LaunchContext: hcomidentity.LaunchContext{
				ProcessID: "process-live",
			},
		}}, nil
	}
	repaired := false
	engine.RepairLaunchContext = func(dir, name, pane string) hcomidentity.LaunchContextRepair {
		repaired = true
		if dir != "/bus" || name != "live" || pane != "pane-live" {
			t.Fatalf("repair args = %q %q %q, want base-name store coordinate", dir, name, pane)
		}
		return hcomidentity.LaunchContextRepair{Status: "written", PaneID: pane, ProcessID: "process-live"}
	}
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginRecognition,
		RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
		Candidate:    v2.SessionRecord{GUID: "guid-agent", Label: "worker", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
		Namespace:    "/bus",
		Evidence:     hcomidentity.Evidence{ProcessID: "process-live"},
	})
	if err != nil || result.Refusal != nil || !repaired {
		t.Fatalf("Complete() = result %+v repaired %v err %v", result, repaired, err)
	}
	row := decodeCompletedRow(t, result.Row)
	if row.Seat == nil || row.Seat.HcomName != "worker-live" || row.Seat.PaneID != "pane-live" {
		t.Fatalf("completed seat = %+v", row.Seat)
	}
}

func TestRepeatedCompletionDoesNotDuplicateUnchangedBindingFacts(t *testing.T) {
	engine := testEngine(t)
	joined := true
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "bus-live", Joined: &joined, SessionID: "session-live", LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-live"}}}, nil
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	request := Request{
		Origin:       OriginRecognition,
		RegistryPath: path,
		Candidate:    v2.SessionRecord{GUID: "guid-repeat", Label: "worker", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
		Evidence:     hcomidentity.Evidence{SessionID: "session-live"},
	}
	first, err := engine.Complete(context.Background(), request)
	if err != nil || first.Refusal != nil {
		t.Fatalf("first completion = %+v err=%v", first, err)
	}
	second, err := engine.Complete(context.Background(), request)
	if err != nil || second.Refusal != nil {
		t.Fatalf("second completion = %+v err=%v", second, err)
	}
	row := decodeCompletedRow(t, second.Row)
	if len(row.Bindings) != 2 {
		t.Fatalf("repeated completion bindings = %+v, want unchanged seat + bus history", row.Bindings)
	}
}

func TestProcessCompletionNeedsPIDAndBusButNeverPane(t *testing.T) {
	engine := testEngine(t)
	joined := true
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "headless-live", Joined: &joined, LaunchContext: launchContext("process-live")}}, nil
	}
	engine.ProcessAlive = func(pid int) bool { return pid == 4242 }
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginResume,
		RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
		Candidate:    v2.SessionRecord{GUID: "guid-headless", Label: "headless", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatProcess, PID: 4242},
		Namespace:    "/bus",
		Evidence:     hcomidentity.Evidence{ProcessID: "process-live"},
	})
	if err != nil || result.Refusal != nil {
		t.Fatalf("Complete() = result %+v err %v", result, err)
	}
	row := decodeCompletedRow(t, result.Row)
	if row.Seat == nil || row.Seat.Kind != SeatProcess || row.Seat.PID != 4242 || row.Seat.PaneID != "" || row.Seat.TerminalID != "" {
		t.Fatalf("process seat = %+v", row.Seat)
	}
}

func TestPaneConflictIsSurfacedAndNeverAppends(t *testing.T) {
	engine := testEngine(t)
	joined := true
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "bus-live", Joined: &joined, SessionID: "session-live"}}, nil
	}
	engine.RepairLaunchContext = func(string, string, string) hcomidentity.LaunchContextRepair {
		return hcomidentity.LaunchContextRepair{Status: "refused", Code: "launch_context_pane_conflict", Cause: "stored pane differs", Remedy: "recreate through hcom"}
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginAdopt,
		RegistryPath: path,
		Candidate:    v2.SessionRecord{GUID: "guid-agent", Label: "worker", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
		Evidence:     hcomidentity.Evidence{SessionID: "session-live"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Refusal == nil || result.Refusal.Code != "launch_context_pane_conflict" || result.Refusal.LaunchContext == nil {
		t.Fatalf("refusal = %+v", result.Refusal)
	}
	if _, statErr := os.Stat(path); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("registry stat err = %v, want not-exist", statErr)
	}
}

func TestBusRosterFailureRefusesWithoutTreatingOutageAsAbsence(t *testing.T) {
	engine := testEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return nil, errors.New("roster unavailable")
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginReconcile,
		RegistryPath: path,
		Candidate:    v2.SessionRecord{GUID: "guid-outage", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Refusal == nil || result.Refusal.Code != RefusalBusUnavailable {
		t.Fatalf("refusal = %+v, want roster-unavailable refusal", result.Refusal)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("registry stat err = %v, want no write", statErr)
	}
}

func TestMultipleMatchingBusRowsFailClosed(t *testing.T) {
	engine := testEngine(t)
	joined := true
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{
			{Name: "bus-a", Joined: &joined, SessionID: "session-live"},
			{Name: "bus-b", Joined: &joined, SessionID: "session-live"},
		}, nil
	}
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginRecognition,
		RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
		Candidate:    v2.SessionRecord{GUID: "guid-ambiguous", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
		Evidence:     hcomidentity.Evidence{SessionID: "session-live"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Refusal == nil || result.Refusal.Code != RefusalBusAmbiguous {
		t.Fatalf("refusal = %+v, want ambiguity refusal", result.Refusal)
	}
}

func TestAttestedBusBindingRequiresExactlyOneJoinedNamedRow(t *testing.T) {
	complete := func(t *testing.T, attested AttestedBinding, rows []hcomidentity.Row) Result {
		t.Helper()
		engine := testEngine(t)
		engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) { return rows, nil }
		engine.RepairLaunchContext = func(string, string, string) hcomidentity.LaunchContextRepair {
			return hcomidentity.LaunchContextRepair{Status: "written"}
		}
		result, err := engine.Complete(context.Background(), Request{
			Origin:       OriginReclaim,
			RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
			Candidate:    v2.SessionRecord{GUID: "guid-attested", Tool: "codex"},
			Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
			Attested:     &attested,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	attested := AttestedBinding{Field: v2.BindingFieldHcomName, Value: "attested-bus"}
	result := complete(t, attested, joinedRows(hcomidentity.Row{Name: "attested-bus"}))
	if result.Refusal != nil {
		t.Fatalf("attested completion refused: %+v", result.Refusal)
	}
	row := decodeCompletedRow(t, result.Row)
	if len(row.Bindings) != 2 || row.Bindings[1].Field != v2.BindingFieldHcomName || row.Bindings[1].EvidenceClass != v2.EvidenceAttested {
		t.Fatalf("attested bindings = %+v", row.Bindings)
	}

	wrongField := complete(t, AttestedBinding{Field: v2.BindingFieldSeat, Value: "attested-bus"}, joinedRows(hcomidentity.Row{Name: "attested-bus"}))
	if wrongField.Refusal == nil || wrongField.Refusal.Code != RefusalAttestation {
		t.Fatalf("wrong-field attestation = %+v, want attestation refusal", wrongField)
	}

	duplicate := complete(t, attested, joinedRows(
		hcomidentity.Row{Name: "attested-bus"},
		hcomidentity.Row{Name: "attested-bus"},
	))
	if duplicate.Refusal == nil || duplicate.Refusal.Code != RefusalBusAmbiguous {
		t.Fatalf("duplicate attestation = %+v, want ambiguity refusal", duplicate)
	}
}

func TestAttestedBindingIsValidatedEvenWhenLiveResolutionSucceeds(t *testing.T) {
	joined := true
	for _, attested := range []AttestedBinding{
		{Field: v2.BindingFieldSeat, Value: "bus-live"},
		{Field: v2.BindingFieldHcomName, Value: ""},
	} {
		engine := testEngine(t)
		engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "bus-live", Joined: &joined, SessionID: "session-live", LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-live"}}}, nil
		}
		result, err := engine.Complete(context.Background(), Request{
			Origin:       OriginReclaim,
			RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
			Candidate:    v2.SessionRecord{GUID: "guid-invalid-attestation", Tool: "codex"},
			Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
			Evidence:     hcomidentity.Evidence{SessionID: "session-live"},
			Attested:     &attested,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Refusal == nil || result.Refusal.Code != RefusalAttestation {
			t.Fatalf("invalid attestation %+v result = %+v, want attestation refusal", attested, result)
		}
	}
}

func TestUnusedValidAttestationDoesNotDowngradeLiveEvidence(t *testing.T) {
	joined := true
	engine := testEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "bus-live", Joined: &joined, SessionID: "session-live", LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-live"}}}, nil
	}
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginReclaim,
		RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
		Candidate:    v2.SessionRecord{GUID: "guid-live-attestation", Tool: "codex"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
		Evidence:     hcomidentity.Evidence{SessionID: "session-live"},
		Attested:     &AttestedBinding{Field: v2.BindingFieldHcomName, Value: "bus-live"},
	})
	if err != nil || result.Refusal != nil {
		t.Fatalf("Complete() = %+v err=%v", result, err)
	}
	row := decodeCompletedRow(t, result.Row)
	if len(row.Bindings) != 2 || row.Bindings[1].EvidenceClass != v2.EvidenceLiveVerified {
		t.Fatalf("bindings = %+v, want live-verified bus evidence", row.Bindings)
	}
}

func TestCompletionSurfacesNoopWriteStatus(t *testing.T) {
	engine := testEngine(t)
	engine.UpdateRegistry = func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
		return []registry.WriteOutcome{{Status: registry.WriteNoop}}, nil
	}
	result, err := engine.Complete(context.Background(), Request{
		Origin:       OriginSpawn,
		RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
		Candidate:    v2.SessionRecord{GUID: "guid-noop", Tool: "bash"},
		Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != registry.WriteNoop || result.Row != nil || result.Refusal != nil {
		t.Fatalf("noop completion = %+v, want explicit noop without row/refusal", result)
	}
}

func TestCompletionUsesSurvivingAttestedBindingOnlyWhenLiveEvidenceAbsent(t *testing.T) {
	joined := true
	engine := testEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "bus-repaired", Joined: &joined}}, nil
	}
	engine.RepairLaunchContext = func(string, string, string) hcomidentity.LaunchContextRepair {
		return hcomidentity.LaunchContextRepair{Status: "written", PaneID: "pane-live"}
	}
	verified := true
	candidate := v2.SessionRecord{
		GUID: "guid-corrected", Event: v2.EventAttestedBinding, Tool: "codex",
		Seat: &v2.Seat{Kind: SeatHerdr, PaneID: "pane-live", TerminalID: "terminal-live", HcomName: "bus-repaired", HcomVerified: &verified},
		Bindings: []v2.BindingFact{
			{ID: "seat-live", Field: v2.BindingFieldSeat, EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: "2026-07-17T00:00:00Z", Seat: &v2.BindingSeat{Kind: SeatHerdr, PaneID: "pane-live", TerminalID: "terminal-live"}},
			{ID: "bus-stale", Field: v2.BindingFieldHcomName, Value: "bus-stale", EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: "2026-07-17T00:00:00Z"},
			{ID: "bus-repaired-id", Field: v2.BindingFieldHcomName, Value: "bus-repaired", EvidenceClass: v2.EvidenceAttested, ObservedAt: "2026-07-17T00:01:00Z", AttestationID: "attestation-id"},
		},
		Attestations:      []v2.Attestation{{ID: "attestation-id", Operation: v2.AttestationRebind, GUID: "guid-corrected", Field: v2.BindingFieldHcomName, Value: "bus-repaired", Statement: "explicit statement", PaneID: "pane-live", TerminalID: "terminal-live", ObservedAt: "2026-07-17T00:01:00Z"}},
		BindingTombstones: []v2.BindingTombstone{{BindingID: "bus-stale", Field: v2.BindingFieldHcomName, CorrectionBindingID: "bus-repaired-id", AttestationID: "attestation-id", TombstonedAt: "2026-07-17T00:01:00Z"}},
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) { return []v2.SessionRecord{candidate}, nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	candidate = projection.Sessions()[0]
	result, err := engine.Complete(context.Background(), Request{
		Origin: OriginReconcile, RegistryPath: path, Candidate: candidate,
		Seat: SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"}, ObservedPane: &LivePane{PaneID: "pane-live", TerminalID: "terminal-live"},
		RequireBus: true,
	})
	if err != nil || result.Refusal != nil {
		t.Fatalf("Complete result=%+v err=%v", result, err)
	}
	row := decodeCompletedRow(t, result.Row)
	if row.Seat == nil || row.Seat.HcomName != "bus-repaired" {
		t.Fatalf("completed row = %+v", row)
	}
}

func TestCompletionDoesNotArmHistoryWhenBusRosterUnavailable(t *testing.T) {
	engine := testEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) { return nil, errors.New("outage") }
	result, err := engine.Complete(context.Background(), Request{
		Origin: OriginReconcile, RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
		Candidate: v2.SessionRecord{GUID: "guid-outage", Tool: "codex", Bindings: []v2.BindingFact{{ID: "bus-attested", Field: v2.BindingFieldHcomName, Value: "bus-repaired", EvidenceClass: v2.EvidenceAttested, ObservedAt: "2026-07-17T00:01:00Z"}}},
		Seat:      SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"}, ObservedPane: &LivePane{PaneID: "pane-live", TerminalID: "terminal-live"}, RequireBus: true,
	})
	if err != nil || result.Refusal == nil || result.Refusal.Code != RefusalBusUnavailable {
		t.Fatalf("Complete result=%+v err=%v, want unavailable refusal", result, err)
	}
}

func TestNarrowEmptyContextFallbackRequiresUnchangedVerifiedSeat(t *testing.T) {
	joined := true
	verified := true
	current := v2.SessionRecord{
		GUID: "guid-fallback", State: v2.StateSeated, Tool: "codex",
		Seat: &v2.Seat{
			Kind: SeatHerdr, TerminalID: "terminal-live", PaneID: "pane-live",
			HcomName: "bus-live", HcomVerified: &verified,
		},
	}
	complete := func(t *testing.T, fallback v2.SessionRecord) Result {
		t.Helper()
		engine := testEngine(t)
		engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "bus-live", Joined: &joined}}, nil
		}
		engine.RepairLaunchContext = func(string, string, string) hcomidentity.LaunchContextRepair {
			return hcomidentity.LaunchContextRepair{Status: "written"}
		}
		result, err := engine.Complete(context.Background(), Request{
			Origin:       OriginReconcile,
			RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
			Candidate:    v2.SessionRecord{GUID: "guid-fallback", Tool: "codex"},
			Seat:         SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"},
			Fallback:     &NarrowFallback{Current: fallback},
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := complete(t, current); result.Refusal != nil {
		t.Fatalf("exact fallback refused: %+v", result.Refusal)
	}
	mutated := current
	seat := *current.Seat
	seat.TerminalID = "terminal-other"
	mutated.Seat = &seat
	if result := complete(t, mutated); result.Refusal == nil || result.Refusal.Code != RefusalBusRowMissing {
		t.Fatalf("mutated fallback result = %+v, want refusal", result)
	}
}

func TestCompletionOriginsProduceIdenticalSeatJSONForEverySeatKind(t *testing.T) {
	origins := []Origin{OriginSpawn, OriginEnroll, OriginEnrollRepair, OriginAdopt, OriginReclaim, OriginResume, OriginReconcile, OriginRecognition}
	shapes := []struct {
		name      string
		tool      string
		claim     SeatClaim
		evidence  hcomidentity.Evidence
		busRows   []hcomidentity.Row
		forbidBus bool
	}{
		{name: "herdr bus", tool: "codex", claim: SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"}, evidence: hcomidentity.Evidence{SessionID: "session-live"}, busRows: joinedRows(hcomidentity.Row{Name: "bus-live", SessionID: "session-live", LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-live"}})},
		{name: "process bus", tool: "codex", claim: SeatClaim{Kind: SeatProcess, PID: 4242}, evidence: hcomidentity.Evidence{ProcessID: "process-live"}, busRows: joinedRows(hcomidentity.Row{Name: "bus-live", LaunchContext: launchContext("process-live")})},
		{name: "herdr busless", tool: "bash", claim: SeatClaim{Kind: SeatHerdr, PaneID: "pane-live"}, forbidBus: true},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
			var want string
			for i, origin := range origins {
				engine := testEngine(t)
				engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
					if shape.forbidBus {
						t.Fatal("busless parity consulted hcom")
					}
					return shape.busRows, nil
				}
				result, err := engine.Complete(context.Background(), Request{
					Origin:       origin,
					RegistryPath: registryPath,
					Candidate:    v2.SessionRecord{GUID: "guid-parity-" + string(rune('a'+i)), Label: "worker-" + string(rune('a'+i)), Tool: shape.tool},
					Seat:         shape.claim,
					Namespace:    "/bus",
					Evidence:     shape.evidence,
				})
				if err != nil || result.Refusal != nil {
					t.Fatalf("origin %q result=%+v err=%v", origin, result, err)
				}
				row := decodeCompletedRow(t, result.Row)
				// Credential generations intentionally rotate at every completion;
				// parity covers the canonical seat shape apart from that owned field.
				row.Seat.CredentialGeneration = ""
				encoded, err := json.Marshal(row.Seat)
				if err != nil {
					t.Fatal(err)
				}
				if want == "" {
					want = string(encoded)
				} else if string(encoded) != want {
					t.Fatalf("origin %q seat = %s, want %s", origin, encoded, want)
				}
			}
		})
	}
}

func joinedRows(rows ...hcomidentity.Row) []hcomidentity.Row {
	joined := true
	for i := range rows {
		rows[i].Joined = &joined
	}
	return rows
}

func testEngine(t *testing.T) Engine {
	t.Helper()
	ids := 0
	return Engine{
		HerdrPane: func(context.Context, string) (LivePane, error) {
			return LivePane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
		},
		ListBus: func(context.Context, string) ([]hcomidentity.Row, error) {
			t.Fatal("unexpected bus list")
			return nil, nil
		},
		RepairLaunchContext: func(string, string, string) hcomidentity.LaunchContextRepair {
			t.Fatal("unexpected launch-context repair")
			return hcomidentity.LaunchContextRepair{}
		},
		ProcessAlive: func(int) bool { return true },
		Now:          func() time.Time { return time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC) },
		NewBindingID: func() (string, error) {
			ids++
			return "binding-" + string(rune('0'+ids)), nil
		},
	}
}

func decodeCompletedRow(t *testing.T, raw []byte) v2.SessionRecord {
	t.Helper()
	var row v2.SessionRecord
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func launchContext(processID string) hcomidentity.LaunchContext {
	raw := []byte(`{"process_id":"` + processID + `"}`)
	var out hcomidentity.LaunchContext
	_ = json.Unmarshal(raw, &out)
	return out
}
