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
// The stat-first order (no connect syscall at all when the file is absent)
// was proven by strace in review; this test pins only the cost and the miss.
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
	// Literal, not ClientBudget×n: the contract is "a hung serve costs at most
	// 150 ms"; a budget raised to 5 s must redden here.
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("hung listener took %s, contract 150 ms", elapsed)
	}
	if ClientBudget != 150*time.Millisecond {
		t.Fatalf("ClientBudget = %s; the owner ruled a fallback may cost at most 150 ms, and only for a hung serve", ClientBudget)
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
			return Response{Vitals: want, Path: "/invented/path.jsonl", ObservedAt: stamp}
		}
		return Response{Miss: true}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	got, ok := Ask(dir, Request{Op: OpVitals, Tool: "claude", Session: "known"})
	if !ok || got.Vitals.Model != want.Model || got.Vitals.ContextUsage.UsedTokens != 32300 || *got.Vitals.ContextUsage.WindowTokens != window || got.Path != "/invented/path.jsonl" || !got.ObservedAt.Equal(stamp) {
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
	first, err := Listen(dir, func(Request) Response { return Response{Path: "first", Vitals: claudesession.Vitals{Model: "m"}} }, nil)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	defer first.Close()
	second, err := Listen(dir, func(Request) Response { return Response{Path: "second", Vitals: claudesession.Vitals{Model: "m"}} }, nil)
	if err != ErrHeldByLiveServe || second != nil {
		t.Fatalf("second Listen = %v, %v; want ErrHeldByLiveServe", second, err)
	}
	if got, ok := Ask(dir, Request{Op: OpVitals}); !ok || got.Path != "first" {
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

// D4. Reddens: `{}` or `null` replies taken as hits (empty cached vitals
// suppressing the direct read).
func TestAskEmptyAndNullRepliesAreMiss(t *testing.T) {
	for _, reply := range []string{"{}\n", "null\n", `{"vitals":{}}` + "\n"} {
		dir := shortDir(t)
		listener, err := net.Listen("unix", Path(dir))
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				_, _ = conn.Write([]byte(reply))
				_ = conn.Close()
			}
		}()
		if got, ok := Ask(dir, Request{Op: OpVitals, Tool: "claude", Session: "x"}); ok || !got.Miss {
			t.Fatalf("reply %q: ok=%v got=%+v; want miss", reply, ok, got)
		}
		_ = listener.Close()
	}
}

// D3. Reddens: Close unlinking the path after the drain wait, which deletes a
// successor's freshly bound socket.
func TestCloseDoesNotUnlinkSuccessor(t *testing.T) {
	dir := shortDir(t)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	first, err := Listen(dir, func(Request) Response {
		entered <- struct{}{}
		<-release
		return Response{Vitals: claudesession.Vitals{Model: "first"}}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	asked := make(chan bool, 1)
	go func() { _, ok := Ask(dir, Request{Op: OpVitals}); asked <- ok }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("answer never entered")
	}
	closed := make(chan struct{})
	go func() { first.Close(); close(closed) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(Path(dir)); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener.Close did not unlink the path")
		}
		time.Sleep(time.Millisecond)
	}
	second, err := Listen(dir, func(Request) Response { return Response{Vitals: claudesession.Vitals{Model: "second"}} }, nil)
	if err != nil {
		t.Fatalf("successor could not bind during the drain: %v", err)
	}
	defer second.Close()
	close(release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("first Close did not return")
	}
	<-asked
	if _, err := os.Stat(Path(dir)); err != nil {
		t.Fatalf("successor socket gone after the first Close: %v", err)
	}
	if got, ok := Ask(dir, Request{Op: OpVitals}); !ok || got.Vitals.Model != "second" {
		t.Fatalf("successor does not answer: %+v %v", got, ok)
	}
}
