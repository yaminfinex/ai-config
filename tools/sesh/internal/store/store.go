// Package store implements the central byte mirror side of the frozen sesh
// wire contract.
package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"sesh/internal/wire"
)

// Config controls store construction.
type Config struct {
	Dir          string
	Logger       *slog.Logger
	AppendBuffer int
}

// Store is a single-process store. SQLite is still used for durable state so
// the process can restart and absorb replayed byte ranges idempotently.
type Store struct {
	dir       string
	mirrorDir string
	db        *sql.DB
	events    chan wire.AppendEvent
	logger    *slog.Logger

	mu             sync.Mutex
	failAppendOnce bool
}

type fileState struct {
	Tool            wire.Tool
	SessionID       string
	FileUUID        string
	Generation      int
	Fingerprint     *string
	HighWater       int64
	Poisoned        bool
	DirtyForReindex bool
	LastPutAt       time.Time
	ConflictPending bool
	ConflictDriven  bool
}

// Open initializes the store directory, mirror directory, and SQLite schema.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Dir == "" {
		return nil, errors.New("store dir is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.AppendBuffer <= 0 {
		cfg.AppendBuffer = 128
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}
	mirrorDir := filepath.Join(cfg.Dir, "mirror")
	if err := os.MkdirAll(mirrorDir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(cfg.Dir, "store.sqlite"))
	if err != nil {
		return nil, err
	}
	s := &Store{
		dir:       cfg.Dir,
		mirrorDir: mirrorDir,
		db:        db,
		events:    make(chan wire.AppendEvent, cfg.AppendBuffer),
		logger:    cfg.Logger,
	}
	if err := s.initSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// AppendEvents returns the in-process event bus consumed by U6.
func (s *Store) AppendEvents() <-chan wire.AppendEvent {
	return s.events
}

// InjectMirrorErrorOnce makes the next mirror append fail before an ACK. It is
// used by tests to exercise the R12 storage-error path.
func (s *Store) InjectMirrorErrorOnce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failAppendOnce = true
}

func (s *Store) initSchema(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS files (
			tool TEXT NOT NULL,
			session_id TEXT NOT NULL,
			file_uuid TEXT NOT NULL,
			generation INTEGER NOT NULL,
			fingerprint TEXT NULL,
			high_water INTEGER NOT NULL DEFAULT 0,
			poisoned INTEGER NOT NULL DEFAULT 0,
			dirty_for_reindex INTEGER NOT NULL DEFAULT 0,
			last_put_at TEXT NULL,
			conflict_pending INTEGER NOT NULL DEFAULT 0,
			conflict_driven INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (tool, session_id, file_uuid, generation)
		)`,
		`CREATE INDEX IF NOT EXISTS files_identity_fingerprint
			ON files(tool, session_id, file_uuid, fingerprint)`,
		`CREATE TABLE IF NOT EXISTS last_seen (
			hostname TEXT NOT NULL,
			os_user TEXT NOT NULL,
			last_put_at TEXT NOT NULL,
			PRIMARY KEY (hostname, os_user)
		)`,
		`CREATE TABLE IF NOT EXISTS fact_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			observed_at TEXT NOT NULL,
			tool TEXT NOT NULL,
			session_id TEXT NOT NULL,
			file_uuid TEXT NOT NULL,
			generation INTEGER NOT NULL,
			hostname TEXT NOT NULL,
			os_user TEXT NOT NULL,
			session_owner TEXT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// Handler returns the v1 HTTP handler.
func (s *Store) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(wire.APIRoot+"/files/", http.HandlerFunc(s.handleFiles))
	return mux
}

func (s *Store) handleFiles(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, wire.APIRoot+"/files/")
	parts := strings.Split(rest, "/")
	switch {
	case r.Method == http.MethodPut && len(parts) == 4 && parts[3] == "bytes":
		s.handlePUTBytes(w, r, parts[0], parts[1], parts[2])
	case r.Method == http.MethodGet && len(parts) == 3:
		s.handleRecovery(w, r, parts[0], parts[1], parts[2])
	default:
		s.writeError(w, wire.ErrMalformedRequest, wire.Tool(""), "", "", 0, 0, "", "malformed v1 file path")
	}
}

