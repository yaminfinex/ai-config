// Package store is bottle's durable storage layer: bottle directories with
// frozen transcripts and meta.json, the registry (names → versions → bottle
// ids, plus the decants map), name/version resolution, and the git
// substrate underneath the store root.
//
// Resolution lives here, not in refs: refs parses name[@version] strings,
// the store resolves them against the registry.
package store

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"ai-config/tools/bottle/internal/refs"
)

// Store is one bottle store rooted at a directory (by default ~/.bottles).
// All methods are safe for concurrent use; mutations are serialized by a
// flock and persisted with atomic writes.
type Store struct {
	root    string
	backend Backend
	warn    io.Writer
	now     func() time.Time

	gitAbsentOnce sync.Once
	config        storeConfig
}

// Option adjusts a Store at Open time.
type Option func(*Store)

// WithWarnWriter redirects best-effort warnings (git substrate degradation)
// away from the default stderr.
func WithWarnWriter(w io.Writer) Option {
	return func(s *Store) { s.warn = w }
}

// WithClock substitutes the time source, for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// DefaultRoot is the conventional store location, $HOME/.bottles. Callers
// that want a different location (tests especially) pass their own root to
// Open and never touch it.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home directory for the default ~/.bottles store: %w", err)
	}
	return filepath.Join(home, ".bottles"), nil
}

