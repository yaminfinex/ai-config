package surface_test

// The max-size-sessions gate: page one of the sessions list is, by
// construction, the page of the LARGEST sessions (most recent = most
// active), so its render cost must be independent of listed-session sizes.
// The projection carries each session's row counts, max timestamp, and
// membership precisely so the hot path never walks a listed session's index
// rows (docs/design/2026-07-13-sesh-store-read-write-split.md, the
// projection-carried aggregates delta). The evidence is structural, on a
// fixture whose page-one sessions are ALL multi-thousand-row: any
// per-listed-session row walk — counts, max timestamps, membership mapping
// — must query sesh_index_messages, so a warm page render recording zero
// statements against that table (outside the stamp probe and the rebuild
// passes) IS the no-row-walks property. The detector is proven against the
// deliberately regressed shape (the live per-key hydration path), per the
// house rule: detectors get proven, not assumed.

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sesh/internal/index"
	"sesh/internal/sqlitedsn"
	"sesh/internal/store"
	"sesh/internal/surface"
	"sesh/internal/wire"
)

const (
	// maxSizeSessions > one page, so paging facts stay honest; every session
	// is max-size, so page one holds nothing but worst-case rows.
	maxSizeSessions = 55
	// rowsPerSession makes a per-listed-session row walk cost ~100k row
	// visits per page — invisible to the small-rows corpus gate, fatal live.
	rowsPerSession = 2000
	// quarantinedPerSession pins that quarantined counts ride the projection
	// too, and that the LATER quarantined timestamp never moves recency.
	quarantinedPerSession = 2
)

func aggSessionID(i int) string {
	return fmt.Sprintf("%08d-0000-4000-8000-00000000aaaa", i)
}

// aggInstant is session i's recency instant: the max NON-quarantined
// timestamp. Ascends with i, so the highest i is the most recent session.
func aggInstant(i int) time.Time {
	return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
}

