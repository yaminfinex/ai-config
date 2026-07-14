package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"sesh/internal/setup"
	"sesh/internal/ship"
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
	for _, name := range []string{"ship", "serve", "reindex", "status", "admin", "setup", "update"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("--help output missing subcommand %q\n%s", name, out.String())
		}
	}
}

// TestStoreStubsNameTheStoreArtifact pins the client-binary contract for the
// store-side command names: never a silent no-op, never an unknown-command or
// unknown-flag death — one clear error naming the binary that has the real
// command, whatever the invocation shape.
func TestStoreStubsNameTheStoreArtifact(t *testing.T) {
	cases := [][]string{
		{"serve"},
		{"serve", "--tsnet", "--data-dir", "/nonexistent"},
		{"reindex"},
		{"reindex", "--data-dir", "/nonexistent"},
		{"admin"},
		{"admin", "drop-file", "claude", "s", "f", "--yes"},
	}
	for _, args := range cases {
		root := newRoot()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil {
			t.Errorf("client %v: want store-only error, got nil", args)
			continue
		}
		if !strings.Contains(err.Error(), "sesh-store") {
			t.Errorf("client %v error %q does not name the sesh-store binary", args, err)
		}
		if strings.Contains(err.Error(), "\n") {
			t.Errorf("client %v error is not one line: %q", args, err)
		}
	}
}

// TestUpdateFailsClosedOnStoreBuild pins the store-side updater contract:
// the release channel serves only fleet client artifacts, so on the store
// build the mutating update path must refuse BEFORE any download — zero
// requests reach the channel — with the line naming `just deploy-store`.
// --check stays a read-only skew probe and must still reach the channel.
func TestUpdateFailsClosedOnStoreBuild(t *testing.T) {
	// HOME is pinned off the real machine config so the updater sees a
	// never-installed node, not this host's live service pin.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SESH_STORE_URL", "")
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	// Any non-empty store command set marks the root as the store build,
	// exactly how cmd/sesh-store assembles it.
	storeMarker := func() *cobra.Command { return &cobra.Command{Use: "serve"} }

	root := newRoot(storeMarker())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"update", "--store-url", server.URL})
	err := root.Execute()
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("store-build update: err=%v exit=%d, want refusal with exit 1", err, ExitCode(err))
	}
	if !strings.Contains(out.String(), "deploy-store") {
		t.Fatalf("store-build update refusal does not name deploy-store:\n%s", out.String())
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("store-build update touched the channel %d times before refusing, want 0", got)
	}

	// --check is read-only and stays allowed: it must reach the channel.
	root = newRoot(storeMarker())
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"update", "--check", "--store-url", server.URL})
	_ = root.Execute()
	if got := hits.Load(); got == 0 {
		t.Fatalf("store-build update --check never queried the channel; --check must stay a live skew probe\n%s", out.String())
	}
	if strings.Contains(out.String(), "refusing on the store build") {
		t.Fatalf("--check hit the store-build refusal:\n%s", out.String())
	}

	// The client build keeps the full mutating path (it fails later here —
	// 404 channel — but must get past the store guard and onto the wire).
	hits.Store(0)
	root = newRoot()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"update", "--store-url", server.URL})
	_ = root.Execute()
	if got := hits.Load(); got == 0 {
		t.Fatal("client-build update never reached the channel; guard is over-broad")
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
