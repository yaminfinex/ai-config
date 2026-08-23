package occupant

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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
	infos map[string]herdrcli.ProcessInfo
	err   error
}

type retryHerdr struct {
	pane  herdrcli.Pane
	info  herdrcli.ProcessInfo
	calls int
}

func (f *retryHerdr) Pane(string) (herdrcli.Pane, error) { return f.pane, nil }
func (f *retryHerdr) Panes() ([]herdrcli.Pane, error)    { return []herdrcli.Pane{f.pane}, nil }
func (f *retryHerdr) ProcessInfo(string) (herdrcli.ProcessInfo, error) {
	f.calls++
	return f.info, nil
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
func (f fakeHerdr) Panes() ([]herdrcli.Pane, error) { return f.panes, f.err }
func (f fakeHerdr) ProcessInfo(id string) (herdrcli.ProcessInfo, error) {
	if info, ok := f.infos[id]; ok {
		return info, f.err
	}
	return f.info, f.err
}

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

func TestProbeClaudeMatchingWitnessesCollapseToArtifact(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "claude")
	pane.AgentSession = sidA
	// A nested claude process sees the same cohort and artifact. Witness
	// count is two, but the proven answer is still exactly one transcript.
	f.proc(t, 31, 30, "claude", cwd, "guid-nested")
	path := filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidA+".jsonl")
	f.transcript(t, path)
	obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
	if obs.Status != Occupied || obs.SID != sidA || obs.PID != 30 {
		t.Fatalf("Probe = %+v", obs)
	}
}

func TestProbeClaudeMungeCollisionRemainsAmbiguous(t *testing.T) {
	f := newFixture(t)
	cwdA := filepath.Join(f.home, "a-b")
	cwdB := filepath.Join(f.home, "a", "b")
	if mungeCWD(cwdA) != mungeCWD(cwdB) {
		t.Fatal("fixture paths do not collide")
	}
	f.proc(t, 10, 1, "bash", cwdA, "")
	f.proc(t, 30, 10, "claude", cwdA, "guid-a")
	f.proc(t, 31, 10, "claude", cwdB, "guid-b")
	pane := herdrcli.Pane{PaneID: "pane-a", Agent: "claude", AgentSession: sidA}
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwdA), sidA+".jsonl"))
	obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: herdrcli.ProcessInfo{ForegroundProcessGroupID: 10}}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
	if obs.Status != Ambiguous {
		t.Fatalf("Probe = %+v", obs)
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

func TestProbeRequeriesOnceWhenToolPIDVanishes(t *testing.T) {
	f := newFixture(t)
	f.proc(t, 10, 1, "bash", f.home, "")
	// A missing cwd link simulates the process disappearing after the status
	// sweep but before permission-sensitive detail reads.
	f.proc(t, 30, 10, "codex", "", "guid-a")
	h := &retryHerdr{
		pane: herdrcli.Pane{PaneID: "pane-a", Agent: "codex"},
		info: herdrcli.ProcessInfo{ForegroundProcessGroupID: 10},
	}
	obs := Probe(Substrate{Herdr: h, ProcRoot: f.root, Home: f.home}, "pane-a")
	if h.calls != 2 || obs.Status != Vacant {
		t.Fatalf("calls/Probe = %d / %+v", h.calls, obs)
	}
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

func TestProbeClaudeDetectionLostUsesActiveWindow(t *testing.T) {
	t.Run("single-active", func(t *testing.T) {
		f := newFixture(t)
		pane, info, cwd := baseTree(t, f, "claude")
		path := filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidA+".jsonl")
		f.transcript(t, path)
		obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
		if obs.Status != Occupied || obs.SID != sidA || len(obs.Evidence) == 0 || obs.Evidence[0] != SignalCohort {
			t.Fatalf("Probe = %+v", obs)
		}
	})
	t.Run("stale-history-filtered", func(t *testing.T) {
		f := newFixture(t)
		pane, info, cwd := baseTree(t, f, "claude")
		dir := filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd))
		active := filepath.Join(dir, sidA+".jsonl")
		stale := filepath.Join(dir, sidB+".jsonl")
		f.transcript(t, active)
		f.transcript(t, stale)
		newest := time.Unix(1_700_000_000, 0)
		if err := os.Chtimes(active, newest, newest); err != nil {
			t.Fatal(err)
		}
		old := newest.Add(-claudeActivityWindow - time.Second)
		if err := os.Chtimes(stale, old, old); err != nil {
			t.Fatal(err)
		}
		obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
		if obs.Status != Occupied || obs.SID != sidA {
			t.Fatalf("Probe = %+v", obs)
		}
	})
}

func TestProbeNestedUnprovenToolDoesNotVetoExactArtifact(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "codex")
	pane.AgentSession = sidA
	rollout := filepath.Join(f.home, ".codex", "sessions", "rollout-now-"+sidA+".jsonl")
	f.transcript(t, rollout)
	if err := os.Symlink(rollout, filepath.Join(f.root, "30", "fd", "42")); err != nil {
		t.Fatal(err)
	}
	// The nested Claude has cohort history but no artifact matching the pane's
	// Codex agent_session report, so it is not a proven competing occupant.
	f.proc(t, 40, 30, "claude", cwd, "guid-nested")
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidB+".jsonl"))
	obs := Probe(Substrate{Herdr: fakeHerdr{pane: pane, info: info}, ProcRoot: f.root, Home: f.home}, pane.PaneID)
	if obs.Status != Occupied || obs.Tool != "codex" || obs.SID != sidA {
		t.Fatalf("Probe = %+v", obs)
	}
}

