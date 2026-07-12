package reconcilecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type options struct {
	help  bool
	apply bool
	json  bool
}

type result struct {
	GUID           string      `json:"guid"`
	ShortGUID      string      `json:"short_guid,omitempty"`
	Label          string      `json:"label"`
	Outcome        string      `json:"outcome"`
	Detail         string      `json:"detail"`
	TerminalID     string      `json:"terminal_id,omitempty"`
	PaneID         string      `json:"pane_id,omitempty"`
	Write          string      `json:"write"`
	Candidates     []candidate `json:"candidates,omitempty"`
	bus            hcomidentity.Result
	busUnavailable bool
}

type candidate struct {
	Name       string `json:"name,omitempty"`
	Agent      string `json:"agent,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	TerminalID string `json:"terminal_id,omitempty"`
	PaneID     string `json:"pane_id,omitempty"`
}

type liveState struct {
	agents    []herdrcli.Agent
	byTerm    map[string]*herdrcli.Agent
	paneTerms map[string]bool
	panePanes map[string]bool
}

type busRoster struct {
	rows []hcomidentity.Row
	err  error
}

func Run(args []string, stdout, stderr io.Writer) int {
	if _, err := exec.LookPath("herdr"); err != nil {
		die(stderr, "herdr not on PATH")
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
			fmt.Fprintf(stderr, "no registry at %s\n", registryPath)
			return 0
		}
		die(stderr, err.Error())
		return 1
	}

	live := buildLiveState()
	active := nonRetiredRows(recs)
	held := heldTerminals(active)

	var results []result
	hasAmbiguous := false
	for _, rec := range active {
		res := reconcileOne(rec, held, live)
		results = append(results, res)
	}
	markDuplicateRebinds(results)
	busRosters := map[string]busRoster{}
	for i, rec := range active {
		results[i] = reconcileBusIdentity(rec, results[i], busRosters)
	}
	for _, res := range results {
		if res.Outcome == "ambiguous" {
			hasAmbiguous = true
		}
	}

	exit := 0
	if hasAmbiguous {
		exit = 1
	}
	if opts.apply && !hasAmbiguous {
		for i, rec := range active {
			res := results[i]
			if !shouldWrite(res) {
				continue
			}
			row, err := updateRow(rec.Raw, res)
			if err != nil {
				res.Write = "error"
				res.Detail = res.Detail + "; write failed: " + err.Error()
				exit = 1
			} else if outcome, err := registry.AppendLegacySessionEvent(registryPath, row, "reconciled", "seated"); err != nil {
				res.Write = "error"
				res.Detail = res.Detail + "; write failed: " + err.Error()
				exit = 1
			} else if err := outcome.Err(); err != nil {
				res.Write = "error"
				res.Detail = res.Detail + "; write failed: " + err.Error()
				exit = 1
			} else {
				res.Write = string(outcome.Status)
			}
			results[i] = res
		}
	}

	if opts.json {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, res := range results {
			if err := enc.Encode(res); err != nil {
				return 1
			}
		}
		return exit
	}

	fmt.Fprintf(stdout, "%-10s %-20s %-30s %s\n", "GUID", "LABEL", "OUTCOME", "DETAIL")
	for _, res := range results {
		fmt.Fprintf(stdout, "%-10s %-20s %-30s %s\n", displayGUID(res), res.Label, res.Outcome, res.Detail)
	}
	return exit
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	var opts options
	for i := 0; i < len(args); {
		switch args[i] {
		case "--apply":
			opts.apply = true
			i++
		case "--json":
			opts.json = true
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
	fmt.Fprint(stdout, `herder reconcile — audit and repair registry coordinates after herdr handoff.

Usage:
  herder reconcile          dry-run latest non-retired registry sessions
  herder reconcile --apply  append replacement rows for safe coordinate refreshes
  herder reconcile --json   emit JSONL instead of a table

Dry-run is the default. --apply is append-only: it writes full replacement rows
through the registry, preserving unknown fields and never mutating old rows. Any
carried bus name is re-verified from live session/pane evidence; a name that
cannot be proven is explicitly marked unverified rather than trusted as clean.

Outcomes follow herder-spec §8.3 decisions D11/D12:
  re-confirm                    stored terminal still identifies the same label
  re-bind (assumed-continuity)  terminal is dead; one name+agent-kind+cwd match exists
  conflict                      stored terminal is live but names a different agent; refuses to act
  ambiguous                     multiple fallback candidates; refuses to guess
  undetected                    pane is alive but absent from agent detection
  gone                          no live agent or pane matches
`)
}

func buildLiveState() liveState {
	client := &herdrcli.Client{}
	out, err := client.Output("agent", "list")
	if err != nil {
		out = []byte(`{"result":{"agents":[]}}`)
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		agents = nil
	}
	live := liveState{
		agents:    agents,
		byTerm:    make(map[string]*herdrcli.Agent),
		paneTerms: make(map[string]bool),
		panePanes: make(map[string]bool),
	}
	for i := range live.agents {
		if live.agents[i].TerminalID != nil {
			live.byTerm[*live.agents[i].TerminalID] = &live.agents[i]
		}
	}
	if paneOut, err := client.Output("pane", "list"); err == nil {
		if panes, err := herdrcli.ParsePaneList(paneOut); err == nil {
			for _, pane := range panes {
				if pane.TerminalID != "" {
					live.paneTerms[pane.TerminalID] = true
				}
				if pane.PaneID != "" {
					live.panePanes[pane.PaneID] = true
				}
			}
		}
	}
	return live
}

func nonRetiredRows(recs []registry.Record) []registry.Record {
	var out []registry.Record
	for _, rec := range registry.LatestByGUID(recs) {
		if registry.IsNonRetired(rec) {
			out = append(out, rec)
		}
	}
	return out
}

func heldTerminals(recs []registry.Record) map[string]string {
	held := make(map[string]string)
	for _, rec := range recs {
		if rec.TerminalID != "" {
			held[rec.TerminalID] = ptrString(rec.GUID)
		}
	}
	return held
}

func reconcileOne(rec registry.Record, held map[string]string, live liveState) result {
	res := result{
		GUID:       ptrString(rec.GUID),
		ShortGUID:  ptrString(rec.ShortGUID),
		Label:      ptrString(rec.Label),
		TerminalID: rec.TerminalID,
		PaneID:     rec.PaneID,
		Write:      "none",
	}
	if res.Label == "" {
		res.Label = "-"
	}

	if rec.TerminalID != "" {
		if agent := live.byTerm[rec.TerminalID]; agent != nil {
			if agent.Name != "" && agent.Name != ptrString(rec.Label) {
				res.Outcome = "conflict"
				res.Detail = fmt.Sprintf("stored terminal is live as name=%q; D11 refuses to unseat, use manual adoption/enroll", agent.Name)
				return res
			}
			res.Outcome = "re-confirm"
			if agent.PaneID != "" && agent.PaneID != rec.PaneID {
				res.PaneID = agent.PaneID
				res.Write = "pending"
				res.Detail = fmt.Sprintf("terminal live; pane refresh %s -> %s", rec.PaneID, agent.PaneID)
			} else {
				res.Detail = "terminal live"
			}
			return res
		}
	}

	matches := fallbackCandidates(rec, held, live)
	switch len(matches) {
	case 1:
		res.Outcome = "re-bind (assumed-continuity)"
		res.TerminalID = matches[0].TerminalID
		res.PaneID = matches[0].PaneID
		res.Write = "pending"
		res.Detail = "one live name+agent-kind+cwd fallback match; identity assumed per D12"
		res.Candidates = matches
		return res
	default:
		if len(matches) > 1 {
			res.Outcome = "ambiguous"
			res.Detail = "multiple fallback candidates; refusing to guess"
			res.Candidates = matches
			return res
		}
	}

	if paneAlive(rec, live) {
		res.Outcome = "undetected"
		res.Detail = "pane is alive, but agent list has no matching detection; restart or relaunch to restore status before rebinding"
		return res
	}
	res.Outcome = "gone"
	res.Detail = "no live agent or pane matches"
	return res
}

func fallbackCandidates(rec registry.Record, held map[string]string, live liveState) []candidate {
	label := ptrString(rec.Label)
	if label == "" || rec.Agent == "" {
		return nil
	}
	wantCWD, requireCWD := rowCWD(rec)
	var out []candidate
	for _, agent := range live.agents {
		if agent.Name != label || agent.Agent != rec.Agent {
			continue
		}
		if requireCWD && agent.CWD != wantCWD {
			continue
		}
		term := ptrString(agent.TerminalID)
		if term == "" {
			continue
		}
		if owner := held[term]; owner != "" && owner != ptrString(rec.GUID) {
			continue
		}
		out = append(out, candidate{
			Name:       agent.Name,
			Agent:      agent.Agent,
			CWD:        agent.CWD,
			TerminalID: term,
			PaneID:     agent.PaneID,
		})
	}
	return out
}

func markDuplicateRebinds(results []result) {
	byTerminal := make(map[string][]int)
	for i, res := range results {
		if res.Outcome != "re-bind (assumed-continuity)" || res.TerminalID == "" {
			continue
		}
		byTerminal[res.TerminalID] = append(byTerminal[res.TerminalID], i)
	}
	for term, indexes := range byTerminal {
		if len(indexes) < 2 {
			continue
		}
		for _, idx := range indexes {
			results[idx].Outcome = "ambiguous"
			results[idx].Detail = fmt.Sprintf("fallback candidate terminal %s is claimed by multiple non-retired sessions; refusing to guess", term)
			results[idx].Write = "none"
		}
	}
}

func paneAlive(rec registry.Record, live liveState) bool {
	return (rec.TerminalID != "" && live.paneTerms[rec.TerminalID]) ||
		(rec.PaneID != "" && live.panePanes[rec.PaneID])
}

func rowCWD(rec registry.Record) (string, bool) {
	if cwd, ok := rawStringField(rec.Raw, "cwd"); ok {
		return cwd, true
	}
	if rec.Provenance != nil && rec.Provenance.CWD != "" {
		return rec.Provenance.CWD, true
	}
	return "", false
}

func shouldWrite(res result) bool {
	return res.Write == "pending" && (res.Outcome == "re-confirm" || res.Outcome == "re-bind (assumed-continuity)")
}

func updateRow(raw []byte, res result) ([]byte, error) {
	updates := map[string]any{}
	if res.TerminalID != "" {
		updates["terminal_id"] = res.TerminalID
	}
	if res.PaneID != "" {
		updates["pane_id"] = res.PaneID
	}
	if !res.busUnavailable {
		verified := res.bus.Verified
		updates["hcom_verified"] = verified
		if res.bus.Verified {
			updates["hcom_name"] = res.bus.Name
		}
	}
	return registry.UpdateRawObject(raw, updates)
}

func reconcileBusIdentity(rec registry.Record, res result, rosters map[string]busRoster) result {
	if res.Outcome != "re-confirm" && res.Outcome != "re-bind (assumed-continuity)" {
		return res
	}
	roster, ok := rosters[rec.HcomDir]
	if !ok {
		roster.rows, roster.err = hcomidentity.List(rec.HcomDir)
		rosters[rec.HcomDir] = roster
	}
	if roster.err != nil {
		res.busUnavailable = true
		res.bus = hcomidentity.Result{Reason: roster.err.Error()}
	} else {
		sessionID := ""
		if rec.Provenance != nil {
			sessionID = rec.Provenance.ToolSessionID
		}
		res.bus = hcomidentity.Resolve(roster.rows, hcomidentity.Evidence{SessionID: sessionID, PaneIDs: []string{res.PaneID}})
	}
	needsWrite := res.bus.Verified && (rec.HcomName != res.bus.Name || rec.HcomVerified == nil || !*rec.HcomVerified)
	needsDowngrade := !res.busUnavailable && !res.bus.Verified && rec.HcomName != "" && (rec.HcomVerified == nil || *rec.HcomVerified)
	if needsWrite || needsDowngrade {
		res.Write = "pending"
		if res.bus.Verified {
			res.Detail += fmt.Sprintf("; bus identity verified as @%s", res.bus.Name)
		} else {
			res.Detail += "; stored bus identity could not be re-verified and will be marked unverified"
		}
	}
	return res
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

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func displayGUID(res result) string {
	if res.ShortGUID != "" {
		return res.ShortGUID
	}
	if res.GUID == "" {
		return "-"
	}
	return registry.ShortGUID(res.GUID)
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder reconcile: %s\n", msg)
}
