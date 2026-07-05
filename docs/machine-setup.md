# Machine Setup

Canonical bring-up for a new machine using this repo.

## Clone

Clone the repo at the normal local checkout path:

```sh
mkdir -p "$HOME/Coding"
git clone <repo-url> "$HOME/Coding/ai-config"
cd "$HOME/Coding/ai-config"
```

## Put `bin/` On PATH

Preview, then install the managed shell block:

```sh
bin/ai-setup --dry-run --shell-path
bin/ai-setup --shell-path
```

Open a fresh shell after this step so `ai-setup`, `ai-doctor`, `herder spawn`, and the other
repo commands resolve from `bin/`.

## Install Symlinks

Preview, then install portable config and skill links:

```sh
bin/ai-setup --dry-run
bin/ai-setup
```

## Install hcom Hooks

hcom hooks are explicit machine config. Install them separately:

```sh
bin/ai-setup --hcom-hooks status
bin/ai-setup --hcom-hooks install
```

## Activate Herder Shims

Herder's `claude` and `codex` PATH shims live in `tools/herder/shims/`. Activate them through
mise `conf.d`:

```sh
bin/ai-setup --shims status
bin/ai-setup --shims install
```

This writes `${XDG_CONFIG_HOME:-$HOME/.config}/mise/conf.d/ai-config-shims.toml` with this checkout's
absolute shim path. mise must be active in your login shells for the drop-in to affect new panes.
The shim directory should appear on PATH exactly once; duplicate entries make shadowing confusing.

`ai-setup --shims install` does not write `HERDER_SHIM_ARGS_CLAUDE`. If you want Claude's
skip-permissions behavior for manual shim launches, set that yourself in machine-local config.

Aliases and shell functions beat PATH shims. Find old overrides with:

```sh
grep -RInE '^[[:space:]]*(alias[[:space:]]+(claude|codex)=|(function[[:space:]]+)?(claude|codex)[[:space:]]*\(\))' \
  "$HOME/.zshrc" "$HOME/.zshenv" "$HOME/.zprofile" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.config" 2>/dev/null
```

Remove or rename any `claude` / `codex` alias or function that should no longer shadow the shims.

## Verify

Run:

```sh
bin/ai-doctor
type -a claude codex
herder spawn --role smoke --agent codex --cwd "$PWD" \
  --prompt 'Reply exactly PONG MACHINE-SETUP, then wait idle.'
```

`type -a` should show this repo's shim path before any real tool binary. `ai-doctor` warns if the
mise file exists but the current shell has zero, duplicate, or shadowed shim entries.

## Updates

After the first setup, use:

```sh
bin/ai-sync
```

`ai-sync` pulls repo updates and heals safe symlink drift. Re-run `bin/ai-setup --shims status`
after moving the checkout, because the mise drop-in stores an absolute path.
