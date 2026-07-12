package continuationstate

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	failed, warnings, err := Unresolved(dir)
	if err != nil || len(warnings) != 0 || len(failed) != 1 {
		t.Fatalf("Unresolved = %+v, warnings=%v, err=%v; want one failure", failed, warnings, err)
	}
	if _, err := Acknowledge(dir, rec.ID, time.Date(2026, 7, 12, 12, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	failed, warnings, err = Unresolved(dir)
	if err != nil || len(warnings) != 0 || len(failed) != 0 {
		t.Fatalf("Unresolved after acknowledgement = %+v, warnings=%v, err=%v; want none", failed, warnings, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, archiveDirName, rec.ID+".json"))
	if err != nil {
		t.Fatalf("acknowledgement did not archive durable record: %v", err)
	}
	var archived Record
	if json.Unmarshal(b, &archived) != nil || archived.AcknowledgedAt == "" {
		t.Fatalf("archived acknowledgement = %+v, want timestamp", archived)
	}
}

func TestClosedOutcomesAreNotUnresolved(t *testing.T) {
	dir := t.TempDir()
	if err := Advance(dir, Record{ID: "armed", Status: "armed", UpdatedAt: "2026-07-12T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"delivered", "queued"} {
		rec := Record{ID: status, Status: "armed", UpdatedAt: "2026-07-12T12:00:00Z"}
		if err := Advance(dir, rec); err != nil {
			t.Fatal(err)
		}
		rec.Status = status
		rec.UpdatedAt = "2026-07-12T12:00:01Z"
		if err := Advance(dir, rec); err != nil {
			t.Fatal(err)
		}
	}
	records, warnings, err := ReadAll(dir)
	if err != nil || len(warnings) != 0 || len(records) != 1 || records[0].Status != "armed" {
		t.Fatalf("hot records = %+v, warnings=%v, err=%v; want only armed", records, warnings, err)
	}
	archived, err := os.ReadDir(filepath.Join(dir, archiveDirName))
	if err != nil || len(archived) != 2 {
		t.Fatalf("archive = %+v, %v; want delivered and queued", archived, err)
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
	b, err := os.ReadFile(filepath.Join(dir, archiveDirName, rec.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var archived Record
	if json.Unmarshal(b, &archived) != nil || len(archived.Lifecycle) != 2 {
		t.Fatalf("archived record = %+v; want armed and delivered transitions", archived)
	}
	if archived.Lifecycle[0].Status != "armed" || archived.Lifecycle[1].Status != "delivered" {
		t.Fatalf("lifecycle = %+v, want armed then delivered", archived.Lifecycle)
	}
}

func TestReadAllSkipsForeignRecordWithoutHidingFailure(t *testing.T) {
	dir := t.TempDir()
	rec := Record{ID: "failed", Status: "failed", UpdatedAt: "2026-07-12T12:00:00Z"}
	if err := Write(dir, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foreign.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	failed, warnings, err := Unresolved(dir)
	if err != nil || len(failed) != 1 || failed[0].ID != rec.ID || len(warnings) != 1 {
		t.Fatalf("Unresolved = %+v, warnings=%v, err=%v; want valid failure plus one warning", failed, warnings, err)
	}
}
