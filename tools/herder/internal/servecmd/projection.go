package servecmd

// projection.go owns the ONE folded agent-store projection the board reads
// and its three refresh triggers: the start fold, the journal watch
// (storewatch.go) and a size-gated safety refold every transcriptSafety
// cadence; plus the broker that nudges SSE connections. It does not fold
// (agentstore does) and does not run the SSE loop.

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"ai-config/tools/herder/internal/agentstore"
)

// folds counts projection folds by this process (test observation, same
// shape as agentstore's replays counter).
var folds atomic.Int64

// projectionCache is the ONE folded agent-store projection the board reads.
// Set at start and by the refresh triggers; SSE clients never fold the
// journal themselves (TASK-125).
type projectionCache struct {
	mu   sync.RWMutex
	proj *agentstore.Projection
	// journalSize is the events.jsonl size at the last fold; the safety
	// refold compares against it so a quiet store is never refolded or
	// rewritten.
	journalSize int64
}

func (c *projectionCache) get() *agentstore.Projection {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proj
}

func (c *projectionCache) set(proj *agentstore.Projection, journalSize int64) {
	c.mu.Lock()
	c.proj = proj
	c.journalSize = journalSize
	c.mu.Unlock()
}

func (c *projectionCache) lastJournalSize() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.journalSize
}

// refreshProjection folds the store once. The socket-owning serve calls
// Load(), which also rewrites snapshot.json (temp+rename); a serve that lost
// the socket to a live sibling calls LoadNoSnapshot() so two writers never
// race on the same file. A failed fold keeps the previous projection.
func refreshProjection(deps dependencies) {
	if deps.store == nil || deps.projection == nil {
		return
	}
	size := journalSize(deps)
	var proj *agentstore.Projection
	var err error
	folds.Add(1)
	if deps.storeWriter {
		proj, err = deps.store.Load()
		if err == nil && proj.SnapshotErr != nil {
			deps.audit("herder serve: snapshot.json refresh failed: %v", proj.SnapshotErr)
		}
	} else {
		proj, err = deps.store.LoadNoSnapshot()
	}
	if err != nil {
		deps.audit("herder serve: agent store read failed; board keeps the last projection: %v", err)
		return
	}
	deps.projection.set(proj, size)
}

// journalSize is the events.jsonl size, taken BEFORE a fold so an append
// during the fold reads as a change on the next safety tick. A missing
// journal is size 0.
func journalSize(deps dependencies) int64 {
	info, err := os.Stat(deps.store.EventsPath())
	if err != nil {
		return 0
	}
	return info.Size()
}

// tickBroker fans one process-level "the store changed" nudge out to every
// SSE connection; the payload is the shared projection, not the tick.
type tickBroker struct {
	mu          sync.Mutex
	subscribers map[chan struct{}]struct{}
}

func newTickBroker() *tickBroker { return &tickBroker{subscribers: map[chan struct{}]struct{}{}} }

func (b *tickBroker) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subscribers, ch)
		b.mu.Unlock()
	}
}

func (b *tickBroker) publish() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// startStoreProjection wires the shared projection. Order matters: the
// journal watch starts BEFORE the first fold so an append in between is not
// lost; then one fold now; then one fold per watch debounce; and every
// deps.transcriptSafety a safety pass that refolds ONLY when events.jsonl
// changed size since the last fold (a nil or failed watcher degrades to this
// pass; a quiet store never refolds and never rewrites snapshot.json). Every
// client shares the result through the cache and the broker. Returns deps
// with the cache and broker set; deps.storeWriter must already say whether
// this serve owns the socket.
func startStoreProjection(ctx context.Context, deps dependencies) dependencies {
	deps.projection = &projectionCache{}
	deps.storeChanges = newTickBroker()
	changes := startStoreWatch(ctx, deps)
	refreshProjection(deps)
	if deps.store == nil {
		return deps
	}
	safety := deps.transcriptSafety
	if safety <= 0 {
		safety = TranscriptSafetyCadence
	}
	go func() {
		ticker := time.NewTicker(safety)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				refreshProjection(deps)
				deps.storeChanges.publish()
			case <-ticker.C:
				if journalSize(deps) == deps.projection.lastJournalSize() {
					continue
				}
				refreshProjection(deps)
				deps.storeChanges.publish()
			}
		}
	}()
	return deps
}
