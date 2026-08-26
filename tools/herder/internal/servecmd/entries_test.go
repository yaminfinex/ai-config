package servecmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/codexsession"
	"ai-config/tools/herder/internal/hcomidentity"
)

const fixtureSessionID = "73100000-0000-4000-8000-000000000731"

type fixtureEntriesResponse struct {
	SessionID  string               `json:"sessionId"`
	Window     fixtureEntriesWindow `json:"window"`
	Entries    *[]fixtureEntry      `json:"entries,omitempty"`
	NextOffset *int64               `json:"nextOffset,omitempty"`
	Reset      *claudesession.Reset `json:"reset,omitempty"`
	Stats      *fixtureEntriesStats `json:"stats,omitempty"`
}

type fixtureEntriesStats struct {
	SidechainSkipped int `json:"sidechainSkipped"`
}

type fixtureEntry struct {
	UUID       string             `json:"uuid,omitempty"`
	Line       int64              `json:"line"`
	ByteOffset int64              `json:"byteOffset"`
	Timestamp  string             `json:"timestamp,omitempty"`
	Kind       claudesession.Kind `json:"kind"`
	Payload    json.RawMessage    `json:"payload"`
}

type fixtureEntriesWindow struct {
	Mode  string `json:"mode"`
	From  int64  `json:"from"`
	Limit int    `json:"limit"`
}

func TestEntriesEndpointReadsCompleteClassifiedWindows(t *testing.T) {
	home, path := writeEntrySession(t, sessionLines(
		`{"type":"user","uuid":"invented-human","timestamp":"2026-01-02T03:04:05Z","origin":{"kind":"human"},"promptSource":"typed","message":{"content":"Invented prompt."}}`,
		`{"type":"assistant","uuid":"invented-answer","timestamp":"2026-01-02T03:04:06Z","message":{"content":[{"type":"text","text":"Invented answer."}]}}`,
		`{"type":"system","uuid":"invented-duration","subtype":"turn_duration","timestamp":"2026-01-02T03:04:07Z"}`,
	)+`{"type":"assistant","uuid":"invented-partial"}`)
	t.Setenv("HOME", home)

	response := requestEntries(t, entryFixtureDeps(), "/api/agents/dore/entries?from=0&limit=2&sessionId="+fixtureSessionID)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	page := decodeEntriesResponse(t, response)
	if page.SessionID != fixtureSessionID || page.Window != (fixtureEntriesWindow{Mode: "from", From: 0, Limit: 2}) {
		t.Fatalf("session/window = %q %#v", page.SessionID, page.Window)
	}
	if page.Entries == nil || len(*page.Entries) != 2 || (*page.Entries)[0].Kind != claudesession.KindHumanPrompt || (*page.Entries)[1].Kind != claudesession.KindAssistantText {
		t.Fatalf("entries = %#v", page.Entries)
	}
	thirdOffset := int64(strings.Index(sessionLines(
		`{"type":"user","uuid":"invented-human","timestamp":"2026-01-02T03:04:05Z","origin":{"kind":"human"},"promptSource":"typed","message":{"content":"Invented prompt."}}`,
		`{"type":"assistant","uuid":"invented-answer","timestamp":"2026-01-02T03:04:06Z","message":{"content":[{"type":"text","text":"Invented answer."}]}}`,
		`{"type":"system","uuid":"invented-duration","subtype":"turn_duration","timestamp":"2026-01-02T03:04:07Z"}`,
	), `{"type":"system"`))
	if page.NextOffset == nil || *page.NextOffset != thirdOffset {
		t.Fatalf("nextOffset = %#v, want %d (path %s)", page.NextOffset, thirdOffset, path)
	}
	if page.Reset != nil || page.Stats == nil || page.Stats.SidechainSkipped != 0 {
		t.Fatalf("reset/stats = %#v %#v", page.Reset, page.Stats)
	}
}

func TestEntriesEndpointFromWithoutSessionIDReturnsCurrentSessionID(t *testing.T) {
	home, _ := writeEntrySession(t, sessionLines(
		`{"type":"assistant","uuid":"invented-current","timestamp":"2026-01-02T03:04:06Z","message":{"content":[{"type":"text","text":"Invented current-session answer."}]}}`,
	))
	t.Setenv("HOME", home)

	response := requestEntries(t, entryFixtureDeps(), "/api/agents/dore/entries?from=0&limit=1")
	page := decodeEntriesResponse(t, response)
	if response.Code != http.StatusOK || page.SessionID != fixtureSessionID {
		t.Fatalf("from-without-sessionId = %d sessionId %q body=%s", response.Code, page.SessionID, response.Body.String())
	}
}

