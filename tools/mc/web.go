package main

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The web surface is server-rendered with plain forms and 303 redirects:
// the URL is the state carrier (ship requirement — refresh and back must be
// lossless). The optional JS layer only swaps HTML rendered by these routes.

type Web struct {
	store      *Store
	bus        *Bus
	ing        *Ingestor
	user       string // default from-name; Tailscale-User-Login overrides
	seat       string
	herderBin  string
	missions   *missionResolver
	motion     *boardMotion
	tpl        *template.Template
	graphCache graphCache
	now        func() time.Time
}

func NewWeb(store *Store, bus *Bus, ing *Ingestor, user, seat, herderBin string, missions *missionResolver) *Web {
	w := &Web{
		store: store, bus: bus, ing: ing, user: user, seat: seat,
		herderBin: herderBin, missions: missions, now: time.Now,
		motion:     newBoardMotion(),
		graphCache: graphCache{entries: map[string]graphCacheEntry{}},
	}
	w.tpl = template.Must(template.New("").Funcs(template.FuncMap{
		"lower":          strings.ToLower,
		"join":           strings.Join,
		"stampTime":      func(at time.Time) timestamp { return formatTimestamp(at, w.now()) },
		"hasHumanPrefix": func(s string) bool { return strings.HasPrefix(s, "human-") },
	}).Parse(pageTpl))
	return w
}

func (w *Web) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mc.js", serveProgressiveJS)
	mux.HandleFunc("GET /tokens.css", serveTokensCSS)
	mux.HandleFunc("GET /{$}", w.desk)
	mux.HandleFunc("GET /talk", w.talkForm)
	mux.HandleFunc("POST /talk", w.talkPost)
	mux.HandleFunc("GET /open", w.openRedirect)
	mux.HandleFunc("GET /threads", w.threads)
	mux.HandleFunc("GET /thread/{id}", w.thread)
	mux.HandleFunc("GET /mission/{slug}", w.mission)
	mux.HandleFunc("GET /mission/{slug}/asks", w.missionAsks)
	mux.HandleFunc("GET /mission/{slug}/review", w.missionReview)
	mux.HandleFunc("GET /asks", w.asks)
	mux.HandleFunc("GET /ask/{id}", w.ask)
	mux.HandleFunc("POST /ask/{id}/reply", w.askReply)
	mux.HandleFunc("POST /ask/{id}/settle", w.askSettle)
	mux.HandleFunc("POST /ask/{id}/close", w.askClose)
	mux.HandleFunc("POST /ask/{id}/link", w.askLink)
	mux.HandleFunc("POST /ask/{id}/widen", w.askWiden)
	mux.HandleFunc("GET /mission/{slug}/file/{rel...}", w.missionFile)
	mux.HandleFunc("POST /thread/{id}/reply", w.reply)
	mux.HandleFunc("POST /thread/{id}/close", w.close)
	mux.HandleFunc("POST /thread/{id}/reopen", w.reopen)
	mux.HandleFunc("POST /thread/{id}/retitle", w.retitle)
	mux.HandleFunc("GET /roster", w.roster)
	mux.HandleFunc("GET /graph", w.graph)
	return securityHeaders(w.conditionalPages(mux))
}

//go:embed mc.js
var progressiveJS []byte

//go:embed tokens.css
var tokensCSS []byte

const contentSecurityPolicy = "script-src 'self'; object-src 'none'; base-uri 'none'"

func serveProgressiveJS(rw http.ResponseWriter, _ *http.Request) {
	rw.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-cache")
	_, _ = rw.Write(progressiveJS)
}

func serveTokensCSS(rw http.ResponseWriter, _ *http.Request) {
	rw.Header().Set("Content-Type", "text/css; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-cache")
	_, _ = rw.Write(tokensCSS)
}

func (w *Web) conditionalPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		asksBacked := r.URL.Path == "/" || r.URL.Path == "/asks" || strings.HasPrefix(r.URL.Path, "/ask/") || strings.HasPrefix(r.URL.Path, "/mission/")
		if r.Method != http.MethodGet || r.URL.Path == "/mc.js" || r.URL.Path == "/tokens.css" || w.store == nil || asksBacked {
			next.ServeHTTP(rw, r)
			return
		}
		cursor, generation := w.store.Version()
		state := sha256.Sum256([]byte(w.userFor(r) + "\x00" + r.URL.RequestURI()))
		etag := fmt.Sprintf(`"%d-%d-%x"`, cursor, generation, state[:6])
		rw.Header().Set("ETag", etag)
		rw.Header().Set("Cache-Control", "no-cache")
		if r.Header.Get("If-None-Match") == etag {
			rw.WriteHeader(http.StatusNotModified)
			return
		}
		next.ServeHTTP(rw, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(rw, r)
	})
}

