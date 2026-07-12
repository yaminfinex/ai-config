package listcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ai-config/tools/herder/internal/continuationstate"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/observerstatus"
	"ai-config/tools/herder/internal/registry"
)

const observerGlobalAdviceKey = "*"
const contextSnapshotFreshFor = 15 * time.Minute

type options struct {
	help       bool
	mode       string
	includeAll bool
	targetGUID string
	ackID      string
}

func Run(args []string, stdout, stderr io.Writer) int {
	opts, code := parseArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.help {
		return 0
	}
	if opts.ackID != "" {
		rec, err := continuationstate.Acknowledge("", opts.ackID, time.Now())
		if err != nil {
			fmt.Fprintf(stderr, "herder list: cannot acknowledge continuation %s: %v. Run `herder list` to inventory unresolved failures.\n", opts.ackID, err)
			return 1
		}
		fmt.Fprintf(stdout, "acknowledged failed continuation %s for @%s; if recovery is still needed, run:\n  %s\n", rec.ID, rec.Target, rec.RecoveryCommand)
		return 0
	}
	if _, err := exec.LookPath("herdr"); err != nil {
		die(stderr, "herdr not on PATH")
		return 1
	}
	if _, err := exec.LookPath("jq"); err != nil {
		die(stderr, "jq not on PATH")
		return 1
	}

	if opts.mode == "teams" {
		return runTeams(stdout)
	}
	var failures []continuationstate.Record
	if opts.mode == "table" || opts.mode == "json" {
		var warnings []error
		var err error
		failures, warnings, err = continuationstate.Unresolved("")
		for _, warning := range warnings {
			fmt.Fprintf(stderr, "herder list: ignoring continuation record: %v\n", warning)
		}
		if err != nil {
			fmt.Fprintf(stderr, "herder list: continuation state unavailable: %v. Inspect %s for durable records.\n", err, continuationstate.DefaultDir())
		}
	}
	if opts.mode == "table" {
		renderContinuationFailures(stdout, failures)
	}

	registryPath := registry.DefaultPath()
	if _, err := os.Stat(registryPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if opts.mode == "json" {
				renderJSONContinuationFailures(stdout, stderr, failures)
			}
			fmt.Fprintf(stderr, "no registry at %s\n", registryPath)
			return 0
		}
		return 1
	}

	if opts.mode == "raw" {
		data, err := os.ReadFile(registryPath)
		if err != nil {
			return 1
		}
		_, _ = stdout.Write(data)
		return 0
	}

	idx := buildLiveIndex()
	advice := loadObserverAdvice()

	if opts.mode == "one" {
		if opts.targetGUID == "" {
			die(stderr, "--guid requires a value")
			return 1
		}
		rec, ok := lastOwnGUIDRecord(registryPath, opts.targetGUID)
		if !ok {
			fmt.Fprintf(stderr, "no record for guid %s\n", opts.targetGUID)
			return 1
		}
		out := reconciledJSON(rec, idx, observerAdviceFor(advice, ptrString(rec.GUID)))
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, out, "", "  "); err != nil {
			return 1
		}
		pretty.WriteByte('\n')
		_, _ = stdout.Write(pretty.Bytes())
		return 0
	}

	recs, err := registry.Load(registryPath)
	if opts.includeAll {
		recs, err = registry.LoadWithArchives(registryPath)
	}
	if err != nil {
		return 1
	}
	collapsed := registry.LatestByGUID(recs)
	if opts.mode == "json" {
		for _, rec := range collapsed {
			if !opts.includeAll && (!registry.IsNonRetired(rec) || rec.Archived) {
				continue
			}
			out := reconciledJSON(rec, idx, observerAdviceFor(advice, ptrString(rec.GUID)))
			fmt.Fprintln(stdout, string(out))
		}
		renderJSONContinuationFailures(stdout, stderr, failures)
		return 0
	}

	now := time.Now()
	fmt.Fprintf(stdout, "%-10s %-20s %-7s %-18s %-9s %-12s %-16s %-11s %s\n",
		"GUID", "LABEL", "AGENT", "PANE", "LIVE", "TEAM", "BUS", "CTX", "ROLE")
	for _, rec := range collapsed {
		if !opts.includeAll && (!registry.IsNonRetired(rec) || rec.Archived) {
			continue
		}
		live, _ := idx.match(rec)
		livePane := rec.PaneID
		liveStatus := idx.unmatchedStatus(rec)
		if rec.Archived {
			liveStatus = "ARCHIVED"
		} else if live != nil {
			if pane, ok := rawStringField(live.Raw, "pane_id"); ok {
				livePane = pane
			}
			liveStatus = "gone"
			if status, ok := rawStringField(live.Raw, "agent_status"); ok {
				liveStatus = status
			}
		}
		team := rec.Team
		if team == "" {
			team = "global"
		}
		bus := "-"
		if rec.HcomName != "" && rec.HcomName != "null" {
			bus = "@" + rec.HcomName
		}
		role := rec.Role
		if flags := observerAdviceFor(advice, ptrString(rec.GUID)); len(flags) > 0 {
			role = role + observerAdviceSuffix(flags)
		}
		ctx := contextSnapshotDisplay(rec, now)
		fmt.Fprintf(stdout, "%-10s %-20s %-7s %-18s %-9s %-12s %-16s %-11s %s\n",
			ptrString(rec.ShortGUID), ptrString(rec.Label), rec.Agent, livePane, liveStatus, team, bus, ctx, role)
	}
	return 0
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	opts := options{mode: "table"}
	for i := 0; i < len(args); {
		switch args[i] {
		case "--all":
			opts.includeAll = true
			i++
		case "--json":
			opts.mode = "json"
			i++
		case "--raw":
			opts.mode = "raw"
			i++
		case "--guid":
			opts.mode = "one"
			if i+1 < len(args) {
				opts.targetGUID = args[i+1]
			}
			i += 2
		case "--teams":
			opts.mode = "teams"
			i++
		case "--ack-continuation":
			if i+1 >= len(args) {
				die(stderr, "--ack-continuation requires an id")
				return opts, 1
			}
			opts.ackID = args[i+1]
			i += 2
		case "-h", "--help":
			printHelp(stdout)
			opts.help = true
			return opts, 0
		default:
			die(stderr, "unknown arg: "+args[i])
			return opts, 1
		}
	}
	return opts, 0
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder list — show spawned agents, reconciled with live herdr state.