func TestEntriesEndpointDispatchesCodexRollout(t *testing.T) {
	home, _ := writeCodexEntrySession(t, sessionLines(
		`{"timestamp":"2026-01-02T03:04:05Z","type":"event_msg","payload":{"type":"user_message","message":"Invented Codex prompt.","images":[],"local_images":[],"text_elements":[]}}`,
		`{"timestamp":"2026-01-02T03:04:06Z","type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Invented Codex answer."}]}}`,
	))
	t.Setenv("HOME", home)
	deps := entryDepsWithRow(hcomidentity.Row{Name: "dore", Tool: "codex", Status: "active", SessionID: fixtureSessionID})

	response := requestEntries(t, deps, "/api/agents/dore/entries?from=0&limit=2")
	page := decodeEntriesResponse(t, response)
	if response.Code != http.StatusOK || page.SessionID != fixtureSessionID || page.Entries == nil || len(*page.Entries) != 2 {
		t.Fatalf("codex response = %d %#v %s", response.Code, page, response.Body.String())
	}
	if (*page.Entries)[0].Kind != claudesession.KindHumanPrompt || (*page.Entries)[1].Kind != claudesession.KindAssistantText {
		t.Fatalf("codex entries = %#v", *page.Entries)
	}
}

func TestEntriesEndpointPassesThroughToolOutputTruncation(t *testing.T) {
	output := strings.Repeat("v", 16*1024+731)
	line := `{"type":"user","uuid":"invented-result","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_invented","is_error":false,"content":` + mustEntryString(output) + `}]}}`
	home, _ := writeEntrySession(t, sessionLines(line))
	t.Setenv("HOME", home)

	response := requestEntries(t, entryFixtureDeps(), "/api/agents/dore/entries?from=0&limit=1")
	page := decodeEntriesResponse(t, response)
	if response.Code != http.StatusOK || page.Entries == nil || len(*page.Entries) != 1 {
		t.Fatalf("response = %d %#v %s", response.Code, page, response.Body.String())
	}
	var payload struct {
		Content    string `json:"content"`
		TotalBytes int    `json:"total_bytes"`
		Truncated  bool   `json:"truncated"`
	}
	if err := json.Unmarshal((*page.Entries)[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Content) != 16*1024 || payload.TotalBytes != len(output) || !payload.Truncated {
		t.Fatalf("payload = content:%d total:%d truncated:%v", len(payload.Content), payload.TotalBytes, payload.Truncated)
	}
}

func TestEntriesEndpointChoosesTailWindow(t *testing.T) {
	content := sessionLines(
		`{"type":"assistant","uuid":"invented-one","message":{"content":[{"type":"text","text":"one"}]}}`,
		`{"type":"assistant","uuid":"invented-two","message":{"content":[{"type":"text","text":"two"}]}}`,
		`{"type":"assistant","uuid":"invented-three","message":{"content":[{"type":"text","text":"three"}]}}`,
	)
	home, _ := writeEntrySession(t, content)
	t.Setenv("HOME", home)

	response := requestEntries(t, entryFixtureDeps(), "/api/agents/dore/entries?limit=2")
	page := decodeEntriesResponse(t, response)
	wantFrom := int64(strings.Index(content, `{"type":"assistant","uuid":"invented-two"`))
	if response.Code != http.StatusOK || page.Window != (fixtureEntriesWindow{Mode: "tail", From: wantFrom, Limit: 2}) {
		t.Fatalf("tail = %d %#v %s", response.Code, page, response.Body.String())
	}
	if page.Entries == nil || len(*page.Entries) != 2 || (*page.Entries)[0].UUID != "invented-two" || (*page.Entries)[1].UUID != "invented-three" || page.NextOffset == nil || *page.NextOffset != int64(len(content)) {
		t.Fatalf("tail entries = %#v next=%#v", page.Entries, page.NextOffset)
	}
}

func TestEntriesEndpointSurfacesTailResets(t *testing.T) {
	home, _ := writeEntrySession(t, sessionLines(`{"type":"system","subtype":"informational"}`))
	t.Setenv("HOME", home)

	for _, test := range []struct {
		name, query string
		reason      claudesession.ResetReason
	}{
		{"session changed", "from=7&sessionId=73200000-0000-4000-8000-000000000732", claudesession.ResetSessionChanged},
		{"truncated", "from=731&sessionId=" + fixtureSessionID, claudesession.ResetTruncated},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := requestEntries(t, entryFixtureDeps(), "/api/agents/dore/entries?"+test.query)
			page := decodeEntriesResponse(t, response)
			if response.Code != http.StatusOK || page.Reset == nil || page.Reset.Reason != test.reason || page.Entries != nil || page.NextOffset != nil || page.Stats != nil {
				t.Fatalf("reset = %d %#v %s", response.Code, page, response.Body.String())
			}
		})
	}
}

