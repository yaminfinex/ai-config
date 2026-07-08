---
id: TASK-040
title: >-
  hookcmd: reTag regex breaks on hcom 0.7.23 single-quote tag line — silent
  tag-line drop for team/tagged spawns
status: In Progress
assignee:
  - unit-aa-ruve
created_date: '2026-07-08 03:39'
updated_date: '2026-07-08 03:40'
labels: []
dependencies: []
priority: medium
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found by the TASK-028 audit, VERIFIED locally by hera: hcom 0.7.23 (bootstrap.rs:92) changed the stock bootstrap tag line from double to single quotes; herder's extract() scrapes it with reTag = 'You are tagged "([^"]+)"' (hook.go:235). Under 0.7.23 the regex misses, tag extraction 'succeeds' empty (tag is optional), and renderBootstrap silently DROPS the whole group-address line for tagged/team agents. The battery cannot catch it: hook_test.go:23 and check-hook-bootstrap.sh:71 feed canned double-quote fixtures, so all suites stay green against a broken live pairing. Fix: make reTag quote-agnostic ('You are tagged [\'"]([^\'"]+)[\'"]'), make the fixtures cover BOTH quote styles (0.7.22 is still installed — both must extract), keep rendered output stable. DEFERRED until the actual upgrade: a live tagged-spawn smoke under 0.7.23 confirming the group line renders (recorded here as the upgrade-time gate; see TASK-028 audit report for the full sequence).
<!-- SECTION:DESCRIPTION:END -->
