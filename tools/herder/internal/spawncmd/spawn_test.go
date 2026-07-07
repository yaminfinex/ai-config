package spawncmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ai-config/tools/herder/internal/registry"
)

func TestHcomEntryAcceptsNumericCreatedAt(t *testing.T) {
	var entries []hcomEntry
	data := []byte(`[{"name":"smoke-p5-tuna","tag":"smoke-p5","directory":"/tmp","created_at":1782979094.0}]`)
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := string(entries[0].CreatedAt); got != "1782979094.0" {
		t.Fatalf("CreatedAt = %q, want numeric value preserved", got)
	}
}

func TestResolveSpawnerBusMatchesEnrolledPane(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	rows := []string{
		// enrolled orchestrator: pane/terminal identity, bus name, NO spawner guid in play
		`{"guid":"guid-hera","short_guid":"guid-her","label":"orchestrator","pane_id":"p_orch","terminal_id":"term_ORCH","hcom_name":"hera","status":"active"}`,
		// closed row holding the SAME pane id from an older session must not win
		`{"guid":"guid-old","short_guid":"guid-old","label":"old","pane_id":"p_stale","terminal_id":"term_STALE","hcom_name":"stale-name","status":"closed"}`,
	}
	for _, row := range rows {
		if err := registry.Append(path, []byte(row)); err != nil {
			t.Fatal(err)
		}
	}

	// spawner identified only by its pane id (the enrolled case: spawnedBy=user)
	if got := resolveSpawnerBus(path, "", "user", "p_orch", ""); got != "hera" {
		t.Fatalf("pane match = %q, want hera", got)
	}
	// terminal id fallback (notifyTo auto-resolved to the spawner's terminal)
	if got := resolveSpawnerBus(path, "term_ORCH", "user", "", ""); got != "hera" {
		t.Fatalf("terminal match via notifyTo = %q, want hera", got)
	}
	// closed rows never resolve by pane coordinates
	if got := resolveSpawnerBus(path, "", "user", "p_stale", ""); got != "" {
		t.Fatalf("closed pane match = %q, want empty", got)
	}
	// guid resolution still wins first
	if got := resolveSpawnerBus(path, "", "guid-hera", "", ""); got != "hera" {
		t.Fatalf("guid match = %q, want hera", got)
	}
}

func TestCheckoutForDirWalksUpToCheckoutRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools", "herder", "shims"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "herder"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "tools", "herder", "internal")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	gotRoot, gotBin, ok := checkoutForDir(nested)
	if !ok || gotRoot != root || gotBin != filepath.Join(root, "bin", "herder") {
		t.Fatalf("checkoutForDir(%q) = (%q, %q, %v), want (%q, ..., true)", nested, gotRoot, gotBin, ok, root)
	}
	if _, _, ok := checkoutForDir(t.TempDir()); ok {
		t.Fatal("checkoutForDir on a plain dir = ok, want miss")
	}
}

func TestRegistryCapturedNameUsesLatestEnrichmentRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := registry.Append(path, []byte(`{"guid":"guid-1","short_guid":"guid","label":"worker-guid","hcom_name":"","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Append(path, []byte(`{"guid":"guid-1","short_guid":"guid","label":"worker-guid","hcom_name":"worker-rive","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-1","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws","branch":"","ts":"2026-07-03T00:00:00Z"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := registryCapturedName(path, "guid-1"); got != "worker-rive" {
		t.Fatalf("registryCapturedName = %q, want worker-rive", got)
	}
}