Usage:
  herder list              table of active records, reconciled with live agents
  herder list --all        include records whose status is not active (e.g. closed)
  herder list --json       reconciled sessions and unresolved failures as JSONL
  herder list --raw        raw registry JSONL, no reconciliation
  herder list --guid GUID  one record as full JSON (exit 1 if not found)
  herder list --teams      list team buses under $HERDER_TEAMS_ROOT
  herder list --ack-continuation ID
                           acknowledge a failed detached continuation after recovery

The table lists unresolved detached continuation failures before agent rows.
Run the recorded recovery command, then use --ack-continuation to clear the
failure from list and observer advice. Use --all to check whether a missing
agent was culled.

In --json output, select kind=="session" for session rows; for compatibility,
a row without kind is also a session row. Unresolved failures have
kind=="unresolved_continuation" and are independent of session-row filters.
`)
}

func renderContinuationFailures(stdout io.Writer, failures []continuationstate.Record) {
	if len(failures) == 0 {
		return
	}
	fmt.Fprintln(stdout, "UNRESOLVED DETACHED CONTINUATIONS")
	for _, rec := range failures {
		fmt.Fprintf(stdout, "  %s  @%s  failed %s: %s\n", rec.ID, rec.Target, rec.UpdatedAt, rec.Reason)
		fmt.Fprintf(stdout, "    recovery: %s\n", rec.RecoveryCommand)
		fmt.Fprintf(stdout, "    log: %s\n", rec.LogPath)
		fmt.Fprintf(stdout, "    after recovery: herder list --ack-continuation %s\n", rec.ID)
	}
	fmt.Fprintln(stdout)
}

func runTeams(stdout io.Writer) int {
	home, _ := os.UserHomeDir()
	root := os.Getenv("HERDER_TEAMS_ROOT")
	if root == "" {
		root = filepath.Join(home, ".hcom", "teams")
	}
	fmt.Fprintf(stdout, "%-20s %s\n", "TEAM", "HCOM_DIR")
	fmt.Fprintf(stdout, "%-20s %s\n", "global", filepath.Join(home, ".hcom"))
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fmt.Fprintf(stdout, "%-20s %s\n", entry.Name(), filepath.Join(root, entry.Name()))
	}
	return 0
}

// liveIndex resolves registry rows to live herdr agents. Terminal ids are the
// durable primary key, but a server update --handoff reissues them (and pane
// ids), stranding every pre-handoff row (TASK-046) — so lookup falls back to
// the row's stored pane_id and then to `agent list`'s name (== the undecorated
// spawn label on 0.7.x). paneTerms/panePanes hold `pane list` coordinates so a
// row whose pane is alive but absent from `agent list` (herdr lost agent
// detection for the process, e.g. it predates a handoff) reads as
// "undetected", not "gone".
type liveIndex struct {
	byTerm    map[string]*herdrcli.Agent
	byPane    map[string]*herdrcli.Agent
	byName    map[string]*herdrcli.Agent
	paneTerms map[string]bool
	panePanes map[string]bool
}

func buildLiveIndex() liveIndex {
	client := &herdrcli.Client{}
	out, err := client.Output("agent", "list")
	if err != nil {
		out = []byte(`{"result":{"agents":[]}}`)
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		agents = nil
	}
	idx := liveIndex{
		byTerm:    make(map[string]*herdrcli.Agent),
		byPane:    make(map[string]*herdrcli.Agent),
		byName:    make(map[string]*herdrcli.Agent),
		paneTerms: make(map[string]bool),
		panePanes: make(map[string]bool),
	}
	nameSeen := make(map[string]int)
	for i := range agents {
		var compact bytes.Buffer
		if err := json.Compact(&compact, agents[i].Raw); err == nil {
			agents[i].Raw = compact.Bytes()
		}
		if agents[i].TerminalID != nil {
			idx.byTerm[*agents[i].TerminalID] = &agents[i]
		}
		if agents[i].PaneID != "" {
			idx.byPane[agents[i].PaneID] = &agents[i]
		}
		if agents[i].Name != "" {
			nameSeen[agents[i].Name]++
			idx.byName[agents[i].Name] = &agents[i]
		}
	}
	// A duplicated live name can never be a safe fallback key.
	for name, n := range nameSeen {
		if n > 1 {
			delete(idx.byName, name)
		}
	}
	if paneOut, err := client.Output("pane", "list"); err == nil {
		if panes, err := herdrcli.ParsePaneList(paneOut); err == nil {
			for _, pane := range panes {
				if pane.TerminalID != "" {
					idx.paneTerms[pane.TerminalID] = true
				}
				if pane.PaneID != "" {
					idx.panePanes[pane.PaneID] = true
				}
			}
		}
	}
	return idx
}

// match resolves a record to its live agent: terminal_id first (durable within
// a server generation), then the stored pane_id (new-format ids don't recycle),
// then the unambiguous live name equal to the row's label. matchedBy reports
// which key hit so JSON consumers can distinguish a primary match from a
// fallback that survived a coordinate epoch change.
func (idx liveIndex) match(rec registry.Record) (*herdrcli.Agent, string) {
	if rec.TerminalID != "" {
		if live := idx.byTerm[rec.TerminalID]; live != nil {
			return live, "terminal"
		}
	}
	if rec.PaneID != "" {
		if live := idx.byPane[rec.PaneID]; live != nil {
			return live, "pane"
		}
	}
	if label := ptrString(rec.Label); label != "" {
		if live := idx.byName[label]; live != nil {
			return live, "name"
		}
	}
	return nil, ""
}

// unmatchedStatus distinguishes a row whose pane is gone from one whose pane
// is alive but invisible to `agent list` (detection lost; only a process
// restart/re-report recovers real status).
func (idx liveIndex) unmatchedStatus(rec registry.Record) string {
	if (rec.TerminalID != "" && idx.paneTerms[rec.TerminalID]) ||
		(rec.PaneID != "" && idx.panePanes[rec.PaneID]) {
		return "undetected"
	}
	return "gone"
}

func lastOwnGUIDRecord(path, target string) (registry.Record, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return registry.Record{}, false
	}
	var hit registry.Record
	ok := false
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		rec, err := decodeRecord(line)
		if err != nil {
			continue
		}
		if ptrString(rec.GUID) == target || ptrString(rec.ShortGUID) == target {
			hit = rec
			ok = true
		}
	}
	return hit, ok
}

func decodeRecord(line string) (registry.Record, error) {
	var rec registry.Record
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return registry.Record{}, err
	}
	rec.Raw = append([]byte(nil), line...)
	return rec, nil
}

func reconciledJSON(rec registry.Record, idx liveIndex, advice []observerstatus.Flag) []byte {
	var adviceFields []string
	if len(advice) > 0 {
		if b, err := json.Marshal(advice); err == nil {
			adviceFields = append(adviceFields, `"observer_advice":`+string(b))
		}
	}
	if rec.Archived {
		fields := append([]string{
			`"archived":true`,
			`"live":null`,
			`"live_pane":null`,
			`"live_status":"ARCHIVED"`,
			`"live_matched_by":null`,
		}, adviceFields...)
		return appendJSONFields(rec.Raw, fields...)
	}
	live, matchedBy := idx.match(rec)
	if live == nil {
		fields := append([]string{
			`"live":null`,
			`"live_pane":null`,
			`"live_status":` + jsonString(idx.unmatchedStatus(rec)),
			`"live_matched_by":null`,
		}, adviceFields...)
		return appendJSONFields(rec.Raw, fields...)
	}
	livePane := "null"
	if pane, ok := rawStringField(live.Raw, "pane_id"); ok {
		livePane = jsonString(pane)
	}
	liveStatus := "gone"
	if status, ok := rawStringField(live.Raw, "agent_status"); ok {
		liveStatus = status
	}
	fields := append([]string{
		`"live":` + string(live.Raw),
		`"live_pane":` + livePane,
		`"live_status":` + jsonString(liveStatus),
		`"live_matched_by":` + jsonString(matchedBy),
	}, adviceFields...)
	return appendJSONFields(rec.Raw, fields...)
}

func renderJSONContinuationFailures(stdout, stderr io.Writer, failures []continuationstate.Record) {
	renderJSONContinuationFailuresWith(stdout, stderr, failures, json.Marshal)
}

func renderJSONContinuationFailuresWith(stdout, stderr io.Writer, failures []continuationstate.Record, marshal func(any) ([]byte, error)) {
	for _, failure := range failures {
		out, err := marshal(struct {
			Kind string `json:"kind"`
			continuationstate.Record
		}{Kind: "unresolved_continuation", Record: failure})
		if err != nil {
			fmt.Fprintf(stderr, "herder list: cannot render unresolved continuation %s as JSON: %v. Inspect %s and retry after correcting the serialization failure.\n", failure.ID, err, continuationstate.DefaultDir())
			continue
		}
		fmt.Fprintln(stdout, string(out))
	}
}

func loadObserverAdvice() map[string][]observerstatus.Flag {
	st, err := observerstatus.Read(observerstatus.DefaultPath())
	if err != nil {
		return map[string][]observerstatus.Flag{}
	}
	out := observerstatus.FlagsByGUID(st)
	for _, flag := range observerstatus.GlobalFlags(st) {
		out[observerGlobalAdviceKey] = append(out[observerGlobalAdviceKey], flag)
	}
	return out
}

func observerAdviceFor(advice map[string][]observerstatus.Flag, guid string) []observerstatus.Flag {
	flags := append([]observerstatus.Flag{}, advice[guid]...)
	flags = append(flags, advice[observerGlobalAdviceKey]...)
	return flags
}

func observerAdviceSuffix(flags []observerstatus.Flag) string {
	var parts []string
	for _, flag := range flags {
		switch flag.Type {
		case "dormant-live":
			parts = append(parts, "observer advice: live occupant observed")
		case "epoch-doubt":
			parts = append(parts, "observer advice: epoch doubt")
		default:
			parts = append(parts, "observer advice: "+flag.Type)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, "; ") + "]"
}

type contextSnapshot struct {
	Pct   string
	TS    int64
	Stale bool
}

func contextSnapshotDisplay(rec registry.Record, now time.Time) string {
	snap, ok := readContextSnapshot(rec, now)
	if !ok {
		return "unknown"
	}
	if snap.Stale {
		return snap.Pct + "% stale"
	}
	return snap.Pct + "%"
}

func readContextSnapshot(rec registry.Record, now time.Time) (contextSnapshot, bool) {
	name := rec.HcomName
	if rec.HcomDir == "" || name == "" || name == "null" {
		return contextSnapshot{}, false
	}
	safeName, ok := safeStatuslineSnapshotName(name)
	if !ok {
		return contextSnapshot{}, false
	}
	vals, err := readStatuslineEnv(filepath.Join(rec.HcomDir, "statusline", safeName+".env"))
	if err != nil {
		return contextSnapshot{}, false
	}
	pct, ok := parseSnapshotPercent(vals["CTX_PCT"])
	if !ok {
		return contextSnapshot{}, false
	}
	ts, ok := parseSnapshotUnix(vals["CTX_TS"])
	if !ok {
		return contextSnapshot{}, false
	}
	nowUnix := now.Unix()
	if ts > nowUnix+int64(contextSnapshotFreshFor/time.Second) {
		return contextSnapshot{}, false
	}
	age := nowUnix - ts
	if age < 0 {
		age = 0
	}
	return contextSnapshot{Pct: pct, TS: ts, Stale: age > int64(contextSnapshotFreshFor/time.Second)}, true
}

func safeStatuslineSnapshotName(name string) (string, bool) {
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return "", false
	}
	return name, true
}

func readStatuslineEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	vals := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[key] = value
	}
	return vals, nil
}

func parseSnapshotUnix(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 0
}

func parseSnapshotPercent(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) || n < 0 || n > 100 {
		return "", false
	}
	return strconv.FormatFloat(math.Round(n), 'f', 0, 64), true
}

func appendJSONFields(raw []byte, fields ...string) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return raw
	}
	out := append([]byte(nil), trimmed[:len(trimmed)-1]...)
	if len(bytes.TrimSpace(trimmed[1:len(trimmed)-1])) > 0 {
		out = append(out, ',')
	}
	out = append(out, strings.Join(fields, ",")...)
	out = append(out, '}')
	return out
}

func rawStringField(raw []byte, key string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", false
	}
	val, ok := obj[key]
	if !ok || bytes.Equal(bytes.TrimSpace(val), []byte("null")) {
		return "", false
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		return "", false
	}
	return s, true
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder list: %s\n", msg)
}
