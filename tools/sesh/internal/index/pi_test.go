package index

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"sesh/internal/store"
	"sesh/internal/wire"
)

const piSID = "019f64a0-1111-7222-8333-444444444444"

func putPi(t *testing.T, st *store.Store, offset int64, body []byte) (*httptest.ResponseRecorder, *wire.AppendEvent) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/files/pi/"+piSID+"/"+piSID+"/bytes?offset="+strconv.FormatInt(offset, 10), bytes.NewReader(body))
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

func TestPiSpecificParserIndexesHeaderAndTreeEntries(t *testing.T) {
	st, idx := newHarness(t)
	raw, err := os.ReadFile(fixture("pi-branched-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rr, ev := putPi(t, st, 0, raw)
	if rr.Code != http.StatusOK || ev == nil {
		t.Fatalf("pi PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := idx.ProcessAppend(t.Context(), *ev); err != nil {
		t.Fatal(err)
	}
	rows, err := idx.queryContext(context.Background(), `SELECT entry_type, message_uuid, role, logical_session_id, quarantine FROM sesh_index_messages WHERE tool = 'pi' ORDER BY line_ordinal`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	seenActive := false
	for rows.Next() {
		var typ, id, role, logical string
		var quarantine int
		if err := rows.Scan(&typ, &id, &role, &logical, &quarantine); err != nil {
			t.Fatal(err)
		}
		if quarantine != 0 || logical != piSID {
			t.Fatalf("pi row typ=%s logical=%q quarantine=%d", typ, logical, quarantine)
		}
		if typ == "session" && id != "" {
			t.Fatalf("header id entered message_uuid dedup domain: %q", id)
		}
		if typ == "message" && id == "active02" {
			seenActive = true
			if role != "assistant" {
				t.Fatalf("nested pi role=%q", role)
			}
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != bytes.Count(raw, []byte("\n")) || !seenActive {
		t.Fatalf("pi indexed rows=%d active=%v", count, seenActive)
	}
}

func TestPiEmptyEntryIDDoesNotParticipate(t *testing.T) {
	parsed, err := parseToolLine(wire.ToolPi, []byte(`{"type":"message","id":"","parentId":null,"message":{"role":"user","content":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.MessageUUID != "" || parsed.Role != "user" {
		t.Fatalf("empty-id pi parse = %+v", parsed)
	}
}
