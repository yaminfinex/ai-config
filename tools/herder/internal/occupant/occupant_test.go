package occupant

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry/v2"
)

const (
	sidA = "11111111-1111-4111-8111-111111111111"
	sidB = "22222222-2222-4222-8222-222222222222"
)

type fakeHerdr struct {
	pane  herdrcli.Pane
	panes []herdrcli.Pane
	info  herdrcli.ProcessInfo
	err   error
}

func (f fakeHerdr) Pane(id string) (herdrcli.Pane, error) {
	if f.err != nil {
		return herdrcli.Pane{}, f.err
	}
	if f.pane.PaneID == id {
		return f.pane, nil
	}
	for _, p := range f.panes {
		if p.PaneID == id {
			return p, nil
		}
	}
	return herdrcli.Pane{}, os.ErrNotExist
}
func (f fakeHerdr) Panes() ([]herdrcli.Pane, error)                  { return f.panes, f.err }
func (f fakeHerdr) ProcessInfo(string) (herdrcli.ProcessInfo, error) { return f.info, f.err }

type fixture struct{ root, home string }

func newFixture(t *testing.T) fixture {
	t.Helper()
	d := t.TempDir()
	f := fixture{root: filepath.Join(d, "proc"), home: filepath.Join(d, "home")}
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) proc(t *testing.T, pid, ppid int, comm, cwd, guid string) {
	t.Helper()
	d := filepath.Join(f.root, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(d, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		if err := os.WriteFile(filepath.Join(d, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("status", fmt.Sprintf("Name:\t%s\nPPid:\t%d\nUid:\t1000\t1000\t1000\t1000\n", comm, ppid))
	write("comm", comm+"\n")
	write("cmdline", comm+"\x00")
	write("environ", "HERDER_GUID="+guid+"\x00")
	if cwd != "" {
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(cwd, filepath.Join(d, "cwd")); err != nil {
			t.Fatal(err)
		}
	}
}

func (f fixture) transcript(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func baseTree(t *testing.T, f fixture, tool string) (herdrcli.Pane, herdrcli.ProcessInfo, string) {
	cwd := filepath.Join(f.home, "work", "repo")
	f.proc(t, 10, 1, "bash", cwd, "")
	f.proc(t, 20, 10, "hcom", cwd, "")
	f.proc(t, 30, 20, tool, cwd, "guid-a")
	return herdrcli.Pane{PaneID: "pane-a", Agent: tool}, herdrcli.ProcessInfo{
		ForegroundProcessGroupID: 10,
		Processes:                []herdrcli.Process{{PID: 10, Name: "bash"}},
	}, cwd
}

func TestProbeHappyClaude(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "claude")
	pane.AgentSession = sidA
	path := filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidA+".jsonl")
	f.transcript(t, path)
	obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
	if obs.Status != Occupied || obs.SID != sidA || obs.Transcript != path || obs.PID != 30 {
		t.Fatalf("Probe = %+v", obs)
	}
	if len(obs.Evidence) < 2 || obs.Evidence[0] != SignalCohort || obs.Evidence[1] != SignalAgentSession {
		t.Fatalf("evidence = %v", obs.Evidence)
	}
}

func TestProbeHappyCodexUsesLeafHolder(t *testing.T) {
	f := newFixture(t)
	pane, info, _ := baseTree(t, f, "codex")
	path := filepath.Join(f.home, ".codex", "sessions", "2026", "08", "23", "rollout-now-"+sidA+".jsonl")
	f.transcript(t, path)
	// The wrapper inherited the same fd. The descendant codex holder wins.
	if err := os.Symlink(path, filepath.Join(f.root, "20", "fd", "41")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(f.root, "30", "fd", "42")); err != nil {
		t.Fatal(err)
	}
	obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
	if obs.Status != Occupied || obs.SID != sidA || obs.PID != 30 {
		t.Fatalf("Probe = %+v", obs)
	}
}

func TestVerdictResumedLineageIsStaleMatch(t *testing.T) {
	row := v2.SessionRecord{SIDs: []v2.SID{{SID: sidA}, {SID: sidB}}}
	got := Verdict(Observation{Status: Occupied, SID: sidA}, row)
	if got.Status != Match || got.MatchAge != Stale {
		t.Fatalf("Verdict = %+v", got)
	}
}

func TestVerdictPositiveMismatchForeign(t *testing.T) {
	got := Verdict(Observation{Status: Occupied, SID: sidB}, v2.SessionRecord{SIDs: []v2.SID{{SID: sidA}}})
	if got.Status != PositiveMismatch || got.SID != sidB {
		t.Fatalf("Verdict = %+v", got)
	}
}

func TestProbeNoOccupantVariants(t *testing.T) {
	t.Run("vacant", func(t *testing.T) {
		f := newFixture(t)
		f.proc(t, 10, 1, "bash", f.home, "")
		pane := herdrcli.Pane{PaneID: "pane-a"}
		obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: herdrcli.ProcessInfo{Processes: []herdrcli.Process{{PID: 10}}}}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
		out := Verdict(obs, v2.SessionRecord{})
		if out.Status != NoOccupant || out.Reason != ReasonVacant {
			t.Fatalf("obs/out = %+v / %+v", obs, out)
		}
	})
	t.Run("pane-gone", func(t *testing.T) {
		obs := Probe(Substrate{Herdr: fakeHerdr{}, ProcRoot: t.TempDir(), Home: t.TempDir()}, "gone")
		out := Verdict(obs, v2.SessionRecord{})
		if out.Status != NoOccupant || out.Reason != ReasonPaneGone {
			t.Fatalf("obs/out = %+v / %+v", obs, out)
		}
	})
}

func TestProbeClaudeDetectionLostMultiTranscriptIsAmbiguous(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "claude")
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidA+".jsonl"))
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidB+".jsonl"))
	obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
	if obs.Status != Ambiguous {
		t.Fatalf("Probe = %+v", obs)
	}
}

