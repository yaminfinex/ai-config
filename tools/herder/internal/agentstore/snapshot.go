package agentstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Load returns the projection: snapshot.json plus the events tail when the
// snapshot's offset still describes the file, else a full replay. It then
// rewrites the snapshot; a rewrite failure is never an error for a read
// (SnapshotErr carries it for callers that care).
func (s *Store) Load() (*Projection, error) {
	proj, err := s.load()
	if err != nil {
		return nil, err
	}
	proj.SnapshotErr = s.writeSnapshot(proj)
	return proj, nil
}

// LoadNoSnapshot replays without touching snapshot.json (one-shot reads, tests, diffing).
func (s *Store) LoadNoSnapshot() (*Projection, error) { return s.load() }

// Replay ignores the snapshot and folds every event from byte 0.
func (s *Store) Replay() (*Projection, error) {
	proj := NewProjection()
	end, err := s.scanEnd(0, proj.Apply)
	if errors.Is(err, os.ErrNotExist) {
		return proj, nil
	}
	if err != nil {
		return nil, err
	}
	proj.EventsOffset = end
	return proj, nil
}

func (s *Store) load() (*Projection, error) {
	proj, ok := s.readSnapshot()
	if !ok {
		return s.Replay()
	}
	end, err := s.scanEnd(proj.EventsOffset, proj.Apply)
	if errors.Is(err, os.ErrNotExist) {
		return s.Replay()
	}
	if err != nil {
		return nil, err
	}
	proj.EventsOffset = end
	return proj, nil
}

// readSnapshot accepts the snapshot only when its offset points at a record
// boundary of the current file; a shorter file (rotation, repair below the
// offset) or a mid-record offset means full replay.
func (s *Store) readSnapshot() (*Projection, bool) {
	raw, err := os.ReadFile(s.SnapshotPath())
	if err != nil {
		return nil, false
	}
	proj := NewProjection()
	if err := json.Unmarshal(raw, proj); err != nil || proj.Version != ProjectionVersion {
		return nil, false
	}
	file, err := os.Open(s.EventsPath())
	if err != nil {
		return nil, false
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || proj.EventsOffset > stat.Size() || proj.EventsOffset < 0 {
		return nil, false
	}
	if proj.EventsOffset > 0 {
		last := make([]byte, 1)
		if _, err := file.ReadAt(last, proj.EventsOffset-1); err != nil && err != io.EOF || last[0] != '\n' {
			return nil, false
		}
	}
	return proj, true
}

// writeSnapshot is temp+rename so a reader never sees a half-written file.
func (s *Store) writeSnapshot(proj *Projection) error {
	encoded, err := proj.Marshal()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".snapshot-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, filepath.Join(s.Dir, "snapshot.json")); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("rename snapshot: %w", err)
	}
	return nil
}
