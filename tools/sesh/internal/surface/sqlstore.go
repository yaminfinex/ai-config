package surface

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sesh/internal/wire"
)

// MirrorPath resolves a mirrored generation to its durable file path — the
// same resolver signature the indexer takes from the store.
type MirrorPath func(tool wire.Tool, sessionID, fileUUID string, generation int) string

// SQLStore satisfies Store from the live store database (the frozen
// sesh_index_messages schema plus store bookkeeping: files, fact
// observations) and the mirror tree. This is the M2 seam implementation; the
// store process owns the DB handle and its lifecycle.
//
// The serve path hands this store the read-only pool (store.ReadDB), never
// the write connection: append transactions hold the writer for corpus-scale
// index work, and a read queued behind one pays that holding per query. The
// methods still collect each result set fully before the next query so they
// also run correctly on a single-connection handle (tests, admin tooling).
//
// The recency ranking is a surface-owned projection: the complete ranked
// (tool, logical) key list — each entry carrying the session's node label
// (hostname, OS user) so the per-node sessions view slices the same list
// instead of growing its own SQL ranking path, its row-count/max-timestamp
// aggregates, and its member file-generation keys, so listing never walks
// a listed session's index rows at request time — plus total, held in
// memory and rebuilt only when the store's cheap version stamp moves.
// Request-time work therefore stays proportional to the page — a stamp
// probe plus key-constrained hydration of per-request data (file
// bookkeeping times, node facts, owner claims), independent of how large
// the listed sessions are.
//
// The rebuild is single-flighted and serve-stale: at most one rebuild runs
// at a time, and a request that observes a moved stamp returns the existing
// projection immediately while the refresh runs in the background. Only the
// cold start (no projection yet) blocks, and every concurrent cold request
// shares that one build. This deliberately supersedes the original
// read-your-own-writes property (rebuild inline whenever the stamp moved):
// under bulk ingest the stamp moves between every request, which degenerated
// to a corpus-scale rebuild per page load. The ranked list, its total, and
// everything a projection entry carries (node label, row-count/timestamp
// aggregates, membership) can lag; page hydration reads live tables only
// for per-request data (file bookkeeping times, facts, claims). Every
// request that sees a moved stamp triggers a refresh, which is what bounds
// the lag for a watched page. The exact staleness bound — including the
// first-request-after-idle exception and the churn-straddling-a-rebuild
// interleaving — is stated in the README surface section and the delta in
// docs/design/2026-07-13-sesh-store-read-write-split.md; this comment only
// owes the mechanism.
type SQLStore struct {
	db         *sql.DB
	mirrorPath MirrorPath
	// log carries the projection rebuild lines under the same identifier-free
	// journal contract (and the same log-contract gate) as the Server's
	// degradation events.
	log *slog.Logger

	// refreshCtx owns every background rebuild; Close cancels it and waits,
	// so no refresh goroutine outlives the component that owns the DB.
	refreshCtx    context.Context
	cancelRefresh context.CancelFunc

	mu      sync.Mutex
	built   bool
	ranking []rankedSession
	// byNode holds the per-node ranked slices, derived from ranking during
	// the same rebuild and swapped with it under mu — one atomic projection.
	// Filtered requests page a prebuilt slice; they never walk the corpus,
	// not even in memory. The tuples are duplicated (~a hundred bytes per
	// session; single-digit MB at a 10^5-session fleet corpus) — a deliberate
	// trade for O(page) filtered requests.
	byNode map[string][]rankedSession
	// byKey is the O(1) single-session lookup over the same snapshot. Pi
	// transcript requests use it so resolving one session does not rescan its
	// complete index before consulting the branch projection.
	byKey map[sessionKey]rankedSession
	stamp rankingStamp
	// rankedInspected counts every ranked entry a request examines during
	// selection and paging — the work-scaling seam the large-corpus gate
	// reads (SQL plans cannot see in-memory walks). Prod cost: one atomic
	// add per request. Any code that iterates ranked entries on the request
	// path MUST charge inspectRanked, or the gate's corpus-walk self-check
	// is the only thing standing between it and silence.
	rankedInspected atomic.Int64
	// refresh is the in-flight single-flighted rebuild, nil when idle.
	refresh *projectionRefresh
	// rebuildHook, when non-nil, runs at each rebuildStage of every rebuild
	// — a test-only choke point (export_test.go) that makes single-flight,
	// serve-stale, and the failure edges provable without timing games. A
	// returned error aborts the rebuild exactly like the query at that stage
	// failing. Guarded by mu; captured into the refresh before its goroutine
	// starts.
	rebuildHook func(rebuildStage) error
}

// rebuildStage names the points in a projection rebuild where the test hook
// interposes.
type rebuildStage int

const (
	rebuildStart      rebuildStage = iota // before any query
	rebuildStamped                        // after the stamp probe, before the ranking query
	rebuildNodeSlices                     // before per-node slice construction (off-lock publication phase)
)

// projectionRefresh is one in-flight projection rebuild. err is set before
// done closes; cold requests (nothing to serve yet) block on done, everyone
// else serves the previous projection without waiting.
type projectionRefresh struct {
	done chan struct{}
	err  error
	hook func(rebuildStage) error
}

// SQLStoreOption configures a SQLStore.
type SQLStoreOption func(*SQLStore)

// WithSQLStoreLogger routes the projection rebuild logs (tests). The default
// is the process-default slog logger — the service journal path, same as the
// Server's degradation events.
func WithSQLStoreLogger(l *slog.Logger) SQLStoreOption {
	return func(s *SQLStore) { s.log = l }
}

