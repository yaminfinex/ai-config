package servecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	PrevOffset *int64               `json:"prevOffset,omitempty"`
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
	Mode   string `json:"mode"`
	From   int64  `json:"from"`
	Before *int64 `json:"before,omitempty"`
	Limit  int    `json:"limit"`
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

func TestEntriesEndpointServesSessionAfterAgentChangesDirectory(t *testing.T) {
	home, _ := writeEntrySession(t, sessionLines(
		`{"type":"assistant","uuid":"invented-stable-cwd","message":{"content":[{"type":"text","text":"Still visible."}]}}`,
	))
	t.Setenv("HOME", home)
	deps := entryDepsWithRow(hcomidentity.Row{
		Name: "dore", Tool: "claude", Status: "active", SessionID: fixtureSessionID,
		Directory: "/invented/violet/tools/herder",
	})

	response := requestEntries(t, deps, "/api/agents/dore/entries?from=0&limit=1")
	page := decodeEntriesResponse(t, response)
	if response.Code != http.StatusOK || page.Entries == nil || len(*page.Entries) != 1 || (*page.Entries)[0].UUID != "invented-stable-cwd" {
		t.Fatalf("cwd-changed response = %d %#v %s", response.Code, page, response.Body.String())
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

func TestEntriesEndpointReadsRealSubagentSidechainShape(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, ".claude", "projects", claudesession.Slug("/invented/violet"))
	parentPath := filepath.Join(project, fixtureSessionID+".jsonl")
	childPath := filepath.Join(project, fixtureSessionID, "subagents", "agent-a35b593a6be7a9ba5.jsonl")
	if err := os.MkdirAll(filepath.Dir(childPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parentPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := sessionLines(
		`{"parentUuid":null,"isSidechain":true,"agentId":"a35b593a6be7a9ba5","type":"user","message":{"role":"user","content":"Invented Task prompt."}}`,
		`{"parentUuid":"invented-child-prompt","isSidechain":true,"agentId":"a35b593a6be7a9ba5","type":"assistant","uuid":"invented-child-answer","message":{"role":"assistant","content":[{"type":"text","text":"Invented Task answer."}]}}`,
	)
	if err := os.WriteFile(childPath, []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	deps := fixtureDeps()
	deps.roster = func() ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{
			{Name: "probe-fame", BaseName: "fame", Tool: "claude", Status: "active", Directory: "/invented/violet", SessionID: fixtureSessionID},
			{Name: "probe-fame_general_purpose_1", BaseName: "fame_general_purpose_1", ParentName: "fame", AgentID: "a35b593a6be7a9ba5", Tool: "claude", Status: "active", Directory: "/invented/violet"},
		}, nil
	}

	response := requestEntries(t, deps, "/api/agents/probe-fame_general_purpose_1/entries?limit=10")
	page := decodeEntriesResponse(t, response)
	if response.Code != http.StatusOK || page.SessionID != "subagent:a35b593a6be7a9ba5" || page.Entries == nil || len(*page.Entries) != 2 || (*page.Entries)[1].UUID != "invented-child-answer" || page.Stats == nil || page.Stats.SidechainSkipped != 0 {
		t.Fatalf("subagent entries = %d %#v %s", response.Code, page, response.Body.String())
	}
}

func TestEntriesEndpointRoutesSubagentFromItsOwnEvidenceWithoutLiveParent(t *testing.T) {
	home := t.TempDir()
	childPath := filepath.Join(home, ".claude", "projects", claudesession.Slug("/invented/violet"), fixtureSessionID, "subagents", "agent-a35b593a6be7a9ba5.jsonl")
	if err := os.MkdirAll(filepath.Dir(childPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(sessionLines(`{"parentUuid":null,"isSidechain":true,"agentId":"a35b593a6be7a9ba5","type":"assistant","uuid":"row-owned","message":{"content":[{"type":"text","text":"Still here."}]}}`)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	deps := entryDepsWithRow(hcomidentity.Row{
		Name: "probe-child", Tool: "claude", Status: "active", ParentName: "fame",
		AgentID: "a35b593a6be7a9ba5", TranscriptPath: childPath,
	})

	response := requestEntries(t, deps, "/api/agents/probe-child/entries?limit=10")
	page := decodeEntriesResponse(t, response)
	if response.Code != http.StatusOK || page.SessionID != "subagent:a35b593a6be7a9ba5" || page.Entries == nil || len(*page.Entries) != 1 || (*page.Entries)[0].UUID != "row-owned" {
		t.Fatalf("row-owned subagent response = %d %#v %s", response.Code, page, response.Body.String())
	}
}

func TestEntriesEndpointResolvesStoppedSubagentFromTranscriptShape(t *testing.T) {
	home := t.TempDir()
	childPath := filepath.Join(home, ".claude", "projects", claudesession.Slug("/invented/violet"), fixtureSessionID, "subagents", "agent-a35b593a6be7a9ba5.jsonl")
	if err := os.MkdirAll(filepath.Dir(childPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(sessionLines(`{"parentUuid":null,"isSidechain":true,"agentId":"a35b593a6be7a9ba5","type":"assistant","uuid":"retained-child","message":{"content":[{"type":"text","text":"Retained."}]}}`)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	row, err := hcomidentity.DecodeStopped("probe-child", []byte("Stopped: child\n  Time: now\n  Tool: claude\n  Transcript: "+childPath+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	deps := fixtureDeps()
	deps.roster = func() ([]hcomidentity.Row, error) { return nil, nil }
	deps.stopped = func(string) (hcomidentity.Row, error) { return row, nil }

	response := requestEntries(t, deps, "/api/agents/probe-child/entries?limit=10")
	page := decodeEntriesResponse(t, response)
	if response.Code != http.StatusOK || page.Entries == nil || len(*page.Entries) != 1 || (*page.Entries)[0].UUID != "retained-child" {
		t.Fatalf("stopped subagent response = %d %#v %s", response.Code, page, response.Body.String())
	}
}

func TestEntriesEndpointRefusesMissingSubagentTranscriptHonestly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	deps := entryDepsWithRow(hcomidentity.Row{
		Name: "probe-child", Tool: "claude", Status: "active", ParentName: "fame", AgentID: "a35b593a6be7a9ba5",
	})
	response := requestEntries(t, deps, "/api/agents/probe-child/entries?limit=10")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"no independent transcript"`) || strings.Contains(response.Body.String(), "missing_session_id") {
		t.Fatalf("missing subagent transcript = %d %s", response.Code, response.Body.String())
	}
}

func TestEntriesEndpointKeepsNonDegradeSubagentResolveRefusalPlain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	deps := entryDepsWithRow(hcomidentity.Row{
		Name: "probe-child", Tool: "claude", Status: "active", ParentName: "fame", AgentID: "INVALID-SUBAGENT-ID",
	})
	response := requestEntries(t, deps, "/api/agents/probe-child/entries?limit=10")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"no session"`) || strings.Contains(response.Body.String(), `"error":"no independent transcript"`) {
		t.Fatalf("non-degrade subagent refusal = %d %s", response.Code, response.Body.String())
	}
}

func TestReadQueueExclusionsUsesToolSpecificCompactBoundaryPolicy(t *testing.T) {
	for _, fixture := range []struct {
		name              string
		path              string
		row               hcomidentity.Row
		put               func(*testing.T, string) (string, string)
		wantOlderExcluded bool
	}{
		{
			name: "claude", path: "../claudesession/testdata/taxonomy.jsonl",
			row:               hcomidentity.Row{Tool: "claude", SessionID: fixtureSessionID, Directory: "/invented/violet"},
			put:               writeEntrySession,
			wantOlderExcluded: true,
		},
		{
			name: "codex", path: "../codexsession/testdata/taxonomy.jsonl",
			row: hcomidentity.Row{Tool: "codex", SessionID: fixtureSessionID},
			put: writeCodexEntrySession,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			content, err := os.ReadFile(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			home, _ := fixture.put(t, string(content))
			t.Setenv("HOME", home)
			excluded, err := readQueueExclusions(fixture.row, map[string]queueCandidate{
				"730": {SentAt: "2026-01-02T03:04:04Z"},
				"733": {SentAt: "2026-01-02T03:04:18Z"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if excluded["730"] != fixture.wantOlderExcluded || excluded["733"] {
				t.Fatalf("compact-boundary exclusions = %#v", excluded)
			}
		})
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
		"/api/agents/dore/entries?before=-1",
		"/api/agents/dore/entries?before=x",
		"/api/agents/dore/entries?before=0&before=0",
		"/api/agents/dore/entries?from=0&before=3",
		"/api/agents/dore/entries?before=1",
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

func TestEntriesEndpointServesRetiredAgentFromStoppedSessionEvidence(t *testing.T) {
	home, _ := writeEntrySession(t, sessionLines(`{"type":"assistant","uuid":"a1","timestamp":"2026-01-02T03:04:05Z","message":{"content":[{"type":"text","text":"retained reply"}]}}`))
	t.Setenv("HOME", home)
	deps := fixtureDeps()
	deps.roster = func() ([]hcomidentity.Row, error) { return nil, nil }
	deps.stopped = func(name string) (hcomidentity.Row, error) {
		return hcomidentity.Row{Name: name, BaseName: name, Tool: "claude", Status: "retired", SessionID: fixtureSessionID, Directory: "/invented/violet"}, nil
	}
	response := requestEntries(t, deps, "/api/agents/dore/entries?limit=500")
	page := decodeEntriesResponse(t, response)
	if response.Code != http.StatusOK || page.SessionID != fixtureSessionID || page.Entries == nil || len(*page.Entries) != 1 {
		t.Fatalf("retired entries = %d %#v", response.Code, page)
	}
}

func TestEntriesEndpointPagesBackwardToStartOfFile(t *testing.T) {
	a := `{"type":"assistant","uuid":"invented-a","message":{"content":[{"type":"text","text":"a"}]}}`
	side := `{"type":"assistant","isSidechain":true,"uuid":"invented-side","message":{"content":[{"type":"text","text":"side"}]}}`
	bookkeeping := `{"type":"queue-operation","uuid":"invented-bookkeeping"}`
	c := `{"type":"assistant","uuid":"invented-c","message":{"content":[{"type":"text","text":"c"}]}}`
	d := `{"type":"assistant","uuid":"invented-d","message":{"content":[{"type":"text","text":"d"}]}}`
	e := `{"type":"assistant","uuid":"invented-e","message":{"content":[{"type":"text","text":"e"}]}}`
	complete := sessionLines(a, side, bookkeeping, c, d, e)
	home, _ := writeEntrySession(t, complete+`{"type":"assistant","uuid":"invented-partial"`)
	t.Setenv("HOME", home)
	offset := func(line string) int64 { return int64(strings.Index(complete, line)) }
	deps := entryFixtureDeps()
	page := func(query string) fixtureEntriesResponse {
		t.Helper()
		response := requestEntries(t, deps, "/api/agents/dore/entries?"+query)
		if response.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", query, response.Code, response.Body.String())
		}
		return decodeEntriesResponse(t, response)
	}
	uuids := func(p fixtureEntriesResponse) []string {
		var ids []string
		for _, entry := range *p.Entries {
			ids = append(ids, entry.UUID)
		}
		return ids
	}

	tail := page("limit=2")
	if tail.Window.From != offset(d) || strings.Join(uuids(tail), ",") != "invented-d,invented-e" || tail.PrevOffset != nil {
		t.Fatalf("tail = %#v", tail)
	}
	before := offset(d)
	first := page(fmt.Sprintf("before=%d&limit=1&sessionId=%s", before, fixtureSessionID))
	if first.SessionID != fixtureSessionID || first.Window != (fixtureEntriesWindow{Mode: "before", From: offset(c), Before: first.Window.Before, Limit: 1}) || first.Window.Before == nil || *first.Window.Before != before {
		t.Fatalf("first page window = %#v", first.Window)
	}
	if strings.Join(uuids(first), ",") != "invented-c" || (*first.Entries)[0].Line != 3 || first.PrevOffset == nil || *first.PrevOffset != offset(c) || first.NextOffset == nil || *first.NextOffset != before || first.Stats.SidechainSkipped != 0 {
		t.Fatalf("first page = %#v", first)
	}
	second := page(fmt.Sprintf("before=%d&limit=5", *first.PrevOffset))
	if strings.Join(uuids(second), ",") != "invented-a" || (*second.Entries)[0].Line != 0 || *second.PrevOffset != 0 || second.Stats.SidechainSkipped != 1 {
		t.Fatalf("second page = %#v", second)
	}
	// The first record filling the window is still the start of file.
	exact := page(fmt.Sprintf("before=%d&limit=2", offset(d)))
	if strings.Join(uuids(exact), ",") != "invented-a,invented-c" || *exact.PrevOffset != 0 {
		t.Fatalf("window ending on the first record = %#v", exact)
	}
	start := page("before=0")
	if start.Entries == nil || len(*start.Entries) != 0 || *start.PrevOffset != 0 {
		t.Fatalf("start of file = %#v", start)
	}

	for _, query := range []string{
		fmt.Sprintf("before=%d", offset(d)+1),
		fmt.Sprintf("before=%d", len(complete)+3),
	} {
		if response := requestEntries(t, deps, "/api/agents/dore/entries?"+query); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "record boundary") {
			t.Fatalf("%s = %d %s", query, response.Code, response.Body.String())
		}
	}
	for _, test := range []struct {
		query  string
		reason claudesession.ResetReason
	}{
		{fmt.Sprintf("before=%d&sessionId=73200000-0000-4000-8000-000000000732", before), claudesession.ResetSessionChanged},
		{fmt.Sprintf("before=%d&sessionId=%s", len(complete)+731, fixtureSessionID), claudesession.ResetTruncated},
	} {
		reset := page(test.query)
		if reset.Reset == nil || reset.Reset.Reason != test.reason || reset.Entries != nil || reset.PrevOffset != nil || reset.NextOffset != nil || reset.Window.Mode != "before" {
			t.Fatalf("%s = %#v", test.query, reset)
		}
	}
}

func TestEntriesEndpointTailCountsSidechainSkipsInItsWindowOnly(t *testing.T) {
	side := `{"type":"assistant","isSidechain":true,"uuid":"invented-side","message":{"content":[{"type":"text","text":"side"}]}}`
	a := `{"type":"assistant","uuid":"invented-a","message":{"content":[{"type":"text","text":"a"}]}}`
	b := `{"type":"assistant","uuid":"invented-b","message":{"content":[{"type":"text","text":"b"}]}}`
	home, _ := writeEntrySession(t, sessionLines(side, a, side, b))
	t.Setenv("HOME", home)
	for _, test := range []struct {
		limit, skipped int
	}{{1, 0}, {2, 1}, {3, 2}} {
		page := decodeEntriesResponse(t, requestEntries(t, entryFixtureDeps(), fmt.Sprintf("/api/agents/dore/entries?limit=%d", test.limit)))
		if page.Stats == nil || page.Stats.SidechainSkipped != test.skipped {
			t.Fatalf("tail limit %d stats = %#v", test.limit, page.Stats)
		}
	}
}

func TestEntriesEndpointPagesSubagentAndCodexBackward(t *testing.T) {
	t.Run("subagent renders its sidechain records", func(t *testing.T) {
		home := t.TempDir()
		project := filepath.Join(home, ".claude", "projects", claudesession.Slug("/invented/violet"))
		childPath := filepath.Join(project, fixtureSessionID, "subagents", "agent-a35b593a6be7a9ba5.jsonl")
		if err := os.MkdirAll(filepath.Dir(childPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, fixtureSessionID+".jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		child := sessionLines(
			`{"isSidechain":true,"agentId":"a35b593a6be7a9ba5","type":"assistant","uuid":"invented-child-one","message":{"role":"assistant","content":[{"type":"text","text":"one"}]}}`,
			`{"isSidechain":true,"agentId":"a35b593a6be7a9ba5","type":"assistant","uuid":"invented-child-two","message":{"role":"assistant","content":[{"type":"text","text":"two"}]}}`,
		)
		if err := os.WriteFile(childPath, []byte(child), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)
		deps := fixtureDeps()
		deps.roster = func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{
				{Name: "probe-fame", BaseName: "fame", Tool: "claude", Status: "active", Directory: "/invented/violet", SessionID: fixtureSessionID},
				{Name: "probe-fame_general_purpose_1", BaseName: "fame_general_purpose_1", ParentName: "fame", AgentID: "a35b593a6be7a9ba5", Tool: "claude", Status: "active", Directory: "/invented/violet"},
			}, nil
		}
		response := requestEntries(t, deps, fmt.Sprintf("/api/agents/probe-fame_general_purpose_1/entries?before=%d&sessionId=subagent:a35b593a6be7a9ba5", len(child)))
		page := decodeEntriesResponse(t, response)
		if response.Code != http.StatusOK || page.SessionID != "subagent:a35b593a6be7a9ba5" || page.Entries == nil || len(*page.Entries) != 2 || (*page.Entries)[0].UUID != "invented-child-one" || *page.PrevOffset != 0 || page.Stats.SidechainSkipped != 0 {
			t.Fatalf("subagent backward = %d %#v %s", response.Code, page, response.Body.String())
		}
	})
	t.Run("codex", func(t *testing.T) {
		prompt := `{"timestamp":"2026-01-02T03:04:05Z","type":"event_msg","payload":{"type":"user_message","message":"Invented Codex prompt.","images":[],"local_images":[],"text_elements":[]}}`
		answer := `{"timestamp":"2026-01-02T03:04:06Z","type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Invented Codex answer."}]}}`
		content := sessionLines(prompt, answer)
		home, _ := writeCodexEntrySession(t, content)
		t.Setenv("HOME", home)
		deps := entryDepsWithRow(hcomidentity.Row{Name: "dore", Tool: "codex", Status: "active", SessionID: fixtureSessionID})
		response := requestEntries(t, deps, fmt.Sprintf("/api/agents/dore/entries?before=%d&limit=1&sessionId=%s", len(content), fixtureSessionID))
		page := decodeEntriesResponse(t, response)
		answerOffset := int64(len(prompt) + 1)
		if response.Code != http.StatusOK || page.Entries == nil || len(*page.Entries) != 1 || (*page.Entries)[0].Kind != claudesession.KindAssistantText || (*page.Entries)[0].Line != 1 || *page.PrevOffset != answerOffset {
			t.Fatalf("codex backward = %d %#v %s", response.Code, page, response.Body.String())
		}
		response = requestEntries(t, deps, fmt.Sprintf("/api/agents/dore/entries?before=%d&limit=1", answerOffset))
		page = decodeEntriesResponse(t, response)
		if response.Code != http.StatusOK || len(*page.Entries) != 1 || (*page.Entries)[0].Kind != claudesession.KindHumanPrompt || *page.PrevOffset != 0 {
			t.Fatalf("codex first page = %d %#v", response.Code, page)
		}
	})
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

func TestPerAgentReadsResolveFromFreshRosterCache(t *testing.T) {
	const nextSessionID = "73200000-0000-4000-8000-000000000732"
	content := sessionLines(`{"type":"assistant","uuid":"invented-a","message":{"content":[{"type":"text","text":"a"}]}}`)
	home, path := writeEntrySession(t, content)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), nextSessionID+".jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	cachedRow := hcomidentity.Row{Name: "dore", Tool: "claude", Status: "active", SessionID: fixtureSessionID, Directory: "/invented/violet"}
	liveRow := cachedRow
	liveRow.SessionID = nextSessionID
	clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	deps := fixtureDeps()
	deps.rosterCache = &rosterCache{now: func() time.Time { return clock }}
	deps.rosterCache.set([]hcomidentity.Row{cachedRow})
	rosterCalls := 0
	deps.roster = func() ([]hcomidentity.Row, error) {
		rosterCalls++
		return []hcomidentity.Row{liveRow}, nil
	}

	// Inside RosterFreshness a cached name costs no hcom call, and the new
	// incarnation hcom already knows is not seen yet: that is the bound.
	clock = clock.Add(RosterFreshness)
	page := decodeEntriesResponse(t, requestEntries(t, deps, "/api/agents/dore/entries?limit=1"))
	if rosterCalls != 0 || page.SessionID != fixtureSessionID {
		t.Fatalf("fresh cache read = calls %d session %q", rosterCalls, page.SessionID)
	}
	detail := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
	if detail.Code != http.StatusOK || rosterCalls != 0 {
		t.Fatalf("fresh cache detail = %d calls %d %s", detail.Code, rosterCalls, detail.Body.String())
	}

	// Past the bound the read asks hcom live: the new incarnation's session
	// replaces the old one and refreshes the cache for the next read.
	clock = clock.Add(time.Nanosecond)
	page = decodeEntriesResponse(t, requestEntries(t, deps, "/api/agents/dore/entries?limit=1"))
	if rosterCalls != 1 || page.SessionID != nextSessionID {
		t.Fatalf("stale cache read = calls %d session %q", rosterCalls, page.SessionID)
	}
	page = decodeEntriesResponse(t, requestEntries(t, deps, "/api/agents/dore/entries?limit=1"))
	if rosterCalls != 1 || page.SessionID != nextSessionID {
		t.Fatalf("refreshed cache read = calls %d session %q", rosterCalls, page.SessionID)
	}

	// A name the fresh cache lacks asks hcom live before the stopped path.
	deps.roster = func() ([]hcomidentity.Row, error) {
		rosterCalls++
		return []hcomidentity.Row{liveRow, {Name: "newcomer", Tool: "claude", Status: "active", SessionID: fixtureSessionID, Directory: "/invented/violet"}}, nil
	}
	deps.stopped = func(string) (hcomidentity.Row, error) {
		t.Fatal("a live newcomer reached the stopped path")
		return hcomidentity.Row{}, nil
	}
	response := requestEntries(t, deps, "/api/agents/newcomer/entries?limit=1")
	if response.Code != http.StatusOK || rosterCalls != 2 {
		t.Fatalf("uncached name = %d calls %d %s", response.Code, rosterCalls, response.Body.String())
	}
}

func TestObserverRosterPollRefreshesRosterCache(t *testing.T) {
	deps := fixtureDeps()
	deps.rosterCache = &rosterCache{}
	row := hcomidentity.Row{Name: "dore", Tool: "claude", SessionID: fixtureSessionID}
	deps.roster = func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{row}, nil }
	if _, err := cachingRoster(deps)(); err != nil {
		t.Fatal(err)
	}
	if rows, ok := deps.rosterCache.fresh(RosterFreshness); !ok || len(rows) != 1 || rows[0].SessionID != row.SessionID {
		t.Fatalf("cache after observer poll = %#v, %v", rows, ok)
	}
	deps.rosterCache = &rosterCache{}
	deps.roster = func() ([]hcomidentity.Row, error) { return nil, errors.New("invented roster outage") }
	if _, err := cachingRoster(deps)(); err == nil {
		t.Fatal("observer poll hid the roster error")
	}
	if _, ok := deps.rosterCache.fresh(RosterFreshness); ok {
		t.Fatal("a failed poll marked the cache fresh")
	}
}

// Every roster writer stamps the time its `hcom list` began, so a slow fetch
// that returns after a newer snapshot was cached cannot resurrect the
// replaced session or restart the freshness clock.
func TestDelayedOlderRosterNeverOverwritesNewerSnapshot(t *testing.T) {
	oldRow := hcomidentity.Row{Name: "dore", Tool: "claude", Status: "active", SessionID: "old", Directory: "/invented/violet"}
	newRow := oldRow
	newRow.SessionID = "new"
	writers := map[string]func(dependencies) error{
		"observer poll": func(deps dependencies) error {
			_, err := cachingRoster(deps)()
			return err
		},
		"per-agent fallback": func(deps dependencies) error {
			_, _, _, err := resolveAgentEvidence(deps, "dore")
			return err
		},
		"fleet read": func(deps dependencies) error {
			_, _, err := readFleetInputs(deps)
			return err
		},
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
			deps := fixtureDeps()
			deps.rosterCache = &rosterCache{now: func() time.Time { return clock }}
			deps.roster = func() ([]hcomidentity.Row, error) {
				// While this fetch is in flight another reader caches the
				// new incarnation at t1; this answer lands at t4.
				clock = clock.Add(time.Second)
				deps.rosterCache.set([]hcomidentity.Row{newRow})
				clock = clock.Add(3 * time.Second)
				return []hcomidentity.Row{oldRow}, nil
			}
			if err := write(deps); err != nil {
				t.Fatal(err)
			}
			rows, ok := deps.rosterCache.get()
			if !ok || len(rows) != 1 || rows[0].SessionID != "new" {
				t.Fatalf("cache after delayed older answer = %#v", rows)
			}
			// The kept snapshot is aged from t1, not restamped at t4.
			if _, ok := deps.rosterCache.fresh(3*time.Second - time.Nanosecond); ok {
				t.Fatal("delayed answer restarted the freshness clock")
			}
		})
	}
}

// A slow fetch is aged from when it began: rows observed longer ago than
// RosterFreshness are never served from the cache, however recently they
// arrived.
func TestSlowRosterFetchIsAgedFromItsStart(t *testing.T) {
	clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	deps := fixtureDeps()
	deps.rosterCache = &rosterCache{now: func() time.Time { return clock }}
	row := hcomidentity.Row{Name: "dore", Tool: "claude", SessionID: fixtureSessionID}
	deps.roster = func() ([]hcomidentity.Row, error) {
		clock = clock.Add(RosterFreshness + time.Second)
		return []hcomidentity.Row{row}, nil
	}
	if _, err := cachingRoster(deps)(); err != nil {
		t.Fatal(err)
	}
	if _, ok := deps.rosterCache.fresh(RosterFreshness); ok {
		t.Fatal("a slow fetch was served as fresh past RosterFreshness from its start")
	}
	if rows, ok := deps.rosterCache.get(); !ok || len(rows) != 1 {
		t.Fatalf("slow fetch was not cached for name resolution: %#v", rows)
	}
	quick := time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC)
	clock = quick
	deps.roster = func() ([]hcomidentity.Row, error) {
		clock = clock.Add(time.Second)
		return []hcomidentity.Row{row}, nil
	}
	if _, err := cachingRoster(deps)(); err != nil {
		t.Fatal(err)
	}
	clock = quick.Add(RosterFreshness)
	if _, ok := deps.rosterCache.fresh(RosterFreshness); !ok {
		t.Fatal("a fetch begun exactly RosterFreshness ago was refused")
	}
	clock = clock.Add(time.Nanosecond)
	if _, ok := deps.rosterCache.fresh(RosterFreshness); ok {
		t.Fatal("a fetch begun past RosterFreshness was served")
	}
}