// userFor derives the human identity. Tailnet identities rule: when the
// request comes through tailscale serve, the login header wins.
func (w *Web) userFor(r *http.Request) string {
	if login := r.Header.Get("Tailscale-User-Login"); login != "" {
		name := login
		if i := strings.IndexByte(name, '@'); i > 0 {
			name = name[:i]
		}
		return "human-" + sanitizeName(name)
	}
	return w.user
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type pageData struct {
	Page   string
	Scope  string
	User   string
	Seat   string
	BusDir string
	Cursor int64
	Error  string
	// the desk (three zones + SYSTEM panel)
	Desk       *deskModel
	ShowClosed bool
	IngestWarn string
	// all threads browse (managed + observed)
	Observed []*Thread
	// across-mission home and in-mission page
	Missions       []missionStatus
	MissionListErr string
	Mission        missionStatus
	Ask            askEntity
	AskMission     missionStatus
	AskError       string
	AskDraft       map[string]string
	ConfirmSettle  bool
	CanWiden       bool
	AskGroups      []askGroup
	MissionThreads []*Thread
	MissionAgents  []rosterAgent
	Artifacts      []missionArtifact
	ArtifactWarn   string
	FilePath       string
	FileHTML       template.HTML
	FilePlain      string
	FileRefusal    string
	FileReason     string
	// thread
	T                *Thread
	ParticipantState string
	// open form
	F          map[string]string
	TalkKind   string
	TalkTarget string
	TalkAction string
	// roster
	Groups []rosterGroup
	// graph
	Graph *graphPage
	Auto  int
	// cockpit shell: URL-carried state and persistent navigation.
	MissionFilter string
	CurrentPath   string
	StateFields   []stateField
	RailItems     []deskItem
	RailBlocked   int
	RailLoops     int
	RailMissions  []missionStrip
	RailAgents    []rosterAgent
	Peek          bool
	Inhabit       bool
	View          string
	ThreadStamp   timestamp
	ReturnURL     string
	ContextHTML   template.HTML
	Messages      []messageView
	Object        objectPanel
}

type stateField struct{ Name, Value string }

type timestamp struct {
	Absolute string
	Relative string
	DateTime string
}

type messageView struct {
	Msg
	Stamp     timestamp
	Day       string
	DayBreak  bool
	Preview   template.HTML
	Remainder template.HTML
	Folded    bool
}

type objectRef struct {
	Label string
	URL   string
}

type objectPanel struct {
	Tasks     []objectRef
	Files     []objectRef
	Backlinks []objectRef
}

func (w *Web) data(r *http.Request, page string) *pageData {
	d := &pageData{
		Page: page, Scope: page, User: w.userFor(r), Seat: w.seat, BusDir: w.bus.Dir,
		Cursor: w.store.Cursor(), F: map[string]string{}, MissionFilter: r.URL.Query().Get("mission"),
		CurrentPath: r.URL.Path, View: r.URL.Query().Get("view"),
	}
	for _, name := range []string{"peek", "view", "window", "focus", "auto", "all", "closed"} {
		if value := r.URL.Query().Get(name); value != "" {
			d.StateFields = append(d.StateFields, stateField{Name: name, Value: value})
		}
	}
	// The desk model also feeds the persistent rail (YOUR TURN / OPEN LOOPS
	// / MISSIONS); the desk itself is global by construction — the mission
	// query never filters it (scope model: URL path is the whole answer).
	d.Desk = w.buildDesk()
	for _, g := range d.Desk.Groups {
		d.RailItems = append(d.RailItems, g.Items...)
	}
	d.RailBlocked = d.Desk.Zone2Blocked
	d.RailLoops = len(d.Desk.Loops)
	d.RailMissions = d.Desk.Strips
	groups, _ := w.rosterGroups(false)
	for _, group := range groups {
		if d.MissionFilter != "" && group.Mission != d.MissionFilter {
			continue
		}
		d.RailAgents = append(d.RailAgents, group.Agents...)
		for _, repo := range group.Repos {
			for _, branch := range repo.Branches {
				d.RailAgents = append(d.RailAgents, branch.Agents...)
			}
		}
	}
	return d
}

func (w *Web) prepareThread(d *pageData, t *Thread) {
	d.T = t
	if d.Peek {
		d.ReturnURL = addQuery("/", "peek", t.ID) + "#rail-" + url.PathEscape(t.ID)
	}
	d.ThreadStamp = formatTimestamp(t.Updated, w.now())
	d.ContextHTML = renderRichText(t.Context, t.Home)
	previousDay := ""
	for _, msg := range t.Msgs {
		at := parseTimestamp(msg.TS)
		stamp := formatTimestamp(at, w.now())
		day := at.UTC().Format("Monday, 2 January 2006")
		lines := strings.Split(strings.ReplaceAll(msg.Text, "\r\n", "\n"), "\n")
		view := messageView{Msg: msg, Stamp: stamp, Day: day, DayBreak: day != previousDay}
		if len(lines) > 30 {
			view.Folded = true
			view.Preview = renderRichText(strings.Join(lines[:10], "\n"), t.Home)
			view.Remainder = renderRichText(strings.Join(lines[10:], "\n"), t.Home)
		} else {
			view.Preview = renderRichText(msg.Text, t.Home)
		}
		d.Messages = append(d.Messages, view)
		previousDay = day
	}
	d.Object = w.objectPanel(t)
}

var richRefPattern = regexp.MustCompile(`(?i)\bTASK-[0-9]+\b|(?:[A-Za-z0-9._-]+/)*[A-Za-z0-9._-]+\.md\b`)

func renderRichText(text, mission string) template.HTML {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out strings.Builder
	inCode := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				out.WriteString("</code></pre>")
			} else {
				out.WriteString("<pre><code>")
			}
			inCode = !inCode
			continue
		}
		if inCode || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			if !inCode {
				out.WriteString("<pre><code>")
			}
			out.WriteString(template.HTMLEscapeString(strings.TrimPrefix(strings.TrimPrefix(line, "    "), "\t")))
			out.WriteByte('\n')
			if !inCode {
				out.WriteString("</code></pre>")
			}
			continue
		}
		out.WriteString(linkRichLine(line, mission))
		out.WriteString("<br>")
	}
	if inCode {
		out.WriteString("</code></pre>")
	}
	return template.HTML(out.String()) // text is escaped; hrefs are built from escaped path/query components
}

func linkRichLine(line, mission string) string {
	if mission == "" {
		return template.HTMLEscapeString(line)
	}
	matches := richRefPattern.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return template.HTMLEscapeString(line)
	}
	var out strings.Builder
	last := 0
	for _, match := range matches {
		out.WriteString(template.HTMLEscapeString(line[last:match[0]]))
		label := line[match[0]:match[1]]
		href := ""
		if strings.HasPrefix(strings.ToUpper(label), "TASK-") {
			href = "/mission/" + url.PathEscape(mission) + "#" + strings.ToLower(label)
		} else {
			href = "/mission/" + url.PathEscape(mission) + "/file/" + strings.TrimLeft(label, "/")
		}
		out.WriteString(`<a href="` + template.HTMLEscapeString(href) + `">` + template.HTMLEscapeString(label) + `</a>`)
		last = match[1]
	}
	out.WriteString(template.HTMLEscapeString(line[last:]))
	return out.String()
}

