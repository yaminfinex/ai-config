package setup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	urlA = "http://sesh.example.ts.net:8765"
	urlB = "http://sesh-new.example.ts.net:8765"
)

// --- provenance digest -------------------------------------------------------

func TestDigestRoundTrip(t *testing.T) {
	for name, cs := range map[string]commentStyle{"unit": unitCommentStyle, "plist": plistCommentStyle} {
		t.Run(name, func(t *testing.T) {
			body := []byte("hello\nworld\n")
			stamped := cs.stamp(body)
			got, prov := cs.split(stamped)
			if prov != ProvenanceIntact {
				t.Fatalf("fresh stamp: want intact, got %v", prov)
			}
			if !bytes.Equal(got, body) {
				t.Fatalf("split body mismatch: %q", got)
			}
		})
	}
}

func TestDigestDetectsEdits(t *testing.T) {
	stamped := unitCommentStyle.stamp([]byte("[Service]\nEnvironment=SESH_STORE_URL=" + urlA + "\n"))
	edited := bytes.Replace(stamped, []byte(urlA), []byte(urlB), 1)
	if _, prov := unitCommentStyle.split(edited); prov != ProvenanceEdited {
		t.Fatalf("URL-only edit: want edited, got %v", prov)
	}
}

func TestDigestAbsentIsLegacy(t *testing.T) {
	legacy := []byte("# operator file\n[Service]\nEnvironment=SESH_STORE_URL=" + urlA + "\n")
	if _, prov := unitCommentStyle.split(legacy); prov != ProvenanceLegacy {
		t.Fatalf("no digest: want legacy, got %v", prov)
	}
	if _, prov := unitCommentStyle.split(nil); prov != ProvenanceLegacy {
		t.Fatalf("nil content: want legacy, got %v", prov)
	}
}

// --- drop-in render/rewrite --------------------------------------------------

func TestDropinFreshRender(t *testing.T) {
	content := RenderDropin(nil, urlA)
	if _, prov := unitCommentStyle.split(content); prov != ProvenanceIntact {
		t.Fatalf("fresh drop-in digest not intact:\n%s", content)
	}
	url, ok := DropinStoreURL(content)
	if !ok || url != urlA {
		t.Fatalf("DropinStoreURL = %q, %v", url, ok)
	}
	if !strings.Contains(string(content), "[Service]") {
		t.Fatalf("fresh drop-in lacks [Service] section:\n%s", content)
	}
}

func TestDropinRoundTripSameURL(t *testing.T) {
	first := RenderDropin(nil, urlA)
	second := RenderDropin(first, urlA)
	if !bytes.Equal(first, second) {
		t.Fatalf("same-URL rewrite is not byte-identical:\n--- first\n%s--- second\n%s", first, second)
	}
}

