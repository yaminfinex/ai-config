package enrollcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
	"ai-config/tools/herder/internal/seatcred"
)

func TestCutoverLockedBuildRefusesConcurrentAmbientReselection(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	if err := seatcred.EnableCutover(registryPath); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	herdr := `#!/bin/sh
if [ "$1 $2" = "pane get" ]; then
  printf '%s\n' '{"result":{"pane":{"pane_id":"p_self","terminal_id":"term_SELF","workspace_id":"ws_self","cwd":"/mock/cwd"}}}'
  exit 0
fi
exit 64
`
	hcom := `#!/bin/sh
if [ "$1 $2" = "list --json" ]; then
  printf '%s\n' '[{"name":"bus-live","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]'
  exit 0
fi
exit 64
`
	for name, body := range map[string]string{"herdr": herdr, "hcom": hcom} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_self")
	t.Setenv("HCOM_SESSION_ID", "sid-live")
	t.Setenv("HCOM_DIR", t.TempDir())

	engine := seatcompletion.DefaultEngine()
	baseUpdate := engine.UpdateRegistry
	injected := false
	engine.UpdateRegistry = func(path string, update registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
		if !injected {
			injected = true
			verified := true
			outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
				return []v2.SessionRecord{{
					Kind: v2.KindSession, GUID: "guid-concurrent", Event: "seated", State: v2.StateSeated, Label: "concurrent", Tool: "codex",
					Seat: &v2.Seat{Kind: "herdr", PaneID: "p_self", TerminalID: "term_SELF", HcomName: "bus-live", HcomVerified: &verified},
				}}, nil
			})
			if err != nil {
				return nil, err
			}
			for _, outcome := range outcomes {
				if err := outcome.Err(); err != nil {
					return nil, err
				}
			}
		}
		return baseUpdate(path, update)
	}

	var stdout, stderr strings.Builder
	if rc := runWithEngine([]string{"--label", "fresh"}, &stdout, &stderr, false, "", engine); rc != 1 {
		t.Fatalf("runWithEngine rc=%d, want locked refusal; stderr=%q", rc, stderr.String())
	}
	for _, want := range []string{"existing live seat guid-concurrent is legacy", "herder credential sweep", "herder credential path --guid guid-concurrent"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr=%q, want %q", stderr.String(), want)
		}
	}
	projection, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sessions := projection.Sessions()
	if len(sessions) != 1 || sessions[0].GUID != "guid-concurrent" {
		t.Fatalf("sessions=%+v, want only concurrent seat and no ambient re-selection append", sessions)
	}
}

func rec(terminalID, hcomName string) registry.Record {
	return registry.Record{TerminalID: terminalID, HcomName: hcomName}
}

func TestStaleSeatClaimPreservesRawV1Protection(t *testing.T) {
	seated := v2.SessionRecord{State: v2.StateSeated, Seat: &v2.Seat{PaneID: "p_current", TerminalID: "term_current"}}
	if got, ok := staleSeatClaim(seated); !ok || got.PaneID != "p_current" || got.TerminalID != "term_current" {
		t.Fatalf("seated claim = %+v, %v", got, ok)
	}
	if got, ok := staleSeatClaim(v2.SessionRecord{State: v2.StateUnseated}); ok {
		t.Fatalf("unseated v2 row claimed a seat: %+v", got)
	}
	legacy := v2.SessionRecord{
		LegacyV1: true,
		State:    v2.StateUnseated,
		Raw:      json.RawMessage(`{"status":"active","pane_id":"p_old","terminal_id":"term_old","hcom_name":"bus-old","hcom_dir":"/old-bus"}`),
	}
	if got, ok := staleSeatClaim(legacy); !ok || got.PaneID != "p_old" || got.TerminalID != "term_old" || got.HcomName != "bus-old" || got.HcomDir != "/old-bus" {
		t.Fatalf("legacy-v1 claim = %+v, %v", got, ok)
	}
	legacy.Raw = json.RawMessage(`{"status":"closed","pane_id":"p_old"}`)
	if got, ok := staleSeatClaim(legacy); ok {
		t.Fatalf("closed legacy-v1 row claimed a seat: %+v", got)
	}
}

