package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// The desk (W1): the return screen, three zones in strict ratified order.
// Zone 1 derives from the asks board's full symmetry (owner-outbound
// entities), zone 2 from inbound decide/reply items sorted blocked-first
// and grouped by anchor, zone 3 from per-mission plan strips rendering the
// crew-authored milestone narrative verbatim — mc never invents rollup
// text. Health signals live in the SYSTEM panel only.

// Spinning metric constants (tuned in-unit, not per-mission knobs): a
// mission spins when, since its last observed board transition, at least
// spinEventFloor attributed bus messages arrived while board transitions
// stayed zero, over a window of at least spinWindowFloor.
const (
	spinEventFloor  = 50
	spinWindowFloor = 30 * time.Minute
)

type deskModel struct {
	Loops        []openLoop
	Answered     int
	WaitingCount int

	Groups       []deskGroup
	Zone2Count   int
	Zone2Blocked int
	Degraded     []deskDegraded

	Strips []missionStrip

	UnanchoredAsks int
	DegradedBoards []string

	BoardStamp timestamp
	BoardWarn  string
}

// openLoop is a zone-1 item: one of the owner's own outbound asks, with
// the counterparty's latest reply surfaced in place when one exists.
type openLoop struct {
	Ask         askEntity
	Mission     string
	Answer      *askReply
	AnswerStamp timestamp
	Asked       timestamp
}

// deskItem is a zone-2 row: an inbound ask entity, or (transitional
// transport) a managed wire raise still travelling as a thread.
type deskItem struct {
	Ask     *askEntity
	Mission string
	Thread  *Thread
	Stamp   timestamp
	created time.Time
}

func (i deskItem) Blocked() *askBlocking {
	if i.Ask != nil {
		return i.Ask.Blocking
	}
	return nil // the blocking field in the raise wire shape is a tooling-lane remainder
}

func (i deskItem) Expects() string {
	if i.Ask != nil {
		return i.Ask.Expects
	}
	return i.Thread.Expects
}

func (i deskItem) Title() string {
	if i.Ask != nil {
		if i.Ask.Framing.Question != "" {
			return i.Ask.Framing.Question
		}
		return "MALFORMED ASK — question missing"
	}
	return i.Thread.Title
}

func (i deskItem) From() string {
	if i.Ask != nil {
		return i.Ask.Asker
	}
	return i.Thread.OpenedBy
}

func (i deskItem) URL() string {
	if i.Ask != nil {
		return "/ask/" + url.PathEscape(i.Ask.ID)
	}
	return "/?peek=" + url.QueryEscape(i.Thread.ID) + "#rail-" + url.PathEscape(i.Thread.ID)
}

func (i deskItem) ThreadID() string {
	if i.Thread != nil {
		return i.Thread.ID
	}
	return ""
}

type deskGroup struct {
	Mission string
	Anchor  askReference
	Context string // crew-authored anchor context (the anchored task's board title)
	Items   []deskItem
	Traces  []deskTrace
	blocked int
	latest  time.Time
}

func (g deskGroup) HasBlocked() bool { return g.blocked > 0 }