func TestProbeDistinctProvenNestedArtifactsAreAmbiguous(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "claude")
	pane.AgentSession = sidA
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidA+".jsonl"))
	f.proc(t, 40, 30, "codex", cwd, "guid-nested")
	rollout := filepath.Join(f.home, ".codex", "sessions", "rollout-now-"+sidB+".jsonl")
	f.transcript(t, rollout)
	if err := os.Symlink(rollout, filepath.Join(f.root, "40", "fd", "42")); err != nil {
		t.Fatal(err)
	}
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
	if obs.Status != Vacant {
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

func TestSelfProbeDifferentPaneProducesPositiveMismatch(t *testing.T) {
	f := newFixture(t)
	cwdA := filepath.Join(f.home, "work", "a")
	cwdB := filepath.Join(f.home, "work", "b")
	f.proc(t, 10, 1, "bash", cwdA, "")
	f.proc(t, 30, 10, "claude", cwdA, "guid-a")
	f.proc(t, 50, 1, "bash", cwdB, "")
	f.proc(t, 60, 50, "claude", cwdB, "guid-b")
	f.proc(t, os.Getpid(), 60, "occupant.test", cwdB, "")
	paneA := herdrcli.Pane{PaneID: "pane-a", Agent: "claude", AgentSession: sidA}
	paneB := herdrcli.Pane{PaneID: "pane-b", Agent: "claude", AgentSession: sidB}
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwdA), sidA+".jsonl"))
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwdB), sidB+".jsonl"))
	h := fakeHerdr{
		panes: []herdrcli.Pane{paneA, paneB},
		infos: map[string]herdrcli.ProcessInfo{
			"pane-a": {ForegroundProcessGroupID: 10},
			"pane-b": {ForegroundProcessGroupID: 50},
		},
	}
	t.Setenv("HERDR_PANE_ID", "pane-a") // positive but foreign entry hint
	obs := SelfProbe(Substrate{Herdr: h, ProcRoot: f.root, Home: f.home})
	out := Verdict(obs, v2.SessionRecord{SIDs: []v2.SID{{SID: sidA}}})
	if obs.Pane.PaneID != "pane-b" || obs.SID != sidB || out.Status != PositiveMismatch {
		t.Fatalf("SelfProbe/Verdict = %+v / %+v", obs, out)
	}
}

func TestIncident268LibraryEssenceReplacementDoesNotMatchOldRow(t *testing.T) {
	f := newFixture(t)
	cwd := filepath.Join(f.home, "replacement")
	f.proc(t, 10, 1, "bash", f.home, "")
	f.proc(t, 50, 1, "bash", cwd, "")
	f.proc(t, 60, 50, "claude", cwd, "guid-new")
	oldPane := herdrcli.Pane{PaneID: "pane-old"}
	newPane := herdrcli.Pane{PaneID: "pane-new", Agent: "claude", AgentSession: sidB}
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidB+".jsonl"))
	h := fakeHerdr{
		panes: []herdrcli.Pane{oldPane, newPane},
		infos: map[string]herdrcli.ProcessInfo{
			"pane-old": {ForegroundProcessGroupID: 10},
			"pane-new": {ForegroundProcessGroupID: 50},
		},
	}
	sub := Substrate{Herdr: h, ProcRoot: f.root, Home: f.home}
	oldObs := Probe(sub, oldPane.PaneID)
	newObs := Probe(sub, newPane.PaneID)
	newVerdict := Verdict(newObs, v2.SessionRecord{SIDs: []v2.SID{{SID: sidA}}})
	if oldObs.Status != Vacant || newObs.SID != sidB || newVerdict.Status != PositiveMismatch {
		t.Fatalf("old/new/verdict = %+v / %+v / %+v", oldObs, newObs, newVerdict)
	}
}

func TestIncident041LibraryEssenceDetectionLostSelfLocation(t *testing.T) {
	f := newFixture(t)
	pane, info, cwd := baseTree(t, f, "claude") // agent_session intentionally absent
	f.transcript(t, filepath.Join(f.home, ".claude", "projects", mungeCWD(cwd), sidA+".jsonl"))
	f.proc(t, os.Getpid(), 30, "occupant.test", cwd, "")
	t.Setenv("HERDR_PANE_ID", pane.PaneID)
	h := fakeHerdr{pane: pane, panes: []herdrcli.Pane{pane}, info: info}
	obs := SelfProbe(Substrate{Herdr: h, ProcRoot: f.root, Home: f.home})
	out := Verdict(obs, v2.SessionRecord{SIDs: []v2.SID{{SID: sidA}}})
	if out.Status != Match || obs.SID != sidA || obs.Evidence[0] != SignalCohort {
		t.Fatalf("SelfProbe/Verdict = %+v / %+v", obs, out)
	}
}

func TestIncident262LibraryEssenceIgnoresBusCopies(t *testing.T) {
	f := newFixture(t)
	cwd := filepath.Join(f.home, "work", "repo")
	f.proc(t, 30, 1, "codex", cwd, "guid-a")
	f.proc(t, os.Getpid(), 30, "occupant.test", cwd, "")
	path := filepath.Join(f.home, ".codex", "sessions", "rollout-now-"+sidA+".jsonl")
	f.transcript(t, path)
	if err := os.Symlink(path, filepath.Join(f.root, "30", "fd", "42")); err != nil {
		t.Fatal(err)
	}
	obs := SelfProbe(Substrate{Herdr: fakeHerdr{}, ProcRoot: f.root, Home: f.home})
	verified := false
	row := v2.SessionRecord{SIDs: []v2.SID{{SID: sidA}}, Seat: &v2.Seat{HcomVerified: &verified}}
	out := Verdict(obs, row)
	if out.Status != Match || obs.SID != sidA {
		t.Fatalf("SelfProbe/Verdict = %+v / %+v", obs, out)
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
