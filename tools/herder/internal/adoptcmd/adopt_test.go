package adoptcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcred"
)

func TestCutoverAdoptNeverSelectsCallerFromAmbientEnvironment(t *testing.T) {
	path := seedAdoptRegistry(t, v2.SessionRecord{
		GUID: "guid-previous", State: v2.StateSeated, Label: "stable", Tool: "codex",
		Seat: &v2.Seat{Kind: "herdr", PaneID: "pane-previous", TerminalID: "term-previous"},
	})
	if err := seatcred.EnableCutover(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))
	t.Setenv("HERDR_PANE_ID", "pane-poison-parent")
	t.Setenv("HERDER_GUID", "guid-poison-parent")
	t.Setenv("HERDER_AGENT", "")
	t.Setenv("HCOM_SESSION_ID", "sid-poison-parent")
	t.Setenv("HCOM_TOOL", "")
	var stdout, stderr strings.Builder
	if rc := Run([]string{"guid-previous"}, &stdout, &stderr); rc != 2 {
		t.Fatalf("Run rc=%d, want credential refusal; stderr=%q", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--credential-file is required") || !strings.Contains(stderr.String(), "hints, not authority") {
		t.Fatalf("stderr=%q, want ambient-authority refusal", stderr.String())
	}
}

func TestCutoverUnseatedAdoptMintsFreshReplacementCredentialWithoutCallerCredential(t *testing.T) {
	path := seedAdoptRegistry(t, v2.SessionRecord{
		GUID: "guid-previous", State: v2.StateUnseated, Label: "stable", Role: "worker", Tool: "codex",
	})
	if err := seatcred.EnableCutover(path); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	herdr := `#!/bin/sh
if [ "$1 $2" = "pane get" ]; then
  printf '%s\n' '{"result":{"pane":{"pane_id":"pane-replacement","terminal_id":"term-replacement","workspace_id":"ws-replacement","cwd":"/mock/cwd"}}}'
fi
exit 0
`
	hcom := `#!/bin/sh
if [ "$1 $2" = "list --json" ]; then
  printf '%s\n' '[]'
  exit 0
fi
if [ "$1" = "start" ]; then
  printf 'intentional reclaim stop\n' >&2
  exit 1
fi
exit 0
`
	for name, body := range map[string]string{"herdr": herdr, "hcom": hcom} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "pane-replacement")
	t.Setenv("HERDER_GUID", "guid-poison-parent")
	t.Setenv("HERDER_AGENT", "")
	t.Setenv("HCOM_SESSION_ID", "sid-poison-parent")
	t.Setenv("HCOM_TOOL", "")
	t.Setenv("HCOM_DIR", t.TempDir())

	var stdout, stderr strings.Builder
	if rc := Run([]string{"guid-previous"}, &stdout, &stderr); rc != 1 {
		t.Fatalf("Run rc=%d, want deliberate late reclaim failure; stderr=%q", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), "--credential-file is required") || !strings.Contains(stderr.String(), "adopt: enroll applied: new guid") {
		t.Fatalf("stderr=%q, want uncredentialed fresh-enroll leg before late failure", stderr.String())
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var replacement *v2.SessionRecord
	for _, session := range projection.Sessions() {
		if session.GUID != "guid-previous" && session.State == v2.StateSeated {
			copy := session
			replacement = &copy
		}
	}
	if replacement == nil || replacement.Seat == nil || replacement.Seat.CredentialGeneration == "" {
		t.Fatalf("replacement=%+v, want fresh seat with first credential committed", replacement)
	}
	if replacement.Provenance.SpawnedBy != "user" && replacement.Provenance.SpawnedBy != "" {
		t.Fatalf("replacement provenance spawned_by=%q, inherited parent must not select attribution", replacement.Provenance.SpawnedBy)
	}
}

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

func TestPinnedReEnrollHintCarriesLabelOwnershipClaim(t *testing.T) {
	got := pinnedReEnroll(v2.SessionRecord{GUID: "guid-current", Label: "renamed-label", Role: "designer"}, "sid-current")
	if want := "HCOM_SESSION_ID=sid-current HERDER_GUID=guid-current HERDER_LABEL=renamed-label herder enroll"; got != want {
		t.Fatalf("pinnedReEnroll() = %q, want %q", got, want)
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
