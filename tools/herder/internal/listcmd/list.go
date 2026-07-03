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

	liveByTerm := liveAgentsByTerminal()

	if opts.mode == "one" {
		if opts.targetGUID == "" {
			die(stderr, "--guid requires a value")
			return 1
		}
		line, ok := grepLastQuoted(registryPath, opts.targetGUID)
		if !ok {
			fmt.Fprintf(stderr, "no record for guid %s\n", opts.targetGUID)
			return 1
		}
		rec, err := decodeRecord(line)
		if err != nil {
			return 1
		}
		out := reconciledJSON(rec, liveByTerm)
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, out, "", "  "); err != nil {
			return 1
		}
		pretty.WriteByte('\n')
		_, _ = stdout.Write(pretty.Bytes())
		return 0
	}

	recs, err := registry.Load(registryPath)
	if err != nil {
		return 1
	}
	collapsed := registry.LatestByGUID(recs)
	if opts.mode == "json" {
		for _, rec := range collapsed {
			if !opts.includeAll && rec.Status != "active" {
				continue
			}
			fmt.Fprintln(stdout, string(reconciledJSON(rec, liveByTerm)))
		}
		return 0
	}

	fmt.Fprintf(stdout, "%-10s %-20s %-7s %-18s %-9s %-12s %-16s %s\n",
		"GUID", "LABEL", "AGENT", "PANE", "LIVE", "TEAM", "BUS", "ROLE")
	for _, rec := range collapsed {
		if !opts.includeAll && rec.Status != "active" {
			continue
		}
		live := liveByTerm[rec.TerminalID]
		livePane := rec.PaneID
		liveStatus := "gone"
		if live != nil {
			if pane, ok := rawStringField(live.Raw, "pane_id"); ok {
				livePane = pane
			}
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
	fmt.Fprint(stdout, `#!/usr/bin/env bash
# herder-list — show spawned agents, optionally reconciled with live herdr state.
#
# Usage:
#   herder-list                       # table of active records, reconciled with live agents
#   herder-list --all                 # include records with status != active
#   herder-list --json                # raw JSONL of reconciled records to stdout
#   herder-list --raw                 # raw registry JSONL without reconciliation
#   herder-list --guid GUID           # single record (full JSON), exit 1 if missing
#   herder-list --teams               # enumerate team buses under $HERDER_TEAMS_ROOT
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

func liveAgentsByTerminal() map[string]*herdrcli.Agent {
	out, err := (&herdrcli.Client{}).Output("agent", "list")
	if err != nil {
		out = []byte(`{"result":{"agents":[]}}`)
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		agents = nil
	}
	live := make(map[string]*herdrcli.Agent)
	for i := range agents {
		if agents[i].TerminalID == nil {
			continue
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, agents[i].Raw); err == nil {
			agents[i].Raw = compact.Bytes()
		}
		live[*agents[i].TerminalID] = &agents[i]
	}
	return live
}

func grepLastQuoted(path, target string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	needle := `"` + target + `"`
	var hit string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.Contains(line, needle) {
			hit = line
		}
	}
	return hit, hit != ""
}

func decodeRecord(line string) (registry.Record, error) {
	var rec registry.Record
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return registry.Record{}, err
	}
	rec.Raw = append([]byte(nil), line...)
	return rec, nil
}

func reconciledJSON(rec registry.Record, liveByTerm map[string]*herdrcli.Agent) []byte {
	live := liveByTerm[rec.TerminalID]
	if live == nil {
		return appendJSONFields(rec.Raw,
			`"live":null`,
			`"live_pane":null`,
			`"live_status":"gone"`,
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
	fmt.Fprintf(stderr, "herder-list: %s\n", msg)
}
