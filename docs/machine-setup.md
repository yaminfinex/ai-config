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

The same file sets `HERDER_SHIM_ARGS_CLAUDE` / `HERDER_SHIM_ARGS_CODEX` so hand-typed `claude`
and `codex` skip permission prompts by default (the shims prepend these before user args).
Delete those two lines locally for an ask-mode machine — but note `ai-setup` restores them on
the next run.

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

**PATH-order shadowing (interactive shells).** If mise activation runs in `~/.zshenv`, any PATH
prepends later in `~/.zshrc` (`~/.local/bin`, nvm, pnpm, …) land *ahead* of the managed entries —
`ai-doctor` then reports `claude: shadowed before expected` even with no alias in sight. mise does
not re-assert ordering on its own; fix it by force-reapplying the managed env as the **last** line
of `~/.zshrc`:

```sh
# Re-assert mise-managed PATH entries (conf.d _.path) after the prepends above.
eval "$(mise hook-env -s zsh --force 2>/dev/null)"
```

No repo paths are hardcoded — the mise conf.d file stays the single source of truth; this line
just replays it after everything else has had its say. Verify with `type -a claude` in a fresh
shell.

## Updates

After the first setup, use:

```sh
bin/ai-sync
```

`ai-sync` pulls repo updates and heals safe symlink drift. Re-run `bin/ai-setup --shims status`
after moving the checkout, because the mise drop-in stores absolute paths.
