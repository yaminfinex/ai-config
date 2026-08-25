package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestLoadParsesRowsAndKeepsRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	raw := `{"guid":"g1","label":"alpha","agent":"codex","status":"active"}`
	writeFile(t, path, raw+"\n")
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].GUID == nil || *recs[0].GUID != "g1" || string(recs[0].Raw) != raw {
		t.Fatalf("unexpected records: %#v", recs)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want not-exist", err)
	}
}

func TestLoadQuarantinesMalformedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	writeFile(t, path, "not-json\n"+sessionJSON("g1", "seed", v2.StateUnseated)+"\n")
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].GUID == nil || *recs[0].GUID != "g1" {
		t.Fatalf("unexpected records: %#v", recs)
	}
}

func TestLatestByGUIDCollapsesAndSorts(t *testing.T) {
	a, b := "a", "b"
	recs := []Record{{GUID: &b, State: "first"}, {GUID: &a, State: "only"}, {GUID: &b, State: "last"}}
	got := LatestByGUID(recs)
	if len(got) != 2 || *got[0].GUID != "a" || *got[1].GUID != "b" || got[1].State != "last" {
		t.Fatalf("unexpected collapse: %#v", got)
	}
}

func TestLockedWriteMintsNodeOnceAndStampsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	writeFile(t, path, sessionJSON("g1", "seed", v2.StateUnseated)+"\n")

	for i := 0; i < 2; i++ {
		outcomes, err := UpdateLocked(path, func(update LockedUpdate) ([]v2.SessionRecord, error) {
			current := v2ByGUID(update.Projection, "g1")
			return []v2.SessionRecord{observedRow(*current)}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(outcomes) != 1 || outcomes[0].Status != WriteApplied {
			t.Fatalf("unexpected outcomes: %#v", outcomes)
		}
	}

	projection := loadProjection(t, path)
	if len(projection.Nodes()) != 1 {
		t.Fatalf("nodes=%d, want 1", len(projection.Nodes()))
	}
	current := v2ByGUID(projection, "g1")
	if current == nil || current.Node == "" || current.Seat == nil || current.Seat.Node != current.Node {
		t.Fatalf("row was not stamped with node identity: %#v", current)
	}
}

func TestLockedWriteReturnsPerCandidateOutcomes(t *testing.T) {
	path := seededRegistry(t, "g1", "g2")
	outcomes, err := UpdateLocked(path, func(update LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{observedRow(*v2ByGUID(update.Projection, "g1")), deadRow(*v2ByGUID(update.Projection, "g2"))}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 || outcomes[0].Status != WriteApplied || outcomes[1].Status != WriteApplied {
		t.Fatalf("unexpected outcomes: %#v", outcomes)
	}
}

func TestLockedWriteRefusalIsAtomicAndReportedPerCandidate(t *testing.T) {
	path := seededRegistry(t, "g1", "g2")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := UpdateLocked(path, func(update LockedUpdate) ([]v2.SessionRecord, error) {
		bad := observedRow(*v2ByGUID(update.Projection, "g2"))
		bad.Cache = nil
		return []v2.SessionRecord{observedRow(*v2ByGUID(update.Projection, "g1")), bad}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("refused batch changed registry\nbefore=%s\nafter=%s", before, after)
	}
	if len(outcomes) != 2 || outcomes[0].Status != WriteRefused || outcomes[1].Status != WriteRefused {
		t.Fatalf("unexpected outcomes: %#v", outcomes)
	}
}

func TestLockedWriteRejectsNonObserverEvents(t *testing.T) {
	path := seededRegistry(t, "g1")
	outcomes, err := UpdateLocked(path, func(update LockedUpdate) ([]v2.SessionRecord, error) {
		row := *v2ByGUID(update.Projection, "g1")
		row.Event = "registered"
		row.RecordedAt = "2026-08-25T00:01:00Z"
		return []v2.SessionRecord{row}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != WriteRefused || !strings.Contains(outcomes[0].Reason, "observer writes cache observations only") {
		t.Fatalf("unexpected outcomes: %#v", outcomes)
	}
}

func TestLegacyRowsRemainUnmigratedDuringCacheMaintenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	legacy := `{"guid":"legacy-guid","agent":"codex","status":"active"}`
	writeFile(t, path, legacy+"\n")
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil })
	if err != nil || len(outcomes) != 0 {
		t.Fatalf("outcomes=%#v err=%v", outcomes, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(legacy+"\n")) {
		t.Fatalf("legacy row was rewritten or migrated:\n%s", data)
	}
	if _, err := os.Stat(path + ".archive"); !os.IsNotExist(err) {
		t.Fatalf("cache maintenance created an archive: %v", err)
	}
}

func TestLockedWriteRefusesHalfPresentNodeState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	writeFile(t, path, sessionJSON("g1", "seed", v2.StateUnseated)+"\n")
	writeFile(t, nodeMarkerPath(path), "11111111-1111-4111-8111-111111111111\n")
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil })
	if len(outcomes) != 0 || err == nil || !strings.Contains(err.Error(), "registry has no node_registered row") {
		t.Fatalf("outcomes=%#v err=%v, want node gate refusal", outcomes, err)
	}
}

func TestUnknownNodeRowsAreReadOnlyButDoNotBlockLocalWrites(t *testing.T) {
	path := seededRegistry(t, "local", "foreign")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	foreign := v2.SessionRecord{Kind: v2.KindSession, GUID: "foreign", Event: "seed", RecordedAt: "2026-08-25T00:00:00Z", Node: "99999999-9999-4999-8999-999999999999", State: v2.StateUnseated}
	encoded, _ := json.Marshal(foreign)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	writeFile(t, path, strings.Join([]string{lines[0], lines[1], string(encoded)}, "\n")+"\n")

	outcomes, err := UpdateLocked(path, func(update LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{deadRow(*v2ByGUID(update.Projection, "foreign"))}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != WriteRefused || !strings.Contains(outcomes[0].Reason, "unknown node") {
		t.Fatalf("unexpected outcomes: %#v", outcomes)
	}
}

func TestTwoProcessFirstWritersConvergeOnOneNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	writeFile(t, path, sessionJSON("g1", "seed", v2.StateUnseated)+"\n")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcomes, err := UpdateLocked(path, func(update LockedUpdate) ([]v2.SessionRecord, error) {
				return []v2.SessionRecord{observedRow(*v2ByGUID(update.Projection, "g1"))}, nil
			})
			if err == nil && (len(outcomes) != 1 || outcomes[0].Status != WriteApplied) {
				err = fmt.Errorf("unexpected outcomes: %#v", outcomes)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if nodes := loadProjection(t, path).Nodes(); len(nodes) != 1 {
		t.Fatalf("nodes=%d, want 1", len(nodes))
	}
}

func TestRotationCompactsLiveCacheWithoutArchives(t *testing.T) {
	path := seededRegistry(t, "g1")
	projection := loadProjection(t, path)
	current := *v2ByGUID(projection, "g1")
	var data []byte
	node, _ := json.Marshal(projection.Nodes()[0])
	data = append(data, append(node, '\n')...)
	for i := 0; i < 20; i++ {
		row := observedRow(current)
		row.RecordedAt = "2026-08-25T00:01:00Z"
		encoded, _ := json.Marshal(row)
		data = append(data, append(encoded, '\n')...)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(rotationThresholdEnv, "1200")
	outcomes, err := UpdateLocked(path, func(update LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{observedRow(*v2ByGUID(update.Projection, "g1"))}, nil
	})
	if err != nil || len(outcomes) != 1 || outcomes[0].Status != WriteApplied {
		t.Fatalf("outcomes=%#v err=%v", outcomes, err)
	}
	after, _ := os.ReadFile(path)
	if bytes.Count(after, []byte("\n")) != 3 {
		t.Fatalf("rotation should reseed node + snapshot then append candidate; got:\n%s", after)
	}
	if _, err := os.Stat(path + ".archive"); !os.IsNotExist(err) {
		t.Fatalf("rotation created archive state: %v", err)
	}
}

func TestRotationDropsRetiredSnapshots(t *testing.T) {
	proj, err := v2.Load(strings.NewReader(sessionJSON("live", "seed", v2.StateUnseated)+"\n"+sessionJSON("old", "seed", v2.StateRetired)+"\n"), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := compactProjectionBytes(proj)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`"guid":"live"`)) || bytes.Contains(out, []byte(`"guid":"old"`)) {
		t.Fatalf("unexpected compact cache: %s", out)
	}
}

func TestRotationSkipsWhenCompactSnapshotStillExceedsThreshold(t *testing.T) {
	path := seededRegistry(t, "g1")
	t.Setenv(rotationThresholdEnv, "1")
	before, _ := os.ReadFile(path)
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil })
	if err != nil || len(outcomes) != 0 {
		t.Fatalf("outcomes=%#v err=%v", outcomes, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("oversized minimal snapshot should not be rewritten repeatedly")
	}
}

func TestRotationInvalidThresholdNamesFix(t *testing.T) {
	path := seededRegistry(t, "g1")
	t.Setenv(rotationThresholdEnv, "not-a-number")
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil })
	if len(outcomes) != 0 || err == nil || !strings.Contains(err.Error(), rotationThresholdEnv) {
		t.Fatalf("outcomes=%#v err=%v, want actionable threshold error", outcomes, err)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("HERDER_STATE_DIR", "/tmp/herder-state")
	if got := DefaultPath(); got != "/tmp/herder-state/registry.jsonl" {
		t.Fatalf("got %q", got)
	}
}

func seededRegistry(t *testing.T, guids ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	nodeID := "11111111-1111-4111-8111-111111111111"
	node := v2.NodeRecord{Kind: v2.KindNode, Event: "node_registered", NodeID: nodeID, RecordedAt: "2026-08-25T00:00:00Z"}
	encoded, _ := json.Marshal(node)
	var data strings.Builder
	data.Write(encoded)
	data.WriteByte('\n')
	for _, guid := range guids {
		row := v2.SessionRecord{Kind: v2.KindSession, GUID: guid, Event: "seed", RecordedAt: "2026-08-25T00:00:00Z", Node: nodeID, State: v2.StateUnseated, Tool: "codex"}
		encoded, _ := json.Marshal(row)
		data.Write(encoded)
		data.WriteByte('\n')
	}
	writeFile(t, path, data.String())
	writeFile(t, nodeMarkerPath(path), nodeID+"\n")
	return path
}

func observedRow(current v2.SessionRecord) v2.SessionRecord {
	current.Event = "observed"
	current.RecordedAt = "2026-08-25T00:01:00Z"
	current.State = v2.StateSeated
	current.Seat = &v2.Seat{Kind: "herdr", PaneID: "pane-1", TerminalID: "term-1"}
	current.Cache = &v2.CacheObservation{PaneID: "pane-1", Liveness: "alive", ObservedAt: current.RecordedAt}
	return current
}

func deadRow(current v2.SessionRecord) v2.SessionRecord {
	current.Event = "observed_dead"
	current.RecordedAt = "2026-08-25T00:01:00Z"
	current.State = v2.StateUnseated
	current.Seat = nil
	current.Cache = &v2.CacheObservation{Liveness: "dead", ObservedAt: current.RecordedAt}
	return current
}

func sessionJSON(guid, event, state string) string {
	row := v2.SessionRecord{Kind: v2.KindSession, GUID: guid, Event: event, RecordedAt: "2026-08-25T00:00:00Z", State: state}
	encoded, _ := json.Marshal(row)
	return string(encoded)
}

func loadProjection(t *testing.T, path string) *v2.Projection {
	t.Helper()
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
