package listcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type options struct {
	help       bool
	mode       string
	includeAll bool
	targetGUID string
}

func Run(args []string, stdout, stderr io.Writer) int {
	if _, err := exec.LookPath("herdr"); err != nil {
		die(stderr, "herdr not on PATH")
		return 1
	}
	if _, err := exec.LookPath("jq"); err != nil {
		die(stderr, "jq not on PATH")
		return 1
	}

	opts, code := parseArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.help {
		return 0
	}

	if opts.mode == "teams" {
		return runTeams(stdout)
	}

	registryPath := registry.DefaultPath()
	if _, err := os.Stat(registryPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
		out := reconciledJSON(rec, idx)
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
			if !opts.includeAll && (rec.Status != "active" || rec.Archived) {
				continue
			}
			fmt.Fprintln(stdout, string(reconciledJSON(rec, idx)))
		}
		return 0
	}

	fmt.Fprintf(stdout, "%-10s %-20s %-7s %-18s %-9s %-12s %-16s %s\n",
		"GUID", "LABEL", "AGENT", "PANE", "LIVE", "TEAM", "BUS", "ROLE")
	for _, rec := range collapsed {
		if !opts.includeAll && (rec.Status != "active" || rec.Archived) {
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
		fmt.Fprintf(stdout, "%-10s %-20s %-7s %-18s %-9s %-12s %-16s %s\n",
			ptrString(rec.ShortGUID), ptrString(rec.Label), rec.Agent, livePane, liveStatus, team, bus, rec.Role)
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
  herder list --json       reconciled records as JSONL on stdout
  herder list --raw        raw registry JSONL, no reconciliation
  herder list --guid GUID  one record as full JSON (exit 1 if not found)
  herder list --teams      list team buses under $HERDER_TEAMS_ROOT

Use --all to check whether a missing agent was culled.
`)
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

func reconciledJSON(rec registry.Record, idx liveIndex) []byte {
	if rec.Archived {
		return appendJSONFields(rec.Raw,
			`"archived":true`,
			`"live":null`,
			`"live_pane":null`,
			`"live_status":"ARCHIVED"`,
			`"live_matched_by":null`,
		)
	}
	live, matchedBy := idx.match(rec)
	if live == nil {
		return appendJSONFields(rec.Raw,
			`"live":null`,
			`"live_pane":null`,
			`"live_status":`+jsonString(idx.unmatchedStatus(rec)),
			`"live_matched_by":null`,
		)
	}
	livePane := "null"
	if pane, ok := rawStringField(live.Raw, "pane_id"); ok {
		livePane = jsonString(pane)
	}
	liveStatus := "gone"
	if status, ok := rawStringField(live.Raw, "agent_status"); ok {
		liveStatus = status
	}
	return appendJSONFields(rec.Raw,
		`"live":`+string(live.Raw),
		`"live_pane":`+livePane,
		`"live_status":`+jsonString(liveStatus),
		`"live_matched_by":`+jsonString(matchedBy),
	)
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
