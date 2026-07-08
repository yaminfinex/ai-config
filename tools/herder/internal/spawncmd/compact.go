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
	"time"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type compactOptions struct {
	Help        bool
	DryRun      bool
	Steer       string
	Then        string
	ThenSet     bool
	ThenTimeout time.Duration
}

// defaultThenTimeout bounds a detached continuation sender's lifetime: long
// enough for a slow pre-compact turn plus compaction, short enough that a wedged
// session never leaves a zombie waiter. --then-timeout overrides it.
const defaultThenTimeout = 15 * time.Minute

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

	self, refuseMsg := resolveSelfRow(recs, pane)
	if self.row == nil {
		dieCompact(stderr, "refused — "+refuseMsg+" herder compact only ever types into the caller's own pane; without proof of self-identity it refuses. Nothing was typed.")
		return 2
	}
	row := self.row
	if row.Agent != "claude" && row.Agent != "codex" {
		dieCompact(stderr, fmt.Sprintf("refused — your registry row records agent %q, which has no interactive composer to type /compact into. Nothing was typed.", row.Agent))
		return 2
	}

	// --then preconditions, checked BEFORE anything is typed (AC#2: a --then
	// that cannot possibly deliver its continuation must not even fire the
	// /compact — the caller asked for compact-THEN-continue, not a bare compact).
	// The continuation targets the caller's OWN verified bus name, captured HERE
	// from the proven self row — never re-resolved from a pane id later (task-034
	// experiment #2). Claude-only: codex compaction semantics differ.
	thenBusName, thenBusDir := "", ""
	if opts.ThenSet {
		if row.Agent != "claude" {
			dieCompact(stderr, fmt.Sprintf("refused — --then is claude-only (codex compaction semantics differ); your registry row records agent %q. Re-run without --then. Nothing was typed.", row.Agent))
			return 2
		}
		thenBusName = row.HcomName
		if thenBusName == "" || thenBusName == "null" {
			dieCompact(stderr, "refused — --then needs your own bus name to deliver the continuation, but your registry row records none (this session is not bus-bound). Re-run without --then, or enroll on the bus first. Nothing was typed.")
			return 2
		}
		thenBusDir = row.HcomDir
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
		// The env pane's LIVE terminal disagrees with the registry row. A
		// durable key alone cannot arbitrate this: HERDER_GUID (and equally a
		// session id) can be stale or inherited by a process that is NOT in
		// that row's pane, and typing there would break the self-pane-only
		// guarantee (codex review P1). Proceed only when a SECOND independent
		// self signal corroborates the row — the caller's current
		// HCOM_SESSION_ID matching the row's recorded session — which proves
		// this process IS that session and the env pane id merely drifted.
		if !self.corroborated {
			dieCompact(stderr, fmt.Sprintf("refused — your pane's live terminal (%s) disagrees with your registry row's (%s) and no second self signal corroborates the row (HCOM_SESSION_ID matching its recorded session). A stale or inherited HERDER_GUID looks exactly like this. Nothing was typed.", pane.TerminalID, row.TerminalID))
			return 2
		}
		fmt.Fprintf(stderr, "herder compact: note — HERDR_PANE_ID (%s) no longer names your terminal (env: %s, registry: %s); session id corroborates the row, using its live pane %s\n", envPane, pane.TerminalID, row.TerminalID, targetPane)
	}

	line := "/compact"
	if opts.Steer != "" {
		line += " " + opts.Steer
	}

	if opts.DryRun {
		fmt.Fprintf(stderr, "herder compact --dry-run: would queue %q into own pane %s (terminal %s, guid %s, resolution: %s)\n",
			line, targetPane, row.TerminalID, ptrOrEmpty(row.GUID), map[bool]string{true: "positional+cwd", false: "durable-key"}[self.positional])
		if opts.ThenSet {
			fmt.Fprintf(stderr, "herder compact --dry-run: --then would arm a detached bus sender to @%s (bus %s) once the paste verified, delivering the continuation (%d chars) after this turn ends (timeout %s)\n",
				thenBusName, busDirLabel(thenBusDir), runeLen(opts.Then), opts.ThenTimeout)
		}
		return 0
	}

	verify, rc := (&bootPaster{Client: herdr, PreflightVisibleOnly: true}).paste(targetPane, line)
	switch {
	case verify == "" && rc == 2:
		dieCompact(stderr, "refused — your pane shows a blocking overlay (modal/interrupted state); /compact was NOT typed. Clear it and retry.")
		thenAbortNote(stderr, opts.ThenSet)
		return 2
	case rc == 0:
		fmt.Fprintf(stderr, "herder compact: %s — %q is in your composer and fires when the current turn ends (verify: %s)\n", queuedWord(verify), line, verify)
		// AC#2 ordering floor: the paste is verified (rc==0), so and only so is
		// it safe to arm the continuation — an unverified /compact must never
		// have a continuation fired behind it into an uncompacted session.
		if opts.ThenSet {
			armCompactThen(stderr, ptrOrEmpty(row.ShortGUID), thenBusName, thenBusDir, opts.Then, int(opts.ThenTimeout/time.Millisecond))
		}
		return 0
	case verify == "not_landed":
		dieCompact(stderr, "paste did not land — nothing appeared in your composer; nothing was submitted. Retry is safe.")
		thenAbortNote(stderr, opts.ThenSet)
		return 1
	default:
		dieCompact(stderr, "typed but submission unverified (verify: "+verify+") — read your own pane before retrying; a blind retry may double-queue /compact.")
		thenAbortNote(stderr, opts.ThenSet)
		return 1
	}
}

