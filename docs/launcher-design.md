# Launcher Design: Shell Functions, Not PATH Shims

Status: landed 2026-08-04. Replaces the global PATH-shim interception generation.

## The problem this retires

Hand-typed `claude`/`codex`/`grok` must route through `bin/herder launch` so agents bind to
the hcom bus at birth and carry their autonomous flags. The previous design intercepted the
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
`tools/herder/tests/check-launchers.sh` / `check-launcher-doctor.sh`.

## The design

Two independent name resolutions existed; both are now deterministic:

1. **Interactive hand-typed launch** — `claude`/`codex`/`grok` are shell **functions**
   (`lib/launchers.sh`), installed by a managed rc block (`bin/ai-setup --rc`,
   `lib/shell-rc.sh`). A function wins name resolution over every PATH participant,
   unconditionally; mise can reorder PATH all day. Default autonomous flags are baked into
   the functions with `HERDER_SHIM_ARGS_*` env override (empty string = ask mode), so a
   shell where mise env never applied still launches correctly. A missing checkout fails
   loud (rc 127), never falls back to a raw launch.
2. **Inside `herder launch`** — hcom resolves the tool by bare name from its inherited
   PATH. `tools/herder/internal/launchcmd/vendor.go` resolves the vendor binary once with a
   skip-list (never a `herder-path-shim`-marked file, never anything mise-owned — that
   includes npm copies inside `mise/installs/node/*/bin`), materializes
   `~/.cache/herder/vendorbin/<tool>/<tool> -> vendor`, and fronts that single-entry dir on
   the **child** PATH only. The print bypass (`claude -p`) uses the same resolver.

Non-interactive callers (scripts, spawners) invoke `"$AI_CONFIG_ROOT/bin/herder" launch`
explicitly — `herder spawn` already does, by absolute path. `command claude` remains the
deliberate raw-vendor escape hatch.

## What stays and why

- `tools/herder/shims/` still exists but is OFF the global PATH. The spawner injects it
  per-spawn for the `hcom` shim (hook-rewrite seam) — a launch-scoped, single-writer PATH
  edit in a child env, which is fine. The claude/codex/grok shim files remain only for
  machines that have not re-run `ai-setup`; they are transition compatibility, deletable
  once the fleet is migrated.
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
- `tools/herder/tests/check-launcher-doctor.sh` — doctor findings in both broken and
  healthy fixtures, duplicate-vendor detection.
- `tools/herder/internal/launchcmd/vendor_test.go` — resolver skip-list, pin dir
  retargeting, child-PATH prepend.
