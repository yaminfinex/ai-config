---
id: TASK-258
title: >-
  Test suites must be robust to ambient seat env: identity vars leak into launch
  tests; real-hcom discovery breaks on mise-shim PATHs
status: To Do
assignee: []
created_date: '2026-07-16 03:34'
updated_date: '2026-07-16 04:55'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 257500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two battery voids in one day, in two different environment classes, from the same root: parts of the test suite depend on ambient environment they neither scrub nor pin.

Class 1 — ambient identity env leaks into suite processes. A seat running the battery carries live identity/launch env (agent-kind preassignment flags, live guid/session vars). Launch-path tests read them and refuse (preassign refusals), failing tests that pass in a clean shell. Some suites already scrub transport/identity env in their fixtures (the wire-attribution tests scrub HCOM_*/HERDER_*/HERDR_*); the launch-path suites do not. Fix: every suite that exercises identity- or launch-sensitive code scrubs (or explicitly pins) ambient HCOM_*/HERDER_*/HERDR_* and agent-launch vars in test setup — audit for the class rather than patching the one observed site.

Class 2 — real-hcom binary discovery breaks under mise-shim-first PATHs. The real-hcom tests walk PATH for an hcom executable, skipping only the repo's own shim directory. When the mise shims dir precedes the real install dir (true for some agent seats and any shell that prepends shims), they pick the mise shim; with the fixture's faked HOME, mise then refuses the operator's real config as an untrusted ancestor local config (trust DB lives under the fake HOME; mise's ancestor walk finds the real config from any cwd under the operator home) and every invocation dies. Deterministic repro exists for both cwd geometries. The HERDER_TEST_HCOM_BIN override already exists and works. Fix directions: extend the discovery skip-filter to mise shim directories (or any path resolving to the mise binary), and/or resolve through the version-manager query once and pin, and document the override in the gate/battery docs so operators of shim-based shells set it.

Acceptance is by clean battery from BOTH environment classes that failed: an identity-bearing agent seat, and a shim-first orchestrator shell.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Launch/identity-sensitive suites scrub or pin ambient transport/identity env in setup (class audit, not single-site patch)
- [ ] #2 Real-hcom discovery is immune to mise-shim-first PATHs (filter or pinned resolution), with the env override documented where the battery is documented
- [ ] #3 Full battery green from an identity-bearing seat env AND from a shim-first shell without per-run workarounds
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
TRUST-STATE CAVEAT (2026-07-16): mid-trial, a worker added a mise trust record for their own worktree path to get their battery green — which MASKS the class-2 repro inside that geometry. The class still reproduces from other cwds/fake-HOME (verified: the repo mise.toml trips the same refusal). Acceptance runs must therefore start from a CLEAN trust state (no per-worktree trust records), and unit cleanup should remove any trust records added during work — dangling records for removed worktrees are residue.

CLASS 3, LIVE OUTAGE (2026-07-16 03:38-04:30): the same mise-trust fragility took down the LAUNCH PATH fleet-wide — every spawned pane (codex AND claude AND bash, any cwd) died at shell init on 'mise ERROR: config not trusted' (repo mise.toml, then per-worktree mise.toml for foreign-repo cwds), and the hcom --run-here launcher STRANDS FOREVER when the pane shell init fails (no timeout, no error surfaced; herder sidecar child exits defunct; registry row minted half-born with no bus bind). Detection cost was high because the pane error is invisible from spawn output. REMEDY APPLIED (host state, announced): explicit mise trust records for the ai-config repo, operator global config, and the affected foreign worktree; verified by probe spawns both geometries. OPEN QUESTIONS for this task: why trusted_config_paths coverage from the global config stopped applying in pane contexts at ~03:38 (first failure follows a mise trust write at 03:33 by five minutes — plausible trust-DB invalidation side effect, unproven); and the launcher no-timeout defect is upstream hcom (ledger candidate filed). Spawn-path smoke (one bash + one codex probe spawn) belongs in any fix verification.

INDEPENDENT CORROBORATION (peer orchestrator, 2026-07-16 ~04:47): a mission-control service shelling hcom from an untrusted-worktree cwd failed SILENTLY through the same window (ingest/keepalive/close-notices dropped, no errors surfaced) until that worktree was mise-trusted — the silent-degradation variant of class 3, worse than the loud launch failure. Collateral: a deploy inside the window booted the service with hcom broken; its fatal-exit path dropped its own seat row (documented exit-deletes-row class), causing a further failed-reclaim boot loop on their side. Class-3 fix verification should include a service-context hcom smoke (non-interactive, cwd in a worktree), not just spawn probes.

MECHANISM 3, PROVEN (peer, hcom life events 101986/102055): the @owner seat row was reaped by hcom's SYSTEM JANITOR ({action:stopped, by:system, reason:stale_cleanup} at 04:43:14) while its holder was ALIVE — the untrusted-mise config had silently starved the service's seat keepalive since boot, the row aged past the staleness threshold, and the janitor cannot distinguish a live-but-mute holder from a dead one. Config-layer breakage therefore converts to IDENTITY LOSS after one staleness window. Row recreated clean at 04:51:47 by service restart.
<!-- SECTION:NOTES:END -->
