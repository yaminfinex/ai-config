package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"sesh/internal/surface"
	"sesh/internal/wire"
)

// Nodes returns last-PUT activity by hostname and OS user.
func (s *Store) Nodes(ctx context.Context, staleAfter time.Duration) ([]surface.NodeStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hostname, os_user, last_put_at FROM last_seen ORDER BY hostname, os_user`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	var out []surface.NodeStatus
	for rows.Next() {
		var n surface.NodeStatus
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

// DropFile deletes one mirrored file identity, its registry row, and its index
// rows, then writes an audit entry.
func (s *Store) DropFile(ctx context.Context, tool wire.Tool, sessionID, fileUUID, reason string) error {
	if _, ok := parseTool(string(tool)); !ok {
		return fmt.Errorf("unknown tool %q", tool)
	}
	var valid bool
	if sessionID, valid = canonicalUUID(sessionID); !valid {
		return errors.New("invalid session_id")
	}
	if fileUUID, valid = canonicalUUID(fileUUID); !valid {
		return errors.New("invalid file_uuid")
	}
	return s.WithWriteLock(func() error {
		gens, err := s.generations(ctx, tool, sessionID, fileUUID, nil)
		if err != nil {
			return err
		}
		if len(gens) == 0 {
			return sql.ErrNoRows
		}
		var genLabels []string
		for _, gen := range gens {
			genLabels = append(genLabels, fmt.Sprintf("%d", gen.Generation))
			if err := os.Remove(s.mirrorPath(gen)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sesh_index_messages WHERE tool = ? AND file_uuid = ?`,
			tool, fileUUID); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM index_file_state WHERE tool = ? AND file_uuid = ?`,
			tool, fileUUID); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM quarantine_ledger WHERE tool = ? AND file_uuid = ?`,
			tool, fileUUID); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE tool = ? AND session_id = ? AND file_uuid = ?`,
			tool, sessionID, fileUUID); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO drop_log(dropped_at, tool, session_id, file_uuid, generations, reason) VALUES (?, ?, ?, ?, ?, ?)`,
			formatTime(time.Now().UTC()), tool, sessionID, fileUUID, strings.Join(genLabels, ","), reason); err != nil {
			return err
		}
		return nil
	})
}

