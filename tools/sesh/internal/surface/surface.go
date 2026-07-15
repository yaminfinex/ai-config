// Package surface serves the read-only team pages: a nodes entry point, one
// flat recency-ordered sessions table (node and person are COLUMNS, not
// groupings — owner ruling 2026-07-14 — optionally filtered to one node),
// transcript drill-down rendered from the message index in bounded message
// windows, and a raw-JSONL fallback from the mirror whenever the index
// cannot render (spec §4.4; plan U7; R14, R16, R17).
//
// The surface reads the frozen index schema of docs/specs/sesh-wire.md
// through the Store interface below and never parses transcript files on any
// node — it runs inside the store process. It exposes zero write actions and
// no search (R17, spec §9). A session the mirror holds must always render
// something: index render → raw fallback → degraded notice, never a 500.
package surface

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"sesh/internal/buildinfo"
	"sesh/internal/wire"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed assets/htmx.min.js
var assetFS embed.FS

// FileRef names one mirrored file generation of a logical session. The wire
// session id (the PUT URL claim) is part of the mirror's addressing —
// mirror/<tool>/<session_id>/<file_uuid>/generation-N — so mirror reads
// carry it.
type FileRef struct {
	WireSessionID string
	FileUUID      string
	Generation    int
	FirstIngestAt time.Time
}

// SessionSummary is what the recency page needs to list one logical session.
// The store derives it from the index plus mirror bookkeeping; the surface
// computes recency policy (R14) from it at view time.
type SessionSummary struct {
	Tool             wire.Tool
	LogicalSessionID string

	// Node facts shipped with the bytes. Sessions with no owner claim group
	// under these, honestly (spec §4.4).
	Hostname string
	OSUser   string

	// OwnerClaims are the distinct SESSION_OWNER values observed for this
	// session in the facts log, in first-observed order. Raw observations,
	// never verdicts (I1) — precedence and conflict rendering happen at
	// view time in owner.go.
	OwnerClaims []string
	// TailnetIdentity is the store-stamped WhoIs user of the shipping node.
	// Empty until tsnet auth lands (M4/U11); the precedence tier is wired
	// so U11 only fills the field.
	TailnetIdentity string

	// MaxTimestampUTC is the maximum parsed timestamp_utc across the
	// session's index rows; nil when no row carries a parsed timestamp
	// (e.g. fully quarantined). Recency falls back to FirstIngestAt then.
	MaxTimestampUTC *time.Time
	FirstIngestAt   time.Time
	// MirroredAt is the last time mirror bytes were accepted for the
	// session — the R14 secondary display field.
	MirroredAt time.Time

	MessageRows     int // non-quarantined index rows
	QuarantinedRows int
	// IndexVersion is the maximum append-only index row id in this logical
	// session's projection snapshot. Pi uses it only as a branch-cache stamp;
	// it is not rendered and does not alter the frozen index schema.
	IndexVersion int64

	// Files in first-ingest order; the raw fallback renders them in this
	// order (R14's fully-quarantined ordering rule).
	Files []FileRef
}

// FullyQuarantined reports whether the index holds nothing renderable for
// this session, which forces the raw-lines fallback (S10).
func (s SessionSummary) FullyQuarantined() bool {
	return s.MessageRows == 0
}

// Recency is the R14 ordering instant: max parsed message timestamp,
// first-ingest time when no parsed timestamp exists.
func (s SessionSummary) Recency() time.Time {
	if s.MaxTimestampUTC != nil {
		return *s.MaxTimestampUTC
	}
	return s.FirstIngestAt
}