func TestDropinRewritePreservesUnknownKeysAndQuoting(t *testing.T) {
	body := "# operator note kept verbatim\n" +
		"[Service]\n" +
		"Environment=\"SESH_STATE_DIR=/var/tmp/sesh state\"\n" +
		"Environment='ODD=a b' SESH_STORE_URL=" + urlA + " \"OTHER=x\"\n" +
		"Environment=SESH_DEBUG=1\n"
	existing := unitCommentStyle.stamp([]byte(body))
	got := RenderDropin(existing, urlB)

	if _, prov := unitCommentStyle.split(got); prov != ProvenanceIntact {
		t.Fatalf("rewritten drop-in digest not intact:\n%s", got)
	}
	url, ok := DropinStoreURL(got)
	if !ok || url != urlB {
		t.Fatalf("rewritten URL = %q, %v", url, ok)
	}
	for _, want := range []string{
		"# operator note kept verbatim",
		"Environment=\"SESH_STATE_DIR=/var/tmp/sesh state\"",
		"Environment='ODD=a b' SESH_STORE_URL=" + urlB + " \"OTHER=x\"",
		"Environment=SESH_DEBUG=1",
	} {
		if !strings.Contains(string(got), want+"\n") {
			t.Errorf("rewrite lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), urlA) {
		t.Errorf("old URL survived the rewrite:\n%s", got)
	}
}

func TestDropinRewriteQuotedURLKeepsQuotes(t *testing.T) {
	existing := unitCommentStyle.stamp([]byte("[Service]\nEnvironment=\"SESH_STORE_URL=" + urlA + "\"\n"))
	got := RenderDropin(existing, urlB)
	if !strings.Contains(string(got), "Environment=\"SESH_STORE_URL="+urlB+"\"\n") {
		t.Fatalf("quoted assignment lost its quotes:\n%s", got)
	}
}

func TestDropinRewriteAppendsWhenURLMissing(t *testing.T) {
	existing := unitCommentStyle.stamp([]byte("[Service]\nEnvironment=SESH_STATE_DIR=/x\n"))
	got := RenderDropin(existing, urlA)
	url, ok := DropinStoreURL(got)
	if !ok || url != urlA {
		t.Fatalf("appended URL = %q, %v", url, ok)
	}
	if !strings.Contains(string(got), "Environment=SESH_STATE_DIR=/x\n") {
		t.Fatalf("existing key lost:\n%s", got)
	}
}

// --- unit render ---------------------------------------------------------------

func TestRenderUnitPinsExecStart(t *testing.T) {
	unit := RenderUnit("/home/u/.local/bin/sesh")
	if !strings.Contains(unit, "ExecStart=/home/u/.local/bin/sesh ship\n") {
		t.Fatalf("unit does not pin the binary:\n%s", unit)
	}
	if strings.Contains(unit, unitPlaceholderExec) {
		t.Fatalf("placeholder survived the render")
	}
	if strings.Contains(unit, "\nEnvironment=") {
		t.Fatalf("unit must not bake environment; the store URL arrives via drop-in only")
	}
}

// --- plist render/rewrite ------------------------------------------------------

func TestPlistFreshRender(t *testing.T) {
	content, err := RenderPlist(nil, "/Users/u/.local/bin/sesh", urlA, "/Users/u")
	if err != nil {
		t.Fatal(err)
	}
	if _, prov := plistCommentStyle.split(content); prov != ProvenanceIntact {
		t.Fatalf("fresh plist digest not intact:\n%s", content)
	}
	if url, ok := PlistStoreURL(content); !ok || url != urlA {
		t.Fatalf("PlistStoreURL = %q, %v", url, ok)
	}
	if exe, ok := PlistExecPath(content); !ok || exe != "/Users/u/.local/bin/sesh" {
		t.Fatalf("PlistExecPath = %q, %v", exe, ok)
	}
	if strings.Contains(string(content), "@") {
		t.Fatalf("unrendered @TOKEN@ left:\n%s", content)
	}
}

func TestPlistRoundTripSameValues(t *testing.T) {
	first, err := RenderPlist(nil, "/Users/u/.local/bin/sesh", urlA, "/Users/u")
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderPlist(first, "/Users/u/.local/bin/sesh", urlA, "/Users/u")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("same-values plist rewrite not byte-identical:\n--- first\n%s--- second\n%s", first, second)
	}
}

func TestPlistURLRewriteLeavesProgramArgumentsUntouched(t *testing.T) {
	first, err := RenderPlist(nil, "/Users/u/.local/bin/sesh", urlA, "/Users/u")
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderPlist(first, "/Users/u/.local/bin/sesh", urlB, "/Users/u")
	if err != nil {
		t.Fatal(err)
	}
	section := func(b []byte) string {
		s := string(b)
		start := strings.Index(s, "<key>ProgramArguments</key>")
		end := strings.Index(s[start:], "</array>")
		return s[start : start+end]
	}
	if section(first) != section(second) {
		t.Fatalf("URL rewrite disturbed ProgramArguments:\n--- before\n%s\n--- after\n%s", section(first), section(second))
	}
	if url, ok := PlistStoreURL(second); !ok || url != urlB {
		t.Fatalf("rewritten plist URL = %q, %v", url, ok)
	}
	if exe, ok := PlistExecPath(second); !ok || exe != "/Users/u/.local/bin/sesh" {
		t.Fatalf("rewritten plist exe = %q, %v", exe, ok)
	}
}

func TestPlistRewriteRefusesForeignShape(t *testing.T) {
	foreign := []byte("<?xml version=\"1.0\"?>\n<plist version=\"1.0\"><dict></dict></plist>\n")
	if _, err := RenderPlist(foreign, "/bin/sesh", urlA, "/Users/u"); err == nil {
		t.Fatal("rewrite of a plist without setup's keys must error")
	}
}

// --- full runs (fake runner) ---------------------------------------------------

type fakeRunner struct {
	calls   []string
	fail    map[string]error
	outputs map[string]string
}

func (f *fakeRunner) key(name string, args ...string) string {
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
	return "", nil
}

func linuxOpts(t *testing.T, home string, runner *fakeRunner) Options {
	t.Helper()
	return Options{
		StoreURL: urlA,
		Home:     home,
		OS:       "linux",
		Exe:      "/fake/bin/sesh",
		User:     "u",
		Runner:   runner,
		Out:      &bytes.Buffer{},
	}
}

func TestLinuxFreshInstall(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{outputs: map[string]string{"loginctl show-user u --property=Linger --value": "yes"}}
	if err := Run(linuxOpts(t, home, runner)); err != nil {
		t.Fatal(err)
	}
	unit, err := os.ReadFile(UnitPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "ExecStart=/fake/bin/sesh ship\n") {
		t.Fatalf("unit not pinned:\n%s", unit)
	}
	dropin, err := os.ReadFile(DropinPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if url, ok := DropinStoreURL(dropin); !ok || url != urlA {
		t.Fatalf("drop-in URL = %q, %v", url, ok)
	}
	want := []string{
		"systemctl --user show-environment",
		"systemctl --user daemon-reload",
		"systemctl --user enable --now sesh-ship.service",
		"loginctl show-user u --property=Linger --value",
	}
	if strings.Join(runner.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("call sequence:\n%s\nwant:\n%s", strings.Join(runner.calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestLinuxPreflightFailureWritesNothing(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{fail: map[string]error{"systemctl --user show-environment": fmt.Errorf("no bus")}}
	err := Run(linuxOpts(t, home, runner))
	if err == nil || !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("want preflight refusal, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(statErr) {
		t.Fatal("preflight failure left files under HOME")
	}
}

func TestLinuxURLMigrationWithoutForce(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	if err := Run(linuxOpts(t, home, runner)); err != nil {
		t.Fatal(err)
	}
	opts := linuxOpts(t, home, runner)
	opts.StoreURL = urlB // digest intact → the one-command URL migration, no --force
	if err := Run(opts); err != nil {
		t.Fatalf("digest-intact drop-in must be replaced on explicit new URL: %v", err)
	}
	dropin, _ := os.ReadFile(DropinPath(home))
	if url, _ := DropinStoreURL(dropin); url != urlB {
		t.Fatalf("URL not migrated: %q", url)
	}
}

func TestLinuxOperatorEditRefusedIncludingURLOnly(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	if err := Run(linuxOpts(t, home, runner)); err != nil {
		t.Fatal(err)
	}
	// A URL-only edit: byte-identical to a canonical render for urlB, which
	// shape equality could never distinguish — the digest does.
	dropin, _ := os.ReadFile(DropinPath(home))
	edited := bytes.ReplaceAll(dropin, []byte(urlA), []byte(urlB))
	if err := os.WriteFile(DropinPath(home), edited, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := linuxOpts(t, home, runner)
	opts.StoreURL = urlA
	err := Run(opts)
	if err == nil || !strings.Contains(err.Error(), "edited since sesh setup wrote it") {
		t.Fatalf("URL-only operator edit must refuse without --force, got %v", err)
	}
	after, _ := os.ReadFile(DropinPath(home))
	if !bytes.Equal(after, edited) {
		t.Fatal("refusal path modified the operator drop-in")
	}

	opts.Force = true
	if err := Run(opts); err != nil {
		t.Fatalf("--force must override: %v", err)
	}
	after, _ = os.ReadFile(DropinPath(home))
	if url, _ := DropinStoreURL(after); url != urlA {
		t.Fatalf("--force rewrite URL = %q", url)
	}
}

func TestLinuxLegacyDropinRefused(t *testing.T) {
	home := t.TempDir()
	legacy := "# Written by install-ship.sh — node-local values only. Re-run --force to change.\n" +
		"[Service]\nEnvironment=SESH_STORE_URL=" + urlA + "\n"
	if err := os.MkdirAll(filepath.Dir(DropinPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DropinPath(home), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	err := Run(linuxOpts(t, home, runner))
	if err == nil || !strings.Contains(err.Error(), "without a provenance digest") {
		t.Fatalf("legacy drop-in must refuse without --force, got %v", err)
	}

	opts := linuxOpts(t, home, runner)
	opts.Force = true
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	adopted, _ := os.ReadFile(DropinPath(home))
	if _, prov := unitCommentStyle.split(adopted); prov != ProvenanceIntact {
		t.Fatal("--force adoption must stamp the drop-in")
	}
}

func TestLinuxDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	var out bytes.Buffer
	opts := linuxOpts(t, home, runner)
	opts.DryRun = true
	opts.Out = &out
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("dry run created files under HOME")
	}
	for _, want := range []string{
		"DRY-RUN: would write " + UnitPath(home),
		"ExecStart=/fake/bin/sesh ship",
		"Environment=SESH_STORE_URL=" + urlA,
		"DRY-RUN: systemctl --user enable --now sesh-ship.service",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry-run output lacks %q:\n%s", want, out.String())
		}
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "daemon-reload") || strings.Contains(call, "enable") {
			t.Errorf("dry run executed %q", call)
		}
	}
}

func TestLinuxLingerWarning(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{outputs: map[string]string{"loginctl show-user u --property=Linger --value": "no"}}
	var out bytes.Buffer
	opts := linuxOpts(t, home, runner)
	opts.Out = &out
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "loginctl enable-linger u") {
		t.Fatalf("missing linger warning:\n%s", out.String())
	}
}

func TestDarwinFreshInstall(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	opts := Options{
		StoreURL: urlA,
		Home:     home,
		OS:       "darwin",
		Exe:      "/fake/bin/sesh",
		UID:      501,
		Runner:   runner,
		Out:      &bytes.Buffer{},
	}
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	plist, err := os.ReadFile(PlistPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if url, ok := PlistStoreURL(plist); !ok || url != urlA {
		t.Fatalf("plist URL = %q, %v", url, ok)
	}
	if exe, ok := PlistExecPath(plist); !ok || exe != "/fake/bin/sesh" {
		t.Fatalf("plist exe = %q, %v", exe, ok)
	}
	want := []string{
		"launchctl bootout gui/501/dev.sesh.ship",
		"launchctl bootstrap gui/501 " + PlistPath(home),
	}
	if strings.Join(runner.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("call sequence:\n%s\nwant:\n%s", strings.Join(runner.calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestDarwinRefusalBeforeAnyAction(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(PlistPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PlistPath(home), []byte("<plist>operator</plist>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	opts := Options{
		StoreURL: urlA, Home: home, OS: "darwin", Exe: "/fake/bin/sesh", UID: 501,
		Runner: runner, Out: &bytes.Buffer{},
	}
	if err := Run(opts); err == nil {
		t.Fatal("foreign plist must refuse without --force")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("refusal ran launchctl: %v", runner.calls)
	}

	// --force on a plist too foreign to rewrite falls back to a fresh render.
	opts.Force = true
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	plist, _ := os.ReadFile(PlistPath(home))
	if url, ok := PlistStoreURL(plist); !ok || url != urlA {
		t.Fatalf("forced plist URL = %q, %v", url, ok)
	}
}
