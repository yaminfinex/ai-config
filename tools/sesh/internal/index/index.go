// Package index parses mirrored transcript bytes into the disposable message
// index described by docs/specs/sesh-wire.md.
package index

import (
	"bufio"
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
	"os"
	"sort"
	"strings"
	"time"

	"sesh/internal/wire"
)

// MirrorPath resolves a mirrored generation path.
type MirrorPath func(tool wire.Tool, sessionID, fileUUID string, generation int) string

const maxIndexedLineBytes = 8 << 20

var (
	// ErrMirrorIncomplete means store metadata references bytes that are not
	// currently readable from the durable mirror.
	ErrMirrorIncomplete = errors.New("mirror incomplete")
	// ErrStoreBusy is returned when another sesh process holds the store DB.
	ErrStoreBusy = errors.New("store database busy")
)

// Indexer consumes append events and rebuilds disposable index tables.
type Indexer struct {
	db                 *sql.DB
	tx                 *sql.Tx
	mirrorPath         MirrorPath
	quarantineObserved map[quarantineKey]time.Time

	failWriteOnce bool

	// naiveMaintenance routes logical-session maintenance through the
	// pre-optimization statement shapes (unpinned corpus-walking plans,
	// unconditional whole-group rewrites). Test-only reference oracle: the
	// differential equivalence gate replays a churned corpus through both
	// shapes and requires byte-identical index outcomes, and the
	// bounded-append gates prove their detectors against this path.
	naiveMaintenance bool

	migrationReindexed bool

	// timing collects per-phase durations for one append transaction; nil
	// outside a processAppend call. Debug-level observability only.
	timing *appendTiming
}

// appendTiming is the per-phase cost breakdown of one append transaction,
// logged at debug level so a live store can show where its single write
// connection's hold time goes.
type appendTiming struct {
	parse   time.Duration
	inherit time.Duration
	insert  time.Duration
	unify   time.Duration
	dedupe  time.Duration
	rows    int
	// maintRows counts rows the logical-session maintenance actually wrote
	// (relabel + ordinal updates, dedupe deletes). A steady-state append —
	// no new logical linkage — writes at most the appended rows (stitching a
	// new row's file_ordinal), never the group or session: the regression
	// gate for per-append session-scale rewrites observes this seam.
	maintRows int64
}

// execMaintenance runs a maintenance write and records how many rows it
// actually changed (see appendTiming.maintRows).
func (idx *Indexer) execMaintenance(ctx context.Context, query string, args ...any) error {
	res, err := idx.execContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if idx.timing != nil {
		if n, err := res.RowsAffected(); err == nil {
			idx.timing.maintRows += n
		}
	}
	return nil
}

// New initializes index tables on the store database.
func New(ctx context.Context, db *sql.DB, mirrorPath MirrorPath) (*Indexer, error) {
	if db == nil {
		return nil, errors.New("index db is required")
	}
	if mirrorPath == nil {
		return nil, errors.New("mirror path resolver is required")
	}
	idx := &Indexer{db: db, mirrorPath: mirrorPath}
	return idx, idx.initSchema(ctx)
}

// InjectWriteFailureOnce makes the next index write fail after parsing. It is
// used to prove dirty-for-reindex behavior.
func (idx *Indexer) InjectWriteFailureOnce() {
	idx.failWriteOnce = true
}

func (idx *Indexer) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if idx.tx != nil {
		return idx.tx.ExecContext(ctx, query, args...)
	}
	return idx.db.ExecContext(ctx, query, args...)
}

func (idx *Indexer) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if idx.tx != nil {
		return idx.tx.QueryContext(ctx, query, args...)
	}
	return idx.db.QueryContext(ctx, query, args...)
}

func (idx *Indexer) queryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if idx.tx != nil {
		return idx.tx.QueryRowContext(ctx, query, args...)
	}
	return idx.db.QueryRowContext(ctx, query, args...)
}

func (idx *Indexer) prepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	if idx.tx != nil {
		return idx.tx.PrepareContext(ctx, query)
	}
	return idx.db.PrepareContext(ctx, query)
}

func (idx *Indexer) initSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sesh_index_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tool TEXT NOT NULL,
			logical_session_id TEXT NOT NULL,
			parsed_logical_session_id TEXT NOT NULL,
			wire_session_id TEXT NOT NULL,
			entry_type TEXT NOT NULL,
			message_uuid TEXT NOT NULL,
			file_uuid TEXT NOT NULL,
			generation INTEGER NOT NULL,
			role TEXT NOT NULL,
			timestamp_utc TEXT NULL,
			file_ordinal INTEGER NOT NULL,
			line_ordinal INTEGER NOT NULL,
			byte_start INTEGER NOT NULL,
			byte_end INTEGER NOT NULL,
			quarantine INTEGER NOT NULL,
			quarantine_reason TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS index_file_state (
			tool TEXT NOT NULL,
			wire_session_id TEXT NOT NULL,
			file_uuid TEXT NOT NULL,
			generation INTEGER NOT NULL,
			complete_offset INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(tool, wire_session_id, file_uuid, generation)
		)`,
		`CREATE TABLE IF NOT EXISTS quarantine_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			observed_at TEXT NOT NULL,
			day TEXT NOT NULL,
			tool TEXT NOT NULL,
			wire_session_id TEXT NOT NULL,
			file_uuid TEXT NOT NULL,
			generation INTEGER NOT NULL,
			line_ordinal INTEGER NOT NULL,
			reason TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS sesh_index_messages_file
			ON sesh_index_messages(tool, wire_session_id, file_uuid, generation)`,
		`CREATE INDEX IF NOT EXISTS sesh_index_messages_logical
			ON sesh_index_messages(tool, logical_session_id)`,
		`CREATE INDEX IF NOT EXISTS sesh_index_messages_overlap
			ON sesh_index_messages(tool, entry_type, message_uuid)`,
	}
	for _, stmt := range stmts {
		if _, err := idx.execContext(ctx, stmt); err != nil {
			return normalizeDBError(err)
		}
	}
	needsReindex, err := idx.ensureParsedLogicalColumn(ctx)
	if err != nil {
		return normalizeDBError(err)
	}
	if needsReindex {
		slog.Default().Info("index parsed logical session migration: rebuilding message index from mirror")
		if err := idx.Reindex(ctx); err != nil {
			return normalizeDBError(err)
		}
		idx.migrationReindexed = true
	}
	return nil
}

func (idx *Indexer) ensureParsedLogicalColumn(ctx context.Context) (bool, error) {
	rows, err := idx.queryContext(ctx, `PRAGMA table_info(sesh_index_messages)`)
	if err != nil {
		return false, err
	}
	hasColumn := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return false, err
		}
		if name == "parsed_logical_session_id" {
			hasColumn = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if hasColumn {
		return false, nil
	}
	if _, err := idx.execContext(ctx, `ALTER TABLE sesh_index_messages ADD COLUMN parsed_logical_session_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return false, err
	}
	return true, nil
}

