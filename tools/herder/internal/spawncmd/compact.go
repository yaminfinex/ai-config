package spawncmd

// herder compact — steered self-compaction (TASK-022). Queues a REAL
// `/compact <steer>` input line into the CALLER'S OWN pane via the
// spawn-private boot-paste engine; the line sits in the composer and fires
// when the current turn ends. This is input automation on one's own pane, a
// deliberate ruled exception to the bus-only transport doctrine (TASK-003
// FINDING 2) — it is NOT a delivery path. The command takes no target: the
// only pane it can ever address is the one it proves to be the caller's own,
// and when self-identity cannot be proven it refuses rather than guesses.
//
// It lives in package spawncmd so the paste engine (bootpaste.go) stays
// package-private: no exported paste API exists, which is what the
// check-compact-contract.sh grep gates pin.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type compactOptions struct {
	Help   bool
	DryRun bool
	Steer  string
}

// RunCompact executes herder compact and returns the process exit code.
func RunCompact(args []string, stdout, stderr io.Writer) int {
	opts, code := parseCompactArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.Help {
		return 0
	}

	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_PANE_ID") == "" {
		dieCompact(stderr, "not running inside a herdr pane (HERDR_ENV/HERDR_PANE_ID required) — herder compact queues input to the caller's OWN pane only")
		return 64
	}
	if strings.ContainsAny(opts.Steer, "\n\r") {
		dieCompact(stderr, "steer must be a single line — an embedded newline would submit early and corrupt the /compact command")
		return 64
	}

	herdr := &herdrcli.Client{}
	if !herdr.Available() {
		dieCompact(stderr, "herdr not on PATH")
		return 1
	}

	// Current truth: which terminal does the pane named by our environment
	// hold NOW. HERDR_PANE_ID was captured at pane start and pane ids can be
	// compacted/reassigned, so it is an entry point, never the paste target.
	envPane := os.Getenv("HERDR_PANE_ID")
	out, err := herdr.Output("pane", "get", envPane)
	if err != nil {
		dieCompact(stderr, "refused — cannot resolve own pane: herdr pane get failed for "+envPane+". Nothing was typed.")
		return 2
	}
	pane, err := herdrcli.ParsePaneGet(out)
	if err != nil || pane.TerminalID == "" {
		dieCompact(stderr, "refused — cannot resolve own pane: no terminal_id for "+envPane+". Nothing was typed.")
		return 2
	}

	recs, err := registry.Load(registry.DefaultPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		dieCompact(stderr, "registry not readable: "+err.Error())
		return 1
	}

	row, positional, refuseMsg := resolveSelfRow(recs, pane)
	if row == nil {
		dieCompact(stderr, "refused — "+refuseMsg+" herder compact only ever types into the caller's own pane; without proof of self-identity it refuses. Nothing was typed.")
		return 2
	}
	if row.Agent != "claude" && row.Agent != "codex" {
		dieCompact(stderr, fmt.Sprintf("refused — your registry row records agent %q, which has no interactive composer to type /compact into. Nothing was typed.", row.Agent))
		return 2
	}

	// Paste target: the CURRENT live pane holding our durable terminal_id —
	// the same drift-proof re-resolution send and cull use. This also yields
	// the canonical pane id the paste engine's status/kind detection matches
	// on (the env value can be a legacy alias herdr's agent list never shows).
	targetPane := ""
	if listOut, listErr := herdr.Output("agent", "list"); listErr == nil {
		if agents, parseErr := herdrcli.ParseAgentList(listOut); parseErr == nil {
			for _, agent := range agents {
				if agent.TerminalID != nil && *agent.TerminalID == row.TerminalID {
					targetPane = agent.PaneID
					break
				}
			}
		}
	}
	if targetPane == "" {
		dieCompact(stderr, "refused — terminal "+row.TerminalID+" is not live in herdr agent list; cannot locate your own pane. Nothing was typed.")
		return 2
	}
	if row.TerminalID != pane.TerminalID {
		// Durable-key identity with a drifted environment pane id: the row's
		// terminal is provably ours, the env pane no longer is. Surface it.
		fmt.Fprintf(stderr, "herder compact: note — HERDR_PANE_ID (%s) no longer names your terminal (env: %s, registry: %s); using the registry terminal's live pane %s\n", envPane, pane.TerminalID, row.TerminalID, targetPane)
	}

	line := "/compact"
	if opts.Steer != "" {
		line += " " + opts.Steer
	}

	if opts.DryRun {
		fmt.Fprintf(stderr, "herder compact --dry-run: would queue %q into own pane %s (terminal %s, guid %s, resolution: %s)\n",
			line, targetPane, row.TerminalID, ptrOrEmpty(row.GUID), map[bool]string{true: "positional+cwd", false: "durable-key"}[positional])
		return 0
	}

	verify, rc := (&bootPaster{Client: herdr, PreflightVisibleOnly: true}).paste(targetPane, line)
	switch {
	case verify == "" && rc == 2:
		dieCompact(stderr, "refused — your pane shows a blocking overlay (modal/interrupted state); /compact was NOT typed. Clear it and retry.")
		return 2
	case rc == 0:
		fmt.Fprintf(stderr, "herder compact: %s — %q is in your composer and fires when the current turn ends (verify: %s)\n", queuedWord(verify), line, verify)
		return 0
	case verify == "not_landed":
		dieCompact(stderr, "paste did not land — nothing appeared in your composer; nothing was submitted. Retry is safe.")
		return 1
	default:
		dieCompact(stderr, "typed but submission unverified (verify: "+verify+") — read your own pane before retrying; a blind retry may double-queue /compact.")
		return 1
	}
}

