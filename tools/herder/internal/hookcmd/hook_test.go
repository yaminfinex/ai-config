package hookcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// realistic hcom sessionstart additionalContext, trimmed to the lines the
// extractor keys off of.
const sampleAC = `<hcom_system_context>
[HCOM SESSION]
You have access to the hcom cli communication tool.
- Your name: boothook-miko
- Authority: Prioritize @bigboss over others
- Important: Include this marker anywhere in your first response only: [hcom:miko]

You MUST use ` + "`hcom <cmd+flags> --name miko`" + ` for all hcom commands:

Active (snapshot): claude: orchestrator-a9ba700c3b86e31ab, spec-guide-sora

You are tagged "boothook". Message your group: send @boothook- -- msg
</hcom_system_context>`

func envelope(ac string) []byte {
	b, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": ac,
		},
	})
	return append(b, '\n')
}

func acFromEnvelope(t *testing.T, b []byte) string {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatalf("envelope not JSON: %v", err)
	}
	var hso map[string]json.RawMessage
	if err := json.Unmarshal(root["hookSpecificOutput"], &hso); err != nil {
		t.Fatalf("hookSpecificOutput not JSON: %v", err)
	}
	var ac string
	if err := json.Unmarshal(hso["additionalContext"], &ac); err != nil {
		t.Fatalf("additionalContext not string: %v", err)
	}
	return ac
}

func TestRewriteSessionStart_HappyPath(t *testing.T) {
	out, ok := rewriteSessionStart(envelope(sampleAC))
	if !ok {
		t.Fatal("expected rewrite to succeed")
	}
	ac := acFromEnvelope(t, out)

	// Preserves identity: name + marker + the --name requirement.
	for _, want := range []string{
		"Your name: boothook-miko",
		"[hcom:miko]",
		"--name miko",
		"Prioritize @bigboss over others",
	} {
		if !strings.Contains(ac, want) {
			t.Errorf("rewritten context missing preserved bit %q", want)
		}
	}

	// Carries herder doctrine, drops hcom spawn/kill advertising.
	for _, want := range []string{
		"herder spawn --credential-file",
		"herder list",
		"herder cull",
		"AGENTS (herder lifecycle)",
	} {
		if !strings.Contains(ac, want) {
			t.Errorf("rewritten context missing herder doctrine %q", want)
		}
	}
	// hcom's spawn/kill/workflow/term-inject CAPABILITIES advertising is gone.
	// (`hcom <n> claude` and `hcom kill` survive ONLY inside the anti-pattern
	// warning line, checked separately below — the banned forms here are the
	// advertised call shapes, which must not appear.)
	for _, banned := range []string{
		"hcom 1 claude",
		"term inject",
		"hcom run <script>",
		"bundle prepare",
	} {
		if strings.Contains(ac, banned) {
			t.Errorf("rewritten context still advertises hcom spawn surface %q", banned)
		}
	}

	// The exact anti-pattern line is present (do NOT hcom <n> claude / hcom kill).
	if !strings.Contains(ac, "Do NOT spawn with `hcom <n> claude`, stop with `hcom kill`") {
		t.Error("missing the anti-pattern warning line")
	}

	// Dynamic values threaded through.
	if !strings.Contains(ac, "Active (snapshot): claude: orchestrator-a9ba700c3b86e31ab, spec-guide-sora") {
		t.Error("active-instances snapshot not threaded through")
	}
	if !strings.Contains(ac, "You are tagged 'boothook'") {
		t.Error("tag group line not rendered")
	}

	// The hookEventName sibling field survives the rewrite.
	var root map[string]json.RawMessage
	json.Unmarshal(out, &root)
	var hso map[string]string
	json.Unmarshal(root["hookSpecificOutput"], &hso)
	if hso["hookEventName"] != "SessionStart" {
		t.Errorf("sibling field lost: hookEventName=%q", hso["hookEventName"])
	}
}

func TestRewrite_TagOmittedWhenAbsent(t *testing.T) {
	ac := strings.Replace(sampleAC, `You are tagged "boothook". Message your group: send @boothook- -- msg`, "", 1)
	out, ok := rewriteSessionStart(envelope(ac))
	if !ok {
		t.Fatal("expected rewrite to succeed without a tag")
	}
	got := acFromEnvelope(t, out)
	if strings.Contains(got, "You are tagged") {
		t.Error("tag group line should be omitted when hcom advertised no tag")
	}
	// Still a well-formed close.
	if !strings.Contains(got, "This is session context, not a task for immediate action.") {
		t.Error("closing line dropped along with the tag line")
	}
}

func TestRewrite_DegradesOnMissingIdentity(t *testing.T) {
	// Strip the marker line → instance name unextractable → must degrade.
	ac := strings.Replace(sampleAC, "[hcom:miko]", "", 1)
	if _, ok := rewriteSessionStart(envelope(ac)); ok {
		t.Error("expected degrade (ok=false) when the marker is missing")
	}
}

