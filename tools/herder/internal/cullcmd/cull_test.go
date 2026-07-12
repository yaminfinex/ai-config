package cullcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestRunClosesSeatedPaneLessRowWithoutForce(t *testing.T) {
	registryPath := seedSeatedCullRow(t, "ghost", "", "")
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
	if got := latestSession(t, registryPath, "guid-ghost"); got.Event != "unseated" || got.State != v2.StateUnseated || got.CloseResult != "already_gone" || got.CloseReason == "" || got.Seat != nil {
		t.Fatalf("latest row = %+v, want unseated already_gone without seat", got)
	}
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.Resolve(recs, "guid-ghost"); got == nil || got.Status != "active" {
		t.Fatalf("legacy latest = %+v, want active dormant row", got)
	}
	if !strings.Contains(stdout.String(), "recorded closed ghost (guid-ghost) pane= → already_gone") {
		t.Fatalf("stdout = %q, want recorded-closed line", stdout.String())
	}
}

func TestRunPaneLessUnannotatedCullAppendsOneVerifiedAnnotation(t *testing.T) {
	registryPath := seedUnseatedCullRow(t, "ghost")
	mockDir, _ := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))

	before := closeRecordCount(t, registryPath, "guid-ghost")
	var stdout, stderr strings.Builder
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("Run rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if after := closeRecordCount(t, registryPath, "guid-ghost"); after != before+1 {
		t.Fatalf("close rows = %d, want %d", after, before+1)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.State != v2.StateUnseated || got.CloseResult != "already_gone" || !strings.Contains(got.CloseReason, "source=cull-verification") || got.RecordedAt == "2026-07-08T00:00:00Z" {
		t.Fatalf("latest row = %+v, want fresh verified already_gone annotation", got)
	}
	if !strings.Contains(stdout.String(), "recorded closed ghost (guid-ghost) pane= → already_gone") {
		t.Fatalf("stdout = %q, want recorded-closed line", stdout.String())
	}

	beforeBytes := mustReadFile(t, registryPath)
	stdout.Reset()
	stderr.Reset()
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("second Run rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if afterBytes := mustReadFile(t, registryPath); string(afterBytes) != string(beforeBytes) {
		t.Fatalf("registry changed after repeat cull\nbefore:\n%s\nafter:\n%s", beforeBytes, afterBytes)
	}
	if !strings.Contains(stdout.String(), "already unseated ghost (guid-ghost) at ") || !strings.Contains(stdout.String(), "close_result=already_gone") {
		t.Fatalf("second stdout = %q, want recorded fact line", stdout.String())
	}
}

