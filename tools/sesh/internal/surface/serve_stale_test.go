package surface_test

// The single-flight/serve-stale gate for the recency projection: under a
// moving stamp and concurrent requests, exactly one rebuild runs at a time,
// requests during a rebuild return promptly with the previous projection,
// and the projection converges within one rebuild once churn stops. The
// companion large-corpus gate (bounded_recency_test.go) pins the plan
// evidence; this gate pins the concurrency behavior, deterministically, by
// parking every rebuild on a test barrier instead of guessing at timing.

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sesh/internal/index"
	"sesh/internal/sqlitedsn"
	"sesh/internal/store"
	"sesh/internal/surface"
	"sesh/internal/wire"
)

const staleCorpusSessions = 200

// insertFreshSession appends one new indexed session through the given
// handle, moving both stamp probes (files MAX(rowid), index MAX(id)).
func insertFreshSession(t *testing.T, db *sql.DB, i int) {
	t.Helper()
	id := corpusID(i)
	at := corpusInstant(i).Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO files (tool, session_id, file_uuid, generation, created_at, updated_at, last_put_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, wire.ToolClaude, id, id, 0, at, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type, message_uuid,
		 file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, 'user', ?, ?, 0, 'user', ?, 0, 0, 0, 16, 0, '')`,
		wire.ToolClaude, id, id, id, id, id, at); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionSingleFlightServeStale(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), store.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(t.Context(), st.DB(), st.MirrorPath); err != nil {
		t.Fatal(err)
	}
	buildBigCorpus(t, st.DB(), staleCorpusSessions)
	mirrorPath := st.MirrorPath
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "store.sqlite")
	db, log := openRecordingDB(t, dbPath)
	// Churn writes go through a separate plain handle so the recorded query
	// log stays exactly the serve path's statements.
	plain, err := sql.Open("sqlite", sqlitedsn.ReadWrite(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	live := surface.NewSQLStore(db, mirrorPath)

	// Every rebuild announces itself on entered, then parks until the test
	// feeds release — "a rebuild is in flight" is now a held channel, not a
	// race to observe.
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	live.SetRebuildBarrier(func() {
		entered <- struct{}{}
		<-release
	})
	releaseOne := func() { release <- struct{}{} }
	assertNoExtraRebuildEntered := func(phase string) {
		t.Helper()
		select {
		case <-entered:
			t.Fatalf("%s: a second concurrent rebuild started; single-flight is broken", phase)
		default:
		}
	}

	// Cold start: concurrent requests share ONE build. All of them block on
	// the same single-flighted rebuild and return the same complete result.
	log.reset()
	const coldRequests = 8
	var wg sync.WaitGroup
	totals := make([]int, coldRequests)
	errs := make([]error, coldRequests)
	for i := 0; i < coldRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, totals[i], errs[i] = live.RecentSessions(t.Context(), 5, 0)
		}(i)
	}
	<-entered // the one shared rebuild is in flight
	releaseOne()
	wg.Wait()
	for i := 0; i < coldRequests; i++ {
		if errs[i] != nil {
			t.Fatalf("cold request %d: %v", i, errs[i])
		}
		if totals[i] != staleCorpusSessions {
			t.Fatalf("cold request %d saw total %d, want %d", i, totals[i], staleCorpusSessions)
		}
	}
	assertNoExtraRebuildEntered("cold start")
	if n := countMatching(log.snapshot(), rebuildMarker); n != 1 {
		t.Fatalf("%d concurrent cold requests ran %d rebuilds, want exactly 1", coldRequests, n)
	}
	live.WaitProjectionIdle()

	// Moving stamp: the first request to observe it serves the previous
	// projection immediately and triggers the background refresh.
	insertFreshSession(t, plain, staleCorpusSessions)
	sums, total, err := live.RecentSessions(t.Context(), 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != staleCorpusSessions {
		t.Fatalf("request on a moved stamp saw total %d, want the stale %d", total, staleCorpusSessions)
	}
	if sums[0].LogicalSessionID == corpusID(staleCorpusSessions) {
		t.Fatal("request on a moved stamp returned the fresh session; it should have served stale")
	}
	<-entered // the refresh it triggered is now parked in flight

	// While that rebuild is held: churn continues and concurrent requests
	// keep arriving. Every one of them must complete with the previous
	// projection — the barrier is still held when wg.Wait returns, which IS
	// the promptness proof — and none may start a duplicate rebuild.
	log.reset()
	insertFreshSession(t, plain, staleCorpusSessions+1)
	const staleRequests = 8
	staleTotals := make([]int, staleRequests)
	staleErrs := make([]error, staleRequests)
	var staleWG sync.WaitGroup
	for i := 0; i < staleRequests; i++ {
		staleWG.Add(1)
		go func(i int) {
			defer staleWG.Done()
			_, staleTotals[i], staleErrs[i] = live.RecentSessions(t.Context(), 5, 0)
		}(i)
	}
	staleWG.Wait()
	for i := 0; i < staleRequests; i++ {
		if staleErrs[i] != nil {
			t.Fatalf("stale request %d: %v", i, staleErrs[i])
		}
		if staleTotals[i] != staleCorpusSessions {
			t.Fatalf("stale request %d saw total %d, want the previous projection's %d", i, staleTotals[i], staleCorpusSessions)
		}
	}
	assertNoExtraRebuildEntered("held rebuild")
	staleLog := log.snapshot()
	if n := countMatching(staleLog, rebuildMarker); n != 0 {
		t.Fatalf("requests during a held rebuild ran %d rebuild queries, want 0", n)
	}
	// Serve-stale requests keep the warm-path plan discipline: stamp probes
	// plus full-key-seek hydration, nothing corpus-shaped.
	assertSeeksOnly(t, plain, staleLog)

	// Churn stops; the held rebuild is released. It reads the stamp at its
	// own start — after all churn — so the projection converges within this
	// ONE rebuild: the very next request serves fresh with no further work.
	releaseOne()
	live.WaitProjectionIdle()
	log.reset()
	sums, total, err = live.RecentSessions(t.Context(), 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != staleCorpusSessions+2 {
		t.Fatalf("converged total = %d, want %d", total, staleCorpusSessions+2)
	}
	if sums[0].LogicalSessionID != corpusID(staleCorpusSessions+1) {
		t.Fatalf("converged newest = %s, want %s", sums[0].LogicalSessionID, corpusID(staleCorpusSessions+1))
	}
	if n := countMatching(log.snapshot(), rebuildMarker); n != 0 {
		t.Fatalf("converged request ran %d rebuilds, want 0 (stamp is current)", n)
	}
	assertNoExtraRebuildEntered("convergence")
}