func (w *Web) objectPanel(t *Thread) objectPanel {
	var panel objectPanel
	seen := map[string]bool{}
	collect := func(text string) {
		for _, label := range richRefPattern.FindAllString(text, -1) {
			key := strings.ToLower(label)
			if seen[key] || t.Home == "" {
				continue
			}
			seen[key] = true
			if strings.HasPrefix(strings.ToUpper(label), "TASK-") {
				panel.Tasks = append(panel.Tasks, objectRef{Label: label, URL: "/mission/" + url.PathEscape(t.Home) + "#" + strings.ToLower(label)})
			} else {
				panel.Files = append(panel.Files, objectRef{Label: label, URL: "/mission/" + url.PathEscape(t.Home) + "/file/" + strings.TrimLeft(label, "/")})
			}
		}
	}
	collect(strings.ToUpper(t.ID))
	collect(t.Context)
	for _, msg := range t.Msgs {
		collect(msg.Text)
	}
	for _, candidate := range w.store.List("", "") {
		if candidate.ID == t.ID {
			continue
		}
		mentions := strings.Contains(candidate.Context, t.ID)
		for _, msg := range candidate.Msgs {
			mentions = mentions || strings.Contains(msg.Text, t.ID)
		}
		if mentions {
			panel.Backlinks = append(panel.Backlinks, objectRef{Label: candidate.Title, URL: "/thread/" + url.PathEscape(candidate.ID)})
		}
	}
	return panel
}

func parseTimestamp(raw string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func formatTimestamp(at, now time.Time) timestamp {
	if at.IsZero() {
		return timestamp{Absolute: "time unavailable", Relative: "time unavailable"}
	}
	at = at.UTC()
	d := now.Sub(at)
	if d < 0 {
		d = 0
	}
	relative := "now"
	switch {
	case d >= 24*time.Hour:
		relative = fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d >= time.Hour:
		relative = fmt.Sprintf("%dh ago", int(d.Hours()))
	case d >= time.Minute:
		relative = fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return timestamp{
		Absolute: at.Format("2006-01-02 15:04:05 MST"),
		Relative: relative,
		DateTime: at.Format(time.RFC3339),
	}
}

func stateURL(path, mission string) string {
	if mission == "" {
		return path
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "mission=" + url.QueryEscape(mission)
}

func safeReturnTarget(raw string) string {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.ContainsAny(raw, "\\\r\n") {
		return raw
	}
	return ""
}

func addQuery(target, key, value string) string {
	fragment := ""
	if i := strings.IndexByte(target, '#'); i >= 0 {
		fragment, target = target[i:], target[:i]
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	return target + separator + url.QueryEscape(key) + "=" + url.QueryEscape(value) + fragment
}

func (w *Web) render(rw http.ResponseWriter, code int, d *pageData) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(code)
	if err := w.tpl.ExecuteTemplate(rw, "page", d); err != nil {
		log.Printf("render: %v", err)
	}
}

func (w *Web) desk(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "desk")
	d.Scope = "desk"
	d.Error = r.URL.Query().Get("err")
	if w.ing != nil {
		d.IngestWarn = w.ing.StallWarning()
	}
	if id := strings.TrimSpace(r.URL.Query().Get("peek")); id != "" {
		t := w.store.Get(id)
		if t == nil {
			http.NotFound(rw, r)
			return
		}
		d.Peek = true
		w.prepareThread(d, t)
	}
	if d.View == "graph" {
		model := w.graphData("30m", w.now())
		d.Graph = &graphPage{
			Window: "30m", View: "map", Mission: d.MissionFilter,
			AsOf:         formatTimestamp(model.AsOf, w.now()).Absolute,
			AsOfRelative: formatTimestamp(model.AsOf, w.now()).Relative,
			Warning:      strings.Join(model.Warnings, " · "),
			Content:      renderGraphSVG(model, d.MissionFilter, "", d.User, r.URL.Query()),
		}
	}
	w.render(rw, 200, d)
}

func (w *Web) mission(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "mission")
	slug := r.PathValue("slug")
	d.Scope = slug
	if d.MissionFilter == "" {
		d.MissionFilter = slug
		filterRailToMission(d, slug)
	}
	d.Mission = w.missions.Status(slug)
	if d.Mission.Slug == "" {
		d.Mission.Slug = slug
	}
	for _, t := range w.store.List("", "") {
		if t.Home == slug {
			d.MissionThreads = append(d.MissionThreads, t)
		}
	}
	groups, rosterErr := w.rosterGroups(false)
	for _, g := range groups {
		if g.Mission == slug {
			d.MissionAgents = g.Agents
			break
		}
	}
	if rosterErr != "" {
		d.Error = "roster: " + rosterErr
	}
	d.Artifacts, d.ArtifactWarn = listMissionArtifacts(d.Mission)
	// A typed unknown_mission (and every other refusal) deliberately remains
	// HTTP 200: this is a useful, refreshable warning page rather than a blank
	// 404 or a server error, and matches degraded status payload handling.
	w.render(rw, http.StatusOK, d)
}

func (w *Web) asks(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "asks")
	d.Missions, d.MissionListErr = w.missions.AllStatuses()
	w.render(rw, http.StatusOK, d)
}

func (w *Web) missionAsks(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "mission-asks")
	slug := r.PathValue("slug")
	d.Scope = slug + " › asks"
	d.Mission = w.missions.Status(slug)
	if d.Mission.Slug == "" {
		d.Mission.Slug = slug
	}
	d.AskGroups = groupAsks(d.Mission.Asks.Entities, nil)
	w.render(rw, http.StatusOK, d)
}

func (w *Web) missionReview(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "review")
	slug := r.PathValue("slug")
	d.Scope = slug + " › review"
	d.Mission = w.missions.Status(slug)
	if d.Mission.Slug == "" {
		d.Mission.Slug = slug
	}
	// Review debt: pending made-for-you decisions — agent-authored rulings
	// awaiting the owner's read (D2.5) — plus open decide/read moments.
	d.AskGroups = groupAsks(d.Mission.Asks.Entities, func(entity askEntity) bool {
		if entity.State != "open" {
			return false
		}
		return entity.Kind == "ruling" || entity.Expects == "decide" || entity.Expects == "read"
	})
	w.render(rw, http.StatusOK, d)
}

type askGroup struct {
	Anchor   askReference
	Entities []askEntity
}

