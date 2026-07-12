package renamecmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

type options struct {
	help        bool
	target      string
	newLabel    string
	takeFrom    string
	confirmLive bool
}

type TransferResult struct {
	TargetGUID       string
	SourceGUID       string
	Label            string
	TargetTerminalID string
}

const (
	AdoptionReasonSeatSuperseded = "seat superseded by replacement process in the same pane"
	AdoptionReasonConfirmedDead  = "operator confirmed old transcript dead"
)

func Run(args []string, stdout, stderr io.Writer) int {
	opts, code := parseArgs(args, stdout, stderr)
	if code != 0 || opts.help {
		return code
	}

	registryPath := registry.DefaultPath()
	if _, err := os.Stat(registryPath); err != nil && errors.Is(err, os.ErrNotExist) {
		die(stderr, "no registry at "+registryPath)
		return 1
	}
	if opts.takeFrom != "" {
		result, err := Transfer(registryPath, opts.target, opts.takeFrom, opts.confirmLive)
		if err != nil {
			die(stderr, err.Error())
			return 1
		}
		fmt.Fprintf(stderr, "transferred label %s from %s to %s\n", result.Label, result.SourceGUID, result.TargetGUID)
		syncHerdrName(result.TargetTerminalID, result.Label, stdout)
		return 0
	}

	var oldLabel, guid, terminalID string
	outcomes, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		rec := registry.V2Resolve(tx.Projection, opts.target)
		if rec == nil {
			return nil, fmt.Errorf("unknown target: %s", opts.target)
		}
		guid = rec.GUID
		oldLabel = rec.Label
		switch rec.State {
		case v2.StateRetired:
			return nil, fmt.Errorf("target %s is retired; run 'herder reopen %s' first", rec.GUID, rec.GUID)
		case v2.StateLost:
			return nil, fmt.Errorf("target %s is lost; lost sessions cannot be renamed", rec.GUID)
		}
		if owner := registry.V2LabelOwner(tx.Projection, opts.newLabel, guid); owner != nil {
			return nil, fmt.Errorf("label %q already belongs to non-retired session %s", opts.newLabel, owner.GUID)
		}
		if rec.Seat != nil {
			terminalID = rec.Seat.TerminalID
		} else if legacy, ok := registry.DecodeLegacyV1Raw(*rec); ok {
			terminalID = legacy.TerminalID
		}
		next := *rec
		next.Event = "labelled"
		next.RecordedAt = ""
		next.Label = opts.newLabel
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		die(stderr, err.Error())
		return 1
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		die(stderr, err.Error())
		return 1
	}
	if err := outcome.Err(); err != nil {
		die(stderr, err.Error())
		return 1
	}

	fmt.Fprintf(stderr, "renamed %s -> %s (%s)\n", oldLabel, opts.newLabel, guid)
	syncHerdrName(terminalID, opts.newLabel, stdout)
	return 0
}

// Transfer atomically releases the source label and assigns it to target. Both
// candidates are decided and appended under one registry lock.
func Transfer(registryPath, target, source string, confirmLive bool) (TransferResult, error) {
	return transfer(registryPath, target, source, transferOptions{confirmLive: confirmLive})
}

// TransferForAdoption atomically unseats a still-seated source while moving
// its label to the replacement. The caller must establish why that seat is
// dead or superseded before invoking this lifecycle-specific form.
func TransferForAdoption(registryPath, target, source, expectedSourcePane, unseatReason string) (TransferResult, error) {
	if unseatReason != AdoptionReasonSeatSuperseded && unseatReason != AdoptionReasonConfirmedDead {
		return TransferResult{}, fmt.Errorf("adoption transfer requires a recognized unseat reason")
	}
	if expectedSourcePane == "" {
		return TransferResult{}, fmt.Errorf("adoption transfer requires the preflight source pane")
	}
	return transfer(registryPath, target, source, transferOptions{unseatSource: true, expectedSourcePane: expectedSourcePane, unseatReason: unseatReason})
}

type transferOptions struct {
	confirmLive        bool
	unseatSource       bool
	expectedSourcePane string
	unseatReason       string
}

