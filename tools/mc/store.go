package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Thread state lives in an append-only JSONL journal + in-memory projection
// (registry pattern). Message TEXT canonically lives on the bus; linked
// messages are cached here keyed by bus event id so pages render without
// per-request bus queries. The ingest cursor is journaled too: one file is
// the whole recoverable state.

type Entry struct {
	TS         string   `json:"ts"`
	Op         string   `json:"op"` // open | link | turn | close | reopen | promote | retitle | cursor
	Thread     string   `json:"thread,omitempty"`
	Title      string   `json:"title,omitempty"`
	Grade      string   `json:"grade,omitempty"` // managed | observed; absent (pre-grade journals) = managed
	Context    string   `json:"context,omitempty"`
	Expects    string   `json:"expects,omitempty"`
	Weight     string   `json:"weight,omitempty"`
	Home       string   `json:"home,omitempty"`
	By         string   `json:"by,omitempty"`
	With       []string `json:"with,omitempty"`
	Turn       string   `json:"turn,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	BusID      int64    `json:"bus_id,omitempty"`
	From       string   `json:"from,omitempty"`
	Text       string   `json:"text,omitempty"`
	Intent     string   `json:"intent,omitempty"`
	ReplyTo    int64    `json:"reply_to,omitempty"`
	MsgTS      string   `json:"msg_ts,omitempty"`
}

type Msg struct {
	BusID   int64
	From    string
	Text    string
	Intent  string
	ReplyTo int64
	TS      string
}

type Thread struct {
	ID         string
	Title      string
	Status     string // open | closed
	Grade      string // managed (on the desk, full lifecycle) | observed (bus thread we merely track)
	Expects    string // decide | act | reply | read
	Weight     string
	Context    string
	Home       string
	OpenedBy   string
	With       []string
	Turn       string // "owner" or the counterparty side
	Resolution string
	Created    time.Time
	Updated    time.Time
	Msgs       []Msg
}

func (t *Thread) HasBusID(id int64) bool {
	for _, m := range t.Msgs {
		if m.BusID == id {
			return true
		}
	}
	return false
}

type Store struct {
	mu         sync.Mutex
	path       string
	f          *os.File
	threads    map[string]*Thread
	cursor     int64
	generation uint64
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: path, threads: map[string]*Thread{}}
	if raw, err := os.ReadFile(path); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for sc.Scan() {
			var e Entry
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				s.apply(&e)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	s.f = f
	return s, nil
}

func (s *Store) append(e *Entry) error {
	e.TS = time.Now().Format(time.RFC3339)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := s.f.Write(append(b, '\n')); err != nil {
		return err
	}
	s.apply(e)
	return nil
}

func (s *Store) apply(e *Entry) {
	s.generation++
	ts, _ := time.Parse(time.RFC3339, e.TS)
	switch e.Op {
	case "cursor":
		if e.BusID > s.cursor {
			s.cursor = e.BusID
		}
	case "open":
		if _, ok := s.threads[e.Thread]; ok {
			return
		}
		grade := e.Grade
		if grade == "" {
			grade = "managed" // pre-grade journal lines predate observed tracking
		}
		s.threads[e.Thread] = &Thread{
			ID: e.Thread, Title: e.Title, Status: "open", Grade: grade,
			Expects: e.Expects, Weight: e.Weight, Context: e.Context,
			Home: e.Home, OpenedBy: e.By, With: e.With, Turn: e.Turn,
			Created: ts, Updated: ts,
		}
	case "link":
		t := s.threads[e.Thread]
		if t == nil || t.HasBusID(e.BusID) {
			return
		}
		t.Msgs = append(t.Msgs, Msg{BusID: e.BusID, From: e.From, Text: e.Text, Intent: e.Intent, ReplyTo: e.ReplyTo, TS: e.MsgTS})
		t.Updated = ts
		if e.From != "" && e.From != t.OpenedBy && !contains(t.With, e.From) {
			t.With = append(t.With, e.From)
		}
	case "turn":
		if t := s.threads[e.Thread]; t != nil {
			t.Turn = e.Turn
			t.Updated = ts
		}
	case "close":
		if t := s.threads[e.Thread]; t != nil {
			t.Status = "closed"
			t.Resolution = e.Resolution
			t.Updated = ts
		}
	case "reopen":
		if t := s.threads[e.Thread]; t != nil {
			t.Status = "open"
			t.Resolution = ""
			t.Updated = ts
		}
	case "promote":
		if t := s.threads[e.Thread]; t != nil {
			t.Grade = "managed"
			t.Status = "open"
			t.Resolution = ""
			if e.Expects != "" {
				t.Expects = e.Expects
			}
			t.Updated = ts
		}
	case "retitle":
		if t := s.threads[e.Thread]; t != nil && t.Grade == "managed" && e.Title != "" {
			t.Title = e.Title
			t.Updated = ts
		}
	}
}

func (s *Store) Cursor() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor
}

// Version is the cheap cache-validator input for server-rendered pages. It
// deliberately exposes only projection metadata: conditional GETs do not
// need to copy or inspect any thread state.
func (s *Store) Version() (int64, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor, s.generation
}

func (s *Store) SetCursor(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id <= s.cursor {
		return nil
	}
	return s.append(&Entry{Op: "cursor", BusID: id})
}

func (s *Store) Get(id string) *Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.threads[id]; t != nil {
		cp := *t
		return &cp
	}
	return nil
}

func (s *Store) Has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.threads[id]
	return ok
}

var expectsRank = map[string]int{"decide": 0, "act": 1, "reply": 2, "read": 3}

func (s *Store) List(status, grade string) []*Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Thread
	for _, t := range s.threads {
		if (status == "" || t.Status == status) && (grade == "" || t.Grade == grade) {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := expectsRank[out[i].Expects], expectsRank[out[j].Expects]
		if ri != rj {
			return ri < rj
		}
		return out[i].Updated.After(out[j].Updated)
	})
	return out
}

// GraphSnapshot exposes only the read-only projections needed by /graph.
// It deliberately derives them from the in-memory store and never appends a
// journal operation.
func (s *Store) GraphSnapshot() (map[int64]string, map[string]graphThreadInfo, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	authors := map[int64]string{}
	threads := map[string]graphThreadInfo{}
	yourTurn := 0
	for id, t := range s.threads {
		threads[id] = graphThreadInfo{Grade: t.Grade, Title: t.Title}
		if t.Status == "open" && t.Grade == "managed" && t.Turn == "owner" {
			yourTurn++
		}
		for _, msg := range t.Msgs {
			authors[msg.BusID] = msg.From
		}
	}
	return authors, threads, yourTurn
}

func (s *Store) Open(id, title, context, expects, weight, home, by string, with []string, turn, grade string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.threads[id]; ok {
		return fmt.Errorf("thread %s already exists", id)
	}
	return s.append(&Entry{Op: "open", Thread: id, Title: title, Context: context,
		Expects: expects, Weight: weight, Home: home, By: by, With: with, Turn: turn, Grade: grade})
}

// Promote lifts an observed thread onto the desk. Linked history is kept, but
// stale lifecycle state is not: corrupt/pre-guard journals must not let an
// observed close hide a later explicit raise. No-op if already managed.
func (s *Store) Promote(thread, expects string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[thread]
	if t == nil {
		return fmt.Errorf("no thread %s", thread)
	}
	if t.Grade == "managed" {
		return nil
	}
	return s.append(&Entry{Op: "promote", Thread: thread, Expects: expects})
}

func (s *Store) Link(thread string, m Msg) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[thread]
	if t == nil {
		return fmt.Errorf("no thread %s", thread)
	}
	if t.HasBusID(m.BusID) {
		return nil
	}
	return s.append(&Entry{Op: "link", Thread: thread, BusID: m.BusID, From: m.From,
		Text: m.Text, Intent: m.Intent, ReplyTo: m.ReplyTo, MsgTS: m.TS})
}

func (s *Store) SetTurn(thread, turn string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.threads[thread]; t == nil || t.Turn == turn {
		return nil
	}
	return s.append(&Entry{Op: "turn", Thread: thread, Turn: turn})
}

func (s *Store) Close(thread, resolution string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[thread]
	if t == nil {
		return fmt.Errorf("no thread %s", thread)
	}
	if t.Grade != "managed" {
		return fmt.Errorf("thread %s is observed; lifecycle writes are refused", thread)
	}
	return s.append(&Entry{Op: "close", Thread: thread, Resolution: resolution})
}

func (s *Store) Reopen(thread string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[thread]
	if t == nil {
		return fmt.Errorf("no thread %s", thread)
	}
	if t.Grade != "managed" {
		return fmt.Errorf("thread %s is observed; lifecycle writes are refused", thread)
	}
	return s.append(&Entry{Op: "reopen", Thread: thread})
}

func (s *Store) Retitle(thread, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[thread]
	if t == nil {
		return fmt.Errorf("no thread %s", thread)
	}
	if t.Grade != "managed" {
		return fmt.Errorf("thread %s is observed; lifecycle writes are refused", thread)
	}
	if title == "" {
		return fmt.Errorf("thread %s needs a title", thread)
	}
	if title == t.Title {
		return nil
	}
	return s.append(&Entry{Op: "retitle", Thread: thread, Title: title})
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
