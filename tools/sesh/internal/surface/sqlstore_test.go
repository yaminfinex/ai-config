package surface_test

// SQLStore integration: the real store + real indexer, fed real fixture
// bytes through the real ingest handler, read back through the M2 seam.
// The shell harness (tests/check-surface-live.sh) covers the same flow over
// real binaries with a real shipper; this test pins the adapter's mapping.

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sesh/internal/index"
	"sesh/internal/store"
	"sesh/internal/surface"
	"sesh/internal/wire"
)

// putFixture ships one fixture's full bytes through the real ingest handler
// and synchronously indexes the resulting append event.
func putFixture(t *testing.T, st *store.Store, idx *index.Indexer, tool wire.Tool, sessionID, fileUUID, fixture string, body []byte) {
	t.Helper()
	putBytesOwned(t, st, idx, tool, sessionID, fileUUID, fixture, body, 0, "")
}

// putBytesOwned is putFixture plus offset control and an optional
// X-Sesh-Session-Owner observation.
func putBytesOwned(t *testing.T, st *store.Store, idx *index.Indexer, tool wire.Tool, sessionID, fileUUID, fixture string, body []byte, offset int64, owner string) {
	t.Helper()
	if fixture != "" {
		raw, err := os.ReadFile(filepath.Join(fixturesDir(), fixture))
		if err != nil {
			t.Fatal(err)
		}
		body = raw
	}
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/v1/files/%s/%s/%s/bytes?offset=%d", tool, sessionID, fileUUID, offset),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "gate-node")
	req.Header.Set(wire.HeaderOSUser, "grace")
	if owner != "" {
		req.Header.Set(wire.HeaderSessionOwner, owner)
	}
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT %s: status %d body %s", fileUUID, rr.Code, rr.Body.String())
	}
	select {
	case ev := <-st.AppendEvents():
		if err := idx.ProcessAppend(t.Context(), ev); err != nil {
			t.Fatalf("index append: %v", err)
		}
	default:
		t.Fatalf("PUT %s produced no append event", fileUUID)
	}
}

func openLiveStore(t *testing.T) (*store.Store, *index.Indexer, *surface.SQLStore) {
	t.Helper()
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	idx, err := index.New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	return st, idx, surface.NewSQLStore(st.DB(), st.MirrorPath)
}

