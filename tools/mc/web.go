package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

// The web surface is server-rendered with plain forms and 303 redirects:
// the URL is the state carrier (ship requirement — refresh and back must be
// lossless). No client-side state, no JS.

type Web struct {
	store     *Store
	bus       *Bus
	ing       *Ingestor
	user      string // default from-name; Tailscale-User-Login overrides
	seat      string
	herderBin string
	missions  *missionResolver
	tpl       *template.Template
}

func NewWeb(store *Store, bus *Bus, ing *Ingestor, user, seat, herderBin string, missions *missionResolver) *Web {
	w := &Web{store: store, bus: bus, ing: ing, user: user, seat: seat, herderBin: herderBin, missions: missions}
	w.tpl = template.Must(template.New("").Funcs(template.FuncMap{
		"ago":            ago,
		"hasHumanPrefix": func(s string) bool { return strings.HasPrefix(s, "human-") },
	}).Parse(pageTpl))
	return w
}

func (w *Web) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", w.inbox)
	mux.HandleFunc("GET /open", w.openForm)
	mux.HandleFunc("POST /open", w.openPost)
	mux.HandleFunc("GET /threads", w.threads)
	mux.HandleFunc("GET /thread/{id}", w.thread)
	mux.HandleFunc("POST /thread/{id}/reply", w.reply)
	mux.HandleFunc("POST /thread/{id}/close", w.close)
	mux.HandleFunc("POST /thread/{id}/reopen", w.reopen)
	mux.HandleFunc("GET /roster", w.roster)
	return mux
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
	User   string
	Seat   string
	BusDir string
	Cursor int64
	Error  string
	// inbox
	YourTurn   []*Thread
	Waiting    []*Thread
	Closed     []*Thread
	ShowClosed bool
	// all threads (observed grade — tracked bus traffic, never Your turn)
	Observed []*Thread
	// thread
	T *Thread
	// open form
	F map[string]string
	// roster
	Groups []rosterGroup
}

func (w *Web) data(r *http.Request, page string) *pageData {
	return &pageData{Page: page, User: w.userFor(r), Seat: w.seat, BusDir: w.bus.Dir, Cursor: w.store.Cursor(), F: map[string]string{}}
}

func (w *Web) render(rw http.ResponseWriter, code int, d *pageData) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(code)
	if err := w.tpl.ExecuteTemplate(rw, "page", d); err != nil {
		log.Printf("render: %v", err)
	}
}

func (w *Web) inbox(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "inbox")
	d.Error = r.URL.Query().Get("err")
	for _, t := range w.store.List("open", "managed") {
		if t.Turn == "owner" {
			d.YourTurn = append(d.YourTurn, t)
		} else {
			d.Waiting = append(d.Waiting, t)
		}
	}
	if r.URL.Query().Get("closed") == "1" {
		d.ShowClosed = true
		d.Closed = w.store.List("closed", "managed")
	}
	w.render(rw, 200, d)
}

// threads is the All-threads view: observed bus traffic mc tracks but does
// not manage. These never enter Your turn; an explicit raise at the seat
// promotes them onto the desk.
func (w *Web) threads(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "threads")
	d.Observed = w.store.List("", "observed")
	w.render(rw, 200, d)
}

func (w *Web) openForm(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "open")
	d.F["to"] = r.URL.Query().Get("to")
	d.F["expects"] = "reply"
	d.F["weight"] = "moment"
	w.render(rw, 200, d)
}

func (w *Web) openPost(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "open")
	for _, k := range []string{"title", "to", "context", "expects", "weight", "home"} {
		d.F[k] = strings.TrimSpace(r.FormValue(k))
	}
	targets := splitNames(d.F["to"])
	// The refusal doctrine: a thread without cold-open context does not open.
	switch {
	case d.F["title"] == "":
		d.Error = "refused: a thread needs a title"
	case len(targets) == 0:
		d.Error = "refused: a thread needs at least one recipient"
	case d.F["context"] == "":
		d.Error = "refused: no thread opens without cold-open context — write what a person switching back in needs to know"
	}
	if d.Error != "" {
		w.render(rw, 422, d)
		return
	}
	id := slug(d.F["title"])
	if w.store.Has(id) {
		id = fmt.Sprintf("%s-%d", id, time.Now().Unix()%100000)
	}
	user := w.userFor(r)
	turn := strings.Join(targets, ",")
	if err := w.store.Open(id, d.F["title"], d.F["context"], d.F["expects"], d.F["weight"], d.F["home"], user, targets, turn, "managed"); err != nil {
		d.Error = err.Error()
		w.render(rw, 500, d)
		return
	}
	intent := "request"
	if d.F["expects"] == "read" {
		intent = "inform"
	}
	body := d.F["title"] + "\n\n" + d.F["context"] + "\n\n[expects: " + d.F["expects"] + " — reply on this thread]"
	if err := w.bus.Send(user, targets, id, "", intent, body); err != nil {
		// Store opened but delivery refused (e.g. unknown agent): close the
		// record honestly rather than leave a phantom open thread.
		_ = w.store.Close(id, "delivery failed: "+err.Error())
		d.Error = "bus refused delivery: " + err.Error()
		w.render(rw, 502, d)
		return
	}
	w.ing.Kick()
	http.Redirect(rw, r, "/thread/"+id, http.StatusSeeOther)
}

