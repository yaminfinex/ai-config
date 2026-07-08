---
title: "Filed-ready implementation task text — herder node observer, phase 1a"
date: 2026-07-08
status: HANDOFF ARTIFACT — text for the orchestrator to file as the phase-1a implementation
  task(s), verbatim or split. Written for a reader with zero run context. Do not implement
  from this file until TASK-073 AC-4 (design review chain) is complete.
---

# Task text: implement the herder node observer (phase 1a — universal seat observer)

TYPE: implementation. The design is settled and reviewed; do not re-open design decisions.
If the design doc and this text disagree, the design doc wins; if reality (the code) makes a
design decision impossible, stop and report — do not improvise around it.

## What you are building

A per-node observer for `herder` (Go CLI in `tools/herder/`): a disposable daemon that
watches every **seated** session in herder's registry — regardless of whether the seat came
from spawn, enroll, resume, or fork — and appends observation facts (unseats on witnessed
death, sid/name enrichment, slow re-confirmations, turnover pairs for sidecar-less seats)
through the same locked writer every CLI verb uses. Today only spawned sessions are watched
(each gets a sidecar process); sessions adopted via `herder enroll` are watched by nobody,
which caused real incidents (a live agent whose registry row said `unseated`, dead rows
trusted, undetected `/clear` identity changes).

## Read first (all in-repo, reachable from main after the design branch merges)

1. Design doc (normative for this task):
   `docs/design/2026-07-08-observer-phase1a-design.md` — especially §1 (five invariants and
   where each is honored — these are owner-ratified and non-negotiable), §5 (loop), §6
   (catch-up sweep), §7 (test suite), §9 (build order).
2. Spec: `docs/specs/herder-spec.md` (RATIFIED). The observer-related errata proposals are in
   `docs/design/2026-07-08-observer-phase1a-spec-errata.md`; check their adjudication status
   with the spec steward before wiring anything that depends on E-10 (observer-side turnover).
3. Source you will touch or link:
   - `tools/herder/internal/registry/write.go` — `UpdateLocked` is the ONLY way you append.
     Its closure receives the projection loaded under the lock; make all check-then-append
     decisions there, never against a cached view. CAUTION: its returned encoded rows
     include node-mint / migration / rotation rows appended before yours — never derive
     your observation outcomes from the raw return (see hard constraints).
   - `tools/herder/internal/registry/v2/registry.go` — the v2 projection you consume.
     NEVER import the legacy `registry.Record`/`Status` view in observer code.
   - `tools/herder/internal/reconcilecmd/reconcile.go` — existing probe logic and the
     `reconciled` row shape; extract/share rather than duplicate where clean.
   - `tools/herder/internal/sidecarcmd/sidecar.go` — the correlation discipline to copy
     (pane-correlated matches append; tag+cwd fallback matches never do) and the socket
     report calls (`pane.report_agent_session`).
   - `tools/herder/internal/herdrcli/herdrcli.go` — CLI fallback client.
   - `tools/herder/tests/mock-herdr`, `tests/mock-hcom` — the mocks your suite extends.
4. herdr socket protocol: newline-delimited JSON `{id, method, params}` on the unix socket
   reported by `herdr status server` (protocol 16 at design time; check `compatible`).
   Schema: `herdr api schema --json`. Verbs used: `session.snapshot`, `pane.get`,
   `pane.process_info`, `events.subscribe` (subscriptions: `pane.created`, `pane.closed`,
   `pane.exited`, `pane.agent_detected` — all broadcast, no per-pane filters needed).

## Build order (ship each step green before the next)

1. `herder observer sweep [--json]` — one level-triggered pass, no daemon: load v2
   projection + `session.snapshot` + `hcom list --json`, adjudicate every seated row per
   design §3 (including the epoch discrimination rule), append via `UpdateLocked`, print
   typed outcome counts (applied/noop/refused, per candidate fact) and flagged anomalies.
   Plus the **step-1 subset** of `tools/herder/tests/check-observer-contract.sh`: scenarios
   T-1..T-7 and T-9..T-11 from design §7 — no daemon-lifecycle tests in this step. Also in
   this step: `herder list` consumes `observer.status.json` flags (AC-11) so the surfacing
   ships with the first sweep.
2. `herder observer run` — the loop: registry tail (inotify + 2s poll fallback, full
   projection reload on change, rotation-aware by inode), persistent socket subscription
   (events schedule debounced targeted probes; reconnect ⇒ full sweep + epoch doubt ON),
   periodic sweep (default 30s), singleton flock on `$HERDER_STATE_DIR/observer.lock`,
   heartbeat mtime, status file `observer.status.json` (atomic rewrite, advisory),
   `observer status` and `observer stop` verbs, SIGTERM clean death. Boot always runs the
   full sweep first. **Suite grows here**: add T-8 (kill -9 disposability) and the
   `status`/`stop` checks.
