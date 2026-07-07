package send

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestConcurrentSendsAreSerialized pins codex review P2-CONCURRENT: two
// concurrent sends sharing sender identity and target must not share a
// receipt. The stub bus keeps a stale receipt (id 41) visible on every
// events call, acks id 42 once ANY send has landed, and never acks the
// second message. Without the send-window lock both senders snapshot
// preMax=41 before either receipt exists, so the first wake's id 42
// satisfies BOTH waiters — two delivered verdicts, one of them false. With
// the lock the window serializes: the winner snapshots 41 and sees 42
// (delivered); the loser snapshots 42 and nothing newer ever appears
// (queued). The stub's events call sleeps to widen the race window, so a
// regression to unlocked behavior fails this test reliably.
func TestConcurrentSendsAreSerialized(t *testing.T) {
	stubDir := t.TempDir()
	stateDir := t.TempDir()
	stub := `#!/usr/bin/env bash
STATE="$STUB_STATE"
case "$1" in
  list) exit 0;;
  send) echo x >>"$STATE/sends"; exit 0;;
  events)
    sleep 0.15
    printf '{"id":41,"data":{"context":"deliver:orchestrator"},"type":"status"}\n'
    if [[ -s "$STATE/sends" ]]; then
      printf '{"id":42,"data":{"context":"deliver:orchestrator"},"type":"status"}\n'
    fi
    exit 0;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_STATE", stateDir)
	busDir := t.TempDir()

	var wg sync.WaitGroup
	verdicts := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			verdicts[i] = DeliverBus("peer-rive", busDir, "hello", 1500)
		}(i)
	}
	wg.Wait()

	counts := map[string]int{}
	for _, v := range verdicts {
		counts[v]++
	}
	if counts["delivered"] != 1 || counts["queued"] != 1 {
		t.Fatalf("verdicts = %v, want exactly one delivered and one queued", verdicts)
	}
}
