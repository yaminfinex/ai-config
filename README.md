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

Canonical new-machine instructions live in [docs/machine-setup.md](docs/machine-setup.md).

After cloning, open an agent inside this repo and ask it to set the repo up, or run:

```sh
bin/ai-setup --dry-run
bin/ai-setup
bin/ai-doctor --quick
```

`ai-setup` requires mise and writes the managed mise PATH config for `bin/` and
`tools/herder/shims/`. Restart the shell after setup.

## Skill Layout

- `skills/`: globally portable skills. `ai-setup` links each `skills/*/SKILL.md` directory into supported home skill roots.
- `.agents/skills/`: project-local skills for this repo. This is canonical for repo-local skill instructions.
- `.claude`: symlink to `.agents`, so Claude sees the same project-local skills without duplicate files.
- Skill `references/` subdirs (e.g. `skills/herder/references/`): supporting material a skill loads on demand. Nothing here is linked into home skill roots by `ai-setup`.

`ai-config-bootstrap` lives under `.agents/skills/` only and is intentionally not copied into global home skill roots.

## Repository Layout

Beyond skills and agent config, the repo also tracks:

- `bin/`, `lib/`: the `ai-*` and `bottle` commands and their shared shell library.
- `tools/bottle/`: the Go implementation behind `bin/bottle` (see `tools/bottle/README.md`).
- `vendor/`: vendored upstream projects some skills build on (e.g. `native-shortcuts-herd`).
- `docs/`: design notes and plans.

## Optional: herdr

The `herder`, `herder-fork`, and `orchestrate` skills drive herdr surfaces and only activate inside a herdr pane (`HERDR_ENV=1`); without herdr they stay dormant and the rest of the repo works normally. `ai-setup` never installs herdr or its shortcuts, and the `bin/vsc-*` / `etc/launchd` editor helpers are opt-in — none of it is required to use the portable skills, `bottle`, or config linking.

## Current Caveats

- This repo does not sync secrets. Keep auth, sessions, histories, caches, telemetry, and SQLite state local.
- `ai-doctor` warns on absolute home paths in portable files. Use `$HOME`, `$PATH`, repo-relative paths, or explicit environment variables.
- Local-only skills are reported but not adopted automatically.
