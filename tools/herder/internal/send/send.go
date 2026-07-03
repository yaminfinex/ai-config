package send

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ai-config/tools/herder/internal/driver"
)

type dryRunRecord struct {
	PaneID      string `json:"pane_id"`
	Target      string `json:"target"`
	ResolvedVia string `json:"resolved_via"`
	Drifted     bool   `json:"drifted"`
	DryRun      bool   `json:"dry_run"`
}

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
	selection := driver.NewSelection()
	if _, err := exec.LookPath("herdr"); err != nil {
		die(stderr, "herdr not on PATH")
		return 64
	}
	if _, err := exec.LookPath("jq"); err != nil {
		die(stderr, "jq not on PATH")
		return 64
	}

	opts, target, message, code := parseArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.Help {
		return 0
	}

	if opts.DryRun {
		return dryRun(selection, target, opts.JSONOutput, stdout, stderr)
	}
	sendOpts := driver.SendOptions{
		NoEnter:    opts.NoEnter,
		NoVerify:   opts.NoVerify,
		Force:      opts.Force,
		TimeoutMS:  opts.TimeoutMS,
		JSONOutput: opts.JSONOutput,
	}
	switch selection.Select(target) {
	case driver.TransportHcom:
		return selection.Hcom.Send(target, message, sendOpts, stdout, stderr)
	default:
		return selection.Herdr.Send(target, message, sendOpts, stdout, stderr)
	}
}

type options struct {
	Help       bool
	NoEnter    bool
	NoVerify   bool
	Force      bool
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
		case "--no-enter":
			opts.NoEnter = true
			i++
		case "--no-verify":
			opts.NoVerify = true
			i++
		case "--force":
			opts.Force = true
			i++
		case "--timeout":
			if i+1 >= len(args) {
				die(stderr, "unknown flag: --timeout")
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
		die(stderr, "target required (guid, label, or pane id)")
		return opts, "", "", 64
	}
	if message == "" && !opts.DryRun {
		die(stderr, "message required")
		return opts, "", "", 64
	}
	return opts, target, message, 0
}

func dryRun(selection *driver.Selection, target string, jsonOut bool, stdout, stderr io.Writer) int {
	switch selection.Select(target) {
	case driver.TransportHcom:
		return dryRunHcom(selection.Hcom, target, jsonOut, stdout, stderr)
	default:
		return dryRunHerdr(selection.Herdr, target, jsonOut, stdout, stderr)
	}
}

func dryRunHerdr(h *driver.Herdr, target string, jsonOut bool, stdout, stderr io.Writer) int {
	res, err := h.Resolve(target)
	if err != nil {
		var resolveErr *driver.ResolveError
		if errors.As(err, &resolveErr) {
			if resolveErr.Code == 2 {
				return 2
			}
			return 1
		}
		return 1
	}
	if res.DriftNote != "" {
		fmt.Fprintf(stderr, "herder-send: %s\n", res.DriftNote)
	}
	fmt.Fprintf(stderr, "herder-send --dry-run: %s -> pane %s (via %s)", target, res.PaneID, res.ResolvedVia)
	if res.Drifted {
		fmt.Fprint(stderr, " [DRIFTED]")
	}
	fmt.Fprintln(stderr)
	if jsonOut {
		record := dryRunRecord{
			PaneID:      res.PaneID,
			Target:      target,
			ResolvedVia: res.ResolvedVia,
			Drifted:     res.Drifted,
			DryRun:      true,
		}
		b, _ := json.Marshal(record)
		fmt.Fprintln(stdout, string(b))
	}
	return 0
}

