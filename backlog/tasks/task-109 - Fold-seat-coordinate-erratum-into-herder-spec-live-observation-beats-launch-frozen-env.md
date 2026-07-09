---
id: TASK-109
title: >-
  Fold seat-coordinate erratum into herder-spec: live observation beats
  launch-frozen env
status: To Do
assignee: []
created_date: '2026-07-09 07:05'
labels: []
dependencies: []
priority: medium
ordinal: 109000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
UNIT TYPE: implement (spec text change; docs-only, stakes-gated review).

The 2026-07-08 systemic review (docs/design/2026-07-08-herder-systemic-review-memo.md, section 2 cluster A) identified a spec-erratum candidate that was never routed: docs/specs/herder-spec.md nowhere states the rule that no seat coordinate (pane id, terminal id, workspace, cwd) may be sourced from launch-frozen environment values when a live observation of the same coordinate is available. Cluster A (stale coordinates) was the single largest defect cluster on the TASK-001..070 board, and the observer work (TASK-073/081) built the live-observation machinery — but the spec never captured the sourcing rule itself, so nothing stops a future verb from trusting frozen env again.

SCOPE: draft the erratum as a normative sentence-or-paragraph in the appropriate herder-spec section (likely the seat/registry semantics section), citing the memo cluster as motivation in the commit message, not the spec text. The ratified spec on main is self-contained; this is the first post-ratification amendment, so add a dated changelog/amendment note consistent with the doc's own conventions (or start one if none exists). Adjudication of exact wording: @hera routes to owner if contested.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder-spec.md on main contains a normative rule that live-observed seat coordinates take precedence over launch-frozen env values, placed in the section governing seat/registry semantics
- [ ] #2 The amendment carries a dated changelog or amendment note in the doc
- [ ] #3 grep of tools/herder for coordinate reads sourced from launch-frozen env (e.g. spawn-time pane/terminal env vars used at verb time) either finds none or each finding is listed in the task as a known, justified exception
<!-- AC:END -->
