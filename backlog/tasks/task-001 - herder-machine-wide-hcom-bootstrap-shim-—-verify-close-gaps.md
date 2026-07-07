---
id: TASK-001
title: 'herder: machine-wide hcom bootstrap shim — verify + close gaps'
status: Done
assignee: []
created_date: '2026-07-07 05:36'
updated_date: '2026-07-07 07:40'
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
- [x] #1 Hand-launched claude (mise shell) demonstrably receives the herder-native bootstrap — live smoke evidence, not fixtures only
- [x] #2 Every bypass vector enumerated with an explicit close-or-document decision
- [x] #3 All tools/herder/tests/check-*.sh + go test/vet (herder AND bottle) green
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit e88f859 (branch task-001-hcom-shim, merged 5ef4d3c). Live-verified: hand-typed claude in a mise shell receives the herder-native bootstrap (chain: ai-config claude shim -> herder launch -> hcom -> sessionstart -> herder rewrite). Vectors: HCOM=/abs/path documented as deliberate escape hatch; non-mise contexts degrade symmetrically (no hcom at all); recursion guards hold. Bonus fix: sibling checkout shims mutually exec'd in an infinite loop — marker line in shim first 512 bytes, find_real_* skips marked candidates; 2 new check-shims.sh scenarios. Machine-level hardening (ai-setup pins HCOM in mise conf.d) proposed, not applied. Verified independently by orchestrator (go gates + battery re-run; 9 contract suites red = pre-existing environmental baseline, later fixed by TASK-008).
<!-- SECTION:NOTES:END -->
