package index

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"sesh/internal/store"
	"sesh/internal/wire"
)

const (
	origID = "2c387aef-72ac-46bc-8ea5-e3b68690a937"
	newID  = "e1be75ad-151b-47fa-9d69-46de1c117843"
)

func fixture(name string) string {
	return filepath.Join("..", "..", "tests", "fixtures", name)
}

func newHarness(t *testing.T) (*store.Store, *Indexer) {
	t.Helper()
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir(), AppendBuffer: 32})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	})
	idx, err := New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	return st, idx
}

func putFixture(t *testing.T, st *store.Store, sessionID, fileUUID, name string) wire.AppendEvent {
	t.Helper()
	raw, err := os.ReadFile(fixture(name))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/"+sessionID+"/"+fileUUID+"/bytes?offset=0", bytes.NewReader(raw))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT fixture %s status=%d body=%s", name, rr.Code, rr.Body.String())
	}
	select {
	case ev := <-st.AppendEvents():
		return ev
	default:
		t.Fatal("missing append event")
	}
	return wire.AppendEvent{}
}

func processFixture(t *testing.T, st *store.Store, idx *Indexer, sessionID, fileUUID, name string) {
	t.Helper()
	if err := idx.ProcessAppend(t.Context(), putFixture(t, st, sessionID, fileUUID, name)); err != nil {
		t.Fatal(err)
	}
}

func TestResumePairUnifiesByOverlapAndDedupesMessageUUIDs(t *testing.T) {
	st, idx := newHarness(t)
	processFixture(t, st, idx, origID, origID, "claude-resume-original.jsonl")
	processFixture(t, st, idx, newID, newID, "claude-resume-new-file.jsonl")

	var sessions int
	if err := st.DB().QueryRow(`SELECT COUNT(DISTINCT logical_session_id) FROM sesh_index_messages WHERE quarantine = 0`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("logical sessions = %d, want 1", sessions)
	}
	var dupes int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM (
		SELECT entry_type, message_uuid, COUNT(*) n FROM sesh_index_messages
		WHERE quarantine = 0 AND message_uuid <> ''
		GROUP BY logical_session_id, entry_type, message_uuid HAVING n > 1
	)`).Scan(&dupes); err != nil {
		t.Fatal(err)
	}
	if dupes != 0 {
		t.Fatalf("duplicate message uuids after overlap unification = %d", dupes)
	}
	var canonical string
	if err := st.DB().QueryRow(`SELECT logical_session_id FROM sesh_index_messages WHERE file_uuid = ? LIMIT 1`, newID).Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	if canonical != origID {
		t.Fatalf("canonical logical_session_id = %q want earliest original %q", canonical, origID)
	}
	gotOrdinals := map[string]int{}
	rows, err := st.DB().Query(`SELECT file_uuid, MIN(file_ordinal) FROM sesh_index_messages WHERE quarantine = 0 GROUP BY file_uuid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var fileUUID string
		var ordinal int
		if err := rows.Scan(&fileUUID, &ordinal); err != nil {
			t.Fatal(err)
		}
		gotOrdinals[fileUUID] = ordinal
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if gotOrdinals[origID] != 0 || gotOrdinals[newID] != 1 {
		t.Fatalf("resume file ordinals = %+v, want original=0 new=1", gotOrdinals)
	}
}

