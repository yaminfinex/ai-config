package surface_test

// The journal contract gate for surface degradation logs: every line the
// surface emits on a degraded path must be identifier-free — no session,
// file, or logical identifiers, no node identities, no query params; route
// class / tool enum / error class / counts only (the same contract as the
// per-request timing lines; transcripts are exactly what sesh ships, and
// identifiers in logs would leak corpus into a different retention domain).
// The degraded paths are driven with errors and panic values that
// DELIBERATELY embed identifiers, so the gate proves the pipeline strips
// them end-to-end, and the detector itself is proven on identifier-carrying
// records (a detector that never trips is assumed, not tested).

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sesh/internal/surface"
	"sesh/internal/wire"
)

// captureHandler records slog output for assertion. The surface never uses
// WithAttrs/WithGroup, so they return the handler unchanged.
type captureHandler struct {
	mu   sync.Mutex
	recs []capturedRecord
}

type capturedRecord struct {
	level slog.Level
	msg   string
	attrs []slog.Attr
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool { attrs = append(attrs, a); return true })
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recs = append(h.recs, capturedRecord{level: r.Level, msg: r.Message, attrs: attrs})
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) records() []capturedRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]capturedRecord(nil), h.recs...)
}

// The pinned journal vocabulary: a surface degradation line's message must
// be one of these constants. A new event means consciously extending this
// list, not interpolating data into a message.
var journalMessages = map[string]bool{
	"surface: nodes query failed":              true,
	"surface: page render failed":              true,
	"surface: panic recovered":                 true,
	"surface: session listing failed":          true,
	"surface: session lookup failed":           true,
	"surface: index rows read failed":          true,
	"surface: mirror range read failed":        true,
	"surface: raw fallback mirror open failed": true,
	"surface: raw fallback mirror read failed": true,
}

// The pinned attribute allowlist, with per-key value contracts below.
var journalAttrKeys = map[string]bool{
	"route":       true,
	"tool":        true,
	"error_class": true,
	"panic_type":  true,
	"rows":        true,
	"files":       true,
}

var journalRoutes = map[string]bool{
	"/": true, "/nodes": true, "/sessions": true, "/fragments/recency": true,
	"/s/*": true, "/assets/*": true, "other": true,
}

var journalTools = map[string]bool{"claude": true, "codex": true}

// journalNeedles are the identifiers seeded through the fixture corpus and
// the failing store's injected errors; none may appear anywhere in a journal
// record.
func journalNeedles() []string {
	return []string{
		uuidNormal, uuidResumeOrig, uuidResumeNew, uuidInterleave, uuidCodexMeta, uuidPartial,
		"workstation", "laptop", "grace", "alice", // node identities
		"SECRET",  // marker embedded in every injected error and panic value
		"node=",   // query params must not reach the journal
		"?page",   // ditto
	}
}

// checkJournalRecord is the detector: one degradation record against the
// contract.
func checkJournalRecord(rec capturedRecord, needles []string) error {
	if rec.level < slog.LevelWarn {
		return fmt.Errorf("level %v: degradation events log at warn or above so the default journal threshold cannot drop them", rec.level)
	}
	if !journalMessages[rec.msg] {
		return fmt.Errorf("message %q is not in the pinned journal vocabulary", rec.msg)
	}
	for _, needle := range needles {
		if strings.Contains(rec.msg, needle) {
			return fmt.Errorf("message %q carries identifier %q", rec.msg, needle)
		}
	}
	for _, a := range rec.attrs {
		if !journalAttrKeys[a.Key] {
			return fmt.Errorf("attr key %q is not in the pinned allowlist", a.Key)
		}
		v := a.Value.Resolve()
		switch a.Key {
		case "route":
			if !journalRoutes[v.String()] {
				return fmt.Errorf("route %q is not a route class", v.String())
			}
		case "tool":
			if !journalTools[v.String()] {
				return fmt.Errorf("tool %q is not a tool enum value", v.String())
			}
		case "rows", "files":
			if v.Kind() != slog.KindInt64 {
				return fmt.Errorf("%s must be a count, got kind %v", a.Key, v.Kind())
			}
		}
		for _, needle := range needles {
			if strings.Contains(v.String(), needle) {
				return fmt.Errorf("attr %s=%q carries identifier %q", a.Key, v.String(), needle)
			}
		}
	}
	return nil
}

