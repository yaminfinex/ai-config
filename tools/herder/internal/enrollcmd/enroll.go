package enrollcmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/observercmd"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

type options struct {
	help  bool
	json  bool
	label string
	role  string
}

func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, false, "")
}

// RunFreshForAdoption enrolls a replacement with a new guid while preserving
// the old row for adopt's subsequent atomic unseat-and-label-transfer batch.
func RunFreshForAdoption(args []string, stdout, stderr io.Writer, oldGUID string) int {
	return run(args, stdout, stderr, true, oldGUID)
}

func run(args []string, stdout, stderr io.Writer, forceFreshGUID bool, preserveGUID string) int {
	opts, code := parseArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.help {
		return 0
	}
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_PANE_ID") == "" {
		die(stderr, "not running inside a herdr pane (HERDR_ENV/HERDR_PANE_ID required)")
		return 1
	}
	if _, err := exec.LookPath("herdr"); err != nil {
		die(stderr, "herdr not on PATH")
		return 1
	}

	paneID := os.Getenv("HERDR_PANE_ID")
	out, err := (&herdrcli.Client{}).Output("pane", "get", paneID)
	if err != nil {
		die(stderr, "herdr pane get failed for "+paneID)
		return 1
	}
	pane, err := herdrcli.ParsePaneGet(out)
	if err != nil {
		die(stderr, "could not parse herdr pane get for "+paneID)
		return 1
	}
	if pane.PaneID == "" {
		pane.PaneID = paneID
	}
	if pane.CWD == "" {
		pane.CWD, _ = os.Getwd()
	}
	hcomDir := os.Getenv("HCOM_DIR")
	liveBus := hcomidentity.ResolveLive(hcomDir, hcomidentity.CurrentEvidence(paneID, pane.PaneID))
	if !liveBus.Verified {
		fmt.Fprintf(stderr, "herder enroll: live bus identity could not be verified (%s); recording hcom_name as unknown. Join this session to hcom, then rerun `herder enroll` to repair the row.\n", liveBus.Reason)
	}

	requestedGUID := os.Getenv("HERDER_GUID")
	guid := requestedGUID
	if forceFreshGUID {
		guid = ""
	}
	short := ""
	label := ""

	registryPath := registry.DefaultPath()
	var appendedRow []byte
	// PreserveToolSessionID needs the append-only history when another writer's
	// latest row dropped its SID. The locked latest projection is appended to
	// this snapshot below so a concurrent current fact wins the fallback scan.
	priorRecords, loadErr := registry.Load(registryPath)
	if loadErr != nil && !os.IsNotExist(loadErr) {
		die(stderr, loadErr.Error())
		return 1
	}

	nowISO := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	var cleanupMessages []string
	outcomes, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		sessions := tx.Projection.Sessions()
		var latest *v2.SessionRecord
		for _, rec := range sessions {
			if guid != "" && rec.GUID == guid {
				cp := rec
				latest = &cp
				break
			}
		}

		coreMatches := matchingLiveSeatRows(sessions, pane, liveBus)
		if latest == nil && len(coreMatches) > 0 {
			switch {
			case forceFreshGUID:
				return nil, fmt.Errorf("refused to enroll a fresh identity: the live terminal, pane, and bus name are already seated on guid %s; repair that identity with a pinned 'herder enroll', run 'herder reconcile --apply' to re-verify it, or use 'herder adopt %s' only for a true replacement", coreMatches[0].GUID, coreMatches[0].GUID)
			case requestedGUID != "":
				return nil, fmt.Errorf("refused to enroll unknown guid %s: the live terminal, pane, and bus name are already seated on guid %s; retry pinned to guid %s, run 'herder reconcile --apply' to re-verify it, or use 'herder adopt %s' for a true replacement", requestedGUID, coreMatches[0].GUID, coreMatches[0].GUID, coreMatches[0].GUID)
			default:
				selected, selectErr := selectMatchingLiveSeat(coreMatches, liveBus)
				if selectErr != nil {
					return nil, selectErr
				}
				latest = selected
				guid = selected.GUID
			}
		}
		if latest == nil && !liveBus.Verified {
			for _, occupied := range matchingPhysicalSeatRows(sessions, pane) {
				// Adoption has already authenticated and deliberately preserved its
				// source for the later atomic unseat-and-label-transfer batch. It is
				// not a competing mint target; every other occupied row still fences.
				if occupied.GUID == preserveGUID {
					continue
				}
				return nil, fmt.Errorf("refused to enroll an unverified bus identity: terminal %s and pane %s are already seated on guid %s; join hcom and retry, run 'herder reconcile --apply' to re-verify the row, or use 'herder adopt %s' for a true replacement", pane.TerminalID, pane.PaneID, occupied.GUID, occupied.GUID)
			}
		}
		if guid == "" {
			newGUID, guidErr := registry.NewGUID()
			if guidErr != nil {
				return nil, guidErr
			}
			guid = newGUID
		}
		short = registry.ShortGUID(guid)
		label = opts.label
		if label == "" && latest != nil {
			label = latest.Label
		}
		if label == "" {
			label = os.Getenv("HERDER_LABEL")
		}
		if label == "" {
			label = "manual-" + short
		}
		proofLabel := label
		if latest != nil && requestedGUID != "" {
			proofLabel = firstNonEmpty(opts.label, os.Getenv("HERDER_LABEL"), "manual-"+short)
		}
		role := opts.role
		if role == "" && latest != nil {
			role = latest.Role
		}
		if role == "" {
			role = firstNonEmpty(os.Getenv("HERDER_ROLE"), "manual")
		}
		if err := verifyExistingGUIDOwner(latest, pane, liveBus, proofLabel); err != nil {
			return nil, err
		}
		if owner := registry.V2LabelOwner(tx.Projection, label, guid); owner != nil {
			return nil, labelOwnerError(label, *owner)
		}

		mechanism := "enroll"
		agent := firstNonEmpty(envTool(), "manual")
		if latest != nil && latest.Provenance.Mechanism != "" {
			mechanism = latest.Provenance.Mechanism
		}
		if latest != nil {
			agent = firstNonEmpty(latest.Tool, agent)
		}
		prov := registry.BuildProvenance(mechanism, "", os.Getenv("HCOM_TAG"), pane.CWD, pane.WorkspaceID)
		if liveBus.Verified && liveBus.SessionID != "" {
			prov.ToolSessionID = liveBus.SessionID
		}
		if prov.ToolSessionID == "" && latest != nil {
			priorGUID := latest.GUID
			priorSID := latest.Provenance.ToolSessionID
			if len(latest.SIDs) > 0 {
				priorSID = latest.SIDs[len(latest.SIDs)-1].SID
			}
			priorProv := registry.Provenance{ToolSessionID: priorSID}
			recordsForPreservation := append([]registry.Record(nil), priorRecords...)
			recordsForPreservation = append(recordsForPreservation, registry.Record{GUID: &priorGUID, Provenance: &priorProv})
			prov = registry.PreserveToolSessionID(prov, recordsForPreservation, guid)
		}
		verified := liveBus.Verified
		rec := registry.Record{
			GUID:         &guid,
			ShortGUID:    &short,
			Label:        &label,
			Role:         role,
			Agent:        agent,
			PaneID:       pane.PaneID,
			TerminalID:   pane.TerminalID,
			HcomDir:      hcomDir,
			HcomName:     liveBus.Name,
			HcomVerified: &verified,
			HcomTag:      os.Getenv("HCOM_TAG"),
			Status:       "active",
			Provenance:   &prov,
		}
		next := registry.V2FromRecord(rec, "seated", v2.StateSeated, nowISO)
		next.Provenance.CWD = pane.CWD
		next.Provenance.WorkspaceID = pane.WorkspaceID
		if latest != nil && latest.Lineage != (v2.Lineage{}) {
			next.Lineage = latest.Lineage
		}

		// The repaired/original identity must become authoritative before any
		// duplicate or stale seat is detached. UpdateLocked applies candidates to
		// an evolving projection and commits the batch atomically in this order.
		rows := []v2.SessionRecord{next}
		detached := make(map[string]bool)
		if latest != nil {
			for _, duplicate := range coreMatches {
				if duplicate.GUID == guid {
					continue
				}
				if !seatSIDCompatible(duplicate, liveBus) {
					return nil, fmt.Errorf("refused to repair guid %s: another seated row %s has the same terminal, pane, and bus name but a conflicting session id; inspect 'herder list --all --json', run 'herder reconcile --apply', or use 'herder adopt <guid>' for the true replacement", guid, duplicate.GUID)
				}
				cleanup := duplicate
				cleanup.Event = "unseated"
				cleanup.State = v2.StateUnseated
				cleanup.RecordedAt = nowISO
				cleanup.Seat = nil
				cleanup.CloseResult = "duplicate_detached"
				cleanup.CloseReason = "source=enroll-repair; shared live seat retained by repaired guid " + guid
				rows = append(rows, cleanup)
				detached[duplicate.GUID] = true
				cleanupMessages = append(cleanupMessages, fmt.Sprintf("detached duplicate seat session %s (%s) after repairing %s", duplicate.Label, duplicate.GUID, guid))
			}
		}

		// A herdr pane hosts one live session at a time. After exact duplicates
		// are handled above, retain the older stale-pane cleanup for different bus
		// identities, guarded by terminal stability and live bus membership.
		for _, priorV2 := range sessions {
			prior, claimsSeat := staleSeatClaim(priorV2)
			if !claimsSeat || prior.PaneID != pane.PaneID || priorV2.GUID == guid || priorV2.GUID == preserveGUID || detached[priorV2.GUID] {
				continue
			}
			if !shouldRetirePriorRow(prior, pane.TerminalID, busJoined) {
				continue
			}
			cleanup := priorV2
			cleanup.Event = "unseated"
			cleanup.State = v2.StateUnseated
			cleanup.RecordedAt = nowISO
			cleanup.Seat = nil
			rows = append(rows, cleanup)
			cleanupMessages = append(cleanupMessages, fmt.Sprintf("unseated stale pane session %s (%s) superseded by re-enroll", priorV2.Label, priorV2.GUID))
		}
		return rows, nil
	})
	if err != nil {
		die(stderr, err.Error())
		return 1
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			die(stderr, err.Error())
			return 1
		}
	}
	if len(outcomes) > 0 {
		appendedRow = outcomes[0].Row
	}
	for _, message := range cleanupMessages {
		fmt.Fprintln(stderr, message)
	}
	fmt.Fprintf(stderr, "enrolled %s (%s) pane=%s terminal=%s\n", label, guid, pane.PaneID, pane.TerminalID)
	if opts.json {
		fmt.Fprintln(stdout, string(appendedRow))
	}
	observercmd.NudgeIfConfigured(stderr)
	return 0
}

