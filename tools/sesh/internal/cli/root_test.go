package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"sesh/internal/index"
	"sesh/internal/setup"
	"sesh/internal/ship"
	"sesh/internal/store"
	"sesh/internal/wire"
	"tailscale.com/tailcfg"
)

func TestHelpListsAllSubcommands(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--help: unexpected error: %v", err)
	}
	for _, name := range []string{"ship", "serve", "reindex", "status", "admin", "setup", "update"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("--help output missing subcommand %q\n%s", name, out.String())
		}
	}
}

// TestShipRequiresStoreURL: `ship` is real since U4; its whole config
// surface is the store URL, and running without one must fail up front
// rather than start a daemon that can only hold position.
func TestShipRequiresStoreURL(t *testing.T) {
	t.Setenv("SESH_STORE_URL", "")
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"ship"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "SESH_STORE_URL") {
		t.Errorf("sesh ship without a store URL: want config error naming SESH_STORE_URL, got %v", err)
	}
}

func TestServeRejectsNonLoopbackBind(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"serve", "--addr", "0.0.0.0:0", "--data-dir", t.TempDir()})
	err := root.Execute()
	if err == nil {
		t.Fatal("sesh serve should reject non-loopback bind before M4")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("serve error %q does not mention loopback", err)
	}
}

func TestServeRejectsNonLoopbackSurfaceBind(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--surface-addr", "0.0.0.0:0", "--data-dir", t.TempDir()})
	err := root.Execute()
	if err == nil {
		t.Fatal("sesh serve should reject non-loopback surface bind before M4")
	}
	if !strings.Contains(err.Error(), "loopback") || !strings.Contains(err.Error(), "surface") {
		t.Fatalf("serve error %q does not mention surface loopback", err)
	}
}

func TestServeHTTPStopsBothListenersAfterInflightDurableWrite(t *testing.T) {
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir(), AppendBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ingest, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	read, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = ingest.Close()
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	ingestHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		st.Handler().ServeHTTP(w, r)
	})
	ctx, cancel := context.WithCancel(t.Context())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serveHTTP(ctx,
			httpEndpoint{listener: ingest, handler: ingestHandler},
			httpEndpoint{listener: read, handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })},
		)
	}()

	body := []byte(`{"type":"user","uuid":"shutdown-write","sessionId":"11111111-1111-1111-1111-111111111111"}` + "\n")
	req, err := http.NewRequest(http.MethodPut, "http://"+ingest.Addr().String()+"/v1/files/claude/11111111-1111-1111-1111-111111111111/22222222-2222-2222-2222-222222222222/bytes?offset=0", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	responseDone := make(chan *http.Response, 1)
	requestErr := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			requestErr <- err
			return
		}
		responseDone <- resp
	}()
	<-entered
	cancel()
	assertListenerStopsAccepting(t, ingest.Addr().String())
	assertListenerStopsAccepting(t, read.Addr().String())
	close(release)
	select {
	case err := <-requestErr:
		t.Fatalf("in-flight durable write failed during graceful shutdown: %v", err)
	case resp := <-responseDone:
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("in-flight durable write status = %d, want 200", resp.StatusCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight durable write did not finish")
	}
	if err := <-serveDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("serveHTTP shutdown error = %v, want context cancellation", err)
	}
	path := st.MirrorPath(wire.ToolClaude, "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", 0)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("durably ACKed mirror = %q, want %q", got, body)
	}
}

func TestServeReturnsThroughCommandOnInterruptAndTerminate(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			ingestAddr := unusedLoopbackAddr(t)
			surfaceAddr := unusedLoopbackAddr(t)
			marker := filepath.Join(t.TempDir(), "returned")
			cmd := exec.Command(os.Args[0], "-test.run=^TestServeSignalHelper$")
			cmd.Env = append(os.Environ(),
				"SESH_TEST_SERVE_HELPER=1",
				"SESH_TEST_INGEST_ADDR="+ingestAddr,
				"SESH_TEST_SURFACE_ADDR="+surfaceAddr,
				"SESH_TEST_DATA_DIR="+t.TempDir(),
				"SESH_TEST_RETURNED_MARKER="+marker,
			)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waitForListener(t, ingestAddr)
			waitForListener(t, surfaceAddr)
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("serve after %s: %v\n%s", sig, err, output.String())
				}
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("serve did not exit after %s\n%s", sig, output.String())
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("serve did not return through command after %s: %v\n%s", sig, err, output.String())
			}
			assertListenerStopsAccepting(t, ingestAddr)
			assertListenerStopsAccepting(t, surfaceAddr)
		})
	}
}

