---
id: TASK-123
title: 'mish U11 — repo integration + docs (README, mise pin)'
status: To Do
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 09:49'
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
Coordination input from sesh orchestrator (2026-07-09, post-U12-merge): sesh U12 install shape is stable-to-copy at code level but not field-proven until their M4 rollout — mish v1 keeps run-from-source deferral. Applicable NOW to this unit: (1) README gate commands are code — every command must be executable verbatim from a cold read; (2) consider a small doc-vs-reality guard in the U10 harness or here (sesh precedent: tests/check-deploy-artifacts.sh greps docs for known-wrong patterns). When packaging is eventually pulled in (follow-up, not this unit): copy tools/sesh/etc/* patterns on sesh-build @ 5105225 — fleet-identical unit + drop-in per-host config, idempotent --dry-run-pure preflighting installer, zero repo-path assumptions; write installer failure modes first; copy files, do not extract shared abstractions at two tools.
<!-- SECTION:NOTES:END -->
