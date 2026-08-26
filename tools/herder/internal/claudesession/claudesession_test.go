package claudesession

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

func TestSlug(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"/home/agent/Coding/ai-config": "-home-agent-Coding-ai-config",
		"relative/work tree":           "-relative-work-tree",
		"/a_b.c":                       "-a-b-c",
	} {
		if got := Slug(input); got != want {
			t.Errorf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	id := "73100000-0000-4000-8000-000000000731"
	row := hcomidentity.Row{Tool: "claude", Directory: "/invented/violet", SessionID: id}
	want := filepath.Join(home, ".claude", "projects", "-invented-violet", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(home, row)
	if err != nil || got != want {
		t.Fatalf("Resolve() = %q, %v; want %q", got, err, want)
	}

	tests := []struct {
		name   string
		mutate func(*hcomidentity.Row)
		reason ResolveReason
	}{
		{"missing", func(r *hcomidentity.Row) { r.SessionID = "" }, ResolveMissingSession},
		{"wrong tool", func(r *hcomidentity.Row) { r.Tool = "codex" }, ResolveWrongTool},
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
}

func TestTaxonomyFixture(t *testing.T) {
	t.Parallel()
	result, err := ReadFrom(filepath.Join("testdata", "taxonomy.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []Kind{
		KindHumanPrompt, KindHcomStub, KindHcomDelivery, KindTaskNotification,
		KindInjectedSystem, KindCommandOutput, KindCompactDivider,
		KindAssistantText, KindThinking, KindToolUse, KindToolResult,
		KindToolResult, KindTurnDuration, KindCompactDivider, KindSystemChip,
		KindUnknown,
	}
	if len(result.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(result.Entries), len(want))
	}
	for i, kind := range want {
		if result.Entries[i].Kind != kind {
			t.Errorf("entry %d kind = %q, want %q", i, result.Entries[i].Kind, kind)
		}
		if result.Entries[i].Line != int64(i) {
			t.Errorf("entry %d line = %d", i, result.Entries[i].Line)
		}
	}
	if result.Entries[15].Kind != KindUnknown || !json.Valid(result.Entries[15].Payload) || !strings.Contains(string(result.Entries[15].Payload), "invented-message-future") {
		t.Fatal("future-sidecar-probe must remain visible with raw payload")
	}
	var hcomPayload struct {
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
	if err := json.Unmarshal(result.Entries[2].Payload, &hcomPayload); err != nil {
		t.Fatal(err)
	}
	if hcomPayload.Subtype != "hook_system_message" || len(hcomPayload.Deliveries) != 2 {
		t.Fatalf("hcom payload = %+v", hcomPayload)
	}
	first, second := hcomPayload.Deliveries[0], hcomPayload.Deliveries[1]
	if first.Intent != "inform" || first.Thread != "violet-grid" || first.MessageID != "731" || first.Sender != "rava" || first.Recipient != "agent-nori" || first.Text != "Inspect the invented violet fixture." {
		t.Fatalf("threaded delivery = %+v", first)
	}
	if second.Intent != "new message" || second.Thread != "" || second.MessageID != "732" || second.Text != "A second invented body." {
		t.Fatalf("new-message delivery = %+v", second)
	}
	assertJSONField(t, result.Entries[10].Payload, "is_error", true)

	var stringResult map[string]json.RawMessage
	if err := json.Unmarshal(result.Entries[10].Payload, &stringResult); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stringResult["toolUseResult"]), "structuredPatch") {
		t.Fatal("structuredPatch was not passed through")
	}
	var blockResult struct {
		ImageCount int `json:"image_count"`
		Content    []struct {
			Type    string `json:"type"`
			Present bool   `json:"present"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result.Entries[11].Payload, &blockResult); err != nil {
		t.Fatal(err)
	}
	if blockResult.ImageCount != 1 || len(blockResult.Content) != 2 || !blockResult.Content[1].Present {
		t.Fatalf("image result = %+v", blockResult)
	}
}

func TestBookkeepingAllowlistIsExact(t *testing.T) {
	t.Parallel()
	want := map[string]struct{}{
		"agent-name": {}, "ai-title": {}, "bridge-session": {},
		"file-history-delta": {}, "file-history-snapshot": {},
		"last-prompt": {}, "mode": {},
		"permission-mode": {}, "pr-link": {}, "queue-operation": {},
		"worktree-state": {},
	}
	if !reflect.DeepEqual(bookkeepingTypes, want) {
		t.Fatalf("bookkeeping allowlist mutated: got %#v want %#v", bookkeepingTypes, want)
	}
	for typ := range want {
		_, render, _ := classify([]byte(`{"type":"`+typ+`"}`), 0, 0)
		if render {
			t.Errorf("bookkeeping type %q rendered", typ)
		}
	}
	entry, render, _ := classify([]byte(`{"type":"future-sidecar-probe","invented":true}`), 0, 0)
	if !render || entry.Kind != KindUnknown {
		t.Fatal("unknown future type must stay visible")
	}
}

func TestQuarantineAndSidechain(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, "{invented corrupt\n"+
		`{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"invented"}]}}`+"\n")
	result, err := ReadFrom(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Quarantine == nil || result.Entries[0].Kind != KindUnknown {
		t.Fatalf("quarantined entries = %#v", result.Entries)
	}
	if result.Stats.SidechainSkipped != 1 {
		t.Fatalf("sidechain skipped = %d", result.Stats.SidechainSkipped)
	}
}

func TestPartialTrailingLineHeldThenEmittedOnce(t *testing.T) {
	t.Parallel()
	complete := `{"type":"assistant","uuid":"invented-one","message":{"content":[{"type":"text","text":"one"}]}}` + "\n"
	partial := `{"type":"assistant","uuid":"invented-two","message":{"content":[{"type":"text","text":"two"}]}}`
	path := writeTemp(t, complete+partial)
	first, err := ReadFrom(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 1 || first.NextOffset != int64(len(complete)) {
		t.Fatalf("first read = %+v", first)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := ReadFrom(path, first.NextOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 1 || second.Entries[0].UUID != "invented-two" || second.Entries[0].Line != 1 {
		t.Fatalf("second read = %+v", second)
	}
}

func TestToolOutputCapIsHonest(t *testing.T) {
	t.Parallel()
	if maxToolOutputBytes != 16384 {
		t.Fatalf("maxToolOutputBytes = %d, want literal 16384", maxToolOutputBytes)
	}
	output := strings.Repeat("v", 16384+731)
	line := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_invented_cap","is_error":false,"content":` + mustString(output) + `}]}}` + "\n"
	result, err := ReadFrom(writeTemp(t, line), 0)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Content    string `json:"content"`
		TotalBytes int    `json:"total_bytes"`
		Truncated  bool   `json:"truncated"`
	}
	if err := json.Unmarshal(result.Entries[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Content) != 16384 || payload.TotalBytes != len(output) || !payload.Truncated {
		t.Fatalf("cap payload = length %d total %d truncated %v", len(payload.Content), payload.TotalBytes, payload.Truncated)
	}
}

func TestCapTextOnlyBacksOffAtUTF8Boundary(t *testing.T) {
	t.Parallel()
	binaryish := []byte(strings.Repeat("v", 16384+8))
	binaryish[731] = 0xff
	capped, truncated := capText(string(binaryish), 16384)
	if !truncated || len(capped) != 16384 {
		t.Fatalf("invalid mid-slice output shrank to %d bytes", len(capped))
	}

	splitRune := strings.Repeat("v", 16383) + "€" + "tail"
	capped, truncated = capText(splitRune, 16384)
	if !truncated || len(capped) != 16383 {
		t.Fatalf("split UTF-8 boundary length = %d, want 16383", len(capped))
	}
}

func TestHookAttachmentFallbackDelivery(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"type":"attachment","attachment":{"type":"hook_additional_context","content":["Invented unheaded hook body."]}}`)
	entry, render, sidechain := classify(raw, 0, 0)
	if !render || sidechain || entry.Kind != KindHcomDelivery {
		t.Fatalf("fallback classification = %#v, render=%v sidechain=%v", entry, render, sidechain)
	}
	var payload struct {
		Subtype    string `json:"subtype"`
		Deliveries []struct {
			Text string `json:"text"`
		} `json:"deliveries"`
	}
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Subtype != "hook_additional_context" || len(payload.Deliveries) != 1 || payload.Deliveries[0].Text != "Invented unheaded hook body." {
		t.Fatalf("fallback payload = %+v", payload)
	}
}

func TestPairIsPure(t *testing.T) {
	t.Parallel()
	result, err := ReadFrom(filepath.Join("testdata", "taxonomy.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]Entry(nil), result.Entries...)
	pairs := Pair(result.Entries)
	if pairs[1].Related == nil || pairs[1].Related.Kind != KindHcomDelivery {
		t.Fatal("hcom stub was not paired with adjacent delivery")
	}
	if pairs[9].Related == nil || pairs[9].Related.Kind != KindToolResult {
		t.Fatal("tool use was not paired by tool_use_id")
	}
	if !reflect.DeepEqual(before, result.Entries) || len(pairs) != len(result.Entries) {
		t.Fatal("Pair mutated or folded the immutable stream")
	}
}

func TestTailResetsAndReads(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, `{"type":"system","subtype":"informational"}`+"\n")
	changed, err := Tail(path, "invented-new", Cursor{SessionID: "invented-old", Offset: 7})
	if err != nil || changed.Reset == nil || changed.Reset.Reason != ResetSessionChanged || len(changed.Read.Entries) != 0 {
		t.Fatalf("session change = %+v, %v", changed, err)
	}
	truncated, err := Tail(path, "invented-new", Cursor{SessionID: "invented-new", Offset: 731})
	if err != nil || truncated.Reset == nil || truncated.Reset.Reason != ResetTruncated || len(truncated.Read.Entries) != 0 {
		t.Fatalf("truncation = %+v, %v", truncated, err)
	}
	fresh, err := Tail(path, "invented-new", Cursor{})
	if err != nil || fresh.Reset != nil || len(fresh.Read.Entries) != 1 || fresh.Cursor.Offset == 0 {
		t.Fatalf("fresh tail = %+v, %v", fresh, err)
	}
}

func assertJSONField(t *testing.T, raw json.RawMessage, key string, want any) {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(object[key], want) {
		t.Fatalf("payload[%q] = %#v, want %#v", key, object[key], want)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invented-session.jsonl")
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
