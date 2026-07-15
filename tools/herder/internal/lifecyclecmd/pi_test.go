package lifecyclecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func installLifecyclePi(t *testing.T, version string) {
	t.Helper()
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent")
	bin := filepath.Join(pkg, "dist", "cli.js")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@earendil-works/pi-coding-agent","version":"`+version+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pathBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(pathBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, filepath.Join(pathBin, "pi")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPreparePiLifecycleRequiresRegistryProvider(t *testing.T) {
	installLifecyclePi(t, "0.80.6")
	t.Setenv("OPENAI_API_KEY", "selected")
	_, _, _, err := preparePiLifecycle("pi", &registry.Record{}, time.Now())
	if err == nil {
		t.Fatal("preparePiLifecycle() succeeded without a registry provider")
	}
	for _, want := range []string{"registry row", "no provider fact", "spawn a fresh Pi session", "--provider"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestResumePiMissingRegistryProviderRefusesBeforePaneCreation(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	row := fmt.Sprintf(`{"guid":"guid-pi-missing-provider","short_guid":"missing","label":"pi-missing-provider","role":"worker","agent":"pi","terminal_id":"term_old","pane_id":"pane_old","hcom_dir":"%s","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"session-pi","tag":"worker","cwd":"%s","ts":"2026-07-15T00:00:00Z"}}`+"\n", filepath.Join(state, ".hcom"), state)
	if err := os.WriteFile(registryPath, []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	configureLifecycleTest(t, state)
	client := &noLaunchHerdr{}
	var stdout, stderr strings.Builder
	code := (&runner{stdout: &stdout, stderr: &stderr, herdr: client}).resume(resumeOptions{target: "pi-missing-provider", cwd: state})
	if code == 0 {
		t.Fatalf("resume succeeded without provider\nstderr:\n%s", stderr.String())
	}
	if client.startCalls != 0 {
		t.Fatalf("agent start called %d times, want zero", client.startCalls)
	}
	for _, want := range []string{"no provider fact", "spawn a fresh Pi session", "--provider"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %s", want, stderr.String())
		}
	}
}

func TestPreparePiLifecycleReconstructsRegistryFactsAndReobservesVendor(t *testing.T) {
	installLifecyclePi(t, "0.80.6")
	t.Setenv("OPENAI_API_KEY", "selected")
	t.Setenv("ANTHROPIC_API_KEY", "ambient-wrong-provider")
	t.Setenv("XAI_API_KEY", "ambient-wrong-provider")
	previous := v2.VendorVersionObservation{Version: "0.80.5", ObservedAt: "2026-07-14T00:00:00Z"}
	rec := &registry.Record{
		Provider:      "openai",
		Model:         "model-from-registry",
		VendorVersion: &v2.VendorVersionHistory{Current: previous},
	}
	now := time.Date(2026, 7, 15, 1, 2, 3, 0, time.UTC)
	provider, model, version, err := preparePiLifecycle("pi", rec, now)
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "model-from-registry" {
		t.Fatalf("facts = (%q, %q), want registry values", provider, model)
	}
	if version == nil || version.Current.Version != "0.80.6" || version.Current.ObservedAt != "2026-07-15T01:02:03Z" || version.Previous == nil || *version.Previous != previous {
		t.Fatalf("version history = %#v, want refreshed current plus previous", version)
	}
}

func TestPiLifecycleLaunchTokensUseReconstructedFacts(t *testing.T) {
	spec := startSpec{
		Mode: "resume", Agent: "pi", VehicleTarget: "session-id", Role: "worker",
		Provider: "openai", Model: "model-from-registry",
	}
	got := lifecycleLaunchTokens("/herder", spec, []string{"--go"})
	want := []string{"/herder", "launch", "--resume", "pi", "session-id", "--tag", "worker", "--provider", "openai", "--model", "model-from-registry", "--go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "anthropic") {
		t.Fatalf("tokens used ambient provider: %v", got)
	}
}

func TestPiLifecycleCarriesResolvedExecutableDirectoryIntoLoginPath(t *testing.T) {
	const shims = "/repo/tools/herder/shims"
	const piBin = "/custom-prefix/node_modules/.bin"
	got := lifecyclePathPrefix(shims, startSpec{Agent: "pi", PiBinDir: piBin})
	for _, want := range []string{shims, piBin} {
		if !strings.Contains(got, want) {
			t.Fatalf("lifecyclePathPrefix(pi) = %q, missing %q", got, want)
		}
	}
	if got := lifecyclePathPrefix(shims, startSpec{Agent: "claude", PiBinDir: piBin}); strings.Contains(got, piBin) {
		t.Fatalf("lifecyclePathPrefix(claude) leaked Pi path: %q", got)
	}
}
