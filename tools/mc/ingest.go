package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// Ingestor folds bus traffic into thread state. Two sources of truth meet
// here: hcom holds the messages, the store holds the state the bus cannot
// (open/closed, expects, turn, context). Linking is keyed by bus event id,
// so ticks are idempotent and the web layer never writes messages locally —
// it sends on the bus and kicks a tick.

type Ingestor struct {
	store *Store
	bus   *Bus
	user  string // the human's from-name (e.g. human-yamen)
	seat  string // the addressable seat agents raise items to (e.g. owner)
	kick  chan struct{}
}

func NewIngestor(store *Store, bus *Bus, user, seat string) *Ingestor {
	return &Ingestor{store: store, bus: bus, user: user, seat: seat, kick: make(chan struct{}, 1)}
}

// Kick requests an immediate tick (used right after a web-initiated send).
func (in *Ingestor) Kick() {
	select {
	case in.kick <- struct{}{}:
	default:
	}
}

func (in *Ingestor) Run(stop <-chan struct{}) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		if err := in.Tick(); err != nil {
			log.Printf("ingest: %v", err)
		}
		select {
		case <-stop:
			return
		case <-t.C:
		case <-in.kick:
		}
	}
}

func (in *Ingestor) Tick() error {
	cursor := in.store.Cursor()
	evs, err := in.bus.EventsSince(cursor, 500)
	if err != nil {
		return err
	}
	// The generic query omits mentions; fetch raise events separately and
	// let their rows (which carry mentions) win the merge. Only the seat is
	// queried: hcom stamps an implicit @bigboss on every mention-free send,
	// so a bigboss mention is indistinguishable from plain chatter and must
	// never count as a raise.
	raises, err := in.bus.MentionsSince(cursor, 500, in.seat)
	if err != nil {
		return err
	}
	byID := map[int64]BusEvent{}
	var order []int64
	for _, ev := range evs {
		byID[ev.ID] = ev
		order = append(order, ev.ID)
	}
	for _, ev := range raises {
		if _, ok := byID[ev.ID]; !ok {
			order = append(order, ev.ID)
		}
		byID[ev.ID] = ev
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	var max int64
	for _, id := range order {
		ev := byID[id]
		if ev.ID > max {
			max = ev.ID
		}
		if ev.Type != "message" {
			continue
		}
		if err := in.fold(ev); err != nil {
			log.Printf("ingest fold #%d: %v", ev.ID, err)
		}
	}
	if max > 0 {
		return in.store.SetCursor(max)
	}
	return nil
}

func (in *Ingestor) fold(ev BusEvent) error {
	d := ev.Data
	tid := d.Thread
	raised := contains(d.Mentions, in.seat)

	var t *Thread
	if tid != "" {
		t = in.store.Get(tid)
	}

	if t == nil && !raised && tid == "" {
		return nil // mention-free, threadless chatter; nothing to track
	}

	// The grade the thread holds once this event folds in: an explicit raise
	// at the seat makes (or keeps) it managed; otherwise it is managed only
	// if it already was.
	managed := raised || (t != nil && t.Grade == "managed")

	expects := "read"
	if d.Intent == "request" {
		expects = "reply"
	}

	switch {
	case t == nil && raised:
		// A raise on an unknown thread auto-opens it managed: the agent's
		// message IS the cold-open context. This is the dogfood loop —
		// orchestrators put items on the desk by opening threads at the seat.
		if tid == "" {
			tid = fmt.Sprintf("desk-%d", ev.ID)
		}
		if err := in.store.Open(tid, titleFrom(d.Text), d.Text, expects, "moment", "", d.From, nil, "owner", "managed"); err != nil {
			return err
		}
	case t == nil:
		// A bus thread never raised at the seat: track it observed — visible
		// under All threads, never on the desk.
		if err := in.store.Open(tid, titleFrom(d.Text), d.Text, "", "", "", d.From, nil, "", "observed"); err != nil {
			return err
		}
	case t.Grade == "observed" && raised:
		if err := in.store.Promote(tid, expects); err != nil {
			return err
		}
	}

	if err := in.store.Link(tid, Msg{BusID: ev.ID, From: d.From, Text: d.Text, Intent: d.Intent, ReplyTo: d.ReplyTo, TS: ev.TS}); err != nil {
		return err
	}
	if !managed {
		return nil // observed threads carry no turn — they are never "your turn"
	}

	// Whose turn: the human spoke → theirs; anyone else spoke → owner's.
	if d.From == in.user {
		t := in.store.Get(tid)
		turn := "them"
		if t != nil && len(t.With) > 0 {
			turn = strings.Join(t.With, ",")
		}
		return in.store.SetTurn(tid, turn)
	}
	return in.store.SetTurn(tid, "owner")
}

func titleFrom(text string) string {
	if i := strings.IndexAny(text, ".!?\n"); i > 0 && i < 80 {
		return text[:i]
	}
	if len(text) > 80 {
		return text[:80]
	}
	return text
}
