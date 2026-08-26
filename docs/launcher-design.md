# Launcher Design: Shell Functions Routing On-Bus Through hcom

Status: revised 2026-08-26 (hand-typed launches route through `hcom <tool> --run-here`).
Previous revisions: 2026-08-04 (direct vendor exec via shell functions, replacing the
global PATH-shim interception generation); 2026-08-24 (teardown briefly dropped hcom
routing on a false assumption — see lesson 2).

## The problem this solves

Hand-typed `claude`/`codex` must (a) resolve the intended vendor binary despite PATH
rewrites, (b) carry their autonomous flags, and (c) land ON the hcom bus with a real
delivery channel. All three have failed independently in the past; the lessons ledger
below is the accumulated cost. The design is: shell functions own name resolution and
vendor identification, hcom owns bus registration and wrapping, and the function hands
the resolved vendor to hcom through a child-PATH pin.

## The design (`lib/launchers.sh`)

`claude`/`codex`/`grok` are shell **functions**, installed by a managed rc block
(`bin/ai-setup --rc`, `lib/shell-rc.sh`). A function wins name resolution over every
PATH participant unconditionally; mise can reorder PATH all day.

Per launch, the function:

1. **Resolves the vendor** by walking PATH once with a skip-list: never a
   `herder-path-shim`-marked file, never anything mise-owned (including symlink targets
   and npm copies in `mise/installs/node/*/bin`). Missing vendor fails loud (rc 127).
2. **Picks the path.** Grok and `claude -p/--print` one-shots exec the vendor directly
   (lessons 3 and 6). Everything else goes on-bus: exec `hcom <tool> --run-here --go
   <default-args> <caller-args>` — hcom creates the identity row, wraps the process
   with a real delivery channel, and runs it in the **current terminal**. `--go` is
   required: without it hcom 0.7.25 prints a launch preview and exits 0 silently.
3. **Pins the vendor for hcom's own lookup**: a single-symlink dir
   (`~/.cache/ai-config/vendorbin/<tool>`) fronted on the **child** PATH only, so
   hcom's bare-name resolution has exactly one deterministic answer (lesson 4).
4. **Scrubs ambient identity** in the child env on every path: all `HCOM_*`,
   `HERDER_*`, `HERDR_*` unset (keeping `HCOM_DIR` — bus location, not identity),
   `HCOM_LAUNCH_INFLIGHT=1` set (lesson 5).
5. **Never falls back to a raw off-bus launch** when hcom is missing — it fails loud
   and names the deliberate bypass (`command claude ...`).

Default autonomous flags are baked into the functions with `HERDER_SHIM_ARGS_*` env
override (empty string = ask mode), so a shell where mise env never applied still
launches correctly. Fleet spawners use `tools/fleet/spawn.sh` (new pane via the fleet
terminal preset); the launcher functions are the current-pane equivalent.

## Lessons ledger — why each piece is load-bearing

1. **PATH shims lose the ordering race** (2026-07, retired 2026-08-04). PATH is a
   global mutable variable with concurrent writers: mise hook-env rewrites order on
   every config-boundary `cd`, rc files and installers prepend, pane servers hand down
   stale `__MISE_*` env. Any writer winning once produced a raw vendor claude —
   off-bus, without `--dangerously-skip-permissions`. Every fix was another PATH
   writer, i.e. another race participant. Only shell functions win unconditionally.
2. **Hooks cannot register a session; neither can `hcom start`** (2026-08-26 incident).
   hcom ≥ 0.7.24 fail-closes its sessionstart hook for unknown sessions — identity
   rows come from launching *through* hcom, never inferred from a hook call
   (`docs/hcom-upgrade.md` gotchas). The 2026-08-24 teardown dropped hcom routing from
   the launchers on the assumption "global hooks bind automatically" — written the day
   after the 0.7.25 pin made it false. Result: every hand-typed session off-bus, and
   worse, `hcom start` from inside a raw session "joins" by making the **stop hook
   block up to `HCOM_TIMEOUT` (24h)** waiting for messages — the session hangs at end
   of turn. `hcom start --as <name>` did not cure a hung seat either. There is no
   raw-then-join path; on-bus means launched through hcom.
3. **`claude -p` through hcom never returns** (task-010). hcom hard-codes `-p/--print`
   as its background switch: stdin nulled, stdout to hcom logs, Stop hook polling up
   to a day. Print one-shots must exec the vendor directly. Codex is unaffected
   (`-p` means `--profile` there; `codex exec` stays on the hcom path).
4. **mise shims are undetectable imposters to bare-name lookup** (task-292). A mise
   shim chosen as "real claude" caused an infinite exec ping-pong (pane frozen at the
   startup banner). hcom resolves the tool by bare name from its inherited PATH, so
   the launcher must pin the already-resolved vendor on the child PATH. The pin
   symlink targets the stable entry point (`~/.local/bin/claude`), so vendor
   self-updates never invalidate it; it is re-verified every launch. The *retired*
   pin cache at `~/.cache/herder/vendorbin` is flagged by ai-doctor
   (`check_retired_vendor_pin_cache`); the live one is `~/.cache/ai-config/vendorbin`.
5. **Vendor CLIs inherit and hijack caller identity**
   (`docs/hazards/agent-cli-identity-hijack.md`, 2026-07-15; task-244). Any child
   inheriting `HCOM_PROCESS_ID` etc. from an identity-bearing shell takes over the
   caller's live bus row and deletes it on exit. The launcher scrubs
   `HCOM_*`/`HERDER_*`/`HERDR_*` (preserving `HCOM_DIR`) in the child env on every
   path, including the direct-exec ones.
6. **Grok stays raw** — fleet Grok support was retired; there is no hcom family for it.
7. **`--run-here` strands forever if shell init fails inside hcom's wrapper**
   (task-258, task-029 — upstream defect, no launch-phase timeout). Managed spawns
   hit this fleet-wide once via mise trust errors. For hand-typed launches the pane
   is visible and interruptible; know the signature: silent pane, no bus row bind.

## What stays and why

- The retired `tools/herder/shims/` files are absent and remain off global PATH.
- conf.d `_.path` still fronts `<checkout>/bin` (herder/mish). Bare-name bin
  wrappers stay PATH-resolved; `ai-doctor`'s login-shell check watches for shadowing.
- `command claude` remains the deliberate escape hatch: raw vendor, no hcom, no
  default flags, no resolver.

## Invariants (doctor-enforced)

- A fresh login shell defines the three launcher functions (`bin/ai-doctor`
  `check_login_shell_resolution`).
- Vendor CLIs exist exactly once, outside mise (`check_vendor_agent_duplicates`); install
  via vendor installers into `~/.local/bin`, never npm-in-mise-node.
- conf.d no longer fronts the shims dir globally (leftover = warning, rerun `ai-setup`).
- No lingering retired pin cache at `~/.cache/herder/vendorbin`
  (`check_retired_vendor_pin_cache`).

## Contracts

- `tools/herder/tests/check-launchers.sh` — on-bus routing (`hcom <tool> --run-here`),
  child-PATH vendor pin, identity scrub with `HCOM_DIR` preservation, `claude -p`
  bypass, grok direct exec, PATH-imposter immunity, loud rc-127 failure with no
  fallback for both missing vendor and missing hcom.
