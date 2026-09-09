package agentstore

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
)

// ProjectionVersion changes whenever Apply's fold changes, so a stale
// snapshot is replayed instead of trusted.
const ProjectionVersion = 1

// EventsKept is how many trailing events each agent record retains.
const EventsKept = 32

// Projection is the replayable fold of events.jsonl. Agents are keyed by
// hcom name; each name holds its incarnations oldest first. A reused name
// after a close is a NEW incarnation that inherits nothing.
type Projection struct {
	Version      int                       `json:"version"`
	EventsOffset int64                     `json:"events_offset"`
	Agents       map[string][]*AgentView   `json:"agents"`
	Requests     map[string]*RequestRecord `json:"requests"`
	// UnnamedSessions are pane-only observations (no hcom name yet), keyed
	// tool + "/" + session id. A later bind by name finds them here.
	UnnamedSessions map[string]*SessionView `json:"unnamed_sessions"`
	SnapshotErr     error                   `json:"-"`
}

// RequestRecord ties launch-requested to its ready/failed outcome, including
// requests that never named an agent (a failed launch fabricates no row).
type RequestRecord struct {
	Requested Event  `json:"requested"`
	State     string `json:"state"` // requested | ready | failed
	Name      string `json:"name,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// AgentView is the one shape the CLI, the board and the detail read. It is
// both the stored incarnation record and the fold result (Binding, Session
// and Incarnation are set from the roster at fold time).
type AgentView struct {
	Name        string      `json:"name"`
	BaseName    string      `json:"base_name,omitempty"`
	Tool        string      `json:"tool,omitempty"`
	Tag         string      `json:"tag,omitempty"`
	Incarnation time.Time   `json:"incarnation"` // roster created_at when known, else the first event's time
	FirstSeen   time.Time   `json:"first_seen"`
	LastSeen    time.Time   `json:"last_seen"`
	Closed      *time.Time  `json:"closed,omitempty"` // culled / launch-failed / mirror.stopped
	CloseReason string      `json:"close_reason,omitempty"`
	Provenance  Provenance  `json:"provenance"`
	Assignment  *Assignment `json:"assignment,omitempty"`
	Annotation  *Annotation `json:"annotation,omitempty"`
	// Manager is the mutable hierarchy pointer ("who manages me"): the latest
	// reparent, else the launcher. Provenance.Launcher is immutable.
	Manager    string        `json:"manager,omitempty"`
	ManagerBy  string        `json:"manager_by,omitempty"`
	ManagerAt  *time.Time    `json:"manager_at,omitempty"`
	Parent     string        `json:"parent,omitempty"`    // hcom parent_name (mirrored)
	FromName   string        `json:"from_name,omitempty"` // fork source
	Binding    *Binding      `json:"binding,omitempty"`   // fold-time: claimed session vs roster
	Session    *SessionView  `json:"session,omitempty"`   // current (nil when none known)
	Sessions   []SessionView `json:"sessions,omitempty"`  // history, newest first
	Events     []Event       `json:"events"`              // last EventsKept
	EventCount int           `json:"event_count"`
}

type Provenance struct {
	Kind           string     `json:"kind"` // registered | mirrored | unregistered
	Launcher       string     `json:"launcher,omitempty"`
	LauncherKind   string     `json:"launcher_kind,omitempty"`
	ModelRequested string     `json:"model_requested,omitempty"`
	Effort         string     `json:"effort,omitempty"`
	Workspace      string     `json:"workspace,omitempty"`
	PaneRequested  string     `json:"pane_requested,omitempty"`
	Pane           string     `json:"pane,omitempty"`
	Cwd            string     `json:"cwd,omitempty"`
	Batch          string     `json:"batch,omitempty"`
	Request        string     `json:"request,omitempty"`
	RequestedAt    *time.Time `json:"requested_at,omitempty"`
	ReadyAt        *time.Time `json:"ready_at,omitempty"`
	State          string     `json:"state,omitempty"` // requested | ready | failed | culled | stopped
}

type Assignment struct {
	Mission string    `json:"mission"`
	Brief   string    `json:"brief,omitempty"`
	Thread  string    `json:"thread,omitempty"`
	Task    string    `json:"task,omitempty"`
	By      string    `json:"by,omitempty"`
	At      time.Time `json:"at"`
}

type Annotation struct {
	Title string    `json:"title,omitempty"`
	Note  string    `json:"note,omitempty"`
	By    string    `json:"by,omitempty"`
	At    time.Time `json:"at"`
}

// Binding records a register claim (name ↔ session) against the roster.
// The roster is always current; a conflict is recorded, never re-keyed.
type Binding struct {
	State   string `json:"state"` // verified | pending | conflict
	Claimed string `json:"claimed,omitempty"`
	Roster  string `json:"roster,omitempty"`
}

type SessionView struct {
	SessionID string     `json:"session_id"`
	Tool      string     `json:"tool,omitempty"`
	Path      string     `json:"path,omitempty"`
	Started   *time.Time `json:"started,omitempty"`
	Ended     *time.Time `json:"ended,omitempty"`
	EndReason string     `json:"end_reason,omitempty"`
	Vitals    VitalsView `json:"vitals"`
}

// VitalsView is the placeholder shape the observer (a later unit) fills.
type VitalsView struct {
	Model          string     `json:"model,omitempty"`
	Used           int64      `json:"used"`
	Window         *int64     `json:"window,omitempty"`
	Percent        *float64   `json:"percent,omitempty"`
	Output         *int64     `json:"output,omitempty"`
	LastActivity   *time.Time `json:"last_activity,omitempty"`
	LastTurnAt     *time.Time `json:"last_turn_at,omitempty"`
	TurnsObserved  int        `json:"turns_observed"`
	TurnsTotal     *int       `json:"turns_total,omitempty"`
	Compactions    int        `json:"compactions"`
	LastCompaction *struct {
		At        time.Time `json:"at"`
		Pre, Post int64
		Trigger   string `json:"trigger"`
	} `json:"last_compaction,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	Phase      string    `json:"phase,omitempty"`
	Err        string    `json:"err,omitempty"`
}

