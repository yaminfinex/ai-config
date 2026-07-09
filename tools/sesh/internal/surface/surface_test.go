package surface_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"sesh/internal/surface"
	"sesh/internal/wire"
)

// testNow is the fixed clock for deterministic age labels.
var testNow = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)

func newServer(t *testing.T, store surface.Store) *surface.Server {
	t.Helper()
	return surface.New(store, surface.WithClock(func() time.Time { return testNow }))
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

func mustGet200(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	code, body := get(t, h, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body:\n%.500s", path, code, body)
	}
	return body
}

// --- AC1: recency uses parsed timestamps, not ingest order ---

func TestRecencyParsedTimestampBeatsIngestOrder(t *testing.T) {
	body := mustGet200(t, newServer(t, corpusStore(t)), "/")

	// claude-normal's last parsed activity (07-02) is newer than the resume
	// pair's (06-28), even though the pair was ingested later (07-04). The
	// backfilled-old session must sort below the live one.
	normal, resume := strings.Index(body, uuidNormal), strings.Index(body, uuidResumeOrig)
	if normal < 0 || resume < 0 {
		t.Fatalf("recency page lacks expected sessions (normal at %d, resume at %d)", normal, resume)
	}
	if normal > resume {
		t.Error("backfilled resume pair (older parsed activity, later ingest) sorts above claude-normal; recency must use parsed timestamps")
	}

	// The fully-quarantined session has no parsed timestamps: it orders by
	// first-ingest (07-06, the newest instant in the corpus) and says so.
	partial := strings.Index(body, uuidPartial)
	if partial < 0 || partial > normal {
		t.Error("fully-quarantined session must order by its first-ingest time (newest here), above parsed sessions")
	}
	if !strings.Contains(body, "first-ingest time") {
		t.Error("fully-quarantined session must label its recency as first-ingest time")
	}
	if !strings.Contains(body, "mirrored at") {
		t.Error("recency page must show mirrored-at as the secondary field (R14)")
	}
}

// --- AC2: quarantined raw fallback; resume pair renders once (S2/S10) ---

func TestFullyQuarantinedSessionRendersRawFallback(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	body := mustGet200(t, srv, "/s/claude/"+uuidPartial)
	if !strings.Contains(body, "raw mirror lines") {
		t.Error("fully-quarantined drill-down must render the raw-lines fallback")
	}
	// First complete line of the fixture must be present verbatim-escaped.
	if !strings.Contains(body, "permission-mode") {
		t.Error("raw fallback must show mirrored line content")
	}
	// The mirrored trailing partial (no newline yet) is bytes too; the raw
	// view stays byte-faithful and shows it.
	if n := strings.Count(body, "rawline"); n < 5 {
		t.Errorf("raw fallback rendered %d lines; want the fixture's complete lines plus the partial tail", n)
	}
}

var dataUUIDRe = regexp.MustCompile(`data-uuid="([^"]+)"`)

func TestResumePairRendersOneTranscript(t *testing.T) {
	store := corpusStore(t)
	srv := newServer(t, store)
	body := mustGet200(t, srv, "/s/claude/"+uuidResumeOrig)

	// No duplicated history: every rendered message uuid appears once.
	seen := map[string]int{}
	for _, m := range dataUUIDRe.FindAllStringSubmatch(body, -1) {
		seen[m[1]]++
	}
	if len(seen) == 0 {
		t.Fatal("transcript rendered no uuid-bearing entries")
	}
	dups := 0
	for uuid, n := range seen {
		if n > 1 {
			dups++
			t.Errorf("message uuid %s rendered %d times; resume pair must render one transcript (S2)", uuid, n)
		}
	}

	// Both mirrored files feed the one transcript.
	if !strings.Contains(body, uuidResumeOrig[:8]) || !strings.Contains(body, uuidResumeNew[:8]) {
		t.Error("transcript header must list both files of the resume pair")
	}

	// Rendered entry count matches the deduped index rows.
	rows, err := store.Rows(context.Background(), wire.ToolClaude, uuidResumeOrig)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(body, `<li class="entry`); n != len(rows) {
		t.Errorf("rendered %d entries, index holds %d deduped rows", n, len(rows))
	}
}

// --- AC3: multi-MB single line truncates; raw fallback stays available ---

func TestOversizedLineTruncatesWithRawAvailable(t *testing.T) {
	srv := newServer(t, giantLineStore(t))
	body := mustGet200(t, srv, "/s/claude/"+uuidNormal)
	if !strings.Contains(body, "truncated") {
		t.Error("multi-MB line must render truncated")
	}
	if len(body) > 1<<20 {
		t.Errorf("transcript page is %d bytes; truncation must keep oversized lines out of the render", len(body))
	}
	if !strings.Contains(body, "/raw") {
		t.Error("truncated render must link the raw fallback")
	}
	raw := mustGet200(t, srv, "/s/claude/"+uuidNormal+"/raw")
	if !strings.Contains(raw, "display-truncated") {
		t.Error("raw view of a multi-MB line must state display truncation with the mirror's true size")
	}
}

// --- display budgets: an adversarially large mirrored session must degrade,
// not OOM the store (pages render buffered) ---

func TestRawFallbackBudgetBoundsManyLargeLines(t *testing.T) {
	// 30 quarantined lines of ~1 MiB each: 30 MiB mirrored, but the raw
	// fallback must stop at its display budget with an honest notice.
	srv := newServer(t, manyLargeLinesStore(t, 30, 1<<20, true))
	body := mustGet200(t, srv, "/s/claude/"+uuidNormal)
	if !strings.Contains(body, "raw mirror lines") {
		t.Fatal("fully-quarantined session must take the raw fallback")
	}
	if len(body) > 10<<20 {
		t.Errorf("raw fallback rendered %d bytes for a 30 MiB session; the display budget must bound the page", len(body))
	}
	if !strings.Contains(body, "display budget reached") {
		t.Error("budget-cut raw page must carry the more-bytes-held-in-mirror notice")
	}
	if strings.Count(body, "rawline") == 0 {
		t.Error("budget must cut the page short, not empty")
	}
}

func TestTranscriptBudgetOmitsRowsHonestly(t *testing.T) {
	// 600 parseable entries rendering ~16 KiB each (block cap): ~9.4 MiB of
	// text, past the 8 MiB budget — the page must stop and say how many
	// rows it left out, pointing at the raw view.
	srv := newServer(t, manyLargeLinesStore(t, 600, 20<<10, false))
	body := mustGet200(t, srv, "/s/claude/"+uuidNormal)
	if len(body) > 10<<20 {
		t.Errorf("transcript rendered %d bytes; the display budget must bound the page", len(body))
	}
	if !strings.Contains(body, "display budget reached") || !strings.Contains(body, "omitted") {
		t.Error("budget-cut transcript must carry the omitted-rows notice")
	}
	if !strings.Contains(body, "/raw") {
		t.Error("budget notice must point at the raw view")
	}
	if n := strings.Count(body, `<li class="entry`); n == 0 || n >= 600 {
		t.Errorf("rendered %d entries; want a nonzero prefix cut by the budget", n)
	}
}

// --- AC4 / R17: zero write surface ---

var writeSurfaceRe = regexp.MustCompile(`(?i)<\s*(form|input|button|select|textarea)\b`)

func TestNoWriteSurfaceOnAnyPage(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	pages := []string{
		"/",
		"/nodes",
		"/fragments/recency",
		"/s/claude/" + uuidNormal,
		"/s/claude/" + uuidResumeOrig,
		"/s/claude/" + uuidInterleave,
		"/s/claude/" + uuidPartial,
		"/s/codex/" + uuidCodexMeta,
		"/s/claude/" + uuidNormal + "/raw",
	}
	for _, path := range pages {
		body := mustGet200(t, srv, path)
		if m := writeSurfaceRe.FindString(body); m != "" {
			t.Errorf("%s renders %q; the page must expose zero form/POST surface (R17)", path, m)
		}
		if strings.Contains(strings.ToLower(body), "hx-post") || strings.Contains(strings.ToLower(body), "hx-delete") {
			t.Errorf("%s carries an htmx write verb", path)
		}
	}
}

func TestNodesPageFlagsStaleLastPut(t *testing.T) {
	store := corpusStore(t)
	store.nodes = []surface.NodeStatus{
		{
			Hostname:  "fresh-host",
			OSUser:    "grace",
			LastPutAt: testNow.Add(-47 * time.Hour),
			Age:       "47h0m0s",
			Stale:     false,
		},
		{
			Hostname:  "stale-host",
			OSUser:    "grace",
			LastPutAt: testNow.Add(-49 * time.Hour),
			Age:       "49h0m0s",
			Stale:     true,
		},
	}
	body := mustGet200(t, newServer(t, store), "/nodes")
	if !strings.Contains(body, "fresh-host") || !strings.Contains(body, "stale-host") {
		t.Fatalf("nodes page missing expected rows:\n%s", body)
	}
	if !strings.Contains(body, "stale &gt;48h") || !strings.Contains(body, "fresh") {
		t.Fatalf("nodes page missing freshness badges:\n%s", body)
	}
}

func TestNonGETMethodsRejected(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, path := range []string{"/", "/nodes", "/s/claude/" + uuidNormal, "/s/claude/" + uuidNormal + "/raw", "/fragments/recency"} {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader("x")))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, rec.Code)
			}
		}
	}
}