func TestEntriesEndpointMapsResolveRefusals(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tests := []struct {
		name   string
		deps   dependencies
		path   string
		status int
		short  string
	}{
		{"unknown agent", entryFixtureDeps(), "/api/agents/missing/entries", http.StatusNotFound, "unknown agent"},
		{"roster unavailable", func() dependencies {
			d := entryFixtureDeps()
			d.roster = func() ([]hcomidentity.Row, error) { return nil, errors.New("invented roster outage") }
			return d
		}(), "/api/agents/dore/entries", http.StatusBadGateway, "substrate unreachable"},
		{"wrong tool", entryDepsWithRow(hcomidentity.Row{Name: "dore", Tool: "gemini", SessionID: fixtureSessionID, Directory: "/invented/violet"}), "/api/agents/dore/entries", http.StatusConflict, "no session"},
		{"missing session", entryDepsWithRow(hcomidentity.Row{Name: "dore", Tool: "claude", Directory: "/invented/violet"}), "/api/agents/dore/entries", http.StatusConflict, "no session"},
		{"invalid session", entryDepsWithRow(hcomidentity.Row{Name: "dore", Tool: "claude", SessionID: "../invented", Directory: "/invented/violet"}), "/api/agents/dore/entries", http.StatusConflict, "no session"},
		{"file absent", entryFixtureDeps(), "/api/agents/dore/entries", http.StatusConflict, "no session"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestEntries(t, test.deps, test.path)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"error":"`+test.short+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestEntriesEndpointValidatesWindow(t *testing.T) {
	home, _ := writeEntrySession(t, "{}\n")
	t.Setenv("HOME", home)
	for _, path := range []string{
		"/api/agents/dore/entries?from=-1",
		"/api/agents/dore/entries?from=not-a-number",
		"/api/agents/dore/entries?limit=0",
		"/api/agents/dore/entries?limit=501",
		"/api/agents/dore/entries?sessionId=" + fixtureSessionID,
	} {
		response := requestEntries(t, entryFixtureDeps(), path)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestEntriesEndpointRejectsWrongMethod(t *testing.T) {
	home, _ := writeEntrySession(t, "{}\n")
	t.Setenv("HOME", home)
	response := httptest.NewRecorder()
	newHandler(entryFixtureDeps()).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/agents/dore/entries", nil))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "GET required") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func entryFixtureDeps() dependencies {
	return entryDepsWithRow(hcomidentity.Row{Name: "dore", Tool: "claude", Status: "active", SessionID: fixtureSessionID, Directory: "/invented/violet"})
}

func entryDepsWithRow(row hcomidentity.Row) dependencies {
	deps := fixtureDeps()
	deps.roster = func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{row}, nil }
	return deps
}

func writeEntrySession(t *testing.T, content string) (string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "projects", claudesession.Slug("/invented/violet"), fixtureSessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, path
}

func writeCodexEntrySession(t *testing.T, content string) (string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "sessions", "2026", "01", "02", "rollout-2026-01-02T03-04-05-"+fixtureSessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := codexsession.Resolve(home, hcomidentity.Row{Tool: "codex", SessionID: fixtureSessionID})
	if err != nil || resolved != path {
		t.Fatalf("fixture Codex resolve = %q, %v", resolved, err)
	}
	return home, path
}

func sessionLines(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func mustEntryString(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func requestEntries(t *testing.T, deps dependencies, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func decodeEntriesResponse(t *testing.T, response *httptest.ResponseRecorder) fixtureEntriesResponse {
	t.Helper()
	var page fixtureEntriesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
