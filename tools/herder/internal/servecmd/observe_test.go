package servecmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/observer"
	"github.com/fsnotify/fsnotify"
)

func seededObserver(t *testing.T) (*observer.Observer, hcomidentity.Row, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	id := "73100000-0000-4000-8000-000000000731"
	path := filepath.Join(home, ".claude", "projects", "-invented-violet", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	record := `{"type":"assistant","isSidechain":false,"message":{"model":"invented-observed","usage":{"input_tokens":11,"cache_creation_input_tokens":101,"cache_read_input_tokens":1009,"output_tokens":19}}}` + "\n"
	if err := os.WriteFile(path, []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	row := hcomidentity.Row{Name: "vile", Tool: "claude", Directory: "/invented/violet", SessionID: id}
	obs := observer.New(observer.Options{Roster: func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{row}, nil }, Home: home, Poll: time.Hour, Sweep: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	obs.Run(ctx)
	return obs, row, path
}

// Reddens: cache bypassed. The transcript is deleted after seeding, so a
// direct read would fail; the detail endpoint still answers from the observer.
func TestReadAgentVitalsServedFromObserver(t *testing.T) {
	obs, row, path := seededObserver(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	vitals, err := readAgentVitals(obs.Lookup)(row)
	if err != nil || vitals.Model != "invented-observed" || vitals.ContextUsage == nil || vitals.ContextUsage.UsedTokens != 1121 {
		t.Fatalf("vitals = %+v, err %v", vitals, err)
	}
}

// Reddens: a miss becoming an empty answer instead of the direct read.
func TestReadAgentVitalsFallsBackWhenUnknown(t *testing.T) {
	_, row, _ := seededObserver(t)
	empty := observer.New(observer.Options{Home: os.Getenv("HOME")})
	vitals, err := readAgentVitals(empty.Lookup)(row)
	if err != nil || vitals.Model != "invented-observed" {
		t.Fatalf("direct fallback = %+v, err %v", vitals, err)
	}
	if vitals, err := readAgentVitals(nil)(row); err != nil || vitals.Model != "invented-observed" {
		t.Fatalf("nil cache = %+v, err %v", vitals, err)
	}
}

// Reddens: per-client folds (two board reads would hold different
// projections); snapshot.json never refreshed or unequal to a full replay.
func TestStoreProjectionSharedAndSnapshotRefreshed(t *testing.T) {
	deps := supervisionDeps(t)
	deps.poll = time.Hour
	deps.fileWatcher = fsnotify.NewWatcher
	deps.storeWriter = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps = startStoreProjection(ctx, deps)
	first := readProjection(deps)
	second := readProjection(deps)
	if first == nil || first != second {
		t.Fatalf("two reads, two folds: %p vs %p", first, second)
	}
	before, err := os.ReadFile(deps.store.SnapshotPath())
	if err != nil {
		t.Fatalf("snapshot.json not written at start: %v", err)
	}
	if _, err := deps.store.Append(agentstore.Event{ID: agentstore.DerivedID([]byte("obs-e1")), At: time.Now().UTC(), Kind: agentstore.KindReparent, Name: "impl-kolo", Manager: "sesh-nabi", By: "ziru", ByKind: "agent"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if after, _ := os.ReadFile(deps.store.SnapshotPath()); len(after) > 0 && !bytes.Equal(after, before) {
			replay, err := deps.store.Replay()
			if err != nil {
				t.Fatal(err)
			}
			want, _ := replay.Marshal()
			if !bytes.Equal(after, want) {
				t.Fatalf("snapshot != replay\n%s\n%s", after, want)
			}
			if readProjection(deps) == first {
				t.Fatal("projection not refreshed after the store change")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("snapshot.json not refreshed within the debounce")
}

// Reddens: a socket-loser serve writing snapshot.json.
func TestSocketLoserDoesNotWriteSnapshot(t *testing.T) {
	deps := supervisionDeps(t)
	deps.storeWriter = false
	deps.projection = &projectionCache{}
	refreshProjection(deps)
	if readProjection(deps) == nil {
		t.Fatal("loser serve still needs a projection")
	}
	if _, err := os.Stat(deps.store.SnapshotPath()); !os.IsNotExist(err) {
		t.Fatalf("snapshot.json written by the non-writer: %v", err)
	}
}

// Reddens: the socket left behind across a --watch re-exec.
func TestReloadRunsBeforeExecHook(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan string, 2)
	reload := make(chan watchConfig, 1)
	config := watchConfig{execPath: "/new/herder", exec: func(string, []string, []string) error {
		order <- "exec"
		return errors.New("test exec stopped")
	}}
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- serve([]net.Listener{listener}, http.NotFoundHandler(), reload, time.Second, func() { order <- "socket closed" }, io.Discard, &stderr)
	}()
	reload <- config
	<-done
	if first, second := <-order, <-order; first != "socket closed" || second != "exec" {
		t.Fatalf("order = %s, %s", first, second)
	}
}
