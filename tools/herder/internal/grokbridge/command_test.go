package grokbridge

import (
	"strings"
	"testing"
)

func TestUnknownSubcommandNamesStopBridge(t *testing.T) {
	var stdout, stderr strings.Builder
	if rc := Run([]string{"unknown"}, &stdout, &stderr); rc != 2 {
		t.Fatalf("unknown subcommand rc=%d, want 2", rc)
	}
	if !strings.Contains(stderr.String(), "stop-bridge") {
		t.Fatalf("unknown subcommand remedy omits stop-bridge: %q", stderr.String())
	}
}

func TestManagedForkCompletionCoordinatesAreParsedForSupervisor(t *testing.T) {
	args := []string{
		"--complete-seat", "--lifecycle-mode", "fork", "--forked-from-guid", "parent-guid",
		"--hcom-dir", "/bus", "--session-id", "child-session",
	}
	cfg, ok := managedCompletionFromArgs(args, "/state", "child-guid")
	if !ok || cfg.Seat != "child-guid" || cfg.StateDir != "/state" || cfg.HcomDir != "/bus" ||
		cfg.SessionID != "child-session" || cfg.LifecycleMode != "fork" || cfg.ForkedFromGUID != "parent-guid" {
		t.Fatalf("managed fork completion config=%+v enabled=%t", cfg, ok)
	}
}
