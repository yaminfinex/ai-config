package observercmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

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
