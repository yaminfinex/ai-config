package observercmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/observerstatus"
	"ai-config/tools/herder/internal/occupant"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

const (
	defaultSweepInterval = 30 * time.Second
	defaultDeadGrace     = 2 * time.Minute
	lockFileName         = "observer.lock"
)

type options struct{ help, json bool }

type busState struct {
	available bool
	rows      map[string]hcomidentity.Row
	roster    []hcomidentity.Row
	err       error
}
type herdrState struct {
	available       bool
	source          string
	connectionGap   bool
	snapshot        herdrcli.Snapshot
	byTerm          map[string]herdrcli.Pane
	procs           map[string]herdrcli.ProcessInfo
	sameEpochAbsent map[string]bool
	err             error
}
type herdrContext struct {
	client        *herdrSocketClient
	seenTerms     map[string]bool
	connectionGap bool
}
type candidate struct {
	kind, guid string
	row        v2.SessionRecord
}
type paneObservation struct {
	Occupant  occupant.Observation
	Bus       hcomidentity.Result
	BusStatus string
}
type sweepResult struct {
	Status     observerstatus.Status `json:"status"`
	Candidates int                   `json:"candidates"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	switch args[0] {
	case "-h", "--help", "help":
		printHelp(stdout)
		return 0
	case "sweep":
		opts, code := parseOptions(args[1:], stdout, stderr)
		if code != 0 || opts.help {
			return code
		}
		return runSweep(opts, stdout, stderr)
	case "run":
		opts, code := parseOptions(args[1:], stdout, stderr)
		if code != 0 || opts.help {
			return code
		}
		return runDaemon(stdout, stderr)
	case "status":
		opts, code := parseOptions(args[1:], stdout, stderr)
		if code != 0 || opts.help {
			return code
		}
		return runStatus(opts, stdout, stderr)
	case "stop":
		opts, code := parseOptions(args[1:], stdout, stderr)
		if code != 0 || opts.help {
			return code
		}
		return runStop(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "herder observer: unknown subcommand %q\n", args[0])
		return 2
	}
}

func parseOptions(args []string, stdout, stderr io.Writer) (options, int) {
	var opts options
	for _, arg := range args {
		switch arg {
		case "--json":
			opts.json = true
		case "-h", "--help":
			printHelp(stdout)
			opts.help = true
		default:
			fmt.Fprintf(stderr, "herder observer: unknown arg: %s\n", arg)
			return opts, 1
		}
	}
	return opts, 0
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder observer — refresh the human-facing fleet ledger from live substrate probes.

Usage:
  herder observer sweep [--json]   run one level-triggered observation pass
  herder observer run              run the singleton per-state-dir observer loop
  herder observer status [--json]  report lock/status-file advice
  herder observer stop             SIGTERM the lockfile pid

The ledger is a display cache, never authority for lifecycle commands. Each
sweep stamps probe-corroborated pane/session state, deduplicates pane claims,
and retires dead observations after a short grace window.
`)
}

func runSweep(opts options, stdout, stderr io.Writer) int {
	res, err := sweepOnce(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "herder observer sweep: %v\n", err)
		return 1
	}
	if opts.json {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(res)
		return 0
	}
	s := res.Status.LastSweepSummary
	fmt.Fprintf(stdout, "observer sweep: candidates=%d applied=%d noop=%d refused=%d flags=%d\n", res.Candidates, s.Applied, s.Noop, s.Refused, len(res.Status.Flags))
	return 0
}

func sweepOnce(stderr io.Writer) (sweepResult, error) { return sweepOnceWithHerdr(stderr, nil) }

