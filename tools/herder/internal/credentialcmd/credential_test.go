package credentialcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcred"
)

func TestSweepIssuesLiveLegacyProcessSeatAndIsIdempotent(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, "registry.jsonl")
	seedSweepSeat(t, path, "")
	t.Setenv("HERDER_STATE_DIR", state)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"sweep"}, &stdout, &stderr); code != 0 {
		t.Fatalf("sweep code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "1/1 (100%)") || !strings.Contains(stdout.String(), "herder credential enable") || !strings.Contains(stderr.String(), "credential issued") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	cutover, err := seatcred.CutoverEnabled(path)
	if err != nil {
		t.Fatal(err)
	}
	if cutover {
		t.Fatal("100% sweep created the owner-gated cutover marker")
	}
	current, err := seatcred.CurrentPath(path, "guid-process")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seatcred.Authenticate(path, current); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"sweep"}, &stdout, &stderr); code != 0 || strings.Contains(stderr.String(), "credential issued") {
		t.Fatalf("repeat code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestEnableRefusesBelowCompleteCoverage(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, "registry.jsonl")
	seedSweepSeat(t, path, "")
	t.Setenv("HERDER_STATE_DIR", state)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"enable"}, &stdout, &stderr); code != 1 {
		t.Fatalf("enable code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "herder credential sweep") {
		t.Fatalf("enable refusal lacks sweep remedy: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	cutover, err := seatcred.CutoverEnabled(path)
	if err != nil {
		t.Fatal(err)
	}
	if cutover {
		t.Fatal("enable below 100% coverage created the cutover marker")
	}
}

func TestEnableCreatesMarkerAtCompleteCoverage(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, "registry.jsonl")
	seedSweepSeat(t, path, "")
	t.Setenv("HERDER_STATE_DIR", state)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"sweep"}, &stdout, &stderr); code != 0 {
		t.Fatalf("sweep code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"enable"}, &stdout, &stderr); code != 0 {
		t.Fatalf("enable code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	cutover, err := seatcred.CutoverEnabled(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cutover {
		t.Fatal("enable at 100% coverage did not create the cutover marker")
	}
}

func TestHelpDocumentsTwoStepCutover(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"herder credential sweep", "herder credential enable", "does not enable", "100% coverage"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestSweepTreatsMissingCurrentFileAsTokenLoss(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, "registry.jsonl")
	seedSweepSeat(t, path, "lost-generation")
	t.Setenv("HERDER_STATE_DIR", state)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"sweep"}, &stdout, &stderr); code != 1 {
		t.Fatalf("sweep code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "repair reissue-credential") || !strings.Contains(stderr.String(), "coverage is incomplete") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.V2ByGUID(projection, "guid-process").Seat.CredentialGeneration; got != "lost-generation" {
		t.Fatalf("generation=%q, want unchanged token-loss marker", got)
	}
}

func seedSweepSeat(t *testing.T, path, generation string) {
	t.Helper()
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind: v2.KindSession, GUID: "guid-process", Event: "registered", RecordedAt: "2026-07-18T00:00:00Z", State: v2.StateSeated,
			Label: "process", Tool: "bash", Seat: &v2.Seat{Kind: "process", PID: os.Getpid(), CredentialGeneration: generation},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}
