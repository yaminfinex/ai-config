package enrollcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
)

// writeResumeWedgeBins installs fake herdr/hcom binaries modelling a resumed
// session: the live pane is p_self, but the single joined bus row's launch
// context still points at the dead launch epoch's pane, and the ambient
// HCOM_SESSION_ID env is stale. Only explicit --session-id/--hcom-name can
// prove the live row.
func writeResumeWedgeBins(t *testing.T, joinedRow string) string {
	t.Helper()
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
  printf '%s\n' '` + joinedRow + `'
  exit 0
fi
exit 64
`
	for name, body := range map[string]string{"herdr": herdr, "hcom": hcom} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

// stubLaunchContextRepair neutralises the downstream launch-context concern so
// these tests isolate the flag -> verified-bus behaviour. The wedge's stale
// launch context is a separate, already-shipped repair (commit 2dd6659).
func engineWithStubbedRepair() seatcompletion.Engine {
	engine := seatcompletion.DefaultEngine()
	engine.RepairLaunchContext = func(_, _, _ string) hcomidentity.LaunchContextRepair {
		return hcomidentity.LaunchContextRepair{Status: "already-present"}
	}
	return engine
}

func TestEnrollExplicitEvidenceCorroboratesResumedWedge(t *testing.T) {
	const joined = `[{"name":"bus-live","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_dead"}}]`

	t.Run("ambient env alone cannot verify and the capable agent is refused", func(t *testing.T) {
		state := t.TempDir()
		registryPath := filepath.Join(state, "registry.jsonl")
		t.Setenv("PATH", writeResumeWedgeBins(t, joined)+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("HERDER_STATE_DIR", state)
		t.Setenv("HERDER_AGENT", "claude")
		t.Setenv("HERDR_ENV", "1")
		t.Setenv("HERDR_PANE_ID", "p_self")
		t.Setenv("HCOM_SESSION_ID", "sid-dead") // frozen at the dead launch epoch
		t.Setenv("HCOM_DIR", t.TempDir())

		var stdout, stderr strings.Builder
		if rc := runWithEngine([]string{"--label", "resumed"}, &stdout, &stderr, false, engineWithStubbedRepair()); rc != 1 {
			t.Fatalf("rc=%d, want 1 (this is the wedge); stderr=%q", rc, stderr.String())
		}
		if !strings.Contains(stderr.String(), "no joined bus row matches") {
			t.Fatalf("stderr=%q, want bus-missing refusal", stderr.String())
		}
		if _, err := os.Stat(registryPath); err == nil {
			projection, loadErr := v2.LoadFile(registryPath, v2.LoadOptions{})
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if len(projection.Sessions()) != 0 {
				t.Fatalf("refused enroll still appended rows: %+v", projection.Sessions())
			}
		}
	})

	t.Run("explicit evidence proves the live row and records it verified", func(t *testing.T) {
		state := t.TempDir()
		registryPath := filepath.Join(state, "registry.jsonl")
		t.Setenv("PATH", writeResumeWedgeBins(t, joined)+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("HERDER_STATE_DIR", state)
		t.Setenv("HERDER_AGENT", "claude")
		t.Setenv("HERDR_ENV", "1")
		t.Setenv("HERDR_PANE_ID", "p_self")
		t.Setenv("HCOM_SESSION_ID", "sid-dead")
		t.Setenv("HCOM_DIR", t.TempDir())

		var stdout, stderr strings.Builder
		rc := runWithEngine([]string{"--label", "resumed", "--session-id", "sid-live", "--hcom-name", "bus-live"}, &stdout, &stderr, false, engineWithStubbedRepair())
		if rc != 0 {
			t.Fatalf("rc=%d, want 0; stderr=%q", rc, stderr.String())
		}
		if strings.Contains(stderr.String(), "could not be verified") {
			t.Fatalf("stderr=%q, explicit evidence should have verified the bus", stderr.String())
		}
		name, verified := seatedBusIdentity(t, registryPath, "resumed")
		if name != "bus-live" || !verified {
			t.Fatalf("seated bus identity = %q verified=%v, want bus-live verified", name, verified)
		}
	})
}

func TestEnrollExplicitEvidenceFailsClosedOnUnjoinedName(t *testing.T) {
	const joined = `[{"name":"bus-live","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_dead"}}]`
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	t.Setenv("PATH", writeResumeWedgeBins(t, joined)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_self")
	t.Setenv("HCOM_SESSION_ID", "sid-dead")
	t.Setenv("HCOM_DIR", t.TempDir())

	var stdout, stderr strings.Builder
	rc := runWithEngine([]string{"--label", "resumed", "--hcom-name", "bus-ghost"}, &stdout, &stderr, false, engineWithStubbedRepair())
	if rc != 1 {
		t.Fatalf("rc=%d, want 1; stderr=%q", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "explicit evidence did not corroborate") {
		t.Fatalf("stderr=%q, want fail-closed corroboration error", stderr.String())
	}
	if _, err := os.Stat(registryPath); err == nil {
		projection, loadErr := v2.LoadFile(registryPath, v2.LoadOptions{})
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if len(projection.Sessions()) != 0 {
			t.Fatalf("fail-closed enroll appended rows: %+v", projection.Sessions())
		}
	}
}

func TestParseArgsRejectsMalformedEvidenceTokens(t *testing.T) {
	for _, flag := range []string{"--session-id", "--hcom-name"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr strings.Builder
			if _, code := parseArgs([]string{flag, "has space"}, &stdout, &stderr); code != 1 {
				t.Fatalf("code=%d, want 1 for %s with whitespace", code, flag)
			}
			if !strings.Contains(stderr.String(), flag+" must be one nonempty token") {
				t.Fatalf("stderr=%q, want token validation error for %s", stderr.String(), flag)
			}
		})
	}
}

func seatedBusIdentity(t *testing.T, registryPath, label string) (string, bool) {
	t.Helper()
	projection, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range projection.Sessions() {
		if session.State == v2.StateSeated && session.Label == label && session.Seat != nil {
			verified := session.Seat.HcomVerified != nil && *session.Seat.HcomVerified
			return session.Seat.HcomName, verified
		}
	}
	t.Fatalf("no seated session labelled %q in registry", label)
	return "", false
}