func staleSeatClaim(rec v2.SessionRecord) (registry.Record, bool) {
	prior := registry.Record{}
	if rec.LegacyV1 {
		legacy, ok := registry.DecodeLegacyV1Raw(rec)
		if !ok || legacy.V1Status != "active" {
			return prior, false
		}
		prior.PaneID = legacy.PaneID
		prior.TerminalID = legacy.TerminalID
		prior.HcomName = legacy.HcomName
		prior.HcomDir = legacy.HcomDir
		return prior, true
	}
	if rec.State != v2.StateSeated || rec.Seat == nil {
		return prior, false
	}
	prior.PaneID = rec.Seat.PaneID
	prior.TerminalID = rec.Seat.TerminalID
	prior.HcomName = rec.Seat.HcomName
	prior.HcomDir = rec.Seat.Namespace
	return prior, true
}

func matchingLiveSeatRows(sessions []v2.SessionRecord, pane herdrcli.Pane, live hcomidentity.Result) []v2.SessionRecord {
	if !live.Verified || live.Name == "" || pane.TerminalID == "" || pane.PaneID == "" {
		return nil
	}
	var matches []v2.SessionRecord
	for _, session := range sessions {
		seat, claimsSeat := staleSeatClaim(session)
		if claimsSeat && seat.TerminalID == pane.TerminalID && seat.PaneID == pane.PaneID && seat.HcomName == live.Name {
			matches = append(matches, session)
		}
	}
	return matches
}

