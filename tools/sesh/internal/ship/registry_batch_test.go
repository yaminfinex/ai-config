package ship

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sesh/internal/wire"
)

func TestRunOncePersistsManyAcknowledgementsWithOneReplacement(t *testing.T) {
	h := newHarness(t)
	h.shipper.MaxBody = 128
	data := fixture(t, "claude-normal.jsonl")
	for i := 0; i < 8; i++ {
		uuid := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
		h.writeClaude("-batch", uuid, data)
	}

	before := h.shipper.Registry.durableReplacements
	h.runOnce()
	if got := h.shipper.Registry.durableReplacements - before; got != 1 {
		t.Fatalf("durable registry replacements = %d, want 1 for the authoritative pass", got)
	}
}

func TestRunOnceFlushesAcknowledgementsWhenAnotherFileHolds(t *testing.T) {
	h := newHarness(t)
	okUUID := "10000000-0000-4000-8000-000000000000"
	holdUUID := "20000000-0000-4000-8000-000000000000"
	data := fixture(t, "claude-normal.jsonl")
	h.writeClaude("-batch", okUUID, data)
	h.writeClaude("-batch", holdUUID, data)
	h.store.unavailableFor = map[string]bool{h.store.key("claude", holdUUID, holdUUID): true}

	if err := h.shipper.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce must surface the held file")
	}
	h.restart()
	c, ok := h.cursor(wire.ToolClaude, okUUID)
	if !ok || c.Offset != int64(len(data)) {
		t.Fatalf("successful cursor after restart = %+v, ok=%v; want its ACK persisted despite the hold", c, ok)
	}
}

func TestRunOnceSurfacesBatchFlushFailureAfterAcknowledgement(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	h.writeClaude("-batch", uuidNormal, data)
	if err := os.RemoveAll(h.stateDir); err != nil {
		t.Fatal(err)
	}

	err := h.shipper.RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cursor registry") {
		t.Fatalf("RunOnce error = %v, want surfaced cursor registry persistence failure", err)
	}
	h.assertMirror("claude", uuidNormal, data)
}

func TestAcknowledgementBeforeBatchFlushReplaysAfterRestart(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	path := h.writeClaude("-batch", uuidNormal, data[:20000])
	h.runOnce()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	h.shipper.Registry.beginBatch()
	d := Discovered{
		Identity: Identity{Tool: wire.ToolClaude, SessionID: uuidNormal, FileUUID: uuidNormal},
		Path:     path,
	}
	if err := h.shipper.shipFile(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.cursor(wire.ToolClaude, uuidNormal); c.Offset != int64(len(data)) {
		t.Fatalf("in-memory cursor = %d, want ACK high-water %d", c.Offset, len(data))
	}

	// Simulate process death before endBatch: release the lock without a flush.
	h.restart()
	if c, _ := h.cursor(wire.ToolClaude, uuidNormal); c.Offset != 20000 {
		t.Fatalf("restart cursor = %d, want last flushed offset 20000", c.Offset)
	}
	h.runOnce()
	h.assertMirror("claude", uuidNormal, data)
	if got := h.store.generationCount("claude", uuidNormal, uuidNormal); got != 1 {
		t.Fatalf("replay opened %d generations, want one idempotently converged generation", got)
	}
}

func BenchmarkRegistryPersistence(b *testing.B) {
	makeRegistry := func(b *testing.B) *Registry {
		b.Helper()
		r, err := OpenRegistry(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(r.Close)
		for i := 0; i < 750; i++ {
			uuid := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
			c := Cursor{Tool: wire.ToolClaude, SessionID: uuid, FileUUID: uuid, Path: filepath.Join("root", uuid+".jsonl")}
			r.cursors[c.Identity().Key()] = c
		}
		r.dirty = true
		if err := r.flush(); err != nil {
			b.Fatal(err)
		}
		return r
	}

	b.Run("per-cursor", func(b *testing.B) {
		r := makeRegistry(b)
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			for i := 0; i < 8; i++ {
				uuid := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
				c, _ := r.Get(Identity{Tool: wire.ToolClaude, SessionID: uuid, FileUUID: uuid})
				c.Offset++
				if err := r.Put(c); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("pass-batched", func(b *testing.B) {
		r := makeRegistry(b)
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			r.beginBatch()
			for i := 0; i < 8; i++ {
				uuid := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
				c, _ := r.Get(Identity{Tool: wire.ToolClaude, SessionID: uuid, FileUUID: uuid})
				c.Offset++
				if err := r.Put(c); err != nil {
					b.Fatal(err)
				}
			}
			if err := r.endBatch(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
