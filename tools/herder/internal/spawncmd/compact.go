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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/occupant"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcred"
)

type compactOptions struct {
	Help        bool
	DryRun      bool
	Steer       string
	Then        string
	ThenSet     bool
	Stop        bool
	ThenTimeout time.Duration
}

// defaultThenTimeout bounds a detached continuation sender's lifetime: long
// enough for a slow pre-compact turn plus compaction, short enough that a wedged
// session never leaves a zombie waiter. --then-timeout overrides it.
const defaultThenTimeout = 15 * time.Minute

// RunCompact executes herder compact and returns the process exit code.
func RunCompact(args []string, stdout, stderr io.Writer) int {
	credentialPath, args, credentialFlagErr := seatcred.ExtractFlag(args)
	if credentialFlagErr != nil {
		dieCompact(stderr, credentialFlagErr.Error())
		return 64
	}
	opts, code := parseCompactArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.Help {
		return 0
	}
	registryPath := registry.DefaultPath()
	cutover, cutoverErr := seatcred.CutoverEnabled(registryPath)
	if cutoverErr != nil {
		dieCompact(stderr, cutoverErr.Error()+" Nothing was typed.")
		return 2
	}
	var selected *seatcred.Selection
	if cutover || credentialPath != "" {
		selection, err := seatcred.Authenticate(registryPath, credentialPath)
		if err != nil {
			dieCompact(stderr, "caller credential refused: "+err.Error()+" Nothing was typed.")
			return 2
		}
		selected = &selection
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

	recs, err := registry.Load(registryPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		dieCompact(stderr, "registry not readable: "+err.Error())
		return 1
	}

	observation := occupant.SelfProbe(occupant.Substrate{Herdr: occupant.CLIQuerier{Client: herdr}})
	claim := os.Getenv("HERDER_GUID")
	if selected != nil {
		claim = selected.GUID
	}
	resolution := occupant.Resolve(observation, v2Sessions(recs), claim)
	if resolution.Row == nil {
		dieCompact(stderr, compactResolutionRefusal(resolution)+" Nothing was typed.")
		return 2
	}
	row := registry.Resolve(recs, resolution.Row.GUID)
	if row == nil {
		dieCompact(stderr, "refused — the live occupant matched a registry guid that is no longer present. Retry so identity can be resolved from a fresh registry snapshot. Nothing was typed.")
		return 2
	}
	if err := healCompactSeat(registryPath, resolution); err != nil {
		dieCompact(stderr, "refused — live occupant matched, but the registry could not append the verb-time observation: "+err.Error()+". Nothing was typed.")
		return 2
	}
	// Refresh after a self-heal so later checks and output use the canonical
	// observation row rather than the pre-operation projection.
	if healed, loadErr := registry.Load(registryPath); loadErr == nil {
		recs = healed
		if refreshed := registry.Resolve(recs, resolution.Row.GUID); refreshed != nil {
			row = refreshed
		}
	}

	if selected != nil {
		// The row's stored terminal_id is provenance, not a location gate: it
		// goes stale at server handoff or detection loss while the caller's
		// pane stays live (TASK-041). The paste target below never depends on
		// it, and the bus verification next is the real credential-vs-caller
		// fence — a fossil mismatch alone must not block self-compaction.
		busRows, listErr := hcomidentity.List(selected.Row.Seat.Namespace)
		if listErr != nil {
			dieCompact(stderr, "refused — credential-selected bus roster unavailable: "+listErr.Error()+". Nothing was typed.")
			return 2
		}
		if verifyErr := seatcred.VerifySelectedBus(busRows, *selected, hcomidentity.Evidence{SessionID: observation.SID}); verifyErr != nil {
			dieCompact(stderr, "refused — "+verifyErr.Error()+" Nothing was typed.")
			return 2
		}
	}
	pane := observation.Pane
	if row.Agent != "claude" && row.Agent != "codex" {
		dieCompact(stderr, fmt.Sprintf("refused — your registry row records agent %q, which has no interactive composer to type /compact into. Nothing was typed.", row.Agent))
		return 2
	}
	if !opts.DryRun && !opts.ThenSet && !opts.Stop {
		if row.Agent == "codex" {
			dieCompact(stderr, "refused — explicit continuation intent is required: pass --stop to compact and go idle. Without it, compaction silently leaves the session dormant. Nothing was typed.")
		} else {
			dieCompact(stderr, "refused — explicit continuation intent is required: pass --then <continuation> to resume after compaction or --stop to compact and go idle. Without either flag, compaction silently leaves the session dormant. Nothing was typed.")
		}
		return 2
	}

	// --then preconditions, checked BEFORE anything is typed (AC#2: a --then
	// that cannot possibly deliver its continuation must not even fire the
	// /compact — the caller asked for compact-THEN-continue, not a bare compact).
	// The continuation targets the caller's OWN verified bus name, captured HERE
	// from the proven self row — never re-resolved from a pane id later (task-034
	// experiment #2). Claude-only: codex compaction semantics differ.
	thenSenderName, thenBusName, thenBusDir := "", "", ""
	if opts.ThenSet {
		if row.Agent != "claude" {
			dieCompact(stderr, fmt.Sprintf("refused — --then is claude-only (codex compaction semantics differ); your registry row records agent %q. Re-run without --then. Nothing was typed.", row.Agent))
			return 2
		}
		thenBusName = row.HcomName
		if thenBusName == "" || thenBusName == "null" {
			dieCompact(stderr, "refused — --then needs your own bus name to deliver the continuation, but your registry row records none (this session is not bus-bound). Run `herder enroll` from this live session to bind its bus identity, then retry. Nothing was typed.")
			return 2
		}
		thenBusDir = row.HcomDir
		rows, listErr := hcomidentity.List(thenBusDir)
		if listErr != nil {
			dieCompact(stderr, fmt.Sprintf("refused — --then row is bus-bound as @%s, but the live bus roster is unavailable (%s). Restore roster access for bus %s, then retry; re-enrolling cannot fix an unavailable roster. Nothing was typed.", thenBusName, listErr.Error(), busDirLabel(thenBusDir)))
			return 2
		}
		// The bus join is armed by the transcript SID observed in this operation,
		// never by launch_context or a persisted repair proof.
		busEvidence := hcomidentity.Evidence{SessionID: observation.SID}
		verified, live := hcomidentity.VerifyStored(rows, busEvidence, thenBusName)
		if !verified {
			cause := live.Reason
			if live.Verified {
				if occupant.ExactEvidence(observation) {
					cause = fmt.Sprintf("registry has @%s but the observed transcript joins live bus @%s", thenBusName, live.Name)
				} else {
					cause = fmt.Sprintf("registry has @%s but cohort-class occupant evidence does not prove that bus binding", thenBusName)
				}
			}
			if cause == "" {
				cause = fmt.Sprintf("stored name @%s is not joined to the observed transcript session", thenBusName)
			}
			dieCompact(stderr, "refused — --then bus identity mismatch: "+cause+". Run `herder enroll` from this live session to bind its bus identity, then retry. Nothing was typed.")
			return 2
		}
		thenSenderName, err = compactThenSenderName(thenBusName)
		if err != nil {
			dieCompact(stderr, fmt.Sprintf("refused — --then cannot construct a distinct continuation sender identity from verified recipient @%s: %v. Repair this session's bus binding with `herder enroll`, then retry. Nothing was typed.", thenBusName, err))
			return 2
		}
	}

	// Paste target: the caller's OWN pane from SelfProbe's live observation.
	// Stored registry coordinates are provenance only and never participate in
	// either self-identity or paste location.
	targetPane := pane.PaneID

	line := "/compact"
	if opts.Steer != "" {
		line += " " + opts.Steer
	}

	if opts.DryRun {
		fmt.Fprintf(stderr, "herder compact --dry-run: would queue %q into own pane %s (terminal %s, guid %s, resolution: occupant-%s)\n",
			line, targetPane, pane.TerminalID, ptrOrEmpty(row.GUID), evidenceClass(observation))
		if opts.ThenSet {
			fmt.Fprintf(stderr, "herder compact --dry-run: --then would arm detached sender %s to deliver to @%s (bus %s) once the paste verified, delivering the continuation (%d chars) after this turn ends (timeout %s)\n",
				thenSenderName, thenBusName, busDirLabel(thenBusDir), runeLen(opts.Then), opts.ThenTimeout)
		}
		return 0
	}

	// KindHint: the row already proved the agent kind, and a detection-lost
	// pane (alive and readable, absent from the agent list) would otherwise
	// leave the paste engine sigil-less and unable to verify submission.
	paste := (&bootPaster{Client: herdr, KindHint: row.Agent, PreflightVisibleOnly: true}).paste(targetPane, line)
	verify, rc := paste.Verify, paste.Code
	switch {
	case verify == "" && rc == 2 && paste.Refusal == "composer_polluted":
		dieCompact(stderr, "refused — your pane still has unsubmitted composer text after the ctrl+u recovery attempt; /compact was NOT typed. Inspect the pane, clear or submit that text by hand, then retry.")
		thenAbortNote(stderr, opts.ThenSet)
		return 2
	case verify == "" && rc == 2:
		dieCompact(stderr, "refused — your pane shows a blocking overlay (modal/interrupted state); /compact was NOT typed. Clear it and retry.")
		thenAbortNote(stderr, opts.ThenSet)
		return 2
	case rc == 0:
		notes := []string(nil)
		if paste.ComposerCleared {
			notes = append(notes, "composer_cleared")
		}
		fmt.Fprintf(stderr, "herder compact: %s — %q is in your composer and fires when the current turn ends (verify: %s%s)\n", queuedWord(verify), line, verify, pasteNoteSuffix(notes))
		// AC#2 ordering floor: the paste is verified (rc==0), so and only so is
		// it safe to arm the continuation — an unverified /compact must never
		// have a continuation fired behind it into an uncompacted session.
		if opts.ThenSet {
			armCompactThen(stderr, ptrOrEmpty(row.ShortGUID), thenSenderName, thenBusName, thenBusDir, opts.Then, int(opts.ThenTimeout/time.Millisecond))
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

func v2Sessions(recs []registry.Record) []v2.SessionRecord {
	latest := registry.LatestByGUID(recs)
	rows := make([]v2.SessionRecord, 0, len(latest))
	for _, rec := range latest {
		var row v2.SessionRecord
		if len(rec.Raw) == 0 || json.Unmarshal(rec.Raw, &row) != nil || row.GUID == "" || row.State == "" {
			row = registry.V2FromRecord(rec, "", rec.State, rec.RecordedAt)
		}
		rows = append(rows, row)
	}
	return rows
}

func compactResolutionRefusal(res occupant.Resolution) string {
	const recovery = " If HERDER_GUID was inherited from another session, unset it and retry; if this seat was never enrolled, run `herder enroll` from this pane."
	switch res.Outcome.Status {
	case occupant.PositiveMismatch:
		if occupant.ExactEvidence(res.Observation) {
			return fmt.Sprintf("refused — positive occupant mismatch: the live transcript sid %q does not belong to the claimed registry identity.", res.Observation.SID)
		}
		return "refused — positive occupant mismatch on cohort-class evidence; the operation is blocked, but that evidence is not authoritative enough to name or displace an owner." + recovery
	case occupant.NoOccupant:
		return "refused — no live tool occupant was found in the caller's pane." + recovery
	case occupant.OutcomeUnprobeable:
		return "refused — the caller's live occupant is unprobeable; compact never guesses a self identity."
	default:
		return "refused — caller identity is ambiguous. If this Claude seat started while agent-session reporting was unavailable, restart it with `herder resume` so SessionStart re-reports the same session id, then retry."
	}
}

func evidenceClass(obs occupant.Observation) string {
	if occupant.ExactEvidence(obs) {
		return "exact"
	}
	return "cohort"
}

// healCompactSeat appends a verb-time observation when a transcript match
// proves that stored coordinates are stale (or the matching SID is historical).
// Mismatch/ambiguity paths never enter this writer.
func healCompactSeat(path string, res occupant.Resolution) error {
	if res.Row == nil || res.Outcome.Status != occupant.Match || res.Observation.Pane.PaneID == "" {
		return nil
	}
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		current := registry.V2ByGUID(tx.Projection, res.Row.GUID)
		if current == nil {
			return nil, fmt.Errorf("matched guid %s disappeared", res.Row.GUID)
		}
		outcome := occupant.Verdict(res.Observation, *current)
		if outcome.Status != occupant.Match {
			return nil, fmt.Errorf("matched guid %s changed during resolution", res.Row.GUID)
		}
		pane := res.Observation.Pane
		unchanged := current.State == v2.StateSeated && current.Seat != nil &&
			current.Seat.PaneID == pane.PaneID && current.Seat.TerminalID == pane.TerminalID &&
			outcome.MatchAge == occupant.Current
		if unchanged {
			return nil, nil
		}

		stamp := time.Now().UTC().Format(time.RFC3339)
		next := *current
		next.Event = "seated"
		next.RecordedAt = stamp
		next.State = v2.StateSeated
		next.ObservedVia = "verb-time occupant probe"
		seat := v2.Seat{Kind: "herdr"}
		if current.Seat != nil {
			seat = *current.Seat
		}
		seat.Node = tx.NodeID
		seat.PaneID = pane.PaneID
		seat.TerminalID = pane.TerminalID
		seat.PID = 0
		if res.Observation.Transcript != "" {
			seat.TranscriptPath = res.Observation.Transcript
		}
		seat.ConfirmedAt = stamp
		next.Seat = &seat
		bindingID, idErr := registry.NewGUID()
		if idErr != nil {
			return nil, idErr
		}
		next.Bindings = append(append([]v2.BindingFact(nil), current.Bindings...), v2.BindingFact{
			ID: bindingID, Field: v2.BindingFieldSeat, EvidenceClass: v2.EvidenceLiveVerified, ObservedAt: stamp,
			Seat: &v2.BindingSeat{Kind: seat.Kind, Node: tx.NodeID, TerminalID: seat.TerminalID, PaneID: seat.PaneID, Namespace: seat.Namespace},
		})
		return []v2.SessionRecord{next}, nil
	})
	if err != nil || len(outcomes) == 0 {
		return err
	}
	outcome, singleErr := registry.SingleOutcome(outcomes)
	if singleErr != nil {
		return singleErr
	}
	return outcome.Err()
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
		case "--stop":
			opts.Stop = true
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
	if opts.ThenSet && opts.Stop {
		dieCompact(stderr, "use --then <continuation> or --stop, not both")
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
		"  herder compact --credential-file PATH [--dry-run] [--then <continuation> [--then-timeout <dur>] | --stop] \\",
		"                 [<steer text> | -- <steer text>]",
		"",
		"Types a real `/compact <steer>` input line into your own composer via the",
		"spawn-private paste engine and submits it. If you are mid-turn (the normal case —",
		"you run this from your own tool call), the line is QUEUED and fires when the",
		"current turn ends: your session compacts in place, steered, and continues.",
		"",
		"Compaction ends the turn. Choose --then to resume automatically or --stop to",
		"explicitly go idle; without that choice the session would become dormant waiting",
		"for human input. --dry-run may omit both because it queues nothing.",
		"",
		"--then <continuation> (compact-then-continue, claude-only): normally /compact",
		"ends the turn and STOPS. With --then, once the /compact paste is VERIFIED, a",
		"detached background sender is armed; it waits for this turn to END, then delivers",
		"<continuation> to your OWN bus name over the hcom bus so it lands AFTER",
		"compaction. It is NOT a second paste: a plain queued line would jump the /compact",
		"queue and be consumed pre-compaction (that is why this is a post-turn bus send).",
		"Turn end is PROVEN, never assumed from a delay: it either observes your live",
		"working→idle status transition or finds it in hcom's event history — a naked",
		"status sample never suffices (a stale read would inject mid-turn). If it cannot",
		"prove the turn ended before --then-timeout it FAILS CLOSED and drops the",
		"continuation loudly (a dropped, re-sendable message beats a silent mid-turn",
		"injection). The continuation targets the bus name proven for THIS session at",
		"compact time — never re-resolved from a pane id. If the /compact paste does not",
		"verify, nothing is armed. Codex is refused: its compaction semantics differ.",
		"Before typing anything, --then also proves that the stored bus name is joined",
		"and belongs to this calling session. A stopped name or a joined neighbor name",
		"is refused. Rerun `herder enroll` from the session to repair the binding.",
		"",
		"This is input automation on your own pane, NOT message delivery — agent-to-agent",
		"messaging stays on the hcom bus (`herder send`). There is no target argument and",
		"no pane flag: the paste target is always your OWN live pane, located from the",
		"calling process's ancestry. HERDR_PANE_ID is only a live-query hint; stored seat",
		"coordinates are provenance and are never identity or location evidence. The live",
		"transcript sid must belong to exactly one registry row's recorded sid lineage; a",
		"credential or HERDER_GUID, when present, is an additional mismatch fence. A stale",
		"matching row is silently rebound to the live observation. Compact refuses on a",
		"positive mismatch, no occupant, ambiguous identity, or an unprobeable occupant;",
		"detection-lost Claude ambiguity advises restarting the seat to re-report its sid.",
		"",
		"Options:",
		"  --dry-run          resolve your own pane and print what would be queued (and",
		"                     what --then would arm), then exit",
		"  --then <msg>       claude-only: after compaction completes, deliver <msg> to",
		"                     your own bus over hcom (compact-then-continue)",
		"  --stop             compact without a continuation and explicitly go idle",
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
		"      overlay, agent has no composer, or own pane unresolvable. Nothing was typed.",
		"  64  usage error, or not inside a herdr pane.",
		"",
		"Context-ceiling recipe (skills/orchestrate): commit WIP + write your HANDOFF/",
		"progress state FIRST (compaction loses anything unpersisted), then:",
		"  herder compact --stop 'keep: current unit, ACs, gate commands, thread name; drop tool output'",
		"To keep going without a human nudging you back afterwards, add a continuation:",
		"  herder compact 'keep: unit, ACs, gate, thread; drop tool output' \\",
		"    --then 'resume the current unit: run the gate, then report DONE'",
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