func TestServeSignalHelper(t *testing.T) {
	if os.Getenv("SESH_TEST_SERVE_HELPER") != "1" {
		return
	}
	root := newRoot()
	root.SetArgs([]string{"serve",
		"--addr", os.Getenv("SESH_TEST_INGEST_ADDR"),
		"--surface-addr", os.Getenv("SESH_TEST_SURFACE_ADDR"),
		"--data-dir", os.Getenv("SESH_TEST_DATA_DIR"),
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("SESH_TEST_RETURNED_MARKER"), []byte("returned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func unusedLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitForListener(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("listener %s did not start: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertListenerStopsAccepting(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatalf("listener %s still accepts connections", addr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTSNetServePlanWrapsHandlersWithTSNetWhoIs(t *testing.T) {
	ts := &fakeTSNetServer{
		result: store.WhoIsResult{
			Identity: "alice@example.com",
			CapMap:   testCapMap(store.CapabilityShip),
		},
	}
	ingestCalled := false
	ingest := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ingestCalled = true
		if got := store.TailnetIdentityFromContext(r.Context()); got != "alice@example.com" {
			t.Fatalf("ingest tailnet identity = %q, want WhoIs identity", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	surfaceCalled := false
	surface := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		surfaceCalled = true
		if got := store.TailnetIdentityFromContext(r.Context()); got != "alice@example.com" {
			t.Fatalf("surface tailnet identity = %q, want WhoIs identity", got)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	plan := newTSNetServePlan(ts, ingest, surface, "127.0.0.1:8765", "127.0.0.1:8766")
	if plan.ingestAddr != ":8765" || plan.surfaceAddr != ":8766" {
		t.Fatalf("tsnet addrs = %q/%q, want :8765/:8766", plan.ingestAddr, plan.surfaceAddr)
	}

	ingestReq := httptest.NewRequest(http.MethodPut, "/v1/files/claude/s/f/bytes?offset=0", nil)
	ingestReq.RemoteAddr = "100.64.0.1:12345"
	ingestRR := httptest.NewRecorder()
	plan.ingestHandler.ServeHTTP(ingestRR, ingestReq)
	if ingestRR.Code != http.StatusNoContent || !ingestCalled {
		t.Fatalf("ingest status=%d called=%v body=%s", ingestRR.Code, ingestCalled, ingestRR.Body.String())
	}
	if len(ts.remotes) != 1 || ts.remotes[0] != "100.64.0.1:12345" {
		t.Fatalf("WhoIs remotes after ingest = %v", ts.remotes)
	}

	readReq := httptest.NewRequest(http.MethodGet, "/", nil)
	readReq.RemoteAddr = "100.64.0.2:23456"
	readDenied := httptest.NewRecorder()
	plan.surfaceHandler.ServeHTTP(readDenied, readReq)
	if readDenied.Code != wire.ErrOutOfGrant.HTTPStatus() || surfaceCalled {
		t.Fatalf("read with ship cap status=%d surfaceCalled=%v", readDenied.Code, surfaceCalled)
	}

	ts.result.CapMap = testCapMap(store.CapabilityRead)
	readOK := httptest.NewRecorder()
	plan.surfaceHandler.ServeHTTP(readOK, readReq)
	if readOK.Code != http.StatusAccepted || !surfaceCalled {
		t.Fatalf("read with read cap status=%d surfaceCalled=%v body=%s", readOK.Code, surfaceCalled, readOK.Body.String())
	}
	if len(ts.remotes) != 3 || ts.remotes[2] != "100.64.0.2:23456" {
		t.Fatalf("WhoIs remotes after read = %v", ts.remotes)
	}
}

// TestTSNetDistributionRouteScopedAuth: design §3 — the distribution routes
// on the ingest listener admit EITHER verb (ship or read), wire ingest stays
// ship-only, and no-verb callers are denied everywhere.
func TestTSNetDistributionRouteScopedAuth(t *testing.T) {
	ts := &fakeTSNetServer{result: store.WhoIsResult{Identity: "alice@example.com"}}
	ingest := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	surface := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	plan := newTSNetServePlan(ts, ingest, surface, ":8765", ":8766")

	serve := func(method, path string, verbs ...string) int {
		ts.result.CapMap = testCapMap(verbs...)
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "100.64.0.9:4242"
		rr := httptest.NewRecorder()
		plan.ingestHandler.ServeHTTP(rr, req)
		return rr.Code
	}
	denied := wire.ErrOutOfGrant.HTTPStatus()

	// Read-only callers reach the distribution surface on the ingest port…
	if got := serve(http.MethodGet, "/install.sh", store.CapabilityRead); got != http.StatusOK {
		t.Errorf("read-only GET /install.sh = %d, want 200", got)
	}
	if got := serve(http.MethodGet, "/releases/latest/VERSION", store.CapabilityRead); got != http.StatusOK {
		t.Errorf("read-only GET /releases/latest/VERSION = %d, want 200", got)
	}
	// …but must not gain PUT ingest.
	if got := serve(http.MethodPut, "/v1/files/claude/s/f/bytes?offset=0", store.CapabilityRead); got != denied {
		t.Errorf("read-only PUT ingest = %d, want %d", got, denied)
	}
	// Ship-only callers keep both ingest and distribution.
	if got := serve(http.MethodPut, "/v1/files/claude/s/f/bytes?offset=0", store.CapabilityShip); got != http.StatusOK {
		t.Errorf("ship PUT ingest = %d, want 200", got)
	}
	if got := serve(http.MethodGet, "/releases/v1/sesh-linux-amd64", store.CapabilityShip); got != http.StatusOK {
		t.Errorf("ship GET release asset = %d, want 200", got)
	}
	// No-verb callers are denied on every route, distribution included.
	if got := serve(http.MethodGet, "/install.sh"); got != denied {
		t.Errorf("no-verb GET /install.sh = %d, want %d", got, denied)
	}
	if got := serve(http.MethodGet, "/releases/latest/VERSION"); got != denied {
		t.Errorf("no-verb GET /releases/latest/VERSION = %d, want %d", got, denied)
	}
	if got := serve(http.MethodPut, "/v1/files/claude/s/f/bytes?offset=0"); got != denied {
		t.Errorf("no-verb PUT ingest = %d, want %d", got, denied)
	}
}

func TestReindexRunsOnEmptyStore(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"reindex", "--data-dir", t.TempDir()})
	if err := root.Execute(); err != nil {
		t.Fatalf("sesh reindex on empty store: %v", err)
	}
}

type fakeTSNetServer struct {
	result  store.WhoIsResult
	remotes []string
}

func (f *fakeTSNetServer) Listen(string, string) (net.Listener, error) {
	panic("not used")
}

func (f *fakeTSNetServer) WhoIs(_ context.Context, remoteAddr string) (store.WhoIsResult, error) {
	f.remotes = append(f.remotes, remoteAddr)
	return f.result, nil
}

func (f *fakeTSNetServer) Close() error {
	return nil
}

func testCapMap(verbs ...string) tailcfg.PeerCapMap {
	values := make([]tailcfg.RawMessage, 0, len(verbs))
	for _, verb := range verbs {
		values = append(values, tailcfg.RawMessage(`{"verb":"`+verb+`"}`))
	}
	return tailcfg.PeerCapMap{store.TailnetCapabilitySesh: values}
}

func TestStatusReportsHealthyStore(t *testing.T) {
	stateDir := t.TempDir()
	writeCursor(t, stateDir, ship.Cursor{
		Tool:      wire.ToolClaude,
		SessionID: "11111111-1111-1111-1111-111111111111",
		FileUUID:  "22222222-2222-2222-2222-222222222222",
		Offset:    12,
		LastAckAt: time.Now().Add(-1 * time.Minute).UTC(),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status", "--state-dir", stateDir, "--store-url", server.URL + "/"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status healthy: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "cursors: 1") || !strings.Contains(out.String(), "store: reachable") {
		t.Fatalf("status output missing healthy facts:\n%s", out.String())
	}
}

func TestStatusFailsOnUnreachableStore(t *testing.T) {
	stateDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	server.Close()

	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status", "--state-dir", stateDir, "--store-url", server.URL})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "store unreachable") {
		t.Fatalf("status unreachable error = %v output:\n%s", err, out.String())
	}
}

func TestStatusResolvesStoreURLFromInstalledConfig(t *testing.T) {
	// No --store-url and no SESH_STORE_URL: interactive status must resolve
	// the URL from the installed service config the way `sesh update` does
	// (on macOS the URL lives only in the launchd plist the service reads;
	// this exercises the same resolution seam through the drop-in on the
	// test platform — setup.InstalledStoreURL's darwin branch is unit-tested
	// beside it).
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SESH_STORE_URL", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	dropin := setup.DropinPath(home)
	if err := os.MkdirAll(filepath.Dir(dropin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropin, setup.RenderDropin(nil, server.URL), 0o644); err != nil {
		t.Fatal(err)
	}

	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status", "--state-dir", t.TempDir()})
	if err := root.Execute(); err != nil {
		t.Fatalf("status with installed config: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "store: reachable") {
		t.Fatalf("status did not resolve the store URL from the installed config:\n%s", out.String())
	}
}

func TestStatusNamesConfigPathWhenUnconfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SESH_STORE_URL", "")

	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status", "--state-dir", t.TempDir()})
	if err := root.Execute(); err != nil {
		t.Fatalf("status unconfigured: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "store: not configured") || !strings.Contains(out.String(), setup.DropinPath(home)) {
		t.Fatalf("unconfigured status must name the consulted config path:\n%s", out.String())
	}
}

func TestStatusFailsOnPoisonedCursor(t *testing.T) {
	// HOME is pinned to keep the test off any real installed service config
	// on the machine running it (status would otherwise ping a live store).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SESH_STORE_URL", "")
	stateDir := t.TempDir()
	writeCursor(t, stateDir, ship.Cursor{
		Tool:      wire.ToolClaude,
		SessionID: "11111111-1111-1111-1111-111111111111",
		FileUUID:  "22222222-2222-2222-2222-222222222222",
		Offset:    12,
		Poisoned:  true,
	})

	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status", "--state-dir", stateDir})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("status poisoned error = %v output:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "POISONED") {
		t.Fatalf("status output missing poison marker:\n%s", out.String())
	}
}

func TestAdminDropFileRefusesWithoutYes(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"admin", "drop-file", "claude", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "--data-dir", t.TempDir()})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("drop-file without --yes error = %v", err)
	}
}

func TestIndexConsumerDrainsServeAppendEvents(t *testing.T) {
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir(), AppendBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	})
	idx, err := index.New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	consumer := startIndexConsumer(ctx, st, idx, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	t.Cleanup(func() {
		if err := consumer.StopAndWait(); err != nil {
			t.Error(err)
		}
	})

	body := []byte(`{"type":"user","uuid":"ok","sessionId":"11111111-1111-1111-1111-111111111111"}` + "\n")
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/11111111-1111-1111-1111-111111111111/22222222-2222-2222-2222-222222222222/bytes?offset=0", bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	var ack wire.Ack
	if err := json.Unmarshal(rr.Body.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		rows, err := idx.RowCount(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if rows == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("index consumer did not drain append event, rows=%d", rows)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRouteClassNeverJournalsIdentifiers pins the debug-log redaction
// contract: parameterized routes collapse to their template, only the finite
// fixed-route allowlist passes through verbatim, and every unknown path —
// client-controlled input, possibly identifier-bearing — collapses to
// "other".
func TestRouteClassNeverJournalsIdentifiers(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/", "/"},
		{"/nodes", "/nodes"},
		{"/sessions", "/sessions"},
		{"/fragments/recency", "/fragments/recency"},
		{"/install.sh", "/install.sh"},
		{"/v1/health", "/v1/health"},
		{"/v1/nodes", "/v1/nodes"},
		{"/v1/files/claude/11111111-1111-1111-1111-111111111111/22222222-2222-2222-2222-222222222222/bytes", "/v1/files/*"},
		{"/s/claude/11111111-1111-1111-1111-111111111111", "/s/*"},
		{"/s/claude/11111111-1111-1111-1111-111111111111/raw", "/s/*"},
		{"/releases/sesh-v0.1.2/sesh-linux-amd64", "/releases/*"},
		{"/assets/htmx.min.js", "/assets/*"},
		// Unknown paths are input-controlled and may carry identifiers;
		// they must never journal verbatim.
		{"/probe/11111111-1111-1111-1111-111111111111", "other"},
		{"/v1/unknown", "other"},
		{"/nodes/extra", "other"},
	}
	for _, c := range cases {
		if got := routeClass(c.path); got != c.want {
			t.Errorf("routeClass(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestReadPathsServeWhileWriteConnectionHeld is the serving-path regression
// gate for the surface remote-TTFB pathology: append transactions hold the
// store's single write connection for corpus-scale index work, so every
// read-serving route (the surface pages, node status on both listeners) must
// read through the store's read-only pool — WAL readers run concurrently
// with the writer. The gate pins the property directly: with a write
// transaction held open, the reads must still complete with real content. A
// regression back onto the write connection blocks these requests (timeout)
// or degrades the page (content assertion). What this cannot prove without a
// real tailnet: the remote-RTT numbers themselves; those come from probes
// against the live store (the post-deploy AFTER probe is pending — see the
// read/write-split design note).
func TestReadPathsServeWhileWriteConnectionHeld(t *testing.T) {
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// index.New creates the index schema the recency queries read, exactly
	// as serve does; the consumer itself is not needed here.
	if _, err := index.New(t.Context(), st.DB(), st.MirrorPath); err != nil {
		t.Fatal(err)
	}

	// One mirrored line so the pages render real rows (raw fallback path;
	// no index consumer needed).
	body := []byte(`{"type":"user","uuid":"gate","sessionId":"11111111-1111-1111-1111-111111111111"}` + "\n")
	put := httptest.NewRequest(http.MethodPut, "/v1/files/claude/11111111-1111-1111-1111-111111111111/22222222-2222-2222-2222-222222222222/bytes?offset=0", bytes.NewReader(body))
	put.Header.Set("Content-Type", wire.ContentTypeBytes)
	put.Header.Set(wire.HeaderWireVersion, "1")
	put.Header.Set(wire.HeaderHostname, "node-a")
	put.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, put)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Take the single write connection and hold it inside an open write
	// transaction, the same state a long append transaction produces.
	tx, err := st.DB().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO last_seen(hostname, os_user, last_put_at) VALUES ('held-writer', 'held-writer', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	surfaceHandler, surfaceStore := newSurfaceHandler(st)
	defer surfaceStore.Close()
	storeHandler := st.Handler()
	checks := []struct {
		name    string
		handler http.Handler
		target  string
		want    string
	}{
		{"surface nodes", surfaceHandler, "/", "node-a"},
		{"surface recency", surfaceHandler, "/sessions", "11111111-1111-1111-1111-111111111111"},
		{"store nodes", storeHandler, "/v1/nodes", "node-a"},
	}
	for _, check := range checks {
		type result struct {
			code int
			body string
		}
		done := make(chan result, 1)
		go func() {
			rec := httptest.NewRecorder()
			check.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, check.target, nil))
			done <- result{rec.Code, rec.Body.String()}
		}()
		select {
		case got := <-done:
			if got.code != http.StatusOK {
				t.Errorf("%s: status %d while the write connection is held", check.name, got.code)
			}
			if !strings.Contains(got.body, check.want) {
				t.Errorf("%s: response lacks %q while the write connection is held (degraded render?)\n%s", check.name, check.want, got.body)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: read blocked behind the held write connection", check.name)
		}
	}
}

func TestIndexConsumerDrainsQueuedEventsBeforeStop(t *testing.T) {
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir(), AppendBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	idx, err := index.New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"user","uuid":"drain-on-stop","sessionId":"11111111-1111-1111-1111-111111111111"}` + "\n")
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/11111111-1111-1111-1111-111111111111/22222222-2222-2222-2222-222222222222/bytes?offset=0", bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	consumer := startIndexConsumer(t.Context(), st, idx, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err := consumer.StopAndWait(); err != nil {
		t.Fatal(err)
	}
	rows, err := idx.RowCount(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("rows after consumer stop = %d, want queued event drained", rows)
	}
}

func TestIndexConsumerMarksBufferedGenerationsDirtyOnDrainTimeout(t *testing.T) {
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir(), AppendBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	idx, err := index.New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	put := func(sessionID, fileUUID, messageUUID string) {
		body := []byte(`{"type":"user","uuid":"` + messageUUID + `","sessionId":"` + sessionID + `"}` + "\n")
		req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/"+sessionID+"/"+fileUUID+"/bytes?offset=0", bytes.NewReader(body))
		req.Header.Set("Content-Type", wire.ContentTypeBytes)
		req.Header.Set(wire.HeaderWireVersion, "1")
		req.Header.Set(wire.HeaderHostname, "node-a")
		req.Header.Set(wire.HeaderOSUser, "grace")
		rr := httptest.NewRecorder()
		st.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
		}
	}
	firstSession := "11111111-1111-1111-1111-111111111111"
	firstFile := "22222222-2222-2222-2222-222222222222"
	strandedSession := "33333333-3333-3333-3333-333333333333"
	strandedFile := "44444444-4444-4444-4444-444444444444"
	put(firstSession, firstFile, "active")
	put(strandedSession, strandedFile, "stranded")

	lockHeld := make(chan struct{})
	releaseLock := make(chan struct{})
	lockDone := make(chan struct{})
	go func() {
		_ = st.WithWriteLock(func() error {
			close(lockHeld)
			<-releaseLock
			return nil
		})
		close(lockDone)
	}()
	<-lockHeld
	consumer := startIndexConsumer(t.Context(), st, idx, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	consumer.drainTimeout = 10 * time.Millisecond
	deadline := time.Now().Add(time.Second)
	for len(st.AppendEvents()) != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("consumer did not start first event; buffered=%d", len(st.AppendEvents()))
		}
		time.Sleep(time.Millisecond)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- consumer.StopAndWait() }()
	time.Sleep(20 * time.Millisecond)
	close(releaseLock)
	<-lockDone
	if err := <-stopDone; err == nil || !strings.Contains(err.Error(), "timed out draining index consumer") {
		t.Fatalf("StopAndWait error = %v, want drain timeout", err)
	}
	var dirty int
	if err := st.DB().QueryRow(`SELECT dirty_for_reindex FROM files
		WHERE tool = ? AND session_id = ? AND file_uuid = ? AND generation = 0`,
		wire.ToolClaude, strandedSession, strandedFile).Scan(&dirty); err != nil {
		t.Fatal(err)
	}
	if dirty != 1 {
		t.Fatalf("stranded generation dirty_for_reindex = %d, want 1", dirty)
	}
}

func TestIndexConsumerFailureMarksGenerationDirty(t *testing.T) {
	st, err := store.Open(t.Context(), store.Config{Dir: t.TempDir(), AppendBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	})
	idx, err := index.New(t.Context(), st.DB(), st.MirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	idx.InjectWriteFailureOnce()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	consumer := startIndexConsumer(ctx, st, idx, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	t.Cleanup(func() {
		if err := consumer.StopAndWait(); err != nil {
			t.Error(err)
		}
	})

	body := []byte(`{"type":"user","uuid":"ok","sessionId":"11111111-1111-1111-1111-111111111111"}` + "\n")
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/11111111-1111-1111-1111-111111111111/22222222-2222-2222-2222-222222222222/bytes?offset=0", bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		var dirty int
		if err := st.DB().QueryRow(`SELECT COUNT(*) FROM files WHERE dirty_for_reindex = 1`).Scan(&dirty); err != nil {
			t.Fatal(err)
		}
		if dirty == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("index consumer failure did not mark generation dirty")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBareAdminErrors(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"admin"})
	if err := root.Execute(); err == nil {
		t.Error("sesh admin without a subcommand should error")
	}
}

func writeCursor(t *testing.T, stateDir string, cur ship.Cursor) {
	t.Helper()
	reg, err := ship.OpenRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	if err := reg.Put(cur); err != nil {
		t.Fatal(err)
	}
}
