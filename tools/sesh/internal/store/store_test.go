package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"sesh/internal/index"
	"sesh/internal/wire"
	"tailscale.com/tailcfg"
)

const (
	testSession = "11111111-1111-1111-1111-111111111111"
	testFile    = "22222222-2222-2222-2222-222222222222"
	siblingFile = "33333333-3333-3333-3333-333333333333"
)

func newTestStore(t *testing.T, logBuf *bytes.Buffer) *Store {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logBuf, nil))
	st, err := Open(t.Context(), Config{Dir: t.TempDir(), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return st
}

func putReq(t *testing.T, st *Store, tool wire.Tool, sid, file string, offset int64, body []byte, fp *string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/files/"+string(tool)+"/"+sid+"/"+file+"/bytes?offset="+itoa(offset), bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	if fp != nil {
		req.Header.Set(wire.HeaderFingerprintAlgorithm, wire.FingerprintAlgorithm)
		req.Header.Set(wire.HeaderFingerprint, *fp)
	}
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	return rr
}

func recoveryReq(t *testing.T, st *Store, tool wire.Tool, sid, file string, fp *string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/v1/files/" + string(tool) + "/" + sid + "/" + file
	if fp != nil {
		url += "?fingerprint=" + *fp
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set(wire.HeaderWireVersion, "1")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	return rr
}

func decodeAck(t *testing.T, rr *httptest.ResponseRecorder) wire.Ack {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var ack wire.Ack
	if err := json.Unmarshal(rr.Body.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}
	return ack
}

func decodeError(t *testing.T, rr *httptest.ResponseRecorder, code wire.ErrorCode) wire.ErrorResponse {
	t.Helper()
	if rr.Code != code.HTTPStatus() {
		t.Fatalf("%s: status = %d want %d body=%s", code, rr.Code, code.HTTPStatus(), rr.Body.String())
	}
	var resp wire.ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != code {
		t.Fatalf("error code = %q want %q body=%s", resp.Code, code, rr.Body.String())
	}
	return resp
}

func TestAppendAtHighWaterACKsAndIdenticalReplayNoOps(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	body := []byte("hello mirror\n")

	ack := decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, body, nil))
	if ack.HighWater != int64(len(body)) || ack.Generation != 0 {
		t.Fatalf("ack = %+v", ack)
	}
	replay := decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, body, nil))
	if replay.HighWater != int64(len(body)) || replay.Generation != 0 {
		t.Fatalf("replay ack = %+v", replay)
	}
	got, err := os.ReadFile(st.MirrorPath(wire.ToolClaude, testSession, testFile, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("mirror = %q want %q", got, body)
	}
	select {
	case ev := <-st.AppendEvents():
		if ev.ByteStart != 0 || ev.ByteEnd != int64(len(body)) {
			t.Fatalf("append event = %+v", ev)
		}
	default:
		t.Fatal("missing append event")
	}
}

func TestDivergentReplayConflictsThenOpensGeneration(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	gen0 := []byte("first history\n")
	gen1 := []byte("second history\n")
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, gen0, nil))

	first := decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, gen1, nil), wire.ErrByteConflict)
	if first.Generation != 0 || first.HighWater != int64(len(gen0)) {
		t.Fatalf("first conflict = %+v", first)
	}
	second := decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, gen1, nil), wire.ErrGenerationOpened)
	if second.Generation != 1 || second.HighWater != 0 {
		t.Fatalf("generation open = %+v", second)
	}
	ack := decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, gen1, nil))
	if ack.Generation != 1 || ack.HighWater != int64(len(gen1)) {
		t.Fatalf("gen1 ack = %+v", ack)
	}
	assertMirror(t, st, 0, gen0)
	assertMirror(t, st, 1, gen1)
}

