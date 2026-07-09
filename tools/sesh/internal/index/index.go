// Package index parses mirrored transcript bytes into the disposable message
// index described by docs/specs/sesh-wire.md.
package index

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
	db         *sql.DB
	mirrorPath MirrorPath

	failWriteOnce bool
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

func (idx *Indexer) initSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sesh_index_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tool TEXT NOT NULL,
			logical_session_id TEXT NOT NULL,
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
	}
	for _, stmt := range stmts {
		if _, err := idx.db.ExecContext(ctx, stmt); err != nil {
			return normalizeDBError(err)
		}
	}
	return nil
}

// ProcessAppend indexes complete JSONL lines newly available for one mirrored
// generation. Trailing partial lines stay mirrored but unindexed.
func (idx *Indexer) ProcessAppend(ctx context.Context, ev wire.AppendEvent) error {
	if idx.failWriteOnce {
		idx.failWriteOnce = false
		_ = idx.markDirty(ctx, ev)
		return errors.New("injected index write failure")
	}
	rows, complete, err := idx.parseComplete(ctx, ev)
	if err != nil {
		_ = idx.markDirty(ctx, ev)
		return normalizeDBError(err)
	}
	if len(rows) == 0 {
		if err := idx.setCompleteOffset(ctx, ev, complete); err != nil {
			_ = idx.markDirty(ctx, ev)
			return normalizeDBError(err)
		}
		return normalizeDBError(idx.clearDirty(ctx, ev))
	}
	if err := idx.insertRows(ctx, rows); err != nil {
		_ = idx.markDirty(ctx, ev)
		return normalizeDBError(err)
	}
	if err := idx.setCompleteOffset(ctx, ev, complete); err != nil {
		_ = idx.markDirty(ctx, ev)
		return normalizeDBError(err)
	}
	if err := idx.unifyLogicalSessions(ctx); err != nil {
		_ = idx.markDirty(ctx, ev)
		return normalizeDBError(err)
	}
	return normalizeDBError(idx.clearDirty(ctx, ev))
}

