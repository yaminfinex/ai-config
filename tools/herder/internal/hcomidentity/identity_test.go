package hcomidentity

import "testing"

func boolPtr(v bool) *bool { return &v }

func TestResolveRefusesConflictingCorrelates(t *testing.T) {
	rows := []Row{
		{Name: "by-session", SessionID: "sess-self", Joined: boolPtr(true)},
		{Name: "by-process", Joined: boolPtr(true), LaunchContext: LaunchContext{ProcessID: "proc-self"}},
	}

	got := Resolve(rows, Evidence{SessionID: "sess-self", ProcessID: "proc-self"})
	if got.Verified {
		t.Fatalf("Resolve = %+v, want conflict refusal", got)
	}
}

func TestResolveIgnoresUnjoinedMatches(t *testing.T) {
	rows := []Row{{Name: "stopped-self", SessionID: "sess-self", Joined: boolPtr(false)}}
	got := Resolve(rows, Evidence{SessionID: "sess-self"})
	if got.Verified {
		t.Fatalf("Resolve = %+v, want unjoined refusal", got)
	}
}

func TestResolveAcceptsEitherRecordedOrCanonicalPane(t *testing.T) {
	rows := []Row{{
		Name: "live-self", Joined: boolPtr(true),
		LaunchContext: LaunchContext{PaneID: "pane-from-launch"},
	}}

	got := Resolve(rows, Evidence{PaneIDs: []string{"pane-from-launch", "pane-canonical"}})
	if !got.Verified || got.Name != "live-self" || got.PaneID != "pane-from-launch" {
		t.Fatalf("Resolve = %+v, want launch-pane correlate to prove live-self", got)
	}
}

func TestResolveUsesCallerProcessWhenPaneFormsMiss(t *testing.T) {
	rows := []Row{{
		Name: "live-self", Joined: boolPtr(true),
		LaunchContext: LaunchContext{PaneID: "pane-from-launch", ProcessID: "process-self"},
	}}
	got := Resolve(rows, Evidence{ProcessID: "process-self", PaneIDs: []string{"pane-stale", "pane-canonical"}})
	if !got.Verified || got.Name != "live-self" {
		t.Fatalf("Resolve = %+v, want caller process to prove live-self", got)
	}
}

func TestResolveUsesExactJoinedNameWhenLaunchCoordinatesAreEmpty(t *testing.T) {
	joined := true
	rows := []Row{{Name: "worker-mine", Joined: &joined}}

	got := Resolve(rows, Evidence{Name: "worker-mine"})
	if !got.Verified || got.Name != "worker-mine" {
		t.Fatalf("Resolve exact name = %+v, want verified worker-mine", got)
	}
}

func TestResolveExactNameFailsClosedOnDuplicateAndConflict(t *testing.T) {
	joined := true
	duplicate := []Row{
		{Name: "worker-mine", Joined: &joined},
		{Name: "worker-mine", Joined: &joined},
	}
	if got := Resolve(duplicate, Evidence{Name: "worker-mine"}); got.Verified || got.Reason != "name matches multiple joined bus rows" {
		t.Fatalf("Resolve duplicate exact name = %+v, want fail-closed name ambiguity", got)
	}

	conflict := []Row{
		{Name: "worker-mine", Joined: &joined},
		{Name: "worker-other", Joined: &joined, LaunchContext: LaunchContext{PaneID: "pane-live"}},
	}
	if got := Resolve(conflict, Evidence{Name: "worker-mine", PaneIDs: []string{"pane-live"}}); got.Verified || got.Reason != "live identity correlates resolve to different bus rows" {
		t.Fatalf("Resolve disagreeing name/pane = %+v, want cross-correlate refusal", got)
	}
}

func TestLaunchContextEmptyRequiresAnActuallyEmptyObject(t *testing.T) {
	tests := []struct {
		raw   string
		empty bool
	}{
		{raw: `[{"name":"empty","launch_context":{}}]`, empty: true},
		{raw: `[{"name":"pane","launch_context":{"pane_id":"p_1"}}]`, empty: false},
		{raw: `[{"name":"unknown","launch_context":{"future_key":true}}]`, empty: false},
		{raw: `[{"name":"null","launch_context":null}]`, empty: false},
	}
	for _, tt := range tests {
		rows, err := decode([]byte(tt.raw))
		if err != nil || len(rows) != 1 {
			t.Fatalf("decode(%s) = (%+v, %v)", tt.raw, rows, err)
		}
		if got := rows[0].LaunchContext.Empty(); got != tt.empty {
			t.Fatalf("LaunchContext.Empty(%s) = %v, want %v", tt.raw, got, tt.empty)
		}
	}
}