// Store is the read seam between the surface and the store process's index
// and mirror. U6's real index and mirror satisfy it at M2; tests back it
// with a fixture-driven fake until then. Implementations may return rows in
// any order — the surface applies the frozen transcript ordering itself.
type Store interface {
	// RecentSessions lists one page of logical sessions ordered most recent
	// first by the R14 instant (max parsed timestamp, first-ingest when
	// none), with a deterministic tie-break. The bound is part of the
	// contract: request-time work must stay proportional to the page — the
	// fleet ships thousands of files per node, so implementations keep a
	// bounded recency projection (keys only, rebuilt amortized) or an
	// equivalent request-time-bounded plan, and never materialize summaries
	// for the whole corpus per request. total is the corpus-wide logical
	// session count.
	RecentSessions(ctx context.Context, limit, offset int) (page []SessionSummary, total int, err error)
	// RecentSessionsByNode is RecentSessions filtered to one node label
	// (hostname, OS user) — same ordering, same bound contract; total is the
	// node's session count. Implementations must not pay corpus-scale work
	// per request for the filter, in SQL or in memory (the live store pages
	// a per-node ranked slice prebuilt during the same single-flighted
	// projection rebuild and swapped atomically with the global ranking).
	RecentSessionsByNode(ctx context.Context, hostname, osUser string, limit, offset int) (page []SessionSummary, total int, err error)
	// Session resolves one logical session; ok=false when unknown.
	Session(ctx context.Context, tool wire.Tool, logicalSessionID string) (sum SessionSummary, ok bool, err error)
	// Rows returns the session's sesh_index_messages rows.
	Rows(ctx context.Context, tool wire.Tool, logicalSessionID string) ([]wire.IndexMessage, error)
	// MirrorRange reads mirrored bytes [start, end) of one file generation.
	MirrorRange(ctx context.Context, tool wire.Tool, wireSessionID, fileUUID string, generation int, start, end int64) ([]byte, error)
	// MirrorFile streams a whole mirrored file generation (raw fallback).
	MirrorFile(ctx context.Context, tool wire.Tool, wireSessionID, fileUUID string, generation int) (io.ReadCloser, error)
}

type nodeStore interface {
	Nodes(ctx context.Context, staleAfter time.Duration) ([]NodeStatus, error)
}

// NodeStatus is one row on the read-only nodes view.
type NodeStatus struct {
	Hostname  string    `json:"hostname"`
	OSUser    string    `json:"os_user"`
	LastPutAt time.Time `json:"last_put_at"`
	Age       string    `json:"age"`
	Stale     bool      `json:"stale"`
	// ShipperVersion is the version the node's shipper last self-reported
	// via its User-Agent ("" = unknown: rows predating the census or a
	// client that does not identify itself). Additive omitempty field on
	// the /v1/nodes JSON: absent for unknown, so pre-census node objects
	// keep their old shape.
	ShipperVersion string `json:"shipper_version,omitempty"`
}

// Server renders the surface. All handlers are GET-only; the page carries no
// form, no POST target, and no search box (R17).
type Server struct {
	store Store
	now   func() time.Time
	log   *slog.Logger
	mux   *http.ServeMux

	// currentVersion anchors the nodes view's support window (current +
	// previous release). It is the running store's own build version — never
	// a hardcoded release string — and may be an untagged/dev form, in which
	// case no node is flagged out of window (the window is unknowable, and
	// a wrong flag is worse than none).
	currentVersion string

	recencyTmpl    *template.Template
	nodesTmpl      *template.Template
	transcriptTmpl *template.Template
	rawTmpl        *template.Template

	// Pi branch trees are an amortized read projection. Rebuilds run outside
	// this mutex and publish atomically; warm requests serve the last complete
	// projection while a changed mirror/index stamp rebuilds in the background.
	piProjectionMu     sync.Mutex
	piProjections      map[string]*piProjectionEntry
	piProjectionCtx    context.Context
	cancelPiProjection context.CancelFunc
	piProjectionWG     sync.WaitGroup
	piProjectionClosed bool
}

// Option configures a Server.
type Option func(*Server)

// WithClock fixes the wall clock used for age labels (tests).
func WithClock(now func() time.Time) Option {
	return func(s *Server) { s.now = now }
}

// WithLogger routes surface degradation logs (tests). The default is the
// process-default slog logger, which is what reaches the service journal on
// the live deployment shape (stderr → journald), same as the per-request
// timing and projection-rebuild lines.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) { s.log = l }
}

