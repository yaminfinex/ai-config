package surface_test

// The large-corpus gate for the bounded homepage: with the fleet onboarding,
// a node's corpus is thousands of transcript files, and the recency page
// must do request-time work proportional to the page, not the corpus. The
// evidence is structural, so a regression to corpus-wide scanning fails
// loudly: a recording driver captures every SQL statement the seam runs, a
// warm request must execute a fixed small number of queries with zero
// projection rebuilds, and every hot-path query's EXPLAIN QUERY PLAN must
// show index seeks — never a SCAN — over the corpus tables.

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"sesh/internal/index"
	"sesh/internal/sqlitedsn"
	"sesh/internal/store"
	"sesh/internal/surface"
	"sesh/internal/wire"
)

const bigCorpusSessions = 5000

// corpusID is the i-th synthetic session id; recency instants ascend with
// i, so the highest i is the most recent session.
func corpusID(i int) string {
	return fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
}

// genID names session i's extra file generations (b: indexed sibling,
// c: mirrored-but-unindexed sibling).
func genID(i int, suffix byte) string {
	return fmt.Sprintf("%08d-000%c-4000-8000-000000000000", i, suffix)
}

func corpusInstant(i int) time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
}

// insertCorpusSession writes one logical session shaped like real ingest
// output: three indexed messages whose LAST timestamp is the session's
// recency instant (max wins, not first or ingest order), plus for some
// sessions an indexed second file and a mirrored-but-unindexed third file —
// the membership shapes the page hydration must resolve without corpus
// scans.
func insertCorpusSession(t *testing.T, ins corpusInserters, i int) (files, msgs int) {
	t.Helper()
	id := corpusID(i)
	max := corpusInstant(i)
	at := max.Format(time.RFC3339Nano)
	if _, err := ins.file.Exec(wire.ToolClaude, id, id, 0, at, at, at); err != nil {
		t.Fatal(err)
	}
	files++
	for line, ts := range []time.Time{max.Add(-2 * time.Minute), max.Add(-time.Minute), max} {
		if _, err := ins.msg.Exec(wire.ToolClaude, id, id, id, "user",
			fmt.Sprintf("%08d-1%03d-4000-8000-000000000000", i, line), id, 0, "user",
			ts.Format(time.RFC3339Nano), 0, line, int64(line)*16, int64(line+1)*16, 0, ""); err != nil {
			t.Fatal(err)
		}
		msgs++
	}
	if _, err := ins.fact.Exec(at, wire.ToolClaude, id, id, 0, "fleet-node", "grace"); err != nil {
		t.Fatal(err)
	}
	if i%25 == 0 {
		// Indexed sibling generation: separate wire claim, unified into the
		// same logical session by the index mapping.
		idb := genID(i, 'b')
		if _, err := ins.file.Exec(wire.ToolClaude, idb, idb, 0, at, at, at); err != nil {
			t.Fatal(err)
		}
		files++
		if _, err := ins.msg.Exec(wire.ToolClaude, id, id, idb, "user",
			fmt.Sprintf("%08d-2000-4000-8000-000000000000", i), idb, 0, "user",
			max.Add(-30*time.Second).Format(time.RFC3339Nano), 1, 0, 0, 16, 0, ""); err != nil {
			t.Fatal(err)
		}
		msgs++
	}
	if i%100 == 50 {
		// Mirrored-but-unindexed sibling: bytes the index holds nothing for;
		// membership comes from the wire claim fallback.
		if _, err := ins.file.Exec(wire.ToolClaude, id, genID(i, 'c'), 0, at, at, at); err != nil {
			t.Fatal(err)
		}
		files++
	}
	return files, msgs
}

type corpusInserters struct {
	file, msg, fact *sql.Stmt
}