func TestRunPaneLessDifferingRepeatCullPreservesRecordedCloseResult(t *testing.T) {
	registryPath := seedAnnotatedUnseatedCullRow(t, "ghost", "closed", "first observer")
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	before := mustReadFile(t, registryPath)
	closed, appended, err := appendClosed(registryPath, *registry.Resolve(recs, "ghost"), "2026-07-08T12:00:00Z", "error", "second observer")
	if err != nil {
		t.Fatal(err)
	}
	if appended {
		t.Fatalf("appendClosed appended on already-unseated row")
	}
	if closed.CloseResult != "closed" || closed.CloseReason != "first observer" {
		t.Fatalf("closed fact = %+v, want original close annotation", closed)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.CloseResult != "closed" || got.CloseReason != "first observer" {
		t.Fatalf("latest row = %+v, want original close annotation", got)
	}
	if after := mustReadFile(t, registryPath); string(after) != string(before) {
		t.Fatalf("registry changed after differing repeat cull\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRunUnannotatedStaleCoordinateCorpseDoesNotStampBlindly(t *testing.T) {
	registryPath := seedUnseatedCullRows(t, v2.SessionRecord{
		GUID:  "guid-ghost",
		Label: "ghost",
		Seat: &v2.Seat{
			Kind:       "herdr",
			PaneID:     "p_stale",
			TerminalID: "term_stale",
		},
	})
	mockDir, _ := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))

	before := mustReadFile(t, registryPath)
	var stdout, stderr strings.Builder
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("Run rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if after := mustReadFile(t, registryPath); string(after) != string(before) {
		t.Fatalf("registry changed after unverifiable cull\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(stdout.String(), "never close-annotated (migrated corpse); gone-ness unverifiable from here") {
		t.Fatalf("stdout = %q, want unverifiable migrated corpse render", stdout.String())
	}
	if strings.Contains(stdout.String(), "close_result=") {
		t.Fatalf("stdout = %q, must not render blank close_result as recorded fact", stdout.String())
	}
}

func TestRunGoneSweepSkipsUnseatedCorpsesByteIdentically(t *testing.T) {
	registryPath := seedUnseatedCullRows(t,
		v2.SessionRecord{GUID: "guid-ghost-a", Label: "ghost-a", CloseResult: "already_gone", CloseReason: "terminal_id not in live agent list"},
		v2.SessionRecord{GUID: "guid-ghost-b", Label: "ghost-b"},
	)
	mockDir, _ := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))

	before := mustReadFile(t, registryPath)
	for i := 0; i < 2; i++ {
		var stdout, stderr strings.Builder
		if rc := Run([]string{"--gone"}, &stdout, &stderr); rc != 0 {
			t.Fatalf("run %d rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", i+1, rc, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "no gone records to cull") {
			t.Fatalf("run %d stdout = %q, want no-op sweep line", i+1, stdout.String())
		}
		if after := mustReadFile(t, registryPath); string(after) != string(before) {
			t.Fatalf("registry changed after gone sweep %d\nbefore:\n%s\nafter:\n%s", i+1, before, after)
		}
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
	if got := closeRecordCount(t, registryPath, "guid-ghost"); got != 0 {
		t.Fatalf("close records = %d, want no close row after write refusal", got)
	}
}

func TestAppendClosedRecordsPaneNotFoundAsUnseated(t *testing.T) {
	registryPath := seedSeatedCullRow(t, "ghost", "p_missing", "")
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	closed, appended, err := appendClosed(registryPath, *registry.Resolve(recs, "ghost"), "2026-07-08T12:00:00Z", "error", "pane_not_found")
	if err != nil {
		t.Fatal(err)
	}
	if !appended {
		t.Fatalf("appendClosed appended = false, want true")
	}
	if closed.Status != "closed" || closed.CloseResult != "error" || closed.CloseReason != "pane_not_found" {
		t.Fatalf("closed legacy row = %+v", closed)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.Event != "unseated" || got.State != v2.StateUnseated || got.CloseResult != "error" || got.CloseReason != "pane_not_found" || got.Seat != nil {
		t.Fatalf("latest row = %+v, want unseated pane_not_found without seat", got)
	}
}

func seedPaneLessCullRow(t *testing.T, label string) string {
	t.Helper()
	return seedUnseatedCullRow(t, label)
}

func seedUnseatedCullRow(t *testing.T, label string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"session","guid":"guid-ghost","event":"migrated_v1","recorded_at":"2026-07-08T00:00:00Z","state":"unseated","label":"`+label+`","role":"worker","tool":"codex"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func seedAnnotatedUnseatedCullRow(t *testing.T, label, closeResult, closeReason string) string {
	t.Helper()
	return seedUnseatedCullRows(t, v2.SessionRecord{GUID: "guid-ghost", Label: label, CloseResult: closeResult, CloseReason: closeReason})
}

func seedUnseatedCullRows(t *testing.T, rows ...v2.SessionRecord) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		out := make([]v2.SessionRecord, 0, len(rows))
		for _, row := range rows {
			row.Kind = v2.KindSession
			row.Event = "unseated"
			row.RecordedAt = "2026-07-08T00:00:00Z"
			row.State = v2.StateUnseated
			row.Role = "worker"
			row.Tool = "codex"
			out = append(out, row)
		}
		return out, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWriteOutcomes(t, outcomes)
	return path
}

func seedSeatedCullRow(t *testing.T, label, paneID, terminalID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWriteOutcomes(t, outcomes)
	return path
}

func assertWriteOutcomes(t *testing.T, outcomes []registry.WriteOutcome) {
	t.Helper()
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
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

func closeRecordCount(t *testing.T, path, guid string) int {
	t.Helper()
	data := mustReadFile(t, path)
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(line, `"guid":"`+guid+`"`) && strings.Contains(line, `"event":"unseated"`) && strings.Contains(line, `"close_result":"`) {
			count++
		}
	}
	return count
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