func (w *Web) thread(rw http.ResponseWriter, r *http.Request) {
	t := w.store.Get(r.PathValue("id"))
	if t == nil {
		http.NotFound(rw, r)
		return
	}
	d := w.data(r, "thread")
	d.T = t
	d.Error = r.URL.Query().Get("err")
	w.render(rw, 200, d)
}

func (w *Web) reply(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := w.store.Get(id)
	if t == nil {
		http.NotFound(rw, r)
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Redirect(rw, r, "/thread/"+id+"?err=refused:+empty+reply", http.StatusSeeOther)
		return
	}
	targets := agentTargets(t, w.userFor(r))
	if len(targets) == 0 {
		http.Redirect(rw, r, "/thread/"+id+"?err=refused:+no+addressable+agent+on+this+thread", http.StatusSeeOther)
		return
	}
	if err := w.bus.Send(w.userFor(r), targets, id, "", r.FormValue("intent"), text); err != nil {
		http.Redirect(rw, r, "/thread/"+id+"?err="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}
	w.ing.Kick()
	time.Sleep(300 * time.Millisecond) // let the kick land so the redirect shows the message
	http.Redirect(rw, r, "/thread/"+id, http.StatusSeeOther)
}

func (w *Web) close(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := w.store.Get(id)
	if t == nil {
		http.NotFound(rw, r)
		return
	}
	res := strings.TrimSpace(r.FormValue("resolution"))
	if res == "" {
		http.Redirect(rw, r, "/thread/"+id+"?err=refused:+no+close+without+a+resolution", http.StatusSeeOther)
		return
	}
	if err := w.store.Close(id, res); err != nil {
		http.Redirect(rw, r, "/thread/"+id+"?err="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}
	if targets := agentTargets(t, w.userFor(r)); len(targets) > 0 {
		if err := w.bus.Send(w.userFor(r), targets, id, "", "inform", "[thread closed] "+res); err != nil {
			log.Printf("close notice for %s: %v", id, err)
			w.ing.Kick()
			http.Redirect(rw, r, "/?err="+template.URLQueryEscaper("closed, but the bus notice failed: "+err.Error()), http.StatusSeeOther)
			return
		}
	}
	w.ing.Kick()
	http.Redirect(rw, r, "/", http.StatusSeeOther)
}

func (w *Web) reopen(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := w.store.Reopen(id); err != nil {
		http.NotFound(rw, r)
		return
	}
	http.Redirect(rw, r, "/thread/"+id, http.StatusSeeOther)
}

type rosterGroup struct {
	Dir    string
	Agents []rosterAgent
}

type rosterAgent struct {
	Name   string
	Tool   string
	Status string
	Detail string
	Unread int
	Role   string
	Branch string
	GUID   string
}

func (w *Web) roster(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "roster")
	showAll := r.URL.Query().Get("all") == "1"
	d.ShowClosed = showAll // reuse: roster "show all" toggle
	busAgents, err := w.bus.List()
	if err != nil {
		d.Error = err.Error()
	}
	herderRows, herr := HerderList(w.herderBin)
	if herr != nil && d.Error == "" {
		d.Error = herr.Error()
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
	groups := map[string]*rosterGroup{}
	add := func(dir string, a rosterAgent) {
		key := w.missions.groupKey(dir)
		g := groups[key]
		if g == nil {
			g = &rosterGroup{Dir: key}
			groups[key] = g
		}
		g.Agents = append(g.Agents, a)
	}
	seen := map[string]bool{}
	for _, a := range busAgents {
		if !showAll && a.Status == "inactive" {
			continue
		}
		ra := rosterAgent{Name: a.Name, Tool: a.Tool, Status: a.Status, Detail: a.StatusCtx, Unread: int(a.Unread)}
		// Group by spawn-grain cwd (declared intent) when herder knows the
		// session; the bus directory tracks the live shell as it wanders.
		dir := a.Directory
		h, ok := bySID[a.SessionID]
		if !ok {
			if h, ok = byName[a.Name]; !ok {
				h, ok = byName[a.BaseName]
			}
		}
		if ok {
			ra.Role, ra.Branch, ra.GUID = h.Role, h.Branch, h.GUID
			seen[h.Label] = true
			if h.Cwd != "" {
				dir = h.Cwd
			}
		}
		add(dir, ra)
	}
	for _, h := range herderRows {
		if h.Label == "" || seen[h.Label] {
			continue
		}
		if !showAll && h.Status != "seated" {
			continue
		}
		add(h.Cwd, rosterAgent{Name: h.Label, Tool: h.Agent, Status: h.Status, Role: h.Role, Branch: h.Branch, GUID: h.GUID})
	}
	for _, g := range groups {
		d.Groups = append(d.Groups, *g)
	}
	sortGroups(d.Groups)
	w.render(rw, 200, d)
}

func sortGroups(gs []rosterGroup) {
	for i := range gs {
		for j := i + 1; j < len(gs); j++ {
			ri, rj := groupRank(gs[i].Dir), groupRank(gs[j].Dir)
			if rj < ri || (rj == ri && gs[j].Dir < gs[i].Dir) {
				gs[i], gs[j] = gs[j], gs[i]
			}
		}
	}
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

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
