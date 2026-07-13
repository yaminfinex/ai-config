package surface_test

// The single-flight/serve-stale gates for the recency projection: under a
// moving stamp and concurrent requests, exactly one rebuild runs at a time,
// requests during a rebuild return promptly with the previous projection,
// convergence follows the documented bound (one extra rebuild when churn
// straddles a rebuild's stamp), a failed rebuild clears the latch and keeps
// serving stale, and a canceled cold waiter neither cancels nor wedges the
// shared build. The companion large-corpus gate (bounded_recency_test.go)
// pins the plan evidence; these gates pin the concurrency state machine,
// deterministically, by parking rebuilds on a staged test hook instead of
// guessing at timing.

import (
	"context"
	"database/sql"
	"errors"
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

// staleFixture is one built corpus behind a recording seam plus a plain
// handle for churn writes, so the recorded query log stays exactly the
// serve path's statements.
type staleFixture struct {
	live  *surface.SQLStore
	plain *sql.DB
	log   *queryLog
}

func openStaleFixture(t *testing.T) staleFixture {
	t.Helper()
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
	plain, err := sql.Open("sqlite", sqlitedsn.ReadWrite(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	live := surface.NewSQLStore(db, mirrorPath)
	t.Cleanup(live.Close)
	return staleFixture{live: live, plain: plain, log: log}
}

// stageBarrier parks every rebuild at one stage: entered signals "a rebuild
// is provably in flight right here", release lets it continue. Other stages
// pass through.
type stageBarrier struct {
	stage   surface.RebuildStage
	entered chan struct{}
	release chan struct{}
}

func newStageBarrier(t *testing.T, stage surface.RebuildStage) *stageBarrier {
	t.Helper()
	b := &stageBarrier{stage: stage, entered: make(chan struct{}, 16), release: make(chan struct{})}
	// Unpark any still-held rebuild before the fixture's Close cleanup
	// drains it, so a mid-test failure fails instead of hanging.
	t.Cleanup(func() { close(b.release) })
	return b
}

func (b *stageBarrier) hook(stage surface.RebuildStage) error {
	if stage == b.stage {
		b.entered <- struct{}{}
		<-b.release
	}
	return nil
}

func (b *stageBarrier) assertNoExtraRebuild(t *testing.T, phase string) {
	t.Helper()
	select {
	case <-b.entered:
		t.Fatalf("%s: a second concurrent rebuild started; single-flight is broken", phase)
	default:
	}
}

func TestProjectionSingleFlightServeStale(t *testing.T) {
	fx := openStaleFixture(t)
	barrier := newStageBarrier(t, surface.RebuildStart)
	fx.live.SetRebuildHook(barrier.hook)

	// Cold start: concurrent requests share ONE build. All of them block on
	// the same single-flighted rebuild and return the same complete result.
	fx.log.reset()
	const coldRequests = 8
	var wg sync.WaitGroup
	totals := make([]int, coldRequests)
	errs := make([]error, coldRequests)
	for i := 0; i < coldRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, totals[i], errs[i] = fx.live.RecentSessions(t.Context(), 5, 0)
		}(i)
	}
	<-barrier.entered // the one shared rebuild is in flight
	barrier.release <- struct{}{}
	wg.Wait()
	for i := 0; i < coldRequests; i++ {
		if errs[i] != nil {
			t.Fatalf("cold request %d: %v", i, errs[i])
		}
		if totals[i] != staleCorpusSessions {
			t.Fatalf("cold request %d saw total %d, want %d", i, totals[i], staleCorpusSessions)
		}
	}
	barrier.assertNoExtraRebuild(t, "cold start")
	if n := countMatching(fx.log.snapshot(), rebuildMarker); n != 1 {
		t.Fatalf("%d concurrent cold requests ran %d rebuilds, want exactly 1", coldRequests, n)
	}
	fx.live.WaitProjectionIdle()

	// Moving stamp: the first request to observe it serves the previous
	// projection immediately and triggers the background refresh.
	insertFreshSession(t, fx.plain, staleCorpusSessions)
	sums, total, err := fx.live.RecentSessions(t.Context(), 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != staleCorpusSessions {
		t.Fatalf("request on a moved stamp saw total %d, want the stale %d", total, staleCorpusSessions)
	}
	if sums[0].LogicalSessionID == corpusID(staleCorpusSessions) {
		t.Fatal("request on a moved stamp returned the fresh session; it should have served stale")
	}
	<-barrier.entered // the refresh it triggered is now parked in flight

	// While that rebuild is held: churn continues and concurrent requests
	// keep arriving. Every one of them must complete with the previous
	// projection — the barrier is still held when wg.Wait returns, which IS
	// the promptness proof — and none may start a duplicate rebuild.
	fx.log.reset()
	insertFreshSession(t, fx.plain, staleCorpusSessions+1)
	const staleRequests = 8
	staleTotals := make([]int, staleRequests)
	staleErrs := make([]error, staleRequests)
	var staleWG sync.WaitGroup
	for i := 0; i < staleRequests; i++ {
		staleWG.Add(1)
		go func(i int) {
			defer staleWG.Done()
			_, staleTotals[i], staleErrs[i] = fx.live.RecentSessions(t.Context(), 5, 0)
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
	barrier.assertNoExtraRebuild(t, "held rebuild")
	staleLog := fx.log.snapshot()
	if n := countMatching(staleLog, rebuildMarker); n != 0 {
		t.Fatalf("requests during a held rebuild ran %d rebuild queries, want 0", n)
	}
	// Serve-stale requests keep the warm-path plan discipline: stamp probes
	// plus full-key-seek hydration, nothing corpus-shaped.
	assertSeeksOnly(t, fx.plain, staleLog)

	// Churn stops; the held rebuild is released. Its stamp probe runs after
	// the barrier, so it sees all churn and the projection converges within
	// this ONE rebuild: the very next request serves fresh with no further
	// work. (Churn straddling a rebuild's stamp has its own gate below.)
	barrier.release <- struct{}{}
	fx.live.WaitProjectionIdle()
	fx.log.reset()
	sums, total, err = fx.live.RecentSessions(t.Context(), 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != staleCorpusSessions+2 {
		t.Fatalf("converged total = %d, want %d", total, staleCorpusSessions+2)
	}
	if sums[0].LogicalSessionID != corpusID(staleCorpusSessions+1) {
		t.Fatalf("converged newest = %s, want %s", sums[0].LogicalSessionID, corpusID(staleCorpusSessions+1))
	}
	if n := countMatching(fx.log.snapshot(), rebuildMarker); n != 0 {
		t.Fatalf("converged request ran %d rebuilds, want 0 (stamp is current)", n)
	}
	barrier.assertNoExtraRebuild(t, "convergence")
}

// Churn straddling a rebuild's stamp: writes landing between the refresh's
// stamp probe and its ranking query are PRESENT in the published ranking
// (the ranking query reads a later snapshot) but not covered by the stored
// stamp, so the next request must trigger exactly one more rebuild —
// conservative re-verification, never silent absorption. This is the
// documented "plus one rebuild when churn straddles the refresh stamp"
// interleaving of the staleness bound.
func TestProjectionChurnStraddlingRebuildStampForcesOneExtraRebuild(t *testing.T) {
	fx := openStaleFixture(t)
	if _, _, err := fx.live.RecentSessions(t.Context(), 5, 0); err != nil {
		t.Fatal(err) // cold build, no hook installed yet
	}
	fx.live.WaitProjectionIdle()

	barrier := newStageBarrier(t, surface.RebuildStamped)
	fx.live.SetRebuildHook(barrier.hook)

	// Move the stamp, trigger a refresh, and park it AFTER its stamp probe.
	insertFreshSession(t, fx.plain, staleCorpusSessions)
	if _, _, err := fx.live.RecentSessions(t.Context(), 5, 0); err != nil {
		t.Fatal(err)
	}
	<-barrier.entered
	// Churn lands in the stamp/ranking gap, then quiesces for good.
	insertFreshSession(t, fx.plain, staleCorpusSessions+1)
	barrier.release <- struct{}{}
	fx.live.WaitProjectionIdle()

	// The published projection already carries the straddling write…
	fx.log.reset()
	sums, total, err := fx.live.RecentSessions(t.Context(), 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != staleCorpusSessions+2 || sums[0].LogicalSessionID != corpusID(staleCorpusSessions+1) {
		t.Fatalf("post-straddle serve: total %d newest %s, want %d / %s (ranking query sees the later snapshot)",
			total, sums[0].LogicalSessionID, staleCorpusSessions+2, corpusID(staleCorpusSessions+1))
	}
	// …but the stored stamp predates it, so that request must have started
	// exactly one more rebuild.
	select {
	case <-barrier.entered:
	default:
		t.Fatal("stamp straddled by churn did not force a re-verifying rebuild; changes were silently absorbed")
	}
	barrier.release <- struct{}{}
	fx.live.WaitProjectionIdle()

	// Now the stamp is current: no further rebuilds.
	fx.log.reset()
	if _, total, err = fx.live.RecentSessions(t.Context(), 5, 0); err != nil || total != staleCorpusSessions+2 {
		t.Fatalf("converged request: total %d err %v", total, err)
	}
	if n := countMatching(fx.log.snapshot(), rebuildMarker); n != 0 {
		t.Fatalf("stamp-current request ran %d rebuilds, want 0", n)
	}
	barrier.assertNoExtraRebuild(t, "post-convergence")
}

// A failed rebuild must clear the single-flight latch: stale keeps serving
// through the failure, and the next request that sees the moved stamp
// starts a fresh rebuild that succeeds. Covers the cold edge too — a cold
// waiter receives the shared build's error, and a retry recovers.
func TestProjectionRebuildFailureClearsLatchAndRetries(t *testing.T) {
	fx := openStaleFixture(t)
	rebuildErr := errors.New("injected rebuild failure")
	var mu sync.Mutex
	failNext := true
	fx.live.SetRebuildHook(func(stage surface.RebuildStage) error {
		if stage != surface.RebuildStart {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if failNext {
			failNext = false
			return rebuildErr
		}
		return nil
	})

	// Cold edge: the shared first build fails; every cold waiter gets the
	// error, nothing is published, the latch is clear.
	if _, _, err := fx.live.RecentSessions(t.Context(), 5, 0); !errors.Is(err, rebuildErr) {
		t.Fatalf("cold request during failed build: err %v, want the shared build's error", err)
	}
	fx.live.WaitProjectionIdle()

	// Retry recovers: the latch was cleared, a fresh build runs and serves.
	if _, total, err := fx.live.RecentSessions(t.Context(), 5, 0); err != nil || total != staleCorpusSessions {
		t.Fatalf("retry after failed cold build: total %d err %v", total, err)
	}
	fx.live.WaitProjectionIdle()

	// Warm edge: stamp moves, the triggered refresh fails — the request
	// already served stale, and stale KEEPS serving through the failure.
	mu.Lock()
	failNext = true
	mu.Unlock()
	insertFreshSession(t, fx.plain, staleCorpusSessions)
	if _, total, err := fx.live.RecentSessions(t.Context(), 5, 0); err != nil || total != staleCorpusSessions {
		t.Fatalf("request triggering the failing refresh: total %d err %v, want stale serve", total, err)
	}
	fx.live.WaitProjectionIdle() // the failed refresh has fully unwound

	// Next request: still serves stale (never an error page), retriggers a
	// rebuild that now succeeds, and the projection converges.
	if _, total, err := fx.live.RecentSessions(t.Context(), 5, 0); err != nil || total != staleCorpusSessions {
		t.Fatalf("request after failed refresh: total %d err %v, want continued stale serve", total, err)
	}
	fx.live.WaitProjectionIdle()
	if _, total, err := fx.live.RecentSessions(t.Context(), 5, 0); err != nil || total != staleCorpusSessions+1 {
		t.Fatalf("converged after retry: total %d err %v, want %d", total, err, staleCorpusSessions+1)
	}
}

// A cold waiter whose request context is canceled returns immediately with
// that error and neither cancels nor wedges the shared build: the other
// waiters still get the completed projection.
func TestProjectionCanceledColdWaiterDoesNotWedgeSharedBuild(t *testing.T) {
	fx := openStaleFixture(t)
	barrier := newStageBarrier(t, surface.RebuildStart)
	fx.live.SetRebuildHook(barrier.hook)

	canceledCtx, cancel := context.WithCancel(t.Context())
	canceledDone := make(chan error, 1)
	var wg sync.WaitGroup
	const survivors = 3
	totals := make([]int, survivors)
	errs := make([]error, survivors)
	go func() {
		_, _, err := fx.live.RecentSessions(canceledCtx, 5, 0)
		canceledDone <- err
	}()
	for i := 0; i < survivors; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, totals[i], errs[i] = fx.live.RecentSessions(t.Context(), 5, 0)
		}(i)
	}
	<-barrier.entered // the one shared build is provably in flight
	cancel()
	// The canceled waiter returns while the build is still held — it waited
	// on the result, never owned the rebuild.
	if err := <-canceledDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cold waiter returned err %v, want context.Canceled", err)
	}
	barrier.release <- struct{}{}
	wg.Wait()
	for i := 0; i < survivors; i++ {
		if errs[i] != nil {
			t.Fatalf("surviving cold waiter %d: %v", i, errs[i])
		}
		if totals[i] != staleCorpusSessions {
			t.Fatalf("surviving cold waiter %d saw total %d, want %d", i, totals[i], staleCorpusSessions)
		}
	}
	barrier.assertNoExtraRebuild(t, "canceled waiter")
	fx.live.WaitProjectionIdle()
}
