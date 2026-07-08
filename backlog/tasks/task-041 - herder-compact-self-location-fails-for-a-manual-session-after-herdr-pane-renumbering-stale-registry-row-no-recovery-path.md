---
id: TASK-041
title: >-
  herder compact: self-location fails for a manual session after herdr pane
  renumbering (stale registry row, no recovery path)
status: To Do
assignee: []
created_date: '2026-07-08 04:34'
updated_date: '2026-07-08 05:04'
labels: []
dependencies: []
priority: medium
ordinal: 41000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (hera, 2026-07-08, first production compact --then attempt): herder compact refused with 'terminal term_65586d58... not live in herdr agent list; cannot locate your own pane'. The orchestrator is a MANUAL session; its registry row records the pane/terminal from enroll time, herdr since renumbered panes (live pane now w6554208c1918a12-3; row says -1 with a dead terminal id), and the self-location ladder (HERDER_GUID -> session id -> registry row) dead-ends on the stale coordinates. Fail-closed refusal is CORRECT (nothing typed) — but there is no recovery affordance: the refusal message doesn't say HOW to re-prove identity. Fix directions: (a) refusal message suggests re-enrolling (herder enroll) to refresh the row — verify enroll actually detects the CURRENT pane in this situation (its env may be equally stale: HERDR_PANE_ID held a legacy p_NNN handle); (b) ladder gains a live fallback: match own process/session against herdr agent list (the live list DOES show the session, correct pane, agent_status) — child-specific by construction; (c) at minimum document the restart-or-reenroll remedy in compact --help. Related: TASK-035 handled this class for SEND (pane-id resolution); compact's SELF path has the same disease from the other side.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): herdr 0.7.0 #569: pane ids are stable handles and closed ids no longer retarget — the renumbering trigger for the original failure likely cannot recur in-session (re-verify; server handoff/restart still reissues coordinates per TASK-046, so the stale-registry-row state remains reachable). Per hera: the recovery-affordance half stands regardless — the refusal message must say HOW to re-prove identity. Suggest re-scoping title to the affordance; TASK-034's blocker status should be re-evaluated after TASK-050 (NEW-4) re-verification.
---
<!-- COMMENTS:END -->
