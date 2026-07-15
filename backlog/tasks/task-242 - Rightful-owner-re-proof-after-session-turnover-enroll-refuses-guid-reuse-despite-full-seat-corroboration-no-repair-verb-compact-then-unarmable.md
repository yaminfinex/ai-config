---
id: TASK-242
title: >-
  Rightful-owner re-proof after session turnover: enroll refuses guid reuse
  despite full seat corroboration; no repair verb; compact --then unarmable
status: To Do
assignee: []
created_date: '2026-07-15 09:03'
labels: []
dependencies: []
ordinal: 241500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident (live orchestrator seat, post identity-repair): a seat whose LIVE session id differs from the row's recorded sid cannot restore its proof bundle by any CLI path, even when terminal_id, pane_id, label, AND live-verified bus name all corroborate ownership:

(1) herder enroll with matching HERDER_GUID refuses on 'calling live session does not match recorded session; enroll the caller under its own guid' — the suggested remedy mints a NEW guid row, which then permanently maps the caller's sid to that row (observed: a retired manual row now owns the sid, poisoning resolution forever).
(2) herder compact with HERDER_GUID + HCOM_SESSION_ID refuses: the two resolve to different identities (guid row vs the stray sid-mapped row).
(3) guid-only compact: pane proof passes but --then cannot arm — row lacks provenance.tool_session_id and ambient live evidence fails; the refusal's remedy (supply HCOM_SESSION_ID) loops back to (2).

Same class as the hcom reclaim-guard strand (see docs/hazards/agent-cli-identity-hijack.md): an anti-hijack guard with no rightful-owner recovery path. The guards are correct to be suspicious; the defect is that full seat corroboration (unchanged terminal + pane + label + live bus binding) cannot overcome a stale recorded sid.

FIX SHAPE (design-first per house rule — this is a write-spine surface): enroll guid-reuse should accept terminal-corroborated ownership as the documented alternative to sid match (help text already claims 'live session id OR unchanged terminal corroborates ownership' — the OR branch appears unreachable when sids disagree; red-first test that exact matrix), recording the new sid + full enroll provenance on success. Sid-to-row resolution must not keep honoring observations on retired rows over a seated row with live corroboration. AC sketch: (a) seated row + unchanged terminal + live bus binding + stale recorded sid → enroll succeeds under the SAME guid, records new sid + provenance.tool_session_id; (b) genuinely inherited guid (different terminal, no bus corroboration) still refused; (c) post-repair compact --then arms via recorded-SID verification; (d) retired rows' sid observations never win resolution against a seated corroborated row; (e) refusal texts keep cause+remedy, and no remedy mints identity as a side effect.
<!-- SECTION:DESCRIPTION:END -->
