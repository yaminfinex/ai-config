package adoptcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/occupant"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcred"
)

func TestCutoverAdoptNeverSelectsCallerFromAmbientEnvironment(t *testing.T) {
	path := seedAdoptRegistry(t, v2.SessionRecord{
		GUID: "guid-previous", State: v2.StateSeated, Label: "stable", Tool: "codex",
		Seat: &v2.Seat{Kind: "herdr", PaneID: "pane-previous", TerminalID: "term-previous"},
	})
	if err := seatcred.EnableCutover(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))
	t.Setenv("HERDR_PANE_ID", "pane-poison-parent")
	t.Setenv("HERDER_GUID", "guid-poison-parent")
	t.Setenv("HERDER_AGENT", "")
	t.Setenv("HCOM_SESSION_ID", "sid-poison-parent")
	t.Setenv("HCOM_TOOL", "")
	var stdout, stderr strings.Builder
	if rc := Run([]string{"guid-previous"}, &stdout, &stderr); rc != 2 {
		t.Fatalf("Run rc=%d, want credential refusal; stderr=%q", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--credential-file is required") || !strings.Contains(stderr.String(), "hints, not authority") {
		t.Fatalf("stderr=%q, want ambient-authority refusal", stderr.String())
	}
}

func TestCutoverUnseatedAdoptMintsFreshReplacementCredentialWithoutCallerCredential(t *testing.T) {
	path := seedAdoptRegistry(t, v2.SessionRecord{
		GUID: "guid-previous", State: v2.StateUnseated, Label: "stable", Role: "worker", Tool: "codex",
	})
	if err := seatcred.EnableCutover(path); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	herdr := `#!/bin/sh
if [ "$1 $2" = "pane get" ]; then
  printf '%s\n' '{"result":{"pane":{"pane_id":"pane-replacement","terminal_id":"term-replacement","workspace_id":"ws-replacement","cwd":"/mock/cwd"}}}'
fi
exit 0
`
	hcom := `#!/bin/sh
if [ "$1 $2" = "list --json" ]; then
  printf '%s\n' '[]'
  exit 0
fi
if [ "$1" = "start" ]; then
  printf 'intentional reclaim stop\n' >&2
  exit 1
fi
exit 0
`
	for name, body := range map[string]string{"herdr": herdr, "hcom": hcom} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "pane-replacement")
	t.Setenv("HERDER_GUID", "guid-poison-parent")
	t.Setenv("HERDER_AGENT", "")
	t.Setenv("HCOM_SESSION_ID", "sid-poison-parent")
	t.Setenv("HCOM_TOOL", "")
	t.Setenv("HCOM_DIR", t.TempDir())

	var stdout, stderr strings.Builder
	if rc := Run([]string{"guid-previous"}, &stdout, &stderr); rc != 1 {
		t.Fatalf("Run rc=%d, want deliberate late reclaim failure; stderr=%q", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), "--credential-file is required") || !strings.Contains(stderr.String(), "adopt: enroll applied: new guid") {
		t.Fatalf("stderr=%q, want uncredentialed fresh-enroll leg before late failure", stderr.String())
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var replacement *v2.SessionRecord
	for _, session := range projection.Sessions() {
		if session.GUID != "guid-previous" && session.State == v2.StateSeated {
			copy := session
			replacement = &copy
		}
	}
	if replacement == nil || replacement.Seat == nil || replacement.Seat.CredentialGeneration == "" {
		t.Fatalf("replacement=%+v, want fresh seat with first credential committed", replacement)
	}
	if replacement.Provenance.SpawnedBy != "user" && replacement.Provenance.SpawnedBy != "" {
		t.Fatalf("replacement provenance spawned_by=%q, inherited parent must not select attribution", replacement.Provenance.SpawnedBy)
	}
}

