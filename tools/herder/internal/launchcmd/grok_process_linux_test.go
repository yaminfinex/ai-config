//go:build linux

package launchcmd

import (
	"syscall"
	"testing"
)

func TestManualGrokBridgeHardKillFencePinsParentDeathSignal(t *testing.T) {
	manual := grokBridgeProcessAttributes(true)
	if manual.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("manual bridge Pdeathsig=%v want=%v", manual.Pdeathsig, syscall.SIGTERM)
	}
	managed := grokBridgeProcessAttributes(false)
	if managed.Pdeathsig != 0 {
		t.Fatalf("managed bridge Pdeathsig=%v want=0", managed.Pdeathsig)
	}
}
