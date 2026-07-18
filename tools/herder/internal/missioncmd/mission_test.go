package missioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestJoinAndLeaveRoundTrip(t *testing.T) {
	guid := setupMembershipTest(t, v2.StateSeated, "")
	t.Setenv("HERDER_GUID", guid)

	var stdout, stderr bytes.Buffer
	if code := RunJoin([]string{"alpha"}, &stdout, &stderr); code != 0 {
		t.Fatalf("join exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	joined := latestMembership(t, guid)
	if joined.Event != "mission_joined" || joined.Mission == nil || joined.Mission.Slug != "alpha" || joined.Mission.Source != "explicit" {
		t.Fatalf("joined row = %+v, mission=%+v", joined, joined.Mission)
	}
	assertMembershipCredential(t, joined)

	stdout.Reset()
	stderr.Reset()
	if code := RunLeave(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("leave exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	left := latestMembership(t, guid)
	if left.Event != "mission_left" || left.Mission != nil {
		t.Fatalf("left row = %+v, mission=%+v", left, left.Mission)
	}
	assertMembershipCredential(t, left)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(left.Raw, &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["mission"]; exists {
		t.Fatalf("mission_left wrote anti-membership field: %s", left.Raw)
	}
}

func TestJoinTargetsRunningAgentByLabel(t *testing.T) {
	guid := setupMembershipTest(t, v2.StateSeated, "")
	t.Setenv("HERDER_GUID", "")
	var stdout, stderr bytes.Buffer
	if code := RunJoin([]string{"alpha", "--target", "worker"}, &stdout, &stderr); code != 0 {
		t.Fatalf("join exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertMissionRow := latestMembership(t, guid)
	if assertMissionRow.Mission == nil || assertMissionRow.Mission.Slug != "alpha" {
		t.Fatalf("targeted join row = %+v", assertMissionRow)
	}
}

func TestJoinDoubleJoinAndEmptyLeaveAreIdempotent(t *testing.T) {
	guid := setupMembershipTest(t, v2.StateSeated, "alpha")
	t.Setenv("HERDER_GUID", guid)
	before := registryLineCount(t)

	var stdout, stderr bytes.Buffer
	if code := RunJoin([]string{"alpha"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "already joined") {
		t.Fatalf("same join exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if got := registryLineCount(t); got != before {
		t.Fatalf("same join appended registry row: before=%d after=%d", before, got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := RunJoin([]string{"beta"}, &stdout, &stderr); code != 1 {
		t.Fatalf("different join exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertTypedRefusal(t, stderr.String(), "already_joined")
	if got := registryLineCount(t); got != before {
		t.Fatalf("refused join appended registry row: before=%d after=%d", before, got)
	}

	if code := RunLeave(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("first leave exit = %d, stderr=%q", code, stderr.String())
	}
	afterLeave := registryLineCount(t)
	stdout.Reset()
	stderr.Reset()
	if code := RunLeave(nil, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "already active") {
		t.Fatalf("empty leave exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if got := registryLineCount(t); got != afterLeave {
		t.Fatalf("empty leave appended registry row: before=%d after=%d", afterLeave, got)
	}
}

func TestJoinTypedRefusals(t *testing.T) {
	t.Run("invalid slug", func(t *testing.T) {
		setupMembershipTest(t, v2.StateSeated, "")
		var stdout, stderr bytes.Buffer
		if code := RunJoin([]string{"Bad--slug"}, &stdout, &stderr); code != 1 {
			t.Fatalf("exit = %d", code)
		}
		assertTypedRefusal(t, stderr.String(), "invalid_mission_slug")
	})

	t.Run("unknown mission", func(t *testing.T) {
		setupMembershipTest(t, v2.StateSeated, "")
		var stdout, stderr bytes.Buffer
		if code := RunJoin([]string{"missing"}, &stdout, &stderr); code != 1 {
			t.Fatalf("exit = %d", code)
		}
		assertTypedRefusal(t, stderr.String(), "mission_not_found")
	})

	t.Run("no live row", func(t *testing.T) {
		guid := setupMembershipTest(t, v2.StateUnseated, "")
		var stdout, stderr bytes.Buffer
		if code := RunJoin([]string{"alpha", "--target", guid}, &stdout, &stderr); code != 1 {
			t.Fatalf("exit = %d", code)
		}
		assertTypedRefusal(t, stderr.String(), "session_not_live")
	})

	t.Run("unknown target", func(t *testing.T) {
		setupMembershipTest(t, v2.StateSeated, "")
		var stdout, stderr bytes.Buffer
		if code := RunJoin([]string{"alpha", "--target", "absent"}, &stdout, &stderr); code != 1 {
			t.Fatalf("exit = %d", code)
		}
		assertTypedRefusal(t, stderr.String(), "session_not_found")
	})

	t.Run("missing caller identity", func(t *testing.T) {
		setupMembershipTest(t, v2.StateSeated, "")
		t.Setenv("HERDER_GUID", "")
		var stdout, stderr bytes.Buffer
		if code := RunJoin([]string{"alpha"}, &stdout, &stderr); code != 1 {
			t.Fatalf("exit = %d", code)
		}
		assertTypedRefusal(t, stderr.String(), "caller_identity_missing")
	})
}

func setupMembershipTest(t *testing.T, state, missionSlug string) string {
	t.Helper()
	home := t.TempDir()
	stateDir := filepath.Join(home, "state")
	missionsRepo := filepath.Join(home, "missions-repo")
	for _, slug := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(missionsRepo, "missions", slug), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("HERDER_STATE_DIR", stateDir)
	t.Setenv("MISSIONS_REPO", missionsRepo)
	t.Setenv("HERDER_GUID", "")
	guid := "guid-membership"
	rec := v2.SessionRecord{
		GUID: guid, Event: "registered", State: state, Label: "worker", Role: "worker", Tool: "codex",
	}
	if state == v2.StateSeated {
		rec.Seat = &v2.Seat{Kind: "herdr", TerminalID: "term_worker", PaneID: "pane_worker", CredentialGeneration: "generation-membership"}
	}
	if missionSlug != "" {
		rec.Mission = &v2.Mission{Slug: missionSlug, Source: "explicit"}
	}
	outcomes, err := registry.UpdateLocked(registry.DefaultPath(), func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{rec}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := registry.SingleOutcome(outcomes); err != nil || outcome.Err() != nil {
		t.Fatalf("seed outcome=%+v err=%v", outcome, err)
	}
	return guid
}

func assertMembershipCredential(t *testing.T, row v2.SessionRecord) {
	t.Helper()
	if row.Seat == nil || row.Seat.CredentialGeneration != "generation-membership" {
		t.Fatalf("membership rewrite stripped credential generation: %+v", row.Seat)
	}
}

func latestMembership(t *testing.T, guid string) v2.SessionRecord {
	t.Helper()
	projection, err := v2.LoadFile(registry.DefaultPath(), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.V2ByGUID(projection, guid)
	if rec == nil {
		t.Fatalf("missing guid %s", guid)
	}
	return *rec
}

func registryLineCount(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(registry.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimSpace(string(data)), "\n"))
}

func assertTypedRefusal(t *testing.T, output, cause string) {
	t.Helper()
	if !strings.Contains(output, "["+cause+"]") || !strings.Contains(output, "remedy:") {
		t.Fatalf("refusal %q lacks cause %q and remedy", output, cause)
	}
}
