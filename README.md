# ai-config

Personal portable agent skills and selected agent configuration.

This repo is the canonical corpus. Live agent config paths are symlinked into it so local edits happen directly in the repo.

## Commands

- `bin/ai-setup`: install or repair live symlinks.
- `bin/ai-doctor`: inspect Git state, symlink drift, local-only skills, likely secrets, and absolute home paths.
- `bin/ai-sync`: pull remote changes and heal safe symlink drift.
- `bin/ai-adopt <skill-path|skill-name>`: copy a local-only skill into `skills/<name>` and relink live roots.
- `bin/ai-push "message"`: stage allowlisted repo files, validate them, commit, and push.
- `bin/bottle`: pin, name, and re-enter agent contexts; run `bin/bottle` (no args) for the agent-first help.

## First Machine Setup

After cloning, open an agent inside this repo and ask it to set the repo up, or run:

```sh
bin/ai-doctor --quick
bin/ai-setup --dry-run
bin/ai-setup
```

To add `bin/` to your shell PATH:

```sh
bin/ai-setup --dry-run --shell-path
bin/ai-setup --shell-path
```

Shell PATH setup is opt-in and uses a managed block in `~/.zshrc` or `~/.bashrc`.

## Skill Layout

- `skills/`: globally portable skills. `ai-setup` links each `skills/*/SKILL.md` directory into supported home skill roots.
- `.agents/skills/`: project-local skills for this repo. This is canonical for repo-local skill instructions.
- `.claude`: symlink to `.agents`, so Claude sees the same project-local skills without duplicate files.
- `references/external/`: upstream reference material used to design personal skills. Nothing here is installed by `ai-setup`.

`ai-config-bootstrap` lives under `.agents/skills/` only and is intentionally not copied into global home skill roots.

## Current Caveats

- This repo does not sync secrets. Keep auth, sessions, histories, caches, telemetry, and SQLite state local.
- `ai-doctor` warns on absolute home paths in portable files. Use `$HOME`, `$PATH`, repo-relative paths, or explicit environment variables.
- Local-only skills are reported but not adopted automatically.
