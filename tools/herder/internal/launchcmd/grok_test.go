package launchcmd

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

func mockGrokBinary(t *testing.T, _ string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grok")
	body := "#!/bin/sh\nexit 0\n"
	writeExecutable(t, path, body)
	return path
}

func mockGrokCompatHookBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grok")
	body := `#!/bin/sh
grok_home="${GROK_HOME:-$HOME/.grok}"
session="$grok_home/sessions/%2Fisolation/$HERDER_GROK_SESSION_ID"
mkdir -p "$session"
if [ -f "$HOME/.claude/settings.json" ] && [ "${GROK_CLAUDE_HOOKS_ENABLED:-1}" != 0 ]; then
  printf '%s\n' '{"timestamp":"2026-01-01T00:00:00Z","method":"session/update","params":{"update":{"sessionUpdate":"hook_execution","runs":[{"name":"global/settings:session_start[0].hooks[0]"}]}}}' > "$session/updates.jsonl"
else
  : > "$session/updates.jsonl"
fi
`
	writeExecutable(t, path, body)
	return path
}

const realShapedGrokHookUpdate = `{"timestamp":"2026-01-01T00:00:00Z","method":"session/update","params":{"update":{"sessionUpdate":"hook_execution","runs":[{"name":"global/settings:session_start[0].hooks[0]"}]}}}`

