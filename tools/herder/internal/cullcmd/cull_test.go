package cullcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestRunClosesPaneLessRowWithoutForce(t *testing.T) {
	registryPath := seedPaneLessCullRow(t, "ghost")
	mockDir, closeProbe := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))

	var stdout, stderr strings.Builder
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("Run rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(closeProbe); !os.IsNotExist(err) {
		t.Fatalf("pane close probe exists = %v, want no pane close call for pane-less row", err)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.Event != "retired" || got.State != v2.StateRetired || got.CloseResult != "already_gone" || got.CloseReason == "" || got.Seat != nil {
		t.Fatalf("latest row = %+v, want retired already_gone without seat", got)
	}
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.Resolve(recs, "guid-ghost"); got == nil || got.Status != "closed" {
		t.Fatalf("legacy latest = %+v, want closed", got)
	}
	if !strings.Contains(stdout.String(), "recorded closed ghost (guid-ghost) pane= → already_gone") {
		t.Fatalf("stdout = %q, want recorded-closed line", stdout.String())
	}
}

func TestRunPaneLessCloseWriteFailureExitsNonzero(t *testing.T) {
	registryPath := seedPaneLessCullRow(t, "ghost")
	mockDir, _ := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))
	t.Setenv("HERDER_TEST_FLOCK_REFUSE", "1")

	var stdout, stderr strings.Builder
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc == 0 {
		t.Fatalf("Run rc = 0, want nonzero\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "recorded closed") || strings.Contains(stdout.String(), "marked closed") {
		t.Fatalf("stdout claims closure despite write failure: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "registry lock unavailable") {
		t.Fatalf("stderr = %q, want lock failure", stderr.String())
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.State == v2.StateRetired {
		t.Fatalf("latest row = %+v, want no retired row after write refusal", got)
	}
}

func TestAppendClosedRecordsPaneNotFoundAsRetired(t *testing.T) {
	registryPath := seedSeatedCullRow(t, "ghost", "p_missing", "")
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := appendClosed(registryPath, *registry.Resolve(recs, "ghost"), "2026-07-08T12:00:00Z", "p_culler", "error", "pane_not_found")
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != "closed" || closed.CloseResult != "error" || closed.CloseReason != "pane_not_found" {
		t.Fatalf("closed legacy row = %+v", closed)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.Event != "retired" || got.State != v2.StateRetired || got.CloseResult != "error" || got.CloseReason != "pane_not_found" || got.Seat != nil {
		t.Fatalf("latest row = %+v, want retired pane_not_found without seat", got)
	}
}

func seedPaneLessCullRow(t *testing.T, label string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if _, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind:       v2.KindSession,
			GUID:       "guid-ghost",
			Event:      "migrated_v1",
			RecordedAt: "2026-07-08T00:00:00Z",
			State:      v2.StateUnseated,
			Label:      label,
			Role:       "worker",
			Tool:       "codex",
		}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return path
}

func seedSeatedCullRow(t *testing.T, label, paneID, terminalID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if _, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind:       v2.KindSession,
			GUID:       "guid-ghost",
			Event:      "registered",
			RecordedAt: "2026-07-08T00:00:00Z",
			State:      v2.StateSeated,
			Label:      label,
			Role:       "worker",
			Tool:       "codex",
			Seat: &v2.Seat{
				Kind:       "herdr",
				TerminalID: terminalID,
				PaneID:     paneID,
			},
		}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return path
}

func installCullTestMocks(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	closeProbe := filepath.Join(dir, "pane_close_called")
	herdr := `#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "agent list")
    printf '{"result":{"agents":[]}}\n'
    ;;
  "pane close")
    printf '%s\n' "${3:-}" >>"` + closeProbe + `"
    printf '{"error":{"code":"pane_not_found"}}\n'
    ;;
  *)
    printf 'mock herdr: unhandled %s\n' "$*" >&2
    exit 64
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "herdr"), []byte(herdr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jq"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, closeProbe
}

func latestSession(t *testing.T, path, guid string) v2.SessionRecord {
	t.Helper()
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.V2ByGUID(proj, guid)
	if got == nil {
		t.Fatalf("missing guid %s", guid)
	}
	return *got
}
