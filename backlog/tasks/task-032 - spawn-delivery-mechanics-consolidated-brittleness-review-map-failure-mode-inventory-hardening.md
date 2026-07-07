---
id: TASK-032
title: >-
  spawn delivery mechanics: consolidated brittleness review (map, failure-mode
  inventory, hardening)
status: Done
assignee:
  - unit-r-zulu
created_date: '2026-07-07 20:31'
updated_date: '2026-07-07 21:59'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
USER CONCERN (2026-07-07): TASK-024 then TASK-031 in quick succession suggest boot-time prompt delivery is brittle — "might be worth a consolidated review across spawning to make sure we do not have brittle mechanics". Diagnosis is part of the deliverable: WHY does this subsystem keep producing incidents?

Working hypothesis to test, not assume: bootpaste drives a TUI whose readiness is only indirectly observable, and each patch (TASK-023 notify resolution, TASK-024 verify evidence gating, TASK-031 boot-swallowed Enter) hardened one observed race while the underlying state machine (ready-wait → paste → land-verify → Enter retries → submit-verify, per agent family) has never been reviewed as a whole. Known facts: claude boots fast and has been reliable; codex boots slow, ready-wait can time out (status=done) before the TUI is interactive, Enters fired blind get swallowed, and a queued bus message cannot flush into an idle codex (no turn boundary) — so a stranded composer is a dead end without manual send-keys.

PHASE A (no code): map the full delivery state machine end-to-end for spawn --prompt and compact, per agent family (claude/codex/bash) — every timeout, retry, evidence gate, and what each failure reports. Place every known incident on the map with root cause. Enumerate residual race windows. Report the map + a ranked hardening proposal on your thread for ratification BEFORE any code. Consider whether ready-wait should be agent-family-aware (codex needs interactive-TUI readiness, not process-alive). PHASE B: implement the ratified subset.

SEQUENCING: TASK-031 (late-submit remedy) is GATED on this review — its fix should fall out of the map, not precede it. Evidence sources: run-log incidents (wave-3 dispatch failures, reviewer-kimi stranding), spawncmd/bootpaste.go + compact.go, launchcmd ready-wait, check-spawn/compact suites + goldens.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 PHASE A map artifact exists (napkin or docs): full delivery state machine per agent family with every timeout/retry/evidence gate named
- [x] #2 Failure-mode inventory: TASK-023/024/031 + wave-3 NOT-confirmed incidents each located on the map with root cause; residual race windows enumerated explicitly
- [x] #3 Ranked hardening proposal reported for ratification BEFORE code, incl. agent-family-aware readiness assessment and the TASK-031 late-submit question
- [x] #4 Ratified subset implemented with suite/golden coverage; TASK-024 evidence gating not weakened; battery green
- [x] #5 TASK-031 resolved or formally superseded; spawn NOT-confirmed hint + README delivery section match post-hardening reality
- [x] #6 QUESTION ZERO (user, 2026-07-07): why is a paste required at all — can spawn initial-prompt delivery ride the hcom bus (TASK-017-style post-registration send) instead of bootpaste? Evaluate explicitly: readiness (registry-bind poll), first-turn semantics (does a bus message wake a never-prompted fresh session, per family — lusa smoke says idle codex delivers; reviewer-kimi dirty-composer says not always), framing (<hcom>-tagged vs plain user prompt), bash/bus-less spawns, slash-command prompts. If viable, bootpaste retires to compact-only and the whole paste/Enter state machine collapses — answer this BEFORE proposing paste hardening
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
DONE by worker-zulu (Unit R, wave 5; branch unit-r-spawn-review; hygiene via orchestrator pen from #3231/#3333/#3388). PHASE A: map napkin (unit-r-spawn-delivery-map.md) — Question Zero answered YES via live probes rosa/keto/vila/rina: bus delivery wakes never-prompted fresh sessions of BOTH families <1s on empty composer; DIRTY composer starves silently on both (kimi 8h); hcom is queue-until-deliverable (send at bind+107ms into booting codex: zero loss). Bootpaste manufactured the exact state that defeated its own fallback — TASK-031 dead end was self-inflicted. PHASE B commits: 9d5064f+2361dfc+e6534bb+f60505e send receipt verification made real+message-specific (verify=delivered was TRIPLY broken: wrong query side — receipts live on RECEIVER as deliver:<SENDER>; same-second --after exclusion; JSONL-vs-array parse — then review hardened: strictly-newer-than-snapshot id + per busDir/sender/target flock for concurrent sends); 6feb865 B1 bus-first spawn delivery (awaitBind HERDER_SPAWN_BIND_MS 60s → in-process DeliverBus full prompt → receipt verify HERDER_SPAWN_VERIFY_MS 20s; codex brief staging deleted; vocabulary delivered/queued/send_failed/not_joined/bind_timeout/ready_match_timeout); 222b1bb review P1: prompt bind gate child-specific only (this-guid enrichment or frozen-launch-pane), bind_ambiguous unreachable+removed; 17b6cd7 README P3. Bootpaste retired to compact+bash, TASK-024 floor untouched (pinned by compact suite + bash_prompt golden). Rulings: --ready-match = additional send gate; --no-ready-wait no-op for bus prompts. --json: brief_file field REMOVED. CODEX REVIEW: 3 rounds — P1 misdelivery-class + P2 receipt races caught by review, not gates; final APPROVE. VERIFICATION: hera battery green in worktree post-DONE, post-P1/P2, post-round-3 (17/17+go each time); live smokes: codex reviewer spawn via R binary = verify delivered (receipt seen) — the kimi scenario dogfooded; TestConcurrentSendsAreSerialized fails unlocked 2/2, passes locked. FOLLOW-UPS: TASK-033 (row enrichment residual), relay.md stale resend line (hera fixes at integration), TASK-029 candidates 7/8/9.
<!-- SECTION:NOTES:END -->
