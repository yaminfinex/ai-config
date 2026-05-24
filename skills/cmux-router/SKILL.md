---
name: cmux-router
description: Route agents to the right vendored cmux reference material before changing, automating, testing, or diagnosing cmux components. Use when working on cmux CLI commands, windows/workspaces/panes/surfaces, settings, shortcuts, browser panels, markdown panels, customization, diagnostics, or cmux-related skills; load only the relevant referenced files under this skill's references/upstream tree.
---

# cmux Router

## Purpose

Use this skill as the entry point for cmux work. It keeps the upstream cmux skill bundle available as references without installing each upstream skill as a live local skill.

Start with `references/index.md`, then load only the specific upstream files relevant to the task.

## Fast Route

- Core topology, identity, panes, surfaces, focus, and health: read `references/upstream/skills/cmux/SKILL.md`.
- Browser panels and browser automation: read `references/upstream/skills/cmux-browser/SKILL.md`.
- Current workspace rules and workspace-scoped automation: read `references/upstream/skills/cmux-workspace/SKILL.md`.
- cmux settings, config reloads, shortcuts, and schema-backed keys: read `references/upstream/skills/cmux-settings/SKILL.md`.
- Markdown viewer panels and live reload: read `references/upstream/skills/cmux-markdown/SKILL.md`.
- Keyboard shortcut behavior: read `references/upstream/skills/cmux-keyboard-shortcuts/SKILL.md`.
- Diagnostics and cmux health checks: read `references/upstream/skills/cmux-diagnostics/SKILL.md`.
- Customization examples: read `references/upstream/skills/cmux-customization/SKILL.md`.

## Usage Rules

- Prefer current `cmux -h` output over stale memory for command syntax.
- Prefer the vendored upstream references for workflows and command intent.
- For settings changes, follow the upstream settings skill instructions before editing config.
- Do not bulk-load every upstream reference. Use `references/index.md` to choose the narrow set.
- If `ai-doctor` reports the cmux references are stale, mention that before relying on them for precise command syntax.

## Freshness

The vendored upstream source marker lives at `references/upstream/SOURCE.md`. `bin/ai-doctor` checks the imported commit against upstream `manaflow-ai/cmux` when not run with `--quick`.
