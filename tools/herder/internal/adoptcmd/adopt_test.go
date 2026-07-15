package adoptcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestDifferentPaneSeatedTargetRefusesBeforeEnrollment(t *testing.T) {
	path := seedAdoptRegistry(t,
		v2.SessionRecord{
			GUID:  "guid-previous",
			State: v2.StateSeated,
			Label: "stable",
			Seat:  &v2.Seat{Kind: "herdr", TerminalID: "term_previous", PaneID: "pane_previous"},
		},
	)
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))
	t.Setenv("HERDR_PANE_ID", "pane_replacement")
	t.Setenv("HCOM_SESSION_ID", "")
	t.Setenv("HCOM_PROCESS_ID", "")
	t.Setenv("HCOM_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	before := mustReadAdoptRegistry(t, path)

	var stdout, stderr strings.Builder
	if rc := Run([]string{"guid-previous"}, &stdout, &stderr); rc == 0 {
		t.Fatalf("Run rc = 0, want refusal")
	}
	for _, want := range []string{
		"seated on pane pane_previous",
		"caller's own pane is not proven",
		"refusing before enrollment",
		"herder adopt guid-previous --confirm-dead",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if strings.Contains(stderr.String(), "herder cull") {
		t.Fatalf("stderr = %q, must not suggest culling from an adoption preflight", stderr.String())
	}
	if after := mustReadAdoptRegistry(t, path); after != before {
		t.Fatalf("preflight refusal changed registry\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestAdoptionUnseatAuthorization(t *testing.T) {
	cases := []struct {
		name        string
		oldPane     string
		caller      hcomidentity.Result
		confirmDead bool
		wantReason  string
		wantError   string
	}{
		{
			name:       "same pane is proven superseded",
			oldPane:    "pane_shared",
			caller:     hcomidentity.Result{Verified: true, PaneID: "pane_shared"},
			wantReason: "seat superseded by replacement process in the same pane",
		},
		{
			name:      "different pane refuses",
			oldPane:   "pane_previous",
			caller:    hcomidentity.Result{Verified: true, PaneID: "pane_replacement"},
			wantError: "caller occupies pane pane_replacement",
		},
		{
			name:      "unresolved caller refuses",
			oldPane:   "pane_previous",
			caller:    hcomidentity.Result{Reason: "ambiguous evidence"},
			wantError: "unverified: ambiguous evidence",
		},
		{
			name:        "operator confirms dead",
			oldPane:     "pane_previous",
			caller:      hcomidentity.Result{Reason: "ambiguous evidence"},
			confirmDead: true,
			wantReason:  "operator confirmed old transcript dead",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, err := adoptionUnseatReason(tc.oldPane, tc.caller, tc.confirmDead)
			if reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReason)
			}
			if tc.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantError)
			}
		})
	}
}

func TestDifferentPaneRemedyIsAcceptedByParser(t *testing.T) {
	var stdout, stderr strings.Builder
	opts, code := parseArgs([]string{"guid-previous", "--confirm-dead"}, &stdout, &stderr)
	if code != 0 || opts.target != "guid-previous" || !opts.confirmDead {
		t.Fatalf("parseArgs = %+v, code %d, stderr %q", opts, code, stderr.String())
	}
}

func TestResumedSessionAssertionIsAcceptedByParser(t *testing.T) {
	var stdout, stderr strings.Builder
	opts, code := parseArgs([]string{"guid-previous", "--confirm-resumed-session"}, &stdout, &stderr)
	if code != 0 || opts.target != "guid-previous" || !opts.confirmResumedSession {
		t.Fatalf("parseArgs = %+v, code %d, stderr %q", opts, code, stderr.String())
	}
}

func seedAdoptRegistry(t *testing.T, recs ...v2.SessionRecord) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		for i := range recs {
			recs[i].Kind = v2.KindSession
			recs[i].Event = "registered"
			recs[i].RecordedAt = "2026-07-12T00:00:00Z"
		}
		return recs, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func mustReadAdoptRegistry(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
