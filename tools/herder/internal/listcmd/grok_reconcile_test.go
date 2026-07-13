package listcmd

import (
	"encoding/json"
	"testing"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/observerstatus"
	"ai-config/tools/herder/internal/registry"
)

func TestGrokEventEnrichmentPreservesUnknownLiveStatusEndToEnd(t *testing.T) {
	guid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	terminalID := "term-neutral"
	rec := registry.Record{
		GUID:       &guid,
		Agent:      "grok",
		TerminalID: terminalID,
		Raw:        json.RawMessage(`{"kind":"session","guid":"` + guid + `","tool":"grok","state":"seated","seat":{"kind":"herdr","terminal_id":"term-neutral"}}`),
	}
	live := herdrcli.Agent{
		TerminalID: &terminalID,
		Agent:      "grok",
		Status:     "working",
		Raw:        json.RawMessage(`{"terminal_id":"term-neutral","agent":"grok","agent_status":"working"}`),
	}
	idx := liveIndex{
		byTerm:    map[string]*herdrcli.Agent{terminalID: &live},
		byPane:    map[string]*herdrcli.Agent{},
		byName:    map[string]*herdrcli.Agent{},
		paneTerms: map[string]bool{},
		panePanes: map[string]bool{},
	}
	observation := observerstatus.Observation{
		TranscriptPath:    "/state/grok-home/sessions/%2Fworkspace/session/chat_history.jsonl",
		TranscriptSource:  "grok-chat-history",
		TranscriptEntries: 3,
		EventStatus:       "tool_execution",
		EventSource:       "grok-events",
	}

	out := reconciledJSON(rec, idx, nil, observation)
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["live_status"] != "unknown" {
		t.Fatalf("live_status = %v, want unknown preserved", decoded["live_status"])
	}
	enrichment, ok := decoded["observer_enrichment"].(map[string]any)
	if !ok || enrichment["event_status"] != "tool_execution" || enrichment["event_source"] != "grok-events" {
		t.Fatalf("observer_enrichment = %#v, want separately labeled Grok event status", decoded["observer_enrichment"])
	}
}
