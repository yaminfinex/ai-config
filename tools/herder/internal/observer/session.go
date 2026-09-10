// session.go owns one session's record: the Key, the phase words, and the
// per-session transitions (resolve, seed, advance, refresh). It does not own
// discovery (discover.go) or watching (watch.go), and it never persists.
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

// state is one session's record; only this package (and its tests) sees it.
// Lookup copies out the three facts a caller may have.
type state struct {
	key        Key
	agent      string
	path       string
	phase      Phase
	offset     int64
	size       int64
	vitals     claudesession.Vitals
	observedAt time.Time
	endedAt    time.Time
	watched    bool
	err        string
}

type session struct {
	state
	row hcomidentity.Row
}

func (s *session) subagent() bool { return sessionvitals.IsSubagent(s.row) }

// resolve fills Path or leaves the session awaiting its file. A typed
// "file absent" refusal is the normal fresh-launch case; any other error is
// recorded and retried on the sweep.
func (o *Observer) resolve(s *session) bool {
	path, err := sessionvitals.ResolvePath(o.opts.Home, s.row)
	if err != nil {
		s.path = ""
		s.phase = PhaseAwaitingFile
		if sessionvitals.IsResolveRefusal(err) {
			s.err = ""
		} else {
			s.err = err.Error()
		}
		return false
	}
	s.path = path
	s.err = ""
	return true
}

// seed is the direct read plus the tail offset; the session becomes tailing.
func (o *Observer) seed(s *session) {
	s.phase = PhaseSeeding
	vitals, end, err := sessionvitals.Seed(s.key.Tool, s.subagent(), s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.phase = PhaseAwaitingFile
			s.err = ""
			return
		}
		s.err = err.Error()
		s.phase = PhaseAwaitingFile
		return
	}
	s.vitals, s.offset, s.size = vitals, end, end
	s.err = ""
	s.phase = PhaseTailing
	s.observedAt = o.opts.Now()
	o.watchSession(s)
}

// advance folds appended bytes; a shorter file re-seeds through truncated.
// Returns the bytes read (tests assert it equals the append).
func (o *Observer) advance(s *session) int64 {
	if s.phase != PhaseTailing {
		return 0
	}
	vitals, end, read, err := sessionvitals.Advance(s.key.Tool, s.subagent(), s.path, s.offset, s.vitals)
	switch {
	case errors.Is(err, sessionvitals.ErrTruncated):
		s.phase = PhaseTruncated
		s.err = claudesession.Reset{Reason: claudesession.ResetTruncated, SessionID: s.key.SessionID, PreviousOffset: s.offset}.Error()
		o.seed(s)
		return 0
	case errors.Is(err, os.ErrNotExist):
		s.phase = PhaseAwaitingFile
		return 0
	case err != nil:
		s.err = err.Error()
		return 0
	}
	s.vitals, s.offset, s.size = vitals, end, end
	s.err = ""
	if read > 0 {
		s.observedAt = o.opts.Now()
	}
	return read
}

// refresh is what a change notification or a sweep does for one session:
// resolve if needed, seed if not yet tailing, else advance.
func (o *Observer) refresh(s *session) int64 {
	switch s.phase {
	case PhaseEnded:
		return 0
	case PhaseAwaitingFile:
		if s.path == "" && !o.resolve(s) {
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
	if o.watch == nil || s.path == "" {
		s.watched = false
		return
	}
	s.watched = o.watch.add(filepath.Dir(s.path))
}