// failingStore drives each degraded path over the fixture corpus. Every
// injected error deliberately embeds session/file identifiers and the SECRET
// marker — the shape real mirror/SQL errors have (paths built from ids) —
// so a journal line that reproduces the error string trips the needles.
type failingStore struct {
	*fakeStore
	failNodes       bool
	failRecent      bool
	panicRecent     bool
	failSession     bool
	failRows        bool
	failMirrorFile  bool
	failMirrorRange bool
}

func (f *failingStore) Nodes(ctx context.Context, staleAfter time.Duration) ([]surface.NodeStatus, error) {
	if f.failNodes {
		return nil, fmt.Errorf("nodes query on workstation: SECRET db gone")
	}
	return f.fakeStore.Nodes(ctx, staleAfter)
}

func (f *failingStore) RecentSessions(ctx context.Context, limit, offset int) ([]surface.SessionSummary, int, error) {
	if f.panicRecent {
		panic("SECRET projection state for " + uuidNormal)
	}
	if f.failRecent {
		return nil, 0, fmt.Errorf("ranking for %s: SECRET pool closed", uuidNormal)
	}
	return f.fakeStore.RecentSessions(ctx, limit, offset)
}

func (f *failingStore) RecentSessionsByNode(ctx context.Context, hostname, osUser string, limit, offset int) ([]surface.SessionSummary, int, error) {
	if f.panicRecent {
		panic("SECRET projection state for " + hostname)
	}
	if f.failRecent {
		return nil, 0, fmt.Errorf("ranking for %s@%s: SECRET pool closed", osUser, hostname)
	}
	return f.fakeStore.RecentSessionsByNode(ctx, hostname, osUser, limit, offset)
}

func (f *failingStore) Session(ctx context.Context, tool wire.Tool, id string) (surface.SessionSummary, bool, error) {
	if f.failSession {
		return surface.SessionSummary{}, false, fmt.Errorf("lookup %s/%s: SECRET disk exploded", tool, id)
	}
	return f.fakeStore.Session(ctx, tool, id)
}

func (f *failingStore) Rows(ctx context.Context, tool wire.Tool, id string) ([]wire.IndexMessage, error) {
	if f.failRows {
		return nil, fmt.Errorf("rows for %s/%s: SECRET index corrupt", tool, id)
	}
	return f.fakeStore.Rows(ctx, tool, id)
}

func (f *failingStore) MirrorFile(ctx context.Context, tool wire.Tool, wireSessionID, fileUUID string, gen int) (io.ReadCloser, error) {
	if f.failMirrorFile {
		return nil, fmt.Errorf("open mirror/%s/%s/%s/generation-%d: SECRET no such file", tool, wireSessionID, fileUUID, gen)
	}
	return f.fakeStore.MirrorFile(ctx, tool, wireSessionID, fileUUID, gen)
}

func (f *failingStore) MirrorRange(ctx context.Context, tool wire.Tool, wireSessionID, fileUUID string, gen int, start, end int64) ([]byte, error) {
	if f.failMirrorRange {
		return nil, fmt.Errorf("read mirror/%s/%s/%s/generation-%d: SECRET i/o error", tool, wireSessionID, fileUUID, gen)
	}
	return f.fakeStore.MirrorRange(ctx, tool, wireSessionID, fileUUID, gen, start, end)
}

func capturingServer(t *testing.T, store surface.Store) (*surface.Server, *captureHandler) {
	t.Helper()
	h := &captureHandler{}
	srv := surface.New(store,
		surface.WithClock(func() time.Time { return testNow }),
		surface.WithCurrentVersion("sesh-v0.3.2"),
		surface.WithLogger(slog.New(h)))
	return srv, h
}