// FragmentID is the /#z2-<group> anchor per the URL scheme.
func (g deskGroup) FragmentID() string {
	raw := g.Anchor.Type + "-" + g.Anchor.Ref
	if g.Mission != "" {
		raw = g.Mission + "-" + raw
	}
	var b strings.Builder
	b.WriteString("z2-")
	for _, r := range strings.ToLower(raw) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (g deskGroup) AnchorLabel() string {
	label := g.Anchor.Type
	if g.Anchor.Ref != "" {
		label += " " + g.Anchor.Ref
	}
	if g.Mission != "" && g.Anchor.Type != "mission" {
		label += " · " + g.Mission
	}
	return label
}

// deskTrace is a co-custodian clearance line rendered in the group the
// entity cleared from (D4.6/A2); the citation always renders.
type deskTrace struct {
	Actor    string
	Action   string
	At       string
	Citation *askReference
	AskID    string
}

// deskDegraded is a targeted-but-shapeless item: visible at the zone
// tail, uncounted, never the owner's turn.
type deskDegraded struct {
	From  string
	Title string
	URL   string
	Stamp timestamp
}

// missionStrip is one zone-3 plan strip. Narrative fields carry
// crew-authored board text verbatim (milestone titles, task titles,
// ask questions) — the strip-text ⊆ board-content proof test keys on
// NarrativeText.
type missionStrip struct {
	Slug    string
	OK      bool
	Warning string

	NeedsYou   int
	Blocked    int
	ReviewDebt int
	MadeForYou []string

	NoNarrative     bool
	Phase           string
	MilestonesDone  int
	MilestonesTotal int
	Done            []string
	Now             string
	NowDone         int
	NowTotal        int
	NowTasks        []stripTask
	Next            []string

	Spin    *spinEvidence
	Fetched timestamp

	lastMove time.Time
}

type stripTask struct{ Title, Status string }

type spinEvidence struct {
	Events int
	Since  timestamp
}

// NarrativeText returns every crew-authored string the strip renders,
// for the strip-text ⊆ board-content proof.
func (s missionStrip) NarrativeText() []string {
	var out []string
	if s.Phase != "" {
		out = append(out, s.Phase)
	}
	out = append(out, s.Done...)
	if s.Now != "" {
		out = append(out, s.Now)
	}
	for _, t := range s.NowTasks {
		out = append(out, t.Title, t.Status)
	}
	out = append(out, s.Next...)
	out = append(out, s.MadeForYou...)
	return out
}

// boardMotion observes board snapshots over the mish read surface and
// records when a transition was last seen. mish exposes no board-change
// timestamp, so this is mc's honest derivation: the window start it
// yields is always rendered in the spinning evidence, and before any
// transition is observed the window opens at first observation.
type boardMotion struct {
	mu   sync.Mutex
	seen map[string]*motionState
}

type motionState struct {
	fingerprint string
	lastMove    time.Time
	since       time.Time
}

func newBoardMotion() *boardMotion {
	return &boardMotion{seen: map[string]*motionState{}}
}

// observe folds one board snapshot and returns the spinning window start
// (last observed transition, or observation start) plus the last observed
// move for the most-moved tiebreak (zero when none was ever observed).
func (bm *boardMotion) observe(slug, fingerprint string, now time.Time) (windowStart, lastMove time.Time) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	st, ok := bm.seen[slug]
	if !ok {
		st = &motionState{fingerprint: fingerprint, since: now}
		bm.seen[slug] = st
	} else if st.fingerprint != fingerprint {
		st.fingerprint = fingerprint
		st.lastMove = now
	}
	if st.lastMove.IsZero() {
		return st.since, time.Time{}
	}
	return st.lastMove, st.lastMove
}

func boardFingerprint(status missionStatus, milestones []missionMilestone) string {
	var b strings.Builder
	for _, t := range status.Board.Tasks {
		b.WriteString(t.ID)
		b.WriteByte('|')
		b.WriteString(t.Status)
		b.WriteByte('\n')
	}
	for _, m := range milestones {
		if m.Completed {
			b.WriteByte('c')
		}
		fmt.Fprintf(&b, "%s|%s|%d/%d\n", m.ID, m.Title, m.Done, m.Total)
	}
	return b.String()
}

func yourTurnExpects(expects string) bool { return expects == "decide" || expects == "reply" }

