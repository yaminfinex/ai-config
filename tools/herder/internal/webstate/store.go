// Package webstate stores small per-user, per-namespace UI records.
package webstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

const (
	DefaultMaxValueBytes = 64 << 10
	DefaultMaxRows       = 10_000
)

var (
	ErrNamespaceNotFound = errors.New("state namespace not found")
	ErrValueTooLarge     = errors.New("state row value is too large")
	ErrRowLimit          = errors.New("state namespace row limit reached")
	ErrUnavailable       = errors.New("state storage unavailable")
	namespacePattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*$`)
	userPattern          = regexp.MustCompile(`^web-[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type Row struct {
	Key     string          `json:"key"`
	Value   json.RawMessage `json:"value"`
	Updated int64           `json:"updated"`
	WriteID string          `json:"writeID"`
	Deleted bool            `json:"deleted"`
}

type Store interface {
	Upsert(user, namespace string, rows []Row) (accepted []string, rev uint64, err error)
	Since(user, namespace string, rev uint64) (rows []Row, currentRev uint64, err error)
}

type Limits struct {
	MaxValueBytes int
	MaxRows       int
}

func DefaultLimits() Limits {
	return Limits{MaxValueBytes: DefaultMaxValueBytes, MaxRows: DefaultMaxRows}
}

func ValidNamespace(namespace string) bool { return namespacePattern.MatchString(namespace) }

// Compare returns positive when the left (updated, writeID) pair wins,
// negative when the right wins, and zero for an idempotent replay.
func Compare(leftUpdated int64, leftWriteID string, rightUpdated int64, rightWriteID string) int {
	if leftUpdated > rightUpdated {
		return 1
	}
	if leftUpdated < rightUpdated {
		return -1
	}
	if leftWriteID > rightWriteID {
		return 1
	}
	if leftWriteID < rightWriteID {
		return -1
	}
	return 0
}

type storedRow struct {
	Row
	Revision uint64 `json:"rev"`
}

type namespaceData struct {
	Revision uint64               `json:"rev"`
	Rows     map[string]storedRow `json:"rows"`
}

type namespaceState struct {
	mu     sync.Mutex
	loaded bool
	data   namespaceData
}

type FileStore struct {
	root   string
	limits Limits
	mu     sync.Mutex
	states map[string]*namespaceState
}

func NewFileStore(root string, limits Limits) (*FileStore, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: state root is empty", ErrUnavailable)
	}
	if limits.MaxValueBytes <= 0 || limits.MaxRows <= 0 {
		return nil, errors.New("state limits must be positive")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("%w: create state root: %v", ErrUnavailable, err)
	}
	return &FileStore{root: root, limits: limits, states: map[string]*namespaceState{}}, nil
}

func (s *FileStore) state(user, namespace string) (*namespaceState, string, error) {
	if !userPattern.MatchString(user) {
		return nil, "", fmt.Errorf("invalid state user %q", user)
	}
	if !namespacePattern.MatchString(namespace) {
		return nil, "", fmt.Errorf("invalid state namespace %q", namespace)
	}
	identity := user + "\x00" + namespace
	s.mu.Lock()
	state := s.states[identity]
	if state == nil {
		state = &namespaceState{}
		s.states[identity] = state
	}
	s.mu.Unlock()
	return state, filepath.Join(s.root, user, namespace+".json"), nil
}

func (s *FileStore) load(state *namespaceState, path string, create bool) error {
	if state.loaded {
		return nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return ErrNamespaceNotFound
		}
		state.data = namespaceData{Rows: map[string]storedRow{}}
		state.loaded = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: read %s: %v", ErrUnavailable, path, err)
	}
	var data namespaceData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("%w: state file %s is unreadable: %v", ErrUnavailable, path, err)
	}
	if data.Rows == nil {
		data.Rows = map[string]storedRow{}
	}
	state.data = data
	state.loaded = true
	return nil
}