func matchingPhysicalSeatRows(sessions []v2.SessionRecord, pane herdrcli.Pane) []v2.SessionRecord {
	if pane.TerminalID == "" || pane.PaneID == "" {
		return nil
	}
	var matches []v2.SessionRecord
	for _, session := range sessions {
		seat, claimsSeat := staleSeatClaim(session)
		if claimsSeat && seat.TerminalID == pane.TerminalID && seat.PaneID == pane.PaneID {
			matches = append(matches, session)
		}
	}
	return matches
}

func latestRecordedSID(session v2.SessionRecord) string {
	if len(session.SIDs) > 0 {
		return session.SIDs[len(session.SIDs)-1].SID
	}
	return session.Provenance.ToolSessionID
}

func seatSIDCompatible(session v2.SessionRecord, live hcomidentity.Result) bool {
	recorded := latestRecordedSID(session)
	return recorded == "" || live.SessionID == "" || recorded == live.SessionID
}

func selectMatchingLiveSeat(matches []v2.SessionRecord, live hcomidentity.Result) (*v2.SessionRecord, error) {
	for _, match := range matches {
		if !seatSIDCompatible(match, live) {
			return nil, fmt.Errorf("refused to choose an occupied seat: guid %s has the same terminal, pane, and bus name but a conflicting session id; inspect 'herder list --all --json', pin the intended guid for re-enroll, run 'herder reconcile --apply', or use 'herder adopt <guid>' for a true replacement", match.GUID)
		}
	}
	if len(matches) == 1 {
		cp := matches[0]
		return &cp, nil
	}
	if live.SessionID != "" {
		var exact *v2.SessionRecord
		for _, match := range matches {
			if latestRecordedSID(match) != live.SessionID {
				continue
			}
			if exact != nil {
				exact = nil
				break
			}
			cp := match
			exact = &cp
		}
		if exact != nil {
			return exact, nil
		}
	}
	return nil, fmt.Errorf("refused to choose among %d seated rows with the same terminal, pane, and bus name; inspect 'herder list --all --json', pin the intended original guid for re-enroll, run 'herder reconcile --apply', or use 'herder adopt <guid>' for a true replacement", len(matches))
}