// Sessions implements surface.Store over the real index and mirror tables.
func (s *Store) Sessions(ctx context.Context) ([]surface.SessionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool, logical_session_id,
		MIN(COALESCE(f.created_at, '')), MAX(COALESCE(f.updated_at, '')),
		COUNT(CASE WHEN m.quarantine = 0 THEN 1 END),
		COUNT(CASE WHEN m.quarantine != 0 THEN 1 END),
		MAX(m.timestamp_utc)
		FROM sesh_index_messages m
		LEFT JOIN files f ON f.tool = m.tool AND f.session_id = m.wire_session_id AND f.file_uuid = m.file_uuid AND f.generation = m.generation
		GROUP BY tool, logical_session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []surface.SessionSummary
	for rows.Next() {
		sum, err := s.scanSessionSummary(ctx, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

func (s *Store) scanSessionSummary(ctx context.Context, rows interface{ Scan(...any) error }) (surface.SessionSummary, error) {
	var sum surface.SessionSummary
	var first, mirrored, maxTS sql.NullString
	if err := rows.Scan(&sum.Tool, &sum.LogicalSessionID, &first, &mirrored, &sum.MessageRows, &sum.QuarantinedRows, &maxTS); err != nil {
		return sum, err
	}
	sum.FirstIngestAt = parseDBTime(first.String)
	sum.MirroredAt = parseDBTime(mirrored.String)
	if maxTS.Valid && maxTS.String != "" {
		ts := parseDBTime(maxTS.String)
		sum.MaxTimestampUTC = &ts
	}
	node, _ := s.latestNodeForSession(ctx, sum.Tool, sum.LogicalSessionID)
	sum.Hostname, sum.OSUser = node.Hostname, node.OSUser
	files, err := s.sessionFiles(ctx, sum.Tool, sum.LogicalSessionID)
	if err != nil {
		return sum, err
	}
	sum.Files = files
	if sum.FirstIngestAt.IsZero() && len(files) > 0 {
		sum.FirstIngestAt = files[0].FirstIngestAt
	}
	return sum, nil
}

func parseDBTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t.UTC()
}

func (s *Store) latestNodeForSession(ctx context.Context, tool wire.Tool, logicalID string) (surface.NodeStatus, error) {
	var n surface.NodeStatus
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT fo.hostname, fo.os_user, fo.observed_at
		FROM fact_observations fo
		JOIN sesh_index_messages m ON m.tool = fo.tool AND m.wire_session_id = fo.session_id AND m.file_uuid = fo.file_uuid AND m.generation = fo.generation
		WHERE m.tool = ? AND m.logical_session_id = ?
		ORDER BY fo.observed_at DESC LIMIT 1`, tool, logicalID).Scan(&n.Hostname, &n.OSUser, &raw)
	if err != nil {
		return n, err
	}
	n.LastPutAt = parseDBTime(raw)
	return n, nil
}

func (s *Store) sessionFiles(ctx context.Context, tool wire.Tool, logicalID string) ([]surface.FileRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.file_uuid, m.generation, MIN(COALESCE(f.created_at, '')) first_ingest
		FROM sesh_index_messages m
		LEFT JOIN files f ON f.tool = m.tool AND f.session_id = m.wire_session_id AND f.file_uuid = m.file_uuid AND f.generation = m.generation
		WHERE m.tool = ? AND m.logical_session_id = ?
		GROUP BY m.file_uuid, m.generation ORDER BY MIN(m.file_ordinal), m.file_uuid, m.generation`, tool, logicalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []surface.FileRef
	for rows.Next() {
		var ref surface.FileRef
		var raw string
		if err := rows.Scan(&ref.FileUUID, &ref.Generation, &raw); err != nil {
			return nil, err
		}
		ref.FirstIngestAt = parseDBTime(raw)
		files = append(files, ref)
	}
	return files, rows.Err()
}

// Session resolves one logical session.
func (s *Store) Session(ctx context.Context, tool wire.Tool, logicalSessionID string) (surface.SessionSummary, bool, error) {
	sums, err := s.Sessions(ctx)
	if err != nil {
		return surface.SessionSummary{}, false, err
	}
	for _, sum := range sums {
		if sum.Tool == tool && sum.LogicalSessionID == logicalSessionID {
			return sum, true, nil
		}
	}
	return surface.SessionSummary{}, false, nil
}

// Rows returns index rows for one logical session.
func (s *Store) Rows(ctx context.Context, tool wire.Tool, logicalSessionID string) ([]wire.IndexMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, tool, logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason
		FROM sesh_index_messages WHERE tool = ? AND logical_session_id = ?`, tool, logicalSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []wire.IndexMessage
	for rows.Next() {
		var row wire.IndexMessage
		var ts sql.NullString
		var quarantine int
		if err := rows.Scan(&row.ID, &row.Tool, &row.LogicalSessionID, &row.WireSessionID, &row.EntryType, &row.MessageUUID, &row.FileUUID, &row.Generation, &row.Role, &ts, &row.FileOrdinal, &row.LineOrdinal, &row.ByteStart, &row.ByteEnd, &quarantine, &row.QuarantineReason); err != nil {
			return nil, err
		}
		if ts.Valid && ts.String != "" {
			t := parseDBTime(ts.String)
			row.TimestampUTC = &t
		}
		row.Quarantine = quarantine != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// MirrorRange reads mirrored bytes [start,end) for a file generation.
func (s *Store) MirrorRange(ctx context.Context, tool wire.Tool, fileUUID string, generation int, start, end int64) ([]byte, error) {
	st, err := s.fileByUUIDGeneration(ctx, tool, fileUUID, generation)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(s.mirrorPath(st))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if end < start {
		end = start
	}
	buf := make([]byte, end-start)
	n, err := f.ReadAt(buf, start)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

// MirrorFile streams a whole mirrored file generation.
func (s *Store) MirrorFile(ctx context.Context, tool wire.Tool, fileUUID string, generation int) (io.ReadCloser, error) {
	st, err := s.fileByUUIDGeneration(ctx, tool, fileUUID, generation)
	if err != nil {
		return nil, err
	}
	return os.Open(s.mirrorPath(st))
}

func (s *Store) fileByUUIDGeneration(ctx context.Context, tool wire.Tool, fileUUID string, generation int) (fileState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool, session_id, file_uuid, generation, fingerprint, high_water,
		poisoned, dirty_for_reindex, COALESCE(last_put_at, ''), conflict_pending, conflict_driven
		FROM files WHERE tool = ? AND file_uuid = ? AND generation = ? ORDER BY session_id LIMIT 1`, tool, fileUUID, generation)
	if err != nil {
		return fileState{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return fileState{}, os.ErrNotExist
	}
	st, err := scanFileState(rows)
	if err != nil {
		return fileState{}, err
	}
	return st, rows.Err()
}
