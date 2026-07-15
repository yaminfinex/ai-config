package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sesh/internal/setup"
)

// testChannel serves a release channel like the store does: latest/VERSION
// once, immutable version paths for assets. It records asset hits so tests
// can assert --check downloads nothing.
type testChannel struct {
	latest    string
	assets    map[string][]byte // "ver/name" → bytes
	assetHits int
	srv       *httptest.Server
}

func newTestChannel(t *testing.T, latest string) *testChannel {
	t.Helper()
	ch := &testChannel{latest: latest, assets: map[string][]byte{}}
	ch.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/releases/")
		if path == "latest/VERSION" {
			if ch.latest == "" {
				http.Error(w, "no release has been published yet", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprintln(w, ch.latest)
			return
		}
		if b, ok := ch.assets[path]; ok {
			if !strings.HasSuffix(path, "SHA256SUMS") {
				ch.assetHits++
			}
			_, _ = w.Write(b)
			return
		}
		http.Error(w, "release file not found", http.StatusNotFound)
	}))
	t.Cleanup(ch.srv.Close)
	return ch
}

func (ch *testChannel) publish(ver string, binary []byte) {
	sum := sha256.Sum256(binary)
	ch.assets[ver+"/sesh-linux-amd64"] = binary
	ch.assets[ver+"/SHA256SUMS"] = []byte(hex.EncodeToString(sum[:]) + "  sesh-linux-amd64\n")
	ch.latest = ver
}

type fakeRunner struct {
	calls   []string
	fail    map[string]error
	outputs map[string]string
}

func (f *fakeRunner) key(name string, args ...string) string {
	if filepath.IsAbs(name) {
		if resolved, err := filepath.EvalSymlinks(name); err == nil {
			name = resolved
		}
	}
	return strings.Join(append([]string{name}, args...), " ")
}

