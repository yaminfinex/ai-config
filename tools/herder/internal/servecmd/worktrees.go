package servecmd

import (
	"maps"
	"slices"
	"sort"
	"sync"

	"ai-config/tools/herder/internal/herdrcli"
)

type workspaceWorktreeKey struct {
	id           string
	linked       bool
	checkoutPath string
	repoRoot     string
}

func worktreeKey(workspaces []herdrcli.Workspace) []workspaceWorktreeKey {
	key := make([]workspaceWorktreeKey, 0, len(workspaces))
	for _, workspace := range workspaces {
		entry := workspaceWorktreeKey{id: workspace.WorkspaceID}
		if workspace.Worktree != nil && workspace.Worktree.IsLinkedWorktree {
			entry.linked = true
			entry.checkoutPath = workspace.Worktree.CheckoutPath
			entry.repoRoot = workspace.Worktree.RepoRoot
		}
		key = append(key, entry)
	}
	sort.Slice(key, func(i, j int) bool {
		a, b := key[i], key[j]
		if a.id != b.id {
			return a.id < b.id
		}
		if a.linked != b.linked {
			return !a.linked
		}
		if a.checkoutPath != b.checkoutPath {
			return a.checkoutPath < b.checkoutPath
		}
		return a.repoRoot < b.repoRoot
	})
	return key
}

func cachedWorktreeParents(inner func([]herdrcli.Workspace) (map[string]string, error)) func([]herdrcli.Workspace) (map[string]string, error) {
	var mu sync.Mutex
	var key []workspaceWorktreeKey
	var parents map[string]string
	initialized := false

	return func(workspaces []herdrcli.Workspace) (map[string]string, error) {
		mu.Lock()
		defer mu.Unlock()

		nextKey := worktreeKey(workspaces)
		if initialized && slices.Equal(key, nextKey) {
			return maps.Clone(parents), nil
		}
		nextParents, err := inner(workspaces)
		if err != nil {
			return nil, err
		}
		key = nextKey
		parents = maps.Clone(nextParents)
		initialized = true
		return maps.Clone(parents), nil
	}
}
