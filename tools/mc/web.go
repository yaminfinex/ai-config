package main

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	mux.HandleFunc("GET /talk", w.talkForm)
	mux.HandleFunc("POST /talk", w.talkPost)
	mux.HandleFunc("GET /open", w.openRedirect)
	mux.HandleFunc("GET /threads", w.threads)
	mux.HandleFunc("GET /thread/{id}", w.thread)
	mux.HandleFunc("GET /mission/{slug}", w.mission)
	mux.HandleFunc("GET /mission/{slug}/file/{rel...}", w.missionFile)
	mux.HandleFunc("POST /thread/{id}/reply", w.reply)
	mux.HandleFunc("POST /thread/{id}/close", w.close)
	mux.HandleFunc("POST /thread/{id}/reopen", w.reopen)
	mux.HandleFunc("POST /thread/{id}/retitle", w.retitle)
	mux.HandleFunc("GET /roster", w.roster)
	return securityHeaders(mux)
}

const contentSecurityPolicy = "script-src 'none'; object-src 'none'; base-uri 'none'"

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
	IngestWarn string
	// all threads (observed grade — tracked bus traffic, never Your turn)
	Observed []*Thread
	// across-mission home and in-mission page
	Missions       []missionStatus
	MissionListErr string
	Mission        missionStatus
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
	if w.ing != nil {
		d.IngestWarn = w.ing.StallWarning()
	}
	if w.missions != nil {
		d.Missions, d.MissionListErr = w.missions.AllStatuses()
	}
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

func (w *Web) mission(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "mission")
	slug := r.PathValue("slug")
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

// threads is the All-threads view: observed bus traffic mc tracks but does
// not manage. These never enter Your turn; an explicit raise at the seat
// promotes them onto the desk.
func (w *Web) threads(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "threads")
	d.Observed = w.store.List("", "observed")
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
	if err := w.bus.Send(user, targets, id, "", "request", d.F["message"]); err != nil {
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
	d.T = t
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
	if !w.requireManaged(rw, r, t) {
		return
	}
	user := w.userFor(r)
	res := strings.TrimSpace(r.FormValue("resolution"))
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
	if targets, err := w.liveAgentTargets(t, user); err != nil {
		// The close is durable even when liveness cannot be inspected. Do not
		// risk a mixed live/dead send, and do not present lookup failure as a
		// delivery failure.
		log.Printf("close notice liveness for %s: %v", id, err)
	} else if len(targets) > 0 {
		if err := w.bus.Send(user, targets, id, "", "inform", "[thread closed] "+res); err != nil {
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
	Name   string
	Tool   string
	Status string
	Detail string
	Unread int
	Role   string
	Branch string
	GUID   string
	// Unmanaged means hcom knows this row but the herder registry does not.
	Unmanaged bool
}

func (w *Web) roster(rw http.ResponseWriter, r *http.Request) {
	d := w.data(r, "roster")
	showAll := r.URL.Query().Get("all") == "1"
	d.ShowClosed = showAll // reuse: roster "show all" toggle
	d.Groups, d.Error = w.rosterGroups(showAll)
	w.render(rw, 200, d)
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
	add := func(dir string, a rosterAgent) {
		if w.missions != nil {
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
		} else {
			ra.Unmanaged = true
		}
		add(dir, ra)
	}
	for _, h := range herderRows {
		if h.Label == "" || h.Label == w.seat || seen[h.Label] {
			continue
		}
		if !showAll && h.Status != "seated" {
			continue
		}
		add(h.Cwd, rosterAgent{Name: h.Label, Tool: h.Agent, Status: h.Status, Role: h.Role, Branch: h.Branch, GUID: h.GUID})
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
				if a.Name == target {
					return []string{target}, g.Mission, nil
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
				if a.Name != "" && a.Name != w.seat && !contains(targets, a.Name) {
					targets = append(targets, a.Name)
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

func (w *Web) liveAgentTargets(t *Thread, user string) ([]string, error) {
	wanted := agentTargets(t, user)
	if len(wanted) == 0 {
		return nil, nil
	}
	agents, err := w.bus.List()
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(agents)*2)
	for _, a := range agents {
		if !liveBusStatus(a.Status) {
			continue
		}
		live[a.Name] = true
		live[a.BaseName] = true
	}
	var out []string
	for _, name := range wanted {
		if live[name] {
			out = append(out, name)
		}
	}
	return out, nil
}

func (w *Web) participantState(t *Thread, user string) string {
	wanted := agentTargets(t, user)
	if len(wanted) == 0 {
		return ""
	}
	live, err := w.liveAgentTargets(t, user)
	if err != nil {
		return "participant liveness unavailable"
	}
	if len(live) == 0 {
		return "all agent participants gone"
	}
	if len(live) < len(wanted) {
		return fmt.Sprintf("%d of %d agent participants live", len(live), len(wanted))
	}
	return fmt.Sprintf("%d agent participants live", len(live))
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