func TestLabelOwnerErrorDistinguishesSeatedAndUnseatedHolders(t *testing.T) {
	active := labelOwnerError("stable", v2.SessionRecord{GUID: "guid-active", State: v2.StateSeated})
	if !strings.Contains(active.Error(), "already belongs to seated session guid-active") || strings.Contains(active.Error(), "herder adopt") {
		t.Fatalf("seated error = %q", active)
	}
	dead := labelOwnerError("stable", v2.SessionRecord{GUID: "guid-dead", State: v2.StateUnseated})
	for _, want := range []string{"state unseated", "dead/unseated", "herder adopt guid-dead", "herder retire guid-dead", "herder rename <target> stable"} {
		if !strings.Contains(dead.Error(), want) {
			t.Fatalf("dead error = %q, want %q", dead, want)
		}
	}
}

func TestVerifyExistingGUIDOwnerRequiresStoredBusAndEitherSessionOrFullSeatProof(t *testing.T) {
	for _, sidMatches := range []bool{false, true} {
		for _, terminalMatches := range []bool{false, true} {
			for _, storedBusMatches := range []bool{false, true} {
				for _, labelMatches := range []bool{false, true} {
					name := fmt.Sprintf("sid=%t/terminal=%t/bus=%t/label=%t", sidMatches, terminalMatches, storedBusMatches, labelMatches)
					t.Run(name, func(t *testing.T) {
						terminalID := "term-other"
						if terminalMatches {
							terminalID = "term-live"
						}
						liveName := "bus-other"
						if storedBusMatches {
							liveName = "bus-stored"
						}
						liveSID := "sid-new"
						if sidMatches {
							liveSID = "sid-recorded"
						}
						label := "different-label"
						if labelMatches {
							label = "stable-label"
						}

						current := &v2.SessionRecord{
							GUID:  "guid-existing",
							State: v2.StateSeated,
							Label: "stable-label",
							Seat: &v2.Seat{
								TerminalID: "term-live",
								HcomName:   "bus-stored",
							},
							SIDs: []v2.SID{{SID: "sid-recorded"}},
						}
						pane := herdrcli.Pane{TerminalID: terminalID}
						live := hcomidentity.Result{Name: liveName, SessionID: liveSID, Verified: true}

						err := verifyExistingGUIDOwner(current, pane, live, label)
						wantAccept := storedBusMatches && (sidMatches || (terminalMatches && labelMatches))
						if wantAccept && err != nil {
							t.Fatalf("full proof refused: %v", err)
						}
						if !wantAccept && err == nil {
							t.Fatal("incomplete ownership proof accepted")
						}
					})
				}
			}
		}
	}
}

func TestVerifyExistingGUIDOwnerTreatsUnverifiedMatchingSessionAsNoBusProof(t *testing.T) {
	current := &v2.SessionRecord{
		GUID:  "guid-existing",
		State: v2.StateSeated,
		Label: "stable-label",
		Seat:  &v2.Seat{TerminalID: "term-live", HcomName: "bus-stored"},
		SIDs:  []v2.SID{{SID: "sid-recorded"}},
	}
	live := hcomidentity.Result{Name: "bus-stored", SessionID: "sid-recorded", Verified: false}
	if err := verifyExistingGUIDOwner(current, herdrcli.Pane{TerminalID: "term-live"}, live, "stable-label"); err == nil {
		t.Fatal("unverified bus result accepted despite matching session id")
	}
}

