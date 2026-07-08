---
id: TASK-073
title: >-
  herder node daemon phase 1a: universal seat observer + spec amendment pass
  (owner-ratified D-via-A re-cut)
status: To Do
assignee: []
created_date: '2026-07-08 11:44'
updated_date: '2026-07-08 20:59'
labels: []
dependencies: []
priority: high
ordinal: 73000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed from tomo's proposal (#12069), sessions/missions design round — owner ratified the design-it-twice pass (D-via-A, observer-first re-cut after the systemic-review memo). Decision record: docs/design/2026-07-08-herder-node-daemon-designs.md on branch sessions-missions-design (1fbe376); verified present and status DECIDED before filing.

Unit: a per-node daemon tails the registry as its work queue and observes EVERY seated row regardless of seat mechanism (spawn/enroll/resume/fork) — closing the systemic memo's cluster E / 3.3(c) enrolled-seat observer blind spot (the class where the manual/enrolled orchestrator is always the least-covered identity: TASK-034/041/042/043/044/050/065/070 territory).

Spec-level invariants riding with the unit (through the erratum/ruling process — spec is RATIFIED, no draft edits):
- Daemon has NO write authority (spec 10 sharpened, not reversed); all appends via the shared locked writer package (byte-indistinguishable from CLI appends).
- Daemon appends obey the 3.1 confirmed-write contract (typed applied|noop|refused — memo finding 2 lineage).
- Projection consumption reads v2 states only, never the legacy view (same AC-31 class as TASK-072 amendment 2).
- Daemon is disposable: no handoff protocol (TASK-046 evidence).

Explicitly OUT of this unit (gated on design work still in grilling): spoke telemetry, inbound deliver verbs, hot reads.

Overlap notes: subsumes memo 3.3(c) — do NOT file enroll-forks-a-sidecar as a stopgap; memo 3.3(a) and (b) remain independently fileable (pending the memo-translation pass). Sequencing: spec amendment/erratum pass runs first (spec-ravu lane), implementation dispatch after TASK-071/072 land (registry write-path adjacency).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 20:59
---
Owner directive (2026-07-08): the design pass this task will become gets TOMO as FINAL reviewer, in addition to the fresh-context adversarial design review. Rationale: tomo is the live claude session that authored the original node-daemon proposal this task was filed from — as final reviewer it checks drift-from-original-intent (golden-agent-style purpose check), complementing the fresh-context reviewer who attacks quality. Sequencing at dispatch: designer produces -> fresh-context adversarial review -> fix round -> tomo final review -> buildable. NOTE for the future orchestrator: tomo is a live session (bus name tomo); if it has been culled or compacted past usefulness by design time, resume/decant it or escalate to owner rather than silently substituting.
---
<!-- COMMENTS:END -->
