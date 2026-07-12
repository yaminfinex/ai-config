---
id: TASK-156
title: >-
  docs drift: compact recipes in herder README + orchestrate skill show bare
  invocations the CLI now refuses
status: In Progress
assignee: []
created_date: '2026-07-12 01:49'
updated_date: '2026-07-12 06:45'
labels: []
dependencies: []
priority: medium
ordinal: 155000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The compact continuation-intent change (merge e52a8f3: bare herder compact refuses without --then or --stop) shipped with help-text and contract-suite coverage only (per its AC scope). Two durable docs now show recipes the CLI refuses: tools/herder/README.md ~262 ('herder compact 'keep: ...'' with neither flag, presented as the primary context-ceiling recipe) and skills/orchestrate/SKILL.md ~168 ('compact in place — herder compact '<steer>'' as first preference, --then mentioned only as an add-on for unattended workers). Fix: update both passages so every shown recipe carries --then (autonomous default) or --stop (interactive opt-out), and state the refusal behavior in one line. Docs-only; bundle-eligible into any herder-adjacent unit (e.g. the injection implement leg's runbook work). Sweep for other bare-compact recipe mentions while in there (docs/new-harness-onboarding.md mentions herder compact in passing — verify no recipe shown).
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Bundled into the TASK-145 implement leg (worker razu).
<!-- SECTION:NOTES:END -->
