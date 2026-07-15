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
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"sesh/internal/index"
	"sesh/internal/store"
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

// The pinned journal vocabulary, message → exact level: a surface journal
// line's message must be one of these constants at its pinned level (a
// degradation demoted to debug would silently vanish from the default
// journal; a debug timing line promoted would flood it). A new event means
// consciously extending this list, not interpolating data into a message.
var journalMessages = map[string]slog.Level{
	"surface: nodes query failed":                  slog.LevelWarn,
	"surface: page render failed":                  slog.LevelWarn,
	"surface: panic recovered":                     slog.LevelError,
	"surface: session listing failed":              slog.LevelWarn,
	"surface: session lookup failed":               slog.LevelWarn,
	"surface: index rows read failed":              slog.LevelWarn,
	"surface: mirror range read failed":            slog.LevelWarn,
	"surface: raw fallback mirror open failed":     slog.LevelWarn,
	"surface: raw fallback mirror read failed":     slog.LevelWarn,
	"recency projection rebuild":                   slog.LevelDebug,
	"recency projection rebuild canceled by close": slog.LevelDebug,
	"recency projection rebuild failed":            slog.LevelWarn,
}

// The pinned attribute allowlist, with per-key value contracts below.
var journalAttrKeys = map[string]bool{
	"route":       true,
	"tool":        true,
	"error_class": true,
	"panic_type":  true,
	"rows":        true,
	"files":       true,
	"duration":    true,
	"sessions":    true,
}

// classShape pins error_class/panic_type values to bare Go-type-name syntax:
// bounded length, no path separators, no spaces, no dashes. An unseeded
// identifier (uuid, hostname, path, raw error text) under an allowlisted key
// fails on shape even when no needle knows it.
var classShape = regexp.MustCompile(`^[A-Za-z0-9_.*\[\]]{1,64}$`)

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
		"SECRET", // marker embedded in every injected error and panic value
		"node=",  // query params must not reach the journal
		"?page",  // ditto
	}
}

// checkJournalRecord is the detector: one degradation record against the
// contract.
func checkJournalRecord(rec capturedRecord, needles []string) error {
	wantLevel, known := journalMessages[rec.msg]
	if !known {
		return fmt.Errorf("message %q is not in the pinned journal vocabulary", rec.msg)
	}
	if rec.level != wantLevel {
		return fmt.Errorf("message %q at level %v, pinned at %v", rec.msg, rec.level, wantLevel)
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
		case "rows", "files", "sessions":
			if v.Kind() != slog.KindInt64 {
				return fmt.Errorf("%s must be a count, got kind %v", a.Key, v.Kind())
			}
		case "duration":
			if v.Kind() != slog.KindDuration {
				return fmt.Errorf("duration must be a time.Duration, got kind %v", v.Kind())
			}
		case "error_class", "panic_type":
			if !classShape.MatchString(v.String()) {
				return fmt.Errorf("%s %q is not a bare class/type name", a.Key, v.String())
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

	wantRows := int64(renderableRowCount(wire.ToolClaude, store.fakeStore.rows[sessionKey(wire.ToolClaude, uuidNormal)]))
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
		{"UNSEEDED uuid under an allowlisted key fails on shape", func(l *slog.Logger) {
			l.Warn("surface: session lookup failed", "error_class", "9d1f2a3b-0000-4000-8000-123456789abc")
		}},
		{"unseeded path under panic_type fails on shape", func(l *slog.Logger) {
			l.Error("surface: panic recovered", "route", "/s/*", "panic_type", "/home/someone/.local/state/sesh")
		}},
		{"raw error text under error_class fails on shape", func(l *slog.Logger) {
			l.Warn("recency projection rebuild failed", "duration", time.Second, "error_class", "database is locked (5) (SQLITE_BUSY)")
		}},
		{"rebuild timing promoted above its pinned debug level", func(l *slog.Logger) {
			l.Warn("recency projection rebuild", "duration", time.Second, "sessions", 3)
		}},
	}
	for _, tc := range trips {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkJournalRecord(emit(tc.fn), needles); err == nil {
				t.Fatal("detector did not trip on an identifier-carrying record; the contract gate proves nothing")
			}
		})
	}

	// And well-formed records pass, so the trips above fail for the
	// right reason rather than the detector rejecting everything.
	passes := []func(l *slog.Logger){
		func(l *slog.Logger) {
			l.Warn("surface: mirror range read failed", "tool", "claude", "error_class", "*errors.errorString", "rows", 7)
		},
		func(l *slog.Logger) {
			l.Debug("recency projection rebuild", "duration", 1500*time.Millisecond, "sessions", 42)
		},
		func(l *slog.Logger) {
			l.Warn("recency projection rebuild failed", "duration", time.Second, "error_class", "canceled")
		},
	}
	for i, fn := range passes {
		if err := checkJournalRecord(emit(fn), needles); err != nil {
			t.Fatalf("detector rejected contract-conforming record %d: %v", i, err)
		}
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

// TestSurfaceJournalRenderFailurePath drives the shared render-failure
// logging path — the one journal path every template-execution branch routes
// through — with a template that executes and fails with an error carrying a
// session id, and pins the emitted record to the contract.
func TestSurfaceJournalRenderFailurePath(t *testing.T) {
	needles := journalNeedles()
	srv, h := capturingServer(t, &failingStore{fakeStore: corpusStore(t)})
	// The template must exist under the exact name production executes
	// ("recency.html") and fail DURING execution, so the identifier rides
	// the real ExecError — a missing definition would fail with just
	// `"recency.html" is undefined` and prove nothing.
	bad := template.Must(template.New("recency.html").Funcs(template.FuncMap{
		"boom": func() (string, error) {
			return "", fmt.Errorf("render %s on workstation: SECRET template exploded", uuidNormal)
		},
	}).Parse(`{{boom}}`))
	// Self-check: the error this template hands the render path really does
	// carry the seeded identifier — otherwise the strip-proof below would be
	// assumed, not tested.
	execErr := bad.ExecuteTemplate(io.Discard, "recency.html", nil)
	if execErr == nil || !strings.Contains(execErr.Error(), uuidNormal) || !strings.Contains(execErr.Error(), "SECRET") {
		t.Fatalf("self-check: constructed template error does not carry the seeded identifiers: %v", execErr)
	}
	srv.SetRecencyTemplateForTest(bad)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sessions = %d, want 200 (render failure must degrade, not 500)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "temporarily unable to render") {
		t.Fatal("render failure did not reach the degraded page; the failing-template seam is not driving the branch")
	}

	recs := h.records()
	found := false
	for _, r := range recs {
		if r.msg != "surface: page render failed" {
			continue
		}
		found = true
		if err := checkJournalRecord(r, needles); err != nil {
			t.Errorf("render-failure record violates the journal contract: %v", err)
		}
		for _, a := range r.attrs {
			if a.Key == "route" && a.Value.String() != "/sessions" {
				t.Errorf("render-failure route = %q, want /sessions", a.Value.String())
			}
		}
	}
	if !found {
		t.Fatalf("no render-failure journal record; the branch would be invisible (got %+v)", recs)
	}
}