// ProcessAppend indexes complete JSONL lines newly available for one mirrored
// generation. Trailing partial lines stay mirrored but unindexed.
func (idx *Indexer) ProcessAppend(ctx context.Context, ev wire.AppendEvent) error {
	return idx.processAppend(ctx, ev, false)
}

func (idx *Indexer) processAppend(ctx context.Context, ev wire.AppendEvent, rebuild bool) error {
	if idx.failWriteOnce {
		idx.failWriteOnce = false
		_ = idx.markDirty(ctx, ev)
		return errors.New("injected index write failure")
	}
	start := time.Now()
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		_ = idx.markDirty(ctx, ev)
		return normalizeDBError(err)
	}
	txAcquired := time.Now()
	// The applyAppend call tree must not mutate Indexer fields: txIdx is a shallow copy.
	txIdx := *idx
	txIdx.tx = tx
	txIdx.timing = &appendTiming{}
	if err := txIdx.applyAppend(ctx, ev, rebuild); err != nil {
		_ = tx.Rollback()
		_ = idx.markDirty(ctx, ev)
		return normalizeDBError(err)
	}
	commitStart := time.Now()
	if err := tx.Commit(); err != nil {
		_ = idx.markDirty(ctx, ev)
		return normalizeDBError(err)
	}
	// Identifier-free by design: session/file identities must not persist
	// in journal logs (corpus leakage into a different retention domain).
	// The phase laps are mutually exclusive and sum (with tx_wait and
	// commit) to total.
	slog.Debug("index append",
		"tool", ev.Tool, "generation", ev.Generation,
		"bytes", ev.ByteEnd-ev.ByteStart, "rows", txIdx.timing.rows,
		"tx_wait", txAcquired.Sub(start),
		"parse", txIdx.timing.parse, "inherit", txIdx.timing.inherit,
		"insert", txIdx.timing.insert, "unify", txIdx.timing.unify,
		"dedupe", txIdx.timing.dedupe, "maint_rows", txIdx.timing.maintRows,
		"commit", time.Since(commitStart), "total", time.Since(start))
	return nil
}

func (idx *Indexer) applyAppend(ctx context.Context, ev wire.AppendEvent, rebuild bool) error {
	phase := idx.timing.phaseClock()
	rows, complete, err := idx.parseComplete(ctx, ev)
	phase(&idx.timing.parse)
	if err != nil {
		return err
	}
	idx.timing.rows = len(rows)
	if len(rows) == 0 {
		if err := idx.setCompleteOffset(ctx, ev, complete); err != nil {
			return err
		}
		return idx.clearDirty(ctx, ev)
	}
	if !rebuild {
		err := idx.inheritFileLogicalSession(ctx, ev, rows)
		phase(&idx.timing.inherit)
		if err != nil {
			return err
		}
	}
	err = idx.insertRows(ctx, rows)
	phase(&idx.timing.insert)
	if err != nil {
		return err
	}
	if err := idx.setCompleteOffset(ctx, ev, complete); err != nil {
		return err
	}
	if !rebuild {
		err := idx.unifyConnectedLogicalSessions(ctx, ev)
		phase(&idx.timing.unify)
		if idx.timing != nil {
			// dedupe runs inside the unify call but is lapped separately;
			// subtract it so the reported phases are exclusive and additive.
			idx.timing.unify -= idx.timing.dedupe
		}
		if err != nil {
			return err
		}
	}
	return idx.clearDirty(ctx, ev)
}

// phaseClock returns a lap timer: each call stores the time since the
// previous call into the given slot. Safe on a nil receiver (no-op).
func (t *appendTiming) phaseClock() func(*time.Duration) {
	last := time.Now()
	return func(slot *time.Duration) {
		now := time.Now()
		if t != nil {
			*slot = now.Sub(last)
		}
		last = now
	}
}