func NewProjection() *Projection {
	return &Projection{Version: ProjectionVersion, Agents: map[string][]*AgentView{}, Requests: map[string]*RequestRecord{}, UnnamedSessions: map[string]*SessionView{}}
}

// Marshal is the canonical snapshot encoding (sorted map keys, UTC times), so
// snapshot+tail and full replay compare byte-equal.
func (p *Projection) Marshal() ([]byte, error) { return json.Marshal(p) }

func closes(kind string) bool {
	return kind == KindCulled || kind == KindLaunchFailed || kind == KindMirrorStopped
}

// attaches reports kinds that never open a new incarnation: they describe the
// end of the latest one even when it is already closed.
func attaches(kind string) bool {
	return closes(kind) || kind == KindCullRequested || kind == KindSessionEnded || kind == KindSessionSupersede
}

// record returns the incarnation an event belongs to, opening a new one when
// the latest is closed and the event is not an end-of-life attachment.
func (p *Projection) record(e Event) *AgentView {
	list := p.Agents[e.Name]
	if n := len(list); n > 0 {
		latest := list[n-1]
		if latest.Closed == nil || attaches(e.Kind) {
			return latest
		}
	}
	view := &AgentView{Name: e.Name, Incarnation: e.At, FirstSeen: e.At, LastSeen: e.At, Provenance: Provenance{Kind: "unregistered"}, Events: []Event{}}
	p.Agents[e.Name] = append(list, view)
	return view
}