// TestSurfaceJournalContract exercises every degraded path with
// identifier-carrying failures and pins the contract on each emitted line.
// It also pins that logging never changes the degraded response itself
// (never-500) and that per-row/per-file failures aggregate to one line per
// error class instead of flooding the journal.
func TestSurfaceJournalContract(t *testing.T) {
	needles := journalNeedles()
	scenarios := []struct {
		name    string
		store   *failingStore
		path    string
		wantMsg string
	}{
		{"nodes query failure", &failingStore{failNodes: true}, "/", "surface: nodes query failed"},
		{"session listing failure with node filter and page", &failingStore{failRecent: true}, "/sessions?node=grace%40workstation&page=2", "surface: session listing failed"},
		{"fragment listing failure", &failingStore{failRecent: true}, "/fragments/recency", "surface: session listing failed"},
		{"panic while listing", &failingStore{panicRecent: true}, "/sessions", "surface: panic recovered"},
		{"session lookup failure", &failingStore{failSession: true}, "/s/claude/" + uuidNormal, "surface: session lookup failed"},
		{"index rows failure", &failingStore{failRows: true}, "/s/claude/" + uuidNormal, "surface: index rows read failed"},
		{"raw fallback open failure", &failingStore{failMirrorFile: true}, "/s/claude/" + uuidResumeOrig + "/raw", "surface: raw fallback mirror open failed"},
		{"mirror range failure", &failingStore{failMirrorRange: true}, "/s/claude/" + uuidNormal, "surface: mirror range read failed"},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			sc.store.fakeStore = corpusStore(t)
			srv, h := capturingServer(t, sc.store)

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, sc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200 (logging must not alter the degraded response)", sc.path, rec.Code)
			}

			recs := h.records()
			matched := 0
			for _, r := range recs {
				if r.msg == sc.wantMsg {
					matched++
				}
			}
			if matched == 0 {
				t.Fatalf("no %q record captured; the degradation would be invisible in the journal (got %d records: %+v)", sc.wantMsg, len(recs), recs)
			}
			for _, r := range recs {
				if err := checkJournalRecord(r, needles); err != nil {
					t.Errorf("record %q violates the journal contract: %v", r.msg, err)
				}
			}
		})
	}
}

// TestSurfaceJournalAggregatesRepeatedFailures pins the noise bound: a
// transcript whose whole mirror generation is unreadable fails MirrorRange
// once per index row, but the journal gets ONE line carrying the row count —
// and the raw fallback likewise aggregates per-file open failures.
func TestSurfaceJournalAggregatesRepeatedFailures(t *testing.T) {
	store := &failingStore{fakeStore: corpusStore(t), failMirrorRange: true}
	srv, h := capturingServer(t, store)
	mustGet200(t, srv, "/s/claude/"+uuidNormal)

	wantRows := int64(len(store.fakeStore.rows[sessionKey(wire.ToolClaude, uuidNormal)]))
	if wantRows < 2 {
		t.Fatalf("fixture session has %d rows; the aggregation scenario needs several", wantRows)
	}
	var mirrorRecs []capturedRecord
	for _, r := range h.records() {
		if r.msg == "surface: mirror range read failed" {
			mirrorRecs = append(mirrorRecs, r)
		}
	}
	if len(mirrorRecs) != 1 {
		t.Fatalf("got %d mirror-range journal lines for %d failing rows, want 1 aggregated line", len(mirrorRecs), wantRows)
	}
	var gotRows int64 = -1
	for _, a := range mirrorRecs[0].attrs {
		if a.Key == "rows" {
			gotRows = a.Value.Int64()
		}
	}
	if gotRows != wantRows {
		t.Errorf("aggregated line reports rows=%d, want %d", gotRows, wantRows)
	}

	store2 := &failingStore{fakeStore: corpusStore(t), failMirrorFile: true}
	srv2, h2 := capturingServer(t, store2)
	mustGet200(t, srv2, "/s/claude/"+uuidResumeOrig+"/raw") // two-file session
	var openRecs []capturedRecord
	for _, r := range h2.records() {
		if r.msg == "surface: raw fallback mirror open failed" {
			openRecs = append(openRecs, r)
		}
	}
	if len(openRecs) != 1 {
		t.Fatalf("got %d raw-fallback journal lines for a 2-file session, want 1 aggregated line", len(openRecs))
	}
	var gotFiles int64 = -1
	for _, a := range openRecs[0].attrs {
		if a.Key == "files" {
			gotFiles = a.Value.Int64()
		}
	}
	if gotFiles != 2 {
		t.Errorf("aggregated line reports files=%d, want 2", gotFiles)
	}
}

