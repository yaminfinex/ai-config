package reconcilecmd

import (
	"encoding/json"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
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
