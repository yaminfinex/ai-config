// Package agentstore is the ONLY reader and writer of
// $HERDER_STATE_DIR/agents/: an append-only events.jsonl, a rebuildable
// snapshot.json projection, and the one-time launch-edges import. Nothing in
// herder consults this store before a lifecycle action; it records what
// callers report and what herder observed.
package agentstore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event kinds (Fable design §4 plus the owner's reparent amendment).
const (
	KindLaunchRequested  = "launch-requested"
	KindLaunchReady      = "launch-ready"
	KindLaunchFailed     = "launch-failed"
	KindCullRequested    = "cull-requested"
	KindCulled           = "culled"
	KindResume           = "resume"
	KindFork             = "fork"
	KindCompactRequested = "compact-requested"
	KindAssign           = "assign"
	KindAnnotate         = "annotate"
	KindReparent         = "reparent"
	KindMirrorCreated    = "mirror.created"
	KindMirrorReady      = "mirror.ready"
	KindMirrorStopped    = "mirror.stopped"
	KindMirrorBatch      = "mirror.batch_launched"
	KindSessionObserved  = "session.observed"
	KindSessionEnded     = "session.ended"
	KindSessionSupersede = "session.superseded"
)

// Kinds lists every accepted kind in CLI order.
var Kinds = []string{
	KindLaunchRequested, KindLaunchReady, KindLaunchFailed,
	KindCullRequested, KindCulled, KindResume, KindFork, KindCompactRequested,
	KindAssign, KindAnnotate, KindReparent,
	KindMirrorCreated, KindMirrorReady, KindMirrorStopped, KindMirrorBatch,
	KindSessionObserved, KindSessionEnded, KindSessionSupersede,
}

// Placement is where a launch was asked to land (launch-requested only).
type Placement struct {
	Workspace string `json:"workspace,omitempty"`
	Pane      string `json:"pane,omitempty"`
	SplitFrom string `json:"split_from,omitempty"`
}

// Event is one line of events.jsonl. Common fields first, then the per-kind
// extras; unused extras are omitted from the line.
type Event struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	By      string    `json:"by,omitempty"`
	ByKind  string    `json:"by_kind,omitempty"` // agent|web|user|unknown|serve|observer|mirror
	Name    string    `json:"name,omitempty"`
	Request string    `json:"request,omitempty"`

	// launch-requested (launch-ready may carry them too: the edge import and
	// wrappers that skipped launch-requested have no request to inherit from).
	Tool         string     `json:"tool,omitempty"`
	Model        string     `json:"model,omitempty"`
	Effort       string     `json:"effort,omitempty"`
	Tag          string     `json:"tag,omitempty"`
	Placement    *Placement `json:"placement,omitempty"`
	PromptRef    string     `json:"prompt_ref,omitempty"`
	LauncherKind string     `json:"launcher_kind,omitempty"`

	// launch-ready / launch-failed / cull / resume / fork
	Batch       string `json:"batch,omitempty"`
	Pane        string `json:"pane,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	Session     string `json:"session,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Close       string `json:"close,omitempty"`
	FromSession string `json:"from_session,omitempty"`
	FromName    string `json:"from_name,omitempty"`
	SteerChars  *int   `json:"steer_chars,omitempty"`

	// assign / annotate / reparent
	Mission string `json:"mission,omitempty"`
	Brief   string `json:"brief,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Task    string `json:"task,omitempty"`
	Title   string `json:"title,omitempty"`
	Note    string `json:"note,omitempty"`
	Manager string `json:"manager,omitempty"`

	// mirror.*
	HcomEvent      string   `json:"hcom_event,omitempty"`
	Instances      []string `json:"instances,omitempty"`
	ParentName     string   `json:"parent_name,omitempty"`
	IsHcomLaunched *bool    `json:"is_hcom_launched,omitempty"`

	// session.*
	Path string `json:"path,omitempty"`
}

// Spec is the per-kind field contract, keyed by CLI flag name (the JSON name
// with "-" for "_", and --from for from_name). It lives ONCE here: the
// package's Append and the register CLI both validate through it.
type Spec struct {
	Required []string
	OneOf    []string // exactly one of these
	Optional []string
}

// CommonFlags are accepted by every kind.
var CommonFlags = []string{"name", "by", "by-kind", "at", "id", "request"}

var mirrorFlags = []string{"hcom-event", "reason", "batch", "instances", "parent-name", "is-hcom-launched"}
var sessionFlags = []string{"tool", "path", "reason"}

var specs = map[string]Spec{
	KindLaunchRequested:  {Required: []string{"tool", "tag"}, OneOf: []string{"workspace", "pane", "split-from"}, Optional: []string{"model", "effort", "prompt-ref", "batch", "launcher-kind"}},
	KindLaunchReady:      {Required: []string{"name"}, Optional: []string{"batch", "pane", "cwd", "session", "tool", "model", "effort", "tag", "workspace", "launcher-kind"}},
	KindLaunchFailed:     {Required: []string{"reason"}, Optional: []string{"batch", "pane"}},
	KindCullRequested:    {Required: []string{"name"}, Optional: []string{"pane"}},
	KindCulled:           {Required: []string{"name", "pane", "close"}},
	KindResume:           {Required: []string{"name"}, Optional: []string{"pane", "from-session"}},
	KindFork:             {Required: []string{"name", "from"}, Optional: []string{"pane"}},
	KindCompactRequested: {Required: []string{"name"}, Optional: []string{"steer-chars"}},
	KindAssign:           {Required: []string{"name", "mission"}, Optional: []string{"brief", "thread", "task"}},
	KindAnnotate:         {Required: []string{"name"}, Optional: []string{"title", "note"}},
	KindReparent:         {Required: []string{"name", "manager"}},
	KindMirrorCreated:    {Required: []string{"name"}, Optional: mirrorFlags},
	KindMirrorReady:      {Required: []string{"name"}, Optional: mirrorFlags},
	KindMirrorStopped:    {Required: []string{"name"}, Optional: mirrorFlags},
	KindMirrorBatch:      {Required: []string{"instances"}, Optional: mirrorFlags}, // name optional: fans out to instances
	KindSessionObserved:  {Required: []string{"session"}, Optional: sessionFlags},  // name optional: pane-only session
	KindSessionEnded:     {Required: []string{"session"}, Optional: sessionFlags},
	KindSessionSupersede: {Required: []string{"session"}, Optional: sessionFlags},
}

// SpecFor returns the contract for kind.
func SpecFor(kind string) (Spec, bool) {
	sp, ok := specs[kind]
	return sp, ok
}

// ByKinds and LauncherKinds are the closed attribution vocabularies.
var ByKinds = []string{"agent", "web", "user", "unknown", "serve", "observer", "mirror"}
var LauncherKinds = []string{"agent", "web", "user", "unknown", "mirror"}
var Tools = []string{"claude", "codex"}

// present reports which flag-named fields carry a value.
func (e Event) present() map[string]bool {
	m := map[string]bool{}
	set := func(name string, on bool) {
		if on {
			m[name] = true
		}
	}
	set("name", e.Name != "")
	set("request", e.Request != "")
	set("tool", e.Tool != "")
	set("model", e.Model != "")
	set("effort", e.Effort != "")
	set("tag", e.Tag != "")
	set("prompt-ref", e.PromptRef != "")
	set("launcher-kind", e.LauncherKind != "")
	set("batch", e.Batch != "")
	set("cwd", e.Cwd != "")
	set("session", e.Session != "")
	set("reason", e.Reason != "")
	set("close", e.Close != "")
	set("from-session", e.FromSession != "")
	set("from", e.FromName != "")
	set("steer-chars", e.SteerChars != nil)
	set("mission", e.Mission != "")
	set("brief", e.Brief != "")
	set("thread", e.Thread != "")
	set("task", e.Task != "")
	set("title", e.Title != "")
	set("note", e.Note != "")
	set("manager", e.Manager != "")
	set("hcom-event", e.HcomEvent != "")
	set("instances", len(e.Instances) > 0)
	set("parent-name", e.ParentName != "")
	set("is-hcom-launched", e.IsHcomLaunched != nil)
	set("path", e.Path != "")
	pane := e.Pane != ""
	if e.Placement != nil {
		set("workspace", e.Placement.Workspace != "")
		set("split-from", e.Placement.SplitFrom != "")
		pane = pane || e.Placement.Pane != ""
	}
	set("pane", pane)
	return m
}

// Validate enforces the per-kind contract. It is the ONE validator: the CLI
// and in-process writers (the serve, later) both go through Append.
func (e Event) Validate() error {
	if !ValidID(e.ID) {
		return fmt.Errorf("event id %q is not UUID-shaped", e.ID)
	}
	if e.At.IsZero() {
		return fmt.Errorf("event at is zero")
	}
	sp, ok := specs[e.Kind]
	if !ok {
		return fmt.Errorf("unknown event kind %q (known: %s)", e.Kind, strings.Join(Kinds, ", "))
	}
	if e.Request != "" && !ValidID(e.Request) {
		return fmt.Errorf("request %q is not UUID-shaped", e.Request)
	}
	if e.ByKind != "" && !contains(ByKinds, e.ByKind) {
		return fmt.Errorf("invalid by_kind %q (one of %s)", e.ByKind, strings.Join(ByKinds, ", "))
	}
	allowed := map[string]bool{}
	for _, list := range [][]string{CommonFlags, sp.Required, sp.OneOf, sp.Optional} {
		for _, name := range list {
			allowed[name] = true
		}
	}
	got := e.present()
	var extra []string
	for name := range got {
		if !allowed[name] {
			extra = append(extra, "--"+name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("%s does not accept %s", e.Kind, strings.Join(extra, ", "))
	}
	for _, name := range sp.Required {
		if !got[name] {
			return fmt.Errorf("%s requires --%s", e.Kind, name)
		}
	}
	if len(sp.OneOf) > 0 {
		n := 0
		for _, name := range sp.OneOf {
			if got[name] {
				n++
			}
		}
		if n != 1 {
			return fmt.Errorf("%s requires exactly one of --%s", e.Kind, strings.Join(sp.OneOf, ", --"))
		}
	}
	if e.Kind == KindLaunchRequested && e.Pane != "" {
		return fmt.Errorf("%s carries its pane in placement, not --pane at top level", e.Kind)
	}
	if e.Tool != "" && !contains(Tools, e.Tool) {
		return fmt.Errorf("unsupported tool %q (one of %s)", e.Tool, strings.Join(Tools, ", "))
	}
	if e.LauncherKind != "" && !contains(LauncherKinds, e.LauncherKind) {
		return fmt.Errorf("invalid launcher_kind %q", e.LauncherKind)
	}
	if e.Close != "" && e.Close != "managed" && e.Close != "label-fallback" {
		return fmt.Errorf("%s requires --close managed|label-fallback", e.Kind)
	}
	if e.SteerChars != nil && *e.SteerChars < 0 {
		return fmt.Errorf("steer_chars must be non-negative")
	}
	if e.Kind == KindAnnotate && e.Title == "" && e.Note == "" {
		return fmt.Errorf("%s requires --title or --note", e.Kind)
	}
	for _, text := range []string{e.Name, e.Manager, e.FromName, e.ParentName, e.By} {
		if strings.ContainsAny(text, "\r\n\t\x00") {
			return fmt.Errorf("identity fields must not contain control characters")
		}
	}
	return nil
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// Encode renders the single line appended to events.jsonl (trailing newline).
func Encode(e Event) ([]byte, error) {
	e.At = e.At.UTC()
	line, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(line, '\n'), nil
}
