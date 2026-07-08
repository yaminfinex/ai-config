---
id: TASK-040
title: >-
  hookcmd: reTag regex breaks on hcom 0.7.23 single-quote tag line — silent
  tag-line drop for team/tagged spawns
status: Done
assignee:
  - unit-aa-ruve
created_date: '2026-07-08 03:39'
updated_date: '2026-07-08 04:32'
labels: []
dependencies: []
priority: medium
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found by the TASK-028 audit, VERIFIED locally by hera: hcom 0.7.23 (bootstrap.rs:92) changed the stock bootstrap tag line from double to single quotes; herder's extract() scrapes it with reTag = 'You are tagged "([^"]+)"' (hook.go:235). Under 0.7.23 the regex misses, tag extraction 'succeeds' empty (tag is optional), and renderBootstrap silently DROPS the whole group-address line for tagged/team agents. The battery cannot catch it: hook_test.go:23 and check-hook-bootstrap.sh:71 feed canned double-quote fixtures, so all suites stay green against a broken live pairing. Fix: make reTag quote-agnostic ('You are tagged [\'"]([^\'"]+)[\'"]'), make the fixtures cover BOTH quote styles (0.7.22 is still installed — both must extract), keep rendered output stable. DEFERRED until the actual upgrade: a live tagged-spawn smoke under 0.7.23 confirming the group line renders (recorded here as the upgrade-time gate; see TASK-028 audit report for the full sequence).
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
reTag was double-quote-only; hcom switched tag quoting to single quotes at 0.7.23, so a 0.7.23 stock bootstrap would silently drop the tag group line on the herder-native sessionstart rewrite. Fix: quote-agnostic matched-pair regex (no mixed-quote match; extraction takes whichever capture fired), byte-stable rewrite proven for both styles (Go TestExtract_TagQuoteAgnostic + check-hook-bootstrap §5b dual-fixture assert_eq). template.go / rendered output unchanged; drift guards untouched. Live 0.7.23 tagged-spawn smoke DEFERRED to hcom-upgrade time (upgrade gate: merge this -> hcom update -> live tagged smoke; see TASK-028 notes). Merged e534220; hera gates green (worktree 18/18; post-merge main 18/18 on re-run — first run had a check-spawn-contract flake, green standalone + full re-run, suspected shared-tmp build-cache contention with concurrent worker batteries). Review: hera line-by-line scrutiny in lieu of codex pass (sliding door: 1-line audit-specified regex + tests; recorded).
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: hera
created: 2026-07-08 04:32
---
UPGRADE GATE PASSED (2026-07-08): machine moved to 0.7.23 (pin af9954b -> bin/ai-setup --shims install; stale mise 0.7.22 + ubi + curl-orphan installs removed). Live tagged-spawn smoke: throwaway claude (@smoke040-zuni) quoted back "You are tagged 'smoke040'. Message your group: hcom send @smoke040- -- msg" — single-quote bootstrap extracted + rendered correctly. Spawn bind/deliver also verified on 0.7.23. Full procedure captured in docs/hcom-upgrade.md.
---
<!-- COMMENTS:END -->
