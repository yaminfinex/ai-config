package observercmd

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ai-config/tools/herder/internal/observerstatus"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

const grokJSONLMaxLine = 16 << 20

var errGrokSessionUndiscovered = errors.New("Grok session directory was not discovered")

type grokTranscriptEntry struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

type grokTranscriptSummary struct {
	Entries   int
	System    int
	User      int
	Assistant int
	Other     int
}

type grokSessionObservation struct {
	TranscriptPath string
	Transcript     grokTranscriptSummary
	EventStatus    string
	EventSource    string
}

type grokArtifactCursor struct {
	transcriptPath   string
	transcriptInfo   os.FileInfo
	transcriptOffset int64
	transcriptFence  grokCursorFence
	transcript       grokTranscriptSummary
	eventsPath       string
	eventsInfo       os.FileInfo
	eventsOffset     int64
	eventsFence      grokCursorFence
	eventStatus      string
}

type grokCursorFence struct {
	digest [sha256.Size]byte
	valid  bool
}

func grokSessionDir(grokHome, sessionID string) (string, error) {
	if !filepath.IsAbs(grokHome) {
		return "", errors.New("Grok home is not absolute; run the observer with HERDER_STATE_DIR set to the seat's state root")
	}
	if !validGrokSessionID(sessionID) {
		return "", errors.New("Grok session identity is missing or malformed; resume or respawn the seat so its explicit session id is recorded")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Clean(grokHome), "sessions", "*", sessionID))
	if err != nil {
		return "", fmt.Errorf("discover Grok session directory: %w", err)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w for explicit session id %s; verify GROK_HOME and resume or respawn the seat", errGrokSessionUndiscovered, sessionID)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("Grok session id %s matched %d directories; remove duplicate session artifacts and retry the observer sweep", sessionID, len(matches))
	}
}

func validGrokSessionID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

func observeGrokSession(grokHome, sessionID string, cursor *grokArtifactCursor) (grokSessionObservation, error) {
	dir, err := grokSessionDir(grokHome, sessionID)
	if err != nil {
		return grokSessionObservation{}, err
	}
	if cursor == nil {
		cursor = &grokArtifactCursor{}
	}
	transcriptPath := filepath.Join(dir, "chat_history.jsonl")
	if err := updateGrokTranscript(transcriptPath, cursor); err != nil {
		return grokSessionObservation{}, fmt.Errorf("read Grok chat history: %w; verify the dedicated GROK_HOME session and retry the observer sweep", err)
	}
	eventsPath := filepath.Join(dir, "events.jsonl")
	err = updateGrokEventStatus(eventsPath, cursor)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return grokSessionObservation{}, fmt.Errorf("read Grok event enrichment: %w; verify the dedicated GROK_HOME session and retry the observer sweep", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		clearGrokEventCursor(cursor)
	}
	obs := grokSessionObservation{
		TranscriptPath: transcriptPath,
		Transcript:     cursor.transcript,
		EventStatus:    cursor.eventStatus,
	}
	if cursor.eventStatus != "" {
		obs.EventSource = "grok-events"
	}
	return obs, nil
}

func updateGrokTranscript(path string, cursor *grokArtifactCursor) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	offset, summary := cursor.transcriptOffset, cursor.transcript
	reset, err := artifactCursorReset(f, path, info, cursor.transcriptPath, cursor.transcriptInfo, offset, cursor.transcriptFence)
	if err != nil {
		return err
	}
	if reset {
		offset = 0
		summary = grokTranscriptSummary{}
	}
	complete, err := completeJSONLOffset(f, info.Size())
	if err != nil {
		return err
	}
	if complete < offset {
		offset = 0
		summary = grokTranscriptSummary{}
	}
	scanner := bufio.NewScanner(io.NewSectionReader(f, offset, complete-offset))
	scanner.Buffer(make([]byte, 64<<10), grokJSONLMaxLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry grokTranscriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			summary.Entries++
			summary.Other++
			continue
		}
		summary.Entries++
		switch entry.Type {
		case "system":
			if len(entry.Content) > 0 {
				summary.System++
			} else {
				summary.Other++
			}
		case "user":
			if len(entry.Content) > 0 {
				summary.User++
			} else {
				summary.Other++
			}
		case "assistant":
			if len(entry.Content) > 0 {
				summary.Assistant++
			} else {
				summary.Other++
			}
		default:
			summary.Other++
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	cursor.transcriptPath = path
	cursor.transcriptInfo = info
	cursor.transcriptOffset = complete
	cursor.transcriptFence, err = readCursorFence(f, complete)
	if err != nil {
		return err
	}
	cursor.transcript = summary
	return nil
}

