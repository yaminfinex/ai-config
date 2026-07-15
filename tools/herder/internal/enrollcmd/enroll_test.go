package enrollcmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

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
