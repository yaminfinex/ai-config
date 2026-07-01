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
- **Naming:** hcom owns identity at launch; `HCOM_INSTANCE_NAME` does NOT pin. Best control is
  `--tag <role>` → `<role>-<random>` (e.g. `phase-3-dune`). Herder **captures** the assigned name
  (launch `Names:` line / `hcom list` by session_id) into its registry; herder's GUID stays the
  internal handle; registry maps guid↔hcom-name. `--tag` also gives free fan-out (`@role-`).

```
 alias claude = hcom claude --run-here   ← everything you launch joins the mesh
 alias codex  = hcom codex  --run-here      (interactive shells already login+authed)
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
- **R4** hcom assumed present; herdr keystroke is the **degraded fallback** (hcom absent → wrapper
  execs the tool directly; herder-send falls back to keystrokes). No boot-window handshake.
- **R5** Global hcom hooks (claude+codex) are installed/removed by an explicit, documented step
  (ai-setup), with a saved reference — never an implicit side effect.
- **R6** Identity: herder registry maps GUID ↔ captured hcom name; herder-send/list/wait/cull resolve
  through it. `--tag <role>` enables fan-out addressing.

## Implementation units

Survives from 001 (keep as-is): **U6** driver interface, **U7** herdr driver + the B1–B5 send-path
fixes (the degraded transport), **U8** hcom driver, **U9** selection. `check-send-contract.sh` stays.

- **W1 — Launch wrapper.** `hcom-launch <tool> [--tag T] [tool-args...]` → exec `hcom <tool>
  --run-here` in login shell; real config dir (no `--dir` isolation); forward bypass flags; degrade
  to raw tool if hcom absent. Used by W2 + W4.
- **W2 — herder-spawn integration.** Launch via W1 with `--tag <role>`; capture hcom-assigned name
  (parse `Names:` / correlate by session_id); store in registry (guid↔name); report in `--json`.
  **Retire U10** (delete spawn-side `hcom start` / `HCOM_DIR` injection / `--bus` join).
- **W3 — Identity resolution.** herder-send/list/wait/cull resolve GUID/label → captured hcom name →
  hcom; herdr keystroke path resolves as today when the peer isn't bus-bound.
- **W4 — Shell aliases.** `claude`/`codex` → W1 (via ai-setup / shell profile), so manual launches
  also join the mesh.
- **W5 — Hook management (R5).** ai-setup installs/removes global hcom hooks for claude+codex;
  reference kept in `napkins/.../hook-reference/`.
- **W6 — Docs.** Rewrite herder + orchestrate SKILL.md to this model (launch-through-hcom;
  capture-naming; degraded herdr fallback; Inv 8/9 keyed on the driver).

## Out of scope (unchanged from 001)
Turn arbitration; enforced write-contention (orchestrate Inv 7). hcom relay/cross-device. hcom
collision-ping as advisory.

## Unresolved questions
1. **Bus scope:** global bus (simplest, matches "hcom is global") vs per-worktree `HCOM_DIR`
   isolation (composes with Inv 7). Lean: global default + optional `HCOM_DIR` override for isolated
   runs. Decide who sets it (wrapper env).
2. **Orchestrator on the mesh?** For `--notify` rings to arrive over hcom, the orchestrator must be a
   bound instance (launched via the wrapper). Else rings fall back to herdr keystroke to its pane.
   Confirm we launch orchestrators via the wrapper too.
3. **Name capture robustness:** parse launch `Names:` vs query `hcom list --json` by session_id/tag/
   dir. Which is race-free when N spawns land together? (session_id correlation looks safest.)
4. **Wrapper form:** PATH shim script named `claude`/`codex` (covers herder-spawn's `exec claude`
   too) vs shell alias (interactive only, herder-spawn calls wrapper explicitly). Trade-off:
   uniformity vs invasiveness.
5. **Codex flags:** bypass/sandbox via `HCOM_CODEX_ARGS` vs CLI tool-args through the wrapper.
6. **Degraded mode parity:** confirm `herder-send`/orchestrate behave exactly as today when hcom is
   absent (R4) — the 001 driver + golden tests should already cover this.
