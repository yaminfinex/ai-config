---
id: TASK-164
title: >-
  herder operator language: align living docs/help with the four-state session
  model
status: In Progress
assignee: []
created_date: '2026-07-12 12:19'
updated_date: '2026-07-12 13:15'
labels: []
dependencies: []
priority: low
ordinal: 163000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Living READMEs, command help, diagnostics, injected lifecycle doctrine, comments, and test labels still teach the retired two-state registry vocabulary (active/closed), which now misdescribes behavior: list default output is non-retired (seated AND unseated) but help says active records; cull writes an unseat but help/output say closed (an unseated session remains resumable and keeps its label lease — cull is not retire); enroll unseats stale seat claims but says retired/closed; send/notify help calls seated coordinate candidates ACTIVE. Replace with seated/unseated/retired/lost language matching actual behavior. Do NOT touch hcom agent-status vocabulary (active/listening) — that is a different state machine.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 List help states default output includes non-retired sessions and explains what --all adds
- [ ] #2 Cull documentation and output say cull unseats a session; no implication of retirement or label release
- [ ] #3 Enroll documentation and output say stale seat claims are unseated, not retired
- [ ] #4 Send/notify coordinate-resolution wording says seated candidates; bus statuses keep their current terms
- [ ] #5 Living README examples and injected doctrine agree with command behavior; help/output goldens updated; full contract battery passes
<!-- AC:END -->
