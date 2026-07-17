---
id: TASK-276
title: >-
  herder: codex spawns systematically miss the bind window post seat-completion
  — sidecar auto-completion unproven, every codex spawn degrades to manual
  enroll
status: Done
assignee: []
created_date: '2026-07-17 07:33'
updated_date: '2026-07-17 15:18'
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
- [x] #1 Root cause established with evidence: exactly which correlate(s) fail for codex rows at bind time and in the sidecar predicate, with timings
- [x] #2 A codex spawn under enrichment lag completes its seat automatically (at birth or via sidecar within a bounded, stated time) without manual enroll, on this host under load
- [x] #3 Refusal text matches reality: the automatic-recovery promise is either made true or reworded to the actual remedy
- [x] #4 All ratified completion fences preserved (refuse-not-mint, multi-match fail-closed, no partial rows); existing suites + spawn goldens green
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-17 07:47
---
Root cause ACCEPTED (builder's isolated probes + lived spawn refusal): (1) awaitBind has only two child proofs — own-guid registry enrichment (cannot exist under refuse-not-mint) or roster pane_id match (codex rows omit pane) — so a row joining at ~30s stays invisible for the whole 60s window; (2) completion Evidence carries empty SessionID (codex sid lag), no ProcessID, and pane ids that match nothing -> zero correlates -> joined_bus_row_missing; (3) the sidecar starts env-scrubbed, extracts only HCOM_PROCESS_ID from owned children (ignores HCOM_INSTANCE_NAME the child demonstrably carries), and its tag+cwd fallback is deliberately non-authoritative -> the automatic-recovery promise is FALSE for the empty-coordinate codex row class. Regression boundary: the seat-completion unit's hardening correctly removed the sidecar ObservedBus shortcut but added no replacement child-specific correlate. Design APPROVED WITH REDIRECT: exact-Name correlate added to hcomidentity.Evidence and consumed by Resolve (joined/nonempty/exactly-one/fail-closed preserved; completions class live-verified) — a tightly-scoped authorized edit on the seam shared with the concurrent break-glass unit (which was notified; its attested-arm sole-caller pin is untouched — the builder's original proposal to reuse the AttestedBinding arm was REFUSED for exactly that collision). Spawn passes the name herder itself minted at launch; the owned-child env probe (guid-keyed, all-must-agree) is the sidecar's recovery path. Red-first pins required incl. making the refusal-text promise true end-to-end.
---

created: 2026-07-17 08:48
---
Adversarial review round 1 (incumbent opus + grok calibration): FIX ROUND REQUIRED. Incumbent P1 (keep-list fence): the launch-frozen env name is admitted alone — after rename/reclaim a stranger holding the derived name gets OUR pane written into THEIR hcom row and their name stamped verified on OUR guid (executed in an isolated copy; merge-missing-only does not protect the empty-coordinate victim class); plus the derived tag+instance form violates the codebase's own no-derivation rule; open severity question for the builder: does hcom collision-suffix names at join (routine divergence) or is rename/reclaim required (narrow). Incumbent P2: sidecar accepts UNVERIFIED WriteNoop and latches permanently for empty-SessionID codex rows (retry predicate can never re-fire) — and the unit INVERTED the previously-pinned no-latch test to make it pass; spawn-side noop verification exists and the asymmetry is the bug. P3 stale docs/reasons + cosmetics. Held under mutation: duplicate-name rowMatches pin (load-bearing — map collapse would otherwise admit), name-vs-pane conflict refusal, tag+cwd non-authoritative (6 tests fail on revert), empty-name double fence, agreement rule incl. scrubbed-grandchild abstention, seam scope exactly as authorized, red-first proven behaviorally against main. CALIBRATION SPLIT: grok seat APPROVED, missing both incumbent findings (frozen-env divergence unconsidered; noop asymmetry read as intended design); also suffered total shell-tool death mid-slot (never mutated the real tree; reported honestly; tree verified quiet by orchestrator). Fix round dispatched (3 findings + the hcom suffix question).
---

created: 2026-07-17 09:22
---
Adversarial review DELTA (incumbent opus @danu, isolated-copy + real-hcom-db read-only verification): APPROVE. Both round-1 findings fixed at the mechanism, pins verified by reversion: FR-1 (P1 stranger-row) — childBusName synthesis fully removed (StoredNameMatches Name-or-BaseName matcher), findRowForOwnedChild never admits the name clue alone (requires HCOM_PROCESS_ID agreement OR base-row PID from hcom.db read-only proven to be one of our HERDER_GUID-owned live PIDs); the exact round-1 stranger scenario now REFUSES (executed against real db). Incumbent hunted the new db read for holes and found none — verified against the live schema (read-only backup, live db untouched) that instances.name PK is the base name (keying by BaseName correct), instances.pid is populated for herder agents, and the full chain resolves TRUE for tonight's actual codex agents (recovery works end-to-end, refusal promise now true). FR-2 (P2 noop-latch) fixed symmetric with spawn (reload + completedRecognitionMatches before accepting WriteNoop; original fence test restored). FR-3 four-signal copy done (10 goldens pure refusal-text, codes/semantics unchanged). No new defect. CALIBRATION: @modi code-trace APPROVE (shell harness dead entire round — persistent grok-seat event; solid proof-path audit, revision-after-incumbent-signal in round 1). ONE non-blocking P2 taken as a PRE-MERGE micro item (maps to keep-list schema-pinned-vendor-db fence): InstancePID is an unpinned db reader — silent fail-closed on schema drift would regress codex binds to 6/6 with no test failing; fix = pin user_version + pid column + tagged real-db fixture. Dispatched; incumbent re-verifies only that.
---

created: 2026-07-17 15:18
---
MERGED a5234d9 (--no-ff, pushed) AFTER U2 with mandatory RE-GATE on the combined tree (both units touched the identity spine — 276 in hcomidentity/sidecar/spawn, U2 in repaircmd/registry): re-gate green 61/61 + 4 modules. Final head b52afd6 = fix + schema-pin. Chain: root-cause evidence (5/5 field refusals + lived spawn refusal) -> design checkpoint (approved with redirect: exact-Name Evidence correlate, NOT the attested arm) -> build -> review round 1 (opus incumbent danu + grok calibration modi) -> fix round (P1 stranger-row: require process proof not name-alone; P2 noop-latch: mirror spawn verification) -> delta APPROVE + one pre-merge schema-pin item -> final APPROVE. All findings mutation-verified; danu verified recovery against the REAL hcom db and schema (accepts real, rejects both drift modes, adhoc NULL-pid not misreported). CROSS-UNIT SEAM settled by danu unprompted: repaircmd never sets Evidence.Name, so 276's name-verify path cannot skip U2's attested-evidence-class arm (no silent live-verified downgrade); U2's converse guard handles verified-disagreement; re-gate confirms. Orchestrator gates: independent + delta-scoped + re-gate all 61/61, identifier sweep clean. Boundary honestly drawn: grok ENROLL still blind (TASK-277), grok SPAWN covered via generation-fenced bridge name. Field-proven: this fix heals the exact class bali reported live (@derive-kulu bind-timeout-then-late-join).
---
<!-- COMMENTS:END -->
