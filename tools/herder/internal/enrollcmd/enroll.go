package enrollcmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

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

	guid := os.Getenv("HERDER_GUID")
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

	// Unseat prior identities bound to this same pane. A herdr pane hosts
	// exactly one live session at a time, but pane ids are display-only and
	// can re-key on moves or reshuffle after restart — so any OTHER active row
	// still claiming this pane_id is a stale identity from an earlier session.
	// Left active its bus name lingers as a forever-'working' row, and pane-id
	// send resolution could pick it over the live one (TASK-035). Mark each
	// closed before appending this session's row.
	nowISO := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	encoded, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		var latest *v2.SessionRecord
		for _, rec := range tx.Projection.Sessions() {
			if rec.GUID == guid {
				cp := rec
				latest = &cp
				break
			}
		}
		if owner := registry.V2LabelOwner(tx.Projection, label, guid); owner != nil {
			return nil, fmt.Errorf("label %q already belongs to active guid %s", label, owner.GUID)
		}
		var rows []v2.SessionRecord
		for _, priorV2 := range tx.Projection.Sessions() {
			prior := registry.LegacyFromV2(priorV2)
			if prior.Status != "active" || prior.PaneID != pane.PaneID || ptrString(prior.GUID) == guid {
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
			fmt.Fprintf(stderr, "retired stale pane row %s (%s) superseded by re-enroll\n", ptrString(prior.Label), ptrString(prior.GUID))
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
		rec := registry.Record{
			GUID:       &guid,
			ShortGUID:  &short,
			Label:      &label,
			Role:       role,
			Agent:      agent,
			PaneID:     pane.PaneID,
			TerminalID: pane.TerminalID,
			HcomDir:    os.Getenv("HCOM_DIR"),
			HcomName:   os.Getenv("HCOM_INSTANCE_NAME"),
			HcomTag:    os.Getenv("HCOM_TAG"),
			Status:     "active",
			Provenance: &prov,
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
	if len(encoded) > 0 {
		appendedRow = encoded[len(encoded)-1]
	}
	fmt.Fprintf(stderr, "enrolled %s (%s) pane=%s terminal=%s\n", label, guid, pane.PaneID, pane.TerminalID)
	if opts.json {
		fmt.Fprintln(stdout, string(appendedRow))
	}
	observercmd.NudgeIfConfigured(stderr)
	return 0
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

Records pane_id, terminal_id, workspace_id, cwd, and hcom coordinates so later
resolution survives pane move re-keying within a server run. After restart,
recorded terminal_id is dead until reconcile or re-enroll. A herdr pane hosts one live
session at a time, so re-enrolling a reused pane RETIRES (closes) any prior
active rows still claiming that pane_id — a dead session's row never lingers as
LIVE=working. Must run inside a herdr pane (HERDR_ENV=1 and HERDR_PANE_ID set);
refuses otherwise.
`)
}

// shouldRetirePriorRow decides whether a prior active row that shares this
// pane_id is a stale identity to close on re-enroll (TASK-035 AC#1). pane_id
// alone is NOT enough: pane ids can re-key on moves and all ids reshuffle
// after restart, so a still-live session may no longer be at its recorded
// pane_id — closing that row would corrupt a LIVE session (review P1-b). It
// refuses to close a row that is plausibly a different, live session:
//   - terminal_id is the move-stable coordinate within a herdr server run;
//     when both rows carry one and they DIFFER, the prior row is another
//     session merely sharing the recorded pane_id — leave it.
//   - a row whose bus name is currently JOINED is by definition live, never
//     stale. The probe is protective ONLY: an unavailable bus returns false so
//     it can never FORCE a close, only prevent one.
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
