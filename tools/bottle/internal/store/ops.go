package store

import (
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-config/tools/bottle/internal/refs"
)

// Rename moves every version of oldName to newName in the registry. It
// refuses an existing target; nothing is ever overwritten. Bottle dirs do
// not move — only the registry entry and each version's meta.json (the old
// name is recorded in previous_names so log lineage stays continuous).
func (s *Store) Rename(oldName, newName string) error {
	if err := refs.ValidateName(newName); err != nil {
		return err
	}
	return s.mutate("rename "+oldName+" to "+newName, func(reg *registry) error {
		entries := reg.Names[oldName]
		if len(entries) == 0 {
			return fmt.Errorf("no bottle named %q", oldName)
		}
		if len(reg.Names[newName]) > 0 {
			return fmt.Errorf("a bottle named %q already exists — rename refuses to overwrite", newName)
		}
		for _, e := range entries {
			b, err := s.Get(e.BottleID)
			if err != nil {
				return err
			}
			b.Meta.PreviousNames = append(b.Meta.PreviousNames, oldName)
			b.Meta.Name = newName
			if err := s.writeMeta(e.BottleID, b.Meta); err != nil {
				return err
			}
		}
		reg.Names[newName] = entries
		delete(reg.Names, oldName)
		return nil
	})
}

// SetNote sets or replaces a bottle's free-text note — the one sanctioned
// mutation of bottle metadata. An unpinned ref edits the latest version.
func (s *Store) SetNote(r refs.Ref, note string) error {
	return s.mutate("note "+r.String(), func(reg *registry) error {
		entry, err := reg.resolve(r)
		if err != nil {
			return err
		}
		b, err := s.Get(entry.BottleID)
		if err != nil {
			return err
		}
		b.Meta.Note = note
		return s.writeMeta(entry.BottleID, b.Meta)
	})
}

// RecordDecant registers a freshly materialized decant session against the
// bottle it was poured from, timestamped so prune and log can stay honest
// about dead decants.
func (s *Store) RecordDecant(sessionID, bottleID string) error {
	if sessionID == "" || bottleID == "" {
		return fmt.Errorf("RecordDecant needs both a session id and a bottle id")
	}
	return s.mutate("decant "+bottleID+" as session "+sessionID, func(reg *registry) error {
		reg.Decants[sessionID] = Decant{BottleID: bottleID, Created: s.now().UTC()}
		return nil
	})
}

// LookupDecant reports which bottle a session was decanted from — how
// rebottle auto-resolves parents.
func (s *Store) LookupDecant(sessionID string) (Decant, bool, error) {
	reg, err := s.readRegistry()
	if err != nil {
		return Decant{}, false, err
	}
	d, ok := reg.Decants[sessionID]
	return d, ok, nil
}

// Decants returns a copy of the whole decants map (session id → Decant).
func (s *Store) Decants() (map[string]Decant, error) {
	reg, err := s.readRegistry()
	if err != nil {
		return nil, err
	}
	return maps.Clone(reg.Decants), nil
}

// RemoveDecants drops decants-map entries; prune uses it after the caller
// has checked which session files no longer exist.
func (s *Store) RemoveDecants(sessionIDs ...string) error {
	return s.mutate("prune decants", func(reg *registry) error {
		for _, id := range sessionIDs {
			delete(reg.Decants, id)
		}
		return nil
	})
}

// Remove deletes one version (pinned ref) or every version of a name
// (unpinned ref): registry entries and bottle dirs both. Versions held as
// parents elsewhere are not protected — their children's log shows
// "(deleted)". Note the git substrate retains removed transcripts in
// history; the rm command owns warning about that.
func (s *Store) Remove(r refs.Ref) error {
	return s.mutate("rm "+r.String(), func(reg *registry) error {
		entries := reg.Names[r.Name]
		if len(entries) == 0 {
			return fmt.Errorf("no bottle named %q", r.Name)
		}
		var doomed, kept []versionEntry
		if r.Pinned() {
			for _, e := range entries {
				if e.Version == r.Version {
					doomed = append(doomed, e)
				} else {
					kept = append(kept, e)
				}
			}
			if len(doomed) == 0 {
				return fmt.Errorf("%s has no version %d", r.Name, r.Version)
			}
		} else {
			doomed = entries
		}
		if len(kept) == 0 {
			delete(reg.Names, r.Name)
		} else {
			reg.Names[r.Name] = kept
		}
		for _, e := range doomed {
			if err := s.backend.Delete(path.Join("store", e.BottleID)); err != nil {
				return err
			}
		}
		return nil
	})
}

// NameInfo is one row of `bottle list`: a name with its latest version,
// version count, and the latest version's age and note.
type NameInfo struct {
	Name     string
	Latest   int
	Versions int
	Created  time.Time
	Note     string
}

// List summarizes every name in the registry, sorted by name. An empty
// store lists as empty, not as an error.
func (s *Store) List() ([]NameInfo, error) {
	reg, err := s.readRegistry()
	if err != nil {
		return nil, err
	}
	infos := make([]NameInfo, 0, len(reg.Names))
	for name, entries := range reg.Names {
		latest, _ := reg.latest(name)
		b, err := s.Get(latest.BottleID)
		if err != nil {
			return nil, err
		}
		infos = append(infos, NameInfo{
			Name:     name,
			Latest:   latest.Version,
			Versions: len(entries),
			Created:  b.Meta.Created,
			Note:     b.Meta.Note,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// AddArtifact attaches a file to a bottle under store/<id>/artifacts/,
// keyed by its cwd-relative path.
func (s *Store) AddArtifact(id, relPath string, data []byte) error {
	clean, err := safeArtifactPath(relPath)
	if err != nil {
		return err
	}
	return s.mutate("attach "+clean+" to "+id, func(reg *registry) error {
		if !reg.hasBottleID(id) {
			return fmt.Errorf("no bottle with id %s", id)
		}
		return s.backend.Write(bottlePath(id, "artifacts", clean), data)
	})
}

// Artifacts lists a bottle's attached files as relative paths, sorted.
func (s *Store) Artifacts(id string) ([]string, error) {
	return s.backend.List(bottlePath(id, "artifacts"))
}

// ReadArtifact returns one attached file's content.
func (s *Store) ReadArtifact(id, relPath string) ([]byte, error) {
	clean, err := safeArtifactPath(relPath)
	if err != nil {
		return nil, err
	}
	return s.backend.Read(bottlePath(id, "artifacts", clean))
}

// safeArtifactPath normalizes an artifact's relative path and refuses
// anything that would escape the artifacts directory.
func safeArtifactPath(p string) (string, error) {
	clean := path.Clean(filepath.ToSlash(p))
	if path.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("artifact path %q escapes the artifacts directory", p)
	}
	return clean, nil
}
