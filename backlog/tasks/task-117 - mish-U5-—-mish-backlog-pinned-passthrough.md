---
id: TASK-117
title: mish U5 — mish backlog pinned passthrough
status: In Progress
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 10:07'
labels:
  - mish
dependencies: []
priority: high
ordinal: 117000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: spec §6.2 passthrough: resolve context → board-exists guard (invariant 6, never ancestor fallthrough) → closed allowlist check → LookPath guard → exec real backlog with cwd pinned to the mission dir, forwarding args/stdio/exit code verbatim. Plan §U5; spec §6.2, R3/R7/R10.

Files: tools/mish/internal/cli/{backlog.go,backlog_test.go,allowlist.go}. Depends on U2+U3.

Settled decisions: DisableFlagParsing on the backlog command — wrapper interprets ONLY leading -h/--help/help (wrapper allowlist summary) and --mission BEFORE the first backlog subcommand token; --mission after the subcommand token forwards untouched (KTD2). Allowlist exactly: task/tasks, draft, board, search, overview, sequence, doc, decision, milestone/milestones, cleanup — exclusions (init, config, agents, browser, completion, instructions, mcp) with spec-mirrored reasons in comments; refusals name the allowlist (KTD9). Guard order exactly as §6.2. Missing binary → install-hint refusal. mish backlog <sub> --help passes through to backlog's own help.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC-11 unit level: init/config/agents/browser/unknown-future each refuse naming the allowlist; allowed subs exec with argv+cwd captured verbatim via fake exec seam
- [ ] #2 AC-6: missing config.yml refuses board-missing; fake seam proves backlog never invoked
- [ ] #3 exit codes from the seam returned verbatim (0, 1, 7)
- [ ] #4 flag-shaped tail args (--ref x@y, -s "In Progress") reach the seam unmodified
- [ ] #5 --mission before subcommand resolves; --mission after subcommand token forwards to backlog
- [ ] #6 bare mish backlog prints wrapper-owned allowlist summary; missing binary → install hint
<!-- AC:END -->
