// Package occupant proves the tool session occupying a live herdr pane.
//
// The package deliberately does not read registry seat coordinates. A pane
// id is only an entry point for a fresh herdr snapshot; identity comes from
// the occupant process and its transcript artifact.
package occupant

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry/v2"
)

const (
	ProcRootEnv = "HERDER_PROBE_PROC_ROOT"
	SelfPIDEnv  = "HERDER_PROBE_SELF_PID"
)

// Claude does not expose an exact pid-to-transcript join. In the
// detection-lost leg, keep only the newest activity cohort: transcripts
// outside this window behind the newest mtime are historical, while two
// transcripts active inside the window remain honestly ambiguous. The
// window is relative to the newest artifact rather than wall-clock now so
// an idle-but-live pane does not become vacant merely because no turn was
// written recently.
const claudeActivityWindow = 5 * time.Minute

type Status string

const (
	Occupied    Status = "OCCUPIED"
	Vacant      Status = "VACANT"
	PaneGone    Status = "PANE_GONE"
	Ambiguous   Status = "AMBIGUOUS"
	Unprobeable Status = "UNPROBEABLE"
)

type Signal string

const (
	SignalFD           Signal = "fd"
	SignalCohort       Signal = "cohort"
	SignalAgentSession Signal = "agent_session"
	SignalEnvironGUID  Signal = "environ-guid"
	SignalAncestry     Signal = "ancestry"
)

type Observation struct {
	Pane       herdrcli.Pane
	Tool       string
	PID        int
	Transcript string
	SID        string
	Evidence   []Signal
	Status     Status
	Err        error
	// provenCandidates is internal decision metadata used when independent
	// tool legs must be collapsed. It never substitutes for SID/artifact
	// evidence exposed to callers.
	provenCandidates int
}

type OutcomeStatus string

const (
	Match              OutcomeStatus = "MATCH"
	PositiveMismatch   OutcomeStatus = "POSITIVE-MISMATCH"
	NoOccupant         OutcomeStatus = "NO-OCCUPANT"
	OutcomeAmbiguous   OutcomeStatus = "AMBIGUOUS"
	OutcomeUnprobeable OutcomeStatus = "UNPROBEABLE"
)

type MatchAge string

const (
	Current MatchAge = "current"
	Stale   MatchAge = "stale"
)

type NoOccupantReason string

const (
	ReasonVacant   NoOccupantReason = "vacant"
	ReasonPaneGone NoOccupantReason = "pane_gone"
)

type Outcome struct {
	Status   OutcomeStatus
	MatchAge MatchAge
	Reason   NoOccupantReason
	SID      string
}

// HerdrQuerier is the complete live substrate seam used by Probe and
// SelfProbe. Tests inject snapshots; CLIQuerier supplies the production CLI.
type HerdrQuerier interface {
	Pane(string) (herdrcli.Pane, error)
	Panes() ([]herdrcli.Pane, error)
	ProcessInfo(string) (herdrcli.ProcessInfo, error)
}

type Substrate struct {
	Herdr    HerdrQuerier
	ProcRoot string
	Home     string
	// SelfPID makes SelfProbe hermetic with an injected ProcRoot. Production
	// leaves it zero and uses os.Getpid. The environment equivalent is honored
	// only alongside an injected proc root so accidental inheritance cannot
	// redirect a production probe into a foreign process tree.
	SelfPID int
}

// CLIQuerier invokes the field-verified herdr 0.8 CLI spelling. In
// particular, process-info takes --pane; the positional form returns help
// with a successful exit code and must never be used.
type CLIClient interface {
	Output(...string) ([]byte, error)
}

type CLIQuerier struct{ Client CLIClient }

func (q CLIQuerier) client() CLIClient {
	if q.Client != nil {
		return q.Client
	}
	return &herdrcli.Client{}
}

func (q CLIQuerier) Pane(id string) (herdrcli.Pane, error) {
	out, err := q.client().Output("pane", "get", id)
	if err != nil {
		return herdrcli.Pane{}, err
	}
	p, err := herdrcli.ParsePaneGet(out)
	if err != nil {
		return p, err
	}
	if p.PaneID == "" {
		return p, os.ErrNotExist
	}
	return p, nil
}

