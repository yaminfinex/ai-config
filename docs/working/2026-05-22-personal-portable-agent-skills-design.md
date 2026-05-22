# Personal Portable Agent Skills: Low-Ceremony Sync Design

Date: 2026-05-22
Status: Working design
Last updated: 2026-05-23 after design review

## Goal

Use one private GitHub repository as the canonical home for personal agent skills and selected agent configuration, shared across multiple machines and multiple agents such as Claude Code, Codex, Cursor, and future tools.

The system should optimize for local iteration:

- Editing a skill in an agent's live config path should edit the canonical corpus.
- Publishing local improvements should be one command.
- Pulling improvements from another machine should be one command.
- Agents should be able to understand and maintain this repo without rediscovering the rules.

## Core Design

Use symlinks to collapse the usual bidirectional-sync problem.

Instead of copying files between a canonical repo and each agent's live config directory, make selected live files point into the repo. The repo working tree becomes the source of truth and the live runtime location at the same time.

This means:

- Harvesting local changes is `git add`, `git commit`, and `git push`.
- Distributing changes to another machine is `git pull`.
- Distributing skills to multiple agents on the same machine is more symlinks to the same repo paths.

The important constraint is that not every agent config directory is safe to replace wholesale. Some paths are agent-owned and contain built-in or generated state. The setup command should link explicit allowlisted paths and avoid masking agent-owned content.

## Proposed Repository Layout

```text
ai-config/
  README.md
  .gitignore

  docs/
    working/
      2026-05-22-personal-portable-agent-skills-design.md

  skills/
    ai-config/
      SKILL.md
    tdd/
      SKILL.md
    debugging/
      SKILL.md

  claude/
    CLAUDE.md
    settings.shared.json
    settings.local.example.json
    hooks/
    commands/

  codex/
    AGENTS.md

  cursor/
    rules/

  bin/
    ai-setup
    ai-doctor
    ai-sync
    ai-harvest

  lib/
    common.sh
```

## Live Machine Layout

Use a hybrid linking strategy:

- Whole-directory links are acceptable for directories that are clearly user-owned.
- Per-item links are required inside mixed agent-owned directories where personal files would collide with built-in or generated siblings.

This keeps new-skill ceremony low for agents whose skills directory is all yours, while avoiding accidental masking of agent-owned content.

Example intended links:

```text
~/.claude/CLAUDE.md          -> ~/Coding/ai-config/claude/CLAUDE.md
~/.claude/hooks              -> ~/Coding/ai-config/claude/hooks
~/.claude/commands           -> ~/Coding/ai-config/claude/commands
~/.claude/skills             -> ~/Coding/ai-config/skills

~/.codex/AGENTS.md           -> ~/Coding/ai-config/codex/AGENTS.md
~/.codex/skills/ai-config    -> ~/Coding/ai-config/skills/ai-config
~/.codex/skills/tdd          -> ~/Coding/ai-config/skills/tdd

~/.cursor/rules              -> ~/Coding/ai-config/cursor/rules
```

Claude Code's `~/.claude/skills` is treated as user-owned for the initial design, so whole-directory linking is appropriate there. Adding a new skill under `skills/` then automatically exposes it to Claude Code without editing `ai-setup`.

Avoid replacing `~/.codex/skills` wholesale because Codex may keep system skills under `~/.codex/skills/.system`. Replacing the whole directory with a symlink can mask built-in skills. For Codex, link personal skill children individually or under an agreed personal namespace if Codex supports it.

This rule generalizes: whole-directory symlinks are acceptable only when the target directory is clearly user-owned. For mixed agent-owned directories, link personal children.

## Commands

The executable layer should be small Bash scripts. Bash is appropriate because the core operations are filesystem checks, symlink management, and Git commands. Avoid adding Python, Node, or other bootstrap dependencies until structured config merging becomes necessary.

### `ai-doctor`

Purpose: inspect the repo and live machine without making risky changes.

Responsibilities:

- Verify expected repo structure.
- Verify expected symlinks.
- Report broken links.
- Report collisions where a real file or directory exists where a symlink should exist.
- Report Git state:
  - dirty working tree
  - ahead or behind upstream
  - missing remote
  - current branch
- Run a concrete secret scan:
  - if `gitleaks` is installed, run `gitleaks detect --staged` for staged changes and an equivalent working-tree scan for allowlisted files
  - otherwise fall back to the regex scan below