// Open opens (creating if needed) the store rooted at root. The root and
// everything under it is mode 0700 — transcripts can contain keys, PII, and
// proprietary code.
func Open(root string, opts ...Option) (*Store, error) {
	s := &Store{
		root:    root,
		backend: &fsBackend{root: root},
		warn:    os.Stderr,
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := s.loadConfig(); err != nil {
		return nil, err
	}
	return s, nil
}

// Root returns the store's root directory.
func (s *Store) Root() string { return s.root }

// Bottle is a stored bottle: its id plus the parsed meta.json.
type Bottle struct {
	ID   string
	Meta Meta
}

// CreateRequest is the input to Create. The transcript is the already
// truncated and rewritten JSONL (transcript surgery is the transcript
// package's job); lineage and compaction annotations are computed by the
// caller and persisted verbatim.
type CreateRequest struct {
	Name       string
	Transcript []byte
	Source     Source
	Parent     *Parent
	Note       string

	InheritedLines             int
	Compacted                  bool
	CompactionReachesInherited bool
	RewoundIntoParent          bool
}

// Create freezes a new bottle: writes its transcript and meta.json under
// store/<id>/ and registers it in the registry. A name that already exists
// bumps the version; nothing is ever overwritten.
func (s *Store) Create(req CreateRequest) (*Bottle, error) {
	if err := refs.ValidateName(req.Name); err != nil {
		return nil, err
	}
	var b *Bottle
	err := s.mutate("create "+req.Name, func(reg *registry) error {
		version := 1
		if latest, ok := reg.latest(req.Name); ok {
			version = latest.Version + 1
		}
		id, err := s.newID(reg)
		if err != nil {
			return err
		}
		meta := Meta{
			Name:                       req.Name,
			Version:                    version,
			Created:                    s.now().UTC(),
			Note:                       req.Note,
			Source:                     req.Source,
			Parent:                     req.Parent,
			InheritedLines:             req.InheritedLines,
			Compacted:                  req.Compacted,
			CompactionReachesInherited: req.CompactionReachesInherited,
			RewoundIntoParent:          req.RewoundIntoParent,
		}
		if err := s.backend.Write(bottlePath(id, "transcript.jsonl"), req.Transcript); err != nil {
			return err
		}
		if err := s.writeMeta(id, meta); err != nil {
			return err
		}
		reg.Names[req.Name] = append(reg.Names[req.Name], versionEntry{Version: version, BottleID: id})
		b = &Bottle{ID: id, Meta: meta}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Resolve resolves a parsed reference against the registry: an unpinned ref
// resolves to the latest version, a pinned ref to that exact version.
func (s *Store) Resolve(r refs.Ref) (*Bottle, error) {
	reg, err := s.readRegistry()
	if err != nil {
		return nil, err
	}
	entry, err := reg.resolve(r)
	if err != nil {
		return nil, err
	}
	return s.Get(entry.BottleID)
}

// Get loads a bottle by id.
func (s *Store) Get(id string) (*Bottle, error) {
	raw, err := s.backend.Read(bottlePath(id, "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("bottle %s: %w", id, err)
	}
	var meta Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("bottle %s: corrupt meta.json: %w", id, err)
	}
	return &Bottle{ID: id, Meta: meta}, nil
}

// ReadTranscript returns a bottle's frozen transcript.
func (s *Store) ReadTranscript(id string) ([]byte, error) {
	return s.backend.Read(bottlePath(id, "transcript.jsonl"))
}

// LogEntry is one version in a name's provenance chain, newest first —
// the data behind `bottle log`.
type LogEntry struct {
	Version                    int
	BottleID                   string
	Created                    time.Time
	Note                       string
	Compacted                  bool
	CompactionReachesInherited bool
	RewoundIntoParent          bool
	Parent                     *ParentRef
}

// ParentRef is a log entry's resolved parent. Name/Version are the parent's
// current identity (parents are tracked by bottle id, so lineage survives
// renames); Deleted marks a parent bottle that has since been removed.
type ParentRef struct {
	BottleID        string
	DecantSessionID string
	Name            string
	Version         int
	Deleted         bool
}

// Display renders the parent for log output: name@version, or "(deleted)"
// when the parent bottle no longer exists.
func (p ParentRef) Display() string {
	if p.Deleted {
		return "(deleted)"
	}
	return fmt.Sprintf("%s@%d", p.Name, p.Version)
}

// Log returns the provenance chain for a name, newest version first.
func (s *Store) Log(name string) ([]LogEntry, error) {
	reg, err := s.readRegistry()
	if err != nil {
		return nil, err
	}
	entries := reg.Names[name]
	if len(entries) == 0 {
		return nil, fmt.Errorf("no bottle named %q", name)
	}
	sorted := append([]versionEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version > sorted[j].Version })

	log := make([]LogEntry, 0, len(sorted))
	for _, e := range sorted {
		b, err := s.Get(e.BottleID)
		if err != nil {
			return nil, err
		}
		entry := LogEntry{
			Version:                    b.Meta.Version,
			BottleID:                   b.ID,
			Created:                    b.Meta.Created,
			Note:                       b.Meta.Note,
			Compacted:                  b.Meta.Compacted,
			CompactionReachesInherited: b.Meta.CompactionReachesInherited,
			RewoundIntoParent:          b.Meta.RewoundIntoParent,
		}
		if p := b.Meta.Parent; p != nil {
			ref := &ParentRef{BottleID: p.BottleID, DecantSessionID: p.DecantSessionID}
			if parent, err := s.Get(p.BottleID); err == nil {
				ref.Name = parent.Meta.Name
				ref.Version = parent.Meta.Version
			} else {
				ref.Deleted = true
			}
			entry.Parent = ref
		}
		log = append(log, entry)
	}
	return log, nil
}

func (s *Store) writeMeta(id string, meta Meta) error {
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return s.backend.Write(bottlePath(id, "meta.json"), append(raw, '\n'))
}

func bottlePath(id string, parts ...string) string {
	return path.Join(append([]string{"store", id}, parts...)...)
}

// idAlphabet is base36: bottle ids are 8 random base36 chars, not content
// hashes.
const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// newID picks an unused 8-char base36 id. Called under the mutation lock,
// so the uniqueness check against the registry cannot race.
func (s *Store) newID(reg *registry) (string, error) {
	max := big.NewInt(int64(len(idAlphabet)))
	for range 100 {
		buf := make([]byte, 8)
		for i := range buf {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", err
			}
			buf[i] = idAlphabet[n.Int64()]
		}
		id := string(buf)
		if reg.hasBottleID(id) {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.root, "store", id)); err == nil {
			continue
		}
		return id, nil
	}
	return "", fmt.Errorf("could not allocate an unused bottle id")
}
