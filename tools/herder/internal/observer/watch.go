// watch.go owns the process-scoped directory watcher: fsnotify over
// transcript directories, the debounce, the ceiling, and the "dead → sweep"
// degrade. It does not know sessions; it emits changed paths and discover.go
// maps them back.
package observer

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherFactory is the fsnotify seam; nil means no watcher (sweep only).
type WatcherFactory func() (*fsnotify.Watcher, error)

// dirWatch is a process-scoped fsnotify subscription over transcript
// DIRECTORIES with a debounce and a ceiling. It emits changed file paths.
//
// Why a second watcher next to servecmd/transcriptwatch.go: that one is per
// SSE connection, keyed by agent name, dies with the client and feeds the
// entry push; this one lives for the process, is keyed by path, has a ceiling
// and feeds the observer. Lifting the shared debounce would touch the push
// path, which this unit does not change; the two are signposted here instead.
type dirWatch struct {
	watcher *fsnotify.Watcher
	audit   func(string, ...any)
	mu      sync.Mutex
	dirs    map[string]bool
	dead    bool
	Changes <-chan []string
}

// startDirWatch returns nil when there is no factory or it fails; every
// session then lives on the sweep (never an error).
func startDirWatch(ctx context.Context, factory WatcherFactory, audit func(string, ...any)) *dirWatch {
	if factory == nil {
		return nil
	}
	watcher, err := factory()
	if err != nil {
		audit("observer: fsnotify unavailable; sessions refresh on the %s sweep: %v", DefaultSweep, err)
		return nil
	}
	changes := make(chan []string, 1)
	w := &dirWatch{watcher: watcher, audit: audit, dirs: map[string]bool{}, Changes: changes}
	go w.run(ctx, changes)
	return w
}

// add watches a directory (idempotent). False means the session must rely on
// the sweep: ceiling reached, inotify refused, or the watcher has died.
func (w *dirWatch) add(dir string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dead {
		return false
	}
	if w.dirs[dir] {
		return true
	}
	if len(w.dirs) >= MaxWatchDirectories {
		return false
	}
	if err := w.watcher.Add(dir); err != nil {
		w.audit("observer: watch %q refused; session refreshes on the sweep: %v", dir, err)
		return false
	}
	w.dirs[dir] = true
	return true
}

func (w *dirWatch) run(ctx context.Context, changes chan<- []string) {
	defer w.watcher.Close()
	pending := map[string]bool{}
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var timerCh <-chan time.Time
	die := func(detail string) {
		w.mu.Lock()
		w.dead = true
		w.mu.Unlock()
		w.audit("observer: directory watcher stopped (%s); every session refreshes on the %s sweep", detail, DefaultSweep)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.watcher.Errors:
			if !ok {
				die("error channel closed")
				return
			}
			die(err.Error())
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				die("event channel closed")
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			pending[filepath.Clean(event.Name)] = true
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(WatchDebounce)
			timerCh = timer.C
		case <-timerCh:
			timerCh = nil
			paths := make([]string, 0, len(pending))
			for path := range pending {
				paths = append(paths, path)
			}
			clear(pending)
			select {
			case changes <- paths:
			default:
				// A batch is already waiting; merge into it on the next tick.
				for _, path := range paths {
					pending[path] = true
				}
				timer.Reset(WatchDebounce)
				timerCh = timer.C
			}
		}
	}
}