func labelOwnerError(label string, owner v2.SessionRecord) error {
	switch owner.State {
	case v2.StateUnseated:
		return fmt.Errorf("label %q is held by guid %s in state %s (dead/unseated); from the replacement pane run 'herder adopt %s', or run 'herder retire %s' then 'herder rename <target> %s'", label, owner.GUID, owner.State, owner.GUID, owner.GUID, label)
	case v2.StateLost:
		return fmt.Errorf("label %q is held by guid %s in state %s; LOST sessions cannot transfer or release labels automatically", label, owner.GUID, owner.State)
	default:
		return fmt.Errorf("label %q already belongs to seated session %s", label, owner.GUID)
	}
}

func verifyExistingGUIDOwner(current *v2.SessionRecord, pane herdrcli.Pane, live hcomidentity.Result, label string) error {
	if current == nil {
		return nil
	}
	switch current.State {
	case v2.StateRetired:
		return fmt.Errorf("refused to re-enroll %s: the existing identity is retired; run 'herder reopen %s' first, then retry the pinned re-enroll", current.GUID, current.GUID)
	case v2.StateLost:
		return fmt.Errorf("refused to re-enroll %s: the existing identity is lost and cannot be re-enrolled automatically; use the explicit lost-session recovery or adoption flow, then retry", current.GUID)
	}
	stored, claimsSeat := staleSeatClaim(*current)
	if !claimsSeat && current.State != v2.StateUnseated {
		return fmt.Errorf("refused to re-enroll %s: the existing identity has unsupported state %q and no seat evidence; repair the registry state explicitly, then retry", current.GUID, current.State)
	}
	if !live.Verified {
		return fmt.Errorf("refused to re-enroll %s: stored bus name %q cannot be corroborated because live bus identity proof is unavailable (%s); restore or join hcom and retry, run 'herder reconcile --apply' to re-verify the row, or use 'herder adopt %s' for a true replacement", current.GUID, stored.HcomName, live.Reason, current.GUID)
	}
	storedBusCaptured := stored.HcomName != "" && stored.HcomName != "null"
	explicitlyUnverified := storedBusExplicitlyUnverified(*current)
	if storedBusCaptured && !explicitlyUnverified && live.Name != stored.HcomName {
		return fmt.Errorf("refused to re-enroll %s: calling live bus @%s does not match stored bus name @%s; restore or join @%s and retry, run 'herder reconcile --apply' if the durable binding is stale, or use 'herder adopt %s' for a true replacement", current.GUID, live.Name, stored.HcomName, stored.HcomName, current.GUID)
	}

	recordedSID := current.Provenance.ToolSessionID
	if len(current.SIDs) > 0 {
		recordedSID = current.SIDs[len(current.SIDs)-1].SID
	}
	if recordedSID != "" && recordedSID == live.SessionID {
		return nil
	}

	terminalMatches := stored.TerminalID != "" && stored.TerminalID == pane.TerminalID
	labelMatches := current.Label != "" && current.Label == label
	if terminalMatches && labelMatches {
		return nil
	}

	sidCause := "no recorded/live session id match"
	if recordedSID != "" {
		sidCause = fmt.Sprintf("calling live session %q does not match recorded session %q", live.SessionID, recordedSID)
	}
	seatCause := ""
	if !terminalMatches {
		seatCause = fmt.Sprintf("live terminal %q does not match recorded terminal %q", pane.TerminalID, stored.TerminalID)
	}
	if !labelMatches {
		if seatCause != "" {
			seatCause += "; "
		}
		seatCause += fmt.Sprintf("requested label %q does not match recorded label %q", label, current.Label)
	}
	if !storedBusCaptured || explicitlyUnverified {
		busCause := "the existing seat has no stored bus name"
		if explicitlyUnverified {
			busCause = fmt.Sprintf("stored bus name @%s is explicitly unverified", stored.HcomName)
		}
		return fmt.Errorf("refused to re-enroll %s: %s and bootstrap ownership proof failed: %s; %s; restore the recorded session id or both terminal and label, run 'herder reconcile --apply' to re-verify the row, or use 'herder adopt %s' for a true replacement, then retry (bare enroll will not mint a replacement)", current.GUID, busCause, sidCause, seatCause, current.GUID)
	}
	return fmt.Errorf("refused to re-enroll %s: %s, and full seat corroboration failed (%s); restore terminal and label while joined as @%s, run 'herder reconcile --apply' to re-verify the row, or use 'herder adopt %s' for a true replacement, then retry (bare enroll will not mint a replacement)", current.GUID, sidCause, seatCause, stored.HcomName, current.GUID)
}