// TestProjectionRebuildJournalContract observes the ACTUAL projection
// rebuild records over the live SQLStore: a cold rebuild forced to fail with
// an identifier-carrying error must journal the pinned warn line with an
// error class, and a successful rebuild the pinned debug timing line — both
// under the same contract as the Server's degradation events.
func TestProjectionRebuildJournalContract(t *testing.T) {
	needles := journalNeedles()
	h := &captureHandler{}
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	idx, err := index.New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	live := surface.NewSQLStore(st.DB(), st.MirrorPath, surface.WithSQLStoreLogger(slog.New(h)))
	t.Cleanup(live.Close)
	putFixture(t, st, idx, wire.ToolClaude, uuidNormal, uuidNormal, "claude-normal.jsonl", nil)

	// The rebuild goroutine journals after it publishes its result, so the
	// record can land shortly after the request returns; poll with a bound.
	waitForRecord := func(msg string) capturedRecord {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			for _, r := range h.records() {
				if r.msg == msg {
					return r
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("no %q journal record within bound (got %+v)", msg, h.records())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	live.SetRebuildHook(func(stage surface.RebuildStage) error {
		if stage == surface.RebuildStamped {
			return fmt.Errorf("stamp probe for %s on workstation: SECRET pool closed", uuidNormal)
		}
		return nil
	})
	if _, _, err := live.RecentSessions(t.Context(), 5, 0); err == nil {
		t.Fatal("cold rebuild with a failing hook must surface the error to the cold waiter")
	}
	waitForRecord("recency projection rebuild failed")

	live.SetRebuildHook(nil)
	if _, total, err := live.RecentSessions(t.Context(), 5, 0); err != nil || total != 1 {
		t.Fatalf("rebuild after clearing the hook: total=%d err=%v, want 1 session", total, err)
	}
	waitForRecord("recency projection rebuild")

	for _, r := range h.records() {
		if err := checkJournalRecord(r, needles); err != nil {
			t.Errorf("rebuild record %q violates the journal contract: %v", r.msg, err)
		}
	}
}

// TestSurfaceJournalConcurrentDegradedRenders pins request isolation of the
// aggregation maps under concurrent degraded renders on ONE Server and one
// logger: N concurrent transcript loads over an unreadable mirror generation
// journal exactly N aggregated lines, each carrying the full per-request row
// count — and the -race package run covers this path.
func TestSurfaceJournalConcurrentDegradedRenders(t *testing.T) {
	fs := &failingStore{fakeStore: corpusStore(t), failMirrorRange: true}
	srv, h := capturingServer(t, fs)
	wantRows := int64(renderableRowCount(wire.ToolClaude, fs.fakeStore.rows[sessionKey(wire.ToolClaude, uuidNormal)]))
	if wantRows < 2 {
		t.Fatalf("fixture session has %d rows; the scenario needs several", wantRows)
	}

	const requests = 16
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/s/claude/"+uuidNormal, nil))
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent GET = %d, want 200", rec.Code)
			}
		}()
	}
	wg.Wait()

	var mirror []capturedRecord
	for _, r := range h.records() {
		if r.msg == "surface: mirror range read failed" {
			mirror = append(mirror, r)
		}
	}
	if len(mirror) != requests {
		t.Fatalf("got %d aggregated mirror-range lines for %d concurrent requests, want one per request", len(mirror), requests)
	}
	for _, r := range mirror {
		var got int64 = -1
		for _, a := range r.attrs {
			if a.Key == "rows" {
				got = a.Value.Int64()
			}
		}
		if got != wantRows {
			t.Errorf("aggregated line reports rows=%d, want %d (cross-request bleed or split)", got, wantRows)
		}
	}
}
