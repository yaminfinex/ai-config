package servecmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdersock"
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
	if _, err := deps.store.Append(agentstore.Event{ID: agentstore.DerivedID([]byte("obs-e1")), At: time.Now().UTC(), Kind: agentstore.KindAssign, Name: "impl-kolo", Manager: "sesh-nabi", By: "ziru", ByKind: "agent"}); err != nil {
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
	// Every read is bounded so the mutation (hook removed) FAILS instead of
	// hanging the package.
	wait := func(label string) string {
		select {
		case v := <-order:
			return v
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no event within 5 s", label)
			return ""
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5 s")
	}
	if first, second := wait("first"), wait("second"); first != "socket closed" || second != "exec" {
		t.Fatalf("order = %s, %s", first, second)
	}
}

// D2. Reddens: a nil/failed store watcher leaving the projection cache frozen
// after the first fold (no poll recovery exists; the safety refold must).
func TestStoreWatcherFailureHasSafetyRefresh(t *testing.T) {
	deps := supervisionDeps(t)
	deps.fileWatcher = nil
	deps.transcriptSafety = 20 * time.Millisecond
	deps.storeWriter = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps = startStoreProjection(ctx, deps)
	before := readProjection(deps)
	if before == nil {
		t.Fatal("no initial projection")
	}
	if _, err := deps.store.Append(agentstore.Event{ID: agentstore.DerivedID([]byte("d2-e1")), At: time.Now().UTC(), Kind: agentstore.KindAssign, Name: "impl-kolo", Manager: "sesh-nabi", By: "ziru", ByKind: "agent"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if after := readProjection(deps); after != before && after.EventsOffset > before.EventsOffset {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("projection frozen at offset %d after the append with no watcher", before.EventsOffset)
}

// D2 (quiet store). Reddens: the safety pass refolding, and rewriting
// snapshot.json, when nothing changed.
func TestSafetyRefreshSkipsQuietStore(t *testing.T) {
	deps := supervisionDeps(t)
	deps.fileWatcher = nil
	deps.transcriptSafety = 10 * time.Millisecond
	deps.storeWriter = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps = startStoreProjection(ctx, deps)
	info, err := os.Stat(deps.store.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	foldsBefore := folds.Load()
	time.Sleep(120 * time.Millisecond)
	after, err := os.Stat(deps.store.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	if folds.Load() != foldsBefore || !after.ModTime().Equal(info.ModTime()) {
		t.Fatalf("quiet store refolded: folds %d→%d, snapshot mtime %s→%s", foldsBefore, folds.Load(), info.ModTime(), after.ModTime())
	}
}

// M7. Reddens: answerVitals answering from anything but observer.Lookup — a
// known awaiting_file session must be a socket miss exactly as it is a
// Lookup miss, and a hit must carry Lookup's fields.
func TestAnswerVitalsMatchesLookupMiss(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	missing := hcomidentity.Row{Name: "fresh", Tool: "claude", Directory: "/invented/none", SessionID: "73100000-0000-4000-8000-000000000001"}
	obs := observer.New(observer.Options{Roster: func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{missing}, nil }, Home: home, Poll: time.Hour, Sweep: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obs.Run(ctx)
	if _, ok := obs.Lookup(missing); ok {
		t.Fatal("awaiting_file must be a Lookup miss")
	}
	answer := answerVitals(obs)
	if got := answer(herdersock.Request{Op: herdersock.OpVitals, Tool: missing.Tool, Session: missing.SessionID}); !got.Miss || got.IsHit() {
		t.Fatalf("socket answer for awaiting_file = %+v; want miss", got)
	}
	seeded, row, _ := seededObserver(t)
	want, _ := seeded.Lookup(row)
	got := answerVitals(seeded)(herdersock.Request{Op: herdersock.OpVitals, Tool: row.Tool, Session: row.SessionID})
	if !got.IsHit() || got.Vitals.Model != want.Vitals.Model || got.Path != want.Path || !got.ObservedAt.Equal(want.ObservedAt) {
		t.Fatalf("socket answer %+v != Lookup %+v", got, want)
	}
}

// M11. Reddens: a fold per SSE connection — two connections and one append
// must cost exactly one fold and hand both the same projection pointer.
func TestTwoConnectionsOneFold(t *testing.T) {
	deps := supervisionDeps(t)
	deps.poll = time.Hour
	deps.fileWatcher = fsnotify.NewWatcher
	deps.storeWriter = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps = startStoreProjection(ctx, deps)
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	readers := make([]*bufio.Reader, 0, 2)
	for i := 0; i < 2; i++ {
		response, err := http.Get(server.URL + "/api/events")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		reader := bufio.NewReader(response.Body)
		for {
			if event, _ := readEvent(t, reader); event == "fleet" {
				break
			}
		}
		readers = append(readers, reader)
	}
	foldsBefore := folds.Load()
	if _, err := deps.store.Append(agentstore.Event{ID: agentstore.DerivedID([]byte("m11-e1")), At: time.Now().UTC(), Kind: agentstore.KindAssign, Name: "impl-kolo", Manager: "sesh-nabi", By: "ziru", ByKind: "agent"}); err != nil {
		t.Fatal(err)
	}
	for i, reader := range readers {
		got := make(chan string, 1)
		go func() {
			for {
				if event, data := readEvent(t, reader); event == "fleet" {
					got <- data
					return
				}
			}
		}()
		select {
		case data := <-got:
			if !strings.Contains(data, `"manager":"sesh-nabi"`) {
				t.Fatalf("client %d fleet after append = %s", i, data)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("client %d did not receive fleet after the append", i)
		}
	}
	if delta := folds.Load() - foldsBefore; delta != 1 {
		t.Fatalf("two clients, one append: %d folds, want 1", delta)
	}
	if readProjection(deps) != readProjection(deps) {
		t.Fatal("projection pointer differs between reads")
	}
}
