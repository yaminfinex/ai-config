package hcomidentity

import "testing"

func boolPtr(v bool) *bool { return &v }

func TestResolvePrefersLiveSessionIdentityOverStoredName(t *testing.T) {
	rows := []Row{
		{Name: "live-self", SessionID: "sess-self", Joined: boolPtr(true)},
		{Name: "live-neighbor", SessionID: "sess-other", Joined: boolPtr(true)},
	}

	got := Resolve(rows, Evidence{SessionID: "sess-self"})
	if !got.Verified || got.Name != "live-self" {
		t.Fatalf("Resolve = %+v, want verified live-self", got)
	}
	if ok, _ := VerifyStored(rows, Evidence{SessionID: "sess-self"}, "stale-launch-name"); ok {
		t.Fatal("VerifyStored accepted a stopped stale name")
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

func TestDecodeAcceptsJSONLines(t *testing.T) {
	rows, err := Decode([]byte("{\"name\":\"one\"}\n{\"name\":\"two\"}\n"))
	if err != nil || len(rows) != 2 || rows[1].Name != "two" {
		t.Fatalf("Decode = (%+v, %v), want two JSONL rows", rows, err)
	}
}
