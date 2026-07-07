---
id: TASK-022
title: 'herder compact: steered self-compaction affordance (post-transport-kill)'
status: Done
assignee: []
created_date: '2026-07-07 07:51'
updated_date: '2026-07-07 09:30'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 22000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-003 FINDING 2 (Unit E, run-herder-dx wave 2): the herdr keystroke transport was load-bearing for queuing REAL INPUT to one's own pane — the steered self-compact mechanism (herder send "$HERDR_PANE_ID" '/compact <steer>') documented in skills/orchestrate. After the single-transport cut, own-pane sends resolve to the bus and arrive as hcom message injection, which does NOT fire compaction. Ruled (orchestrator): no self-pane exception inside send — transport doctrine stays pure; instead a dedicated affordance: herder compact <steer> (or herder input --self), reusing spawn's boot-paste engine on the caller's own pane. This is input automation, not inter-agent delivery. INTERIM (until this lands): agents at context ceiling stop and hand off to a fresh spawn (HANDOFF report + successor), no self-compact.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 herder compact '<steer>' queues a real /compact input line to the caller's own pane that fires on turn end (live smoke)
- [x] #2 refuses when run outside a herdr pane / non-self targets; does not reintroduce a general keystroke transport (grep-gate)
- [x] #3 skills/orchestrate + playbook context-discipline wording updated from interim back to self-compact; 16 suites + go gates green; docs/help per DoD
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commits 72d6b9b + 19acb48 (codex review fixes) on unit-i-compact. New herder compact [--dry-run] <steer> in internal/spawncmd/compact.go: queues a real /compact <steer> into the CALLER OWN pane via the package-private bootpaste engine; no target arg exists. Self-identity: HERDER_GUID + HCOM_SESSION_ID resolved independently (disagreement=refuse); row-terminal vs live-env-terminal disagreement refuses unless both keys corroborate (stale/inherited guid types NOWHERE); positional fallback needs terminal+cwd corroboration. Paste target re-resolved from durable terminal_id (HERDR_PANE_ID env proved to be a legacy alias live). Preflight VISIBLE-screen only for compact. Contract: tests/check-compact-contract.sh — 24 goldens + 4 grep gates (engine confined to spawncmd, no exported paste API, keystroke verbs nowhere else, no target/pane flag). AC#1 live-smoked twice: probe agents ran herder compact from their own Bash tool mid-turn, /compact queued and fired at turn end, context 35k->0k and 31k->0k with steer applied (probe resumes: 70b573db smoke 1, 78ead524 delta re-smoke; both culled). Docs: compact --help, README delivery exceptions + context-ceiling, send --help, skills/orchestrate SKILL.md/state-files.md/relay.md/sequential-phases.md reversed from INTERIM to compact-in-place. Gates 17/17 + go green. VERIFICATION STORY: hera gates+dry-run probes green -> codex review REQUEST-CHANGES (P1 stale/inherited durable key could type into another pane; P2 composer-empty evidence not airtight against pre-Enter clearing; P3 sequential-phases doctrine) -> all fixed in 19acb48 -> codex delta re-review APPROVE (no findings) + hera delta verification green. Follow-ups (not filed): --json record parity; codex-agent /compact steer semantics smoke; HCOM_SESSION_ID identity path live smoke.
<!-- SECTION:NOTES:END -->
