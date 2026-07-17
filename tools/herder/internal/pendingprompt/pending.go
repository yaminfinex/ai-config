// Package pendingprompt owns the bounded hand-off of an initial prompt that
// outlives spawn's bus-bind window.
package pendingprompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	defaultLifetime = 24 * time.Hour
	markerLifetime  = 10 * time.Minute
)

type Actor string

const (
	ActorManual  Actor = "manual"
	ActorSidecar Actor = "sidecar"
)

// Record contains the delivery coordinates spawn already verified plus the
// prompt that did not fit inside its bind window. The file is mode 0600.
type Record struct {
	Version   int       `json:"version"`
	GUID      string    `json:"guid"`
	Sender    string    `json:"sender"`
	BusDir    string    `json:"bus_dir"`
	Message   string    `json:"message"`
	VerifyMS  int       `json:"verify_ms"`
	ExpiresAt time.Time `json:"expires_at"`
}

type marker struct {
	Version   int       `json:"version"`
	GUID      string    `json:"guid"`
	Digest    string    `json:"digest"`
	Actor     Actor     `json:"actor"`
	ExpiresAt time.Time `json:"expires_at"`
}

type AttemptResult struct {
	Managed    bool
	Suppressed bool
	Verdict    string
}

// Store persists one pending prompt atomically. A later store for the same
// child replaces only an identical hand-off; conflicting content is refused.
func Store(registryPath string, record Record, now time.Time) error {
	if record.GUID == "" || record.Sender == "" || record.Message == "" {
		return errors.New("pending prompt requires child guid, verified sender, and message")
	}
	if record.Version == 0 {
		record.Version = 1
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = now.Add(defaultLifetime)
	}
	if err := PruneAll(registryPath, now); err != nil {
		return err
	}
	return withLock(registryPath, record.GUID, func(paths statePaths) error {
		_ = removeExpired(paths, now)
		if existing, err := readRecord(paths.pending); err == nil {
			if existing.GUID != record.GUID || digest(existing.Message) != digest(record.Message) {
				return errors.New("a different pending prompt already exists for this child")
			}
		}
		_ = os.Remove(paths.marker)
		return writeJSONAtomic(paths.pending, record)
	})
}

// PruneAll is the observer/spawn GC hook for hand-offs whose child never
// acquired a canonical seat and therefore cannot be selected by cull.
func PruneAll(registryPath string, now time.Time) error {
	dir := filepath.Join(filepath.Dir(registryPath), "pending-prompts")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var envelope struct {
			GUID      string    `json:"guid"`
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err := readJSON(filepath.Join(dir, entry.Name()), &envelope); err != nil || envelope.GUID == "" || envelope.ExpiresAt.After(now) || seen[envelope.GUID] {
			continue
		}
		seen[envelope.GUID] = true
		if err := Prune(registryPath, envelope.GUID, now); err != nil {
			return err
		}
	}
	return nil
}

// Attempt serializes manual and sidecar delivery for one child. A successful
// delivery removes the plaintext pending record and leaves only a short-lived
// digest marker so the competing actor can suppress one blind duplicate.
func Attempt(registryPath, guid, message string, actor Actor, now time.Time, deliver func(Record) string) (AttemptResult, error) {
	result := AttemptResult{}
	if guid == "" || (actor == ActorManual && message == "") || (actor != ActorManual && actor != ActorSidecar) {
		return result, nil
	}
	err := withLock(registryPath, guid, func(paths statePaths) error {
		_ = removeExpired(paths, now)
		wantDigest := digest(message)
		if delivered, err := readMarker(paths.marker); err == nil && delivered.GUID == guid && (actor == ActorSidecar || delivered.Digest == wantDigest) {
			result.Managed = true
			result.Suppressed = true
			result.Verdict = "already_delivered"
			if delivered.Actor != actor {
				_ = os.Remove(paths.marker)
			}
			return nil
		}

		pending, err := readRecord(paths.pending)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if actor == ActorSidecar {
			wantDigest = digest(pending.Message)
		}
		if pending.GUID != guid || digest(pending.Message) != wantDigest {
			return nil
		}
		result.Managed = true
		if deliver == nil {
			return errors.New("pending prompt delivery callback is missing")
		}
		result.Verdict = deliver(pending)
		if result.Verdict != "delivered" && result.Verdict != "queued" {
			return nil
		}
		if err := os.Remove(paths.pending); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		markerExpiry := pending.ExpiresAt
		if minimum := now.Add(markerLifetime); markerExpiry.Before(minimum) {
			markerExpiry = minimum
		}
		delivered := marker{Version: 1, GUID: guid, Digest: wantDigest, Actor: actor, ExpiresAt: markerExpiry}
		return writeJSONAtomic(paths.marker, delivered)
	})
	return result, err
}

// Prune removes expired plaintext and delivery markers even when the child
// never becomes correlatable.
func Prune(registryPath, guid string, now time.Time) error {
	if guid == "" {
		return nil
	}
	return withLock(registryPath, guid, func(paths statePaths) error {
		return removeExpired(paths, now)
	})
}

// Cleanup removes all hand-off state when the corresponding seat is unseated.
func Cleanup(registryPath, guid string) error {
	if guid == "" {
		return nil
	}
	return withLock(registryPath, guid, func(paths statePaths) error {
		var joined error
		for _, path := range []string{paths.pending, paths.marker} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				joined = errors.Join(joined, err)
			}
		}
		return joined
	})
}

type statePaths struct {
	pending string
	marker  string
	lock    string
}

func pathsFor(registryPath, guid string) statePaths {
	sum := sha256.Sum256([]byte(guid))
	stem := hex.EncodeToString(sum[:16])
	dir := filepath.Join(filepath.Dir(registryPath), "pending-prompts")
	return statePaths{
		pending: filepath.Join(dir, stem+".json"),
		marker:  filepath.Join(dir, stem+".delivered.json"),
		lock:    filepath.Join(dir, stem+".lock"),
	}
}

func withLock(registryPath, guid string, fn func(statePaths) error) error {
	paths := pathsFor(registryPath, guid)
	if err := os.MkdirAll(filepath.Dir(paths.lock), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(paths.lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	return fn(paths)
}

func removeExpired(paths statePaths, now time.Time) error {
	var joined error
	if record, err := readRecord(paths.pending); err == nil && !record.ExpiresAt.After(now) {
		joined = errors.Join(joined, removeIfPresent(paths.pending))
	}
	if delivered, err := readMarker(paths.marker); err == nil && !delivered.ExpiresAt.After(now) {
		joined = errors.Join(joined, removeIfPresent(paths.marker))
	}
	return joined
}

func removeIfPresent(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func readRecord(path string) (Record, error) {
	var record Record
	err := readJSON(path, &record)
	return record, err
}

func readMarker(path string) (marker, error) {
	var delivered marker
	err := readJSON(path, &delivered)
	return delivered, err
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode pending prompt state: %w", err)
	}
	return nil
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pending-prompt-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func digest(message string) string {
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:])
}
