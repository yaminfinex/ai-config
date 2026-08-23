package spawncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/occupant"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcred"
)

func TestCutoverCompactNeverSelectsCallerFromAmbientEnvironment(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	if err := seatcred.EnableCutover(registryPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "pane-poison-parent")
	t.Setenv("HERDER_GUID", "guid-poison-parent")
	t.Setenv("HCOM_SESSION_ID", "sid-poison-parent")
	var stdout, stderr strings.Builder
	if rc := RunCompact([]string{"--dry-run", "compact me"}, &stdout, &stderr); rc != 2 {
		t.Fatalf("RunCompact rc=%d, want credential refusal; stderr=%q", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--credential-file is required") || !strings.Contains(stderr.String(), "hints, not authority") {
		t.Fatalf("stderr=%q, want ambient-authority refusal", stderr.String())
	}
}

func TestHealCompactSeatAppendsLiveObservationForStaleCoordinates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	seed := []byte(`{"guid":"guid-self","label":"self","agent":"claude","status":"active","terminal_id":"term-old","pane_id":"pane-old","provenance":{"mechanism":"enroll","tool_session_id":"sid-self"}}`)
	if err := registry.Append(path, seed); err != nil {
		t.Fatal(err)
	}
	res := occupant.Resolution{
		Observation: occupant.Observation{
			Pane: herdrcli.Pane{PaneID: "pane-live", TerminalID: "term-live"},
			PID:  42, Transcript: "/tmp/sid-self.jsonl", SID: "sid-self", Status: occupant.Occupied,
		},
		Outcome: occupant.Outcome{Status: occupant.Match, MatchAge: occupant.Current, SID: "sid-self"},
		Row:     &v2.SessionRecord{GUID: "guid-self"},
	}
	if err := healCompactSeat(path, res); err != nil {
		t.Fatal(err)
	}
	recs, err := registry.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := registry.Resolve(recs, "guid-self")
	if got == nil || got.PaneID != "pane-live" || got.TerminalID != "term-live" || got.ObservedVia != "verb-time occupant probe" {
		t.Fatalf("healed row = %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"evidence_class":"live-verified"`) || !strings.Contains(string(data), `"transcript_path":"/tmp/sid-self.jsonl"`) {
		t.Fatalf("healed evidence missing: %s", data)
	}
}
