package grokbridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/registry"
)

func rawEvent(t *testing.T, id int64, text string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(Event{ID: id, Type: "message", Data: json.RawMessage(`{"from":"peer","text":` + strconvQuote(text) + `,"intent":"request","thread":"work"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
func openTestJournal(t *testing.T) *Journal {
	t.Helper()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	if _, err = j.AdvanceGeneration(); err != nil {
		t.Fatal(err)
	}
	return j
}
func queue(t *testing.T, j *Journal, id int64) Receipt {
	t.Helper()
	r, added, err := j.Queue(rawEvent(t, id, "payload"))
	if err != nil || !added {
		t.Fatalf("queue: added=%v err=%v", added, err)
	}
	return r
}

func TestReceiptStateMachineContracts(t *testing.T) {
	t.Run("journal_pending_fetch_ack", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 1)
		p, err := j.Pending(1, true)
		if err != nil || len(p) != 1 {
			t.Fatal(err)
		}
		if _, err = j.Fetch(1, 1); err != nil {
			t.Fatal(err)
		}
		if err = j.Ack(1, 1); err != nil {
			t.Fatal(err)
		}
		if got := j.receipts[1].Status(); got != "delivered" {
			t.Fatalf("status=%s", got)
		}
	})
	t.Run("journal_idle_delivery", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 2)
		if err := j.Surface(2, "wake", 1); err != nil {
			t.Fatal(err)
		}
		j.Fetch(2, 1)
		j.Ack(2, 1)
		if !j.receipts[2].Acked {
			t.Fatal("not delivered")
		}
	})
	t.Run("journal_wake_does_not_advance_delivery", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 3)
		j.Surface(3, "wake", 1)
		events := filepath.Join(t.TempDir(), "events.jsonl")
		if err := os.WriteFile(events, []byte("{\"event\":\"phase_changed\",\"phase\":\"tool_execution\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if idle, err := sessionIdle(events); err != nil || idle {
			t.Fatalf("busy phase idle=%v err=%v", idle, err)
		}
		if j.receipts[3].Fetched || j.receipts[3].Acked {
			t.Fatal("wake advanced receipt")
		}
	})
	t.Run("journal_duplicate_surfaces", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 4)
		j.Surface(4, "wake", 1)
		j.Surface(4, "nudge", 1)
		j.Fetch(4, 1)
		j.Fetch(4, 1)
		j.Ack(4, 1)
		if j.receipts[4].Surfaces != 4 {
			t.Fatalf("surfaces=%d", j.receipts[4].Surfaces)
		}
	})
	t.Run("journal_duplicate_ack", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 5)
		j.Fetch(5, 1)
		j.Ack(5, 1)
		if err := j.Ack(5, 1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("journal_out_of_order", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 3)
		queue(t, j, 5)
		j.Fetch(5, 1)
		j.Ack(5, 1)
		j.Fetch(3, 1)
		j.Ack(3, 1)
		if !j.receipts[3].Acked || !j.receipts[5].Acked {
			t.Fatal("independent ids did not deliver")
		}
	})
	t.Run("journal_ack_before_fetch_rejected", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 7)
		if err := j.Ack(7, 1); err == nil || !strings.Contains(err.Error(), "fetch before ack") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("journal_foreign_id_rejected", func(t *testing.T) {
		j := openTestJournal(t)
		if _, err := j.Fetch(88, 1); err == nil {
			t.Fatal("foreign fetch accepted")
		}
	})
	t.Run("journal_restart_replays_queued", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "journal.jsonl")
		j, _ := OpenJournal(path)
		j.AdvanceGeneration()
		queue(t, j, 9)
		j.Close()
		j, _ = OpenJournal(path)
		defer j.Close()
		if j.Cursor() != 9 || j.receipts[9].Status() != "queued" {
			t.Fatal("queued state not replayed")
		}
	})
	t.Run("journal_restart_preserves_pending", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "journal.jsonl")
		j, _ := OpenJournal(path)
		j.AdvanceGeneration()
		queue(t, j, 10)
		j.Surface(10, "wake", 1)
		j.Close()
		j, _ = OpenJournal(path)
		defer j.Close()
		j.AdvanceGeneration()
		p, _ := j.Pending(2, true)
		if len(p) != 1 {
			t.Fatal("pending not recovered")
		}
	})
	t.Run("journal_queue_independent_of_tap", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 11)
		p, _ := j.Pending(1, false)
		if len(p) != 1 {
			t.Fatal("pending lost without tap")
		}
	})
	t.Run("journal_failure_after_fetch_persists", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "journal.jsonl")
		j, _ := OpenJournal(path)
		j.AdvanceGeneration()
		queue(t, j, 12)
		j.Fetch(12, 1)
		j.Close()
		j, _ = OpenJournal(path)
		defer j.Close()
		if !j.receipts[12].Fetched || j.receipts[12].Acked {
			t.Fatal("fetched state not preserved")
		}
	})
	t.Run("journal_relist_surfaces", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 13)
		p, _ := j.Pending(1, true)
		if len(p) != 1 || !j.receipts[13].Surfaced {
			t.Fatal("re-list did not surface")
		}
	})
	t.Run("journal_reopen_same_spool", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "journal.jsonl")
		j, _ := OpenJournal(path)
		j.AdvanceGeneration()
		queue(t, j, 14)
		j.Close()
		j, _ = OpenJournal(path)
		defer j.Close()
		if j.Cursor() != 14 {
			t.Fatal("resume lost cursor")
		}
	})
	t.Run("journal_distinct_spool_isolation", func(t *testing.T) {
		root := t.TempDir()
		a, _ := OpenJournal(filepath.Join(root, "a", "journal.jsonl"))
		defer a.Close()
		a.AdvanceGeneration()
		queue(t, a, 15)
		b, _ := OpenJournal(filepath.Join(root, "b", "journal.jsonl"))
		defer b.Close()
		b.AdvanceGeneration()
		if _, err := b.Fetch(15, 1); err == nil {
			t.Fatal("cross-seat fetch accepted")
		}
	})
	t.Run("journal_stale_generation_rejected", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 16)
		j.AdvanceGeneration()
		if _, err := j.Fetch(16, 1); err == nil || !strings.Contains(err.Error(), "reconnect") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("wake_line_excludes_payload", func(t *testing.T) {
		j := openTestJournal(t)
		r := queue(t, j, 17)
		line := wakeLine(r)
		if strings.Contains(line, "payload") || !strings.HasPrefix(line, "HCOM id=") {
			t.Fatalf("line=%q", line)
		}
	})
	t.Run("journal_delivered_only_after_ack", func(t *testing.T) {
		j := openTestJournal(t)
		queue(t, j, 18)
		j.Surface(18, "wake", 1)
		if j.receipts[18].Status() == "delivered" {
			t.Fatal("wake claimed delivery")
		}
		j.Fetch(18, 1)
		if j.receipts[18].Status() == "delivered" {
			t.Fatal("fetch claimed delivery")
		}
		j.Ack(18, 1)
		if j.receipts[18].Status() != "delivered" {
			t.Fatal("ack did not claim delivery")
		}
	})
}

func TestT23DualBinderLockAndGenerationFence(t *testing.T) {
	state := t.TempDir()
	m := startMockBridge(t, state, "owning-session")
	oldClient := m.client(t)
	if _, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: m.b.cfg.HcomBin}); err == nil {
		t.Fatal("second binder acquired lock")
	}
	m.close()
	m = startMockBridge(t, state, "owning-session")
	defer m.close()
	if m.b.generation != 2 {
		t.Fatalf("generation=%d", m.b.generation)
	}
	m.queue(t, 23, "fenced")
	pending, err := oldClient.Call(Request{Op: "pending"})
	if err != nil || len(pending.Pending) != 1 {
		t.Fatalf("straddling client pending=%+v err=%v", pending, err)
	}
	if oldClient.generation != 2 {
		t.Fatalf("client generation=%d", oldClient.generation)
	}
	if m.b.journal.receipts[23].Surfaces != 1 {
		t.Fatalf("surfaces=%d, stale request may have executed", m.b.journal.receipts[23].Surfaces)
	}
}

func TestTapFailureIsSilentAndDiagnosedToFile(t *testing.T) {
	state := t.TempDir()
	var out, stderr bytes.Buffer
	code := runTap([]string{"--seat", "seat", "--state-dir", state}, &out, &stderr)
	if code == 0 {
		t.Fatal("missing bridge unexpectedly succeeded")
	}
	if out.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("tap leaked output stdout=%q stderr=%q", out.String(), stderr.String())
	}
	if data, err := os.ReadFile(filepath.Join(state, "grok", "seat", "tap.log")); err != nil || len(data) == 0 {
		t.Fatalf("diagnostic missing data=%q err=%v", data, err)
	}
}

func TestRetireUnackedTransitionsOnlyPendingMessages(t *testing.T) {
	j := openTestJournal(t)
	queue(t, j, 1)
	queue(t, j, 2)
	if _, err := j.Fetch(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack(1, 1); err != nil {
		t.Fatal(err)
	}
	count, err := j.RetireUnacked(1)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if j.receipts[1].Status() != "delivered" || j.receipts[2].Status() != "undeliverable" {
		t.Fatalf("statuses=%s,%s", j.receipts[1].Status(), j.receipts[2].Status())
	}
}

func TestSocketPathLengthPreflightNamesRemedy(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(t.TempDir(), strings.Repeat("segment", 18))
	_, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: deep, HcomBin: bin})
	if err == nil || !strings.Contains(err.Error(), "shorten --state-dir") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegistryMintedGUIDIsAcceptedAsSeatIdentity(t *testing.T) {
	guid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	state, err := os.MkdirTemp("/tmp", "grok-seat-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(state) })
	b, err := OpenBinder(BinderConfig{Seat: guid, StateDir: state, HcomBin: bin})
	if err != nil {
		t.Fatalf("registry-minted guid %q rejected: %v", guid, err)
	}
	b.Close()
}

func TestDefaultWaitUsesHcomScaleWithoutCorrectnessWeight(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: t.TempDir(), HcomBin: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.cfg.Wait != 60*time.Second {
		t.Fatalf("wait=%s", b.cfg.Wait)
	}
}
