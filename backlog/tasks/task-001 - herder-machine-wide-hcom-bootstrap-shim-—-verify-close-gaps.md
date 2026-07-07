---
id: TASK-001
title: 'herder: machine-wide hcom bootstrap shim — verify + close gaps'
status: Done
assignee: []
created_date: '2026-07-07 05:36'
updated_date: '2026-07-07 06:26'
labels:
  - run-herder-bootstrap
dependencies: []
priority: high
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
DECISION (user, 2026-07-07): bootstrap takeover goes machine-wide, not just per-spawn.

Context: herder-spawned agents get the herder-native session bootstrap because spawn prepends tools/herder/shims to the child PATH (shims/hcom -> herder hook, which rewrites hcom sessionstart additionalContext via hookcmd/template.go). DISCOVERY: ~/.config/mise/conf.d/ai-config.toml (managed by bin/ai-setup --shims) ALREADY puts tools/herder/shims on PATH machine-wide, and that dir contains the hcom shim — so hand-launched sessions in mise-activated shells may already be covered. This task is verify + close gaps, not greenfield.

Work:
1. Live-verify: launch claude by hand (NOT via herder spawn) in a mise-activated shell; confirm the session bootstrap is the herder-native rewrite, not stock hcom.
2. Enumerate bypass vectors and close-or-document each: (a) installed hooks run cmd=${HCOM:-hcom} — an HCOM env var pointing at the real binary bypasses the PATH shim (hcom re-exports HCOM=hcom to children; check what hand-launched sessions inherit); (b) non-mise contexts (GUI-launched editors, launchd) miss the _.path injection; (c) recursion guards (HERDER_HOOK_HCOM preset + PATH-walk skipping the shim dir) must hold under global scope.
3. If a code/setup change lands, extend check-shims.sh / check-mise-path-install.sh accordingly.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Hand-launched claude (mise shell) demonstrably receives the herder-native bootstrap — live smoke evidence, not fixtures only
- [ ] #2 Every bypass vector enumerated with an explicit close-or-document decision
- [ ] #3 All tools/herder/tests/check-*.sh + go test/vet (herder AND bottle) green
<!-- AC:END -->