func countGlobalSettingsHookExecutions(t *testing.T, data []byte) int {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	count := 0
	for scanner.Scan() {
		var envelope struct {
			Params struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
					Runs          []struct {
						Name string `json:"name"`
					} `json:"runs"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			t.Fatalf("decode Grok update %q: %v", scanner.Bytes(), err)
		}
		update := envelope.Params.Update
		if update.SessionUpdate != "hook_execution" {
			continue
		}
		for _, run := range update.Runs {
			if strings.HasPrefix(run.Name, "global/settings:") {
				count++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan Grok updates: %v", err)
	}
	return count
}

func TestCountGlobalSettingsHookExecutionsMatchesRealEnvelope(t *testing.T) {
	if got := countGlobalSettingsHookExecutions(t, []byte(realShapedGrokHookUpdate+"\n")); got != 1 {
		t.Fatalf("real-shaped hook update count = %d, want 1", got)
	}
}

func prepareTestGrok(t *testing.T, version string) (grokLaunchPlan, string) {
	t.Helper()
	root := t.TempDir()
	hcom := filepath.Join(root, "hcom-real")
	writeExecutable(t, hcom, "#!/bin/sh\nexit 0\n")
	credential := randomCredential(t)
	t.Setenv("XAI_API_KEY", credential)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("GROK_HOME", filepath.Join(root, "ambient-grok-home"))
	t.Setenv("HERDER_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("HCOM_DIR", filepath.Join(root, "hcom"))
	seat, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", seat)
	grok := mockGrokBinary(t, version)
	t.Setenv("PATH", filepath.Dir(grok)+string(os.PathListSeparator)+os.Getenv("PATH"))
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

func TestResolveRealHcomPrefersRealThenFallsBackToArgv0DispatchShim(t *testing.T) {
	dispatchDir := t.TempDir()
	dispatcher := filepath.Join(dispatchDir, "dispatcher")
	writeExecutable(t, dispatcher, "#!/bin/sh\n[ \"${0##*/}\" = hcom ] || exit 97\nprintf 'dispatch:%s\\n' \"$PWD\"\n")
	shimDir := t.TempDir()
	dispatchLink := filepath.Join(shimDir, "dispatch-link")
	if err := os.Symlink(dispatcher, dispatchLink); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(shimDir, "hcom")
	if err := os.Symlink(dispatchLink, shim); err != nil {
		t.Fatal(err)
	}

	realDir := t.TempDir()
	realHcom := filepath.Join(realDir, "hcom")
	writeExecutable(t, realHcom, "#!/bin/sh\nprintf 'real:%s\\n' \"$PWD\"\n")
	t.Setenv("HERDER_REAL_HCOM", "")
	t.Setenv("HERDER_HOOK_HCOM", "")
	t.Setenv("PATH", strings.Join([]string{shimDir, realDir}, string(os.PathListSeparator)))

	if got := resolveRealHcom(); got != realHcom {
		t.Fatalf("resolved hcom = %q, want real binary %q after argv0 shim", got, realHcom)
	}
	nonProjectDir := t.TempDir()
	cmd := exec.Command(resolveRealHcom())
	cmd.Dir = nonProjectDir
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "real:"+nonProjectDir {
		t.Fatalf("resolved hcom from non-project cwd: err=%v output=%q", err, out)
	}

	// A shim-only PATH must still launch successfully: keep the first dispatch
	// shim as the last resort and preserve its invoked hcom name.
	t.Setenv("PATH", shimDir)
	if got := resolveRealHcom(); got != shim {
		t.Fatalf("shim-only PATH resolved hcom = %q, want invoked shim %q", got, shim)
	}
	cmd = exec.Command(resolveRealHcom())
	cmd.Dir = nonProjectDir
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "dispatch:"+nonProjectDir {
		t.Fatalf("shim-only PATH from non-project cwd: err=%v output=%q", err, out)
	}

	// The explicit escape hatch remains exact: preserving the invoked symlink
	// lets an argv0 dispatcher see the hcom tool name instead of its own target.
	t.Setenv("HERDER_REAL_HCOM", shim)
	if got := resolveRealHcom(); got != shim {
		t.Fatalf("explicit override = %q, want invoked path %q", got, shim)
	}
	cmd = exec.Command(resolveRealHcom())
	cmd.Dir = nonProjectDir
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "dispatch:"+nonProjectDir {
		t.Fatalf("explicit argv0 override from non-project cwd: err=%v output=%q", err, out)
	}
}

func TestGrokFamilyDefaultsOnAndChecksAuthBeforeState(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDER_GROK_SESSION_ID", "")
	if !IsHcomCapable("grok") {
		t.Fatal("Grok family is not available by default")
	}
	if _, err := prepareGrokLaunch(nil); err == nil || err.Error() != GrokAuthError() {
		t.Fatalf("default-on auth preflight error = %v", err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("auth preflight wrote state before refusing: %v", err)
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
	t.Setenv("XAI_API_KEY", randomCredential(t))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HCOM_DIR", filepath.Join(root, "hcom"))
	grok := mockGrokBinary(t, "")
	t.Setenv("PATH", filepath.Dir(grok)+string(os.PathListSeparator)+os.Getenv("PATH"))
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

func TestGrokAuthFailureIsSeatScopedAndNamesLoginProfileRemedy(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDER_GUID", "seat-neutral")
	_, err := prepareGrokLaunch(nil)
	if err == nil || err.Error() != GrokAuthError() {
		t.Fatalf("auth preflight error = %v", err)
	}
	for _, want := range []string{"XAI_API_KEY", "$HOME/.profile", "fresh pane"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("auth refusal %q missing %q", err, want)
		}
	}
	recordGrokLaunchFailure(err)
	if got := ReadGrokLaunchFailure(state, "seat-neutral"); got != GrokAuthError() {
		t.Fatalf("launch failure marker = %q", got)
	}
}

func TestT20ResolvedBinaryUsesNormalPathAfterHerderShimsWithoutExecution(t *testing.T) {
	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "grok")
	writeExecutable(t, shim, "#!/bin/sh\n# herder-path-shim\nexit 88\n")
	vendorDir := t.TempDir()
	vendor := filepath.Join(vendorDir, "grok")
	marker := filepath.Join(vendorDir, "executed")
	writeExecutable(t, vendor, "#!/bin/sh\nprintf ran > \""+marker+"\"\n")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+vendorDir)

	got, err := resolveGrokBinary()
	if err != nil || got != vendor {
		t.Fatalf("resolved vendor=%q err=%v, want %q", got, err, vendor)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("binary resolution executed vendor: %v", err)
	}
}

func TestGrokCheckReportsPathWithoutExecutingVendorOrTouchingLiveHome(t *testing.T) {
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
	t.Setenv("XAI_API_KEY", randomCredential(t))
	t.Setenv("HOME", liveHome)
	t.Setenv("GROK_HOME", liveGrok)
	t.Setenv("HERDER_STATE_DIR", liveState)
	vendorDir := t.TempDir()
	vendor := filepath.Join(vendorDir, "grok")
	executed := filepath.Join(vendorDir, "executed")
	writeExecutable(t, vendor, "#!/bin/sh\nprintf ran > \""+executed+"\"\n")
	t.Setenv("PATH", vendorDir)

	var stdout, stderr bytes.Buffer
	rc := RunGrokCheck(nil, &stdout, &stderr)
	if rc != 0 || stderr.Len() != 0 {
		t.Fatalf("check rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != "path="+vendor+"\n" {
		t.Fatalf("check output = %q", got)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "untouched\n" {
		t.Fatalf("live Grok home changed: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(stateSentinel); err != nil || string(data) != "untouched\n" {
		t.Fatalf("live herder state changed: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(executed); !os.IsNotExist(err) {
		t.Fatalf("doctor check executed vendor: %v", err)
	}
}

func TestT20LaunchUsesDefaultHomeVendorUpdatesAndSeatBridgePlugin(t *testing.T) {
	plan, _ := prepareTestGrok(t, "0.2.93")
	joined := strings.Join(plan.Argv, "\n")
	for _, want := range []string{"--no-subagents", "--always-approve", "--model", grokDefaultModel, "--plugin-dir"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %q", want, plan.Argv)
		}
	}
	if strings.Contains(joined, "--no-auto-update") {
		t.Fatalf("launch suppressed vendor updates: %q", plan.Argv)
	}
	if want := filepath.Join(os.Getenv("HOME"), ".grok"); plan.GrokHome != want {
		t.Fatalf("Grok home=%q want default %q", plan.GrokHome, want)
	}
	pluginDir := plan.Argv[len(plan.Argv)-1]
	manifest, err := os.ReadFile(filepath.Join(pluginDir, ".grok-plugin", "plugin.json"))
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
	for _, want := range []string{`"name": "herder-hcom"`, `"command": "` + exe + `"`, `"args": ["grok", "mcp"]`} {
		if !strings.Contains(string(manifest), want) {
			t.Errorf("bridge plugin missing %q: %s", want, manifest)
		}
	}
	if _, err := os.Stat(filepath.Join(plan.GrokHome, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("launch wrote default-home config: %v", err)
	}
}

func TestDefaultHomeGrokSessionRecordsNoClaudeCompatHookExecutions(t *testing.T) {
	root := t.TempDir()
	ownerHome := filepath.Join(root, "owner-home")
	settings := filepath.Join(ownerHome, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"true"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	hcom := filepath.Join(root, "hcom-real")
	writeExecutable(t, hcom, "#!/bin/sh\nexit 0\n")
	seat, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", ownerHome)
	t.Setenv("GROK_HOME", filepath.Join(root, "ambient-managed-home"))
	t.Setenv("HERDER_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("HCOM_DIR", filepath.Join(root, "hcom"))
	t.Setenv("HERDER_GUID", seat)
	t.Setenv("HERDER_GROK_SESSION_ID", sid)
	grok := mockGrokCompatHookBinary(t)
	t.Setenv("PATH", filepath.Dir(grok)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDER_REAL_HCOM", hcom)
	t.Setenv("GROK_CLAUDE_HOOKS_ENABLED", "1")
	t.Setenv("XAI_API_KEY", randomCredential(t))

	plan, err := prepareGrokLaunch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if envValue(plan.Env, "GROK_HOME") != "" {
		t.Fatalf("default-home launch retained GROK_HOME=%q", envValue(plan.Env, "GROK_HOME"))
	}
	runSession := func(env []string) {
		t.Helper()
		cmd := exec.Command(plan.Binary, plan.Argv[1:]...)
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("managed mock session failed: %v output=%q", err, output)
		}
	}
	updates := filepath.Join(plan.GrokHome, "sessions", "%2Fisolation", sid, "updates.jsonl")

	// Prove the mock and matcher can observe the vendor's real hook event shape
	// before testing the managed launch's suppression branch.
	runSession(replaceLaunchEnv(plan.Env, map[string]string{"GROK_CLAUDE_HOOKS_ENABLED": "1"}))
	data, err := os.ReadFile(updates)
	if err != nil {
		t.Fatal(err)
	}
	if got := countGlobalSettingsHookExecutions(t, data); got == 0 {
		t.Fatalf("enabled Claude hook scanner recorded no real-shaped hook_execution event: %s", data)
	}

	runSession(plan.Env)
	data, err = os.ReadFile(updates)
	if err != nil {
		t.Fatal(err)
	}
	if got := countGlobalSettingsHookExecutions(t, data); got != 0 {
		t.Fatalf("managed session recorded %d hook_execution event(s): %s", got, data)
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
		"--no-auto-update", "--auto-update", "--disable-auto-update", "--plugin-dir", "--agents", "--agent", "--subagents",
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
	if _, err := prepareGrokLifecycleLaunch(nil, lifecycle); err == nil || !strings.Contains(err.Error(), "absent from the default Grok home") {
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

func TestGrokBridgePluginRewritesChangedExecutablePath(t *testing.T) {
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
	pluginDir := plan.Argv[len(plan.Argv)-1]
	manifest, err := os.ReadFile(filepath.Join(pluginDir, ".grok-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"command": "`+second+`"`) || strings.Contains(string(manifest), first) {
		t.Fatalf("bridge plugin did not move to second executable: %s", manifest)
	}
}

func TestGrokLaunchNeverRewritesOwnerConfig(t *testing.T) {
	plan, _ := prepareTestGrok(t, "0.2.93")
	configPath := filepath.Join(plan.GrokHome, "config.toml")
	if err := os.MkdirAll(plan.GrokHome, 0o700); err != nil {
		t.Fatal(err)
	}
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
	if string(config) != "[cli]\nauto_update = true\n" {
		t.Fatalf("owner config was rewritten: %s", config)
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

func TestT21ChildEnvironmentUsesDefaultHomeWithoutPersistingCredential(t *testing.T) {
	plan, credential := prepareTestGrok(t, "0.2.93")
	env := strings.Join(plan.Env, "\n")
	for _, want := range []string{"HOME=" + os.Getenv("HOME"), "GROK_CLAUDE_HOOKS_ENABLED=0", "HERDER_STATE_DIR=" + plan.StateDir, "HERDER_GROK_SEAT=" + plan.Seat, "HERDER_GROK_SESSION_ID=" + plan.SessionID, "HERDER_REAL_HCOM=" + plan.HcomBin} {
		if !strings.Contains(env, want) {
			t.Errorf("child environment missing %q", want)
		}
	}
	if strings.Contains(env, "GROK_HOME=") {
		t.Errorf("child environment retained a home override")
	}
	argv := strings.Join(plan.Argv, "\n")
	pluginDir := plan.Argv[len(plan.Argv)-1]
	manifest, err := os.ReadFile(filepath.Join(pluginDir, ".grok-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(argv, credential) || strings.Contains(string(manifest), credential) {
		t.Fatal("inherited credential persisted outside the process environment")
	}
}

func TestT21ProcEnvironmentUsesDefaultHomeBeforeAnyModelPrompt(t *testing.T) {
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
		if err == nil && strings.Contains(string(data), "GROK_CLAUDE_HOOKS_ENABLED=0\x00") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	env := strings.ReplaceAll(string(data), "\x00", "\n")
	for _, want := range []string{"HOME=" + os.Getenv("HOME"), "GROK_CLAUDE_HOOKS_ENABLED=0", "HCOM_DIR=" + plan.HcomDir, "HERDER_STATE_DIR=" + plan.StateDir} {
		if !strings.Contains(env, want+"\n") {
			t.Errorf("/proc child environment missing %q", want)
		}
	}
	if strings.Contains(env, "GROK_HOME=") {
		t.Fatalf("/proc child environment retained a Grok home override: %q", env)
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