// Reindex rebuilds disposable index rows from the mirror and store registry.
func (idx *Indexer) Reindex(ctx context.Context) error {
	observed, err := idx.quarantineObservedTimes(ctx)
	if err != nil {
		return normalizeDBError(err)
	}
	idx.quarantineObserved = observed
	defer func() {
		idx.quarantineObserved = nil
	}()
	for _, stmt := range []string{
		`DELETE FROM sesh_index_messages`,
		`DELETE FROM index_file_state`,
		`DELETE FROM quarantine_ledger`,
	} {
		if _, err := idx.execContext(ctx, stmt); err != nil {
			return normalizeDBError(err)
		}
	}
	gens, err := idx.generations(ctx)
	if err != nil {
		return normalizeDBError(err)
	}
	for _, gen := range gens {
		ev := wire.AppendEvent{
			Tool:          gen.Tool,
			WireSessionID: gen.SessionID,
			FileUUID:      gen.FileUUID,
			Generation:    gen.Generation,
			ByteStart:     0,
			ByteEnd:       gen.HighWater,
		}
		if err := idx.processAppend(ctx, ev, true); err != nil {
			if errors.Is(err, ErrMirrorIncomplete) {
				continue
			}
			return normalizeDBError(err)
		}
	}
	if err := idx.unifyLogicalSessions(ctx); err != nil {
		return normalizeDBError(err)
	}
	return nil
}

type generation struct {
	Tool       wire.Tool
	SessionID  string
	FileUUID   string
	Generation int
	HighWater  int64
}

func (idx *Indexer) generations(ctx context.Context) ([]generation, error) {
	rows, err := idx.queryContext(ctx, `SELECT tool, session_id, file_uuid, generation, high_water FROM files ORDER BY tool, session_id, file_uuid, generation`)
	if err != nil {
		return nil, normalizeDBError(err)
	}
	defer rows.Close()
	var out []generation
	for rows.Next() {
		var gen generation
		if err := rows.Scan(&gen.Tool, &gen.SessionID, &gen.FileUUID, &gen.Generation, &gen.HighWater); err != nil {
			return nil, err
		}
		out = append(out, gen)
	}
	return out, rows.Err()
}