func updateGrokEventStatus(path string, cursor *grokArtifactCursor) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	offset, status := cursor.eventsOffset, cursor.eventStatus
	reset, err := artifactCursorReset(f, path, info, cursor.eventsPath, cursor.eventsInfo, offset, cursor.eventsFence)
	if err != nil {
		return err
	}
	if reset {
		offset = 0
		status = ""
	}
	complete, err := completeJSONLOffset(f, info.Size())
	if err != nil {
		return err
	}
	if complete < offset {
		offset = 0
		status = ""
	}
	scanner := bufio.NewScanner(io.NewSectionReader(f, offset, complete-offset))
	scanner.Buffer(make([]byte, 64<<10), grokJSONLMaxLine)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Phase string `json:"phase"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("line %d is not valid JSON: %w", lineNo, err)
		}
		switch event.Type {
		case "phase_changed":
			if safeGrokEventLabel(event.Phase) {
				status = event.Phase
			}
		case "turn_started", "tool_started", "tool_completed", "permission_requested", "permission_resolved":
			status = event.Type
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	cursor.eventsPath = path
	cursor.eventsInfo = info
	cursor.eventsOffset = complete
	cursor.eventsFence, err = readCursorFence(f, complete)
	if err != nil {
		return err
	}
	cursor.eventStatus = status
	return nil
}

func artifactCursorReset(f *os.File, path string, info os.FileInfo, priorPath string, priorInfo os.FileInfo, offset int64, fence grokCursorFence) (bool, error) {
	if priorInfo == nil || path != priorPath || !os.SameFile(priorInfo, info) || info.Size() < offset {
		return true, nil
	}
	if offset == 0 || !fence.valid {
		return false, nil
	}
	current, err := readCursorFence(f, offset)
	if err != nil {
		return false, err
	}
	return current != fence, nil
}

func readCursorFence(f *os.File, offset int64) (grokCursorFence, error) {
	const fenceSize int64 = 64
	start := offset - fenceSize
	if start < 0 {
		start = 0
	}
	buf := make([]byte, offset-start)
	if len(buf) == 0 {
		return grokCursorFence{}, nil
	}
	if _, err := f.ReadAt(buf, start); err != nil {
		return grokCursorFence{}, err
	}
	return grokCursorFence{digest: sha256.Sum256(buf), valid: true}, nil
}

func clearGrokEventCursor(cursor *grokArtifactCursor) {
	cursor.eventsPath = ""
	cursor.eventsInfo = nil
	cursor.eventsOffset = 0
	cursor.eventsFence = grokCursorFence{}
	cursor.eventStatus = ""
}

func completeJSONLOffset(f *os.File, size int64) (int64, error) {
	if size == 0 {
		return 0, nil
	}
	var last [1]byte
	if _, err := f.ReadAt(last[:], size-1); err != nil {
		return 0, err
	}
	if last[0] == '\n' {
		return size, nil
	}
	const blockSize int64 = 4096
	var scanned int64
	for end := size; end > 0; {
		start := end - blockSize
		if start < 0 {
			start = 0
		}
		buf := make([]byte, end-start)
		scanned += int64(len(buf))
		if scanned > grokJSONLMaxLine {
			return 0, fmt.Errorf("incomplete JSONL record exceeds %d bytes", grokJSONLMaxLine)
		}
		if _, err := f.ReadAt(buf, start); err != nil {
			return 0, err
		}
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			return start + int64(i) + 1, nil
		}
		end = start
	}
	return 0, nil
}

func safeGrokEventLabel(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func grokObservations(records []v2.SessionRecord, stateDir string, stderr io.Writer, cursors map[string]*grokArtifactCursor) (map[string]observerstatus.Observation, []observerstatus.Flag) {
	out := map[string]observerstatus.Observation{}
	var flags []observerstatus.Flag
	active := map[string]bool{}
	for _, rec := range records {
		if rec.State != v2.StateSeated || rec.Tool != "grok" || rec.GUID == "" {
			continue
		}
		sessionID := latestSID(rec)
		if sessionID == "" {
			continue
		}
		active[rec.GUID] = true
		cursor := &grokArtifactCursor{}
		if cursors != nil {
			if cursors[rec.GUID] == nil {
				cursors[rec.GUID] = cursor
			} else {
				cursor = cursors[rec.GUID]
			}
		}
		obs, err := observeGrokSession(filepath.Join(stateDir, "grok-home"), sessionID, cursor)
		if errors.Is(err, errGrokSessionUndiscovered) {
			flags = append(flags, observerstatus.Flag{
				GUID:      rec.GUID,
				Label:     rec.Label,
				Type:      "grok-session-undiscovered",
				Severity:  "warning",
				Detail:    "explicit Grok session id has no matching directory under the dedicated GROK_HOME; observer keeps live status unknown",
				Suggested: "verify GROK_HOME and resume or respawn the seat",
			})
			continue
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			if stderr != nil {
				fmt.Fprintf(stderr, "herder observer sweep: Grok session artifacts for %s were not enriched: %v\n", rec.GUID, err)
			}
			continue
		}
		out[rec.GUID] = observerstatus.Observation{
			TranscriptPath:    obs.TranscriptPath,
			TranscriptSource:  "grok-chat-history",
			TranscriptEntries: obs.Transcript.Entries,
			EventStatus:       obs.EventStatus,
			EventSource:       obs.EventSource,
		}
	}
	for guid := range cursors {
		if !active[guid] {
			delete(cursors, guid)
		}
	}
	return out, flags
}