func groupAsks(entities []askEntity, keep func(askEntity) bool) []askGroup {
	indices := map[string]int{}
	var groups []askGroup
	for _, entity := range entities {
		if keep != nil && !keep(entity) {
			continue
		}
		key := entity.Anchor.Type + "\x00" + entity.Anchor.Ref
		index, ok := indices[key]
		if !ok {
			index = len(groups)
			indices[key] = index
			groups = append(groups, askGroup{Anchor: entity.Anchor})
		}
		groups[index].Entities = append(groups[index].Entities, entity)
	}
	return groups
}

func (w *Web) ask(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "ask")
	id := r.PathValue("id")
	_, status, found := w.missions.FindAsk(id)
	if !found {
		http.NotFound(rw, r)
		return
	}
	entity, envelope := w.missions.Ask(status.Slug, id)
	if !envelope.OK {
		d.AskError = askEnvelopeError(envelope)
	} else {
		d.Ask = entity
	}
	d.AskMission = status
	d.Scope = status.Slug + " › ask"
	d.ConfirmSettle = r.URL.Query().Get("confirm") == "settle"
	d.CanWiden = w.userFor(r) == status.Manifest.Owner
	d.AskDraft = map[string]string{}
	for _, key := range []string{"prose", "choice", "reason", "outcome", "link_type", "link_ref", "member"} {
		d.AskDraft[key] = r.URL.Query().Get(key)
	}
	if queryErr := r.URL.Query().Get("err"); queryErr != "" {
		d.Error = queryErr
	}
	w.render(rw, http.StatusOK, d)
}

func askEnvelopeError(envelope askEnvelope) string {
	message := envelope.Refusal
	if message == "" {
		message = envelope.Reason
	} else if envelope.Reason != "" {
		message += ": " + envelope.Reason
	}
	if envelope.Remedy != "" {
		message += " — " + envelope.Remedy
	}
	return message
}

func (w *Web) askReply(rw http.ResponseWriter, r *http.Request) {
	w.mutateAsk(rw, r, "reply", map[string]any{"prose": strings.TrimSpace(r.FormValue("prose"))})
}

func (w *Web) askSettle(rw http.ResponseWriter, r *http.Request) {
	if r.FormValue("confirmed") != "true" {
		http.Redirect(rw, r, "/ask/"+url.PathEscape(r.PathValue("id"))+"?err=settlement+requires+confirmation", http.StatusSeeOther)
		return
	}
	input := map[string]any{"prose": strings.TrimSpace(r.FormValue("prose"))}
	if choice := strings.TrimSpace(r.FormValue("choice")); choice != "" {
		input["choice"] = choice
	}
	w.mutateAsk(rw, r, "settle", input)
}

func (w *Web) askClose(rw http.ResponseWriter, r *http.Request) {
	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" {
		reason = "No additional reason supplied."
	}
	w.mutateAsk(rw, r, "close", map[string]any{
		"outcome": strings.TrimSpace(r.FormValue("outcome")),
		"reason":  reason,
	})
}

func (w *Web) askLink(rw http.ResponseWriter, r *http.Request) {
	w.mutateAsk(rw, r, "link", map[string]any{
		"link": map[string]string{
			"type": strings.TrimSpace(r.FormValue("link_type")),
			"ref":  strings.TrimSpace(r.FormValue("link_ref")),
		},
		"set_anchor": r.FormValue("set_anchor") == "true",
	})
}

func (w *Web) askWiden(rw http.ResponseWriter, r *http.Request) {
	w.mutateAsk(rw, r, "widen-membership", map[string]any{"member": strings.TrimSpace(r.FormValue("member"))})
}

func (w *Web) mutateAsk(rw http.ResponseWriter, r *http.Request, verb string, input map[string]any) {
	id := r.PathValue("id")
	_, status, found := w.missions.FindAsk(id)
	if !found {
		http.NotFound(rw, r)
		return
	}
	input["actor"] = w.userFor(r)
	input["if_updated_at"] = r.FormValue("if_updated_at")
	envelope := w.missions.MutateAsk(status.Slug, verb, id, input)
	dest := "/ask/" + url.PathEscape(id)
	if envelope.OK {
		// Desk composers answer in place: a successful verb returns the owner
		// to where they acted instead of forcing an entity-page detour.
		if target := safeReturnTarget(r.URL.Query().Get("return")); target != "" {
			dest = target
		}
	}
	if !envelope.OK {
		values := url.Values{"err": {askEnvelopeError(envelope)}}
		// The redirected GET refreshes the entity through mish while retaining
		// the user's draft in URL-carried state, including stale-write refusals.
		for _, key := range []string{"prose", "choice", "reason", "outcome", "link_type", "link_ref", "member"} {
			if value := strings.TrimSpace(r.FormValue(key)); value != "" {
				values.Set(key, value)
			}
		}
		if verb == "settle" {
			values.Set("confirm", "settle")
		}
		dest += "?" + values.Encode()
	}
	http.Redirect(rw, r, dest, http.StatusSeeOther)
}

// filterRailToMission goes mission-local (the F8 move, one level up): the
// rail shows only this mission's turn items, loops, and strip.
func filterRailToMission(d *pageData, mission string) {
	items := d.RailItems[:0]
	d.RailBlocked = 0
	for _, item := range d.RailItems {
		if item.Mission != mission {
			continue
		}
		items = append(items, item)
		if item.Blocked() != nil {
			d.RailBlocked++
		}
	}
	d.RailItems = items
	if d.Desk != nil {
		d.RailLoops = 0
		for _, loop := range d.Desk.Loops {
			if loop.Mission == mission {
				d.RailLoops++
			}
		}
	}
	var strips []missionStrip
	for _, strip := range d.RailMissions {
		if strip.Slug == mission {
			strips = append(strips, strip)
		}
	}
	d.RailMissions = strips
}

