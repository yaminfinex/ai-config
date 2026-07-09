---
id: TASK-113
title: mish U1 — module bootstrap + CLI skeleton (tools/mish)
status: Done
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 09:54'
labels:
  - mish
dependencies: []
priority: high
ordinal: 113000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: a building, testable, cross-compiling mish binary with exactly three verbs (new/backlog/status) registered as stubs, and nothing else. Plan §U1; spec §6, R1.

Files: tools/mish/go.mod, cmd/mish/main.go, internal/cli/{root.go,deps.go,root_test.go}.

Settled decisions (do not reverse; stop-and-report if one seems wrong): standalone module 'mish', go 1.26.x, GOTOOLCHAIN=local, zero imports from the rest of ai-config (KTD1); cobra, file-per-subcommand (KTD2); thin main delegating to cli.Run(args, stdout, stderr) int — NOT sesh's Execute() error shape — exit grammar 0/1/2 (KTD9); deps struct (env/cwd/exec/git/clock/stdio seams) constructed in one place (KTD4). Eyeball sesh structure via: git show sesh-build:tools/mish is absent — use git ls-tree/show on branch sesh-build under tools/sesh for conventions (root_test surface pin, .gitignore, README placement).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 tools/mish builds: go test ./... && go vet ./... green from tools/mish with the mise go toolchain
- [ ] #2 root_test pins the surface: exactly three subcommands (new, backlog, status)
- [ ] #3 unknown subcommand exits 2 with a message naming mish for the command list
- [ ] #4 bare mish and mish --help exit 0
- [ ] #5 GOOS=darwin GOARCH=arm64 and GOOS=linux GOARCH=amd64 builds succeed
- [ ] #6 no imports from ai-config outside tools/mish; module name is mish
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to mish-build @ 73e4a33 (--no-ff). Worker: codex mish-u1-boma, branch mish-u1-skeleton (2ebd861 + review fix bbbfd27). Orchestrator verification: gates re-run uncached from worker worktree + post-merge on mish-build — green. Review finding (fixed): cobra default completion cmd was a live fourth verb (R1/M4 breach); DisableDefaultCmd + executed-surface tests added. Ruling: default help command stays (M17 surface, not a verb); U8 owns final help rendering.
<!-- SECTION:NOTES:END -->
