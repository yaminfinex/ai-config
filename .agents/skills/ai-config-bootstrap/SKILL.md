---
name: ai-config-bootstrap
description: Use when a freshly cloned ai-config repo needs to set itself up on the current machine, install symlinks, inspect local skill drift, or optionally add ai-config/bin to the shell PATH.
---

# ai-config-bootstrap

Use this only for first-time or repair setup from inside the `ai-config` repo. This skill is intentionally project-local and must not be installed into global home skill roots.

## Setup Flow

When the user asks to "set me up on this machine", "install this repo", or similar:

1. Run `bin/ai-doctor --quick`.
2. Run `bin/ai-setup --dry-run` and inspect the output.
3. If the dry run only creates expected links or backs up clear collisions, run `bin/ai-setup`.
4. If the user wants command names available globally, run `bin/ai-setup --dry-run --shell-path` first.
5. Run `bin/ai-setup --shell-path` only after explicit approval for shell startup changes.
6. Run `bin/ai-doctor --quick` again and report remaining warnings.
7. If `ai-doctor` warns that `statusLine.command` is missing from `~/.claude/settings.json`, merge the `statusLine` block from `claude/settings.local.example.json` into `~/.claude/settings.json`. `settings.json` is local-only and never symlinked.

Do not manually edit `.zshrc`, `.bashrc`, or other shell startup files. Use `bin/ai-setup --shell-path`.

Do not adopt local-only skills automatically. If `ai-doctor` reports local-only skills, list them and ask which should be adopted with `bin/ai-adopt <skill-path|skill-name>`.

Do not run `bin/ai-push` unless the user explicitly asks to publish changes.
