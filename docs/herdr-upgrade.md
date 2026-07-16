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
   a2. `bash tools/herder/tests/check-live-contract.sh` — required live substrate
      contract tier. It must pass the herdr agent-list envelope, API schema snapshot,
      and socket `session.snapshot` nested-shape checks against the installed binary.
      A visible skip is acceptable only on machines without a running herdr server.
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

## What the 0.7.3 → 0.7.4 handoff actually did (2026-07-16)

Much gentler than 0.6.10 → 0.7.3 — the upstream handoff fixes (socket-path
preservation, slow-shutdown wait, response flush) held:

1. **Pane ids were STABLE** — no coordinate reissue. **Terminal ids were reissued**
   (same scheme, new values), which is what broke agent detection, not pane keys.
2. **Occupants survived cleanly.** A pre-handoff bash worker ticking every 30s showed
   zero gap across the swap.
3. **Every live session went detection-lost** (hook reports predate the new server),
   and `reconcile --apply` records those rows **unseated** — the dormant default.
   Recovery per session, from its OWN pane:
   `(cd <repo> && HCOM_SESSION_ID=<sid> HERDER_GUID=<guid> herder enroll)` —
   the same-guid repair re-seats and re-verifies the bus name. Until the repair-path
   label/role preservation fix ships, follow with `herder rename <guid> <label>`.
   Sessions whose function is bus-only can defer re-seating to a natural boundary.
4. Codex workers with name+kind+cwd matches were **auto-re-bound** by reconcile
   (D12 assumed-continuity) — no manual action.
5. **Fork's 0.7.3 crash shape did not reproduce**: forking a session without a
   recorded tool_session_id now refuses typed ("nothing to fork from"). The full
   fork path (live sid parent) was not exercised this round.
6. The api schema changed without a protocol bump (still 16): metadata `tokens`
   replaced `custom_status`, popup-pane and graphics params added. Only the
   schema-drift golden failed in the live-contract tier; grep confirmed no herder
   code touched removed fields. Update the golden + parsing in one change.
7. **Observer generation recovery works**: the running daemon detected
   "server is shutting down", retried on its steady 30s loop without crashing, and
   re-established sweeps against the new server on its own.

## Controlled restart drill (repeatable procedure)

Run this at every herdr upgrade (it was proven live on the 0.7.4 handoff); it
doubles as the recovery drill for unplanned restarts.

Setup (before the restart):
1. Main green and pushed; board committed; note `herdr --version`.
2. Warn the fleet on the bus: hold identity-bearing herder verbs; transient
   `herder list` output is not ground truth until ALL CLEAR.
3. Spawn a disposable ticking worker as the survival specimen:
   `herder spawn --role drill --agent bash --prompt 'while true; do sleep 30; echo TICK $(date +%H:%M:%S); done'`
4. Snapshot `herder list --all` and the drill pane's coordinates.

Restart: `herdr update --handoff` (occupants survive; cold restart kills them).

Reconciliation and gate (in order):
1. `herdr --version` — confirm the jump.
2. `hcom list` — bus should be unaffected (different substrate).
3. `bash tools/herder/tests/check-live-contract.sh` + diff `herdr api schema --json`
   against the golden. Schema-drift-only failures are expected upgrade artifacts.
4. `herder reconcile` dry-run — READ the classifications (gone / undetected /
   re-bind), then `--apply`. Expect: undetected rows become unseated (dormant
   default), D12 matches re-bind automatically.
5. Verify the drill worker's pane ticked across the swap (read its pane; look for
   a gap at the handoff timestamp).
6. Spawn probe end-to-end: spawn a bash agent with an echo prompt, read the output
   from its pane, cull it.
7. Re-seat your own row (pinned enroll recipe above), restore label, then verify
   self-resolution via `herder list`.
8. ALL CLEAR to the fleet with the per-session re-seat recipe.

Success criteria: version jumped; bus never degraded; drill ticks unbroken; spawn
probe round-trips; reconcile classifications all explained (no ambiguity refusals
left unresolved); own row seated + bus-verified; no session lost except by choice.
