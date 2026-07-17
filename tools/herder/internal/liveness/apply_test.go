package liveness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestApplyRejectsNonDeathVerdictWithoutWriting(t *testing.T) {
	path, rec := seededSeated(t)
	before, _ := os.ReadFile(path)
	_, err := ApplyPositiveDeath(path, rec.GUID, Anchor(rec.Seat), Verdict{Class: VerdictAlive, Cause: CauseLiveEvidence}, time.Now(), "test_probe")
	if err != ErrNotPositiveDeath {
		t.Fatalf("error = %v, want ErrNotPositiveDeath; removing the applier class guard must fail this test", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("non-death verdict changed registry bytes")
	}
}

func TestEvidenceConflictCannotBeApplied(t *testing.T) {
	path, rec := seededSeated(t)
	before, _ := os.ReadFile(path)
	conflict := Evaluate(Input{Holder: Signal{State: StateAlive}, Pane: Signal{State: StateDead}, PaneEpoch: EpochSame})
	_, err := ApplyPositiveDeath(path, rec.GUID, Anchor(rec.Seat), conflict, time.Now(), "test_probe")
	if err != ErrNotPositiveDeath {
		t.Fatalf("error = %v, want ErrNotPositiveDeath; conflict-to-unseat mutation must fail this test", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("evidence conflict appended an unseat")
	}
}

func TestApplyPositiveDeathRecordsEvidenceAndFirstObserverWins(t *testing.T) {
	path, rec := seededSeated(t)
	stamp := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	verdict := Evaluate(Input{Holder: Signal{State: StateDead, ObservedVia: "holder_wait"}})
	first, err := ApplyPositiveDeath(path, rec.GUID, Anchor(rec.Seat), verdict, stamp, "sidecar")
	if err != nil || first.Status != registry.WriteApplied {
		t.Fatalf("first apply = %+v, %v", first, err)
	}
	second, err := ApplyPositiveDeath(path, rec.GUID, Anchor(rec.Seat), verdict, stamp.Add(time.Hour), "observer")
	if err != nil || second.Status != registry.WriteNoop {
		t.Fatalf("second apply = %+v, %v", second, err)
	}
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.V2ByGUID(proj, rec.GUID)
	if got == nil || got.State != v2.StateUnseated || got.RecordedAt != stamp.Format(time.RFC3339) || got.CloseResult != "observed_dead" {
		t.Fatalf("latest = %+v", got)
	}
	if got.CloseReason != "cause_class=holder_exited; evidence=holder_exited" || got.ObservedVia != "holder_wait+sidecar" {
		t.Fatalf("evidence fields = reason=%q via=%q", got.CloseReason, got.ObservedVia)
	}
}

func TestApplyRefusesStaleSeatAnchor(t *testing.T) {
	path, rec := seededSeated(t)
	anchor := Anchor(rec.Seat)
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		current := registry.V2ByGUID(tx.Projection, rec.GUID)
		next := *current
		seat := *current.Seat
		seat.PaneID = "pane-reborn"
		next.Seat = &seat
		next.Event = "reconciled"
		next.RecordedAt = "2026-07-17T11:00:00Z"
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		t.Fatal(err)
	}
	if err := outcome.Err(); err != nil {
		t.Fatal(err)
	}
	verdict := Evaluate(Input{Holder: Signal{State: StateDead, ObservedVia: "holder_wait"}})
	if _, err := ApplyPositiveDeath(path, rec.GUID, anchor, verdict, time.Now(), "sidecar"); err != ErrSeatChanged {
		t.Fatalf("error = %v, want ErrSeatChanged", err)
	}
}

func seededSeated(t *testing.T) (string, v2.SessionRecord) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	verified := true
	rec := v2.SessionRecord{
		Kind: v2.KindSession, GUID: "guid-live", Event: "seated", RecordedAt: "2026-07-17T10:00:00Z",
		State: v2.StateSeated, Label: "worker", Role: "worker", Tool: "codex",
		Seat: &v2.Seat{Kind: "herdr", Node: "node-local", TerminalID: "terminal-live", PaneID: "pane-live", HcomName: "bus-live", HcomVerified: &verified},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, rec
}