func transfer(registryPath, target, source string, opts transferOptions) (TransferResult, error) {
	var result TransferResult
	outcomes, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		rows, resolved, err := transferCandidatesWithOptions(tx, target, source, opts)
		result = resolved
		return rows, err
	})
	if err != nil {
		return TransferResult{}, err
	}
	if len(outcomes) != 2 {
		return TransferResult{}, fmt.Errorf("registry transfer returned %d outcomes for two candidates", len(outcomes))
	}
	for i, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			return TransferResult{}, err
		}
		if outcome.Status != registry.WriteApplied {
			return TransferResult{}, fmt.Errorf("registry transfer candidate %d was %s, not applied", i+1, outcome.Status)
		}
	}
	return result, nil
}

func transferCandidates(tx registry.LockedUpdate, target, source string, confirmLive bool) ([]v2.SessionRecord, TransferResult, error) {
	return transferCandidatesWithOptions(tx, target, source, transferOptions{confirmLive: confirmLive})
}

func transferCandidatesWithOptions(tx registry.LockedUpdate, target, source string, opts transferOptions) ([]v2.SessionRecord, TransferResult, error) {
	targetRec := registry.V2Resolve(tx.Projection, target)
	if targetRec == nil {
		return nil, TransferResult{}, fmt.Errorf("unknown target: %s", target)
	}
	sourceRec := registry.V2Resolve(tx.Projection, source)
	if sourceRec == nil {
		return nil, TransferResult{}, fmt.Errorf("unknown source: %s", source)
	}
	if targetRec.GUID == sourceRec.GUID {
		return nil, TransferResult{}, fmt.Errorf("target and --take-from source resolve to the same guid %s; use plain rename to choose a different label", targetRec.GUID)
	}
	if targetRec.Label != "" && targetRec.Label == sourceRec.Label {
		return nil, TransferResult{}, fmt.Errorf("target %s and source %s both already claim label %q; refusing to partially repair a duplicate-label registry", targetRec.GUID, sourceRec.GUID, sourceRec.Label)
	}
	switch sourceRec.State {
	case v2.StateSeated:
		if !opts.confirmLive && !opts.unseatSource {
			return nil, TransferResult{}, fmt.Errorf("source %s is seated-and-live; rerun with --confirm-live to explicitly transfer its label, or cull it first", sourceRec.GUID)
		}
		if opts.unseatSource && (sourceRec.Seat == nil || sourceRec.Seat.PaneID != opts.expectedSourcePane) {
			currentPane := ""
			if sourceRec.Seat != nil {
				currentPane = sourceRec.Seat.PaneID
			}
			return nil, TransferResult{}, fmt.Errorf("source %s seat changed after adoption preflight from pane %s to pane %s; refusing to unseat it", sourceRec.GUID, opts.expectedSourcePane, currentPane)
		}
	case v2.StateLost:
		return nil, TransferResult{}, fmt.Errorf("source %s is lost; labels cannot be taken from LOST sessions", sourceRec.GUID)
	case v2.StateRetired:
		return nil, TransferResult{}, fmt.Errorf("source %s is retired and already released its label; use plain 'herder rename %s <label>'", sourceRec.GUID, targetRec.GUID)
	case v2.StateUnseated:
		if sourceRec.Seat != nil {
			return nil, TransferResult{}, fmt.Errorf("source %s is unseated but still has a seat; refusing anomalous row", sourceRec.GUID)
		}
	default:
		return nil, TransferResult{}, fmt.Errorf("source %s has unknown state %q; refusing label transfer", sourceRec.GUID, sourceRec.State)
	}
	if sourceRec.Label == "" {
		return nil, TransferResult{}, fmt.Errorf("source %s has no label to transfer; use plain rename on target %s", sourceRec.GUID, targetRec.GUID)
	}
	switch targetRec.State {
	case v2.StateRetired:
		return nil, TransferResult{}, fmt.Errorf("target %s is retired; run 'herder reopen %s' first", targetRec.GUID, targetRec.GUID)
	case v2.StateLost:
		return nil, TransferResult{}, fmt.Errorf("target %s is lost; lost sessions cannot be renamed", targetRec.GUID)
	}

	targetTerminalID := ""
	if targetRec.Seat != nil {
		targetTerminalID = targetRec.Seat.TerminalID
	} else if legacy, ok := registry.DecodeLegacyV1Raw(*targetRec); ok {
		targetTerminalID = legacy.TerminalID
	}
	result := TransferResult{
		TargetGUID:       targetRec.GUID,
		SourceGUID:       sourceRec.GUID,
		Label:            sourceRec.Label,
		TargetTerminalID: targetTerminalID,
	}
	released := *sourceRec
	released.RecordedAt = ""
	released.Label = ""
	if sourceRec.State == v2.StateSeated && opts.unseatSource {
		released.Event = "adoption_source_released"
		released.State = v2.StateUnseated
		released.Seat = nil
		released.CloseResult = "adopted"
		released.CloseReason = opts.unseatReason
	} else {
		released.Event = "label_transferred"
	}
	claimed := *targetRec
	claimed.Event = "label_transferred"
	claimed.RecordedAt = ""
	claimed.Label = sourceRec.Label
	return []v2.SessionRecord{released, claimed}, result, nil
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	var opts options
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		printHelp(stdout)
		opts.help = true
		return opts, 0
	}
	if len(args) == 0 {
		die(stderr, "usage: herder rename <target> <new-label> | herder rename <target> --take-from <other> [--confirm-live]")
		return opts, 1
	}
	opts.target = args[0]
	for i := 1; i < len(args); {
		switch args[i] {
		case "--take-from":
			if i+1 >= len(args) {
				die(stderr, "--take-from requires a target")
				return opts, 1
			}
			opts.takeFrom = args[i+1]
			i += 2
		case "--confirm-live":
			opts.confirmLive = true
			i++
		default:
			if opts.newLabel != "" {
				die(stderr, "unexpected arg: "+args[i])
				return opts, 1
			}
			opts.newLabel = args[i]
			i++
		}
	}
	if opts.takeFrom != "" && opts.newLabel != "" {
		die(stderr, "a new label cannot be combined with --take-from; the source label is transferred")
		return opts, 1
	}
	if opts.takeFrom == "" && opts.confirmLive {
		die(stderr, "--confirm-live requires --take-from")
		return opts, 1
	}
	if opts.takeFrom == "" && opts.newLabel == "" {
		die(stderr, "new label must not be empty")
		return opts, 1
	}
	return opts, 0
}

