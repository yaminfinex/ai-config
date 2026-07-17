---
id: TASK-277
title: >-
  herder enroll: degraded no-bus compat path mints unverified rows for
  bus-capable tools misclassified as manual
status: To Do
assignee: []
created_date: '2026-07-17 08:24'
labels:
  - herder
  - identity-migration
dependencies: []
priority: medium
ordinal: 276500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-traced residual from the codex-bind unit's boundary analysis (verified in code by the builder, reproduced live on a grok seat tonight). enrollcmd deliberately warns 'recording hcom_name as unknown', passes RequireBus=false, and classifies the candidate tool as firstNonEmpty(envTool(), 'manual') when live bus identity resolution fails. For grok seats the launch path scrubs HCOM_TOOL and exports no agent marker, so envTool() is empty -> candidate becomes 'manual' -> seatcompletion takes its sanctioned non-bus early return -> a SEATED row is minted with empty hcom_name / hcom_verified=false while the agent demonstrably IS joined to the bus. This is intentional pre-existing compatibility behavior (the warning prescribes rerunning enroll after joining) but it violates the strict seat-completion invariant (bus-capable seats verify or refuse; no partial rows) whenever the actual tool is bus-capable but unidentifiable from env. Scope: (a) make enroll's tool classification robust for bus-capable tools whose env is scrubbed (grok at minimum — the bridge knows the name; consider passing it or detecting the bridge), or (b) narrow the compat path so a tool that cannot be proven busless refuses with the join remedy instead of minting unverified — decide via design checkpoint against the completion contract and the ratified keep-list (no weakened refusals, no widened admitting predicates). Related: the exact-name Evidence correlate (codex-bind unit) heals spawn paths but NOT enroll/reconcile, whose callers do not populate Evidence.Name — evaluate whether they safely can.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design checkpoint settles the compat-vs-refuse question for unidentifiable bus-capable tools with rationale against the completion invariant
- [ ] #2 A grok seat running the documented enroll recovery ends verified (correct hcom_name) or refused with the join remedy — never seated-unverified; pinned by a fixture reproducing tonight's live shape
- [ ] #3 Existing enroll suites + goldens green; keep-list re-audit of the diff
<!-- AC:END -->
