package grokbridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/pendingprompt"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestManagedForkBridgeCompletesCanonicalSeatByBaseNameAndHandsOffPromptOnce(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	busDir := filepath.Join(root, "bus")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(filepath.Join(state, "grok", "seat-guid"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(busDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "grok", "seat-guid", "bus-name"), []byte("base-seat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	herdr := filepath.Join(binDir, "herdr")
	if err := os.WriteFile(herdr, []byte("#!/bin/sh\nprintf '%s\\n' '{\"result\":{\"pane\":{\"pane_id\":\"pane-live\",\"terminal_id\":\"term-live\",\"workspace_id\":\"workspace-live\",\"cwd\":\"/repo\"}}}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	sendLog := filepath.Join(root, "sends")
	hcom := filepath.Join(binDir, "hcom")
	hcomScript := `#!/bin/sh
if [ "$1" = list ] && [ "$2" = --json ]; then
  printf '%s\n' '[{"name":"worker-base-seat","base_name":"base-seat","tool":"grok","joined":true,"session_id":"","launch_context":{"process_id":"seat-guid","pane_id":"pane-live"}}]'
  exit 0
fi
if [ "$1" = list ]; then exit 0; fi
if [ "$1" = events ]; then printf '%s\n' '[]'; exit 0; fi
if [ "$1" = send ]; then printf '%s\n' "$*" >> "$HERDER_TEST_SEND_LOG"; exit 0; fi
exit 1
`
	if err := os.WriteFile(hcom, []byte(hcomScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDER_TEST_SEND_LOG", sendLog)
	t.Setenv("HERDER_LABEL", "grok-worker")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_SPAWNED_BY", "parent-guid")
	t.Setenv("HERDER_MISSION_SLUG", "mission-one")
	t.Setenv("HERDER_MISSION_SOURCE", "explicit")

	registryPath := filepath.Join(state, "registry.jsonl")
	if err := pendingprompt.Store(registryPath, pendingprompt.Record{
		GUID: "seat-guid", Sender: "sender-seat", BusDir: busDir, Message: "initial prompt", VerifyMS: 1,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cfg := managedCompletionConfig{
		Seat: "seat-guid", StateDir: state, HcomDir: busDir, SessionID: "grok-session", PaneID: "pane-live",
		LifecycleMode: "fork", ForkedFromGUID: "parent-seat-guid",
	}
	done, err := completeManagedSeat(context.Background(), cfg)
	if err != nil || !done {
		t.Fatalf("completeManagedSeat done=%t err=%v", done, err)
	}
	projection, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current := registry.V2ByGUID(projection, "seat-guid")
	if current == nil || current.State != v2.StateSeated || current.Seat == nil {
		t.Fatalf("canonical row=%+v", current)
	}
	if current.Seat.HcomName != "worker-base-seat" || current.Seat.CredentialGeneration == "" || current.Provenance.ToolSessionID != "grok-session" {
		t.Fatalf("canonical completion lost bus/credential/session facts: %+v", current)
	}
	if current.Mission == nil || current.Mission.Slug != "mission-one" || current.Mission.Source != "explicit" {
		t.Fatalf("canonical completion lost mission: %+v", current.Mission)
	}
	if current.Provenance.Mechanism != "fork" || current.Provenance.ForkedFrom != "parent-seat-guid" {
		t.Fatalf("canonical completion lost fork provenance: %+v", current.Provenance)
	}
	data, err := os.ReadFile(sendLog)
	if err != nil || !strings.Contains(string(data), "send --from sender-seat @worker-base-seat -- initial prompt") {
		t.Fatalf("pending prompt send log=%q err=%v", data, err)
	}

	// Exact replay reaches the same pendingprompt marker and cannot submit twice.
	done, err = completeManagedSeat(context.Background(), cfg)
	if err != nil || !done {
		t.Fatalf("replay done=%t err=%v", done, err)
	}
	data, err = os.ReadFile(sendLog)
	if err != nil || strings.Count(string(data), "send --from") != 1 {
		t.Fatalf("pending prompt replay duplicated send: %q err=%v", data, err)
	}
}

func TestManagedCompletionConvergesWhenConcurrentWriterSeatsInsideLock(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	cfg := managedCompletionConfig{Seat: "race-seat", StateDir: state, HcomDir: "/bus", SessionID: "race-session", PaneID: "pane-race", LifecycleMode: "fork"}
	pane := herdrcli.Pane{PaneID: "pane-race", TerminalID: "term-race", WorkspaceID: "workspace-race", CWD: "/repo"}
	row := managedCompletionCandidate(cfg, pane, "worker-race")
	verified := true
	row.Seat = &v2.Seat{
		Kind: "herdr", PaneID: pane.PaneID, TerminalID: pane.TerminalID, Namespace: cfg.HcomDir,
		HcomName: "worker-race", HcomVerified: &verified,
	}
	seedOutcomes, err := registry.UpdateLocked(registryPath, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{row}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seedOutcomes) != 1 || seedOutcomes[0].Status != registry.WriteApplied {
		t.Fatalf("race seed outcomes=%+v", seedOutcomes)
	}
	innerCalled := false
	outcomes, err := managedCompletionRegistryWriter(cfg, pane, "worker-race")(registryPath, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		innerCalled = true
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if innerCalled {
		t.Fatal("concurrent canonical row fell through to stale completion update")
	}
	if len(outcomes) != 1 || (outcomes[0].Status != registry.WriteApplied && outcomes[0].Status != registry.WriteNoop) {
		t.Fatalf("race convergence outcomes=%+v", outcomes)
	}
	projection, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil || !canonicalManagedSeatMatches(registry.V2ByGUID(projection, cfg.Seat), cfg, pane, "worker-race") {
		t.Fatalf("race convergence lost canonical row: err=%v", err)
	}
}
