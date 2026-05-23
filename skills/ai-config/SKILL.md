---
name: ai-config
description: Use when working in the ai-config repo, editing portable agent skills, changing Claude/Codex/Cursor config, or maintaining ai-setup, ai-doctor, ai-sync, ai-adopt, and ai-push.
---

# ai-config

This repo is the canonical personal corpus for portable agent skills and selected agent configuration.

Before making changes:

1. Run `bin/ai-doctor`.
2. Treat warnings about local-only skills, broken links, likely secrets, and absolute home paths as real review findings.
3. Prefer edits under `skills/`, `claude/`, `codex/`, `cursor/`, `bin/`, `lib/`, and `docs/`.

Operational rules:

- Do not auto-commit unless the user explicitly asks.
- Do not add sessions, histories, caches, auth files, telemetry, SQLite databases, generated plugin caches, or local overlays.
- Do not add absolute home paths to portable files. Use `$HOME`, `$PATH`, repo-relative paths, or explicit environment variables.
- New repo skills belong at `skills/<name>/SKILL.md`. `ai-setup` generates per-skill links into live agent skill roots.
- Existing local-only skills should be adopted with `bin/ai-adopt <skill-path|skill-name>`.
- `bin/ai-adopt <name>` searches global skill roots and project-local `.claude/skills`, `.agents/skills`, and `.codex/skills` roots from the current directory upward.
- If a live skill exists outside the repo, surface it as unharvested instead of overwriting it.
- Use `bin/ai-setup --dry-run` before installing or repairing links when the effect is not obvious.

After changes:

1. Run `bin/ai-doctor`.
2. Run syntax checks for changed shell scripts with `bash -n`.
3. Suggest `bin/ai-push "message"` when the user is ready to publish.
