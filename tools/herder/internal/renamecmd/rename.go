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

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		printHelp(stdout)
		return 0
	}
	if len(args) != 2 {
		die(stderr, "usage: herder rename <target> <new-label>")
		return 1
	}
	target, newLabel := args[0], args[1]
	if newLabel == "" {
		die(stderr, "new label must not be empty")
		return 1
	}

	registryPath := registry.DefaultPath()
	var oldLabel, guid, terminalID string
	if _, err := os.Stat(registryPath); err != nil && errors.Is(err, os.ErrNotExist) {
		die(stderr, "no registry at "+registryPath)
		return 1
	}
	_, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		rec := registry.V2Resolve(tx.Projection, target)
		if rec == nil {
			return nil, fmt.Errorf("unknown target: %s", target)
		}
		guid = rec.GUID
		oldLabel = rec.Label
		switch rec.State {
		case v2.StateRetired:
			return nil, fmt.Errorf("target %s is retired; run 'herder reopen %s' first", rec.GUID, rec.GUID)
		case v2.StateLost:
			return nil, fmt.Errorf("target %s is lost; lost sessions cannot be renamed", rec.GUID)
		}
		if owner := registry.V2LabelOwner(tx.Projection, newLabel, guid); owner != nil {
			return nil, fmt.Errorf("label %q already belongs to active guid %s", newLabel, owner.GUID)
		}
		terminalID = registry.LegacyFromV2(*rec).TerminalID
		next := *rec
		next.Event = "labelled"
		next.RecordedAt = ""
		next.Label = newLabel
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		die(stderr, err.Error())
		return 1
	}

	fmt.Fprintf(stderr, "renamed %s -> %s (%s)\n", oldLabel, newLabel, guid)
	syncHerdrName(terminalID, newLabel, stdout)
	return 0
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

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder rename — give an enrolled agent a new label (registry + herdr, best-effort).

Usage:
  herder rename <target> <new-label>

<target> is a short-guid, full guid, label, or pane_id. Appends a new registry
record carrying the new label (append-only JSONL) and asks herdr to rename the
pane too; if the herdr rename fails, the registry is still updated.

If it fails:
  - "unknown target": run 'herder list' to find the right guid/label.
  - "label already belongs to active guid": the label is taken by a live agent —
    pick another, or cull the holder first.
`)
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder rename: %s\n", msg)
}
