package launchcmd

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func randomCredential(t *testing.T) string {
	t.Helper()
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func mockGrokBinary(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grok-build")
	body := "#!/bin/sh\ncase \"$*\" in\n  *--version*) printf 'grok " + version + " (build)\\n' ;;\n  *--help*) printf '%s\\n' '--no-subagents --session-id --rules' ;;\n  *) exit 0 ;;\nesac\n"
	writeExecutable(t, path, body)
	return path
}

func prepareTestGrok(t *testing.T, version string) (grokLaunchPlan, string) {
	t.Helper()
	root := t.TempDir()
	hcom := filepath.Join(root, "hcom-real")
	writeExecutable(t, hcom, "#!/bin/sh\nexit 0\n")
	credential := randomCredential(t)
	t.Setenv(grokActivationEnv, "1")
	t.Setenv("XAI_API_KEY", credential)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("HERDER_GROK_CHILD_HOME", filepath.Join(root, "child-home"))
	t.Setenv("HERDER_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("HCOM_DIR", filepath.Join(root, "hcom"))
	t.Setenv("HERDER_GUID", "seat-neutral")
	t.Setenv("HERDER_GROK_BIN", mockGrokBinary(t, version))
	t.Setenv("HERDER_REAL_HCOM", hcom)
	sid, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GROK_SESSION_ID", sid)
	plan, err := prepareGrokLaunch(nil)
	if err != nil {
		t.Fatal(err)
	}
	return plan, credential
}

func TestGrokActivationGateDefaultsClosed(t *testing.T) {
	t.Setenv(grokActivationEnv, "")
	if GrokActivated() || IsHcomCapable("grok") {
		t.Fatal("Grok family activated without the explicit gate")
	}
	if !strings.Contains(GrokActivationError(), "HERDER_GROK_ACTIVATED=1") {
		t.Fatalf("activation error lacks remedy: %s", GrokActivationError())
	}
}

func TestGrokAuthFailureIsSeatScopedAndNamesServerEnvironmentRemedy(t *testing.T) {
	state := t.TempDir()
	t.Setenv(grokActivationEnv, "1")
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDER_GUID", "seat-neutral")
	_, err := prepareGrokLaunch(nil)
	if err == nil || err.Error() != GrokAuthError() {
		t.Fatalf("auth preflight error = %v", err)
	}
	recordGrokLaunchFailure(err)
	if got := ReadGrokLaunchFailure(state, "seat-neutral"); got != GrokAuthError() {
		t.Fatalf("launch failure marker = %q", got)
	}
}

func TestT20ResolvedBinaryVersionAndCapabilityGate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", filepath.Join(root, "state"))
	path := mockGrokBinary(t, "0.2.99")
	t.Setenv("HERDER_GROK_BIN", path)
	_, _, err := gateGrokBinary(filepath.Join(root, "state"))
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "0.2.99") || !strings.Contains(err.Error(), "0.2.93") {
		t.Fatalf("unsupported gate error = %v", err)
	}
	t.Setenv("HERDER_GROK_SUPPORTED_VERSIONS", "0.2.99")
	gotPath, gotVersion, err := gateGrokBinary(filepath.Join(root, "state"))
	if err != nil || gotPath != path || gotVersion != "0.2.99" {
		t.Fatalf("configured supported set: path=%q version=%q err=%v", gotPath, gotVersion, err)
	}
}

func TestT20LaunchArgvAndControlledHomePinBothUpdateSuppressors(t *testing.T) {
	plan, _ := prepareTestGrok(t, "0.2.93")
	joined := strings.Join(plan.Argv, "\n")
	for _, want := range []string{"--no-auto-update", "--no-subagents", "--always-approve", "--model", grokDefaultModel} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %q", want, plan.Argv)
		}
	}
	config, err := os.ReadFile(filepath.Join(plan.GrokHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"auto_update = false", "hooks = false", `command = "herder"`, `args = ["grok", "mcp"]`} {
		if !strings.Contains(string(config), want) {
			t.Errorf("controlled config missing %q: %s", want, config)
		}
	}
}

func TestT21ChildEnvironmentPinsIsolationWithoutPersistingCredential(t *testing.T) {
	plan, credential := prepareTestGrok(t, "0.2.93")
	env := strings.Join(plan.Env, "\n")
	for _, want := range []string{"HOME=" + filepath.Dir(plan.StateDir) + "/child-home", "GROK_HOME=" + plan.GrokHome, "HERDER_STATE_DIR=" + plan.StateDir, "HERDER_GROK_SEAT=" + plan.Seat, "HERDER_GROK_SESSION_ID=" + plan.SessionID, "HERDER_REAL_HCOM=" + plan.HcomBin} {
		if !strings.Contains(env, want) {
			t.Errorf("child environment missing %q", want)
		}
	}
	argv := strings.Join(plan.Argv, "\n")
	config, err := os.ReadFile(filepath.Join(plan.GrokHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(argv, credential) || strings.Contains(string(config), credential) {
		t.Fatal("inherited credential persisted outside the process environment")
	}
}

func TestT21ProcEnvironmentIsIsolatedBeforeAnyModelPrompt(t *testing.T) {
	plan, _ := prepareTestGrok(t, "0.2.93")
	cmd := exec.Command("sleep", "30")
	cmd.Env = plan.Env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	var data []byte
	var err error
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid), "environ"))
		if err == nil && strings.Contains(string(data), "GROK_HOME="+plan.GrokHome+"\x00") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	env := strings.ReplaceAll(string(data), "\x00", "\n")
	wantHome := os.Getenv("HERDER_GROK_CHILD_HOME")
	for _, want := range []string{"HOME=" + wantHome, "GROK_HOME=" + plan.GrokHome, "HCOM_DIR=" + plan.HcomDir, "HERDER_STATE_DIR=" + plan.StateDir} {
		if !strings.Contains(env, want+"\n") {
			t.Errorf("/proc child environment missing %q", want)
		}
	}
	if inherited := os.Getenv("HOME"); inherited != wantHome && strings.Contains(env, "HOME="+inherited+"\n") {
		t.Fatalf("/proc child environment leaked reset HOME %q", inherited)
	}
}

func TestT22PreassignedSessionIdentityIsUUIDv7AndNeverCWDKeyed(t *testing.T) {
	first, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if !isUUIDv7(first) || !isUUIDv7(second) || first == second {
		t.Fatalf("session ids are not independent UUIDv7 values: %q %q", first, second)
	}
	doctrine := grokDoctrine("seat-address", "seat-neutral", first)
	if !strings.Contains(doctrine, first) || !strings.Contains(doctrine, "only the first output line") {
		t.Fatalf("doctrine missing identity or send-output hygiene: %s", doctrine)
	}
}
