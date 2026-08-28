package fileresolver

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

const (
	matchSlab16Size = 100 * 1024
	matchSlab32Size = 2048
)

func init() {
	if !algo.Init("path") {
		panic("fzf rejected path scoring scheme")
	}
}

// Tier records how a candidate matched. Prefix and suffix share a ranking
// band but remain distinct so callers can apply the exact-or-suffix auto-open
// rule without re-ranking.
type Tier string

const (
	TierExact  Tier = "exact"
	TierPrefix Tier = "prefix"
	TierSuffix Tier = "suffix"
	TierFuzzy  Tier = "fuzzy"
)

// Result is one ranked root-relative candidate.
type Result struct {
	Root  string `json:"root"`
	Path  string `json:"path"`
	Tier  Tier   `json:"tier"`
	Score int    `json:"score"`
}

// Request describes one lookup. Roots is the complete candidate universe;
// RootPreference is an optional ordered ranking boost over that universe.
type Request struct {
	Query          string
	Roots          []string
	RootPreference []string
	Refresh        bool
}

// CandidateSource supplies root-relative candidates without exposing the
// resolver to the index implementation or cache mechanics.
type CandidateSource interface {
	Candidates(ctx context.Context, root string, refresh bool) ([]string, error)
}

// Resolver is the narrow engine seam used by later server units.
type Resolver interface {
	Resolve(ctx context.Context, request Request) ([]Result, error)
}

type resolver struct {
	source CandidateSource
}

// New returns an fzf-backed file resolver.
func New(source CandidateSource) Resolver {
	return &resolver{source: source}
}

func (r *resolver) Resolve(ctx context.Context, request Request) ([]Result, error) {
	query := NormalizeQuery(request.Query).Path
	if query == "" {
		return nil, nil
	}
	if r.source == nil {
		return nil, fmt.Errorf("file resolver has no candidate source")
	}

	roots := uniqueRoots(request.Roots)
	rootRanks := rankRoots(roots, request.RootPreference)
	pattern := []rune(query)
	slab := util.MakeSlab(matchSlab16Size, matchSlab32Size)
	results := make([]Result, 0)
	for _, root := range roots {
		paths, err := r.source.Candidates(ctx, root, request.Refresh)
		if err != nil {
			return nil, fmt.Errorf("candidates for root %q: %w", root, err)
		}
		for _, path := range paths {
			tier, score, ok := match(path, pattern, slab)
			if ok {
				results = append(results, Result{Root: root, Path: path, Tier: tier, Score: score})
			}
		}
	}

	sort.SliceStable(results, func(left, right int) bool {
		a, b := results[left], results[right]
		aBand, bBand := tierBand(a.Tier), tierBand(b.Tier)
		if aBand != bBand {
			return aBand < bBand
		}
		if rootRanks[a.Root] != rootRanks[b.Root] {
			return rootRanks[a.Root] < rootRanks[b.Root]
		}
		return a.Score > b.Score
	})
	return results, nil
}

func match(path string, pattern []rune, slab *util.Slab) (Tier, int, bool) {
	chars := util.ToChars([]byte(path))
	if result, _ := algo.EqualMatch(false, true, true, &chars, pattern, false, slab); result.Start >= 0 {
		return TierExact, result.Score, true
	}
	if result, _ := algo.PrefixMatch(false, true, true, &chars, pattern, false, slab); result.Start >= 0 {
		return TierPrefix, result.Score, true
	}
	if result, _ := algo.SuffixMatch(false, true, true, &chars, pattern, false, slab); result.Start >= 0 {
		return TierSuffix, result.Score, true
	}
	if result, _ := algo.FuzzyMatchV2(false, true, true, &chars, pattern, false, slab); result.Start >= 0 {
		return TierFuzzy, result.Score, true
	}
	return "", 0, false
}

func tierBand(tier Tier) int {
	switch tier {
	case TierExact:
		return 0
	case TierPrefix, TierSuffix:
		return 1
	default:
		return 2
	}
}

func uniqueRoots(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	unique := make([]string, 0, len(roots))
	for _, root := range roots {
		root = filepath.Clean(root)
		if !seen[root] {
			seen[root] = true
			unique = append(unique, root)
		}
	}
	return unique
}

func rankRoots(roots, preference []string) map[string]int {
	ranks := make(map[string]int, len(roots))
	next := 0
	rootSet := make(map[string]bool, len(roots))
	for _, root := range roots {
		rootSet[root] = true
	}
	for _, root := range preference {
		root = filepath.Clean(root)
		if !rootSet[root] {
			continue
		}
		if _, exists := ranks[root]; !exists {
			ranks[root] = next
			next++
		}
	}
	for _, root := range roots {
		if _, exists := ranks[root]; !exists {
			ranks[root] = next
			next++
		}
	}
	return ranks
}

// ResolveWithinRoot resolves candidate through symlinks and returns it only
// when the real path remains inside the real root.
func ResolveWithinRoot(root, candidate string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("root must be absolute: %q", root)
	}
	root = filepath.Clean(root)
	requested := candidate
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, requested)
	}
	requested = filepath.Clean(requested)

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", root, err)
	}
	realCandidate, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("resolve path %q within root %q: %w", requested, realRoot, err)
	}
	relative, err := filepath.Rel(realRoot, realCandidate)
	if err != nil {
		return "", fmt.Errorf("compare path %q resolved to %q with root %q: %w", requested, realCandidate, realRoot, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves to %q outside root %q", requested, realCandidate, realRoot)
	}
	return realCandidate, nil
}
