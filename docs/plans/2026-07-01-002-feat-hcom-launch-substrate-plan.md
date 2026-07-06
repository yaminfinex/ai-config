---
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
execution: code
supersedes: 2026-07-01-001-feat-hcom-messaging-substrate-plan.md
title: "feat: hcom-owned launch substrate for herder (validated by spikes)"
date: 2026-07-01
plan_depth: deep
---

# feat: hcom-owned launch substrate — herder launches *through* hcom

## Why this supersedes plan 001

Plan 001 (KTD4) kept herder ignorant of hcom ("herder launches, hcom is a pure bus"). Spikes proved
that's the source of all the pain: **binding a session to the bus is fundamentally the launcher's
job**, so keeping herder the raw launcher forced brittle workarounds (spawn-side `hcom start` =
name-only zombie; env `HCOM_LAUNCHED=1` = random name; SessionStart hook = killed the child;
agent-led `hcom start` = brittle slow-boot). KTD4 was decided without the user; it is retired.

## Validated architecture (all points spike-proven)

**Launch each agent via `hcom <tool> --run-here`, exec'd inside a login shell, using the real
(non-isolated) config dir.** Evidence:

- `hcom <tool> --run-here` launches the agent **in-place** in herder's own herdr pane; hcom does the
  bind → `bindings: hooks, pty`, reliably, for **both claude and codex** (codex delivery confirmed:
  `deliver:` ack → agent ran the injected command).