// Apply folds one event. It is total: an event never fails to apply.
func (p *Projection) Apply(e Event, _ int64) {
	at := e.At.UTC()
	e.At = at
	if e.Kind == KindLaunchRequested {
		p.Requests[e.ID] = &RequestRecord{Requested: e, State: "requested", Name: e.Name}
		if e.Name == "" {
			return
		}
	}
	if e.Kind == KindLaunchFailed && e.Name == "" {
		if req := p.Requests[e.Request]; req != nil {
			req.State, req.Reason = "failed", e.Reason
		}
		return
	}
	if e.Kind == KindMirrorBatch && len(e.Instances) > 0 {
		// A batch names its subjects in instances; fan out one apply per name.
		for _, name := range e.Instances {
			child := e
			child.Name, child.Instances = name, nil
			child.Kind = KindMirrorCreated
			p.Apply(child, 0)
		}
		if e.Name == "" {
			return
		}
	}
	if e.Name == "" {
		if e.Session != "" && (e.Kind == KindSessionObserved || e.Kind == KindSessionEnded || e.Kind == KindSessionSupersede) {
			p.unnamedSession(e)
		}
		return
	}
	v := p.record(e)
	v.EventCount++
	v.Events = append(v.Events, e)
	if len(v.Events) > EventsKept {
		v.Events = v.Events[len(v.Events)-EventsKept:]
	}
	if at.After(v.LastSeen) {
		v.LastSeen = at
	}
	if at.Before(v.FirstSeen) {
		v.FirstSeen, v.Incarnation = at, at
	}
	// setLauncher fills an empty launcher; a registered fact (kind != mirror)
	// also supersedes weaker mirrored attribution, carrying the default
	// manager along unless someone reparented explicitly.
	setLauncher := func(by, kind string) {
		if by == "" {
			return
		}
		pr := &v.Provenance
		if pr.Launcher != "" && !(pr.LauncherKind == "mirror" && kind != "mirror") {
			return
		}
		managerFollows := v.Manager == "" || (v.ManagerAt == nil && v.Manager == pr.Launcher)
		pr.Launcher, pr.LauncherKind = by, kind
		if managerFollows {
			v.Manager = by
		}
	}
	registered := func() {
		if v.Provenance.Kind != "registered" {
			v.Provenance.Kind = "registered"
		}
	}
	absorb := func(src Event) {
		if v.Tool == "" {
			v.Tool = src.Tool
		}
		if v.Tag == "" {
			v.Tag = src.Tag
		}
		pr := &v.Provenance
		if pr.ModelRequested == "" {
			pr.ModelRequested = src.Model
		}
		if pr.Effort == "" {
			pr.Effort = src.Effort
		}
		if src.Placement != nil {
			if pr.Workspace == "" {
				pr.Workspace = src.Placement.Workspace
			}
			if pr.PaneRequested == "" {
				pr.PaneRequested = firstNonEmpty(src.Placement.Pane, src.Placement.SplitFrom)
			}
		}
	}
	switch e.Kind {
	case KindLaunchRequested:
		registered()
		absorb(e)
		v.Provenance.Request, v.Provenance.State = e.ID, "requested"
		v.Provenance.RequestedAt = &at
		setLauncher(e.By, firstNonEmpty(e.LauncherKind, e.ByKind, "agent"))
	case KindLaunchReady:
		registered()
		if req := p.Requests[e.Request]; req != nil {
			req.State, req.Name = "ready", e.Name
			absorb(req.Requested)
			v.Provenance.Request = req.Requested.ID
			t := req.Requested.At
			v.Provenance.RequestedAt = &t
			setLauncher(req.Requested.By, firstNonEmpty(req.Requested.LauncherKind, req.Requested.ByKind, "agent"))
		}
		absorb(e)
		setLauncher(e.By, firstNonEmpty(e.LauncherKind, e.ByKind, "agent"))
		v.Provenance.State = "ready"
		v.Provenance.ReadyAt = &at
		v.Provenance.Batch = firstNonEmpty(e.Batch, v.Provenance.Batch)
		v.Provenance.Pane = firstNonEmpty(e.Pane, v.Provenance.Pane)
		v.Provenance.Cwd = firstNonEmpty(e.Cwd, v.Provenance.Cwd)
		if e.Session != "" {
			v.openSession(e.Session, firstNonEmpty(e.Tool, v.Tool), "", at, "launch")
		}
	case KindLaunchFailed:
		if req := p.Requests[e.Request]; req != nil {
			req.State, req.Reason, req.Name = "failed", e.Reason, e.Name
		}
		v.Provenance.State = "failed"
		v.close(at, "launch-failed: "+e.Reason)
	case KindCullRequested:
		// intent only; the row stays live until culled
	case KindCulled:
		v.Provenance.State = "culled"
		v.close(at, "culled")
		v.endSession(at, "culled")
	case KindResume:
		registered()
		from := firstNonEmpty(e.FromSession, currentSession(v))
		if from != "" {
			v.endSession(at, "resumed")
		}
		if e.Pane != "" {
			v.Provenance.Pane = e.Pane
		}
	case KindFork:
		registered()
		v.FromName = e.FromName
		setLauncher(e.By, firstNonEmpty(e.ByKind, "agent"))
		if e.Pane != "" {
			v.Provenance.Pane = e.Pane
		}
		if v.Provenance.State == "" {
			v.Provenance.State = "ready"
		}
	case KindCompactRequested:
		// recorded in Events only
	case KindAssign:
		v.Assignment = &Assignment{Mission: e.Mission, Brief: e.Brief, Thread: e.Thread, Task: e.Task, By: e.By, At: at}
	case KindAnnotate:
		v.Annotation = &Annotation{Title: e.Title, Note: e.Note, By: e.By, At: at}
	case KindReparent:
		// Event time orders reparents, not arrival: an older one landing
		// late never overwrites a newer manager.
		if v.ManagerAt != nil && at.Before(*v.ManagerAt) {
			break
		}
		v.Manager, v.ManagerBy = e.Manager, e.By
		v.ManagerAt = &at
	case KindMirrorCreated, KindMirrorReady, KindMirrorBatch:
		if v.Provenance.Kind == "unregistered" {
			v.Provenance.Kind = "mirrored"
		}
		setLauncher(e.By, "mirror")
		if e.ParentName != "" {
			v.Parent = e.ParentName
		}
		if v.Provenance.State == "" && e.Kind == KindMirrorReady {
			v.Provenance.State = "ready"
		}
	case KindMirrorStopped:
		v.Provenance.State = "stopped"
		v.close(at, "stopped: "+e.Reason)
		v.endSession(at, firstNonEmpty(e.Reason, "stopped"))
	case KindSessionObserved:
		v.openSession(e.Session, firstNonEmpty(e.Tool, v.Tool), e.Path, at, "observed")
	case KindSessionEnded, KindSessionSupersede:
		reason := firstNonEmpty(e.Reason, map[string]string{KindSessionEnded: "ended", KindSessionSupersede: "superseded"}[e.Kind])
		v.endNamedSession(e.Session, at, reason)
	}
}

