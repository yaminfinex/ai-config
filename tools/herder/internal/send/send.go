package send

// herder send is BUS-ONLY (TASK-003, locked): the hcom bus is THE delivery
// transport. Every target form resolves through the spawn registry to a
// recorded bus name; a target that cannot resolve to a bus-bound agent is a
// clear hard error (exit 2) — keystrokes are never typed. The old herdr
// keystroke transport survives in exactly one deliberate place: spawn's
// boot-time initial-prompt paste (internal/spawncmd/bootpaste.go), which is
// spawn-private and not reachable from here.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/seatcred"
)

type hcomDryRunRecord struct {
	Target    string `json:"target"`
	Transport string `json:"transport"`
	HcomName  string `json:"hcom_name"`
	HcomDir   string `json:"hcom_dir"`
	DryRun    bool   `json:"dry_run"`
}

type hcomDryRunRefuseRecord struct {
	Target    string `json:"target"`
	Transport string `json:"transport"`
	Would     string `json:"would"`
	DryRun    bool   `json:"dry_run"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	credentialPath, args, credentialFlagErr := seatcred.ExtractFlag(args)
	if credentialFlagErr != nil {
		die(stderr, credentialFlagErr.Error())
		return 64
	}
	if os.Getenv("HERDR_ENV") != "1" {
		die(stderr, "not running inside a herdr pane (HERDR_ENV != 1)")
		return 64
	}

	opts, target, message, code := parseArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.Help {
		return 0
	}
	registryPath := registry.DefaultPath()
	var selected *seatcred.Selection
	if seatcred.CutoverEnabled(registryPath) || credentialPath != "" {
		selection, err := seatcred.Authenticate(registryPath, credentialPath)
		if err != nil {
			die(stderr, "caller credential refused: "+err.Error())
			return 2
		}
		selected = &selection
	}

	forced := false
	switch os.Getenv("HERDER_BUS") {
	case "herdr":
		die(stderr, "HERDER_BUS=herdr is gone — the herdr keystroke transport was removed; hcom is the only transport (unset HERDER_BUS, or use auto|hcom)")
		return 64
	case "hcom":
		forced = true
	}

	recs, err := registry.Load(registryPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		die(stderr, "registry not readable: "+err.Error())
		return 1
	}
	sender := &busSender{}

	rec := registry.Resolve(recs, target)
	var dormantCoordinate *registry.Record
	if rec == nil {
		// Pane targets are display coordinates and terminal targets are
		// run-scoped coordinates, so a coordinate match can still be ambiguous
		// in the registry (for example, stale manual-enroll identities from a
		// prior session on the same pane). A single candidate resolves exactly
		// as before — bus-less bash rows and not-joined-yet claude/codex rows
		// flow through to the deliver path unchanged. Only when >1 candidate
		// matches is bus liveness a tiebreaker: deliver to the sole joined row,
		// and refuse loudly with the candidate list when 0 or >1 are live
		// (never guess).
		candidates := registry.SeatedCandidatesByPaneOrTerminal(recs, target)
		switch len(candidates) {
		case 0:
			dormantCoordinate = registry.UnseatedByPaneOrTerminal(recs, target)
		case 1:
			rec = &candidates[0]
		default:
			chosen, code := disambiguatePane(sender, candidates, target, stderr)
			if chosen == nil {
				return code
			}
			rec = chosen
		}
	}

	if rec == nil {
		if dormantCoordinate != nil {
			addressKind := "label"
			address := ptrString(dormantCoordinate.Label)
			if address == "" {
				addressKind = "guid"
				address = ptrString(dormantCoordinate.GUID)
			}
			if opts.DryRun {
				fmt.Fprintf(stderr, "herder send --dry-run: would REFUSE (exit 2): coordinate %q matches an unseated registry row that holds no seat; pane/terminal resolution only reaches seated rows. Send by %s %q instead, or retry the coordinate after the session is re-recognized or enrolled.\n", target, addressKind, address)
				if opts.JSONOutput {
					writeCompactJSON(stdout, hcomDryRunRefuseRecord{Target: target, Transport: "hcom", Would: "refuse", DryRun: true})
				}
				return 2
			}
			fmt.Fprintf(stderr, "herder send: refused — coordinate %q matches an unseated registry row that holds no seat; pane/terminal resolution only reaches seated rows. Send by %s %q instead, or retry the coordinate after the session is re-recognized or enrolled. Nothing was typed or sent.\n", target, addressKind, address)
			return 2
		}
		if forced {
			// Forced-hcom debug affordance (unchanged from the driver era): an
			// unregistered target is a literal bus name on the ambient HCOM_DIR.
			if opts.DryRun {
				fmt.Fprintf(stderr, "herder send --dry-run: %s -> hcom bus @%s (HCOM_DIR=%s)\n", target, target, ambientHcomDir())
				if opts.JSONOutput {
					writeCompactJSON(stdout, hcomDryRunRecord{
						Target:    target,
						Transport: "hcom",
						HcomName:  target,
						HcomDir:   ambientHcomDir(),
						DryRun:    true,
					})
				}
				return 0
			}
			senderName, err := callerSender(recs, selected, "")
			if err != nil {
				writeSenderRefusal(stderr, err)
				return 2
			}
			return sender.send(senderName, target, target, "", message, opts.TimeoutMS, opts.JSONOutput, stdout, stderr)
		}
		if opts.DryRun {
			fmt.Fprintf(stderr, "herder send --dry-run: would REFUSE (exit 2): no registry row matches %s — bus-only (keystroke transport removed)\n", target)
			if opts.JSONOutput {
				writeCompactJSON(stdout, hcomDryRunRefuseRecord{Target: target, Transport: "hcom", Would: "refuse", DryRun: true})
			}
			return 2
		}
		fmt.Fprintf(stderr, "herder send: refused — no registry row matches '%s' (tried guid, short-guid, label, terminal_id, pane_id); herder send is bus-only, the keystroke transport was removed. Nothing was typed or sent.\n", target)
		return 2
	}

	if rec.HcomName == "" || rec.HcomName == "null" {
		if opts.DryRun {
			fmt.Fprintf(stderr, "herder send --dry-run: would REFUSE (exit 2): %s has no recorded bus name — not bus-bound\n", target)
			if opts.JSONOutput {
				writeCompactJSON(stdout, hcomDryRunRefuseRecord{Target: target, Transport: "hcom", Would: "refuse", DryRun: true})
			}
			return 2
		}
		fmt.Fprintf(stderr, "herder send: refused — %s has no recorded bus name (not launched through hcom); herder send is bus-only, the keystroke transport was removed. Nothing was typed or sent.\n", target)
		return 2
	}

	if opts.DryRun {
		dir := rec.HcomDir
		if dir == "" || dir == "null" {
			dir = ambientHcomDir()
		}
		fmt.Fprintf(stderr, "herder send --dry-run: %s -> hcom bus @%s (HCOM_DIR=%s)\n", target, rec.HcomName, dir)
		if opts.JSONOutput {
			writeCompactJSON(stdout, hcomDryRunRecord{
				Target:    target,
				Transport: "hcom",
				HcomName:  rec.HcomName,
				HcomDir:   rec.HcomDir,
				DryRun:    true,
			})
		}
		return 0
	}

	senderName, err := callerSender(recs, selected, rec.HcomDir)
	if err != nil {
		writeSenderRefusal(stderr, err)
		return 2
	}
	return sender.sendPending(registryPath, ptrString(rec.GUID), senderName, target, rec.HcomName, rec.HcomDir, message, opts.TimeoutMS, opts.JSONOutput, stdout, stderr)
}

func callerSender(recs []registry.Record, selected *seatcred.Selection, busDir string) (string, error) {
	if selected != nil {
		return credentialCallerSender(*selected, busDir)
	}
	return verifiedCallerSender(recs, busDir)
}

func credentialCallerSender(selected seatcred.Selection, busDir string) (string, error) {
	if selected.Row.Seat == nil || selected.Row.Seat.HcomName == "" {
		return "", &SenderIdentityRefusal{Cause: "credential-selected caller has no recorded bus name", Remedy: "Run `herder enroll --credential-file PATH` after joining hcom, then retry"}
	}
	rows, err := hcomidentity.List(busDir)
	if err != nil {
		return "", &SenderIdentityRefusal{Cause: "live bus roster unavailable: " + err.Error(), Remedy: "Restore access to the selected seat's hcom roster, then retry"}
	}
	paneIDs, _ := currentCallerCoordinates()
	if err := seatcred.VerifySelectedBus(rows, selected, hcomidentity.CurrentEvidence(paneIDs...)); err != nil {
		return "", &SenderIdentityRefusal{Cause: err.Error(), Remedy: "Use the credential belonging to this live caller, or scrub stale HCOM_*/HERDER_*/HERDR_* correlates"}
	}
	if _, count := hcomidentity.JoinedNamedCount(rows, selected.Row.Seat.HcomName); count != 1 {
		return "", &SenderIdentityRefusal{Cause: fmt.Sprintf("credential-selected bus name @%s resolves to %d joined rows", selected.Row.Seat.HcomName, count), Remedy: "Restore exactly one joined row for the credential-selected seat, then retry"}
	}
	return selected.Row.Seat.HcomName, nil
}

func verifiedCallerSender(recs []registry.Record, busDir string) (string, error) {
	paneIDs, keys := currentCallerCoordinates()
	evidence := hcomidentity.CurrentEvidence(paneIDs...)
	liveName, err := ResolveLiveSender(busDir, evidence)
	if err != nil {
		return "", err
	}

	if guid := os.Getenv("HERDER_GUID"); guid != "" {
		row := registry.Resolve(recs, guid)
		if row == nil {
			return "", &SenderIdentityRefusal{
				Cause:  "HERDER_GUID does not resolve to a registry row for the calling session",
				Remedy: "Run `herder enroll` from this session to restore its registry binding, then retry",
			}
		}
		return requireStoredSender(row, liveName)
	}
	if sid := os.Getenv("HCOM_SESSION_ID"); sid != "" {
		if row := registry.ResolveByToolSessionID(recs, sid); row != nil {
			// A stale session correlate may still point at an unseated predecessor.
			// Let current pane/terminal evidence recover the seated caller; a
			// conflicting seated row remains a hard refusal below.
			if registry.IsSeated(*row) {
				return requireStoredSender(row, liveName)
			}
		}
	}

	var matched *registry.Record
	for _, key := range keys {
		for _, candidate := range registry.SeatedCandidatesByPaneOrTerminal(recs, key) {
			if candidate.HcomName != liveName {
				continue
			}
			if matched != nil && !sameRecordIdentity(matched, &candidate) {
				return "", &SenderIdentityRefusal{
					Cause:  "multiple seated registry rows match the caller's live bus identity and pane/terminal evidence",
					Remedy: "Reconcile or re-enroll this session so one seated row owns the live bus identity, then retry",
				}
			}
			copy := candidate
			matched = &copy
		}
	}
	if matched == nil {
		return "", &SenderIdentityRefusal{
			Cause:  fmt.Sprintf("live caller evidence proves @%s, but no seated registry row with that bus name matches the caller's pane or terminal", liveName),
			Remedy: "Run `herder enroll` from this session to restore its registry binding, then retry",
		}
	}
	return liveName, nil
}

func currentCallerCoordinates() (paneIDs, registryKeys []string) {
	envPane := os.Getenv("HERDR_PANE_ID")
	paneIDs = []string{envPane}
	registryKeys = []string{envPane}
	if envPane == "" {
		return uniqueNonEmpty(paneIDs), uniqueNonEmpty(registryKeys)
	}
	client := &herdrcli.Client{}
	if out, err := client.Output("pane", "get", envPane); err == nil {
		if pane, parseErr := herdrcli.ParsePaneGet(out); parseErr == nil {
			paneIDs = append(paneIDs, pane.PaneID)
			registryKeys = append(registryKeys, pane.PaneID, pane.TerminalID)
		}
	}
	return uniqueNonEmpty(paneIDs), uniqueNonEmpty(registryKeys)
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func requireStoredSender(row *registry.Record, liveName string) (string, error) {
	if !registry.IsSeated(*row) {
		return "", &SenderIdentityRefusal{
			Cause:  "the calling session's registry row is not seated",
			Remedy: "Run `herder enroll` from this session to restore its live registry seat, then retry",
		}
	}
	if row.HcomName == liveName {
		return liveName, nil
	}
	stored := row.HcomName
	if stored == "" || stored == "null" {
		stored = "(none)"
	} else {
		stored = "@" + stored
	}
	return "", &SenderIdentityRefusal{
		Cause:  fmt.Sprintf("the calling session's registry row records %s but live evidence proves @%s", stored, liveName),
		Remedy: "Run `herder enroll` from this session to repair its registry bus binding, then retry",
	}
}

func sameRecordIdentity(a, b *registry.Record) bool {
	if a == nil || b == nil || a.GUID == nil || b.GUID == nil {
		return false
	}
	return *a.GUID == *b.GUID
}

func writeSenderRefusal(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "herder send: refused — sender identity is not verified: %s. Nothing was sent.\n", err)
}

// disambiguatePane picks the single bus-live row among several seated sessions
// that all claim one pane/terminal coordinate. Liveness is a TIEBREAKER, not a
// gate: it runs ONLY when len(candidates) > 1 (the caller passes a lone
// candidate straight through). It returns the chosen row, or (nil, exitCode)
// with a loud candidate list when the coordinate is ambiguous — 0 live rows
// (cannot tell which session owns the pane now) or >1 live rows (genuinely
// two joined agents). Silently picking one, as the old last-in-guid-order
// resolution did, is the misdelivery this refuses (TASK-035).
func disambiguatePane(sender *busSender, candidates []registry.Record, target string, stderr io.Writer) (*registry.Record, int) {
	chosen, live := registry.PickLiveCandidate(candidates, func(r registry.Record) bool {
		return sender.joined(r.HcomName, r.HcomDir)
	})
	if chosen != nil {
		return chosen, 0
	}
	if len(live) == 0 {
		fmt.Fprintf(stderr, "herder send: refused — %d seated sessions claim '%s' but none is joined on the bus; cannot tell which session owns the pane now. Candidates:\n%sAddress one directly by its guid or label. Nothing was sent.\n", len(candidates), target, formatCandidates(candidates))
		return nil, 2
	}
	fmt.Fprintf(stderr, "herder send: refused — %d rows claiming '%s' are bus-live at once; refusing to guess which. Candidates:\n%sAddress one directly by its guid or label. Nothing was sent.\n", len(live), target, formatCandidates(live))
	return nil, 2
}

// formatCandidates renders one indented line per row for an ambiguity refusal:
// enough identity (guid, label, bus name) for the operator to pick a target.
func formatCandidates(recs []registry.Record) string {
	var b strings.Builder
	for i := range recs {
		rec := recs[i]
		bus := rec.HcomName
		if bus == "" || bus == "null" {
			bus = "(no bus name)"
		} else {
			bus = "@" + bus
		}
		fmt.Fprintf(&b, "  - guid=%s label=%s bus=%s\n", ptrString(rec.GUID), ptrString(rec.Label), bus)
	}
	return b.String()
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ambientHcomDir() string {
	if dir := os.Getenv("HCOM_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hcom")
}

type options struct {
	Help       bool
	TimeoutMS  int
	JSONOutput bool
	DryRun     bool
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, string, string, int) {
	opts := options{TimeoutMS: 3000}
	target := ""
	message := ""
	for i := 0; i < len(args); {
		arg := args[i]
		switch arg {
		case "--dry-run":
			opts.DryRun = true
			i++
		case "--timeout":
			if i+1 >= len(args) {
				die(stderr, "--timeout requires a value")
				return opts, "", "", 64
			}
			var timeout int
			if _, err := fmt.Sscanf(args[i+1], "%d", &timeout); err != nil {
				timeout = 3000
			}
			opts.TimeoutMS = timeout
			i += 2
		case "--json":
			opts.JSONOutput = true
			i++
		case "-h", "--help":
			printHelp(stdout)
			opts.Help = true
			return opts, "", "", 0
		default:
			if len(arg) >= 2 && arg[:2] == "--" {
				die(stderr, "unknown flag: "+arg)
				return opts, "", "", 64
			}
			if target == "" {
				target = arg
			} else if message == "" {
				message = arg
			} else {
				die(stderr, "extra positional arg: "+arg)
				return opts, "", "", 64
			}
			i++
		}
	}

	if target == "" {
		die(stderr, "target required (guid, label, terminal_id, or pane id)")
		return opts, "", "", 64
	}
	if message == "" && !opts.DryRun {
		die(stderr, "message required")
		return opts, "", "", 64
	}
	return opts, target, message, 0
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder send: %s\n", msg)
}

func printHelp(stdout io.Writer) {
	lines := []string{
		"herder send — deliver a message to a spawned agent over the hcom bus, delivery verified.",
		"",
		"Usage:",
		"  herder send --credential-file PATH <target> <message> [options]",
		"",
		"<target> is a short-guid, full guid, label, terminal_id (term_*), or raw pane_id.",
		"Every form resolves through the spawn registry to the agent's recorded bus name:",
		"guid/label match the latest row; terminal/pane ids match the seated session(s) holding",
		"the coordinate (drift-proof across pane move re-keying). Terminal ids are run-scoped:",
		"after a herdr restart resolution refuses rather than mis-sends; recover via reconcile.",
		"A pane id is display-only, so one coordinate can match several seated sessions; resolution",
		"then delivers to the single row currently JOINED on the bus and REFUSES (exit 2) with",
		"the candidate list when the coordinate is ambiguous (0 or >1 rows bus-live) rather than",
		"guessing. hcom is THE transport — a target with no bus-bound registry row is refused",
		"(exit 2); nothing is ever typed into a pane.",
		"The credential selects the caller first; live session/process/pane evidence may only",
		"verify or refuse that selected row and can never select another. Missing or conflicting proof refuses with",
		"an enroll/repair remedy; no user-facing label or synthetic sender is substituted.",
		"(The herdr keystroke transport was removed. The one surviving keystroke path is",
		"spawn's boot-time initial-prompt paste, owned by `herder spawn`.)",
		"",
		"Options:",
		"  --credential-file PATH  registry-current immutable per-seat credential (required)",
		"  --dry-run       resolve the target and print where it would send, then exit without sending",
		"  --timeout MS    max wait for a delivery receipt on the bus (default 3000)",
		"  --json          emit a JSON record of the send on stdout",
		"",
		"Exit codes:",
		"  0   sent + verified, OR queued. \"queued\" means no delivery receipt inside the window —",
		"      normal for a busy target; the bus injects the message at its next turn boundary.",
		"      Do NOT resend; a resend double-delivers.",
		"  1   hcom send itself failed (transient — the bus errored).",
		"  2   refused: sender identity is not verified, target is not bus-bound (no registry row",
		"      or recorded bus name), or target is not joined on its recorded bus. Nothing was sent.",
		"  64  usage error.",
		"",
		"If it fails:",
		"  - exit 2 \"no recorded bus name\": the agent was not launched through hcom (bash panes,",
		"    sidecar rows) — there is no delivery path to it; interact via its pane directly.",
		"  - watching long work: don't loop send/wait — have the worker report back (spawn --notify).",
		"",
		"Transport (HERDER_BUS env: auto|hcom):",
		"  auto (default) resolves through the registry and refuses non-bus targets; hcom forces",
		"  the bus for debugging — an unregistered target is then treated as a literal bus name on",
		"  the ambient HCOM_DIR. The old herdr keystroke value is a hard error. No flag ever names",
		"  a transport.",
		"",
		"Context ceiling:",
		"  A bus message cannot type a slash command, so steered self-compaction is NOT a send:",
		"  use `herder compact '<steer>'` — it queues a real /compact into your OWN pane (self",
		"  only) that fires at turn end. Persist state first (commit + progress report).",
	}
	fmt.Fprint(stdout, strings.Join(lines, "\n")+"\n")
}
