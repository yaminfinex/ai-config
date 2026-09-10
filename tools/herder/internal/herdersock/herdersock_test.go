package herdersock

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-config/tools/herder/internal/claudesession"
)

// shortDir keeps unix socket paths under the 108-byte limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hsock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func timed(t *testing.T, label string, budget time.Duration, fn func()) time.Duration {
	t.Helper()
	const rounds = 20
	start := time.Now()
	for i := 0; i < rounds; i++ {
		fn()
	}
	per := time.Since(start) / rounds
	if per > budget {
		t.Fatalf("%s took %s per call, budget %s", label, per, budget)
	}
	t.Logf("%s: %s per call", label, per)
	return per
}

// Reddens: dialing or waiting on the timeout when no serve exists.
func TestAskAbsentSocketIsInstant(t *testing.T) {
	dir := shortDir(t)
	timed(t, "absent socket", time.Millisecond, func() {
		if _, ok := Ask(dir, Request{Op: OpVitals, Tool: "claude", Session: "x"}); ok {
			t.Fatal("hit without a serve")
		}
	})
}

// Reddens: a stale file treated as a hung serve (150 ms), or left behind.
func TestAskStaleSocketUnlinksAndIsInstant(t *testing.T) {
	dir := shortDir(t)
	path := Path(dir)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	// Close without unlink: the "serve died" case.
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = listener.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale file should remain: %v", err)
	}
	timed(t, "stale socket", time.Millisecond, func() {
		if _, ok := Ask(dir, Request{Op: OpVitals}); ok {
			t.Fatal("hit on a stale socket")
		}
		// Re-create the stale file for the next round.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			l, _ := net.Listen("unix", path)
			l.(*net.UnixListener).SetUnlinkOnClose(false)
			_ = l.Close()
		}
	})
	_, _ = Ask(dir, Request{Op: OpVitals})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("client did not unlink the stale socket")
	}
}

// Reddens: no deadline on a listener that accepts and never answers.
func TestAskHungListenerBounded(t *testing.T) {
	dir := shortDir(t)
	listener, err := net.Listen("unix", Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()
	start := time.Now()
	if _, ok := Ask(dir, Request{Op: OpVitals}); ok {
		t.Fatal("hit from a silent listener")
	}
	if elapsed := time.Since(start); elapsed > ClientBudget*3 {
		t.Fatalf("hung listener took %s, budget %s", elapsed, ClientBudget)
	}
}

// Reddens: field drift between the two ends; miss not honoured.
func TestListenAnswersHitAndMiss(t *testing.T) {
	dir := shortDir(t)
	window := int64(258400)
	percent := 12.5
	want := claudesession.Vitals{Model: "invented-model", ContextUsage: &claudesession.ContextUsage{UsedTokens: 32300, InputTokens: 32300, WindowTokens: &window, UsedPercent: &percent}}
	stamp := time.Date(2026, 9, 10, 1, 2, 3, 0, time.UTC)
	server, err := Listen(dir, func(r Request) Response {
		if r.Session == "known" {
			return Response{Vitals: want, Path: "/invented/path.jsonl", Phase: "tailing", ObservedAt: stamp}
		}
		return Response{Miss: true}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	got, ok := Ask(dir, Request{Op: OpVitals, Tool: "claude", Session: "known"})
	if !ok || got.Vitals.Model != want.Model || got.Vitals.ContextUsage.UsedTokens != 32300 || *got.Vitals.ContextUsage.WindowTokens != window || got.Path != "/invented/path.jsonl" || got.Phase != "tailing" || !got.ObservedAt.Equal(stamp) {
		t.Fatalf("hit = %+v ok=%v", got, ok)
	}
	if _, ok := Ask(dir, Request{Op: OpVitals, Tool: "claude", Session: "other"}); ok {
		t.Fatal("miss reported as hit")
	}
	if _, ok := Ask(dir, Request{Op: "board"}); ok {
		t.Fatal("unknown op reported as hit")
	}
}

// Reddens: second serve steals the socket or crashes; stale file not replaced.
func TestListenReplacesStaleAndYieldsToLive(t *testing.T) {
	dir := shortDir(t)
	path := Path(dir)
	stale, _ := net.Listen("unix", path)
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = stale.Close()
	first, err := Listen(dir, func(Request) Response { return Response{Phase: "first", Vitals: claudesession.Vitals{Model: "m"}} }, nil)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	defer first.Close()
	second, err := Listen(dir, func(Request) Response { return Response{Phase: "second"} }, nil)
	if err != ErrHeldByLiveServe || second != nil {
		t.Fatalf("second Listen = %v, %v; want ErrHeldByLiveServe", second, err)
	}
	if got, ok := Ask(dir, Request{Op: OpVitals}); !ok || got.Phase != "first" {
		t.Fatalf("first serve no longer answers: %+v %v", got, ok)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, err %v", info.Mode(), err)
	}
	first.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Close did not unlink the socket")
	}
}

func TestPathIsUnderStateDir(t *testing.T) {
	if Path("/x/y") != filepath.Join("/x/y", SocketName) {
		t.Fatal(Path("/x/y"))
	}
}
