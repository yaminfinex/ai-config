// Package fileroots derives the opaque readable universe for file endpoints.
//
// The universe is git top levels only: every live agent cwd maps to the top
// level of the repository containing it, and a cwd outside any repository
// contributes nothing. Configured roots must themselves be top levels.
package fileroots

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// CanonicalConfigured canonicalises --root flags and refuses any that is not
// an existing directory or not a git repository top level.
func CanonicalConfigured(ctx context.Context, paths []string) ([]string, error) {
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
		top, isRepo, err := repoctx.TopLevel(ctx, canonical)
		if err != nil {
			return nil, fmt.Errorf("configured root %q: %w", path, err)
		}
		if !isRepo || top != canonical {
			return nil, fmt.Errorf("configured root %q is not a git repository top level", path)
		}
		if !seen[canonical] {
			seen[canonical] = true
			configured = append(configured, canonical)
		}
	}
	return configured, nil
}

// Build maps each live agent cwd to its git top level. Two cwds in one
// repository become one root by construction; a repository nested inside
// another stays its own root; a linked worktree's top level is the worktree.
func Build(ctx context.Context, configured []string, agents []Agent) (Set, error) {
	set := Set{Configured: append([]string(nil), configured...), AgentRoot: make(map[string]string)}
	seen := make(map[string]bool)
	for _, root := range configured {
		if !seen[root] {
			seen[root] = true
			set.Roots = append(set.Roots, root)
		}
	}
	for _, agent := range agents {
		if agent.Name == "" || agent.CWD == "" {
			continue
		}
		path, ok, err := canonicalDirectory(agent.CWD)
		if err != nil || !ok {
			continue
		}
		top, isRepo, err := repoctx.TopLevel(ctx, path)
		if err != nil {
			return Set{}, err
		}
		if !isRepo {
			continue
		}
		set.AgentRoot[agent.Name] = top
		if !seen[top] {
			seen[top] = true
			set.Roots = append(set.Roots, top)
		}
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

// MostSpecific returns the live root with the longest prefix match on the
// absolute path: the root equal to the path or a directory ancestor of it.
func (s Set) MostSpecific(path string) (string, bool) {
	if !filepath.IsAbs(path) {
		return "", false
	}
	path = filepath.Clean(path)
	best := ""
	for _, root := range s.Roots {
		if root == path || strings.HasPrefix(path, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator)) {
			if len(root) > len(best) {
				best = root
			}
		}
	}
	return best, best != ""
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