func (s *FileStore) Upsert(user, namespace string, rows []Row) ([]string, uint64, error) {
	state, path, err := s.state(user, namespace)
	if err != nil {
		return nil, 0, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := s.load(state, path, true); err != nil {
		return nil, 0, err
	}

	keys := make([]string, 0, len(rows))
	candidates := map[string]Row{}
	for _, candidate := range rows {
		if candidate.Key == "" || candidate.WriteID == "" {
			return nil, state.data.Revision, errors.New("state row key and writeID are required")
		}
		if len(candidate.Value) > s.limits.MaxValueBytes {
			return nil, state.data.Revision, fmt.Errorf("%w: key %q has %d bytes; limit is %d", ErrValueTooLarge, candidate.Key, len(candidate.Value), s.limits.MaxValueBytes)
		}
		if !json.Valid(candidate.Value) {
			return nil, state.data.Revision, fmt.Errorf("state row %q value must be valid JSON", candidate.Key)
		}
		current, exists := candidates[candidate.Key]
		if !exists {
			keys = append(keys, candidate.Key)
			candidates[candidate.Key] = candidate
		} else if Compare(candidate.Updated, candidate.WriteID, current.Updated, current.WriteID) > 0 {
			candidates[candidate.Key] = candidate
		}
	}
	newRows := 0
	for key := range candidates {
		if _, exists := state.data.Rows[key]; !exists {
			newRows++
		}
	}
	if len(state.data.Rows)+newRows > s.limits.MaxRows {
		return nil, state.data.Revision, fmt.Errorf("%w: namespace would contain %d rows including tombstones; limit is %d", ErrRowLimit, len(state.data.Rows)+newRows, s.limits.MaxRows)
	}

	next := namespaceData{Revision: state.data.Revision, Rows: make(map[string]storedRow, len(state.data.Rows)+newRows)}
	for key, value := range state.data.Rows {
		next.Rows[key] = value
	}
	accepted := make([]string, 0, len(candidates))
	for _, key := range keys {
		candidate := candidates[key]
		current, exists := next.Rows[key]
		if exists && Compare(candidate.Updated, candidate.WriteID, current.Updated, current.WriteID) <= 0 {
			continue
		}
		next.Revision++
		next.Rows[key] = storedRow{Row: candidate, Revision: next.Revision}
		accepted = append(accepted, key)
	}
	if len(accepted) == 0 {
		return accepted, state.data.Revision, nil
	}
	if err := save(path, next); err != nil {
		return nil, state.data.Revision, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	state.data = next
	return accepted, next.Revision, nil
}

func (s *FileStore) Since(user, namespace string, revision uint64) ([]Row, uint64, error) {
	state, path, err := s.state(user, namespace)
	if err != nil {
		return nil, 0, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := s.load(state, path, false); err != nil {
		return nil, 0, err
	}
	changed := make([]storedRow, 0)
	for _, candidate := range state.data.Rows {
		if revision == 0 || candidate.Revision > revision {
			changed = append(changed, candidate)
		}
	}
	sort.Slice(changed, func(i, j int) bool {
		if changed[i].Revision != changed[j].Revision {
			return changed[i].Revision < changed[j].Revision
		}
		return changed[i].Key < changed[j].Key
	})
	rows := make([]Row, len(changed))
	for index, candidate := range changed {
		rows[index] = candidate.Row
	}
	return rows, state.data.Revision, nil
}

func save(path string, data namespaceData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("state rename may not be durable: %w", err)
	}
	if err := dir.Sync(); err != nil {
		dir.Close()
		return fmt.Errorf("state rename may not be durable: %w", err)
	}
	return dir.Close()
}

type unavailableStore struct{ err error }

func Unavailable(err error) Store {
	if err == nil {
		err = ErrUnavailable
	}
	return unavailableStore{err: err}
}

func (s unavailableStore) Upsert(string, string, []Row) ([]string, uint64, error) {
	return nil, 0, fmt.Errorf("%w: %v", ErrUnavailable, s.err)
}

func (s unavailableStore) Since(string, string, uint64) ([]Row, uint64, error) {
	return nil, 0, fmt.Errorf("%w: %v", ErrUnavailable, s.err)
}
