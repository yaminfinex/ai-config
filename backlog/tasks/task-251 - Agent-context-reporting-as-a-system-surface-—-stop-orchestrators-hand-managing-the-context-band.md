---
id: TASK-251
title: >-
  Agent context reporting as a system surface — stop orchestrators hand-managing
  the context band
status: To Do
assignee: []
created_date: '2026-07-15 23:38'
labels:
  - herder
dependencies: []
priority: high
ordinal: 250500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER FILING (high importance, 2026-07-15, intent verbatim): "we need some context reporting as part of agents... orchestrators are trying to manage this themselves and this needs a system answer instead — maybe herdr already has an integration (I do not think so), or something in the ecosystem can help, not clear."

PROBLEM: the standing context-band doctrine (compact between 200k-250k, self AND workers) is enforced by orchestrators MANUALLY — reading pane statuslines, doing pane reads, guessing between unit boundaries. That is toil, misses fast-burning workers, and every orchestrator reinvents it.

WHAT ALREADY EXISTS (research starting points, verified on this machine):
1. The hcom statusline snapshot dir (~/.hcom/statusline/<sid|name>.env) ALREADY carries CTX_PCT per agent, written via the statusline path the sidecar maintains — claude agents populate it live (values 6-20 observed); codex agents have the file but CTX_PCT is EMPTY (coverage gap #1); two keying schemes (sid vs bare name) and ~97 accumulated files suggest hygiene issues.
2. Claude Code statusline hook input carries context_window.used_percentage / total_input_tokens / context_window_size as structured JSON — the data source is rich; only a percentage survives today.
3. herdr 0.7.4 (releasing now) adds custom metadata tokens + pane/workspace metadata reporting through the CLI and socket API for sidebar rows — a plausible ecosystem rendering surface (answering the owner question: herdr has no context integration today, but 0.7.4 metadata tokens could carry one).

UNIT SHAPE (typed, chained — separate units per house doctrine):
RESEARCH first: codex-side context availability (statusline hook or otherwise), what the snapshot spine can carry, herdr 0.7.4 metadata-token fit, whether hcom upstream has anything.
DESIGN second (registry/consumer surfaces are load-bearing — full design checkpoint): where context surfaces (herder list column? live status? bus events? observer advisories at band thresholds?), push vs pull, staleness semantics, codex gap closure, snapshot-dir hygiene.
IMPLEMENT last, from the reviewed design.

GOAL STATE (owner intent, not settled design): an orchestrator never manually polls context — the system reports it (visible per-seat) and ideally warns when a seat enters the compaction band.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Research memo: per-harness context data availability (claude verified, codex/grok/pi), snapshot spine capacity, herdr 0.7.4 metadata-token fit, upstream options
- [ ] #2 Reviewed design: system surface(s) for per-agent context + band-threshold signalling, staleness semantics, keying/hygiene of the snapshot dir
- [ ] #3 Implementation per approved design: orchestrators read context from a system surface (no pane reads); band entry is signalled
- [ ] #4 Docs/skill hygiene: context-band doctrine references the system surface once it ships
<!-- AC:END -->