func sweepOnceWithHerdr(stderr io.Writer, hctx *herdrContext) (sweepResult, error) {
	registryPath := registry.DefaultPath()
	stateDir := filepath.Dir(registryPath)
	now := time.Now().UTC()
	st := observerstatus.Status{Schema: "herder.observer.status.v1", Advice: true, PID: os.Getpid(), BuildHash: buildHash(), HeartbeatAt: now.Format(time.RFC3339), LastSweepAt: now.Format(time.RFC3339), ProtocolCompatible: true, Confirmed: map[string]string{}}
	proj, err := loadProjection(registryPath, stderr)
	if err != nil {
		return sweepResult{}, err
	}
	hd := loadHerdrState(hctx, stderr)
	if !hd.available {
		st.ProtocolCompatible = false
		if hd.err != nil {
			st.ProtocolDetail = hd.err.Error()
		}
	} else {
		st.ProtocolDetail = fmt.Sprintf("source=%s connection_gap=%t", firstNonEmpty(hd.source, "unknown"), hd.connectionGap)
	}
	bus := loadBusState()
	grace := durationEnv("HERDER_OBSERVER_DEAD_GRACE", defaultDeadGrace)
	var cands []candidate
	if hd.available {
		cands = buildCacheCandidates(proj, observePanes(proj, hd, bus), now, grace)
	} else {
		cands = archiveDeadCandidates(proj, now, grace)
	}
	summary := applyCandidates(registryPath, cands, stderr)
	for _, cand := range cands {
		if cand.kind == "stamp" && cand.row.Cache != nil {
			st.Confirmed[cand.guid] = cand.row.Cache.ObservedAt
		}
	}
	st.LastSweepSummary = summary
	if err := observerstatus.WriteAtomic(observerstatus.PathForStateDir(stateDir), st); err != nil {
		return sweepResult{}, err
	}
	return sweepResult{Status: st, Candidates: len(cands)}, nil
}
func loadProjection(path string, stderr io.Writer) (*v2.Projection, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return v2.Load(strings.NewReader(""), v2.LoadOptions{Stderr: stderr})
		}
		return nil, err
	}
	defer f.Close()
	return v2.Load(f, v2.LoadOptions{Stderr: stderr})
}

func loadHerdrState(hctx *herdrContext, stderr io.Writer) herdrState {
	if hctx != nil && hctx.client != nil {
		return loadHerdrStateSocket(hctx, "socket")
	}
	client, st, err := connectHerdrRPCClient(stderr)
	if err != nil {
		if cliFallbackAllowed(st) {
			if hd := loadHerdrStateCLI("cli-fallback"); hd.available {
				hd.err = fmt.Errorf("herdr socket protocol incompatible; using CLI fallback: %w", err)
				return hd
			}
		}
		if st.detail != "" {
			return herdrState{err: fmt.Errorf("%s: %w", st.detail, err)}
		}
		return herdrState{err: err}
	}
	defer client.Close()
	return loadHerdrStateSocket(&herdrContext{client: client, seenTerms: map[string]bool{}, connectionGap: true}, "socket")
}

func cliFallbackAllowed(st socketStatus) bool {
	return os.Getenv("HERDER_OBSERVER_ALLOW_CLI_FALLBACK") == "1" &&
		st.discovered &&
		st.protocol != 0 &&
		st.protocol != supportedHerdrProtocol
}

func loadHerdrStateSocket(hctx *herdrContext, source string) herdrState {
	snap, err := hctx.client.snapshot()
	if err != nil {
		return herdrState{source: source, err: fmt.Errorf("herdr socket session.snapshot failed: %w", err)}
	}
	previousSeen := map[string]bool{}
	for term, seen := range hctx.seenTerms {
		previousSeen[term] = seen
	}
	if hctx.seenTerms == nil {
		hctx.seenTerms = map[string]bool{}
	}
	hd := herdrState{
		available:       true,
		source:          source,
		connectionGap:   hctx.connectionGap,
		snapshot:        snap,
		byTerm:          map[string]herdrcli.Pane{},
		procs:           map[string]herdrcli.ProcessInfo{},
		sameEpochAbsent: map[string]bool{},
	}
	for _, pane := range snap.Panes {
		if pane.TerminalID != "" {
			hd.byTerm[pane.TerminalID] = pane
			hctx.seenTerms[pane.TerminalID] = true
		}
	}
	for _, agent := range snap.Agents {
		if agent.TerminalID == nil || *agent.TerminalID == "" {
			continue
		}
		if _, ok := hd.byTerm[*agent.TerminalID]; !ok {
			hd.byTerm[*agent.TerminalID] = herdrcli.Pane{
				PaneID:      agent.PaneID,
				TerminalID:  *agent.TerminalID,
				Agent:       agent.Agent,
				AgentStatus: agent.Status,
				Label:       agent.Name,
				CWD:         agent.CWD,
			}
		}
		hctx.seenTerms[*agent.TerminalID] = true
	}
	if !hctx.connectionGap {
		for term := range previousSeen {
			if _, ok := hd.byTerm[term]; !ok {
				hd.sameEpochAbsent[term] = true
			}
		}
	}
	for term, pane := range hd.byTerm {
		id := firstNonEmpty(pane.PaneID, term)
		pi, err := hctx.client.processInfo(id)
		if err != nil {
			continue
		}
		hd.procs[term] = pi
	}
	hctx.connectionGap = false
	return hd
}

