package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Bus wraps the hcom CLI — the proven interface. Message text canonically
// lives in hcom's own store; mc never touches hcom.db directly.

type Bus struct {
	Hcom string // path to the real hcom binary (NOT herder's capture shim)
	Dir  string // HCOM_DIR override; empty = live bus
}

type BusEvent struct {
	ID       int64  `json:"id"`
	Instance string `json:"instance"`
	TS       string `json:"ts"`
	Type     string `json:"type"`
	Data     struct {
		From     string   `json:"from"`
		Text     string   `json:"text"`
		Thread   string   `json:"thread"`
		ReplyTo  int64    `json:"reply_to_local"`
		Intent   string   `json:"intent"`
		Mentions []string `json:"mentions"`
	} `json:"data"`
}

func (b *Bus) run(args ...string) ([]byte, error) {
	cmd := exec.Command(b.Hcom, args...)
	// Scrub inherited HCOM_*/HERDER_* session vars: if mc is launched from
	// inside an agent session, that session's bus identity must not leak
	// into the seat's hcom calls.
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HCOM") || strings.HasPrefix(kv, "HERDER") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	if b.Dir != "" {
		cmd.Env = append(cmd.Env, "HCOM_DIR="+b.Dir)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("hcom %s: %s", args[0], firstLine(errb.String()))
	}
	return out.Bytes(), nil
}

func firstLine(s string) string {
	if i := bytes.IndexByte([]byte(s), '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Send posts to the live rail as an ext sender (--from). Unknown @mentions
// make hcom fail hard — that error is surfaced, not swallowed: it is the
// refusal semantic.
func (b *Bus) Send(from string, to []string, thread, replyTo, intent, text string) error {
	args := []string{"send"}
	for _, t := range to {
		args = append(args, "@"+t)
	}
	if thread != "" {
		args = append(args, "--thread", thread)
	}
	if replyTo != "" {
		args = append(args, "--reply-to", replyTo)
	}
	if intent != "" {
		args = append(args, "--intent", intent)
	}
	args = append(args, "--from", from, "--", text)
	_, err := b.run(args...)
	return err
}

// EventsSince drains all bus events with id > cursor, ascending (NDJSON).
// limit is the forward page size, not a maximum total result count.
// NOTE: this output shape omits the mentions field — hcom only includes it
// when a --mention filter is present. Use MentionsSince for raise detection.
func (b *Bus) EventsSince(cursor int64, limit int) ([]BusEvent, error) {
	return b.eventsSince(cursor, 0, limit, nil, "")
}

// MentionsSince drains all events since cursor that @mention any of names
// (same flag repeated = OR in hcom), with the mentions field populated.
// limit is the forward page size, not a maximum total result count.
func (b *Bus) MentionsSince(cursor int64, limit int, names ...string) ([]BusEvent, error) {
	return b.mentionsSince(cursor, 0, limit, names...)
}

// LatestEventID returns the current bus head without draining history.
func (b *Bus) LatestEventID() (int64, error) {
	out, err := b.run("events", "--last", "1")
	if err != nil {
		return 0, err
	}
	evs := parseBusEvents(out)
	if len(evs) == 0 && len(bytes.TrimSpace(out)) > 0 {
		return 0, fmt.Errorf("unparseable bus head: %.80q", out)
	}
	if len(evs) == 0 {
		return 0, nil
	}
	return evs[0].ID, nil
}

// MentionsThrough is MentionsSince bounded to an already captured generic
// event head. That keeps mention enrichment from moving an ingest cursor past
// traffic created between the two queries.
func (b *Bus) MentionsThrough(cursor, head int64, limit int, names ...string) ([]BusEvent, error) {
	return b.mentionsSince(cursor, head, limit, names...)
}

func (b *Bus) mentionsSince(cursor, head int64, limit int, names ...string) ([]BusEvent, error) {
	var args []string
	var predicates []string
	for _, n := range names {
		args = append(args, "--mention", n)
		predicates = append(predicates, fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(msg_mentions) WHERE value='%s')", sqlQuote(n)))
	}
	return b.eventsSince(cursor, head, limit, args, strings.Join(predicates, " OR "))
}

// eventsSince drains oldest-first pages. hcom's normal --last limit is applied
// newest-first, so using it with id > cursor can jump the cursor over an
// arbitrarily large middle of the backlog. The membership subquery chooses the
// oldest matching IDs even though hcom renders each page newest-first.
func (b *Bus) eventsSince(cursor, head int64, limit int, filterArgs []string, predicate string) ([]BusEvent, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("event page limit must be positive")
	}

	var all []BusEvent
	next := cursor
	for {
		where := fmt.Sprintf("id IN (SELECT id FROM events_v WHERE id > %d", next)
		if head > 0 {
			where += fmt.Sprintf(" AND id <= %d", head)
		}
		if predicate != "" {
			where += " AND (" + predicate + ")"
		}
		where += fmt.Sprintf(" ORDER BY id ASC LIMIT %d)", limit)

		args := append([]string{"events"}, filterArgs...)
		// --last controls only the outer snapshot size here. Page membership is
		// already restricted to the oldest IDs by the subquery, so it cannot
		// turn this into a newest-first tail grab.
		args = append(args, "--sql", where, "--last", fmt.Sprint(limit))
		page, err := b.query(args...)
		if err != nil {
			return nil, err
		}
		sort.Slice(page, func(i, j int) bool { return page[i].ID < page[j].ID })
		all = append(all, page...)
		if len(page) < limit {
			return all, nil
		}
		last := page[len(page)-1].ID
		if last <= next {
			return nil, fmt.Errorf("event page made no progress past cursor %d", next)
		}
		next = last
	}
}

func sqlQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (b *Bus) query(args ...string) ([]BusEvent, error) {
	out, err := b.run(args...)
	if err != nil {
		return nil, err
	}
	return parseBusEvents(out), nil
}

func parseBusEvents(out []byte) []BusEvent {
	var evs []BusEvent
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e BusEvent
		if json.Unmarshal(line, &e) == nil && e.ID > 0 {
			evs = append(evs, e)
		}
	}
	return evs
}