func (idx *Indexer) parseComplete(ctx context.Context, ev wire.AppendEvent) ([]indexedMessage, int64, error) {
	start, err := idx.completeOffset(ctx, ev)
	if err != nil {
		return nil, 0, err
	}
	if ev.ByteEnd <= start {
		return nil, start, nil
	}
	path := idx.mirrorPath(ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation)
	f, err := os.Open(path)
	if err != nil {
		return nil, start, fmt.Errorf("%w: %v", ErrMirrorIncomplete, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, start, fmt.Errorf("%w: %v", ErrMirrorIncomplete, err)
	}
	if st.Size() < ev.ByteEnd {
		return nil, start, fmt.Errorf("%w: mirror short: have %d want %d", ErrMirrorIncomplete, st.Size(), ev.ByteEnd)
	}
	baseOrdinal, err := lineOrdinalAt(f, start)
	if err != nil {
		return nil, start, err
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, start, err
	}
	var rows []indexedMessage
	lineStart := start
	lineOrdinal := baseOrdinal
	complete := start
	reader := bufio.NewReaderSize(io.NewSectionReader(f, start, ev.ByteEnd-start), 64*1024)
	for {
		line, lineBytes, ok, err := readCompleteLine(reader)
		if err != nil {
			return nil, start, err
		}
		if !ok {
			break
		}
		lineEnd := lineStart + lineBytes
		var row wire.IndexMessage
		if line == nil {
			row = quarantineLine(ev, "line_too_long", lineOrdinal, lineStart, lineEnd)
		} else {
			row = parseLine(ev, line, lineOrdinal, lineStart, lineEnd)
		}
		rows = append(rows, indexedMessage{IndexMessage: row, ParsedLogicalSessionID: row.LogicalSessionID})
		lineStart = lineEnd
		complete = lineEnd
		lineOrdinal++
	}
	return rows, complete, nil
}

type indexedMessage struct {
	wire.IndexMessage
	ParsedLogicalSessionID string
}

func readCompleteLine(r *bufio.Reader) ([]byte, int64, bool, error) {
	var line []byte
	var lineBytes int64
	tooLong := false
	for {
		frag, err := r.ReadSlice('\n')
		switch {
		case err == nil:
			body := frag[:len(frag)-1]
			lineBytes += int64(len(body)) + 1
			if tooLong || len(line)+len(body) > maxIndexedLineBytes {
				return nil, lineBytes, true, nil
			}
			line = append(line, body...)
			return line, lineBytes, true, nil
		case errors.Is(err, bufio.ErrBufferFull):
			lineBytes += int64(len(frag))
			if tooLong || len(line)+len(frag) > maxIndexedLineBytes {
				tooLong = true
				line = nil
				continue
			}
			if line == nil {
				line = make([]byte, 0, maxIndexedLineBytes)
			}
			line = append(line, frag...)
		case errors.Is(err, io.EOF):
			return nil, 0, false, nil
		default:
			return nil, 0, false, err
		}
	}
}

func lineOrdinalAt(f *os.File, offset int64) (int64, error) {
	if offset == 0 {
		return 0, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	buf := make([]byte, 32*1024)
	var ordinal int64
	var read int64
	for read < offset {
		want := int64(len(buf))
		if remaining := offset - read; remaining < want {
			want = remaining
		}
		n, err := io.ReadFull(f, buf[:want])
		ordinal += int64(bytes.Count(buf[:n], []byte{'\n'}))
		read += int64(n)
		if err != nil {
			return 0, err
		}
	}
	return ordinal, nil
}

func parseLine(ev wire.AppendEvent, line []byte, lineOrdinal int64, byteStart, byteEnd int64) wire.IndexMessage {
	parsed, err := parseToolLine(ev.Tool, line)
	row := wire.IndexMessage{
		Tool:             ev.Tool,
		LogicalSessionID: parsed.LogicalSessionID,
		WireSessionID:    ev.WireSessionID,
		EntryType:        parsed.EntryType,
		MessageUUID:      parsed.MessageUUID,
		FileUUID:         ev.FileUUID,
		Generation:       ev.Generation,
		Role:             parsed.Role,
		TimestampUTC:     parsed.TimestampUTC,
		FileOrdinal:      int64(ev.Generation),
		LineOrdinal:      lineOrdinal,
		ByteStart:        byteStart,
		ByteEnd:          byteEnd,
	}
	if row.LogicalSessionID == "" {
		row.LogicalSessionID = ev.WireSessionID
	}
	if row.EntryType == "" {
		row.EntryType = "unknown"
	}
	if row.Role == "" {
		row.Role = "unknown"
	}
	if err != nil {
		row.LogicalSessionID = ev.WireSessionID
		row.EntryType = "unparseable"
		row.Role = "unknown"
		row.Quarantine = true
		row.QuarantineReason = err.Error()
	}
	return row
}

func quarantineLine(ev wire.AppendEvent, reason string, lineOrdinal int64, byteStart, byteEnd int64) wire.IndexMessage {
	return wire.IndexMessage{
		Tool:             ev.Tool,
		LogicalSessionID: ev.WireSessionID,
		WireSessionID:    ev.WireSessionID,
		EntryType:        "unparseable",
		FileUUID:         ev.FileUUID,
		Generation:       ev.Generation,
		Role:             "unknown",
		FileOrdinal:      int64(ev.Generation),
		LineOrdinal:      lineOrdinal,
		ByteStart:        byteStart,
		ByteEnd:          byteEnd,
		Quarantine:       true,
		QuarantineReason: reason,
	}
}

type parsedLine struct {
	LogicalSessionID string
	EntryType        string
	MessageUUID      string
	Role             string
	TimestampUTC     *time.Time
}

func parseToolLine(tool wire.Tool, line []byte) (parsedLine, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return parsedLine{}, fmt.Errorf("invalid_json")
	}
	out := parsedLine{
		LogicalSessionID: stringField(raw, "sessionId"),
		EntryType:        stringField(raw, "type"),
		MessageUUID:      stringField(raw, "uuid"),
		Role:             "unknown",
		TimestampUTC:     parseTime(stringField(raw, "timestamp")),
	}
	switch tool {
	case wire.ToolClaude:
		if msg, ok := raw["message"]; ok {
			var m map[string]json.RawMessage
			if json.Unmarshal(msg, &m) == nil {
				if role := stringField(m, "role"); role != "" {
					out.Role = role
				}
			}
		}
	case wire.ToolCodex:
		if payload, ok := raw["payload"]; ok {
			var p map[string]json.RawMessage
			if json.Unmarshal(payload, &p) == nil {
				if out.EntryType == "session_meta" {
					out.LogicalSessionID = stringField(p, "id")
				}
				if out.MessageUUID == "" {
					out.MessageUUID = stringField(p, "id")
				}
				if role := stringField(p, "role"); role != "" {
					out.Role = role
				}
				if item, ok := p["item"]; ok {
					var it map[string]json.RawMessage
					if json.Unmarshal(item, &it) == nil {
						if out.MessageUUID == "" {
							out.MessageUUID = stringField(it, "id")
						}
						if role := stringField(it, "role"); role != "" {
							out.Role = role
						}
					}
				}
			}
		}
	}
	return out, nil
}

func stringField(raw map[string]json.RawMessage, key string) string {
	if v, ok := raw[key]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
	}
	return ""
}

