---
id: TASK-201
title: >-
  Dissolve grok managed-home machinery — default vendor home + vendor binary per
  owner ruling
status: Done
assignee: []
created_date: '2026-07-14 01:47'
updated_date: '2026-07-14 03:05'
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
MERGED 6efacee (20 files, +601/-601-class). Dissolution: seedGrokHome / managed GROK_HOME / HERDER_GROK_CHILD_HOME / version-pin gating removed; launch resolves vendor grok by PATH; ~/.grok is the working home; ai-doctor observational. KEPT+retested: bridge, GROK_CLAUDE_HOOKS_ENABLED=0 suppression, identity-env allowlist, credential-by-name, owner-config immutability.
FIRST ATTEMPT (c7a94e6) registered the hcom bridge MCP via an INVENTED --plugin-dir/plugin.json surface — BOTH reviewers rejected (solu: real grok 0.2.99 rejects --plugin-dir on the interactive TUI; ziri: only characterized surface is config.toml [mcp_servers]). Worker then CHARACTERIZED the real binary and found the admissible surface: project-scope <launch-cwd>/.grok/config.toml [mcp_servers.hcom] (snake_case), resolved from the project layer with owner ~/.grok absent, no GROK_HOME. FIX (1264391): cwd-binding invariant (config-dir == grok effective cwd) enforced in code + pinned by a real-binary integration test (project-layer resolve; --cwd divergence -> zero layers); symlink-escape refusal; unrelated-TOML preservation; 0600 no-rewrite. Honest scoping: resolved-set evidence, not live-TUI activation.
GATE 58/58 (worktree) + 58/58 (post-merge main). DUAL APPROVE 1264391: ziri (opus incumbent, authority) + solu (grok calibration), only non-blocking P3s. Reviews: review-201-brief.md, implement-201-brief.md. Follow-up agent work remains filed (TASK-191 binder-env). Retires the managed-home machinery per owner default-homes ruling (standing-orders 20.8).
<!-- SECTION:NOTES:END -->
