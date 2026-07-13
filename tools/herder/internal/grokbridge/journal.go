package grokbridge

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Event struct {
	ID        int64           `json:"id"`
	Timestamp string          `json:"ts,omitempty"`
	Type      string          `json:"type,omitempty"`
	Instance  string          `json:"instance,omitempty"`
	Data      json.RawMessage `json:"data"`
	Raw       json.RawMessage `json:"-"`
}

type Message struct {
	From         string   `json:"from"`
	Text         string   `json:"text"`
	Intent       string   `json:"intent"`
	Thread       string   `json:"thread"`
	Scope        string   `json:"scope,omitempty"`
	DeliveredTo  []string `json:"delivered_to,omitempty"`
	Mentions     []string `json:"mentions,omitempty"`
	ReplyTo      string   `json:"reply_to,omitempty"`
	ReplyToLocal int64    `json:"reply_to_local,omitempty"`
}

type Record struct {
	Kind       string          `json:"kind"`
	At         string          `json:"at"`
	Generation uint64          `json:"generation,omitempty"`
	ID         int64           `json:"id,omitempty"`
	Event      json.RawMessage `json:"event,omitempty"`
	Hash       string          `json:"hash,omitempty"`
	Surface    string          `json:"surface,omitempty"`
	Repeat     bool            `json:"repeat,omitempty"`
	Result     string          `json:"result,omitempty"`
}

type Receipt struct {
	Event       Event
	Raw         json.RawMessage
	Message     Message
	Hash        string
	Surfaced    bool
	Fetched     bool
	Acked       bool
	Retired     bool
	Surfaces    int
	Nudges      int
	LastSurface time.Time
}

func (r Receipt) Status() string {
	if r.Retired {
		return "undeliverable"
	}
	if r.Acked {
		return "delivered"
	}
	if r.Fetched {
		return "fetched"
	}
	if r.Surfaced {
		return "surfaced"
	}
	return "queued"
}

type Journal struct {
	mu         sync.Mutex
	path       string
	f          *os.File
	receipts   map[int64]*Receipt
	generation uint64
	cursor     int64
	pending    int
	retired    int
	retiring   bool
}

