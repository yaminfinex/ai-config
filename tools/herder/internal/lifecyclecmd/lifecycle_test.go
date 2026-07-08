package lifecyclecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestResolveTargetWithArchiveFallbackSkipsArchivesOnLiveHit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	archDir := filepath.Join(dir, "registry.jsonl.archive")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(archDir, "0002-rotation.jsonl")); err != nil {
		t.Fatal(err)
	}
	guid := "guid-live-0000"
	label := "live"
	live := []registry.Record{{GUID: &guid, Label: &label, Status: "active"}}

	recs, rec, err := resolveTargetWithArchiveFallback(live, path, "live")
	if err != nil {
		t.Fatalf("live hit consulted broken archive: %v", err)
	}
	if rec == nil || rec.GUID == nil || *rec.GUID != guid {
		t.Fatalf("rec = %+v, want live row", rec)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want live-only set", len(recs))
	}

	if _, _, err := resolveTargetWithArchiveFallback(live, path, "missing"); err == nil {
		t.Fatal("live miss did not consult broken archive")
	}
}

func TestResumeRefusesRetiredSession(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.jsonl")
	if _, err := registry.UpdateLocked(registryPath, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind:       v2.KindSession,
			GUID:       "guid-retired",
			Event:      "retired",
			RecordedAt: "2026-07-08T00:00:00Z",
			State:      v2.StateRetired,
			Label:      "old",
			Tool:       "codex",
			SIDs:       []v2.SID{{SID: "session-retired", Source: "harvest"}},
		}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	mockBin := t.TempDir()
	for _, tool := range []string{"herdr", "hcom"} {
		if err := os.WriteFile(filepath.Join(mockBin, tool), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", mockBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDER_STATE_DIR", dir)

	var stdout, stderr strings.Builder
	rc := (&runner{stdout: &stdout, stderr: &stderr, herdr: fakeHerdrClient{}}).resume(resumeOptions{target: "old"})
	if rc == 0 {
		t.Fatalf("resume rc = 0, want refusal\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "session is retired") || !strings.Contains(stderr.String(), "herder reopen") {
		t.Fatalf("stderr = %q, want reopen guidance", stderr.String())
	}
}

type fakeHerdrClient struct{}

func (fakeHerdrClient) Combined(args ...string) ([]byte, int, error) {
	return []byte(`{"result":{"type":"ok"}}`), 0, nil
}

func (fakeHerdrClient) Output(args ...string) ([]byte, error) {
	return []byte(`{"result":{"agents":[]}}`), nil
}
