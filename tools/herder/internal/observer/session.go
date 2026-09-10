package observer

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/sessionvitals"
)

// Key identifies one transcript. AgentID is part of the key because a Claude
// subagent row shares its parent's session_id but resolves to its own file.
type Key struct {
	Tool      string
	SessionID string
	AgentID   string
}

// KeyFor is the one place a roster row becomes a Key.
func KeyFor(row hcomidentity.Row) Key {
	key := Key{Tool: row.Tool, SessionID: row.SessionID}
	if sessionvitals.IsSubagent(row) {
		key.AgentID = row.AgentID
	}
	return key
}

// Phase is the session's tail state. The same words appear in the socket
// response and in docs.
type Phase string

const (
	PhaseAwaitingFile Phase = "awaiting_file" // discovered, transcript not on disk yet
	PhaseSeeding      Phase = "seeding"       // reverse scan in progress (transient)
	PhaseTailing      Phase = "tailing"       // offset valid, advancing on change
	PhaseTruncated    Phase = "truncated"     // file shrank below offset; re-seeding
	PhaseEnded        Phase = "ended"         // superseded or gone from the roster; kept for TTL
)

// Snapshot is the read-only copy Lookup and tests see.
type Snapshot struct {
	Key        Key
	Agent      string
	Path       string
	Phase      Phase
	Offset     int64
	Size       int64
	Vitals     claudesession.Vitals
	ObservedAt time.Time
	EndedAt    time.Time
	Watched    bool
	Err        string
}

type session struct {
	Snapshot
	row hcomidentity.Row
}

func (s *session) snapshot() Snapshot { return s.Snapshot }

func (s *session) subagent() bool { return sessionvitals.IsSubagent(s.row) }

// resolve fills Path or leaves the session awaiting its file. A typed
// "file absent" refusal is the normal fresh-launch case; any other error is
// recorded and retried on the sweep.
func (o *Observer) resolve(s *session) bool {
	path, err := sessionvitals.ResolvePath(o.opts.Home, s.row)
	if err != nil {
		s.Path = ""
		s.Phase = PhaseAwaitingFile
		if sessionvitals.IsResolveRefusal(err) {
			s.Err = ""
		} else {
			s.Err = err.Error()
		}
		return false
	}
	s.Path = path
	s.Err = ""
	return true
}

// seed is the direct read plus the tail offset; the session becomes tailing.
func (o *Observer) seed(s *session) {
	s.Phase = PhaseSeeding
	vitals, end, err := sessionvitals.Seed(s.Key.Tool, s.subagent(), s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.Phase = PhaseAwaitingFile
			s.Err = ""
			return
		}
		s.Err = err.Error()
		s.Phase = PhaseAwaitingFile
		return
	}
	s.Vitals, s.Offset, s.Size = vitals, end, end
	s.Err = ""
	s.Phase = PhaseTailing
	s.ObservedAt = o.opts.Now()
	o.watchSession(s)
}

// advance folds appended bytes; a shorter file re-seeds through truncated.
// Returns the bytes read (tests assert it equals the append).
func (o *Observer) advance(s *session) int64 {
	if s.Phase != PhaseTailing {
		return 0
	}
	vitals, end, read, err := sessionvitals.Advance(s.Key.Tool, s.subagent(), s.Path, s.Offset, s.Vitals)
	switch {
	case errors.Is(err, sessionvitals.ErrTruncated):
		s.Phase = PhaseTruncated
		s.Err = claudesession.Reset{Reason: claudesession.ResetTruncated, SessionID: s.Key.SessionID, PreviousOffset: s.Offset}.Error()
		o.seed(s)
		return 0
	case errors.Is(err, os.ErrNotExist):
		s.Phase = PhaseAwaitingFile
		return 0
	case err != nil:
		s.Err = err.Error()
		return 0
	}
	s.Vitals, s.Offset, s.Size = vitals, end, end
	s.Err = ""
	if read > 0 {
		s.ObservedAt = o.opts.Now()
	}
	return read
}

// refresh is what a change notification or a sweep does for one session:
// resolve if needed, seed if not yet tailing, else advance.
func (o *Observer) refresh(s *session) int64 {
	switch s.Phase {
	case PhaseEnded:
		return 0
	case PhaseAwaitingFile:
		if s.Path == "" && !o.resolve(s) {
			return 0
		}
		o.seed(s)
		return 0
	default:
		return o.advance(s)
	}
}

// watchSession asks the directory watcher for the transcript's directory;
// Watched=false means this session lives on the sweep.
func (o *Observer) watchSession(s *session) {
	if o.watch == nil || s.Path == "" {
		s.Watched = false
		return
	}
	s.Watched = o.watch.add(filepath.Dir(s.Path))
}