// WithCurrentVersion overrides the build version anchoring the nodes view's
// support window (tests; production uses buildinfo.Version).
func WithCurrentVersion(v string) Option {
	return func(s *Server) { s.currentVersion = v }
}

// New builds the surface handler over a Store.
func New(store Store, opts ...Option) *Server {
	projectionCtx, cancelProjection := context.WithCancel(context.Background())
	s := &Server{
		store:              store,
		now:                time.Now,
		log:                slog.Default(),
		currentVersion:     buildinfo.Version,
		piProjections:      map[string]*piProjectionEntry{},
		piProjectionCtx:    projectionCtx,
		cancelPiProjection: cancelProjection,
	}
	for _, o := range opts {
		o(s)
	}
	s.recencyTmpl = mustPage("recency.html")
	s.nodesTmpl = mustPage("nodes.html")
	s.transcriptTmpl = mustPage("transcript.html")
	s.rawTmpl = mustPage("raw.html")

	mux := http.NewServeMux()
	// '/' is the nodes entry point; the flat all-nodes sessions list lives at
	// the stable /sessions URL (?node= filters it, ?page= pages it), and the
	// htmx poll target /fragments/recency takes the same selectors. /nodes
	// redirects to '/' so pre-rework links keep working.
	mux.HandleFunc("GET /{$}", s.handleNodes)
	mux.Handle("GET /nodes", http.RedirectHandler("/", http.StatusMovedPermanently))
	mux.HandleFunc("GET /sessions", s.handleRecency)
	mux.HandleFunc("GET /fragments/recency", s.handleRecencyFragment)
	mux.HandleFunc("GET /s/{tool}/{session}", s.handleTranscript)
	mux.HandleFunc("GET /s/{tool}/{session}/raw", s.handleRaw)
	mux.Handle("GET /assets/", http.FileServerFS(assetFS))
	s.mux = mux
	return s
}

// Close cancels and drains Pi projection rebuilds. Call it after the HTTP
// server has stopped accepting requests and before closing the backing Store.
func (s *Server) Close() {
	s.piProjectionMu.Lock()
	if !s.piProjectionClosed {
		s.piProjectionClosed = true
		s.cancelPiProjection()
	}
	s.piProjectionMu.Unlock()
	s.piProjectionWG.Wait()
}

// nodeRow is one entry-point row: the node's last-PUT status plus the link
// into its filtered sessions view.
type nodeRow struct {
	NodeStatus
	// Node is the display label (os_user@hostname) shared with the sessions
	// table's node column.
	Node string
	// SessionsURL is the node's flat recency view — the same list as
	// /sessions, filtered to this node, paginated identically.
	SessionsURL string
	// VersionUnknown marks a node whose shipper version cannot be judged:
	// nothing recorded, or a token that does not parse as a release version.
	VersionUnknown bool
	// VersionBehind marks a node outside the support window (current +
	// previous release, ops/README version-skew policy). Only ever set when
	// the running store's own version pins the window.
	VersionBehind bool
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	ns, ok := s.store.(nodeStore)
	if !ok {
		s.writeDegraded(w, "node status unavailable")
		return
	}
	nodes, err := ns.Nodes(r.Context(), 48*time.Hour)
	if err != nil {
		s.log.Warn("surface: nodes query failed", "error_class", errClass(err))
		s.writeDegraded(w, "node status unavailable")
		return
	}
	floor, windowKnown := supportWindowFloor(s.currentVersion)
	rows := make([]nodeRow, 0, len(nodes))
	for _, n := range nodes {
		label := nodeLabel(n.Hostname, n.OSUser)
		row := nodeRow{
			NodeStatus:  n,
			Node:        label,
			SessionsURL: sessionsURL("/sessions", label, 1),
		}
		if v, ok := parseVersion(n.ShipperVersion); !ok {
			row.VersionUnknown = true
		} else if windowKnown && compareVersions(v, floor) < 0 {
			row.VersionBehind = true
		}
		rows = append(rows, row)
	}
	data := struct {
		Now   time.Time
		Nodes []nodeRow
	}{Now: s.now(), Nodes: rows}
	if err := s.render(w, s.nodesTmpl, "nodes.html", data); err != nil {
		s.logRenderFailure("/", err)
		s.writeDegraded(w, "node status render failed")
	}
}

