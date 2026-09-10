package servecmd

// This file is the serve's side of the observer unit: it builds the
// process-scoped observer, publishes it over the local socket, points the
// detail endpoint's vitals read at it, and owns the one agent-store
// projection every SSE client shares. It deliberately holds no transcript
// parsing (sessionvitals) and no protocol (herdersock).

import (
	"context"
	"errors"
	"sync"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdersock"
	"ai-config/tools/herder/internal/observer"
	"ai-config/tools/herder/internal/sessionvitals"
)

// readAgentVitals is GET /api/agents/{name}'s vitals read: the same
// cache-then-direct body the CLI uses (sessionvitals.ReadWith) with the
// in-process observer as the cache, so the detail endpoint stops scanning a
// transcript per request once the observer knows the session. A nil cache
// (tests, or a serve without a state dir) is the direct read alone.
func readAgentVitals(cache sessionvitals.Lookup) func(hcomidentity.Row) (claudesession.Vitals, error) {
	return func(row hcomidentity.Row) (claudesession.Vitals, error) {
		result, err := sessionvitals.ReadWith(cache, row)
		return result.Vitals, err
	}
}

// startObserver builds and starts the observer from the serve's own seams
// (roster reader, fsnotify factory, clock, audit).
func startObserver(ctx context.Context, deps dependencies) *observer.Observer {
	obs := observer.New(observer.Options{
		Roster:  deps.roster,
		Now:     deps.now,
		Watcher: observer.WatcherFactory(deps.transcriptWatcher),
		Poll:    deps.poll,
		Sweep:   deps.transcriptSafety,
		Audit:   deps.audit,
	})
	obs.Run(ctx)
	return obs
}

// answerVitals is the socket's answer function: nothing but observer.Lookup
// marshalled, plus the phase word for diagnostics. The hit/miss rule is the
// observer's, so the socket and the in-serve path cannot disagree.
func answerVitals(obs *observer.Observer) func(herdersock.Request) herdersock.Response {
	return func(request herdersock.Request) herdersock.Response {
		row := hcomidentity.Row{Tool: request.Tool, SessionID: request.Session, AgentID: request.AgentID}
		result, ok := obs.Lookup(row)
		if !ok {
			return herdersock.Response{Miss: true}
		}
		response := herdersock.Response{Vitals: result.Vitals, Path: result.Path, ObservedAt: result.ObservedAt}
		if snapshot, known := obs.Snapshot(observer.KeyFor(row)); known {
			response.Phase = string(snapshot.Phase)
		}
		return response
	}
}

// startSocket listens on <stateDir>/herder.sock. ErrHeldByLiveServe means
// another serve on this state dir answers already: audit once, run without a
// socket, and (storeWriter=false) never write snapshot.json.
func startSocket(stateDir string, obs *observer.Observer, deps dependencies) *herdersock.Server {
	server, err := herdersock.Listen(stateDir, answerVitals(obs), deps.audit)
	if errors.Is(err, herdersock.ErrHeldByLiveServe) {
		deps.audit("herder serve: %s held by a live serve; not listening on the socket and not refreshing snapshot.json", herdersock.Path(stateDir))
		return nil
	}
	if err != nil {
		deps.audit("herder serve: socket unavailable; CLI reads transcripts directly: %v", err)
		return nil
	}
	deps.audit("herder serve: answering vitals on %s", server.Path())
	return server
}

// projectionCache is the ONE folded agent-store projection the board reads.
// It is set at Run and by the process-level store watch; SSE clients never
// fold the journal themselves (TASK-125).
type projectionCache struct {
	mu   sync.RWMutex
	proj *agentstore.Projection
}

func (c *projectionCache) get() *agentstore.Projection {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proj
}

func (c *projectionCache) set(proj *agentstore.Projection) {
	c.mu.Lock()
	c.proj = proj
	c.mu.Unlock()
}

// refreshProjection folds the store once. The socket-owning serve calls
// Load(), which also rewrites snapshot.json (temp+rename); a serve that lost
// the socket to a live sibling calls LoadNoSnapshot() so two writers never
// race on the same file. A failed fold keeps the previous projection.
func refreshProjection(deps dependencies) {
	if deps.store == nil || deps.projection == nil {
		return
	}
	var proj *agentstore.Projection
	var err error
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
	deps.projection.set(proj)
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

// startStoreProjection wires the shared projection: fold once now, then
// once per store-watch debounce, shared by every client through the
// projection cache and the broker. Returns deps with the cache and broker
// set; deps.storeWriter must already say whether this serve owns the socket.
func startStoreProjection(ctx context.Context, deps dependencies) dependencies {
	deps.projection = &projectionCache{}
	deps.storeChanges = newTickBroker()
	refreshProjection(deps)
	changes := startStoreWatch(ctx, deps)
	if changes == nil {
		return deps
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				refreshProjection(deps)
				deps.storeChanges.publish()
			}
		}
	}()
	return deps
}
