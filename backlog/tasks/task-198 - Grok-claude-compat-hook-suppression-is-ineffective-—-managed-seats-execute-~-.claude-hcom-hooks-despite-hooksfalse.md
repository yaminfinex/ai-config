---
id: TASK-198
title: >-
  Grok claude-compat hook suppression is ineffective — managed seats execute
  ~/.claude hcom hooks despite hooks=false
status: To Do
assignee: []
created_date: '2026-07-13 22:49'
labels: []
dependencies: []
ordinal: 197000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
VERIFIED 2026-07-14 in a live managed grok reviewer session: updates.jsonl records hook_execution events named global/settings:<event>[0].hooks[0] for session_start, user_prompt_submit, pre_tool_use, post_tool_use, and stop — 45 executions in one session, each ~45-55ms, status success. These are the hcom claude hooks from ~/.claude/settings.json, executed by the grok claude-compat layer. Herder controlled config writes [compat.claude] hooks = false into the managed GROK_HOME config.toml (seedGrokHome), and the vendor hooks guide names that exact switch — but suppression demonstrably does not take effect. Likely cause: grok reads the compat toggle from the real HOME (~/.grok/config.toml — HOME is NOT re-pointed for grok seats unless HERDER_GROK_CHILD_HOME is set) rather than GROK_HOME, or the config surface changed in the pinned 0.2.93. Impact: an uncontrolled code path (hcom claude-protocol verbs with ambient env) runs on every prompt/tool-call/turn of every managed grok seat — same boundary class as the closed identity-env task; currently harmless-looking (fast, exit 0, integration rides bridge+MCP not hooks) but unowned. Fix space: make the suppression effective on the surface grok actually reads (without touching the live ~/.grok — owner-preserved); or neutralize via env (GROK_* compat env var per vendor docs) in the launch allowlist; pin a test that a managed seat session records ZERO hook_execution events. Candidate upstream flag if the config surface is a vendor bug.
<!-- SECTION:DESCRIPTION:END -->
