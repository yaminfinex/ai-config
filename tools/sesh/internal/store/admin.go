package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
// rows. The audit row is committed before any destructive DB or filesystem
// change so an interrupted drop cannot lose the deletion trail.
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
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO drop_log(dropped_at, tool, session_id, file_uuid, generations, reason) VALUES (?, ?, ?, ?, ?, ?)`,
			formatTime(time.Now().UTC()), tool, sessionID, fileUUID, strings.Join(genLabels, ","), reason); err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		if _, err := tx.ExecContext(ctx, `DELETE FROM sesh_index_messages WHERE tool = ? AND wire_session_id = ? AND file_uuid = ?`,
			tool, sessionID, fileUUID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM index_file_state WHERE tool = ? AND wire_session_id = ? AND file_uuid = ?`,
			tool, sessionID, fileUUID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM quarantine_ledger WHERE tool = ? AND wire_session_id = ? AND file_uuid = ?`,
			tool, sessionID, fileUUID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE tool = ? AND session_id = ? AND file_uuid = ?`,
			tool, sessionID, fileUUID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		committed = true
		for _, gen := range gens {
			if err := os.Remove(s.mirrorPath(gen)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}