func loadHerdrStateCLI(source string) herdrState {
	client := &herdrcli.Client{}
	// herdr 0.8.0 moved the CLI snapshot from `session snapshot` to `api
	// snapshot`; the envelope ({"result":{"snapshot":{...}}}) is unchanged.
	out, err := client.Output("api", "snapshot")
	if err != nil {
		return herdrState{source: source, err: fmt.Errorf("herdr CLI session.snapshot unavailable")}
	}
	snap, err := herdrcli.ParseSessionSnapshot(out)
	if err != nil {
		return herdrState{source: source, err: fmt.Errorf("herdr CLI session.snapshot parse failed: %w", err)}
	}
	hd := herdrState{
		available:       true,
		source:          source,
		connectionGap:   true,
		snapshot:        snap,
		byTerm:          map[string]herdrcli.Pane{},
		procs:           map[string]herdrcli.ProcessInfo{},
		sameEpochAbsent: map[string]bool{},
	}
	for _, pane := range snap.Panes {
		if pane.TerminalID != "" {
			hd.byTerm[pane.TerminalID] = pane
		}
	}
	for _, agent := range snap.Agents {
		if agent.TerminalID == nil || *agent.TerminalID == "" {
			continue
		}
		if _, ok := hd.byTerm[*agent.TerminalID]; !ok {
			hd.byTerm[*agent.TerminalID] = herdrcli.Pane{
				PaneID:      agent.PaneID,
				TerminalID:  *agent.TerminalID,
				Agent:       agent.Agent,
				AgentStatus: agent.Status,
				Label:       agent.Name,
				CWD:         agent.CWD,
			}
		}
	}
	for term, pane := range hd.byTerm {
		id := firstNonEmpty(pane.PaneID, term)
		// herdr 0.8 verb shape: `pane process-info --pane <id>` (the
		// positional `process_info` spelling returns help text, rc=0).
		out, err := client.Output("pane", "process-info", "--pane", id)
		if err != nil {
			continue
		}
		if pi, err := herdrcli.ParseProcessInfo(out); err == nil {
			hd.procs[term] = pi
		}
	}
	return hd
}

func loadBusState() busState {
	listed, err := hcomidentity.List("")
	if err != nil {
		return busState{err: err}
	}
	rows := map[string]hcomidentity.Row{}
	for _, row := range listed {
		if row.Name != "" {
			rows[row.Name] = row
		}
	}
	return busState{available: true, rows: rows, roster: listed}
}

type snapshotQuerier struct{ state herdrState }

func (q snapshotQuerier) Pane(id string) (herdrcli.Pane, error) {
	for _, p := range q.state.byTerm {
		if p.PaneID == id || p.TerminalID == id {
			return p, nil
		}
	}
	return herdrcli.Pane{}, os.ErrNotExist
}
func (q snapshotQuerier) Panes() ([]herdrcli.Pane, error) {
	out := make([]herdrcli.Pane, 0, len(q.state.byTerm))
	for _, p := range q.state.byTerm {
		out = append(out, p)
	}
	return out, nil
}
func (q snapshotQuerier) ProcessInfo(id string) (herdrcli.ProcessInfo, error) {
	for term, p := range q.state.byTerm {
		if p.PaneID == id || p.TerminalID == id {
			info, ok := q.state.procs[term]
			if !ok {
				return herdrcli.ProcessInfo{}, errors.New("process ancestry unavailable")
			}
			return info, nil
		}
	}
	return herdrcli.ProcessInfo{}, os.ErrNotExist
}

