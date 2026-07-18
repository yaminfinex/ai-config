package pendingprompt

import (
	"encoding/json"
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
	if _, err := os.Stat(paths.marker); err != nil {
		t.Fatalf("durable delivery marker missing after competing actor observed it: %v", err)
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
	if err := os.WriteFile(paths.lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Prune(registryPath, record.GUID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.pending); !os.IsNotExist(err) {
		t.Fatalf("expired pending record remains: %v", err)
	}
	if _, err := os.Stat(paths.lock); !os.IsNotExist(err) {
		t.Fatalf("legacy per-guid lock remains after prune: %v", err)
	}

	record.GUID = "culled-child"
	record.ExpiresAt = now.Add(time.Hour)
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathsFor(registryPath, record.GUID).lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(registryPath, record.GUID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pathsFor(registryPath, record.GUID).pending); !os.IsNotExist(err) {
		t.Fatalf("culled pending record remains: %v", err)
	}
	if _, err := os.Stat(pathsFor(registryPath, record.GUID).lock); !os.IsNotExist(err) {
		t.Fatalf("legacy per-guid lock remains after cleanup: %v", err)
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
	if _, err := os.Stat(pathsFor(registryPath, record.GUID).marker); !os.IsNotExist(err) {
		t.Fatalf("failed delivery left suppression marker: %v", err)
	}
}

func TestSymlinkReplacementIsRefusedBeforeDelivery(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	now := time.Now().UTC()
	record := Record{GUID: "child-guid", Sender: "sender", Message: "operator prompt"}
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	paths := pathsFor(registryPath, record.GUID)
	attack := record
	attack.Message = "attacker-controlled prompt"
	attackPath := filepath.Join(t.TempDir(), "replacement.json")
	data, err := json.Marshal(attack)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attackPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.pending); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(attackPath, paths.pending); err != nil {
		t.Fatal(err)
	}

	calls := 0
	result, err := Attempt(registryPath, record.GUID, "", ActorSidecar, now, func(Record) string {
		calls++
		return "delivered"
	})
	if err == nil || result.Managed || calls != 0 {
		t.Fatalf("symlink replacement attempt = %+v calls=%d err=%v", result, calls, err)
	}
}

func TestSymlinkMarkerIsRefusedBeforePendingRead(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	now := time.Now().UTC()
	record := Record{GUID: "child-guid", Sender: "sender", Message: "operator prompt"}
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	paths := pathsFor(registryPath, record.GUID)
	attackPath := filepath.Join(t.TempDir(), "marker.json")
	data, err := json.Marshal(marker{Version: 1, GUID: record.GUID, Digest: digest(record.Message), Actor: ActorManual, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attackPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(attackPath, paths.marker); err != nil {
		t.Fatal(err)
	}

	calls := 0
	result, err := Attempt(registryPath, record.GUID, record.Message, ActorManual, now, func(Record) string {
		calls++
		return "delivered"
	})
	if err == nil || result.Managed || calls != 0 {
		t.Fatalf("symlink marker attempt = %+v calls=%d err=%v", result, calls, err)
	}
}

func TestCrashDuringDeliveryLeavesSuppressionCommitted(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	now := time.Now().UTC()
	record := Record{GUID: "child-guid", Sender: "sender", Message: "operator prompt"}
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	paths := pathsFor(registryPath, record.GUID)

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("delivery crash probe did not panic")
			}
		}()
		_, _ = Attempt(registryPath, record.GUID, "", ActorSidecar, now, func(Record) string {
			if _, err := os.Stat(paths.marker); err != nil {
				t.Fatalf("suppression marker was not committed before delivery: %v", err)
			}
			panic("simulated process crash")
		})
	}()

	retryCalls := 0
	retry, err := Attempt(registryPath, record.GUID, "", ActorSidecar, now, func(Record) string {
		retryCalls++
		return "delivered"
	})
	if err != nil || !retry.Managed || !retry.Suppressed || retryCalls != 0 {
		t.Fatalf("post-crash retry = %+v calls=%d err=%v", retry, retryCalls, err)
	}
}

func TestPruneAllSweepsLegacyPerGUIDLocks(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	paths := pathsFor(registryPath, "retired-child")
	if err := os.MkdirAll(filepath.Dir(paths.lock), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PruneAll(registryPath, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.lock); !os.IsNotExist(err) {
		t.Fatalf("legacy per-guid lock remains after global prune: %v", err)
	}
}

func TestAttemptWithoutPendingStateDoesNotCreateLock(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	result, err := Attempt(registryPath, "ordinary-child", "ordinary message", ActorManual, time.Now().UTC(), func(Record) string {
		t.Fatal("ordinary send should not enter pending delivery")
		return "delivered"
	})
	if err != nil || result.Managed {
		t.Fatalf("ordinary attempt = %+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(registryPath), "pending-prompts", ".lock")); !os.IsNotExist(err) {
		t.Fatalf("no-state attempt created directory lock: %v", err)
	}
	if _, err := os.Stat(pathsFor(registryPath, "ordinary-child").lock); !os.IsNotExist(err) {
		t.Fatalf("no-state attempt created per-guid lock: %v", err)
	}
}

func TestRepeatedStorePreservesCrashSuppression(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	now := time.Now().UTC()
	record := Record{GUID: "child-guid", Sender: "sender", Message: "operator prompt"}
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = Attempt(registryPath, record.GUID, "", ActorSidecar, now, func(Record) string {
			panic("simulated process crash")
		})
	}()
	if err := Store(registryPath, record, now); err != nil {
		t.Fatal(err)
	}
	retryCalls := 0
	retry, err := Attempt(registryPath, record.GUID, "", ActorSidecar, now, func(Record) string {
		retryCalls++
		return "delivered"
	})
	if err != nil || !retry.Suppressed || retryCalls != 0 {
		t.Fatalf("retry after repeated store = %+v calls=%d err=%v", retry, retryCalls, err)
	}
}