func (p *Projection) unnamedSession(e Event) {
	key := e.Tool + "/" + e.Session
	at := e.At
	view := p.UnnamedSessions[key]
	if view == nil {
		view = &SessionView{SessionID: e.Session, Tool: e.Tool, Started: &at}
		p.UnnamedSessions[key] = view
	}
	if e.Path != "" {
		view.Path = e.Path
	}
	if e.Kind != KindSessionObserved && view.Ended == nil {
		view.Ended, view.EndReason = &at, firstNonEmpty(e.Reason, strings.TrimPrefix(e.Kind, "session."))
	}
}

// endNamedSession closes the session the event names (not whichever is
// current); an unknown id is recorded in Events only.
func (v *AgentView) endNamedSession(id string, at time.Time, reason string) {
	for i := range v.Sessions {
		if v.Sessions[i].SessionID == id {
			if v.Sessions[i].Ended == nil {
				t := at
				v.Sessions[i].Ended, v.Sessions[i].EndReason = &t, reason
			}
			return
		}
	}
}

func (v *AgentView) close(at time.Time, reason string) {
	if v.Closed == nil {
		t := at
		v.Closed, v.CloseReason = &t, reason
	}
}

func currentSession(v *AgentView) string {
	if len(v.Sessions) > 0 && v.Sessions[0].Ended == nil {
		return v.Sessions[0].SessionID
	}
	return ""
}

