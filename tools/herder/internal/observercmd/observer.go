package observercmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/continuationstate"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/hookcmd"
	"ai-config/tools/herder/internal/observerstatus"
	"ai-config/tools/herder/internal/pendingprompt"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
)

const (
	defaultSweepInterval     = 30 * time.Second
	defaultReconfirmInterval = time.Hour
	doctrineReceiptRetention = 24 * time.Hour
	lockFileName             = "observer.lock"
)

type options struct {
	help bool
	json bool
}

type hcomRow = hcomidentity.Row
type hcomLaunchContext = hcomidentity.LaunchContext

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
	grokCursors   map[string]*grokArtifactCursor
	connectionGap bool
}

type candidate struct {
	kind string
	guid string
	row  v2.SessionRecord
	sid  string
	bus  hcomidentity.Result
	seat *v2.Seat
}

type sweepResult struct {
	Status     observerstatus.Status `json:"status"`
	Candidates int                   `json:"candidates"`
}

type doctrineCandidate struct {
	Name  string
	Token string
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
	fmt.Fprint(stdout, `herder observer — observe seated sessions and append witnessed facts.

Usage:
  herder observer sweep [--json]   run one level-triggered observation pass
  herder observer run              run the singleton per-state-dir observer loop
  herder observer status [--json]  report lock/status-file advice
  herder observer stop             SIGTERM the lockfile pid

The observer is advice until it appends a registry row: observer.status.json can
flag dormant-live, epoch-doubt, or failed-continuation findings for operators,
but the registry remains truth. Recover a failed continuation with its suggested
command, then clear the finding explicitly with
herder list --ack-continuation ID. Observation facts use the same locked registry
writer as every CLI verb; there is no observer write service or IPC append
surface.
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
	for _, flag := range res.Status.Flags {
		fmt.Fprintf(stdout, "observer advice: %s %s %s\n", firstNonEmpty(flag.GUID, flag.Label, "-"), flag.Type, flag.Detail)
	}
	return 0
}

func sweepOnce(stderr io.Writer) (sweepResult, error) {
	return sweepOnceWithHerdr(stderr, nil)
}

func sweepOnceWithHerdr(stderr io.Writer, hctx *herdrContext) (sweepResult, error) {
	registryPath := registry.DefaultPath()
	stateDir := filepath.Dir(registryPath)
	now := time.Now().UTC()
	if err := pendingprompt.PruneAll(registryPath, now); err != nil {
		fmt.Fprintf(stderr, "herder observer: pending prompt GC failed: %v\n", err)
	}
	st := observerstatus.Status{
		Schema:             "herder.observer.status.v1",
		Advice:             true,
		PID:                os.Getpid(),
		BuildHash:          buildHash(),
		HeartbeatAt:        now.Format(time.RFC3339),
		LastSweepAt:        now.Format(time.RFC3339),
		ProtocolCompatible: true,
		Confirmed:          map[string]string{},
		DoctrineDeliveries: map[string]string{},
	}
	proj, err := loadProjection(registryPath, stderr)
	if err != nil {
		return sweepResult{}, err
	}
	hd := loadHerdrState(hctx, stderr)
	if !hd.available {
		st.ProtocolCompatible = false
		st.ProtocolDetail = hd.err.Error()
	} else {
		st.ProtocolDetail = fmt.Sprintf("source=%s connection_gap=%t", firstNonEmpty(hd.source, "unknown"), hd.connectionGap)
	}
	bus := loadBusState()
	sessions := proj.Sessions()
	st.DoctrineDeliveries = priorDoctrineDeliveries(stateDir, hd, bus, now)
	var grokCursors map[string]*grokArtifactCursor
	if hctx != nil {
		if hctx.grokCursors == nil {
			hctx.grokCursors = map[string]*grokArtifactCursor{}
		}
		grokCursors = hctx.grokCursors
	}
	grokObservationState, grokFlags := grokObservations(sessions, stateDir, stderr, grokCursors)
	st.Observations = grokObservationState
	cands := buildCandidates(proj, hd, bus, now)
	doctrine := doctrineCandidates(proj, hd, bus, st.DoctrineDeliveries, joinedHcomRow)
	flags := advisoryFlags(proj, hd)
	flags = append(flags, grokFlags...)
	flags = append(flags, epochFlags(proj, hd, bus)...)
	flags = append(flags, continuationFailureFlags(proj, stateDir, stderr)...)
	summary := applyCandidates(registryPath, cands, stderr)
	deliverDoctrine(doctrine, st.DoctrineDeliveries, sendDoctrine, now)
	for _, rec := range sessions {
		if rec.State == v2.StateSeated && rec.Seat != nil {
			st.Confirmed[rec.GUID] = rec.Seat.ConfirmedAt
		}
	}
	st.LastSweepSummary = summary
	st.Flags = flags
	if err := observerstatus.WriteAtomic(observerstatus.PathForStateDir(stateDir), st); err != nil {
		return sweepResult{}, err
	}
	return sweepResult{Status: st, Candidates: len(cands)}, nil
}

// Receipt loss or status rotation deliberately fails toward re-delivery: informational doctrine spam is safer than silence.
func priorDoctrineDeliveries(stateDir string, hd herdrState, bus busState, now time.Time) map[string]string {
	receipts := map[string]string{}
	prior, err := observerstatus.Read(observerstatus.PathForStateDir(stateDir))
	if err != nil {
		return receipts
	}
	for token, stamp := range prior.DoctrineDeliveries {
		if keepDoctrineReceipt(token, stamp, hd, bus, now) {
			receipts[token] = stamp
		}
	}
	return receipts
}

func keepDoctrineReceipt(token, stamp string, hd herdrState, bus busState, now time.Time) bool {
	if !hd.available || !bus.available {
		return true
	}
	processID, sessionID, ok := strings.Cut(token, ":")
	if !ok || processID == "" || sessionID == "" {
		return false
	}
	sameProcess := false
	sameSession := false
	for _, row := range bus.rows {
		if row.LaunchContext.ProcessID == processID && row.SessionID == sessionID {
			return true
		}
		sameProcess = sameProcess || row.LaunchContext.ProcessID == processID
		sameSession = sameSession || row.SessionID == sessionID
	}
	if sameProcess || sameSession {
		return false
	}
	for _, pane := range hd.byTerm {
		if pane.AgentSession == sessionID {
			return true
		}
	}
	deliveredAt, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false
	}
	age := now.Sub(deliveredAt)
	return age < doctrineReceiptRetention || age < 0
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
	out, err := client.Output("session", "snapshot")
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
		out, err := client.Output("pane", "process_info", id)
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

// doctrineCandidates finds only unmanaged Codex sessions for which the live
// herdr pane/process, tool session id, and joined hcom process row all agree.
// Every match is exact and child-specific; ambiguity or a missing leg yields
// no candidate.
func doctrineCandidates(proj *v2.Projection, hd herdrState, bus busState, receipts map[string]string, joined func(hcomRow) bool) []doctrineCandidate {
	if proj == nil || !hd.available || !bus.available || joined == nil {
		return nil
	}
	var out []doctrineCandidate
	for _, correlation := range doctrineCorrelations(hd, bus) {
		if !joined(correlation.row) || managedCorrelation(proj, correlation.pane, correlation.row) {
			continue
		}
		if _, delivered := receipts[correlation.token]; delivered {
			continue
		}
		out = append(out, doctrineCandidate{Name: correlation.row.Name, Token: correlation.token})
	}
	return out
}

type doctrineCorrelation struct {
	pane  herdrcli.Pane
	row   hcomRow
	token string
}

func doctrineCorrelations(hd herdrState, bus busState) []doctrineCorrelation {
	if !hd.available || !bus.available {
		return nil
	}
	var out []doctrineCorrelation
	for term, pane := range hd.byTerm {
		if pane.PaneID == "" || pane.AgentSession == "" {
			continue
		}
		if liveCodexPID(hd.procs[term]) == 0 {
			continue
		}
		matches := make([]hcomRow, 0, 1)
		for _, row := range bus.rows {
			if row.Tool != "codex" || row.SessionID != pane.AgentSession || row.LaunchContext.PaneID != pane.PaneID || row.LaunchContext.ProcessID == "" || row.ProcessBound == nil || !*row.ProcessBound {
				continue
			}
			matches = append(matches, row)
		}
		if len(matches) != 1 {
			continue
		}
		out = append(out, doctrineCorrelation{
			pane:  pane,
			row:   matches[0],
			token: matches[0].LaunchContext.ProcessID + ":" + pane.AgentSession,
		})
	}
	return out
}

func liveCodexPID(pi herdrcli.ProcessInfo) int {
	for _, proc := range pi.Processes {
		if proc.PID > 0 && len(proc.Argv) > 0 && filepath.Base(proc.Argv[0]) == "codex" {
			return proc.PID
		}
	}
	return 0
}

func managedCorrelation(proj *v2.Projection, pane herdrcli.Pane, row hcomRow) bool {
	for _, rec := range proj.Sessions() {
		if rec.State != v2.StateSeated || rec.Seat == nil {
			continue
		}
		if rec.Seat.TerminalID == pane.TerminalID || rec.Seat.PaneID == pane.PaneID || rec.Seat.HcomName == row.Name || latestSID(rec) == pane.AgentSession {
			return true
		}
	}
	return false
}

func joinedHcomRow(row hcomRow) bool {
	out, err := exec.Command("hcom", "list", row.Name, "--json").Output()
	if err != nil {
		return false
	}
	var current hcomRow
	if json.Unmarshal(out, &current) != nil {
		var rows []hcomRow
		if json.Unmarshal(out, &rows) != nil || len(rows) != 1 {
			return false
		}
		current = rows[0]
	}
	return current.SessionID != "" && current.SessionID == row.SessionID && current.Tool == "codex"
}

func deliverDoctrine(candidates []doctrineCandidate, receipts map[string]string, send func(string) bool, now time.Time) {
	if receipts == nil || send == nil {
		return
	}
	for _, cand := range candidates {
		if _, exists := receipts[cand.Token]; exists {
			continue
		}
		if send(cand.Name) {
			receipts[cand.Token] = now.UTC().Format(time.RFC3339)
		}
	}
}

func sendDoctrine(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "hcom", "send", "@"+name, "--from", "herder-observer", "--intent", "inform", "--", hookcmd.CodexResumeAddendum)
	return cmd.Run() == nil
}

func buildCandidates(proj *v2.Projection, hd herdrState, bus busState, now time.Time) []candidate {
	overlap, recordedHerdr := herdrOverlap(proj, hd)
	var out []candidate
	for _, rec := range proj.Sessions() {
		if rec.State == v2.StateUnseated && rec.CloseResult == "observed_dead" && rec.ObservedVia != "" {
			out = append(out, candidate{guid: rec.GUID, row: rec})
			continue
		}
		if rec.State != v2.StateSeated || rec.Seat == nil {
			continue
		}
		switch rec.Seat.Kind {
		case "process":
			if processDead(rec, bus) {
				out = append(out, unseatCandidate(rec, now, "process pid gone and bus row stale", "process sweep"))
			}
		default:
			if !hd.available {
				continue
			}
			if cand, ok := sidObservationCandidate(rec, hd, bus, now); ok {
				out = append(out, cand)
				continue
			}
			pane, present := hd.byTerm[rec.Seat.TerminalID]
			if present {
				if pi, ok := hd.procs[rec.Seat.TerminalID]; ok && occupantGone(pi) {
					out = append(out, unseatCandidate(rec, now, "pane present but foreground tool process is gone", "snapshot sweep + process_info"))
					continue
				}
				if shouldReconfirm(rec, now) {
					out = append(out, reconfirmCandidate(rec, pane, bus, now))
				}
				continue
			}
			if hd.sameEpochAbsent[rec.Seat.TerminalID] {
				out = append(out, unseatCandidate(rec, now, "terminal_id absent after prior sighting on uninterrupted herdr socket connection", "socket subscription sweep"))
				continue
			}
			if recordedHerdr >= 2 && overlap == 0 {
				continue
			}
			if recordedHerdr == 1 && !busCorroboratesDead(rec, bus) {
				continue
			}
			out = append(out, unseatCandidate(rec, now, "terminal_id absent from snapshot with positive epoch/bus evidence", "snapshot sweep"))
		}
	}
	return out
}

func sidObservationCandidate(rec v2.SessionRecord, hd herdrState, bus busState, now time.Time) (candidate, bool) {
	if !observerOwnedSeat(rec) {
		return candidate{}, false
	}
	newSID := observedSID(rec, hd, bus)
	if newSID == "" {
		return candidate{}, false
	}
	priorSID := latestSID(rec)
	if priorSID == newSID {
		return candidate{}, false
	}
	identity := resolveSeatBus(rec, newSID, bus)
	if priorSID == "" {
		return recognisedCandidate(rec, newSID, identity, now), true
	}
	return turnoverCandidate(rec, newSID, identity, now), true
}

func recognisedCandidate(rec v2.SessionRecord, newSID string, identity hcomidentity.Result, now time.Time) candidate {
	stamp := now.Format(time.RFC3339)
	next := rec
	next.Event = "recognised"
	next.State = v2.StateSeated
	next.RecordedAt = stamp
	next.SIDs = append(append([]v2.SID(nil), rec.SIDs...), v2.SID{SID: newSID, ObservedAt: stamp, Source: "harvest"})
	next.Continuity = "confirmed"
	next.ObservedVia = "observer sid enrichment"
	if next.Seat != nil {
		seat := *next.Seat
		seat.ConfirmedAt = stamp
		applyBusIdentity(&seat, identity)
		next.Seat = &seat
	}
	return candidate{kind: "recognised", guid: rec.GUID, row: next, sid: newSID, bus: identity, seat: cloneSeat(next.Seat)}
}

func turnoverCandidate(rec v2.SessionRecord, newSID string, identity hcomidentity.Result, now time.Time) candidate {
	stamp := now.Format(time.RFC3339)
	old := rec
	old.Event = "unseated"
	old.RecordedAt = stamp
	old.State = v2.StateUnseated
	old.Seat = nil
	old.CloseResult = "displaced"
	old.CloseReason = "observer detected sid turnover in sidecar-less seat"
	old.ObservedVia = "observer turnover"
	seat := cloneSeat(rec.Seat)
	if seat != nil {
		applyBusIdentity(seat, identity)
	}
	return candidate{kind: "turnover", guid: rec.GUID, row: old, sid: newSID, bus: identity, seat: seat}
}

func turnoverRowsLocked(proj *v2.Projection, rec v2.SessionRecord, newSID string, identity hcomidentity.Result, now time.Time) ([]v2.SessionRecord, bool) {
	current := registry.V2ByGUID(proj, rec.GUID)
	if current == nil || current.State != v2.StateSeated || current.Seat == nil || !observerOwnedSeat(*current) {
		return nil, false
	}
	priorSID := latestSID(*current)
	if newSID == "" || priorSID == newSID || priorSID == "" || turnoverAlreadyRecorded(proj, current.GUID, newSID) {
		return nil, false
	}
	guid, err := registry.NewGUID()
	if err != nil {
		return nil, false
	}
	stamp := now.Format(time.RFC3339)
	childSeat := cloneSeat(current.Seat)
	if childSeat != nil {
		childSeat.ConfirmedAt = stamp
		applyBusIdentity(childSeat, identity)
	}
	child := v2.SessionRecord{
		Kind:       v2.KindSession,
		GUID:       guid,
		Event:      "registered",
		RecordedAt: stamp,
		State:      v2.StateSeated,
		Role:       current.Role,
		Tool:       current.Tool,
		Provider:   current.Provider,
		Model:      current.Model,
		VendorVersion: func() *v2.VendorVersionHistory {
			if current.VendorVersion == nil {
				return nil
			}
			cloned := *current.VendorVersion
			if cloned.Previous != nil {
				previous := *cloned.Previous
				cloned.Previous = &previous
			}
			return &cloned
		}(),
		Seat:       childSeat,
		SIDs:       []v2.SID{{SID: newSID, ObservedAt: stamp, Source: "harvest"}},
		Continuity: "confirmed",
		Lineage:    v2.Lineage{ClearedFrom: current.GUID},
		Provenance: v2.Provenance{
			Mechanism: "clear",
			SpawnedBy: firstNonEmpty(current.GUID, "observer"),
			CWD:       current.Provenance.CWD,
			TS:        stamp,
		},
		ObservedVia: "observer turnover",
	}
	old := *current
	old.Event = "unseated"
	old.RecordedAt = stamp
	old.State = v2.StateUnseated
	old.Seat = nil
	old.Lineage.DisplacedBy = guid
	old.CloseResult = "displaced"
	old.CloseReason = "observer detected sid turnover in sidecar-less seat"
	old.ObservedVia = "observer turnover"
	return []v2.SessionRecord{child, old}, true
}

func recognisedRowLocked(proj *v2.Projection, rec v2.SessionRecord, newSID string, identity hcomidentity.Result, now time.Time) (v2.SessionRecord, bool) {
	current := registry.V2ByGUID(proj, rec.GUID)
	if current == nil || current.State != v2.StateSeated || current.Seat == nil || !observerOwnedSeat(*current) {
		return v2.SessionRecord{}, false
	}
	priorSID := latestSID(*current)
	if newSID == "" || priorSID == newSID || priorSID != "" {
		return v2.SessionRecord{}, false
	}
	return recognisedCandidate(*current, newSID, identity, now).row, true
}

func observerOwnedSeat(rec v2.SessionRecord) bool {
	return rec.Seat != nil && rec.Seat.Kind != "process" && rec.Provenance.Mechanism == "enroll"
}

func observedSID(rec v2.SessionRecord, hd herdrState, bus busState) string {
	if rec.Seat == nil {
		return ""
	}
	if pane, ok := hd.byTerm[rec.Seat.TerminalID]; ok && pane.AgentSession != "" {
		return pane.AgentSession
	}
	if identity := resolveSeatBus(rec, "", bus); identity.Verified {
		return identity.SessionID
	}
	return ""
}

func resolveSeatBus(rec v2.SessionRecord, sessionID string, bus busState) hcomidentity.Result {
	if !bus.available || rec.Seat == nil {
		return hcomidentity.Result{Reason: "live bus roster unavailable"}
	}
	rows := bus.roster
	if rows == nil {
		rows = make([]hcomidentity.Row, 0, len(bus.rows))
		for _, row := range bus.rows {
			rows = append(rows, row)
		}
	}
	return hcomidentity.Resolve(rows, hcomidentity.Evidence{SessionID: sessionID, PaneIDs: []string{rec.Seat.PaneID}})
}

func applyBusIdentity(seat *v2.Seat, identity hcomidentity.Result) {
	if seat == nil {
		return
	}
	verified := identity.Verified
	seat.HcomVerified = &verified
	if identity.Verified {
		seat.HcomName = identity.Name
	}
}

func latestSID(rec v2.SessionRecord) string {
	if len(rec.SIDs) == 0 {
		return ""
	}
	return rec.SIDs[len(rec.SIDs)-1].SID
}

func turnoverAlreadyRecorded(proj *v2.Projection, clearedFrom, sid string) bool {
	for _, rec := range proj.Sessions() {
		if rec.Lineage.ClearedFrom != clearedFrom {
			continue
		}
		if latestSID(rec) == sid {
			return true
		}
	}
	return false
}

func cloneSeat(seat *v2.Seat) *v2.Seat {
	if seat == nil {
		return nil
	}
	cp := *seat
	return &cp
}

func unseatCandidate(rec v2.SessionRecord, now time.Time, reason, via string) candidate {
	next := rec
	next.Event = "unseated"
	next.State = v2.StateUnseated
	next.RecordedAt = now.Format(time.RFC3339)
	next.Seat = nil
	next.CloseResult = "observed_dead"
	next.CloseReason = reason
	next.ObservedVia = via
	return candidate{kind: "unseat", guid: rec.GUID, row: next}
}

func reconfirmCandidate(rec v2.SessionRecord, pane herdrcli.Pane, bus busState, now time.Time) candidate {
	next := rec
	next.Event = "reconciled"
	next.State = v2.StateSeated
	next.RecordedAt = now.Format(time.RFC3339)
	next.ObservedVia = "snapshot sweep"
	identity := hcomidentity.Result{}
	if next.Seat != nil {
		seat := *next.Seat
		seat.ConfirmedAt = next.RecordedAt
		if pane.PaneID != "" {
			seat.PaneID = pane.PaneID
		}
		current := rec
		current.Seat = &seat
		identity = resolveSeatBus(current, latestSID(rec), bus)
		if identity.Verified || seat.HcomName != "" {
			applyBusIdentity(&seat, identity)
		}
		next.Seat = &seat
	}
	kind := "confirm"
	if !sameBindingCoordinates(rec.Seat, next.Seat) {
		kind = "reconfirm"
	}
	return candidate{kind: kind, guid: rec.GUID, row: next, bus: identity, seat: cloneSeat(next.Seat)}
}

func sameBindingCoordinates(a, b *v2.Seat) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Kind == b.Kind && a.Node == b.Node && a.TerminalID == b.TerminalID && a.PaneID == b.PaneID && a.PID == b.PID && a.Namespace == b.Namespace && a.HcomName == b.HcomName && boolValue(a.HcomVerified) == boolValue(b.HcomVerified)
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func applyCandidates(path string, cands []candidate, stderr io.Writer) observerstatus.Summary {
	var summary observerstatus.Summary
	plain := make([]candidate, 0, len(cands))
	for _, cand := range cands {
		if cand.kind != "recognised" && cand.kind != "turnover" && cand.kind != "reconfirm" {
			plain = append(plain, cand)
			continue
		}
		status, err := completeRecognition(path, cand)
		if err != nil {
			fmt.Fprintf(stderr, "herder observer sweep: candidate %s refused: %v\n", cand.guid, err)
			summary.Refused++
			continue
		}
		switch status {
		case registry.WriteApplied:
			summary.Applied++
		case registry.WriteNoop:
			summary.Noop++
		case registry.WriteRefused:
			summary.Refused++
		}
	}
	plainSummary := applyPlainCandidates(path, plain, stderr)
	summary.Applied += plainSummary.Applied
	summary.Noop += plainSummary.Noop
	summary.Refused += plainSummary.Refused
	return summary
}

func applyPlainCandidates(path string, cands []candidate, stderr io.Writer) observerstatus.Summary {
	var summary observerstatus.Summary
	if len(cands) == 0 {
		return summary
	}
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		rows := make([]v2.SessionRecord, 0, len(cands))
		for _, cand := range cands {
			if current := registry.V2ByGUID(tx.Projection, cand.guid); current != nil && cand.row.Event == "unseated" && (current.State == v2.StateRetired || current.State == v2.StateLost) {
				continue
			}
			rows = append(rows, cand.row)
		}
		return rows, nil
	})
	if err != nil {
		fmt.Fprintf(stderr, "herder observer sweep: refused %d candidate(s): %v\n", len(cands), err)
		summary.Refused = len(cands)
		return summary
	}
	if len(outcomes) == 0 {
		summary.Noop = len(cands)
		return summary
	}
	batchRefused := false
	for _, outcome := range outcomes {
		if outcome.Status == registry.WriteRefused {
			batchRefused = true
			break
		}
	}
	if batchRefused {
		summary.Refused = len(cands)
		return summary
	}
	for _, outcome := range outcomes {
		if outcome.Status == registry.WriteApplied {
			summary.Applied++
		} else {
			summary.Noop++
		}
	}
	summary.Noop += len(cands) - len(outcomes)
	return summary
}

func completeRecognition(path string, cand candidate) (registry.WriteStatus, error) {
	if cand.seat == nil {
		return registry.WriteRefused, errors.New("recognition completion requires a live seat")
	}
	seat := *cand.seat
	engine := seatcompletion.DefaultEngine()
	observedPane := seatcompletion.LivePane{PaneID: seat.PaneID, TerminalID: seat.TerminalID}
	request := seatcompletion.Request{
		Origin:       seatcompletion.OriginRecognition,
		RegistryPath: path,
		Candidate:    cand.row,
		Seat:         seatcompletion.SeatClaim{Kind: seatcompletion.SeatHerdr, PaneID: seat.PaneID},
		ObservedPane: &observedPane,
		ObservedBus:  &cand.bus,
		Namespace:    seat.Namespace,
		Evidence:     hcomidentity.Evidence{SessionID: cand.bus.SessionID, PaneIDs: []string{seat.PaneID}},
	}
	request.BuildLocked = func(tx registry.LockedUpdate, _ v2.Seat) (v2.SessionRecord, []v2.SessionRecord, []v2.SessionRecord, error) {
		now := time.Now().UTC()
		switch cand.kind {
		case "turnover":
			rows, ok := turnoverRowsLocked(tx.Projection, cand.row, cand.sid, cand.bus, now)
			if !ok || len(rows) != 2 {
				return v2.SessionRecord{}, nil, nil, errors.New("turnover no longer matches the live registry state")
			}
			return rows[0], nil, rows[1:], nil
		case "recognised":
			row, ok := recognisedRowLocked(tx.Projection, cand.row, cand.sid, cand.bus, now)
			if !ok {
				return v2.SessionRecord{}, nil, nil, errors.New("recognition no longer matches the live registry state")
			}
			return row, nil, nil, nil
		default:
			current := registry.V2ByGUID(tx.Projection, cand.guid)
			if current == nil || current.State != v2.StateSeated {
				return v2.SessionRecord{}, nil, nil, errors.New("reconfirmation no longer matches a seated registry row")
			}
			return cand.row, nil, nil, nil
		}
	}
	result, err := engine.Complete(context.Background(), request)
	if err != nil {
		return registry.WriteRefused, err
	}
	if result.Refusal != nil {
		return registry.WriteRefused, result.Refusal
	}
	status := registry.WriteNoop
	for _, outcome := range result.Outcomes {
		if err := outcome.Err(); err != nil {
			return registry.WriteRefused, err
		}
		if outcome.Status == registry.WriteApplied {
			status = registry.WriteApplied
		}
	}
	return status, nil
}

func advisoryFlags(proj *v2.Projection, hd herdrState) []observerstatus.Flag {
	if !hd.available {
		return nil
	}
	var flags []observerstatus.Flag
	for _, rec := range proj.Sessions() {
		if rec.State != v2.StateUnseated {
			continue
		}
		var matches []herdrcli.Pane
		for _, pane := range hd.byTerm {
			if rec.Label == "" || pane.Label == "" || rec.Label != pane.Label {
				continue
			}
			matches = append(matches, pane)
		}
		if len(matches) > 1 {
			flags = append(flags, observerstatus.Flag{
				GUID:      rec.GUID,
				Label:     rec.Label,
				Type:      "ambiguous-dormant-live",
				Severity:  "warning",
				Detail:    "multiple live panes match this unseated row label; observer refuses to guess",
				Suggested: "herder enroll explicitly from the intended pane",
			})
			continue
		}
		if len(matches) == 1 {
			pane := matches[0]
			flags = append(flags, observerstatus.Flag{
				GUID:       rec.GUID,
				Label:      rec.Label,
				Type:       "dormant-live",
				Severity:   "warning",
				Detail:     "unseated registry row has live matching pane label",
				Suggested:  "herder enroll or herder reconcile --apply",
				TerminalID: pane.TerminalID,
				PaneID:     pane.PaneID,
			})
		}
	}
	return flags
}

func continuationFailureFlags(proj *v2.Projection, stateDir string, stderr io.Writer) []observerstatus.Flag {
	records, warnings, err := continuationstate.Unresolved(filepath.Join(stateDir, "continuations"))
	for _, warning := range warnings {
		fmt.Fprintf(stderr, "herder observer sweep: ignoring continuation record: %v\n", warning)
	}
	if err != nil {
		fmt.Fprintf(stderr, "herder observer sweep: continuation state unavailable: %v\n", err)
		return nil
	}
	flags := make([]observerstatus.Flag, 0, len(records))
	for _, rec := range records {
		guid, label, ok := continuationTarget(proj, rec.Target)
		if !ok {
			continue
		}
		flags = append(flags, observerstatus.Flag{
			GUID:      guid,
			Label:     label,
			Type:      "failed-continuation",
			Severity:  "warning",
			Detail:    fmt.Sprintf("detached continuation %s failed at %s: %s (log: %s)", rec.ID, rec.UpdatedAt, rec.Reason, rec.LogPath),
			Suggested: rec.RecoveryCommand + "; then herder list --ack-continuation " + rec.ID,
		})
	}
	return flags
}

func continuationTarget(proj *v2.Projection, target string) (string, string, bool) {
	var guid, label string
	for _, rec := range proj.Sessions() {
		// Normal writes strip seats from retired/lost rows; this guard is belt-and-braces
		// for externally authored rows and future writers that might retain stale seats.
		if rec.State == v2.StateRetired || rec.State == v2.StateLost || rec.Seat == nil || rec.Seat.HcomName != target {
			continue
		}
		if guid != "" {
			return "", "", false
		}
		guid = rec.GUID
		label = rec.Label
	}
	return guid, label, guid != ""
}

func epochFlags(proj *v2.Projection, hd herdrState, bus busState) []observerstatus.Flag {
	if !hd.available {
		return nil
	}
	if !hd.connectionGap {
		return nil
	}
	overlap, recorded := herdrOverlap(proj, hd)
	if recorded >= 2 && overlap == 0 {
		return []observerstatus.Flag{{
			Type:      "epoch-doubt",
			Severity:  "warning",
			Detail:    "no recorded seated terminal ids appear in the current snapshot; absence verdicts paused",
			Suggested: "herder reconcile",
		}}
	}
	if recorded == 1 && overlap == 0 {
		for _, rec := range proj.Sessions() {
			if rec.State != v2.StateSeated || rec.Seat == nil || rec.Seat.Kind == "process" {
				continue
			}
			if busCorroboratesDead(rec, bus) {
				continue
			}
			return []observerstatus.Flag{{
				GUID:       rec.GUID,
				Label:      rec.Label,
				Type:       "epoch-doubt",
				Severity:   "warning",
				Detail:     "single recorded terminal absent after a connection gap without dead-bus corroboration",
				Suggested:  "herder reconcile",
				TerminalID: rec.Seat.TerminalID,
				PaneID:     rec.Seat.PaneID,
			}}
		}
	}
	return nil
}

func herdrOverlap(proj *v2.Projection, hd herdrState) (int, int) {
	if !hd.available {
		return 0, 0
	}
	overlap := 0
	recorded := 0
	for _, rec := range proj.Sessions() {
		if rec.State != v2.StateSeated || rec.Seat == nil || rec.Seat.Kind == "process" || rec.Seat.TerminalID == "" {
			continue
		}
		recorded++
		if _, ok := hd.byTerm[rec.Seat.TerminalID]; ok {
			overlap++
		}
	}
	return overlap, recorded
}

func occupantGone(pi herdrcli.ProcessInfo) bool {
	return len(pi.Processes) == 0
}

func shouldReconfirm(rec v2.SessionRecord, now time.Time) bool {
	if rec.Seat == nil || rec.Seat.ConfirmedAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, rec.Seat.ConfirmedAt)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05Z", rec.Seat.ConfirmedAt)
	}
	if err != nil {
		return true
	}
	return now.Sub(t) >= reconfirmInterval()
}

func processDead(rec v2.SessionRecord, bus busState) bool {
	if rec.Seat == nil || rec.Seat.PID == 0 {
		return false
	}
	if err := syscall.Kill(rec.Seat.PID, 0); err == nil {
		return false
	}
	return busCorroboratesDead(rec, bus)
}

func busCorroboratesDead(rec v2.SessionRecord, bus busState) bool {
	if !bus.available || rec.Seat == nil || rec.Seat.HcomName == "" {
		return false
	}
	row, ok := bus.rows[rec.Seat.HcomName]
	if !ok {
		return false
	}
	if row.ProcessBound != nil && !*row.ProcessBound {
		return true
	}
	return row.StatusAge > 300 && row.Status != "working" && row.Status != "idle"
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
		fmt.Fprintf(stdout, "observer advice: %s %s %s\n", firstNonEmpty(flag.GUID, flag.Label, "-"), flag.Type, flag.Detail)
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

func reconfirmInterval() time.Duration {
	return durationEnv("HERDER_OBSERVER_RECONFIRM_INTERVAL", defaultReconfirmInterval)
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