func (w *Web) missionFile(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "file")
	slug := r.PathValue("slug")
	d.Mission = w.missions.Status(slug)
	if d.Mission.Slug == "" {
		d.Mission.Slug = slug
	}
	d.FilePath = r.PathValue("rel")
	if !d.Mission.OK || d.Mission.MissionDir == "" {
		d.FileRefusal = "mission_unreadable"
		d.FileReason = "mission file is unavailable until mission status recovers"
		w.render(rw, http.StatusOK, d)
		return
	}
	name, err := missionFilePath(d.Mission.MissionDir, d.FilePath)
	if err != nil {
		var refusal *fileRefusal
		if errors.As(err, &refusal) {
			d.FileRefusal, d.FileReason = refusal.kind, refusal.reason
			w.render(rw, refusal.status, d)
			return
		}
		d.FileRefusal, d.FileReason = "file_unreadable", "mission file is unreadable"
		w.render(rw, http.StatusOK, d)
		return
	}
	contents, err := os.ReadFile(name)
	if err != nil {
		d.FileRefusal, d.FileReason = "file_unreadable", "mission file is unreadable"
		w.render(rw, http.StatusOK, d)
		return
	}
	if strings.EqualFold(filepath.Ext(name), ".md") || strings.EqualFold(filepath.Ext(name), ".markdown") {
		d.FileHTML, err = renderMarkdown(contents)
		if err != nil {
			d.FileRefusal, d.FileReason = "markdown_render_failed", "markdown could not be rendered"
			w.render(rw, http.StatusOK, d)
			return
		}
	} else {
		d.FilePlain = string(contents)
	}
	w.render(rw, http.StatusOK, d)
}

// threads is the browse-all view (observed included): every bus thread mc
// tracks, managed or observed, browsable when the owner goes looking —
// never pushed. ?mission= is a plain list filter here (scope model §1.1).
func (w *Web) threads(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "threads")
	for _, t := range w.store.List("", "") {
		if d.MissionFilter != "" && t.Home != d.MissionFilter {
			continue
		}
		d.Observed = append(d.Observed, t)
	}
	w.render(rw, 200, d)
}

func (w *Web) talkForm(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "talk")
	w.setTalkTarget(d, r)
	w.render(rw, 200, d)
}

// Human-authored talk always opens a managed request at moment weight. These
// agent-shaped journal fields are derived here rather than exposed by the form.
const (
	humanTalkExpects = "reply"
	humanTalkWeight  = "moment"
)

func (w *Web) talkPost(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "talk")
	w.setTalkTarget(d, r)
	for _, k := range []string{"title", "message"} {
		d.F[k] = strings.TrimSpace(r.FormValue(k))
	}
	targets, home, targetErr := w.resolveTalkTarget(d.TalkKind, d.TalkTarget)
	switch {
	case d.TalkKind == "" || d.TalkTarget == "":
		d.Error = "choose an agent or mission to talk to"
	case targetErr != nil:
		d.Error = targetErr.Error()
	case d.F["message"] == "":
		d.Error = "write a message before sending"
	}
	if d.Error != "" {
		w.render(rw, 422, d)
		return
	}
	title := d.F["title"]
	if title == "" {
		title = derivedTitle(d.F["message"])
	}
	baseID := slug(title)
	id := baseID
	for suffix := 2; w.store.Has(id); suffix++ {
		id = fmt.Sprintf("%s-%d", baseID, suffix)
	}
	user := w.userFor(r)
	turn := strings.Join(targets, ",")
	if err := w.store.Open(id, title, d.F["message"], humanTalkExpects, humanTalkWeight, home, user, targets, turn, "managed"); err != nil {
		d.Error = err.Error()
		w.render(rw, 500, d)
		return
	}
	warn, err := w.sendToThread(user, targets, id, "request", d.F["message"])
	if err != nil {
		_ = w.store.Close(id, "delivery failed: "+err.Error())
		d.Error = "bus refused delivery: " + err.Error()
		w.render(rw, 502, d)
		return
	}
	w.ing.Kick()
	redirectThread(rw, r, id, warn)
}

func (w *Web) openRedirect(rw http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("agent") == "" && q.Get("to") != "" {
		q.Set("agent", q.Get("to"))
		q.Del("to")
	}
	dest := "/talk"
	if encoded := q.Encode(); encoded != "" {
		dest += "?" + encoded
	}
	http.Redirect(rw, r, dest, http.StatusSeeOther)
}

func (w *Web) setTalkTarget(d *pageData, r *http.Request) {
	switch {
	case strings.TrimSpace(r.URL.Query().Get("agent")) != "":
		d.TalkKind = "agent"
		d.TalkTarget = strings.TrimSpace(r.URL.Query().Get("agent"))
		d.TalkAction = "/talk?agent=" + template.URLQueryEscaper(d.TalkTarget)
	case strings.TrimSpace(r.URL.Query().Get("mission")) != "":
		d.TalkKind = "mission"
		d.TalkTarget = strings.TrimSpace(r.URL.Query().Get("mission"))
		d.TalkAction = "/talk?mission=" + template.URLQueryEscaper(d.TalkTarget)
	}
}

func (w *Web) thread(rw http.ResponseWriter, r *http.Request) {
	t := w.store.Get(r.PathValue("id"))
	if t == nil {
		http.NotFound(rw, r)
		return
	}
	d := w.data(r, "thread")
	d.Inhabit = true
	w.prepareThread(d, t)
	d.Error = r.URL.Query().Get("err")
	d.ParticipantState = w.participantState(t, d.User)
	w.render(rw, 200, d)
}

