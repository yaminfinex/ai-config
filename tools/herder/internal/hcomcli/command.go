// Package hcomcli builds anonymous hcom subprocesses for the web API.
package hcomcli

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// CommandContext preserves the selected bus directory while removing any
// agent/pane identity inherited by a server launched from an agent session.
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "hcom", args...)
	cmd.Env = make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "HCOM_") && name != "HCOM_DIR" {
			continue
		}
		switch name {
		case "HERDR_PANE_ID", "HERDR_TAB_ID", "HERDR_WORKSPACE_ID":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	return cmd
}
