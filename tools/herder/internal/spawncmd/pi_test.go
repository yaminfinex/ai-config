package spawncmd

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestPiSpawnRequiresProviderAndAllowsModel(t *testing.T) {
	for _, args := range [][]string{
		{"--role", "worker", "--agent", "pi"},
		{"--role", "worker", "--agent", "pi", "--provider", ""},
		{"--role", "worker", "--agent", "pi", "--provider", "google"},
	} {
		var stderr strings.Builder
		if _, code := parseArgs(args, io.Discard, &stderr); code == 0 {
			t.Fatalf("parseArgs(%v) accepted invalid provider", args)
		}
		if !strings.Contains(stderr.String(), "provider") {
			t.Errorf("parseArgs(%v) error lacks provider cause: %s", args, stderr.String())
		}
	}

	opts, code := parseArgs([]string{"--role", "worker", "--agent", "pi", "--provider", "openai", "--model", "gpt-test"}, io.Discard, io.Discard)
	if code != 0 {
		t.Fatal("valid Pi provider/model refused")
	}
	if opts.Provider != "openai" || opts.Model != "gpt-test" || len(opts.ExtraArgs) < 2 || opts.ExtraArgs[0] != "--model" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestPiSpawnParseCallSiteRefusesOwnedPassthrough(t *testing.T) {
	var stderr strings.Builder
	args := []string{"--role", "worker", "--agent", "pi", "--provider", "openai", "--extra-arg", "--api-key=stand-in"}
	if _, code := parseArgs(args, io.Discard, &stderr); code == 0 {
		t.Fatal("Pi spawn parse boundary accepted an owned credential passthrough")
	}
	for _, want := range []string{"--api-key", "refused", "environment"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("spawn refusal missing %q: %s", want, stderr.String())
		}
	}
}

func TestNonPiSpawnProviderRefusesWithTypedMessage(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			var stderr strings.Builder
			args := []string{"--role", "worker", "--agent", agent, "--provider", "openai"}
			if _, code := parseArgs(args, io.Discard, &stderr); code == 0 {
				t.Fatalf("non-Pi agent %s accepted --provider", agent)
			}
			if got, want := strings.TrimSpace(stderr.String()), "herder spawn: --provider is supported only for --agent pi"; got != want {
				t.Fatalf("refusal = %q, want %q", got, want)
			}
		})
	}
}

func TestPiTeamBusRefusesWithCauseAndRemedy(t *testing.T) {
	for _, args := range [][]string{
		{"--role", "worker", "--agent", "pi", "--provider", "openai", "--team", "alpha"},
		{"--team", "alpha", "--agent", "pi", "--provider", "openai", "--role", "worker"},
	} {
		var stderr strings.Builder
		if _, code := parseArgs(args, io.Discard, &stderr); code == 0 {
			t.Fatalf("parseArgs(%v) accepted Pi team bus", args)
		}
		for _, want := range []string{"global bus", "remove --team", "Pi"} {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("refusal missing %q: %s", want, stderr.String())
			}
		}
	}
}

func TestPiBindPredicateRequiresAllRosterFacts(t *testing.T) {
	full := hcomEntry{Name: "worker-pi", Tool: "pi", HooksBound: true, SessionID: "session-pi", TranscriptPath: "/scratch/session.jsonl"}
	if !piBindReady(full) {
		t.Fatal("full Pi bind predicate rejected")
	}
	negative := []hcomEntry{
		{Name: "worker-pi", Tool: "pi", SessionID: "session-pi"},
		{Name: "worker-pi", Tool: "pi", HooksBound: true},
		{Name: "worker-pi", Tool: "codex", HooksBound: true, SessionID: "session-pi"},
	}
	for i, entry := range negative {
		if piBindReady(entry) {
			t.Errorf("negative case %d accepted: %+v", i, entry)
		}
	}
}