func observePanes(proj *v2.Projection, hd herdrState, bus busState) map[string]paneObservation {
	observed := map[string]paneObservation{}
	querier := snapshotQuerier{state: hd}
	byPane := map[string][]v2.SessionRecord{}
	for _, rec := range proj.Sessions() {
		paneID := recordPaneID(rec)
		seated := rec.State == v2.StateSeated && rec.Seat != nil && rec.Seat.Kind != "process"
		recoverable := rec.Cache != nil && rec.Cache.Liveness == "dead" && anyLiveBusRow([]v2.SessionRecord{rec}, bus)
		if paneID == "" || (!seated && !recoverable) {
			continue
		}
		byPane[paneID] = append(byPane[paneID], rec)
	}
	for paneID, rows := range byPane {
		obs := occupant.Probe(occupant.Substrate{Herdr: querier}, paneID)
		// A tool process can exist before its transcript/session artifact is
		// ready. That boot-order gap is not proof of vacancy and must not turn a
		// wrapper birth stamp into a dead row.
		if obs.Status == occupant.Vacant && paneHasToolProcess(hd, paneID) {
			obs.Status = occupant.Unprobeable
		}
		if !observationCorroboratesAny(obs, rows) {
			if relocated, ok := relocateRows(rows, hd, bus, func(id string) occupant.Observation {
				return occupant.Probe(occupant.Substrate{Herdr: occupant.CLIQuerier{}}, id)
			}, func(id string) occupant.Observation {
				return occupant.Probe(occupant.Substrate{Herdr: querier}, id)
			}); ok {
				obs = relocated
			} else if anyLiveBusRow(rows, bus) {
				// Live hook/PTY evidence outranks a stale coordinate. If identity
				// relocation cannot yet resolve the new pane, leave the cache row
				// untouched and retry next sweep.
				obs.Status = occupant.Unprobeable
			}
		}
		po := paneObservation{Occupant: obs}
		if obs.Status == occupant.Occupied && bus.available {
			po.Bus = hcomidentity.Resolve(bus.roster, hcomidentity.Evidence{SessionID: obs.SID, PaneIDs: []string{paneID}})
			if po.Bus.Verified {
				if row, ok := bus.rows[po.Bus.Name]; ok {
					po.BusStatus = row.Status
				}
			}
		}
		observed[paneID] = po
	}
	return observed
}

func observationCorroboratesAny(obs occupant.Observation, rows []v2.SessionRecord) bool {
	if obs.Status != occupant.Occupied || obs.SID == "" {
		return false
	}
	for _, rec := range rows {
		if latestSID(rec) == obs.SID || latestSID(rec) == "" {
			return true
		}
	}
	return false
}

func relocateRows(rows []v2.SessionRecord, hd herdrState, bus busState, aliasProbe, paneProbe func(string) occupant.Observation) (occupant.Observation, bool) {
	for _, rec := range rows {
		if _, live := liveBusRow(rec, bus); !live {
			continue
		}
		if obs := aliasProbe(recordPaneID(rec)); occupantMatchesRow(obs, rec) {
			return obs, true
		} else if obs.Pane.PaneID != "" && obs.Pane.PaneID != recordPaneID(rec) {
			// herdr preserves old pane ids as relocation aliases. An exact live
			// bus identity plus an alias resolving to a new coordinate is enough
			// to heal the display cache even when the transcript probe is briefly
			// ambiguous (notably for long-lived Claude sessions).
			obs.Status = occupant.Occupied
			obs.SID = latestSID(rec)
			obs.Tool = firstNonEmpty(obs.Tool, rec.Tool)
			return obs, true
		}
		for _, pane := range hd.byTerm {
			if pane.PaneID == "" || (pane.Label != rec.Label && pane.Label != recordHcomName(rec)) {
				continue
			}
			if obs := paneProbe(pane.PaneID); occupantMatchesRow(obs, rec) {
				return obs, true
			}
		}
		for _, pane := range hd.byTerm {
			if pane.PaneID == "" {
				continue
			}
			if obs := paneProbe(pane.PaneID); occupantMatchesRow(obs, rec) {
				return obs, true
			}
		}
	}
	return occupant.Observation{}, false
}

func occupantMatchesRow(obs occupant.Observation, rec v2.SessionRecord) bool {
	return obs.Status == occupant.Occupied && obs.SID != "" && (latestSID(rec) == "" || obs.SID == latestSID(rec))
}

func anyLiveBusRow(rows []v2.SessionRecord, bus busState) bool {
	for _, rec := range rows {
		if _, ok := liveBusRow(rec, bus); ok {
			return true
		}
	}
	return false
}

func liveBusRow(rec v2.SessionRecord, bus busState) (hcomidentity.Row, bool) {
	if !bus.available {
		return hcomidentity.Row{}, false
	}
	sid := latestSID(rec)
	name := recordHcomName(rec)
	for _, row := range bus.roster {
		if row.Status == "inactive" || row.Status == "stopped" || row.SessionID == "" {
			continue
		}
		if (sid != "" && row.SessionID == sid) || (name != "" && row.Name == name) {
			return row, true
		}
	}
	return hcomidentity.Row{}, false
}

func paneHasToolProcess(hd herdrState, paneID string) bool {
	for term, pane := range hd.byTerm {
		if pane.PaneID != paneID {
			continue
		}
		for _, process := range hd.procs[term].Processes {
			name := strings.ToLower(filepath.Base(process.Name))
			if name == "" && len(process.Argv) > 0 {
				name = strings.ToLower(filepath.Base(process.Argv[0]))
			}
			if name == "codex" || strings.HasPrefix(name, "codex-") || name == "claude" || strings.HasPrefix(name, "claude-") {
				return true
			}
		}
	}
	return false
}