- Warn on absolute home paths in portable tracked files.
- Print concrete remediation steps.

`ai-doctor` should come first in implementation because it gives humans and agents a shared, non-mutating view of the current state.

Minimum fallback secret regex:

```text
(sk-[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{36}|-----BEGIN [A-Z ]+PRIVATE KEY-----)
```

Minimum portability regex for tracked portable files:

```text
/(Users|home)/[A-Za-z0-9._-]+/
```

The portability scan should target files that are meant to work across machines, especially:

```text
claude/hooks/
claude/commands/
codex/AGENTS.md
cursor/rules/
skills/
bin/
lib/
```

Absolute home paths are forbidden in synced portable files. Use `$HOME`, `$PATH`, repo-relative paths, or command discovery instead.

### `ai-setup`

Purpose: install or repair live links on a machine.

Responsibilities:

- Create required parent directories such as `~/.claude`, `~/.claude/skills`, `~/.codex/skills`, and `~/.cursor`.
- Link only allowlisted paths.
- If the target path already points to the desired source, do nothing.
- If the target path is a broken or wrong symlink, replace it after reporting the change.
- If the target path is a real file or directory, back it up before linking.
- Never delete user data.
- Be idempotent and safe to re-run.

Backups should live next to the agent config root in an `.ai-config-backup` directory so recovery is predictable.

For a collision at `~/.claude/CLAUDE.md`, use:

```text
~/.claude/.ai-config-backup/20260522T171500/CLAUDE.md
```

For nested paths, preserve the relative path under the timestamped backup directory.

### `ai-sync`

Purpose: pull changes from the canonical remote and repair local links.

Responsibilities:

- Run `ai-doctor --quick`.
- Refuse to pull with uncommitted changes unless passed an explicit `--autostash`.
- Run `git pull --rebase`.
- Run narrow link verification and heal only safe symlink drift.
- Print a concise summary of changed files.

Default behavior should prioritize predictability over cleverness. It is acceptable for `ai-sync` to stop and tell the user to harvest or stash local changes first.

`ai-sync` should not run the full `ai-setup` by default. Full setup is an explicit command because it may back up collisions or install new categories of links. Sync should only repair expected symlinks that are missing, broken, or pointed at the wrong repo path and have no real file or directory collision.

### `ai-harvest`

Purpose: commit and push intentional local changes.

Responsibilities:

- Run `ai-doctor --quick`.
- Refuse to proceed if obvious secrets are detected.
- Stage only allowlisted paths.
- Show the staged diff summary.
- Commit only if there is a staged diff.
- Push to the configured upstream.

Use an allowlist rather than `git add -A` across the whole repo.

Initial allowlist:

```text
skills/
claude/CLAUDE.md
claude/hooks/
claude/commands/
claude/settings.shared.json
claude/settings.local.example.json
codex/AGENTS.md
cursor/rules/
bin/
lib/
docs/
README.md
.gitignore
```

Default commit message:

```text
harvest: <hostname> <iso-8601 timestamp>
```

Allow a custom message:

```text
ai-harvest "improve debugging skill"
```

## Agent-Facing Meta Skill

Create `skills/ai-config/SKILL.md` to make the repository self-describing for agents.

The skill should instruct agents to:

- Treat this repo as the canonical personal agent config corpus.
- Run `bin/ai-doctor` before edits.
- Prefer edits under `skills/`, `claude/`, `codex/`, `cursor/`, `bin/`, `lib/`, and `docs/`.
- Avoid committing sessions, histories, caches, auth files, telemetry, SQLite databases, generated plugin caches, and machine-local overlays.
- Run `bin/ai-doctor` after edits.
- Suggest `bin/ai-harvest` when the user appears ready to publish changes.
- Use `bin/ai-sync` before major edits if the repo is behind upstream.
- Never auto-commit unless explicitly asked.

The scripts are the stable mechanical API. The skill is the operating manual that lets agents maintain the repo consistently.

The skill is a nudge, not the enforcement boundary. Whether an agent invokes `skills/ai-config/SKILL.md` depends on that agent's skill-selection behavior and the wording of the task. `ai-doctor` is the primary gate because it is executable and can be called directly by humans, hooks, and agents.

## Auto-Harvest Decision

Do not auto-commit.

Use hooks for visibility, not mutation.

Recommended behavior:

- Session start hook: run `ai-doctor --quick` and warn if the repo is dirty, ahead, behind, or has broken links.
- Session stop hook: print a concise dirty-state summary and suggest `ai-harvest` when appropriate.
- Commit boundary remains explicit via manual `ai-harvest`.

Rationale:

- The expensive failure is not forgetting to push. The expensive failure is distributing half-edited instructions to every machine and agent.
- Skills act like policy for future agents. Surprise commits can silently degrade future behavior.
- Session boundaries are not semantic completion boundaries.
- Dirty-state reminders reduce drift without creating noisy or bad commits.

If this proves too manual, improve `ai-harvest` ergonomics before adding automation.

## Secrets and Local State

Do not commit live settings files unless they are proven stable and secret-free.

Prefer this pattern:

```text
claude/settings.shared.json
claude/settings.local.example.json
```

Ignore machine-local overlays and generated state:

```text
*.local.json
settings.json
.credentials.json
auth.json
history.jsonl
sessions/
cache/
telemetry/
*.sqlite
*.sqlite-*
```

If an agent does not support settings overlays, keep that agent's live settings file local for now. Do not introduce encrypted config management until there is a concrete need to sync secrets.

Chezmoi plus age remains a possible later upgrade if encrypted portable secrets or per-machine templating become necessary. It is not part of the initial design.

Secret detection must be concrete from the first implementation. The initial implementation should:

- prefer `gitleaks` when installed
- fall back to the regex in the `ai-doctor` section
- scan staged files before commit
- scan allowlisted portable files during `ai-doctor`
- fail closed for `ai-harvest` when a likely secret is detected

## Machine-Portability Rule

Tracked portable files must not contain absolute user-home paths such as `/Users/yamen/...` or `/home/yamen/...`.

This applies to:

- hooks
- commands
- skills
- agent instructions
- scripts
- shared settings

Portable files should use:

- `$HOME` for home-relative paths
- `$PATH` command lookup where possible
- repo-relative paths resolved from the script location
- explicit environment variables for machine-specific tools

`ai-doctor` should enforce this with the portability regex above and print the offending file and line. Without this rule, the second machine can fail in subtle ways that look like symlink or setup problems.

## Conflict Handling

Use normal Git conflict handling initially.

Do not introduce per-machine branches by default. They add ceremony and can hide divergence until later. Direct commits to the same private repo are acceptable while conflicts are rare.

If conflicts become frequent, revisit:

- per-machine branches
- a protected main branch with PR-style merges
- skill-level ownership or file splitting

## Drift Handling

`ai-sync` and `ai-setup` have different drift behavior.

`ai-sync` should heal only narrow symlink drift after a pull. `ai-setup` is the explicit command for installing or repairing the full link set.

Safe for `ai-sync` to heal:

- expected symlink is missing and parent directory is user-owned
- expected symlink points to the wrong repo path
- expected symlink is broken

Warn and stop during `ai-sync`, but allow `ai-setup` to back up before changing:

- a real file exists where a symlink is expected
- a real directory exists where a symlink is expected
- an agent-owned mixed directory would be masked by a whole-directory symlink

## Plugin Marketplace

Do not register this repo as a Claude Code private marketplace in the initial implementation.

The symlink mechanism and marketplace mechanism solve different problems:

- Symlinks are for live personal iteration.
- Marketplaces are for packaged distribution, versioning, and installable bundles.

Adding marketplace registration now would duplicate the initial mechanism and add ceremony. Revisit once there are stable, self-contained skill bundles worth versioning.

## Implementation Order

1. Initialize the repo and commit this design.
2. Add `.gitignore` with local-state and secret exclusions.
3. Add `lib/common.sh`.
4. Implement `ai-doctor`, including secret and absolute-home-path scans.
5. Implement `ai-setup`, including predictable backup directories.
6. Implement `ai-sync`, with narrow safe link healing only.
7. Implement `ai-harvest`, using the allowlist and `ai-doctor` gate.
8. Add `skills/ai-config/SKILL.md`.
9. Run `ai-doctor`.
10. Adopt existing local config incrementally.

## Open Questions for Review

- Which exact paths should be linked for Claude Code, Codex, and Cursor in the first implementation?
- Should `ai-setup` default to backing up collisions automatically, or require an explicit `--adopt` / `--force-backup` flag?
- Should `ai-harvest` require a custom message, or is the default timestamped message acceptable for low-friction usage?
- Should settings overlays be implemented now, or deferred until a specific agent requires them?