// thenAbortNote states plainly that --then armed nothing when the /compact
// paste did not verify (AC#2): the caller is never left wondering whether a
// continuation is about to fire into an uncompacted session.
func thenAbortNote(stderr io.Writer, thenSet bool) {
	if thenSet {
		fmt.Fprintln(stderr, "herder compact: --then NOT armed — the /compact paste was not verified, so no continuation was scheduled. Nothing will be delivered.")
	}
}

// selfIdentity is resolveSelfRow's verdict: the caller's own registry row,
// how it was proven, and whether a SECOND independent signal (guid row and
// session row agreeing) corroborates it — required before the row's terminal
// may override a disagreeing live env pane (codex review P1).
type selfIdentity struct {
	row          *registry.Record
	positional   bool
	corroborated bool
}

// resolveSelfRow proves which registry identity is the caller's own. Durable
// keys first: HERDER_GUID (every herder-spawned/forked/resumed session) and
// the hcom session id recorded in provenance — when BOTH are present they
// must agree on one identity (a mismatch means at least one is stale or
// inherited: refuse, never pick). Only when neither exists does it fall back
// to positional resolution by the CURRENT terminal — and then it demands
// corroborating evidence (the pane's foreground cwd matching our own working
// directory) because a positional match cannot otherwise be told apart from a
// neighbour after pane-id churn.
func resolveSelfRow(recs []registry.Record, pane herdrcli.Pane) (selfIdentity, string) {
	guid := os.Getenv("HERDER_GUID")
	sessionID := os.Getenv("HCOM_SESSION_ID")

	var guidRow, sessRow *registry.Record
	if guid != "" {
		guidRow = registry.Resolve(recs, guid)
		if guidRow == nil {
			return selfIdentity{}, "HERDER_GUID=" + guid + " has no registry row."
		}
	}
	if sessionID != "" {
		sessRow = registry.ResolveByToolSessionID(recs, sessionID)
	}
	if guidRow != nil && sessRow != nil && !sameGUID(guidRow, sessRow) {
		return selfIdentity{}, fmt.Sprintf("HERDER_GUID (%s) and HCOM_SESSION_ID (%s) resolve to DIFFERENT identities (%s vs %s) — at least one is stale or inherited.", guid, sessionID, ptrOrEmpty(guidRow.GUID), ptrOrEmpty(sessRow.GUID))
	}
	if row := firstRow(guidRow, sessRow); row != nil {
		if row.TerminalID == "" {
			return selfIdentity{}, "your registry row (guid " + ptrOrEmpty(row.GUID) + ") records no terminal_id."
		}
		return selfIdentity{row: row, corroborated: guidRow != nil && sessRow != nil}, ""
	}

	row := registry.ActiveByPaneOrTerminal(recs, pane.TerminalID)
	if row == nil {
		return selfIdentity{positional: true}, "no registry row proves this pane is yours (no HERDER_GUID, no session match, no active row for terminal " + pane.TerminalID + ")."
	}
	wd, _ := os.Getwd()
	paneCWD := pane.ForegroundCWD
	if paneCWD == "" {
		paneCWD = pane.CWD
	}
	if wd == "" || paneCWD == "" || wd != paneCWD {
		return selfIdentity{positional: true}, fmt.Sprintf("positional identity only (terminal %s) and the pane's foreground cwd (%q) does not corroborate this process's cwd (%q).", pane.TerminalID, paneCWD, wd)
	}
	return selfIdentity{row: row, positional: true}, ""
}

func sameGUID(a, b *registry.Record) bool {
	return a.GUID != nil && b.GUID != nil && *a.GUID == *b.GUID
}