func buildCacheCandidates(proj *v2.Projection, observed map[string]paneObservation, now time.Time, grace time.Duration) []candidate {
	var out []candidate
	touched := map[string]bool{}
	byPane := map[string][]v2.SessionRecord{}
	for _, rec := range proj.Sessions() {
		paneID := recordPaneID(rec)
		seated := rec.State == v2.StateSeated && rec.Seat != nil && rec.Seat.Kind != "process"
		recoverable := rec.Cache != nil && rec.Cache.Liveness == "dead"
		if paneID != "" && (seated || recoverable) {
			byPane[paneID] = append(byPane[paneID], rec)
		}
	}
	panes := make([]string, 0, len(byPane))
	for pane := range byPane {
		panes = append(panes, pane)
	}
	sort.Strings(panes)
	for _, pane := range panes {
		rows := byPane[pane]
		po, ok := observed[pane]
		if !ok {
			continue
		}
		switch po.Occupant.Status {
		case occupant.Occupied:
			winner := corroboratedRow(rows, po.Occupant.SID)
			if winner < 0 {
				for _, rec := range rows {
					out = append(out, deadCandidate(rec, now))
				}
				continue
			}
			for i, rec := range rows {
				if i != winner {
					out = append(out, duplicateCandidate(rec, now))
				}
			}
			out = append(out, stampCandidate(rows[winner], po, now))
			touched[rows[winner].GUID] = true
		case occupant.Vacant, occupant.PaneGone:
			for _, rec := range rows {
				out = append(out, deadCandidate(rec, now))
			}
		}
	}
	for _, cand := range archiveDeadCandidates(proj, now, grace) {
		if !touched[cand.guid] {
			out = append(out, cand)
		}
	}
	return out
}

func corroboratedRow(rows []v2.SessionRecord, sid string) int {
	winner, bestRank := -1, 0
	for i, rec := range rows {
		rank := 0
		current := latestSID(rec)
		if sid != "" && current == sid {
			rank = 2
		} else if current == "" {
			rank = 1
		}
		if rank != 0 && (winner < 0 || rank > bestRank || (rank == bestRank && newerRow(rec, rows[winner]))) {
			winner, bestRank = i, rank
		}
	}
	return winner
}
func newerRow(a, b v2.SessionRecord) bool {
	ta, ea := time.Parse(time.RFC3339, a.RecordedAt)
	tb, eb := time.Parse(time.RFC3339, b.RecordedAt)
	if ea == nil && eb == nil && !ta.Equal(tb) {
		return ta.After(tb)
	}
	if a.Ordinal != b.Ordinal {
		return a.Ordinal > b.Ordinal
	}
	return a.GUID < b.GUID
}

func stampCandidate(rec v2.SessionRecord, po paneObservation, now time.Time) candidate {
	stamp := now.UTC().Format(time.RFC3339)
	next := rec
	next.Event = "observed"
	next.RecordedAt = stamp
	next.State = v2.StateSeated
	next.Label = firstNonEmpty(next.Label, cacheLabel(rec))
	next.ObservedVia = "observer+occupant_probe"
	seat := cloneSeat(rec.Seat)
	if seat == nil {
		seat = &v2.Seat{Kind: "herdr"}
	}
	seat.Kind = "herdr"
	seat.PaneID = firstNonEmpty(po.Occupant.Pane.PaneID, seat.PaneID)
	seat.TerminalID = firstNonEmpty(po.Occupant.Pane.TerminalID, seat.TerminalID)
	seat.ConfirmedAt = stamp
	name := seat.HcomName
	if po.Bus.Verified {
		name = po.Bus.Name
		verified := true
		seat.HcomVerified = &verified
	}
	seat.HcomName = name
	if po.Occupant.SID != "" && latestSID(next) != po.Occupant.SID {
		next.SIDs = append(append([]v2.SID(nil), next.SIDs...), v2.SID{SID: po.Occupant.SID, ObservedAt: stamp, Source: "observer"})
	}
	next.Continuity = "confirmed"
	next.Tool = firstNonEmpty(po.Occupant.Tool, next.Tool)
	next.Seat = seat
	liveness := firstNonEmpty(po.BusStatus, "alive")
	next.Cache = &v2.CacheObservation{PaneID: seat.PaneID, TerminalID: seat.TerminalID, OccupantKind: next.Tool, SessionID: po.Occupant.SID, HcomName: name, Label: firstNonEmpty(rec.Label, cacheLabel(rec), name), Liveness: liveness, ObservedAt: stamp}
	return candidate{kind: "stamp", guid: rec.GUID, row: next}
}

