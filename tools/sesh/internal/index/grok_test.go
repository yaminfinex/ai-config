package index

// Grok index semantics: what a message row is for a grok chat_history line,
// and how a rewind-driven rewrite (the store's generation path) lands in the
// index. Grok lines carry no message uuid and no timestamp, so the frozen
// schema's empty-uuid rules apply: no dedup, no overlap unification, logical
// session = wire session id, recency = first-ingest.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"sesh/internal/store"
	"sesh/internal/wire"
)

const grokSID = "71ebdd45-2641-49e8-87f5-b8d9f3706714"

// putGrok PUTs raw bytes for the grok fixture identity and returns the
// recorder plus any published append event.
func putGrok(t *testing.T, st *store.Store, offset int64, body []byte) (*httptest.ResponseRecorder, *wire.AppendEvent) {
	t.Helper()
	url := "/v1/files/grok/" + grokSID + "/" + grokSID + "/bytes?offset=" + strconv.FormatInt(offset, 10)
	req := httptest.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	select {
	case ev := <-st.AppendEvents():
		return rr, &ev
	default:
		return rr, nil
	}
}

func grokRows(t *testing.T, idx *Indexer) []wire.IndexMessage {
	t.Helper()
	rows, err := idx.queryContext(context.Background(), `SELECT entry_type, message_uuid, role, timestamp_utc, logical_session_id, generation, file_ordinal, line_ordinal, quarantine
		FROM sesh_index_messages WHERE tool = 'grok' ORDER BY file_ordinal, line_ordinal`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []wire.IndexMessage
	for rows.Next() {
		var r wire.IndexMessage
		var ts *string
		var quarantine int
		if err := rows.Scan(&r.EntryType, &r.MessageUUID, &r.Role, &ts, &r.LogicalSessionID, &r.Generation, &r.FileOrdinal, &r.LineOrdinal, &quarantine); err != nil {
			t.Fatal(err)
		}
		if ts != nil {
			t.Fatalf("grok row parsed a timestamp %q; chat_history lines carry none", *ts)
		}
		r.Quarantine = quarantine != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGrokLinesIndexWithDerivedRoles(t *testing.T) {
	st, idx := newHarness(t)
	raw, err := os.ReadFile(fixture("grok-chat-history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rr, ev := putGrok(t, st, 0, raw)
	if rr.Code != http.StatusOK || ev == nil {
		t.Fatalf("grok PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := idx.ProcessAppend(t.Context(), *ev); err != nil {
		t.Fatal(err)
	}

	rows := grokRows(t, idx)
	if len(rows) != bytes.Count(raw, []byte("\n")) {
		t.Fatalf("indexed %d rows, want one per complete line (%d)", len(rows), bytes.Count(raw, []byte("\n")))
	}
	wantRole := map[string]string{
		"system": "system", "user": "user", "assistant": "assistant",
		"reasoning": "assistant", "tool_result": "tool",
	}
	seen := map[string]int{}
	for _, r := range rows {
		if r.Quarantine {
			t.Fatalf("fixture line quarantined: %+v", r)
		}
		if r.MessageUUID != "" {
			t.Fatalf("grok row has a message uuid %q; lines carry none and empty uuids never dedupe", r.MessageUUID)
		}
		if r.LogicalSessionID != grokSID {
			t.Fatalf("logical session %q, want wire claim fallback %q", r.LogicalSessionID, grokSID)
		}
		want, ok := wantRole[r.EntryType]
		if !ok {
			t.Fatalf("unexpected entry type %q", r.EntryType)
		}
		if r.Role != want {
			t.Fatalf("entry type %q got role %q, want %q", r.EntryType, r.Role, want)
		}
		seen[r.EntryType]++
	}
	// The fixture carries the full observed live spread.
	for _, et := range []string{"system", "user", "assistant", "reasoning", "tool_result"} {
		if seen[et] == 0 {
			t.Fatalf("fixture indexed no %q rows; type spread lost", et)
		}
	}
}

// TestGrokRewindLandsAsNewGenerationBothIndexed pins the rewrite story: a
// grok rewind truncates and rewrites chat_history.jsonl, the shipper re-ships
// from zero, and the store's conflict handshake opens a new generation. Both
// histories index under the same logical session; with no message uuids
// nothing dedupes, so the transcript shows generation 0 in full and then the
// rewritten history — the documented degraded floor for uuid-less transcripts.
func TestGrokRewindLandsAsNewGenerationBothIndexed(t *testing.T) {
	st, idx := newHarness(t)
	line := func(role, text string) []byte {
		b, _ := json.Marshal(map[string]string{"type": role, "content": text})
		return append(b, '\n')
	}
	prefix := append(line("system", "rules"), line("user", "first question")...)
	gen0 := append(append([]byte{}, prefix...), line("assistant", "first answer")...)
	// Rewind: same prefix, divergent continuation.
	gen1 := append(append([]byte{}, prefix...), line("assistant", "second answer after rewind")...)

	rr, ev := putGrok(t, st, 0, gen0)
	if rr.Code != http.StatusOK || ev == nil {
		t.Fatalf("gen0 PUT status=%d", rr.Code)
	}
	if err := idx.ProcessAppend(t.Context(), *ev); err != nil {
		t.Fatal(err)
	}

	// The shipper's post-truncation re-ship from zero: first divergence is
	// byte_conflict, the retry opens a fresh generation, the third PUT ships
	// the rewritten history into it (the frozen handshake).
	if rr, _ := putGrok(t, st, 0, gen1); rr.Code != http.StatusConflict {
		t.Fatalf("first divergent PUT status=%d, want 409 byte_conflict", rr.Code)
	}
	rr, _ = putGrok(t, st, 0, gen1)
	if rr.Code != http.StatusConflict {
		t.Fatalf("second divergent PUT status=%d, want 409 generation_opened", rr.Code)
	}
	var opened wire.ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &opened); err != nil || opened.Code != wire.ErrGenerationOpened {
		t.Fatalf("second divergence: %s (%v)", rr.Body.String(), err)
	}
	rr, ev = putGrok(t, st, 0, gen1)
	if rr.Code != http.StatusOK || ev == nil {
		t.Fatalf("gen1 PUT status=%d", rr.Code)
	}
	if ev.Generation != opened.Generation {
		t.Fatalf("append landed in generation %d, want opened %d", ev.Generation, opened.Generation)
	}
	if err := idx.ProcessAppend(t.Context(), *ev); err != nil {
		t.Fatal(err)
	}

	rows := grokRows(t, idx)
	if len(rows) != 6 {
		t.Fatalf("indexed %d rows, want all 3 lines of both generations", len(rows))
	}
	byGen := map[int]int{}
	ordinals := map[int]int64{}
	for _, r := range rows {
		byGen[r.Generation]++
		ordinals[r.Generation] = r.FileOrdinal
		if r.LogicalSessionID != grokSID {
			t.Fatalf("generation %d row left the logical session: %q", r.Generation, r.LogicalSessionID)
		}
	}
	if byGen[0] != 3 || byGen[opened.Generation] != 3 {
		t.Fatalf("rows per generation = %v, want 3 and 3", byGen)
	}
	if ordinals[0] >= ordinals[opened.Generation] {
		t.Fatalf("file ordinals %v must order generation 0 before the rewind generation", ordinals)
	}
}
