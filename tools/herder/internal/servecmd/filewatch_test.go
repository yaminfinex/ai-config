package servecmd

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-config/tools/herder/internal/fileroots"
	"ai-config/tools/herder/internal/hcomidentity"
	"github.com/fsnotify/fsnotify"
)

func realFileWatchDeps(root string) dependencies {
	deps := fixtureDeps()
	deps.fileWatcher = fsnotify.NewWatcher
	deps.roots = func(context.Context, []string, []hcomidentity.Row) (fileroots.Set, error) {
		return fileroots.Set{Roots: []string{root}, Configured: []string{root}, AgentRoot: map[string]string{}}, nil
	}
	return deps
}

func waitForFileFact(t *testing.T, facts <-chan fileChangeFact, match func(fileChangeFact) bool) fileChangeFact {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case fact := <-facts:
			if match(fact) {
				return fact
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for file-change fact")
		}
	}
}

func TestRealFileWatcherObservesOperationsAndCoalescesRapidWrites(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "note.md")
	if err := os.WriteFile(file, []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subscription := startFileWatches(ctx, realFileWatchDeps(root), []fileWatchRequest{
		{Kind: "folder", Root: root, Path: ""},
		{Kind: "file", Root: root, Path: "note.md"},
	})
	if subscription == nil {
		t.Fatal("real fsnotify subscription was not created")
	}
	defer subscription.Close()

	created := filepath.Join(root, "created.txt")
	if err := os.WriteFile(created, []byte("created"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileFact(t, subscription.Facts, func(fact fileChangeFact) bool { return fact.Kind == "folder" })

	for index := range 8 {
		if err := os.WriteFile(file, []byte(strconv.Itoa(index)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	waitForFileFact(t, subscription.Facts, func(fact fileChangeFact) bool { return fact.Kind == "file" && fact.Path == "note.md" })
	select {
	case fact := <-subscription.Facts:
		if fact.Kind == "file" && fact.Path == "note.md" {
			t.Fatalf("rapid write storm emitted a duplicate fact: %+v", fact)
		}
	case <-time.After(2 * fileWatchDebounce):
	}

	moved := filepath.Join(root, "moved.md")
	if err := os.Rename(file, moved); err != nil {
		t.Fatal(err)
	}
	waitForFileFact(t, subscription.Facts, func(fact fileChangeFact) bool { return fact.Kind == "file" })
	if err := os.Rename(moved, file); err != nil {
		t.Fatal(err)
	}
	waitForFileFact(t, subscription.Facts, func(fact fileChangeFact) bool { return fact.Kind == "file" })
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	waitForFileFact(t, subscription.Facts, func(fact fileChangeFact) bool { return fact.Kind == "file" })
}

func TestRealFileWatcherRetainsNewestSixtyFourDirectories(t *testing.T) {
	root := t.TempDir()
	requests := make([]fileWatchRequest, 0, maxFileWatchDirectories+2)
	for index := 0; index < maxFileWatchDirectories+2; index++ {
		path := "dir-" + strconv.Itoa(index)
		if err := os.Mkdir(filepath.Join(root, path), 0o700); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, fileWatchRequest{Kind: "folder", Root: root, Path: path})
	}
	subscription := startFileWatches(context.Background(), realFileWatchDeps(root), requests)
	if subscription == nil {
		t.Fatal("real bounded subscription was not created")
	}
	defer subscription.Close()
	for _, index := range []int{0, 2, maxFileWatchDirectories + 1} {
		path := filepath.Join(root, "dir-"+strconv.Itoa(index), "marker")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for !seen["dir-2"] || !seen["dir-65"] {
		select {
		case fact := <-subscription.Facts:
			seen[fact.Path] = true
		case <-deadline.C:
			t.Fatalf("retained directory facts=%v", seen)
		}
	}
	if seen["dir-0"] {
		t.Fatal("oldest directory was watched despite the 64-directory cap")
	}
}

func TestEventStreamEmitsRealFileFactAndReclaimsWatcherOnClose(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "live.md")
	if err := os.WriteFile(file, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := realFileWatchDeps(root)
	deps.poll = time.Hour
	deps.heartbeat = time.Hour
	var active atomic.Int64
	deps.fileWatcherDelta = func(delta int) { active.Add(int64(delta)) }
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	watches, err := json.Marshal([]fileWatchRequest{{Kind: "file", Root: root, Path: "live.md"}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL + "/api/events?watches=" + url.QueryEscape(string(watches)))
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("first event=%q", event)
	}
	if event, _ := readEvent(t, reader); event != "fleet" {
		t.Fatalf("second event=%q", event)
	}
	if active.Load() != 1 {
		t.Fatalf("active real watchers=%d", active.Load())
	}
	if err := os.WriteFile(file, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	event, data := readEvent(t, reader)
	if event != "file-change" || !strings.Contains(data, `"kind":"file"`) || !strings.Contains(data, `"path":"live.md"`) {
		t.Fatalf("event=%q data=%s", event, data)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for active.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if active.Load() != 0 {
		t.Fatalf("watcher leaked after SSE close: active=%d", active.Load())
	}
}
