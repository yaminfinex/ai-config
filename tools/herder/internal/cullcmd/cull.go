package cullcmd

import (
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
	v2 "ai-config/tools/herder/internal/registry/v2"
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
	proj, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		die(stderr, err.Error())
		return 1
	}

	liveAgents := liveAgents()
	targets := selectTargets(registry.LatestByGUID(recs), proj, liveAgents, opts)
	if len(targets) == 0 {
		if opts.goneOnly {
			fmt.Fprintln(stdout, "no gone records to cull")
			return 0
		}
		fmt.Fprintln(stderr, "no matching active records")
		return 1
	}

	nowISO := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	ok := true
	for _, rec := range targets {
		if !processTarget(registryPath, rec, liveAgents, opts, nowISO, stdout, stderr) {
			ok = false
		}
	}
	if !ok {
		return 1
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
		"herder cull — close a spawned agent's pane and mark its registry record closed.",
		"",
		"Usage:",
		"  herder cull --guid GUID      close the agent with this short or full guid",
		"  herder cull --label LABEL    close the agent with this label",
		"  herder cull --pane PANE_ID   close the agent at this pane id",
		"  herder cull --gone           close registry records whose pane is no longer live",
		"",
		"Options:",
		"  --dry-run    print what would be closed without acting",
		"  --force      skip terminal_id verification — use ONLY when you've confirmed the",
		"               agent is dead and just need to mark the registry row closed",
		"",
		"Behavior:",
		"  Before closing, confirms the live pane's terminal_id matches the one recorded at",
		"  spawn. Within one herdr server run a stale pane_id points at nothing, not another",
		"  agent; after a restart, ids reshuffle. Within a run, cull retargets to the",
		"  original agent's current pane (via terminal_id) so it never closes someone",
		"  else's work. Each close appends a new closed record (the registry is append-only JSONL).",
		"  A row with neither pane_id nor terminal_id has nothing to verify or close; cull",
		"  records it closed without requiring --force, matching --gone recovery.",
		"",
		"If it fails:",
		"  - \"not live anywhere\": the agent is already gone; the row is recorded closed.",
		"  - an identity mismatch you don't understand: run `herder list` to re-resolve the",
		"    agent, and pass --force only once you've confirmed the target is truly dead.",
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

func selectTargets(recs []registry.Record, proj *v2.Projection, live map[string]herdrcli.Agent, opts options) []registry.Record {
	var out []registry.Record
	for _, rec := range recs {
		if !registry.IsNonRetired(rec) {
			continue
		}
		if opts.goneOnly {
			current := registry.V2ByGUID(proj, ptrString(rec.GUID))
			if current == nil || current.State != v2.StateSeated {
				continue
			}
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

func processTarget(registryPath string, rec registry.Record, live map[string]herdrcli.Agent, opts options, nowISO string, stdout, stderr io.Writer) bool {
	guid := ptrString(rec.GUID)
	label := ptrString(rec.Label)
	pane := rec.PaneID
	term := rec.TerminalID

	if opts.dryRun {
		fmt.Fprintf(stdout, "would cull %s (%s) pane=%s\n", label, guid, pane)
		return true
	}

	if opts.goneOnly {
		closed, appended, err := appendClosed(registryPath, rec, nowISO, "already_gone", "terminal_id not in live agent list")
		if err != nil {
			die(stderr, err.Error())
			return false
		}
		reportClosedFact(stdout, closed, appended, "already_gone", label, guid, pane)
		if appended {
			dropBusEntryIfGone(closed, opts.force, stdout)
		}
		return true
	}

	if pane == "" && term == "" {
		closed, appended, err := appendClosed(registryPath, rec, nowISO, "already_gone", "source=cull-verification; no seat coordinates")
		if err != nil {
			die(stderr, err.Error())
			return false
		}
		reportClosedFact(stdout, closed, appended, "already_gone", label, guid, pane)
		if appended {
			dropBusEntryIfGone(closed, opts.force, stdout)
		}
		return true
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
				if rec.CloseResult == "" && isAlreadyUnseated(registryPath, guid) {
					reportUnverifiable(stdout, rec, label, guid)
					return true
				}
				if vrc == 1 {
					fmt.Fprintf(stderr, "pane %s gone and terminal %s not live anywhere; recording closed without API call\n", pane, term)
				} else {
					fmt.Fprintf(stderr, "pane %s reassigned to another terminal and %s not live anywhere; recording closed\n", pane, term)
				}
				closed, appended, err := appendClosed(registryPath, rec, nowISO, "already_gone", "source=cull-verification; terminal_id not in live agent list")
				if err != nil {
					die(stderr, err.Error())
					return false
				}
				reportClosedFact(stdout, closed, appended, "already_gone", label, guid, pane)
				if appended {
					dropBusEntryIfGone(closed, opts.force, stdout)
				}
				return true
			}
		}
	}

	result, _, _ := (&herdrcli.Client{}).Combined("pane", "close", pane)
	closedOK := closeResultType(result)
	if closedOK == "error" {
		reason := closeErrorReason(result)
		closed, appended, err := appendClosed(registryPath, rec, nowISO, "error", reason)
		if err != nil {
			die(stderr, err.Error())
			return false
		}
		if appended {
			fmt.Fprintf(stdout, "cull errored %s (%s) pane=%s → %s (still marked closed in registry)\n", label, guid, pane, reason)
			dropBusEntry(closed, stdout)
		} else {
			reportClosedFact(stdout, closed, false, "error", label, guid, pane)
		}
		return true
	}
	closed, appended, err := appendClosed(registryPath, rec, nowISO, closedOK, "")
	if err != nil {
		die(stderr, err.Error())
		return false
	}
	if appended {
		fmt.Fprintf(stdout, "culled %s (%s) pane=%s → %s\n", label, guid, pane, closedOK)
		dropBusEntry(closed, stdout)
	} else {
		reportClosedFact(stdout, closed, false, closedOK, label, guid, pane)
	}
	return true
}

func reportClosedFact(stdout io.Writer, rec registry.Record, appended bool, result, fallbackLabel, fallbackGUID, pane string) {
	if appended {
		fmt.Fprintf(stdout, "recorded closed %s (%s) pane=%s → %s\n", fallbackLabel, fallbackGUID, pane, result)
		return
	}
	label := ptrString(rec.Label)
	if label == "" {
		label = fallbackLabel
	}
	guid := ptrString(rec.GUID)
	if guid == "" {
		guid = fallbackGUID
	}
	closeResult := rec.CloseResult
	if closeResult == "" {
		closeResult = "never-close-annotated"
	}
	fmt.Fprintf(stdout, "already unseated %s (%s) at %s, close_result=%s\n", label, guid, rec.RecordedAt, closeResult)
}

func reportUnverifiable(stdout io.Writer, rec registry.Record, fallbackLabel, fallbackGUID string) {
	label := ptrString(rec.Label)
	if label == "" {
		label = fallbackLabel
	}
	guid := ptrString(rec.GUID)
	if guid == "" {
		guid = fallbackGUID
	}
	fmt.Fprintf(stdout, "already unseated %s (%s) at %s; never close-annotated (migrated corpse); gone-ness unverifiable from here\n", label, guid, rec.RecordedAt)
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

func appendClosed(path string, rec registry.Record, nowISO, result, reason string) (registry.Record, bool, error) {
	guid := ptrString(rec.GUID)
	rec = latestForGUID(path, rec)
	var already *v2.SessionRecord
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		latest := registry.V2ByGUID(tx.Projection, guid)
		if latest == nil {
			return nil, fmt.Errorf("registry close failed for %s: latest record not found", guid)
		}
		if latest.State == v2.StateUnseated && latest.CloseResult != "" && !latest.LegacyV1 {
			cp := *latest
			already = &cp
			return nil, nil
		}
		next := *latest
		next.Event = "unseated"
		next.State = v2.StateUnseated
		next.RecordedAt = nowISO
		next.Seat = nil
		next.CloseResult = result
		next.CloseReason = reason
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		return rec, false, err
	}
	if already != nil {
		rec.State = already.State
		rec.RecordedAt = already.RecordedAt
		rec.CloseResult = already.CloseResult
		rec.CloseReason = already.CloseReason
		return rec, false, nil
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		return rec, false, err
	}
	if err := outcome.Err(); err != nil {
		return rec, false, err
	}
	if outcome.Status != registry.WriteApplied {
		return rec, false, fmt.Errorf("registry close failed for %s: no close record appended", guid)
	}
	rec.State = v2.StateUnseated
	rec.CloseResult = result
	rec.CloseReason = reason
	return rec, true, nil
}

func isAlreadyUnseated(path, guid string) bool {
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		return false
	}
	current := registry.V2ByGUID(proj, guid)
	return current != nil && current.State == v2.StateUnseated
}

func latestForGUID(path string, rec registry.Record) registry.Record {
	guid := ptrString(rec.GUID)
	if guid == "" {
		return rec
	}
	recs, err := registry.Load(path)
	if err != nil {
		return rec
	}
	for _, latest := range registry.LatestByGUID(recs) {
		if ptrString(latest.GUID) == guid {
			return latest
		}
	}
	return rec
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
	// An already-absent bus row is the EXPECTED post-timeout/cull state — the
	// agent's hcom entry was reaped when its pane died — not a failure. Report it
	// as a plain note instead of an alarming "drop failed".
	if strings.Contains(strings.ToLower(reason), "not found") {
		fmt.Fprintf(stdout, "bus: @%s already gone (nothing to drop)\n", hcomName)
		return
	}
	fmt.Fprintf(stdout, "bus: drop failed (%s) — pane closed anyway\n", reason)
}

func dropBusEntryIfGone(rec registry.Record, force bool, stdout io.Writer) {
	if !force && busEntryJoined(rec) {
		fmt.Fprintf(stdout, "bus: @%s still joined; not dropped without --force\n", rec.HcomName)
		return
	}
	dropBusEntry(rec, stdout)
}

func busEntryJoined(rec registry.Record) bool {
	if rec.HcomName == "" || rec.HcomName == "null" {
		return false
	}
	if _, err := exec.LookPath("hcom"); err != nil {
		return false
	}
	cmd := exec.Command("hcom", "list", rec.HcomName)
	if rec.HcomDir != "" && rec.HcomDir != "null" {
		cmd.Env = setEnv(os.Environ(), "HCOM_DIR", rec.HcomDir)
	}
	return cmd.Run() == nil
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
	fmt.Fprintf(stderr, "herder cull: %s\n", msg)
}
