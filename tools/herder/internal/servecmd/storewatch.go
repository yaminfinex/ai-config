package servecmd

import (
	"context"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// startStoreWatch watches the agent store's journal (events.jsonl) so a store
// append — a spawn's register, a future reparent — refolds the shared
// projection within the file-watch debounce instead of waiting for the poll.
// Started once per process by startStoreProjection (projection.go), never
// per SSE connection. The directory is watched (the journal is appended in
// place and may be created after the serve starts); only the journal file is
// a trigger. Nil when the store or the watcher factory is absent or the
// directory cannot be watched; then the process-level safety refold in
// projection.go (every TranscriptSafetyCadence, only when events.jsonl
// changed size) catches the change — the SSE poll does not, it reads the
// shared projection.
func startStoreWatch(ctx context.Context, deps dependencies) <-chan struct{} {
	if deps.store == nil || deps.fileWatcher == nil {
		return nil
	}
	watcher, err := deps.fileWatcher()
	if err != nil {
		return nil
	}
	journal := filepath.Clean(deps.store.EventsPath())
	if err := watcher.Add(filepath.Dir(journal)); err != nil {
		_ = watcher.Close()
		return nil
	}
	changes := make(chan struct{}, 1)
	go func() {
		defer watcher.Close()
		timer := time.NewTimer(time.Hour)
		if !timer.Stop() {
			<-timer.C
		}
		defer timer.Stop()
		var timerCh <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Clean(event.Name) != journal || event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(fileWatchDebounce)
				timerCh = timer.C
			case <-timerCh:
				timerCh = nil
				select {
				case changes <- struct{}{}:
				default:
				}
			}
		}
	}()
	return changes
}