func TestSQLStoreRendersResumePairOnceFromLiveIndex(t *testing.T) {
	st, idx, live := openLiveStore(t)
	putFixture(t, st, idx, wire.ToolClaude, uuidResumeOrig, uuidResumeOrig, "claude-resume-original.jsonl", nil)
	putFixture(t, st, idx, wire.ToolClaude, uuidResumeNew, uuidResumeNew, "claude-resume-new-file.jsonl", nil)

	sums, total, err := live.RecentSessions(t.Context(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || total != 1 {
		t.Fatalf("resume pair produced %d sessions (total %d), want 1 (overlap unification)", len(sums), total)
	}
	sum := sums[0]
	if sum.LogicalSessionID != uuidResumeOrig {
		t.Errorf("logical id = %s, want the original file's content id (earliest first-ingest)", sum.LogicalSessionID)
	}
	if len(sum.Files) != 2 || sum.Files[0].FileUUID != uuidResumeOrig || sum.Files[1].FileUUID != uuidResumeNew {
		t.Errorf("files = %+v, want [orig, resumed] in first-ingest order", sum.Files)
	}
	if sum.Hostname != "gate-node" || sum.OSUser != "grace" {
		t.Errorf("node facts = %s/%s, want gate-node/grace", sum.Hostname, sum.OSUser)
	}
	if len(sum.OwnerClaims) != 0 {
		t.Errorf("owner claims = %v; no SESSION_OWNER fact was shipped, absence must stay honest", sum.OwnerClaims)
	}
	if sum.MaxTimestampUTC == nil || !strings.HasPrefix(sum.MaxTimestampUTC.Format("2006-01-02"), "2026-06-28") {
		t.Errorf("max parsed timestamp = %v, want the pair's real last activity on 2026-06-28", sum.MaxTimestampUTC)
	}
	// 206 + 269 lines - 141 overlapping (entry_type, uuid) pairs = 334,
	// the corpus README's verified S2 arithmetic. Drift here means the live
	// indexer and the frozen dedup rule disagree.
	if sum.MessageRows != 334 {
		t.Errorf("message rows = %d, want 334", sum.MessageRows)
	}

	// Render through the real seam: one WINDOWED transcript — the newest
	// 200-row window plus one older window tiling the 334 deduped rows — and
	// no duplicated uuids across the windows.
	srv := newServer(t, live)
	page1 := mustGet200(t, srv, "/s/claude/"+uuidResumeOrig)
	page2 := mustGet200(t, srv, "/s/claude/"+uuidResumeOrig+"?page=2")
	seen := map[string]int{}
	for _, m := range dataUUIDRe.FindAllStringSubmatch(page1+page2, -1) {
		seen[m[1]]++
	}
	for uuid, n := range seen {
		if n > 1 {
			t.Errorf("uuid %s rendered %d times from the live index (S2)", uuid, n)
		}
	}
	if n := strings.Count(page1, `<li class="entry`); n != surface.TranscriptWindowMessages {
		t.Errorf("newest window rendered %d entries, want %d", n, surface.TranscriptWindowMessages)
	}
	if !strings.Contains(page1, "messages 135–334 of 334") {
		t.Error("newest window must label its slice of the session")
	}
	if n := strings.Count(page2, `<li class="entry`); n != 334-surface.TranscriptWindowMessages {
		t.Errorf("older window rendered %d entries, want %d", n, 334-surface.TranscriptWindowMessages)
	}
	mustGet200(t, srv, "/s/claude/"+uuidResumeOrig+"/raw")
}

// TestNodeFilterLabelConsistentUnderServeStale pins the filter/display
// invariant on the node-filtered view: the filter selects on the
// projection's node label and the response renders that same label — one
// snapshot per response — even while a later fact observation has re-homed
// the session and the projection is serving stale. After the triggered
// refresh, the session appears only under its new node, live-labeled.
func TestNodeFilterLabelConsistentUnderServeStale(t *testing.T) {
	st, idx, live := openLiveStore(t)
	putFixture(t, st, idx, wire.ToolClaude, uuidNormal, uuidNormal, "claude-normal.jsonl", nil)

	// Cold build: the session lists under its shipping node, labeled so.
	sums, total, err := live.RecentSessionsByNode(t.Context(), "gate-node", "grace", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(sums) != 1 || sums[0].Hostname != "gate-node" || sums[0].OSUser != "grace" {
		t.Fatalf("cold filtered page = %d sums (total %d, label %s@%s), want 1 labeled gate-node/grace",
			len(sums), total, sums[0].OSUser, sums[0].Hostname)
	}

	// The node facts move: a later observation re-homes the wire session.
	// Facts are an append-only log, so this is a plain INSERT (the same
	// shape a PUT from the new node records).
	if _, err := st.DB().ExecContext(t.Context(), `INSERT INTO fact_observations
		(observed_at, tool, session_id, file_uuid, generation, hostname, os_user, session_owner)
		VALUES (?, ?, ?, ?, 0, 'moved-node', 'grace', NULL)`,
		time.Now().UTC().Format(time.RFC3339Nano), wire.ToolClaude, uuidNormal, uuidNormal); err != nil {
		t.Fatal(err)
	}

	// Warm request on the OLD node observes the moved stamp, serves the
	// stale projection (membership includes the session), and must label
	// every row with the REQUESTED node — never the live moved-to label.
	sums, total, err = live.RecentSessionsByNode(t.Context(), "gate-node", "grace", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(sums) != 1 {
		t.Fatalf("stale filtered page = %d sums (total %d), want the serve-stale membership of 1", len(sums), total)
	}
	if sums[0].Hostname != "gate-node" || sums[0].OSUser != "grace" {
		t.Errorf("stale filtered row labeled %s@%s; the filtered response must render its filter's label",
			sums[0].OSUser, sums[0].Hostname)
	}

	// Converged: the old node is empty; the new node lists it, live-labeled.
	live.WaitProjectionIdle()
	if _, total, err = live.RecentSessionsByNode(t.Context(), "gate-node", "grace", 10, 0); err != nil || total != 0 {
		t.Fatalf("after refresh, old node total = %d (err %v), want 0", total, err)
	}
	sums, total, err = live.RecentSessionsByNode(t.Context(), "moved-node", "grace", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(sums) != 1 || sums[0].Hostname != "moved-node" {
		t.Fatalf("after refresh, new node page = %d sums (total %d), want 1 labeled moved-node", len(sums), total)
	}
}

func TestSQLStoreListsMirroredButUnindexedSession(t *testing.T) {
	st, idx, live := openLiveStore(t)
	// A trailing partial line only: mirrored bytes, zero complete lines,
	// zero index rows. The mirror is truth — the surface must list it and
	// serve the raw fallback rather than go blind (S10 posture).
	partial := []byte(`{"type":"user","uuid":"cut-mid-`)
	fileUUID := "3f3f3f3f-4444-5555-6666-777777777777"
	putFixture(t, st, idx, wire.ToolClaude, fileUUID, fileUUID, "", partial)

	sums, total, err := live.RecentSessions(t.Context(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || total != 1 {
		t.Fatalf("mirrored-but-unindexed file produced %d sessions (total %d), want 1", len(sums), total)
	}
	if sums[0].MessageRows != 0 || !sums[0].FullyQuarantined() {
		t.Errorf("summary %+v: want zero renderable rows forcing the raw fallback", sums[0])
	}

	srv := newServer(t, live)
	body := mustGet200(t, srv, "/s/claude/"+fileUUID)
	if !strings.Contains(body, "raw mirror lines") {
		t.Error("unindexed mirrored session must serve the raw fallback")
	}
	if !strings.Contains(body, "cut-mid-") {
		t.Error("raw fallback must show the mirrored partial bytes")
	}
}

func TestSQLStoreCollectsOwnerClaimsFromObservationLog(t *testing.T) {
	st, idx, live := openLiveStore(t)
	raw, err := os.ReadFile(filepath.Join(fixturesDir(), "claude-normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	cut := bytes.IndexByte(raw, '\n') + 1

	// Two PUTs observing the same owner: one claim, attributed cleanly.
	putBytesOwned(t, st, idx, wire.ToolClaude, uuidNormal, uuidNormal, "", raw[:cut], 0, "alice")
	putBytesOwned(t, st, idx, wire.ToolClaude, uuidNormal, uuidNormal, "", raw[cut:], int64(cut), "alice")
	sums, _, err := live.RecentSessions(t.Context(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || len(sums[0].OwnerClaims) != 1 || sums[0].OwnerClaims[0] != "alice" {
		t.Fatalf("same-owner observations: sessions %+v, want one session with one alice claim", sums)
	}
	if do := sums[0].DisplayOwner(); do.Name != "alice" || do.Conflict {
		t.Fatalf("display owner = %+v, want alice without conflict", do)
	}

	// A later PUT observing a DIFFERENT owner for the same session: the log
	// keeps both observations (append-only, I8 — never retracted) and the
	// view renders honest absence with the conflict label. A different
	// fixture, because identical content ids would unify the two sessions.
	raw2, err := os.ReadFile(filepath.Join(fixturesDir(), "claude-interleaved-writers-standin.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	cut2 := bytes.IndexByte(raw2, '\n') + 1
	putBytesOwned(t, st, idx, wire.ToolClaude, uuidInterleave, uuidInterleave, "", raw2[:cut2], 0, "carol")
	putBytesOwned(t, st, idx, wire.ToolClaude, uuidInterleave, uuidInterleave, "", raw2[cut2:], int64(cut2), "dave")
	sum, ok, err := live.Session(t.Context(), wire.ToolClaude, uuidInterleave)
	if err != nil || !ok {
		t.Fatalf("conflicted session lookup: ok=%v err=%v", ok, err)
	}
	if len(sum.OwnerClaims) != 2 {
		t.Fatalf("owner claims = %v, want both observations kept", sum.OwnerClaims)
	}
	if do := sum.DisplayOwner(); !do.Conflict || do.Name != "" {
		t.Fatalf("display owner = %+v, want conflict with honest absence", do)
	}
	// The recency projection serves stale-while-revalidating: the earlier
	// RecentSessions built it before the conflicted session's PUTs, so
	// trigger the refresh and wait for convergence before asserting on the
	// rendered list.
	if _, _, err := live.RecentSessions(t.Context(), 10, 0); err != nil {
		t.Fatal(err)
	}
	live.WaitProjectionIdle()
	body := mustGet200(t, newServer(t, live), "/sessions")
	if !strings.Contains(body, "conflicting claims") {
		t.Error("sessions page must badge the conflicted session")
	}
	if !strings.Contains(body, `<td>alice <span class="source">SESSION_OWNER fact</span></td>`) {
		t.Error("cleanly claimed session must fill the person column with alice and its source")
	}
}

func TestSQLStoreUsesStoreStampedTailnetIdentity(t *testing.T) {
	st, idx, live := openLiveStore(t)
	putFixture(t, st, idx, wire.ToolClaude, uuidNormal, uuidNormal, "claude-normal.jsonl", nil)
	if _, err := st.DB().ExecContext(t.Context(), `UPDATE fact_observations SET tailnet_identity = ?`, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	sums, _, err := live.RecentSessions(t.Context(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sums))
	}
	if sums[0].TailnetIdentity != "alice@example.com" {
		t.Fatalf("tailnet identity = %q, want store-stamped WhoIs identity", sums[0].TailnetIdentity)
	}
	if do := sums[0].DisplayOwner(); do.Name != "alice@example.com" || do.Source != "tailnet identity" || !do.Claimed {
		t.Fatalf("display owner = %+v, want tailnet identity attribution", do)
	}
}

func TestSQLStoreNodesFlagsStaleLastPut(t *testing.T) {
	st, _, live := openLiveStore(t)
	now := time.Now().UTC()
	for _, row := range []struct {
		host, user string
		at         time.Time
	}{
		{"fresh-host", "grace", now.Add(-47 * time.Hour)},
		{"stale-host", "grace", now.Add(-49 * time.Hour)},
	} {
		if _, err := st.DB().ExecContext(t.Context(), `INSERT INTO last_seen(hostname, os_user, last_put_at) VALUES (?, ?, ?)`,
			row.host, row.user, row.at.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	nodes, err := live.Nodes(t.Context(), 48*time.Hour)
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
