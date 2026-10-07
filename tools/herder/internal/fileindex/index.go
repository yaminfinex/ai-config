// Package fileindex builds and caches the file candidates for opaque absolute
// roots. A search never waits on git for a root it has indexed before: a
// stale root answers from its last list and refreshes in the background.
package fileindex

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"ai-config/tools/herder/internal/filecandidate"
)

const (
	// DefaultTTL is the staleness bound: a list older than this is still
	// served, and the lookup that finds it so starts one background refresh.
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

	mu       sync.Mutex
	cache    map[string]cacheEntry
	inflight map[string]*load
}

type cacheEntry struct {
	refreshed  time.Time
	candidates []filecandidate.Candidate
}

// load is one `git ls-files` for a root. At most one runs per root at a time;
// every lookup that needs it waits on done instead of starting another.
type load struct {
	done       chan struct{}
	candidates []filecandidate.Candidate
	err        error
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
		ttl:      ttl,
		now:      now,
		run:      run,
		cache:    make(map[string]cacheEntry),
		inflight: make(map[string]*load),
	}
}

// Candidates returns root-relative files and their unique ancestor
// directories. A cached root answers at once; past the TTL it still answers
// from its last list and starts one background refresh, so a new file is
// findable from the lookup after that refresh lands. Only a root never
// indexed (or whose last refresh failed) waits on git, sharing one load with
// every concurrent lookup. A true refresh bypasses the cached list and waits
// for a load that began after the call.
func (i *Index) Candidates(ctx context.Context, root string, refresh bool) ([]filecandidate.Candidate, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("file index root must be absolute: %q", root)
	}
	root = filepath.Clean(root)

	if refresh {
		return i.forced(ctx, root)
	}
	i.mu.Lock()
	if entry, cached := i.cache[root]; cached {
		if !i.now().Before(entry.refreshed.Add(i.ttl)) && i.inflight[root] == nil {
			i.startLocked(root)
		}
		i.mu.Unlock()
		return slices.Clone(entry.candidates), nil
	}
	current := i.inflight[root]
	if current == nil {
		current = i.startLocked(root)
	}
	i.mu.Unlock()
	return current.wait(ctx)
}

// forced waits out any load already running for root (it may have listed
// the tree before the caller's change), then waits on a load of its own; a
// forced lookup arriving meanwhile shares that newer load.
func (i *Index) forced(ctx context.Context, root string) ([]filecandidate.Candidate, error) {
	i.mu.Lock()
	if running := i.inflight[root]; running != nil {
		i.mu.Unlock()
		select {
		case <-running.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		i.mu.Lock()
	}
	current := i.inflight[root]
	if current == nil {
		current = i.startLocked(root)
	}
	i.mu.Unlock()
	return current.wait(ctx)
}

// startLocked starts the root's one load. Callers hold i.mu and have seen
// no load running for root.
func (i *Index) startLocked(root string) *load {
	current := &load{done: make(chan struct{})}
	i.inflight[root] = current
	go func() {
		// The load outlives any one request: a cancelled caller must not
		// cancel the refresh every other caller of this root is waiting on.
		candidates, err := i.load(context.Background(), root)
		i.mu.Lock()
		if err == nil {
			// Stamp after the load: a load longer than the TTL must not
			// arrive stale.
			i.cache[root] = cacheEntry{refreshed: i.now(), candidates: candidates}
		} else {
			// A failed refresh drops the list, so the next lookup waits and
			// reports the failure rather than serving a vanished root.
			delete(i.cache, root)
		}
		delete(i.inflight, root)
		current.candidates, current.err = candidates, err
		close(current.done)
		i.mu.Unlock()
	}()
	return current
}

func (l *load) wait(ctx context.Context) ([]filecandidate.Candidate, error) {
	select {
	case <-l.done:
		if l.err != nil {
			return nil, l.err
		}
		return slices.Clone(l.candidates), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (i *Index) load(ctx context.Context, root string) ([]filecandidate.Candidate, error) {
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
	// A non-git root is never walked: the readable universe is git top
	// levels only, so reaching here is a loud failure for that root.

	return nil, fmt.Errorf("index root %q: not a git repository", root)
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

func parseCandidates(out []byte) []filecandidate.Candidate {
	parts := strings.Split(string(out), "\x00")
	files := make([]string, 0, len(parts))
	directories := make(map[string]struct{})
	for _, candidatePath := range parts {
		candidatePath = filepath.ToSlash(strings.TrimPrefix(candidatePath, "./"))
		if candidatePath == "" || candidatePath == ".git" || strings.HasPrefix(candidatePath, ".git/") {
			continue
		}
		files = append(files, candidatePath)
		for directory := path.Dir(candidatePath); directory != "."; directory = path.Dir(directory) {
			directories[directory] = struct{}{}
		}
	}
	slices.Sort(files)
	candidates := make([]filecandidate.Candidate, 0, len(files)+len(directories))
	for _, file := range files {
		candidates = append(candidates, filecandidate.Candidate{Path: file, Kind: filecandidate.KindFile})
	}
	for directory := range directories {
		candidates = append(candidates, filecandidate.Candidate{Path: directory, Kind: filecandidate.KindDir})
	}
	slices.SortFunc(candidates, func(a, b filecandidate.Candidate) int {
		if a.Path != b.Path {
			return strings.Compare(a.Path, b.Path)
		}
		return strings.Compare(string(a.Kind), string(b.Kind))
	})
	return candidates
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