// --- never-500 on a mirrored session ---

type erroringStore struct {
	surface.Store
	rowsErr    bool
	mirrorErr  bool
	lookupErr  bool
	sessionErr error
}

func (e *erroringStore) Session(ctx context.Context, tool wire.Tool, id string) (surface.SessionSummary, bool, error) {
	if e.lookupErr {
		return surface.SessionSummary{}, false, io.ErrUnexpectedEOF
	}
	return e.Store.Session(ctx, tool, id)
}

func (e *erroringStore) Rows(ctx context.Context, tool wire.Tool, id string) ([]wire.IndexMessage, error) {
	if e.rowsErr {
		return nil, io.ErrUnexpectedEOF
	}
	return e.Store.Rows(ctx, tool, id)
}

func (e *erroringStore) MirrorRange(ctx context.Context, tool wire.Tool, wireSessionID, fileUUID string, gen int, start, end int64) ([]byte, error) {
	if e.mirrorErr {
		return nil, io.ErrUnexpectedEOF
	}
	return e.Store.MirrorRange(ctx, tool, wireSessionID, fileUUID, gen, start, end)
}

func (e *erroringStore) MirrorFile(ctx context.Context, tool wire.Tool, wireSessionID, fileUUID string, gen int) (io.ReadCloser, error) {
	if e.mirrorErr {
		return nil, io.ErrUnexpectedEOF
	}
	return e.Store.MirrorFile(ctx, tool, wireSessionID, fileUUID, gen)
}

