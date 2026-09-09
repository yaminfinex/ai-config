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
	SteerChars  int    `json:"steer_chars,omitempty"`

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

// Validate enforces the per-kind contract before a line is appended.
func (e Event) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("event id is empty")
	}
	if e.At.IsZero() {
		return fmt.Errorf("event at is zero")
	}
	if !knownKind(e.Kind) {
		return fmt.Errorf("unknown event kind %q (known: %s)", e.Kind, strings.Join(Kinds, ", "))
	}
	need := func(field, value string) error {
		if value == "" {
			return fmt.Errorf("%s requires --%s", e.Kind, field)
		}
		return nil
	}
	switch e.Kind {
	case KindLaunchRequested:
		if err := need("tool", e.Tool); err != nil {
			return err
		}
		if e.Placement == nil || (e.Placement.Workspace == "" && e.Placement.Pane == "" && e.Placement.SplitFrom == "") {
			return fmt.Errorf("%s requires one of --workspace, --pane, --split-from", e.Kind)
		}
	case KindLaunchReady:
		return need("name", e.Name)
	case KindLaunchFailed:
		return need("reason", e.Reason)
	case KindCulled:
		if err := need("name", e.Name); err != nil {
			return err
		}
		if e.Close != "managed" && e.Close != "label-fallback" {
			return fmt.Errorf("%s requires --close managed|label-fallback", e.Kind)
		}
	case KindFork:
		if err := need("name", e.Name); err != nil {
			return err
		}
		return need("from", e.FromName)
	case KindAssign:
		if err := need("name", e.Name); err != nil {
			return err
		}
		return need("mission", e.Mission)
	case KindAnnotate:
		if err := need("name", e.Name); err != nil {
			return err
		}
		if e.Title == "" && e.Note == "" {
			return fmt.Errorf("%s requires --title or --note", e.Kind)
		}
	case KindReparent:
		if err := need("name", e.Name); err != nil {
			return err
		}
		return need("manager", e.Manager)
	case KindSessionObserved, KindSessionEnded, KindSessionSupersede:
		if err := need("name", e.Name); err != nil {
			return err
		}
		return need("session", e.Session)
	default:
		return need("name", e.Name)
	}
	return nil
}

func knownKind(kind string) bool {
	i := sort.SearchStrings(sortedKinds, kind)
	return i < len(sortedKinds) && sortedKinds[i] == kind
}

var sortedKinds = func() []string {
	out := append([]string(nil), Kinds...)
	sort.Strings(out)
	return out
}()

// Encode renders the single line appended to events.jsonl (trailing newline).
func Encode(e Event) ([]byte, error) {
	e.At = e.At.UTC()
	line, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(line, '\n'), nil
}
