package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tick folds every event in the drain, then journals the cursor ONCE at the
// end. A crash in that window — deploy restart, OOM, kill -9 mid-drain — leaves
// the folded `open desk-N` durable with no cursor entry, so the event refolds on
// reboot. Refolding must be a no-op: fold synthesizes desk-<id> for a threadless
// raise, and if it opens that id blind, Store.Open returns "already exists" and
// stop-at-first-fold-failure wedges ingest permanently — the cursor never
// advances and no bus traffic is ever ingested again.
func TestRefoldAfterCrashBeforeCursorIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	jpath := filepath.Join(dir, "journal.jsonl")

	// Event 10 is a raise at the seat with NO thread id: fold must synthesize
	// desk-10. Event 11 is ordinary traffic that must still land after recovery.
	hcom := writeExecutable(t, dir, "hcom", `#!/bin/sh
cursor=$(printf '%s\n' "$*" | sed -n 's/.*id > \([0-9][0-9]*\).*/\1/p')
[ -z "$cursor" ] && cursor=0
[ "$cursor" -lt 10 ] && printf '%s\n' '{"id":10,"type":"message","ts":"2026-07-15T00:00:00Z","data":{"from":"agent-a","text":"please look","mentions":["owner"],"intent":"request"}}'
[ "$cursor" -lt 11 ] && printf '%s\n' '{"id":11,"type":"message","ts":"2026-07-15T00:00:01Z","data":{"from":"agent-a","text":"follow up","mentions":["owner"],"intent":"inform","thread":"desk-10"}}'
exit 0
`)

	s, err := OpenStore(jpath)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewIngestor(s, &Bus{Hcom: hcom}, "human-yamen", "owner").Tick(); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if s.Get("desk-10") == nil {
		t.Fatal("first tick should have opened desk-10")
	}

	// Simulate the crash: drop the trailing cursor entry the tick had just
	// appended. Everything the folds wrote is already durable.
	raw, err := os.ReadFile(jpath)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if !strings.Contains(line, `"op":"cursor"`) {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(jpath, []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s2, err := OpenStore(jpath)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Get("desk-10") == nil {
		t.Fatal("desk-10 must survive the crash")
	}
	if s2.Cursor() >= 10 {
		t.Fatalf("cursor = %d, want < 10 — the crash must leave event 10 unacknowledged", s2.Cursor())
	}

	// Reboot: event 10 refolds. It must be a no-op, not a permanent wedge.
	in2 := NewIngestor(s2, &Bus{Hcom: hcom}, "human-yamen", "owner")
	if err := in2.Tick(); err != nil {
		t.Fatalf("refold after crash must be idempotent, got: %v", err)
	}
	if got := s2.Cursor(); got != 11 {
		t.Fatalf("cursor = %d, want 11 — ingest must drain past the refolded raise", got)
	}
	if t10 := s2.Get("desk-10"); t10 == nil || len(t10.Msgs) != 2 {
		t.Fatalf("desk-10 = %#v, want 2 linked msgs with no duplicates", t10)
	}
}
