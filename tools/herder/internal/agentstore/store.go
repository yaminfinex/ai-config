package agentstore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// MaxLineBytes bounds one appended line; the write is a single write(2) and
// must stay well inside what O_APPEND delivers atomically.
const MaxLineBytes = 16 * 1024

// DefaultLockTimeout bounds the only thing register ever blocks on.
const DefaultLockTimeout = 2 * time.Second

// ErrUnavailable wraps every failure that means "the store cannot be written":
// register maps it to exit 3.
var ErrUnavailable = errors.New("store unavailable")

// Store is one agents/ directory. Zero value is unusable; use Open.
type Store struct {
	Dir         string // <state>/agents
	EdgesPath   string // <state>/launch-edges.jsonl (import source)
	LockTimeout time.Duration
	Stderr      io.Writer // one-line diagnostics (torn tail, import); nil = discard
	ImportErr   error     // set by Open when the first-open import could not create events.jsonl
}

// Receipt is what Append returns; a replayed id returns the receipt of the
// line that already exists and appends nothing.
type Receipt struct {
	Event    Event `json:"event"`
	Offset   int64 `json:"offset"`
	Replayed bool  `json:"replayed"`
}

func (s *Store) EventsPath() string   { return filepath.Join(s.Dir, "events.jsonl") }
func (s *Store) SnapshotPath() string { return filepath.Join(s.Dir, "snapshot.json") }

// Open resolves the store under stateDir and runs the one-time edge import:
// when events.jsonl is absent and launch-edges.jsonl exists, every edge line
// becomes a launch-ready event (launcher_kind web) with a deterministic id.
// An import failure is recorded in ImportErr, never returned: readers print
// unregistered rows and one warning; only register turns it into exit 3.
func Open(stateDir string, stderr io.Writer) *Store {
	s := &Store{
		Dir:         filepath.Join(stateDir, "agents"),
		EdgesPath:   filepath.Join(stateDir, "launch-edges.jsonl"),
		LockTimeout: DefaultLockTimeout,
		Stderr:      stderr,
	}
	s.ImportErr = s.importEdges()
	return s
}

func (s *Store) warn(format string, args ...any) {
	if s.Stderr != nil {
		fmt.Fprintf(s.Stderr, "herder: agent store: "+format+"\n", args...)
	}
}

