package sidecarcmd

import (
	"os"
	"path/filepath"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestPiSidecarPredicateRequiresToolHooksAndSession(t *testing.T) {
	full := &hcomRow{Tool: "pi", HooksBound: true, SessionID: "session-pi", TranscriptPath: "/scratch/session.jsonl"}
	if !piRowBound(full) {
		t.Fatal("full Pi sidecar predicate rejected")
	}
	for _, row := range []*hcomRow{
		{Tool: "pi", SessionID: "session-pi"},
		{Tool: "pi", HooksBound: true},
		{Tool: "codex", HooksBound: true, SessionID: "session-pi"},
	} {
		if piRowBound(row) {
			t.Errorf("partial Pi sidecar predicate accepted: %+v", row)
		}
	}
}

func TestPiSidecarPersistsFullBindAndRefreshedVersionFacts(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, "registry.jsonl")
	initial := v2.VendorVersionObservation{Version: "0.80.5", ObservedAt: "2026-07-15T00:00:00Z"}
	outcomes, err := registry.UpdateLocked(registryPath, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			GUID: "guid-pi", Event: "registered", RecordedAt: initial.ObservedAt, State: v2.StateSeated,
			Label: "worker-pi", Role: "worker", Tool: "pi", Provider: "openai", Model: "gpt-test",
			VendorVersion: &v2.VendorVersionHistory{Current: initial},
			Seat:          &v2.Seat{Kind: "herdr", PaneID: "p_pi"},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := registry.SingleOutcome(outcomes); err != nil || outcome.Err() != nil {
		t.Fatalf("seed registry: outcome=%+v err=%v", outcome, err)
	}

	pkg := filepath.Join(root, "pkg")
	if err := os.MkdirAll(filepath.Join(pkg, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pkg, "dist", "cli.js")
	if err := os.WriteFile(entry, []byte("not executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@earendil-works/pi-coding-agent","version":"0.80.6"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(entry, filepath.Join(bin, "pi")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("HERDER_GUID", "guid-pi")
	t.Setenv("HERDER_LABEL", "worker-pi")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HCOM_DIR", filepath.Join(root, ".hcom"))

	s := &sidecar{tool: "pi", paneID: "p_pi", registry: registryPath, cwd: root, completeSeat: testSeatCompletion(t)}
	row := &hcomRow{
		Name: "worker-pi", Tool: "pi", HooksBound: true, SessionID: "session-pi",
		TranscriptPath: filepath.Join(root, "session.jsonl"), Tag: "worker", Directory: root,
		LaunchContext: launchContext("p_pi", "process-pi"),
	}
	if !s.appendEnrichment(row) {
		t.Fatal("full Pi enrichment was not appended")
	}
	projection, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.V2ByGUID(projection, "guid-pi")
	if got == nil || got.Seat == nil || !got.Seat.HooksBound || got.Seat.TranscriptPath != row.TranscriptPath {
		t.Fatalf("bind facts missing after enrichment: %+v", got)
	}
	if len(got.SIDs) != 1 || got.SIDs[0].SID != "session-pi" {
		t.Fatalf("session fact missing after enrichment: %+v", got.SIDs)
	}
	if got.VendorVersion == nil || got.VendorVersion.Current.Version != "0.80.6" || got.VendorVersion.Previous == nil || got.VendorVersion.Previous.Version != "0.80.5" {
		t.Fatalf("version history not refreshed at bind: %+v", got.VendorVersion)
	}
}
