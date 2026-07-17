package repaircmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
)

func TestParseRefusesOutOfVocabularyAndMultipleFields(t *testing.T) {
	for _, args := range [][]string{
		{"rebind", "--guid", "guid-one", "--field", "label", "--value", "x"},
		{"rebind", "--guid", "guid-one", "--field", "seat", "--value", "x"},
		{"rebind", "--guid", "guid-one", "--field", "hcom_name", "--value", "x", "extra"},
		{"rebind", "--guid", "guid-one", "--field", "hcom_name", "--value", "x\nforged"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Fatalf("parseArgs(%q) succeeded", args)
		}
	}
}

func TestNoAttestationRefusesWithoutMutation(t *testing.T) {
	service, path := testService(t)
	service.CollectProof = func(context.Context, v2.SessionRecord, Request) (Proof, error) {
		return Proof{}, ErrAttestationRequired
	}
	before := readFile(t, path)
	_, err := service.Execute(context.Background(), Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldHcomName, Value: "bus-new"})
	if err == nil || !strings.Contains(err.Error(), "attestation") {
		t.Fatalf("Execute err = %v", err)
	}
	if after := readFile(t, path); after != before {
		t.Fatalf("registry changed on missing attestation")
	}
}

func TestSuccessfulBusRebindPreservesIdentityAndTombstonesOldBinding(t *testing.T) {
	service, path := testService(t)
	result, err := service.Execute(context.Background(), Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldHcomName, Value: "bus-new"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != registry.WriteApplied {
		t.Fatalf("status = %q", result.Status)
	}
	rec := loadCurrent(t, path)
	if rec.Label != "stable-label" || rec.Role != "worker" || rec.Lineage.ForkedFrom != "parent-guid" {
		t.Fatalf("identity changed: %+v", rec)
	}
	if rec.Seat == nil || rec.Seat.HcomName != "bus-new" || rec.Event != v2.EventAttestedBinding {
		t.Fatalf("repaired row = %+v", rec)
	}
	if len(rec.Attestations) != 1 || len(rec.BindingTombstones) != 1 || rec.BindingTombstones[0].BindingID != "binding-old-bus" {
		t.Fatalf("audit histories = attestations %+v tombstones %+v", rec.Attestations, rec.BindingTombstones)
	}
}

func TestCommittedRepairIsAuditedAndRateLimited(t *testing.T) {
	service, path := testService(t)
	if _, err := service.Execute(context.Background(), Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldHcomName, Value: "bus-new"}); err != nil {
		t.Fatal(err)
	}
	service.Now = func() time.Time { return time.Date(2026, 7, 17, 0, 2, 0, 0, time.UTC) }
	_, err := service.Execute(context.Background(), Request{Operation: OperationReissueCredential, GUID: "guid-repair"})
	if err == nil || !strings.Contains(err.Error(), "one committed break-glass operation per 10m") || !strings.Contains(err.Error(), "retry in 8m") {
		t.Fatalf("rate-limit err = %v", err)
	}
	if got := len(loadCurrent(t, path).Attestations); got != 1 {
		t.Fatalf("attestations = %d, want 1", got)
	}
}

