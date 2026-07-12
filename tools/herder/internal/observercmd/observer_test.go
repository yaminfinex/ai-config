package observercmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestApplyCandidatesRefusalLeavesBatchUnapplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if _, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{
			{GUID: "guid-healthy", Label: "healthy", State: v2.StateSeated},
			{GUID: "guid-poison", Label: "poison", State: v2.StateSeated},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	healthy := registry.V2ByGUID(proj, "guid-healthy")
	if healthy == nil {
		t.Fatal("missing healthy session")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cands := []candidate{
		unseatCandidate(*healthy, time.Now().UTC(), "process gone", "process sweep"),
		{
			kind: "unseat",
			guid: "guid-poison",
			row: v2.SessionRecord{
				GUID:     "guid-poison",
				Event:    "legacy_v1_mapped",
				State:    v2.StateUnseated,
				Label:    "poison",
				Tool:     "claude",
				LegacyV1: true,
			},
		},
	}

	var stderr bytes.Buffer
	summary := applyCandidates(path, cands, &stderr)
	if summary.Refused != len(cands) || summary.Applied != 0 {
		t.Fatalf("summary = %+v, want all candidates refused", summary)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("registry changed after refused observer batch:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestReconfirmRefreshesBusIdentityFromLiveCorrelate(t *testing.T) {
	rec := v2.SessionRecord{
		GUID:  "guid-self",
		State: v2.StateSeated,
		Seat:  &v2.Seat{Kind: "herdr", PaneID: "p_old", HcomName: "poisoned-name"},
		SIDs:  []v2.SID{{SID: "sess-live"}},
	}
	joined := true
	bus := busState{available: true, rows: map[string]hcomidentity.Row{
		"live-self": {Name: "live-self", SessionID: "sess-live", Joined: &joined, LaunchContext: hcomidentity.LaunchContext{PaneID: "p_new"}},
	}}

	cand := reconfirmCandidate(rec, herdrcli.Pane{PaneID: "p_new"}, bus, time.Now().UTC())
	if cand.row.Seat == nil || cand.row.Seat.HcomName != "live-self" || cand.row.Seat.HcomVerified == nil || !*cand.row.Seat.HcomVerified {
		t.Fatalf("reconfirmed seat = %+v, want verified live-self", cand.row.Seat)
	}
}

func TestSuccessorCarryMarksBusIdentityUnverifiedWithoutProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	current := v2.SessionRecord{
		GUID:       "guid-old",
		Event:      "seated",
		State:      v2.StateSeated,
		Seat:       &v2.Seat{Kind: "herdr", PaneID: "p_self", HcomName: "poisoned-name"},
		SIDs:       []v2.SID{{SID: "sess-old"}},
		Provenance: v2.Provenance{Mechanism: "enroll"},
	}
	if _, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{current}, nil
	}); err != nil {
		t.Fatal(err)
	}
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}

	rows, ok := turnoverRowsLocked(proj, current, "sess-new", hcomidentity.Result{Reason: "no live proof"}, time.Now().UTC())
	if !ok || len(rows) != 2 || rows[0].Seat == nil || rows[0].Seat.HcomVerified == nil || *rows[0].Seat.HcomVerified {
		t.Fatalf("turnover rows = %+v, want successor with explicit unverified bus identity", rows)
	}
}
