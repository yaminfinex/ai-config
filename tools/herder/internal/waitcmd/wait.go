package waitcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type options struct {
	help      bool
	status    string
	timeoutMS string
	read      bool
	lines     string
	source    string
	target    string
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

	client := &herdrcli.Client{}
	paneOut, _ := client.Output("pane", "list")
	paneID, ok := resolvePane(opts.target, paneOut, stderr)
	if !ok {
		return 1
	}
	if paneID == "" {
		paneID = opts.target
	}

	waitRC, err := client.Run("wait", "agent-status", paneID, "--status", opts.status, "--timeout", opts.timeoutMS)
	if err != nil {
		waitRC = 1
	}
	if waitRC != 0 {
		if opts.status == "idle" && doneIsIdleEquivalent(paneID) {
			waitRC = 0
		} else {
			fmt.Fprintf(stderr, "herder wait: timeout waiting for %s to reach status=%s\n", paneID, opts.status)
			if agentDetectionLost(paneID, paneOut) {
				fmt.Fprintf(stderr, "herder wait: pane %s is alive, but herdr agent detection is lost (agent get reports unknown or absent). This usually means the process predates a server handoff; restart the agent in the pane or relaunch it to restore status.\n", paneID)
			}
		}
	}

	if opts.read {
		runPaneRead(paneID, opts.source, opts.lines, stdout, stderr)
	}
	return waitRC
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	opts := options{status: "idle", timeoutMS: "60000", lines: "30", source: "recent-unwrapped"}
	for i := 0; i < len(args); {
		arg := args[i]
		switch arg {
		case "--status":
			if i+1 < len(args) {
				opts.status = args[i+1]
			}
			i += 2
		case "--timeout":
			if i+1 < len(args) {
				opts.timeoutMS = args[i+1]
			}
			i += 2
		case "--read":
			opts.read = true
			i++
		case "--lines":
			if i+1 < len(args) {
				opts.lines = args[i+1]
			}
			i += 2
		case "--source":
			if i+1 < len(args) {
				opts.source = args[i+1]
			}
			i += 2
		case "-h", "--help":
			printHelp(stdout)
			opts.help = true
			return opts, 0
		default:
			if len(arg) >= 2 && arg[:2] == "--" {
				die(stderr, "unknown flag: "+arg)
				return opts, 1
			}
			if opts.target != "" {
				die(stderr, "extra positional arg: "+arg)
				return opts, 1
			}
			opts.target = arg
			i++
		}
	}
	if opts.target == "" {
		die(stderr, "target required (guid, label, or pane id)")
		return opts, 1
	}
	return opts, 0
}

func printHelp(stdout io.Writer) {
	lines := []string{
		"herder wait — block until a spawned agent reaches a status; optionally read its screen.",
		"",
		"Usage:",
		"  herder wait <target> [--status idle|working|blocked|done|unknown] [--timeout MS]",
		"                       [--read] [--lines N] [--source visible|recent|recent-unwrapped]",
		"",
		"<target> is a short-guid, full guid, label, or pane_id. A guid/label resolves to",
		"the agent's CURRENT pane (drift-proof as herdr compacts pane ids); a raw pane_id",
		"is used verbatim.",
		"",
		"Options:",
		"  --status S      status to wait for (default idle). claude never emits `done`; codex",
		"                  CAN report `done` (herdr-native, seen even mid-boot) — wait treats",
		"                  codex `done` as `idle`, so `idle` is the readiness signal for both.",
		"  --timeout MS    give up after MS (default 60000).",
		"  --read          after waiting, print the pane's recent screen to stdout.",
		"  --lines N       lines to read with --read (default 30).",
		"  --source SRC    read source: visible | recent | recent-unwrapped (default recent-unwrapped).",
		"",
		"Use wait for boot-settle and post-send verification — NOT for watching long work.",
		"For completion, have the worker ring you (spawn --notify, or a bus message); polling",
		"wait in a loop is the wrong shape. If wait returns sooner than expected, read the",
		"pane and decide before calling again rather than trusting the status as final.",
		"",
		"Exit codes:",
		"  0   target reached the requested status (for claude/codex, `done`≈`idle`).",
		"  1   timed out, or the target is not live — gone or culled (check `herder list --all`).",
	}
	fmt.Fprint(stdout, strings.Join(lines, "\n")+"\n")
}

func resolvePane(target string, paneOut []byte, stderr io.Writer) (string, bool) {
	recs, err := registry.Load(registry.DefaultPath())
	var rec *registry.Record
	if err == nil {
		rec = registry.Resolve(recs, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if rec == nil {
		return target, true
	}
	term := rec.TerminalID
	stored := rec.PaneID
	if term == "" {
		return stored, true
	}
	panes, paneErr := herdrcli.ParsePaneList(paneOut)
	for _, pane := range panes {
		if pane.TerminalID == term {
			return pane.PaneID, true
		}
	}
	if paneErr != nil || len(panes) == 0 {
		fmt.Fprintf(stderr, "herder wait: could not read live pane list; cannot resolve %s\n", target)
	} else {
		fmt.Fprintf(stderr, "herder wait: %s (terminal %s) is not live anywhere — agent gone or culled\n", displayName(rec, target), term)
	}
	return "", false
}

func runPaneRead(paneID, source, lines string, stdout, stderr io.Writer) {
	cmd := exec.Command("herdr", "pane", "read", paneID, "--source", source, "--lines", lines)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	_ = cmd.Run()
}

func doneIsIdleEquivalent(paneID string) bool {
	out, err := exec.Command("herdr", "agent", "get", paneID).Output()
	if err != nil {
		return false
	}
	var envelope struct {
		Result struct {
			Agent struct {
				Agent       string `json:"agent"`
				AgentStatus string `json:"agent_status"`
			} `json:"agent"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &envelope) != nil {
		return false
	}
	return envelope.Result.Agent.Agent == "codex" && envelope.Result.Agent.AgentStatus == "done"
}

func agentDetectionLost(paneID string, paneOut []byte) bool {
	panes, err := herdrcli.ParsePaneList(paneOut)
	if err != nil {
		return false
	}
	found := false
	for _, pane := range panes {
		if pane.PaneID == paneID {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	out, err := exec.Command("herdr", "agent", "get", paneID).Output()
	if err != nil {
		return true
	}
	var envelope struct {
		Result struct {
			Agent struct {
				Agent       string `json:"agent"`
				AgentStatus string `json:"agent_status"`
			} `json:"agent"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &envelope) != nil {
		return true
	}
	return envelope.Result.Agent.Agent == "" || envelope.Result.Agent.AgentStatus == "" || envelope.Result.Agent.AgentStatus == "unknown"
}

func displayName(rec *registry.Record, fallback string) string {
	if rec != nil && rec.Label != nil && *rec.Label != "" {
		return *rec.Label
	}
	return fallback
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder wait: %s\n", msg)
}
