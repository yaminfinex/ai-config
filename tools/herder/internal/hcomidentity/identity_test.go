package hcomidentity

import "testing"

func boolPtr(v bool) *bool { return &v }

func TestResolvePrefersLiveSessionIdentityOverStoredName(t *testing.T) {
	rows := []Row{
		{Name: "worker-live-self", BaseName: "live-self", SessionID: "sess-self", Joined: boolPtr(true)},
		{Name: "live-neighbor", SessionID: "sess-other", Joined: boolPtr(true)},
	}

	got := Resolve(rows, Evidence{SessionID: "sess-self"})
	if !got.Verified || got.Name != "worker-live-self" || got.BaseName != "live-self" {
		t.Fatalf("Resolve = %+v, want verified tagged identity with base name", got)
	}
	if ok, _ := VerifyStored(rows, Evidence{SessionID: "sess-self"}, "stale-launch-name"); ok {
		t.Fatal("VerifyStored accepted a stopped stale name")
	}
}

func TestJoinedStoredCountResolvesTaggedFullOrBaseName(t *testing.T) {
	rows := []Row{{Name: "worker-peer-seat", BaseName: "peer-seat", Joined: boolPtr(true)}}
	for _, stored := range []string{"worker-peer-seat", "peer-seat"} {
		row, count := JoinedStoredCount(rows, stored)
		if count != 1 || row.Name != "worker-peer-seat" || row.BaseName != "peer-seat" {
			t.Fatalf("JoinedStoredCount(%q) = (%+v, %d), want tagged row", stored, row, count)
		}
	}
	if StoredNameMatches("worker-peer-seat", "peer-seat", "other-peer-seat") {
		t.Fatal("StoredNameMatches manufactured a display name instead of matching an emitted form")
	}
}

func TestVerifyStoredRejectsJoinedNeighbor(t *testing.T) {
	rows := []Row{
		{Name: "live-self", SessionID: "sess-self", Joined: boolPtr(true)},
		{Name: "live-neighbor", SessionID: "sess-other", Joined: boolPtr(true)},
	}

	ok, got := VerifyStored(rows, Evidence{SessionID: "sess-self"}, "live-neighbor")
	if ok || !got.Verified || got.Name != "live-self" {
		t.Fatalf("VerifyStored = (%v, %+v), want mismatch against verified live-self", ok, got)
	}
}

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

func TestCurrentEvidenceIncludesCallerProcessAndAllPaneForms(t *testing.T) {
	t.Setenv("HCOM_SESSION_ID", "")
	t.Setenv("HCOM_PROCESS_ID", "process-self")
	got := CurrentEvidence("pane-from-launch", "pane-canonical")
	if got.ProcessID != "process-self" || len(got.PaneIDs) != 2 || got.PaneIDs[0] != "pane-from-launch" || got.PaneIDs[1] != "pane-canonical" {
		t.Fatalf("CurrentEvidence = %+v, want caller process plus launch/canonical panes", got)
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

func TestDecodeAcceptsJSONLines(t *testing.T) {
	rows, err := Decode([]byte("{\"name\":\"one\"}\n{\"name\":\"two\"}\n"))
	if err != nil || len(rows) != 2 || rows[1].Name != "two" {
		t.Fatalf("Decode = (%+v, %v), want two JSONL rows", rows, err)
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
		rows, err := Decode([]byte(tt.raw))
		if err != nil || len(rows) != 1 {
			t.Fatalf("Decode(%s) = (%+v, %v)", tt.raw, rows, err)
		}
		if got := rows[0].LaunchContext.Empty(); got != tt.empty {
			t.Fatalf("LaunchContext.Empty(%s) = %v, want %v", tt.raw, got, tt.empty)
		}
	}
}

func TestResolveExactSessionPaneRequiresOneRowMatchingBoth(t *testing.T) {
	joined := boolPtr(true)
	rows := []Row{
		{Name: "right", SessionID: "sid", Joined: joined, LaunchContext: LaunchContext{PaneID: "pane"}},
		{Name: "session-only", SessionID: "sid", Joined: joined, LaunchContext: LaunchContext{PaneID: "elsewhere"}},
		{Name: "pane-only", SessionID: "other", Joined: joined, LaunchContext: LaunchContext{PaneID: "pane"}},
	}
	if got := ResolveExactSessionPane(rows, "sid", "pane"); !got.Verified || got.Name != "right" {
		t.Fatalf("ResolveExactSessionPane = %+v, want unique row matching both", got)
	}
	rows = append(rows, Row{Name: "duplicate", SessionID: "sid", Joined: joined, LaunchContext: LaunchContext{PaneID: "pane"}})
	if got := ResolveExactSessionPane(rows, "sid", "pane"); got.Verified {
		t.Fatalf("ResolveExactSessionPane duplicate = %+v, want refusal", got)
	}
}
