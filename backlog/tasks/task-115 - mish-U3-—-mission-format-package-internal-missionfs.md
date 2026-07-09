---
id: TASK-115
title: mish U3 — mission format package (internal/missionfs)
status: Done
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 10:07'
labels:
  - mish
dependencies: []
priority: high
ordinal: 115000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: one package owning the mission-dir format: manifest read/write (§4.2 five closed keys), slug validation (§4.3), board config read (five §4.4 pins + statuses), task scan (status counts, duplicate IDs), artifacts scan (count, newest mtime). Plan §U3; spec §4, R2/R4/R7 read side.

Files: tools/mish/internal/missionfs/{manifest.go,slug.go,board.go,scan.go} + _test.go files + testdata/. Depends on U1.

Settled decisions: YAML lib for reads (KTD8); manifest writer emits the §4.2 skeleton exactly; slug regex ^[a-z0-9][a-z0-9-]{0,63}$ plus no-trailing-hyphen and no-consecutive-hyphens refusals; pin drift surfaces as typed findings; task scan parses only frontmatter id:/status:, scan set = backlog/tasks/ PLUS backlog/completed/ (cleanup ages Done tasks there; closed boards must still count them) while drafts/docs/decisions are excluded; fixtures CUT FROM REAL backlog-1.47.1 output, never hand-written (KTD5 hygiene). backlog 1.47.1 is on PATH for cutting fixtures.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC-2 slug table: Perf_Regression, -x, a--b, x-, 65-char — each refused with a one-line reason
- [ ] #2 manifest round-trip preserves the five keys; unknown key detected; mission:≠dirname detected; invalid status: value detected
- [ ] #3 each of the five pins individually drifted → detected; duplicate task IDs across two files detected
- [ ] #4 a task aged into completed/ still counts as Done; a draft is not counted
- [ ] #5 status counts follow config.yml's configured status order, not a hardcoded vocabulary
- [ ] #6 artifacts scan on a missing dir reports missing rather than erroring; fixtures documented as real-cut
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to mish-build @ 88f7d84 (--no-ff). Worker: codex mish-u3-kazu, branch mish-u3-missionfs (eef9cd1 + hardening a4f4022). Orchestrator verification: gates re-run uncached + post-merge green. Cross-family review (opus): ACCEPT-with-hardening; fixtures verified real-cut against live backlog 1.47.1. Hardening applied pre-merge: FindingMissingBoard (parity with ArtifactScan.Missing), malformed task files skipped with FindingMalformedTask, FindingUnknownTaskStatus for out-of-vocab statuses, FindingMissingManifestKey for absent closed keys; +boundary tests.
<!-- SECTION:NOTES:END -->