func firstRow(rows ...*registry.Record) *registry.Record {
	for _, row := range rows {
		if row != nil {
			return row
		}
	}
	return nil
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
		case "--then":
			if i+1 >= len(args) {
				dieCompact(stderr, "--then requires a continuation message (the prompt to deliver over the bus after compaction)")
				return opts, 64
			}
			opts.Then = args[i+1]
			opts.ThenSet = true
			i++
		case "--then-timeout":
			if i+1 >= len(args) {
				dieCompact(stderr, "--then-timeout requires a duration (e.g. 15m, 900s)")
				return opts, 64
			}
			dur, err := time.ParseDuration(args[i+1])
			if err != nil || dur <= 0 {
				dieCompact(stderr, "--then-timeout must be a positive Go duration (e.g. 15m, 900s): "+args[i+1])
				return opts, 64
			}
			opts.ThenTimeout = dur
			i++
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
	if opts.ThenSet && strings.TrimSpace(opts.Then) == "" {
		dieCompact(stderr, "--then continuation message is empty — pass the prompt to deliver after compaction, or drop --then")
		return opts, 64
	}
	if opts.ThenTimeout == 0 {
		opts.ThenTimeout = defaultThenTimeout
	}
	return opts, 0
}

func printCompactHelp(stdout io.Writer) {
	lines := []string{
		"herder compact — queue a steered /compact into the CALLER'S OWN pane (self only).",
		"",
		"Usage:",
		"  herder compact [--dry-run] [--then <continuation> [--then-timeout <dur>]] \\",
		"                 [<steer text> | -- <steer text>]",
		"",
		"Types a real `/compact <steer>` input line into your own composer via the",
		"spawn-private paste engine and submits it. If you are mid-turn (the normal case —",
		"you run this from your own tool call), the line is QUEUED and fires when the",
		"current turn ends: your session compacts in place, steered, and continues.",
		"",
		"--then <continuation> (compact-then-continue, claude-only): normally /compact",
		"ends the turn and STOPS. With --then, once the /compact paste is VERIFIED, a",
		"detached background sender is armed; it waits for this turn to END (watching your",
		"own hcom session status flip working→idle, never a fixed sleep), then delivers",
		"<continuation> to your OWN bus name over the hcom bus so it lands AFTER",
		"compaction. It is NOT a second paste: a plain queued line would jump the /compact",
		"queue and be consumed pre-compaction (that is why this is a post-turn bus send).",
		"The continuation targets the bus name proven for THIS session at compact time —",
		"never re-resolved from a pane id. If the /compact paste does not verify, nothing",
		"is armed (no continuation ever fires into an uncompacted session). Codex is",
		"refused: its compaction semantics differ.",
		"",
		"This is input automation on your own pane, NOT message delivery — agent-to-agent",
		"messaging stays on the hcom bus (`herder send`). There is no target argument and",
		"no pane flag: the only pane herder compact can address is the one it PROVES to be",
		"yours — via HERDER_GUID, else your recorded session id, else an active registry",
		"row for your current terminal corroborated by matching cwd. If guid and session",
		"id disagree, or your row's terminal disagrees with your live pane without a",
		"session-id corroboration (a stale/inherited HERDER_GUID looks exactly like",
		"that), it refuses. Unprovable identity, a non-composer agent (bash), or a dead",
		"terminal is refused; it never guesses.",
		"",
		"Options:",
		"  --dry-run          resolve your own pane and print what would be queued (and",
		"                     what --then would arm), then exit",
		"  --then <msg>       claude-only: after compaction completes, deliver <msg> to",
		"                     your own bus over hcom (compact-then-continue)",
		"  --then-timeout <d> bound the detached sender's wait for turn end (default 15m);",
		"                     on timeout it gives up loudly in its log, never zombies",
		"  --                 everything after is steer text (for steers starting with --)",
		"",
		"--then diagnostics: the detached sender logs one line per phase (armed → turn",
		"ended → delivered/queued, or TIMEOUT with a manual-send remedy) to",
		"<herder-state-dir>/compact-then/compact-then-<short>-<pid>.log.",
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
		"To keep going without a human nudging you back afterwards, add a continuation:",
		"  herder compact 'keep: unit, ACs, gate, thread; drop tool output' \\",
		"    --then 'resume TASK-XXX: run the gate, then report DONE on thread unit-w'",
		"",
		"If it fails:",
		"  - exit 2 \"no registry row proves this pane is yours\": run inside a",
		"    herder-spawned session, or `herder enroll` this pane first.",
		"  - exit 1 unverified: check your composer/transcript — the line usually DID",
		"    submit; do not resend blind. Note: a line left UNSUBMITTED in a composer",
		"    also starves incoming hcom delivery (nothing injects until it is submitted",
		"    or cleared) — don't leave one stranded.",
	}
	fmt.Fprint(stdout, strings.Join(lines, "\n")+"\n")
}