func deadCandidate(rec v2.SessionRecord, now time.Time) candidate {
	stamp := now.UTC().Format(time.RFC3339)
	next := rec
	next.Event = "observed_dead"
	next.RecordedAt = stamp
	next.State = v2.StateUnseated
	next.Seat = nil
	next.CloseResult = "observed_dead"
	next.CloseReason = "pane gone or recorded occupant no longer corroborated by ancestry probe"
	next.ObservedVia = "observer+occupant_probe"
	cache := cacheFromRow(rec)
	cache.Liveness = "dead"
	cache.ObservedAt = stamp
	next.Cache = &cache
	return candidate{kind: "dead", guid: rec.GUID, row: next}
}
func duplicateCandidate(rec v2.SessionRecord, now time.Time) candidate {
	stamp := now.UTC().Format(time.RFC3339)
	next := rec
	next.Event = "observation_archived"
	next.RecordedAt = stamp
	next.State = v2.StateRetired
	next.Seat = nil
	next.CloseResult = "deduplicated"
	next.CloseReason = "another row on the same pane was corroborated by the occupant probe"
	next.ObservedVia = "observer+occupant_probe"
	cache := cacheFromRow(rec)
	cache.Liveness = "duplicate"
	cache.ObservedAt = stamp
	next.Cache = &cache
	return candidate{kind: "retire-duplicate", guid: rec.GUID, row: next}
}
func archiveDeadCandidates(proj *v2.Projection, now time.Time, grace time.Duration) []candidate {
	var out []candidate
	for _, rec := range proj.Sessions() {
		if rec.State != v2.StateUnseated || rec.Cache == nil || rec.Cache.Liveness != "dead" {
			continue
		}
		observedAt, err := time.Parse(time.RFC3339, firstNonEmpty(rec.Cache.ObservedAt, rec.RecordedAt))
		if err != nil || now.Before(observedAt.Add(grace)) {
			continue
		}
		stamp := now.UTC().Format(time.RFC3339)
		next := rec
		next.Event = "observation_archived"
		next.RecordedAt = stamp
		next.State = v2.StateRetired
		next.Seat = nil
		cache := *rec.Cache
		cache.ObservedAt = stamp
		next.Cache = &cache
		out = append(out, candidate{kind: "archive-dead", guid: rec.GUID, row: next})
	}
	return out
}
func cacheFromRow(rec v2.SessionRecord) v2.CacheObservation {
	if rec.Cache != nil {
		cache := *rec.Cache
		return cache
	}
	cache := v2.CacheObservation{Label: rec.Label, OccupantKind: rec.Tool, SessionID: latestSID(rec)}
	if rec.Seat != nil {
		cache.PaneID = rec.Seat.PaneID
		cache.TerminalID = rec.Seat.TerminalID
		cache.HcomName = rec.Seat.HcomName
	}
	return cache
}
func latestSID(rec v2.SessionRecord) string {
	if len(rec.SIDs) == 0 {
		return ""
	}
	return rec.SIDs[len(rec.SIDs)-1].SID
}
func recordPaneID(rec v2.SessionRecord) string {
	if rec.Seat != nil && rec.Seat.PaneID != "" {
		return rec.Seat.PaneID
	}
	if rec.Cache != nil {
		return rec.Cache.PaneID
	}
	return ""
}
func recordHcomName(rec v2.SessionRecord) string {
	if rec.Seat != nil && rec.Seat.HcomName != "" {
		return rec.Seat.HcomName
	}
	if rec.Cache != nil {
		return rec.Cache.HcomName
	}
	return ""
}
func cacheLabel(rec v2.SessionRecord) string {
	if rec.Cache != nil {
		return rec.Cache.Label
	}
	return ""
}
func cloneSeat(seat *v2.Seat) *v2.Seat {
	if seat == nil {
		return nil
	}
	copy := *seat
	return &copy
}