func (q CLIQuerier) Panes() ([]herdrcli.Pane, error) {
	out, err := q.client().Output("pane", "list")
	if err != nil {
		return nil, err
	}
	return herdrcli.ParsePaneList(out)
}

func (q CLIQuerier) ProcessInfo(id string) (herdrcli.ProcessInfo, error) {
	out, err := q.client().Output("pane", "process-info", "--pane", id)
	if err != nil {
		return herdrcli.ProcessInfo{}, err
	}
	return herdrcli.ParseProcessInfo(out)
}

func normalized(sub Substrate) Substrate {
	if sub.Herdr == nil {
		sub.Herdr = CLIQuerier{}
	}
	if sub.ProcRoot == "" {
		sub.ProcRoot = os.Getenv(ProcRootEnv)
	}
	if sub.ProcRoot == "" {
		sub.ProcRoot = "/proc"
	}
	if sub.Home == "" {
		sub.Home, _ = os.UserHomeDir()
	}
	return sub
}

func Probe(sub Substrate, paneID string) Observation {
	sub = normalized(sub)
	pane, err := sub.Herdr.Pane(paneID)
	if err != nil || pane.PaneID == "" {
		return Observation{Status: PaneGone, Err: err}
	}
	info, err := sub.Herdr.ProcessInfo(pane.PaneID)
	if err != nil {
		return Observation{Pane: pane, Status: Unprobeable, Err: err}
	}
	obs := probeSnapshot(sub, pane, info)
	if errors.Is(obs.Err, os.ErrNotExist) || errors.Is(obs.Err, syscall.ENOENT) {
		// A /proc entry may vanish between the herdr snapshot and descent.
		// Re-query exactly once before classifying the pane as vacant.
		if retry, retryErr := sub.Herdr.ProcessInfo(pane.PaneID); retryErr == nil {
			obs = probeSnapshot(sub, pane, retry)
		}
	}
	return obs
}

