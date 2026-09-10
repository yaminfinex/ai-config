package observer

import (
	"os"
	"path/filepath"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
)

// sync reconciles the table with one roster read. Pure over the rows except
// for path resolution and seeding of NEW sessions; known sessions are left to
// the watcher and the sweep.
func (o *Observer) sync(rows []hcomidentity.Row) {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.opts.Now()
	live := make(map[Key]bool, len(rows))
	for _, row := range rows {
		if row.Tool != "claude" && row.Tool != "codex" || row.SessionID == "" {
			continue
		}
		key := KeyFor(row)
		live[key] = true
		if previous, known := o.byName[row.Name]; known && previous != key {
			// Same name, new session (resume): the old record is superseded.
			o.end(previous, now)
		}
		o.byName[row.Name] = key
		if s, ok := o.sessions[key]; ok {
			s.row = row
			if s.Phase == PhaseEnded {
				// A session that came back (roster blip) resumes tailing.
				s.Phase = PhaseTailing
				s.EndedAt = time.Time{}
				o.refresh(s)
			}
			continue
		}
		s := &session{Snapshot: Snapshot{Key: key, Agent: row.Name, Phase: PhaseAwaitingFile}, row: row}
		o.sessions[key] = s
		if o.resolve(s) {
			o.seed(s)
		} else {
			o.watchExpectedDirectory(s)
		}
	}
	for name, key := range o.byName {
		if !live[key] {
			delete(o.byName, name)
			o.end(key, now)
		}
	}
	for key, s := range o.sessions {
		if s.Phase == PhaseEnded && !s.EndedAt.IsZero() && now.Sub(s.EndedAt) >= o.opts.TTL {
			delete(o.sessions, key)
		}
	}
}

func (o *Observer) end(key Key, now time.Time) {
	s, ok := o.sessions[key]
	if !ok || s.Phase == PhaseEnded {
		return
	}
	s.Phase = PhaseEnded
	s.EndedAt = now
}

// watchExpectedDirectory lets a fresh launch (awaiting_file) turn into
// tailing on the fsnotify create event instead of the next sweep. The
// directory is the tool's known location when it already exists.
func (o *Observer) watchExpectedDirectory(s *session) {
	if o.watch == nil {
		return
	}
	var dir string
	switch s.Key.Tool {
	case "codex":
		if s.row.TranscriptPath != "" {
			dir = filepath.Dir(s.row.TranscriptPath)
		}
	default:
		if home := o.opts.Home; home != "" && s.row.Directory != "" {
			dir = filepath.Join(home, ".claude", "projects", claudesession.Slug(s.row.Directory))
		}
	}
	if dir == "" {
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return
	}
	s.Watched = o.watch.add(dir)
}

// onPaths handles one debounced batch of changed transcript paths.
func (o *Observer) onPaths(paths []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	changed := make(map[string]bool, len(paths))
	for _, path := range paths {
		changed[path] = true
	}
	for _, s := range o.sessions {
		if s.Phase == PhaseEnded {
			continue
		}
		if s.Path != "" && changed[s.Path] {
			o.refresh(s)
			continue
		}
		if s.Phase == PhaseAwaitingFile {
			// A create in a watched directory may be this session's file.
			o.refresh(s)
		}
	}
}

// sweep is the safety pass: stat every non-ended session; grown → advance,
// shrunk → truncated → seed, missing → awaiting_file, awaiting → resolve.
func (o *Observer) sweep() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, s := range o.sessions {
		if s.Phase == PhaseEnded {
			continue
		}
		if s.Phase == PhaseAwaitingFile {
			o.refresh(s)
			continue
		}
		info, err := os.Stat(s.Path)
		if err != nil {
			s.Phase = PhaseAwaitingFile
			continue
		}
		if info.Size() != s.Size {
			o.refresh(s)
		}
	}
}
