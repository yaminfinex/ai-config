---
id: TASK-201
title: >-
  Dissolve grok managed-home machinery — default vendor home + vendor binary per
  owner ruling
status: In Progress
assignee: []
created_date: '2026-07-14 01:47'
updated_date: '2026-07-14 02:03'
labels: []
dependencies: []
ordinal: 200000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER RULING 2026-07-14 (default homes; recorded standing-orders 20.8): grok seats run against the LIVE default home (~/.grok) and the vendor-installed auto-updated binary; the managed-home layer dissolves. Settled decisions (do not relitigate): single-purpose machines; ringfencing expressly not required; claude/codex live-home fleet norm extends to grok; seat-scoped behavior deltas ride LAUNCH ENV only (GROK_CLAUDE_HOOKS_ENABLED=0 stays), never owner config writes. SCOPE: remove seedGrokHome + managed GROK_HOME pinning + HERDER_GROK_CHILD_HOME machinery and the 0.2.93 version-pin gating from launch (launchcmd + spawncmd); launch resolves the vendor grok normally (PATH/shim semantics decided in-unit and documented); KEEP the bridge, the identity-env allowlist, the hooks-suppression env override, and credential scoping in env construction; update tests/goldens accordingly (the zero-hook_execution acceptance test must survive against default-home launch shape); update README/docs where managed home is described. SEQUENCING: dispatch ONLY after the grok steady-state and hook-suppression branches merge — same files. ALSO SUBSUMES the ai-doctor vendor-probe capture: with the preservation regime retired the quarantine premise dissolved; retain one line item — a default doctor run should report agent binaries without surprising side effects, assess while touching launch code. Full battery + adversarial review per house rules.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Dispatched worker-muho (codex 5.6 high, worktree task-201, thread task201) — unblocked by 197+198 merges.
<!-- SECTION:NOTES:END -->
