# Machine Setup

Canonical bring-up for a new machine using this repo.

## Prerequisites

- `git`
- `mise` (install from <https://mise.jdx.dev/>)

`ai-setup` requires mise. It writes PATH entries through mise `conf.d`; there is no shell rc
fallback.

## Clone

```sh
mkdir -p "$HOME/Coding"
git clone <repo-url> "$HOME/Coding/ai-config"
cd "$HOME/Coding/ai-config"
```

## Setup

Preview, then install portable config links, skill links, and the managed mise PATH file:

```sh
bin/ai-setup --dry-run
bin/ai-setup
```

This writes `${XDG_CONFIG_HOME:-$HOME/.config}/mise/conf.d/ai-config.toml` with absolute paths for:

- `<checkout>/bin`
- `<checkout>/tools/herder/shims`

Restart the shell after setup so `ai-setup`, `ai-doctor`, `herder`, `claude`, and `codex` resolve
from the managed PATH entries.

## Optional hcom Hooks

hcom hooks are explicit machine config. Install them separately:

```sh
bin/ai-setup --hcom-hooks status
bin/ai-setup --hcom-hooks install
```

## Verify

Run:

```sh
bin/ai-doctor
type -a herder claude codex
herder spawn --role smoke --agent codex --cwd "$PWD" \
  --prompt 'Reply exactly PONG MACHINE-SETUP, then wait idle.'
```

`type -a` should show this repo's `bin/herder` before any other `herder`, and this repo's
`tools/herder/shims/{claude,codex}` before real tool binaries. `ai-doctor` warns if the mise file
is missing, unmanaged, incomplete, duplicated on PATH, or shadowed by aliases/functions/earlier
executables.

Aliases and shell functions beat PATH shims. Find old overrides with:

```sh
grep -RInE '^[[:space:]]*(alias[[:space:]]+(claude|codex|herder)=|(function[[:space:]]+)?(claude|codex|herder)[[:space:]]*\(\))' \
  "$HOME/.zshrc" "$HOME/.zshenv" "$HOME/.zprofile" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.config" 2>/dev/null
```

Remove or rename any `herder`, `claude`, or `codex` alias or function that should no longer shadow
the managed paths.

## Updates

After the first setup, use:

```sh
bin/ai-sync
```

`ai-sync` pulls repo updates and heals safe symlink drift. Re-run `bin/ai-setup --shims status`
after moving the checkout, because the mise drop-in stores absolute paths.