func TestGenerationAbsentFromDedupKey(t *testing.T) {
	st, idx := newHarness(t)
	processFixture(t, st, idx, origID, origID, "claude-resume-original.jsonl")
	raw, err := os.ReadFile(fixture("claude-resume-original.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	gen1 := wire.AppendEvent{Tool: wire.ToolClaude, WireSessionID: origID, FileUUID: origID, Generation: 1, ByteStart: 0, ByteEnd: int64(len(raw))}
	if err := copyFile(st.MirrorPath(wire.ToolClaude, origID, origID, 1), fixture("claude-resume-original.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO files(tool, session_id, file_uuid, generation, high_water, created_at, updated_at) VALUES (?, ?, ?, 1, ?, 'later', 'later')`,
		wire.ToolClaude, origID, origID, len(raw)); err != nil {
		t.Fatal(err)
	}
	if err := idx.ProcessAppend(t.Context(), gen1); err != nil {
		t.Fatal(err)
	}
	var dupes int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM (
		SELECT logical_session_id, entry_type, message_uuid, COUNT(*) n FROM sesh_index_messages
		WHERE quarantine = 0 AND message_uuid <> ''
		GROUP BY logical_session_id, entry_type, message_uuid HAVING n > 1
	)`).Scan(&dupes); err != nil {
		t.Fatal(err)
	}
	if dupes != 0 {
		t.Fatalf("generation leaked into dedup key; duplicate count = %d", dupes)
	}
}

func TestEntryTypeIsPartOfDedupAndOverlapKey(t *testing.T) {
	st, idx := newHarness(t)
	a := []byte(`{"type":"user","uuid":"same","sessionId":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}` + "\n")
	b := []byte(`{"type":"file-history-snapshot","uuid":"same","sessionId":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"}` + "\n")
	putBytes(t, st, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 0, a)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	putBytes(t, st, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 0, b)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := st.DB().QueryRow(`SELECT COUNT(DISTINCT logical_session_id) FROM sesh_index_messages`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 2 {
		t.Fatalf("sessions merged across entry types, got %d", sessions)
	}
}

func TestExactlyOneOverlappingPairDoesNotUnify(t *testing.T) {
	st, idx := newHarness(t)
	aSession := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	bSession := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	a := []byte(
		`{"type":"user","uuid":"shared","sessionId":"` + aSession + `"}` + "\n" +
			`{"type":"assistant","uuid":"a-only","sessionId":"` + aSession + `"}` + "\n")
	b := []byte(
		`{"type":"user","uuid":"shared","sessionId":"` + bSession + `"}` + "\n" +
			`{"type":"assistant","uuid":"b-only","sessionId":"` + bSession + `"}` + "\n")
	putBytes(t, st, aSession, aSession, 0, a)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	putBytes(t, st, bSession, bSession, 0, b)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := st.DB().QueryRow(`SELECT COUNT(DISTINCT logical_session_id) FROM sesh_index_messages WHERE quarantine = 0`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 2 {
		t.Fatalf("one overlapping pair unified sessions, got %d sessions", sessions)
	}
}

func TestFileHistorySnapshotPairDoesNotMergeUnrelatedSessions(t *testing.T) {
	st, idx := newHarness(t)
	aSession := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	bSession := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	a := []byte(`{"type":"file-history-snapshot","uuid":"shared-snapshot","sessionId":"` + aSession + `"}` + "\n")
	b := []byte(`{"type":"file-history-snapshot","uuid":"shared-snapshot","sessionId":"` + bSession + `"}` + "\n")
	putBytes(t, st, aSession, aSession, 0, a)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	putBytes(t, st, bSession, bSession, 0, b)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := st.DB().QueryRow(`SELECT COUNT(DISTINCT logical_session_id) FROM sesh_index_messages WHERE quarantine = 0`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 2 {
		t.Fatalf("file-history-snapshot pair merged unrelated sessions, got %d sessions", sessions)
	}
}

func TestTrailingPartialHeldBackUntilCompleted(t *testing.T) {
	st, idx := newHarness(t)
	full, err := os.ReadFile(fixture("claude-normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	partial, err := os.ReadFile(fixture("claude-trailing-partial.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	heldStart := int64(bytes.LastIndexByte(partial, '\n') + 1)
	heldEnd := int64(bytes.IndexByte(full[heldStart:], '\n')) + heldStart + 1
	processFixture(t, st, idx, "45308169-72e6-4cbe-a05c-2a0025db055e", "45308169-72e6-4cbe-a05c-2a0025db055e", "claude-trailing-partial.jsonl")
	var rowsBefore int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sesh_index_messages`).Scan(&rowsBefore); err != nil {
		t.Fatal(err)
	}
	var heldRows int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sesh_index_messages WHERE byte_start = ?`, heldStart).Scan(&heldRows); err != nil {
		t.Fatal(err)
	}
	if heldRows != 0 {
		t.Fatalf("held-back partial line was indexed early at byte_start=%d", heldStart)
	}
	putBytes(t, st, "45308169-72e6-4cbe-a05c-2a0025db055e", "45308169-72e6-4cbe-a05c-2a0025db055e", int64(len(partial)), full[len(partial):])
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	var rowsAfter int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sesh_index_messages`).Scan(&rowsAfter); err != nil {
		t.Fatal(err)
	}
	if rowsAfter <= rowsBefore {
		t.Fatalf("completed partial did not add rows: before=%d after=%d", rowsBefore, rowsAfter)
	}
	var gotStart, gotEnd int64
	if err := st.DB().QueryRow(`SELECT byte_start, byte_end FROM sesh_index_messages WHERE byte_start = ?`, heldStart).Scan(&gotStart, &gotEnd); err != nil {
		t.Fatal(err)
	}
	if gotStart != heldStart || gotEnd != heldEnd {
		t.Fatalf("completed held-back line span = %d-%d, want %d-%d", gotStart, gotEnd, heldStart, heldEnd)
	}
}

func TestUnparseableJSONQuarantinesAndCounts(t *testing.T) {
	st, idx := newHarness(t)
	body := []byte("[]\n{\"type\":\"user\",\"uuid\":\"ok\",\"sessionId\":\"11111111-1111-1111-1111-111111111111\"}\n")
	putBytes(t, st, testSession, testFile, 0, body)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	counts, err := idx.QuarantineCounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].Count != 1 {
		t.Fatalf("quarantine counts = %+v", counts)
	}
	var rows int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sesh_index_messages WHERE quarantine = 0`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("good line after quarantine not indexed, rows=%d", rows)
	}
}

func TestQuarantineObservedAtSurvivesReindex(t *testing.T) {
	st, idx := newHarness(t)
	body := []byte("[]\nnot-json\n")
	putBytes(t, st, testSession, testFile, 0, body)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	original := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if _, err := st.DB().ExecContext(t.Context(), `UPDATE quarantine_ledger SET observed_at = ?, day = ? WHERE line_ordinal = 0`, formatTime(original), original.Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().ExecContext(t.Context(), `UPDATE quarantine_ledger SET observed_at = 'not-a-time', day = 'not-a-day' WHERE line_ordinal = 1`); err != nil {
		t.Fatal(err)
	}
	before, err := idx.QuarantineCounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("quarantine counts before reindex = %+v", before)
	}
	start := time.Now().UTC()
	if err := idx.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	end := time.Now().UTC()
	after, err := idx.QuarantineCounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(after) == 0 {
		t.Fatalf("quarantine counts after reindex = %+v", after)
	}
	rows, err := st.DB().QueryContext(t.Context(), `SELECT line_ordinal, observed_at FROM quarantine_ledger ORDER BY line_ordinal`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	observedByLine := map[int64]string{}
	for rows.Next() {
		var line int64
		var observed string
		if err := rows.Scan(&line, &observed); err != nil {
			t.Fatal(err)
		}
		observedByLine[line] = observed
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if observedByLine[0] != formatTime(original) {
		t.Fatalf("healthy observed_at after reindex = %q want %q", observedByLine[0], formatTime(original))
	}
	regenerated, err := time.Parse(time.RFC3339Nano, observedByLine[1])
	if err != nil {
		t.Fatalf("corrupt observed_at was not regenerated as a timestamp: %q", observedByLine[1])
	}
	if regenerated.Before(start) || regenerated.After(end) {
		t.Fatalf("regenerated observed_at = %s, want between %s and %s", regenerated, start, end)
	}
}

func TestReindexReproducesChecksumAndHealsDirtyFlag(t *testing.T) {
	st, idx := newHarness(t)
	processFixture(t, st, idx, origID, origID, "claude-resume-original.jsonl")
	beforeSum, beforeRows, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	idx.InjectWriteFailureOnce()
	if err := idx.ProcessAppend(t.Context(), putFixture(t, st, newID, newID, "claude-resume-new-file.jsonl")); err == nil {
		t.Fatal("expected injected index failure")
	}
	var dirty int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM files WHERE dirty_for_reindex = 1`).Scan(&dirty); err != nil {
		t.Fatal(err)
	}
	if dirty == 0 {
		t.Fatal("index failure did not mark dirty_for_reindex")
	}
	if err := idx.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	sum1, rows1, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	sum2, rows2, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if sum1 != sum2 || rows1 != rows2 {
		t.Fatalf("reindex not idempotent: %s/%d then %s/%d", sum1, rows1, sum2, rows2)
	}
	if rows1 <= beforeRows || sum1 == beforeSum {
		t.Fatalf("reindex did not include second file: before %s/%d after %s/%d", beforeSum, beforeRows, sum1, rows1)
	}
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM files WHERE dirty_for_reindex = 1`).Scan(&dirty); err != nil {
		t.Fatal(err)
	}
	if dirty != 0 {
		t.Fatalf("reindex did not heal dirty flags: %d", dirty)
	}
}

func TestIncrementalAppendMatchesReindexChecksum(t *testing.T) {
	st, idx := newHarness(t)
	for i := 0; i < 25; i++ {
		sessionID := syntheticUUID(10_000 + i)
		body := syntheticSessionBody(sessionID, fmt.Sprintf("unrelated-%02d", i), 6, time.Date(2026, 7, 9, 12, i, 0, 0, time.UTC))
		putBytes(t, st, sessionID, sessionID, 0, body)
		if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
			t.Fatal(err)
		}
	}

	origSession := syntheticUUID(20_000)
	resumeSession := syntheticUUID(20_001)
	origFile := syntheticUUID(21_000)
	resumeFile := syntheticUUID(21_001)
	orig := syntheticSessionBody(origSession, "resume-original", 8, time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC))
	resume := syntheticResumeBody(resumeSession, "resume-new", []string{"resume-original-02", "resume-original-03"}, 5, time.Date(2026, 7, 9, 14, 0, 0, 0, time.UTC))
	putBytes(t, st, origSession, origFile, 0, orig)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	putBytes(t, st, resumeSession, resumeFile, 0, resume)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}

	incrementalSum, incrementalRows, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	reindexedSum, reindexedRows, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if incrementalSum != reindexedSum || incrementalRows != reindexedRows {
		t.Fatalf("incremental checksum %s/%d does not match reindex %s/%d", incrementalSum, incrementalRows, reindexedSum, reindexedRows)
	}
}

func TestPostUnifyAppendStaysInCurrentLogicalSession(t *testing.T) {
	for _, tc := range []struct {
		name          string
		origSession   string
		resumeSession string
	}{
		{
			name:          "resume id sorts before canonical",
			origSession:   syntheticUUID(20_100),
			resumeSession: syntheticUUID(20_099),
		},
		{
			name:          "resume id sorts after canonical",
			origSession:   syntheticUUID(20_200),
			resumeSession: syntheticUUID(20_201),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, idx := newHarness(t)
			origFile := syntheticUUID(21_100)
			resumeFile := syntheticUUID(21_101)
			orig := syntheticSessionBody(tc.origSession, "post-unify-original", 6, time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC))
			resume := syntheticResumeBody(tc.resumeSession, "post-unify-resume", []string{"post-unify-original-02", "post-unify-original-03"}, 3, time.Date(2026, 7, 9, 14, 0, 0, 0, time.UTC))
			putBytes(t, st, tc.origSession, origFile, 0, orig)
			if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
				t.Fatal(err)
			}
			putBytes(t, st, tc.resumeSession, resumeFile, 0, resume)
			if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
				t.Fatal(err)
			}

			tail := []byte(`{"type":"message","uuid":"post-unify-tail","sessionId":"` + tc.resumeSession + `","timestamp":"2026-07-09T15:00:00Z","message":{"role":"user"}}` + "\n")
			putBytes(t, st, tc.resumeSession, resumeFile, int64(len(resume)), tail)
			if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
				t.Fatal(err)
			}

			assertOneLogicalSession(t, st, tc.origSession, []string{origFile, resumeFile})
			assertChecksumMatchesReindex(t, idx)
		})
	}
}