- **Login shell** (`$SHELL -lic`) sources PATH/mise/**auth** — hcom's own preset uses non-login
  `bash` and **breaks auth** (observed: re-login prompt, dead agent). Herder must stay the pane owner.
- **Real config dir** (don't let hcom's `--dir` isolate `CLAUDE_CONFIG_DIR`/`CODEX_HOME`): the
  isolated dir lacks the global bypass-permissions acceptance → a blocking modal. Real dir + **global
  hcom hooks** = auth + acceptance + hooks all present → no modal (validated: claude ran immediately).
- **Naming:** hcom owns identity at launch; environment-based instance-name pinning does NOT pin. Best control is
  `--tag <role>` → `<role>-<random>` (e.g. `phase-3-dune`). Herder **captures** the assigned name
  by launch-pane correlation, with tag+cwd+recency as fallback, into its registry; herder's GUID
  stays the internal handle; registry maps guid↔hcom-name. `--tag` also gives free fan-out (`@role-`).

```
 PATH shim claude → hcom claude --run-here   ← everything you launch joins the mesh
 PATH shim codex  → hcom codex  --run-here      (shipped as shims per D4, not aliases;
                                                 interactive shells already login+authed)
        │
 herder-spawn ── execs the SAME wrapper in its login-shell pane (not raw `exec claude`)
        ▼
 agent in a herdr pane, hcom-bound from birth, auth intact, tag-named
   herder owns: pane, GUID/registry, --notify ring, briefs
   hcom owns:   bind + identity (name)         herder captures the name
   herder-send: hcom (bound peer) | herdr keystroke (degraded fallback only)
```

## Requirements

- **R1** Launch every managed agent bound to the bus via `hcom <tool> --run-here` in a login shell,
  real config dir. Both claude + codex.
- **R2** herder-spawn keeps its surface (roles, GUID, tabs, `--notify`, briefs, registry) but
  delegates the process launch to the wrapper; captures the hcom name; retires spawn-side `hcom start`.
- **R3** Transport-agnostic command surface preserved (R1 of 001 still holds): no command/flag names
  hcom; `herder-send` dispatches hcom-bound-peer vs herdr keystroke.
- **R4** hcom is a **hard dependency** (D6): the wrapper **errors** if hcom is absent — no raw-tool
  degrade. The herdr keystroke driver (U7+B1–B5) survives ONLY as the transport for **non-bus
  targets** (plain `bash` panes, a peer in its boot window), not a whole-system fallback. No
  boot-window handshake.
- **R5** Global hcom hooks (claude+codex) are installed/removed by an explicit, documented step
  (ai-setup), with a saved reference — never an implicit side effect. (Hooks are config, not shell
  state — kept separate from the mise-managed shell layer, R7.)
- **R6** Identity: herder registry maps GUID ↔ captured hcom name; herder-send/list/wait/cull resolve
  through it. `--tag <role>` enables fan-out addressing. Name capture is by **launch-pane correlation**
  against `hcom list --json` (D3 as amended: `launch_context.pane_id` == the pane id frozen at launch,
  unique per spawn; tag+cwd+recency as best-effort fallback).
- **R7** Shell/env/PATH changes are **mise-managed, not hand-rolled** (D-mise): global mise `[env]`
  puts the shim dir on PATH and holds any static HCOM defaults; per-worktree `mise.toml` `[env]`
  supplies the optional `HCOM_DIR` isolation override (D1). Old hand-rolled `claude`/`codex`
  aliases/functions are removed. **hcom itself is a mise-managed tool** (`[tools] hcom = …`), making
  the R4/D6 hard-dependency declarative + reproducible instead of relying on an ambient brew install
  (verify a mise backend exists for hcom in W5; brew stays the fallback).

## Implementation units

Survives from 001 (keep as-is): **U6** driver interface, **U7** herdr driver + the B1–B5 send-path
fixes (the degraded transport), **U8** hcom driver, **U9** selection. `check-send-contract.sh` stays.

- **W1 — Launch wrapper (shipped; live-proven).** `hcom-launch <tool> [--tag T] [tool-args...]` →
  exec `hcom <tool> --run-here [--tag T] <tool-args>` in login shell; real config dir (no `--dir`
  isolation); forward bypass flags (`--dangerously-skip-permissions`). **hcom absent ⇒ error, no raw
  degrade (D6/R4).** **Recursion guard** (`HCOM_LAUNCH_INFLIGHT=1`) so `hcom claude` doesn't re-hit
  the W4 shim. **Config-dir passthrough** (per D1) pins `CLAUDE_CONFIG_DIR`/`CODEX_HOME`/`GEMINI_CLI_HOME`
  to the real dir only when `HCOM_DIR` is set and not `~/.hcom`, so an isolated `HCOM_DIR` ringfences
  the bus without breaking auth/hooks while the global bus leaves config env unset. Inherits
  `HCOM_DIR` from env untouched. Used by W2 + W4.
- **W2 — herder-spawn integration (shipped).** Launch via W1 with `--tag <role>`. **Team (D7):** resolve the
  child's team (`--team` → `$HERDER_TEAM` → global), compute `HCOM_DIR = $HERDER_TEAMS_ROOT/<team>`
  (or `~/.hcom` for global), **PIN it into the child's launch env**. **Capture** the hcom-assigned name
  by correlating in that HCOM_DIR (pane_id + dir/tag/recency; transient key only). **Store in registry:
  `team`, `hcom_dir`, `hcom_name`, `hcom_tag(role)`** (durable; not pane_id); report in `--json`.
  **Retire U10** (delete spawn-side `hcom start` / old `HCOM_DIR` injection / `--bus` join).
- **W3 — Identity resolution (shipped).** herder-send/list/wait/cull resolve GUID/label → registry
  `(team → hcom_dir, hcom_name)` → **scope the hcom op to that team's bus**
  (`HCOM_DIR=<hcom_dir> hcom send --from <orch> @<hcom_name>` — the proven cross-team external-send
  bridge, now automatic). herdr keystroke path resolves as today when the peer isn't bus-bound.
  `herder-list` shows a `team` column and can enumerate teams from `$HERDER_TEAMS_ROOT`.
- **W4 — Shell integration (shipped repo-side; machine activation deferred post-merge).** Shim dir holding `claude`/`codex` shims → W1; prepared machine changes put it on PATH
  via **mise `[env]` `_.path`** (global config), not hand-edited profiles, and remove the old `claude`
  alias + `codex` function after merge. Per-worktree `HCOM_DIR` isolation via worktree `mise.toml` `[env]` (D1).
- **W5 — Hook management (R5; shipped).** ai-setup installs/removes global hcom hooks for claude+codex
  (`~/.claude/settings.json`, codex `config.toml`); reference kept in `napkins/.../hook-reference/`.
  Separate from W4's mise shell layer — hooks are config, not shell state.
- **W6 — Docs (shipped).** Rewrite herder + orchestrate SKILL.md to this model (launch-through-hcom;
  capture-naming; degraded herdr fallback; Inv 8/9 keyed on the driver).

## Out of scope (unchanged from 001)
Turn arbitration; enforced write-contention (orchestrate Inv 7). hcom relay/cross-device. hcom
collision-ping as advisory.

## Decisions (resolved with user, 2026-07-01)
1. **D1 — Bus scope:** **global bus by default + optional `HCOM_DIR` override**, and the override is
   **mise-managed** — a worktree that wants isolation drops a `mise.toml` `[env] HCOM_DIR =
   "{{config_root}}/.hcom"` (auto-loaded, `/Users/yamen/Coding` already trusted).
   **Ringfencing model (spike-proven):** `team = HCOM_DIR bus` (hard isolation — user agents don't
   resolve/deliver across HCOM_DIRs; only hcom's system agent is global) · `role = --tag` (addressing
   /fan-out only, never crosses a bus) · `cross-team = an explicit HCOM_DIR-scoped external --from
   send` (no first-class cross-dir addressing; `relay` = cross-device). **Auth caveat + fix:** any
   `HCOM_DIR != ~/.hcom` puts hcom in "local mode" and derives per-tool config dirs from HCOM_DIR's
   PARENT (`CLAUDE_CONFIG_DIR=<parent>/.claude`, …) → fresh dir = re-login + lost settings + no hooks
   bind. **W1 hcom-launch pins the real config dir only when `HCOM_DIR` is set and not `~/.hcom`**
   (`CLAUDE_CONFIG_DIR:-$HOME/.claude`, codex→`CODEX_HOME`, gemini→`GEMINI_CLI_HOME`; hcom passes a
   pre-set value through) so bus location and config dir are independent axes — isolated bus + real
   auth + hooks bind. On the global bus, it leaves config env unset to avoid the Claude set-vs-unset
   onboarding bug.
2. **D2 — Orchestrator on the mesh:** **yes** — orchestrators are launched via the wrapper too, so
   `--notify` rings arrive over hcom (no keystroke-to-pane ring fallback).
3. **D3 — Name capture (amended at W2 to match buildable reality):** correlation against `hcom list
   --json` by **launch-pane id** (`launch_context.pane_id` == the pane id frozen into the child's env
   at launch — unique per spawn, immune to later pane-id compaction), with tag+cwd+recency as a
   best-effort fallback only. The original child-session correlation lean is unimplementable from the spawner:
   herder cannot know the child's session id pre-launch (the tool generates it). NOT `Names:` scraping.
4. **D4 — Wrapper form:** **PATH shim script** named `claude`/`codex` (covers herder-spawn's `exec`
   and manual launches uniformly). Its dir is put on PATH by mise (R7), not a hand-edited profile.
5. **D5 — Codex flags:** **CLI tool-args through the wrapper** (uniform with claude; per-spawn
   control). Any static default may live in mise `[env]`, but the mechanism is positional tool-args.
6. **D6 — hcom required (supersedes "degraded parity"):** hcom is a **hard dependency** (R4). Wrapper
   errors if hcom absent (no raw-tool degrade). Keystroke driver kept ONLY for non-bus bash panes /
   boot-window — not a system-wide fallback.
- **D-mise — Shell managed by mise (R7):** PATH shim dir + env via mise `[env]`, not hand-rolled
  aliases/functions. Removes the existing `claude` alias + `codex` function.
- **D7 — Team model (friendly, disk-discoverable):** a **team = a named subdir under a teams root**.
  `HERDER_TEAMS_ROOT` env, **default `~/.hcom/teams`**; **`HCOM_DIR = $HERDER_TEAMS_ROOT/<team>`**
  (pure function: team name → bus). **No team → the global `~/.hcom` bus** (frictionless default for
  standing cross-repo agents; needs no flag). Team chosen by `--team <name>` (explicit) → else inherit
  `$HERDER_TEAM` (orchestrator's team, so a whole run stays on one bus) → else global. Team names are
  validated as safe single path segments (no `/`, spaces). This is the **primary ringfencing lever**
  and SUPERSEDES the earlier mise-per-worktree raw-`HCOM_DIR` idea (mise may still set
  `HERDER_TEAM=<team>` per worktree — friendlier than a raw path). Auth-safe by construction via the
  W1 config-dir passthrough (every team dir trips hcom "local mode"; the passthrough keeps auth real).
  **herder-spawn resolves + PINS the child's `HCOM_DIR` at launch** and records `team` + resolved
  `hcom_dir` + `hcom_name` + `hcom_tag(role)`; W3 send/list/wait/cull scope by the recorded team bus.
- **D8 — Cull bus-drop:** `herder-cull` best-effort drops a culled hcom peer's bus entry with
  `hcom kill <hcom_name>` scoped to the recorded `hcom_dir`; failures are advisory and never fail cull.

## Open follow-ups (non-blocking)
- **W1 recursion guard:** shim `claude` calls `hcom claude` — must not re-resolve `claude` back to the
  shim. Guard via env sentinel (e.g. `HCOM_LAUNCH_INFLIGHT=1` → shim execs the real binary) or pass
  hcom the real binary path. Shim must also forward existing bypass flags (`--dangerously-skip-permissions`).
