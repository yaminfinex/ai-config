package ship

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Run is the daemon loop. fsnotify events are a hint that wakes an early
// pass; the periodic rescan is the guarantee (queue overflows, moves, files
// created while the shipper was down). Every pass is the same authoritative
// RunOnce — backfill and live tailing are one code path (I3).
func (s *Shipper) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		s.logger().Warn("fsnotify unavailable; rescan-only mode", "err", err)
		watcher = nil
	} else {
		defer watcher.Close()
		s.watchDirs(watcher)
	}

	ticker := time.NewTicker(s.rescan())
	defer ticker.Stop()

	// wake coalesces bursts of events into one pending pass.
	wake := make(chan struct{}, 1)
	if watcher != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-watcher.Events:
					if !ok {
						return
					}
					// New directories must be watched as they appear
					// (project dirs and dated codex dirs churn).
					if ev.Op.Has(fsnotify.Create) {
						if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
							_ = watcher.Add(ev.Name)
						}
					}
					select {
					case wake <- struct{}{}:
					default:
					}
				case _, ok := <-watcher.Errors:
					if !ok {
						return
					}
					// Overflow or watch error: the rescan guarantees catch-up.
				}
			}
		}()
	}

	attempt := 0
	for {
		passStarted := time.Now()
		err := s.RunOnce(ctx)
		switch {
		case err == nil:
			attempt = 0
		case errors.Is(err, errHold):
			attempt++
			s.logger().Warn("holding position; store not accepting", "attempt", attempt, "err", err)
		default:
			// Non-hold errors are surfaced but never kill the daemon: files
			// are the interface and the next pass retries what it can.
			attempt = 0
			s.logger().Error("pass error", "err", err)
		}

		var delay <-chan time.Time
		if attempt > 0 {
			delay = time.After(s.backoff(attempt))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if watcher != nil {
				s.watchDirs(watcher)
			}
		case <-wake:
			// Debounce the hint so one save burst is one pass.
			if err := waitUntil(ctx, time.Now().Add(200*time.Millisecond)); err != nil {
				return err
			}
			// Continuous transcript appends keep wake pending. Admit at most
			// one authoritative pass per interval instead of running at the
			// debounce ceiling forever.
			if err := waitUntil(ctx, passStarted.Add(s.minHintInterval())); err != nil {
				return err
			}
		case <-delay:
		}
	}
}

func waitUntil(ctx context.Context, deadline time.Time) error {
	d := time.Until(deadline)
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// watchDirs (re-)registers every directory under both roots. It runs when the
// watcher is armed and on periodic rescans. Create events add new directories
// between rescans without making every hint re-walk both trees.
func (s *Shipper) watchDirs(w *fsnotify.Watcher) {
	for _, root := range []string{s.Roots.Claude, s.Roots.Codex} {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				_ = w.Add(path)
			}
			return nil
		})
	}
}