type BusAgent struct {
	Name      string  `json:"name"`
	BaseName  string  `json:"base_name"`
	SessionID string  `json:"session_id"`
	Status    string  `json:"status"`
	StatusCtx string  `json:"status_context"`
	Directory string  `json:"directory"`
	Tool      string  `json:"tool"`
	Unread    float64 `json:"unread_count"`
	AgeSec    float64 `json:"status_age_seconds"`
}

func (b *Bus) List() ([]BusAgent, error) {
	out, err := b.run("list", "--json")
	if err != nil {
		return nil, err
	}
	var agents []BusAgent
	if err := json.Unmarshal(out, &agents); err != nil {
		return nil, fmt.Errorf("parse hcom list: %w", err)
	}
	return agents, nil
}

// EnsureSeat registers (or reclaims) the addressable seat identity. Without
// a registered seat, agent sends to @<seat> fail hard. Reclaim is cwd-bound
// in hcom; if it refuses but the seat already exists, that's fine — the
// keepalive listen loop (cwd-agnostic) revives its status.
func (b *Bus) EnsureSeat(seat string) error {
	_, startErr := b.run("start", "--as", seat)
	if startErr == nil {
		return nil
	}
	agents, err := b.List()
	if err != nil {
		return startErr
	}
	for _, a := range agents {
		if a.Name == seat || a.BaseName == seat {
			return nil
		}
	}
	return startErr
}

// SeatKeepalive blocks in `hcom listen` loops under the seat identity. This
// both drains the seat's delivery queue and keeps its status from decaying
// to stopped (idle registered names get stale-swept, and sends to stopped
// agents fail) — the loop is load-bearing, not cosmetic.
func (b *Bus) SeatKeepalive(seat string, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		if _, err := b.run("listen", "50", "--name", seat); err != nil {
			log.Printf("seat keepalive: %v", err)
			time.Sleep(10 * time.Second)
		}
	}
}