func TestAcceptedSameUIDPTYForgeryPathCompletesCeremonyAndCommitsAudit(t *testing.T) {
	service, path := testService(t)
	master, slave := openTestPTY(t)
	request := Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldHcomName, Value: "bus-new"}
	challenge := "same-uid-loopback-challenge"
	expected := attestationStatement(challenge, request)
	var visibleMu sync.Mutex
	visible := ""
	injected := make(chan struct{})
	observedTwice := make(chan struct{})
	readCount := 0
	automationDone := make(chan error, 1)
	go func() {
		visibleMu.Lock()
		visible = challenge
		visibleMu.Unlock()
		close(injected)
		<-observedTwice
		visibleMu.Lock()
		visible = ""
		visibleMu.Unlock()
		_, err := master.WriteString(expected + "\n")
		automationDone <- err
	}()

	collector := DefaultProofCollector(io.Discard)
	collector.OpenTTY = func() (*os.File, error) { return slave, nil }
	collector.PaneGet = func(context.Context, string) (herdrcli.Pane, error) {
		return herdrcli.Pane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
	}
	collector.ReadVisible = func(context.Context, string) (string, error) {
		if readCount == 0 {
			<-injected
		}
		visibleMu.Lock()
		defer visibleMu.Unlock()
		readCount++
		if readCount == 2 {
			close(observedTwice)
		}
		return visible, nil
	}
	collector.NewChallenge = func() (string, error) { return challenge, nil }
	collector.Wait = func(time.Duration) {}
	service.CollectProof = collector.Collect

	result, err := service.Execute(context.Background(), request)
	if err != nil || result.Status != registry.WriteApplied {
		t.Fatalf("same-uid loopback result=%+v err=%v", result, err)
	}
	if err := <-automationDone; err != nil {
		t.Fatal(err)
	}
	rec := loadCurrent(t, path)
	if len(rec.Attestations) != 1 || rec.Attestations[0].Statement != expected || len(rec.BindingTombstones) != 1 {
		t.Fatalf("same-uid loopback audit = %+v tombstones=%+v", rec.Attestations, rec.BindingTombstones)
	}
	service.Now = func() time.Time { return time.Date(2026, 7, 17, 0, 2, 0, 0, time.UTC) }
	_, err = service.Execute(context.Background(), Request{Operation: OperationReissueCredential, GUID: "guid-repair"})
	if err == nil || !strings.Contains(err.Error(), "rate limit") || !strings.Contains(err.Error(), "retry in 8m") {
		t.Fatalf("post-forgery rate-limit err = %v", err)
	}
}

func TestConcurrentRepairsCommitOnceAndLoserGetsRateWindow(t *testing.T) {
	tests := []struct {
		name      string
		request   Request
		configure func(*Service)
	}{
		{
			name:    "identity rebind completion finalizer",
			request: Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldHcomName, Value: "bus-new"},
		},
		{
			name:    "empty launch context completion finalizer",
			request: Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldLaunchContext, Value: "pane-live"},
			configure: func(service *Service) {
				joined := true
				service.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
					return []hcomidentity.Row{{Name: "bus-old", Joined: &joined}}, nil
				}
			},
		},
		{
			name:    "wrong launch context authorization append",
			request: Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldLaunchContext, Value: "pane-live"},
			configure: func(service *Service) {
				joined := true
				service.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
					return []hcomidentity.Row{{Name: "bus-old", Joined: &joined, LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-wrong"}}}, nil
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, path := testService(t)
			if tt.configure != nil {
				tt.configure(&service)
			}
			arrived := make(chan struct{}, 2)
			release := make(chan struct{})
			service.CollectProof = func(context.Context, v2.SessionRecord, Request) (Proof, error) {
				arrived <- struct{}{}
				<-release
				return Proof{Statement: "explicit statement", PaneID: "pane-live", TerminalID: "terminal-live"}, nil
			}
			type execution struct {
				result Result
				err    error
			}
			results := make(chan execution, 2)
			for range 2 {
				go func() {
					result, err := service.Execute(context.Background(), tt.request)
					results <- execution{result: result, err: err}
				}()
			}
			<-arrived
			<-arrived
			close(release)

			var successes int
			var loser error
			for range 2 {
				execution := <-results
				if execution.err == nil {
					successes++
				} else {
					loser = execution.err
				}
			}
			if successes != 1 {
				t.Fatalf("successful executions = %d, want exactly one", successes)
			}
			if loser == nil || !strings.Contains(loser.Error(), "rate limit") || !strings.Contains(loser.Error(), "retry in 10m") {
				t.Fatalf("loser error = %v, want rate limit with remaining window", loser)
			}
			if got := len(loadCurrent(t, path).Attestations); got != 1 {
				t.Fatalf("committed attestations = %d, want 1", got)
			}
		})
	}
}

