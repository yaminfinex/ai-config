package launchcmd

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
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
	root := t.TempDir()
	state := filepath.Join(root, "state")
	t.Setenv(grokActivationEnv, "")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDER_GROK_SESSION_ID", "")
	if GrokActivated() || IsHcomCapable("grok") {
		t.Fatal("Grok family activated without the explicit gate")
	}
	if !strings.Contains(GrokActivationError(), "HERDER_GROK_ACTIVATED=1") {
		t.Fatalf("activation error lacks remedy: %s", GrokActivationError())
	}
	var stdout, stderr bytes.Buffer
	if rc := Run([]string{"grok"}, &stdout, &stderr); rc == 0 {
		t.Fatal("inactive manual launch unexpectedly succeeded")
	}
	if os.Getenv("HERDER_GUID") != "" || os.Getenv("HERDER_GROK_SESSION_ID") != "" {
		t.Fatal("inactive manual launch minted identity before refusing")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("inactive manual launch wrote state before refusing: %v", err)
	}
}

func TestManualGrokLaunchReplacesUnregisteredAmbientIdentity(t *testing.T) {
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDER_GROK_SESSION_ID", "")
	manual, err := ensureManualGrokIdentity()
	if err != nil || !manual {
		t.Fatal(err)
	}
	seat := os.Getenv("HERDER_GUID")
	sid := os.Getenv("HERDER_GROK_SESSION_ID")
	if !validGrokSeat(seat) || !isUUIDv7(sid) {
		t.Fatalf("minted seat=%q sid=%q", seat, sid)
	}
	manual, err = ensureManualGrokIdentity()
	if err != nil || !manual {
		t.Fatal(err)
	}
	if os.Getenv("HERDER_GUID") == seat || os.Getenv("HERDER_GROK_SESSION_ID") == sid {
		t.Fatal("unregistered ambient identity was silently adopted")
	}
}

func TestManagedGrokPreassignmentIsPreservedBeforeRegistryBind(t *testing.T) {
	seat, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(grokPreassignedEnv, "1")
	t.Setenv("HERDER_GUID", seat)
	t.Setenv("HERDER_GROK_SESSION_ID", sid)
	manual, err := ensureManualGrokIdentity()
	if err != nil || manual {
		t.Fatalf("managed preassignment: manual=%v err=%v", manual, err)
	}
	if os.Getenv("HERDER_GUID") != seat || os.Getenv("HERDER_GROK_SESSION_ID") != sid {
		t.Fatal("managed identity changed before registry bind")
	}
}

