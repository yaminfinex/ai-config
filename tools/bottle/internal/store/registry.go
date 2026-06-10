package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"ai-config/tools/bottle/internal/refs"
)

const (
	registryFile = "registry.json"
	lockFile     = "registry.lock"
)

// versionEntry is one version of a name in the registry, per the origin
// spec's `name → [{version, bottle_id}]` schema.
type versionEntry struct {
	Version  int    `json:"version"`
	BottleID string `json:"bottle_id"`
}

// Decant is one entry in the registry's decants map: which bottle a decanted
// session was poured from, and when.
type Decant struct {
	BottleID string    `json:"bottle_id"`
	Created  time.Time `json:"created"`
}

// registry is the on-disk registry.json: names → versions → bottle ids,
// plus the decants map (session id → Decant) that rebottle resolves parents
// through.
type registry struct {
	Names   map[string][]versionEntry `json:"names"`
	Decants map[string]Decant         `json:"decants"`
}

func newRegistry() *registry {
	return &registry{
		Names:   map[string][]versionEntry{},
		Decants: map[string]Decant{},
	}
}

// latest returns the highest version entry for name, or ok=false when the
// name has no versions.
func (r *registry) latest(name string) (versionEntry, bool) {
	var best versionEntry
	for _, e := range r.Names[name] {
		if e.Version > best.Version {
			best = e
		}
	}
	return best, best.Version > 0
}

// resolve maps a parsed ref to its registry entry: latest version when
// unpinned, the exact version when pinned.
func (r *registry) resolve(ref refs.Ref) (versionEntry, error) {
	entries := r.Names[ref.Name]
	if len(entries) == 0 {
		return versionEntry{}, fmt.Errorf("no bottle named %q", ref.Name)
	}
	if !ref.Pinned() {
		latest, _ := r.latest(ref.Name)
		return latest, nil
	}
	for _, e := range entries {
		if e.Version == ref.Version {
			return e, nil
		}
	}
	return versionEntry{}, fmt.Errorf("%s has no version %d", ref.Name, ref.Version)
}

func (r *registry) hasBottleID(id string) bool {
	for _, entries := range r.Names {
		for _, e := range entries {
			if e.BottleID == id {
				return true
			}
		}
	}
	return false
}

// readRegistry loads registry.json; a store with no registry yet reads as
// empty. Reads take no lock: AtomicSwap guarantees a coherent file.
func (s *Store) readRegistry() (*registry, error) {
	raw, err := s.backend.Read(registryFile)
	if os.IsNotExist(err) {
		return newRegistry(), nil
	}
	if err != nil {
		return nil, err
	}
	reg := newRegistry()
	if err := json.Unmarshal(raw, reg); err != nil {
		return nil, fmt.Errorf("corrupt %s: %w", registryFile, err)
	}
	return reg, nil
}

func (s *Store) writeRegistry(reg *registry) error {
	raw, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return s.backend.AtomicSwap(registryFile, append(raw, '\n'))
}

// lockExclusive takes the store-wide mutation lock: a flock on a lockfile
// beside the registry. It serializes registry read-modify-write cycles (and
// the git auto-commit) across goroutines and processes; the atomic swap
// alone would prevent torn files but not lost updates.
func (s *Store) lockExclusive() (unlock func(), err error) {
	f, err := os.OpenFile(filepath.Join(s.root, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", lockFile, err)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// mutate runs fn over the registry under the exclusive lock, persists the
// result atomically, and auto-commits to the git substrate (best-effort).
// All registry mutations go through here.
func (s *Store) mutate(commitMsg string, fn func(reg *registry) error) error {
	unlock, err := s.lockExclusive()
	if err != nil {
		return err
	}
	defer unlock()
	reg, err := s.readRegistry()
	if err != nil {
		return err
	}
	if err := fn(reg); err != nil {
		return err
	}
	if err := s.writeRegistry(reg); err != nil {
		return err
	}
	s.autoCommit(commitMsg)
	return nil
}
