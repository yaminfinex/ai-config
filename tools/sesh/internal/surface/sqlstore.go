package surface

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
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
// The store DB runs a single connection, so no method here may start a query
// while another result set is open — collect first, then query again.
type SQLStore struct {
	db         *sql.DB
	mirrorPath MirrorPath
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

// genLogicalCTE maps every mirrored file generation to its logical session:
// the index's mapping when one exists, else the wire session claim (honest
// fallback, matches the schema rule). The mirror is truth and the surface
// must never be blind to it — unindexed generations still list, and render
// raw.
const genLogicalCTE = `WITH mapped AS (
		SELECT DISTINCT tool, wire_session_id, file_uuid, generation, logical_session_id
		FROM sesh_index_messages
	), gen AS (
		SELECT f.tool AS tool,
			COALESCE(m.logical_session_id, f.session_id) AS logical,
			f.session_id AS wire_id,
			f.file_uuid AS file_uuid,
			f.generation AS generation,
			COALESCE(f.created_at, '') AS created_at,
			COALESCE(f.last_put_at, '') AS last_put_at
		FROM files f
		LEFT JOIN mapped m
			ON m.tool = f.tool AND m.wire_session_id = f.session_id
			AND m.file_uuid = f.file_uuid AND m.generation = f.generation
	)`

// RecentSessions returns one page of logical sessions, most recent first by
// the R14 instant. The page is cut by LIMIT inside SQLite — the fleet's
// corpus (thousands of files per node) must never be materialized per
// request — and only the page's sessions are hydrated afterwards.
func (s *SQLStore) RecentSessions(ctx context.Context, limit, offset int) ([]SessionSummary, int, error) {
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}
	keys, err := s.recentSessionKeys(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.sessionCount(ctx)
	if err != nil {
		return nil, 0, err
	}
	sums, err := s.hydrateSessions(ctx, keys)
	if err != nil {
		return nil, 0, err
	}
	return sums, total, nil
}

// recentSessionKeys ranks every logical session by the R14 recency instant —
// max parsed non-quarantined timestamp, first-ingest when none — entirely in
// SQL and returns only the requested page of keys. julianday keeps the
// comparison temporal across RFC3339 fractional-precision variants (same
// posture as the max-timestamp lookup below); the tool+logical tie-break
// keeps page cuts deterministic.
func (s *SQLStore) recentSessionKeys(ctx context.Context, limit, offset int) ([]sessionKey, error) {
	rows, err := s.db.QueryContext(ctx, genLogicalCTE+`,
		sess AS (
			SELECT tool, logical, MIN(julianday(created_at)) AS first_ingest_jd
			FROM gen GROUP BY tool, logical
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
		ORDER BY COALESCE(ts.max_ts_jd, sess.first_ingest_jd) DESC, sess.tool, sess.logical
		LIMIT ? OFFSET ?`, limit, offset)
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

// sessionCount is the corpus-wide logical session count for the paging label.
func (s *SQLStore) sessionCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, genLogicalCTE+`
		SELECT COUNT(*) FROM (SELECT DISTINCT tool, logical FROM gen)`).Scan(&n)
	return n, err
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

// keyValuesClause renders a row-value IN list — `(VALUES (?, ?), …)` — plus
// its bind args for a set of two-column keys.
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
// given sessions, keyed back to their session.
func (s *SQLStore) memberGenerations(ctx context.Context, keys []sessionKey) (map[sessionKey][]mirrorGen, error) {
	clause, args := sessionKeyValues(keys)
	rows, err := s.db.QueryContext(ctx, genLogicalCTE+`
		SELECT tool, logical, wire_id, file_uuid, generation, created_at, last_put_at
		FROM gen WHERE (tool, logical) IN `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[sessionKey][]mirrorGen{}
	for rows.Next() {
		var g mirrorGen
		var logical, created, lastPut string
		if err := rows.Scan(&g.tool, &logical, &g.wireID, &g.fileUUID, &g.gen, &created, &lastPut); err != nil {
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
	rows, err := s.db.QueryContext(ctx, `SELECT tool, logical_session_id,
			COALESCE(SUM(CASE WHEN quarantine = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN quarantine = 1 THEN 1 ELSE 0 END), 0)
		FROM sesh_index_messages
		WHERE (tool, logical_session_id) IN `+clause+`
		GROUP BY tool, logical_session_id`, args...)
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
	rows, err := s.db.QueryContext(ctx, `SELECT tool, session_id, hostname, os_user, COALESCE(tailnet_identity, ''), id
		FROM fact_observations
		WHERE id IN (
			SELECT MAX(id) FROM fact_observations
			WHERE (tool, session_id) IN `+clause+`
			GROUP BY tool, session_id
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
	rows, err := s.db.QueryContext(ctx, `SELECT tool, session_id, session_owner, MIN(id) AS first_id
		FROM fact_observations
		WHERE session_owner IS NOT NULL AND session_owner <> ''
			AND (tool, session_id) IN `+clause+`
		GROUP BY tool, session_id, session_owner
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
	rows, err := s.db.QueryContext(ctx, `SELECT tool, logical_session_id, timestamp_utc FROM (
			SELECT tool, logical_session_id, timestamp_utc,
				ROW_NUMBER() OVER (
					PARTITION BY tool, logical_session_id
					ORDER BY julianday(timestamp_utc) DESC, timestamp_utc DESC
				) AS rn
			FROM sesh_index_messages
			WHERE quarantine = 0 AND timestamp_utc IS NOT NULL
				AND (tool, logical_session_id) IN `+clause+`
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
