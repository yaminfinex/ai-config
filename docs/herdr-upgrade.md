# herdr upgrade runbook

> **Doctrine flipped 2026-08-24.** The dated incident sections preserve the
> retired herder lifecycle commands as history. The live procedure and drill
> use hcom, herdr, `tools/fleet`, and herder's display-cache observer only.

How to move this machine to a new herdr release without stranding the live fleet or
corrupting herder's display cache. Written after the 0.6.10 → 0.7.3 `herdr update --handoff` (2026-07-08);
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

## What the 0.7.5 → 0.8.0 stable auto-update broke (2026-08-04)

Protocol 16 → 19 and a CLI verb re-organization. The subscription surface the observer
drives (events.subscribe request/ack, pane.* subscription variants, session.snapshot
envelope) survived byte-identical — the migration was pins plus three verb renames:

1. **`herdr agent send` retired** (socket method `agent.send` removed) — split into
   `agent prompt` (paste + submit + optional wait) and the surviving `pane send-text`
   (paste WITHOUT submit). herder's paste engine (`spawncmd/bootpaste.go`) moved to
   `pane send-text`; probed live: text sits on the composer line until the separate
   Enter leg submits, matching the old `agent send` exactly.
2. **Top-level `herdr wait` retired** — `wait agent-status <p> --status S` became
   `agent wait <p> --until S` (same single-state match + `--timeout`); `waitcmd` updated.
3. **`herdr session snapshot` CLI verb retired** — the CLI read moved to
   `herdr api snapshot`; same `{"result":{"snapshot":{…}}}` envelope. (The SOCKET method
   `session.snapshot` is unchanged; only the observer's CLI fallback needed the rename.)
4. Everything else herder drives (pane get/list/close/move/split/run/send-keys,
   report-agent(-session), agent list/get/read/rename/focus, workspace/worktree list,
   tab create) survived unchanged. Protocol pin lives at `observercmd/socket.go`
   (`supportedHerdrProtocol`); schema golden at
   `tests/goldens/live-contract/herdr-api-schema.json`.

The resident observer daemon self-degraded honestly (`protocol_compatible=false`, no
crash) from the 0.8.0 arrival until the pin bump rolled out — restart it on the new
build as part of the gate (step 4e).

## Procedure for the next herdr upgrade

1. **Audit before updating.** Read the upstream release notes for every version being
   jumped. Diff `herdr api schema --json` against the saved golden. Check pane/terminal
   identifiers, pane process-info, pane run/close, tab/worktree placement, integration
   hooks, and session snapshot/event subscription shapes. File a task per delta first.
2. **Snapshot state.** Note current version (`herdr --version`), commit any pending board
   state, snapshot `hcom list --json`, `herdr pane list`, and `herder list`, then
   make sure main is green.
3. **Notify the fleet.** Hold placement and pane-close actions during the handoff; hcom
   messaging may continue.
4. **Run `herdr update --handoff`** so occupants survive.
5. **Post-upgrade gate, immediately:** confirm `herdr --version`; run
   `tools/herder/tests/check-live-contract.sh`; compare `hcom list --json` and
   `herdr pane list` to the snapshots; run `herder list` and inspect every
   displayed join gap without treating the display as authority.
6. **Lifecycle smoke.** Use `tools/fleet/spawn.sh` to place one disposable Claude or
   Codex seat, verify an `hcom send` round trip, then `tools/fleet/cull.sh` it. Exercise
   `hcom r`/`hcom f` only when the release changed resume/fork or placement behavior.
7. **Restart detection-lost agents** at natural boundaries when herdr lost their session
   binding; hcom identity and transcript continuity remain the authority.
8. **Record the delta** in the run log and file board tasks for anything new. If response
   shapes changed, update the api-schema golden and any herdrcli parsing + goldens in the
   same change.

## Historical prevention before the 2026-08-24 teardown

> The commands in this section are retained only to explain the old incidents.

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
2. Warn the fleet on the bus: hold placement and pane-close actions; herder list
   remains display cache throughout.
3. Create a disposable herdr pane running a bounded shell ticker as the survival
   specimen; record its exact pane id and cleanup command.
4. Snapshot `hcom list --json`, `herdr pane list`, and the then-current herder display.

Restart: `herdr update --handoff` (occupants survive; cold restart kills them).

Reconciliation and gate (in order):
1. `herdr --version` — confirm the jump.
2. `hcom list` — bus should be unaffected (different substrate).
3. `bash tools/herder/tests/check-live-contract.sh` + diff `herdr api schema --json`
   against the golden. Schema-drift-only failures are expected upgrade artifacts.
4. Run `herder list`; inspect every displayed join gap without treating the
   display as authority.
5. Verify the drill pane ticked across the swap (read its pane; look for
   a gap at the handoff timestamp).
6. Spawn one disposable Claude/Codex probe through `tools/fleet/spawn.sh`, verify
   hook binding and `hcom send`, then cull with `tools/fleet/cull.sh`.
7. Close the exact ticker pane after verifying its identity.
8. Send ALL CLEAR with any detection-lost sessions named for natural-boundary restart.

Success criteria: version jumped; bus never degraded; drill ticks remained unbroken;
fleet spawn/message/cull round-tripped; all `herder list` gaps were explained; no
session or pane was lost except by choice.