func TestPiSpawnAppendsMultiBinaryDirectoryAfterInheritedPath(t *testing.T) {
	root := t.TempDir()
	shims := filepath.Join(root, "shims")
	systemBin := filepath.Join(root, "system-bin")
	piBin := filepath.Join(root, "node_modules", ".bin")
	for _, dir := range []string{shims, systemBin, piBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(shims, "hcom"),
		filepath.Join(systemBin, "git"),
		filepath.Join(piBin, "git"),
		filepath.Join(piBin, "pi"),
	} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	pathValue := agentPathValue(shims, systemBin, "pi", piBin)
	t.Setenv("PATH", pathValue)
	if got, err := exec.LookPath("hcom"); err != nil || got != filepath.Join(shims, "hcom") {
		t.Fatalf("shim lookup = (%q, %v), want shim-first hcom", got, err)
	}
	if got, err := exec.LookPath("git"); err != nil || got != filepath.Join(systemBin, "git") {
		t.Fatalf("git lookup = (%q, %v), Pi dependency directory shadowed inherited command", got, err)
	}
	if got, err := exec.LookPath("pi"); err != nil || got != filepath.Join(piBin, "pi") {
		t.Fatalf("Pi lookup = (%q, %v), want trailing Pi fallback", got, err)
	}

	wantLogin := shims + ":$PATH:" + piBin
	if got := agentLoginPathExpression(shims, "pi", piBin); got != wantLogin {
		t.Fatalf("login PATH = %q, want %q", got, wantLogin)
	}
	if got := agentLoginPathExpression(shims, "codex", piBin); strings.Contains(got, piBin) {
		t.Fatalf("non-Pi login PATH leaked Pi directory: %q", got)
	}
	if got := agentPathValue(shims, systemBin, "codex", piBin); strings.Contains(got, piBin) {
		t.Fatalf("non-Pi PATH leaked Pi directory: %q", got)
	}
}

func TestPiBindTimeoutHardFailsWithConfirmedCleanup(t *testing.T) {
	for _, prompt := range []string{"", "deliver after bind"} {
		t.Run(map[bool]string{true: "prompted", false: "promptless"}[prompt != ""], func(t *testing.T) {
			client := &cleanupHerdr{}
			var stderr strings.Builder
			r := &runner{opts: options{Agent: "pi", Prompt: prompt}, herdr: client, stderr: &stderr}
			if code := r.failUnboundPi("", "bind-timeout(1ms)", "p_new", "term_new"); code != 1 {
				t.Fatalf("failUnboundPi() = %d, want 1", code)
			}
			if !client.closed {
				t.Fatal("Pi bind timeout did not close pane")
			}
			for _, want := range []string{"Pi", "hooks", "session", "cleanup confirmed"} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr missing %q: %s", want, stderr.String())
				}
			}
		})
	}
}

func TestPiSpawnRegistryPersistsLaunchAndBindFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	history := launchcmd.RefreshPiVendorVersion(nil, v2.VendorVersionObservation{Version: "0.80.6", ObservedAt: "2026-07-15T01:00:00Z"})
	r := &runner{}
	record := spawnRecord{
		GUID: "guid-pi", ShortGUID: "guid-pi", Label: "worker-pi", Role: "worker", Agent: "pi",
		Provider: "openai", Model: "gpt-test", VendorVersion: history,
		PaneID: "p_pi", TerminalID: "term_pi", HcomName: "worker-pi", HcomDir: "/scratch/.hcom",
		HooksBound: true, TranscriptPath: "/scratch/session.jsonl", Status: "active", StartedAt: "2026-07-15T01:00:00Z",
		Provenance: registry.Provenance{ToolSessionID: "session-pi", TS: "2026-07-15T01:00:00Z"},
	}
	if err := r.registerSpawn(path, record); err != nil {
		t.Fatal(err)
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.V2ByGUID(projection, "guid-pi")
	if got == nil || got.Provider != "openai" || got.Model != "gpt-test" || got.VendorVersion == nil {
		t.Fatalf("registry row missing launch facts: %+v", got)
	}
	if got.Seat == nil || !got.Seat.HooksBound || got.Seat.TranscriptPath != "/scratch/session.jsonl" || len(got.SIDs) != 1 || got.SIDs[0].SID != "session-pi" {
		t.Fatalf("registry row missing bind facts: %+v", got)
	}
}
