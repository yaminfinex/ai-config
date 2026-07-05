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
	recs, err := registry.Load(registryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			die(stderr, "no registry at "+registryPath)
			return 1
		}
		die(stderr, err.Error())
		return 1
	}
	rec := registry.Resolve(recs, target)
	if rec == nil {
		die(stderr, "unknown target: "+target)
		return 1
	}
	guid := ptrString(rec.GUID)
	if owner := registry.ActiveLabelOwner(recs, newLabel, guid); owner != nil {
		die(stderr, fmt.Sprintf("label %q already belongs to active guid %s", newLabel, ptrString(owner.GUID)))
		return 1
	}

	base := rec.Raw
	if len(bytes.TrimSpace(base)) == 0 {
		base = []byte(`{}`)
	}
	row, err := registry.UpdateRawObject(base, map[string]any{"label": newLabel})
	if err != nil {
		die(stderr, err.Error())
		return 1
	}
	if err := registry.Append(registryPath, row); err != nil {
		die(stderr, err.Error())
		return 1
	}

	fmt.Fprintf(stderr, "renamed %s -> %s (%s)\n", ptrString(rec.Label), newLabel, guid)
	syncHerdrName(rec.TerminalID, newLabel, stdout)
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
	fmt.Fprint(stdout, `herder rename — append a new label for an enrolled agent.

Usage:
  herder rename <target> <new-label>
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
