package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sesh/internal/index"
	"sesh/internal/ship"
	"sesh/internal/store"
	"sesh/internal/wire"
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
	for _, name := range []string{"ship", "serve", "reindex", "status", "admin"} {
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

func TestServeRejectsNonLoopbackReadBind(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"serve", "--addr", "127.0.0.1:0", "--read-addr", "0.0.0.0:0", "--data-dir", t.TempDir()})
	err := root.Execute()
	if err == nil {
		t.Fatal("sesh serve should reject non-loopback read bind before M4")
	}
	if !strings.Contains(err.Error(), "loopback") || !strings.Contains(err.Error(), "read") {
		t.Fatalf("serve error %q does not mention read loopback", err)
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

func TestStatusFailsOnPoisonedCursor(t *testing.T) {
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
	startIndexConsumer(ctx, st, idx, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

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
	startIndexConsumer(ctx, st, idx, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

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
