package hookcmd

import (
	"strings"
	"testing"
)

// TASK-002 pins: the SUBAGENTS doctrine is back in the bootstrap, per-tool.
// Claude's recipe rides the sessionstart rewrite (the bare `sessionstart` verb
// is claude-only in hcom, so template placement IS the gating); codex's block
// is a separate const that launchcmd threads into launch args.

func TestBootstrapTemplate_ClaudeSubagentsBlock(t *testing.T) {
	out, ok := rewriteSessionStart(envelope(sampleAC))
	if !ok {
		t.Fatal("expected rewrite to succeed")
	}
	ac := acFromEnvelope(t, out)

	// hcom's original CLAUDE_ONLY recipe, reinstated verbatim.
	for _, want := range []string{
		"## SUBAGENTS",
		"Run Task with background=true",
		"Tell subagent: `use hcom`",
		"DO NOT give them any specific hcom syntax",
		"hcom config -i self subagent_timeout [SEC]",
		// herder addition: keep Task subagents distinct from peer sessions.
		"for a separate peer session use `herder spawn` instead",
	} {
		if !strings.Contains(ac, want) {
			t.Errorf("claude bootstrap missing SUBAGENTS bit %q", want)
		}
	}

	// The codex-only doctrine must NOT leak into the claude bootstrap.
	if strings.Contains(ac, "Codex has no Task/subagent tool") {
		t.Error("codex SUBAGENTS block leaked into the claude sessionstart rewrite")
	}
}

// TASK-014 pins: the codex block grew from SUBAGENTS-only to the full herder
// addendum. hcom's own bootstrap always lands FIRST in developer_instructions
// and cannot be removed at the launch-args seam, so the block supersedes the
// hcom-native lifecycle guidance rather than replacing the whole bootstrap.
func TestCodexBootstrapBlock_Content(t *testing.T) {
	for _, want := range []string{
		// Addendum framing: names what stands and what is overridden.
		"[HERDER SESSION ADDENDUM]",
		"SUPERSEDED",
		// Shared herder lifecycle doctrine (verbatim from herderAgentsSection).
		"## AGENTS (herder lifecycle)",
		"herder spawn --role",
		"herder cull",
		"Do NOT spawn with `hcom <n> claude`, stop with `hcom kill`",
		"Delivery is verified",
		"`herder <command> --help`",
		// Codex-specific subagent doctrine.
		"## SUBAGENTS",
		"Codex has no Task/subagent tool",
	} {
		if !strings.Contains(CodexBootstrapBlock, want) {
			t.Errorf("codex bootstrap block missing %q", want)
		}
	}

	// The block is delivered verbatim (no render pass), so it must not carry
	// unresolved {placeholders} from the claude template's vocabulary.
	for _, banned := range []string{"{display_name}", "{instance_name}", "{SENDER}", "{tag}", "{active_instances}"} {
		if strings.Contains(CodexBootstrapBlock, banned) {
			t.Errorf("codex bootstrap block carries unresolved placeholder %q", banned)
		}
	}

	// Claude's Task-tool recipe must not leak into the codex block.
	if strings.Contains(CodexBootstrapBlock, "background=true") {
		t.Error("claude Task recipe leaked into the codex bootstrap block")
	}
}

// The AGENTS doctrine must be textually shared between the three delivery
// surfaces — the claude sessionstart rewrite, the codex launch block, and the
// codex post-resume re-delivery — so they cannot drift apart.
func TestHerderAgentsSection_SharedAcrossSurfaces(t *testing.T) {
	if !strings.Contains(bootstrapTemplate, herderAgentsSection) {
		t.Error("claude bootstrapTemplate no longer embeds herderAgentsSection verbatim")
	}
	if !strings.Contains(CodexBootstrapBlock, herderAgentsSection) {
		t.Error("CodexBootstrapBlock no longer embeds herderAgentsSection verbatim")
	}
	if !strings.Contains(CodexResumeAddendum, herderAgentsSection) {
		t.Error("CodexResumeAddendum no longer embeds herderAgentsSection verbatim")
	}
}

// TASK-017 pins: resumed/forked codex sessions get the addendum re-delivered
// as a bus MESSAGE (hcom strips user developer_instructions on those paths),
// so the variant differs from the launch block ONLY in its preamble — the
// doctrine tail (AGENTS + codex SUBAGENTS) is byte-identical by construction,
// and both blocks must end with that exact shared tail.
func TestCodexResumeAddendum_Content(t *testing.T) {
	sharedTail := herderAgentsSection + "\n\n" + codexSubagentsSection
	if !strings.HasSuffix(CodexBootstrapBlock, sharedTail) {
		t.Error("CodexBootstrapBlock no longer ends with the shared doctrine tail")
	}
	if !strings.HasSuffix(CodexResumeAddendum, sharedTail) {
		t.Error("CodexResumeAddendum no longer ends with the shared doctrine tail")
	}

	for _, want := range []string{
		// Message framing: self-identifies, no reply, repeat is a no-op.
		"[HERDER SESSION ADDENDUM — re-delivered after resume/fork]",
		"do NOT reply to this message",
		"resumed or forked through herder",
		"nothing has changed",
		// Same supersede contract as the launch block.
		"SUPERSEDED",
	} {
		if !strings.Contains(CodexResumeAddendum, want) {
			t.Errorf("codex resume addendum missing %q", want)
		}
	}

	// It arrives mid-conversation, not as system context: it must not point at
	// surrounding context it cannot see from a message.
	if strings.Contains(CodexResumeAddendum, "context above") {
		t.Error("codex resume addendum references 'context above' — invalid for message delivery")
	}

	// Delivered verbatim (no render pass) — no unresolved claude placeholders.
	for _, banned := range []string{"{display_name}", "{instance_name}", "{SENDER}", "{tag}", "{active_instances}"} {
		if strings.Contains(CodexResumeAddendum, banned) {
			t.Errorf("codex resume addendum carries unresolved placeholder %q", banned)
		}
	}

	// Claude's Task-tool recipe must not leak into the codex doctrine.
	if strings.Contains(CodexResumeAddendum, "background=true") {
		t.Error("claude Task recipe leaked into the codex resume addendum")
	}
}
