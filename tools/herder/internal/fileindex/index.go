// Package fileindex builds and briefly caches the file candidates for opaque
// absolute roots.
package fileindex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTTL     = 5 * time.Second
	commandTimeout = 10 * time.Second
)

// RunFunc runs a command in dir and returns its combined output. It is a seam
// for deterministic tests; production callers normally leave Options.Run nil.
type RunFunc func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

// Options configures an Index. Zero values select production defaults.
type Options struct {
	TTL time.Duration
	Now func() time.Time
	Run RunFunc
}

// Index caches only derived candidate lists. Losing an Index loses no source
// of truth; the next lookup rebuilds the requested root.
type Index struct {
	ttl time.Duration
	now func() time.Time
	run RunFunc

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	refreshed time.Time
	paths     []string
}

// New returns a per-root candidate index.
func New(options Options) *Index {
	ttl := options.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	run := options.Run
	if run == nil {
		run = runCommand
	}
	return &Index{
		ttl:   ttl,
		now:   now,
		run:   run,
		cache: make(map[string]cacheEntry),
	}
}

// Candidates returns root-relative file paths. A true refresh bypasses an
// otherwise fresh cache entry.
func (i *Index) Candidates(ctx context.Context, root string, refresh bool) ([]string, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("file index root must be absolute: %q", root)
	}
	root = filepath.Clean(root)
	now := i.now()

	if !refresh {
		i.mu.Lock()
		entry, ok := i.cache[root]
		i.mu.Unlock()
		if ok && now.Before(entry.refreshed.Add(i.ttl)) {
			return slices.Clone(entry.paths), nil
		}
	}

	paths, err := i.load(ctx, root)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	i.cache[root] = cacheEntry{refreshed: now, paths: slices.Clone(paths)}
	i.mu.Unlock()
	return slices.Clone(paths), nil
}

func (i *Index) load(ctx context.Context, root string) ([]string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := i.run(commandCtx, root, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err == nil {
		return parseCandidates(out), nil
	}
	if commandCtx.Err() != nil {
		return nil, fmt.Errorf("index git root %q: %w", root, commandCtx.Err())
	}
	if !strings.Contains(string(out), "not a git repository") {
		return nil, fmt.Errorf("git ls-files in %q failed: %w: %s", root, err, strings.TrimSpace(string(out)))
	}

	out, err = i.run(commandCtx, root, "rg", "--files", "--hidden", "--no-require-git", "--null", "--glob", "!.git", "--glob", "!.git/**")
	if err != nil {
		if commandCtx.Err() != nil {
			return nil, fmt.Errorf("index non-git root %q: %w", root, commandCtx.Err())
		}
		return nil, fmt.Errorf("rg --files in non-git root %q failed: %w: %s", root, err, strings.TrimSpace(string(out)))
	}
	return parseCandidates(out), nil
}

func parseCandidates(out []byte) []string {
	parts := strings.Split(string(out), "\x00")
	paths := make([]string, 0, len(parts))
	for _, path := range parts {
		path = filepath.ToSlash(strings.TrimPrefix(path, "./"))
		if path == "" || path == ".git" || strings.HasPrefix(path, ".git/") {
			continue
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

func runCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd.CombinedOutput()
}