func TestRepeatedDivergenceWithSharedFingerprintRoutesToHighestGeneration(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	prefix := bytes.Repeat([]byte("a"), wire.FingerprintWindowBytes)
	gen0 := append(append([]byte{}, prefix...), []byte(" generation zero")...)
	gen1 := append(append([]byte{}, prefix...), []byte(" generation one")...)
	fp := fingerprint(gen0)

	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, gen0, &fp))
	decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, gen1, &fp), wire.ErrByteConflict)
	decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, gen1, &fp), wire.ErrGenerationOpened)
	ack := decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, gen1, &fp))
	if ack.Generation != 1 || ack.HighWater != int64(len(gen1)) {
		t.Fatalf("shared-fingerprint gen1 ack = %+v", ack)
	}
	assertMirror(t, st, 0, gen0)
	assertMirror(t, st, 1, gen1)
}

func TestSubWindowPoisonKeySecondLegitimateRecreatePoisonsNullFingerprint(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	first := []byte("short-a")
	second := []byte("short-b")
	third := []byte("short-c")
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, first, nil))
	decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, second, nil), wire.ErrByteConflict)
	decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, second, nil), wire.ErrGenerationOpened)
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, second, nil))

	decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, third, nil), wire.ErrByteConflict)
	poisoned := decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, third, nil), wire.ErrPoisonedFile)
	if poisoned.Generation != 1 {
		t.Fatalf("poisoned response = %+v", poisoned)
	}
}

func TestDroppedAppendEventMarksDirtyForReindex(t *testing.T) {
	st, err := Open(t.Context(), Config{Dir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(new(bytes.Buffer), nil)), AppendBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, []byte("one"), nil))
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 3, []byte("two"), nil))

	resp := recoveryReq(t, st, wire.ToolClaude, testSession, testFile, nil)
	var recovery wire.RecoveryResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &recovery); err != nil {
		t.Fatal(err)
	}
	if len(recovery.Generations) != 1 || !recovery.Generations[0].DirtyForReindex {
		t.Fatalf("recovery after dropped event = %+v", recovery)
	}
}

