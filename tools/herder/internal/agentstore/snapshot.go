package agentstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	var events []Event
	end, err := s.scanEnd(0, func(event Event, _ int64) { events = append(events, event) })
	if errors.Is(err, os.ErrNotExist) {
		return proj, nil
	}
	if err != nil {
		return nil, err
	}
	aliases := replayAliases(events)
	for _, event := range events {
		if alias, ok := aliases[event.Name]; ok && alias.contains(event.At) {
			event.Name = alias.full
		}
		for i, name := range event.Instances {
			if alias, ok := aliases[name]; ok && alias.contains(event.At) {
				event.Instances[i] = alias.full
			}
		}
		proj.Apply(event, 0)
	}
	for _, alias := range aliases {
		for _, view := range proj.Agents[alias.full] {
			if alias.contains(view.FirstSeen) && view.ManagerAt == nil && view.Provenance.LauncherKind == "user" && alias.manager != "" {
				view.Manager = alias.manager
			}
		}
	}
	proj.EventsOffset = end
	return proj, nil
}

type replayAlias struct {
	full, manager string
	start, end    time.Time
}

func (a replayAlias) contains(at time.Time) bool {
	// Windows are half-open [start,end); a zero end means still open.
	return !at.Before(a.start) && (a.end.IsZero() || at.Before(a.end))
}

// replayAliases repairs the old mirror's base-name keys without guessing
// across tags. A base is eligible only when exactly one registered launch
// window contains one of that base record's mirror.ready facts.
func replayAliases(events []Event) map[string]replayAlias {
	type candidate struct {
		replayAlias
		base string
	}
	var candidates []candidate
	requests := map[string]Event{}
	for _, event := range events {
		if event.Kind == KindLaunchRequested {
			requests[event.ID] = event
		}
	}
	for _, event := range events {
		if event.Kind != KindLaunchReady || event.Request == "" {
			continue
		}
		request, ok := requests[event.Request]
		if !ok || request.Tag == "" {
			continue
		}
		prefix := request.Tag + "-"
		if !strings.HasPrefix(event.Name, prefix) || len(event.Name) == len(prefix) {
			continue
		}
		candidates = append(candidates, candidate{replayAlias: replayAlias{full: event.Name, start: request.At}, base: strings.TrimPrefix(event.Name, prefix)})
	}
	for i := range candidates {
		for _, event := range events {
			if event.Name == candidates[i].full && closes(event.Kind) && event.At.After(candidates[i].start) && (candidates[i].end.IsZero() || event.At.Before(candidates[i].end)) {
				candidates[i].end = event.At
			}
		}
	}
	ready := map[string][]time.Time{}
	for _, event := range events {
		if event.Kind == KindMirrorReady {
			ready[event.Name] = append(ready[event.Name], event.At)
		}
	}
	byBase := map[string][]candidate{}
	for _, candidate := range candidates {
		for _, at := range ready[candidate.base] {
			if candidate.contains(at) {
				byBase[candidate.base] = append(byBase[candidate.base], candidate)
				break
			}
		}
	}
	aliases := map[string]replayAlias{}
	for base, matches := range byBase {
		if len(matches) == 1 {
			alias := matches[0].replayAlias
			for _, event := range events {
				if event.Kind == KindMirrorReady && event.Name == base && alias.contains(event.At) {
					alias.manager = event.By
					break
				}
			}
			aliases[base] = alias
		}
	}
	return aliases
}

func (s *Store) load() (*Projection, error) {
	proj, ok := s.readSnapshot()
	if !ok {
		return s.Replay()
	}
	replayTail := false
	end, err := s.scanEnd(proj.EventsOffset, func(event Event, offset int64) {
		if strings.HasPrefix(event.Kind, "mirror.") && event.Name != "" && proj.Latest(event.Name) == nil {
			replayTail = true
		}
		proj.Apply(event, offset)
	})
	if errors.Is(err, os.ErrNotExist) {
		return s.Replay()
	}
	if err != nil {
		return nil, err
	}
	if replayTail {
		return s.Replay()
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
