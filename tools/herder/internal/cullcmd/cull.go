package cullcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type options struct {
	help     bool
	selector string
	value    string
	goneOnly bool
	dryRun   bool
	force    bool
}

func Run(args []string, stdout, stderr io.Writer) int {
	if os.Getenv("HERDR_ENV") != "1" {
		die(stderr, "not running inside a herdr pane (HERDR_ENV != 1)")
		return 1
	}
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

	registryPath := registry.DefaultPath()
	recs, err := registry.Load(registryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			die(stderr, "no registry at "+registryPath)
			return 1
		}
		return 1
	}

	liveAgents := liveAgents()
	targets := selectTargets(registry.LatestByGUID(recs), liveAgents, opts)
	if len(targets) == 0 {
		fmt.Fprintln(stderr, "no matching active records")
		return 1
	}

	nowISO := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	culler := os.Getenv("HERDR_PANE_ID")
	if culler == "" {
		culler = "unknown"
	}
	for _, rec := range targets {
		processTarget(registryPath, rec, liveAgents, opts, nowISO, culler, stdout, stderr)
	}
	return 0
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	var opts options
	for i := 0; i < len(args); {
		switch args[i] {
		case "--guid":
			opts.selector = "guid"
			if i+1 < len(args) {
				opts.value = args[i+1]
			}
			i += 2
		case "--label":
			opts.selector = "label"
			if i+1 < len(args) {
				opts.value = args[i+1]
			}
			i += 2
		case "--pane":
			opts.selector = "pane"
			if i+1 < len(args) {
				opts.value = args[i+1]
			}
			i += 2
		case "--gone":
			opts.goneOnly = true
			i++
		case "--dry-run":
			opts.dryRun = true
			i++
		case "--force":
			opts.force = true
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
	if !opts.goneOnly && opts.selector == "" {
		die(stderr, "selector required (--guid, --label, --pane, or --gone)")
		return opts, 1
	}
	return opts, 0
}

func printHelp(stdout io.Writer) {
	lines := []string{
		"#!/usr/bin/env bash",
		"# herder-cull — close a spawned agent's pane and mark its registry record closed.",
		"#",
		"# Usage:",
		"#   herder-cull --guid GUID                 # match by short or full guid",
		"#   herder-cull --label LABEL                # match by label",
		"#   herder-cull --pane PANE_ID               # match by pane id",
		"#   herder-cull --gone                       # close registry entries whose pane is no longer live",
		"#   add --dry-run to print what would happen without acting.",
		"#   add --force to skip terminal_id verification (use only when you've confirmed",
		"#       the agent is dead and you need to mark the registry row closed).",
		"#",
		"# Updates the registry by appending a new closed record (registry is append-only JSONL).",
		"#",
		"# Safety: before calling `herdr pane close`, looks up the pane via `herdr pane get`",
		"# and confirms `terminal_id` matches the spawn-time terminal_id from the registry",
		"# row. Mismatch means herdr id compaction has reassigned the pane_id to a",
		"# different agent — refuse + log rather than close someone else's work.",
	}
	fmt.Fprint(stdout, strings.Join(lines, "\n")+"\n")
}

func liveAgents() map[string]herdrcli.Agent {
	out, err := (&herdrcli.Client{}).Output("agent", "list")
	if err != nil {
		out = []byte(`{"result":{"agents":[]}}`)
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		agents = nil
	}
	live := make(map[string]herdrcli.Agent)
	for _, agent := range agents {
		if agent.TerminalID == nil {
			continue
		}
		live[*agent.TerminalID] = agent
	}
	return live
}

func selectTargets(recs []registry.Record, live map[string]herdrcli.Agent, opts options) []registry.Record {
	var out []registry.Record
	for _, rec := range recs {
		if rec.Status != "active" {
			continue
		}
		if opts.goneOnly {
			if _, ok := live[rec.TerminalID]; !ok {
				out = append(out, rec)
			}
			continue
		}
		switch opts.selector {
		case "guid":
			if ptrEq(rec.GUID, opts.value) || ptrEq(rec.ShortGUID, opts.value) {
				out = append(out, rec)
			}
		case "label":
			if ptrEq(rec.Label, opts.value) {
				out = append(out, rec)
			}
		case "pane":
			if rec.PaneID == opts.value {
				out = append(out, rec)
			}
		}
	}
	return out
}

func processTarget(registryPath string, rec registry.Record, live map[string]herdrcli.Agent, opts options, nowISO, culler string, stdout, stderr io.Writer) {
	guid := ptrString(rec.GUID)
	label := ptrString(rec.Label)
	pane := rec.PaneID
	term := rec.TerminalID

	if opts.dryRun {
		fmt.Fprintf(stdout, "would cull %s (%s) pane=%s\n", label, guid, pane)
		return
	}

	if opts.goneOnly {
		appendClosed(registryPath, rec, nowISO, culler, "already_gone", "terminal_id not in live agent list")
		fmt.Fprintf(stdout, "recorded closed %s (%s) pane=%s → already_gone\n", label, guid, pane)
		dropBusEntry(rec, stdout)
		return
	}

	if !opts.force && term != "" {
		vrc := verifyPaneIdentity(pane, term)
		if vrc == 1 || vrc == 2 {
			livePane := livePaneForTerm(live, term)
			if livePane != "" {
				fmt.Fprintf(stderr, "pane id drifted for %s (%s): registry=%s, terminal %s now live at %s — retargeting\n",
					label, guid, pane, term, livePane)
				pane = livePane
			} else {
				if vrc == 1 {
					fmt.Fprintf(stderr, "pane %s gone and terminal %s not live anywhere; recording closed without API call\n", pane, term)
				} else {
					fmt.Fprintf(stderr, "pane %s reassigned to another terminal and %s not live anywhere; recording closed\n", pane, term)
				}
				appendClosed(registryPath, rec, nowISO, culler, "already_gone", "terminal_id not in live agent list")
				dropBusEntry(rec, stdout)
				return
			}
		}
	}

	result, _, _ := (&herdrcli.Client{}).Combined("pane", "close", pane)
	closedOK := closeResultType(result)
	if closedOK == "error" {
		reason := closeErrorReason(result)
		appendClosed(registryPath, rec, nowISO, culler, "error", reason)
		fmt.Fprintf(stdout, "cull errored %s (%s) pane=%s → %s (still marked closed in registry)\n", label, guid, pane, reason)
		dropBusEntry(rec, stdout)
		return
	}
	appendClosed(registryPath, rec, nowISO, culler, closedOK, "")
	fmt.Fprintf(stdout, "culled %s (%s) pane=%s → %s\n", label, guid, pane, closedOK)
	dropBusEntry(rec, stdout)
}

func verifyPaneIdentity(pane, wantTerm string) int {
	out, _, _ := (&herdrcli.Client{}).Combined("pane", "get", pane)
	got, err := herdrcli.ParsePaneGet(out)
	if err != nil || got.TerminalID == "" {
		return 1
	}
	if got.TerminalID != wantTerm {
		return 2
	}
	return 0
}

func livePaneForTerm(live map[string]herdrcli.Agent, term string) string {
	agent, ok := live[term]
	if !ok {
		return ""
	}
	return agent.PaneID
}

func closeResultType(out []byte) string {
	var envelope struct {
		Result struct {
			Type string `json:"type"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil || envelope.Result.Type == "" {
		return "error"
	}
	return envelope.Result.Type
}

func closeErrorReason(out []byte) string {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return "unknown_error"
	}
	if envelope.Error.Code != "" {
		return envelope.Error.Code
	}
	if envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	return "unknown_error"
}

func appendClosed(path string, rec registry.Record, nowISO, culler, result, reason string) {
	row := bytes.TrimSpace(rec.Raw)
	replacement := []byte(`"status":"closed"`)
	if bytes.Contains(row, []byte(`"status"`)) {
		row = bytes.Replace(row, []byte(`"status":"`+rec.Status+`"`), replacement, 1)
	} else {
		row = appendJSONFields(row, string(replacement))
	}
	row = appendJSONFields(row,
		`"closed_at":`+jsonString(nowISO),
		`"closed_by_pane":`+jsonString(culler),
		`"close_result":`+jsonString(result),
		`"close_reason":`+jsonString(reason),
	)
	_ = registry.Append(path, row)
}

func dropBusEntry(rec registry.Record, stdout io.Writer) {
	hcomName := rec.HcomName
	if hcomName == "" {
		return
	}
	if _, err := exec.LookPath("hcom"); err != nil {
		return
	}
	cmd := exec.Command("hcom", "kill", hcomName)
	if rec.HcomDir != "" {
		cmd.Env = setEnv(os.Environ(), "HCOM_DIR", rec.HcomDir)
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		fmt.Fprintf(stdout, "bus: dropped @%s\n", hcomName)
		return
	}
	rc := 1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		rc = exitErr.ExitCode()
	}
	reason := strings.Join(strings.Fields(string(out)), " ")
	if reason == "" {
		reason = fmt.Sprintf("exit %d", rc)
	}
	fmt.Fprintf(stdout, "bus: drop failed (%s) — pane closed anyway\n", reason)
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

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			cp := append([]string(nil), env...)
			cp[i] = prefix + value
			return cp
		}
	}
	return append(append([]string(nil), env...), prefix+value)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func ptrEq(s *string, v string) bool {
	return s != nil && *s == v
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder-cull: %s\n", msg)
}