func TestManualGrokLaunchRefusesForeignFamilyGUIDWithoutSeatState(t *testing.T) {
	state := t.TempDir()
	foreign, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	row := `{"guid":"` + foreign + `","short_guid":"` + registry.ShortGUID(foreign) + `","label":"claude-seat","agent":"claude","status":"active"}` + "\n"
	if err := os.WriteFile(filepath.Join(state, "registry.jsonl"), []byte(row), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(grokActivationEnv, "1")
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDER_GUID", foreign)
	t.Setenv("HERDER_GROK_SESSION_ID", "inherited-claude-session")

	var stdout, stderr bytes.Buffer
	if rc := Run([]string{"grok"}, &stdout, &stderr); rc == 0 {
		t.Fatal("foreign-family ambient GUID was adopted")
	}
	for _, want := range []string{"refused inherited HERDER_GUID", `tool "claude", not grok`, "unset HERDER_GUID"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("refusal %q missing %q", stderr.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(state, "grok", foreign)); !os.IsNotExist(err) {
		t.Fatalf("foreign GUID acquired Grok state: %v", err)
	}
}

func TestManualMintedIdentityUsesPreassignedPlanAndCollisionFence(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	hcom := filepath.Join(root, "hcom-real")
	writeExecutable(t, hcom, "#!/bin/sh\nexit 0\n")
	t.Setenv(grokActivationEnv, "1")
	t.Setenv("XAI_API_KEY", randomCredential(t))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HCOM_DIR", filepath.Join(root, "hcom"))
	t.Setenv("HERDER_GROK_BIN", mockGrokBinary(t, "0.2.93"))
	t.Setenv("HERDER_REAL_HCOM", hcom)
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDER_GROK_SESSION_ID", "")

	manual, err := ensureManualGrokIdentity()
	if err != nil || !manual {
		t.Fatal(err)
	}
	seat, sid := os.Getenv("HERDER_GUID"), os.Getenv("HERDER_GROK_SESSION_ID")
	plan, err := prepareGrokLaunch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Seat != seat || plan.SessionID != sid || plan.Mode != "launch" {
		t.Fatalf("manual plan identity drifted: seat=%q sid=%q mode=%q", plan.Seat, plan.SessionID, plan.Mode)
	}
	if _, err := os.Stat(filepath.Join(state, "registry.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("manual plan wrote a half-registered row before bind: %v", err)
	}

	sessionDir := filepath.Join(plan.GrokHome, "sessions", "%2Fmanual", sid)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareGrokLaunch(nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("manual minted sid bypassed collision fence: %v", err)
	}
}

func TestManualGrokWrapperRetiresAfterNormalExit(t *testing.T) {
	plan := grokLaunchPlan{Binary: "/bin/sh", Argv: []string{"grok", "-c", "exit 7"}}
	retired := 0
	rc := runManualGrokProcessWithSignals(plan, io.Discard, make(chan os.Signal), func(grokLaunchPlan) error {
		retired++
		return nil
	})
	if rc != 7 || retired != 1 {
		t.Fatalf("rc=%d retired=%d", rc, retired)
	}
}

func TestManualGrokWrapperSignalConvergesDetachedBridgeToRetired(t *testing.T) {
	plan := grokLaunchPlan{Binary: "/bin/sh", Argv: []string{"grok", "-c", "trap 'exit 143' TERM; while :; do sleep 1; done"}}
	signals := make(chan os.Signal, 1)
	retired := make(chan struct{}, 1)
	result := make(chan int, 1)
	go func() {
		result <- runManualGrokProcessWithSignals(plan, io.Discard, signals, func(grokLaunchPlan) error {
			retired <- struct{}{}
			return nil
		})
	}()
	time.Sleep(100 * time.Millisecond)
	signals <- syscall.SIGTERM
	select {
	case rc := <-result:
		if rc != 143 {
			t.Fatalf("signal exit rc=%d", rc)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manual wrapper did not converge after SIGTERM")
	}
	select {
	case <-retired:
	default:
		t.Fatal("manual wrapper left its detached bridge unretired")
	}
}

func TestManualGrokBridgeHardKillFenceUsesParentDeathRetirement(t *testing.T) {
	manual := grokBridgeProcessAttributes(true)
	if !manual.Setsid {
		t.Fatalf("manual bridge attrs = %+v", manual)
	}
	if got, want := grokBridgeHardKillFenced(), runtime.GOOS == "linux"; got != want {
		t.Fatalf("hard-kill fence=%v want=%v on %s", got, want, runtime.GOOS)
	}
	managed := grokBridgeProcessAttributes(false)
	if !managed.Setsid {
		t.Fatalf("managed bridge attrs = %+v", managed)
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

func TestGrokCheckUsesLaunchGateWithoutActivationOrLiveHome(t *testing.T) {
	root := t.TempDir()
	liveHome := filepath.Join(root, "live-home")
	liveGrok := filepath.Join(liveHome, ".grok")
	liveState := filepath.Join(root, "live-state")
	if err := os.MkdirAll(liveGrok, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(liveState, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(liveGrok, "sentinel")
	if err := os.WriteFile(sentinel, []byte("untouched\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateSentinel := filepath.Join(liveState, "sentinel")
	if err := os.WriteFile(stateSentinel, []byte("untouched\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(grokActivationEnv, "")
	t.Setenv("XAI_API_KEY", randomCredential(t))
	t.Setenv("HOME", liveHome)
	t.Setenv("GROK_HOME", liveGrok)
	t.Setenv("HERDER_STATE_DIR", liveState)
	t.Setenv("HERDER_GROK_BIN", mockGrokBinary(t, "0.2.93"))

	var stdout, stderr bytes.Buffer
	rc := RunGrokCheck(nil, &stdout, &stderr)
	if rc != 0 || stderr.Len() != 0 {
		t.Fatalf("check rc=%d stderr=%q", rc, stderr.String())
	}
	for _, want := range []string{"path=", "version=0.2.93"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("check output %q missing %q", stdout.String(), want)
		}
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "untouched\n" {
		t.Fatalf("live Grok home changed: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(stateSentinel); err != nil || string(data) != "untouched\n" {
		t.Fatalf("live herder state changed: data=%q err=%v", data, err)
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
		"--session-id", "--session-id=value", "-s", "--resume", "-r", "--continue", "-c", "--continue=1", "--fork-session",
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

func TestGrokLifecycleModesBuildOnlyHerderOwnedIdentityArgs(t *testing.T) {
	parent, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	child, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode       string
		target     string
		preassign  string
		wantSuffix []string
	}{
		{mode: "launch", preassign: child, wantSuffix: []string{"--session-id", child, "--rules", "doctrine", grokBootPrompt}},
		{mode: "resume", target: parent, preassign: parent, wantSuffix: []string{"--resume", parent, "--rules", "doctrine", grokBootPrompt}},
		{mode: "fork", target: parent, preassign: child, wantSuffix: []string{"--resume", parent, "--fork-session", "--session-id", child, "--rules", "doctrine", grokBootPrompt}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			lifecycle, err := BuildGrokLifecyclePlan(tc.mode, tc.target, tc.preassign)
			if err != nil {
				t.Fatal(err)
			}
			plan := grokLaunchPlan{Mode: lifecycle.Mode, SessionID: lifecycle.SessionID, ParentSID: lifecycle.ParentSID, Argv: []string{"grok", "--no-subagents"}}
			got := appendGrokLifecycleArgs(plan, "doctrine")
			want := append([]string{"grok", "--no-subagents"}, tc.wantSuffix...)
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("argv=%q, want %q", got, want)
			}
			for _, arg := range []string{"--continue", "-c", "--continue=1"} {
				if err := validatePreparedGrokArgs([]string{arg}, false); err == nil || !strings.Contains(err.Error(), "remove") {
					t.Fatalf("%s lifecycle passthrough %q was not refused at shared launch layer: %v", tc.mode, arg, err)
				}
			}
		})
	}

	for _, arg := range []string{"--resume", "--fork-session", "--session-id"} {
		if err := validatePreparedGrokArgs([]string{arg}, false); err == nil || !strings.Contains(err.Error(), "remove") {
			t.Fatalf("lifecycle passthrough %q was not refused at launch layer: %v", arg, err)
		}
	}
}

func TestGrokLifecycleParserCarriesResumeAndForkTargetsAsOwnedModeData(t *testing.T) {
	parent, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	child, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		sid  string
	}{
		{name: "resume", args: []string{"--resume", "grok", parent}, sid: parent},
		{name: "fork", args: []string{"--fork", "grok", parent, "--parent-session", parent}, sid: child},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(grokActivationEnv, "1")
			t.Setenv("HERDER_GROK_SESSION_ID", tc.sid)
			t.Setenv("XAI_API_KEY", "")
			var stdout, stderr bytes.Buffer
			if rc := Run(tc.args, &stdout, &stderr); rc == 0 || !strings.Contains(stderr.String(), GrokAuthError()) {
				t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
			}
		})
	}
}

func TestGrokForkPreassignmentChecksAllSessionDirectoriesAndPreservesParent(t *testing.T) {
	base, _ := prepareTestGrok(t, "0.2.93")
	parent, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	child, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	parentDir := filepath.Join(base.GrokHome, "sessions", "%2Fparent-cwd", parent)
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	parentHistory := filepath.Join(parentDir, "chat_history.jsonl")
	const sentinel = "parent-history-must-stay-byte-identical\n"
	if err := os.WriteFile(parentHistory, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := BuildGrokLifecyclePlan("fork", parent, child)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := prepareGrokLifecycleLaunch(nil, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SessionID != child || plan.ParentSID != parent || plan.Mode != "fork" {
		t.Fatalf("fork plan=%+v", plan)
	}
	if got, err := os.ReadFile(parentHistory); err != nil || string(got) != sentinel {
		t.Fatalf("parent history=%q err=%v", got, err)
	}
	childDir := filepath.Join(base.GrokHome, "sessions", "%2Fanother-cwd", child)
	if err := os.MkdirAll(childDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareGrokLifecycleLaunch(nil, lifecycle); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("fork collision error=%v", err)
	}
}

func TestGrokResumeRequiresRecordedSessionInControlledHome(t *testing.T) {
	base, _ := prepareTestGrok(t, "0.2.93")
	sid, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := BuildGrokLifecyclePlan("resume", sid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareGrokLifecycleLaunch(nil, lifecycle); err == nil || !strings.Contains(err.Error(), "absent from the controlled home") {
		t.Fatalf("missing resume error=%v", err)
	}
	if err := os.MkdirAll(filepath.Join(base.GrokHome, "sessions", "%2Fresume-cwd", sid), 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareGrokLifecycleLaunch(nil, lifecycle)
	if err != nil || plan.SessionID != sid || plan.Mode != "resume" {
		t.Fatalf("resume plan=%+v err=%v", plan, err)
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
	if err == nil || strings.Contains(err.Error(), sentinel) || !strings.Contains(err.Error(), "code 37") || !strings.Contains(err.Error(), evil) || !strings.Contains(err.Error(), "run it by hand") {
		t.Fatalf("scrubbed probe error = %v", err)
	}
	recordGrokLaunchFailure(err)
	marker := ReadGrokLaunchFailure(state, "probe-seat")
	if strings.Contains(marker, sentinel) || !strings.Contains(marker, "code 37") {
		t.Fatalf("scrubbed launch marker = %q", marker)
	}
}

func TestGrokProbeEnvironmentContainsNoAPIKeys(t *testing.T) {
	root := t.TempDir()
	env := grokProbeEnv([]string{
		"PATH=/bin",
		"HOME=/live/home",
		"GROK_HOME=/live/grok",
		"LANG=C.UTF-8",
		"LC_ALL=C",
		"TERM=xterm",
		"TMPDIR=/tmp",
		"XAI_API_KEY=one",
		"OPENAI_API_KEY=two",
		"ANTHROPIC_API_KEY=three",
		"SERVICE_TOKEN=four",
		"CLIENT_SECRET=five",
		"DB_PASSWORD=six",
		"CREDENTIAL_FILE=seven",
		"UNRELATED=value",
	}, root)
	allowed := map[string]string{
		"PATH": "/bin", "HOME": root, "GROK_HOME": filepath.Join(root, "grok-home"),
		"LANG": "C.UTF-8", "LC_ALL": "C", "TERM": "xterm", "TMPDIR": "/tmp",
	}
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		if want, ok := allowed[key]; !ok || value != want {
			t.Fatalf("probe environment retained non-allowlisted entry %q", key)
		}
		delete(allowed, key)
	}
	if len(allowed) != 0 {
		t.Fatalf("probe environment omitted allowlisted entries: %v", allowed)
	}
}

func TestSeedGrokHomeRewritesChangedExecutablePath(t *testing.T) {
	original := grokMCPExecutable
	t.Cleanup(func() { grokMCPExecutable = original })
	first := filepath.Join(t.TempDir(), "herder-a")
	second := filepath.Join(t.TempDir(), "herder-b")
	grokMCPExecutable = func() (string, error) { return first, nil }
	plan, _ := prepareTestGrok(t, "0.2.93")
	grokMCPExecutable = func() (string, error) { return second, nil }
	if _, err := prepareGrokLaunch(nil); err != nil {
		t.Fatalf("second launch path rewrite failed: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(plan.GrokHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `command = `+strconv.Quote(second)) || strings.Contains(string(config), first) {
		t.Fatalf("controlled config did not move to second executable: %s", config)
	}
}

func TestSeedGrokHomeReenforcesControlledConfigAfterTamper(t *testing.T) {
	plan, _ := prepareTestGrok(t, "0.2.93")
	configPath := filepath.Join(plan.GrokHome, "config.toml")
	if err := os.WriteFile(configPath, []byte("[cli]\nauto_update = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareGrokLaunch(nil); err != nil {
		t.Fatalf("relaunch after tamper failed: %v", err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "auto_update = true") || !strings.Contains(string(config), "auto_update = false") || !strings.Contains(string(config), "hooks = false") {
		t.Fatalf("controlled config was not re-enforced: %s", config)
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
