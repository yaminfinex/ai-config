// Package fileindex builds and briefly caches the file candidates for opaque
// absolute roots.
package fileindex

import (
	"bytes"
	"context"
	"errors"
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
	maxErrorDetail = 4 * 1024
)

// CommandOutput keeps candidate data separate from command diagnostics.
type CommandOutput struct {
	Stdout []byte
	Stderr []byte
}

// RunFunc runs a command in dir and returns its output. It is a seam
// for deterministic tests; production callers normally leave Options.Run nil.
type RunFunc func(ctx context.Context, dir, name string, args ...string) (CommandOutput, error)

// DegradedError reports a partial non-git index whose returned candidates are
// still usable. Callers must surface the diagnostic rather than silently
// treating the root as complete.
type DegradedError struct {
	detail string
}

func (e *DegradedError) Error() string  { return e.detail }
func (e *DegradedError) Degraded() bool { return true }

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
	degraded  string
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
			return slices.Clone(entry.paths), degradedError(entry.degraded)
		}
	}

	paths, err := i.load(ctx, root)
	var degraded *DegradedError
	if err != nil && !errors.As(err, &degraded) {
		return nil, err
	}
	i.mu.Lock()
	entry := cacheEntry{refreshed: now, paths: slices.Clone(paths)}
	if degraded != nil {
		entry.degraded = degraded.Error()
	}
	i.cache[root] = entry
	i.mu.Unlock()
	return slices.Clone(paths), degradedError(entry.degraded)
}

func (i *Index) load(ctx context.Context, root string) ([]string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := i.run(commandCtx, root, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err == nil {
		return parseCandidates(out.Stdout), nil
	}
	if commandCtx.Err() != nil {
		return nil, fmt.Errorf("index git root %q: %w", root, commandCtx.Err())
	}
	if !strings.Contains(string(out.Stderr), "not a git repository") {
		return nil, fmt.Errorf("git ls-files in %q failed: %w: %s", root, err, errorDetail(out))
	}

	out, err = i.run(commandCtx, root, "rg", "--files", "--hidden", "--no-require-git", "--null", "--glob", "!.git", "--glob", "!.git/**")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 && len(out.Stdout) == 0 {
			return []string{}, nil
		}
		if commandCtx.Err() != nil {
			return nil, fmt.Errorf("index non-git root %q: %w", root, commandCtx.Err())
		}
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 2 && len(out.Stdout) > 0 && len(out.Stderr) > 0 {
			return parseCandidates(out.Stdout), &DegradedError{detail: fmt.Sprintf("rg --files in non-git root %q was partial: %s", root, errorDetail(out))}
		}
		return nil, fmt.Errorf("rg --files in non-git root %q failed: %w: %s", root, err, errorDetail(out))
	}
	return parseCandidates(out.Stdout), nil
}

func degradedError(detail string) error {
	if detail == "" {
		return nil
	}
	return &DegradedError{detail: detail}
}

func errorDetail(out CommandOutput) string {
	detail := bytes.TrimSpace(out.Stderr)
	if len(detail) == 0 {
		detail = bytes.TrimSpace(out.Stdout)
	}
	if len(detail) <= maxErrorDetail {
		return string(detail)
	}
	return string(detail[:maxErrorDetail]) + " [detail truncated]"
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

func runCommand(ctx context.Context, dir, name string, args ...string) (CommandOutput, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return CommandOutput{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}
