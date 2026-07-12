// Package continuationstate stores detached compact continuation outcomes.
// The records deliberately live beside, but never inside, the session registry.
package continuationstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-config/tools/herder/internal/registry"
)

const (
	Schema         = "herder.continuation.v1"
	dirName        = "continuations"
	archiveDirName = "archive"
)

type Record struct {
	Schema          string       `json:"schema"`
	ID              string       `json:"id"`
	Status          string       `json:"status"`
	Target          string       `json:"target"`
	UpdatedAt       string       `json:"updated_at"`
	Reason          string       `json:"reason,omitempty"`
	LogPath         string       `json:"log_path"`
	RecoveryCommand string       `json:"recovery_command"`
	AcknowledgedAt  string       `json:"acknowledged_at,omitempty"`
	Lifecycle       []Transition `json:"lifecycle"`
}

type Transition struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Reason    string `json:"reason,omitempty"`
}

func DefaultDir() string {
	return filepath.Join(filepath.Dir(registry.DefaultPath()), dirName)
}

func Write(dir string, rec Record) error {
	if !safeID(rec.ID) {
		return fmt.Errorf("invalid continuation id %q", rec.ID)
	}
	if dir == "" {
		dir = DefaultDir()
	}
	rec.Schema = Schema
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, ".continuation.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, rec.ID+".json"))
}

// Advance persists a new lifecycle state while retaining prior transitions.
// A missing or unreadable predecessor does not prevent a terminal outcome from
// becoming independently useful: the supplied transition is still written.
func Advance(dir string, rec Record) error {
	if dir == "" {
		dir = DefaultDir()
	}
	if safeID(rec.ID) {
		if b, err := os.ReadFile(filepath.Join(dir, rec.ID+".json")); err == nil {
			var prior Record
			if json.Unmarshal(b, &prior) == nil && prior.Schema == Schema && prior.ID == rec.ID {
				rec.Lifecycle = append(rec.Lifecycle, prior.Lifecycle...)
			}
		}
	}
	rec.Lifecycle = append(rec.Lifecycle, Transition{
		Status: rec.Status, Timestamp: rec.UpdatedAt, Reason: rec.Reason,
	})
	if err := Write(dir, rec); err != nil {
		return err
	}
	if rec.Status == "delivered" || rec.Status == "queued" {
		return archive(dir, rec.ID)
	}
	return nil
}

// ReadAll returns every parseable hot record. A malformed, foreign, or
// unreadable individual JSON file is reported as a warning and skipped so it
// cannot hide valid failures. Directory-level failures remain fatal.
func ReadAll(dir string) ([]Record, []error, error) {
	if dir == "" {
		dir = DefaultDir()
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var records []Record
	var warnings []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			warnings = append(warnings, fmt.Errorf("read continuation %s: %w", entry.Name(), err))
			continue
		}
		var rec Record
		if err := json.Unmarshal(b, &rec); err != nil {
			warnings = append(warnings, fmt.Errorf("read continuation %s: %w", entry.Name(), err))
			continue
		}
		if rec.Schema != Schema || !safeID(rec.ID) || entry.Name() != rec.ID+".json" {
			warnings = append(warnings, fmt.Errorf("read continuation %s: unsupported or invalid record", entry.Name()))
			continue
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].UpdatedAt == records[j].UpdatedAt {
			return records[i].ID < records[j].ID
		}
		return records[i].UpdatedAt < records[j].UpdatedAt
	})
	return records, warnings, nil
}

func Unresolved(dir string) ([]Record, []error, error) {
	records, warnings, err := ReadAll(dir)
	if err != nil {
		return nil, warnings, err
	}
	out := records[:0]
	for _, rec := range records {
		if rec.Status == "failed" && rec.AcknowledgedAt == "" {
			out = append(out, rec)
		}
	}
	return out, warnings, nil
}

func Acknowledge(dir, id string, now time.Time) (Record, error) {
	if !safeID(id) {
		return Record{}, fmt.Errorf("invalid continuation id %q", id)
	}
	if dir == "" {
		dir = DefaultDir()
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		return Record{}, err
	}
	if rec.Schema != Schema || rec.ID != id {
		return Record{}, fmt.Errorf("continuation %s has invalid record identity", id)
	}
	if rec.Status != "failed" {
		return Record{}, fmt.Errorf("continuation %s is %s, not an unresolved failure", id, rec.Status)
	}
	if rec.AcknowledgedAt == "" {
		rec.AcknowledgedAt = now.UTC().Format(time.RFC3339)
		if err := Write(dir, rec); err != nil {
			return Record{}, err
		}
	}
	if err := archive(dir, rec.ID); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func archive(dir, id string) error {
	archiveDir := filepath.Join(dir, archiveDirName)
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, id+".json"), filepath.Join(archiveDir, id+".json"))
}

func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id &&
		!strings.ContainsAny(id, `/\\`)
}