func syncHerdrName(terminalID, label string, stdout io.Writer) {
	if terminalID == "" {
		fmt.Fprintln(stdout, "herdr: rename skipped (registry row has no terminal_id)")
		return
	}
	if _, err := exec.LookPath("herdr"); err != nil {
		fmt.Fprintln(stdout, "herdr: rename skipped (herdr not on PATH)")
		return
	}
	out, rc, err := (&herdrcli.Client{}).Combined("agent", "rename", terminalID, label)
	if err == nil && rc == 0 {
		fmt.Fprintf(stdout, "herdr: renamed %s to %s\n", terminalID, label)
		return
	}
	reason := "command failed"
	if err != nil {
		reason = err.Error()
	} else if len(bytes.TrimSpace(out)) > 0 {
		reason = string(bytes.TrimSpace(out))
	} else {
		reason = fmt.Sprintf("exit %d", rc)
	}
	fmt.Fprintf(stdout, "herdr: rename failed (%s) — registry updated anyway\n", reason)
}

// SyncHerdrName mirrors an already-applied registry label to the live herdr
// pane best-effort. Composite commands call it after their atomic write.
func SyncHerdrName(terminalID, label string, stdout io.Writer) {
	syncHerdrName(terminalID, label, stdout)
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder rename — assign or atomically transfer a session label.

Usage:
  herder rename <target> <new-label>
  herder rename <target> --take-from <other> [--confirm-live]

<target> and <other> accept a short-guid, full guid, label, or pane_id. Plain
rename appends one labelled record. --take-from releases <other>'s current
label and assigns it to <target> in one locked registry update; no free-label
window is observable. A seated-and-live source refuses unless --confirm-live
explicitly acknowledges taking its live address. The flag never weakens the
lost or retired refusals. Retired sources direct the caller to plain rename.

After the registry update, herder asks herdr to rename the target pane
best-effort; registry success is retained if that cosmetic sync fails.
`)
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder rename: %s\n", msg)
}
