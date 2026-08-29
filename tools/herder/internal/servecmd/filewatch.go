package servecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/fileroots"
	"github.com/fsnotify/fsnotify"
)

const (
	maxFileWatchRequestBytes = 64 << 10
	maxFileWatchTargets      = 256
	maxFileWatchDirectories  = 64
	fileWatchDebounce        = 120 * time.Millisecond
)

type fileWatchRequest struct {
	Kind string `json:"kind"`
	Root string `json:"root"`
	Path string `json:"path"`
}

type fileChangeFact struct {
	Kind string `json:"kind"`
	Root string `json:"root"`
	Path string `json:"path"`
}

type resolvedFileWatch struct {
	fact      fileChangeFact
	directory string
	file      string
}

type fileWatchSubscription struct {
	cancel context.CancelFunc
	done   chan struct{}
	Facts  <-chan fileChangeFact
}

func (s *fileWatchSubscription) Close() {
	if s == nil {
		return
	}
	s.cancel()
	<-s.done
}

func parseFileWatchRequests(raw string) ([]fileWatchRequest, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxFileWatchRequestBytes {
		return nil, fmt.Errorf("watches must not exceed %d bytes", maxFileWatchRequestBytes)
	}
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '[' {
		return nil, errors.New("watches must be one JSON array of documented file/folder targets")
	}
	var requests []fileWatchRequest
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&requests); err != nil {
		return nil, errors.New("watches must be one JSON array of documented file/folder targets")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("watches must contain one JSON value")
	}
	if len(requests) > maxFileWatchTargets {
		requests = requests[len(requests)-maxFileWatchTargets:]
	}
	for _, request := range requests {
		if (request.Kind != "file" && request.Kind != "folder") || request.Root == "" {
			return nil, errors.New("each watch requires kind file|folder, a root, and a root-relative path")
		}
		if request.Kind == "file" && request.Path == "" {
			return nil, errors.New("file watch paths must not be empty")
		}
	}
	return requests, nil
}

func resolveFileWatches(set fileroots.Set, requests []fileWatchRequest) []resolvedFileWatch {
	resolved := make([]resolvedFileWatch, 0, len(requests))
	for _, request := range requests {
		if !set.Contains(request.Root) {
			continue
		}
		path, ok := cleanWatchPath(request.Path, request.Kind == "folder")
		if !ok {
			continue
		}
		directoryPath := path
		fileName := ""
		if request.Kind == "file" {
			directoryPath = filepath.Dir(path)
			fileName = filepath.Base(path)
		}
		directory, err := fileresolver.ResolveWithinRoot(request.Root, directoryPath)
		if err != nil {
			continue
		}
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			continue
		}
		watch := resolvedFileWatch{
			fact:      fileChangeFact{Kind: request.Kind, Root: request.Root, Path: request.Path},
			directory: filepath.Clean(directory),
		}
		if fileName != "" {
			watch.file = filepath.Join(directory, fileName)
		}
		resolved = append(resolved, watch)
	}

	// Requests are oldest-to-newest. Retain all targets in the newest 64
	// distinct directories, then restore declaration order for deterministic
	// matching and tests.
	retainedDirectories := make(map[string]bool, maxFileWatchDirectories)
	retained := make([]resolvedFileWatch, 0, len(resolved))
	for index := len(resolved) - 1; index >= 0; index-- {
		watch := resolved[index]
		if !retainedDirectories[watch.directory] && len(retainedDirectories) == maxFileWatchDirectories {
			continue
		}
		retainedDirectories[watch.directory] = true
		retained = append(retained, watch)
	}
	for left, right := 0, len(retained)-1; left < right; left, right = left+1, right-1 {
		retained[left], retained[right] = retained[right], retained[left]
	}
	return retained
}

func cleanWatchPath(path string, allowEmpty bool) (string, bool) {
	if strings.IndexByte(path, 0) >= 0 || filepath.IsAbs(path) {
		return "", false
	}
	if path == "" && allowEmpty {
		return ".", true
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if cleaned == "." && !allowEmpty || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		if component == ".git" {
			return "", false
		}
	}
	return cleaned, true
}

func startFileWatches(ctx context.Context, deps dependencies, requests []fileWatchRequest) *fileWatchSubscription {
	if len(requests) == 0 || deps.fileWatcher == nil {
		return nil
	}
	set, _, err := liveRootSet(ctx, deps)
	if err != nil {
		return nil
	}
	targets := resolveFileWatches(set, requests)
	if len(targets) == 0 {
		return nil
	}
	watcher, err := deps.fileWatcher()
	if err != nil {
		return nil
	}
	byDirectory := make(map[string][]resolvedFileWatch)
	for _, target := range targets {
		byDirectory[target.directory] = append(byDirectory[target.directory], target)
	}
	for directory := range byDirectory {
		if err := watcher.Add(directory); err != nil {
			delete(byDirectory, directory)
		}
	}
	if len(byDirectory) == 0 {
		_ = watcher.Close()
		return nil
	}

	watchCtx, cancel := context.WithCancel(ctx)
	facts := make(chan fileChangeFact, maxFileWatchTargets)
	done := make(chan struct{})
	if deps.fileWatcherDelta != nil {
		deps.fileWatcherDelta(1)
	}
	go func() {
		defer close(done)
		defer watcher.Close()
		if deps.fileWatcherDelta != nil {
			defer deps.fileWatcherDelta(-1)
		}
		runFileWatch(watchCtx, watcher, byDirectory, facts)
	}()
	return &fileWatchSubscription{cancel: cancel, done: done, Facts: facts}
}

func runFileWatch(ctx context.Context, watcher *fsnotify.Watcher, byDirectory map[string][]resolvedFileWatch, facts chan<- fileChangeFact) {
	pending := make(map[fileChangeFact]time.Time)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var timerCh <-chan time.Time
	resetTimer := func() {
		if len(pending) == 0 {
			timerCh = nil
			return
		}
		var next time.Time
		for _, deadline := range pending {
			if next.IsZero() || deadline.Before(next) {
				next = deadline
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(max(next.Sub(time.Now()), 0))
		timerCh = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// Watch failures are intentionally silent; manual refresh remains.
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			eventPath := filepath.Clean(event.Name)
			directory := filepath.Dir(eventPath)
			matches := append([]resolvedFileWatch(nil), byDirectory[directory]...)
			if eventPath != directory {
				// fsnotify reports a watched directory's own rename/removal using
				// the directory path rather than a child path. Every logical target
				// beneath it may have vanished and must refetch honestly.
				matches = append(matches, byDirectory[eventPath]...)
			}
			for _, target := range matches {
				matched := target.file != "" && eventPath == target.file
				if target.file == "" {
					matched = eventPath != directory || eventPath == target.directory
				}
				if eventPath == target.directory {
					matched = true
				}
				if matched {
					pending[target.fact] = time.Now().Add(fileWatchDebounce)
				}
			}
			resetTimer()
		case now := <-timerCh:
			for fact, deadline := range pending {
				if deadline.After(now) {
					continue
				}
				select {
				case facts <- fact:
				default:
				}
				delete(pending, fact)
			}
			resetTimer()
		}
	}
}
