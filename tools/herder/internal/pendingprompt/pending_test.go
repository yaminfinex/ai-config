package pendingprompt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManualDeliverySuppressesSidecarReplayAndRemovesPlaintext(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	record := Record{GUID: "child-guid", Sender: "sender", BusDir: "/bus", Message: "initial prompt", VerifyMS: 20}
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	paths := pathsFor(registryPath, record.GUID)
	if info, err := os.Stat(paths.pending); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("pending mode = %v err=%v, want 0600", info, err)
	}

	manualCalls := 0
	manual, err := Attempt(registryPath, record.GUID, record.Message, ActorManual, now, func(got Record) string {
		manualCalls++
		return "delivered"
	})
	if err != nil || !manual.Managed || manual.Suppressed || manual.Verdict != "delivered" || manualCalls != 1 {
		t.Fatalf("manual attempt = %+v calls=%d err=%v", manual, manualCalls, err)
	}
	if _, err := os.Stat(paths.pending); !os.IsNotExist(err) {
		t.Fatalf("plaintext pending record remains: %v", err)
	}

	sidecarCalls := 0
	sidecar, err := Attempt(registryPath, record.GUID, "", ActorSidecar, now, func(Record) string {
		sidecarCalls++
		return "delivered"
	})
	if err != nil || !sidecar.Managed || !sidecar.Suppressed || sidecar.Verdict != "already_delivered" || sidecarCalls != 0 {
		t.Fatalf("sidecar attempt = %+v calls=%d err=%v", sidecar, sidecarCalls, err)
	}
	if _, err := os.Stat(paths.marker); !os.IsNotExist(err) {
		t.Fatalf("delivery marker remains after competing actor observed it: %v", err)
	}
}

func TestExpiredAndUnseatedStateIsCleanedUp(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	record := Record{GUID: "never-seated", Sender: "sender", Message: "prompt", ExpiresAt: now.Add(time.Minute)}
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	paths := pathsFor(registryPath, record.GUID)
	if err := Prune(registryPath, record.GUID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.pending); !os.IsNotExist(err) {
		t.Fatalf("expired pending record remains: %v", err)
	}

	record.GUID = "culled-child"
	record.ExpiresAt = now.Add(time.Hour)
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(registryPath, record.GUID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pathsFor(registryPath, record.GUID).pending); !os.IsNotExist(err) {
		t.Fatalf("culled pending record remains: %v", err)
	}
}

func TestPruneAllRemovesExpiredSeatlessRecords(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	for _, record := range []Record{
		{GUID: "expired-child", Sender: "sender", Message: "expired", ExpiresAt: now.Add(-time.Minute)},
		{GUID: "live-child", Sender: "sender", Message: "live", ExpiresAt: now.Add(time.Hour)},
	} {
		// Store before each record's expiry so the store-time sweep does not
		// remove the fixture that this test intends PruneAll to collect.
		if err := Store(registryPath, record, now.Add(-2*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneAll(registryPath, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pathsFor(registryPath, "expired-child").pending); !os.IsNotExist(err) {
		t.Fatalf("expired seatless record remains: %v", err)
	}
	if _, err := os.Stat(pathsFor(registryPath, "live-child").pending); err != nil {
		t.Fatalf("unexpired record was pruned: %v", err)
	}
}

func TestFailedDeliveryRemainsPending(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	now := time.Now().UTC()
	record := Record{GUID: "child-guid", Sender: "sender", Message: "prompt"}
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	result, err := Attempt(registryPath, record.GUID, record.Message, ActorSidecar, now, func(Record) string { return "send_failed" })
	if err != nil || !result.Managed || result.Verdict != "send_failed" {
		t.Fatalf("attempt = %+v err=%v", result, err)
	}
	if _, err := os.Stat(pathsFor(registryPath, record.GUID).pending); err != nil {
		t.Fatalf("failed delivery lost pending record: %v", err)
	}
}
