package reconcilecmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestUpdateRowMarksCarriedBusNameUnverified(t *testing.T) {
	raw := []byte(`{"guid":"guid-self","hcom_name":"old-name","terminal_id":"term-old","pane_id":"p-old"}`)
	out, err := updateRow(raw, result{TerminalID: "term-new", PaneID: "p-new", bus: hcomidentity.Result{Reason: "no live proof"}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		HcomName     string `json:"hcom_name"`
		HcomVerified *bool  `json:"hcom_verified"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.HcomName != "old-name" || got.HcomVerified == nil || *got.HcomVerified {
		t.Fatalf("updated row = %+v, want carried name explicitly unverified", got)
	}
}

func TestReconcileApplyCandidateCarriesCredentialGeneration(t *testing.T) {
	raw := []byte(`{"kind":"session","guid":"guid-reconcile","event":"seated","state":"seated","seat":{"kind":"herdr","terminal_id":"term-old","pane_id":"pane-old","credential_generation":"generation-reconcile"}}`)
	updated, err := updateRow(raw, result{TerminalID: "term-new", PaneID: "pane-new", busUnavailable: true})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := registry.SessionEventFromJSON(updated, "seated", v2.StateSeated)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Seat == nil || candidate.Seat.CredentialGeneration != "generation-reconcile" {
		t.Fatalf("reconcile candidate stripped credential generation: %+v", candidate.Seat)
	}
}

func TestOneShotAbsenceIsObservationGapNotGone(t *testing.T) {
	guid, label := "fixture-guid", "fixture-label"
	rec := registry.Record{GUID: &guid, Label: &label, TerminalID: "terminal-absent", PaneID: "pane-absent"}
	live := liveState{
		byTerm: map[string]*herdrcli.Agent{}, paneTerms: map[string]bool{}, panePanes: map[string]bool{},
	}
	got := reconcileOne(rec, map[string]string{}, live)
	if got.Outcome != "observation_gap" || !strings.Contains(got.Detail, "not positive death evidence") {
		t.Fatalf("reconcile result = %+v", got)
	}
}

func TestUnavailableRosterPreservesVerifiedBindingDuringCoordinateRefresh(t *testing.T) {
	verified := true
	rec := registry.Record{HcomDir: "/hcom", HcomName: "live-self", HcomVerified: &verified}
	res := reconcileBusIdentity(rec, result{
		Outcome: "re-confirm", Write: "pending", TerminalID: "term-new", PaneID: "pane-new",
	}, map[string]busRoster{"/hcom": {err: errors.New("roster unavailable")}})
	if !res.busUnavailable || res.Detail != "" {
		t.Fatalf("reconcile result = %+v, want unavailable roster without downgrade detail", res)
	}

	raw := []byte(`{"guid":"guid-self","hcom_name":"live-self","hcom_verified":true,"terminal_id":"term-old","pane_id":"pane-old"}`)
	out, err := updateRow(raw, res)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		HcomVerified *bool `json:"hcom_verified"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.HcomVerified == nil || !*got.HcomVerified {
		t.Fatalf("updated row hcom_verified = %v, want prior true preserved", got.HcomVerified)
	}
}

func TestUpdateRowReplacesBusNameWithVerifiedLiveIdentity(t *testing.T) {
	raw := []byte(`{"guid":"guid-self","hcom_name":"old-name","terminal_id":"term-old","pane_id":"p-old"}`)
	out, err := updateRow(raw, result{TerminalID: "term-new", PaneID: "p-new", bus: hcomidentity.Result{Name: "live-self", Verified: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		HcomName     string `json:"hcom_name"`
		HcomVerified *bool  `json:"hcom_verified"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.HcomName != "live-self" || got.HcomVerified == nil || !*got.HcomVerified {
		t.Fatalf("updated row = %+v, want verified live-self", got)
	}
}