func TestCreatorProvenanceDoesNotGrantChildOwnershipToCallerSID(t *testing.T) {
	t.Setenv("HCOM_SESSION_ID", "sid-creator")
	guid, label := "guid-child", "child-label"
	prov := registry.BuildProvenance("spawn", "guid-creator", "", "worker", t.TempDir(), "")
	child := registry.V2FromRecord(registry.Record{
		GUID:       &guid,
		Label:      &label,
		Role:       "worker",
		Agent:      "claude",
		Provenance: &prov,
	}, "registered", v2.StateUnseated, "2026-07-17T00:00:00Z")

	t.Run("caller SID is not projected onto child", func(t *testing.T) {
		if len(child.SIDs) != 0 || child.Continuity != "assumed" {
			t.Fatalf("child identity evidence = sids %+v, continuity %q; want no SIDs and assumed continuity", child.SIDs, child.Continuity)
		}
	})

	t.Run("caller SID cannot prove child ownership", func(t *testing.T) {
		live := hcomidentity.Result{Name: "creator-bus", SessionID: "sid-creator", Verified: true}
		if err := verifyExistingGUIDOwner(&child, herdrcli.Pane{TerminalID: "term-creator"}, live, "creator-label"); err == nil {
			t.Fatal("creator SID alone proved ownership of the child row")
		}
	})

	t.Run("self flow keeps explicit current SID", func(t *testing.T) {
		selfGUID, selfLabel := "guid-self", "self-label"
		selfProv := registry.BuildProvenance("enroll", "", "sid-creator", "worker", t.TempDir(), "")
		self := registry.V2FromRecord(registry.Record{
			GUID:       &selfGUID,
			Label:      &selfLabel,
			Role:       "worker",
			Agent:      "claude",
			Provenance: &selfProv,
		}, "registered", v2.StateUnseated, "2026-07-17T00:00:00Z")
		if len(self.SIDs) != 1 || self.SIDs[0].SID != "sid-creator" || self.SIDs[0].Source != "harvest" || self.Continuity != "confirmed" {
			t.Fatalf("self identity evidence = sids %+v, continuity %q; want explicit current SID and confirmed continuity", self.SIDs, self.Continuity)
		}
		live := hcomidentity.Result{Name: "self-bus", SessionID: "sid-creator", Verified: true}
		if err := verifyExistingGUIDOwner(&self, herdrcli.Pane{TerminalID: "term-other"}, live, "other-label"); err != nil {
			t.Fatalf("explicit self SID did not prove self ownership: %v", err)
		}
	})
}

func TestVerifyExistingGUIDOwnerBootstrapsAbsentStoredBusName(t *testing.T) {
	tests := []struct {
		name        string
		recordedSID string
		liveSID     string
		terminalID  string
		label       string
		wantAccept  bool
	}{
		{
			name:        "matching session alone",
			recordedSID: "sid-recorded",
			liveSID:     "sid-recorded",
			terminalID:  "term-other",
			label:       "different-label",
			wantAccept:  true,
		},
		{
			name:       "matching terminal and label without recorded session",
			liveSID:    "sid-live",
			terminalID: "term-live",
			label:      "stable-label",
			wantAccept: true,
		},
		{
			name:        "neither session nor full seat proof",
			recordedSID: "sid-recorded",
			liveSID:     "sid-other",
			terminalID:  "term-other",
			label:       "different-label",
			wantAccept:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := &v2.SessionRecord{
				GUID:  "guid-existing",
				State: v2.StateSeated,
				Label: "stable-label",
				Seat:  &v2.Seat{TerminalID: "term-live"},
			}
			if tt.recordedSID != "" {
				current.SIDs = []v2.SID{{SID: tt.recordedSID}}
			}
			live := hcomidentity.Result{Name: "bus-live", SessionID: tt.liveSID, Verified: true}
			err := verifyExistingGUIDOwner(current, herdrcli.Pane{TerminalID: tt.terminalID}, live, tt.label)
			if tt.wantAccept && err != nil {
				t.Fatalf("bootstrap proof refused: %v", err)
			}
			if !tt.wantAccept && err == nil {
				t.Fatal("incomplete bootstrap proof accepted")
			}
		})
	}
}

func TestVerifyExistingGUIDOwnerStoredNameVerificationMutationMatrix(t *testing.T) {
	verified := true
	unverified := false
	tests := []struct {
		name         string
		verification *bool
		wantAccept   bool
	}{
		{name: "explicit unverified bootstraps", verification: &unverified, wantAccept: true},
		{name: "verified remains strict", verification: &verified, wantAccept: false},
		{name: "absent verification remains strict", verification: nil, wantAccept: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := &v2.SessionRecord{
				GUID:  "guid-existing",
				State: v2.StateSeated,
				Label: "stable-label",
				Seat: &v2.Seat{
					TerminalID:   "term-live",
					HcomName:     "bus-stored",
					HcomVerified: tt.verification,
				},
				SIDs: []v2.SID{{SID: "sid-recorded"}},
			}
			live := hcomidentity.Result{Name: "bus-different", SessionID: "sid-recorded", Verified: true}
			err := verifyExistingGUIDOwner(current, herdrcli.Pane{TerminalID: "term-live"}, live, "stable-label")
			if tt.wantAccept && err != nil {
				t.Fatalf("bootstrap proof refused: %v", err)
			}
			if !tt.wantAccept && err == nil {
				t.Fatal("strict stored-name mismatch accepted")
			}
		})
	}
}