// buildMaxSizeCorpus writes n logical sessions of rowsPerSession
// non-quarantined rows each (timestamps ascending to aggInstant(i)) plus
// quarantinedPerSession quarantined rows — one of them stamped LATER than
// every clean row, so recency provably ignores quarantined timestamps.
// Bulk INSERTs stand in for real PUTs purely for build speed, same posture
// as the 5k-corpus gate.
func buildMaxSizeCorpus(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	insFile, err := tx.Prepare(`INSERT INTO files
		(tool, session_id, file_uuid, generation, created_at, updated_at, last_put_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	insMsg, err := tx.Prepare(`INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type,
		 message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal,
		 line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, 'user', ?, ?, ?, 'user', ?, 0, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	insFact, err := tx.Prepare(`INSERT INTO fact_observations
		(observed_at, tool, session_id, file_uuid, generation, hostname, os_user, session_owner)
		VALUES (?, ?, ?, ?, 0, 'agg-node', 'grace', NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := aggSessionID(i)
		max := aggInstant(i)
		at := max.Format(time.RFC3339Nano)
		if _, err := insFile.Exec(wire.ToolClaude, id, id, 0, at, at, at); err != nil {
			t.Fatal(err)
		}
		if _, err := insFact.Exec(at, wire.ToolClaude, id, id); err != nil {
			t.Fatal(err)
		}
		for line := 0; line < rowsPerSession; line++ {
			ts := max.Add(-time.Duration(rowsPerSession-1-line) * time.Second)
			if _, err := insMsg.Exec(wire.ToolClaude, id, id, id,
				fmt.Sprintf("%08d-1%06d-4000-8000-000000000000", i, line), id, 0,
				ts.Format(time.RFC3339Nano), line, int64(line)*16, int64(line+1)*16, 0, ""); err != nil {
				t.Fatal(err)
			}
		}
		for q := 0; q < quarantinedPerSession; q++ {
			// The first quarantined row is the session's LATEST timestamp of
			// all; recency (R14: max PARSED NON-QUARANTINED) must not move.
			ts := max.Add(time.Duration(q+1) * time.Minute)
			if _, err := insMsg.Exec(wire.ToolClaude, id, id, id,
				fmt.Sprintf("%08d-2%06d-4000-8000-000000000000", i, q), id, 0,
				ts.Format(time.RFC3339Nano), rowsPerSession+q,
				int64(rowsPerSession+q)*16, int64(rowsPerSession+q+1)*16, 1, "gate: deliberate quarantine"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// indexRowWalkQueries is the no-row-walks detector: the recorded statements
// touching the message index outside the stamp probe and the projection
// rebuild's two corpus passes. On the sessions-list hot path there must be
// none — every way to recompute a listed session's aggregates or membership
// (row counts, max timestamps, the mapping DISTINCT) reads
// sesh_index_messages, so an empty result is the property itself.
func indexRowWalkQueries(queries []string) []string {
	var out []string
	for _, q := range queries {
		if !strings.Contains(q, "sesh_index_messages") {
			continue
		}
		if strings.Contains(q, stampMarker) || strings.Contains(q, rebuildMarker) || strings.Contains(q, memberMarker) {
			continue
		}
		out = append(out, q)
	}
	return out
}

func TestSessionsPageIndependentOfListedSessionSizes(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), store.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(t.Context(), st.DB(), st.MirrorPath); err != nil {
		t.Fatal(err)
	}
	buildMaxSizeCorpus(t, st.DB(), maxSizeSessions)
	mirrorPath := st.MirrorPath
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "store.sqlite")
	db, log := openRecordingDB(t, dbPath)
	plain, err := sql.Open("sqlite", sqlitedsn.ReadWrite(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	live := surface.NewSQLStore(db, mirrorPath)

	// Cold request: the one corpus-scale build, and a correct page — the
	// projection-carried aggregates must match what live queries would say.
	sums, total, err := live.RecentSessions(t.Context(), 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != maxSizeSessions || len(sums) != 50 {
		t.Fatalf("page = %d sessions of total %d, want 50 of %d", len(sums), total, maxSizeSessions)
	}
	newest := sums[0]
	if newest.LogicalSessionID != aggSessionID(maxSizeSessions-1) {
		t.Errorf("newest session = %s, want %s", newest.LogicalSessionID, aggSessionID(maxSizeSessions-1))
	}
	for _, sum := range sums {
		if sum.MessageRows != rowsPerSession || sum.QuarantinedRows != quarantinedPerSession {
			t.Fatalf("session %s aggregates = %d rows (+%d quarantined), want %d (+%d)",
				sum.LogicalSessionID, sum.MessageRows, sum.QuarantinedRows, rowsPerSession, quarantinedPerSession)
		}
	}
	if !newest.Recency().Equal(aggInstant(maxSizeSessions - 1)) {
		t.Errorf("newest recency = %v, want max NON-quarantined timestamp %v (a later quarantined row exists and must not win)",
			newest.Recency(), aggInstant(maxSizeSessions-1))
	}
	if len(newest.Files) != 1 || newest.Files[0].FileUUID != newest.LogicalSessionID {
		t.Errorf("newest membership = %+v, want its one file generation", newest.Files)
	}

	// Warm render through the real handler: page-one work must be a fixed
	// handful of queries, zero of them against the message index, all plans
	// full-key seeks — on a page where EVERY listed session is
	// multi-thousand-row. This is AC territory: with per-listed-session row
	// walks this page pays ~100k row visits; without them its cost does not
	// depend on session sizes at all.
	srv := newServer(t, live)
	log.reset()
	start := time.Now()
	body := mustGet200(t, srv, "/sessions")
	elapsed := time.Since(start)
	warm := log.snapshot()
	if !strings.Contains(body, fmt.Sprintf("showing latest 50 of %d sessions", maxSizeSessions)) {
		t.Error("sessions page must state its bound")
	}
	if !strings.Contains(body, fmt.Sprintf(">%d (+%d quarantined)</td>", rowsPerSession, quarantinedPerSession)) {
		t.Error("rows column must render the projection-carried counts")
	}
	if len(warm) > 5 {
		t.Errorf("warm max-size page ran %d queries, want a fixed handful (<=5):\n%s",
			len(warm), strings.Join(warm, "\n---\n"))
	}
	if walks := indexRowWalkQueries(warm); len(walks) != 0 {
		t.Errorf("warm max-size page walked listed sessions' index rows:\n%s", strings.Join(walks, "\n---\n"))
	}
	assertSeeksOnly(t, plain, warm)
	// Generous wall ceiling — the structural assertions above carry the
	// proof; this only keeps "renders within budget" honest against a
	// pathological plan the recorder cannot see.
	if elapsed > 2*time.Second {
		t.Errorf("warm max-size page took %v", elapsed)
	}

	// Aggregate staleness pins to the serve-stale doctrine: rows appended
	// after the last rebuild are invisible to the listed count until the
	// triggered refresh lands — never a blocking recount, never a lie about
	// which snapshot is served.
	id := aggSessionID(maxSizeSessions - 1)
	at := aggInstant(maxSizeSessions).Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type, message_uuid,
		 file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, 'user', ?, ?, 0, 'user', ?, 0, 9000001, 0, 16, 0, '')`,
		wire.ToolClaude, id, id, id, fmt.Sprintf("%08d-3000000-4000-8000-000000000000", maxSizeSessions-1), id, at); err != nil {
		t.Fatal(err)
	}
	stale, _, err := live.RecentSessions(t.Context(), 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stale[0].MessageRows != rowsPerSession {
		t.Errorf("request observing the moved stamp served count %d; it must serve the previous snapshot's %d while the refresh runs",
			stale[0].MessageRows, rowsPerSession)
	}
	live.WaitProjectionIdle()
	fresh, _, err := live.RecentSessions(t.Context(), 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fresh[0].MessageRows != rowsPerSession+1 || !fresh[0].Recency().Equal(aggInstant(maxSizeSessions)) {
		t.Errorf("after the triggered refresh: count %d recency %v, want %d and %v",
			fresh[0].MessageRows, fresh[0].Recency(), rowsPerSession+1, aggInstant(maxSizeSessions))
	}

	// Negative self-check: the deliberately regressed hydration shape — the
	// live per-key path the page USED to take, which walks every index row
	// of every listed session — must trip the detector, or the detector is
	// theater.
	t.Run("row-walk detector flags the live hydration shape", func(t *testing.T) {
		ids := make([]string, 0, len(sums))
		for _, sum := range sums {
			ids = append(ids, sum.LogicalSessionID)
		}
		log.reset()
		n, err := live.HydrateLiveForTest(t.Context(), wire.ToolClaude, ids)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(ids) {
			t.Fatalf("live hydration self-check assembled %d of %d sessions (harness broke)", n, len(ids))
		}
		if walks := indexRowWalkQueries(log.snapshot()); len(walks) == 0 {
			t.Fatal("the live per-key hydration shape passed the no-row-walks detector; the detector is not observing the hot path's SQL")
		}
	})
}
