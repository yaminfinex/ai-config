---
id: TASK-114
title: mish U2 — context resolution package (internal/resolve)
status: Done
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 10:04'
labels:
  - mish
dependencies: []
priority: high
ordinal: 114000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: spec §5 context resolution as a pure, heavily tested package: --mission flag → cwd-inside-mission-dir → single ancestor-chain .mission marker; refuse-never-guess; every §5.3 failure is a typed refusal with the spec's guidance. Plan §U2; spec §5, R5, R6.

Files: tools/mish/internal/resolve/{resolve.go,resolve_test.go}. Depends on U1 (branch from mish-build after U1 merges).

Settled decisions: input = (flag value, cwd, env lookup, fs) via seams (KTD4); marker walk collects ALL .mission files on the ancestor chain before deciding so the two-marker refusal names both paths; cwd-inside-mission-dir requires mission.md with parent chain under missions/; MISSIONS_REPO carries repo location only, never mission identity; nothing scans the repo for candidates (invariant 12).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC-9 at unit level: flag beats cwd beats marker; two markers on one chain refuse naming both paths; flag naming missing dir refuses
- [ ] #2 AC-10 at unit level: no context → guidance naming flag+cwd+marker; marker → missing mission names the slug; MISSIONS_REPO unset where needed → setup guidance
- [ ] #3 marker file with trailing content beyond line 1 resolves on line 1
- [ ] #4 cwd inside missions/<slug>/backlog/tasks/ resolves to <slug>; a mission.md outside a missions/ parent chain does not resolve
- [ ] #5 unit tests cover every branch of the §5.3 flowchart; no fs scan beyond ancestor walks
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to mish-build @ 7cdab7e (--no-ff). Worker: codex mish-u2-boho, branch mish-u2-resolve (906d72e + review fixes cb63a51). Orchestrator verification: gates re-run uncached + post-merge green. Cross-family review (opus): FIX-NEEDED → fixed: blank-marker false resolution (HIGH), slug traversal escape (MED-HIGH, §4.3 pattern enforced locally in resolve), marker line-1 whitespace trim, os.Getwd fallback removed, marker-read errors typed, edge tests added. Rulings journaled in napkins/mish-build/run-log.md.
<!-- SECTION:NOTES:END -->
