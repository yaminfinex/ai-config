package servecmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func waitForTranscriptBatch(t *testing.T, changes <-chan []string) []string {
	t.Helper()
	select {
	case names := <-changes:
		return names
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transcript change batch")
		return nil
	}
}

func TestTranscriptWatcherCoalescesBurstsAndObservesAtomicReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	if err := os.WriteFile(path, []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	subscription, err := startTranscriptWatches(context.Background(), fsnotify.NewWatcher, transcriptWatchDebounce)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if err := subscription.Update([]transcriptWatchTarget{{Agent: "dore", Path: path}}); err != nil {
		t.Fatal(err)
	}

	for index := range 8 {
		file, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if openErr != nil {
			t.Fatal(openErr)
		}
		_, writeErr := file.WriteString(strconv.Itoa(index))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("write=%v close=%v", writeErr, closeErr)
		}
	}
	if names := waitForTranscriptBatch(t, subscription.Changes); len(names) != 1 || names[0] != "dore" {
		t.Fatalf("burst names=%v", names)
	}
	select {
	case duplicate := <-subscription.Changes:
		t.Fatalf("write burst emitted duplicate batch %v", duplicate)
	case <-time.After(2 * transcriptWatchDebounce):
	}

	replacement := filepath.Join(root, "replacement.jsonl")
	if err := os.WriteFile(replacement, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if names := waitForTranscriptBatch(t, subscription.Changes); len(names) != 1 || names[0] != "dore" {
		t.Fatalf("replacement names=%v", names)
	}
}

func TestTranscriptWatcherRebuildsPathsAndSupportsOneHundredPanels(t *testing.T) {
	root := t.TempDir()
	subscription, err := startTranscriptWatches(context.Background(), fsnotify.NewWatcher, transcriptWatchDebounce)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	targets := make([]transcriptWatchTarget, 0, 100)
	for index := range 100 {
		directory := filepath.Join(root, strconv.Itoa(index))
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "session.jsonl")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, transcriptWatchTarget{Agent: "agent-" + strconv.Itoa(index), Path: path})
	}
	if err := subscription.Update(targets); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targets[99].Path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if names := waitForTranscriptBatch(t, subscription.Changes); len(names) != 1 || names[0] != "agent-99" {
		t.Fatalf("hundred-panel names=%v", names)
	}

	newPath := filepath.Join(root, "rotated.jsonl")
	if err := os.WriteFile(newPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := subscription.Update([]transcriptWatchTarget{{Agent: "agent-99", Path: newPath}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("resumed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if names := waitForTranscriptBatch(t, subscription.Changes); len(names) != 1 || names[0] != "agent-99" {
		t.Fatalf("rebuilt names=%v", names)
	}
}

func TestTranscriptWatcherSetupFailureIsReturnedForSafetySweepFallback(t *testing.T) {
	want := errors.New("watch limit reached")
	if subscription, err := startTranscriptWatches(context.Background(), func() (*fsnotify.Watcher, error) {
		return nil, want
	}, transcriptWatchDebounce); subscription != nil || !errors.Is(err, want) {
		t.Fatalf("subscription=%v err=%v", subscription, err)
	}
}

func TestTranscriptWatcherRuntimeStopIsReportedForSafetySweepFallback(t *testing.T) {
	subscription, err := startTranscriptWatches(context.Background(), fsnotify.NewWatcher, transcriptWatchDebounce)
	if err != nil {
		t.Fatal(err)
	}
	if err := subscription.watcher.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case watchErr := <-subscription.Errors:
		if watchErr == nil {
			t.Fatal("runtime watcher stop reported a nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime watcher stop was silent")
	}
	subscription.Close()
}