// buildDesk assembles the whole return screen from the mish surface and
// the thread store. It is the one place zone membership is decided.
func (w *Web) buildDesk() *deskModel {
	model := &deskModel{}
	now := w.now()

	var statuses []missionStatus
	if w.missions != nil {
		statuses, model.BoardWarn = w.missions.AllStatuses()
	}
	if len(statuses) > 0 {
		model.BoardStamp = formatTimestamp(statuses[0].FetchedAt, now)
	}

	groups := map[string]*deskGroup{}
	var groupOrder []string
	type pendingTrace struct {
		key   string
		trace deskTrace
	}
	var pendingTraces []pendingTrace
	addItem := func(mission string, anchor askReference, ctx string, item deskItem) {
		key := mission + "\x00" + anchor.Type + "\x00" + anchor.Ref
		g, ok := groups[key]
		if !ok {
			g = &deskGroup{Mission: mission, Anchor: anchor, Context: ctx}
			groups[key] = g
			groupOrder = append(groupOrder, key)
		}
		g.Items = append(g.Items, item)
		if item.Blocked() != nil {
			g.blocked++
		}
		if item.created.After(g.latest) {
			g.latest = item.created
		}
	}

	type stripCounts struct {
		needsYou, blocked, reviewDebt int
		madeForYou                    []string
	}
	counts := map[string]*stripCounts{}
	countFor := func(slug string) *stripCounts {
		c, ok := counts[slug]
		if !ok {
			c = &stripCounts{}
			counts[slug] = c
		}
		return c
	}

	for si := range statuses {
		st := statuses[si]
		owner := st.Manifest.Owner
		taskTitles := map[string]string{}
		for _, t := range st.Board.Tasks {
			taskTitles[strings.ToUpper(t.ID)] = t.Title
		}
		for ei := range st.Asks.Entities {
			e := st.Asks.Entities[ei]
			e.Mission = st.Slug
			created := parseTimestamp(e.CreatedAt)
			if e.State == "open" && e.Anchor.Type == "" && e.Anchor.Ref == "" {
				model.UnanchoredAsks++
			}
			if e.Kind == "ruling" {
				// Made-for-you decisions never queue in zone 2 (D2.5); until the
				// W5 acknowledge verbs exist every open agent-authored ruling is
				// honestly unreviewed review debt.
				if e.State == "open" && e.Asker != owner {
					c := countFor(st.Slug)
					c.reviewDebt++
					if e.Framing.Question != "" {
						c.madeForYou = append(c.madeForYou, e.Framing.Question)
					}
				}
				continue
			}
			if e.Asker == owner {
				if e.State == "open" {
					loop := openLoop{Ask: e, Mission: st.Slug, Asked: formatTimestamp(created, now)}
					for ri := len(e.Replies) - 1; ri >= 0; ri-- {
						if e.Replies[ri].Actor != owner {
							reply := e.Replies[ri]
							loop.Answer = &reply
							loop.AnswerStamp = formatTimestamp(parseTimestamp(reply.At), now)
							break
						}
					}
					model.Loops = append(model.Loops, loop)
				}
				continue
			}
			if e.State != "open" {
				// Co-custodian clearances stay visible as one-line traces in the
				// group they cleared from, citation included (D4.6, A2); they
				// attach after every open item has formed its group.
				for _, tr := range e.Traces {
					if tr.Action != "withdraw" && tr.Action != "close" {
						continue
					}
					pendingTraces = append(pendingTraces, pendingTrace{
						key:   st.Slug + "\x00" + e.Anchor.Type + "\x00" + e.Anchor.Ref,
						trace: deskTrace{Actor: tr.Actor, Action: tr.Action, At: tr.At, Citation: tr.Citation, AskID: e.ID},
					})
				}
				continue
			}
			entity := e
			item := deskItem{Ask: &entity, Mission: st.Slug, Stamp: formatTimestamp(created, now), created: created}
			switch {
			case yourTurnExpects(e.Expects):
				addItem(st.Slug, e.Anchor, taskTitles[strings.ToUpper(e.Anchor.Ref)], item)
				c := countFor(st.Slug)
				c.needsYou++
				if e.Blocking != nil {
					c.blocked++
				}
			case e.Expects == "act" || e.Expects == "read":
				// FYI tier: visible on boards and browse surfaces, never zone 2.
			default:
				model.Degraded = append(model.Degraded, deskDegraded{
					From: e.Asker, Title: item.Title(), URL: item.URL(), Stamp: item.Stamp,
				})
			}
		}
	}

	// Transitional transport: wire raises still travel as managed threads;
	// a decide/reply raise is a zone-2 item anchored at its mission (or the
	// node bucket). FYI-tier raises live on the browse surfaces (D2.1).
	for _, t := range w.store.List("open", "managed") {
		if t.Turn != "owner" || !yourTurnExpects(t.Expects) {
			continue
		}
		anchor := askReference{Type: "node"}
		if t.Home != "" {
			anchor = askReference{Type: "mission", Ref: t.Home}
		}
		item := deskItem{Thread: t, Mission: t.Home, Stamp: formatTimestamp(t.Updated, now), created: t.Created}
		addItem(t.Home, anchor, "", item)
		if t.Home != "" {
			countFor(t.Home).needsYou++
		}
	}

	for _, pending := range pendingTraces {
		if g, ok := groups[pending.key]; ok {
			g.Traces = append(g.Traces, pending.trace)
		}
	}

	sort.SliceStable(model.Loops, func(i, j int) bool {
		a, b := model.Loops[i], model.Loops[j]
		if (a.Answer != nil) != (b.Answer != nil) {
			return a.Answer != nil
		}
		return parseTimestamp(a.Ask.CreatedAt).Before(parseTimestamp(b.Ask.CreatedAt))
	})
	for _, loop := range model.Loops {
		if loop.Answer != nil {
			model.Answered++
		} else {
			model.WaitingCount++
		}
	}

	for _, key := range groupOrder {
		g := groups[key]
		sort.SliceStable(g.Items, func(i, j int) bool {
			a, b := g.Items[i], g.Items[j]
			if (a.Blocked() != nil) != (b.Blocked() != nil) {
				return a.Blocked() != nil
			}
			return a.created.Before(b.created)
		})
		model.Zone2Count += len(g.Items)
		model.Zone2Blocked += g.blocked
		model.Groups = append(model.Groups, *g)
	}
	sort.SliceStable(model.Groups, func(i, j int) bool {
		a, b := model.Groups[i], model.Groups[j]
		if (a.blocked > 0) != (b.blocked > 0) {
			return a.blocked > 0
		}
		return a.latest.After(b.latest)
	})

	for _, st := range statuses {
		strip := missionStrip{Slug: st.Slug, OK: st.OK && st.Board.Available, Fetched: formatTimestamp(st.FetchedAt, now)}
		if c := counts[st.Slug]; c != nil {
			strip.NeedsYou, strip.Blocked, strip.ReviewDebt, strip.MadeForYou = c.needsYou, c.blocked, c.reviewDebt, c.madeForYou
		}
		if !strip.OK {
			strip.Warning = st.CardWarning()
			model.DegradedBoards = append(model.DegradedBoards, st.Slug)
			model.Strips = append(model.Strips, strip)
			continue
		}
		if len(st.Warnings) > 0 {
			model.DegradedBoards = append(model.DegradedBoards, st.Slug)
		}
		var milestones []missionMilestone
		if w.missions != nil {
			milestones = w.missions.Milestones(st.Slug)
		}
		stripNarrativeFromBoard(&strip, st, milestones)
		if w.motion != nil {
			windowStart, lastMove := w.motion.observe(st.Slug, boardFingerprint(st, milestones), now)
			strip.lastMove = lastMove
			events := w.missionBusEvents(st.Slug, windowStart)
			if events >= spinEventFloor && now.Sub(windowStart) >= spinWindowFloor {
				strip.Spin = &spinEvidence{Events: events, Since: formatTimestamp(windowStart, now)}
			}
		}
		model.Strips = append(model.Strips, strip)
	}

	sortStrips(model.Strips)

	return model
}