func applyCandidates(path string, cands []candidate, stderr io.Writer) observerstatus.Summary {
	var summary observerstatus.Summary
	if len(cands) == 0 {
		return summary
	}
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		rows := make([]v2.SessionRecord, 0, len(cands))
		for _, cand := range cands {
			rows = append(rows, cand.row)
		}
		return rows, nil
	})
	if err != nil {
		fmt.Fprintf(stderr, "herder observer sweep: refused %d cache stamp(s): %v\n", len(cands), err)
		summary.Refused = len(cands)
		return summary
	}
	for _, outcome := range outcomes {
		switch outcome.Status {
		case registry.WriteApplied:
			summary.Applied++
		case registry.WriteNoop:
			summary.Noop++
		default:
			summary.Refused++
		}
	}
	summary.Noop += len(cands) - len(outcomes)
	return summary
}
func runDaemon(stdout, stderr io.Writer) int {
	lock, ok := acquireObserverLock(stderr)
	if !ok {
		return 0
	}
	defer lock.Close()
	interval := sweepInterval()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	for {
		client, _, err := connectHerdrSocket(stderr)
		if err != nil {
			fmt.Fprintf(stderr, "herder observer run: herdr socket connect failed: %v; retrying after %s\n", err, interval)
			sweepDaemonOnce(stderr, nil, lock.path)
			if waitOrSignal(interval, signals) {
				return 0
			}
			continue
		}
		hctx := &herdrContext{client: client, seenTerms: map[string]bool{}, connectionGap: true}
		if err := client.subscribeObserverEvents(); err != nil {
			fmt.Fprintf(stderr, "herder observer run: events.subscribe failed: %v; retrying after %s\n", err, interval)
			client.Close()
			sweepDaemonOnce(stderr, nil, lock.path)
			if waitOrSignal(interval, signals) {
				return 0
			}
			continue
		}
		if err := sweepDaemonOnce(stderr, hctx, lock.path); err != nil {
			fmt.Fprintf(stderr, "herder observer run: reconnecting after initial sweep failed: %v; retrying after %s\n", err, interval)
			client.Close()
			if waitOrSignal(interval, signals) {
				return 0
			}
			continue
		}
		ticker := time.NewTicker(interval)
		reconnect := false
		reconnectCause := ""
		for !reconnect {
			if client.isClosed() {
				reconnect = true
				reconnectCause = client.closeCause().Error()
				break
			}
			select {
			case <-ticker.C:
				if client.isClosed() {
					reconnect = true
					reconnectCause = client.closeCause().Error()
					break
				}
				if err := sweepDaemonOnce(stderr, hctx, lock.path); err != nil {
					reconnect = true
					reconnectCause = fmt.Sprintf("sweep failed: %v", err)
				}
			case <-signals:
				ticker.Stop()
				client.Close()
				return 0
			default:
				if client.nextEvent(250 * time.Millisecond) {
					if client.isClosed() {
						reconnect = true
						reconnectCause = client.closeCause().Error()
						break
					}
					// Events are latency hints. A full sweep is still the correctness
					// path, and it subsumes a targeted probe while preserving the
					// same uninterrupted socket generation.
					if err := sweepDaemonOnce(stderr, hctx, lock.path); err != nil {
						reconnect = true
						reconnectCause = fmt.Sprintf("event-triggered sweep failed: %v", err)
					}
				}
				select {
				case <-client.closed:
					reconnect = true
					if reconnectCause == "" {
						reconnectCause = client.closeCause().Error()
					}
				default:
				}
			}
		}
		ticker.Stop()
		client.Close()
		if reconnectCause == "" {
			reconnectCause = "herdr socket reconnect requested"
		}
		fmt.Fprintf(stderr, "herder observer run: reconnecting after %s; retrying after %s\n", reconnectCause, interval)
		select {
		case <-signals:
			return 0
		default:
		}
		if waitOrSignal(interval, signals) {
			return 0
		}
	}
}

func sweepDaemonOnce(stderr io.Writer, hctx *herdrContext, heartbeatPath string) error {
	res, err := sweepOnceWithHerdr(stderr, hctx)
	if err != nil {
		fmt.Fprintf(stderr, "herder observer run: sweep failed: %v\n", err)
		return err
	}
	if !res.Status.ProtocolCompatible {
		fmt.Fprintf(stderr, "herder observer run: sweep transport unhealthy: %s\n", res.Status.ProtocolDetail)
		return errors.New(res.Status.ProtocolDetail)
	}
	if err := touch(heartbeatPath); err != nil {
		fmt.Fprintf(stderr, "herder observer run: heartbeat touch failed: %v\n", err)
		return err
	}
	return nil
}

func waitOrSignal(d time.Duration, signals <-chan os.Signal) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return false
	case <-signals:
		return true
	}
}

type observerLock struct {
	file *os.File
	path string
}