func TestNever500OnMirroredSession(t *testing.T) {
	base := corpusStore(t)

	t.Run("index unavailable falls back to raw", func(t *testing.T) {
		srv := newServer(t, &erroringStore{Store: base, rowsErr: true})
		body := mustGet200(t, srv, "/s/claude/"+uuidNormal)
		if !strings.Contains(body, "raw mirror lines") {
			t.Error("index failure must render the raw fallback")
		}
	})

	t.Run("index and mirror both unavailable degrades to a 200 notice", func(t *testing.T) {
		srv := newServer(t, &erroringStore{Store: base, rowsErr: true, mirrorErr: true})
		body := mustGet200(t, srv, "/s/claude/"+uuidNormal)
		if !strings.Contains(body, "unreadable") && !strings.Contains(body, "unable to render") {
			t.Errorf("total failure must still render an honest notice, got:\n%.300s", body)
		}
	})

	t.Run("per-row mirror failure renders notices inline", func(t *testing.T) {
		srv := newServer(t, &erroringStore{Store: base, mirrorErr: true})
		body := mustGet200(t, srv, "/s/claude/"+uuidNormal)
		if !strings.Contains(body, "mirror line unavailable") {
			t.Error("row-level mirror failures must render as inline notices")
		}
	})

	t.Run("lookup failure degrades", func(t *testing.T) {
		srv := newServer(t, &erroringStore{Store: base, lookupErr: true})
		mustGet200(t, srv, "/s/claude/"+uuidNormal)
	})
}

func TestUnknownSessionAndToolAre404(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	for _, path := range []string{
		"/s/claude/no-such-session",
		"/s/vim/" + uuidNormal, // unknown tool: not in the closed enum
		"/nope",
	} {
		if code, _ := get(t, srv, path); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
}

// --- plumbing: fragment + embedded asset ---

func TestRecencyFragmentAndAsset(t *testing.T) {
	srv := newServer(t, corpusStore(t))

	frag := mustGet200(t, srv, "/fragments/recency")
	if !strings.HasPrefix(strings.TrimSpace(frag), `<div id="recency"`) {
		t.Errorf("fragment must be the bare recency div for htmx swap, got:\n%.120s", frag)
	}
	if strings.Contains(frag, "<!doctype") {
		t.Error("fragment must not carry the full page shell")
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/htmx.min.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx asset = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("htmx asset content-type = %q", ct)
	}
	if rec.Body.Len() < 10<<10 {
		t.Errorf("htmx asset is %d bytes; embedded file looks wrong", rec.Body.Len())
	}
}
