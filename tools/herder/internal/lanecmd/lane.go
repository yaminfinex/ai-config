// Package lanecmd is `herder lane …`: create, rename, lead, move into and link
// lanes. Every verb appends agent-store events (lane.* and assign --lane);
// nothing here gates a lifecycle action. Any caller may run any verb.
package lanecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/eventcmd"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herderstate"
)

const exitUnavailable = 3

type dependencies struct {
	roster func() ([]hcomidentity.Row, error)
	now    func() time.Time
}

var liveDependencies = dependencies{roster: hcomidentity.List, now: time.Now}

func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, liveDependencies)
}

// options are the flags every verb accepts; each verb checks its own.
type options struct {
	lead, by, byKind string
	remove, all      bool
	asJSON           bool
	positional       []string
}

func parse(args []string) (options, error) {
	var o options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") {
			o.positional = append(o.positional, arg)
			continue
		}
		takes := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("--%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "lead":
			o.lead, err = takes()
		case "by":
			o.by, err = takes()
		case "by-kind":
			o.byKind, err = takes()
		case "remove":
			o.remove = true
		case "all":
			o.all = true
		case "json":
			o.asJSON = true
		default:
			return o, fmt.Errorf("unknown flag %q", arg)
		}
		if err != nil {
			return o, err
		}
	}
	return o, nil
}

// session is one verb's view of the store: the projection, the live roster,
// and which lanes have a live member.
type session struct {
	store   *agentstore.Store
	proj    *agentstore.Projection
	roster  []hcomidentity.Row
	members map[string][]string
	opts    options
	now     time.Time
	stdout  io.Writer
	stderr  io.Writer
}

// refusal is a usage or rule error (exit 2); notFound is exit 1.
type refusal struct{ error }
type notFound struct{ error }

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	verb := args[0]
	if verb == "-h" || verb == "--help" || verb == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	handler, ok := verbs[verb]
	if !ok {
		fmt.Fprintf(stderr, "herder lane: unknown verb %q\n%s", verb, usage)
		return 2
	}
	opts, err := parse(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "herder lane %s: %v\n", verb, err)
		return 2
	}
	stateDir, err := herderstate.Dir()
	if err != nil {
		fmt.Fprintf(stderr, "herder lane %s: store unavailable: %v\n", verb, err)
		return exitUnavailable
	}
	store := agentstore.Open(stateDir, stderr)
	if store.ImportErr != nil {
		fmt.Fprintf(stderr, "herder lane %s: store unavailable: %v\n", verb, store.ImportErr)
		return exitUnavailable
	}
	proj, err := store.LoadNoSnapshot()
	if err != nil {
		fmt.Fprintf(stderr, "herder lane %s: store unavailable: %v\n", verb, err)
		return exitUnavailable
	}
	roster, err := deps.roster()
	if err != nil {
		// Without hcom no lane has a known live member: names resolve among
		// every lane and list shows nothing unless --all.
		fmt.Fprintf(stderr, "herder lane %s: live hcom roster unavailable (%v); lanes have no known live members\n", verb, err)
		roster = nil
	}
	s := &session{store: store, proj: proj, roster: roster, members: proj.LaneMembers(roster), opts: opts, now: deps.now().UTC(), stdout: stdout, stderr: stderr}
	if err := handler(s); err != nil {
		fmt.Fprintf(stderr, "herder lane %s: %v\n", verb, err)
		var nf notFound
		var rf refusal
		switch {
		case errors.As(err, &nf):
			return 1
		case errors.As(err, &rf):
			return 2
		case errors.Is(err, agentstore.ErrUnavailable):
			return exitUnavailable
		}
		return 2
	}
	return 0
}

var verbs = map[string]func(*session) error{
	"new":    (*session).newLane,
	"rename": (*session).rename,
	"lead":   (*session).setLead,
	"move":   (*session).move,
	"link":   func(s *session) error { return s.link(false) },
	"unlink": func(s *session) error { return s.link(true) },
	"repo":   (*session).repo,
	"list":   (*session).list,
	"show":   (*session).show,
}

func (s *session) want(n int, shape string) error {
	if len(s.opts.positional) != n {
		return refusal{fmt.Errorf("usage: herder lane %s", shape)}
	}
	return nil
}

