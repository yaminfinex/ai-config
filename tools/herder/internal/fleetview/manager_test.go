package fleetview

import (
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
)

// TestManagerEdgeStates pins the derivation table: each case reddens one
// branch (operator seed, literal user, live full name, live unique base
// name, ambiguous base name, closed record, off-roster record, no record,
// web identity, empty).
func TestManagerEdgeStates(t *testing.T) {
	roster := []hcomidentity.Row{
		{Name: "ziru", BaseName: "ziru"},
		{Name: "orch-hamo", BaseName: "hamo"},
		{Name: "impl-dupe", BaseName: "dupe"},
		{Name: "sesh-dupe", BaseName: "dupe"},
		{Name: "a-web-dupe", BaseName: "web-dupe"},
		{Name: "b-web-dupe", BaseName: "web-dupe"},
	}
	proj := agentstore.NewProjection()
	now := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	proj.Apply(agentstore.Event{ID: agentstore.DerivedID([]byte("m1")), At: now, Kind: agentstore.KindLaunchReady, Name: "orch-dead", By: "ziru", ByKind: "agent"}, 0)
	proj.Apply(agentstore.Event{ID: agentstore.DerivedID([]byte("m2")), At: now.Add(time.Second), Kind: agentstore.KindCulled, Name: "orch-dead", Pane: "p1", Close: "managed", By: "ziru", ByKind: "agent"}, 0)
	proj.Apply(agentstore.Event{ID: agentstore.DerivedID([]byte("m3")), At: now, Kind: agentstore.KindLaunchReady, Name: "gone-open", By: "ziru", ByKind: "agent"}, 0)
	// A historical base-name record (the mirror keyed by base before unit 1)
	// must not turn an ambiguous base into a tombstone.
	proj.Apply(agentstore.Event{ID: agentstore.DerivedID([]byte("m4")), At: now.Add(-time.Hour), Kind: agentstore.KindMirrorReady, Name: "dupe", By: "ziru", ByKind: "mirror"}, 0)
	at := now
	cases := []struct {
		name        string
		view        agentstore.AgentView
		wantManager string
		wantState   string
	}{
		{"registered by a human shell", agentstore.AgentView{Manager: "ubuntu", Provenance: agentstore.Provenance{LauncherKind: "user"}}, "ubuntu", ManagerOperator},
		{"registered from the web", agentstore.AgentView{Manager: "web-owner", Provenance: agentstore.Provenance{LauncherKind: "web"}}, "web-owner", ManagerOperator},
		{"mirrored hcom user", agentstore.AgentView{Manager: "user", Provenance: agentstore.Provenance{LauncherKind: "mirror"}}, "user", ManagerOperator},
		{"assigned literal human", agentstore.AgentView{Manager: "human", ManagerAt: &at, Provenance: agentstore.Provenance{LauncherKind: "agent"}}, "human", ManagerOperator},
		{"assigned to a web identity with no record", agentstore.AgentView{Manager: "web-owner", ManagerAt: &at, Provenance: agentstore.Provenance{LauncherKind: "agent"}}, "web-owner", ManagerOperator},
		{"live full name", agentstore.AgentView{Manager: "ziru", Provenance: agentstore.Provenance{LauncherKind: "agent"}}, "ziru", ManagerLive},
		{"live unique base name resolves", agentstore.AgentView{Manager: "hamo", Provenance: agentstore.Provenance{LauncherKind: "mirror"}}, "orch-hamo", ManagerLive},
		{"ambiguous base name with a historical base record stays unknown, never ended", agentstore.AgentView{Manager: "dupe", Provenance: agentstore.Provenance{LauncherKind: "mirror"}}, "dupe", ManagerUnknown},
		{"ambiguous base name that looks like a web identity stays unknown", agentstore.AgentView{Manager: "web-dupe", ManagerAt: &at, Provenance: agentstore.Provenance{LauncherKind: "agent"}}, "web-dupe", ManagerUnknown},
		{"closed record", agentstore.AgentView{Manager: "orch-dead", Provenance: agentstore.Provenance{LauncherKind: "agent"}}, "orch-dead", ManagerEnded},
		{"open record off the roster", agentstore.AgentView{Manager: "gone-open", Provenance: agentstore.Provenance{LauncherKind: "agent"}}, "gone-open", ManagerEnded},
		{"no record", agentstore.AgentView{Manager: "fimu", Provenance: agentstore.Provenance{LauncherKind: "agent"}}, "fimu", ManagerUnknown},
		{"literal unknown", agentstore.AgentView{Manager: "unknown", Provenance: agentstore.Provenance{LauncherKind: "mirror"}}, "unknown", ManagerUnknown},
		{"empty", agentstore.AgentView{}, "", ManagerUnknown},
	}
	for _, c := range cases {
		view := c.view
		manager, state := managerEdge(&view, roster, proj)
		if manager != c.wantManager || state != c.wantState {
			t.Errorf("%s: = %q/%q, want %q/%q", c.name, manager, state, c.wantManager, c.wantState)
		}
	}
}
