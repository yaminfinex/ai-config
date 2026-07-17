---
id: TASK-276
title: >-
  herder: codex spawns systematically miss the bind window post seat-completion
  — sidecar auto-completion unproven, every codex spawn degrades to manual
  enroll
status: In Progress
assignee: []
created_date: '2026-07-17 07:33'
updated_date: '2026-07-17 07:47'
labels:
  - herder
  - identity-migration
dependencies: []
priority: high
ordinal: 275500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field class, same night the canonical seat-completion unit merged: 4/4 codex spawns on this host hit bind-timeout(60000ms) -> 'seat completion refused [joined_bus_row_missing]' with pane correctly preserved and NO registry row (1 in this fleet, 3 in a peer fleet via compound+single spawns), while codex spawns succeeded earlier the same day pre-merge. Evidence from the in-fleet case: the child JOINED the bus ~27-57s after launch (ready event on record), yet bind still refused — codex roster rows omit the pane coordinate and the session-id enrichment lags (known structural class), so the completion-step Evidence correlates (SessionID/ProcessID/PaneIDs) cannot match a codex row inside the window even when the row exists. The refusal's promised automatic recovery ('its sidecar will complete the seat') did NOT fire within ~4 minutes despite the sidecar process alive and actively polling; the documented manual recovery (herder enroll from the live seat, deliverable over the bus when the child joined) worked first try and is currently the ONLY practical path. Under pre-merge behavior the row was minted creator-side and lag only delayed prompt delivery; refuse-not-mint is the ratified design, but the operational outcome is that codex spawns now routinely require manual recovery. Scope: (a) establish why the sidecar's correlated-recognition cannot (or how slowly it can) complete a codex seat — if its predicate needs the same absent correlates, the automatic path is structurally dead for codex and the refusal text overpromises; (b) make codex spawns complete at birth again: candidates include a codex-aware bind window, matching on additional correlates herder itself knows (it launched the pane and knows the child pid + the hcom name it minted — the launcher env carries the name), or having spawn consult the roster row by its OWN minted name once joined; (c) keep the ratified fences: no partial rows, refuse-not-mint, multi-match fail-closed, no ambient re-selection. Design checkpoint required before code (touches the completion evidence path).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause established with evidence: exactly which correlate(s) fail for codex rows at bind time and in the sidecar predicate, with timings
- [ ] #2 A codex spawn under enrichment lag completes its seat automatically (at birth or via sidecar within a bounded, stated time) without manual enroll, on this host under load
- [ ] #3 Refusal text matches reality: the automatic-recovery promise is either made true or reworded to the actual remedy
- [ ] #4 All ratified completion fences preserved (refuse-not-mint, multi-match fail-closed, no partial rows); existing suites + spawn goldens green
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-17 07:47
---
Root cause ACCEPTED (builder's isolated probes + lived spawn refusal): (1) awaitBind has only two child proofs — own-guid registry enrichment (cannot exist under refuse-not-mint) or roster pane_id match (codex rows omit pane) — so a row joining at ~30s stays invisible for the whole 60s window; (2) completion Evidence carries empty SessionID (codex sid lag), no ProcessID, and pane ids that match nothing -> zero correlates -> joined_bus_row_missing; (3) the sidecar starts env-scrubbed, extracts only HCOM_PROCESS_ID from owned children (ignores HCOM_INSTANCE_NAME the child demonstrably carries), and its tag+cwd fallback is deliberately non-authoritative -> the automatic-recovery promise is FALSE for the empty-coordinate codex row class. Regression boundary: the seat-completion unit's hardening correctly removed the sidecar ObservedBus shortcut but added no replacement child-specific correlate. Design APPROVED WITH REDIRECT: exact-Name correlate added to hcomidentity.Evidence and consumed by Resolve (joined/nonempty/exactly-one/fail-closed preserved; completions class live-verified) — a tightly-scoped authorized edit on the seam shared with the concurrent break-glass unit (which was notified; its attested-arm sole-caller pin is untouched — the builder's original proposal to reuse the AttestedBinding arm was REFUSED for exactly that collision). Spawn passes the name herder itself minted at launch; the owned-child env probe (guid-keyed, all-must-agree) is the sidecar's recovery path. Red-first pins required incl. making the refusal-text promise true end-to-end.
---
<!-- COMMENTS:END -->
