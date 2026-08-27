package codexsession

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
)

const fixtureSessionID = "73100000-0000-4000-8000-000000000731"

func TestResolveDatedRollout(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "sessions", "2026", "01", "02", "rollout-2026-01-02T03-04-05-"+fixtureSessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	row := hcomidentity.Row{Tool: "codex", SessionID: fixtureSessionID}
	if got, err := Resolve(home, row); err != nil || got != path {
		t.Fatalf("Resolve() = %q, %v; want %q", got, err, path)
	}

	tests := []struct {
		name   string
		mutate func(*hcomidentity.Row)
		reason ResolveReason
	}{
		{"wrong tool", func(r *hcomidentity.Row) { r.Tool = "claude" }, ResolveWrongTool},
		{"missing", func(r *hcomidentity.Row) { r.SessionID = "" }, ResolveMissingSession},
		{"invalid", func(r *hcomidentity.Row) { r.SessionID = "../invented" }, ResolveInvalidSession},
		{"absent", func(r *hcomidentity.Row) { r.SessionID = "73200000-0000-4000-8000-000000000732" }, ResolveFileAbsent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := row
			tc.mutate(&candidate)
			_, err := Resolve(home, candidate)
			var typed *ResolveError
			if !errors.As(err, &typed) || typed.Reason != tc.reason {
				t.Fatalf("Resolve error = %#v, want reason %q", err, tc.reason)
			}
		})
	}

	second := filepath.Join(home, ".codex", "sessions", "2026", "01", "03", "rollout-2026-01-03T03-04-05-"+fixtureSessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(second), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(home, row)
	var typed *ResolveError
	if !errors.As(err, &typed) || typed.Reason != ResolveAmbiguousFile {
		t.Fatalf("ambiguous Resolve error = %#v", err)
	}
}

