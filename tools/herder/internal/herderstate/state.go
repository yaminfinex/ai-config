// Package herderstate resolves Herder's node-local state directory.
package herderstate

import (
	"os"
	"path/filepath"
)

// Dir resolves $HERDER_STATE_DIR, then $XDG_STATE_HOME/herder, then
// ~/.local/state/herder. Tests and isolated serves set HERDER_STATE_DIR.
func Dir() (string, error) {
	if dir := os.Getenv("HERDER_STATE_DIR"); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "herder"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "herder"), nil
}
