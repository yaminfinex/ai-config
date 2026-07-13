package observercmd

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestGrokSessionAdapterDiscoversOpaqueCWDAndUsesRecordedChatHistory(t *testing.T) {
	stateDir := t.TempDir()
	grokHome := filepath.Join(stateDir, "grok-home")
	sid := newTestGUID(t)
	// Node's encodeURIComponent escapes @ while Go's url.PathEscape does not.
	// Treat the vendor's cwd directory as opaque and discover solely by SID.
	sessionDir := filepath.Join(grokHome, "sessions", "%2Fworkspace%2F%40scope%2Fapp", sid)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyGrokFixture(t, sessionDir, "chat_history.jsonl")
	copyGrokFixture(t, sessionDir, "events.jsonl")
	// The hook-advertised path is deliberately present and must never win.
	if err := os.WriteFile(filepath.Join(sessionDir, "updates.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	discovered, err := grokSessionDir(grokHome, sid)
	if err != nil {
		t.Fatal(err)
	}
	if discovered != sessionDir {
		t.Fatalf("session dir = %q, want opaque vendor directory %q", discovered, sessionDir)
	}
	obs, err := observeGrokSession(grokHome, sid, nil)
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

func TestGrokSessionDirRefusesDuplicateSIDMatches(t *testing.T) {
	grokHome := filepath.Join(t.TempDir(), "grok-home")
	sid := newTestGUID(t)
	for _, opaqueCWD := range []string{"opaque-a", "opaque-b"} {
		if err := os.MkdirAll(filepath.Join(grokHome, "sessions", opaqueCWD, sid), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := grokSessionDir(grokHome, sid)
	if err == nil || dir != "" {
		t.Fatalf("duplicate SID discovery = %q, %v; want cause+remedy refusal", dir, err)
	}
	if !strings.Contains(err.Error(), "matched 2 directories") || !strings.Contains(err.Error(), "resolve duplicate session artifacts and retry") {
		t.Fatalf("duplicate SID error = %q, want match count and safe remedy", err)
	}
}

func TestGrokDiscoveryMissWaitsOneSweepThenProducesSafeFlag(t *testing.T) {
	t.Setenv("HERDER_OBSERVER_SWEEP_INTERVAL", "30s")
	guid := newTestGUID(t)
	sid := newTestGUID(t)
	now := time.Date(2026, 7, 13, 7, 30, 0, 0, time.UTC)
	rec := v2.SessionRecord{
		GUID:  guid,
		Label: "neutral-seat",
		Tool:  "grok",
		State: v2.StateSeated,
		Seat:  &v2.Seat{ConfirmedAt: now.Add(-sweepInterval() / 2).Format(time.RFC3339)},
		SIDs:  []v2.SID{{SID: sid}},
	}
	stateDir := t.TempDir()
	observations, flags := grokObservationsAt([]v2.SessionRecord{rec}, stateDir, nil, nil, now)
	if len(observations) != 0 || len(flags) != 0 {
		t.Fatalf("fresh-seat observations/flags = %+v/%+v, want boot-window silence", observations, flags)
	}

	rec.Seat.ConfirmedAt = now.Add(-sweepInterval() - time.Second).Format(time.RFC3339)
	observations, flags = grokObservationsAt([]v2.SessionRecord{rec}, stateDir, nil, nil, now)
	if len(observations) != 0 {
		t.Fatalf("observations = %+v, want none for missing artifacts", observations)
	}
	if len(flags) != 1 || flags[0].GUID != guid || flags[0].Type != "grok-session-undiscovered" || flags[0].Severity != "warning" {
		t.Fatalf("flags = %+v, want labelled discovery-miss warning", flags)
	}
	if strings.Contains(flags[0].Suggested, "respawn") || !strings.Contains(flags[0].Suggested, "first completed turn") {
		t.Fatalf("suggested remedy = %q, want non-destructive first-turn guidance", flags[0].Suggested)
	}
}

func TestGrokTranscriptToleratesUnknownShapeAndCompletesPartialTail(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "chat_history.jsonl")
	events := filepath.Join(dir, "events.jsonl")
	prefix := []byte("{\"type\":\"user\",\"content\":\"one\"}\n" +
		"{\"type\":\"tool_call\",\"tool\":\"shell\"}\n")
	partial := []byte("{\"type\":\"assistant\",\"content\":\"par")
	if err := os.WriteFile(transcript, append(prefix, partial...), 0o600); err != nil {
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
	if cursor.transcript.Entries != 2 || cursor.transcript.User != 1 || cursor.transcript.Other != 1 {
		t.Fatalf("first summary = %+v, want user plus tolerant tool record", cursor.transcript)
	}
	if cursor.transcriptOffset != int64(len(prefix)) || cursor.eventStatus != "tool_execution" {
		t.Fatalf("cursor offset/status = %d/%q, want %d/tool_execution", cursor.transcriptOffset, cursor.eventStatus, len(prefix))
	}

	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("tial\"}\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := updateGrokTranscript(transcript, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.transcript.Entries != 3 || cursor.transcript.Assistant != 1 || cursor.transcript.Other != 1 {
		t.Fatalf("completed summary = %+v, want no double-count and no loss", cursor.transcript)
	}
	if err := updateGrokTranscript(transcript, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.transcript.Entries != 3 {
		t.Fatalf("stable summary = %+v, want completed tail counted once", cursor.transcript)
	}
}

func TestGrokCursorResetsOnShorterSameInodeTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat_history.jsonl")
	first := []byte("{\"type\":\"user\",\"content\":\"one\"}\n")
	data := append(append([]byte{}, first...), []byte("{\"type\":\"assistant\",\"content\":\"two\"}\n")...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := &grokArtifactCursor{}
	if err := updateGrokTranscript(path, cursor); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, int64(len(first))); err != nil {
		t.Fatal(err)
	}
	if err := updateGrokTranscript(path, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.transcript.Entries != 1 || cursor.transcript.User != 1 || cursor.transcript.Assistant != 0 {
		t.Fatalf("truncated summary = %+v, want shorter same-inode reset", cursor.transcript)
	}
}

func TestGrokCursorResetsOnRenameReplacementWithIdenticalFence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	anchor := "{\"type\":\"system\",\"content\":\"" + strings.Repeat("a", 96) + "\"}\n"
	tail := "{\"type\":\"assistant\",\"content\":\"" + strings.Repeat("z", 96) + "\"}\n"
	oldMiddle := "{\"type\":\"user\",\"content\":\"AAAAAA\"}\n"
	newMiddle := "{\"type\":\"system\",\"content\":\"BBBB\"}\n"
	oldData := []byte(anchor + oldMiddle + tail)
	newData := []byte(anchor + newMiddle + tail)
	if len(oldData) != len(newData) {
		t.Fatalf("test fixture sizes differ: %d != %d", len(oldData), len(newData))
	}
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := &grokArtifactCursor{}
	if err := updateGrokTranscript(path, cursor); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "replacement.jsonl")
	if err := os.WriteFile(replacement, newData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := updateGrokTranscript(path, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.transcript.System != 2 || cursor.transcript.User != 0 || cursor.transcript.Assistant != 1 {
		t.Fatalf("replacement summary = %+v, want inode-triggered reset despite identical fence", cursor.transcript)
	}
}

func TestGrokCursorStoresDigestNotRawTranscriptBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat_history.jsonl")
	data := []byte("{\"type\":\"user\",\"content\":\"raw-marker-must-not-be-retained\"}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := &grokArtifactCursor{}
	if err := updateGrokTranscript(path, cursor); err != nil {
		t.Fatal(err)
	}
	fenceBytes := data
	if len(fenceBytes) > 64 {
		fenceBytes = fenceBytes[len(fenceBytes)-64:]
	}
	want := sha256.Sum256(fenceBytes)
	if !cursor.transcriptFence.valid || cursor.transcriptFence.digest != want {
		t.Fatalf("cursor fence = %+v, want SHA-256 digest of bounded fence", cursor.transcriptFence)
	}
	if bytes.Contains(cursor.transcriptFence.digest[:], []byte("raw-marker")) {
		t.Fatal("cursor fence retained raw transcript content")
	}
}

func TestGrokEventStatusClearsWhenArtifactDisappears(t *testing.T) {
	grokHome := filepath.Join(t.TempDir(), "grok-home")
	sid := newTestGUID(t)
	sessionDir := filepath.Join(grokHome, "sessions", "opaque-cwd", sid)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyGrokFixture(t, sessionDir, "chat_history.jsonl")
	path := filepath.Join(sessionDir, "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"phase_changed\",\"phase\":\"tool_execution\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := &grokArtifactCursor{}
	obs, err := observeGrokSession(grokHome, sid, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if obs.EventStatus != "tool_execution" {
		t.Fatalf("initial event status = %q, want tool_execution", obs.EventStatus)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	obs, err = observeGrokSession(grokHome, sid, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if obs.EventStatus != "" || obs.EventSource != "" {
		t.Fatalf("observation after disappearance = %+v, want stale event evidence absent", obs)
	}
	if cursor.eventStatus != "" || cursor.eventsPath != "" || cursor.eventsInfo != nil || cursor.eventsOffset != 0 || cursor.eventsFence.valid {
		t.Fatalf("event cursor after disappearance = %+v, want stale evidence cleared", cursor)
	}
}

func TestGrokRecordedToolAndPermissionEventsArePinned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data, err := os.ReadFile(filepath.Join("testdata", "grok-session", "events-tool-permission.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := &grokArtifactCursor{}
	if err := updateGrokEventStatus(path, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.eventStatus != "permission_resolved" {
		t.Fatalf("event status = %q, want recorded permission cycle terminus", cursor.eventStatus)
	}
}

func TestGrokEventAdapterDoesNotInventStatusFromUnknownEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data := []byte("{\"type\":\"phase_changed\",\"phase\":\"waiting_for_model\"}\n" +
		"{\"type\":\"phase_changed\",\"phase\":\"hostile status\"}\n" +
		"{\"type\":\"turn_ended\"}\n" +
		"{\"type\":\"unrecognised_vendor_event\",\"phase\":\"idle\"}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cursor := &grokArtifactCursor{}
	if err := updateGrokEventStatus(path, cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.eventStatus != "waiting_for_model" {
		t.Fatalf("event status = %q, want last supported on-disk evidence without guessed turn_ended or idle", cursor.eventStatus)
	}
}

func TestGrokObservationsRequireExplicitSessionIdentity(t *testing.T) {
	stateDir := t.TempDir()
	rec := v2.SessionRecord{
		GUID:       newTestGUID(t),
		Tool:       "grok",
		State:      v2.StateSeated,
		Provenance: v2.Provenance{CWD: filepath.Join(t.TempDir(), "workspace")},
	}
	observations, flags := grokObservations([]v2.SessionRecord{rec}, stateDir, nil, nil)
	if len(observations) != 0 || len(flags) != 0 {
		t.Fatalf("without session identity observations/flags = %+v/%+v, want none", observations, flags)
	}
}

func newTestGUID(t *testing.T) string {
	t.Helper()
	guid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	return guid
}

func copyGrokFixture(t *testing.T, dstDir, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "grok-session", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
