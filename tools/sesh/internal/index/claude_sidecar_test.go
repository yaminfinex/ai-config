package index

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"sesh/internal/wire"
)

const sidecarSessionID = "10000000-0000-0000-0000-000000000000"

var claudeMetadataTypes = []string{
	"agent-name",
	"ai-title",
	"bridge-session",
	"file-history-snapshot",
	"last-prompt",
	"mode",
	"permission-mode",
	"pr-link",
	"queue-operation",
	"worktree-state",
}

var claudeUnknownVisibleTypes = []string{
	"fork-context-ref",
	"result",
	"started",
	"future-sidecar-probe",
}

func TestClaudeSidecarFixturePremiseAndClassification(t *testing.T) {
	raw, err := os.Open(fixture("claude-sidecar-entry-types.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	wantTypes := map[string]bool{"user": true, "assistant": true}
	for _, typ := range append(append([]string{}, claudeMetadataTypes...), claudeUnknownVisibleTypes...) {
		wantTypes[typ] = true
	}
	scanner := bufio.NewScanner(raw)
	for scanner.Scan() {
		var line struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("fixture line is not valid JSON: %v", err)
		}
		delete(wantTypes, line.Type)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(wantTypes) != 0 {
		t.Fatalf("fixture premise missing types: %v", wantTypes)
	}

	st, idx := newHarness(t)
	processFixture(t, st, idx, sidecarSessionID, sidecarSessionID, "claude-sidecar-entry-types.jsonl")

	for _, typ := range claudeMetadataTypes {
		var role, messageUUID string
		if err := st.DB().QueryRow(`SELECT role, message_uuid FROM sesh_index_messages
			WHERE tool = ? AND entry_type = ?`, wire.ToolClaude, typ).Scan(&role, &messageUUID); err != nil {
			t.Fatalf("query %s: %v", typ, err)
		}
		if role != "meta" || messageUUID != "" {
			t.Errorf("sidecar %s classified role=%q uuid=%q, want meta with empty uuid", typ, role, messageUUID)
		}
	}

	for _, typ := range claudeUnknownVisibleTypes {
		var role string
		if err := st.DB().QueryRow(`SELECT role FROM sesh_index_messages
			WHERE tool = ? AND entry_type = ?`, wire.ToolClaude, typ).Scan(&role); err != nil {
			t.Fatalf("query %s: %v", typ, err)
		}
		if role != "unknown" {
			t.Errorf("non-admitted type %s role=%q, want degraded-visible unknown", typ, role)
		}
	}
}

func TestClaudeSidecarsIncrementalMatchReindexInBothArrivalOrders(t *testing.T) {
	raw, err := os.ReadFile(fixture("claude-sidecar-entry-types.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	files := []string{
		"10000000-0000-0000-0000-000000000010",
		"10000000-0000-0000-0000-000000000020",
	}
	for _, first := range files {
		first := first
		second := files[0]
		if first == files[0] {
			second = files[1]
		}
		t.Run(first+" arrives first", func(t *testing.T) {
			st, idx := newHarness(t)
			for _, fileUUID := range []string{first, second} {
				putBytes(t, st, sidecarSessionID, fileUUID, 0, raw)
				if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
					t.Fatal(err)
				}
			}
			assertChecksumMatchesReindex(t, idx)
			assertReindexFixedPoint(t, idx)
		})
	}
}
