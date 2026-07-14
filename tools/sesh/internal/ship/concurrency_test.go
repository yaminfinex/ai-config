package ship

// Invariants of the bounded-parallel authoritative pass: total in-flight
// store requests never exceed the shipper's bound, no identity ever has two
// operations in flight (per-file PUT ordering is strictly sequential), one
// file's hold never aborts the others, and duplicate discoveries of one
// identity collapse to a single worker. The measurement test at the bottom
// records the AC's 3k-file first-pass number against a latency-injected
// store.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// corpusUUID derives a valid, unique session UUID for synthetic corpora.
func corpusUUID(i int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-000000000000", i+1)
}

func TestParallelPassBoundsInFlightAndPreservesPerFileOrder(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	const files = 32
	for i := range files {
		h.writeClaude("proj", corpusUUID(i), data)
	}
	// Small PUT bodies force several sequential PUTs per file so an
	// interleaving bug would surface as out-of-order offsets or a same-key
	// overlap; the delay makes workers actually overlap at the store.
	h.shipper.MaxBody = 8 << 10
	h.store.handleDelay = 10 * time.Millisecond

	h.runOnce()

	maxInflight, sameKeyOverlap := h.store.concurrencyObserved()
	if maxInflight > defaultFileConcurrency {
		t.Errorf("max in-flight store requests = %d, want <= bound %d", maxInflight, defaultFileConcurrency)
	}
	if maxInflight < 2 {
		t.Errorf("max in-flight store requests = %d; the pass never overlapped requests, so nothing was parallel", maxInflight)
	}
	if sameKeyOverlap {
		t.Error("two operations were in flight for one identity; per-file ordering invariant broken")
	}
	for i := range files {
		uuid := corpusUUID(i)
		h.assertMirror("claude", uuid, data)
		offsets := h.store.puts("claude", uuid, uuid)
		for j := 1; j < len(offsets); j++ {
			if offsets[j] <= offsets[j-1] {
				t.Fatalf("PUT offsets for %s not strictly increasing: %v", uuid, offsets)
			}
		}
	}
}

func TestParallelRecoveryBoundsInFlight(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	const files = 32
	for i := range files {
		uuid := corpusUUID(i)
		h.writeClaude("proj", uuid, data)
	}
	h.store.handleDelay = 10 * time.Millisecond
	h.runOnce() // everything shipped and cursors recorded

	// Lose the registry: the next pass is a full recovery pass (one GET per
	// identity) followed by quiescence checks.
	h.shipper.Registry.Close()
	if err := os.Remove(h.stateDir + "/cursors.json"); err != nil {
		t.Fatal(err)
	}
	h.openShipper()
	if !h.shipper.Registry.NeedsRecovery {
		t.Fatal("registry deletion did not set NeedsRecovery")
	}
	h.runOnce()

	maxInflight, sameKeyOverlap := h.store.concurrencyObserved()
	if maxInflight > defaultFileConcurrency {
		t.Errorf("max in-flight requests during recovery = %d, want <= bound %d", maxInflight, defaultFileConcurrency)
	}
	if sameKeyOverlap {
		t.Error("two operations were in flight for one identity during recovery")
	}
	for i := range files {
		uuid := corpusUUID(i)
		// One GET from the fresh-registry first pass, one from the recovery
		// pass after the registry was lost.
		if got := h.store.recoveries("claude", uuid, uuid); got != 2 {
			t.Fatalf("recovery GETs for %s = %d, want 2", uuid, got)
		}
		h.assertMirror("claude", uuid, data)
	}
}

func TestParallelPassOneHoldDoesNotAbortOthers(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	const files = 12
	held := corpusUUID(3)
	for i := range files {
		h.writeClaude("proj", corpusUUID(i), data)
	}
	h.store.unavailableFor = map[string]bool{h.store.key("claude", held, held): true}
	h.store.handleDelay = 5 * time.Millisecond

	if err := h.shipper.RunOnce(context.Background()); !errors.Is(err, errHold) {
		t.Fatalf("RunOnce with one held file = %v, want errHold", err)
	}
	for i := range files {
		uuid := corpusUUID(i)
		if uuid == held {
			continue
		}
		h.assertMirror("claude", uuid, data)
	}

	h.store.unavailableFor = nil
	h.runOnce()
	h.assertMirror("claude", held, data)
}

