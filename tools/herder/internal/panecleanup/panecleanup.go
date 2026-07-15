package panecleanup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"ai-config/tools/herder/internal/herdrcli"
)

// Client is the herdr command surface needed to close and verify a pane.
type Client interface {
	Combined(args ...string) ([]byte, int, error)
}

// Result reports whether teardown was proved and why.
type Result struct {
	Confirmed bool
	Detail    string
}

// ClosePreservingFocus closes paneID and restores the previously focused pane
// only when herdr moved focus and that pane is still addressable. Focus
// discovery and restoration are best-effort: they never replace the close
// command's result.
func ClosePreservingFocus(client Client, paneID string) ([]byte, int, error) {
	// A deliberate operator focus change during these API round trips is
	// indistinguishable from close-induced movement. Prefer restoring the
	// pre-close target so ordinary background closes never steal focus.
	prior := focusedPane(client)
	closeOut, closeRC, closeErr := client.Combined("pane", "close", paneID)
	if closeErr != nil || closeRC != 0 || prior == "" {
		return closeOut, closeRC, closeErr
	}
	if prior == paneID {
		return closeOut, closeRC, closeErr
	}

	current := focusedPane(client)
	if current == "" || current == prior {
		return closeOut, closeRC, closeErr
	}
	priorOut, priorRC, priorErr := client.Combined("pane", "get", prior)
	if priorErr != nil || priorRC != 0 || paneLookupAbsent(priorOut) {
		return closeOut, closeRC, closeErr
	}
	priorPane, err := herdrcli.ParsePaneGet(priorOut)
	if err != nil || priorPane.PaneID != prior {
		return closeOut, closeRC, closeErr
	}
	_, _, _ = client.Combined("agent", "focus", prior)
	return closeOut, closeRC, closeErr
}

func focusedPane(client Client) string {
	out, rc, err := client.Combined("pane", "list")
	if err != nil || rc != 0 {
		return ""
	}
	panes, err := herdrcli.ParsePaneList(out)
	if err != nil {
		return ""
	}
	for _, pane := range panes {
		if pane.Focused {
			return pane.PaneID
		}
	}
	return ""
}

// CloseConfirmed closes only the pane that still carries expectedTerminal,
// then proves the pane id is no longer addressable.
func CloseConfirmed(client Client, paneID, expectedTerminal string) Result {
	before, beforeRC, beforeErr := client.Combined("pane", "get", paneID)
	if beforeErr != nil {
		return Result{Detail: "pre-close pane lookup could not run: " + beforeErr.Error()}
	}
	if paneLookupAbsent(before) {
		return Result{Confirmed: true, Detail: "pane already absent before cleanup"}
	}
	if beforeRC != 0 {
		return Result{Detail: fmt.Sprintf("pre-close pane lookup exited %d: %s", beforeRC, compactMessage(before))}
	}
	pane, err := herdrcli.ParsePaneGet(before)
	if err != nil {
		return Result{Detail: "pre-close pane lookup was unreadable: " + compactMessage(before)}
	}
	if expectedTerminal == "" {
		return Result{Detail: fmt.Sprintf("refused to close %s: launched terminal identity is empty", paneID)}
	}
	if pane.TerminalID != expectedTerminal {
		return Result{Detail: fmt.Sprintf("refused to close %s: terminal changed from %s to %s", paneID, expectedTerminal, pane.TerminalID)}
	}

	closeOut, closeRC, closeErr := ClosePreservingFocus(client, paneID)
	if closeErr != nil {
		return Result{Detail: "pane close could not run: " + closeErr.Error()}
	}
	if closeRC != 0 {
		return Result{Detail: fmt.Sprintf("pane close exited %d: %s", closeRC, compactMessage(closeOut))}
	}

	after, afterRC, afterErr := client.Combined("pane", "get", paneID)
	if afterErr != nil {
		return Result{Detail: "post-close verification could not run: " + afterErr.Error()}
	}
	if paneLookupAbsent(after) {
		return Result{Confirmed: true, Detail: "pane close confirmed"}
	}
	if afterRC != 0 {
		return Result{Detail: fmt.Sprintf("post-close pane lookup exited %d without confirming absence: %s", afterRC, compactMessage(after))}
	}
	return Result{Detail: "pane close returned success but the pane is still addressable"}
}

func paneLookupAbsent(out []byte) bool {
	text := strings.ToLower(string(out))
	if strings.Contains(text, "pane_not_found") ||
		strings.Contains(text, "pane not found") ||
		strings.Contains(text, "no such pane") {
		return true
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil || len(envelope.Result) == 0 || bytes.Equal(envelope.Result, []byte("null")) {
		return false
	}
	var result struct {
		Pane herdrcli.Pane `json:"pane"`
	}
	return json.Unmarshal(envelope.Result, &result) == nil && result.Pane.PaneID == ""
}

func compactMessage(out []byte) string {
	return strings.Join(strings.Fields(string(out)), " ")
}
