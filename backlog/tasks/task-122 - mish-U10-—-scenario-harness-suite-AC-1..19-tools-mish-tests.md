---
id: TASK-122
title: mish U10 — scenario harness suite AC-1..19 (tools/mish/tests)
status: To Do
assignee: []
created_date: '2026-07-09 09:46'
labels:
  - mish
dependencies: []
priority: high
ordinal: 122000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: the spec's §9 acceptance scenarios as executable, hermetic shell checks against the REAL backlog binary — the milestone gate. Plan §U10; spec §9, R11/R12. Depends on U4–U7 (runs parallel with U8).

Files: tools/mish/tests/lib.sh, check-scaffold.sh (AC-1..4), check-nesting.sh (AC-5..8), check-resolution.sh (AC-9..10), check-surface.sh (AC-11..13), check-isolation.sh (AC-14), check-multiwriter.sh (AC-15), check-references.sh (AC-19), check-backlog-floor.sh (AC-5..7+AC-19 standalone — the version-change gate), fixtures/ + fixtures/README.md.

Settled decisions: sesh harness shape (lib.sh + check scripts printing ALL GREEN — see git show sesh-build:tools/sesh/tests/); each check builds temp missions repos (git-backed for AC-5/7/12/15, plain dir for AC-14) driving the built mish binary; AC-14 no-mutation audit via PATH-shim git wrapper logging every invocation — zero on non-git run, read-only-only on git-backed run; AC-15 = two clones of a temp origin + real merge; AC-16/18 are doctrine scenarios validated in U9, NOT scripted here (spec says so); minimal env (env -i style) with explicit MISSIONS_REPO/SESSION_OWNER control; fixtures real-cut, documented in fixtures/README.md; backlog version asserted in lib.sh.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 for f in tools/mish/tests/check-*.sh; do bash $f; done prints ALL GREEN on this machine with mise's backlog on PATH
- [ ] #2 AC-1..15 and AC-19 each covered one-to-one by a named check
- [ ] #3 check-backlog-floor.sh re-runs AC-5..7 + AC-19 standalone
- [ ] #4 AC-14 audit: git PATH shim proves zero git invocations on non-git repo; only read-only subcommands on git-backed repo
- [ ] #5 every check hermetic: temp dirs, explicit env, no dependence on the developer's environment
<!-- AC:END -->