func TestProbeUnprobeableToolsPassThrough(t *testing.T) {
	for _, tool := range []string{"grok", "pi"} {
		t.Run(tool, func(t *testing.T) {
			f := newFixture(t)
			f.proc(t, 10, 1, "bash", f.home, "")
			pane := herdrcli.Pane{PaneID: "pane-a", Agent: tool}
			obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: herdrcli.ProcessInfo{Processes: []herdrcli.Process{{PID: 10}}}}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
			if Verdict(obs, v2.SessionRecord{}).Status != OutcomeUnprobeable {
				t.Fatalf("Probe = %+v", obs)
			}
		})
	}
}

func TestProbeRequiresClaudeTranscriptArtifact(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "claude")
	pane.AgentSession = sidA
	// Another artifact in the process cohort proves that the report-only sid
	// cannot be accepted, while leaving the true occupant ambiguous.
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidB+".jsonl"))
	obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
	if obs.Status != Ambiguous || obs.SID != "" {
		t.Fatalf("hint alone proved identity: %+v", obs)
	}
}

func TestProcRootEnvironmentHook(t *testing.T) {
	f := newFixture(t)
	pane, info, _ := baseTree(t, f, "codex")
	t.Setenv(ProcRootEnv, f.root)
	obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, Home: f.home}, pane.PaneID)
	if obs.Status != Vacant || obs.Tool != "codex" {
		t.Fatalf("Probe = %+v", obs)
	}
}

func TestSelfProbeAncestry(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "claude")
	pane.AgentSession = sidA
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidA+".jsonl"))
	// Represent this go test process below the occupant in the injected tree.
	f.proc(t, os.Getpid(), 30, "occupant.test", cwd, "")
	t.Setenv("HERDR_PANE_ID", "stale-pane")
	h := fakeHerdr{pane: pane, panes: []herdrcli.Pane{pane}, info: info}
	obs := SelfProbe(Substrate{Herdr: h, ProcRoot: f.root, Home: f.home})
	if obs.Status != Occupied || obs.SID != sidA || obs.Evidence[len(obs.Evidence)-1] != SignalAncestry {
		t.Fatalf("SelfProbe = %+v", obs)
	}
}

func TestSelfProbeForeignTreeFailsClosed(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "claude")
	pane.AgentSession = sidA
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidA+".jsonl"))
	f.proc(t, os.Getpid(), 99, "occupant.test", cwd, "")
	h := fakeHerdr{pane: pane, panes: []herdrcli.Pane{pane}, info: info}
	obs := SelfProbe(Substrate{Herdr: h, ProcRoot: f.root, Home: f.home})
	if obs.Status != Vacant || obs.SID != "" {
		t.Fatalf("foreign tree accepted: %+v", obs)
	}
}

func TestSelfProbeWorksWithoutPaneInventory(t *testing.T) {
	f := newFixture(t)
	cwd := filepath.Join(f.home, "work", "repo")
	f.proc(t, 30, 1, "codex", cwd, "guid-a")
	f.proc(t, os.Getpid(), 30, "occupant.test", cwd, "")
	path := filepath.Join(f.home, ".codex", "sessions", "rollout-now-"+sidA+".jsonl")
	f.transcript(t, path)
	if err := os.Symlink(path, filepath.Join(f.root, "30", "fd", "42")); err != nil {
		t.Fatal(err)
	}
	obs := SelfProbe(Substrate{Herdr: fakeHerdr{panes: []herdrcli.Pane{}}, ProcRoot: f.root, Home: f.home})
	if obs.Status != Occupied || obs.SID != sidA || obs.Pane.PaneID != "" {
		t.Fatalf("SelfProbe = %+v", obs)
	}
}

func TestRecordedSIDSetExcludesCrossRowLineage(t *testing.T) {
	row := v2.SessionRecord{SIDs: []v2.SID{{SID: sidA}}, Provenance: v2.Provenance{ToolSessionID: sidB}, Lineage: v2.Lineage{ForkedFrom: "guid-parent"}}
	got := RecordedSIDSet(row)
	if len(got) != 2 || got[0] != sidA || got[1] != sidB {
		t.Fatalf("RecordedSIDSet = %v", got)
	}
}

func TestLiveHerdrProcessInfoAndSelfProbe(t *testing.T) {
	if os.Getenv("HERDR_PANE_ID") == "" {
		t.Skip("live smoke: no HERDR_PANE_ID")
	}
	if _, err := exec.LookPath("herdr"); err != nil {
		t.Skip("live smoke: herdr unavailable")
	}
	q := CLIQuerier{}
	info, err := q.ProcessInfo(os.Getenv("HERDR_PANE_ID"))
	if err != nil {
		t.Fatalf("live process-info flag form: %v", err)
	}
	if info.ForegroundProcessGroupID == 0 && len(info.Processes) == 0 {
		t.Fatal("live process-info returned no anchors (possible CLI verb drift)")
	}
	paneObs := Probe(Substrate{Herdr: q}, os.Getenv("HERDR_PANE_ID"))
	t.Logf("live pane Probe = %s err=%v", paneObs, paneObs.Err)
	obs := SelfProbe(Substrate{Herdr: q})
	if obs.Status != Occupied {
		t.Fatalf("live SelfProbe = %s", obs)
	}
}
