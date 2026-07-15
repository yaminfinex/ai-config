package spawncmd

import (
	"io"
	"os"
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

func TestPiSpawnCarriesResolvedExecutableDirectoryIntoChildPath(t *testing.T) {
	const shims = "/repo/tools/herder/shims"
	const piBin = "/custom-prefix/node_modules/.bin"
	want := shims + string(os.PathListSeparator) + piBin
	if got := agentPathPrefix(shims, "pi", piBin); got != want {
		t.Fatalf("agentPathPrefix(pi) = %q, want %q", got, want)
	}
	if got := agentPathPrefix(shims, "codex", piBin); got != shims {
		t.Fatalf("agentPathPrefix(codex) = %q, want no Pi path leakage", got)
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
