---
id: TASK-121
title: mish U9 — companion skill (skills/mish)
status: In Progress
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 10:46'
labels:
  - mish
dependencies: []
priority: medium
ordinal: 121000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: the thin, mission-owned companion skill (M17): frontmatter name: mish + trigger-rich description; body points at mish help as the real surface, then carries ONLY what spans git and multi-writer judgment: when to mint a mission, custody-commit worked examples (AC-17 grammar for new/adopt/harvest/rename/close), §7.2 conflict taxonomy with AC-16 and AC-18 fully worked in a references doc, disjoint-artifact-path convention, pull-before-create rhythm. Plan §U9; spec §8, R9. Depends on U8.

Files: skills/mish/SKILL.md, skills/mish/references/multi-writer-walkthrough.md.

Settled decisions: bottling-shaped stub (KTD10 — see skills/bottling/SKILL.md for shape); nothing herder- or orchestrate-specific anywhere; no CLI mechanics that belong in help; general mission doctrine never moves into the orchestrate skill. Prose artifact — no tests; validated by review against spec §8.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 worked custody examples parse against the §8.2 grammar mission(<slug>): <verb> <summary> for new/adopt/harvest/rename/close (AC-17)
- [ ] #2 AC-16 (authority conflict) and AC-18 (rename) fully worked in references/multi-writer-walkthrough.md
- [ ] #3 skill contains no CLI mechanics that belong in help; nothing herder/orchestrate-specific
- [ ] #4 frontmatter: name + trigger-rich description only (house skill shape)
<!-- AC:END -->