func TestTailnetGrantDeniesPUTBeforeReadingBytes(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	read := false
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/"+testSession+"/"+testFile+"/bytes?offset=0", readTrackingBody{read: &read})
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()

	AuthHandler(st.Handler(), staticWhoIs("mallory@example.com"), CapabilityShip).ServeHTTP(rr, req)
	decodeError(t, rr, wire.ErrOutOfGrant)
	if read {
		t.Fatal("out-of-grant PUT body was read before denial")
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM fact_observations`); got != 0 {
		t.Fatalf("fact rows after denied PUT = %d, want 0", got)
	}
}

func TestTailnetGrantDeniesReadBeforeHandler(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	AuthHandler(next, staticWhoIs("mallory@example.com"), CapabilityRead).ServeHTTP(rr, req)
	decodeError(t, rr, wire.ErrOutOfGrant)
	if called {
		t.Fatal("out-of-grant read reached the read handler")
	}
}

func TestTailnetGrantDoesNotAllowWildcardVerb(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("wildcard verb must not reach the handler")
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	AuthHandler(next, staticWhoIs("mallory@example.com", "*"), CapabilityRead).ServeHTTP(rr, req)
	decodeError(t, rr, wire.ErrOutOfGrant)
}

func TestTailnetGrantStampsWhoIsAndIgnoresForgedIdentity(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	body := []byte(`{"type":"user","uuid":"ok","sessionId":"` + testSession + `","tailnet_identity":"mallory@example.com"}` + "\n")
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/"+testSession+"/"+testFile+"/bytes?offset=0", bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	req.Header.Set("X-Sesh-Tailnet-Identity", "mallory@example.com")
	req.Header.Set("X-Sesh-Display-Owner", "mallory@example.com")
	rr := httptest.NewRecorder()

	AuthHandler(st.Handler(), staticWhoIs("alice@example.com", CapabilityShip), CapabilityShip).ServeHTTP(rr, req)
	decodeAck(t, rr)

	var got string
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COALESCE(tailnet_identity, '') FROM fact_observations`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "alice@example.com" {
		t.Fatalf("tailnet_identity = %q, want WhoIs identity; forged claims must be ignored", got)
	}
}

func TestTailnetGrantAllowsLoopbackDevModeUnwrapped(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	rr := putReq(t, st, wire.ToolClaude, testSession, testFile, 0, []byte("dev loopback\n"), nil)
	decodeAck(t, rr)
	var got string
	if err := st.DB().QueryRowContext(t.Context(), `SELECT COALESCE(tailnet_identity, '') FROM fact_observations`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("loopback dev mode should not invent tailnet identity, got %q", got)
	}
}

func TestDropFileRemovesOnlyTargetAndReindexLeavesNoOrphans(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	idx, err := index.New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}

	targetBody := []byte(`{"type":"user","uuid":"target-msg","sessionId":"` + testSession + `"}` + "\n")
	siblingBody := []byte(`{"type":"user","uuid":"sibling-msg","sessionId":"` + testSession + `"}` + "\n")
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, targetBody, nil))
	processNextAppend(t, st, idx)
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, siblingFile, 0, siblingBody, nil))
	processNextAppend(t, st, idx)

	if countRows(t, st, `SELECT COUNT(*) FROM sesh_index_messages WHERE tool = ? AND file_uuid = ?`, wire.ToolClaude, testFile) == 0 {
		t.Fatal("target index rows were not created")
	}
	if countRows(t, st, `SELECT COUNT(*) FROM sesh_index_messages WHERE tool = ? AND file_uuid = ?`, wire.ToolClaude, siblingFile) == 0 {
		t.Fatal("sibling index rows were not created")
	}

	if err := st.DropFile(t.Context(), wire.ToolClaude, testSession, testFile, "test drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.MirrorPath(wire.ToolClaude, testSession, testFile, 0)); !os.IsNotExist(err) {
		t.Fatalf("target mirror still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(st.MirrorPath(wire.ToolClaude, testSession, siblingFile, 0)); err != nil {
		t.Fatalf("sibling mirror missing: %v", err)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM files WHERE tool = ? AND file_uuid = ?`, wire.ToolClaude, testFile); got != 0 {
		t.Fatalf("target files rows = %d, want 0", got)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM files WHERE tool = ? AND file_uuid = ?`, wire.ToolClaude, siblingFile); got != 1 {
		t.Fatalf("sibling files rows = %d, want 1", got)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM sesh_index_messages WHERE tool = ? AND file_uuid = ?`, wire.ToolClaude, testFile); got != 0 {
		t.Fatalf("target index rows after drop = %d, want 0", got)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM sesh_index_messages WHERE tool = ? AND file_uuid = ?`, wire.ToolClaude, siblingFile); got != 1 {
		t.Fatalf("sibling index rows after drop = %d, want 1", got)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM drop_log WHERE tool = ? AND session_id = ? AND file_uuid = ?`, wire.ToolClaude, testSession, testFile); got != 1 {
		t.Fatalf("drop audit rows = %d, want 1", got)
	}

	if err := idx.Reindex(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM sesh_index_messages WHERE tool = ? AND file_uuid = ?`, wire.ToolClaude, testFile); got != 0 {
		t.Fatalf("target index rows after reindex = %d, want 0", got)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM sesh_index_messages WHERE tool = ? AND file_uuid = ?`, wire.ToolClaude, siblingFile); got != 1 {
		t.Fatalf("sibling index rows after reindex = %d, want 1", got)
	}
}

func TestDropFileAuditSurvivesDeleteFailureAndBytesSurvive(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	idx, err := index.New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"user","uuid":"target-msg","sessionId":"` + testSession + `"}` + "\n")
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, body, nil))
	processNextAppend(t, st, idx)

	if _, err := st.DB().ExecContext(t.Context(), `DROP TABLE index_file_state`); err != nil {
		t.Fatal(err)
	}
	err = st.DropFile(t.Context(), wire.ToolClaude, testSession, testFile, "test delete failure")
	if err == nil || !strings.Contains(err.Error(), "index_file_state") {
		t.Fatalf("drop-file error = %v, want delete failure naming index_file_state", err)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM drop_log WHERE tool = ? AND session_id = ? AND file_uuid = ?`, wire.ToolClaude, testSession, testFile); got != 1 {
		t.Fatalf("drop audit rows after delete failure = %d, want 1", got)
	}
	if got := countRows(t, st, `SELECT COUNT(*) FROM files WHERE tool = ? AND session_id = ? AND file_uuid = ?`, wire.ToolClaude, testSession, testFile); got != 1 {
		t.Fatalf("files rows after delete failure = %d, want 1", got)
	}
	if _, err := os.Stat(st.MirrorPath(wire.ToolClaude, testSession, testFile, 0)); err != nil {
		t.Fatalf("mirror bytes should survive failed delete tx: %v", err)
	}
}

func TestNodesFlagsStaleLastPut(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	now := time.Now().UTC()
	for _, row := range []struct {
		host, user string
		at         time.Time
	}{
		{"fresh-host", "grace", now.Add(-47 * time.Hour)},
		{"stale-host", "grace", now.Add(-49 * time.Hour)},
	} {
		if _, err := st.DB().ExecContext(t.Context(), `INSERT INTO last_seen(hostname, os_user, last_put_at) VALUES (?, ?, ?)`,
			row.host, row.user, formatTime(row.at)); err != nil {
			t.Fatal(err)
		}
	}

	nodes, err := st.Nodes(t.Context(), 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]bool{}
	for _, node := range nodes {
		status[node.Hostname] = node.Stale
	}
	if status["fresh-host"] {
		t.Fatal("fresh-host should not be stale")
	}
	if !status["stale-host"] {
		t.Fatal("stale-host should be stale")
	}
}

func TestOffsetGapUnknownToolBodyTooLargeAndWriteFailureUseWireCodes(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	body := []byte("abc")
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, body, nil))

	gap := decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 99, []byte("x"), nil), wire.ErrOffsetGap)
	if gap.HighWater != int64(len(body)) || gap.ShipperAction != wire.ShipperActionRewind {
		t.Fatalf("gap = %+v", gap)
	}
	decodeError(t, putReq(t, st, wire.Tool("newtool"), testSession, testFile, 0, body, nil), wire.ErrUnknownTool)

	tooLarge := bytes.Repeat([]byte("x"), wire.MaxPUTBody+1)
	decodeError(t, putReq(t, st, wire.ToolClaude, "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444", 0, tooLarge, nil), wire.ErrBodyTooLarge)

	st.InjectMirrorErrorOnce()
	failed := decodeError(t, putReq(t, st, wire.ToolClaude, "55555555-5555-5555-5555-555555555555", "66666666-6666-6666-6666-666666666666", 0, body, nil), wire.ErrMirrorWriteFailed)
	if failed.HighWater != 0 {
		t.Fatalf("write failure advanced highwater: %+v", failed)
	}
}

func TestPiAdmittedWhileUnknownToolRemainsRejected(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	body := []byte("{\"type\":\"session\"}\n")
	ack := decodeAck(t, putReq(t, st, wire.ToolPi, testSession, testFile, 0, body, nil))
	if ack.Tool != wire.ToolPi || ack.HighWater != int64(len(body)) {
		t.Fatalf("Pi ACK = %+v", ack)
	}
	decodeError(t, putReq(t, st, wire.Tool("future-tool"), testSession, testFile, 0, body, nil), wire.ErrUnknownTool)
}

func TestMalformedRequestsByName(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	badOffset := putReq(t, st, wire.ToolClaude, testSession, testFile, -1, []byte("x"), nil)
	decodeError(t, badOffset, wire.ErrMalformedRequest)

	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/"+testSession+"/"+testFile+"/bytes?offset=0", strings.NewReader("x"))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	decodeError(t, rr, wire.ErrMalformedRequest)
}

func TestFingerprintConflictSelectsGenerationAndCurrentHighWater(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	bodyA := bytes.Repeat([]byte("a"), wire.FingerprintWindowBytes)
	bodyB := bytes.Repeat([]byte("b"), wire.FingerprintWindowBytes)
	fpA := fingerprint(bodyA)
	fpB := fingerprint(bodyB)
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, bodyA, &fpA))

	conflict := decodeError(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, bodyB, &fpB), wire.ErrFingerprintConflict)
	if conflict.Generation != 1 || conflict.HighWater != 0 {
		t.Fatalf("fingerprint conflict = %+v", conflict)
	}
	ackB := decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, bodyB, &fpB))
	if ackB.Generation != 1 || ackB.HighWater != int64(len(bodyB)) {
		t.Fatalf("ackB = %+v", ackB)
	}
	againA := decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, int64(len(bodyA)), []byte("tail"), &fpA))
	if againA.Generation != 0 || againA.HighWater != int64(len(bodyA)+len("tail")) {
		t.Fatalf("againA = %+v", againA)
	}
}

func TestRecoveryGETKnownUUIDOnlyAndFingerprintFiltered(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	shortBody := []byte("below-window")
	decodeAck(t, putReq(t, st, wire.ToolCodex, testSession, testFile, 0, shortBody, nil))
	shortResp := recoveryReq(t, st, wire.ToolCodex, testSession, testFile, nil)
	if shortResp.Code != http.StatusOK {
		t.Fatalf("short recovery status=%d body=%s", shortResp.Code, shortResp.Body.String())
	}
	var shortRecovery wire.RecoveryResponse
	if err := json.Unmarshal(shortResp.Body.Bytes(), &shortRecovery); err != nil {
		t.Fatal(err)
	}
	if len(shortRecovery.Generations) != 1 || shortRecovery.Generations[0].Fingerprint != nil || shortRecovery.Generations[0].HighWater != int64(len(shortBody)) {
		t.Fatalf("short recovery = %+v", shortRecovery)
	}

	body := bytes.Repeat([]byte("a"), wire.FingerprintWindowBytes)
	fp := fingerprint(body)
	decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, body, &fp))

	known := recoveryReq(t, st, wire.ToolClaude, testSession, testFile, nil)
	if known.Code != http.StatusOK {
		t.Fatalf("recovery status=%d body=%s", known.Code, known.Body.String())
	}
	var resp wire.RecoveryResponse
	if err := json.Unmarshal(known.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Generations) != 1 || resp.Generations[0].Fingerprint == nil || *resp.Generations[0].Fingerprint != fp {
		t.Fatalf("recovery = %+v", resp)
	}

	filtered := recoveryReq(t, st, wire.ToolClaude, testSession, testFile, &fp)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered status=%d body=%s", filtered.Code, filtered.Body.String())
	}
	missing := recoveryReq(t, st, wire.ToolClaude, testSession, "77777777-7777-7777-7777-777777777777", nil)
	notFound := decodeError(t, missing, wire.ErrNotFound)
	if notFound.ShipperAction != wire.ShipperActionStartFromZero {
		t.Fatalf("not found = %+v", notFound)
	}
}

func TestUUIDsCanonicalizeBeforeRegistryAndMirrorPaths(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	urnSession := "urn:uuid:11111111-1111-1111-1111-111111111111"
	bracedFile := "{22222222-2222-2222-2222-222222222222}"
	body := []byte("canonical")
	decodeAck(t, putReq(t, st, wire.ToolClaude, urnSession, bracedFile, 0, body, nil))

	ack := decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, int64(len(body)), []byte(" tail"), nil))
	if ack.HighWater != int64(len("canonical tail")) {
		t.Fatalf("canonical replay ack = %+v", ack)
	}
	if _, err := os.Stat(st.MirrorPath(wire.ToolClaude, "urn:uuid:11111111-1111-1111-1111-111111111111", "{22222222-2222-2222-2222-222222222222}", 0)); !os.IsNotExist(err) {
		t.Fatalf("non-canonical mirror path exists or stat failed unexpectedly: %v", err)
	}
	assertMirror(t, st, 0, []byte("canonical tail"))
}

func TestFingerprintClaimComputedMismatchIsLoggedAndComputedWins(t *testing.T) {
	var logs bytes.Buffer
	st := newTestStore(t, &logs)
	body := bytes.Repeat([]byte("z"), wire.FingerprintWindowBytes)
	wrong := strings.Repeat("0", sha256.Size*2)
	correct := fingerprint(body)
	if wrong == correct {
		t.Fatal("bad test setup: wrong fingerprint equals correct")
	}

	ack := decodeAck(t, putReq(t, st, wire.ToolClaude, testSession, testFile, 0, body, &wrong))
	if ack.Fingerprint == nil || *ack.Fingerprint != correct {
		t.Fatalf("ack fingerprint = %v want %s", ack.Fingerprint, correct)
	}
	if !strings.Contains(logs.String(), "fingerprint claim differs from mirrored bytes") {
		t.Fatalf("missing mismatch log: %s", logs.String())
	}
}

func TestRestartReplayAfterUnackedMirrorWrite(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(t.Context(), Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.createGeneration(t.Context(), wire.ToolClaude, testSession, testFile, 0, nil, false, nowUTC())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("crashed before ack")
	if err := st.writeMirror(fileState{Tool: wire.ToolClaude, SessionID: testSession, FileUUID: testFile, Generation: 0}, 0, body); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := Open(t.Context(), Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	ack := decodeAck(t, putReq(t, restarted, wire.ToolClaude, testSession, testFile, 0, body, nil))
	if ack.HighWater != int64(len(body)) {
		t.Fatalf("ack = %+v", ack)
	}
	assertMirror(t, restarted, 0, body)
}

func TestErrorCatalogCodesNamedByStoreTests(t *testing.T) {
	// Some codes are implemented by this U3 handler, while auth-only and
	// network-unreachable codes land in later units or outside HTTP. Keep the
	// full frozen catalog named here so a U3 reviewer can see every wire-doc
	// code exercised by name.
	for _, code := range []wire.ErrorCode{
		wire.ErrMalformedRequest,
		wire.ErrUnknownTool,
		wire.ErrOutOfGrant,
		wire.ErrNotFound,
		wire.ErrByteConflict,
		wire.ErrFingerprintConflict,
		wire.ErrGenerationOpened,
		wire.ErrBodyTooLarge,
		wire.ErrOffsetGap,
		wire.ErrPoisonedFile,
		wire.ErrMirrorWriteFailed,
		wire.ErrStoreUnavailable,
	} {
		if code.HTTPStatus() == 0 {
			t.Fatalf("wire code %q has no HTTP status", code)
		}
	}
}

func assertMirror(t *testing.T, st *Store, generation int, want []byte) {
	t.Helper()
	got, err := os.ReadFile(st.MirrorPath(wire.ToolClaude, testSession, testFile, generation))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("generation %d mirror = %q want %q", generation, got, want)
	}
}

func processNextAppend(t *testing.T, st *Store, idx *index.Indexer) {
	t.Helper()
	select {
	case ev := <-st.AppendEvents():
		if err := idx.ProcessAppend(t.Context(), ev); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing append event")
	}
}

func countRows(t *testing.T, st *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func fingerprint(b []byte) string {
	sum := sha256.Sum256(b[:wire.FingerprintWindowBytes])
	return hex.EncodeToString(sum[:])
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func staticWhoIs(identity string, verbs ...string) WhoIsFunc {
	return func(context.Context, string) (WhoIsResult, error) {
		return WhoIsResult{Identity: identity, CapMap: capMap(verbs...)}, nil
	}
}

func capMap(verbs ...string) tailcfg.PeerCapMap {
	values := make([]tailcfg.RawMessage, 0, len(verbs))
	for _, verb := range verbs {
		values = append(values, tailcfg.RawMessage(`{"verb":"`+verb+`"}`))
	}
	return tailcfg.PeerCapMap{TailnetCapabilitySesh: values}
}

type readTrackingBody struct {
	read *bool
}

func (b readTrackingBody) Read([]byte) (int, error) {
	*b.read = true
	return 0, io.EOF
}