// SelfProbe locates the caller from live process ancestry. HERDR_PANE_ID is
// only a fast entry hint; if it is stale or absent every live pane is tried.
func SelfProbe(sub Substrate) Observation {
	self := selfProbePID(sub)
	sub = normalized(sub)
	if hint := os.Getenv("HERDR_PANE_ID"); hint != "" {
		obs := Probe(sub, hint)
		if obs.Status == Occupied && isAncestor(sub.ProcRoot, obs.PID, self) {
			obs.Evidence = append(obs.Evidence, SignalAncestry)
			return obs
		}
	}
	panes, err := sub.Herdr.Panes()
	if err != nil {
		return Observation{Status: Unprobeable, Err: err}
	}
	var matches []Observation
	for _, pane := range panes {
		obs := Probe(sub, pane.PaneID)
		if obs.Status == Occupied && isAncestor(sub.ProcRoot, obs.PID, self) {
			obs.Evidence = append(obs.Evidence, SignalAncestry)
			matches = append(matches, obs)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	if len(matches) > 1 {
		return Observation{Status: Ambiguous}
	}
	// A caller outside herdr's pane inventory (for example an ssh or cron
	// command invoked beneath a tool) can still prove identity pid-first.
	// There are deliberately no seat coordinates to heal in this case.
	if obs := probeSelfAncestry(sub, self); obs.Status == Occupied || obs.Status == Ambiguous || obs.Status == Unprobeable {
		return obs
	}
	return Observation{Status: Vacant}
}

func selfProbePID(sub Substrate) int {
	if sub.SelfPID != 0 {
		return sub.SelfPID
	}
	procRootInjected := os.Getenv(ProcRootEnv) != "" || (sub.ProcRoot != "" && sub.ProcRoot != "/proc")
	if procRootInjected {
		if pid, err := strconv.Atoi(os.Getenv(SelfPIDEnv)); err == nil && pid != 0 {
			return pid
		}
	}
	return os.Getpid()
}

func probeSelfAncestry(sub Substrate, self int) Observation {
	entries, err := scanProc(sub.ProcRoot)
	if err != nil {
		return Observation{Status: Unprobeable, Err: err}
	}
	byPID := map[int]procEntry{}
	for _, e := range entries {
		byPID[e.pid] = e
	}
	for pid, hops := self, 0; pid > 0 && hops < 64; hops++ {
		e, ok := byPID[pid]
		if !ok {
			return Observation{Status: Vacant}
		}
		if toolName(e.comm) != "" {
			obs := probeSnapshot(sub, herdrcli.Pane{}, herdrcli.ProcessInfo{ForegroundProcessGroupID: e.pid})
			if obs.Status == Occupied {
				obs.Evidence = append(obs.Evidence, SignalAncestry)
			}
			return obs
		}
		if e.ppid == pid {
			break
		}
		pid = e.ppid
	}
	return Observation{Status: Vacant}
}

func Verdict(obs Observation, row v2.SessionRecord) Outcome {
	switch obs.Status {
	case Vacant:
		return Outcome{Status: NoOccupant, Reason: ReasonVacant}
	case PaneGone:
		return Outcome{Status: NoOccupant, Reason: ReasonPaneGone}
	case Ambiguous:
		return Outcome{Status: OutcomeAmbiguous}
	case Unprobeable:
		return Outcome{Status: OutcomeUnprobeable}
	}
	if obs.Status != Occupied || obs.SID == "" {
		return Outcome{Status: OutcomeAmbiguous}
	}
	recorded := map[string]bool{}
	for _, sid := range row.SIDs {
		if sid.SID != "" {
			recorded[sid.SID] = true
		}
	}
	if row.Provenance.ToolSessionID != "" {
		recorded[row.Provenance.ToolSessionID] = true
	}
	if !recorded[obs.SID] {
		return Outcome{Status: PositiveMismatch, SID: obs.SID}
	}
	age := Stale
	if len(row.SIDs) > 0 && row.SIDs[len(row.SIDs)-1].SID == obs.SID {
		age = Current
	}
	if len(row.SIDs) == 0 && row.Provenance.ToolSessionID == obs.SID {
		age = Current
	}
	return Outcome{Status: Match, MatchAge: age, SID: obs.SID}
}

type procEntry struct {
	pid, ppid       int
	comm, cwd, guid string
}

func probeSnapshot(sub Substrate, pane herdrcli.Pane, info herdrcli.ProcessInfo) Observation {
	anchors := map[int]bool{}
	addProcessInfoAnchors(anchors, info)
	entries, permissionErr := scanProc(sub.ProcRoot)
	if permissionErr != nil {
		if errors.Is(permissionErr, os.ErrNotExist) || errors.Is(permissionErr, syscall.ENOENT) {
			return Observation{Pane: pane, Status: Vacant, Err: permissionErr}
		}
		return Observation{Pane: pane, Status: Unprobeable, Err: permissionErr}
	}
	var tools []procEntry
	vanished := false
	for _, e := range entries {
		if !descendsFrom(e.pid, anchors, entries) {
			continue
		}
		if toolName(e.comm) != "" {
			detailed, detailErr := procDetails(sub.ProcRoot, e)
			if detailErr != nil {
				if errors.Is(detailErr, os.ErrPermission) || errors.Is(detailErr, syscall.EACCES) {
					return Observation{Pane: pane, Tool: toolName(e.comm), PID: e.pid, Status: Unprobeable, Err: detailErr}
				}
				if errors.Is(detailErr, os.ErrNotExist) || errors.Is(detailErr, syscall.ENOENT) {
					vanished = true
				}
				continue
			}
			tools = append(tools, detailed)
		}
	}
	if len(tools) == 0 {
		for _, p := range info.Processes {
			name := p.Name
			if name == "" && len(p.Argv) > 0 {
				name = filepath.Base(p.Argv[0])
			}
			if toolName(name) != "" {
				return Observation{Pane: pane, Tool: toolName(name), PID: p.PID, Status: Unprobeable}
			}
		}
		if toolName(pane.Agent) == "" && pane.Agent != "" {
			return Observation{Pane: pane, Tool: pane.Agent, Status: Unprobeable}
		}
		obs := Observation{Pane: pane, Status: Vacant}
		if vanished {
			obs.Err = syscall.ENOENT
		}
		return obs
	}
	kinds := map[string]bool{}
	for _, e := range tools {
		kinds[toolName(e.comm)] = true
	}
	var legs []Observation
	if kinds["codex"] {
		legs = append(legs, probeCodex(sub, pane, entries, anchors))
	}
	if kinds["claude"] {
		legs = append(legs, probeClaude(sub, pane, tools, entries, anchors))
	}
	obs := collapseProvenLegs(pane, legs)
	if obs.Status == Vacant && vanished {
		obs.Err = syscall.ENOENT
	}
	return obs
}

func addProcessInfoAnchors(anchors map[int]bool, info herdrcli.ProcessInfo) {
	if info.ShellPID > 0 {
		anchors[info.ShellPID] = true
	}
	if info.ForegroundProcessGroupID > 0 {
		anchors[info.ForegroundProcessGroupID] = true
	}
	for _, p := range info.Processes {
		if p.PID > 0 {
			anchors[p.PID] = true
		}
	}
}

// collapseProvenLegs computes unanimity over answers, not witnesses. Nested
// tool subprocesses are common; an unproven nested leg cannot veto an exact
// occupant artifact, while two distinct proven artifacts fail closed.
func collapseProvenLegs(pane herdrcli.Pane, legs []Observation) Observation {
	proven := map[string]Observation{}
	status := Vacant
	for _, obs := range legs {
		switch obs.Status {
		case Occupied:
			key := obs.Tool + "\x00" + obs.SID + "\x00" + obs.Transcript
			proven[key] = obs
		case Ambiguous:
			if obs.provenCandidates > 1 {
				return Observation{Pane: pane, Status: Ambiguous, provenCandidates: obs.provenCandidates}
			}
			if status != Unprobeable {
				status = Ambiguous
			}
		case Unprobeable:
			status = Unprobeable
		}
	}
	if len(proven) == 1 {
		for _, obs := range proven {
			return obs
		}
	}
	if len(proven) > 1 {
		return Observation{Pane: pane, Status: Ambiguous, provenCandidates: len(proven)}
	}
	return Observation{Pane: pane, Status: status}
}

func scanProc(root string) ([]procEntry, error) {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []procEntry
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(root, d.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil {
			if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) {
				return nil, err
			}
			continue
		}
		ppid := statusPPID(raw)
		commRaw, _ := os.ReadFile(filepath.Join(dir, "comm"))
		comm := strings.TrimSpace(string(commRaw))
		if comm == "" {
			cmd, _ := os.ReadFile(filepath.Join(dir, "cmdline"))
			if first := strings.Split(string(cmd), "\x00"); len(first) > 0 {
				comm = filepath.Base(first[0])
			}
		}
		out = append(out, procEntry{pid: pid, ppid: ppid, comm: comm})
	}
	return out, nil
}

// procDetails is intentionally delayed until a process is proven to descend
// from this pane's anchors. A machine may contain other-user codex/claude
// processes whose cwd/environ are EACCES; those unrelated processes must not
// make a same-user pane unprobeable.
func procDetails(root string, e procEntry) (procEntry, error) {
	dir := filepath.Join(root, strconv.Itoa(e.pid))
	cwd, err := os.Readlink(filepath.Join(dir, "cwd"))
	if err != nil {
		return e, err
	}
	env, err := os.ReadFile(filepath.Join(dir, "environ"))
	if err != nil {
		return e, err
	}
	e.cwd = cwd
	e.guid = envValue(env, "HERDER_GUID")
	return e, nil
}

func statusPPID(raw []byte) int {
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "PPid:" {
			n, _ := strconv.Atoi(f[1])
			return n
		}
	}
	return 0
}

