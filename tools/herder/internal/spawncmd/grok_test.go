package spawncmd

import (
	"bytes"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/launchcmd"
)

func TestT19GrokPassthroughRefusals(t *testing.T) {
	cases := []string{
		"--session-id", "--session-id=value", "-s", "--resume", "-r", "--continue", "-c", "--continue=1", "--fork-session",
		"--rules", "--permission-mode", "--always-approve", "--bypassPermissions",
		"--no-auto-update", "--auto-update", "--disable-auto-update", "--agents", "--agent", "--subagents",
		"--no-subagents", "--no-no-subagents", "HOME=/tmp/elsewhere", "GROK_HOME=/tmp/elsewhere",
	}
	for _, arg := range cases {
		t.Run(strings.TrimLeft(strings.ReplaceAll(arg, "=", "_"), "-"), func(t *testing.T) {
			if err := launchcmd.ValidateGrokExtraArgs([]string{arg}, false); err == nil || !strings.Contains(err.Error(), "remove") {
				t.Fatalf("validateGrokExtraArgs(%q) = %v, want targeted refusal with remedy", arg, err)
			}
		})
	}
}

func TestT19GrokModelPassthroughOnlyConflictsWithFirstClassModel(t *testing.T) {
	if err := launchcmd.ValidateGrokExtraArgs([]string{"--model", "grok-4.5"}, false); err != nil {
		t.Fatalf("passthrough-only model refused: %v", err)
	}
	if err := launchcmd.ValidateGrokExtraArgs([]string{"--model=grok-4.5"}, true); err == nil || !strings.Contains(err.Error(), "--model conflicts") {
		t.Fatalf("first-class model collision = %v", err)
	}
}

func TestGrokNormalAndSafePermissionMapping(t *testing.T) {
	if got := defaultPermFlag("grok"); got != "--always-approve" {
		t.Fatalf("normal Grok permission flag = %q", got)
	}
	if !hasExplicitPermFlag([]string{"--always-approve"}) {
		t.Fatal("Grok permission flag not recognized as explicit")
	}
}

func TestGrokAbsentKeyRefusesAtEntryInsteadOfBindingTimeout(t *testing.T) {
	t.Setenv("HERDER_GROK_ACTIVATED", "1")
	t.Setenv("XAI_API_KEY", "")
	var stdout, stderr bytes.Buffer
	_, rc := parseArgs([]string{"--role", "neutral", "--agent", "grok"}, &stdout, &stderr)
	if rc == 0 || !strings.Contains(stderr.String(), "environment that launches the herdr server") || strings.Contains(stderr.String(), "bind-timeout") {
		t.Fatalf("absent-key refusal rc=%d stderr=%q", rc, stderr.String())
	}
}
