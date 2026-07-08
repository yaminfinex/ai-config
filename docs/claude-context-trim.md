# Claude context trim

Decisions and mechanism for keeping Claude Code's per-request context overhead
down, applied consistently across machines. Prompted by
[How to kill the bloat in Claude Code's system prompt](https://www.aihero.dev/how-to-kill-the-bloat-in-claude-codes-system-prompt).

## Mechanism

`claude/settings.shared.json` is the repo-managed fragment of
`~/.claude/settings.json`. `bin/ai-setup` (and therefore `bin/ai-sync`, which
runs `ai-setup --heal-only`) melds it into the live file:

- Objects merge recursively; the shared fragment wins on scalar conflicts.
- Arrays union, so machine-local `permissions.deny` entries survive.
- Keys that exist only in the live file (hooks, model, theme, ...) are never
  touched or removed. Removing a key from the fragment does NOT remove it from
  live files — retire settings by hand or with a one-off note here.
- The live file is backed up to the ai-config backup dir before the first
  write of a run; the merge is skipped entirely when already in sync.

`bin/ai-doctor` warns when live settings are missing fragment keys.
`claude/settings.local.example.json` remains the example for machine-local
(`settings.local.json`) overrides.

## What is trimmed and why (2026-07)

Measured baseline: roughly 15–18k tokens of per-request overhead. Verify any
change with `/context` in a fresh session.

- `permissions.deny: ["Artifact"]` — the Artifact tool has one of the largest
  loaded schemas (~1k tokens) and had zero uses across all local transcripts.
  Empirically verified (2026-07-08): a bare tool name in `permissions.deny`
  removes the tool from the request payload entirely.
- `disableWorkflows: true` — the Workflow tool is the single largest schema
  (~3.5k tokens) and is not used directly. Caveat: `deep-research` fans out
  through it (all 4 historical Workflow uses were deep-research runs), so
  expect that skill to degrade or fail; remove the key from a machine's
  `settings.local.json`-managed live file by hand if that matters again.

## What was deliberately NOT trimmed

- `skills/improve-architecture` stays model-invocable — its description is
  light (~380 bytes) and hiding it isn't worth losing proactive triggering.
- `disableBundledSkills` — all-or-nothing, and would remove `deep-research`,
  `verify`, and `code-review`, which are in use.
- compound-engineering plugin (~1k tokens of skill descriptions, only 4 of 20
  skills ever used) — vendoring the used skills is impractical (ce-plan alone
  ships 24 support files and cross-references half the plugin), and
  `Skill(...)` deny rules were empirically tested (2026-07-08) and do NOT
  remove skills from the prompt catalogue, only block invocation. Trimming it
  while keeping an upstream update path is tracked as TASK-038.
- `ScheduleWakeup` — 135 uses (orchestration loops depend on it).
- Denying deferred tools (NotebookEdit, CronCreate, DesignSync, ...) — current
  Claude Code already defers their schemas; each costs one name-only line, so
  there is nothing meaningful to reclaim. Much of the article's per-tool
  advice predates deferred tool loading.
- hcom session injection (~900 tokens) — load-bearing for the team-bus setup.
