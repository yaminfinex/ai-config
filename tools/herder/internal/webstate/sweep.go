package webstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SweepPolicy says which rows a sweep removes outright, without leaving a
// tombstone. Namespaces lists the only namespaces a sweep touches; every
// other namespace is left exactly as it is. TombstonesBefore purges
// tombstones whose Updated (ms) is older; zero keeps every tombstone. Absent
// reports a live row whose agent is gone; nil removes no live row.
//
// Only list namespaces whose rows may come back harmlessly. Clients replay
// their cached rows and merge additively, so a purged tombstone lets a stale
// client re-add the deleted row; owner content (notes, spaces) must keep its
// tombstones forever.
type SweepPolicy struct {
	Namespaces       map[string]bool
	TombstonesBefore int64
	Absent           func(namespace string, row Row) bool
}

// SweepResult names each namespace a sweep changed, with its new revision
// and how many rows went.
type SweepResult struct {
	User      string
	Namespace string
	Rev       uint64
	Removed   int
}

// Sweep removes rows by policy from every stored user and namespace. A
// namespace that loses rows takes one new revision, which also becomes its
// Floor: a client whose cursor is older is answered with every current row
// on its next pull. A row removed here simply stops appearing; the client
// keeps its own copy until its own pruning drops it.
func (s *FileStore) Sweep(policy SweepPolicy) ([]SweepResult, error) {
	users, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("%w: read state root: %v", ErrUnavailable, err)
	}
	var results []SweepResult
	var problems []error
	for _, user := range users {
		if !user.IsDir() || !userPattern.MatchString(user.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(s.root, user.Name()))
		if err != nil {
			problems = append(problems, err)
			continue
		}
		for _, file := range files {
			namespace, ok := strings.CutSuffix(file.Name(), ".json")
			if !ok || file.IsDir() || !namespacePattern.MatchString(namespace) || !policy.Namespaces[namespace] {
				continue
			}
			result, err := s.sweepNamespace(user.Name(), namespace, policy)
			if err != nil {
				problems = append(problems, err)
			} else if result.Removed > 0 {
				results = append(results, result)
			}
		}
	}
	return results, errors.Join(problems...)
}

func (s *FileStore) sweepNamespace(user, namespace string, policy SweepPolicy) (SweepResult, error) {
	result := SweepResult{User: user, Namespace: namespace}
	state, path, err := s.state(user, namespace)
	if err != nil {
		return result, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := s.load(state, path, false); err != nil {
		if errors.Is(err, ErrNamespaceNotFound) {
			return result, nil
		}
		return result, err
	}
	next := namespaceData{Revision: state.data.Revision, Floor: state.data.Floor, Rows: make(map[string]storedRow, len(state.data.Rows))}
	for key, row := range state.data.Rows {
		purge := row.Deleted && row.Updated < policy.TombstonesBefore
		absent := !row.Deleted && policy.Absent != nil && policy.Absent(namespace, row.Row)
		if purge || absent {
			result.Removed++
			continue
		}
		next.Rows[key] = row
	}
	result.Rev = state.data.Revision
	if result.Removed == 0 {
		return result, nil
	}
	next.Revision++
	next.Floor = next.Revision
	if err := save(path, next); err != nil {
		return SweepResult{User: user, Namespace: namespace}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	state.data = next
	result.Rev = next.Revision
	return result, nil
}
