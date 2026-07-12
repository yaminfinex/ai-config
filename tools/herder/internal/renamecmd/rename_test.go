package renamecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestTransferMovesLabelAndPreservesLifecycleStates(t *testing.T) {
	path := seedTransferRegistry(t,
		v2.SessionRecord{GUID: "guid-current", State: v2.StateSeated, Label: "temporary", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_current", PaneID: "pane_current"}},
		v2.SessionRecord{GUID: "guid-previous", State: v2.StateUnseated, Label: "stable"},
	)
	result, err := Transfer(path, "guid-current", "stable", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetGUID != "guid-current" || result.SourceGUID != "guid-previous" || result.Label != "stable" || result.TargetTerminalID != "term_current" {
		t.Fatalf("result = %+v", result)
	}
	current := latestTransferSession(t, path, "guid-current")
	previous := latestTransferSession(t, path, "guid-previous")
	if current.State != v2.StateSeated || current.Label != "stable" || current.Seat == nil || current.Seat.TerminalID != "term_current" {
		t.Fatalf("target = %+v, want seated stable target with original seat", current)
	}
	if previous.State != v2.StateUnseated || previous.Label != "" || previous.Seat != nil {
		t.Fatalf("source = %+v, want unseated unlabelled source", previous)
	}
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owner := registry.V2LabelOwner(proj, "stable", "")
	if owner == nil || owner.GUID != "guid-current" {
		t.Fatalf("stable owner = %+v, want guid-current", owner)
	}
}

func TestTransferRefusesSourceLifecycleStates(t *testing.T) {
	cases := []struct {
		name        string
		state       string
		confirmLive bool
		want        string
	}{
		{name: "seated without confirmation", state: v2.StateSeated, want: "seated-and-live"},
		{name: "lost despite confirmation", state: v2.StateLost, confirmLive: true, want: "LOST sessions"},
		{name: "retired despite confirmation", state: v2.StateRetired, confirmLive: true, want: "use plain 'herder rename"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := v2.SessionRecord{GUID: "guid-source", State: tc.state, Label: "stable"}
			if tc.state == v2.StateSeated {
				source.Seat = &v2.Seat{Kind: "herdr", TerminalID: "term_source"}
			}
			path := seedTransferRegistry(t,
				v2.SessionRecord{GUID: "guid-target", State: v2.StateUnseated, Label: "temporary"},
				source,
			)
			before := mustReadTransferFile(t, path)
			_, err := Transfer(path, "guid-target", "guid-source", tc.confirmLive)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Transfer error = %v, want containing %q", err, tc.want)
			}
			after := mustReadTransferFile(t, path)
			if before != after {
				t.Fatalf("refusal changed registry\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestTransferAllowsConfirmedSeatedSource(t *testing.T) {
	path := seedTransferRegistry(t,
		v2.SessionRecord{GUID: "guid-target", State: v2.StateSeated, Label: "temporary", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_target"}},
		v2.SessionRecord{GUID: "guid-source", State: v2.StateSeated, Label: "stable", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_source"}},
	)
	if _, err := Transfer(path, "guid-target", "guid-source", true); err != nil {
		t.Fatal(err)
	}
	if got := latestTransferSession(t, path, "guid-source"); got.State != v2.StateSeated || got.Label != "" || got.Seat == nil {
		t.Fatalf("source = %+v, want seated unlabelled source with seat preserved", got)
	}
}

func TestAdoptionTransferAtomicallyUnseatsSourceAndMovesLabel(t *testing.T) {
	path := seedTransferRegistry(t,
		v2.SessionRecord{GUID: "guid-replacement", State: v2.StateSeated, Label: "temporary", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_replacement", PaneID: "pane_replacement"}},
		v2.SessionRecord{GUID: "guid-previous", State: v2.StateSeated, Label: "stable", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_previous", PaneID: "pane_replacement"}},
	)
	result, err := TransferForAdoption(path, "guid-replacement", "guid-previous", "pane_replacement", AdoptionReasonSeatSuperseded)
	if err != nil {
		t.Fatal(err)
	}
	if result.Label != "stable" || result.SourceGUID != "guid-previous" || result.TargetGUID != "guid-replacement" {
		t.Fatalf("result = %+v", result)
	}
	previous := latestTransferSession(t, path, "guid-previous")
	if previous.Event != "adoption_source_released" || previous.State != v2.StateUnseated || previous.Label != "" || previous.Seat != nil {
		t.Fatalf("source = %+v, want atomically unseated and unlabelled", previous)
	}
	if previous.CloseResult != "adopted" || previous.CloseReason != AdoptionReasonSeatSuperseded {
		t.Fatalf("source evidence = %+v", previous)
	}
	replacement := latestTransferSession(t, path, "guid-replacement")
	if replacement.State != v2.StateSeated || replacement.Label != "stable" || replacement.Seat == nil {
		t.Fatalf("target = %+v, want seated stable replacement", replacement)
	}
	data := mustReadTransferFile(t, path)
	if !strings.Contains(data, `"event":"adoption_source_released"`) || !strings.Contains(data, `"event":"label_transferred"`) {
		t.Fatalf("registry lacks both atomic batch rows:\n%s", data)
	}
}

func TestAdoptionTransferExposesNoIntermediateRelease(t *testing.T) {
	path := seedTransferRegistry(t,
		v2.SessionRecord{GUID: "guid-replacement", State: v2.StateSeated, Label: "temporary", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_replacement", PaneID: "pane_replacement"}},
		v2.SessionRecord{GUID: "guid-previous", State: v2.StateSeated, Label: "stable", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_previous", PaneID: "pane_previous"}},
	)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
			rows, _, candidateErr := transferCandidatesWithOptions(tx, "guid-replacement", "guid-previous", transferOptions{
				unseatSource:       true,
				expectedSourcePane: "pane_previous",
				unseatReason:       AdoptionReasonConfirmedDead,
			})
			close(entered)
			<-release
			return rows, candidateErr
		})
		if err == nil {
			for _, outcome := range outcomes {
				if outcome.Status != registry.WriteApplied {
					err = outcome.Err()
					if err == nil {
						err = &unexpectedOutcomeError{status: outcome.Status}
					}
					break
				}
			}
		}
		done <- err
	}()
	<-entered

	before := latestTransferSession(t, path, "guid-previous")
	if before.State != v2.StateSeated || before.Label != "stable" || before.Seat == nil {
		t.Fatalf("source became observably partial before commit: %+v", before)
	}
	if target := latestTransferSession(t, path, "guid-replacement"); target.Label != "temporary" {
		t.Fatalf("target changed before source release committed: %+v", target)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after := latestTransferSession(t, path, "guid-previous")
	if after.State != v2.StateUnseated || after.Label != "" || after.Seat != nil {
		t.Fatalf("source after commit = %+v", after)
	}
	if target := latestTransferSession(t, path, "guid-replacement"); target.Label != "stable" {
		t.Fatalf("target after commit = %+v", target)
	}
}

func TestAdoptionTransferRefusesChangedSourceSeat(t *testing.T) {
	path := seedTransferRegistry(t,
		v2.SessionRecord{GUID: "guid-replacement", State: v2.StateSeated, Label: "temporary", Seat: &v2.Seat{Kind: "herdr", PaneID: "pane_replacement"}},
		v2.SessionRecord{GUID: "guid-previous", State: v2.StateSeated, Label: "stable", Seat: &v2.Seat{Kind: "herdr", PaneID: "pane_current"}},
	)
	before := mustReadTransferFile(t, path)
	_, err := TransferForAdoption(path, "guid-replacement", "guid-previous", "pane_preflight", AdoptionReasonConfirmedDead)
	if err == nil || !strings.Contains(err.Error(), "seat changed after adoption preflight") {
		t.Fatalf("error = %v, want changed-seat refusal", err)
	}
	if after := mustReadTransferFile(t, path); after != before {
		t.Fatalf("changed-seat refusal wrote registry\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestTransferHoldsLockAcrossBothCandidates(t *testing.T) {
	path := seedTransferRegistry(t,
		v2.SessionRecord{GUID: "guid-target", State: v2.StateUnseated, Label: "temporary"},
		v2.SessionRecord{GUID: "guid-source", State: v2.StateUnseated, Label: "stable"},
	)
	entered := make(chan struct{})
	release := make(chan struct{})
	transferDone := make(chan error, 1)
	go func() {
		outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
			rows, _, candidateErr := transferCandidates(tx, "guid-target", "guid-source", false)
			close(entered)
			<-release
			return rows, candidateErr
		})
		if err == nil {
			for _, outcome := range outcomes {
				if outcome.Status != registry.WriteApplied {
					err = outcome.Err()
					if err == nil {
						err = &unexpectedOutcomeError{status: outcome.Status}
					}
					break
				}
			}
		}
		transferDone <- err
	}()
	<-entered

	writerStarted := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		close(writerStarted)
		outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
			return []v2.SessionRecord{{GUID: "guid-writer", Event: "registered", State: v2.StateUnseated, Label: "other"}}, nil
		})
		if err == nil {
			for _, outcome := range outcomes {
				err = outcome.Err()
			}
		}
		writerDone <- err
	}()
	<-writerStarted

	select {
	case err := <-writerDone:
		t.Fatalf("concurrent writer completed while transfer callback held lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.V2ByGUID(proj, "guid-source"); got == nil || got.Label != "stable" {
		t.Fatalf("source changed before atomic transfer committed: %+v", got)
	}
	if got := registry.V2ByGUID(proj, "guid-target"); got == nil || got.Label != "temporary" {
		t.Fatalf("target changed before atomic transfer committed: %+v", got)
	}

	close(release)
	if err := <-transferDone; err != nil {
		t.Fatal(err)
	}
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
	if got := latestTransferSession(t, path, "guid-target"); got.Label != "stable" {
		t.Fatalf("target after transfer = %+v", got)
	}
}

type unexpectedOutcomeError struct{ status registry.WriteStatus }

func (e *unexpectedOutcomeError) Error() string {
	return "unexpected write outcome: " + string(e.status)
}

func seedTransferRegistry(t *testing.T, recs ...v2.SessionRecord) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		for i := range recs {
			recs[i].Kind = v2.KindSession
			recs[i].Event = "registered"
			recs[i].RecordedAt = "2026-07-12T00:00:00Z"
		}
		return recs, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func latestTransferSession(t *testing.T, path, guid string) v2.SessionRecord {
	t.Helper()
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.V2ByGUID(proj, guid)
	if rec == nil {
		t.Fatalf("missing guid %s", guid)
	}
	return *rec
}

func mustReadTransferFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
