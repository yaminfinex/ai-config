package send

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/occupant"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcred"
)

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

func TestSenderResolutionRefusalKeepsCohortMismatchOwnerFree(t *testing.T) {
	err := senderResolutionRefusal(occupant.Resolution{
		Observation: occupant.Observation{Status: occupant.Occupied, SID: "sid-other", Evidence: []occupant.Signal{occupant.SignalCohort}},
		Outcome:     occupant.Outcome{Status: occupant.PositiveMismatch},
	})
	assertSenderRefusalContains(t, err, "cohort-class", "not authoritative enough to name an owner", "unset it and retry")
}

func TestSenderResolutionRefusalMakesAmbiguityActionable(t *testing.T) {
	err := senderResolutionRefusal(occupant.Resolution{Outcome: occupant.Outcome{Status: occupant.OutcomeAmbiguous}})
	assertSenderRefusalContains(t, err, "ambiguous", "herder resume", "re-reports")
}

func TestCutoverNeverSelectsCallerFromAmbientEnvironment(t *testing.T) {
	stateDir := t.TempDir()
	registryPath := filepath.Join(stateDir, "registry.jsonl")
	if err := seatcred.EnableCutover(registryPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_STATE_DIR", stateDir)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDER_GUID", "poison-guid")
	t.Setenv("HCOM_SESSION_ID", "poison-session")
	t.Setenv("HERDR_PANE_ID", "poison-pane")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"target", "payload"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run exit=%d stderr=%q", code, stderr.String())
	}
	assertTextContains(t, stderr.String(), "--credential-file is required", "hints, not authority")
}

func TestInvalidPresentCutoverMarkerFailsClosed(t *testing.T) {
	stateDir := t.TempDir()
	registryPath := filepath.Join(stateDir, "registry.jsonl")
	if err := seatcred.EnableCutover(registryPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(stateDir, "credentials", "cutover-v1"), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_STATE_DIR", stateDir)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDER_GUID", "guid-poison")
	t.Setenv("HCOM_SESSION_ID", "session-poison")
	t.Setenv("HERDR_PANE_ID", "pane-poison")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"target", "payload"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run exit=%d, want fail-closed marker refusal; stderr=%q", code, stderr.String())
	}
	assertTextContains(t, stderr.String(), "present but invalid", "time-bounded rollback")
}

func TestCutoverCredentialWorksWithIdentityEnvironmentScrubbed(t *testing.T) {
	stateDir := t.TempDir()
	busDir := t.TempDir()
	registryPath := filepath.Join(stateDir, "registry.jsonl")
	staged, err := seatcred.Stage(registryPath, "guid-caller")
	if err != nil {
		t.Fatal(err)
	}
	verified := true
	outcomes, err := registry.UpdateLocked(registryPath, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{
			{Kind: v2.KindSession, GUID: "guid-caller", Event: "seated", RecordedAt: "2026-07-18T00:00:00Z", State: v2.StateSeated, Label: "caller", Tool: "codex", Seat: &v2.Seat{Kind: "herdr", HcomName: "caller-bus", HcomVerified: &verified, Namespace: busDir, CredentialGeneration: staged.File.Generation}},
			{Kind: v2.KindSession, GUID: "guid-target", Event: "seated", RecordedAt: "2026-07-18T00:00:00Z", State: v2.StateSeated, Label: "target", Tool: "codex", Seat: &v2.Seat{Kind: "herdr", HcomName: "target-bus", HcomVerified: &verified, Namespace: busDir}},
		}, nil
	})
	if err != nil {
		staged.Abort()
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := staged.Close(registryPath, staged.File.Generation); err != nil {
		t.Fatal(err)
	}
	if err := seatcred.EnableCutover(registryPath); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	stub := `#!/bin/sh
if [ "$1 $2" = "list --json" ]; then
  printf '%s\n' '[{"name":"caller-bus","base_name":"caller","joined":true},{"name":"target-bus","base_name":"target","joined":true}]'
  exit 0
fi
case "$1" in list|send|events) exit 0;; esac
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDER_STATE_DIR", stateDir)
	t.Setenv("HERDR_ENV", "1")
	for _, key := range []string{"HERDER_GUID", "HERDR_PANE_ID", "HCOM_SESSION_ID", "HCOM_PROCESS_ID", "HCOM_INSTANCE_NAME"} {
		t.Setenv(key, "")
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--credential-file", staged.Path, "--timeout", "1", "target", "payload"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertTextContains(t, stderr.String(), "@target-bus", "verify=queued")
}

func assertSenderRefusalContains(t *testing.T, err error, fragments ...string) {
	t.Helper()
	var refusal *SenderIdentityRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %T %v, want SenderIdentityRefusal", err, err)
	}
	assertTextContains(t, refusal.Error(), fragments...)
}

func assertTextContains(t *testing.T, got string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(got, fragment) {
			t.Fatalf("text %q does not contain %q", got, fragment)
		}
	}
}

func stringPointer(value string) *string { return &value }

func writeRegistryRecords(t *testing.T, stateDir string, records ...registry.Record) {
	t.Helper()
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	for _, record := range records {
		row := map[string]any{
			"kind":  "session",
			"guid":  *record.GUID,
			"label": *record.Label,
			"state": record.State,
			"seat": map[string]any{
				"hcom_name": record.HcomName,
				"namespace": record.HcomDir,
			},
		}
		if err := enc.Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stateDir, "registry.jsonl"), data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
