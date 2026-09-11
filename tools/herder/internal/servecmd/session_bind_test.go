package servecmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
)

// runLifeMirror feeds lives through startLifeMirror once and returns the
// replayed projection.
func runLifeMirror(t *testing.T, deps dependencies, lives ...hcomevents.Life) *agentstore.Projection {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	deps.life = func(_ context.Context, _ *hcomevents.Cursor, emit func(hcomevents.Life) error) error {
		defer close(done)
		defer cancel()
		for _, life := range lives {
			if err := emit(life); err != nil {
				return err
			}
		}
		return nil
	}
	startLifeMirror(ctx, deps)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("life mirror did not finish")
	}
	projection, err := deps.store.Replay()
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func lastEvent(t *testing.T, proj *agentstore.Projection, name string) agentstore.Event {
	t.Helper()
	view := proj.Latest(name)
	if view == nil || len(view.Events) == 0 {
		t.Fatalf("no record for %s: names=%v", name, proj.Names())
	}
	return view.Events[len(view.Events)-1]
}

func TestLifeMirrorStampsRosterSessionOnReadyAndCreated(t *testing.T) {
	deps := fixtureDeps()
	deps.store = agentstore.Open(t.TempDir(), nil)
	deps.rosterCache = &rosterCache{}
	deps.rosterCache.set([]hcomidentity.Row{{Name: "query-topo-guna", BaseName: "guna", SessionID: "S-guna"}, {Name: "impl-nife", BaseName: "nife"}})
	rosterCalls := 0
	deps.roster = func() ([]hcomidentity.Row, error) {
		rosterCalls++
		return nil, nil
	}
	proj := runLifeMirror(t, deps,
		hcomevents.Life{ID: 1, TS: "2026-09-11T02:30:33Z", Instance: "guna", Action: "created", By: "user"},
		hcomevents.Life{ID: 2, TS: "2026-09-11T02:30:38Z", Instance: "guna", Action: "ready", By: "riko"},
		hcomevents.Life{ID: 3, TS: "2026-09-11T02:30:39Z", Instance: "nife", Action: "created", By: "user"},
		hcomevents.Life{ID: 4, TS: "2026-09-11T02:30:40Z", Instance: "guna", Action: "stopped", By: "session", Reason: "exit:other"},
	)
	guna := proj.Latest("query-topo-guna")
	if guna == nil || len(guna.Events) != 3 || guna.Events[0].Session != "S-guna" || guna.Events[1].Session != "S-guna" || guna.Events[2].Session != "" {
		t.Fatalf("guna events = %+v", guna)
	}
	if guna.Sessions[0].SessionID != "S-guna" || guna.Sessions[0].Ended == nil || guna.Sessions[0].EndReason != "exit:other" {
		t.Fatalf("guna sessions = %+v", guna.Sessions)
	}
	// created stamps only what the cache already holds: no refetch for a row
	// without a session.
	if e := lastEvent(t, proj, "impl-nife"); e.Session != "" || rosterCalls != 0 {
		t.Fatalf("created refetched or stamped: event=%+v rosterCalls=%d", e, rosterCalls)
	}
}

func TestLifeMirrorReadyRefetchesRosterOnceWhenCacheHasNoSession(t *testing.T) {
	deps := fixtureDeps()
	deps.store = agentstore.Open(t.TempDir(), nil)
	deps.rosterCache = &rosterCache{}
	deps.rosterCache.set([]hcomidentity.Row{{Name: "query-topo-guna", BaseName: "guna"}})
	rosterCalls := 0
	deps.roster = func() ([]hcomidentity.Row, error) {
		rosterCalls++
		return []hcomidentity.Row{{Name: "query-topo-guna", BaseName: "guna", SessionID: "S-late"}}, nil
	}
	proj := runLifeMirror(t, deps, hcomevents.Life{ID: 11, TS: "2026-09-11T02:30:38Z", Instance: "guna", Action: "ready", By: "riko"})
	if e := lastEvent(t, proj, "query-topo-guna"); e.Kind != agentstore.KindMirrorReady || e.Session != "S-late" || rosterCalls != 1 {
		t.Fatalf("refetch: event=%+v rosterCalls=%d", e, rosterCalls)
	}
	if rows, _ := deps.rosterCache.get(); len(rows) != 1 || rows[0].SessionID != "S-late" {
		t.Fatalf("refetch did not refresh the cache: %+v", rows)
	}
}

