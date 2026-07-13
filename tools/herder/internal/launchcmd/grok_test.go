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

	"ai-config/tools/herder/internal/registry"
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
	body := "#!/bin/sh\nif [ \"${XAI_API_KEY+x}\" = x ] || [ \"${OPENAI_API_KEY+x}\" = x ] || [ \"${ANTHROPIC_API_KEY+x}\" = x ]; then printf '%s\\n' 'credential-shaped probe env present' >&2; exit 91; fi\ncase \"$*\" in\n  *--version*) printf 'grok " + version + " (build)\\n' ;;\n  *--help*) printf '%s\\n' '--no-subagents --session-id --rules' ;;\n  *) exit 0 ;;\nesac\n"
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
	seat, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", seat)
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
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"auto_update = false", "hooks = false", `command = ` + strconv.Quote(exe), `args = ["grok", "mcp"]`} {
		if !strings.Contains(string(config), want) {
			t.Errorf("controlled config missing %q: %s", want, config)
		}
	}
}

func TestSpawnMintedGUIDDrivesRealLaunchBuilder(t *testing.T) {
	plan, _ := prepareTestGrok(t, "0.2.93")
	if !validGrokSeat(plan.Seat) {
		t.Fatalf("registry-minted seat rejected by launch builder: %q", plan.Seat)
	}
	if got := envValue(plan.Env, "HERDER_GROK_SEAT"); got != plan.Seat {
		t.Fatalf("spawn-shaped child seat = %q, want %q", got, plan.Seat)
	}
}

func TestGrokSeatGuardRejectsPathAndNUL(t *testing.T) {
	for _, seat := range []string{"", "bad/seat", `bad\seat`, "bad\x00seat"} {
		if validGrokSeat(seat) {
			t.Errorf("unsafe seat accepted: %q", seat)
		}
	}
}

func TestGrokLaunchLayerRefusesOwnedArgCollisions(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		safe string
	}{
		{name: "agents object", args: []string{"--agents", `{"evil":{}}`}},
		{name: "safe always approve", args: []string{"--always-approve"}, safe: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(grokActivationEnv, "1")
			t.Setenv("XAI_API_KEY", randomCredential(t))
			t.Setenv("HERDER_GROK_SAFE", tc.safe)
			if _, err := prepareGrokLaunch(tc.args); err == nil || !strings.Contains(err.Error(), "remove") {
				t.Fatalf("prepareGrokLaunch(%q) = %v, want launch-layer refusal", tc.args, err)
			}
		})
	}
}

func TestGrokLaunchLayerMirrorsFullOwnedArgRefusals(t *testing.T) {
	cases := []string{
		"--session-id", "--session-id=value", "-s", "--resume", "-r", "--fork-session",
		"--rules", "--permission-mode", "--bypassPermissions",
		"--no-auto-update", "--auto-update", "--disable-auto-update", "--agents", "--agent", "--subagents",
		"--no-subagents", "--no-no-subagents", "HOME=/tmp/elsewhere", "GROK_HOME=/tmp/elsewhere",
	}
	for _, arg := range cases {
		t.Run(strings.TrimLeft(strings.ReplaceAll(arg, "=", "_"), "-"), func(t *testing.T) {
			if err := validatePreparedGrokArgs([]string{arg}, false); err == nil || !strings.Contains(err.Error(), "remove") {
				t.Fatalf("validatePreparedGrokArgs(%q) = %v, want targeted refusal", arg, err)
			}
		})
	}
}

func TestGrokProbeStripsCredentialAndSuppressesChildStderr(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	credential := "probe-credential-must-not-escape"
	t.Setenv("XAI_API_KEY", credential)
	t.Setenv("OPENAI_API_KEY", credential)
	t.Setenv("ANTHROPIC_API_KEY", credential)
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv(grokActivationEnv, "1")
	t.Setenv("HERDER_GUID", "probe-seat")

	// The normal mock exits if the probe inherits XAI_API_KEY. A successful
	// gate therefore proves the probe environment omitted the key by name.
	path := mockGrokBinary(t, "0.2.93")
	t.Setenv("HERDER_GROK_BIN", path)
	if _, _, err := gateGrokBinary(state); err != nil {
		t.Fatalf("credential-free probe failed: %v", err)
	}

	sentinel := "XAI_API_KEY=env-shaped-sentinel"
	evil := filepath.Join(root, "grok-stderr")
	writeExecutable(t, evil, "#!/bin/sh\nprintf '%s\\n' '"+sentinel+"' >&2\nexit 37\n")
	t.Setenv("HERDER_GROK_BIN", evil)
	_, _, err := gateGrokBinary(state)
	if err == nil || strings.Contains(err.Error(), sentinel) || !strings.Contains(err.Error(), "code 37") || !strings.Contains(err.Error(), evil) {
		t.Fatalf("scrubbed probe error = %v", err)
	}
	recordGrokLaunchFailure(err)
	marker := ReadGrokLaunchFailure(state, "probe-seat")
	if strings.Contains(marker, sentinel) || !strings.Contains(marker, "code 37") {
		t.Fatalf("scrubbed launch marker = %q", marker)
	}
}

func TestGrokProbeEnvironmentContainsNoAPIKeys(t *testing.T) {
	env := withoutCredentialEnv([]string{
		"PATH=/bin",
		"XAI_API_KEY=one",
		"OPENAI_API_KEY=two",
		"ANTHROPIC_API_KEY=three",
		"SERVICE_TOKEN=four",
		"CLIENT_SECRET=five",
		"DB_PASSWORD=six",
		"CREDENTIAL_FILE=seven",
	})
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasSuffix(strings.ToUpper(key), "_API_KEY") {
			t.Fatalf("probe environment retained credential-shaped name %q", key)
		}
	}
	if got := strings.Join(env, "\n"); got != "PATH=/bin" {
		t.Fatalf("probe credential scrub retained unexpected entries: %q", got)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
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
