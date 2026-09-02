package herderstate

import (
	"path/filepath"
	"testing"
)

func TestDirUsesHerderThenXDGThenHomePrecedence(t *testing.T) {
	t.Setenv("HERDER_STATE_DIR", "/tmp/explicit-herder-state")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	t.Setenv("HOME", "/tmp/home")
	if got, err := Dir(); err != nil || got != "/tmp/explicit-herder-state" {
		t.Fatalf("explicit Dir()=%q err=%v", got, err)
	}

	t.Setenv("HERDER_STATE_DIR", "")
	if got, err := Dir(); err != nil || got != filepath.Join("/tmp/xdg-state", "herder") {
		t.Fatalf("XDG Dir()=%q err=%v", got, err)
	}

	t.Setenv("XDG_STATE_HOME", "")
	if got, err := Dir(); err != nil || got != filepath.Join("/tmp/home", ".local", "state", "herder") {
		t.Fatalf("home Dir()=%q err=%v", got, err)
	}
}