// resolveSelfRow proves which registry identity is the caller's own. Durable
// keys first: HERDER_GUID (every herder-spawned/forked/resumed session), then
// the hcom session id recorded in provenance. Only when neither exists does it
// fall back to positional resolution by the CURRENT terminal — and then it
// demands corroborating evidence (the pane's foreground cwd matching our own
// working directory) because a positional match cannot otherwise be told apart
// from a neighbour after pane-id churn. Returns (row, positional, refuseMsg).
func resolveSelfRow(recs []registry.Record, pane herdrcli.Pane) (*registry.Record, bool, string) {
	if guid := os.Getenv("HERDER_GUID"); guid != "" {
		row := registry.Resolve(recs, guid)
		if row == nil {
			return nil, false, "HERDER_GUID=" + guid + " has no registry row."
		}
		if row.TerminalID == "" {
			return nil, false, "your registry row (guid " + guid + ") records no terminal_id."
		}
		return row, false, ""
	}
	if sessionID := os.Getenv("HCOM_SESSION_ID"); sessionID != "" {
		if row := registry.ResolveByToolSessionID(recs, sessionID); row != nil {
			if row.TerminalID == "" {
				return nil, false, "your registry row (session " + sessionID + ") records no terminal_id."
			}
			return row, false, ""
		}
	}
	row := registry.ActiveByPaneOrTerminal(recs, pane.TerminalID)
	if row == nil {
		return nil, true, "no registry row proves this pane is yours (no HERDER_GUID, no session match, no active row for terminal " + pane.TerminalID + ")."
	}
	wd, _ := os.Getwd()
	paneCWD := pane.ForegroundCWD
	if paneCWD == "" {
		paneCWD = pane.CWD
	}
	if wd == "" || paneCWD == "" || wd != paneCWD {
		return nil, true, fmt.Sprintf("positional identity only (terminal %s) and the pane's foreground cwd (%q) does not corroborate this process's cwd (%q).", pane.TerminalID, paneCWD, wd)
	}
	return row, true, ""
}

func queuedWord(verify string) string {
	if verify == "queued" {
		return "queued"
	}
	return "submitted"
}

func ptrOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dieCompact(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder compact: %s\n", msg)
}

func parseCompactArgs(args []string, stdout, stderr io.Writer) (compactOptions, int) {
	opts := compactOptions{}
	var steerParts []string
	steerOnly := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if steerOnly {
			steerParts = append(steerParts, arg)
			continue
		}
		switch arg {
		case "--":
			steerOnly = true
		case "--dry-run":
			opts.DryRun = true
		case "-h", "--help":
			printCompactHelp(stdout)
			opts.Help = true
			return opts, 0
		default:
			if strings.HasPrefix(arg, "--") {
				dieCompact(stderr, "unknown flag: "+arg+" (steer text starting with -- goes after a `--` separator)")
				return opts, 64
			}
			steerParts = append(steerParts, arg)
		}
	}
	opts.Steer = strings.TrimSpace(strings.Join(steerParts, " "))
	return opts, 0
}

func printCompactHelp(stdout io.Writer) {
	lines := []string{
		"herder compact — queue a steered /compact into the CALLER'S OWN pane (self only).",
		"",
		"Usage:",
		"  herder compact [--dry-run] [<steer text> | -- <steer text>]",
		"",
		"Types a real `/compact <steer>` input line into your own composer via the",
		"spawn-private paste engine and submits it. If you are mid-turn (the normal case —",
		"you run this from your own tool call), the line is QUEUED and fires when the",
		"current turn ends: your session compacts in place, steered, and continues.",
		"",
		"This is input automation on your own pane, NOT message delivery — agent-to-agent",
		"messaging stays on the hcom bus (`herder send`). There is no target argument and",
		"no pane flag: the only pane herder compact can address is the one it PROVES to be",
		"yours — via HERDER_GUID, else your recorded session id, else an active registry",
		"row for your current terminal corroborated by matching cwd. Unprovable identity,",
		"a non-composer agent (bash), or a dead terminal is refused; it never guesses.",
		"",
		"Options:",
		"  --dry-run   resolve your own pane and print what would be queued, then exit",
		"  --          everything after is steer text (for steers starting with --)",
		"",
		"Exit codes:",
		"  0   queued/submitted — /compact fires at the end of the current turn.",
		"  1   paste attempted but not verified (read your OWN pane before retrying —",
		"      a blind retry may double-queue), or herdr/registry unavailable.",
		"  2   refused: self-identity unprovable, pane blocked by a modal/interrupt",
		"      overlay, agent has no composer, or terminal not live. Nothing was typed.",
		"  64  usage error, or not inside a herdr pane.",
		"",
		"Context-ceiling recipe (skills/orchestrate): commit WIP + write your HANDOFF/",
		"progress state FIRST (compaction loses anything unpersisted), then:",
		"  herder compact 'keep: current unit, ACs, gate commands, thread name; drop tool output'",
		"",
		"If it fails:",
		"  - exit 2 \"no registry row proves this pane is yours\": run inside a",
		"    herder-spawned session, or `herder enroll` this pane first.",
		"  - exit 1 unverified: check your composer/transcript — the line usually DID",
		"    submit; do not resend blind.",
	}
	fmt.Fprint(stdout, strings.Join(lines, "\n")+"\n")
}
