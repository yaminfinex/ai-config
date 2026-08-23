---
id: TASK-302
title: >-
  slim-down pass 2: characterize the occupant probe per tool as a pure function
  with fixtures
status: Done
assignee: []
created_date: '2026-08-21 04:40'
updated_date: '2026-08-21 05:08'
labels:
  - herder
  - slimdown
dependencies:
  - TASK-301
references:
  - napkins/herder-slimdown-charter.md
priority: high
ordinal: 301500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Wayfinding pass 2 (charter: napkins/herder-slimdown-charter.md). Characterize the occupant probe — pane → herdr pane.process_info → pid → transcript path → sid — per tool (claude, codex now; grok/pi later) as a small pure function with fixtures. Steal, don't reinvent: sesh already solved pid→transcript per tool (tools/sesh, R3: codex via open rollout fd = exact; claude via /proc correlation); herd-web's internal/procscan + internal/resolve (~/Coding/herd-web) solved pane↔pid↔HERDER_GUID joining read-only. pane.process_info is query-only (no pids in events) so resolution is snapshot-shaped: lifecycle events as wake hints + re-query. Deliverable is the function contract + fixture corpus, not wiring into verbs.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Probe contract written: inputs, outputs, error taxonomy (match / positive mismatch / no occupant / ambiguous), per-tool pid→transcript strategy documented
- [x] #2 Fixtures cover claude and codex live shapes; reuse of sesh/herd-web code identified concretely (which files/functions)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Delivered 2026-08-21: napkins/occupant-probe-contract.md (verified against HEAD ada6bf7 + live herdr 0.8.0, read-only). Wayfinder spot-verified sesh codexOwner leaf-holder rule and herd-web codexTranscriptPaths citations — exact. Contract: Probe/SelfProbe over injectable Substrate{Herdr, ProcRoot, Home}; outcomes MATCH(current|stale) / POSITIVE-MISMATCH(foreign) / NO-OCCUPANT(vacant|pane_gone) / AMBIGUOUS / UNPROBEABLE(grok, pi, SeatProcess). Lineage rule: MATCH = membership in row.SIDs ∪ Provenance.ToolSessionID; cross-row Lineage links guids not sids; a new unrecorded sid NEVER silently matches (resume mints no sid — live-verified for codex via 3-month-old rollout fd). Key live findings: (1) tool pid is NOT in pane.process_info foreground_processes (hcom pty wrapper) — mandatory /proc descent leg; observer liveCodexPID must not generalize as-is; (2) claude has NO exact pid→sid join — composes agent_session report + transcript-existence + cohort corroboration; detection-lost claude in a colliding cohort is honestly AMBIGUOUS (new owner question U8: accept residual AMBIGUOUS vs file upstream env-sid ask; sesh spec already records the ask at session-service-spec.md:180-181); (3) EACCES≠ENOENT: cross-user /proc → UNPROBEABLE not NO-OCCUPANT; (4) herdrcli.ProcessInfo drops the name field the descent filter needs. Fixture plan: 12 fixtures, 11 hermetic (ProcRoot/Home injection, synthetic /proc with real symlinks — sesh precedent); fixtures 8-10 are the TASK-303 red fixtures named by verbatim field refusals; process_info mocking already exists in check-observer-contract.sh:32-37. U6 sharpened: enroll --session-id/--hcom-name cannot delete until grok/pi carve-outs land.
<!-- SECTION:NOTES:END -->
