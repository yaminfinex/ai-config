# Machine Setup

Canonical bring-up for a new machine using this repo.

## Prerequisites

- `git`
- `mise` (install from <https://mise.jdx.dev/>)
- `herdr` — the terminal/session server the fleet lifecycle drives (pane placement, worktree
  panes, managed close) and the `herder` display cache observes. **Baseline: 0.8.0.** Unlike
  `hcom`, herdr is *not* managed by `ai-setup`/mise; install it out-of-band via its own
  installer and keep it current with `herdr update --handoff`. See `docs/herdr-upgrade.md`.
  (`hcom`, the message-bus dependency, *is* installed and pinned by `ai-setup` — see Setup
  below.)

`ai-setup` requires mise. It writes the `bin/` PATH entry through mise `conf.d`, and installs
one managed rc block in `~/.bashrc` (and `~/.zshrc` when present) that defines the interactive
agent launcher functions — see Setup below.

## Clone

```sh
mkdir -p "$HOME/Coding"
git clone <repo-url> "$HOME/Coding/ai-config"
cd "$HOME/Coding/ai-config"
```

## Setup

Install the vendor agent CLIs first — **never via npm/mise** (a copy inside a mise-managed
node install shadows the vendor binary and launches raw, off-bus agents; `ai-doctor` flags
such copies):

- `claude` — the vendor installer, landing in `~/.local/bin/claude`
- `codex` — the vendor binary in `~/.local/bin/codex`
- `grok` — optional; fleet support is retired, the launcher function is only a direct
  vendor passthrough (the Grok Build installer, `~/.grok/bin`)

Then preview and run setup:

```sh
bin/ai-setup --dry-run
bin/ai-setup
```

This installs portable config links, skill links, and two pieces of launch machinery:

1. `${XDG_CONFIG_HOME:-$HOME/.config}/mise/conf.d/ai-config.toml` — puts `<checkout>/bin`
   (only) on the mise-managed PATH. Retired herder shims are never installed on global PATH.
2. A managed rc block in `~/.bashrc` (and `~/.zshrc` if present) sourcing `lib/launchers.sh`,
   which defines `claude`, `codex`, and `grok` as shell FUNCTIONS that resolve and execute
   the vendor CLI directly. Functions win name resolution over every PATH entry, so no PATH
   writer — mise hook-env rewriting order on `cd`, an installer prepend, stale inherited
   env — can reroute a hand-typed launch. Manage it with `bin/ai-setup --rc status|install|remove`.
   See `docs/launcher-design.md` for why interception moved off PATH entirely.

The functions resolve the absolute vendor binary once with a skip-list (never a retired
herder shim, never anything mise-owned, including symlink targets), then exec it without a
second PATH lookup. Global hooks register Claude/Codex with hcom and herdr automatically;
Grok fleet support is retired, so its function is only a direct vendor launcher.

The conf.d file sets `HERDER_SHIM_ARGS_CLAUDE` / `HERDER_SHIM_ARGS_CODEX`, overriding the
launcher functions' baked default flags. Export them empty for an ask-mode machine (the
functions bake autonomous defaults, so deleting the lines alone is not enough).

Bypass the function's resolver and default flags deliberately with `command claude ...`.

It also declares `hcom` as a managed mise tool (`[tools] "github:aannoo/hcom"`) and installs it —
hcom is a hard dependency of the fleet wrapper and surviving bus substrate.
The `github:` backend pulls the prebuilt, attestation-verified release binary; no brew or compile.
Pinned for reproducibility — bump the version in `lib/mise-path.sh`. (Homebrew
`brew install aannoo/hcom/hcom`, the `hcom-installer.sh` script, and `uv tool install hcom` also
work, but sit outside mise's management.)

Restart the shell after setup so the launcher functions load and `ai-setup`, `ai-doctor`,
`herder`, and `hcom` resolve from the managed PATH entry.

## hcom Hooks (required for fleet seats)

hcom hooks are explicit machine config, layered on top of the hcom binary that `ai-setup` already
installed above. Post-teardown they are the ONLY path by which a launched agent self-registers on
the bus — a machine without them runs raw, off-bus agents. Install them separately:

```sh
bin/ai-setup --hcom-hooks status
bin/ai-setup --hcom-hooks install
```

## Fleet wiring (bus machines)

Three machine-side registrations complete a fleet-capable box. All are idempotent; re-run
the first after moving the checkout (it stores absolute paths).

1. **The managed terminal preset** — registers `[terminal.presets.fleet]` in
   `~/.hcom/config.toml` (never as the box default; `tools/fleet/spawn.sh` selects it per
   launch). Without it, placed spawns fail:

   ```sh
   tools/fleet/preset-install.sh
   ```

2. **The lifecycle doctrine note** — the one-time bootstrap note every hcom-launched agent
   reads at boot. Point it at this checkout's drop-in context doc (run from the checkout
   root):

   ```sh
   hcom config notes "Lifecycle doctrine: spawn/cull peers via $PWD/tools/fleet (spawn.sh / cull.sh / selfcompact.sh); messaging, compact-inject, resume (hcom r), fork (hcom f) via hcom; placement via herdr. herder is display-cache ONLY (list + observer) — never a lifecycle gateway. Full doctrine: $PWD/docs/session-context-fleet.md"
   ```

3. **herdr's claude session registration** — the SessionStart hook that reports claude
   session ids to herdr, the display cache's session evidence:

   ```sh
   herdr integration install claude
   ```

   Note `herdr integration status` verifies the hook script file only, NOT the settings
   registration; the battery's `tools/herder/tests/check-live-contract.sh` pins the live
   registration itself (the 2026-08-21 silent settings-wipe class).

## Verify

Run:

```sh
bin/ai-doctor
type -a herder claude codex hcom
tools/fleet/spawn.sh codex --tag smoke --workspace <workspace-id> \
  --prompt 'Reply exactly PONG MACHINE-SETUP, then wait idle.'
```

`type -t claude` (and `codex`, `grok`) must print `function` in a fresh interactive shell;
`type -a herder` should show this repo's `bin/herder` first. `ai-doctor` runs the same probes
in a fresh login shell (so an already-corrected seat can't mask a broken terminal), flags
mise-owned duplicate vendor CLIs, and warns if the mise conf.d file is missing, unmanaged, or
still fronting the shims dir globally (the pre-function generation — rerun `bin/ai-setup`).

Stale `alias claude=...` or hand-rolled functions in rc files load AFTER the managed block if
they appear later in the file and would win — `ai-doctor`'s login-shell check catches the
symptom; remove the stale definition.

PATH-order shadowing (the old failure class: mise hook-env re-fronting its shims dir on every
config-boundary `cd`, beating any rc-file ordering fix) is retired by design: functions do not
participate in PATH resolution. No `hook-env --force` replay lines are needed; delete them if
a machine still carries one.

## Updates

After the first setup, use:

```sh
bin/ai-sync
```

`ai-sync` pulls repo updates and heals safe symlink drift. Re-run `bin/ai-setup --shims status`
after moving the checkout, because the mise drop-in stores absolute paths.
