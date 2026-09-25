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
4. The default run also writes the managed mise `conf.d` PATH file and the managed rc block that defines the launcher functions; the step-2 dry run shows both. `bin/ai-setup --rc status|remove` and `bin/ai-setup --shims status|remove` inspect or undo them.
5. Run `bin/ai-doctor --quick` again and report remaining warnings.
6. If `ai-doctor` still warns that `statusLine.command` is missing from `~/.claude/settings.json`, the settings meld did not run: `bin/ai-setup` melds `claude/settings.shared.json` (which carries the `statusLine` block) into `~/.claude/settings.json`, so re-run it rather than hand-merging. `settings.json` is local-only and never symlinked.

Shell startup files are owned by the managed rc block: change them through `bin/ai-setup --rc install|remove`, never by hand-editing `.zshrc` or `.bashrc`.

Do not adopt local-only skills automatically. If `ai-doctor` reports local-only skills, list them and ask which should be adopted with `bin/ai-adopt <skill-path|skill-name>`.

Do not run `bin/ai-push` unless the user explicitly asks to publish changes.