func dryRunHcom(h *driver.Hcom, target string, jsonOut bool, stdout, stderr io.Writer) int {
	res, err := h.Resolve(target)
	if err != nil {
		var resolveErr *driver.ResolveError
		if errors.As(err, &resolveErr) && resolveErr.Code == 2 {
			fmt.Fprintf(stderr, "herder-send --dry-run: would REFUSE (exit 2): %s has no recorded bus name — not bus-bound\n", target)
			if jsonOut {
				writeJSON(stdout, hcomDryRunRefuseRecord{
					Target:    target,
					Transport: "hcom",
					Would:     "refuse",
					DryRun:    true,
				})
			}
			return 2
		}
		return 1
	}

	hcomDir := res.Dir
	team := res.Team
	if !res.Found {
		hcomDir = os.Getenv("HCOM_DIR")
		if hcomDir == "" {
			home, _ := os.UserHomeDir()
			hcomDir = filepath.Join(home, ".hcom")
		}
		team = "global"
	}
	displayTeam := team
	if displayTeam == "" {
		displayTeam = "global"
	}
	displayDir := hcomDir
	if displayDir == "" {
		displayDir = os.Getenv("HCOM_DIR")
		if displayDir == "" {
			home, _ := os.UserHomeDir()
			displayDir = filepath.Join(home, ".hcom")
		}
	}
	fmt.Fprintf(stderr, "herder-send --dry-run: %s -> hcom bus @%s (team: %s, HCOM_DIR=%s)\n", target, res.Name, displayTeam, displayDir)
	if jsonOut {
		writeJSON(stdout, hcomDryRunRecord{
			Target:    target,
			Transport: "hcom",
			HcomName:  res.Name,
			HcomDir:   hcomDir,
			Team:      team,
			DryRun:    true,
		})
	}
	return 0
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder-send: %s\n", msg)
}

func printHelp(stdout io.Writer) {
	lines := []string{
		"#!/usr/bin/env bash",
		"# herder-send — send a message to an already-spawned peer agent, safely.",
		"#",
		"# Usage:",
		"#   herder-send <target> <message> [opts]",
		"#",
		"# <target>: short-guid, full guid, label, terminal_id (term_*), or pane_id. A",
		"# guid/label is resolved to the agent's CURRENT pane via its durable terminal_id",
		"# (registry pane_ids go stale as herdr compacts ids — see \"resolve\" below); a bare",
		"# term_* is resolved the same drift-proof way without needing a registry record",
		"# (this is what `herder-spawn --notify` injects for the orchestrator ring). A raw",
		"# pane_id is used verbatim.",
		"#",
		"# Options:",
		"#   --dry-run            resolve the target and report where it would send, then",
		"#                        exit WITHOUT pasting anything (debug a mis-send safely)",
		"#   --no-enter           place text into the prompt but don't submit",
		"#   --no-verify          skip post-send delivery verification",
		"#   --force              skip pre-flight state check (still verifies delivery)",
		"#   --timeout MS         max time to wait for the prompt buffer to clear (default 3000)",
		"#   --json               emit a JSON record on stdout describing the send",
		"#",
		"# The send is dispatched through the delivery-driver interface, which selects",
		"# a transport (herdr keystroke, hcom hooks, etc.) automatically.",
		"#",
		"# Exit codes:",
		"#   0  — sent + verified, OR queued (target was busy; message accepted to run next)",
		"#   1  — send or verification failed",
		"#   2  — refused: target gone (terminal not live) or in interrupted/modal state",
		"#   64 — usage error",
		"#",
		"",
		"set -euo pipefail",
		"",
		"die() { printf 'herder-send: %s\\n' \"$*\" >&2; exit 64; }",
		"[[ \"${HERDR_ENV:-}\" = \"1\" ]] || die \"not running inside a herdr pane (HERDR_ENV != 1)\"",
		"command -v herdr >/dev/null 2>&1 || die \"herdr not on PATH\"",
		"command -v jq >/dev/null 2>&1 || die \"jq not on PATH\"",
		"",
		"TARGET=\"\"",
	}
	fmt.Fprint(stdout, strings.Join(lines, "\n")+"\n")
}

func writeJSON(stdout io.Writer, record any) {
	b, _ := json.Marshal(record)
	fmt.Fprintln(stdout, string(b))
}