// NewSQLStore builds the live Store over the store's database and mirror.
// Callers that shut the database down must Close this store first.
func NewSQLStore(db *sql.DB, mirrorPath MirrorPath, opts ...SQLStoreOption) *SQLStore {
	ctx, cancel := context.WithCancel(context.Background())
	s := &SQLStore{db: db, mirrorPath: mirrorPath, refreshCtx: ctx, cancelRefresh: cancel, log: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Close stops the background refresh machinery: it cancels the refresh
// context and waits until no rebuild is in flight. Run it before closing
// the database this store reads.
func (s *SQLStore) Close() {
	s.cancelRefresh()
	s.waitProjectionIdle()
}

// waitProjectionIdle blocks until no rebuild is in flight. A refresh
// triggered concurrently with Close fails fast on the canceled context and
// clears itself, so the loop terminates.
func (s *SQLStore) waitProjectionIdle() {
	for {
		s.mu.Lock()
		run := s.refresh
		s.mu.Unlock()
		if run == nil {
			return
		}
		<-run.done
	}
}

// mirrorGen is one files-table generation row.
type mirrorGen struct {
	tool      wire.Tool
	wireID    string
	fileUUID  string
	gen       int
	createdAt time.Time
	lastPutAt time.Time
}

// sessionKey names one logical session across the paged read path.
type sessionKey struct {
	tool    wire.Tool
	logical string
}

// rankedSession is one projection entry: the session key, the node label
// the per-node filter selects on, the session's row-count/max-timestamp
// aggregates, and its member file-generation keys. Everything here is the
// ranking-time snapshot; like the ranked order itself it can lag under
// serve-stale (staleness bound: the delta in
// docs/design/2026-07-13-sesh-store-read-write-split.md). The aggregates
// and membership ride the projection deliberately: computing them live
// walked every index row of every listed session per render, and page one
// lists the most recent = largest sessions, so the first page paid
// hundreds of thousands of row visits per render. Page hydration reads
// live tables only for genuinely per-request data — file-generation
// bookkeeping times, node facts, owner claims — each a full-key seek per
// page item, never a per-session row walk.
type rankedSession struct {
	key      sessionKey
	hostname string
	osUser   string
	// messageRows/quarantinedRows/maxTimestampUTC are the rebuild-time
	// per-session aggregates over sesh_index_messages; maxTimestampUTC is
	// nil when no non-quarantined row carries a parsed timestamp.
	messageRows     int
	quarantinedRows int
	indexVersion    int64
	maxTimestampUTC *time.Time
	// members are the session's mirrored file generations as of the rebuild
	// (indexed mapping, wire-claim fallback for unindexed generations) —
	// the keys page hydration seeks the files table with.
	members []memberGen
}

// memberGen names one member file generation of a ranked session; the tool
// lives on the session key.
type memberGen struct {
	wireID   string
	fileUUID string
	gen      int
}

// projectionNodeKey addresses one node's ranked slice.
func projectionNodeKey(hostname, osUser string) string {
	return hostname + "\x00" + osUser
}

// inspectRanked charges n ranked entries against the request-path work
// counter (see the rankedInspected field).
func (s *SQLStore) inspectRanked(n int) {
	s.rankedInspected.Add(int64(n))
}

// deriveNodeSlices builds the per-node ranked slices from the freshly built
// ranking — rebuild-time work, amortized exactly like the ranking query,
// and one publication phase with its hook: it must run OFF the projection
// mutex (the choke gate parks rebuilds here; if this phase ever moves under
// s.mu, parked rebuilds hold the request mutex and the serve-stale gates
// hang loudly instead of passing — verified by running exactly that
// regression against the gate).
func (s *SQLStore) deriveNodeSlices(run *projectionRefresh, ranking []rankedSession) (map[string][]rankedSession, error) {
	if run.hook != nil {
		if err := run.hook(rebuildNodeSlices); err != nil {
			return nil, err
		}
	}
	out := map[string][]rankedSession{}
	for _, r := range ranking {
		k := projectionNodeKey(r.hostname, r.osUser)
		out[k] = append(out[k], r)
	}
	return out, nil
}

// rankingStamp is the cheap store version probe guarding the projection:
// ranking inputs only ever arrive as INSERTs (index rows are appended, file
// generations are new rows, fact observations are an append-only log; the
// drop-file repair runs with serve stopped), so three b-tree MAX lookups
// detect every change the ranking can see — facts included, because the
// projection carries each session's node label for the per-node filter.
type rankingStamp struct {
	indexMax int64
	filesMax int64
	factsMax int64
}

// rankingStampSQL reads the MAX probes in one round trip. Keep the text
// distinctive: the large-corpus gate whitelists it when proving that warm
// requests never scan a corpus table.
const rankingStampSQL = `SELECT
	COALESCE((SELECT MAX(id) FROM sesh_index_messages), 0),
	COALESCE((SELECT MAX(rowid) FROM files), 0),
	COALESCE((SELECT MAX(id) FROM fact_observations), 0)`

// RecentSessions returns one page of logical sessions, most recent first by
// the R14 instant. The page is a slice of the maintained recency projection
// — the fleet's corpus (thousands of files per node) is never materialized
// per request — and only the page's sessions are hydrated, by key.
func (s *SQLStore) RecentSessions(ctx context.Context, limit, offset int) ([]SessionSummary, int, error) {
	ranking, _, err := s.projectionSnapshot(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.pageOf(ctx, ranking, limit, offset)
}

// RecentSessionsByNode is RecentSessions filtered to one node label
// (hostname, OS user). It pages the node's PREBUILT ranked slice — derived
// during the same single-flighted rebuild and swapped atomically with the
// global ranking — so the filter adds no SQL ranking path, no corpus scan,
// and no per-request corpus walk, in SQL or in memory. total is the node's
// session count.
//
// Filter/display consistency: the filter selected on the projection's node
// label, so the response RENDERS that label too — one snapshot for select
// and display. Label hydration reads live facts, which can have moved since
// the snapshot (serve-stale); without this override one response could list
// a session under node A while labeling its row B. The stale read has
// already triggered the refresh that re-homes the session on a later
// request. The unfiltered list keeps live-hydrated labels — it has no
// filter invariant to hold.
func (s *SQLStore) RecentSessionsByNode(ctx context.Context, hostname, osUser string, limit, offset int) ([]SessionSummary, int, error) {
	_, byNode, err := s.projectionSnapshot(ctx)
	if err != nil {
		return nil, 0, err
	}
	sums, total, err := s.pageOf(ctx, byNode[projectionNodeKey(hostname, osUser)], limit, offset)
	if err != nil {
		return nil, 0, err
	}
	for i := range sums {
		sums[i].Hostname, sums[i].OSUser = hostname, osUser
	}
	return sums, total, nil
}

// pageOf slices one page out of a ranked list and hydrates exactly those
// sessions from their projection entries.
func (s *SQLStore) pageOf(ctx context.Context, ranking []rankedSession, limit, offset int) ([]SessionSummary, int, error) {
	total := len(ranking)
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	page := ranking[offset:]
	if len(page) > limit {
		page = page[:limit]
	}
	// One charge per page entry per request: this covers every examination
	// of these entries downstream (key collection and assembly in
	// hydrateRankedPage receive exactly this slice and can touch nothing
	// beyond it), so the work-scaling gate's bound stays the slice size.
	s.inspectRanked(len(page))
	sums, err := s.hydrateRankedPage(ctx, page)
	if err != nil {
		return nil, 0, err
	}
	return sums, total, nil
}

// projectionSnapshot returns the current recency projection: the global
// ranked list and the per-node ranked slices, one consistent snapshot.
// Steady state (no new bytes since the last render) costs one probe; a
// moved stamp serves the existing projection immediately and triggers the
// single-flighted background refresh, so no request after the cold start
// ever waits on a corpus-scale rebuild.
func (s *SQLStore) projectionSnapshot(ctx context.Context) ([]rankedSession, map[string][]rankedSession, error) {
	var stamp rankingStamp
	if err := s.db.QueryRowContext(ctx, rankingStampSQL).Scan(&stamp.indexMax, &stamp.filesMax, &stamp.factsMax); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	if s.built {
		ranking, byNode := s.ranking, s.byNode
		if stamp != s.stamp {
			s.startRefreshLocked()
		}
		s.mu.Unlock()
		return ranking, byNode, nil
	}
	// Cold start: nothing to serve stale — join the single-flighted first
	// build so concurrent cold requests share one rebuild.
	run := s.startRefreshLocked()
	s.mu.Unlock()
	select {
	case <-run.done:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	if run.err != nil {
		return nil, nil, run.err
	}
	s.mu.Lock()
	ranking, byNode := s.ranking, s.byNode
	s.mu.Unlock()
	return ranking, byNode, nil
}

// startRefreshLocked returns the in-flight rebuild, starting one when idle.
// Callers hold s.mu.
func (s *SQLStore) startRefreshLocked() *projectionRefresh {
	if s.refresh != nil {
		return s.refresh
	}
	run := &projectionRefresh{done: make(chan struct{}), hook: s.rebuildHook}
	s.refresh = run
	go s.runRefresh(run)
	return run
}

// runRefresh executes one projection rebuild off the request path, on the
// store-lifetime refresh context: the request that triggered it returns
// (stale) long before the rebuild finishes, and Close — not request
// lifetimes — owns the goroutine. The duration lands in the debug journal —
// identifier-free by construction (a duration and a count), same contract
// as the per-request timing.
func (s *SQLStore) runRefresh(run *projectionRefresh) {
	start := time.Now()
	var stamp rankingStamp
	var ranking []rankedSession
	var byNode map[string][]rankedSession
	var byKey map[sessionKey]rankedSession
	err := func() error {
		if run.hook != nil {
			if err := run.hook(rebuildStart); err != nil {
				return err
			}
		}
		// Stamp before ranking: a write landing between the two reads leaves
		// the stored stamp conservative, so the next probe sees it as moved
		// and refreshes again — changes are never silently absorbed (at the
		// cost of one extra rebuild when churn straddles this gap).
		if err := s.db.QueryRowContext(s.refreshCtx, rankingStampSQL).Scan(&stamp.indexMax, &stamp.filesMax, &stamp.factsMax); err != nil {
			return err
		}
		if run.hook != nil {
			if err := run.hook(rebuildStamped); err != nil {
				return err
			}
		}
		var err error
		ranking, err = s.rankSessions(s.refreshCtx)
		if err != nil {
			return err
		}
		// Membership snapshot in the same work phase. The two corpus reads
		// are not one transaction; a write landing between them can leave a
		// ranked session without members (hydration then skips it, honest
		// absence) or an orphan membership (ignored) for one projection
		// lifetime — the conservative stamp above already forces the
		// re-verifying rebuild that converges it, same doctrine as churn
		// straddling the stamp/ranking gap.
		members, err := s.projectionMembers(s.refreshCtx)
		if err != nil {
			return err
		}
		for i := range ranking {
			ranking[i].members = members[ranking[i].key]
		}
		byNode, err = s.deriveNodeSlices(run, ranking)
		if err != nil {
			return err
		}
		byKey = make(map[sessionKey]rankedSession, len(ranking))
		for _, ranked := range ranking {
			byKey[ranked.key] = ranked
		}
		return nil
	}()
	s.mu.Lock()
	if err == nil {
		// One atomic swap under mu: the global ranking, its per-node
		// slices, and the stamp always describe the same snapshot. ONLY the
		// pointer/header assignments happen under the mutex — every
		// corpus-scale phase, slice construction included, ran above,
		// outside the lock warm requests take.
		s.built, s.ranking, s.byNode, s.byKey, s.stamp = true, ranking, byNode, byKey, stamp
	}
	run.err = err
	s.refresh = nil
	s.mu.Unlock()
	close(run.done)
	switch {
	case err == nil:
		s.log.Debug("recency projection rebuild", "duration", time.Since(start), "sessions", len(ranking))
	case s.refreshCtx.Err() != nil:
		// Shutdown races a triggered refresh by design; not a failure.
		s.log.Debug("recency projection rebuild canceled by close", "duration", time.Since(start))
	default:
		// Stale keeps serving; the next request that sees a moved stamp
		// retries. Cold waiters got the error through run.err. The error is
		// journaled as a class, never verbatim: a SQL/pool error can embed
		// the DB path, which on a live node carries the OS user's home.
		s.log.Warn("recency projection rebuild failed", "duration", time.Since(start), "error_class", errClass(err))
	}
}

// rankSessions is the projection rebuild's ranking pass: every logical
// session ranked by the R14 recency instant — max parsed non-quarantined
// timestamp, first-ingest when none — entirely in SQL, each row carrying
// the key, the node label, and the per-session aggregates the sessions
// list renders (row counts, max timestamp). The node label is the latest
// fact observation across the session's member wire sessions, the same
// winner the page hydration picks. This is a deliberately corpus-wide read
// on the surface (its sibling projectionMembers is the other), and it runs
// amortized (stamp + floor), never per request. julianday keeps the
// comparison temporal across RFC3339 fractional-precision variants (same
// posture as the single-session max-timestamp lookup below); the
// tool+logical tie-break keeps page cuts deterministic. The ts CTE reads
// the max-timestamp ROW's string (not just its julianday, which lacks the
// precision to reconstruct a nanosecond instant) through SQLite's
// documented bare-column-with-a-single-MAX behavior — a window function
// gives an explicit tie-break but measurably slows the corpus pass, and a
// tie on the julian instant can only waver the DISPLAYED timestamp within
// the julianday resolution, never the ranked order (jd ties rank equal).
func (s *SQLStore) rankSessions(ctx context.Context) ([]rankedSession, error) {
	rows, err := s.db.QueryContext(ctx, `WITH mapped AS (
			SELECT DISTINCT tool, wire_session_id, file_uuid, generation, logical_session_id
			FROM sesh_index_messages
		),
		members AS (
			SELECT f.tool AS tool,
				COALESCE(m.logical_session_id, f.session_id) AS logical,
				f.session_id AS wire_session_id,
				f.created_at AS created_at
			FROM files f
			LEFT JOIN mapped m
				ON m.tool = f.tool AND m.wire_session_id = f.session_id
				AND m.file_uuid = f.file_uuid AND m.generation = f.generation
		),
		sess AS (
			SELECT tool, logical, MIN(julianday(created_at)) AS first_ingest_jd
			FROM members
			GROUP BY tool, logical
		),
		node_fact AS (
			SELECT mb.tool AS tool, mb.logical AS logical, MAX(fo.id) AS fact_id
			FROM (SELECT DISTINCT tool, logical, wire_session_id FROM members) mb
			JOIN fact_observations fo
				ON fo.tool = mb.tool AND fo.session_id = mb.wire_session_id
			GROUP BY mb.tool, mb.logical
		),
		counts AS (
			SELECT tool, logical_session_id AS logical,
				SUM(CASE WHEN quarantine = 0 AND NOT (tool = 'claude' AND role = 'meta' AND entry_type IN
					('agent-name', 'ai-title', 'bridge-session', 'file-history-snapshot', 'last-prompt', 'mode',
					 'permission-mode', 'pr-link', 'queue-operation', 'worktree-state')) THEN 1 ELSE 0 END) AS message_rows,
				SUM(CASE WHEN quarantine = 1 THEN 1 ELSE 0 END) AS quarantined_rows,
				MAX(id) AS index_version
			FROM sesh_index_messages
			GROUP BY tool, logical_session_id
		),
		ts AS (
			SELECT tool, logical_session_id AS logical, timestamp_utc,
				MAX(julianday(timestamp_utc)) AS max_ts_jd
			FROM sesh_index_messages
			WHERE quarantine = 0 AND timestamp_utc IS NOT NULL
			GROUP BY tool, logical_session_id
		)
		SELECT sess.tool, sess.logical,
			COALESCE(fo.hostname, ''), COALESCE(fo.os_user, ''),
			COALESCE(c.message_rows, 0), COALESCE(c.quarantined_rows, 0),
			COALESCE(c.index_version, 0),
			COALESCE(ts.timestamp_utc, '')
		FROM sess
		LEFT JOIN node_fact nf ON nf.tool = sess.tool AND nf.logical = sess.logical
		LEFT JOIN fact_observations fo ON fo.id = nf.fact_id
		LEFT JOIN counts c ON c.tool = sess.tool AND c.logical = sess.logical
		LEFT JOIN ts ON ts.tool = sess.tool AND ts.logical = sess.logical
		ORDER BY COALESCE(ts.max_ts_jd, sess.first_ingest_jd) DESC, sess.tool, sess.logical`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rankedSession
	for rows.Next() {
		var r rankedSession
		var maxTS string
		if err := rows.Scan(&r.key.tool, &r.key.logical, &r.hostname, &r.osUser,
			&r.messageRows, &r.quarantinedRows, &r.indexVersion, &maxTS); err != nil {
			return nil, err
		}
		if maxTS != "" {
			// Unparseable timestamps degrade to the first-ingest fallback,
			// the same posture as the live max-timestamp lookup.
			if t, err := time.Parse(time.RFC3339Nano, maxTS); err == nil {
				u := t.UTC()
				r.maxTimestampUTC = &u
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// projectionMembers is the projection rebuild's membership pass: every
// mirrored file generation resolved to its logical session — the indexed
// mapping when the index holds rows for the generation, the wire claim
// otherwise (honest fallback, same rule as the live memberGenerations) —
// so page hydration seeks the files table by exact generation keys instead
// of re-deriving membership from a per-session index-row walk. Corpus-wide
// by design, amortized exactly like rankSessions; the member_of_logical
// alias is the marker the plan gate whitelists rebuild SQL by.
func (s *SQLStore) projectionMembers(ctx context.Context) (map[sessionKey][]memberGen, error) {
	rows, err := s.db.QueryContext(ctx, `WITH mapped AS (
			SELECT DISTINCT tool, wire_session_id, file_uuid, generation, logical_session_id
			FROM sesh_index_messages
		)
		SELECT f.tool, COALESCE(m.logical_session_id, f.session_id) AS member_of_logical,
			f.session_id, f.file_uuid, f.generation
		FROM files f
		LEFT JOIN mapped m
			ON m.tool = f.tool AND m.wire_session_id = f.session_id
			AND m.file_uuid = f.file_uuid AND m.generation = f.generation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[sessionKey][]memberGen{}
	for rows.Next() {
		var key sessionKey
		var g memberGen
		if err := rows.Scan(&key.tool, &key.logical, &g.wireID, &g.fileUUID, &g.gen); err != nil {
			return nil, err
		}
		out[key] = append(out[key], g)
	}
	return out, rows.Err()
}

// Session resolves one logical session by hydrating exactly that key —
// never the full listing.
func (s *SQLStore) Session(ctx context.Context, tool wire.Tool, logicalSessionID string) (SessionSummary, bool, error) {
	if tool == wire.ToolPi {
		if _, _, err := s.projectionSnapshot(ctx); err != nil {
			return SessionSummary{}, false, err
		}
		key := sessionKey{tool, logicalSessionID}
		s.mu.Lock()
		ranked, ok := s.byKey[key]
		s.mu.Unlock()
		if ok {
			sums, err := s.hydrateRankedPage(ctx, []rankedSession{ranked})
			if err != nil {
				return SessionSummary{}, false, err
			}
			if len(sums) == 0 {
				return SessionSummary{}, false, nil
			}
			return sums[0], true, nil
		}
		// A moved stamp deliberately serves the previous projection while
		// refreshing. A miss in that stale snapshot is not authoritative: a
		// newly indexed direct link must use the exact-key live lookup below
		// instead of becoming a false 404. Known projected sessions retain the
		// stale O(1) fast path above.
	}
	sums, err := s.hydrateSessions(ctx, []sessionKey{{tool, logicalSessionID}})
	if err != nil {
		return SessionSummary{}, false, err
	}
	if len(sums) == 0 {
		return SessionSummary{}, false, nil
	}
	return sums[0], true, nil
}

// hydrateSessions assembles full summaries for exactly the given keys, in
// key order, entirely from the live tables. This is the single-session
// path for the legacy transcript adapters: its per-key queries walk the one
// session's index rows. Pi instead resolves from the projection because its
// active branch can be much smaller than the full tree. The sessions LIST
// must never take this path for its page: page one lists the largest sessions,
// and per-listed-session row walks are exactly the cost the projection-carried
// aggregates removed (hydrateRankedPage; the max-size-sessions fixture gate
// pins it).
func (s *SQLStore) hydrateSessions(ctx context.Context, keys []sessionKey) ([]SessionSummary, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	members, err := s.memberGenerations(ctx, keys)
	if err != nil {
		return nil, err
	}
	var wires []wireKey
	seenWire := map[wireKey]bool{}
	for _, gens := range members {
		for _, g := range gens {
			wk := wireKey{g.tool, g.wireID}
			if !seenWire[wk] {
				seenWire[wk] = true
				wires = append(wires, wk)
			}
		}
	}
	counts, err := s.rowCounts(ctx, keys)
	if err != nil {
		return nil, err
	}
	maxTimestamps, err := s.maxTimestamps(ctx, keys)
	if err != nil {
		return nil, err
	}
	facts, err := s.latestFacts(ctx, wires)
	if err != nil {
		return nil, err
	}
	claims, err := s.ownerClaims(ctx, wires)
	if err != nil {
		return nil, err
	}

	var out []SessionSummary
	for _, key := range keys {
		gens := members[key]
		if len(gens) == 0 {
			continue // unknown session: lookup miss, honest absence
		}
		sum := assembleSummary(key, gens, facts, claims)
		if c, ok := counts[countKey(key.tool, key.logical)]; ok {
			sum.MessageRows, sum.QuarantinedRows = c.messages, c.quarantined
			sum.IndexVersion = c.indexVersion
		}
		if ts, ok := maxTimestamps[countKey(key.tool, key.logical)]; ok {
			sum.MaxTimestampUTC = ts
		}
		out = append(out, sum)
	}
	return out, nil
}

// hydrateRankedPage assembles summaries for one page of projection entries.
// The aggregates (row counts, max timestamp) and the membership come from
// the entries themselves — the ranking-time snapshot, staleness bounded by
// the serve-stale doctrine — and only genuinely per-request data is read
// live: file-generation bookkeeping times (last_put_at moves on every
// accepted PUT and renders as "mirrored at"), node facts, owner claims.
// Every live read is a full-key seek per page item; nothing here touches
// sesh_index_messages, so the page's cost is independent of how large its
// listed sessions are (the max-size-sessions fixture gate pins exactly
// that).
func (s *SQLStore) hydrateRankedPage(ctx context.Context, page []rankedSession) ([]SessionSummary, error) {
	if len(page) == 0 {
		return nil, nil
	}
	var fileKeys []fileGenKey
	seenFile := map[fileGenKey]bool{}
	var wires []wireKey
	seenWire := map[wireKey]bool{}
	for _, r := range page {
		for _, m := range r.members {
			fk := fileGenKey{r.key.tool, m.wireID, m.fileUUID, m.gen}
			if !seenFile[fk] {
				seenFile[fk] = true
				fileKeys = append(fileKeys, fk)
			}
			wk := wireKey{r.key.tool, m.wireID}
			if !seenWire[wk] {
				seenWire[wk] = true
				wires = append(wires, wk)
			}
		}
	}
	gens, err := s.fileGenerations(ctx, fileKeys)
	if err != nil {
		return nil, err
	}
	facts, err := s.latestFacts(ctx, wires)
	if err != nil {
		return nil, err
	}
	claims, err := s.ownerClaims(ctx, wires)
	if err != nil {
		return nil, err
	}

	var out []SessionSummary
	for _, r := range page {
		var mgens []mirrorGen
		for _, m := range r.members {
			if g, ok := gens[fileGenKey{r.key.tool, m.wireID, m.fileUUID, m.gen}]; ok {
				mgens = append(mgens, g)
			}
			// A member the files table no longer holds vanished since the
			// rebuild (drop-file repair runs with serve stopped, but stay
			// honest): skip it rather than render bookkeeping we don't have.
		}
		if len(mgens) == 0 {
			continue // session vanished since the rebuild: honest absence
		}
		sum := assembleSummary(r.key, mgens, facts, claims)
		sum.MessageRows, sum.QuarantinedRows = r.messageRows, r.quarantinedRows
		sum.IndexVersion = r.indexVersion
		sum.MaxTimestampUTC = r.maxTimestampUTC
		out = append(out, sum)
	}
	return out, nil
}

// assembleSummary builds the summary fields both hydration paths share from
// a session's member generations plus the facts/claims lookups: the file
// list in first-ingest order, the ingest/mirror instants, the node facts
// winner (max fact id), and the deduplicated owner claims. Row-count and
// max-timestamp aggregates are the caller's to fill — live queries on the
// single-session path, the projection snapshot on the page path.
func assembleSummary(key sessionKey, gens []mirrorGen, facts map[string]factRow, claims map[string][]string) SessionSummary {
	sort.Slice(gens, func(i, j int) bool {
		a, b := gens[i], gens[j]
		if !a.createdAt.Equal(b.createdAt) {
			return a.createdAt.Before(b.createdAt)
		}
		if a.fileUUID != b.fileUUID {
			return a.fileUUID < b.fileUUID
		}
		return a.gen < b.gen
	})
	sum := SessionSummary{
		Tool:             key.tool,
		LogicalSessionID: key.logical,
		FirstIngestAt:    gens[0].createdAt,
	}
	var factID int64 = -1
	seenClaim := map[string]bool{}
	for _, g := range gens {
		sum.Files = append(sum.Files, FileRef{
			WireSessionID: g.wireID,
			FileUUID:      g.fileUUID,
			Generation:    g.gen,
			FirstIngestAt: g.createdAt,
		})
		if g.lastPutAt.After(sum.MirroredAt) {
			sum.MirroredAt = g.lastPutAt
		}
		if f, ok := facts[factKey(g.tool, g.wireID)]; ok && f.id > factID {
			factID = f.id
			sum.Hostname, sum.OSUser = f.hostname, f.osUser
			sum.TailnetIdentity = f.tailnetIdentity
		}
		for _, c := range claims[factKey(g.tool, g.wireID)] {
			if !seenClaim[c] {
				seenClaim[c] = true
				sum.OwnerClaims = append(sum.OwnerClaims, c)
			}
		}
	}
	return sum
}

// Rows returns the session's index rows in storage order; the surface
// applies the frozen transcript ordering itself.
func (s *SQLStore) Rows(ctx context.Context, tool wire.Tool, logicalSessionID string) ([]wire.IndexMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
			id, tool, logical_session_id, wire_session_id, entry_type, message_uuid,
			file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal,
			byte_start, byte_end, quarantine, quarantine_reason
		FROM sesh_index_messages WHERE tool = ? AND logical_session_id = ?`,
		tool, logicalSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []wire.IndexMessage
	for rows.Next() {
		var (
			m          wire.IndexMessage
			ts         sql.NullString
			quarantine int
		)
		if err := rows.Scan(&m.ID, &m.Tool, &m.LogicalSessionID, &m.WireSessionID,
			&m.EntryType, &m.MessageUUID, &m.FileUUID, &m.Generation, &m.Role, &ts,
			&m.FileOrdinal, &m.LineOrdinal, &m.ByteStart, &m.ByteEnd, &quarantine,
			&m.QuarantineReason); err != nil {
			return nil, err
		}
		m.Quarantine = quarantine != 0
		if ts.Valid && ts.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, ts.String); err == nil {
				t = t.UTC()
				m.TimestampUTC = &t
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MirrorRange reads mirrored bytes [start, end) of one file generation,
// clamped to what the mirror currently holds.
func (s *SQLStore) MirrorRange(_ context.Context, tool wire.Tool, wireSessionID, fileUUID string, generation int, start, end int64) ([]byte, error) {
	f, err := os.Open(s.mirrorPath(tool, wireSessionID, fileUUID, generation))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if start < 0 || start > st.Size() {
		return nil, fmt.Errorf("range start %d out of mirror size %d", start, st.Size())
	}
	if end > st.Size() {
		end = st.Size()
	}
	buf := make([]byte, end-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

// MirrorFile streams a whole mirrored file generation.
func (s *SQLStore) MirrorFile(_ context.Context, tool wire.Tool, wireSessionID, fileUUID string, generation int) (io.ReadCloser, error) {
	return os.Open(s.mirrorPath(tool, wireSessionID, fileUUID, generation))
}

// Nodes returns last-PUT activity by hostname and OS user for the nodes view.
func (s *SQLStore) Nodes(ctx context.Context, staleAfter time.Duration) ([]NodeStatus, error) {
	// Small-table class: last_seen is one row per node, so the full scan +
	// sort stays proportional to fleet size, not the corpus. COALESCE keeps
	// pre-census NULL rows scanning as "" (rendered unknown).
	rows, err := s.db.QueryContext(ctx, `SELECT hostname, os_user, last_put_at, COALESCE(shipper_version, '') FROM last_seen ORDER BY hostname, os_user`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	var out []NodeStatus
	for rows.Next() {
		var n NodeStatus
		var raw string
		if err := rows.Scan(&n.Hostname, &n.OSUser, &raw, &n.ShipperVersion); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, err
		}
		n.LastPutAt = t.UTC()
		age := now.Sub(n.LastPutAt)
		n.Age = age.Round(time.Second).String()
		n.Stale = age > staleAfter
		out = append(out, n)
	}
	return out, rows.Err()
}

// --- queries (each fully drains its result set before the next runs; every
// one is constrained to the page's keys so no request scans the corpus) ---

func countKey(tool wire.Tool, logical string) string {
	return string(tool) + "\x00" + logical
}

func factKey(tool wire.Tool, wireID string) string {
	return string(tool) + "\x00" + wireID
}

// wireKey names one wire session (the facts log's addressing).
type wireKey struct {
	tool   wire.Tool
	wireID string
}

// keyValuesClause renders a requested-keys derived table — `(VALUES (?, ?),
// …)` — plus its bind args for a set of two-column keys. Hydration queries
// JOIN it (columns column1/column2) instead of using a row-value IN, so the
// per-key equality terms reach the named index as a full-key seek.
func keyValuesClause(pairs [][2]any) (string, []any) {
	var b strings.Builder
	args := make([]any, 0, 2*len(pairs))
	b.WriteString("(VALUES ")
	for i, p := range pairs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(?, ?)")
		args = append(args, p[0], p[1])
	}
	b.WriteString(")")
	return b.String(), args
}

func sessionKeyValues(keys []sessionKey) (string, []any) {
	pairs := make([][2]any, len(keys))
	for i, k := range keys {
		pairs[i] = [2]any{string(k.tool), k.logical}
	}
	return keyValuesClause(pairs)
}

func wireKeyValues(keys []wireKey) (string, []any) {
	pairs := make([][2]any, len(keys))
	for i, k := range keys {
		pairs[i] = [2]any{string(k.tool), k.wireID}
	}
	return keyValuesClause(pairs)
}

// fileGenKey addresses one mirrored file generation — the files table's
// primary key.
type fileGenKey struct {
	tool     wire.Tool
	wireID   string
	fileUUID string
	gen      int
}

// fileKeyValues is keyValuesClause for the four-column generation keys
// (columns column1..column4).
func fileKeyValues(keys []fileGenKey) (string, []any) {
	var b strings.Builder
	args := make([]any, 0, 4*len(keys))
	b.WriteString("(VALUES ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(?, ?, ?, ?)")
		args = append(args, string(k.tool), k.wireID, k.fileUUID, k.gen)
	}
	b.WriteString(")")
	return b.String(), args
}

// fileGenerations reads the bookkeeping times of exactly the given file
// generations — one full-key seek on the files primary key per requested
// generation. This is the page path's only files access: membership itself
// came from the projection snapshot. INDEXED BY pins the primary-key
// autoindex so the optimizer can never drift to files_identity_fingerprint
// and seek a 3-column prefix whose cost grows with generations per wire
// session (see memberGenerations for the pin rationale); the max-size
// fixture gate asserts this exact plan term-by-term, all four key columns,
// with a query-specific check — the shared allowlist tolerates shorter
// files-PK prefixes that older shapes legitimately use.
func (s *SQLStore) fileGenerations(ctx context.Context, keys []fileGenKey) (map[fileGenKey]mirrorGen, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	clause, args := fileKeyValues(keys)
	rows, err := s.db.QueryContext(ctx, `SELECT f.tool, f.session_id, f.file_uuid, f.generation,
			COALESCE(f.created_at, ''), COALESCE(f.last_put_at, '')
		FROM `+clause+` AS k
		JOIN files f INDEXED BY sqlite_autoindex_files_1
			ON f.tool = k.column1 AND f.session_id = k.column2
			AND f.file_uuid = k.column3 AND f.generation = k.column4`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[fileGenKey]mirrorGen{}
	for rows.Next() {
		var g mirrorGen
		var created, lastPut string
		if err := rows.Scan(&g.tool, &g.wireID, &g.fileUUID, &g.gen, &created, &lastPut); err != nil {
			return nil, err
		}
		g.createdAt = parseStoreTime(created)
		g.lastPutAt = parseStoreTime(lastPut)
		out[fileGenKey{g.tool, g.wireID, g.fileUUID, g.gen}] = g
	}
	return out, rows.Err()
}

// memberGenerations returns the mirrored file generations of exactly the
// given sessions, keyed back to their session. Two index-driven branches
// instead of a corpus-wide mapping pass: generations the index maps to a
// requested logical id, plus unindexed generations whose wire claim IS the
// requested id (honest fallback, matches the schema rule — the mirror is
// truth and the surface must never be blind to it; those render raw).
//
// INDEXED BY is load-bearing on every sesh_index_messages access here and in
// the sibling hydration queries: without it (and without ANALYZE stats) the
// optimizer has been observed picking another index whose projected columns
// feed the later join — e.g. sesh_index_messages_file or _overlap — seeking
// only its tool=? prefix and walking every message row of the tool per
// request. Pinning the index keeps the plan a full-key seek per requested
// key, and turns index drift into a hard query error instead of a silent
// corpus walk. The large-corpus gate asserts the resulting plans
// term-by-term.
func (s *SQLStore) memberGenerations(ctx context.Context, keys []sessionKey) (map[sessionKey][]mirrorGen, error) {
	mappedClause, mappedArgs := sessionKeyValues(keys)
	wireClause, wireArgs := sessionKeyValues(keys)
	rows, err := s.db.QueryContext(ctx, `SELECT mk.logical, f.tool, f.session_id, f.file_uuid, f.generation,
			COALESCE(f.created_at, ''), COALESCE(f.last_put_at, '')
		FROM (
			SELECT DISTINCT m.tool AS tool, m.wire_session_id AS wire_session_id,
				m.file_uuid AS file_uuid, m.generation AS generation,
				m.logical_session_id AS logical
			FROM `+mappedClause+` AS k
			JOIN sesh_index_messages m INDEXED BY sesh_index_messages_logical
				ON m.tool = k.column1 AND m.logical_session_id = k.column2
		) mk
		JOIN files f ON f.tool = mk.tool AND f.session_id = mk.wire_session_id
			AND f.file_uuid = mk.file_uuid AND f.generation = mk.generation
		UNION ALL
		SELECT f.session_id, f.tool, f.session_id, f.file_uuid, f.generation,
			COALESCE(f.created_at, ''), COALESCE(f.last_put_at, '')
		FROM `+wireClause+` AS k
		JOIN files f ON f.tool = k.column1 AND f.session_id = k.column2
		WHERE NOT EXISTS (
			SELECT 1 FROM sesh_index_messages m INDEXED BY sesh_index_messages_file
			WHERE m.tool = f.tool AND m.wire_session_id = f.session_id
				AND m.file_uuid = f.file_uuid AND m.generation = f.generation
		)`, append(mappedArgs, wireArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[sessionKey][]mirrorGen{}
	for rows.Next() {
		var g mirrorGen
		var logical, created, lastPut string
		if err := rows.Scan(&logical, &g.tool, &g.wireID, &g.fileUUID, &g.gen, &created, &lastPut); err != nil {
			return nil, err
		}
		g.createdAt = parseStoreTime(created)
		g.lastPutAt = parseStoreTime(lastPut)
		key := sessionKey{g.tool, logical}
		out[key] = append(out[key], g)
	}
	return out, rows.Err()
}

type rowCount struct {
	messages     int
	quarantined  int
	indexVersion int64
}

func (s *SQLStore) rowCounts(ctx context.Context, keys []sessionKey) (map[string]rowCount, error) {
	clause, args := sessionKeyValues(keys)
	// Requested-keys join + INDEXED BY: full-key seeks only (see
	// memberGenerations for why the pin is load-bearing).
	rows, err := s.db.QueryContext(ctx, `SELECT m.tool, m.logical_session_id,
			COALESCE(SUM(CASE WHEN m.quarantine = 0 AND NOT (m.tool = 'claude' AND m.role = 'meta' AND m.entry_type IN
				('agent-name', 'ai-title', 'bridge-session', 'file-history-snapshot', 'last-prompt', 'mode',
				 'permission-mode', 'pr-link', 'queue-operation', 'worktree-state')) THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN m.quarantine = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(MAX(m.id), 0)
		FROM `+clause+` AS k
		JOIN sesh_index_messages m INDEXED BY sesh_index_messages_logical
			ON m.tool = k.column1 AND m.logical_session_id = k.column2
		GROUP BY m.tool, m.logical_session_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]rowCount{}
	for rows.Next() {
		var tool wire.Tool
		var logical string
		var c rowCount
		if err := rows.Scan(&tool, &logical, &c.messages, &c.quarantined, &c.indexVersion); err != nil {
			return nil, err
		}
		out[countKey(tool, logical)] = c
	}
	return out, rows.Err()
}

type factRow struct {
	id              int64
	hostname        string
	osUser          string
	tailnetIdentity string
}

// latestFacts returns the most recent (hostname, os_user) observation per
// given wire session. Facts are an append-only observation log; "latest"
// here picks the node label for grouping, it never rewrites owner facts (U10
// owns owner precedence).
func (s *SQLStore) latestFacts(ctx context.Context, keys []wireKey) (map[string]factRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	clause, args := wireKeyValues(keys)
	// Requested-keys join + INDEXED BY: full-key seeks only (see
	// memberGenerations for why the pin is load-bearing); the outer lookup
	// resolves each winning observation by rowid.
	rows, err := s.db.QueryContext(ctx, `SELECT tool, session_id, hostname, os_user, COALESCE(tailnet_identity, ''), id
		FROM fact_observations
		WHERE id IN (
			SELECT MAX(fo.id)
			FROM `+clause+` AS k
			JOIN fact_observations fo INDEXED BY fact_observations_session
				ON fo.tool = k.column1 AND fo.session_id = k.column2
			GROUP BY fo.tool, fo.session_id
		)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]factRow{}
	for rows.Next() {
		var tool wire.Tool
		var wireID string
		var f factRow
		if err := rows.Scan(&tool, &wireID, &f.hostname, &f.osUser, &f.tailnetIdentity, &f.id); err != nil {
			return nil, err
		}
		out[factKey(tool, wireID)] = f
	}
	return out, rows.Err()
}

// ownerClaims returns the distinct SESSION_OWNER observations per given wire
// session in first-observed order. Raw claims only — precedence and
// conflict handling are owner.go's view-time job (R15, I1).
func (s *SQLStore) ownerClaims(ctx context.Context, keys []wireKey) (map[string][]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	clause, args := wireKeyValues(keys)
	// Requested-keys join + INDEXED BY: full-key seeks only (see
	// memberGenerations for why the pin is load-bearing).
	rows, err := s.db.QueryContext(ctx, `SELECT fo.tool, fo.session_id, fo.session_owner, MIN(fo.id) AS first_id
		FROM `+clause+` AS k
		JOIN fact_observations fo INDEXED BY fact_observations_session
			ON fo.tool = k.column1 AND fo.session_id = k.column2
		WHERE fo.session_owner IS NOT NULL AND fo.session_owner <> ''
		GROUP BY fo.tool, fo.session_id, fo.session_owner
		ORDER BY first_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var tool wire.Tool
		var wireID, owner string
		var firstID int64
		if err := rows.Scan(&tool, &wireID, &owner, &firstID); err != nil {
			return nil, err
		}
		key := factKey(tool, wireID)
		out[key] = append(out[key], owner)
	}
	return out, rows.Err()
}

func (s *SQLStore) maxTimestamps(ctx context.Context, keys []sessionKey) (map[string]*time.Time, error) {
	clause, args := sessionKeyValues(keys)
	// Requested-keys join + INDEXED BY: full-key seeks only (see
	// memberGenerations for why the pin is load-bearing).
	rows, err := s.db.QueryContext(ctx, `SELECT tool, logical_session_id, timestamp_utc FROM (
			SELECT m.tool AS tool, m.logical_session_id AS logical_session_id,
				m.timestamp_utc AS timestamp_utc,
				ROW_NUMBER() OVER (
					PARTITION BY m.tool, m.logical_session_id
					ORDER BY julianday(m.timestamp_utc) DESC, m.timestamp_utc DESC
				) AS rn
			FROM `+clause+` AS k
			JOIN sesh_index_messages m INDEXED BY sesh_index_messages_logical
				ON m.tool = k.column1 AND m.logical_session_id = k.column2
			WHERE m.quarantine = 0 AND m.timestamp_utc IS NOT NULL
		) WHERE rn = 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*time.Time{}
	for rows.Next() {
		var tool wire.Tool
		var logical, raw string
		if err := rows.Scan(&tool, &logical, &raw); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			continue
		}
		u := t.UTC()
		out[countKey(tool, logical)] = &u
	}
	return out, rows.Err()
}

func parseStoreTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
