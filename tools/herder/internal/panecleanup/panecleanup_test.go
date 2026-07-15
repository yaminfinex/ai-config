package panecleanup

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type response struct {
	want string
	out  []byte
	rc   int
	err  error
}

type scriptedClient struct {
	responses []response
	calls     []string
}

func (c *scriptedClient) Combined(args ...string) ([]byte, int, error) {
	call := strings.Join(args, " ")
	c.calls = append(c.calls, call)
	if len(c.responses) == 0 {
		return nil, 64, errors.New("unexpected call")
	}
	r := c.responses[0]
	c.responses = c.responses[1:]
	if r.want != "" && call != r.want {
		return nil, 64, fmt.Errorf("call = %q, want %q", call, r.want)
	}
	return r.out, r.rc, r.err
}

func assertScriptConsumed(t *testing.T, client *scriptedClient) {
	t.Helper()
	if len(client.responses) != 0 {
		t.Fatalf("%d scripted response(s) were not consumed; calls = %v", len(client.responses), client.calls)
	}
}

func TestClosePreservingFocusRestoresSurvivingPriorPaneAfterMovement(t *testing.T) {
	client := &scriptedClient{
		responses: []response{
			{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
			{want: "pane close p_target", out: []byte(`{"result":{"type":"closed"}}`)},
			{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_other","focused":true}]}}`)},
			{want: "pane get p_owner", out: []byte(`{"result":{"pane":{"pane_id":"p_owner"}}}`)},
			{want: "agent focus p_owner", out: []byte(`{"result":{"type":"focused"}}`)},
		},
	}
	_, rc, err := ClosePreservingFocus(client, "p_target")
	if err != nil || rc != 0 {
		t.Fatalf("ClosePreservingFocus() rc=%d err=%v, want success", rc, err)
	}
	assertScriptConsumed(t, client)
}

func TestClosePreservingFocusDoesNotRefocusWhenFocusStayedPut(t *testing.T) {
	client := &scriptedClient{
		responses: []response{
			{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
			{want: "pane close p_target", out: []byte(`{"result":{"type":"closed"}}`)},
			{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
		},
	}
	_, rc, err := ClosePreservingFocus(client, "p_target")
	if err != nil || rc != 0 {
		t.Fatalf("ClosePreservingFocus() rc=%d err=%v, want success", rc, err)
	}
	assertScriptConsumed(t, client)
}

func TestClosePreservingFocusDoesNotRefocusClosedPriorPane(t *testing.T) {
	client := &scriptedClient{
		responses: []response{
			{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_target","focused":true}]}}`)},
			{want: "pane close p_target", out: []byte(`{"result":{"type":"closed"}}`)},
		},
	}
	_, rc, err := ClosePreservingFocus(client, "p_target")
	if err != nil || rc != 0 {
		t.Fatalf("ClosePreservingFocus() rc=%d err=%v, want success", rc, err)
	}
	assertScriptConsumed(t, client)
}

func TestClosePreservingFocusDoesNotRefocusMissingPriorPane(t *testing.T) {
	client := &scriptedClient{
		responses: []response{
			{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
			{want: "pane close p_target", out: []byte(`{"result":{"type":"closed"}}`)},
			{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_other","focused":true}]}}`)},
			{want: "pane get p_owner", out: []byte(`{"error":{"code":"pane_not_found"}}`), rc: 4},
		},
	}
	_, rc, err := ClosePreservingFocus(client, "p_target")
	if err != nil || rc != 0 {
		t.Fatalf("ClosePreservingFocus() rc=%d err=%v, want success", rc, err)
	}
	assertScriptConsumed(t, client)
}

func TestAlreadyAbsentLookupShapesAreConfirmed(t *testing.T) {
	tests := []struct {
		name string
		out  string
		rc   int
	}{
		{name: "live error response", out: `{"error":{"code":"pane_not_found"}}`, rc: 1},
		{name: "empty pane response after error", out: `{"result":{}}`, rc: 4},
		{name: "empty pane response after success", out: `{"result":{"pane":{}}}`, rc: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &scriptedClient{responses: []response{{out: []byte(tt.out), rc: tt.rc}}}
			got := CloseConfirmed(client, "p_new", "term_new")
			if !got.Confirmed || got.Detail != "pane already absent before cleanup" {
				t.Fatalf("CloseConfirmed() = %+v, want confirmed prior absence", got)
			}
			if len(client.calls) != 1 {
				t.Fatalf("calls = %v, want lookup only", client.calls)
			}
		})
	}
}

func TestEmptyLookupBodyDoesNotClaimAbsence(t *testing.T) {
	client := &scriptedClient{responses: []response{{rc: 1}}}
	got := CloseConfirmed(client, "p_new", "term_new")
	if got.Confirmed || got.Detail != "pre-close pane lookup exited 1: " {
		t.Fatalf("CloseConfirmed() = %+v, want unconfirmed empty diagnostic", got)
	}
}

func TestCloseConfirmationAcceptsAbsentLookupShapes(t *testing.T) {
	tests := []response{
		{out: []byte(`{"error":{"code":"pane_not_found"}}`), rc: 1},
		{out: []byte(`{"result":{}}`), rc: 4},
		{out: []byte(`{"result":{"pane":{}}}`), rc: 0},
	}
	for _, after := range tests {
		client := &scriptedClient{responses: []response{
			{out: []byte(`{"result":{"pane":{"pane_id":"p_new","terminal_id":"term_new"}}}`)},
			{out: []byte(`{"result":{"panes":[]}}`)},
			{out: []byte(`{"result":{"type":"closed"}}`)},
			after,
		}}
		got := CloseConfirmed(client, "p_new", "term_new")
		if !got.Confirmed || got.Detail != "pane close confirmed" {
			t.Fatalf("CloseConfirmed() with after=%s rc=%d = %+v, want confirmed close", after.out, after.rc, got)
		}
	}
}

func TestCloseRefusesUnverifiableTerminalIdentity(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		live     string
		want     string
	}{
		{name: "empty expected identity", live: "term_new", want: "launched terminal identity is empty"},
		{name: "mismatched identity", expected: "term_old", live: "term_new", want: "terminal changed from term_old to term_new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &scriptedClient{responses: []response{{out: []byte(`{"result":{"pane":{"pane_id":"p_new","terminal_id":"` + tt.live + `"}}}`)}}}
			got := CloseConfirmed(client, "p_new", tt.expected)
			if got.Confirmed || !strings.Contains(got.Detail, tt.want) {
				t.Fatalf("CloseConfirmed() = %+v, want refusal containing %q", got, tt.want)
			}
			if len(client.calls) != 1 {
				t.Fatalf("calls = %v, want no close after identity refusal", client.calls)
			}
		})
	}
}