func (l observerLock) Close() {
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
}

func acquireObserverLock(stderr io.Writer) (observerLock, bool) {
	path := filepath.Join(filepath.Dir(registry.DefaultPath()), lockFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(stderr, "herder observer run: %v\n", err)
		return observerLock{}, false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "herder observer run: %v\n", err)
		return observerLock{}, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return observerLock{}, false
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "pid=%d\nbuild=%s\nstarted_at=%s\n", os.Getpid(), buildHash(), time.Now().UTC().Format(time.RFC3339))
	_ = f.Sync()
	return observerLock{file: f, path: path}, true
}

func runStatus(opts options, stdout, stderr io.Writer) int {
	path := observerstatus.DefaultPath()
	st, err := observerstatus.Read(path)
	if err != nil && !observerstatus.Missing(err) {
		fmt.Fprintf(stderr, "herder observer status: %v\n", err)
		return 1
	}
	if opts.json {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(st)
		return 0
	}
	if observerstatus.Missing(err) {
		fmt.Fprintln(stdout, "observer status: no observer.status.json (no advice available)")
		return 0
	}
	s := st.LastSweepSummary
	fmt.Fprintf(stdout, "observer status: pid=%d build=%s heartbeat=%s last_sweep=%s applied=%d noop=%d refused=%d protocol_compatible=%t\n",
		st.PID, firstNonEmpty(st.BuildHash, "unknown"), st.HeartbeatAt, st.LastSweepAt, s.Applied, s.Noop, s.Refused, st.ProtocolCompatible)
	for _, flag := range st.Flags {
		fmt.Fprintf(stdout, "observer advice: %s %s cause_class=%s observed_at=%s observed_via=%s %s\n",
			firstNonEmpty(flag.GUID, flag.Label, "-"), flag.Type, firstNonEmpty(flag.CauseClass, "-"),
			firstNonEmpty(flag.ObservedAt, "-"), firstNonEmpty(strings.Join(flag.ObservedVia, ","), "-"), flag.Detail)
	}
	return 0
}

func runStop(stdout, stderr io.Writer) int {
	path := filepath.Join(filepath.Dir(registry.DefaultPath()), lockFileName)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stdout, "observer stop: no lockfile")
			return 0
		}
		fmt.Fprintf(stderr, "herder observer stop: %v\n", err)
		return 1
	}
	pid := parsePID(string(b))
	if pid == 0 {
		fmt.Fprintln(stdout, "observer stop: no pid in lockfile")
		return 0
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		fmt.Fprintf(stderr, "herder observer stop: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "observer stop: signalled pid %d\n", pid)
	return 0
}

func NudgeIfConfigured(stderr io.Writer) {
	if !autostartEnabled() {
		return
	}
	stateDir := filepath.Dir(registry.DefaultPath())
	lockPath := filepath.Join(stateDir, lockFileName)
	if freshHeartbeat(lockPath) {
		return
	}
	if b, err := os.ReadFile(lockPath); err == nil {
		if pid := parsePID(string(b)); pid != 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
			time.Sleep(100 * time.Millisecond)
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	if err := startDetachedObserver(stateDir); err != nil {
		fmt.Fprintf(stderr, "herder observer nudge: %v\n", err)
	}
}

func autostartEnabled() bool {
	if truthy(os.Getenv("HERDER_OBSERVER_AUTOSTART")) {
		return true
	}
	configPath := filepath.Join(filepath.Dir(registry.DefaultPath()), "config.json")
	b, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	var cfg struct {
		Observer struct {
			Autostart bool `json:"autostart"`
		} `json:"observer"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return false
	}
	return cfg.Observer.Autostart
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func freshHeartbeat(lockPath string) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) <= 5*sweepInterval()
}

func startDetachedObserver(stateDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "observer.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(exe, "observer", "run")
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func parsePID(s string) int {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "pid=") {
			n, _ := strconv.Atoi(strings.TrimPrefix(line, "pid="))
			return n
		}
	}
	return 0
}

func buildHash() string {
	if v := os.Getenv("HERDER_BUILD_HASH"); v != "" {
		return v
	}
	return "dev"
}

func sweepInterval() time.Duration {
	return durationEnv("HERDER_OBSERVER_SWEEP_INTERVAL", defaultSweepInterval)
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err == nil {
		return d
	}
	if sec, err := strconv.Atoi(v); err == nil {
		return time.Duration(sec) * time.Second
	}
	return fallback
}

func touch(path string) error {
	now := time.Now()
	return os.Chtimes(path, now, now)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