func TestDifferentPaneSeatedTargetRefusesBeforeEnrollment(t *testing.T) {
	path := seedAdoptRegistry(t,
		v2.SessionRecord{
			GUID:  "guid-previous",
			State: v2.StateSeated,
			Label: "stable",
			Seat:  &v2.Seat{Kind: "herdr", TerminalID: "term_previous", PaneID: "pane_previous"},
		},
	)
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(path))
	t.Setenv("HERDR_PANE_ID", "pane_replacement")
	t.Setenv("HCOM_SESSION_ID", "")
	t.Setenv("HCOM_PROCESS_ID", "")
	t.Setenv("HCOM_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	before := mustReadAdoptRegistry(t, path)

	var stdout, stderr strings.Builder
	if rc := Run([]string{"guid-previous"}, &stdout, &stderr); rc == 0 {
		t.Fatalf("Run rc = 0, want refusal")
	}
	for _, want := range []string{
		"seated on pane pane_previous",
		"recorded pane is gone",
		"refusing before enrollment",
		"herder adopt guid-previous --confirm-dead",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if strings.Contains(stderr.String(), "herder cull") {
		t.Fatalf("stderr = %q, must not suggest culling from an adoption preflight", stderr.String())
	}
	if after := mustReadAdoptRegistry(t, path); after != before {
		t.Fatalf("preflight refusal changed registry\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestAdoptionUnseatAuthorization(t *testing.T) {
	old := v2.SessionRecord{
		GUID: "guid-previous", Tool: "codex",
		Seat: &v2.Seat{PaneID: "pane_previous"},
		SIDs: []v2.SID{{SID: adoptSIDOld}},
	}
	caller := hcomidentity.Result{Verified: true, PaneID: "pane_replacement"}

	t.Run("same pane flag-free path remains seat-superseded", func(t *testing.T) {
		samePane := old
		samePane.Seat = &v2.Seat{PaneID: "pane_shared"}
		reason, err := adoptionUnseatReason(samePane, hcomidentity.Result{Verified: true, PaneID: "pane_shared"}, false, occupant.Substrate{})
		if err != nil || reason != "seat superseded by replacement process in the same pane" {
			t.Fatalf("reason/error = %q / %v", reason, err)
		}
		_, err = adoptionUnseatReason(samePane, hcomidentity.Result{Verified: true, PaneID: "pane_shared"}, true, occupant.Substrate{})
		if err == nil || !strings.Contains(err.Error(), "--confirm-dead is unnecessary") || !strings.Contains(err.Error(), "rerun without it: 'herder adopt guid-previous'") {
			t.Fatalf("surplus flag error = %v", err)
		}
	})

	t.Run("pane gone requires and honors confirm-dead", func(t *testing.T) {
		sub := adoptProbeSubstrate(t, adoptProbeGone, false)
		if _, err := adoptionUnseatReason(old, caller, false, sub); err == nil || !strings.Contains(err.Error(), "herder adopt guid-previous --confirm-dead") {
			t.Fatalf("missing flag remedy: %v", err)
		}
		reason, err := adoptionUnseatReason(old, caller, true, sub)
		if err != nil || reason != "operator confirmed old transcript dead" {
			t.Fatalf("reason/error = %q / %v", reason, err)
		}
	})

	t.Run("vacant pane proceeds flag-free and refuses unnecessary flag", func(t *testing.T) {
		// The shell_pid-present wire still omits the tool pid from foreground_processes.
		sub := adoptProbeSubstrate(t, adoptProbeVacant, true)
		reason, err := adoptionUnseatReason(old, caller, false, sub)
		if err != nil || reason != "operator confirmed old transcript dead" {
			t.Fatalf("reason/error = %q / %v", reason, err)
		}
		if _, err := adoptionUnseatReason(old, caller, true, sub); err == nil || !strings.Contains(err.Error(), "--confirm-dead is unnecessary") || !strings.Contains(err.Error(), "herder adopt guid-previous") {
			t.Fatalf("missing flag-free rerun: %v", err)
		}
	})

	t.Run("matching old occupant proves seat alive", func(t *testing.T) {
		// The shell_pid-absent wire forces discovery through the /proc descent leg.
		sub := adoptProbeSubstrate(t, adoptProbeMatch, false)
		for _, confirmDead := range []bool{false, true} {
			_, err := adoptionUnseatReason(old, caller, confirmDead, sub)
			if err == nil || !strings.Contains(err.Error(), "seat is alive") || !strings.Contains(err.Error(), "session "+adoptSIDOld) || !strings.Contains(err.Error(), "herder cull --guid guid-previous") {
				t.Fatalf("confirmDead=%v error=%v", confirmDead, err)
			}
		}
	})

	t.Run("foreign and unprobeable occupants fail closed", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			sub  occupant.Substrate
			want string
		}{
			{name: "foreign", sub: adoptProbeSubstrate(t, adoptProbeForeign, true), want: "foreign or ambiguous"},
			{name: "unprobeable", sub: adoptProbeSubstrate(t, adoptProbeUnprobeable, false), want: "unprobeable"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := adoptionUnseatReason(old, caller, true, tc.sub)
				if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "--confirm-dead is not applicable") || !strings.Contains(err.Error(), "herder compact") {
					t.Fatalf("error=%v", err)
				}
			})
		}
	})
}

const (
	adoptSIDOld     = "11111111-1111-4111-8111-111111111111"
	adoptSIDForeign = "22222222-2222-4222-8222-222222222222"
)

type adoptProbeCase int

const (
	adoptProbeGone adoptProbeCase = iota
	adoptProbeVacant
	adoptProbeMatch
	adoptProbeForeign
	adoptProbeUnprobeable
)

type adoptFakeHerdr struct {
	pane    herdrcli.Pane
	info    herdrcli.ProcessInfo
	paneErr error
	infoErr error
}

