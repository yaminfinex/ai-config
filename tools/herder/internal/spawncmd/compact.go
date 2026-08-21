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
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/seatcred"
	"ai-config/tools/herder/internal/shellquote"
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

	// Self-location by live evidence only (TASK-041). PRIMARY: the occupant
	// probe pointed at oneself — enumerate the live panes and find the ONE
	// whose process tree contains this process. The caller is provably
	// inside its own pane, with no env, no herdr agent detection, and no
	// stored coordinates needed — manual seats herdr never launched
	// included. An authoritative no-match or multi-match fails closed.
	// FALLBACK (probe transport unavailable only — older herdr without
	// pane.process_info, partial probe outage): HERDR_PANE_ID re-resolved
	// through a live `pane get`; the env value is a launch-epoch entry
	// point, never trusted un-resolved.
	envPane := os.Getenv("HERDR_PANE_ID")
	pane, probeErr := locateOwnPaneByPID(herdr)
	switch {
	case probeErr == nil:
		// Located by occupancy proof.
	case errors.Is(probeErr, errSelfProbeNoMatch), errors.Is(probeErr, errSelfProbeAmbiguous):
		dieCompact(stderr, "refused — cannot prove which pane is yours: "+probeErr.Error()+". herder compact only ever types into the caller's own pane; without proof it refuses. If you ARE at a pane you can see, the manual recovery is: herdr pane send-keys <your-pane> ctrl+u; herdr pane send-text <your-pane> '/compact <steer>'; herdr pane send-keys <your-pane> Enter. Nothing was typed.")
		return 2
	default:
		if os.Getenv("HERDR_ENV") != "1" || envPane == "" {
			dieCompact(stderr, "cannot locate own pane: occupant probe unavailable ("+probeErr.Error()+") and not running inside a herdr pane environment (HERDR_ENV/HERDR_PANE_ID absent) — herder compact queues input to the caller's OWN pane only")
			return 64
		}
		staleEnvRecovery := " If HERDR_PANE_ID is stale (a restarted or resumed-in-place session inherits the old value), find your live pane id with `herdr pane list` and re-run as HERDR_PANE_ID=<live-pane-id> herder compact ..."
		out, err := herdr.Output("pane", "get", envPane)
		if err != nil {
			dieCompact(stderr, "refused — cannot resolve own pane: occupant probe unavailable ("+probeErr.Error()+") and herdr pane get failed for "+envPane+"."+staleEnvRecovery+" Nothing was typed.")
			return 2
		}
		pane, err = herdrcli.ParsePaneGet(out)
		if err != nil || pane.TerminalID == "" {
			dieCompact(stderr, "refused — cannot resolve own pane: occupant probe unavailable ("+probeErr.Error()+") and no terminal_id for "+envPane+"."+staleEnvRecovery+" Nothing was typed.")
			return 2
		}
	}

	recs, err := registry.Load(registryPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		dieCompact(stderr, "registry not readable: "+err.Error())
		return 1
	}

	self := selfIdentity{}
	if selected == nil {
		var refuseMsg string
		self, refuseMsg = resolveSelfRow(recs, pane)
		if self.row == nil {
			dieCompact(stderr, "refused — "+refuseMsg+" herder compact only ever types into the caller's own pane; without proof of self-identity it refuses. Nothing was typed.")
			return 2
		}
	} else {
		row := registry.Resolve(recs, selected.GUID)
		if row == nil || !registry.IsSeated(*row) {
			dieCompact(stderr, "refused — the credential-selected guid has no seated compatibility row. Nothing was typed.")
			return 2
		}
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
		if verifyErr := seatcred.VerifySelectedBus(busRows, *selected, hcomidentity.CurrentEvidence(envPane, pane.PaneID)); verifyErr != nil {
			dieCompact(stderr, "refused — "+verifyErr.Error()+" Nothing was typed.")
			return 2
		}
		self = selfIdentity{row: row}
	}
	row := self.row
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
			dieCompact(stderr, "refused — --then needs your own bus name to deliver the continuation, but your registry row records none (this session is not bus-bound). The bus can be live while an adopted row remains unbound when a hand-resumed process has no ambient identity proof. Find the transcript session id in the resumed agent's session metadata (the id used by its resume command). From this pane, replace <resumed-session-id> in this repair command with that id, then run it: "+pinnedBusRepair(row)+". Retry --then afterward. Nothing was typed.")
			return 2
		}
		thenBusDir = row.HcomDir
		rows, listErr := hcomidentity.List(thenBusDir)
		if listErr != nil {
			dieCompact(stderr, fmt.Sprintf("refused — --then row is bus-bound as @%s, but the live bus roster is unavailable (%s). Restore roster access for bus %s, then retry; re-enrolling cannot fix an unavailable roster. Nothing was typed.", thenBusName, listErr.Error(), busDirLabel(thenBusDir)))
			return 2
		}
		busEvidence := hcomidentity.CurrentEvidence(envPane, pane.PaneID)
		// A pinned re-enroll can repair a hand-resumed row even though it cannot
		// retroactively inject HCOM_SESSION_ID into the already-running parent
		// process. Once self-row identity is proven above, the repair's recorded
		// session id can drive the live roster check. Older repair writers omitted
		// hcom_verified, so their other persisted fields must match the exact old
		// enroll shape before the id is used. Some are writer-derived consistency
		// and drift checks, not independent corroboration; the joined live roster
		// match below remains the proof that arms delivery.
		recordedSID, recordedUnavailable := "", ""
		if busEvidence.SessionID == "" {
			recordedSID, recordedUnavailable = recordedBusSessionEvidence(row)
			if recordedSID != "" {
				busEvidence.SessionID = recordedSID
			}
		}
		verified, live := hcomidentity.VerifyStored(rows, busEvidence, thenBusName)
		if !verified {
			cause := live.Reason
			if live.Verified {
				cause = fmt.Sprintf("registry has @%s but the calling session is live as @%s", thenBusName, live.Name)
				dieCompact(stderr, "refused — --then bus identity mismatch: "+cause+". Rerun `herder enroll` from this session to repair its bus binding, then retry. Nothing was typed.")
			} else if recordedUnavailable != "" {
				dieCompact(stderr, fmt.Sprintf("refused — --then row is bus-bound as @%s, but recorded-SID verification cannot arm: %s; ambient live evidence also failed (%s). Supply HCOM_SESSION_ID for this invocation or repair that recorded proof, then retry. Nothing was typed.", thenBusName, recordedUnavailable, cause))
			} else if recordedSID != "" {
				dieCompact(stderr, fmt.Sprintf("refused — --then row is bus-bound as @%s and recorded-SID verification armed with %q, but %s. Restore the matching joined bus row, or re-enroll if the stored binding is stale, then retry. Nothing was typed.", thenBusName, recordedSID, cause))
			} else {
				if cause == "" {
					cause = fmt.Sprintf("stored name @%s is not provably the calling session", thenBusName)
				}
				dieCompact(stderr, "refused — --then bus identity mismatch: "+cause+". Rerun `herder enroll` from this session to repair its bus binding, then retry. Nothing was typed.")
			}
			return 2
		}
		thenSenderName, err = compactThenSenderName(thenBusName)
		if err != nil {
			dieCompact(stderr, fmt.Sprintf("refused — --then cannot construct a distinct continuation sender identity from verified recipient @%s: %v. Repair this session's bus binding with `herder enroll`, then retry. Nothing was typed.", thenBusName, err))
			return 2
		}
	}

	// Paste target: the caller's OWN live pane — envPane re-resolved through
	// `pane get` at entry. pane.PaneID is the canonical id (the env value can
	// be a legacy alias herdr's agent list never shows). Stored registry
	// coordinates are provenance, never location: a row whose terminal_id
	// went stale (server handoff, detection loss, resume-in-place) must not
	// block self-compaction, and since the target never depends on the row, a
	// stale or inherited HERDER_GUID can mislabel the row but can never
	// redirect the paste — the self-pane-only guarantee rests on the live
	// env-pane resolution alone (TASK-041; slimdown charter decisions 1-2).
	targetPane := pane.PaneID
	if row.TerminalID != "" && row.TerminalID != pane.TerminalID {
		fmt.Fprintf(stderr, "herder compact: note — registry row (guid %s) records terminal %s but your live pane %s holds %s; stored coordinates are stale, proceeding on live evidence\n", ptrOrEmpty(row.GUID), row.TerminalID, targetPane, pane.TerminalID)
	}

	line := "/compact"
	if opts.Steer != "" {
		line += " " + opts.Steer
	}

	if opts.DryRun {
		fmt.Fprintf(stderr, "herder compact --dry-run: would queue %q into own pane %s (terminal %s, guid %s, resolution: %s)\n",
			line, targetPane, pane.TerminalID, ptrOrEmpty(row.GUID), map[bool]string{true: "positional+cwd", false: "durable-key"}[self.positional])
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

// recordedBusSessionEvidence returns the durable session correlate captured
// when a bus binding was proven. Newer repair writers mark that proof with
// hcom_verified=true.
// The older manual-repair shape omitted that bit, so it is accepted only when
// its persisted seated/enroll shape is internally consistent. V2FromRecord
// derives both confirmed continuity and the harvest SID from
// provenance.tool_session_id; those checks defend compatibility and writer
// drift, but do not provide independent corroboration. Explicit false stays
// fail-closed because it means the binding was not proven, whether written by
// a failed verification or by a conservative default/carry path.
func recordedBusSessionEvidence(row *registry.Record) (string, string) {
	if row.HcomVerified != nil && !*row.HcomVerified {
		return "", "seat.hcom_verified is explicitly false"
	}
	if row.Provenance == nil || row.Provenance.ToolSessionID == "" {
		return "", "the row has no provenance.tool_session_id"
	}
	sid := row.Provenance.ToolSessionID
	if row.HcomVerified != nil && *row.HcomVerified {
		return sid, ""
	}

	var persisted struct {
		State      string `json:"state"`
		Status     string `json:"status"`
		Continuity string `json:"continuity"`
		Seat       struct {
			HcomName string `json:"hcom_name"`
		} `json:"seat"`
		SIDs []struct {
			SID    string `json:"sid"`
			Source string `json:"source"`
		} `json:"sids"`
		Provenance struct {
			Mechanism     string `json:"mechanism"`
			ToolSessionID string `json:"tool_session_id"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(row.Raw, &persisted); err != nil {
		return "", "the compatibility proof row cannot be decoded"
	}
	if persisted.State == "" && persisted.Status != "" {
		return "", "seat.hcom_verified is absent and legacy-v1 rows do not carry the v2 recorded-SID repair proof"
	}
	if persisted.State != "seated" {
		return "", fmt.Sprintf("seat.hcom_verified is absent and row state is %q, not seated", persisted.State)
	}
	if persisted.Seat.HcomName == "" || persisted.Seat.HcomName != row.HcomName {
		return "", "seat.hcom_verified is absent and the persisted bus name is missing or inconsistent"
	}
	if persisted.Provenance.Mechanism != "enroll" {
		return "", fmt.Sprintf("seat.hcom_verified is absent and provenance.mechanism is %q, not enroll", persisted.Provenance.Mechanism)
	}
	if persisted.Continuity != "confirmed" {
		return "", fmt.Sprintf("seat.hcom_verified is absent and continuity is %q, not confirmed", persisted.Continuity)
	}
	if persisted.Provenance.ToolSessionID != sid {
		return "", "seat.hcom_verified is absent and provenance.tool_session_id is inconsistent"
	}
	for _, recorded := range persisted.SIDs {
		if recorded.SID == sid && recorded.Source == "harvest" {
			return sid, ""
		}
	}
	return "", "seat.hcom_verified is absent and no harvest SID matches provenance.tool_session_id"
}

func pinnedBusRepair(row *registry.Record) string {
	return "HCOM_SESSION_ID='<resumed-session-id>'" +
		" HERDER_GUID=" + shellquote.Quote(ptrOrEmpty(row.GUID)) +
		" HERDER_LABEL=" + shellquote.Quote(ptrOrEmpty(row.Label)) +
		" HERDER_ROLE=" + shellquote.Quote(row.Role) +
		" herder enroll"
}

// thenAbortNote states plainly that --then armed nothing when the /compact
// paste did not verify (AC#2): the caller is never left wondering whether a
// continuation is about to fire into an uncompacted session.
func thenAbortNote(stderr io.Writer, thenSet bool) {
	if thenSet {
		fmt.Fprintln(stderr, "herder compact: --then NOT armed — the /compact paste was not verified, so no continuation was scheduled. Nothing will be delivered.")
	}
}

// selfIdentity is resolveSelfRow's verdict: the caller's own registry row and
// how it was proven. The row names WHO the caller is (agent kind, --then bus
// binding); it never locates the paste target, which is always the caller's
// own live pane.
type selfIdentity struct {
	row        *registry.Record
	positional bool
}

// resolveSelfRow proves which registry identity is the caller's own. Durable
// keys first: HERDER_GUID (every herder-spawned/forked/resumed session) and
// the hcom session id recorded in provenance — when BOTH are present they
// must agree on one identity (a mismatch means at least one is stale or
// inherited: refuse, never pick). Only when neither exists does it fall back
// to resolution by the caller's LIVE coordinates (current terminal, then
// current canonical pane id — the latter survives terminal reissue at server
// handoff) — and then it demands corroborating evidence (the pane's
// foreground cwd containing our own working directory) because a
// coordinate-only match cannot otherwise be told apart from a stale registry
// identity after restart reshuffle. Every refusal names a recovery step
// (TASK-041 AC#2).
func resolveSelfRow(recs []registry.Record, pane herdrcli.Pane) (selfIdentity, string) {
	guid := os.Getenv("HERDER_GUID")
	sessionID := os.Getenv("HCOM_SESSION_ID")

	var guidRow, sessRow *registry.Record
	if guid != "" {
		guidRow = registry.Resolve(recs, guid)
		if guidRow == nil {
			return selfIdentity{}, "HERDER_GUID=" + guid + " has no registry row. If it was inherited from another session's environment, re-run with it unset; to seat this session, run `herder enroll` from this pane."
		}
	}
	if sessionID != "" {
		sessRow = registry.ResolveByToolSessionID(recs, sessionID)
	}
	if guidRow != nil && sessRow != nil && !sameGUID(guidRow, sessRow) {
		return selfIdentity{}, fmt.Sprintf("HERDER_GUID (%s) and HCOM_SESSION_ID (%s) resolve to DIFFERENT identities (%s vs %s) — at least one is stale or inherited. Unset the stale variable and retry, or run `herder enroll` from this pane to re-prove identity.", guid, sessionID, ptrOrEmpty(guidRow.GUID), ptrOrEmpty(sessRow.GUID))
	}
	if row := firstRow(guidRow, sessRow); row != nil {
		return selfIdentity{row: row}, ""
	}

	row := registry.SeatedByPaneOrTerminal(recs, pane.TerminalID)
	if row == nil {
		row = registry.SeatedByPaneOrTerminal(recs, pane.PaneID)
	}
	if row == nil {
		return selfIdentity{positional: true}, "no registry row proves this pane is yours (no HERDER_GUID, no session match, no seated session for terminal " + pane.TerminalID + " or pane " + pane.PaneID + "). Run `herder enroll` from this pane to seat it, then retry."
	}
	wd, _ := os.Getwd()
	paneCWD := pane.ForegroundCWD
	if paneCWD == "" {
		paneCWD = pane.CWD
	}
	if wd == "" || paneCWD == "" || !cwdWithin(wd, paneCWD) {
		return selfIdentity{positional: true}, fmt.Sprintf("positional identity only (terminal %s) and the pane's foreground cwd (%q) does not corroborate this process's cwd (%q). Re-run from that directory (or a subdirectory of it), or run `herder enroll` from this pane to bind a durable identity.", pane.TerminalID, paneCWD, wd)
	}
	return selfIdentity{row: row, positional: true}, ""
}

// cwdWithin accepts wd equal to paneCWD or nested anywhere beneath it: a
// process running in a subdirectory of the pane's foreground cwd is the same
// session (TASK-041 AC#3, lale field report).
func cwdWithin(wd, paneCWD string) bool {
	return wd == paneCWD || strings.HasPrefix(wd, strings.TrimRight(paneCWD, "/")+"/")
}

var (
	errSelfProbeNoMatch   = errors.New("no live pane's process tree contains this process")
	errSelfProbeAmbiguous = errors.New("multiple live panes' process trees contain this process")
)

// locateOwnPaneByPID is the occupant probe pointed at oneself (TASK-041,
// compact-scoped forerunner of slimdown charter decision 1): find the live
// pane whose process tree contains this process. herder runs as a tool-call
// descendant of the agent process, itself a descendant of the pane shell, so
// a pane owns the caller when its shell pid or any foreground pid is this
// process or one of its ancestors. pid 1 is everyone's ancestor and proves
// nothing, so it never matches.
//
// Errors are three-way: errSelfProbeNoMatch means every pane answered and
// none contains us (authoritative — the caller is not in any live pane);
// errSelfProbeAmbiguous means more than one does (fail closed, never pick);
// any other error means the probe transport could not answer (pane list
// unsupported/failed, or no pane's process_info was queryable) and the
// caller may fall back to other entry points.
func locateOwnPaneByPID(herdr *herdrcli.Client) (herdrcli.Pane, error) {
	out, err := herdr.Output("pane", "list")
	if err != nil {
		return herdrcli.Pane{}, fmt.Errorf("herdr pane list failed: %w", err)
	}
	panes, err := herdrcli.ParsePaneList(out)
	if err != nil {
		return herdrcli.Pane{}, fmt.Errorf("herdr pane list unparseable: %w", err)
	}
	if len(panes) == 0 {
		return herdrcli.Pane{}, errors.New("herdr pane list reports no panes")
	}
	ancestors := selfAndAncestorPIDs()
	matches := []herdrcli.Pane(nil)
	probed := 0
	for _, pane := range panes {
		infoOut, infoErr := herdr.Output("pane", "process_info", pane.PaneID)
		if infoErr != nil {
			continue
		}
		info, parseErr := herdrcli.ParseProcessInfo(infoOut)
		if parseErr != nil {
			continue
		}
		probed++
		if paneOwnsPIDs(info, ancestors) {
			matches = append(matches, pane)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.PaneID
		}
		return herdrcli.Pane{}, fmt.Errorf("%w (pid %d, panes %s)", errSelfProbeAmbiguous, os.Getpid(), strings.Join(ids, ", "))
	case probed == 0:
		return herdrcli.Pane{}, errors.New("pane.process_info answered for no pane")
	case probed < len(panes):
		// Zero matches but some panes were unprobeable: not authoritative —
		// the caller could be in one of the unprobed panes.
		return herdrcli.Pane{}, fmt.Errorf("pane.process_info answered for only %d of %d panes and none of those contains this process", probed, len(panes))
	default:
		return herdrcli.Pane{}, fmt.Errorf("%w (pid %d, %d panes probed)", errSelfProbeNoMatch, os.Getpid(), probed)
	}
}

func paneOwnsPIDs(info herdrcli.ProcessInfo, pids map[int]bool) bool {
	if info.ShellPID > 1 && pids[info.ShellPID] {
		return true
	}
	for _, proc := range info.Processes {
		if proc.PID > 1 && pids[proc.PID] {
			return true
		}
	}
	return false
}

// selfAndAncestorPIDs is this process plus its ancestry walked toward init
// (excluded: pid 1 proves nothing). Cycles and unreadable parents terminate
// the walk.
func selfAndAncestorPIDs() map[int]bool {
	pids := map[int]bool{}
	for pid := os.Getpid(); pid > 1 && !pids[pid]; pid = parentPID(pid) {
		pids[pid] = true
	}
	return pids
}

// parentPID reads the parent from /proc (field 4 of stat, after the last ')'
// since comm may contain spaces or parens), falling back to `ps` where /proc
// does not exist (darwin).
func parentPID(pid int) int {
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		s := string(data)
		if idx := strings.LastIndexByte(s, ')'); idx >= 0 {
			if fields := strings.Fields(s[idx+1:]); len(fields) >= 2 {
				if ppid, err := strconv.Atoi(fields[1]); err == nil {
					return ppid
				}
			}
		}
		return 0
	}
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return ppid
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
		"no pane flag: the paste target is always your OWN live pane (HERDR_PANE_ID",
		"re-resolved live at entry), so a stale registry row can never redirect the",
		"paste. The registry row names WHO you are — via HERDER_GUID / credential, else",
		"your recorded session id, else a seated row for your live terminal or pane id",
		"corroborated by cwd — and stale stored coordinates on that row are noted, never",
		"blocking. It still refuses when identity is unprovable: guid and session id",
		"disagreeing, no row at all, a non-composer agent (bash), or an unresolvable",
		"HERDR_PANE_ID; every refusal names a recovery step.",
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