func (f *fakeRunner) Run(name string, args ...string) error {
	k := f.key(name, args...)
	f.calls = append(f.calls, k)
	if f.fail != nil {
		if err, ok := f.fail[k]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeRunner) Output(name string, args ...string) (string, error) {
	k := f.key(name, args...)
	f.calls = append(f.calls, k)
	if f.outputs != nil {
		if out, ok := f.outputs[k]; ok {
			return out, nil
		}
	}
	return "", fmt.Errorf("fake: no output for %s", k)
}

// installedNode lays down a served-service install: a target binary at the
// unit's pinned path plus the drop-in carrying the store URL.
func installedNode(t *testing.T, storeURL string) (home, target string) {
	t.Helper()
	home = t.TempDir()
	target = filepath.Join(home, ".local", "bin", "sesh")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(setup.UnitPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup.UnitPath(home), []byte(setup.RenderUnit(target)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(setup.DropinPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup.DropinPath(home), setup.RenderDropin(nil, storeURL), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, target
}

func baseOpts(home, target string, runner *fakeRunner, out *bytes.Buffer) Options {
	return Options{
		OS:      "linux",
		Arch:    "amd64",
		Home:    home,
		Exe:     target,
		Version: "v1.0.0",
		Runner:  runner,
		Out:     out,
	}
}

func verifiedRunner(pid, version string) *fakeRunner {
	return &fakeRunner{outputs: map[string]string{
		"systemctl --user show sesh-ship.service --property=MainPID --value": pid,
		"/proc/" + pid + "/exe version":                                      version,
	}}
}

func TestUpdateConvergesBinaryAndService(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home, target := installedNode(t, ch.srv.URL)
	runner := verifiedRunner("4242", "v1.1.0")
	var out bytes.Buffer

	if err := Run(baseOpts(home, target, runner, &out)); err != nil {
		t.Fatalf("update: %v\noutput:\n%s", err, out.String())
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new-binary" {
		t.Fatalf("target = %q", got)
	}
	prev, err := os.ReadFile(target + ".prev")
	if err != nil || string(prev) != "old-binary" {
		t.Fatalf("prev = %q err=%v (previous binary must be retained)", prev, err)
	}
	if !strings.Contains(out.String(), "v1.0.0 -> v1.1.0") {
		t.Fatalf("from -> to not printed:\n%s", out.String())
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "systemctl --user restart sesh-ship.service") {
		t.Fatalf("service not restarted:\n%s", joined)
	}
	if !strings.Contains(out.String(), "restarted and verified") {
		t.Fatalf("verification not reported:\n%s", out.String())
	}
	// The store URL came from the drop-in — no flag was passed.
}

func TestUpdateAlreadyUpToDate(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.0.0", []byte("same"))
	home, target := installedNode(t, ch.srv.URL)
	runner := &fakeRunner{}
	var out bytes.Buffer
	if err := Run(baseOpts(home, target, runner, &out)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already up to date: v1.0.0") {
		t.Fatalf("output:\n%s", out.String())
	}
	if len(runner.calls) != 0 {
		t.Fatalf("no-op update touched the system: %v", runner.calls)
	}
}

func TestCheckReportsWithoutDownloading(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home, target := installedNode(t, ch.srv.URL)
	var out bytes.Buffer
	opts := baseOpts(home, target, &fakeRunner{}, &out)
	opts.Check = true

	err := Run(opts)
	if !errors.Is(err, ErrUpdateAvailable) {
		t.Fatalf("want ErrUpdateAvailable, got %v", err)
	}
	if !strings.Contains(out.String(), "v1.0.0 -> v1.1.0") {
		t.Fatalf("check must print from -> to:\n%s", out.String())
	}
	if ch.assetHits != 0 {
		t.Fatalf("--check downloaded %d assets", ch.assetHits)
	}
	if got, _ := os.ReadFile(target); string(got) != "old-binary" {
		t.Fatal("--check replaced the binary")
	}

	// Up to date → nil (exit 0).
	ch.latest = "v1.0.0"
	if err := Run(opts); err != nil {
		t.Fatalf("check when current: %v", err)
	}
}

// TestMalformedLatestRefusedBeforeAnyFetch replays the first-live-publish
// field bug: a mangled publish served the literal bytes 'sesh-v0.1.0n' (no
// trailing newline) as latest — valid charset, nonexistent version. The
// updater must refuse it loudly at the VERSION read instead of building a
// 404 asset URL, for both update and --check.
func TestMalformedLatestRefusedBeforeAnyFetch(t *testing.T) {
	var otherRequests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/latest/VERSION" {
			_, _ = w.Write([]byte("sesh-v0.1.0n")) // the literal regression bytes, raw
			return
		}
		otherRequests = append(otherRequests, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	home, target := installedNode(t, srv.URL)
	var out bytes.Buffer
	opts := baseOpts(home, target, &fakeRunner{}, &out)

	for _, check := range []bool{false, true} {
		opts.Check = check
		err := Run(opts)
		if err == nil || !strings.Contains(err.Error(), `"sesh-v0.1.0n"`) ||
			!strings.Contains(err.Error(), "invalid version string") {
			t.Fatalf("check=%v: want a loud invalid-version error naming the bytes, got %v", check, err)
		}
	}
	if len(otherRequests) != 0 {
		t.Fatalf("malformed latest still led to fetches: %v", otherRequests)
	}
	if got, _ := os.ReadFile(target); string(got) != "old-binary" {
		t.Fatal("malformed latest replaced the binary")
	}
}

// The channel version shape is a cross-side contract with scripts/release.sh
// and install.sh; this pins the Go side of it.
func TestVersionShape(t *testing.T) {
	accept := []string{
		"sesh-v0.1.0", "sesh-v0.1.0-3-gab12cd3", "v1.1.0", "v99.0.1",
		"abcdef1", "f3a4c51deadbeef",
	}
	reject := []string{
		"sesh-v0.1.0n", "v1.1.0n", "sesh-v0.1.0-dirty", "latest",
		"sesh-v0.1.0 extra", "", "../v1.1.0", "vTEST-1",
		// arity is exactly major.minor.patch
		"v1", "v1.1", "sesh-v0.1", "sesh-v1.2.3.4",
	}
	for _, v := range accept {
		if !versionRE.MatchString(v) {
			t.Errorf("versionRE rejected valid version %q", v)
		}
	}
	for _, v := range reject {
		if versionRE.MatchString(v) {
			t.Errorf("versionRE accepted malformed version %q", v)
		}
	}
}

// TestLatestRollbackPropagatesAsVisibleDowngrade: equality-only semantics —
// an operator rewriting latest to an older version converges the fleet
// down, and the from -> to line makes it visible (design §6.2, AC#5).
func TestLatestRollbackPropagatesAsVisibleDowngrade(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v0.9.0", []byte("rollback-binary"))
	home, target := installedNode(t, ch.srv.URL)
	runner := verifiedRunner("77", "v0.9.0")
	var out bytes.Buffer

	if err := Run(baseOpts(home, target, runner, &out)); err != nil {
		t.Fatalf("downgrade: %v\n%s", err, out.String())
	}
	if got, _ := os.ReadFile(target); string(got) != "rollback-binary" {
		t.Fatal("downgrade did not converge to latest")
	}
	if !strings.Contains(out.String(), "v1.0.0 -> v0.9.0") {
		t.Fatalf("downgrade not visible as from -> to:\n%s", out.String())
	}
}

func TestChecksumMismatchLeavesInstallUntouched(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	ch.assets["v1.1.0/SHA256SUMS"] = []byte(strings.Repeat("0", 64) + "  sesh-linux-amd64\n")
	home, target := installedNode(t, ch.srv.URL)
	runner := &fakeRunner{}
	var out bytes.Buffer

	err := Run(baseOpts(home, target, runner, &out))
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum failure, got %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old-binary" {
		t.Fatal("checksum failure replaced the binary")
	}
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Fatal("checksum failure created a prev")
	}
	if stray, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".sesh-update-*.tmp")); len(stray) != 0 {
		t.Fatalf("temp files left behind: %v", stray)
	}
	if len(runner.calls) != 0 {
		t.Fatal("pre-replacement failure must not touch the service")
	}
}

