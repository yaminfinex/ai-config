package observercmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestGrokSessionAdapterUsesRecordedChatHistoryAndLabelsEvents(t *testing.T) {
	stateDir := t.TempDir()
	cwd := "/workspace/sample workspace"
	sid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	sessionDir, err := grokSessionDir(filepath.Join(stateDir, "grok-home"), cwd, sid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(stateDir, "grok-home", "sessions", "%2Fworkspace%2Fsample%20workspace", sid)
	if sessionDir != wantDir {
		t.Fatalf("session dir = %q, want recorded URL-encoded layout %q", sessionDir, wantDir)
	}
	for _, name := range []string{"chat_history.jsonl", "events.jsonl"} {
		data, err := os.ReadFile(filepath.Join("testdata", "grok-session", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sessionDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The hook-advertised path is deliberately present and must never win.
	if err := os.WriteFile(filepath.Join(sessionDir, "updates.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	obs, err := observeGrokSession(filepath.Join(stateDir, "grok-home"), cwd, sid, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(obs.TranscriptPath) != "chat_history.jsonl" || strings.Contains(obs.TranscriptPath, "updates.jsonl") {
		t.Fatalf("transcript path = %q, want chat_history.jsonl and never updates.jsonl", obs.TranscriptPath)
	}
	if obs.Transcript.Entries != 3 || obs.Transcript.System != 1 || obs.Transcript.User != 1 || obs.Transcript.Assistant != 1 || obs.Transcript.Other != 0 {
		t.Fatalf("transcript summary = %+v, want recorded system/user/assistant sequence", obs.Transcript)
	}
	if obs.EventStatus != "tool_execution" || obs.EventSource != "grok-events" {
		t.Fatalf("event enrichment = %q via %q, want tool_execution via grok-events", obs.EventStatus, obs.EventSource)
	}
}

func TestGrokEventAdapterDoesNotInventStatusFromUnknownEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data := []byte("{\"type\":\"phase_changed\",\"phase\":\"waiting_for_model\"}\n" +
		"{\"type\":\"phase_changed\",\"phase\":\"hostile status\"}\n" +
		"{\"type\":\"unrecognised_vendor_event\",\"phase\":\"idle\"}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := &grokArtifactCursor{}
	err := updateGrokEventStatus(path, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.eventStatus != "waiting_for_model" {
		t.Fatalf("event status = %q, want last supported on-disk evidence without guessed idle", cursor.eventStatus)
	}
}

func TestGrokArtifactCursorResetsOnReplacementAndTruncation(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "chat_history.jsonl")
	events := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(transcript, []byte("{\"type\":\"user\",\"content\":\"one\"}\n{\"type\":\"assistant\",\"content\":\"two\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(events, []byte("{\"type\":\"phase_changed\",\"phase\":\"tool_execution\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := &grokArtifactCursor{}
	if err := updateGrokTranscript(transcript, cursor); err != nil {
		t.Fatal(err)
	}
	if err := updateGrokEventStatus(events, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.transcript.Entries != 2 || cursor.eventStatus != "tool_execution" {
		t.Fatalf("initial cursor = %+v, want two entries and tool_execution", cursor)
	}

	replacement := filepath.Join(dir, "replacement.jsonl")
	if err := os.WriteFile(replacement, []byte("{\"type\":\"system\",\"content\":\"replacement\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, transcript); err != nil {
		t.Fatal(err)
	}
	if err := updateGrokTranscript(transcript, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.transcript.Entries != 1 || cursor.transcript.System != 1 {
		t.Fatalf("replacement cursor = %+v, want reset one-entry summary", cursor.transcript)
	}

	if err := os.WriteFile(events, []byte("{\"type\":\"phase_changed\",\"phase\":\"waiting_for_model\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateGrokEventStatus(events, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.eventStatus != "waiting_for_model" {
		t.Fatalf("truncated event cursor = %q, want reset waiting_for_model", cursor.eventStatus)
	}
}

func TestGrokObservationsRequireExplicitSessionIdentity(t *testing.T) {
	stateDir := t.TempDir()
	guid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	rec := v2.SessionRecord{
		GUID:       guid,
		Tool:       "grok",
		State:      v2.StateSeated,
		Provenance: v2.Provenance{CWD: filepath.Join(t.TempDir(), "workspace")},
	}
	if got := grokObservations([]v2.SessionRecord{rec}, stateDir, nil, nil); len(got) != 0 {
		t.Fatalf("observations without session identity = %+v, want none", got)
	}
}
