package retirecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestRetireUnseatedReleasesLabel(t *testing.T) {
	path := seedRetireRegistry(t, v2.SessionRecord{
		GUID:       "guid-alpha",
		Event:      "migrated_v1",
		RecordedAt: "2026-07-08T00:00:00Z",
		State:      v2.StateUnseated,
		Label:      "alpha",
		Role:       "worker",
		Tool:       "codex",
	})
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))

	var stdout, stderr strings.Builder
	if rc := RunRetire([]string{"alpha", "--json"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("RunRetire rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	got := latestSession(t, path, "guid-alpha")
	if got.Event != "retired" || got.State != v2.StateRetired || got.Label != "" || got.Seat != nil {
		t.Fatalf("latest = %+v, want retired unlabelled without seat", got)
	}
	if !strings.Contains(stderr.String(), `retired "alpha" (guid-alpha); label released`) {
		t.Fatalf("stderr = %q, want retired label release", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"event":"retired"`) || strings.Contains(stdout.String(), `"label"`) {
		t.Fatalf("json stdout = %q, want retired row without label", stdout.String())
	}
}

func TestRetireRefusalsAndIdempotentNoop(t *testing.T) {
	cases := []struct {
		name       string
		rec        v2.SessionRecord
		wantRC     int
		wantStderr string
	}{
		{
			name: "seated",
			rec: v2.SessionRecord{
				GUID:  "guid-seated",
				State: v2.StateSeated,
				Label: "seated",
				Seat:  &v2.Seat{Kind: "herdr", PaneID: "p_seated"},
			},
			wantRC:     1,
			wantStderr: "cull first",
		},
		{
			name:       "lost",
			rec:        v2.SessionRecord{GUID: "guid-lost", State: v2.StateLost, Label: "lost"},
			wantRC:     1,
			wantStderr: "LOST sessions cannot be retired",
		},
		{
			name:       "already-retired",
			rec:        v2.SessionRecord{GUID: "guid-retired", State: v2.StateRetired, Label: "retired"},
			wantRC:     0,
			wantStderr: "no registry row appended",
		},
		{
			name:       "unseated-with-seat",
			rec:        v2.SessionRecord{GUID: "guid-anomaly", State: v2.StateUnseated, Label: "anomaly", Seat: &v2.Seat{Kind: "herdr", PaneID: "p_bad"}},
			wantRC:     1,
			wantStderr: "still has a seat",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := seedRetireRegistry(t, c.rec)
			t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))
			before := sessionEventCount(t, path, c.rec.GUID, "retired")

			var stdout, stderr strings.Builder
			if rc := RunRetire([]string{c.rec.Label}, &stdout, &stderr); rc != c.wantRC {
				t.Fatalf("RunRetire rc = %d, want %d\nstdout:\n%s\nstderr:\n%s", rc, c.wantRC, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), c.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), c.wantStderr)
			}
			after := sessionEventCount(t, path, c.rec.GUID, "retired")
			if c.name == "already-retired" && after != before {
				t.Fatalf("retired event count = %d after no-op, want %d", after, before)
			}
		})
	}
}

func TestReopenRetiredStripsLabelAndRefusesNonRetired(t *testing.T) {
	path := seedRetireRegistry(t,
		v2.SessionRecord{GUID: "guid-retired", State: v2.StateRetired, Label: "old"},
		v2.SessionRecord{GUID: "guid-unseated", State: v2.StateUnseated, Label: "open"},
	)
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))

	var stdout, stderr strings.Builder
	if rc := RunReopen([]string{"old", "--json"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("RunReopen rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	got := latestSession(t, path, "guid-retired")
	if got.Event != "reopened" || got.State != v2.StateUnseated || got.Label != "" || got.Seat != nil {
		t.Fatalf("latest = %+v, want reopened unseated unlabelled", got)
	}

	stdout.Reset()
	stderr.Reset()
	if rc := RunReopen([]string{"open"}, &stdout, &stderr); rc == 0 {
		t.Fatalf("RunReopen on unseated rc = 0, want nonzero")
	}
	if !strings.Contains(stderr.String(), "not retired") {
		t.Fatalf("stderr = %q, want not-retired refusal", stderr.String())
	}
}

func seedRetireRegistry(t *testing.T, recs ...v2.SessionRecord) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	_, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		for i := range recs {
			if recs[i].Kind == "" {
				recs[i].Kind = v2.KindSession
			}
			if recs[i].Event == "" {
				recs[i].Event = "registered"
			}
			if recs[i].RecordedAt == "" {
				recs[i].RecordedAt = "2026-07-08T00:00:00Z"
			}
		}
		return recs, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func latestSession(t *testing.T, path, guid string) v2.SessionRecord {
	t.Helper()
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.V2ByGUID(proj, guid)
	if rec == nil {
		t.Fatalf("missing guid %s", guid)
	}
	return *rec
}

func sessionEventCount(t *testing.T, path, guid, event string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(line, `"guid":"`+guid+`"`) && strings.Contains(line, `"event":"`+event+`"`) {
			count++
		}
	}
	return count
}
