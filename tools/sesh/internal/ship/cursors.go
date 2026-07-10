package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"sesh/internal/wire"
)

// SchemaGeneration is the cursor-registry schema this binary reads and
// writes. A registry file carrying a HIGHER generation was written by a newer
// sesh build: this writer refuses (see NewerRegistryError) rather than risk
// destroying state it does not understand.
const SchemaGeneration = 1

const (
	registryFileName = "cursors.json"
	lockFileName     = "cursors.lock"
)

// Cursor is one tracked file identity's shipping state. Offset advances only
// on the store's durable ACK; the path is advisory (last place the identity
// was seen), never identity.
type Cursor struct {
	Tool        wire.Tool `json:"tool"`
	SessionID   string    `json:"session_id"`
	FileUUID    string    `json:"file_uuid"`
	Path        string    `json:"path"`
	Offset      int64     `json:"offset"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Poisoned    bool      `json:"poisoned,omitempty"`
	LastAckAt   time.Time `json:"last_ack_at,omitempty"`
	// SessionOwner records an observed SESSION_OWNER correlation for this
	// session. Once observed it is never retracted by process death (I8).
	// Written by the U9 correlation unit; carried here so the registry
	// schema does not change under it.
	SessionOwner string `json:"session_owner,omitempty"`
}

// Identity returns the cursor's file identity.
func (c Cursor) Identity() Identity {
	return Identity{Tool: c.Tool, SessionID: c.SessionID, FileUUID: c.FileUUID}
}

type registryFile struct {
	SchemaGeneration int               `json:"schema_generation"`
	Cursors          map[string]Cursor `json:"cursors"`
}

// NewerRegistryError is the typed refusal when the registry file was written
// by a newer schema generation than this binary understands. Its text names
// the cause and the remedy and must never advise deleting or moving the
// registry: deleting it would silently discard the newer build's state and
// re-ship the world (the herder registry incident class, backlog tasks
// 083/084).
type NewerRegistryError struct {
	Path             string
	FileGeneration   int
	BinaryGeneration int
}

func (e *NewerRegistryError) Error() string {
	return fmt.Sprintf(
		"cursor registry %s carries schema generation %d but this sesh build only understands generation %d: "+
			"this binary is older than the registry (likely cause: an outdated sesh build on this node). "+
			"Remedy: run the newer sesh build that wrote the registry, or upgrade this installation and retry. "+
			"The registry file has been left untouched.",
		e.Path, e.FileGeneration, e.BinaryGeneration)
}

// LockedRegistryError is the typed refusal when another process holds the
// registry lock — one shipper per OS user, and the lifetime flock is the
// single-instance lock.
type LockedRegistryError struct{ Path string }

func (e *LockedRegistryError) Error() string {
	return fmt.Sprintf("cursor registry lock %s is held by another process: another sesh ship instance is already running for this user", e.Path)
}

// Registry is the single per-user cursor state file: JSON, written
// atomically (temp + fsync + rename) while an exclusive flock on a sidecar
// lock file is held for the daemon's lifetime.
type Registry struct {
	dir                 string
	lockFile            *os.File
	cursors             map[string]Cursor
	dirty               bool
	batchDepth          int
	durableReplacements uint64
	// NeedsRecovery is true when the registry was missing or unreadable at
	// open: cursors must be rebuilt from rescan + recovery GETs before
	// shipping resumes. An unreadable file is renamed aside (never deleted).
	NeedsRecovery bool
}

// OpenRegistry opens (or creates) the per-user registry under dir, taking
// the exclusive lifetime flock. It fails typed on a held lock or a
// newer-generation file; a missing or unreadable file yields an empty
// registry with NeedsRecovery set.
func OpenRegistry(dir string) (*Registry, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dir, lockFileName)
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lf.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, &LockedRegistryError{Path: lockPath}
		}
		// flock unavailable (e.g. some network mounts): refuse rather than
		// proceed unlocked (herder-spec 5.2 discipline).
		return nil, fmt.Errorf("cannot acquire registry lock %s: %w (refusing to run unlocked)", lockPath, err)
	}
	r := &Registry{dir: dir, lockFile: lf, cursors: map[string]Cursor{}}

	path := filepath.Join(dir, registryFileName)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		r.NeedsRecovery = true
		return r, nil
	}
	if err != nil {
		r.Close()
		return nil, err
	}
	var f registryFile
	if jerr := json.Unmarshal(raw, &f); jerr != nil || f.SchemaGeneration == 0 {
		// Unreadable = lost: rename aside (never delete) and rebuild.
		aside := path + ".unreadable-" + time.Now().UTC().Format("20060102T150405Z")
		if rerr := os.Rename(path, aside); rerr != nil {
			r.Close()
			return nil, fmt.Errorf("cursor registry %s is unreadable and could not be set aside: %w", path, rerr)
		}
		r.NeedsRecovery = true
		return r, nil
	}
	if f.SchemaGeneration > SchemaGeneration {
		r.Close()
		return nil, &NewerRegistryError{Path: path, FileGeneration: f.SchemaGeneration, BinaryGeneration: SchemaGeneration}
	}
	if f.Cursors != nil {
		r.cursors = f.Cursors
	}
	return r, nil
}

// Close releases the lifetime lock.
func (r *Registry) Close() {
	if r.lockFile != nil {
		_ = syscall.Flock(int(r.lockFile.Fd()), syscall.LOCK_UN)
		r.lockFile.Close()
		r.lockFile = nil
	}
}

// Get returns the cursor for an identity.
func (r *Registry) Get(id Identity) (Cursor, bool) {
	c, ok := r.cursors[id.Key()]
	return c, ok
}

// All returns a snapshot of every cursor.
func (r *Registry) All() []Cursor {
	out := make([]Cursor, 0, len(r.cursors))
	for _, c := range r.cursors {
		out = append(out, c)
	}
	return out
}

// LoadSnapshot reads the cursor registry without taking the daemon lock. It
// is for status reporting only and never writes or repairs the file.
func LoadSnapshot(dir string) ([]Cursor, error) {
	raw, err := os.ReadFile(filepath.Join(dir, registryFileName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f registryFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if f.SchemaGeneration > SchemaGeneration {
		return nil, &NewerRegistryError{Path: filepath.Join(dir, registryFileName), FileGeneration: f.SchemaGeneration, BinaryGeneration: SchemaGeneration}
	}
	out := make([]Cursor, 0, len(f.Cursors))
	for _, c := range f.Cursors {
		out = append(out, c)
	}
	return out, nil
}

// beginBatch defers durable persistence until the matching endBatch. Cursor
// mutations remain immediately visible in memory; callers must only make
// authoritative state transitions before invoking Put or Delete.
func (r *Registry) beginBatch() {
	r.batchDepth++
}

// endBatch closes one batch and durably persists all accumulated mutations
// when the outermost batch ends.
func (r *Registry) endBatch() error {
	if r.batchDepth == 0 {
		return errors.New("cursor registry batch ended without a matching begin")
	}
	r.batchDepth--
	if r.batchDepth > 0 {
		return nil
	}
	return r.flush()
}

// Put upserts a cursor. Outside a batch it persists immediately; inside a
// batch the outermost endBatch performs the durable replacement.
func (r *Registry) Put(c Cursor) error {
	r.cursors[c.Identity().Key()] = c
	r.dirty = true
	if r.batchDepth > 0 {
		return nil
	}
	return r.flush()
}

// Delete GCs a cursor (file deletion; the mirror retains). Persistence uses
// the same immediate-or-batched rule as Put.
func (r *Registry) Delete(id Identity) error {
	delete(r.cursors, id.Key())
	r.dirty = true
	if r.batchDepth > 0 {
		return nil
	}
	return r.flush()
}

func (r *Registry) flush() error {
	if !r.dirty {
		return nil
	}
	if err := r.save(); err != nil {
		return err
	}
	r.dirty = false
	r.durableReplacements++
	return nil
}

func (r *Registry) save() error {
	f := registryFile{SchemaGeneration: SchemaGeneration, Cursors: r.cursors}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(r.dir, registryFileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(r.dir, registryFileName)); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// fsync the directory so the rename itself is durable. Every syscall in
	// the durability chain failing fails the save (R23 / herder-spec 5.2:
	// a hidden storage failure is exactly what the discipline exists to
	// surface); at-least-once replay makes a failed-but-actually-durable
	// save safe.
	d, err := os.Open(r.dir)
	if err != nil {
		return fmt.Errorf("cursor registry rename may not be durable: open %s: %w", r.dir, err)
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return fmt.Errorf("cursor registry rename may not be durable: fsync %s: %w", r.dir, err)
	}
	if err := d.Close(); err != nil {
		return fmt.Errorf("cursor registry rename may not be durable: close %s: %w", r.dir, err)
	}
	return nil
}

// StateDir resolves the registry directory: ${SESH_STATE_DIR}, else
// ${XDG_STATE_HOME}/sesh, else ~/.local/state/sesh.
func StateDir() (string, error) {
	if d := os.Getenv("SESH_STATE_DIR"); d != "" {
		return d, nil
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "sesh"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "sesh"), nil
}

// ErrPoisonedLocal marks a file the store has poisoned; the shipper keeps
// its cursor frozen and stops retrying (deletion GC still applies).
var ErrPoisonedLocal = errors.New("file is poisoned; not retrying")
