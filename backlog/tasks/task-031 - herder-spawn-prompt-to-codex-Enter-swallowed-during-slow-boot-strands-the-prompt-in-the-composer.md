---
id: TASK-031
title: >-
  herder spawn --prompt to codex: Enter swallowed during slow boot strands the
  prompt in the composer
status: To Do
assignee: []
created_date: '2026-07-07 20:24'
updated_date: '2026-07-07 20:55'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live incident (hera, 2026-07-07, reviewer-d2da9f3f): spawn --prompt into a fresh codex reported "NOT confirmed (verify: not_delivered, ready: timeout(status=done))" — truthful. Post-mortem: bootpaste pasted the prompt and fired its Enter retries while the codex TUI was still booting (ready-wait had already timed out), so the text landed in the composer but every Enter was swallowed. Result: codex sat idle holding the full prompt unsubmitted. A queued bus resend cannot flush into an idle codex (no turn boundary ever comes), so the session was stranded until a manual `herdr pane send-keys <pane> Enter` submitted it (agent went working immediately).

Direction to evaluate: when ready-wait times out but the paste landed (composerHoldsPayload evidence), spawn could keep a bounded late-submit loop — re-check readiness, then re-Enter while the composer still holds the payload sigil — instead of giving up at boot-window end. Alternatively/additionally: document the manual remedy in the spawn NOT-confirmed hint (it currently says "read the pane first" but not the send-keys Enter recovery). Relates to TASK-024 (verify evidence semantics) — do not weaken its pre-Enter evidence gating.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Repro pinned: a slow-boot codex fixture (or mock delay) demonstrates paste-lands/Enter-swallowed producing not_delivered with the prompt stranded
- [ ] #2 Chosen remedy implemented: bounded post-timeout late-submit (only with composer-payload evidence) OR spawn hint text names the manual send-keys Enter recovery — decision recorded with rationale
- [ ] #3 TASK-024 evidence gating preserved (no false delivered); spawn goldens reviewed line-by-line if verify text changes
- [ ] #4 Pinned gate green (go vet/test + full battery, env -u)
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-07 20:31
---
GATED on TASK-032 (consolidated spawn delivery review, user-raised) — the late-submit remedy should fall out of that map, not precede it.
---

created: 2026-07-07 20:55
---
Unit R phase A verdict (ratified): SUPERSEDED by TASK-032 B1/B4 pending phase-B verification — root cause reframed: the stranded composer was bootpaste-manufactured, and it is the very state that blocks bus delivery (both families, not codex-specific). Spawn moves to bus-first delivery; the late-submit loop is not needed. Manual remedy (send-keys Enter) gets documented via B2. Final disposition at Unit R acceptance.
---
<!-- COMMENTS:END -->
