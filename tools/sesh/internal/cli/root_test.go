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

func TestStubsReportNotImplemented(t *testing.T) {
	stubs := [][]string{
		{"status"},
		{"admin", "drop-file"},
	}
	for _, args := range stubs {
		root := newRoot()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil {
			t.Errorf("sesh %s: expected not-implemented error, got nil", strings.Join(args, " "))
			continue
		}
		if !strings.Contains(err.Error(), "not implemented") {
			t.Errorf("sesh %s: error %q does not say not implemented", strings.Join(args, " "), err)
		}
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