// Append validates, takes the lock (bounded), repairs a torn tail, checks the
// id for replay, and writes the line with one write(2) plus fsync.
func (s *Store) Append(e Event) (Receipt, error) {
	if err := e.Validate(); err != nil {
		return Receipt{}, err
	}
	if !ValidID(e.ID) {
		return Receipt{}, fmt.Errorf("event id %q is not UUID-shaped", e.ID)
	}
	e.At = e.At.UTC()
	line, err := Encode(e)
	if err != nil {
		return Receipt{}, err
	}
	if len(line) > MaxLineBytes {
		return Receipt{}, fmt.Errorf("event line is %d bytes, refusing more than %d", len(line), MaxLineBytes)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	file, err := os.OpenFile(s.EventsPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer file.Close()
	timeout := s.LockTimeout
	if timeout <= 0 {
		timeout = DefaultLockTimeout
	}
	if err := lockFile(file, timeout); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer unlockFile(file)

	if err := s.repairTornTail(file); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if existing, offset, found, err := s.findID(e.ID); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	} else if found {
		return Receipt{Event: existing, Offset: offset, Replayed: true}, nil
	}
	end, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	n, err := file.Write(line)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if n != len(line) {
		return Receipt{}, fmt.Errorf("%w: short write %d of %d bytes", ErrUnavailable, n, len(line))
	}
	if err := file.Sync(); err != nil {
		return Receipt{}, fmt.Errorf("%w: fsync: %v", ErrUnavailable, err)
	}
	return Receipt{Event: e, Offset: end}, nil
}

// repairTornTail runs under the lock: a final line without its newline never
// had a receipt (the writer died between write and return, or the disk was
// full), so it is truncated to the last complete record.
func (s *Store) repairTornTail(file *os.File) error {
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	size := stat.Size()
	if size == 0 {
		return nil
	}
	read, err := os.Open(s.EventsPath())
	if err != nil {
		return err
	}
	defer read.Close()
	last := make([]byte, 1)
	if _, err := read.ReadAt(last, size-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	end, err := completeEnd(read, size)
	if err != nil {
		return err
	}
	if err := os.Truncate(s.EventsPath(), end); err != nil {
		return err
	}
	s.warn("repaired torn tail of events.jsonl: dropped %d bytes of an incomplete record", size-end)
	return nil
}

// completeEnd returns the byte after the last newline at or before size.
func completeEnd(read io.ReaderAt, size int64) (int64, error) {
	const block = 64 * 1024
	buf := make([]byte, block)
	for pos := size; pos > 0; {
		n := int64(block)
		if pos < n {
			n = pos
		}
		if _, err := read.ReadAt(buf[:n], pos-n); err != nil && err != io.EOF {
			return 0, err
		}
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			return pos - n + int64(i) + 1, nil
		}
		pos -= n
	}
	return 0, nil
}

// findID scans complete lines for id (replay detection). The file is a few
// thousand short lines at most before rotation, so a scan is cheaper than an
// index that could drift.
func (s *Store) findID(id string) (Event, int64, bool, error) {
	var found Event
	var at int64
	hit := false
	err := s.scan(0, func(e Event, offset int64) {
		if e.ID == id {
			found, at, hit = e, offset, true
		}
	})
	if errors.Is(err, os.ErrNotExist) {
		return Event{}, 0, false, nil
	}
	return found, at, hit, err
}

// scan visits every complete, well-formed line from offset. A trailing
// partial line is ignored (same contract as sessionjsonl.CompleteEnd); a
// malformed complete line is skipped with one warning, never fatal. It
// returns the byte offset after the last complete line.
func (s *Store) scan(from int64, visit func(Event, int64)) error {
	_, err := s.scanEnd(from, visit)
	return err
}

func (s *Store) scanEnd(from int64, visit func(Event, int64)) (int64, error) {
	file, err := os.Open(s.EventsPath())
	if err != nil {
		return from, err
	}
	defer file.Close()
	if _, err := file.Seek(from, io.SeekStart); err != nil {
		return from, err
	}
	reader := bufio.NewReaderSize(file, 64*1024)
	offset := from
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return offset, nil // partial trailing line ignored
			}
			return offset, err
		}
		start := offset
		offset += int64(len(line))
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(trimmed, &e); err != nil || e.ID == "" || e.Kind == "" {
			s.warn("skipping malformed record at byte %d", start)
			continue
		}
		visit(e, start)
	}
}

// importEdges is the one-time launch-edges.jsonl import (see Open).
func (s *Store) importEdges() error {
	if _, err := os.Stat(s.EventsPath()); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	raw, err := os.ReadFile(s.EdgesPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	type edge struct {
		Name, Launcher, Tool, Model, Effort, Tag, Workspace, Pane string
		Time                                                      time.Time
	}
	imported := 0
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ed edge
		if err := json.Unmarshal(line, &ed); err != nil || ed.Name == "" {
			s.warn("skipping malformed launch edge: %.80s", line)
			continue
		}
		e := Event{
			ID: DerivedID(line), At: ed.Time, Kind: KindLaunchReady,
			By: ed.Launcher, ByKind: "web", LauncherKind: "web", Name: ed.Name,
			Tool: ed.Tool, Model: ed.Model, Effort: ed.Effort, Tag: ed.Tag, Pane: ed.Pane,
		}
		if ed.Workspace != "" {
			e.Placement = &Placement{Workspace: ed.Workspace}
		}
		if e.At.IsZero() {
			e.At = time.Unix(0, 0).UTC()
		}
		if _, err := s.Append(e); err != nil {
			return err
		}
		imported++
	}
	if imported > 0 {
		s.warn("imported %d launch edge(s) from %s", imported, s.EdgesPath)
	}
	return nil
}