// resolve finds a lane by id or by current name (case-insensitive). A name
// shared by several lanes picks the one with a live member; still ambiguous
// asks for an id.
func (s *session) resolve(ref string) (*agentstore.LaneView, error) {
	if lane := s.proj.Lanes[ref]; lane != nil {
		return lane, nil
	}
	var matches, live []*agentstore.LaneView
	for _, lane := range s.proj.Lanes {
		if strings.EqualFold(lane.Name, ref) {
			matches = append(matches, lane)
			if len(s.members[lane.ID]) > 0 {
				live = append(live, lane)
			}
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(live) == 1:
		return live[0], nil
	case len(matches) == 0:
		return nil, notFound{fmt.Errorf("no lane %q", ref)}
	}
	var ids []string
	for _, lane := range matches {
		ids = append(ids, lane.ID)
	}
	sort.Strings(ids)
	return nil, refusal{fmt.Errorf("lane name %q is ambiguous; use an id: %s", ref, strings.Join(ids, ", "))}
}

// nameTaken refuses a name already held by another lane with a live member.
func (s *session) nameTaken(name, except string) error {
	for _, lane := range s.proj.Lanes {
		if lane.ID != except && strings.EqualFold(lane.Name, name) && len(s.members[lane.ID]) > 0 {
			return refusal{fmt.Errorf("lane name %q is taken by %s", name, lane.ID)}
		}
	}
	return nil
}

// seatView is the seat's current record: its live roster incarnation, else
// its latest record.
func (s *session) seatView(seat string) *agentstore.AgentView {
	for i := range s.roster {
		if s.roster[i].Name == seat {
			return s.proj.ViewForRoster(&s.roster[i], s.roster)
		}
	}
	return s.proj.View(seat, nil)
}

// leaving refuses to move seat out of a lane it leads: a lane never loses its
// lead to a move. Setting another lead first frees the seat.
func (s *session) leaving(seat, target string) error {
	view := s.seatView(seat)
	if view == nil || view.LaneID == "" || view.LaneID == target {
		return nil
	}
	if from := s.proj.Lanes[view.LaneID]; from != nil && from.Lead == seat {
		return refusal{fmt.Errorf("%s leads lane %s (%s); set another lead first: herder lane lead %s <seat>", seat, from.Name, from.ID, from.Name)}
	}
	return nil
}

func (s *session) inLane(seat, lane string) bool {
	view := s.seatView(seat)
	return view != nil && view.LaneID == lane
}

func (s *session) event(kind string, fill func(*agentstore.Event)) agentstore.Event {
	by, byKind := s.opts.by, s.opts.byKind
	if by == "" {
		by, byKind = eventcmd.DefaultBy()
	}
	if byKind == "" {
		byKind = "agent"
	}
	e := agentstore.Event{ID: agentstore.NewID(s.now), At: s.now, Kind: kind, By: by, ByKind: byKind}
	fill(&e)
	return e
}

func (s *session) append(events ...agentstore.Event) error {
	for _, e := range events {
		if err := e.Validate(); err != nil {
			return refusal{err}
		}
	}
	for _, e := range events {
		if _, err := s.store.Append(e); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) assign(seat, lane string) agentstore.Event {
	return s.event(agentstore.KindAssign, func(e *agentstore.Event) { e.Name, e.Lane = seat, lane })
}

func (s *session) newLane() error {
	if err := s.want(1, "new <name> --lead <seat>"); err != nil {
		return err
	}
	name, lead := s.opts.positional[0], strings.TrimSpace(s.opts.lead)
	if lead == "" {
		return refusal{errors.New("--lead <seat> is required")}
	}
	if err := s.nameTaken(name, ""); err != nil {
		return err
	}
	created := s.event(agentstore.KindLaneCreated, func(e *agentstore.Event) { e.LaneName, e.Lead = name, lead })
	if err := s.leaving(lead, created.ID); err != nil {
		return err
	}
	if err := s.append(created, s.assign(lead, created.ID)); err != nil {
		return err
	}
	fmt.Fprintf(s.stdout, "lane=%s\n", created.ID)
	return nil
}

func (s *session) rename() error {
	if err := s.want(2, "rename <lane> <name>"); err != nil {
		return err
	}
	lane, err := s.resolve(s.opts.positional[0])
	if err != nil {
		return err
	}
	name := s.opts.positional[1]
	if err := s.nameTaken(name, lane.ID); err != nil {
		return err
	}
	return s.append(s.event(agentstore.KindLaneUpdated, func(e *agentstore.Event) { e.Lane, e.LaneName = lane.ID, name }))
}

func (s *session) setLead() error {
	if err := s.want(2, "lead <lane> <seat>"); err != nil {
		return err
	}
	lane, err := s.resolve(s.opts.positional[0])
	if err != nil {
		return err
	}
	seat := s.opts.positional[1]
	var events []agentstore.Event
	if !s.inLane(seat, lane.ID) {
		if err := s.leaving(seat, lane.ID); err != nil {
			return err
		}
		events = append(events, s.assign(seat, lane.ID))
	}
	events = append(events, s.event(agentstore.KindLaneUpdated, func(e *agentstore.Event) { e.Lane, e.Lead = lane.ID, seat }))
	return s.append(events...)
}

func (s *session) move() error {
	if err := s.want(2, "move <seat> <lane>"); err != nil {
		return err
	}
	seat := s.opts.positional[0]
	lane, err := s.resolve(s.opts.positional[1])
	if err != nil {
		return err
	}
	if err := s.leaving(seat, lane.ID); err != nil {
		return err
	}
	return s.append(s.assign(seat, lane.ID))
}

func (s *session) link(remove bool) error {
	verb := "link"
	if remove {
		verb = "unlink"
	}
	if err := s.want(2, verb+" <lane> <owner/repo#n>"); err != nil {
		return err
	}
	lane, err := s.resolve(s.opts.positional[0])
	if err != nil {
		return err
	}
	pr := s.opts.positional[1]
	return s.append(s.event(agentstore.KindLaneLink, func(e *agentstore.Event) { e.Lane, e.PR, e.Remove = lane.ID, pr, remove }))
}

func (s *session) repo() error {
	if err := s.want(2, "repo <lane> <owner/repo> [--remove]"); err != nil {
		return err
	}
	lane, err := s.resolve(s.opts.positional[0])
	if err != nil {
		return err
	}
	repo := s.opts.positional[1]
	return s.append(s.event(agentstore.KindLaneLink, func(e *agentstore.Event) { e.Lane, e.Repo, e.Remove = lane.ID, repo, s.opts.remove }))
}

// laneOutput is the --json shape of list and show.
type laneOutput struct {
	*agentstore.LaneView
	Members []string `json:"members"`
}

func (s *session) output(lane *agentstore.LaneView) laneOutput {
	members := append([]string{}, s.members[lane.ID]...)
	return laneOutput{LaneView: lane, Members: members}
}

func (s *session) list() error {
	if err := s.want(0, "list [--all] [--json]"); err != nil {
		return err
	}
	var lanes []laneOutput
	for _, lane := range s.proj.Lanes {
		if s.opts.all || len(s.members[lane.ID]) > 0 {
			lanes = append(lanes, s.output(lane))
		}
	}
	sort.Slice(lanes, func(i, j int) bool {
		if a, b := strings.ToLower(lanes[i].Name), strings.ToLower(lanes[j].Name); a != b {
			return a < b
		}
		return lanes[i].ID < lanes[j].ID
	})
	if s.opts.asJSON {
		if lanes == nil {
			lanes = []laneOutput{}
		}
		return s.writeJSON(lanes)
	}
	if len(lanes) == 0 {
		return nil
	}
	w := tabwriter.NewWriter(s.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "LANE\tLEAD\tMEMBERS\tID")
	for _, lane := range lanes {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", lane.Name, lane.Lead, len(lane.Members), lane.ID)
	}
	return w.Flush()
}

func (s *session) show() error {
	if err := s.want(1, "show <lane> [--json]"); err != nil {
		return err
	}
	lane, err := s.resolve(s.opts.positional[0])
	if err != nil {
		return err
	}
	out := s.output(lane)
	if s.opts.asJSON {
		return s.writeJSON(out)
	}
	w := tabwriter.NewWriter(s.stdout, 0, 4, 2, ' ', 0)
	field := func(label, value string) {
		if value == "" {
			value = "-"
		}
		fmt.Fprintf(w, "%s\t%s\n", label, value)
	}
	field("lane", lane.Name)
	field("id", lane.ID)
	field("earlier_names", strings.Join(lane.Names, ", "))
	field("lead", lane.Lead)
	field("members", strings.Join(out.Members, ", "))
	field("prs", strings.Join(lane.PRs, ", "))
	field("repos", strings.Join(lane.Repos, ", "))
	field("created", lane.Created.UTC().Format(time.RFC3339)+" by "+lane.By)
	return w.Flush()
}

func (s *session) writeJSON(value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(s.stdout, "%s\n", encoded)
	return nil
}

const usage = `herder lane — lanes: a named line of work with one lead and its member seats.

Usage:
  herder lane new <name> --lead <seat>          create a lane; the lead joins it
  herder lane rename <lane> <name>              earlier names stay in the record
  herder lane lead <lane> <seat>                set the lead (moves the seat in)
  herder lane move <seat> <lane>                move a seat into a lane
  herder lane link <lane> <owner/repo#n>        link a pull request
  herder lane unlink <lane> <owner/repo#n>
  herder lane repo <lane> <owner/repo> [--remove]
  herder lane list [--all] [--json]             lanes with a live member (--all: every lane)
  herder lane show <lane> [--json]

<lane> is a lane id or a name (case-insensitive; a name shared by several
lanes picks the one with a live member). A lane name is unique among lanes
with a live member. move and lead refuse to take a lane's lead out of it;
set another lead first. Writing verbs accept --by WHO and --by-kind KIND
(default as herder assign). There is no close verb: a lane with no live
member drops out of list and the board, and its record stays.

Exit 0 done, 1 lane not found, 2 usage or refused, 3 store unavailable.
`
