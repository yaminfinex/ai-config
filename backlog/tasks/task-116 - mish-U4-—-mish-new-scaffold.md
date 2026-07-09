---
id: TASK-116
title: mish U4 — mish new scaffold
status: In Progress
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 10:07'
labels:
  - mish
dependencies: []
priority: high
ordinal: 116000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: spec §6.1 scaffold end to end: validate slug → refuse existing dir → write manifest → write pinned board from fixture template → artifacts/ + keep-file → .mission marker per rules. Plan §U4; spec §6.1, §4.1–4.4, R2/R5/R6.

Files: tools/mish/internal/cli/{new.go,new_test.go}, board template fixture under internal/missionfs/testdata/. Depends on U2+U3.

Settled decisions: NO exec of backlog init — board written directly from a fixture cut from a real backlog init run (KTD5); no git operations ever (invariant 4); authority default = OS user, NEVER $SESSION_OWNER; owner chain = --owner → $SESSION_OWNER → OS user; both echoed with their source (flag/env/OS user); five pins + project_name: <slug>; marker rules: chain-walk first — different-slug marker anywhere on chain refuses, same-slug → no-op, --no-marker or cwd inside missions repo → skip; scaffold contains NOTHING beyond the §4.1 tree (no AGENTS.md litter); refusals per KTD9 grammar.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC-1: full §4.1 tree; five frontmatter keys; five pins + project_name; marker content = slug; echo lines with sources, with and without SESSION_OWNER set
- [ ] #2 AC-2 refusals wired at the CLI (slug table + existing slug + unset MISSIONS_REPO)
- [ ] #3 AC-3 marker matrix: different-slug refuses; same-slug no-op; --no-marker skips; invoked from inside missions repo writes none
- [ ] #4 --title default is the slug with hyphens spaced; keep-file exists under artifacts/
- [ ] #5 directory listing compared exactly against the §4.1 tree (AGENTS.md litter test)
- [ ] #6 no git invocation in any code path of new
<!-- AC:END -->