func (v *AgentView) openSession(id, tool, path string, at time.Time, reason string) {
	if len(v.Sessions) > 0 && v.Sessions[0].SessionID == id {
		if path != "" {
			v.Sessions[0].Path = path
		}
		return
	}
	v.endSession(at, "superseded")
	t := at
	v.Sessions = append([]SessionView{{SessionID: id, Tool: tool, Path: path, Started: &t}}, v.Sessions...)
}

func (v *AgentView) endSession(at time.Time, reason string) {
	if len(v.Sessions) > 0 && v.Sessions[0].Ended == nil {
		t := at
		v.Sessions[0].Ended, v.Sessions[0].EndReason = &t, reason
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Names returns every agent name in the projection, sorted.
func (p *Projection) Names() []string {
	names := make([]string, 0, len(p.Agents))
	for name := range p.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Latest returns the newest incarnation for name (no roster evidence).
func (p *Projection) Latest(name string) *AgentView {
	list := p.Agents[name]
	if len(list) == 0 {
		return nil
	}
	return list[len(list)-1]
}

// Incarnation picks the record that matches hcom's creation time: the first
// incarnation not closed before createdAt. A record whose first event
// predates createdAt belongs to an EARLIER life of the name unless its open
// session is the roster's session: a raw `hcom kill`, a crash or a missed
// wrapper leaves no close event, and roster creation is the newer evidence.
// With no roster time (zero), the latest incarnation is the only answer.
func (p *Projection) Incarnation(name string, createdAt time.Time, rosterSession string) *AgentView {
	list := p.Agents[name]
	if len(list) == 0 {
		return nil
	}
	if createdAt.IsZero() {
		return list[len(list)-1]
	}
	for _, v := range list {
		if v.Closed != nil && v.Closed.Before(createdAt) {
			continue
		}
		if v.FirstSeen.Before(createdAt) && (rosterSession == "" || currentSession(v) != rosterSession) {
			return nil
		}
		return v
	}
	return nil
}

// View folds the roster row into a copy of the matching record: Incarnation
// becomes the roster's created_at, the current Session is the roster's
// session id, and Binding records agreement or conflict with any claim. Nil
// when the store has no record for this incarnation (unregistered).
func (p *Projection) View(name string, roster *hcomidentity.Row) *AgentView {
	var stored *AgentView
	if roster != nil {
		stored = p.Incarnation(name, roster.CreatedAt, roster.SessionID)
	} else {
		stored = p.Latest(name)
	}
	if stored == nil {
		return nil
	}
	view := *stored
	view.Events = append([]Event(nil), stored.Events...)
	view.Sessions = append([]SessionView(nil), stored.Sessions...)
	if len(view.Sessions) > 0 && view.Sessions[0].Ended == nil {
		current := view.Sessions[0]
		view.Session = &current
	}
	if roster == nil {
		return &view
	}
	if !roster.CreatedAt.IsZero() {
		view.Incarnation = roster.CreatedAt.UTC()
	}
	if view.Tool == "" {
		view.Tool = roster.Tool
	}
	if view.Tag == "" {
		view.Tag = roster.Tag
	}
	view.BaseName = roster.BaseName
	if roster.ParentName != "" {
		view.Parent = roster.ParentName
	}
	claimed := ""
	if view.Session != nil {
		claimed = view.Session.SessionID
	}
	switch {
	case roster.SessionID == "" && claimed == "":
	case roster.SessionID == "":
		view.Binding = &Binding{State: "pending", Claimed: claimed}
	case claimed == "" || claimed == roster.SessionID:
		view.Binding = &Binding{State: "verified", Claimed: claimed, Roster: roster.SessionID}
		view.Session = &SessionView{SessionID: roster.SessionID, Tool: roster.Tool, Path: roster.TranscriptPath}
		if len(view.Sessions) > 0 && view.Sessions[0].SessionID == roster.SessionID {
			view.Session = &view.Sessions[0]
		}
	default:
		view.Binding = &Binding{State: "conflict", Claimed: claimed, Roster: roster.SessionID}
		view.Session = &SessionView{SessionID: roster.SessionID, Tool: roster.Tool, Path: roster.TranscriptPath}
	}
	return &view
}