func OpenJournal(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_, statErr := os.Stat(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	if os.IsNotExist(statErr) {
		d, openErr := os.Open(filepath.Dir(path))
		if openErr != nil {
			f.Close()
			return nil, openErr
		}
		err = d.Sync()
		d.Close()
		if err != nil {
			f.Close()
			return nil, err
		}
	}
	// A crash may leave a partial final write. It never gated an external claim
	// because the newline-bearing record was not fsynced, so discard only that
	// unterminated suffix before replay. Corruption in a complete line still
	// fails closed below.
	if data, readErr := os.ReadFile(path); readErr == nil && len(data) > 0 && data[len(data)-1] != '\n' {
		cut := bytes.LastIndexByte(data, '\n') + 1
		if err := f.Truncate(int64(cut)); err != nil {
			f.Close()
			return nil, err
		}
	}
	j := &Journal{path: path, f: f, receipts: make(map[int64]*Receipt)}
	if err := j.replay(); err != nil {
		f.Close()
		return nil, err
	}
	return j, nil
}

func (j *Journal) Close() error       { j.mu.Lock(); defer j.mu.Unlock(); return j.f.Close() }
func (j *Journal) Cursor() int64      { j.mu.Lock(); defer j.mu.Unlock(); return j.cursor }
func (j *Journal) Generation() uint64 { j.mu.Lock(); defer j.mu.Unlock(); return j.generation }

func (j *Journal) Counts() (pending, retired int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.pending, j.retired
}

func (j *Journal) replay() error {
	if _, err := j.f.Seek(0, 0); err != nil {
		return err
	}
	s := bufio.NewScanner(j.f)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for s.Scan() {
		line++
		var rec Record
		if err := json.Unmarshal(s.Bytes(), &rec); err != nil {
			return fmt.Errorf("replay journal line %d: %w; repair the incomplete journal record before restarting the bridge", line, err)
		}
		if err := j.apply(rec); err != nil {
			return fmt.Errorf("replay journal line %d: %w", line, err)
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	_, err := j.f.Seek(0, 2)
	return err
}

func (j *Journal) apply(rec Record) error {
	switch rec.Kind {
	case "generation":
		if rec.Generation <= j.generation {
			return fmt.Errorf("generation %d does not advance %d; inspect the seat journal for concurrent writers", rec.Generation, j.generation)
		}
		j.generation = rec.Generation
	case "queued":
		if _, exists := j.receipts[rec.ID]; exists {
			return nil
		}
		var ev Event
		if err := json.Unmarshal(rec.Event, &ev); err != nil {
			return fmt.Errorf("decode queued event %d: %w", rec.ID, err)
		}
		var msg Message
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			return fmt.Errorf("decode queued message %d: %w", rec.ID, err)
		}
		j.receipts[rec.ID] = &Receipt{Event: ev, Raw: append(json.RawMessage(nil), rec.Event...), Message: msg, Hash: rec.Hash}
		j.pending++
		if rec.ID > j.cursor {
			j.cursor = rec.ID
		}
	case "surfaced":
		r, ok := j.receipts[rec.ID]
		if !ok {
			return fmt.Errorf("surface references unknown id %d; restore a consistent journal", rec.ID)
		}
		r.Surfaced, r.Surfaces = true, r.Surfaces+1
		if rec.Surface == "nudge" {
			r.Nudges++
		}
		if at, err := time.Parse(time.RFC3339Nano, rec.At); err == nil {
			r.LastSurface = at
		}
	case "fetched":
		r, ok := j.receipts[rec.ID]
		if !ok {
			return fmt.Errorf("fetch references unknown id %d; restore a consistent journal", rec.ID)
		}
		r.Surfaced, r.Fetched = true, true
	case "acked":
		r, ok := j.receipts[rec.ID]
		if !ok {
			return fmt.Errorf("ack references unknown id %d; restore a consistent journal", rec.ID)
		}
		if !r.Fetched {
			return fmt.Errorf("ack for id %d has no fetch; restore a consistent journal", rec.ID)
		}
		if !r.Acked && !r.Retired {
			j.pending--
		}
		r.Acked = true
	case "undeliverable":
		r, ok := j.receipts[rec.ID]
		if !ok {
			return fmt.Errorf("retirement references unknown id %d", rec.ID)
		}
		if !r.Retired {
			if !r.Acked {
				j.pending--
			}
			j.retired++
		}
		r.Retired = true
	case "outbound":
	default:
		return fmt.Errorf("unknown record kind %q; upgrade herder or repair the journal", rec.Kind)
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (j *Journal) append(rec Record, durable bool) error {
	rec.At = now()
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err = j.f.Write(b); err != nil {
		return err
	}
	if durable {
		if err = j.f.Sync(); err != nil {
			return err
		}
	}
	return j.apply(rec)
}

func (j *Journal) AdvanceGeneration() (uint64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	n := j.generation + 1
	if err := j.append(Record{Kind: "generation", Generation: n}, true); err != nil {
		return 0, err
	}
	return n, nil
}

func (j *Journal) Queue(raw json.RawMessage) (Receipt, bool, error) {
	receipt, added, _, err := j.queuePendingChange(raw)
	return receipt, added, err
}

func (j *Journal) queuePendingChange(raw json.RawMessage) (Receipt, bool, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	pendingBefore := j.pending
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return Receipt{}, false, false, fmt.Errorf("decode hcom event: %w", err)
	}
	if ev.ID <= 0 {
		return Receipt{}, false, false, errors.New("hcom event is missing a positive numeric id")
	}
	if existing, ok := j.receipts[ev.ID]; ok {
		return *existing, false, false, nil
	}
	var msg Message
	if err := json.Unmarshal(ev.Data, &msg); err != nil {
		return Receipt{}, false, false, fmt.Errorf("decode hcom event %d data: %w", ev.ID, err)
	}
	h := sha256.Sum256([]byte(msg.Text))
	hash := hex.EncodeToString(h[:4])
	copyRaw := append(json.RawMessage(nil), raw...)
	if err := j.append(Record{Kind: "queued", ID: ev.ID, Event: copyRaw, Hash: hash}, true); err != nil {
		return Receipt{}, false, false, err
	}
	if j.retiring {
		if err := j.append(Record{Kind: "undeliverable", ID: ev.ID, Generation: j.generation}, true); err != nil {
			return Receipt{}, false, false, err
		}
	}
	return *j.receipts[ev.ID], true, pendingBefore != j.pending, nil
}

func (j *Journal) Surface(id int64, kind string, gen uint64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if gen != j.generation {
		return staleGeneration(gen, j.generation)
	}
	if _, ok := j.receipts[id]; !ok {
		return fmt.Errorf("message id %d is not queued for this seat", id)
	}
	return j.append(Record{Kind: "surfaced", ID: id, Surface: kind, Generation: gen}, false)
}

func (j *Journal) Pending(gen uint64, surface bool) ([]Receipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if gen != j.generation {
		return nil, staleGeneration(gen, j.generation)
	}
	ids := make([]int64, 0)
	for id, r := range j.receipts {
		if !r.Acked && !r.Retired {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	out := make([]Receipt, 0, len(ids))
	for _, id := range ids {
		if surface {
			if err := j.append(Record{Kind: "surfaced", ID: id, Surface: "pending", Generation: gen}, false); err != nil {
				return nil, err
			}
		}
		out = append(out, *j.receipts[id])
	}
	return out, nil
}

func (j *Journal) Fetch(id int64, gen uint64) (Receipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if gen != j.generation {
		return Receipt{}, staleGeneration(gen, j.generation)
	}
	r, ok := j.receipts[id]
	if !ok {
		return Receipt{}, fmt.Errorf("message id %d is not queued for this seat", id)
	}
	repeat := r.Fetched
	if err := j.append(Record{Kind: "surfaced", ID: id, Surface: "fetch", Generation: gen}, false); err != nil {
		return Receipt{}, err
	}
	if err := j.append(Record{Kind: "fetched", ID: id, Generation: gen, Repeat: repeat}, false); err != nil {
		return Receipt{}, err
	}
	return *r, nil
}

func (j *Journal) Ack(id int64, gen uint64) error {
	_, err := j.ackPendingChange(id, gen)
	return err
}

func (j *Journal) ackPendingChange(id int64, gen uint64) (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	pendingBefore := j.pending
	if gen != j.generation {
		return false, staleGeneration(gen, j.generation)
	}
	r, ok := j.receipts[id]
	if !ok {
		return false, fmt.Errorf("message id %d is not queued for this seat", id)
	}
	if !r.Fetched {
		return false, fmt.Errorf("message id %d: fetch before ack is required; call fetch_message first", id)
	}
	if err := j.append(Record{Kind: "acked", ID: id, Generation: gen, Repeat: r.Acked}, true); err != nil {
		return false, err
	}
	return pendingBefore != j.pending, nil
}

func (j *Journal) RecordOutbound(result string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.append(Record{Kind: "outbound", Result: result}, true)
}

func (j *Journal) RetireUnacked(gen uint64) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if gen != j.generation {
		return 0, staleGeneration(gen, j.generation)
	}
	j.retiring = true
	ids := make([]int64, 0)
	for id, r := range j.receipts {
		if !r.Acked && !r.Retired {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, k int) bool { return ids[i] < ids[k] })
	for _, id := range ids {
		if err := j.append(Record{Kind: "undeliverable", ID: id, Generation: gen}, true); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func (j *Journal) NudgeCandidates(gen uint64, olderThan time.Time, max int) ([]Receipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if gen != j.generation {
		return nil, staleGeneration(gen, j.generation)
	}
	var out []Receipt
	for _, r := range j.receipts {
		if r.Surfaced && !r.Fetched && !r.Acked && !r.Retired && r.Nudges < max && !r.LastSurface.After(olderThan) {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Event.ID < out[j].Event.ID })
	return out, nil
}

func staleGeneration(got, current uint64) error {
	return fmt.Errorf("stale bridge generation %d (current %d); reconnect to the seat bridge and retry", got, current)
}
