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

	"ai-config/tools/herder/internal/registry"
)

type hcomDryRunRecord struct {
	Target    string `json:"target"`
	Transport string `json:"transport"`
	HcomName  string `json:"hcom_name"`
	HcomDir   string `json:"hcom_dir"`
	Team      string `json:"team"`
	DryRun    bool   `json:"dry_run"`
}

type hcomDryRunRefuseRecord struct {
	Target    string `json:"target"`
	Transport string `json:"transport"`
	Would     string `json:"would"`
	DryRun    bool   `json:"dry_run"`
}

func Run(args []string, stdout, stderr io.Writer) int {
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

	forced := false
	switch os.Getenv("HERDER_BUS") {
	case "herdr":
		die(stderr, "HERDER_BUS=herdr is gone — the herdr keystroke transport was removed; hcom is the only transport (unset HERDER_BUS, or use auto|hcom)")
		return 64
	case "hcom":
		forced = true
	}

	recs, err := registry.Load(registry.DefaultPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		die(stderr, "registry not readable: "+err.Error())
		return 1
	}
	sender := &busSender{}

	rec := registry.Resolve(recs, target)
	if rec == nil {
		// Pane/terminal targets are positional and reused across sessions, so
		// one coordinate can match several active rows (a reused pane
		// accumulates a stale manual-enroll identity per prior session,
		// TASK-035). A single candidate resolves exactly as before — bus-less
		// bash rows and not-joined-yet claude/codex rows flow through to the
		// deliver path unchanged. Only when >1 candidate matches is bus
		// liveness a tiebreaker: deliver to the sole joined row, and refuse
		// loudly with the candidate list when 0 or >1 are live (never guess).
		candidates := registry.ActiveCandidatesByPaneOrTerminal(recs, target)
		switch len(candidates) {
		case 0:
			// rec stays nil → unregistered-target path below.
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
		if forced {
			// Forced-hcom debug affordance (unchanged from the driver era): an
			// unregistered target is a literal bus name on the ambient HCOM_DIR.
			if opts.DryRun {
				fmt.Fprintf(stderr, "herder send --dry-run: %s -> hcom bus @%s (team: global, HCOM_DIR=%s)\n", target, target, ambientHcomDir())
				if opts.JSONOutput {
					writeCompactJSON(stdout, hcomDryRunRecord{
						Target:    target,
						Transport: "hcom",
						HcomName:  target,
						HcomDir:   ambientHcomDir(),
						Team:      "global",
						DryRun:    true,
					})
				}
				return 0
			}
			return sender.send(target, target, "", message, opts.TimeoutMS, opts.JSONOutput, stdout, stderr)
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
		team := rec.Team
		if team == "" {
			team = "global"
		}
		dir := rec.HcomDir
		if dir == "" || dir == "null" {
			dir = ambientHcomDir()
		}
		fmt.Fprintf(stderr, "herder send --dry-run: %s -> hcom bus @%s (team: %s, HCOM_DIR=%s)\n", target, rec.HcomName, team, dir)
		if opts.JSONOutput {
			writeCompactJSON(stdout, hcomDryRunRecord{
				Target:    target,
				Transport: "hcom",
				HcomName:  rec.HcomName,
				HcomDir:   rec.HcomDir,
				Team:      rec.Team,
				DryRun:    true,
			})
		}
		return 0
	}

	return sender.send(target, rec.HcomName, rec.HcomDir, message, opts.TimeoutMS, opts.JSONOutput, stdout, stderr)
}

// disambiguatePane picks the single bus-live row among several active rows
// that all claim one pane/terminal coordinate. Liveness is a TIEBREAKER, not a
// gate: it runs ONLY when len(candidates) > 1 (the caller passes a lone
// candidate straight through). It returns the chosen row, or (nil, exitCode)
// with a loud candidate list when the coordinate is ambiguous — 0 live rows
// (cannot tell which session owns the pane now) or >1 live rows (genuinely
// two joined agents). Silently picking one, as the old last-in-guid-order
// resolution did, is the misdelivery this refuses (TASK-035).
func disambiguatePane(sender *busSender, candidates []registry.Record, target string, stderr io.Writer) (*registry.Record, int) {
	var live []registry.Record
	for i := range candidates {
		if sender.joined(candidates[i].HcomName, candidates[i].HcomDir) {
			live = append(live, candidates[i])
		}
	}
	switch len(live) {
	case 1:
		return &live[0], 0
	case 0:
		fmt.Fprintf(stderr, "herder send: refused — %d active rows claim '%s' but none is joined on the bus; cannot tell which session owns the pane now. Candidates:\n%sAddress one directly by its guid or label. Nothing was sent.\n", len(candidates), target, formatCandidates(candidates))
		return nil, 2
	default:
		fmt.Fprintf(stderr, "herder send: refused — %d rows claiming '%s' are bus-live at once; refusing to guess which. Candidates:\n%sAddress one directly by its guid or label. Nothing was sent.\n", len(live), target, formatCandidates(live))
		return nil, 2
	}
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
		"  herder send <target> <message> [options]",
		"",
		"<target> is a short-guid, full guid, label, terminal_id (term_*), or raw pane_id.",
		"Every form resolves through the spawn registry to the agent's recorded bus name:",
		"guid/label match the latest row; terminal/pane ids match the ACTIVE row(s) holding",
		"the coordinate (drift-proof as herdr compacts pane ids). A pane is positional and",
		"reused across sessions, so one coordinate can match several active rows; resolution",
		"then delivers to the single row currently JOINED on the bus and REFUSES (exit 2) with",
		"the candidate list when the coordinate is ambiguous (0 or >1 rows bus-live) rather than",
		"guessing. hcom is THE transport — a target with no bus-bound registry row is refused",
		"(exit 2); nothing is ever typed into a pane.",
		"(The herdr keystroke transport was removed. The one surviving keystroke path is",
		"spawn's boot-time initial-prompt paste, owned by `herder spawn`.)",
		"",
		"Options:",
		"  --dry-run       resolve the target and print where it would send, then exit without sending",
		"  --timeout MS    max wait for a delivery receipt on the bus (default 3000)",
		"  --json          emit a JSON record of the send on stdout",
		"",
		"Exit codes:",
		"  0   sent + verified, OR queued. \"queued\" means no delivery receipt inside the window —",
		"      normal for a busy target; the bus injects the message at its next turn boundary.",
		"      Do NOT resend; a resend double-delivers.",
		"  1   hcom send itself failed (transient — the bus errored).",
		"  2   refused: target is not bus-bound (no registry row, or a row without a recorded",
		"      bus name), or not joined on its recorded bus. Nothing was sent.",
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
