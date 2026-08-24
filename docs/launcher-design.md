# Launcher Design: Direct Vendor Shell Functions, Not PATH Shims

Status: landed 2026-08-04. Replaces the global PATH-shim interception generation.

## The problem this retires

Hand-typed `claude`/`codex`/`grok` must resolve the intended vendor binary despite PATH
rewrites and carry their autonomous flags. The previous design intercepted the
bare names with `tools/herder/shims/` fronted on the machine-wide PATH (mise conf.d `_.path`,
plus rc-file re-fronting blocks as that eroded).

That design lost a PATH-ordering race it could not win. PATH is a global mutable variable
with concurrent writers: mise hook-env (which *rewrites the order on every config-boundary
`cd`* and promotes its own shims dir to front), rc files, installer prepends, and pane
servers handing down stale environments (`__MISE_*` diffs included). Any writer winning once
produced the observed failure: a raw vendor claude, off-bus, without
`--dangerously-skip-permissions` — worst on `cd` into a project with its own mise config,
where mise's auto-regenerated `claude` shim dispatched to an accidental npm copy inside a
node install. Every fix was another PATH writer that re-asserted order later, i.e. another
race participant. See the repro and post-mortem trail in this doc's history and
`tools/herder/tests/check-launchers.sh` and this document's history.

## The design

One deterministic name resolution remains:

1. **Interactive hand-typed launch** — `claude`/`codex`/`grok` are shell **functions**
   (`lib/launchers.sh`), installed by a managed rc block (`bin/ai-setup --rc`,
   `lib/shell-rc.sh`). A function wins name resolution over every PATH participant,
   unconditionally; mise can reorder PATH all day. Default autonomous flags are baked into
   the functions with `HERDER_SHIM_ARGS_*` env override (empty string = ask mode), so a
   shell where mise env never applied still launches correctly. The function walks PATH
   once with a skip-list (never a `herder-path-shim`-marked file, never anything mise-owned,
   including symlink targets and npm copies in `mise/installs/node/*/bin`) and execs the
   absolute vendor entry point in a child process. A missing vendor fails loud (rc 127).
   Claude and Codex join hcom through their global hooks and bind to herdr through its
   integration; the launcher adds no registration layer. Grok is a direct unsupported
   vendor launch because fleet Grok support was retired.

Fleet spawners use `tools/fleet/spawn.sh`, which launches through hcom's managed terminal
preset. `command claude` remains the deliberate escape hatch that bypasses the function's
default flags and resolver.

## What stays and why

- The retired `tools/herder/shims/` files are absent and remain off global PATH.
- conf.d `_.path` still fronts `<checkout>/bin` (herder/mish/bottle). Bare-name bin
  wrappers stay PATH-resolved; `ai-doctor`'s login-shell check watches for shadowing.

## Invariants (doctor-enforced)

- A fresh login shell defines the three launcher functions (`bin/ai-doctor`
  `check_login_shell_resolution`).
- Vendor CLIs exist exactly once, outside mise (`check_vendor_agent_duplicates`); install
  via vendor installers into `~/.local/bin`, never npm-in-mise-node.
- conf.d no longer fronts the shims dir globally (leftover = warning, rerun `ai-setup`).

## Contracts

- `tools/herder/tests/check-launchers.sh` — function behavior, PATH-imposter immunity,
  loud failure without fallback.
