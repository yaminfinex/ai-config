package enrollcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
	"unicode"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/observercmd"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
	"ai-config/tools/herder/internal/seatcred"
)

type options struct {
	help      bool
	json      bool
	label     string
	role      string
	sessionID string
	hcomName  string
}

func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, false)
}

// RunFreshForAdoption enrolls a replacement with a new guid and a temporary
// label. Adopt's take leg transfers the source label atomically afterward.
func RunFreshForAdoption(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, true)
}

func run(args []string, stdout, stderr io.Writer, forceFreshGUID bool) int {
	return runWithEngine(args, stdout, stderr, forceFreshGUID, seatcompletion.DefaultEngine())
}

func runWithEngine(args []string, stdout, stderr io.Writer, forceFreshGUID bool, engine seatcompletion.Engine) int {
	credentialPath, args, credentialFlagErr := seatcred.ExtractFlag(args)
	if credentialFlagErr != nil {
		die(stderr, credentialFlagErr.Error())
		return 2
	}
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
	// Long-lived and resumed sessions keep HCOM_SESSION_ID/HCOM_PROCESS_ID frozen
	// at the dead launch epoch, so env-only evidence cannot corroborate the live
	// bus row. --session-id/--hcom-name let the operator supply the live truth;
	// the frozen ambient hints are dropped and the explicit values are proven
	// against the live roster by the same Resolve engine (they are evidence, not
	// an override of it). A stale or wrong assertion still fails closed below.
	evidence := hcomidentity.CurrentEvidence(paneID, pane.PaneID)
	explicitEvidence := opts.sessionID != "" || opts.hcomName != ""
	if explicitEvidence {
		evidence.SessionID = ""
		evidence.ProcessID = ""
	}
	if opts.sessionID != "" {
		evidence.SessionID = opts.sessionID
	}
	if opts.hcomName != "" {
		evidence.Name = opts.hcomName
	}
	liveBus := hcomidentity.ResolveLive(hcomDir, evidence)
	if explicitEvidence && !liveBus.Verified {
		die(stderr, fmt.Sprintf("explicit evidence did not corroborate one joined bus row (%s); run `hcom list --json` in this namespace and pass the exact joined --hcom-name and/or --session-id", liveBus.Reason))
		return 1
	}
	if !liveBus.Verified {
		fmt.Fprintf(stderr, "herder enroll: live bus identity could not be verified (%s); recording hcom_name as unknown. Join this session to hcom before enrolling when a verified bus binding is required.\n", liveBus.Reason)
	}

	registryPath := registry.DefaultPath()
	cutover, cutoverErr := seatcred.CutoverEnabled(registryPath)
	if cutoverErr != nil {
		die(stderr, cutoverErr.Error())
		return 2
	}
	var selected *seatcred.Selection
	if credentialPath != "" {
		selection, authErr := seatcred.Authenticate(registryPath, credentialPath)
		if authErr != nil {
			die(stderr, "caller credential refused: "+authErr.Error())
			return 2
		}
		selected = &selection
		if selection.Row.Seat == nil || selection.Row.Seat.TerminalID != pane.TerminalID {
			die(stderr, "ambient pane does not verify the credential-selected seat; refusing without re-selection")
			return 2
		}
		if rows, listErr := hcomidentity.List(hcomDir); listErr != nil {
			die(stderr, "credential-selected bus roster unavailable: "+listErr.Error())
			return 2
		} else if verifyErr := seatcred.VerifySelectedBus(rows, selection, evidence); verifyErr != nil {
			die(stderr, verifyErr.Error())
			return 2
		}
	}
	requestedGUID := ""
	if selected != nil {
		requestedGUID = selected.GUID
	} else if !cutover {
		requestedGUID = os.Getenv("HERDER_GUID")
	}
	guid := requestedGUID
	if forceFreshGUID {
		guid = ""
	}
	short := ""
	label := ""

	var appendedRow []byte
	nowISO := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	credentialGUID := guid
	if credentialGUID == "" {
		credentialGUID, err = registry.NewGUID()
		if err != nil {
			die(stderr, err.Error())
			return 1
		}
		guid = credentialGUID
	}
	buildRows := func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		sessions := tx.Projection.Sessions()
		var latest *v2.SessionRecord
		for _, rec := range sessions {
			if guid != "" && rec.GUID == guid {
				cp := rec
				latest = &cp
				break
			}
		}

		if latest != nil {
			return nil, fmt.Errorf("refused to re-enroll existing guid %s: identity repair is no longer an enroll operation; run 'herder adopt %s' from the replacement session", latest.GUID, latest.GUID)
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
		if label == "" && !forceFreshGUID {
			label = os.Getenv("HERDER_LABEL")
		}
		if label == "" {
			label = "manual-" + short
		}
		role := opts.role
		if role == "" {
			role = firstNonEmpty(os.Getenv("HERDER_ROLE"), "manual")
		}
		if owner := registry.V2LabelOwner(tx.Projection, label, guid); owner != nil {
			return nil, labelOwnerError(label, *owner)
		}

		mechanism := "enroll"
		agent := firstNonEmpty(envTool(), "manual")
		verifiedSessionID := ""
		if liveBus.Verified && liveBus.SessionID != "" {
			verifiedSessionID = liveBus.SessionID
		}
		provenanceSpawner := ""
		if cutover {
			provenanceSpawner = "user"
			if selected != nil {
				provenanceSpawner = selected.GUID
			}
		}
		prov := registry.BuildProvenance(mechanism, provenanceSpawner, verifiedSessionID, os.Getenv("HCOM_TAG"), pane.CWD, pane.WorkspaceID)
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
		return []v2.SessionRecord{next}, nil
	}
	result, err := engine.Complete(context.Background(), seatcompletion.Request{
		Origin:         seatcompletion.OriginEnroll,
		RegistryPath:   registryPath,
		CredentialGUID: credentialGUID,
		Candidate:      v2.SessionRecord{Tool: firstNonEmpty(envTool(), "manual")},
		Seat: seatcompletion.SeatClaim{
			Kind:       seatcompletion.SeatHerdr,
			PaneID:     pane.PaneID,
			TerminalID: pane.TerminalID,
		},
		ObservedPane: &seatcompletion.LivePane{PaneID: pane.PaneID, TerminalID: pane.TerminalID},
		ObservedBus:  &liveBus,
		Namespace:    hcomDir,
		Evidence:     hcomidentity.CurrentEvidence(paneID, pane.PaneID),
		RequireBus:   liveBus.Verified,
		BuildLocked: func(tx registry.LockedUpdate, _ v2.Seat) (v2.SessionRecord, []v2.SessionRecord, []v2.SessionRecord, error) {
			rows, buildErr := buildRows(tx)
			if buildErr != nil {
				return v2.SessionRecord{}, nil, nil, buildErr
			}
			if len(rows) == 0 {
				return v2.SessionRecord{}, nil, nil, fmt.Errorf("enroll produced no completed candidate")
			}
			return rows[0], nil, rows[1:], nil
		},
	})
	if err != nil {
		die(stderr, err.Error())
		return 1
	}
	if result.Refusal != nil {
		die(stderr, fmt.Sprintf("seat completion refused [%s]: %s", result.Refusal.Code, result.Refusal.Cause))
		return 1
	}
	for _, outcome := range result.Outcomes {
		if err := outcome.Err(); err != nil {
			die(stderr, err.Error())
			return 1
		}
	}
	appendedRow = result.Row
	fmt.Fprintf(stderr, "enrolled %s (%s) pane=%s terminal=%s\n", label, guid, pane.PaneID, pane.TerminalID)
	fmt.Fprintf(stderr, "credential generation=%s path=%s\n", result.CredentialGeneration, result.CredentialPath)
	if opts.json {
		fmt.Fprintln(stdout, string(appendedRow))
	}
	observercmd.NudgeIfConfigured(stderr)
	return 0
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
		case "--session-id":
			if i+1 >= len(args) {
				die(stderr, "--session-id requires a value")
				return opts, 1
			}
			if !validEvidenceToken(args[i+1]) {
				die(stderr, "--session-id must be one nonempty token without whitespace or control characters")
				return opts, 1
			}
			opts.sessionID = args[i+1]
			i += 2
		case "--hcom-name":
			if i+1 >= len(args) {
				die(stderr, "--hcom-name requires a value")
				return opts, 1
			}
			if !validEvidenceToken(args[i+1]) {
				die(stderr, "--hcom-name must be one nonempty token without whitespace or control characters")
				return opts, 1
			}
			opts.hcomName = args[i+1]
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
herder send/wait/list/cull. Enroll creates a fresh identity; it does not repair
or re-key an existing guid. Ambient HERDER_*/HCOM_*/HERDR_* values are hints.

Usage:
  herder enroll [--credential-file PATH] [--label LABEL] [--role ROLE]
                [--session-id ID] [--hcom-name NAME] [--json]

Options:
  --credential-file PATH
                  select the caller identity during credential cutover
  --label LABEL   label to record ($HERDER_LABEL, else manual-<short>)
  --role ROLE     role to record ($HERDER_ROLE, else "manual")
  --session-id ID explicit live tool-session id (HCOM_SESSION_ID) to corroborate
                  when the ambient launch env has gone stale (resumed/long-lived
                  session). Proven against the live roster, not trusted blindly.
  --hcom-name NAME explicit live bus name to corroborate for the same reason.
  --json          print the appended registry record as JSON on stdout

Records spawn-time pane, terminal, workspace, cwd, and live-verified hcom
coordinates as provenance. Later identity resolution probes the live occupant;
it never treats those recorded coordinates as proof. Must run inside a herdr
pane (HERDR_ENV=1 and HERDR_PANE_ID set); refuses otherwise. The launch-time
HCOM_INSTANCE_NAME is never trusted. If the current bus row cannot be proven
from session/process/pane identity, hcom_name is recorded as unknown. On a
resumed or long-lived session the frozen
HCOM_SESSION_ID/HCOM_PROCESS_ID env belong to a dead launch epoch and can no
longer prove the live row; pass --session-id and/or --hcom-name from 'hcom list
--json' to supply the live truth. Those values are corroborated against the live
roster exactly like ambient evidence — a wrong or stale assertion fails closed.
An inherited or credential-selected guid that already exists is refused without
mutation; use adopt from the replacement session. Label ownership remains unique,
and labels move only through adopt or rename's explicit transfer operation.
`)
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

func validEvidenceToken(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
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