func (s *Store) handlePUTBytes(w http.ResponseWriter, r *http.Request, rawTool, sessionID, fileUUID string) {
	tool, ok := parseTool(rawTool)
	if !ok {
		s.writeError(w, wire.ErrUnknownTool, wire.Tool(rawTool), sessionID, fileUUID, 0, 0, "", "unknown tool")
		return
	}
	if !validUUID(sessionID) || !validUUID(fileUUID) {
		s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", "invalid session_id or file_uuid")
		return
	}
	if r.Header.Get(wire.HeaderWireVersion) != strconv.Itoa(wire.Version) {
		s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", "missing or wrong wire version")
		return
	}
	if !contentTypeOK(r.Header.Get("Content-Type")) {
		s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", "content-type must be application/octet-stream")
		return
	}
	hostname, osUser := r.Header.Get(wire.HeaderHostname), r.Header.Get(wire.HeaderOSUser)
	if hostname == "" || osUser == "" {
		s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", "hostname and os user headers are required")
		return
	}
	offset, err := parseOffset(r.URL.Query().Get("offset"))
	if err != nil {
		s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", "invalid offset")
		return
	}
	fp, err := parseFingerprintHeaders(r)
	if err != nil {
		s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", err.Error())
		return
	}
	body, errCode, err := readBody(r)
	if err != nil {
		s.writeError(w, errCode, tool, sessionID, fileUUID, 0, 0, "", err.Error())
		return
	}
	resp, ev, code, msg, action := s.putBytes(r.Context(), tool, sessionID, fileUUID, fp, offset, body, hostname, osUser, r.Header.Get(wire.HeaderSessionOwner))
	if code != "" {
		generation, highWater := 0, int64(0)
		if resp != nil {
			generation = resp.Generation
			highWater = resp.HighWater
		}
		s.writeError(w, code, tool, sessionID, fileUUID, generation, highWater, action, msg)
		return
	}
	if ev != nil {
		select {
		case s.events <- *ev:
		default:
			s.logger.Warn("append event dropped", "tool", tool, "session_id", sessionID, "file_uuid", fileUUID, "generation", ev.Generation)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Store) handleRecovery(w http.ResponseWriter, r *http.Request, rawTool, sessionID, fileUUID string) {
	tool, ok := parseTool(rawTool)
	if !ok {
		s.writeError(w, wire.ErrUnknownTool, wire.Tool(rawTool), sessionID, fileUUID, 0, 0, "", "unknown tool")
		return
	}
	if !validUUID(sessionID) || !validUUID(fileUUID) {
		s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", "invalid session_id or file_uuid")
		return
	}
	if r.Header.Get(wire.HeaderWireVersion) != strconv.Itoa(wire.Version) {
		s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", "missing or wrong wire version")
		return
	}
	fpParam := r.URL.Query().Get("fingerprint")
	var fp *string
	if fpParam != "" {
		if !validFingerprint(fpParam) {
			s.writeError(w, wire.ErrMalformedRequest, tool, sessionID, fileUUID, 0, 0, "", "invalid fingerprint")
			return
		}
		fp = &fpParam
	}
	resp, err := s.recovery(r.Context(), tool, sessionID, fileUUID, fp)
	if errors.Is(err, sql.ErrNoRows) {
		s.writeError(w, wire.ErrNotFound, tool, sessionID, fileUUID, 0, 0, wire.ShipperActionStartFromZero, "no mirror state for file identity")
		return
	}
	if err != nil {
		s.writeError(w, wire.ErrStoreUnavailable, tool, sessionID, fileUUID, 0, 0, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Store) putBytes(ctx context.Context, tool wire.Tool, sessionID, fileUUID string, fp *string, offset int64, body []byte, hostname, osUser, owner string) (*wire.Ack, *wire.AppendEvent, wire.ErrorCode, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target, code, msg, action, err := s.selectGeneration(ctx, tool, sessionID, fileUUID, fp)
	if err != nil {
		return nil, nil, wire.ErrStoreUnavailable, err.Error(), ""
	}
	if code != "" {
		ack := s.ack(target)
		return &ack, nil, code, msg, action
	}
	if target.Poisoned {
		ack := s.ack(target)
		return &ack, nil, wire.ErrPoisonedFile, "file identity is poisoned", ""
	}
	if offset > target.HighWater {
		ack := s.ack(target)
		return &ack, nil, wire.ErrOffsetGap, "offset beyond high-water", wire.ShipperActionRewind
	}

	if offset < target.HighWater {
		ev, code, msg, action, err := s.handleOverlap(ctx, &target, offset, body, hostname, osUser, owner)
		ack := s.ack(target)
		if err != nil {
			return &ack, nil, wire.ErrMirrorWriteFailed, err.Error(), ""
		}
		if code != "" {
			return &ack, nil, code, msg, action
		}
		return &ack, ev, "", "", ""
	}

	ev, err := s.appendAtHighWater(ctx, &target, body, hostname, osUser, owner)
	ack := s.ack(target)
	if err != nil {
		return &ack, nil, wire.ErrMirrorWriteFailed, err.Error(), ""
	}
	return &ack, ev, "", "", ""
}

func (s *Store) selectGeneration(ctx context.Context, tool wire.Tool, sessionID, fileUUID string, fp *string) (fileState, wire.ErrorCode, string, string, error) {
	gens, err := s.generations(ctx, tool, sessionID, fileUUID, nil)
	if err != nil {
		return fileState{}, "", "", "", err
	}
	now := time.Now().UTC()
	if len(gens) == 0 {
		st, err := s.createGeneration(ctx, tool, sessionID, fileUUID, 0, fp, false, now)
		return st, "", "", "", err
	}
	if fp != nil {
		for _, gen := range gens {
			if gen.Fingerprint != nil && *gen.Fingerprint == *fp {
				return gen, "", "", "", nil
			}
		}
		active := gens[len(gens)-1]
		if active.Fingerprint == nil {
			if err := s.setFingerprint(ctx, &active, fp); err != nil {
				return fileState{}, "", "", "", err
			}
			return active, "", "", "", nil
		}
		st, err := s.createGeneration(ctx, tool, sessionID, fileUUID, active.Generation+1, fp, false, now)
		if err != nil {
			return fileState{}, "", "", "", err
		}
		return st, wire.ErrFingerprintConflict, "request fingerprint selected a different generation", "", nil
	}
	return gens[len(gens)-1], "", "", "", nil
}

func (s *Store) handleOverlap(ctx context.Context, target *fileState, offset int64, body []byte, hostname, osUser, owner string) (*wire.AppendEvent, wire.ErrorCode, string, string, error) {
	overlap := min(int64(len(body)), target.HighWater-offset)
	matches, err := s.compareMirror(*target, offset, body[:overlap])
	if err != nil {
		return nil, "", "", "", err
	}
	if !matches {
		code, msg, err := s.handleDivergence(ctx, target)
		return nil, code, msg, "", err
	}
	if int64(len(body)) == overlap {
		if err := s.recordSuccess(ctx, target, hostname, osUser, owner); err != nil {
			return nil, "", "", "", err
		}
		return nil, "", "", "", nil
	}
	body = body[overlap:]
	ev, err := s.appendAtHighWater(ctx, target, body, hostname, osUser, owner)
	return ev, "", "", "", err
}

func (s *Store) handleDivergence(ctx context.Context, target *fileState) (wire.ErrorCode, string, error) {
	if !target.ConflictPending {
		if err := s.setConflictPending(ctx, target, true); err != nil {
			return "", "", err
		}
		return wire.ErrByteConflict, "bytes diverge from ACKed mirror range", nil
	}
	exists, err := s.conflictDrivenExists(ctx, target.Tool, target.SessionID, target.FileUUID, target.Fingerprint)
	if err != nil {
		return "", "", err
	}
	if exists {
		if err := s.poisonFingerprint(ctx, target.Tool, target.SessionID, target.FileUUID, target.Fingerprint); err != nil {
			return "", "", err
		}
		target.Poisoned = true
		return wire.ErrPoisonedFile, "conflict recurred for file identity", nil
	}
	next := target.Generation + 1
	active, err := s.activeGeneration(ctx, target.Tool, target.SessionID, target.FileUUID)
	if err != nil {
		return "", "", err
	}
	if active.Generation >= next {
		next = active.Generation + 1
	}
	st, err := s.createGeneration(ctx, target.Tool, target.SessionID, target.FileUUID, next, target.Fingerprint, true, time.Now().UTC())
	if err != nil {
		return "", "", err
	}
	*target = st
	return wire.ErrGenerationOpened, "opened new generation after repeated divergence", nil
}

func (s *Store) appendAtHighWater(ctx context.Context, target *fileState, body []byte, hostname, osUser, owner string) (*wire.AppendEvent, error) {
	if len(body) == 0 {
		if err := s.recordSuccess(ctx, target, hostname, osUser, owner); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if s.failAppendOnce {
		s.failAppendOnce = false
		return nil, errors.New("injected mirror write failure")
	}
	start := target.HighWater
	if err := s.writeMirror(*target, start, body); err != nil {
		return nil, err
	}
	target.HighWater += int64(len(body))
	if err := s.updateFingerprintFromMirror(ctx, target); err != nil {
		return nil, err
	}
	if err := s.recordSuccess(ctx, target, hostname, osUser, owner); err != nil {
		return nil, err
	}
	return &wire.AppendEvent{
		Tool:          target.Tool,
		WireSessionID: target.SessionID,
		FileUUID:      target.FileUUID,
		Generation:    target.Generation,
		ByteStart:     start,
		ByteEnd:       target.HighWater,
	}, nil
}

func (s *Store) writeMirror(target fileState, offset int64, body []byte) error {
	path := s.mirrorPath(target)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteAt(body, offset); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func (s *Store) compareMirror(target fileState, offset int64, want []byte) (bool, error) {
	if len(want) == 0 {
		return true, nil
	}
	f, err := os.Open(s.mirrorPath(target))
	if err != nil {
		return false, err
	}
	defer f.Close()
	got := make([]byte, len(want))
	if _, err := f.ReadAt(got, offset); err != nil {
		return false, err
	}
	return bytes.Equal(got, want), nil
}

func (s *Store) updateFingerprintFromMirror(ctx context.Context, target *fileState) error {
	if target.HighWater < wire.FingerprintWindowBytes {
		return nil
	}
	f, err := os.Open(s.mirrorPath(*target))
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, wire.FingerprintWindowBytes)
	if _, err := io.ReadFull(f, buf); err != nil {
		return err
	}
	sum := sha256.Sum256(buf)
	computed := hex.EncodeToString(sum[:])
	if target.Fingerprint != nil && *target.Fingerprint != computed {
		s.logger.Warn("fingerprint claim differs from mirrored bytes",
			"tool", target.Tool,
			"session_id", target.SessionID,
			"file_uuid", target.FileUUID,
			"generation", target.Generation,
			"claim", *target.Fingerprint,
			"computed", computed,
		)
	}
	if target.Fingerprint == nil || *target.Fingerprint != computed {
		return s.setFingerprint(ctx, target, &computed)
	}
	return nil
}

func (s *Store) mirrorPath(target fileState) string {
	return filepath.Join(s.mirrorDir, string(target.Tool), target.SessionID, target.FileUUID, fmt.Sprintf("generation-%d.jsonl", target.Generation))
}

// MirrorPath returns the durable mirror path for tests and later tooling.
func (s *Store) MirrorPath(tool wire.Tool, sessionID, fileUUID string, generation int) string {
	return s.mirrorPath(fileState{Tool: tool, SessionID: sessionID, FileUUID: fileUUID, Generation: generation})
}

func (s *Store) ack(st fileState) wire.Ack {
	return wire.Ack{
		WireVersion:          wire.Version,
		Status:               wire.StatusAck,
		Tool:                 st.Tool,
		SessionID:            st.SessionID,
		FileUUID:             st.FileUUID,
		Generation:           st.Generation,
		HighWater:            st.HighWater,
		FingerprintAlgorithm: wire.FingerprintAlgorithm,
		Fingerprint:          st.Fingerprint,
	}
}

func (s *Store) recovery(ctx context.Context, tool wire.Tool, sessionID, fileUUID string, fp *string) (wire.RecoveryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gens, err := s.generations(ctx, tool, sessionID, fileUUID, fp)
	if err != nil {
		return wire.RecoveryResponse{}, err
	}
	if len(gens) == 0 {
		return wire.RecoveryResponse{}, sql.ErrNoRows
	}
	resp := wire.RecoveryResponse{
		WireVersion:            wire.Version,
		Tool:                   tool,
		SessionID:              sessionID,
		FileUUID:               fileUUID,
		FingerprintAlgorithm:   wire.FingerprintAlgorithm,
		FingerprintWindowBytes: wire.FingerprintWindowBytes,
	}
	for _, gen := range gens {
		resp.Generations = append(resp.Generations, wire.GenerationState{
			Generation:      gen.Generation,
			Fingerprint:     gen.Fingerprint,
			HighWater:       gen.HighWater,
			Poisoned:        gen.Poisoned,
			DirtyForReindex: gen.DirtyForReindex,
			LastPutAt:       gen.LastPutAt,
		})
	}
	return resp, nil
}

func (s *Store) generations(ctx context.Context, tool wire.Tool, sessionID, fileUUID string, fp *string) ([]fileState, error) {
	query := `SELECT tool, session_id, file_uuid, generation, fingerprint, high_water,
		poisoned, dirty_for_reindex, COALESCE(last_put_at, ''), conflict_pending, conflict_driven
		FROM files WHERE tool = ? AND session_id = ? AND file_uuid = ?`
	args := []any{tool, sessionID, fileUUID}
	if fp != nil {
		query += ` AND fingerprint = ?`
		args = append(args, *fp)
	}
	query += ` ORDER BY generation ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fileState
	for rows.Next() {
		st, err := scanFileState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) activeGeneration(ctx context.Context, tool wire.Tool, sessionID, fileUUID string) (fileState, error) {
	gens, err := s.generations(ctx, tool, sessionID, fileUUID, nil)
	if err != nil {
		return fileState{}, err
	}
	if len(gens) == 0 {
		return fileState{}, sql.ErrNoRows
	}
	return gens[len(gens)-1], nil
}

func scanFileState(scanner interface {
	Scan(dest ...any) error
}) (fileState, error) {
	var st fileState
	var fp sql.NullString
	var lastPut string
	var poisoned, dirty, pending, conflictDriven int
	if err := scanner.Scan(&st.Tool, &st.SessionID, &st.FileUUID, &st.Generation, &fp, &st.HighWater, &poisoned, &dirty, &lastPut, &pending, &conflictDriven); err != nil {
		return fileState{}, err
	}
	if fp.Valid {
		st.Fingerprint = &fp.String
	}
	st.Poisoned = poisoned != 0
	st.DirtyForReindex = dirty != 0
	st.ConflictPending = pending != 0
	st.ConflictDriven = conflictDriven != 0
	if lastPut != "" {
		t, err := time.Parse(time.RFC3339Nano, lastPut)
		if err != nil {
			return fileState{}, err
		}
		st.LastPutAt = t
	}
	return st, nil
}

func (s *Store) createGeneration(ctx context.Context, tool wire.Tool, sessionID, fileUUID string, generation int, fp *string, conflictDriven bool, now time.Time) (fileState, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO files
		(tool, session_id, file_uuid, generation, fingerprint, high_water, poisoned, dirty_for_reindex, conflict_pending, conflict_driven, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, ?, ?, ?)`,
		tool, sessionID, fileUUID, generation, nullableString(fp), boolInt(conflictDriven), formatTime(now), formatTime(now))
	if err != nil {
		return fileState{}, err
	}
	return fileState{Tool: tool, SessionID: sessionID, FileUUID: fileUUID, Generation: generation, Fingerprint: fp, ConflictDriven: conflictDriven}, nil
}

func (s *Store) setFingerprint(ctx context.Context, st *fileState, fp *string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE files SET fingerprint = ?, updated_at = ? WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = ?`,
		nullableString(fp), formatTime(time.Now().UTC()), st.Tool, st.SessionID, st.FileUUID, st.Generation)
	if err != nil {
		return err
	}
	st.Fingerprint = fp
	return nil
}

func (s *Store) setConflictPending(ctx context.Context, st *fileState, pending bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE files SET conflict_pending = ?, updated_at = ? WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = ?`,
		boolInt(pending), formatTime(time.Now().UTC()), st.Tool, st.SessionID, st.FileUUID, st.Generation)
	if err != nil {
		return err
	}
	st.ConflictPending = pending
	return nil
}

func (s *Store) recordSuccess(ctx context.Context, st *fileState, hostname, osUser, owner string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE files SET high_water = ?, last_put_at = ?, conflict_pending = 0, updated_at = ?
		WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = ?`,
		st.HighWater, formatTime(now), formatTime(now), st.Tool, st.SessionID, st.FileUUID, st.Generation)
	if err != nil {
		return err
	}
	st.LastPutAt = now
	st.ConflictPending = false
	_, err = s.db.ExecContext(ctx, `INSERT INTO last_seen(hostname, os_user, last_put_at) VALUES (?, ?, ?)
		ON CONFLICT(hostname, os_user) DO UPDATE SET last_put_at = excluded.last_put_at`,
		hostname, osUser, formatTime(now))
	if err != nil {
		return err
	}
	var ownerValue any
	if owner != "" {
		ownerValue = owner
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO fact_observations
		(observed_at, tool, session_id, file_uuid, generation, hostname, os_user, session_owner)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(now), st.Tool, st.SessionID, st.FileUUID, st.Generation, hostname, osUser, ownerValue)
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) conflictDrivenExists(ctx context.Context, tool wire.Tool, sessionID, fileUUID string, fp *string) (bool, error) {
	query := `SELECT COUNT(*) FROM files WHERE tool = ? AND session_id = ? AND file_uuid = ? AND conflict_driven = 1 AND `
	args := []any{tool, sessionID, fileUUID}
	if fp == nil {
		query += `fingerprint IS NULL`
	} else {
		query += `fingerprint = ?`
		args = append(args, *fp)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) poisonFingerprint(ctx context.Context, tool wire.Tool, sessionID, fileUUID string, fp *string) error {
	query := `UPDATE files SET poisoned = 1, updated_at = ? WHERE tool = ? AND session_id = ? AND file_uuid = ? AND `
	args := []any{formatTime(time.Now().UTC()), tool, sessionID, fileUUID}
	if fp == nil {
		query += `fingerprint IS NULL`
	} else {
		query += `fingerprint = ?`
		args = append(args, *fp)
	}
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// Serve runs the store on a listener. U11 can replace the listener with tsnet.
func (s *Store) Serve(l net.Listener) error {
	return http.Serve(l, s.Handler())
}

func (s *Store) writeError(w http.ResponseWriter, code wire.ErrorCode, tool wire.Tool, sessionID, fileUUID string, generation int, highWater int64, action, message string) {
	if action == "" {
		switch code {
		case wire.ErrOffsetGap:
			action = wire.ShipperActionRewind
		case wire.ErrNotFound:
			action = wire.ShipperActionStartFromZero
		}
	}
	writeJSON(w, code.HTTPStatus(), wire.ErrorResponse{
		WireVersion:   wire.Version,
		Code:          code,
		Message:       message,
		Tool:          tool,
		SessionID:     sessionID,
		FileUUID:      fileUUID,
		Generation:    generation,
		HighWater:     highWater,
		ShipperAction: action,
	})
}

func parseTool(raw string) (wire.Tool, bool) {
	switch wire.Tool(raw) {
	case wire.ToolClaude, wire.ToolCodex:
		return wire.Tool(raw), true
	default:
		return wire.Tool(raw), false
	}
}

func validUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

func parseOffset(raw string) (int64, error) {
	if raw == "" {
		return 0, errors.New("missing offset")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("invalid offset")
	}
	return n, nil
}

func contentTypeOK(raw string) bool {
	mt, _, err := mime.ParseMediaType(raw)
	return err == nil && mt == wire.ContentTypeBytes
}

func parseFingerprintHeaders(r *http.Request) (*string, error) {
	fp := r.Header.Get(wire.HeaderFingerprint)
	alg := r.Header.Get(wire.HeaderFingerprintAlgorithm)
	if fp == "" {
		if alg != "" {
			return nil, errors.New("fingerprint algorithm without fingerprint")
		}
		return nil, nil
	}
	if alg != wire.FingerprintAlgorithm {
		return nil, errors.New("missing or wrong fingerprint algorithm")
	}
	if !validFingerprint(fp) {
		return nil, errors.New("invalid fingerprint")
	}
	return &fp, nil
}

func validFingerprint(fp string) bool {
	if len(fp) != sha256.Size*2 || strings.ToLower(fp) != fp {
		return false
	}
	_, err := hex.DecodeString(fp)
	return err == nil
}

func readBody(r *http.Request) ([]byte, wire.ErrorCode, error) {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, wire.MaxPUTBody+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, wire.ErrMalformedRequest, err
	}
	if len(body) > wire.MaxPUTBody {
		return nil, wire.ErrBodyTooLarge, errors.New("PUT body exceeds maximum")
	}
	return body, "", nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