func TestDuplicateIdentityDiscoveriesCollapseToOneWorker(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	uuid := corpusUUID(0)
	// The same session file at two paths (a copied project directory): one
	// identity, so the pass must hand it to exactly one worker.
	h.writeClaude("proj-a", uuid, data)
	h.writeClaude("proj-b", uuid, data)
	h.shipper.MaxBody = 8 << 10
	h.store.handleDelay = 5 * time.Millisecond

	h.runOnce()

	if _, sameKeyOverlap := h.store.concurrencyObserved(); sameKeyOverlap {
		t.Error("duplicate discovery of one identity ran on two workers concurrently")
	}
	h.assertMirror("claude", uuid, data)
	offsets := h.store.puts("claude", uuid, uuid)
	for j := 1; j < len(offsets); j++ {
		if offsets[j] <= offsets[j-1] {
			t.Fatalf("PUT offsets for duplicated identity not strictly increasing: %v", offsets)
		}
	}
}

// TestConcurrentRunOnceCallsSerialize pins the pass-level half of the
// ordering invariant inside Shipper itself: RunOnce is exclusive (passMu),
// so two synchronized calls on one Shipper — a re-entry no production caller
// performs today — must serialize rather than hand one identity to two
// workers or race on Registry.NeedsRecovery (the -race run of this test is
// the proof for the latter).
func TestConcurrentRunOnceCallsSerialize(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	const files = 8
	for i := range files {
		h.writeClaude("proj", corpusUUID(i), data)
	}
	h.store.handleDelay = 10 * time.Millisecond

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = h.shipper.RunOnce(context.Background())
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent RunOnce call %d: %v", i, err)
		}
	}
	if _, sameKeyOverlap := h.store.concurrencyObserved(); sameKeyOverlap {
		t.Fatal("re-entrant RunOnce put one identity on two workers; the pass mutex is not serializing")
	}
	for i := range files {
		h.assertMirror("claude", corpusUUID(i), data)
	}
}

// TestSameKeyOverlapDetectorFires is the negative self-check for the
// fake-store latch every ordering-invariant test above relies on: a
// deliberate same-key overlap at the instrumentation boundary must trip it,
// and sequential enters must not. The full-stack proof was run externally
// (dedupe disabled in a scratch copy: TestDuplicateIdentityDiscoveries-
// CollapseToOneWorker failed with the latch set and duplicated PUT offsets
// [0 0 8192 8192 ...]); this test preserves that proof in-tree so a future
// change cannot neuter the latch while every positive test stays green.
func TestSameKeyOverlapDetectorFires(t *testing.T) {
	fs := newFakeStore()
	fs.trackEnter("claude/a/a")
	fs.trackEnter("claude/a/a") // deliberate same-key overlap
	fs.trackExit("claude/a/a")
	fs.trackExit("claude/a/a")
	maxInflight, sameKeyOverlap := fs.concurrencyObserved()
	if !sameKeyOverlap {
		t.Fatal("deliberate same-key overlap did not trip the latch")
	}
	if maxInflight != 2 {
		t.Fatalf("max in-flight = %d, want 2", maxInflight)
	}

	sequential := newFakeStore()
	sequential.trackEnter("claude/a/a")
	sequential.trackExit("claude/a/a")
	sequential.trackEnter("claude/a/a")
	sequential.trackExit("claude/a/a")
	if _, overlap := sequential.concurrencyObserved(); overlap {
		t.Fatal("latch tripped on strictly sequential same-key operations")
	}
}

// TestMeasure3kCorpusFirstPass records the AC measurement: total first-pass
// time (recovery GET + initial PUT per file) over a 3,000-file corpus against
// a store answering each request after an injected 10 ms service delay (a
// WAN-RTT stand-in — the real link is ~177 ms, so wall-clock scales up but
// the serial/parallel ratio is the RTT-bound one). Gated behind
// SESH_MEASURE_3K=1: it deliberately spends ~a minute serializing the
// baseline.
func TestMeasure3kCorpusFirstPass(t *testing.T) {
	if os.Getenv("SESH_MEASURE_3K") != "1" {
		t.Skip("set SESH_MEASURE_3K=1 to run the 3k-corpus first-pass measurement")
	}
	data := fixture(t, "claude-normal.jsonl")
	const files = 3000
	measure := func(concurrency int) time.Duration {
		h := newHarness(t)
		h.store.handleDelay = 10 * time.Millisecond
		h.shipper.fileConcurrency = concurrency
		for i := range files {
			h.writeClaude("proj", corpusUUID(i), data)
		}
		// Fresh registry: NeedsRecovery drives one recovery GET per file
		// before its initial PUT — the true first onboarding pass.
		if !h.shipper.Registry.NeedsRecovery {
			t.Fatal("fresh harness registry unexpectedly recovered")
		}
		start := time.Now()
		h.runOnce()
		return time.Since(start)
	}
	serial := measure(1)
	parallel := measure(0) // 0 = defaultFileConcurrency
	t.Logf("3k-file first pass, 10ms injected store delay: serial=%s bounded(%d)=%s speedup=%.1fx",
		serial, defaultFileConcurrency, parallel, float64(serial)/float64(parallel))
}
