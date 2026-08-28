// Package fileroots derives the opaque readable universe for file endpoints.
package fileroots

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ai-config/tools/herder/internal/repoctx"
)

type Agent struct {
	Name string
	CWD  string
}

type Set struct {
	Roots      []string
	Configured []string
	AgentRoot  map[string]string
}

func CanonicalConfigured(paths []string) ([]string, error) {
	seen := make(map[string]bool, len(paths))
	configured := make([]string, 0, len(paths))
	for _, path := range paths {
		canonical, ok, err := canonicalDirectory(path)
		if err != nil {
			return nil, fmt.Errorf("configured root %q: %w", path, err)
		}
		if !ok {
			return nil, fmt.Errorf("configured root %q is not an existing directory", path)
		}
		if !seen[canonical] {
			seen[canonical] = true
			configured = append(configured, canonical)
		}
	}
	return configured, nil
}

func Build(ctx context.Context, configured []string, agents []Agent) (Set, error) {
	type candidate struct {
		path       string
		worktree   bool
		firstIndex int
	}
	byPath := make(map[string]*candidate)
	agentPath := make(map[string]string, len(agents))
	for index, agent := range agents {
		if agent.Name == "" || agent.CWD == "" {
			continue
		}
		path, ok, err := canonicalDirectory(agent.CWD)
		if err != nil || !ok {
			continue
		}
		linked, err := repoctx.LinkedWorktree(ctx, path)
		if err != nil {
			return Set{}, err
		}
		if existing := byPath[path]; existing != nil {
			existing.worktree = existing.worktree || linked
		} else {
			byPath[path] = &candidate{path: path, worktree: linked, firstIndex: index}
		}
		agentPath[agent.Name] = path
	}

	candidates := make([]*candidate, 0, len(byPath))
	for _, candidate := range byPath {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].firstIndex < candidates[j].firstIndex })
	retained := make(map[string]bool, len(candidates))
	mapped := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		parent := ""
		if !candidate.worktree {
			for _, possible := range candidates {
				if possible.path == candidate.path || !contains(possible.path, candidate.path) {
					continue
				}
				if parent == "" || len(possible.path) < len(parent) {
					parent = possible.path
				}
			}
		}
		if parent == "" {
			parent = candidate.path
			retained[parent] = true
		}
		mapped[candidate.path] = parent
	}

	set := Set{Configured: append([]string(nil), configured...), AgentRoot: make(map[string]string)}
	seen := make(map[string]bool)
	for _, root := range configured {
		if !seen[root] {
			seen[root] = true
			set.Roots = append(set.Roots, root)
		}
	}
	for _, candidate := range candidates {
		if retained[candidate.path] && !seen[candidate.path] {
			seen[candidate.path] = true
			set.Roots = append(set.Roots, candidate.path)
		}
	}
	for name, path := range agentPath {
		set.AgentRoot[name] = mapped[path]
	}
	return set, nil
}

func (s Set) Contains(root string) bool {
	for _, candidate := range s.Roots {
		if candidate == root {
			return true
		}
	}
	return false
}

func (s Set) Preference(agent string) []string {
	preference := make([]string, 0, len(s.Roots))
	seen := make(map[string]bool, len(s.Roots))
	appendRoot := func(root string) {
		if root != "" && s.Contains(root) && !seen[root] {
			seen[root] = true
			preference = append(preference, root)
		}
	}
	appendRoot(s.AgentRoot[agent])
	for _, root := range s.Configured {
		appendRoot(root)
	}
	for _, root := range s.Roots {
		appendRoot(root)
	}
	return preference
}

func canonicalDirectory(path string) (string, bool, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(absolute))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return filepath.Clean(resolved), info.IsDir(), nil
}

func contains(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