func TestLifeMirrorReadyWithoutSessionAfterRefetchWritesUnstamped(t *testing.T) {
	deps := fixtureDeps()
	deps.store = agentstore.Open(t.TempDir(), nil)
	deps.rosterCache = &rosterCache{}
	deps.rosterCache.set([]hcomidentity.Row{{Name: "query-topo-guna", BaseName: "guna"}})
	audits := 0
	deps.audit = func(string, ...any) { audits++ }
	rosterCalls := 0
	deps.roster = func() ([]hcomidentity.Row, error) {
		rosterCalls++
		if rosterCalls == 2 {
			return nil, context.DeadlineExceeded
		}
		return []hcomidentity.Row{{Name: "query-topo-guna", BaseName: "guna"}}, nil
	}
	proj := runLifeMirror(t, deps,
		hcomevents.Life{ID: 21, TS: "2026-09-11T02:30:38Z", Instance: "guna", Action: "ready", By: "riko"},
		hcomevents.Life{ID: 22, TS: "2026-09-11T02:30:39Z", Instance: "guna", Action: "ready", By: "riko"},
	)
	view := proj.Latest("query-topo-guna")
	if view == nil || len(view.Events) != 2 || view.Events[0].Session != "" || view.Events[1].Session != "" || len(view.Sessions) != 0 {
		t.Fatalf("unstamped ready: %+v", view)
	}
	// one refetch per ready event (never a loop), a failing refetch is not an
	// error, and nothing is audited.
	if rosterCalls != 2 || audits != 0 {
		t.Fatalf("rosterCalls=%d audits=%d", rosterCalls, audits)
	}
}

// TestAssignmentAfterAnnotateOnClosedLifeShowsOnTheFleetRow is the diag-ruzu
// sequence end to end: a closed life, an annotate that opens a record before
// hcom creates the roster row, the roster row's ready mirrored WITH its
// session, then POST assignment; the fleet payload row must show the manager,
// group and manager_state.
func TestAssignmentAfterAnnotateOnClosedLifeShowsOnTheFleetRow(t *testing.T) {
	deps := supervisionDeps(t)
	base, _ := deps.roster()
	rosterCreated := time.Date(2026, 9, 11, 2, 30, 33, 0, time.UTC)
	roster := append(base, hcomidentity.Row{Name: "query-topo-guna", BaseName: "guna", Tool: "claude", Status: "listening", SessionID: "S-guna-2", CreatedAt: rosterCreated})
	deps.roster = func() ([]hcomidentity.Row, error) { return roster, nil }
	deps.rosterCache = &rosterCache{}
	deps.rosterCache.set(roster)
	old := rosterCreated.Add(-30 * time.Minute)
	for _, e := range []agentstore.Event{
		{ID: agentstore.DerivedID([]byte("ruzu-1")), At: old, Kind: agentstore.KindMirrorReady, Name: "query-topo-guna", By: "riko", ByKind: "mirror", Session: "S-guna-1"},
		{ID: agentstore.DerivedID([]byte("ruzu-2")), At: old.Add(10 * time.Minute), Kind: agentstore.KindMirrorStopped, Name: "query-topo-guna", By: "session", ByKind: "mirror", Reason: "exit:other"},
		{ID: agentstore.DerivedID([]byte("ruzu-3")), At: rosterCreated.Add(-3 * time.Minute), Kind: agentstore.KindAnnotate, Name: "query-topo-guna", By: "riko", ByKind: "agent", Title: "queries-program-lead"},
	} {
		if _, err := deps.store.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	fleet := func() string {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/fleet", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("fleet status=%d body=%s", response.Code, response.Body.String())
		}
		return response.Body.String()
	}
	if body := fleet(); !strings.Contains(body, `"agent":"query-topo-guna"`) || strings.Contains(body, `"queries-program-lead"`) {
		t.Fatalf("before the ready the record must fold as unregistered: %s", body)
	}
	runLifeMirror(t, deps, hcomevents.Life{ID: 245709, TS: "2026-09-11T02:30:38Z", Instance: "guna", Action: "ready", By: "riko"})
	request := httptest.NewRequest(http.MethodPost, "/api/agents/query-topo-guna/assignment", strings.NewReader(`{"manager":"ziru","group":"fleet-refit"}`))
	request.SetPathValue("busName", "query-topo-guna")
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("assignment status=%d body=%s", response.Code, response.Body.String())
	}
	body := fleet()
	start := strings.Index(body, `"agent":"query-topo-guna"`)
	if start < 0 {
		t.Fatalf("fleet has no guna row: %s", body)
	}
	row := body[start:]
	if end := strings.Index(row, "}"); end >= 0 {
		row = row[:end]
	}
	for _, want := range []string{`"manager":"ziru"`, `"manager_state":"live"`, `"group":"fleet-refit"`, `"title":"queries-program-lead"`, `"created_at":"2026-09-11T02:30:33Z"`} {
		if !strings.Contains(row, want) {
			t.Fatalf("fleet row missing %s: %s", want, row)
		}
	}
}