// TestSurfaceJournalDetectorTrips proves the detector on
// deliberately identifier-carrying records emitted through the same handler
// pipeline: an allowlist that never rejects would pin nothing.
func TestSurfaceJournalDetectorTrips(t *testing.T) {
	needles := journalNeedles()
	emit := func(fn func(l *slog.Logger)) capturedRecord {
		h := &captureHandler{}
		fn(slog.New(h))
		recs := h.records()
		if len(recs) != 1 {
			t.Fatalf("expected exactly one emitted record, got %d", len(recs))
		}
		return recs[0]
	}
	trips := []struct {
		name string
		fn   func(l *slog.Logger)
	}{
		{"identifier under a non-allowlisted key", func(l *slog.Logger) {
			l.Warn("surface: session lookup failed", "session_id", uuidNormal)
		}},
		{"identifier smuggled into an allowlisted value", func(l *slog.Logger) {
			l.Warn("surface: session lookup failed", "error_class", "open mirror/"+uuidNormal+": no such file")
		}},
		{"identifier interpolated into the message", func(l *slog.Logger) {
			l.Warn("surface: session lookup " + uuidNormal + " failed")
		}},
		{"node identity as a value", func(l *slog.Logger) {
			l.Warn("surface: nodes query failed", "tool", "workstation")
		}},
		{"raw request path as a route", func(l *slog.Logger) {
			l.Warn("surface: page render failed", "route", "/s/claude/"+uuidNormal)
		}},
		{"debug-level degradation the default journal would drop", func(l *slog.Logger) {
			l.Debug("surface: nodes query failed", "error_class", "none")
		}},
	}
	for _, tc := range trips {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkJournalRecord(emit(tc.fn), needles); err == nil {
				t.Fatal("detector did not trip on an identifier-carrying record; the contract gate proves nothing")
			}
		})
	}

	// And a well-formed record passes, so the trips above fail for the
	// right reason rather than the detector rejecting everything.
	ok := emit(func(l *slog.Logger) {
		l.Warn("surface: mirror range read failed", "tool", "claude", "error_class", "*errors.errorString", "rows", 7)
	})
	if err := checkJournalRecord(ok, needles); err != nil {
		t.Fatalf("detector rejected a contract-conforming record: %v", err)
	}
}

// TestSurfaceDefaultLoggerReachesProcessDefault pins the AC1 wiring at the
// package seam: a Server built WITHOUT WithLogger logs degradation events to
// the process-default slog logger — the stderr → journald path on the live
// deployment shape. io.Discard as the default is exactly the regression this
// lane fixed.
func TestSurfaceDefaultLoggerReachesProcessDefault(t *testing.T) {
	h := &captureHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })

	srv := surface.New(&failingStore{fakeStore: corpusStore(t), failRecent: true},
		surface.WithClock(func() time.Time { return testNow }),
		surface.WithCurrentVersion("sesh-v0.3.2"))
	mustGet200(t, srv, "/sessions")

	for _, r := range h.records() {
		if r.msg == "surface: session listing failed" {
			return
		}
	}
	t.Fatal("degradation event did not reach the process-default logger; the journal would show a degraded page as healthy")
}
