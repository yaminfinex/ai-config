package surface_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
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
	// The current version is pinned so the nodes view's support window is
	// deterministic under `go test` (buildinfo.Version is "dev" there,
	// which would leave the window unknowable and the goldens flag-free).
	return surface.New(store,
		surface.WithClock(func() time.Time { return testNow }),
		surface.WithCurrentVersion("sesh-v0.3.2"))
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
	body := mustGet200(t, newServer(t, corpusStore(t)), "/sessions")

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
	rows, err := store.Rows(context.Background(), wire.ToolClaude, uuidResumeOrig)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) <= surface.TranscriptWindowMessages {
		t.Fatalf("fixture holds %d rows; this test needs the resume pair to span more than one %d-message window",
			len(rows), surface.TranscriptWindowMessages)
	}

	// The transcript is windowed (bounded pages, latest window first): walk
	// every window and assert the union renders the pair once, whole.
	seen := map[string]int{}
	entries := 0
	var whole strings.Builder
	lastPage := (len(rows) + surface.TranscriptWindowMessages - 1) / surface.TranscriptWindowMessages
	for page := 1; page <= lastPage; page++ {
		path := "/s/claude/" + uuidResumeOrig
		if page > 1 {
			path += "?page=" + strconv.Itoa(page)
		}
		body := mustGet200(t, srv, path)
		whole.WriteString(body)
		entries += strings.Count(body, `<li class="entry`)
		for _, m := range dataUUIDRe.FindAllStringSubmatch(body, -1) {
			seen[m[1]]++
		}
		want := surface.TranscriptWindowMessages
		if page == lastPage {
			want = len(rows) - (lastPage-1)*surface.TranscriptWindowMessages
		}
		if n := strings.Count(body, `<li class="entry`); n != want {
			t.Errorf("window %d rendered %d entries, want %d", page, n, want)
		}
	}

	// No duplicated history: every rendered message uuid appears once across
	// the windows (S2), and the windows tile the deduped index exactly.
	if len(seen) == 0 {
		t.Fatal("transcript rendered no uuid-bearing entries")
	}
	for uuid, n := range seen {
		if n > 1 {
			t.Errorf("message uuid %s rendered %d times; resume pair must render one transcript (S2)", uuid, n)
		}
	}
	if entries != len(rows) {
		t.Errorf("windows rendered %d entries in total, index holds %d deduped rows", entries, len(rows))
	}

	// Both mirrored files feed the one transcript.
	body := whole.String()
	if !strings.Contains(body, uuidResumeOrig[:8]) || !strings.Contains(body, uuidResumeNew[:8]) {
		t.Error("transcript header must list both files of the resume pair")
	}
}

// --- transcript windowing: an arbitrarily large session renders bounded
// pages with the recency pager idiom; raw stays whole-file ---