func mustPage(page string) *template.Template {
	t := template.New(page).Funcs(template.FuncMap{
		"fmtTime": fmtTime,
		"fmtAge":  fmtAge,
		"fmtSize": fmtSize,
	})
	return template.Must(t.ParseFS(templateFS, "templates/layout.html", "templates/"+page))
}

// ServeHTTP dispatches with a last-resort recover: a panic while serving a
// session page must not surface as a 500 (the never-500 contract covers
// mirrored sessions; the degraded page is the floor for everything).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			// The panic value is arbitrary and may embed page data; only its
			// type and the route class reach the journal.
			s.log.Error("surface: panic recovered", "route", logRoute(r.URL.Path), "panic_type", fmt.Sprintf("%T", rec))
			s.writeDegraded(w, "renderer panic")
		}
	}()
	s.mux.ServeHTTP(w, r)
}

// errClass collapses an error to an identifier-free class label for the
// journal. Raw error strings are off-limits: mirror and SQL errors embed
// paths built from session and file identities, and transcripts are exactly
// what sesh ships — identifiers in logs would leak corpus into a different
// retention domain (the same contract as the per-request timing lines).
// Known conditions get stable names; anything else reports the innermost
// error's Go type, which is source text, never user data.
func errClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, fs.ErrNotExist):
		return "not_exist"
	}
	root := err
	for {
		u := errors.Unwrap(root)
		if u == nil {
			break
		}
		root = u
	}
	return fmt.Sprintf("%T", root)
}

// logRoute maps a request path to the surface's own identifier-free route
// classes for journal lines: parameterized routes collapse to their pattern
// and anything unknown is "other" — the raw path is client-supplied input
// and must never reach the journal.
func logRoute(p string) string {
	switch {
	case p == "/" || p == "/nodes" || p == "/sessions" || p == "/fragments/recency":
		return p
	case strings.HasPrefix(p, "/s/"):
		return "/s/*"
	case strings.HasPrefix(p, "/assets/"):
		return "/assets/*"
	default:
		return "other"
	}
}

// logRenderFailure is the ONE journal path for template-execution failures.
// Every render branch routes through it — the log-contract gate drives this
// path with a deliberately failing, identifier-carrying template, so a
// branch hand-rolling its own message or attrs would sit outside the gate.
func (s *Server) logRenderFailure(route string, err error) {
	s.log.Warn("surface: page render failed", "route", route, "error_class", errClass(err))
}