func parseTime(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		t, err := time.Parse(layout, raw)
		if err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

func (idx *Indexer) insertRows(ctx context.Context, rows []indexedMessage) error {
	dedupInsert, err := idx.prepareContext(ctx, `INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ''
		WHERE NOT EXISTS (
			SELECT 1 FROM sesh_index_messages
			WHERE quarantine = 0 AND tool = ? AND logical_session_id = ? AND entry_type = ? AND message_uuid = ?
		)`)
	if err != nil {
		return err
	}
	defer dedupInsert.Close()
	plainInsert, err := idx.prepareContext(ctx, `INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '')`)
	if err != nil {
		return err
	}
	defer plainInsert.Close()
	quarantineInsert, err := idx.prepareContext(ctx, `INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`)
	if err != nil {
		return err
	}
	defer quarantineInsert.Close()
	ledgerInsert, err := idx.prepareContext(ctx, `INSERT INTO quarantine_ledger
		(observed_at, day, tool, wire_session_id, file_uuid, generation, line_ordinal, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ledgerInsert.Close()

	for _, row := range rows {
		if row.Quarantine {
			if err := idx.insertQuarantine(ctx, quarantineInsert, ledgerInsert, row.IndexMessage); err != nil {
				return err
			}
			continue
		}
		args := []any{row.Tool, row.LogicalSessionID, row.ParsedLogicalSessionID, row.WireSessionID, row.EntryType, row.MessageUUID,
			row.FileUUID, row.Generation, row.Role, nullableTime(row.TimestampUTC), row.FileOrdinal, row.LineOrdinal, row.ByteStart, row.ByteEnd}
		if row.MessageUUID != "" {
			_, err = dedupInsert.ExecContext(ctx, append(args, row.Tool, row.LogicalSessionID, row.EntryType, row.MessageUUID)...)
			if err != nil {
				return err
			}
			continue
		}
		_, err := plainInsert.ExecContext(ctx, args...)
		if err != nil {
			return err
		}
	}
	return nil
}

func (idx *Indexer) insertQuarantine(ctx context.Context, messageStmt, ledgerStmt *sql.Stmt, row wire.IndexMessage) error {
	_, err := messageStmt.ExecContext(ctx,
		row.Tool, row.LogicalSessionID, row.LogicalSessionID, row.WireSessionID, row.EntryType, row.MessageUUID, row.FileUUID, row.Generation, row.Role, nullableTime(row.TimestampUTC), row.FileOrdinal, row.LineOrdinal, row.ByteStart, row.ByteEnd, row.QuarantineReason)
	if err != nil {
		return err
	}
	observed := time.Now().UTC()
	if idx.quarantineObserved != nil {
		if t, ok := idx.quarantineObserved[newQuarantineKey(row)]; ok {
			observed = t
		}
	}
	_, err = ledgerStmt.ExecContext(ctx,
		formatTime(observed), observed.Format("2006-01-02"), row.Tool, row.WireSessionID, row.FileUUID, row.Generation, row.LineOrdinal, row.QuarantineReason)
	return err
}

type quarantineKey struct {
	tool        wire.Tool
	wireID      string
	fileUUID    string
	generation  int
	lineOrdinal int64
	reason      string
}

func newQuarantineKey(row wire.IndexMessage) quarantineKey {
	return quarantineKey{
		tool:        row.Tool,
		wireID:      row.WireSessionID,
		fileUUID:    row.FileUUID,
		generation:  row.Generation,
		lineOrdinal: row.LineOrdinal,
		reason:      row.QuarantineReason,
	}
}

func (idx *Indexer) quarantineObservedTimes(ctx context.Context) (map[quarantineKey]time.Time, error) {
	rows, err := idx.queryContext(ctx, `SELECT observed_at, tool, wire_session_id, file_uuid, generation, line_ordinal, reason FROM quarantine_ledger`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[quarantineKey]time.Time{}
	for rows.Next() {
		var raw string
		var key quarantineKey
		if err := rows.Scan(&raw, &key.tool, &key.wireID, &key.fileUUID, &key.generation, &key.lineOrdinal, &key.reason); err != nil {
			return nil, err
		}
		observed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			continue
		}
		out[key] = observed.UTC()
	}
	return out, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func (idx *Indexer) inheritFileLogicalSession(ctx context.Context, ev wire.AppendEvent, rows []indexedMessage) error {
	existing, err := idx.fileLogicalSessions(ctx, ev)
	if err != nil {
		return err
	}
	if len(existing) != 1 || existing[0] == ev.WireSessionID {
		return nil
	}
	for i := range rows {
		if !rows[i].Quarantine {
			rows[i].LogicalSessionID = existing[0]
		}
	}
	return nil
}

// INDEXED BY is load-bearing: without it the planner prefers the logical
// index to satisfy DISTINCT/ORDER BY and walks every row of the tool — a
// corpus scan per append (see the write-side plan gate). The naive shape is
// the pre-optimization reference oracle (see Indexer.naiveMaintenance).
const (
	fileLogicalSessionsSQL = `SELECT DISTINCT logical_session_id
		FROM sesh_index_messages INDEXED BY sesh_index_messages_file
		WHERE quarantine = 0 AND tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?
			AND logical_session_id <> parsed_logical_session_id
			AND parsed_logical_session_id <> ''
		ORDER BY logical_session_id`
	naiveFileLogicalSessionsSQL = `SELECT DISTINCT logical_session_id FROM sesh_index_messages
		WHERE quarantine = 0 AND tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?
			AND logical_session_id <> parsed_logical_session_id
			AND parsed_logical_session_id <> ''
		ORDER BY logical_session_id`
)

func (idx *Indexer) fileLogicalSessions(ctx context.Context, ev wire.AppendEvent) ([]string, error) {
	query := fileLogicalSessionsSQL
	if idx.naiveMaintenance {
		query = naiveFileLogicalSessionsSQL
	}
	rows, err := idx.queryContext(ctx, query, ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var logical string
		if err := rows.Scan(&logical); err != nil {
			return nil, err
		}
		out = append(out, logical)
	}
	return out, rows.Err()
}

func (idx *Indexer) completeOffset(ctx context.Context, ev wire.AppendEvent) (int64, error) {
	var offset int64
	err := idx.queryRowContext(ctx, `SELECT complete_offset FROM index_file_state WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?`,
		ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation).Scan(&offset)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return offset, err
}

func (idx *Indexer) setCompleteOffset(ctx context.Context, ev wire.AppendEvent, offset int64) error {
	_, err := idx.execContext(ctx, `INSERT INTO index_file_state(tool, wire_session_id, file_uuid, generation, complete_offset)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tool, wire_session_id, file_uuid, generation) DO UPDATE SET complete_offset = excluded.complete_offset`,
		ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation, offset)
	return err
}

func (idx *Indexer) markDirty(ctx context.Context, ev wire.AppendEvent) error {
	_, err := idx.execContext(ctx, `UPDATE files SET dirty_for_reindex = 1 WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = ?`,
		ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation)
	return err
}

func (idx *Indexer) clearDirty(ctx context.Context, ev wire.AppendEvent) error {
	_, err := idx.execContext(ctx, `UPDATE files SET dirty_for_reindex = 0 WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = ?`,
		ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation)
	return err
}

func (idx *Indexer) unifyConnectedLogicalSessions(ctx context.Context, ev wire.AppendEvent) error {
	start, ok, err := idx.fileSummary(ctx, ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation)
	if err != nil || !ok {
		return err
	}
	group, err := idx.connectedFiles(ctx, start)
	if err != nil {
		return err
	}
	if len(group) == 0 {
		return nil
	}
	sort.Slice(group, func(i, j int) bool {
		if group[i].firstIngest != group[j].firstIngest {
			return group[i].firstIngest < group[j].firstIngest
		}
		return group[i].key < group[j].key
	})
	canonical := group[0].logicalID
	if canonical == "" {
		canonical = group[0].wireID
	}
	if len(group) > 1 {
		for _, f := range group {
			if err := idx.relabelFile(ctx, canonical, f); err != nil {
				return err
			}
		}
	}
	if err := idx.updateFileOrdinalsForFiles(ctx, group); err != nil {
		return err
	}
	dedupeStart := time.Now()
	err = idx.dedupeLogical(ctx, group[0].tool, canonical)
	if idx.timing != nil {
		idx.timing.dedupe = time.Since(dedupeStart)
	}
	return err
}

func (idx *Indexer) connectedFiles(ctx context.Context, start fileSummary) ([]fileSummary, error) {
	seen := map[string]fileSummary{start.key: start}
	queue := []fileSummary{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, fn := range []func(context.Context, fileSummary) ([]fileSummary, error){
			idx.sameLogicalFiles,
			idx.overlappingFiles,
		} {
			next, err := fn(ctx, cur)
			if err != nil {
				return nil, err
			}
			for _, f := range next {
				if _, ok := seen[f.key]; ok {
					continue
				}
				seen[f.key] = f
				queue = append(queue, f)
			}
		}
	}
	out := make([]fileSummary, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	return out, nil
}

// INDEXED BY is load-bearing: without it the planner prefers the file index
// to satisfy GROUP BY and walks every row of the tool instead of seeking the
// logical session (see the write-side plan gate). The naive shape is the
// pre-optimization reference oracle (see Indexer.naiveMaintenance).
const (
	sameLogicalFilesSQL = `SELECT m.tool, m.wire_session_id, m.file_uuid, m.generation,
		MIN(m.logical_session_id), COALESCE(f.created_at, ''), MIN(m.byte_start)
		FROM sesh_index_messages m INDEXED BY sesh_index_messages_logical
		LEFT JOIN files f ON f.tool = m.tool AND f.session_id = m.wire_session_id AND f.file_uuid = m.file_uuid AND f.generation = m.generation
		WHERE m.quarantine = 0 AND m.tool = ? AND m.logical_session_id = ?
		GROUP BY m.tool, m.wire_session_id, m.file_uuid, m.generation`
	naiveSameLogicalFilesSQL = `SELECT m.tool, m.wire_session_id, m.file_uuid, m.generation,
		MIN(m.logical_session_id), COALESCE(f.created_at, ''), MIN(m.byte_start)
		FROM sesh_index_messages m
		LEFT JOIN files f ON f.tool = m.tool AND f.session_id = m.wire_session_id AND f.file_uuid = m.file_uuid AND f.generation = m.generation
		WHERE m.quarantine = 0 AND m.tool = ? AND m.logical_session_id = ?
		GROUP BY m.tool, m.wire_session_id, m.file_uuid, m.generation`
)

func (idx *Indexer) sameLogicalFiles(ctx context.Context, f fileSummary) ([]fileSummary, error) {
	query := sameLogicalFilesSQL
	if idx.naiveMaintenance {
		query = naiveSameLogicalFilesSQL
	}
	rows, err := idx.queryContext(ctx, query, f.tool, f.logicalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFileSummaries(rows)
}

func (idx *Indexer) overlappingFiles(ctx context.Context, f fileSummary) ([]fileSummary, error) {
	rows, err := idx.queryContext(ctx, `SELECT m.tool, m.wire_session_id, m.file_uuid, m.generation,
		MIN(m.logical_session_id), COALESCE(reg.created_at, ''), MIN(m.byte_start)
		FROM (
			SELECT tool, entry_type, message_uuid
			FROM sesh_index_messages INDEXED BY sesh_index_messages_file
			WHERE quarantine = 0
				AND message_uuid <> ''
				AND tool = ?
				AND wire_session_id = ?
				AND file_uuid = ?
				AND generation = ?
		) seed
		JOIN sesh_index_messages m INDEXED BY sesh_index_messages_overlap
			ON m.tool = seed.tool AND m.entry_type = seed.entry_type AND m.message_uuid = seed.message_uuid
		LEFT JOIN files reg ON reg.tool = m.tool AND reg.session_id = m.wire_session_id AND reg.file_uuid = m.file_uuid AND reg.generation = m.generation
		WHERE m.quarantine = 0
			AND m.message_uuid <> ''
		GROUP BY m.tool, m.wire_session_id, m.file_uuid, m.generation
		HAVING COUNT(DISTINCT m.entry_type || char(31) || m.message_uuid) >= 2`,
		f.tool, f.wireID, f.fileUUID, f.generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFileSummaries(rows)
}

func (idx *Indexer) unifyLogicalSessions(ctx context.Context) error {
	files, err := idx.fileSummaries(ctx)
	if err != nil {
		return err
	}
	parent := map[string]string{}
	find := func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for _, f := range files {
		parent[f.key] = f.key
	}
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[i].tool != files[j].tool {
				continue
			}
			if files[i].logicalID != "" && files[i].logicalID == files[j].logicalID {
				union(files[i].key, files[j].key)
				continue
			}
			if overlapCount(files[i].pairs, files[j].pairs) >= 2 {
				union(files[i].key, files[j].key)
			}
		}
	}
	groups := map[string][]fileSummary{}
	for _, f := range files {
		groups[find(f.key)] = append(groups[find(f.key)], f)
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].firstIngest != group[j].firstIngest {
				return group[i].firstIngest < group[j].firstIngest
			}
			return group[i].key < group[j].key
		})
		canonical := group[0].logicalID
		if canonical == "" {
			canonical = group[0].wireID
		}
		for _, f := range group {
			if _, err := idx.execContext(ctx, `UPDATE sesh_index_messages SET logical_session_id = ? WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?`,
				canonical, f.tool, f.wireID, f.fileUUID, f.generation); err != nil {
				return err
			}
		}
	}
	if err := idx.updateFileOrdinals(ctx); err != nil {
		return err
	}
	return idx.dedupeAll(ctx)
}

func (idx *Indexer) updateFileOrdinals(ctx context.Context) error {
	files, err := idx.fileSummaries(ctx)
	if err != nil {
		return err
	}
	groups := map[string][]fileSummary{}
	for _, f := range files {
		key := string(f.tool) + "\x00" + f.logicalID
		groups[key] = append(groups[key], f)
	}
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			if group[i].firstIngest != group[j].firstIngest {
				return group[i].firstIngest < group[j].firstIngest
			}
			return group[i].key < group[j].key
		})
		if err := idx.updateFileOrdinalsForFiles(ctx, group); err != nil {
			return err
		}
	}
	return nil
}

// relabelFile and updateFileOrdinalsForFiles carry a final predicate that
// excludes rows already at the target value, so a steady-state append — no
// new logical linkage — rewrites zero rows instead of rewriting the whole
// group per append. The resulting table state is identical either way: both
// target columns are NOT NULL in the frozen schema, so <> cannot filter an
// old NULL that the unconditional UPDATE would repair. The naive shapes are
// the pre-optimization reference oracle (see Indexer.naiveMaintenance).
func (idx *Indexer) relabelFile(ctx context.Context, canonical string, f fileSummary) error {
	if idx.naiveMaintenance {
		return idx.execMaintenance(ctx, `UPDATE sesh_index_messages SET logical_session_id = ? WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?`,
			canonical, f.tool, f.wireID, f.fileUUID, f.generation)
	}
	return idx.execMaintenance(ctx, `UPDATE sesh_index_messages SET logical_session_id = ? WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ? AND logical_session_id <> ?`,
		canonical, f.tool, f.wireID, f.fileUUID, f.generation, canonical)
}

func (idx *Indexer) updateFileOrdinalsForFiles(ctx context.Context, group []fileSummary) error {
	for ordinal, f := range group {
		if idx.naiveMaintenance {
			if err := idx.execMaintenance(ctx, `UPDATE sesh_index_messages SET file_ordinal = ? WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?`,
				ordinal, f.tool, f.wireID, f.fileUUID, f.generation); err != nil {
				return err
			}
			continue
		}
		if err := idx.execMaintenance(ctx, `UPDATE sesh_index_messages SET file_ordinal = ? WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ? AND file_ordinal <> ?`,
			ordinal, f.tool, f.wireID, f.fileUUID, f.generation, ordinal); err != nil {
			return err
		}
	}
	return nil
}

func (idx *Indexer) dedupeAll(ctx context.Context) error {
	_, err := idx.execContext(ctx, `DELETE FROM sesh_index_messages
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
					ROW_NUMBER() OVER (
						PARTITION BY tool, logical_session_id, entry_type, message_uuid
						ORDER BY timestamp_utc IS NULL, timestamp_utc, file_ordinal, line_ordinal, file_uuid, generation, id
					) AS rn
				FROM sesh_index_messages
				WHERE quarantine = 0 AND message_uuid <> ''
			)
			WHERE rn > 1
		)`)
	return err
}

// INDEXED BY is load-bearing: without it the planner feeds the window from
// the overlap index at its tool=? prefix — a corpus scan per append (see the
// write-side plan gate). The naive shape is the pre-optimization reference
// oracle (see Indexer.naiveMaintenance).
const (
	dedupeLogicalSQL = `DELETE FROM sesh_index_messages
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
					ROW_NUMBER() OVER (
						PARTITION BY tool, logical_session_id, entry_type, message_uuid
						ORDER BY timestamp_utc IS NULL, timestamp_utc, file_ordinal, line_ordinal, file_uuid, generation, id
					) AS rn
				FROM sesh_index_messages INDEXED BY sesh_index_messages_logical
				WHERE quarantine = 0 AND message_uuid <> '' AND tool = ? AND logical_session_id = ?
			)
			WHERE rn > 1
		)`
	naiveDedupeLogicalSQL = `DELETE FROM sesh_index_messages
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
					ROW_NUMBER() OVER (
						PARTITION BY tool, logical_session_id, entry_type, message_uuid
						ORDER BY timestamp_utc IS NULL, timestamp_utc, file_ordinal, line_ordinal, file_uuid, generation, id
					) AS rn
				FROM sesh_index_messages
				WHERE quarantine = 0 AND message_uuid <> '' AND tool = ? AND logical_session_id = ?
			)
			WHERE rn > 1
		)`
)

func (idx *Indexer) dedupeLogical(ctx context.Context, tool wire.Tool, logical string) error {
	query := dedupeLogicalSQL
	if idx.naiveMaintenance {
		query = naiveDedupeLogicalSQL
	}
	return idx.execMaintenance(ctx, query, tool, logical)
}

type fileSummary struct {
	key         string
	tool        wire.Tool
	wireID      string
	fileUUID    string
	generation  int
	logicalID   string
	firstIngest string
	pairs       map[string]bool
}

func (idx *Indexer) fileSummary(ctx context.Context, tool wire.Tool, wireID, fileUUID string, generation int) (fileSummary, bool, error) {
	rows, err := idx.queryContext(ctx, `SELECT m.tool, m.wire_session_id, m.file_uuid, m.generation,
		MIN(m.logical_session_id), COALESCE(f.created_at, ''), MIN(m.byte_start)
		FROM sesh_index_messages m
		LEFT JOIN files f ON f.tool = m.tool AND f.session_id = m.wire_session_id AND f.file_uuid = m.file_uuid AND f.generation = m.generation
		WHERE m.quarantine = 0 AND m.tool = ? AND m.wire_session_id = ? AND m.file_uuid = ? AND m.generation = ?
		GROUP BY m.tool, m.wire_session_id, m.file_uuid, m.generation`,
		tool, wireID, fileUUID, generation)
	if err != nil {
		return fileSummary{}, false, err
	}
	defer rows.Close()
	files, err := scanFileSummaries(rows)
	if err != nil || len(files) == 0 {
		return fileSummary{}, false, err
	}
	return files[0], true, nil
}

func (idx *Indexer) fileSummaries(ctx context.Context) ([]fileSummary, error) {
	rows, err := idx.queryContext(ctx, `SELECT m.tool, m.wire_session_id, m.file_uuid, m.generation,
		MIN(m.logical_session_id), COALESCE(f.created_at, ''), MIN(m.byte_start)
		FROM sesh_index_messages m
		LEFT JOIN files f ON f.tool = m.tool AND f.session_id = m.wire_session_id AND f.file_uuid = m.file_uuid AND f.generation = m.generation
		WHERE m.quarantine = 0 GROUP BY m.tool, m.wire_session_id, m.file_uuid, m.generation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files, err := scanFileSummaries(rows)
	if err != nil {
		return nil, err
	}
	for i := range files {
		pairs, err := idx.overlapPairs(ctx, files[i])
		if err != nil {
			return nil, err
		}
		files[i].pairs = pairs
	}
	return files, nil
}

type summaryRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanFileSummaries(rows summaryRows) ([]fileSummary, error) {
	var files []fileSummary
	for rows.Next() {
		var f fileSummary
		var first int64
		var created string
		if err := rows.Scan(&f.tool, &f.wireID, &f.fileUUID, &f.generation, &f.logicalID, &created, &first); err != nil {
			return nil, err
		}
		f.key = fmt.Sprintf("%s/%s/%s/%d", f.tool, f.wireID, f.fileUUID, f.generation)
		if f.generation == 0 {
			f.firstIngest = created + fmt.Sprintf("/%020d", first)
		} else {
			f.firstIngest = fmt.Sprintf("z%020d", first)
		}
		f.pairs = map[string]bool{}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (idx *Indexer) overlapPairs(ctx context.Context, f fileSummary) (map[string]bool, error) {
	rows, err := idx.queryContext(ctx, `SELECT entry_type, message_uuid FROM sesh_index_messages
		WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ? AND quarantine = 0 AND message_uuid <> ''`,
		f.tool, f.wireID, f.fileUUID, f.generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var typ, id string
		if err := rows.Scan(&typ, &id); err != nil {
			return nil, err
		}
		out[typ+"\x00"+id] = true
	}
	return out, rows.Err()
}

func overlapCount(a, b map[string]bool) int {
	n := 0
	for p := range a {
		if b[p] {
			n++
		}
	}
	return n
}

// QuarantineCount is one day bucket from the quarantine ledger.
type QuarantineCount struct {
	Day   string
	Count int
}

// QuarantineCounts returns quarantine ledger counts by day.
func (idx *Indexer) QuarantineCounts(ctx context.Context) ([]QuarantineCount, error) {
	rows, err := idx.queryContext(ctx, `SELECT day, COUNT(*) FROM quarantine_ledger GROUP BY day ORDER BY day`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuarantineCount
	for rows.Next() {
		var c QuarantineCount
		if err := rows.Scan(&c.Day, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Checksum summarizes current index content ignoring store-local row ids.
func (idx *Indexer) Checksum(ctx context.Context) (string, int, error) {
	rows, err := idx.queryContext(ctx, `SELECT tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, COALESCE(timestamp_utc, ''), file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason
		FROM sesh_index_messages ORDER BY tool, logical_session_id, wire_session_id, file_uuid, generation, line_ordinal, byte_start, entry_type, message_uuid`)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	h := sha256.New()
	n := 0
	vals := make([]any, 16)
	ptrs := make([]any, 16)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return "", 0, err
		}
		for _, v := range vals {
			fmt.Fprintf(h, "%v\x1f", v)
		}
		fmt.Fprint(h, "\n")
		n++
	}
	return hex.EncodeToString(h.Sum(nil)), n, rows.Err()
}

// RowCount returns the number of indexed message rows.
func (idx *Indexer) RowCount(ctx context.Context) (int, error) {
	var n int
	err := idx.queryRowContext(ctx, `SELECT COUNT(*) FROM sesh_index_messages`).Scan(&n)
	return n, err
}

func normalizeDBError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "database is busy") {
		return fmt.Errorf("%w: stop live sesh serve or retry after it finishes writing", ErrStoreBusy)
	}
	return err
}
