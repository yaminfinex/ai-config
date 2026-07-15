package send

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
)

func TestVerifyStoredSenderAcceptsPaneProofWhenSessionIDIsStale(t *testing.T) {
	binDir := t.TempDir()
	stub := `#!/bin/sh
printf '%s\n' '[{"name":"live-self","session_id":"current-session","joined":true,"launch_context":{"pane_id":"pane-self"}}]'
`
	if err := os.WriteFile(filepath.Join(binDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := VerifyStoredSender("live-self", t.TempDir(), hcomidentity.Evidence{
		SessionID: "stale-session",
		PaneIDs:   []string{"pane-self", "pane-canonical"},
	})
	if err != nil || got != "live-self" {
		t.Fatalf("VerifyStoredSender = (%q, %v), want pane-proven live-self", got, err)
	}
}

func TestVerifyStoredSenderRefusalCarriesCauseAndRemedy(t *testing.T) {
	_, err := VerifyStoredSender("", t.TempDir(), hcomidentity.Evidence{})
	var refusal *SenderIdentityRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %T %v, want SenderIdentityRefusal", err, err)
	}
	if refusal.Cause == "" || refusal.Remedy == "" {
		t.Fatalf("refusal = %+v, want typed cause and remedy", refusal)
	}
}

func TestCallerCoordinatesKeepTerminalOutOfBusPaneEvidence(t *testing.T) {
	binDir := t.TempDir()
	stub := `#!/bin/sh
printf '%s\n' '{"result":{"pane":{"pane_id":"pane-canonical","terminal_id":"terminal-coordinate"}}}'
`
	if err := os.WriteFile(filepath.Join(binDir, "herdr"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_PANE_ID", "pane-launch")

	paneIDs, registryKeys := currentCallerCoordinates()
	if want := []string{"pane-launch", "pane-canonical"}; !reflect.DeepEqual(paneIDs, want) {
		t.Fatalf("pane evidence = %v, want %v", paneIDs, want)
	}
	if want := []string{"pane-launch", "pane-canonical", "terminal-coordinate"}; !reflect.DeepEqual(registryKeys, want) {
		t.Fatalf("registry coordinates = %v, want %v", registryKeys, want)
	}
}

func TestVerifiedCallerSenderRecoversFromStaleUnseatedSessionViaPane(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "herdr"), []byte(`#!/bin/sh
printf '%s\n' '{"result":{"pane":{"pane_id":"pane-canonical","terminal_id":"terminal-coordinate"}}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hcom"), []byte(`#!/bin/sh
printf '%s\n' '[{"name":"live-self","session_id":"current-session","joined":true,"launch_context":{"pane_id":"pane-canonical"}}]'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDR_PANE_ID", "pane-launch")
	t.Setenv("HCOM_SESSION_ID", "stale-session")
	t.Setenv("HCOM_PROCESS_ID", "")

	oldGUID, currentGUID := "guid-predecessor", "guid-current"
	recs := []registry.Record{
		{GUID: &oldGUID, State: "unseated", HcomName: "stale-self", Provenance: &registry.Provenance{ToolSessionID: "stale-session"}},
		{GUID: &currentGUID, State: "seated", PaneID: "pane-canonical", TerminalID: "terminal-coordinate", HcomName: "live-self"},
	}
	got, err := verifiedCallerSender(recs, t.TempDir())
	if err != nil || got != "live-self" {
		t.Fatalf("verifiedCallerSender = (%q, %v), want pane-proven live-self", got, err)
	}
}