// writeDegraded is the render floor: a plain 200 notice, so an operator sees
// the failure without the page ever going fully blind or 500ing.
func (s *Server) writeDegraded(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>sesh — degraded</title></head><body><p>sesh surface: temporarily unable to render this view (%s). The mirror is unaffected; retry or check store logs.</p><p><a href="/">back to nodes</a> · <a href="/sessions">all sessions</a></p></body></html>`,
		template.HTMLEscapeString(reason))
}

// render executes a page template into a buffer first, so a mid-render
// failure can still fall back instead of emitting a torn page.
func (s *Server) render(w http.ResponseWriter, tmpl *template.Template, name string, data any) error {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(buf.Bytes())
	return err
}

func (s *Server) handleRecency(w http.ResponseWriter, r *http.Request) {
	data, err := s.recencyData(r.Context(), pageParam(r), r.URL.Query().Get("node"))
	if err != nil {
		s.log.Warn("surface: session listing failed", "route", "/sessions", "error_class", errClass(err))
		s.writeDegraded(w, "session listing unavailable")
		return
	}
	if err := s.render(w, s.recencyTmpl, "recency.html", data); err != nil {
		s.logRenderFailure("/sessions", err)
		s.writeDegraded(w, "recency render failed")
	}
}

func (s *Server) handleRecencyFragment(w http.ResponseWriter, r *http.Request) {
	data, err := s.recencyData(r.Context(), pageParam(r), r.URL.Query().Get("node"))
	if err != nil {
		s.log.Warn("surface: session listing failed", "route", "/fragments/recency", "error_class", errClass(err))
		s.writeDegraded(w, "session listing unavailable")
		return
	}
	if err := s.render(w, s.recencyTmpl, "recencyBody", data); err != nil {
		s.logRenderFailure("/fragments/recency", err)
		s.writeDegraded(w, "recency render failed")
	}
}

// resolveSession validates the tool segment and looks the session up.
// Unknown tool or unknown session are honest 404s — the never-500 contract
// covers sessions the mirror holds, not arbitrary URLs.
func (s *Server) resolveSession(w http.ResponseWriter, r *http.Request) (SessionSummary, bool) {
	tool := wire.Tool(r.PathValue("tool"))
	if tool != wire.ToolClaude && tool != wire.ToolCodex && tool != wire.ToolGrok && tool != wire.ToolPi {
		http.NotFound(w, r)
		return SessionSummary{}, false
	}
	id := r.PathValue("session")
	sum, ok, err := s.store.Session(r.Context(), tool, id)
	if err != nil {
		// Cannot even tell whether the session is mirrored; degrade rather
		// than guess a 404 or throw a 500.
		s.log.Warn("surface: session lookup failed", "tool", string(tool), "error_class", errClass(err))
		s.writeDegraded(w, "session lookup failed")
		return SessionSummary{}, false
	}
	if !ok {
		http.NotFound(w, r)
		return SessionSummary{}, false
	}
	return sum, true
}

func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	sum, ok := s.resolveSession(w, r)
	if !ok {
		return
	}
	var (
		rows         []wire.IndexMessage
		markers      map[transcriptRowKey]piBranchMarker
		branchNotice string
		err          error
	)
	if sum.Tool == wire.ToolPi {
		rows, markers, branchNotice, err = s.piProjectionSnapshot(r.Context(), sum)
	} else {
		rows, err = s.store.Rows(r.Context(), sum.Tool, sum.LogicalSessionID)
	}
	switch {
	case err != nil:
		s.log.Warn("surface: index rows read failed", "tool", string(sum.Tool), "error_class", errClass(err))
		s.serveRawFallback(w, r, sum, "index unavailable — raw mirror lines")
		return
	case !renderableFromIndex(rows):
		s.serveRawFallback(w, r, sum, "no renderable index rows (quarantined or unindexed) — raw mirror lines")
		return
	}
	page := s.transcriptData(r.Context(), sum, rows, markers, branchNotice, pageParam(r))
	if err := s.render(w, s.transcriptTmpl, "transcript.html", page); err != nil {
		s.logRenderFailure("/s/*", err)
		s.serveRawFallback(w, r, sum, "transcript render failed — raw mirror lines")
	}
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	sum, ok := s.resolveSession(w, r)
	if !ok {
		return
	}
	s.serveRawFallback(w, r, sum, "raw mirror lines")
}

// renderableFromIndex reports whether the index gives the transcript page
// anything to stand on. All-quarantined or empty row sets go to the mirror
// fallback (S10): quarantine markers alone are not a transcript.
func renderableFromIndex(rows []wire.IndexMessage) bool {
	for _, row := range rows {
		if !row.Quarantine {
			return true
		}
	}
	return false
}

func (s *Server) transcriptURL(sum SessionSummary) string {
	return "/s/" + url.PathEscape(string(sum.Tool)) + "/" + url.PathEscape(sum.LogicalSessionID)
}

func (s *Server) rawURL(sum SessionSummary) string {
	return s.transcriptURL(sum) + "/raw"
}