3. Nudge-start from spawn/enroll/resume/fork behind config `observer.autostart`
   (default OFF in this task; flipping it on is a follow-up decision after a bake period).
   **Suite grows here**: nudge-start checks (absent daemon started; stale heartbeat
   replaced; second nudge no-ops).

## Hard constraints (each maps to an owner-ratified invariant; violating one fails review)

- No privileged write path, no IPC append surface, no listening socket of any kind. Registry
  appends only via `UpdateLocked`. No verb may ever wait on the observer.
- Every append's outcome (applied/noop/refused) is resolved **per candidate observation
  fact** — by identifying which of the observer's own candidate rows survived normalization
  and mapping errors to preserved refusal categories — NEVER from `UpdateLocked`'s raw
  returned rows (they include node-mint/migration/rotation rows; counting them fakes
  `applied` on a fresh state dir). Outcomes are logged and counted; a refused outcome
  triggers a re-sweep, never a blind retry.
- v2 projection only. The contract suite must grep-enforce this (T-9).
- No durable observer state that a later generation reads: no cursor file, no queue. The
  lockfile and status file are written, never read across generations. kill -9 must be a
  fully supported stop.
- Unseat requires positive evidence of death (design §3 verdict table). Absence of evidence
  (socket down, bus errored, probe timeout) appends nothing. Never produce `state: lost`.
  Dormant-live rows and unregistered occupants are flagged, never repaired or registered.

## Acceptance criteria

- [ ] 1 `herder observer sweep` exists; run against a registry with a seated row whose pane
      is gone (mock-herdr), it appends exactly one `unseated` row with
      `close_result: observed_dead` and evidence in `close_reason`; re-running is a typed noop.
- [ ] 2 The contract suite `check-observer-contract.sh` passes in CI alongside the existing
      check-* suites, hermetically (temp state dir, mocks, no live herdr/hcom needed) —
      split by build step: T-1..T-7 + T-9..T-11 green at the end of step 1; T-8 +
      status/stop checks green at the end of step 2; nudge checks at step 3. Each step is
      independently landable with its subset green.
- [ ] 3 An *enrolled* seat (created via `herder enroll` against mock-herdr) whose occupant
      dies is unseated by the observer with no sidecar involved — the blind-spot scenario,
      pinned as T-1.
- [ ] 4 *(step 2)* `observer run`: second instance exits 0 without acting; kill -9 then
      restart converges to identical registry end-state (T-8); `observer status` reports
      pid, build hash, heartbeat age, last sweep summary; `observer stop` terminates it.
- [ ] 5 With the herdr socket unavailable or protocol-incompatible, the observer keeps
      adjudicating process seats and bus freshness, appends no herdr-seat verdicts, and says
      so in status output (T-4).
- [ ] 6 Sidecar coexistence: concurrent sidecar enrichment and observer sweeps never revert a
      rename or resurrect a culled session (T-5, mirrors spec AC-32).
- [ ] 7 Re-confirmation rows appear for a long-seated healthy seat at the configured interval
      (default 60m; test with the interval shrunk), as `reconciled` rows re-stamping
      `seat.confirmed_at`.
- [ ] 8 Observer-side turnover is part of this task's definition of done: once spec erratum
      E-10 is accepted, `/clear` in an enrolled seat yields the child-first turnover pair
      from the observer, idempotently (T-2). If E-10 is still unadjudicated when you reach
      it, BLOCK on the spec steward — do not skip. If the steward REJECTS E-10, STOP and
      return this task to design review; shipping phase 1a without turnover is not an
      implementation decision.
- [ ] 9 No observer code imports the legacy registry view (T-9 grep gate green).
- [ ] 10 README/help text: `herder observer --help` documents run/sweep/status/stop and the
      advice-not-truth doctrine in one paragraph.
- [ ] 11 `herder list` (the default operator view) annotates rows flagged by the observer —
      dormant-live rows, epoch-wide doubt — inline and explicitly marked as observer advice,
      sourced from `observer.status.json`; a missing or deleted status file drops the
      annotation without error. (Design §6.3; review finding P1-3. Test: T-6 asserts the
      flag appears in `list` output, not only in sweep --json.)
- [ ] 12 The epoch discrimination rule (design §3) is implemented and pinned by T-11: a
      snapshot with wholesale-reissued terminal ids produces zero unseat rows and an
      epoch-doubt flag; partial overlap unseats only the absent seat; a lone absent seat
      after a connection gap unseats only with corroborating dead-bus evidence.

## Out of scope (do not build)

Spoke telemetry, inbound deliver verbs, hot reads/`watch`, any sidecar behavior change,
auto-re-seating, occupant registration, plugin packaging, autostart default-on.