func TestPostUnifyAppendCanBridgeTransitiveResumeChain(t *testing.T) {
	st, idx := newHarness(t)
	aSession := syntheticUUID(20_300)
	bSession := syntheticUUID(20_299)
	cSession := syntheticUUID(20_301)
	aFile := syntheticUUID(21_300)
	bFile := syntheticUUID(21_301)
	cFile := syntheticUUID(21_302)

	aBody := syntheticSessionBody(aSession, "chain-a", 6, time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC))
	bBody := syntheticResumeBody(bSession, "chain-b", []string{"chain-a-02", "chain-a-03"}, 2, time.Date(2026, 7, 9, 14, 0, 0, 0, time.UTC))
	putBytes(t, st, aSession, aFile, 0, aBody)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	putBytes(t, st, bSession, bFile, 0, bBody)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}

	bridge := []byte(
		`{"type":"message","uuid":"chain-bridge-00","sessionId":"` + bSession + `","timestamp":"2026-07-09T15:00:00Z","message":{"role":"user"}}` + "\n" +
			`{"type":"message","uuid":"chain-bridge-01","sessionId":"` + bSession + `","timestamp":"2026-07-09T15:00:01Z","message":{"role":"assistant"}}` + "\n")
	putBytes(t, st, bSession, bFile, int64(len(bBody)), bridge)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}

	cBody := syntheticResumeBody(cSession, "chain-c", []string{"chain-bridge-00", "chain-bridge-01"}, 2, time.Date(2026, 7, 9, 16, 0, 0, 0, time.UTC))
	putBytes(t, st, cSession, cFile, 0, cBody)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}

	assertOneLogicalSession(t, st, aSession, []string{aFile, bFile, cFile})
	assertChecksumMatchesReindex(t, idx)
}

