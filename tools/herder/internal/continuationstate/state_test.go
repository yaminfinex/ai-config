package continuationstate

import (
	"os"
	"testing"
	"time"
)

func TestFailureRemainsUnresolvedUntilAcknowledged(t *testing.T) {
	dir := t.TempDir()
	rec := Record{
		ID: "compact-then-self-42", Status: "failed", Target: "worker-hone",
		UpdatedAt: "2026-07-12T12:00:00Z", Reason: "turn end never proven",
		LogPath: "/tmp/diagnostic.log", RecoveryCommand: "herder send worker-hone -- 'continue'",
	}
	if err := Write(dir, rec); err != nil {
		t.Fatal(err)
	}
	failed, err := Unresolved(dir)
	if err != nil || len(failed) != 1 {
		t.Fatalf("Unresolved = %+v, %v; want one failure", failed, err)
	}
	if _, err := Acknowledge(dir, rec.ID, time.Date(2026, 7, 12, 12, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	failed, err = Unresolved(dir)
	if err != nil || len(failed) != 0 {
		t.Fatalf("Unresolved after acknowledgement = %+v, %v; want none", failed, err)
	}
	if _, err := os.Stat(dir + "/" + rec.ID + ".json"); err != nil {
		t.Fatalf("acknowledgement removed durable record: %v", err)
	}
}

func TestClosedOutcomesAreNotUnresolved(t *testing.T) {
	dir := t.TempDir()
	for _, status := range []string{"armed", "delivered", "queued"} {
		if err := Write(dir, Record{ID: status, Status: status, UpdatedAt: "2026-07-12T12:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	failed, err := Unresolved(dir)
	if err != nil || len(failed) != 0 {
		t.Fatalf("Unresolved = %+v, %v; want no closed outcomes", failed, err)
	}
}

func TestAdvanceRetainsLifecycleTransitions(t *testing.T) {
	dir := t.TempDir()
	rec := Record{ID: "sender-42", Status: "armed", UpdatedAt: "2026-07-12T12:00:00Z"}
	if err := Advance(dir, rec); err != nil {
		t.Fatal(err)
	}
	rec.Status = "delivered"
	rec.UpdatedAt = "2026-07-12T12:00:01Z"
	if err := Advance(dir, rec); err != nil {
		t.Fatal(err)
	}
	records, err := ReadAll(dir)
	if err != nil || len(records) != 1 || len(records[0].Lifecycle) != 2 {
		t.Fatalf("records = %+v, %v; want armed and delivered transitions", records, err)
	}
	if records[0].Lifecycle[0].Status != "armed" || records[0].Lifecycle[1].Status != "delivered" {
		t.Fatalf("lifecycle = %+v, want armed then delivered", records[0].Lifecycle)
	}
}
