package index

// The bounded-append gate: index maintenance per append must stay
// proportional to the appended bytes and the touched logical session, never
// the corpus. The evidence is structural, mirroring the surface's
// large-corpus gate: a recording driver captures every SQL statement one
// steady-state append runs, every storage access in every statement's
// EXPLAIN QUERY PLAN must be a full-key seek on its pinned index (a
// prefix-only SEARCH `(tool=?)` is a corpus walk wearing a SEARCH costume),
// and the journaled maint_rows seam must report zero maintenance writes for
// an append that introduces no new logical linkage. Both detectors are
// proven against the naive pre-optimization shapes.

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"sesh/internal/sqlitedsn"
	"sesh/internal/store"
	"sesh/internal/wire"
)

// --- recording driver: captures every SQL statement the indexer runs ---

type writeQueryLog struct {
	mu      sync.Mutex
	queries []string
}

func (l *writeQueryLog) add(q string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queries = append(l.queries, q)
}

func (l *writeQueryLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queries = nil
}

func (l *writeQueryLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.queries...)
}

type writeRecordingDriver struct {
	inner driver.Driver
	log   *writeQueryLog
}

func (d *writeRecordingDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &writeRecordingConn{Conn: conn, log: d.log}, nil
}

// writeRecordingConn embeds only driver.Conn, so database/sql cannot see the
// wrapped driver's context-aware fast paths and every statement funnels
// through Prepare — exactly the choke point the recorder needs.
type writeRecordingConn struct {
	driver.Conn
	log *writeQueryLog
}

func (c *writeRecordingConn) Prepare(query string) (driver.Stmt, error) {
	c.log.add(query)
	return c.Conn.Prepare(query)
}

var (
	writeRecordOnce   sync.Once
	writeRecordedSQL  = &writeQueryLog{}
	writeRecordDriver = "sqlite-write-query-recorder"
)

func openWriteRecordingDB(t *testing.T, path string) (*sql.DB, *writeQueryLog) {
	t.Helper()
	writeRecordOnce.Do(func() {
		probe, err := sql.Open("sqlite", sqlitedsn.ReadWrite(path))
		if err != nil {
			t.Fatal(err)
		}
		inner := probe.Driver()
		_ = probe.Close()
		sql.Register(writeRecordDriver, &writeRecordingDriver{inner: inner, log: writeRecordedSQL})
	})
	db, err := sql.Open(writeRecordDriver, sqlitedsn.ReadWrite(path))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, writeRecordedSQL
}

// --- plan evidence ---

// allowedWritePlanRes are the only ways an append-path statement may touch
// storage: full-key seeks on the pinned indexes — $-anchored with every
// equality term spelled out, so a prefix-only SEARCH fails the gate — rowid
// lookups, and scans of bounded derived constructs (the dedupe window's
// subquery materializations and constant-row INSERT sources). Plan text is
// tied to the shipped SQLite build; if it drifts, this list fails loudly and
// gets re-derived, never loosened to "any SEARCH".
var allowedWritePlanRes = []*regexp.Regexp{
	regexp.MustCompile(`^SEARCH \S+ USING (?:COVERING )?INDEX sesh_index_messages_file \(tool=\? AND wire_session_id=\? AND file_uuid=\? AND generation=\?\)$`),
	regexp.MustCompile(`^SEARCH \S+ USING (?:COVERING )?INDEX sesh_index_messages_logical \(tool=\? AND logical_session_id=\?\)$`),
	regexp.MustCompile(`^SEARCH \S+ USING (?:COVERING )?INDEX sesh_index_messages_overlap \(tool=\? AND entry_type=\? AND message_uuid=\?\)$`),
	regexp.MustCompile(`^SEARCH \S+ USING (?:COVERING )?INDEX sqlite_autoindex_files_1 \(tool=\? AND session_id=\? AND file_uuid=\? AND generation=\?\)(?: LEFT-JOIN)?$`),
	regexp.MustCompile(`^SEARCH \S+ USING (?:COVERING )?INDEX sqlite_autoindex_index_file_state_1 \(tool=\? AND wire_session_id=\? AND file_uuid=\? AND generation=\?\)$`),
	regexp.MustCompile(`^SEARCH \S+ USING INTEGER PRIMARY KEY \(rowid=\?\)$`),
	regexp.MustCompile(`^SCAN (?:\(subquery-\d+\)|CONSTANT ROW|\d+-ROW VALUES CLAUSE)$`),
}