func storedBusExplicitlyUnverified(current v2.SessionRecord) bool {
	if current.Seat != nil {
		return current.Seat.HcomVerified != nil && !*current.Seat.HcomVerified
	}
	if current.LegacyV1 {
		legacy, ok := registry.DecodeLegacyV1Raw(current)
		return ok && legacy.HcomVerified != nil && !*legacy.HcomVerified
	}
	return false
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	var opts options
	for i := 0; i < len(args); {
		switch args[i] {
		case "--label":
			if i+1 >= len(args) {
				die(stderr, "--label requires a value")
				return opts, 1
			}
			opts.label = args[i+1]
			i += 2
		case "--role":
			if i+1 >= len(args) {
				die(stderr, "--role requires a value")
				return opts, 1
			}
			opts.role = args[i+1]
			i += 2
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
	fmt.Fprint(stdout, `herder enroll — register the CURRENT herdr pane in the herder registry.

Run from inside a herdr pane to make the running agent (or shell) addressable by
herder send/wait/list/cull. Identity comes from HERDER_GUID/HERDER_LABEL/HERDER_ROLE
if set, else a fresh guid and a "manual-<short>" label are generated.

Usage:
  herder enroll [--label LABEL] [--role ROLE] [--json]

Options:
  --label LABEL   label to record (repair: stored; empty/fresh: $HERDER_LABEL,
                  else manual-<short>)
  --role ROLE     role to record (repair: stored; empty/fresh: $HERDER_ROLE,
                  else "manual")
  --json          print the appended registry record as JSON on stdout

Records pane_id, terminal_id, workspace_id, cwd, and live-verified hcom
coordinates so later
resolution survives pane move re-keying within a server run. After restart,
recorded terminal_id is dead until reconcile or re-enroll. A herdr pane hosts one live
session at a time, so re-enrolling a reused pane UNSEATS any prior seated
sessions still claiming that pane_id — a dead session never lingers as
LIVE=working. Must run inside a herdr pane (HERDR_ENV=1 and HERDR_PANE_ID set);
refuses otherwise. The launch-time HCOM_INSTANCE_NAME is never trusted. If the
current bus row cannot be proven from session/process/pane identity, hcom_name is
recorded as unknown. Rerun herder enroll from the existing session to recapture
and repair its bus binding. Reusing an existing guid requires either an exact
recorded/live session id match, or both unchanged terminal and caller-claimed
label. On a pinned repair, that proof label comes from --label, HERDER_LABEL, or
manual-<short>; the stored label is never substituted as ownership proof. The
repair write preserves non-empty stored label and role; only explicit
--label/--role flags replace them. HERDER_LABEL/HERDER_ROLE backstop an empty
stored field on repair and remain the defaults for fresh enrolls.
A verified stored bus name must also equal the caller's verified live name. If an
older seat has no stored bus name, or explicitly records hcom_verified=false, a
verified caller may bootstrap it with the same session-or-seat proof. The repair
captures its live name and session id, so later re-enrolls use the strict
stored-name rule; an absent verification field remains strict. Before minting,
bare enroll checks for an existing row with the same terminal, pane, and live bus
name and repairs that guid or refuses instead of duplicating the seat. A pinned
repair detaches exact duplicate rows only after it appends the repaired original,
without closing the shared pane. An inherited guid that belongs to another
session is refused. Refusals point to reconcile re-verification or explicit adopt
when those are the real recovery paths; bare enroll never mints a replacement as
a refusal remedy.
`)
}

// shouldRetirePriorRow decides whether a prior seated session that shares this
// pane_id is a stale identity to unseat on re-enroll (TASK-035 AC#1). pane_id
// alone is NOT enough: pane ids can re-key on moves and all ids reshuffle
// after restart, so a still-live session may no longer be at its recorded
// pane_id — unseating that session would corrupt a LIVE session (review P1-b). It
// refuses to unseat a session that is plausibly a different, live session:
//   - terminal_id is the move-stable coordinate within a herdr server run;
//     when both rows carry one and they DIFFER, the prior row is another
//     session merely sharing the recorded pane_id — leave it.
//   - a row whose bus name is currently JOINED is by definition live, never
//     stale. The probe is protective ONLY: an unavailable bus returns false so
//     it can never FORCE an unseat, only prevent one.
func shouldRetirePriorRow(prior registry.Record, paneTerminalID string, joined func(name, dir string) bool) bool {
	if prior.TerminalID != "" && paneTerminalID != "" && prior.TerminalID != paneTerminalID {
		return false
	}
	if prior.HcomName != "" && prior.HcomName != "null" && joined != nil && joined(prior.HcomName, prior.HcomDir) {
		return false
	}
	return true
}

// busJoined reports whether name is currently joined on the bus at dir, via the
// same `hcom list <name>` probe send/spawn use (exit 0 ⇒ joined). Best-effort:
// a missing/erroring hcom yields false, so liveness can only protect a row from
// retirement, never trigger one.
func busJoined(name, dir string) bool {
	if name == "" {
		return false
	}
	cmd := exec.Command("hcom", "list", name)
	cmd.Env = os.Environ()
	if dir != "" && dir != "null" {
		cmd.Env = append(cmd.Env, "HCOM_DIR="+dir)
	}
	return cmd.Run() == nil
}

func envTool() string {
	if v := os.Getenv("HERDER_AGENT"); v != "" {
		return v
	}
	if v := os.Getenv("HCOM_TOOL"); v != "" {
		return v
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder enroll: %s\n", msg)
}
