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

	guid := os.Getenv("HERDER_GUID")
	if forceFreshGUID {
		guid = ""
	}
	if guid == "" {
		var err error
		guid, err = registry.NewGUID()
		if err != nil {
			die(stderr, err.Error())
			return 1
		}
	}
	short := registry.ShortGUID(guid)
	label := firstNonEmpty(opts.label, os.Getenv("HERDER_LABEL"))
	if label == "" {
		label = "manual-" + short
	}
	role := firstNonEmpty(opts.role, os.Getenv("HERDER_ROLE"), "manual")

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

	// Unseat prior identities bound to this same pane. A herdr pane hosts
	// exactly one live session at a time, but pane ids are display-only and
	// can re-key on moves or reshuffle after restart — so any OTHER seated session
	// still claiming this pane_id is a stale identity from an earlier session.
	// Left seated its bus name lingers as a forever-'working' row, and pane-id
	// send resolution could pick it over the live one (TASK-035). Mark each
	// unseated before appending this session's row.
	nowISO := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	outcomes, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		var latest *v2.SessionRecord
		for _, rec := range tx.Projection.Sessions() {
			if rec.GUID == guid {
				cp := rec
				latest = &cp
				break
			}
		}
		if err := verifyExistingGUIDOwner(latest, pane, liveBus, label); err != nil {
			return nil, err
		}
		if owner := registry.V2LabelOwner(tx.Projection, label, guid); owner != nil {
			return nil, labelOwnerError(label, *owner)
		}
		var rows []v2.SessionRecord
		for _, priorV2 := range tx.Projection.Sessions() {
			prior, claimsSeat := staleSeatClaim(priorV2)
			if !claimsSeat || prior.PaneID != pane.PaneID || priorV2.GUID == guid || priorV2.GUID == preserveGUID {
				continue
			}
			if !shouldRetirePriorRow(prior, pane.TerminalID, busJoined) {
				continue
			}
			next := priorV2
			next.Event = "unseated"
			next.State = v2.StateUnseated
			next.RecordedAt = nowISO
			next.Seat = nil
			rows = append(rows, next)
			fmt.Fprintf(stderr, "unseated stale pane session %s (%s) superseded by re-enroll\n", priorV2.Label, priorV2.GUID)
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
		rows = append(rows, next)
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
		appendedRow = outcomes[len(outcomes)-1].Row
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
		return fmt.Errorf("refused to re-enroll %s: stored bus name %q cannot be corroborated because live bus identity proof is unavailable (%s); restore or join that existing bus identity, then retry the same pinned re-enroll", current.GUID, stored.HcomName, live.Reason)
	}
	storedBusCaptured := stored.HcomName != "" && stored.HcomName != "null"
	if storedBusCaptured && live.Name != stored.HcomName {
		return fmt.Errorf("refused to re-enroll %s: calling live bus @%s does not match stored bus name @%s; restore or join @%s from the existing session, then retry the same pinned re-enroll", current.GUID, live.Name, stored.HcomName, stored.HcomName)
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
	if !storedBusCaptured {
		return fmt.Errorf("refused to re-enroll %s: the existing seat has no stored bus name and bootstrap ownership proof failed: %s; %s; restore either the recorded session id or both the recorded terminal and label, then retry from a verified live bus row (bare enroll will not mint a replacement)", current.GUID, sidCause, seatCause)
	}
	return fmt.Errorf("refused to re-enroll %s: %s, and full seat corroboration failed (%s); restore the recorded terminal and label while joined as @%s, then retry the same pinned re-enroll (bare enroll will not mint a replacement)", current.GUID, sidCause, seatCause, stored.HcomName)
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
  --label LABEL   label to record (default: $HERDER_LABEL, else manual-<short>)
  --role ROLE     role to record (default: $HERDER_ROLE, else "manual")
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
and repair its bus binding. Reusing an existing guid always requires the caller's
verified live bus name to equal the stored bus name. With that proof, ownership
requires either an exact recorded/live session id match, or both unchanged
terminal and unchanged label. If an older or adopted seated row has no stored bus
name, a verified live caller may bootstrap it with the same session-or-seat proof;
the successful repair captures its live name and session id, so later re-enrolls
use the strict stored-name rule. An inherited guid that belongs to another
session is refused; bare enroll never mints a replacement as a refusal remedy.
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
