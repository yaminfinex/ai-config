package surface

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"sort"
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

// Sessions lists every logical session the index knows, plus mirrored file
// generations the index holds nothing for (yet or ever): the mirror is
// truth and the surface must never be blind to it — those render raw.
func (s *SQLStore) Sessions(ctx context.Context) ([]SessionSummary, error) {
	gens, err := s.fileGenerations(ctx)
	if err != nil {
		return nil, err
	}
	logicalOf, err := s.logicalMapping(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.rowCounts(ctx)
	if err != nil {
		return nil, err
	}
	facts, err := s.latestFacts(ctx)
	if err != nil {
		return nil, err
	}

	type sessionKey struct {
		tool    wire.Tool
		logical string
	}
	group := map[sessionKey][]mirrorGen{}
	for _, g := range gens {
		logical, ok := logicalOf[genKey(g.tool, g.wireID, g.fileUUID, g.gen)]
		if !ok {
			// Mirrored but unindexed: the wire claim is the only session
			// identity available (honest fallback, matches the schema rule).
			logical = g.wireID
		}
		key := sessionKey{g.tool, logical}
		group[key] = append(group[key], g)
	}

	var out []SessionSummary
	for key, members := range group {
		sort.Slice(members, func(i, j int) bool {
			a, b := members[i], members[j]
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
			FirstIngestAt:    members[0].createdAt,
		}
		var factID int64 = -1
		for _, g := range members {
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
			}
		}
		if c, ok := counts[countKey(key.tool, key.logical)]; ok {
			sum.MessageRows, sum.QuarantinedRows = c.messages, c.quarantined
		}
		if sum.MessageRows > 0 {
			ts, err := s.maxTimestamp(ctx, key.tool, key.logical)
			if err != nil {
				return nil, err
			}
			sum.MaxTimestampUTC = ts
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tool != out[j].Tool {
			return out[i].Tool < out[j].Tool
		}
		return out[i].LogicalSessionID < out[j].LogicalSessionID
	})
	return out, nil
}

// Session resolves one logical session. Session counts are small (a team's
// recent work); reusing the full listing keeps one code path honest.
func (s *SQLStore) Session(ctx context.Context, tool wire.Tool, logicalSessionID string) (SessionSummary, bool, error) {
	sums, err := s.Sessions(ctx)
	if err != nil {
		return SessionSummary{}, false, err
	}
	for _, sum := range sums {
		if sum.Tool == tool && sum.LogicalSessionID == logicalSessionID {
			return sum, true, nil
		}
	}
	return SessionSummary{}, false, nil
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

// --- queries (each fully drains its result set before the next runs) ---

func genKey(tool wire.Tool, wireID, fileUUID string, gen int) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", tool, wireID, fileUUID, gen)
}

func countKey(tool wire.Tool, logical string) string {
	return string(tool) + "\x00" + logical
}

func factKey(tool wire.Tool, wireID string) string {
	return string(tool) + "\x00" + wireID
}

func (s *SQLStore) fileGenerations(ctx context.Context) ([]mirrorGen, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool, session_id, file_uuid, generation,
			COALESCE(created_at, ''), COALESCE(last_put_at, '') FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mirrorGen
	for rows.Next() {
		var g mirrorGen
		var created, lastPut string
		if err := rows.Scan(&g.tool, &g.wireID, &g.fileUUID, &g.gen, &created, &lastPut); err != nil {
			return nil, err
		}
		g.createdAt = parseStoreTime(created)
		g.lastPutAt = parseStoreTime(lastPut)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *SQLStore) logicalMapping(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT tool, wire_session_id, file_uuid, generation, logical_session_id
		FROM sesh_index_messages`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var tool wire.Tool
		var wireID, fileUUID, logical string
		var gen int
		if err := rows.Scan(&tool, &wireID, &fileUUID, &gen, &logical); err != nil {
			return nil, err
		}
		out[genKey(tool, wireID, fileUUID, gen)] = logical
	}
	return out, rows.Err()
}

type rowCount struct {
	messages    int
	quarantined int
}

func (s *SQLStore) rowCounts(ctx context.Context) (map[string]rowCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool, logical_session_id,
			COALESCE(SUM(CASE WHEN quarantine = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN quarantine = 1 THEN 1 ELSE 0 END), 0)
		FROM sesh_index_messages GROUP BY tool, logical_session_id`)
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
	id       int64
	hostname string
	osUser   string
}

// latestFacts returns the most recent (hostname, os_user) observation per
// wire session. Facts are an append-only observation log; "latest" here
// picks the node label for grouping, it never rewrites owner facts (U10
// owns owner precedence).
func (s *SQLStore) latestFacts(ctx context.Context) (map[string]factRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool, session_id, hostname, os_user, id
		FROM fact_observations
		WHERE id IN (SELECT MAX(id) FROM fact_observations GROUP BY tool, session_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]factRow{}
	for rows.Next() {
		var tool wire.Tool
		var wireID string
		var f factRow
		if err := rows.Scan(&tool, &wireID, &f.hostname, &f.osUser, &f.id); err != nil {
			return nil, err
		}
		out[factKey(tool, wireID)] = f
	}
	return out, rows.Err()
}

// maxTimestamp finds the session's newest parsed timestamp. julianday
// compares the RFC3339Nano strings chronologically (lexicographic MAX would
// mis-order second-exact timestamps against sub-second ones); the string
// tiebreak keeps it deterministic past julianday's float precision.
func (s *SQLStore) maxTimestamp(ctx context.Context, tool wire.Tool, logical string) (*time.Time, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT timestamp_utc FROM sesh_index_messages
		WHERE tool = ? AND logical_session_id = ? AND quarantine = 0 AND timestamp_utc IS NOT NULL
		ORDER BY julianday(timestamp_utc) DESC, timestamp_utc DESC LIMIT 1`,
		tool, logical).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, nil // unparseable timestamp string: honest absence
	}
	t = t.UTC()
	return &t, nil
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
