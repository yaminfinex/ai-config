---
id: TASK-078
title: >-
  Design-task pattern: evaluate after the seat-observer design pass, decide
  whether to capture into the orchestrate skill
status: To Do
assignee: []
created_date: '2026-07-08 20:57'
updated_date: '2026-07-08 22:05'
labels: []
dependencies: []
priority: medium
ordinal: 78000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner + orchestrator agreed a pattern for work that needs a DESIGN pass before it is buildable (agreed 2026-07-08 in-session, first application will be the seat-observer task, currently TASK-073). The pattern, self-contained:

1. TASKS ARE TYPED AT CAPTURE: a design task states explicitly that it is not ready to build, and its deliverable is a design — never code.
2. A DESIGN TASK IS DONE when it produces: (a) a durable design document (reachable per the capture contract); (b) proposed spec errata routed through the spec-steward lane when the ratified spec is affected — the designer proposes, never edits; (c) the follow-on implementation task(s) written FILED-READY: plain language, reachable references, acceptance criteria written by the designer while design intent is fresh. The design pass is what makes acceptance-criteria-at-capture-time honest for architectural work.
3. DESIGNER AGENT: a top-class reasoning model (currently Fable) with full delegation freedom — subagents for wide reading, its own jury for genuinely contested sub-decisions. Constraints: designs in a worktree that only grows docs artifacts; never dispatches implementation (unit-cutting stays with the orchestrator); hands implementation-task text to the orchestrator, who files it (single-writer backlog).
4. ADVERSARIAL DESIGN REVIEW is a stakes-gated option, exercised before declaring a design buildable — a fresh-context reviewer (different family, or a second fresh same-class lens) attacks the design; catching a design defect here costs a review, catching it at implementation review costs a unit.

THE DECISION THIS TASK HOLDS OPEN: do NOT encode design-task typing into the orchestrate skill yet. Run the pattern on the seat-observer design pass first; then come back here and decide — capture it into the skill (where: probably the Shape-the-run section, honoring the centralise-model-doctrine and no-strewing rules), amend it based on what went wrong, or drop it. The evaluation should ask: did the designer's filed-ready implementation tasks survive dispatch without the orchestrator re-inventing scope? Did the design review catch anything? Was the delegation freedom used or was it a plain single-agent unit?
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Seat-observer design pass has completed using this pattern and its outcome is summarized in a comment here
- [ ] #2 Explicit decision recorded: capture into skill (with placement), amend, or drop — with reasons
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 22:05
---
FIRST DATA POINT — seat-observer design pass completed under this pattern (2026-07-08, one session): Designer (Fable) delivered in ~55 min: design doc answering all 8 settle-items, errata E-1..E-11, filed-ready impl task text. PATTERN OBSERVATIONS: (1) designer asked ZERO mid-unit questions — the remediated capture (task text as sole substance, thin mechanics brief) carried everything; (2) designer flagged its four contested scope calls to the reviewer in a dedicated section instead of self-adjudicating — the reviewer adjudicated all four (favorable with caveats), which worked exactly as the pattern intended; (3) delegation freedom was used lightly (source survey + live socket probes, no jury requested — each call had stated rationale and cheap reversal); (4) cross-family quality review (codex on Fable) produced 3 P1 + 3 P2, all substantive — the standout caught a false-applied trap in UpdateLocked return semantics at design time; one fix round to delta APPROVE; (5) tomo intent review (INTENT-OK) added distinct value beyond quality: caught that E-2 deviates from decision-record wording (needs an explicit steward call) and that the 60m cadence row class is designer-added, not owner-ratified — drift-detection the quality reviewer did not attempt; (6) the filed-ready impl text survived the reviewer's cold read ('buildable step by step') and filed cleanly as TASK-080 with 12 ACs. VERDICT SO FAR: pattern worked end-to-end with zero orchestrator scope re-invention. Remaining before the capture-into-skill decision: watch whether TASK-080's dispatch actually proceeds without re-inventing scope (the pattern's ultimate test).
---
<!-- COMMENTS:END -->
