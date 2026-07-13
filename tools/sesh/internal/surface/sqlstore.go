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
// (tool, logical) key list plus total, held in memory and rebuilt only when
// the store's cheap version stamp moves. Request-time work therefore stays
// proportional to the page — a stamp probe plus key-constrained hydration.
//
// The rebuild is single-flighted and serve-stale: at most one rebuild runs
// at a time, and a request that observes a moved stamp returns the existing
// projection immediately while the refresh runs in the background. Only the
// cold start (no projection yet) blocks, and every concurrent cold request
// shares that one build. This deliberately supersedes the original
// read-your-own-writes property (rebuild inline whenever the stamp moved):
// under bulk ingest the stamp moves between every request, which degenerated
// to a corpus-scale rebuild per page load. Only the ranked key list and its
// total can lag — page hydration always reads the live tables — and the lag
// is bounded for any watched page because every request that sees a moved
// stamp triggers a refresh: at most one poll interval plus one rebuild
// behind the store, converging within one rebuild once ingest quiesces (see
// the README surface section and the delta in
// docs/design/2026-07-13-sesh-store-read-write-split.md).
type SQLStore struct {
	db         *sql.DB
	mirrorPath MirrorPath

	mu      sync.Mutex
	built   bool
	ranking []sessionKey
	stamp   rankingStamp
	// refresh is the in-flight single-flighted rebuild, nil when idle.
	refresh *projectionRefresh
	// rebuildBarrier, when non-nil, runs at the start of every rebuild —
	// a test-only choke point (export_test.go) that makes the single-flight
	// and serve-stale behavior provable without timing games. Guarded by mu;
	// captured into the refresh before its goroutine starts.
	rebuildBarrier func()
}

// projectionRefresh is one in-flight projection rebuild. err is set before
// done closes; cold requests (nothing to serve yet) block on done, everyone
// else serves the previous projection without waiting.
type projectionRefresh struct {
	done    chan struct{}
	err     error
	barrier func()
}

