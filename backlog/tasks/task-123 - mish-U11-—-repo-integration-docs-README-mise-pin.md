---
id: TASK-123
title: 'mish U11 — repo integration + docs (README, mise pin)'
status: Done
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 10:59'
labels:
  - mish
dependencies: []
priority: medium
ordinal: 123000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: the surrounding repo knows about mish. README (bottle/herder shape): what mish is (one paragraph pointing at the spec), dev invocation (go build from tools/mish; runs from source — install/ship packaging DEFERRED until sesh's U12 shape is copied, say so), a Gates section with the exact Verification Contract commands, golden-regen flow, and the version-change re-verification rule. mise.toml: pin "npm:backlog.md" from latest to the verified 1.47.x. Plan §U11; R11, KTD11. Depends on U8+U10.

Files: tools/mish/README.md, mise.toml.

Settled decisions: do NOT build install/ship packaging, launchers, or bin/mish (deferred by plan scope boundary); mise pin to 1.47.1 (the verified version); no version enforcement in the CLI.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 cold read of the README suffices to run every gate verbatim
- [ ] #2 mise.toml pins npm:backlog.md to 1.47.1; mise install yields it; harness lib asserts the floor
- [ ] #3 README documents run-from-source + the packaging deferral
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to mish-build @ 7f0d095 (--no-ff). Worker: codex mish-u11-goma (2e98f97). mise.toml pins npm:backlog.md 1.47.1. README: portable mise-based setup (orchestrator-verified in a cold --noprofile shell), gates = verification contract with run-all.sh as documented runner, floor gate + golden regen + packaging deferral documented. Review fix applied pre-merge: machine-local Go path removed. sesh coordination input (installer disciplines) preserved in earlier notes for the future packaging follow-up.
<!-- SECTION:NOTES:END -->