func TestSuccessfulSIDRebindAppendsAttestedHistoryAndTombstonesLegacyValue(t *testing.T) {
	service, path := testService(t)
	result, err := service.Execute(context.Background(), Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldSID, Value: "sid-repaired"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != registry.WriteApplied {
		t.Fatalf("status = %q", result.Status)
	}
	rec := loadCurrent(t, path)
	if len(rec.SIDs) != 2 || rec.SIDs[1].SID != "sid-repaired" {
		t.Fatalf("sids = %+v", rec.SIDs)
	}
	if len(rec.BindingTombstones) != 1 || rec.BindingTombstones[0].Field != v2.BindingFieldSID {
		t.Fatalf("legacy prior sid tombstone = %+v", rec.BindingTombstones)
	}
	fact, status := registry.LatestSufficientBinding(rec, v2.BindingFieldSID, registry.LiveEvidenceAbsent)
	if status != registry.BindingSelected || fact.Value != "sid-repaired" || fact.AttestationID == "" {
		t.Fatalf("sid fact = %+v status=%q", fact, status)
	}
}

func TestReissueCredentialAuthenticatesWithoutRebindingIdentity(t *testing.T) {
	service, path := testService(t)
	before := loadCurrent(t, path)
	result, err := service.Execute(context.Background(), Request{Operation: OperationReissueCredential, GUID: "guid-repair"})
	if err != nil || result.Status != registry.WriteApplied {
		t.Fatalf("reissue result=%+v err=%v", result, err)
	}
	after := loadCurrent(t, path)
	if after.Seat.HcomName != before.Seat.HcomName || len(after.SIDs) != len(before.SIDs) || len(after.BindingTombstones) != 0 {
		t.Fatalf("reissue changed identity before=%+v after=%+v", before, after)
	}
	if len(after.Attestations) != 1 || after.Attestations[0].Operation != v2.AttestationReissueCredential {
		t.Fatalf("reissue audit = %+v", after.Attestations)
	}
}

func TestWrongNonemptyLaunchContextRecordsAuthorizationWithoutRewrite(t *testing.T) {
	service, path := testService(t)
	joined := true
	service.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "bus-old", Joined: &joined, LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-wrong"}}}, nil
	}
	result, err := service.Execute(context.Background(), Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldLaunchContext, Value: "pane-live"})
	if err != nil || !result.UpstreamGated {
		t.Fatalf("wrong context result=%+v err=%v", result, err)
	}
	rec := loadCurrent(t, path)
	if len(rec.Attestations) != 1 || rec.Attestations[0].Operation != v2.AttestationAuthorizeRecreate || rec.Seat.PaneID != "pane-live" {
		t.Fatalf("authorization row = %+v", rec)
	}
}

func TestEmptyLaunchContextEndsInAttestedCompletion(t *testing.T) {
	service, path := testService(t)
	joined := true
	service.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "bus-old", Joined: &joined}}, nil
	}
	result, err := service.Execute(context.Background(), Request{Operation: OperationRebind, GUID: "guid-repair", Field: v2.BindingFieldLaunchContext, Value: "pane-live"})
	if err != nil || result.Status != registry.WriteApplied {
		t.Fatalf("empty context result=%+v err=%v", result, err)
	}
	if rec := loadCurrent(t, path); len(rec.Attestations) != 1 || rec.Attestations[0].Field != v2.BindingFieldLaunchContext {
		t.Fatalf("completed row = %+v", rec)
	}
}

