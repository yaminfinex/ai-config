---
id: TASK-169
title: 'grok: isolated pane + roster demo (no production diff)'
status: To Do
assignee: []
created_date: '2026-07-12 21:03'
labels: []
dependencies: []
priority: high
ordinal: 168000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Using the installed Grok Build CLI (0.2.93) and completely throwaway HOME/GROK_HOME/HCOM_DIR/HERDER_STATE_DIR, prove the fastest current path: raw Grok TUI launched by herder, one typed task completed, isolated hcom registration observed, one outbound hcom message sent after the operator supplies the assigned bus name — and explicitly demonstrate that inbound delivery is absent or unverified. This is a roster/pane demo, NOT integration: Grok fires inherited Claude hooks so hcom shows a healthy-looking row mislabelled tool:claude that cannot receive. No live registry/panes/observer/config, no installs/updates, no first-class claims. Full ground truth: docs/design/grok-onboarding-memo.md + docs/grok-integration-characterization.md. BLOCKED ON OWNER: auth path choice + inference-spend authorization.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Owner chose auth path and authorized one inference smoke; no credential value appears anywhere (commands, logs, memo, task, registry, bus)
- [ ] #2 Isolated roots for all four state/config namespaces; separate pane or private terminal server; teardown documented
- [ ] #3 grok opens interactively in a herder-created pane and completes a harmless prompt; herder + isolated hcom rows recorded with honest unknown/mislabelled fields
- [ ] #4 Outbound hcom message proven once with receipt; inbound probed once and reported delivered/queued/refused/absent without blind retries; report states plainly this is roster/pane only
<!-- AC:END -->
