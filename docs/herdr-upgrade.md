# herdr upgrade runbook

How to move this machine to a new herdr release without stranding the herder registry or
the live fleet. Written after the 0.6.10 → 0.7.3 `herdr update --handoff` (2026-07-08);
shaped by what actually broke. Companion: `docs/hcom-upgrade.md` (different substrate,
different failure modes — an hcom upgrade breaks hooks/binding; a herdr upgrade breaks
coordinates/detection).

## Ownership model

- herdr is a self-updating binary at `~/.local/bin/herdr`; upgrades go through
  `herdr update [--handoff]`. It is NOT mise-pinned (unlike hcom) — there is no repo pin
  to bump, which also means nothing in the repo tells you the version changed. Record the
  before/after versions in the run log when you upgrade.
- `--handoff` performs a live server handoff: **panes and agent processes survive**, but
  substrate identity does not (see below). A cold restart kills occupants; reconciliation
  handles both without knowing which happened (spec §8.3, AC-23/24).

## What the 0.6.10 → 0.7.3 handoff actually broke

1. **Every pre-handoff registry coordinate went dead.** The new server reissued terminal
   ids in a new scheme (`term_`+16hex → `term_`+13hex) and changed the pane-id scheme
   (`w…-N` → `w…:pN`). Rows keyed on old coordinates could not resolve even for agents
   that were alive and visible (TASK-046). `herder send/wait` to pre-handoff agents failed;
   `herder list` showed LIVE=gone fleet-wide.
2. **Surviving pre-handoff processes became detection-lost.** Their hook reports never
   re-reach the new server: absent from `agent list`, pane `agent_status=unknown`, so
   `herder wait` hangs at `status=unknown` forever. The ONLY recovery is restarting the
   agent process (same shape as the hcom stale-PATH gotcha). Upstream gap — server-side
   re-adoption without a fresh report is unfiled upstream (tracked via TASK-029).
3. **`herder fork` native path died** ("launch failed before lifecycle bind", pane exits
   instantly; TASK-051, open). Workaround: `herder spawn --extra-arg --resume
   --extra-arg <session-id> --extra-arg --fork-session`.
4. **"Stable ids" has a precise meaning:** never-recycled, NOT immutable. pane/tab/
   workspace ids re-key when a pane moves ACROSS workspaces (same-workspace tab moves keep
   pane_id); terminal_id survives moves but is REISSUED at server handoff. Only the
   registry guid survives everything — which is the point of the herder-spec.
5. The orchestrator's own row went unresolvable until re-enrolled. Post-ratification the
   legal composite is: `herder enroll` (new guid) + `rename <new> <label> --take-from
   <old>` + `retire <old>` — never reuse a guid across transcripts (spec D1, TASK-042).

## Procedure for the next herdr upgrade

1. **Audit before updating.** Read the upstream release notes for every version being
   jumped. Diff `herdr api schema --json` (0.7.2+) against a saved golden — protocol
   version bumps and response-shape changes are herder's main exposure. Check specifically:
   pane/terminal id formats, `agent list` envelope and fields, `pane move` semantics,
   `wait agent-status` behaviour. File a board task per delta before touching the machine.
2. **Snapshot state.** Note current version (`herdr --version`), commit any pending board
   state, and make sure main is green — you want a clean baseline if the gate fails.
3. **Run `herdr update --handoff`** (occupants survive). Expect breakage classes 1–2 above
   regardless: coordinate reissue is apparently normal at handoff.
4. **Post-upgrade gate, immediately, in this order:**
   a. `herdr --version` — confirm the jump.
   b. `hcom list` — bus side should be UNAFFECTED (different substrate); if bus identity
      broke too, you have an hcom problem, see the other runbook.
   c. `herder reconcile` (dry-run) — review classifications, then `herder reconcile
      --apply`. Re-binds dead-keyed rows for live agents; `undetected` rows are
      detection-lost processes (class 2) — plan restarts for them at natural boundaries.
   d. `herder wait <your-own-label> --read` — proves self-resolution.
   e. Spawn probe: `herder spawn --role gate-probe --agent bash --split down --prompt
      'echo GATE_OK'`, read it, cull it. Proves spawn/inject/read/cull end-to-end.
   f. Fork probe: `herder fork --self --label gate-fork` + cull (currently expected to
      fail — TASK-051; remove this parenthetical when fixed).
   g. `herder list` — statuses should read true; anything `undetected` needs an agent
      restart, anything `gone` should be genuinely dead.
5. **Restart detection-lost agents** (class 2) at natural boundaries — sessions survive in
   the registry as unseated/undetected and re-seat via enroll/observation on restart.
6. **Record the delta** in the run log and file board tasks for anything new. If response
   shapes changed, update the api-schema golden and any herdrcli parsing + goldens in the
   same change.

## Prevention that is already in place

- `herder reconcile` (TASK-046, merged `a5e73fe`): the one-time migration tool for
  coordinate reissue — dry-run default, all-or-nothing `--apply`, refuses ambiguity.
- Liveness tri-state (`undetected` vs `gone`) stops the false-dead misreads.
- Sidecar sid self-reporting (TASK-053, merged `7d48494`): sids ride herdr's
  HandoffManifest, so post-handoff re-adoption gets a real key. Effective for spawns
  started AFTER it shipped; codex sids pending the upstream hcom hook fix (TASK-045/F3).
- `wait` now emits detection-lost guidance instead of a bare timeout.