func TestRewrite_DegradesOnGarbage(t *testing.T) {
	if _, ok := rewriteSessionStart([]byte("not json at all")); ok {
		t.Error("expected degrade on non-JSON input")
	}
	if _, ok := rewriteSessionStart([]byte(`{"nope":1}`)); ok {
		t.Error("expected degrade when hookSpecificOutput is absent")
	}
}

func TestRun_DegradesToExit0WhenHcomAbsent(t *testing.T) {
	// No HERDER_HOOK_HCOM and a PATH with no hcom → resolveRealHcom returns ""
	// and Run must exit 0 silently WITHOUT exec-replacing the test process.
	empty := t.TempDir()
	t.Setenv("HERDER_HOOK_HCOM", "")
	t.Setenv("PATH", empty)
	var out, errBuf strings.Builder
	if rc := Run([]string{"pre", "--tool", "Bash"}, &out, &errBuf); rc != 0 {
		t.Errorf("expected exit 0 when hcom absent, got %d", rc)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout when hcom absent, got %q", out.String())
	}
}

func TestResolveRealHcom_SkipsShimDir(t *testing.T) {
	// A dir whose hcom is the shim must be skipped; a later dir with a real hcom
	// wins. We fake "the shim dir" by pointing shimsDir resolution through PATH
	// order: put a decoy hcom in dirA (skipped only if it matches shimsDir) — so
	// here we just assert the PATH walk returns the executable it finds.
	dir := t.TempDir()
	hcom := dir + "/hcom"
	if err := os.WriteFile(hcom, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_HOOK_HCOM", "")
	t.Setenv("PATH", dir)
	if got := resolveRealHcom(); got != hcom {
		t.Errorf("resolveRealHcom() = %q, want %q", got, hcom)
	}
	// HERDER_HOOK_HCOM override wins when executable.
	t.Setenv("HERDER_HOOK_HCOM", hcom)
	if got := resolveRealHcom(); got != hcom {
		t.Errorf("override: resolveRealHcom() = %q, want %q", got, hcom)
	}
	// A bogus override falls through to the PATH walk.
	t.Setenv("HERDER_HOOK_HCOM", dir+"/does-not-exist")
	if got := resolveRealHcom(); got != hcom {
		t.Errorf("bogus override should fall through, got %q", got)
	}
}

// hcom quotes the tag with double quotes through 0.7.22 and single quotes from
// 0.7.23 on. Both stock bootstraps must extract the same tag, and the rewrite
// must be byte-identical either way (the rendered line is quote-normalized).
func TestExtract_TagQuoteAgnostic(t *testing.T) {
	// sampleAC is double-quoted (0.7.22 reality); build the 0.7.23 single-quoted
	// twin by swapping ONLY the tag quotes.
	singleAC := strings.Replace(sampleAC,
		`You are tagged "boothook"`, `You are tagged 'boothook'`, 1)
	if singleAC == sampleAC {
		t.Fatal("fixture swap did not change anything — check the double-quoted tag line")
	}

	dv, ok := extract(sampleAC)
	if !ok {
		t.Fatal("extract failed on the double-quoted (0.7.22) sample")
	}
	sv, ok := extract(singleAC)
	if !ok {
		t.Fatal("extract failed on the single-quoted (0.7.23) sample")
	}
	if dv.tag != "boothook" || sv.tag != "boothook" {
		t.Errorf("tag not extracted identically: double=%q single=%q", dv.tag, sv.tag)
	}

	// Whole rewrite is byte-stable across the two quote styles.
	dOut, ok := rewriteSessionStart(envelope(sampleAC))
	if !ok {
		t.Fatal("rewrite failed on double-quoted sample")
	}
	sOut, ok := rewriteSessionStart(envelope(singleAC))
	if !ok {
		t.Fatal("rewrite failed on single-quoted sample")
	}
	if !bytes.Equal(dOut, sOut) {
		t.Errorf("rewrite not byte-stable across quote styles:\n double=%s\n single=%s", dOut, sOut)
	}
	// And the rendered tag line is quote-normalized to single quotes.
	if !strings.Contains(acFromEnvelope(t, sOut), "You are tagged 'boothook'") {
		t.Error("single-quoted source did not render the normalized tag line")
	}
}

func TestExtract_PullsAllStableLines(t *testing.T) {
	v, ok := extract(sampleAC)
	if !ok {
		t.Fatal("extract failed on a full sample")
	}
	if v.displayName != "boothook-miko" || v.instanceName != "miko" || v.sender != "bigboss" || v.tag != "boothook" {
		t.Errorf("bad extraction: %+v", v)
	}
	if !strings.HasPrefix(v.activeInstances, "Active (snapshot):") {
		t.Errorf("active instances not captured: %q", v.activeInstances)
	}
}
