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
     decisions there, never against a cached view.
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
   design §3, append via `UpdateLocked`, print typed outcome counts (applied/noop/refused)
   and flagged anomalies. Plus `tools/herder/tests/check-observer-contract.sh` with scenarios
   T-1..T-10 from design §7.
2. `herder observer run` — the loop: registry tail (inotify + 2s poll fallback, full
   projection reload on change, rotation-aware by inode), persistent socket subscription
   (events schedule debounced targeted probes; reconnect ⇒ full sweep), periodic sweep
   (default 30s), singleton flock on `$HERDER_STATE_DIR/observer.lock`, heartbeat mtime,
   status file `observer.status.json` (atomic rewrite, advisory), `observer status` and
   `observer stop` verbs, SIGTERM clean death. Boot always runs the full sweep first.
3. Nudge-start from spawn/enroll/resume/fork behind config `observer.autostart`
   (default OFF in this task; flipping it on is a follow-up decision after a bake period).

## Hard constraints (each maps to an owner-ratified invariant; violating one fails review)

- No privileged write path, no IPC append surface, no listening socket of any kind. Registry
  appends only via `UpdateLocked`. No verb may ever wait on the observer.
- Every append's outcome (applied/noop/refused) is logged and counted; a refused outcome
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
- [ ] 2 The full contract suite `check-observer-contract.sh` (T-1..T-10 per design §7) passes
      in CI alongside the existing check-* suites, hermetically (temp state dir, mocks, no
      live herdr/hcom needed).
- [ ] 3 An *enrolled* seat (created via `herder enroll` against mock-herdr) whose occupant
      dies is unseated by the observer with no sidecar involved — the blind-spot scenario,
      pinned as T-1.
- [ ] 4 `observer run`: second instance exits 0 without acting; kill -9 then restart
      converges to identical registry end-state (T-8); `observer status` reports pid, build
      hash, heartbeat age, last sweep summary; `observer stop` terminates it.
- [ ] 5 With the herdr socket unavailable or protocol-incompatible, the observer keeps
      adjudicating process seats and bus freshness, appends no herdr-seat verdicts, and says
      so in status output (T-4).
- [ ] 6 Sidecar coexistence: concurrent sidecar enrichment and observer sweeps never revert a
      rename or resurrect a culled session (T-5, mirrors spec AC-32).
- [ ] 7 Re-confirmation rows appear for a long-seated healthy seat at the configured interval
      (default 60m; test with the interval shrunk), as `reconciled` rows re-stamping
      `seat.confirmed_at`.
- [ ] 8 If spec erratum E-10 was adjudicated IN: `/clear` in an enrolled seat yields the
      child-first turnover pair from the observer, idempotently (T-2). If OUT: T-2 is
      dropped and observer turnover code is not built.
- [ ] 9 No observer code imports the legacy registry view (T-9 grep gate green).
- [ ] 10 README/help text: `herder observer --help` documents run/sweep/status/stop and the
      advice-not-truth doctrine in one paragraph.

## Out of scope (do not build)

Spoke telemetry, inbound deliver verbs, hot reads/`watch`, any sidecar behavior change,
auto-re-seating, occupant registration, plugin packaging, autostart default-on.