// TestCrashBetweenPrevAndRenameNeverLosesTarget: the §6.4 ordering — at the
// injected crash point the target still exists (old bytes), prev exists, and
// the NEXT run cleans the stray temp and completes.
func TestCrashBetweenPrevAndRenameNeverLosesTarget(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home, target := installedNode(t, ch.srv.URL)
	var out bytes.Buffer

	testHookAfterPrevLink = func() error {
		// The downloaded temp must already be executable HERE — before the
		// rename that could make it the target — proving the mode was set
		// inside the download's durability chain (chmod before final fsync),
		// never patched on afterwards.
		stray, globErr := filepath.Glob(filepath.Join(filepath.Dir(target), ".sesh-update-*.tmp"))
		if globErr != nil || len(stray) != 1 {
			t.Errorf("expected exactly one temp at the crash point, got %v (%v)", stray, globErr)
		} else if st, statErr := os.Stat(stray[0]); statErr != nil || st.Mode().Perm() != 0o755 {
			t.Errorf("temp mode at crash point = %v (%v), want 0755 set before the final fsync", st.Mode().Perm(), statErr)
		}
		return fmt.Errorf("injected crash before rename")
	}
	err := Run(baseOpts(home, target, &fakeRunner{}, &out))
	testHookAfterPrevLink = nil
	if err == nil || !strings.Contains(err.Error(), "injected crash") {
		t.Fatalf("want injected crash, got %v", err)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || string(got) != "old-binary" {
		t.Fatalf("target missing or wrong at crash point: %q %v", got, readErr)
	}
	if prev, readErr := os.ReadFile(target + ".prev"); readErr != nil || string(prev) != "old-binary" {
		t.Fatalf("prev missing at crash point: %v", readErr)
	}

	// Simulate the not-yet-cleaned temp of a harder crash (the deferred
	// remove would not run on SIGKILL), then prove the next run recovers.
	if err := os.WriteFile(filepath.Join(filepath.Dir(target), ".sesh-update-stray.tmp"), []byte("junk"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := verifiedRunner("99", "v1.1.0")
	out.Reset()
	if err := Run(baseOpts(home, target, runner, &out)); err != nil {
		t.Fatalf("recovery run: %v\n%s", err, out.String())
	}
	if got, _ := os.ReadFile(target); string(got) != "new-binary" {
		t.Fatal("recovery run did not converge")
	}
	if stray, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".sesh-update-*.tmp")); len(stray) != 0 {
		t.Fatalf("stray temp not cleaned on next run: %v", stray)
	}
}

func TestRestartFailureReportedNeverClaimsRollback(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home, target := installedNode(t, ch.srv.URL)
	runner := &fakeRunner{fail: map[string]error{
		"systemctl --user restart sesh-ship.service": fmt.Errorf("unit failed"),
	}}
	var out bytes.Buffer

	err := Run(baseOpts(home, target, runner, &out))
	if err == nil || !strings.Contains(err.Error(), "restart failed") {
		t.Fatalf("want restart failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "binary updated to v1.1.0") {
		t.Fatalf("failure must state the on-disk truth: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new-binary" {
		t.Fatal("restart failure must keep the forward binary")
	}
}

// TestPostStartVerificationFailureSurfacesR23Verbatim: §6.6 — the new binary
// ran (may have migrated the registry), verification fails, and the R23
// refusal from the journal is surfaced verbatim; the forward binary stays.
func TestPostStartVerificationFailureSurfacesR23Verbatim(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home, target := installedNode(t, ch.srv.URL)
	r23 := `cursor registry /home/u/.local/state/sesh/cursors.json carries schema generation 7 but this sesh build only understands generation 6: this binary is older than the registry (likely cause: an outdated sesh build on this node). Remedy: run the newer sesh build that wrote the registry, or upgrade this installation and retry. The registry file has been left untouched.`
	runner := &fakeRunner{outputs: map[string]string{
		"systemctl --user show sesh-ship.service --property=MainPID --value": "0",
		"journalctl --user -u sesh-ship.service -n 50 --no-pager":            "Jul 13 sesh[1]: " + r23,
	}}
	var out bytes.Buffer
	oldInterval := verifyPollInterval
	verifyPollInterval = 0
	defer func() { verifyPollInterval = oldInterval }()

	err := Run(baseOpts(home, target, runner, &out))
	if err == nil || !strings.Contains(err.Error(), "failed-but-forward") {
		t.Fatalf("want failed-but-forward, got %v", err)
	}
	if !strings.Contains(err.Error(), "carries schema generation 7") ||
		!strings.Contains(err.Error(), "left untouched") {
		t.Fatalf("R23 not surfaced verbatim:\n%v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new-binary" {
		t.Fatal("failed-but-forward must keep the new binary in place")
	}
}

func TestRefusesWhenNotTheServicePinnedBinary(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home, target := installedNode(t, ch.srv.URL)
	other := filepath.Join(home, "elsewhere-sesh")
	if err := os.WriteFile(other, []byte("other"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts := baseOpts(home, target, &fakeRunner{}, &out)
	opts.Exe = other

	err := Run(opts)
	if err == nil || !strings.Contains(err.Error(), "not the service's pinned executable") {
		t.Fatalf("want pin refusal, got %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old-binary" {
		t.Fatal("pin refusal replaced the target")
	}
}

func TestAcceptsSameBinaryThroughSymlinkedParent(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))

	base := t.TempDir()
	realTemp := filepath.Join(base, "real")
	linkedTemp := filepath.Join(base, "linked")
	if err := os.Mkdir(realTemp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTemp, linkedTemp); err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp(linkedTemp, "home-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	target := filepath.Join(home, ".local", "bin", "sesh")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(setup.UnitPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup.UnitPath(home), []byte(setup.RenderUnit(target)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(setup.DropinPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup.DropinPath(home), setup.RenderDropin(nil, ch.srv.URL), 0o644); err != nil {
		t.Fatal(err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedTarget == target {
		t.Fatalf("fixture does not exercise distinct path spellings: %s", target)
	}

	var out bytes.Buffer
	if err := Run(baseOpts(home, target, verifiedRunner("4242", "v1.1.0"), &out)); err != nil {
		t.Fatalf("same binary through symlinked parent refused: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new-binary" {
		t.Fatal("same-binary update did not replace the target")
	}
}

func TestRefusesWhenRunningBinaryCannotBeResolved(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home, target := installedNode(t, ch.srv.URL)

	var out bytes.Buffer
	opts := baseOpts(home, target, &fakeRunner{}, &out)
	opts.Exe = filepath.Join(home, "missing-sesh")
	err := Run(opts)
	if err == nil || !strings.Contains(err.Error(), "cannot resolve the running path") {
		t.Fatalf("want closed-guard resolution error, got %v", err)
	}
	for _, want := range []string{opts.Exe, target, "re-run `sesh setup` to repin"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("resolution error %q does not contain %q", err, want)
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "old-binary" {
		t.Fatal("resolution failure replaced the target")
	}
}

func TestRefusesWithRemedyWhenPinnedBinaryIsDangling(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home, target := installedNode(t, ch.srv.URL)
	dangling := filepath.Join(home, "dangling-sesh")
	if err := os.Symlink(filepath.Join(home, "deleted-sesh"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup.UnitPath(home), []byte(setup.RenderUnit(dangling)), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := Run(baseOpts(home, target, &fakeRunner{}, &out))
	if err == nil || !strings.Contains(err.Error(), "cannot resolve the pinned path") {
		t.Fatalf("want closed-guard pinned-path error, got %v", err)
	}
	for _, want := range []string{target, dangling, "re-run `sesh setup` to repin"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("resolution error %q does not contain %q", err, want)
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "old-binary" {
		t.Fatal("resolution failure replaced the target")
	}
}

func TestPreSetupUseWithExplicitURLSkipsService(t *testing.T) {
	ch := newTestChannel(t, "")
	ch.publish("v1.1.0", []byte("new-binary"))
	home := t.TempDir() // no unit, no drop-in
	exe := filepath.Join(home, "sesh")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	var out bytes.Buffer
	opts := baseOpts(home, exe, runner, &out)
	opts.StoreURL = ch.srv.URL

	if err := Run(opts); err != nil {
		t.Fatalf("pre-setup update: %v\n%s", err, out.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "new-binary" {
		t.Fatal("binary not replaced")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("serviceless update touched the service: %v", runner.calls)
	}
	if !strings.Contains(out.String(), "no installed service") {
		t.Fatalf("serviceless mode must say so:\n%s", out.String())
	}
}

func TestNoInstalledConfigAndNoFlagRefuses(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(home, "sesh")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Run(baseOpts(home, exe, &fakeRunner{}, &out))
	if err == nil || !strings.Contains(err.Error(), "no store URL") {
		t.Fatalf("want no-store-URL refusal, got %v", err)
	}
}

func TestUnpublishedChannelReported(t *testing.T) {
	ch := newTestChannel(t, "") // 503 until published
	home, target := installedNode(t, ch.srv.URL)
	var out bytes.Buffer
	err := Run(baseOpts(home, target, &fakeRunner{}, &out))
	if err == nil || !strings.Contains(err.Error(), "not published a release") {
		t.Fatalf("want unpublished-channel error, got %v", err)
	}
}

func TestDarwinVerifyChecksOnDiskWithCaveat(t *testing.T) {
	ch := newTestChannel(t, "")
	sum := sha256.Sum256([]byte("new-binary"))
	ch.assets["v1.1.0/sesh-darwin-arm64"] = []byte("new-binary")
	ch.assets["v1.1.0/SHA256SUMS"] = []byte(hex.EncodeToString(sum[:]) + "  sesh-darwin-arm64\n")
	ch.latest = "v1.1.0"

	base := t.TempDir()
	realTemp := filepath.Join(base, "real")
	linkedTemp := filepath.Join(base, "linked")
	if err := os.Mkdir(realTemp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTemp, linkedTemp); err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp(linkedTemp, "home-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	target := filepath.Join(home, ".local", "bin", "sesh")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedTarget == target {
		t.Fatalf("fixture does not exercise distinct path spellings: %s", target)
	}
	plist, err := setup.RenderPlist(nil, target, ch.srv.URL, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(setup.PlistPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup.PlistPath(home), plist, 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{outputs: map[string]string{}}
	runner.outputs[runner.key(target, "version")] = "v1.1.0"
	var out bytes.Buffer
	opts := Options{
		OS: "darwin", Arch: "arm64", Home: home, Exe: target,
		Version: "v1.0.0", UID: 501, Runner: runner, Out: &out,
	}
	if err := Run(opts); err != nil {
		t.Fatalf("darwin update: %v\n%s", err, out.String())
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "launchctl kickstart -k "+setup.LaunchdServiceTarget(501)) {
		t.Fatalf("kickstart not called:\n%s", joined)
	}
	if !strings.Contains(out.String(), "verified on disk") {
		t.Fatalf("macOS on-disk caveat missing:\n%s", out.String())
	}
}