func TestVerifyExistingGUIDOwnerUnverifiedStoredNameStillNeedsOwnership(t *testing.T) {
	unverified := false
	current := &v2.SessionRecord{
		GUID:  "guid-existing",
		State: v2.StateSeated,
		Label: "stable-label",
		Seat: &v2.Seat{
			TerminalID:   "term-recorded",
			HcomName:     "bus-stored",
			HcomVerified: &unverified,
		},
		SIDs: []v2.SID{{SID: "sid-recorded"}},
	}
	live := hcomidentity.Result{Name: "bus-different", SessionID: "sid-different", Verified: true}
	if err := verifyExistingGUIDOwner(current, herdrcli.Pane{TerminalID: "term-different"}, live, "different-label"); err == nil {
		t.Fatal("unverified stored name bypassed session-or-seat ownership proof")
	}
}

func TestSelectMatchingLiveSeatUsesSIDOnlyToRefine(t *testing.T) {
	live := hcomidentity.Result{SessionID: "sid-live", Verified: true}
	missingSID := v2.SessionRecord{GUID: "guid-missing"}
	exactSID := v2.SessionRecord{GUID: "guid-exact", SIDs: []v2.SID{{SID: "sid-live"}}}

	selected, err := selectMatchingLiveSeat([]v2.SessionRecord{missingSID, exactSID}, live)
	if err != nil {
		t.Fatalf("unique exact SID refused: %v", err)
	}
	if selected.GUID != exactSID.GUID {
		t.Fatalf("selected guid = %q, want %q", selected.GUID, exactSID.GUID)
	}

	if _, err := selectMatchingLiveSeat([]v2.SessionRecord{exactSID, {
		GUID: "guid-exact-too",
		SIDs: []v2.SID{{SID: "sid-live"}},
	}}, live); err == nil {
		t.Fatal("multiple exact SID matches were not refused as ambiguous")
	}

	if _, err := selectMatchingLiveSeat([]v2.SessionRecord{{
		GUID: "guid-conflict",
		SIDs: []v2.SID{{SID: "sid-other"}},
	}}, live); err == nil {
		t.Fatal("conflicting SID was allowed to permit occupied-seat selection")
	}
}

// TestShouldRetirePriorRow pins TASK-035 P1-b: retire-on-reenroll must not
// close a row that could be a different, still-live session sharing a
// moved/reshuffled pane_id. terminal_id is the move-stable coordinate; a joined
// bus name is definitionally live.
func TestShouldRetirePriorRow(t *testing.T) {
	never := func(string, string) bool { return false }
	always := func(string, string) bool { return true }

	cases := []struct {
		name       string
		prior      registry.Record
		paneTermID string
		joined     func(string, string) bool
		want       bool
	}{
		{"same terminal, not joined -> retire", rec("term_A", "bus_a"), "term_A", never, true},
		{"different terminal both present -> keep (re-key guard)", rec("term_A", "bus_a"), "term_B", never, false},
		{"different terminal but joined -> keep", rec("term_A", "bus_a"), "term_B", always, false},
		{"same terminal but currently joined -> keep (live)", rec("term_A", "bus_a"), "term_A", always, false},
		{"prior has no terminal -> falls through to retire", rec("", "bus_a"), "term_B", never, true},
		{"enrolling pane has no terminal -> retire", rec("term_A", "bus_a"), "", never, true},
		{"bus-less prior, same terminal -> retire (probe skipped)", rec("term_A", ""), "term_A", always, true},
	}
	for _, c := range cases {
		if got := shouldRetirePriorRow(c.prior, c.paneTermID, c.joined); got != c.want {
			t.Errorf("%s: shouldRetirePriorRow = %v, want %v", c.name, got, c.want)
		}
	}
}
