package servecmd

// observe.go is the serve's side of the observer unit: it builds the
// process-scoped observer, publishes it over the local socket and points the
// detail endpoint's vitals read at it. It holds no transcript parsing
// (sessionvitals), no protocol (herdersock) and no store folding
// (projection.go).

import (
	"context"
	"errors"

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
// (roster reader, fsnotify factory, clock, audit). Signpost: the observer
// runs its own 2 s roster poll today because the serve has no process-level
// one (each SSE connection polls for itself); a later unit gives the serve
// ONE roster poll feeding rosterCache, the SSE connections and the observer.
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
// marshalled. The hit/miss rule is the observer's, so the socket and the
// in-serve path cannot disagree.
func answerVitals(obs *observer.Observer) func(herdersock.Request) herdersock.Response {
	return func(request herdersock.Request) herdersock.Response {
		row := hcomidentity.Row{Tool: request.Tool, SessionID: request.Session, AgentID: request.AgentID}
		result, ok := obs.Lookup(row)
		if !ok {
			return herdersock.Response{Miss: true}
		}
		return herdersock.Response{Vitals: result.Vitals, Path: result.Path, ObservedAt: result.ObservedAt}
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
