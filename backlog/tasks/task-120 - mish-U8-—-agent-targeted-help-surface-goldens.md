---
id: TASK-120
title: mish U8 — agent-targeted help surface + goldens
status: In Progress
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 10:37'
labels:
  - mish
dependencies: []
priority: high
ordinal: 120000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: the M17 doctrine surface: root help = concept model (mission, slug, marker, authority/owner, custody) + verb table; per-verb help = working prose (new: §6.1 + owner/authority chains; backlog: allowlist table + exclusion rationale + §8.3 references vocabulary and replace-not-append edge; status: warning meanings + staleness semantics) plus doctrine paragraphs: git rhythm §8.1, custody grammar §8.2, closeout §8.4, rename §8.5, marker hygiene §8.6. All golden-pinned. Plan §U8; spec §8, R8/R10. Depends on U4–U7.

Files: tools/mish/internal/cli/help.go (or Long strings), help_golden_test.go, testdata/golden/*.txt.

Settled decisions (KTD3, binding): write help text and goldens FROM THE SPEC FIRST, then make code match — never generate goldens from what the code happens to print; goldens byte-exact, regenerated only via -update; root help line budget; bottle precedent (tools/bottle/internal/cli/help_golden_test.go) for mechanics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 golden match for root and each verb (--help); goldens regenerated only via -update
- [ ] #2 line-budget test on root help
- [ ] #3 drift test: help verb table matches the registered command set
- [ ] #4 backlog help names every allowlisted subcommand and none of the excluded ones as available
- [ ] #5 helps carry: custody grammar mission(<slug>): <verb>, closeout checklist, rename procedure, marker hygiene, git rhythm, references vocabulary + replace edge
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Carry-over from U5 review (binding for U8): spec §6.2 requires bare `mish backlog` wrapper help to carry the EXCLUSION RATIONALE, not just excluded names — current output lists names only; per-exclusion reasons exist as comments in allowlist.go and must appear in the help text/goldens. Also U1 ruling: cobra default help command stays; U8 owns final help rendering. Lesson from U1: pin help surface on EXECUTED output, not constructed command tree.
<!-- SECTION:NOTES:END -->