func envValue(raw []byte, key string) string {
	for _, kv := range strings.Split(string(raw), "\x00") {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

func descendsFrom(pid int, anchors map[int]bool, entries []procEntry) bool {
	byPID := map[int]int{}
	for _, e := range entries {
		byPID[e.pid] = e.ppid
	}
	for p, hops := pid, 0; p > 0 && hops < 64; hops++ {
		if anchors[p] {
			return true
		}
		next, ok := byPID[p]
		if !ok || next == p {
			break
		}
		p = next
	}
	return false
}

func isAncestor(root string, ancestor, pid int) bool {
	for p, hops := pid, 0; p > 0 && hops < 64; hops++ {
		if p == ancestor {
			return true
		}
		raw, err := os.ReadFile(filepath.Join(root, strconv.Itoa(p), "status"))
		if err != nil {
			return false
		}
		next := statusPPID(raw)
		if next == p {
			return false
		}
		p = next
	}
	return false
}

func toolName(name string) string {
	name = strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	switch {
	case name == "codex" || strings.HasPrefix(name, "codex-"):
		return "codex"
	case name == "claude" || strings.HasPrefix(name, "claude-"):
		return "claude"
	default:
		return ""
	}
}

var rolloutSID = regexp.MustCompile(`(?i)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)

func probeCodex(sub Substrate, pane herdrcli.Pane, all []procEntry, anchors map[int]bool) Observation {
	type candidate struct {
		procEntry
		path, sid string
	}
	var cs []candidate
	vanished := false
	// Inspect every process below the pane anchor, not only codex-named
	// processes: launch shells may inherit the rollout fd. Leaf-holder
	// disambiguation below removes those ancestors without guessing.
	for _, e := range all {
		if !descendsFrom(e.pid, anchors, all) {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(sub.ProcRoot, strconv.Itoa(e.pid), "fd"))
		if err != nil {
			if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) {
				return Observation{Pane: pane, Tool: "codex", PID: e.pid, Status: Unprobeable, Err: err}
			}
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) {
				vanished = true
			}
			continue
		}
		for _, fd := range fds {
			path, err := os.Readlink(filepath.Join(sub.ProcRoot, strconv.Itoa(e.pid), "fd", fd.Name()))
			if err != nil {
				continue
			}
			path = strings.TrimSuffix(path, " (deleted)")
			if !strings.Contains(filepath.ToSlash(path), "/.codex/sessions/") || !strings.HasPrefix(filepath.Base(path), "rollout-") {
				continue
			}
			m := rolloutSID.FindStringSubmatch(filepath.Base(path))
			if len(m) == 2 {
				cs = append(cs, candidate{e, path, m[1]})
			}
		}
	}
	if len(cs) == 0 {
		// A tool process without an open rollout is booting, exiting, or not a
		// session-bearing codex occupant. It supplies no occupant evidence.
		obs := Observation{Pane: pane, Tool: "codex", Status: Vacant}
		if vanished {
			obs.Err = syscall.ENOENT
		}
		return obs
	}
	holders := map[int]bool{}
	for _, c := range cs {
		holders[c.pid] = true
	}
	ancestor := map[int]bool{}
	byPID := map[int]int{}
	for _, e := range all {
		byPID[e.pid] = e.ppid
	}
	for _, c := range cs {
		for p, hops := c.ppid, 0; p > 1 && hops < 64; hops++ {
			if holders[p] {
				ancestor[p] = true
			}
			p = byPID[p]
		}
	}
	var leaves []candidate
	for _, c := range cs {
		if !ancestor[c.pid] {
			leaves = append(leaves, c)
		}
	}
	sids := map[string]bool{}
	for _, c := range leaves {
		sids[c.sid] = true
	}
	if len(sids) != 1 || len(leaves) == 0 {
		return Observation{Pane: pane, Tool: "codex", Status: Ambiguous, provenCandidates: len(sids)}
	}
	c := leaves[0]
	if toolName(c.comm) != "codex" {
		return Observation{Pane: pane, Tool: "codex", Status: Ambiguous}
	}
	detailed, err := procDetails(sub.ProcRoot, c.procEntry)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) {
			return Observation{Pane: pane, Tool: "codex", PID: c.pid, Status: Unprobeable, Err: err}
		}
		return Observation{Pane: pane, Tool: "codex", Status: Vacant, Err: err}
	}
	c.procEntry = detailed
	ev := []Signal{SignalFD}
	if c.guid != "" {
		ev = append(ev, SignalEnvironGUID)
	}
	return Observation{Pane: pane, Tool: "codex", PID: c.pid, Transcript: c.path, SID: c.sid, Evidence: ev, Status: Occupied}
}

var mungeRe = regexp.MustCompile(`[^A-Za-z0-9]`)

func mungeCWD(cwd string) string { return mungeRe.ReplaceAllString(cwd, "-") }

func probeClaude(sub Substrate, pane herdrcli.Pane, tools, all []procEntry, anchors map[int]bool) Observation {
	var claudes []procEntry
	for _, e := range tools {
		if toolName(e.comm) == "claude" {
			claudes = append(claudes, e)
		}
	}
	if pane.AgentSession != "" {
		var matches []procEntry
		var path string
		cohortCandidates := 0
		for _, e := range claudes {
			dir := filepath.Join(sub.Home, ".claude", "projects", mungeCWD(e.cwd))
			files, _ := claudeTranscripts(dir)
			cohortCandidates += len(files)
			p := filepath.Join(dir, pane.AgentSession+".jsonl")
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				matches = append(matches, e)
				path = p
			} else if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) {
				return Observation{Pane: pane, Tool: "claude", PID: e.pid, Status: Unprobeable, Err: err}
			}
		}
		if len(matches) > 0 {
			if !sameProcessCohort(matches) {
				return Observation{Pane: pane, Tool: "claude", Status: Ambiguous, provenCandidates: 2}
			}
			e := outermostProcess(sub.ProcRoot, matches)
			ev := []Signal{SignalCohort, SignalAgentSession}
			if e.guid != "" {
				ev = append(ev, SignalEnvironGUID)
			}
			return Observation{Pane: pane, Tool: "claude", PID: e.pid, Transcript: path, SID: pane.AgentSession, Evidence: ev, Status: Occupied}
		}
		if cohortCandidates == 0 {
			return Observation{Pane: pane, Tool: "claude", Status: Vacant}
		}
		return Observation{Pane: pane, Tool: "claude", Status: Ambiguous}
	}
	type candidate struct {
		procEntry
		path, sid string
		mtime     time.Time
	}
	var cs []candidate
	for _, e := range claudes {
		dir := filepath.Join(sub.Home, ".claude", "projects", mungeCWD(e.cwd))
		files, err := claudeTranscripts(dir)
		if err != nil {
			if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) {
				return Observation{Pane: pane, Tool: "claude", PID: e.pid, Status: Unprobeable, Err: err}
			}
			continue
		}
		for _, p := range files {
			st, statErr := os.Stat(p)
			if statErr != nil {
				if errors.Is(statErr, os.ErrPermission) || errors.Is(statErr, syscall.EACCES) {
					return Observation{Pane: pane, Tool: "claude", PID: e.pid, Status: Unprobeable, Err: statErr}
				}
				continue
			}
			sid := strings.TrimSuffix(filepath.Base(p), ".jsonl")
			if sid != "" {
				cs = append(cs, candidate{procEntry: e, path: p, sid: sid, mtime: st.ModTime()})
			}
		}
	}
	var newest time.Time
	otherPanes, panesErr := sub.Herdr.Panes()
	if panesErr != nil {
		return Observation{Pane: pane, Tool: "claude", Status: Unprobeable, Err: panesErr}
	}
	allPaneAnchors := map[int]bool{}
	for pid := range anchors {
		allPaneAnchors[pid] = true
	}
	for _, other := range otherPanes {
		if other.PaneID == pane.PaneID {
			continue
		}
		otherInfo, infoErr := sub.Herdr.ProcessInfo(other.PaneID)
		if infoErr != nil {
			return Observation{Pane: pane, Tool: "claude", Status: Unprobeable, Err: infoErr}
		}
		addProcessInfoAnchors(allPaneAnchors, otherInfo)
	}
	cohorts := map[string]bool{}
	for _, e := range claudes {
		cohorts[mungeCWD(e.cwd)] = true
	}
	for _, e := range all {
		if toolName(e.comm) != "claude" || descendsFrom(e.pid, allPaneAnchors, all) {
			continue
		}
		cwd, cwdErr := os.Readlink(filepath.Join(sub.ProcRoot, strconv.Itoa(e.pid), "cwd"))
		if cwdErr != nil {
			// Other-uid cwd links are unreadable and their transcripts live
			// under a different HOME, so they cannot collide with this cohort.
			// Vanished processes are the ordinary snapshot race.
			continue
		}
		if cohorts[mungeCWD(cwd)] {
			// A readable on-box Claude outside this pane's descent can write
			// any transcript in the shared cohort, so recency cannot attribute
			// a SID to this pane. Exited writers and off-box writers sharing a
			// network HOME remain genuinely invisible (§5.1); their surviving
			// files retain the explicitly weaker cohort evidence class.
			return Observation{Pane: pane, Tool: "claude", Status: Ambiguous}
		}
	}
	reportedElsewhere := map[string]bool{}
	unresolvedPeer := false
	for _, other := range otherPanes {
		if other.PaneID == pane.PaneID {
			continue
		}
		if other.AgentSession != "" {
			reportedElsewhere[other.AgentSession] = true
			continue
		}
		otherCWD := other.ForegroundCWD
		if otherCWD == "" {
			otherCWD = other.CWD
		}
		if (otherCWD != "" && cohorts[mungeCWD(otherCWD)]) || (otherCWD == "" && other.Agent == "claude") {
			// When both same-cohort panes lose agent_session, cohort evidence
			// has no pid-to-sid join and cannot say which transcript belongs to
			// which pane. This is the accepted residual from contract §5.1:
			// fail closed rather than allowing recency to pick an owner.
			unresolvedPeer = true
		}
	}
	if unresolvedPeer {
		return Observation{Pane: pane, Tool: "claude", Status: Ambiguous}
	}
	eligible := cs[:0]
	for _, c := range cs {
		if !reportedElsewhere[c.sid] {
			eligible = append(eligible, c)
		}
	}
	cs = eligible
	for _, c := range cs {
		if c.mtime.After(newest) {
			newest = c.mtime
		}
	}
	unique := map[string][]candidate{}
	for _, c := range cs {
		if !newest.IsZero() && c.mtime.Before(newest.Add(-claudeActivityWindow)) {
			continue
		}
		key := c.sid + "\x00" + c.path
		unique[key] = append(unique[key], c)
	}
	if len(unique) == 0 {
		return Observation{Pane: pane, Tool: "claude", Status: Vacant}
	}
	if len(unique) > 1 {
		return Observation{Pane: pane, Tool: "claude", Status: Ambiguous, provenCandidates: len(unique)}
	}
	var witnesses []candidate
	for _, v := range unique {
		witnesses = v
	}
	procs := make([]procEntry, 0, len(witnesses))
	for _, witness := range witnesses {
		procs = append(procs, witness.procEntry)
	}
	if !sameProcessCohort(procs) {
		return Observation{Pane: pane, Tool: "claude", Status: Ambiguous, provenCandidates: 2}
	}
	chosen := outermostProcess(sub.ProcRoot, procs)
	c := witnesses[0]
	for _, witness := range witnesses {
		if witness.pid == chosen.pid {
			c = witness
			break
		}
	}
	ev := []Signal{SignalCohort}
	if c.guid != "" {
		ev = append(ev, SignalEnvironGUID)
	}
	return Observation{Pane: pane, Tool: "claude", PID: c.pid, Transcript: c.path, SID: c.sid, Evidence: ev, Status: Occupied}
}

func claudeTranscripts(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	return paths, nil
}

func outermostProcess(root string, matches []procEntry) procEntry {
	for _, candidate := range matches {
		outermost := true
		for _, other := range matches {
			if candidate.pid != other.pid && !isAncestor(root, candidate.pid, other.pid) {
				outermost = false
				break
			}
		}
		if outermost {
			return candidate
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].pid < matches[j].pid })
	return matches[0]
}

func sameProcessCohort(matches []procEntry) bool {
	if len(matches) == 0 {
		return true
	}
	cwd := matches[0].cwd
	for _, match := range matches[1:] {
		if match.cwd != cwd {
			return false
		}
	}
	return true
}

// RecordedSIDSet exposes the exact lineage membership rule for callers that
// need to check ambiguity across several registry rows before Verdict.
func RecordedSIDSet(row v2.SessionRecord) []string {
	set := map[string]bool{}
	for _, sid := range row.SIDs {
		if sid.SID != "" {
			set[sid.SID] = true
		}
	}
	if row.Provenance.ToolSessionID != "" {
		set[row.Provenance.ToolSessionID] = true
	}
	out := make([]string, 0, len(set))
	for sid := range set {
		out = append(out, sid)
	}
	sort.Strings(out)
	return out
}

func (o Observation) String() string {
	return fmt.Sprintf("%s tool=%s pid=%d sid=%s pane=%s", o.Status, o.Tool, o.PID, o.SID, o.Pane.PaneID)
}