func TestRunPrintsBreakGlassLoudnessOnFailedProof(t *testing.T) {
	service, _ := testService(t)
	service.CollectProof = func(context.Context, v2.SessionRecord, Request) (Proof, error) {
		return Proof{}, ErrCorroborationFailed
	}
	var stderr bytes.Buffer
	rc := runWithService([]string{"rebind", "--guid", "guid-repair", "--field", "hcom_name", "--value", "bus-new"}, &bytes.Buffer{}, &stderr, service)
	if rc == 0 || !strings.Contains(stderr.String(), "BREAK-GLASS") || !strings.Contains(stderr.String(), "corroboration") {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
}

func testService(t *testing.T) (Service, string) {
	t.Helper()
	path := t.TempDir() + "/registry.jsonl"
	verified := true
	mustUpdateRegistry(t, path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			GUID: "guid-repair", Event: "seated", State: v2.StateSeated, Label: "stable-label", Role: "worker", Tool: "codex",
			Lineage: v2.Lineage{ForkedFrom: "parent-guid"},
			SIDs:    []v2.SID{{SID: "sid-old", Source: v2.EvidenceHarvest, ObservedAt: "2026-07-17T00:00:00Z"}},
			Seat:    &v2.Seat{Kind: "herdr", Node: tx.NodeID, TerminalID: "terminal-live", PaneID: "pane-live", HcomName: "bus-old", HcomVerified: &verified, Namespace: "/bus"},
			Bindings: []v2.BindingFact{
				{ID: "binding-seat", Field: v2.BindingFieldSeat, EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: "2026-07-17T00:00:00Z", Seat: &v2.BindingSeat{Kind: "herdr", Node: tx.NodeID, TerminalID: "terminal-live", PaneID: "pane-live", Namespace: "/bus"}},
				{ID: "binding-old-bus", Field: v2.BindingFieldHcomName, Value: "bus-old", EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: "2026-07-17T00:00:00Z"},
			},
		}}, nil
	})
	var idMu sync.Mutex
	idCounter := 0
	nextID := func() (string, error) {
		idMu.Lock()
		defer idMu.Unlock()
		idCounter++
		return fmt.Sprintf("repair-test-id-%d", idCounter), nil
	}
	now := func() time.Time { return time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC) }
	complete := func(ctx context.Context, request seatcompletion.Request) (seatcompletion.Result, error) {
		engine := seatcompletion.Engine{
			HerdrPane: func(context.Context, string) (seatcompletion.LivePane, error) {
				return seatcompletion.LivePane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
			},
			ListBus: func(context.Context, string) ([]hcomidentity.Row, error) {
				joined := true
				name := request.Candidate.Seat.HcomName
				paneID := "pane-live"
				if request.Attested != nil && request.Attested.Field == v2.BindingFieldHcomName {
					paneID = ""
				}
				return []hcomidentity.Row{{Name: name, Status: "listening", Joined: &joined, SessionID: "sid-new", LaunchContext: hcomidentity.LaunchContext{PaneID: paneID}}}, nil
			},
			RepairLaunchContext: func(string, string, string) hcomidentity.LaunchContextRepair {
				return hcomidentity.LaunchContextRepair{Status: "written", PaneID: "pane-live"}
			},
			Now: now, NewBindingID: nextID, UpdateRegistry: registry.UpdateLocked,
		}
		return engine.Complete(ctx, request)
	}
	return Service{
		RegistryPath: path, Now: now, NewID: nextID, Complete: complete,
		ListBus: func(context.Context, string) ([]hcomidentity.Row, error) {
			joined := true
			return []hcomidentity.Row{{Name: "bus-old", Joined: &joined}}, nil
		},
		CollectProof: func(context.Context, v2.SessionRecord, Request) (Proof, error) {
			return Proof{Statement: "explicit statement", PaneID: "pane-live", TerminalID: "terminal-live"}, nil
		},
	}, path
}

func mustUpdateRegistry(t *testing.T, path string, fn registry.LockedUpdateFunc) {
	t.Helper()
	outcomes, err := registry.UpdateLocked(path, fn)
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func loadCurrent(t *testing.T, path string) v2.SessionRecord {
	t.Helper()
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return projection.Sessions()[0]
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func openTestPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = slave.Close()
		_ = master.Close()
	})
	return master, slave
}
