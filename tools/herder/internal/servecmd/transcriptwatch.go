package servecmd

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const transcriptWatchDebounce = 120 * time.Millisecond

type transcriptWatchTarget struct {
	Agent string
	Path  string
}

type transcriptWatchSubscription struct {
	cancel  context.CancelFunc
	done    chan struct{}
	watcher *fsnotify.Watcher
	mu      sync.RWMutex
	byPath  map[string][]string
	dirs    map[string]bool
	Changes <-chan []string
	Errors  <-chan error
}

func startTranscriptWatches(ctx context.Context, factory func() (*fsnotify.Watcher, error), debounce time.Duration) (*transcriptWatchSubscription, error) {
	if factory == nil {
		return nil, nil
	}
	watcher, err := factory()
	if err != nil {
		return nil, err
	}
	watchCtx, cancel := context.WithCancel(ctx)
	changes := make(chan []string, 1)
	errors := make(chan error, 1)
	subscription := &transcriptWatchSubscription{
		cancel: cancel, done: make(chan struct{}), watcher: watcher,
		byPath: make(map[string][]string), dirs: make(map[string]bool),
		Changes: changes, Errors: errors,
	}
	go subscription.run(watchCtx, changes, errors, debounce)
	return subscription, nil
}

func (s *transcriptWatchSubscription) Update(targets []transcriptWatchTarget) error {
	if s == nil {
		return nil
	}
	byPath := make(map[string][]string, len(targets))
	dirs := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target.Agent == "" || target.Path == "" {
			continue
		}
		path := filepath.Clean(target.Path)
		byPath[path] = append(byPath[path], target.Agent)
		dirs[filepath.Dir(path)] = true
	}
	for path := range byPath {
		sort.Strings(byPath[path])
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	added := make([]string, 0)
	for directory := range dirs {
		if s.dirs[directory] {
			continue
		}
		if err := s.watcher.Add(directory); err != nil {
			for _, rollback := range added {
				_ = s.watcher.Remove(rollback)
			}
			return fmt.Errorf("watch transcript directory %q: %w", directory, err)
		}
		added = append(added, directory)
	}
	for directory := range s.dirs {
		if !dirs[directory] {
			_ = s.watcher.Remove(directory)
		}
	}
	s.byPath = byPath
	s.dirs = dirs
	return nil
}

func (s *transcriptWatchSubscription) Close() {
	if s == nil {
		return
	}
	s.cancel()
	<-s.done
}

func (s *transcriptWatchSubscription) run(ctx context.Context, changes chan<- []string, watchErrors chan<- error, debounce time.Duration) {
	defer close(s.done)
	defer s.watcher.Close()
	pending := make(map[string]bool)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var timerCh <-chan time.Time
	reportStopped := func(detail string) {
		if ctx.Err() != nil {
			return
		}
		select {
		case watchErrors <- fmt.Errorf("transcript watcher stopped: %s", detail):
		default:
		}
	}
	resetTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(debounce)
		timerCh = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-s.watcher.Errors:
			if !ok {
				reportStopped("error channel closed")
				return
			}
			select {
			case watchErrors <- err:
			default:
			}
			return
		case event, ok := <-s.watcher.Events:
			if !ok {
				reportStopped("event channel closed")
				return
			}
			path := filepath.Clean(event.Name)
			s.mu.RLock()
			agents := append([]string(nil), s.byPath[path]...)
			s.mu.RUnlock()
			if len(agents) == 0 {
				continue
			}
			for _, agent := range agents {
				pending[agent] = true
			}
			resetTimer()
		case <-timerCh:
			timerCh = nil
			names := make([]string, 0, len(pending))
			for agent := range pending {
				names = append(names, agent)
			}
			sort.Strings(names)
			clear(pending)
			select {
			case changes <- names:
			default:
			}
		}
	}
}