func TestTranscriptWindowBoundsLargeSession(t *testing.T) {
	// 600 modest entries: three windows. Page one is the NEWEST window and
	// links older history; the raw route still serves the whole session.
	const n = 600
	srv := newServer(t, manyLargeLinesStore(t, n, 64, false))
	body := mustGet200(t, srv, "/s/claude/"+uuidNormal)
	if got := strings.Count(body, `<li class="entry`); got != surface.TranscriptWindowMessages {
		t.Errorf("page one rendered %d entries, want the %d-message window", got, surface.TranscriptWindowMessages)
	}
	if !strings.Contains(body, "messages 401–600 of 600") {
		t.Error("page one must label the latest window (messages 401–600 of 600)")
	}
	if !strings.Contains(body, `?page=2">older`) {
		t.Error("page one must link the older window")
	}
	if strings.Contains(body, "← newer") {
		t.Error("the newest window must not offer a newer link")
	}

	oldest := mustGet200(t, srv, "/s/claude/"+uuidNormal+"?page=3")
	if !strings.Contains(oldest, "showing messages 1–200 of 600") {
		t.Error("the oldest window must label its slice")
	}
	if strings.Contains(oldest, "older →") {
		t.Error("the oldest window must not offer an older link")
	}
	if !strings.Contains(oldest, `<a href="/s/claude/`+uuidNormal+`?page=2">← newer</a>`) {
		t.Error("the oldest window must link the newer window")
	}

	// Junk and past-the-end selectors stay never-500 and honest: junk (and
	// Atoi overflow) falls back to the latest window, any parseable
	// past-the-end value — MaxInt64 included — clamps to the oldest real
	// window.
	for path, want := range map[string]string{
		"/s/claude/" + uuidNormal + "?page=banana":               "messages 401–600 of 600",
		"/s/claude/" + uuidNormal + "?page=99999999999999999999": "messages 401–600 of 600",
		"/s/claude/" + uuidNormal + "?page=9223372036854775807":  "messages 1–200 of 600",
		"/s/claude/" + uuidNormal + "?page=7":                    "messages 1–200 of 600",
	} {
		if body := mustGet200(t, srv, path); !strings.Contains(body, want) {
			t.Errorf("GET %s must render %q", path, want)
		}
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
	// One window of multi-block entries rendering ~64 KiB each (four blocks
	// at the 16 KiB block cap): ~12.5 MiB of text inside a single window,
	// past the 8 MiB budget — the byte backstop must stop the page and say
	// how many rows it left out, pointing at the raw view.
	srv := newServer(t, manyMultiBlockLinesStore(t, surface.TranscriptWindowMessages, 4, 20<<10))
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
	if n := strings.Count(body, `<li class="entry`); n == 0 || n >= surface.TranscriptWindowMessages {
		t.Errorf("rendered %d entries; want a nonzero prefix of the window cut by the budget", n)
	}
}

// --- AC4 / R17: zero write surface ---

var writeSurfaceRe = regexp.MustCompile(`(?i)<\s*(form|input|button|select|textarea)\b`)

func TestNoWriteSurfaceOnAnyPage(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	pages := []string{
		"/",
		"/sessions",
		"/sessions?node=grace@workstation",
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
	body := mustGet200(t, newServer(t, store), "/")
	if !strings.Contains(body, "grace@fresh-host") || !strings.Contains(body, "grace@stale-host") {
		t.Fatalf("nodes page missing expected rows:\n%s", body)
	}
	if !strings.Contains(body, "stale &gt;48h") || !strings.Contains(body, "fresh") {
		t.Fatalf("nodes page missing freshness badges:\n%s", body)
	}
}

// The version census column: each node shows its shipper's
// last-reported version against the support window (current + previous
// release) pinned by the running store's own build version. Out-of-window
// nodes are visibly flagged; unknown or unparsable versions flag as unknown
// and must never 500.
func TestNodesPageVersionColumn(t *testing.T) {
	store := corpusStore(t)
	store.nodes = []surface.NodeStatus{
		{Hostname: "current-host", OSUser: "grace", LastPutAt: testNow.Add(-time.Hour), Age: "1h0m0s", ShipperVersion: "sesh-v0.3.2"},
		{Hostname: "previous-host", OSUser: "grace", LastPutAt: testNow.Add(-time.Hour), Age: "1h0m0s", ShipperVersion: "sesh-v0.3.1"},
		{Hostname: "behind-host", OSUser: "grace", LastPutAt: testNow.Add(-time.Hour), Age: "1h0m0s", ShipperVersion: "sesh-v0.2.9"},
		{Hostname: "ahead-host", OSUser: "grace", LastPutAt: testNow.Add(-time.Hour), Age: "1h0m0s", ShipperVersion: "sesh-v0.3.3"},
		{Hostname: "precensus-host", OSUser: "grace", LastPutAt: testNow.Add(-time.Hour), Age: "1h0m0s"},
		{Hostname: "devbuild-host", OSUser: "grace", LastPutAt: testNow.Add(-time.Hour), Age: "1h0m0s", ShipperVersion: "dev"},
	}
	srv := surface.New(store,
		surface.WithClock(func() time.Time { return testNow }),
		surface.WithCurrentVersion("sesh-v0.3.2"))
	body := mustGet200(t, srv, "/")
	rows := strings.Split(body, "<tr>")
	badges := map[string]string{}
	for _, row := range rows {
		for _, host := range []string{"current-host", "previous-host", "behind-host", "ahead-host", "precensus-host", "devbuild-host"} {
			if strings.Contains(row, host) {
				badges[host] = row
			}
		}
	}
	for _, host := range []string{"current-host", "previous-host", "ahead-host"} {
		if strings.Contains(badges[host], "out of window") || strings.Contains(badges[host], ">unknown<") {
			t.Errorf("%s is in-window but flagged:\n%s", host, badges[host])
		}
	}
	if !strings.Contains(badges["behind-host"], "out of window") {
		t.Errorf("behind-host (0.2.9 vs window 0.3.1+) not flagged:\n%s", badges["behind-host"])
	}
	for _, host := range []string{"precensus-host", "devbuild-host"} {
		if !strings.Contains(badges[host], ">unknown<") {
			t.Errorf("%s must flag as unknown:\n%s", host, badges[host])
		}
		if strings.Contains(badges[host], "out of window") {
			t.Errorf("%s must not be flagged out of window:\n%s", host, badges[host])
		}
	}
	if !strings.Contains(badges["devbuild-host"], "dev") {
		t.Errorf("an unparsable version token must still be displayed:\n%s", badges["devbuild-host"])
	}

	// A dev/untagged store cannot pin the window: same fleet, no out-of-window
	// flags, and the page still renders (never 500 over a version string).
	devSrv := surface.New(store,
		surface.WithClock(func() time.Time { return testNow }),
		surface.WithCurrentVersion("dev"))
	devBody := mustGet200(t, devSrv, "/")
	if strings.Contains(devBody, "out of window") {
		t.Errorf("dev store flagged nodes out of an unknowable window:\n%s", devBody)
	}
}

// --- nodes-first navigation: '/' is the entry point, node rows link into
// per-node filtered sessions views, the all-nodes list stays reachable ---

func TestNodesEntryPointLinksFilteredSessions(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	body := mustGet200(t, srv, "/")
	if !strings.Contains(body, `href="/sessions?node=grace%40workstation"`) ||
		!strings.Contains(body, `href="/sessions?node=alice%40laptop"`) {
		t.Fatalf("nodes entry page must link each node's filtered sessions view:\n%s", body)
	}
	if !strings.Contains(body, `href="/sessions"`) {
		t.Error("the all-nodes sessions list must stay reachable from the entry page")
	}
	// The entry point is cheap: no session listing, no transcript links.
	if strings.Contains(body, `href="/s/`) {
		t.Error("nodes entry page must not list sessions")
	}

	// '/nodes' (the pre-rework URL) still lands on the entry point.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nodes", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/" {
		t.Errorf("GET /nodes = %d → %q, want 301 → /", rec.Code, rec.Header().Get("Location"))
	}
}

func TestNodeFilteredSessionsView(t *testing.T) {
	srv := newServer(t, corpusStore(t))

	// alice@laptop owns exactly the codex session in the fixture corpus.
	body := mustGet200(t, srv, "/sessions?node=alice@laptop")
	if !strings.Contains(body, "showing latest 1 of 1 sessions") {
		t.Error("node-filtered list must state its own bound (1 session for alice@laptop)")
	}
	if !strings.Contains(body, uuidCodexMeta) {
		t.Error("node-filtered list must include the node's session")
	}
	for _, other := range []string{uuidNormal, uuidResumeOrig, uuidInterleave, uuidPartial} {
		if strings.Contains(body, other) {
			t.Errorf("node-filtered list for alice@laptop leaked session %s", other)
		}
	}
	// The poll target and heading carry the filter, so a refresh never
	// silently widens the view.
	if !strings.Contains(body, `hx-get="/fragments/recency?node=alice%40laptop"`) {
		t.Error("filtered page's poll must refresh the filtered fragment")
	}
	if !strings.Contains(body, "sessions on alice@laptop") {
		t.Error("filtered page must state its node")
	}

	// grace@workstation holds the other five fixture sessions.
	body = mustGet200(t, srv, "/sessions?node=grace@workstation")
	if !strings.Contains(body, "showing latest 5 of 5 sessions") {
		t.Errorf("node-filtered list must count grace@workstation's 5 sessions")
	}

	// A node label that matches nothing renders an honest empty page.
	body = mustGet200(t, srv, "/sessions?node=nobody@nowhere")
	if !strings.Contains(body, "No sessions mirrored for this node.") {
		t.Error("unknown node filter must render an honest empty page, never an error")
	}

	// The filtered fragment mirrors the page body (htmx swap target).
	frag := mustGet200(t, srv, "/fragments/recency?node=alice@laptop")
	if !strings.HasPrefix(strings.TrimSpace(frag), `<div id="recency"`) {
		t.Errorf("filtered fragment must be the bare recency div, got:\n%.120s", frag)
	}
	if !strings.Contains(frag, uuidCodexMeta) {
		t.Error("filtered fragment must carry the node's sessions")
	}
}

func TestNonGETMethodsRejected(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, path := range []string{"/", "/sessions", "/s/claude/" + uuidNormal, "/s/claude/" + uuidNormal + "/raw", "/fragments/recency"} {
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

	t.Run("grok index unavailable falls back to raw", func(t *testing.T) {
		srv := newServer(t, &erroringStore{Store: base, rowsErr: true})
		body := mustGet200(t, srv, "/s/grok/"+uuidGrokChat)
		if !strings.Contains(body, "raw mirror lines") {
			t.Error("grok index failure must render the raw fallback")
		}
	})

	t.Run("grok index and mirror both unavailable degrades to a 200 notice", func(t *testing.T) {
		srv := newServer(t, &erroringStore{Store: base, rowsErr: true, mirrorErr: true})
		body := mustGet200(t, srv, "/s/grok/"+uuidGrokChat)
		if !strings.Contains(body, "unreadable") && !strings.Contains(body, "unable to render") {
			t.Errorf("grok total failure must still render an honest notice, got:\n%.300s", body)
		}
	})
}

func TestUnknownSessionAndToolAre404(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	for _, path := range []string{
		"/s/claude/no-such-session",
		"/s/grok/no-such-session",
		"/s/vim/" + uuidNormal, // unknown tool: not in the closed enum
		"/nope",
	} {
		if code, _ := get(t, srv, path); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
}

// --- pager edge paths: absurd page values must not overflow or lie ---

func TestRecencyPageParamAtMaxIntStaysSane(t *testing.T) {
	srv := newServer(t, corpusStore(t))

	// Exactly MaxInt64 parses, so it must be capped before the offset
	// arithmetic: far past the 5-session corpus means an honest past-the-end
	// notice, no negative ranges, zero session rows, no older link, and a
	// newer link back to the last real page (page one renders at
	// "/sessions").
	for _, path := range []string{
		"/sessions?page=9223372036854775807",
		"/fragments/recency?page=9223372036854775807",
	} {
		body := mustGet200(t, srv, path)
		if !strings.Contains(body, "past the end") {
			t.Errorf("GET %s must say it is past the end", path)
		}
		if strings.Contains(body, "sessions -") || strings.Contains(body, "–-") {
			t.Errorf("GET %s renders a negative session range", path)
		}
		if n := strings.Count(body, `href="/s/`); n != 0 {
			t.Errorf("GET %s renders %d session links past the end", path, n)
		}
		if strings.Contains(body, "older →") {
			t.Errorf("GET %s offers an older link past the end", path)
		}
		if !strings.Contains(body, `<a href="/sessions">← newer</a>`) {
			t.Errorf("GET %s must link back to the last real page", path)
		}
	}

	// Past MaxInt64 (Atoi overflow), negative, and junk all fall back to
	// page one.
	for _, path := range []string{"/sessions?page=99999999999999999999", "/sessions?page=-7", "/sessions?page=banana"} {
		body := mustGet200(t, srv, path)
		if !strings.Contains(body, "showing latest 6 of 6 sessions") {
			t.Errorf("GET %s must fall back to page one", path)
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