// Reindex rebuilds disposable index rows from the mirror and store registry.
func (idx *Indexer) Reindex(ctx context.Context) error {
	for _, stmt := range []string{
		`DELETE FROM sesh_index_messages`,
		`DELETE FROM index_file_state`,
		`DELETE FROM quarantine_ledger`,
	} {
		if _, err := idx.db.ExecContext(ctx, stmt); err != nil {
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
		if err := idx.ProcessAppend(ctx, ev); err != nil {
			if errors.Is(err, ErrMirrorIncomplete) {
				continue
			}
			return normalizeDBError(err)
		}
	}
	return normalizeDBError(idx.unifyLogicalSessions(ctx))
}

type generation struct {
	Tool       wire.Tool
	SessionID  string
	FileUUID   string
	Generation int
	HighWater  int64
}

func (idx *Indexer) generations(ctx context.Context) ([]generation, error) {
	rows, err := idx.db.QueryContext(ctx, `SELECT tool, session_id, file_uuid, generation, high_water FROM files ORDER BY tool, session_id, file_uuid, generation`)
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

func (idx *Indexer) parseComplete(ctx context.Context, ev wire.AppendEvent) ([]wire.IndexMessage, int64, error) {
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
	buf := make([]byte, ev.ByteEnd-start)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, start, err
	}
	lastNL := bytes.LastIndexByte(buf, '\n')
	if lastNL < 0 {
		return nil, start, nil
	}
	completeBytes := buf[:lastNL+1]
	complete := start + int64(len(completeBytes))
	var rows []wire.IndexMessage
	lineStart := start
	lineOrdinal := baseOrdinal
	for len(completeBytes) > 0 {
		nl := bytes.IndexByte(completeBytes, '\n')
		line := completeBytes[:nl]
		lineEnd := lineStart + int64(len(line)) + 1
		var row wire.IndexMessage
		if len(line) > maxIndexedLineBytes {
			row = quarantineLine(ev, "line_too_long", lineOrdinal, lineStart, lineEnd)
		} else {
			row = parseLine(ev, append([]byte(nil), line...), lineOrdinal, lineStart, lineEnd)
		}
		rows = append(rows, row)
		completeBytes = completeBytes[nl+1:]
		lineStart = lineEnd
		lineOrdinal++
	}
	return rows, complete, nil
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

func (idx *Indexer) insertRows(ctx context.Context, rows []wire.IndexMessage) error {
	for _, row := range rows {
		if row.Quarantine {
			if err := idx.insertQuarantine(ctx, row); err != nil {
				return err
			}
			continue
		}
		if row.MessageUUID != "" {
			exists, err := idx.dedupExists(ctx, row)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			_, err = idx.db.ExecContext(ctx, `INSERT INTO sesh_index_messages
				(tool, logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '')`,
				row.Tool, row.LogicalSessionID, row.WireSessionID, row.EntryType, row.MessageUUID, row.FileUUID, row.Generation, row.Role, nullableTime(row.TimestampUTC), row.FileOrdinal, row.LineOrdinal, row.ByteStart, row.ByteEnd)
			if err != nil {
				return err
			}
			continue
		}
		_, err := idx.db.ExecContext(ctx, `INSERT INTO sesh_index_messages
			(tool, logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '')`,
			row.Tool, row.LogicalSessionID, row.WireSessionID, row.EntryType, row.MessageUUID, row.FileUUID, row.Generation, row.Role, nullableTime(row.TimestampUTC), row.FileOrdinal, row.LineOrdinal, row.ByteStart, row.ByteEnd)
		if err != nil {
			return err
		}
	}
	return nil
}

func (idx *Indexer) dedupExists(ctx context.Context, row wire.IndexMessage) (bool, error) {
	var n int
	err := idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sesh_index_messages
		WHERE quarantine = 0 AND tool = ? AND logical_session_id = ? AND entry_type = ? AND message_uuid = ?`,
		row.Tool, row.LogicalSessionID, row.EntryType, row.MessageUUID).Scan(&n)
	return n > 0, err
}

func (idx *Indexer) insertQuarantine(ctx context.Context, row wire.IndexMessage) error {
	_, err := idx.db.ExecContext(ctx, `INSERT INTO sesh_index_messages
		(tool, logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		row.Tool, row.LogicalSessionID, row.WireSessionID, row.EntryType, row.MessageUUID, row.FileUUID, row.Generation, row.Role, nullableTime(row.TimestampUTC), row.FileOrdinal, row.LineOrdinal, row.ByteStart, row.ByteEnd, row.QuarantineReason)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = idx.db.ExecContext(ctx, `INSERT INTO quarantine_ledger
		(observed_at, day, tool, wire_session_id, file_uuid, generation, line_ordinal, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(now), now.Format("2006-01-02"), row.Tool, row.WireSessionID, row.FileUUID, row.Generation, row.LineOrdinal, row.QuarantineReason)
	return err
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

func (idx *Indexer) completeOffset(ctx context.Context, ev wire.AppendEvent) (int64, error) {
	var offset int64
	err := idx.db.QueryRowContext(ctx, `SELECT complete_offset FROM index_file_state WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?`,
		ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation).Scan(&offset)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return offset, err
}

func (idx *Indexer) setCompleteOffset(ctx context.Context, ev wire.AppendEvent, offset int64) error {
	_, err := idx.db.ExecContext(ctx, `INSERT INTO index_file_state(tool, wire_session_id, file_uuid, generation, complete_offset)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tool, wire_session_id, file_uuid, generation) DO UPDATE SET complete_offset = excluded.complete_offset`,
		ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation, offset)
	return err
}

func (idx *Indexer) markDirty(ctx context.Context, ev wire.AppendEvent) error {
	_, err := idx.db.ExecContext(ctx, `UPDATE files SET dirty_for_reindex = 1 WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = ?`,
		ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation)
	return err
}

func (idx *Indexer) clearDirty(ctx context.Context, ev wire.AppendEvent) error {
	_, err := idx.db.ExecContext(ctx, `UPDATE files SET dirty_for_reindex = 0 WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = ?`,
		ev.Tool, ev.WireSessionID, ev.FileUUID, ev.Generation)
	return err
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
			if _, err := idx.db.ExecContext(ctx, `UPDATE sesh_index_messages SET logical_session_id = ? WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?`,
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
		for ordinal, f := range group {
			if _, err := idx.db.ExecContext(ctx, `UPDATE sesh_index_messages SET file_ordinal = ? WHERE tool = ? AND wire_session_id = ? AND file_uuid = ? AND generation = ?`,
				ordinal, f.tool, f.wireID, f.fileUUID, f.generation); err != nil {
				return err
			}
		}
	}
	return nil
}

func (idx *Indexer) dedupeAll(ctx context.Context) error {
	_, err := idx.db.ExecContext(ctx, `DELETE FROM sesh_index_messages
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

func (idx *Indexer) fileSummaries(ctx context.Context) ([]fileSummary, error) {
	rows, err := idx.db.QueryContext(ctx, `SELECT m.tool, m.wire_session_id, m.file_uuid, m.generation,
		MIN(m.logical_session_id), COALESCE(f.created_at, ''), MIN(m.byte_start)
		FROM sesh_index_messages m
		LEFT JOIN files f ON f.tool = m.tool AND f.session_id = m.wire_session_id AND f.file_uuid = m.file_uuid AND f.generation = m.generation
		WHERE m.quarantine = 0 GROUP BY m.tool, m.wire_session_id, m.file_uuid, m.generation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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
	if err := rows.Err(); err != nil {
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

func (idx *Indexer) overlapPairs(ctx context.Context, f fileSummary) (map[string]bool, error) {
	rows, err := idx.db.QueryContext(ctx, `SELECT entry_type, message_uuid FROM sesh_index_messages
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
	rows, err := idx.db.QueryContext(ctx, `SELECT day, COUNT(*) FROM quarantine_ledger GROUP BY day ORDER BY day`)
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
	rows, err := idx.db.QueryContext(ctx, `SELECT tool, logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, COALESCE(timestamp_utc, ''), file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason
		FROM sesh_index_messages ORDER BY tool, logical_session_id, wire_session_id, file_uuid, generation, line_ordinal, byte_start, entry_type, message_uuid`)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	h := sha256.New()
	n := 0
	vals := make([]any, 15)
	ptrs := make([]any, 15)
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
	err := idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sesh_index_messages`).Scan(&n)
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