func TestTaxonomyFixtureAndDuplicateSuppression(t *testing.T) {
	t.Parallel()
	result, err := ReadFrom(filepath.Join("testdata", "taxonomy.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []Kind{
		KindHumanPrompt, KindAssistantText, KindThinking, KindToolUse,
		KindToolResult, KindToolUse, KindToolResult, KindHcomStub,
		KindHcomDelivery, KindInjectedSystem, KindTurnDuration,
		KindCompactDivider, KindUnknown,
	}
	if len(result.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %#v", len(result.Entries), len(want), result.Entries)
	}
	for i, kind := range want {
		if result.Entries[i].Kind != kind {
			t.Errorf("entry %d kind = %q, want %q", i, result.Entries[i].Kind, kind)
		}
	}
	if result.Entries[len(result.Entries)-1].Quarantine == nil || result.Entries[len(result.Entries)-1].Quarantine.Reason != "unknown_type" {
		t.Fatal("future top-level type was not quarantined visibly")
	}
	assertPayloadField(t, result.Entries[4].Payload, "is_error", true)
	assertPayloadField(t, result.Entries[6].Payload, "image_count", float64(1))
	assertPayloadField(t, result.Entries[10].Payload, "durationMs", float64(7310))
	if !strings.Contains(string(result.Entries[0].Payload), "Invented prompt") || !strings.Contains(string(result.Entries[1].Payload), "Invented answer") {
		t.Fatal("normalized message payloads lost visible text")
	}
	if !strings.Contains(string(result.Entries[9].Payload), "Invented system injection.") {
		t.Fatal("developer injection lost visible text")
	}

	var hcom struct {
		Subtype    string `json:"subtype"`
		Deliveries []struct {
			Intent    string `json:"intent"`
			Thread    string `json:"thread"`
			MessageID string `json:"message_id"`
			Sender    string `json:"sender"`
			Recipient string `json:"recipient"`
			Text      string `json:"text"`
		} `json:"deliveries"`
	}
	if err := json.Unmarshal(result.Entries[8].Payload, &hcom); err != nil {
		t.Fatal(err)
	}
	if hcom.Subtype != "developer_message" || len(hcom.Deliveries) != 1 || hcom.Deliveries[0].Intent != "request" || hcom.Deliveries[0].Thread != "violet-grid" || hcom.Deliveries[0].MessageID != "731" || hcom.Deliveries[0].Text != "Inspect the invented violet fixture." {
		t.Fatalf("hcom delivery = %+v", hcom)
	}

	pairs := Pair(result.Entries)
	if pairs[3].Related == nil || pairs[3].Related.Kind != KindToolResult || pairs[7].Related == nil || pairs[7].Related.Kind != KindHcomDelivery {
		t.Fatalf("pairs = %#v", pairs)
	}
}

func TestCompleteLinesQuarantineAndTail(t *testing.T) {
	t.Parallel()
	complete := "{invented corrupt\n" + `{"timestamp":"2026-01-02T03:04:05Z","type":"event_msg","payload":{"type":"user_message","message":"one"}}` + "\n"
	partial := `{"timestamp":"2026-01-02T03:04:06Z","type":"event_msg","payload":{"type":"user_message","message":"two"}}`
	path := writeTemp(t, complete+partial)
	first, err := ReadFrom(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 2 || first.Entries[0].Quarantine == nil || first.Entries[1].Kind != KindHumanPrompt || first.NextOffset != int64(len(complete)) {
		t.Fatalf("first read = %+v", first)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	second, err := ReadFrom(path, first.NextOffset)
	if err != nil || len(second.Entries) != 1 || second.Entries[0].Line != 2 {
		t.Fatalf("second read = %+v, %v", second, err)
	}
}

func TestToolOutputCapAndUTF8Boundary(t *testing.T) {
	t.Parallel()
	output := strings.Repeat("v", maxToolOutputBytes+731)
	line := `{"timestamp":"2026-01-02T03:04:05Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_invented","output":` + mustString(output) + `}}` + "\n"
	result, err := ReadFrom(writeTemp(t, line), 0)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Content string `json:"content"`
		Total   int    `json:"total_bytes"`
		Cut     bool   `json:"truncated"`
	}
	if err := json.Unmarshal(result.Entries[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Content) != maxToolOutputBytes || payload.Total != len(output) || !payload.Cut {
		t.Fatalf("cap payload = %+v content=%d", payload, len(payload.Content))
	}
	split := strings.Repeat("v", maxToolOutputBytes-1) + "€tail"
	capped, cut := capText(split, maxToolOutputBytes)
	if !cut || len(capped) != maxToolOutputBytes-1 {
		t.Fatalf("UTF-8 cap = %d, %v", len(capped), cut)
	}
}

func TestCustomToolExactExitWrapperMarksError(t *testing.T) {
	t.Parallel()
	payload := normalizeToolResult("call_invented", json.RawMessage(`[{"type":"input_text","text":"Script completed\nOutput:\n"},{"type":"input_text","text":"Exit code: 7\nOutput:\ninvented failure"}]`), true)
	assertPayloadField(t, payload, "is_error", true)
	payload = normalizeToolResult("call_invented", json.RawMessage(`[{"type":"input_text","text":"Script completed\nOutput:\nindigo"}]`), true)
	assertPayloadField(t, payload, "is_error", false)
}

func TestLongRunSchemaVariants(t *testing.T) {
	t.Parallel()
	content := `{"timestamp":"2026-01-02T03:04:05Z","type":"event_msg","payload":{"type":"task_complete","duration_ms":731,"completed_at":1780000000,"last_agent_message":"Invented final."}}` + "\n" +
		`{"timestamp":"2026-01-02T03:04:06Z","type":"response_item","payload":{"type":"tool_search_call","call_id":"call_invented","status":"completed","execution":"client","arguments":{"query":"invented","limit":8}}}` + "\n"
	result, err := ReadFrom(writeTemp(t, content), 0)
	if err != nil || len(result.Entries) != 2 || result.Entries[0].Kind != KindTurnDuration || result.Entries[1].Kind != KindToolUse {
		t.Fatalf("long-run variants = %+v, %v", result, err)
	}
}

func TestTailResetsAndReads(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, `{"timestamp":"2026-01-02T03:04:05Z","type":"event_msg","payload":{"type":"user_message","message":"invented"}}`+"\n")
	changed, err := Tail(path, "invented-new", Cursor{SessionID: "invented-old", Offset: 7})
	if err != nil || changed.Reset == nil || changed.Reset.Reason != ResetSessionChanged {
		t.Fatalf("session change = %+v, %v", changed, err)
	}
	truncated, err := Tail(path, "invented-new", Cursor{SessionID: "invented-new", Offset: 731})
	if err != nil || truncated.Reset == nil || truncated.Reset.Reason != ResetTruncated {
		t.Fatalf("truncation = %+v, %v", truncated, err)
	}
	fresh, err := TailWindow(path, "invented-new", Cursor{}, 1)
	if err != nil || fresh.Reset != nil || len(fresh.Read.Entries) != 1 || fresh.Cursor.Offset == 0 {
		t.Fatalf("fresh tail = %+v, %v", fresh, err)
	}
}

func assertPayloadField(t *testing.T, raw json.RawMessage, key string, want any) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload[key], want) {
		t.Fatalf("payload[%q] = %#v, want %#v", key, payload[key], want)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invented-rollout.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustString(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
