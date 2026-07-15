package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
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

// EventsSince returns bus events with id > cursor, ascending (NDJSON).
// NOTE: this output shape omits the mentions field — hcom only includes it
// when a --mention filter is present. Use MentionsSince for raise detection.
func (b *Bus) EventsSince(cursor int64, limit int) ([]BusEvent, error) {
	return b.query("events", "--sql", fmt.Sprintf("id > %d", cursor), "--last", fmt.Sprint(limit))
}

// MentionsSince returns events since cursor that @mention any of names
// (same flag repeated = OR in hcom), with the mentions field populated.
func (b *Bus) MentionsSince(cursor int64, limit int, names ...string) ([]BusEvent, error) {
	args := []string{"events"}
	for _, n := range names {
		args = append(args, "--mention", n)
	}
	args = append(args, "--sql", fmt.Sprintf("id > %d", cursor), "--last", fmt.Sprint(limit))
	return b.query(args...)
}

func (b *Bus) query(args ...string) ([]BusEvent, error) {
	out, err := b.run(args...)
	if err != nil {
		return nil, err
	}
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
	return evs, nil
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
