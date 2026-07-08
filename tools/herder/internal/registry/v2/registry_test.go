package v2

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "tests", "fixtures", "registry-v2", name)
}

func loadFixture(t *testing.T, name string) (*Projection, string) {
	t.Helper()
	var stderr bytes.Buffer
	p, err := LoadFile(fixture(t, name), LoadOptions{LocalNodeID: "node-local", Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	return p, stderr.String()
}

func TestMixedKindsPartitionBeforeCollapse(t *testing.T) {
	p, stderr := loadFixture(t, "mixed-kinds.jsonl")
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if len(p.Quarantined()) != 0 || len(p.Anomalies()) != 0 {
		t.Fatalf("quarantine=%v anomalies=%v, want none", p.Quarantined(), p.Anomalies())
	}
	if got := p.Sessions(); len(got) != 1 || got[0].GUID != "node-local" || got[0].State != StateUnseated {
		t.Fatalf("Sessions = %+v, want latest unseated session guid node-local only", got)
	}
	if got := p.Nodes(); len(got) != 1 || got[0].NodeID != "node-local" {
		t.Fatalf("Nodes = %+v, want node-local", got)
	}
	if got := p.Namespaces(); len(got) != 1 || got[0].NamespaceID != "ns-default" {
		t.Fatalf("Namespaces = %+v, want ns-default", got)
	}
	if got := p.Epochs(); len(got) != 1 || got[0].EpochID != "epoch-hcom-1" {
		t.Fatalf("Epochs = %+v, want epoch-hcom-1", got)
	}
}

func TestTornRowsAreQuarantined(t *testing.T) {
	p, stderr := loadFixture(t, "torn-rows.jsonl")
	if len(p.Quarantined()) != 2 {
		t.Fatalf("quarantined = %+v, want two bad lines", p.Quarantined())
	}
	if !strings.Contains(stderr, "quarantined line 2") || !strings.Contains(stderr, "quarantined line 3") {
		t.Fatalf("stderr = %q, want warnings for lines 2 and 3", stderr)
	}
	if got := p.Sessions(); len(got) != 2 || got[0].GUID != "valid-after" || got[1].GUID != "valid-before" {
		t.Fatalf("Sessions = %+v, want both valid rows sorted by guid", got)
	}
}

func TestProjectionAnomaliesAreLoudAndDeterministic(t *testing.T) {
	p, _ := loadFixture(t, "duplicate-labels.jsonl")
	var sawLabel, sawSeat, sawNode bool
	for _, a := range p.Anomalies() {
		switch a.Type {
		case "duplicate-live-label":
			sawLabel = true
			if a.Label != "shared" || a.WinnerGUID != "guid-alpha" || strings.Join(a.GUIDs, ",") != "guid-beta,guid-alpha" {
				t.Fatalf("duplicate label anomaly = %+v, want guid-alpha as file-order winner", a)
			}
		case "double-seated-session":
			sawSeat = true
			if a.GUID != "guid-alpha" {
				t.Fatalf("double-seat anomaly = %+v, want guid-alpha", a)
			}
		case "unknown-node":
			sawNode = true
			if a.GUID != "guid-remote" || a.Node != "node-remote" {
				t.Fatalf("unknown-node anomaly = %+v, want guid-remote/node-remote", a)
			}
		}
	}
	if !sawLabel || !sawSeat || !sawNode {
		t.Fatalf("anomalies = %+v, want duplicate label, double seat, and unknown node", p.Anomalies())
	}
	if got := p.Sessions(); len(got) != 3 {
		t.Fatalf("Sessions len = %d, want 3", len(got))
	}
}

func TestLegacyV1RealShapesMapReadOnly(t *testing.T) {
	p, stderr := loadFixture(t, "v1-real-shape.jsonl")
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if len(p.Quarantined()) != 0 || len(p.Anomalies()) != 0 {
		t.Fatalf("quarantine=%v anomalies=%v, want none", p.Quarantined(), p.Anomalies())
	}
	sessions := p.Sessions()
	if len(sessions) != 4 {
		t.Fatalf("got %d sessions, want 4 after byte-duplicate v1 rows collapse by guid", len(sessions))
	}
	byGUID := map[string]SessionRecord{}
	for _, rec := range sessions {
		byGUID[rec.GUID] = rec
		if !rec.LegacyV1 || rec.Event != "legacy_v1_mapped" {
			t.Fatalf("legacy row not marked as read-only mapped: %+v", rec)
		}
		if rec.Seat != nil {
			t.Fatalf("legacy row got seated verbatim: %+v", rec)
		}
	}
	corpse := byGUID["2447b0e6-5004-4aca-84cd-08d7798dad52"]
	if corpse.State != StateUnseated || corpse.Tool != "claude" || corpse.Continuity != "assumed" {
		t.Fatalf("corpse active = %+v, want unseated claude assumed", corpse)
	}
	closed := byGUID["366fb03a-2f91-47f8-8a6c-eee954e413a5"]
	if closed.State != StateRetired || len(closed.SIDs) != 1 || closed.Continuity != "confirmed" {
		t.Fatalf("closed hcom row = %+v, want retired with seeded sid and confirmed continuity", closed)
	}
	team := byGUID["24cb80b1-852f-4d30-8f78-e241aaf7c97e"]
	if team.State != StateUnseated || team.Tool != "claude" || len(team.SIDs) != 1 {
		t.Fatalf("teams-era active row = %+v, want unseated and sid-seeded", team)
	}
	bash := byGUID["01b4c997-36b3-4684-9d99-8fd20c29889d"]
	if bash.State != StateUnseated || bash.Tool != "bash" {
		t.Fatalf("bash row = %+v, want bash tool preserved and unseated", bash)
	}
}