func (f adoptFakeHerdr) Pane(string) (herdrcli.Pane, error) { return f.pane, f.paneErr }
func (f adoptFakeHerdr) Panes() ([]herdrcli.Pane, error)    { return []herdrcli.Pane{f.pane}, f.paneErr }
func (f adoptFakeHerdr) ProcessInfo(string) (herdrcli.ProcessInfo, error) {
	return f.info, f.infoErr
}

func adoptProbeSubstrate(t *testing.T, probeCase adoptProbeCase, shellPID bool) occupant.Substrate {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proc")
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if probeCase == adoptProbeGone {
		return occupant.Substrate{Herdr: adoptFakeHerdr{paneErr: os.ErrNotExist}, ProcRoot: root, Home: home}
	}
	pane := herdrcli.Pane{PaneID: "pane_previous", Agent: "codex"}
	info := herdrcli.ProcessInfo{ForegroundProcessGroupID: 10, Processes: []herdrcli.Process{{PID: 10, Name: "bash"}}}
	if shellPID {
		info.ShellPID = 10
	}
	writeAdoptProc(t, root, 10, 1, "bash", home)
	if probeCase == adoptProbeUnprobeable {
		return occupant.Substrate{Herdr: adoptFakeHerdr{pane: pane, infoErr: syscall.EACCES}, ProcRoot: root, Home: home}
	}
	if probeCase == adoptProbeMatch || probeCase == adoptProbeForeign {
		writeAdoptProc(t, root, 30, 10, "codex", home)
		sid := adoptSIDOld
		if probeCase == adoptProbeForeign {
			sid = adoptSIDForeign
		}
		transcript := filepath.Join(home, ".codex", "sessions", "2026", "08", "23", "rollout-now-"+sid+".jsonl")
		if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(transcript, filepath.Join(root, "30", "fd", "42")); err != nil {
			t.Fatal(err)
		}
	}
	return occupant.Substrate{Herdr: adoptFakeHerdr{pane: pane, info: info}, ProcRoot: root, Home: home}
}

func writeAdoptProc(t *testing.T, root string, pid, ppid int, comm, cwd string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"status":  fmt.Sprintf("Name:\t%s\nPPid:\t%d\nUid:\t1000\t1000\t1000\t1000\n", comm, ppid),
		"comm":    comm + "\n",
		"cmdline": comm + "\x00",
		"environ": "\x00",
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
		t.Fatal(err)
	}
}

func TestDifferentPaneRemedyIsAcceptedByParser(t *testing.T) {
	var stdout, stderr strings.Builder
	opts, code := parseArgs([]string{"guid-previous", "--confirm-dead"}, &stdout, &stderr)
	if code != 0 || opts.target != "guid-previous" || !opts.confirmDead {
		t.Fatalf("parseArgs = %+v, code %d, stderr %q", opts, code, stderr.String())
	}
}

func TestResumeSIDForAdoptionRequiresOccupantLineageMatch(t *testing.T) {
	old := v2.SessionRecord{
		GUID: "guid-previous",
		Tool: "codex",
		SIDs: []v2.SID{{SID: "sid-resumed"}},
	}
	matched := occupant.Observation{Status: occupant.Occupied, Tool: "codex", SID: "sid-resumed", Transcript: "/rollout/sid-resumed.jsonl", Evidence: []occupant.Signal{occupant.SignalFD}}
	if got := resumeSIDForAdoption(matched, old); got != "sid-resumed" {
		t.Fatalf("resumeSIDForAdoption(match) = %q", got)
	}
	mismatched := matched
	mismatched.SID = "sid-foreign"
	if got := resumeSIDForAdoption(mismatched, old); got != "" {
		t.Fatalf("resumeSIDForAdoption(mismatch) = %q, want empty", got)
	}
}

func TestRemovedResumedSessionFlagIsRejected(t *testing.T) {
	var stdout, stderr strings.Builder
	if _, code := parseArgs([]string{"guid-previous", "--confirm-resumed-session"}, &stdout, &stderr); code == 0 {
		t.Fatalf("removed flag parsed successfully")
	}
}

func TestAdoptHelpKeepsConfirmDeadAndDropsResumedAssertion(t *testing.T) {
	var help strings.Builder
	printHelp(&help)
	if !strings.Contains(help.String(), "--confirm-dead") {
		t.Fatalf("help dropped --confirm-dead: %q", help.String())
	}
	if strings.Contains(help.String(), "--confirm-resumed-session") {
		t.Fatalf("help retained removed resumed-session assertion: %q", help.String())
	}
}

func seedAdoptRegistry(t *testing.T, recs ...v2.SessionRecord) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		for i := range recs {
			recs[i].Kind = v2.KindSession
			recs[i].Event = "registered"
			recs[i].RecordedAt = "2026-07-12T00:00:00Z"
		}
		return recs, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func mustReadAdoptRegistry(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