var writePlanOpRe = regexp.MustCompile(`^(?:SEARCH|SCAN) `)

// touchedComponentRows counts the rows already carrying the labels of the
// logical sessions an append is about to unify — the pre-maintenance half of
// the component-bound denominator (the appended rows are added after the
// append reports them).
func touchedComponentRows(t *testing.T, db *sql.DB, sessions ...string) int64 {
	t.Helper()
	var total int64
	for _, session := range sessions {
		var n int64
		if err := db.QueryRow(`SELECT COUNT(*) FROM sesh_index_messages WHERE tool = ? AND logical_session_id = ?`,
			wire.ToolClaude, session).Scan(&n); err != nil {
			t.Fatal(err)
		}
		total += n
	}
	return total
}

// writePlanViolations runs EXPLAIN QUERY PLAN (through a plain,
// non-recording handle) for each statement and returns every SEARCH/SCAN
// line that is not on the full-key allowlist — the caller decides whether
// that is a failure (append path) or the expected detection (gate
// self-check). Bind values do not change SQLite's plan shape for these
// statements, so dummies stand in for the recorded args.
func writePlanViolations(t *testing.T, plain *sql.DB, queries []string) []string {
	t.Helper()
	var found []string
	for _, q := range queries {
		args := make([]any, strings.Count(q, "?"))
		for i := range args {
			args[i] = "x"
		}
		rows, err := plain.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatalf("explain: %v\nquery: %s", err, q)
		}
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
				t.Fatal(err)
			}
			if !writePlanOpRe.MatchString(detail) {
				continue // structural lines: co-routines, temp b-trees, list subqueries
			}
			allowed := false
			for _, re := range allowedWritePlanRes {
				if re.MatchString(detail) {
					allowed = true
					break
				}
			}
			if !allowed {
				found = append(found, fmt.Sprintf("%s\nquery: %s", detail, q))
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
	return found
}