// sortStrips implements the settled strip sort keys (§2 job 1):
// most-needs-me descending on the tuple (blocked decide/reply, all open
// decide/reply, review debt — A7: a mission owing a review cannot sink),
// then most-moved (recency of the last observed board transition).
func sortStrips(strips []missionStrip) {
	sort.SliceStable(strips, func(i, j int) bool {
		a, b := strips[i], strips[j]
		if a.Blocked != b.Blocked {
			return a.Blocked > b.Blocked
		}
		if a.NeedsYou != b.NeedsYou {
			return a.NeedsYou > b.NeedsYou
		}
		if a.ReviewDebt != b.ReviewDebt {
			return a.ReviewDebt > b.ReviewDebt
		}
		return a.lastMove.After(b.lastMove)
	})
}

// stripNarrativeFromBoard fills the strip with the crew-authored milestone
// grouping verbatim; a board without milestones renders degraded-honest.
func stripNarrativeFromBoard(strip *missionStrip, st missionStatus, milestones []missionMilestone) {
	if len(milestones) == 0 {
		strip.NoNarrative = true
		return
	}
	strip.MilestonesTotal = len(milestones)
	var active []missionMilestone
	for _, m := range milestones {
		if m.Completed {
			strip.MilestonesDone++
			strip.Done = append(strip.Done, m.Title)
		} else {
			active = append(active, m)
		}
	}
	if len(active) == 0 {
		strip.Phase = milestones[len(milestones)-1].Title
		return
	}
	strip.Phase = active[0].Title
	strip.Now = active[0].Title
	strip.NowDone, strip.NowTotal = active[0].Done, active[0].Total
	for _, m := range active[1:] {
		strip.Next = append(strip.Next, m.Title)
	}
	for _, t := range st.Board.Tasks {
		if strings.EqualFold(t.Status, "in progress") {
			strip.NowTasks = append(strip.NowTasks, stripTask{Title: t.Title, Status: t.Status})
			if len(strip.NowTasks) == 3 {
				break
			}
		}
	}
}

// missionBusEvents counts attributed bus activity: messages on threads
// homed at the mission since the window start. Attribution rides thread
// homes — traffic mc cannot attribute never inflates the evidence.
func (w *Web) missionBusEvents(slug string, since time.Time) int {
	if slug == "" {
		return 0
	}
	events := 0
	for _, t := range w.store.List("", "") {
		if t.Home != slug {
			continue
		}
		for _, m := range t.Msgs {
			if at := parseTimestamp(m.TS); !at.IsZero() && at.After(since) {
				events++
			}
		}
	}
	return events
}