// buildBigCorpus fills the live store schema with n logical sessions
// (n plus n/25 plus n/100 file generations). Bulk INSERTs stand in for real
// PUTs purely for build speed — the read seam under test queries the same
// tables the ingest path writes (per-PUT mechanics have their own gates).
func buildBigCorpus(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	insFile, err := tx.Prepare(`INSERT INTO files
		(tool, session_id, file_uuid, generation, created_at, updated_at, last_put_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	insMsg, err := tx.Prepare(`INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type,
		 message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal,
		 line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	insFact, err := tx.Prepare(`INSERT INTO fact_observations
		(observed_at, tool, session_id, file_uuid, generation, hostname, os_user, session_owner)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	ins := corpusInserters{file: insFile, msg: insMsg, fact: insFact}
	for i := 0; i < n; i++ {
		insertCorpusSession(t, ins, i)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// --- recording driver: captures every SQL statement the seam prepares ---

type queryLog struct {
	mu      sync.Mutex
	queries []string
}

func (l *queryLog) add(q string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queries = append(l.queries, q)
}

func (l *queryLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queries = nil
}

func (l *queryLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.queries...)
}

type recordingDriver struct {
	inner driver.Driver
	log   *queryLog
}

func (d *recordingDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &recordingConn{Conn: conn, log: d.log}, nil
}

// recordingConn embeds only driver.Conn, so database/sql cannot see the
// wrapped driver's context-aware fast paths and every statement funnels
// through Prepare — exactly the choke point the recorder needs.
type recordingConn struct {
	driver.Conn
	log *queryLog
}

func (c *recordingConn) Prepare(query string) (driver.Stmt, error) {
	c.log.add(query)
	return c.Conn.Prepare(query)
}

var (
	recordOnce   sync.Once
	recordedSQL  = &queryLog{}
	recordDriver = "sqlite-query-recorder"
)

func openRecordingDB(t *testing.T, path string) (*sql.DB, *queryLog) {
	t.Helper()
	recordOnce.Do(func() {
		probe, err := sql.Open("sqlite", sqlitedsn.ReadWrite(path))
		if err != nil {
			t.Fatal(err)
		}
		inner := probe.Driver()
		_ = probe.Close()
		sql.Register(recordDriver, &recordingDriver{inner: inner, log: recordedSQL})
	})
	db, err := sql.Open(recordDriver, sqlitedsn.ReadWrite(path))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, recordedSQL
}

// --- plan evidence ---

// rebuildMarker appears only in the projection rebuild's SQL; stampMarker
// only in the cheap version probe. Everything else a warm request runs must
// seek an index on the corpus tables.
const (
	rebuildMarker = "first_ingest_jd"
	stampMarker   = "MAX(rowid) FROM files"
)

var corpusScanRe = regexp.MustCompile(`SCAN (files|sesh_index_messages|fact_observations)\b`)

// corpusScans runs EXPLAIN QUERY PLAN (through a plain, non-recording
// handle) for each hot-path query and returns every corpus-table SCAN it
// finds — the caller decides whether that is a failure (warm path) or the
// expected detection (gate self-check). Bind values do not change SQLite's
// plan shape for these queries, so dummies stand in for the recorded args.
func corpusScans(t *testing.T, plain *sql.DB, queries []string) []string {
	t.Helper()
	var found []string
	for _, q := range queries {
		if strings.Contains(q, stampMarker) {
			continue // two b-tree MAX probes, O(log n) by construction
		}
		args := make([]any, strings.Count(q, "?"))
		for i := range args {
			args[i] = "x"
		}
		rows, err := plain.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatalf("explain: %v\nquery: %s", err, q)
		}
		for rows.Next() {
			cols, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			vals := make([]any, len(cols))
			for i := range vals {
				vals[i] = new(any)
			}
			if err := rows.Scan(vals...); err != nil {
				t.Fatal(err)
			}
			detail := fmt.Sprintf("%v", *vals[len(vals)-1].(*any))
			if corpusScanRe.MatchString(detail) {
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

// assertSeeksOnly is the warm-path posture: zero rebuilds, zero corpus scans.
func assertSeeksOnly(t *testing.T, plain *sql.DB, queries []string) {
	t.Helper()
	if n := countMatching(queries, rebuildMarker); n != 0 {
		t.Errorf("projection rebuild ran %d times on the warm path", n)
	}
	for _, scan := range corpusScans(t, plain, queries) {
		t.Errorf("corpus table scan on the warm path: %s", scan)
	}
}

func countMatching(queries []string, substr string) int {
	n := 0
	for _, q := range queries {
		if strings.Contains(q, substr) {
			n++
		}
	}
	return n
}

func TestHomepageBoundedOnLargeCorpus(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), store.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(t.Context(), st.DB(), st.MirrorPath); err != nil {
		t.Fatal(err)
	}
	buildBigCorpus(t, st.DB(), bigCorpusSessions)
	mirrorPath := st.MirrorPath
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "store.sqlite")
	db, log := openRecordingDB(t, dbPath)
	plain, err := sql.Open("sqlite", sqlitedsn.ReadWrite(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	live := surface.NewSQLStore(db, mirrorPath)

	// Cold request: builds the projection once, returns one correct page.
	start := time.Now()
	sums, total, err := live.RecentSessions(t.Context(), 50, 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if total != bigCorpusSessions {
		t.Fatalf("total = %d, want %d", total, bigCorpusSessions)
	}
	if len(sums) != 50 {
		t.Fatalf("page holds %d sessions, want 50", len(sums))
	}
	if sums[0].LogicalSessionID != corpusID(bigCorpusSessions-1) {
		t.Errorf("newest session = %s, want %s", sums[0].LogicalSessionID, corpusID(bigCorpusSessions-1))
	}
	for i := 1; i < len(sums); i++ {
		if sums[i].Recency().After(sums[i-1].Recency()) {
			t.Fatalf("page not ordered most recent first at index %d", i)
		}
	}
	// Session 4950 carries all three membership shapes; the page hydration
	// must assemble it fully (3 files, 3+1 indexed rows, max-timestamp
	// recency) without corpus scans.
	var full *surface.SessionSummary
	for i := range sums {
		if sums[i].LogicalSessionID == corpusID(4950) {
			full = &sums[i]
		}
	}
	if full == nil {
		t.Fatal("page one lacks session 4950")
	}
	if len(full.Files) != 3 || full.MessageRows != 4 {
		t.Errorf("session 4950 hydrated %d files / %d rows, want 3 files / 4 rows", len(full.Files), full.MessageRows)
	}
	if !full.Recency().Equal(corpusInstant(4950)) {
		t.Errorf("session 4950 recency = %v, want max parsed timestamp %v", full.Recency(), corpusInstant(4950))
	}
	// Generous ceiling: the real cost is milliseconds; a corpus scan storm
	// blows straight through it.
	if elapsed > 5*time.Second {
		t.Errorf("cold RecentSessions took %v on a %d-session corpus", elapsed, bigCorpusSessions)
	}

	// Page two continues where page one stopped.
	page2, _, err := live.RecentSessions(t.Context(), 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 50 {
		t.Fatalf("second page holds %d sessions, want 50", len(page2))
	}
	if page2[0].LogicalSessionID != corpusID(bigCorpusSessions-51) {
		t.Fatalf("second page starts at %s, want %s", page2[0].LogicalSessionID, corpusID(bigCorpusSessions-51))
	}

	// Warm request against an unchanged store: one cheap stamp probe plus
	// page hydration — a fixed handful of queries, no rebuild, regardless of
	// corpus size.
	log.reset()
	if _, _, err := live.RecentSessions(t.Context(), 50, 0); err != nil {
		t.Fatal(err)
	}
	warm := log.snapshot()
	if len(warm) > 7 {
		t.Errorf("warm request ran %d queries, want a fixed handful (<=7):\n%s", len(warm), strings.Join(warm, "\n---\n"))
	}
	if countMatching(warm, stampMarker) != 1 {
		t.Errorf("warm request ran %d stamp probes, want 1", countMatching(warm, stampMarker))
	}
	if countMatching(warm, rebuildMarker) != 0 {
		t.Error("unchanged store must not trigger a rebuild")
	}
	assertSeeksOnly(t, plain, warm)

	// The transcript route's lookup is page-shaped too: single-session
	// hydration, no corpus scans.
	log.reset()
	if _, ok, err := live.Session(t.Context(), wire.ToolClaude, corpusID(4950)); err != nil || !ok {
		t.Fatalf("session lookup: ok=%v err=%v", ok, err)
	}
	assertSeeksOnly(t, plain, log.snapshot())

	// The rendered homepage stays bounded: one page of session links, the
	// honest bound label, history reachable, poll pinned to its page.
	srv := newServer(t, live)
	body := mustGet200(t, srv, "/")
	if n := strings.Count(body, `href="/s/`); n != 50 {
		t.Errorf("homepage links %d sessions, want exactly the page's 50", n)
	}
	if !strings.Contains(body, fmt.Sprintf("showing latest 50 of %d sessions", bigCorpusSessions)) {
		t.Error("homepage must state the bound (showing latest N of Z sessions)")
	}
	if !strings.Contains(body, `href="/?page=2"`) {
		t.Error("homepage must link the older history")
	}
	if len(body) > 256<<10 {
		t.Errorf("homepage is %d bytes for a %d-session corpus; render is not bounded", len(body), bigCorpusSessions)
	}
	body = mustGet200(t, srv, "/?page=100")
	if !strings.Contains(body, fmt.Sprintf("showing sessions 4951–%d of %d", bigCorpusSessions, bigCorpusSessions)) {
		t.Error("deep page must label its slice of the corpus")
	}
	if strings.Contains(body, `>older →</a>`) {
		t.Error("last page must not offer an older link")
	}
	if n := strings.Count(body, `href="/s/`); n != 50 {
		t.Errorf("deep page links %d sessions, want 50", n)
	}
	if !strings.Contains(body, `hx-get="/fragments/recency?page=100"`) {
		t.Error("deep page's poll must refresh its own page, not page one")
	}

	// A new session arriving moves the stamp: the next request rebuilds
	// exactly once and surfaces it.
	newest := bigCorpusSessions
	at := corpusInstant(newest).Format(time.RFC3339Nano)
	id := corpusID(newest)
	if _, err := db.Exec(`INSERT INTO files (tool, session_id, file_uuid, generation, created_at, updated_at, last_put_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, wire.ToolClaude, id, id, 0, at, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type, message_uuid,
		 file_uuid, generation, role, timestamp_utc, file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, 'user', ?, ?, 0, 'user', ?, 0, 0, 0, 16, 0, '')`,
		wire.ToolClaude, id, id, id, id, id, at); err != nil {
		t.Fatal(err)
	}
	log.reset()
	body = mustGet200(t, srv, "/")
	if !strings.Contains(body, id) {
		t.Error("fresh session must appear once the stamp moves")
	}
	if !strings.Contains(body, fmt.Sprintf("showing latest 50 of %d sessions", bigCorpusSessions+1)) {
		t.Error("total must follow the corpus")
	}
	if n := countMatching(log.snapshot(), rebuildMarker); n != 1 {
		t.Errorf("stamp change triggered %d rebuilds, want exactly 1", n)
	}

	// A burst of polls against the unchanged store re-slices the projection
	// with one cheap probe each and zero rebuilds.
	log.reset()
	for i := 0; i < 5; i++ {
		mustGet200(t, srv, "/")
	}
	burst := log.snapshot()
	if countMatching(burst, rebuildMarker) != 0 {
		t.Errorf("burst against an unchanged store ran %d rebuilds, want 0", countMatching(burst, rebuildMarker))
	}
	if len(burst) > 5*7 {
		t.Errorf("burst of 5 renders ran %d queries; per-request work is not fixed", len(burst))
	}

	// /nodes reads bookkeeping only, whatever the corpus size.
	log.reset()
	mustGet200(t, srv, "/nodes")
	for _, q := range log.snapshot() {
		if strings.Contains(q, "sesh_index_messages") {
			t.Errorf("/nodes queried the message index: %s", q)
		}
	}

	// Gate self-check: the plan evidence must actually catch a regression.
	// Dropping the facts bookkeeping index forces the hydration lookups back
	// to corpus scans; corpusScans must flag them or this whole gate is
	// theater. Runs last — it degrades the DB it checks.
	t.Run("plan gate catches a reintroduced corpus scan", func(t *testing.T) {
		if _, err := plain.Exec(`DROP INDEX fact_observations_session`); err != nil {
			t.Fatal(err)
		}
		log.reset()
		if _, ok, err := live.Session(t.Context(), wire.ToolClaude, corpusID(4950)); err != nil || !ok {
			t.Fatalf("session lookup: ok=%v err=%v", ok, err)
		}
		if scans := corpusScans(t, plain, log.snapshot()); len(scans) == 0 {
			t.Error("dropping the facts index produced no flagged corpus scan; the plan gate cannot catch regressions")
		}
	})
}