func TestAppendMaintenanceBoundedOnCorpus(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), store.Config{Dir: dir, AppendBuffer: 32})
	if err != nil {
		t.Fatal(err)
	}
	idx, err := New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	buildAppendCostCorpus(t, st.DB(), 500, 10)

	// A unified resume pair brought to steady state through real ingest:
	// the gated append lands on a group whose labels, ordinals, and dedupe
	// state are already settled.
	origSession := syntheticUUID(75_000)
	resumeSession := syntheticUUID(75_001)
	origFile := syntheticUUID(76_000)
	resumeFile := syntheticUUID(76_001)
	origBody := syntheticSessionBody(origSession, "gate-orig", 12, time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC))
	resumeBody := syntheticResumeBody(resumeSession, "gate-resume", []string{"gate-orig-02", "gate-orig-03"}, 6, time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC))
	putBytes(t, st, origSession, origFile, 0, origBody)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}
	putBytes(t, st, resumeSession, resumeFile, 0, resumeBody)
	if err := idx.ProcessAppend(t.Context(), <-st.AppendEvents()); err != nil {
		t.Fatal(err)
	}

	// Mirror two pending appends, to be indexed through the recording
	// handle: a steady-state tail and a linkage-creating second resume.
	tail := []byte(`{"type":"message","uuid":"gate-tail-00","sessionId":"` + resumeSession + `","timestamp":"2026-07-11T11:00:00Z","message":{"role":"user"}}` + "\n")
	putBytes(t, st, resumeSession, resumeFile, int64(len(resumeBody)), tail)
	tailEv := <-st.AppendEvents()
	linkSession := syntheticUUID(75_002)
	linkFile := syntheticUUID(76_002)
	linkBody := syntheticResumeBody(linkSession, "gate-link", []string{"gate-orig-04", "gate-orig-05"}, 3, time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC))
	putBytes(t, st, linkSession, linkFile, 0, linkBody)
	linkEv := <-st.AppendEvents()
	// A heavy-dedupe linkage: every appended row duplicates a component row,
	// so dedupe deletes as many rows as arrived. Legitimate maintenance here
	// approaches the accounting ceiling — the case a post-dedupe denominator
	// would eventually falsely reject.
	heavySession := syntheticUUID(75_003)
	heavyFile := syntheticUUID(76_003)
	heavyShared := make([]string, 10)
	for i := range heavyShared {
		heavyShared[i] = fmt.Sprintf("gate-orig-%02d", i)
	}
	heavyBody := syntheticResumeBody(heavySession, "gate-heavy", heavyShared, 0, time.Date(2026, 7, 11, 12, 30, 0, 0, time.UTC))
	putBytes(t, st, heavySession, heavyFile, 0, heavyBody)
	heavyEv := <-st.AppendEvents()
	mirrorPath := st.MirrorPath
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "store.sqlite")
	db, log := openWriteRecordingDB(t, dbPath)
	plain, err := sql.Open("sqlite", sqlitedsn.ReadWrite(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	recIdx, err := New(t.Context(), db, mirrorPath)
	if err != nil {
		t.Fatal(err)
	}

	capture := &phaseCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(prev)

	// Steady-state append: maintenance writes bounded by the appended rows
	// (the only legitimate write is stitching a new row's file_ordinal to
	// its group ordinal — never a group- or session-scale rewrite), and
	// every recorded statement plans as full-key seeks only.
	log.reset()
	if err := recIdx.ProcessAppend(t.Context(), tailEv); err != nil {
		t.Fatal(err)
	}
	steadyQueries := log.snapshot()
	appended := capture.intSum("rows")
	if appended != 1 {
		t.Errorf("steady-state append indexed %d rows, want 1", appended)
	}
	steadyMaint := capture.intSum("maint_rows")
	if steadyMaint > appended {
		t.Errorf("steady-state append wrote %d maintenance rows for %d appended rows; maintenance must stay append-bounded", steadyMaint, appended)
	}
	for _, v := range writePlanViolations(t, plain, steadyQueries) {
		t.Errorf("non-full-key storage access on the append path: %s", v)
	}

	// Linkage-creating append, gated two-sided. A merge legitimately
	// rewrites the losing session's rows once (canonical label + ordinals,
	// plus dedupe deletes), so its writes are bounded by the touched
	// connected component — at most one relabel, one ordinal write, and one
	// delete per row that existed pre-maintenance or arrived in the append.
	// The denominator is therefore the PRE-maintenance touched cardinality
	// (component before the merge plus pending appended rows), NOT the
	// post-dedupe survivor count: a duplicate-heavy merge deletes most of
	// what it touched, and a survivor denominator would falsely reject it.
	// The lower side proves the seam is alive, not trivially zero.
	linkTouched := touchedComponentRows(t, plain, origSession, linkSession)
	capture.reset()
	log.reset()
	if err := recIdx.ProcessAppend(t.Context(), linkEv); err != nil {
		t.Fatal(err)
	}
	linkMaint := capture.intSum("maint_rows")
	linkTouched += capture.intSum("rows")
	if linkAppended := capture.intSum("rows"); linkMaint <= linkAppended {
		t.Errorf("linkage-creating append reported %d maintenance writes for %d appended rows; the maint_rows seam is dead", linkMaint, linkAppended)
	}
	if linkMaint > 3*linkTouched {
		t.Errorf("linkage append wrote %d maintenance rows for a %d-row touched component; maintenance must stay component-bounded", linkMaint, linkTouched)
	}
	for _, v := range writePlanViolations(t, plain, log.snapshot()) {
		t.Errorf("non-full-key storage access on the linkage append path: %s", v)
	}

	// Duplicate-heavy merge: every appended row dies in dedupe (M*U pre-work
	// against U survivors), so maintenance approaches the accounting ceiling
	// while staying perfectly legitimate — the shape a survivor-count
	// denominator would falsely reject must pass the pre-work bound.
	heavyTouched := touchedComponentRows(t, plain, origSession, heavySession)
	capture.reset()
	log.reset()
	if err := recIdx.ProcessAppend(t.Context(), heavyEv); err != nil {
		t.Fatal(err)
	}
	heavyMaint := capture.intSum("maint_rows")
	heavyAppended := capture.intSum("rows")
	heavyTouched += heavyAppended
	if heavyMaint <= heavyAppended {
		t.Errorf("heavy-dedupe merge reported %d maintenance writes for %d appended rows; the maint_rows seam is dead", heavyMaint, heavyAppended)
	}
	var heavySurvivors int64
	if err := plain.QueryRow(`SELECT COUNT(*) FROM sesh_index_messages WHERE tool = ? AND wire_session_id = ?`,
		wire.ToolClaude, heavySession).Scan(&heavySurvivors); err != nil {
		t.Fatal(err)
	}
	if heavySurvivors != 0 {
		t.Fatalf("heavy-dedupe fixture drifted: %d appended rows survived, want 0 (all duplicates)", heavySurvivors)
	}
	if heavyMaint > 3*heavyTouched {
		t.Errorf("heavy-dedupe merge wrote %d maintenance rows for a %d-row touched component; maintenance must stay component-bounded", heavyMaint, heavyTouched)
	}
	for _, v := range writePlanViolations(t, plain, log.snapshot()) {
		t.Errorf("non-full-key storage access on the heavy-dedupe append path: %s", v)
	}

	// The component bound only gates anything if it sits far below the
	// corpus; a fixture drift that grows the component to corpus size would
	// make the assertion vacuous.
	var corpusRows int64
	if err := plain.QueryRow(`SELECT COUNT(*) FROM sesh_index_messages`).Scan(&corpusRows); err != nil {
		t.Fatal(err)
	}
	for name, touched := range map[string]int64{"linkage": linkTouched, "heavy-dedupe": heavyTouched} {
		if 3*touched >= corpusRows {
			t.Fatalf("fixture too small to discriminate: %s bound %d vs corpus %d rows", name, 3*touched, corpusRows)
		}
	}

	// Corpus-charge negative: a deliberate corpus-walk write shape must trip
	// the component bound, or the detector is theater. The self-assigning
	// UPDATE touches every row of the tool without changing any value.
	scratchTiming := &appendTiming{}
	scratch := &Indexer{db: db, timing: scratchTiming}
	if err := scratch.execMaintenance(t.Context(), `UPDATE sesh_index_messages SET file_ordinal = file_ordinal WHERE tool = ?`, wire.ToolClaude); err != nil {
		t.Fatal(err)
	}
	if scratchTiming.maintRows <= 3*linkTouched || scratchTiming.maintRows <= 3*heavyTouched {
		t.Errorf("maint_rows detector missed a deliberate corpus-walk write (%d rows charged, bounds %d/%d)", scratchTiming.maintRows, 3*linkTouched, 3*heavyTouched)
	}

	// Corpus-walk negative: the naive pre-optimization statement shapes must
	// trip the plan detector, or the allowlist is theater.
	for name, naive := range map[string]string{
		"fileLogicalSessions": naiveFileLogicalSessionsSQL,
		"sameLogicalFiles":    naiveSameLogicalFilesSQL,
		"dedupeLogical":       naiveDedupeLogicalSQL,
	} {
		if v := writePlanViolations(t, plain, []string{naive}); len(v) == 0 {
			t.Errorf("plan detector missed the naive %s corpus walk", name)
		}
	}

	// Rewrite negative: the same steady-state shape processed through the
	// naive maintenance must report whole-group maintenance writes.
	tail2 := []byte(`{"type":"message","uuid":"gate-tail-01","sessionId":"` + resumeSession + `","timestamp":"2026-07-11T13:00:00Z","message":{"role":"user"}}` + "\n")
	tailPath := mirrorPath(wire.ToolClaude, resumeSession, resumeFile, 0)
	f, err := os.OpenFile(tailPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(tail2); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	naiveIdx, err := New(t.Context(), db, mirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	naiveIdx.naiveMaintenance = true
	capture.reset()
	if err := naiveIdx.ProcessAppend(t.Context(), wire.AppendEvent{
		Tool: wire.ToolClaude, WireSessionID: resumeSession, FileUUID: resumeFile, Generation: 0,
		ByteStart: tailEv.ByteEnd, ByteEnd: tailEv.ByteEnd + int64(len(tail2)),
	}); err != nil {
		t.Fatal(err)
	}
	if got := capture.intSum("maint_rows"); got <= capture.intSum("rows") {
		t.Errorf("maint_rows detector missed the naive whole-group rewrite on a steady-state append (reported %d)", got)
	}
}
