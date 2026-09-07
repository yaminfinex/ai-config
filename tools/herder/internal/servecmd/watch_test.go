package servecmd

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceWatchSnapshotTracksWrapperInputsOnly(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module test\n")
	write("cmd/herder/main.go", "package main\n")
	write("internal/pkg/code.go", "package pkg\n")
	write("internal/webui/dist/app.js", "one")
	write("internal/ignored.txt", "ignored")

	target := sourceWatchTarget{root: root}
	initial, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	write("internal/ignored.txt", "changed")
	ignored, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if ignored != initial {
		t.Fatal("non-wrapper input changed the source snapshot")
	}
	write("internal/webui/dist/app.js", "two")
	asset, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if asset == initial {
		t.Fatal("embedded distribution change did not change source snapshot")
	}
}

func TestDrainingHandlerRefusesNewRequestsWithOnePlainReason(t *testing.T) {
	handler := newDrainingHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("request reached handler after drain began")
	}))
	<-handler.beginDrain()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/events", nil))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Connection") != "close" || response.Body.String() != "server restarting\n" {
		t.Fatalf("response=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
}

func TestDrainingHandlerCancelsExistingSSEBeforeWaiting(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	handler := newDrainingHandler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}))
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/events", nil))
		close(done)
	}()
	<-entered
	idle := handler.beginDrain()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("drain did not cancel the existing SSE context")
	}
	select {
	case <-idle:
	case <-time.After(time.Second):
		t.Fatal("drain did not become idle after SSE cancellation")
	}
	<-done
}

func TestWatchReloadDrainsSlowRequestBeforeExec(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "finished")
	})
	executed := make(chan struct{})
	reload := make(chan watchConfig, 1)
	config := watchConfig{execPath: "/new/herder", exec: func(string, []string, []string) error {
		close(executed)
		return errors.New("test exec stopped")
	}}
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- serve([]net.Listener{listener}, handler, reload, time.Second, io.Discard, &stderr) }()
	responseDone := make(chan string, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String())
		if requestErr != nil {
			responseDone <- requestErr.Error()
			return
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		responseDone <- string(body)
	}()
	<-entered
	reload <- config
	select {
	case <-executed:
		t.Fatal("re-exec ran before the accepted request finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if body := <-responseDone; body != "finished" {
		t.Fatalf("slow response=%q", body)
	}
	select {
	case <-executed:
	case <-time.After(time.Second):
		t.Fatal("re-exec did not run after drain")
	}
	if code := <-done; code != 1 || !bytes.Contains(stderr.Bytes(), []byte("test exec stopped")) {
		t.Fatalf("serve=%d stderr=%q", code, stderr.String())
	}
}

func TestExecutableWatchSnapshotFollowsSymlinkReplacement(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	link := filepath.Join(root, "herder")
	if err := os.WriteFile(first, []byte("one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("two-two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	target := executableWatchTarget{path: link}
	before, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	after, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("symlink target replacement did not change executable snapshot")
	}
}