func TestLineOrdinalComesFromBytePositionAfterDedupDeletion(t *testing.T) {
	st, idx := newHarness(t)
	body := []byte(
		`{"type":"user","uuid":"a","sessionId":"` + testSession + `"}` + "\n" +
			`{"type":"assistant","uuid":"b","sessionId":"` + testSession + `"}` + "\n")
	putBytes(t, st, testSession, testFile, 0, body)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`DELETE FROM sesh_index_messages WHERE message_uuid = 'b'`); err != nil {
		t.Fatal(err)
	}
	tail := []byte(`{"type":"user","uuid":"c","sessionId":"` + testSession + `"}` + "\n")
	putBytes(t, st, testSession, testFile, int64(len(body)), tail)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	var ordinal int
	if err := st.DB().QueryRow(`SELECT line_ordinal FROM sesh_index_messages WHERE message_uuid = 'c'`).Scan(&ordinal); err != nil {
		t.Fatal(err)
	}
	if ordinal != 2 {
		t.Fatalf("line ordinal after deleted tail row = %d, want byte-position ordinal 2", ordinal)
	}
}

func BenchmarkProcessAppendWithUnrelatedCorpus(b *testing.B) {
	for _, unrelated := range []int{0, 50, 200} {
		b.Run(fmt.Sprintf("unrelated_%d", unrelated), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				st, idx := newBenchHarness(b)
				for n := 0; n < unrelated; n++ {
					sessionID := syntheticUUID(30_000 + n)
					body := syntheticSessionBody(sessionID, fmt.Sprintf("bench-unrelated-%04d", n), 4, time.Date(2026, 7, 9, 15, n%60, 0, 0, time.UTC))
					putBytesBench(b, st, sessionID, sessionID, 0, body)
					if err := idx.ProcessAppend(b.Context(), <-st.AppendEvents()); err != nil {
						b.Fatal(err)
					}
				}
				targetSession := syntheticUUID(40_000 + i%1000)
				targetFile := syntheticUUID(41_000 + i%1000)
				body := syntheticSessionBody(targetSession, fmt.Sprintf("bench-target-%04d", i), 4, time.Date(2026, 7, 9, 16, 0, 0, 0, time.UTC))
				b.StartTimer()
				putBytesBench(b, st, targetSession, targetFile, 0, body)
				if err := idx.ProcessAppend(b.Context(), <-st.AppendEvents()); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				if err := st.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestReindexSkipsAndMarksMissingMirror(t *testing.T) {
	st, idx := newHarness(t)
	if _, err := st.DB().Exec(`INSERT INTO files(tool, session_id, file_uuid, generation, high_water, created_at, updated_at) VALUES (?, ?, ?, 0, 10, 'now', 'now')`,
		wire.ToolClaude, testSession, testFile); err != nil {
		t.Fatal(err)
	}
	shortFile := "33333333-3333-3333-3333-333333333333"
	shortPath := st.MirrorPath(wire.ToolClaude, testSession, shortFile, 0)
	if err := os.MkdirAll(filepath.Dir(shortPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shortPath, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO files(tool, session_id, file_uuid, generation, high_water, created_at, updated_at) VALUES (?, ?, ?, 0, 10, 'now', 'now')`,
		wire.ToolClaude, testSession, shortFile); err != nil {
		t.Fatal(err)
	}
	if err := idx.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	var dirty int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM files WHERE dirty_for_reindex = 1`).Scan(&dirty); err != nil {
		t.Fatal(err)
	}
	if dirty != 2 {
		t.Fatalf("missing/short mirror dirty count = %d, want 2", dirty)
	}
}

func TestOverlongLineQuarantinesAndContinues(t *testing.T) {
	st, idx := newHarness(t)
	body := append(bytes.Repeat([]byte("x"), maxIndexedLineBytes+1), '\n')
	body = append(body, []byte(`{"type":"user","uuid":"ok","sessionId":"`+testSession+`"}`+"\n")...)
	path := st.MirrorPath(wire.ToolClaude, testSession, testFile, 0)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO files(tool, session_id, file_uuid, generation, high_water, created_at, updated_at) VALUES (?, ?, ?, 0, ?, 'now', 'now')`,
		wire.ToolClaude, testSession, testFile, len(body)); err != nil {
		t.Fatal(err)
	}
	if err := idx.ProcessAppend(t.Context(), wire.AppendEvent{Tool: wire.ToolClaude, WireSessionID: testSession, FileUUID: testFile, Generation: 0, ByteEnd: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	var quarantined, good int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sesh_index_messages WHERE quarantine = 1 AND quarantine_reason = 'line_too_long'`).Scan(&quarantined); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sesh_index_messages WHERE quarantine = 0 AND message_uuid = 'ok'`).Scan(&good); err != nil {
		t.Fatal(err)
	}
	if quarantined != 1 || good != 1 {
		t.Fatalf("overlong quarantine/good rows = %d/%d, want 1/1", quarantined, good)
	}
}

func TestLargeTrailingPartialDoesNotAllocateWholeRange(t *testing.T) {
	st, idx := newHarness(t)
	const size = 30 << 20
	path := st.MirrorPath(wire.ToolClaude, testSession, testFile, 0)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(f, zeroReader{}, size); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO files(tool, session_id, file_uuid, generation, high_water, created_at, updated_at) VALUES (?, ?, ?, 0, ?, 'now', 'now')`,
		wire.ToolClaude, testSession, testFile, size); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := idx.ProcessAppend(t.Context(), wire.AppendEvent{Tool: wire.ToolClaude, WireSessionID: testSession, FileUUID: testFile, Generation: 0, ByteEnd: size}); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 12<<20 {
		t.Fatalf("ProcessAppend allocated %d bytes for a %d-byte trailing partial; want bounded under 12MiB", delta, size)
	}
	if got, err := idx.RowCount(t.Context()); err != nil {
		t.Fatal(err)
	} else if got != 0 {
		t.Fatalf("trailing partial indexed %d rows, want 0", got)
	}
}

const (
	testSession = "11111111-1111-1111-1111-111111111111"
	testFile    = "22222222-2222-2222-2222-222222222222"
)

func putBytes(t *testing.T, st *store.Store, sessionID, fileUUID string, offset int64, body []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/"+sessionID+"/"+fileUUID+"/bytes?offset="+strconv.FormatInt(offset, 10), bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	var ack wire.Ack
	if err := json.Unmarshal(rr.Body.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}
}

func putBytesBench(b *testing.B, st *store.Store, sessionID, fileUUID string, offset int64, body []byte) {
	b.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/"+sessionID+"/"+fileUUID+"/bytes?offset="+strconv.FormatInt(offset, 10), bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		b.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func newBenchHarness(b *testing.B) (*store.Store, *Indexer) {
	b.Helper()
	st, err := store.Open(b.Context(), store.Config{Dir: b.TempDir(), AppendBuffer: 32})
	if err != nil {
		b.Fatal(err)
	}
	idx, err := New(b.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		_ = st.Close()
		b.Fatal(err)
	}
	return st, idx
}

func syntheticUUID(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}

func syntheticSessionBody(sessionID, prefix string, count int, start time.Time) []byte {
	var buf bytes.Buffer
	for i := 0; i < count; i++ {
		fmt.Fprintf(&buf, `{"type":"message","uuid":"%s-%02d","sessionId":"%s","timestamp":"%s","message":{"role":"user"}}`+"\n",
			prefix, i, sessionID, start.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano))
	}
	return buf.Bytes()
}

func syntheticResumeBody(sessionID, prefix string, shared []string, count int, start time.Time) []byte {
	var buf bytes.Buffer
	for _, uuid := range shared {
		fmt.Fprintf(&buf, `{"type":"message","uuid":"%s","sessionId":"%s","timestamp":"%s","message":{"role":"user"}}`+"\n",
			uuid, sessionID, start.Format(time.RFC3339Nano))
	}
	for i := 0; i < count; i++ {
		fmt.Fprintf(&buf, `{"type":"message","uuid":"%s-%02d","sessionId":"%s","timestamp":"%s","message":{"role":"assistant"}}`+"\n",
			prefix, i, sessionID, start.Add(time.Duration(i+len(shared))*time.Second).Format(time.RFC3339Nano))
	}
	return buf.Bytes()
}

func assertOneLogicalSession(t *testing.T, st *store.Store, want string, files []string) {
	t.Helper()
	for _, fileUUID := range files {
		var count int
		var logical string
		if err := st.DB().QueryRow(`SELECT COUNT(DISTINCT logical_session_id), MIN(logical_session_id)
			FROM sesh_index_messages WHERE quarantine = 0 AND file_uuid = ?`, fileUUID).Scan(&count, &logical); err != nil {
			t.Fatal(err)
		}
		if count != 1 || logical != want {
			t.Fatalf("file %s logical sessions = %d/%q, want one %q", fileUUID, count, logical, want)
		}
	}
}

func assertChecksumMatchesReindex(t *testing.T, idx *Indexer) {
	t.Helper()
	incrementalSum, incrementalRows, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	reindexedSum, reindexedRows, err := idx.Checksum(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if incrementalSum != reindexedSum || incrementalRows != reindexedRows {
		t.Fatalf("incremental checksum %s/%d does not match reindex %s/%d", incrementalSum, incrementalRows, reindexedSum, reindexedRows)
	}
}

func copyFile(dst, src string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