// NewSQLStore builds the live Store over the store's database and mirror.
func NewSQLStore(db *sql.DB, mirrorPath MirrorPath) *SQLStore {
	return &SQLStore{db: db, mirrorPath: mirrorPath}
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

// rankingStamp is the cheap store version probe guarding the projection:
// ranking inputs only ever arrive as INSERTs (index rows are appended, file
// generations are new rows; the drop-file repair runs with serve stopped),
// so two b-tree MAX lookups detect every change the ranking can see.
type rankingStamp struct {
	indexMax int64
	filesMax int64
}

// rankingStampSQL reads both MAX probes in one round trip. Keep the text
// distinctive: the large-corpus gate whitelists it when proving that warm
// requests never scan a corpus table.
const rankingStampSQL = `SELECT
	COALESCE((SELECT MAX(id) FROM sesh_index_messages), 0),
	COALESCE((SELECT MAX(rowid) FROM files), 0)`

// RecentSessions returns one page of logical sessions, most recent first by
// the R14 instant. The page is a slice of the maintained recency projection
// — the fleet's corpus (thousands of files per node) is never materialized
// per request — and only the page's sessions are hydrated, by key.
func (s *SQLStore) RecentSessions(ctx context.Context, limit, offset int) ([]SessionSummary, int, error) {
	ranking, err := s.rankedKeys(ctx)
	if err != nil {
		return nil, 0, err
	}
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
	sums, err := s.hydrateSessions(ctx, page)
	if err != nil {
		return nil, 0, err
	}
	return sums, total, nil
}

// rankedKeys returns the current recency projection. Steady state (no new
// bytes since the last render) costs one probe; a moved stamp serves the
// existing projection immediately and triggers the single-flighted
// background refresh, so no request after the cold start ever waits on a
// corpus-scale rebuild.
func (s *SQLStore) rankedKeys(ctx context.Context) ([]sessionKey, error) {
	var stamp rankingStamp
	if err := s.db.QueryRowContext(ctx, rankingStampSQL).Scan(&stamp.indexMax, &stamp.filesMax); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.built {
		ranking := s.ranking
		if stamp != s.stamp {
			s.startRefreshLocked()
		}
		s.mu.Unlock()
		return ranking, nil
	}
	// Cold start: nothing to serve stale — join the single-flighted first
	// build so concurrent cold requests share one rebuild.
	run := s.startRefreshLocked()
	s.mu.Unlock()
	select {
	case <-run.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if run.err != nil {
		return nil, run.err
	}
	s.mu.Lock()
	ranking := s.ranking
	s.mu.Unlock()
	return ranking, nil
}

// startRefreshLocked returns the in-flight rebuild, starting one when idle.
// Callers hold s.mu.
func (s *SQLStore) startRefreshLocked() *projectionRefresh {
	if s.refresh != nil {
		return s.refresh
	}
	run := &projectionRefresh{done: make(chan struct{}), barrier: s.rebuildBarrier}
	s.refresh = run
	go s.runRefresh(run)
	return run
}

// runRefresh executes one projection rebuild off the request path. It runs
// on context.Background deliberately: the request that triggered it returns
// (stale) long before the rebuild finishes, and the refresh must outlive it.
// The duration lands in the debug journal — identifier-free by construction
// (a duration and a count), same contract as the per-request timing.
func (s *SQLStore) runRefresh(run *projectionRefresh) {
	if run.barrier != nil {
		run.barrier()
	}
	ctx := context.Background()
	start := time.Now()
	// Stamp before ranking: a write landing between the two reads leaves the
	// stored stamp conservative, so the next probe sees it as moved and
	// refreshes again — changes are never silently absorbed.
	var stamp rankingStamp
	err := s.db.QueryRowContext(ctx, rankingStampSQL).Scan(&stamp.indexMax, &stamp.filesMax)
	var ranking []sessionKey
	if err == nil {
		ranking, err = s.rankSessionKeys(ctx)
	}
	s.mu.Lock()
	if err == nil {
		s.built, s.ranking, s.stamp = true, ranking, stamp
	}
	run.err = err
	s.refresh = nil
	s.mu.Unlock()
	close(run.done)
	if err != nil {
		// Stale keeps serving; the next request that sees a moved stamp
		// retries. Cold waiters got the error through run.err.
		slog.Warn("recency projection rebuild failed", "duration", time.Since(start), "error", err)
		return
	}
	slog.Debug("recency projection rebuild", "duration", time.Since(start), "sessions", len(ranking))
}

// rankSessionKeys is the projection rebuild: every logical session ranked by
// the R14 recency instant — max parsed non-quarantined timestamp,
// first-ingest when none — entirely in SQL, keys only. This is the one
// deliberately corpus-wide read on the surface, and it runs amortized
// (stamp + floor), never per request. julianday keeps the comparison
// temporal across RFC3339 fractional-precision variants (same posture as
// the max-timestamp lookup below); the tool+logical tie-break keeps page
// cuts deterministic.
func (s *SQLStore) rankSessionKeys(ctx context.Context) ([]sessionKey, error) {
	rows, err := s.db.QueryContext(ctx, `WITH mapped AS (
			SELECT DISTINCT tool, wire_session_id, file_uuid, generation, logical_session_id
			FROM sesh_index_messages
		),
		sess AS (
			SELECT f.tool AS tool,
				COALESCE(m.logical_session_id, f.session_id) AS logical,
				MIN(julianday(f.created_at)) AS first_ingest_jd
			FROM files f
			LEFT JOIN mapped m
				ON m.tool = f.tool AND m.wire_session_id = f.session_id
				AND m.file_uuid = f.file_uuid AND m.generation = f.generation
			GROUP BY f.tool, COALESCE(m.logical_session_id, f.session_id)
		),
		ts AS (
			SELECT tool, logical_session_id AS logical, MAX(julianday(timestamp_utc)) AS max_ts_jd
			FROM sesh_index_messages
			WHERE quarantine = 0 AND timestamp_utc IS NOT NULL
			GROUP BY tool, logical_session_id
		)
		SELECT sess.tool, sess.logical
		FROM sess
		LEFT JOIN ts ON ts.tool = sess.tool AND ts.logical = sess.logical
		ORDER BY COALESCE(ts.max_ts_jd, sess.first_ingest_jd) DESC, sess.tool, sess.logical`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionKey
	for rows.Next() {
		var k sessionKey
		if err := rows.Scan(&k.tool, &k.logical); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Session resolves one logical session by hydrating exactly that key —
// never the full listing.
func (s *SQLStore) Session(ctx context.Context, tool wire.Tool, logicalSessionID string) (SessionSummary, bool, error) {
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
// key order. Every query below is constrained to the keys (or their wire
// session ids), so the work per request is proportional to the page, not the
// corpus.
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
		if c, ok := counts[countKey(key.tool, key.logical)]; ok {
			sum.MessageRows, sum.QuarantinedRows = c.messages, c.quarantined
		}
		if ts, ok := maxTimestamps[countKey(key.tool, key.logical)]; ok {
			sum.MaxTimestampUTC = ts
		}
		out = append(out, sum)
	}
	return out, nil
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
	rows, err := s.db.QueryContext(ctx, `SELECT hostname, os_user, last_put_at FROM last_seen ORDER BY hostname, os_user`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	var out []NodeStatus
	for rows.Next() {
		var n NodeStatus
		var raw string
		if err := rows.Scan(&n.Hostname, &n.OSUser, &raw); err != nil {
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
	messages    int
	quarantined int
}

func (s *SQLStore) rowCounts(ctx context.Context, keys []sessionKey) (map[string]rowCount, error) {
	clause, args := sessionKeyValues(keys)
	// Requested-keys join + INDEXED BY: full-key seeks only (see
	// memberGenerations for why the pin is load-bearing).
	rows, err := s.db.QueryContext(ctx, `SELECT m.tool, m.logical_session_id,
			COALESCE(SUM(CASE WHEN m.quarantine = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN m.quarantine = 1 THEN 1 ELSE 0 END), 0)
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
		if err := rows.Scan(&tool, &logical, &c.messages, &c.quarantined); err != nil {
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
