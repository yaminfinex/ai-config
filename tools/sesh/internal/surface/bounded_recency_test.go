package surface_test

// The large-corpus gate for the bounded homepage: with the fleet onboarding,
// a node's corpus is thousands of transcript files, and the recency page
// must render one LIMITed page of it — the cut made inside SQLite, never by
// truncating a full scan in Go — inside a sane time budget. A recording
// driver wraps the live store DB so the test can see the SQL the read seam
// actually runs.

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
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

// corpusID is the i-th synthetic session id; instants ascend with i, so the
// highest i is the most recent session.
func corpusID(i int) string {
	return fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
}

func corpusInstant(i int) time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
}

// buildBigCorpus fills the live store schema with n one-file sessions. Bulk
// INSERTs stand in for n real PUTs purely for build speed — the read seam
// under test queries the same tables the ingest path writes (the per-PUT
// mechanics have their own gates).
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
	for i := 0; i < n; i++ {
		id := corpusID(i)
		at := corpusInstant(i).Format(time.RFC3339Nano)
		if _, err := insFile.Exec(wire.ToolClaude, id, id, 0, at, at, at); err != nil {
			t.Fatal(err)
		}
		if _, err := insMsg.Exec(wire.ToolClaude, id, id, id, "user",
			fmt.Sprintf("%08d-1111-4000-8000-000000000000", i), id, 0, "user", at,
			0, 0, 0, 16, 0, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := insFact.Exec(at, wire.ToolClaude, id, id, 0, "fleet-node", "grace"); err != nil {
			t.Fatal(err)
		}
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

func anyQueryContains(queries []string, substr string) bool {
	for _, q := range queries {
		if strings.Contains(q, substr) {
			return true
		}
	}
	return false
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

	db, log := openRecordingDB(t, filepath.Join(dir, "store.sqlite"))
	live := surface.NewSQLStore(db, mirrorPath)

	// Page one: bounded rows, newest first, corpus-wide total.
	log.reset()
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
	// Generous ceiling: the real cost is milliseconds; a full-corpus
	// materialization or per-session query storm blows straight through it.
	if elapsed > 5*time.Second {
		t.Errorf("RecentSessions took %v on a %d-session corpus; the query is not bounded", elapsed, bigCorpusSessions)
	}
	// The cut must happen inside SQLite, and hydration must be constrained
	// to the page's keys — not a Go-side truncation of full-table reads.
	queries := log.snapshot()
	if !anyQueryContains(queries, "LIMIT ? OFFSET ?") {
		t.Error("no executed query carries LIMIT ? OFFSET ?; the page cut is not SQL-level")
	}
	if !anyQueryContains(queries, "IN (VALUES") {
		t.Error("no executed query constrains hydration to the page's keys")
	}

	// Page two continues where page one stopped, no overlap.
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

	// The rendered homepage stays bounded too: one page of session links, an
	// honest "showing latest N" label, and history reachable via the pager.
	srv := newServer(t, live)
	start = time.Now()
	body := mustGet200(t, srv, "/")
	elapsed = time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("homepage render took %v on a %d-session corpus", elapsed, bigCorpusSessions)
	}
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

	// Deep pages render the same bounded way, and the last page ends the
	// pager honestly.
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

	// /nodes stays cheap and unaffected by corpus size (one bookkeeping
	// table, not the index).
	log.reset()
	start = time.Now()
	mustGet200(t, srv, "/nodes")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("/nodes took %v", elapsed)
	}
	for _, q := range log.snapshot() {
		if strings.Contains(q, "sesh_index_messages") {
			t.Errorf("/nodes queried the message index: %s", q)
		}
	}
}