func (w *Web) reply(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := w.store.Get(id)
	if t == nil {
		http.NotFound(rw, r)
		return
	}
	if !w.requireManaged(rw, r, t) {
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Redirect(rw, r, "/thread/"+id+"?err=refused:+empty+reply", http.StatusSeeOther)
		return
	}
	warn, err := w.sendToThread(w.userFor(r), agentTargets(t, w.userFor(r)), id, r.FormValue("intent"), text)
	if err != nil {
		http.Redirect(rw, r, "/thread/"+id+"?err="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}
	w.ing.Kick()
	time.Sleep(300 * time.Millisecond) // let the kick land so the redirect shows the message
	if target := safeReturnTarget(r.URL.Query().Get("return")); target != "" {
		if warn != "" {
			target = addQuery(target, "err", warn)
		}
		http.Redirect(rw, r, target, http.StatusSeeOther)
		return
	}
	redirectThread(rw, r, id, warn)
}

func (w *Web) close(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := w.store.Get(id)
	if t == nil {
		http.NotFound(rw, r)
		return
	}
	if !w.requireManaged(rw, r, t) {
		return
	}
	user := w.userFor(r)
	res := strings.TrimSpace(r.FormValue("resolution"))
	if res == "" {
		res = strings.TrimSpace(r.FormValue("text"))
	}
	if res == "" {
		if strings.HasPrefix(user, "human-") {
			res = "closed by " + user
		} else {
			http.Redirect(rw, r, "/thread/"+id+"?err=refused:+no+close+without+a+resolution", http.StatusSeeOther)
			return
		}
	}
	if err := w.store.Close(id, res); err != nil {
		http.Redirect(rw, r, "/thread/"+id+"?err="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}
	warn := ""
	if wanted := agentTargets(t, user); len(wanted) > 0 {
		var err error
		warn, err = w.sendToThread(user, wanted, id, "inform", "[thread closed] "+res)
		if err != nil {
			log.Printf("close notice for %s: %v", id, err)
			w.ing.Kick()
			http.Redirect(rw, r, "/?err="+template.URLQueryEscaper("closed, but the bus notice failed: "+err.Error()), http.StatusSeeOther)
			return
		}
	}
	w.ing.Kick()
	dest := "/"
	if target := safeReturnTarget(r.URL.Query().Get("return")); target != "" {
		dest = target
	} else if r.URL.Query().Get("cockpit") == "1" {
		mission := r.URL.Query().Get("mission")
		dest = stateURL("/", mission)
		for _, next := range w.store.List("open", "managed") {
			if next.Turn == "owner" && (mission == "" || next.Home == mission) {
				dest = addQuery(stateURL("/", mission), "peek", next.ID) + "#rail-" + url.PathEscape(next.ID)
				break
			}
		}
	}
	if warn != "" {
		dest += "?err=" + template.URLQueryEscaper(warn)
	}
	http.Redirect(rw, r, dest, http.StatusSeeOther)
}

func (w *Web) reopen(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := w.store.Get(id)
	if t == nil {
		http.NotFound(rw, r)
		return
	}
	if !w.requireManaged(rw, r, t) {
		return
	}
	if err := w.store.Reopen(id); err != nil {
		http.Redirect(rw, r, "/thread/"+id+"?err="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}
	if target := safeReturnTarget(r.URL.Query().Get("return")); target != "" {
		http.Redirect(rw, r, target, http.StatusSeeOther)
		return
	}
	http.Redirect(rw, r, "/thread/"+id, http.StatusSeeOther)
}

func (w *Web) retitle(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := w.store.Get(id)
	if t == nil {
		http.NotFound(rw, r)
		return
	}
	if !w.requireManaged(rw, r, t) {
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if err := w.store.Retitle(id, title); err != nil {
		http.Redirect(rw, r, "/thread/"+id+"?err="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}
	if target := safeReturnTarget(r.URL.Query().Get("return")); target != "" {
		http.Redirect(rw, r, target, http.StatusSeeOther)
		return
	}
	http.Redirect(rw, r, "/thread/"+id, http.StatusSeeOther)
}

func (w *Web) requireManaged(rw http.ResponseWriter, r *http.Request, t *Thread) bool {
	if t.Grade == "managed" {
		return true
	}
	http.Redirect(rw, r, "/thread/"+t.ID+"?err=refused:+observed+threads+are+read-only", http.StatusSeeOther)
	return false
}

type rosterGroup struct {
	Dir     string
	Mission string
	Agents  []rosterAgent
	Repos   []rosterRepoGroup
}

type rosterRepoGroup struct {
	Repo     string
	Branches []rosterBranchGroup
}

type rosterBranchGroup struct {
	Branch string
	Agents []rosterAgent
}

type rosterAgent struct {
	Name string
	// Address is hcom's canonical bus name. Name remains the descriptive
	// herder label rendered in the roster (for example builder-nezi).
	Address       string
	Tool          string
	Status        string
	Detail        string
	Unread        int
	Role          string
	Branch        string
	GUID          string
	MissionSource string
	// Unmanaged means hcom knows this row but the herder registry does not.
	Unmanaged bool
}

func (w *Web) roster(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "roster")
	showAll := r.URL.Query().Get("all") == "1"
	d.ShowClosed = showAll // reuse: roster "show all" toggle
	groups, errText := w.rosterGroups(showAll)
	if d.MissionFilter != "" {
		filtered := groups[:0]
		for _, g := range groups {
			if g.Mission == d.MissionFilter {
				filtered = append(filtered, g)
			}
		}
		groups = filtered
	}
	d.Groups, d.Error = groups, errText
	w.render(rw, 200, d)
}

func (w *Web) graph(rw http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	window := q.Get("window")
	if window == "" {
		window = "30m"
	}
	if _, ok := graphWindows[window]; !ok {
		http.Error(rw, "window must be one of 10m, 30m, 2h, or 8h", http.StatusBadRequest)
		return
	}
	view := q.Get("view")
	if view == "" {
		view = "map"
	}
	if view != "map" && view != "matrix" {
		http.Error(rw, "view must be map or matrix", http.StatusBadRequest)
		return
	}
	auto := 0
	if raw := q.Get("auto"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 || value > 3600 {
			http.Error(rw, "auto must be a positive number of seconds up to 3600", http.StatusBadRequest)
			return
		}
		auto = value
	}

	model := w.graphData(window, w.now())
	d := w.data(r, "graph")
	d.Auto = auto
	d.Graph = &graphPage{
		Window: window, View: view, Mission: q.Get("mission"), Focus: cleanGraphName(q.Get("focus")),
		AsOf: model.AsOf.UTC().Format(time.RFC3339), AsOfRelative: formatTimestamp(model.AsOf, w.now()).Relative,
		Warning: strings.Join(model.Warnings, " · "),
	}
	autoOff := cloneValues(q)
	autoOff.Del("auto")
	auto10 := cloneValues(q)
	auto10.Set("auto", "10")
	d.Graph.AutoOffURL = "/graph"
	if encoded := autoOff.Encode(); encoded != "" {
		d.Graph.AutoOffURL += "?" + encoded
	}
	d.Graph.Auto10URL = "/graph?" + auto10.Encode()
	d.Graph.WindowLinks, d.Graph.ViewLinks = graphPageLinks(q, window, view)
	if view == "matrix" {
		d.Graph.Content = renderGraphMatrix(model, d.Graph.Mission, d.Graph.Focus)
	} else {
		d.Graph.Content = renderGraphSVG(model, d.Graph.Mission, d.Graph.Focus, d.User, q)
	}
	w.render(rw, http.StatusOK, d)
}

func (w *Web) rosterGroups(showAll bool) ([]rosterGroup, string) {
	busAgents, err := w.bus.List()
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	herderRows, herr := HerderList(w.herderBin)
	if herr != nil && errText == "" {
		errText = herr.Error()
	}
	byName := map[string]HerderRow{}
	bySID := map[string]HerderRow{}
	for _, h := range herderRows {
		if h.Label != "" {
			byName[h.Label] = h
		}
		for _, sid := range h.SIDs {
			bySID[sid] = h
		}
	}
	missionGroups := map[string]*rosterGroup{}
	missionless := map[string]map[string][]rosterAgent{}
	add := func(dir string, declared *HerderMission, a rosterAgent) {
		if declared != nil && declared.Slug != "" {
			g := missionGroups[declared.Slug]
			if g == nil {
				g = &rosterGroup{Dir: "mission: " + declared.Slug, Mission: declared.Slug}
				missionGroups[declared.Slug] = g
			}
			a.MissionSource = declared.Source
			g.Agents = append(g.Agents, a)
			return
		}
		if declared == nil && w.missions != nil {
			if mission := w.missions.Slug(dir); mission != "" {
				g := missionGroups[mission]
				if g == nil {
					g = &rosterGroup{Dir: "mission: " + mission, Mission: mission}
					missionGroups[mission] = g
				}
				g.Agents = append(g.Agents, a)
				return
			}
		}
		repo := repoIdentity(dir)
		branch := a.Branch
		if branch == "" {
			branch = liveGitBranch(dir)
		}
		a.Branch = branch
		if missionless[repo] == nil {
			missionless[repo] = map[string][]rosterAgent{}
		}
		missionless[repo][branch] = append(missionless[repo][branch], a)
	}
	seen := map[string]bool{}
	for _, a := range busAgents {
		if a.Name == w.seat || a.BaseName == w.seat {
			continue
		}
		if !showAll && a.Status == "inactive" {
			continue
		}
		ra := rosterAgent{Name: a.Name, Address: canonicalBusName(a), Tool: a.Tool, Status: a.Status, Detail: a.StatusCtx, Unread: int(a.Unread)}
		// Group by spawn-grain cwd (declared intent) when herder knows the
		// session; the bus directory tracks the live shell as it wanders.
		dir := a.Directory
		var declared *HerderMission
		h, ok := bySID[a.SessionID]
		if !ok {
			if h, ok = byName[a.Name]; !ok {
				h, ok = byName[a.BaseName]
			}
		}
		if ok {
			ra.Role, ra.Branch, ra.GUID = h.Role, h.Branch, h.GUID
			declared = h.Mission
			seen[h.Label] = true
			if h.Cwd != "" {
				dir = h.Cwd
			}
		} else {
			ra.Unmanaged = true
		}
		add(dir, declared, ra)
	}
	for _, h := range herderRows {
		if h.Label == "" || h.Label == w.seat || seen[h.Label] {
			continue
		}
		if !showAll && h.Status != "seated" {
			continue
		}
		add(h.Cwd, h.Mission, rosterAgent{Name: h.Label, Address: h.Label, Tool: h.Agent, Status: h.Status, Role: h.Role, Branch: h.Branch, GUID: h.GUID})
	}
	var out []rosterGroup
	for _, g := range missionGroups {
		out = append(out, *g)
	}
	if len(missionless) > 0 {
		noMission := rosterGroup{Dir: "no mission"}
		for repo, branches := range missionless {
			rg := rosterRepoGroup{Repo: repo}
			for branch, agents := range branches {
				sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
				rg.Branches = append(rg.Branches, rosterBranchGroup{Branch: branch, Agents: agents})
			}
			sort.Slice(rg.Branches, func(i, j int) bool { return rg.Branches[i].Branch < rg.Branches[j].Branch })
			noMission.Repos = append(noMission.Repos, rg)
		}
		sort.Slice(noMission.Repos, func(i, j int) bool { return noMission.Repos[i].Repo < noMission.Repos[j].Repo })
		out = append(out, noMission)
	}
	sortGroups(out)
	return out, errText
}

func repoIdentity(dir string) string {
	if dir == "" {
		return "(unknown repo)"
	}
	if common, err := gitOutput(dir, "rev-parse", "--git-common-dir"); err == nil && common != "" {
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		common = filepath.Clean(common)
		if filepath.Base(common) == ".git" {
			return filepath.Base(filepath.Dir(common))
		}
		return strings.TrimSuffix(filepath.Base(common), ".git")
	}
	if remote, err := gitOutput(dir, "remote", "get-url", "origin"); err == nil && remote != "" {
		remote = strings.TrimSuffix(strings.TrimRight(remote, "/"), ".git")
		if i := strings.LastIndexAny(remote, "/:"); i >= 0 {
			remote = remote[i+1:]
		}
		if remote != "" {
			return remote
		}
	}
	return filepath.Clean(dir)
}

func liveGitBranch(dir string) string {
	if branch, err := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && branch != "" {
		if branch == "HEAD" {
			return "(detached)"
		}
		return branch
	}
	return "(unknown branch)"
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (w *Web) resolveTalkTarget(kind, target string) ([]string, string, error) {
	groups, errText := w.rosterGroups(false)
	switch kind {
	case "agent":
		for _, g := range groups {
			for _, a := range g.Agents {
				if a.Name == target || a.Address == target {
					return []string{a.Address}, g.Mission, nil
				}
			}
			for _, repo := range g.Repos {
				for _, branch := range repo.Branches {
					for _, a := range branch.Agents {
						if a.Name == target || a.Address == target {
							return []string{a.Address}, g.Mission, nil
						}
					}
				}
			}
		}
		// Active-only roster grouping deliberately omits inactive rows, but a
		// direct target may still name one. Canonicalize known historical rows
		// before recording the participant even though delivery will be
		// thread-only until that agent returns.
		if agents, err := w.bus.List(); err == nil {
			for _, a := range agents {
				if a.Name == target || a.BaseName == target {
					return []string{canonicalBusName(a)}, "", nil
				}
			}
		}
		// Let hcom remain the authority for an agent name even when roster
		// enrichment is unavailable or briefly stale.
		return []string{target}, "", nil
	case "mission":
		for _, g := range groups {
			if g.Mission != target {
				continue
			}
			var targets []string
			for _, a := range g.Agents {
				if a.Address != "" && a.Address != w.seat && !contains(targets, a.Address) {
					targets = append(targets, a.Address)
				}
			}
			if len(targets) == 0 {
				return nil, "", fmt.Errorf("mission %s has no active agents", target)
			}
			return targets, target, nil
		}
		if errText != "" {
			return nil, "", fmt.Errorf("cannot resolve mission %s: %s", target, errText)
		}
		return nil, "", fmt.Errorf("mission %s has no active agents", target)
	default:
		return nil, "", fmt.Errorf("choose an agent or mission to talk to")
	}
}

func sortGroups(gs []rosterGroup) {
	sort.Slice(gs, func(i, j int) bool {
		if (gs[i].Mission != "") != (gs[j].Mission != "") {
			return gs[i].Mission != ""
		}
		return gs[i].Dir < gs[j].Dir
	})
}

// agentTargets is who a bus send from the human should address: every
// thread participant except the human themself and any other ext human
// (human-* is our identity convention; ext senders are not @-addressable
// and hcom hard-fails the whole send on one bad mention).
func agentTargets(t *Thread, user string) []string {
	var out []string
	for _, name := range append([]string{t.OpenedBy}, t.With...) {
		if name == "" || name == user || strings.HasPrefix(name, "human-") || contains(out, name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

func liveBusStatus(status string) bool {
	switch status {
	case "active", "listening", "blocked":
		return true
	default:
		return false
	}
}

func canonicalBusName(a BusAgent) string {
	if a.BaseName != "" {
		return a.BaseName
	}
	return a.Name
}

type participantTargets struct {
	wanted []string
	live   []string
}

func canonicalParticipantTargets(wanted []string, agents []BusAgent) participantTargets {
	aliases := make(map[string]string, len(agents)*2)
	live := make(map[string]bool, len(agents))
	for _, a := range agents {
		canonical := canonicalBusName(a)
		aliases[a.Name] = canonical
		aliases[a.BaseName] = canonical
		if liveBusStatus(a.Status) {
			live[canonical] = true
		}
	}
	var out participantTargets
	for _, name := range wanted {
		canonical := aliases[name]
		if canonical == "" {
			canonical = name
		}
		if !contains(out.wanted, canonical) {
			out.wanted = append(out.wanted, canonical)
		}
		if live[canonical] && !contains(out.live, canonical) {
			out.live = append(out.live, canonical)
		}
	}
	return out
}

func (w *Web) participantTargets(wanted []string) (participantTargets, error) {
	if len(wanted) == 0 {
		return participantTargets{}, nil
	}
	agents, err := w.bus.List()
	if err != nil {
		return participantTargets{}, err
	}
	return canonicalParticipantTargets(wanted, agents), nil
}

func (w *Web) participantState(t *Thread, user string) string {
	wanted := agentTargets(t, user)
	if len(wanted) == 0 {
		return ""
	}
	targets, err := w.participantTargets(wanted)
	if err != nil {
		return "participant liveness unavailable"
	}
	if len(targets.live) == 0 {
		return "all agent participants gone"
	}
	if len(targets.live) < len(targets.wanted) {
		return fmt.Sprintf("%d of %d agent participants live", len(targets.live), len(targets.wanted))
	}
	return fmt.Sprintf("%d agent participants live", len(targets.live))
}

// sendToThread is the only outbound bus-send path. It resolves canonical bus
// names, removes dead mentions, and always attempts a seat-addressed thread
// post when liveness is unavailable or a participant dies between the check
// and send. The seat is kept alive by EnsureSeat and SeatKeepalive.
func (w *Web) sendToThread(user string, wanted []string, thread, intent, text string) (string, error) {
	targets, lookupErr := w.participantTargets(wanted)
	if lookupErr != nil {
		if err := w.bus.Send(user, []string{w.seat}, thread, "", intent, text); err != nil {
			return "", err
		}
		return "message posted to thread; participant liveness unavailable", nil
	}
	if len(targets.live) == 0 {
		if err := w.bus.Send(user, []string{w.seat}, thread, "", intent, text); err != nil {
			return "", err
		}
		return "", nil
	}
	if err := w.bus.Send(user, targets.live, thread, "", intent, text); err == nil {
		return "", nil
	}
	if err := w.bus.Send(user, []string{w.seat}, thread, "", intent, text); err != nil {
		return "", err
	}
	return "message posted to thread; participant delivery changed before send", nil
}

func redirectThread(rw http.ResponseWriter, r *http.Request, id, warning string) {
	dest := "/thread/" + id
	if warning != "" {
		dest += "?err=" + template.URLQueryEscaper(warning)
	}
	http.Redirect(rw, r, dest, http.StatusSeeOther)
}

func splitNames(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '@' }) {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func slug(title string) string {
	var b strings.Builder
	b.WriteString("t-")
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 2:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func derivedTitle(message string) string {
	message = strings.TrimSpace(message)
	end := len(message)
	for i, r := range message {
		if r == '\n' || r == '.' || r == '!' || r == '?' {
			end = i
			break
		}
	}
	title := strings.TrimSpace(message[:end])
	if title == "" {
		title = message
	}
	runes := []rune(title)
	if len(runes) > 80 {
		title = strings.TrimSpace(string(runes[:80]))
	}
	return title
}
