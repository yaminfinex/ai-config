---
id: TASK-076
title: >-
  statusline snapshot: publish context metrics (CTX_PCT/CTX_TOKENS/CTX_SIZE) for
  orchestrator reads
status: Done
assignee: []
created_date: '2026-07-08 20:27'
updated_date: '2026-07-09 13:07'
labels: []
dependencies: []
priority: high
ordinal: 76000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner question (2026-07-08): is pane read really the only way to read a spawned agent's context? Today yes — claude/statusline.sh RECEIVES context_window.used_percentage/total_input_tokens/context_window_size on stdin (lines 19-21) but only renders ctx:NN% into the pane; the $HCOM_DIR/statusline/<instance>.env contract (TASK-067) is one-way (herder sidecar -> statusline: HCOM_UNREAD/LAST_TS/LAST_AGE_S). Orchestrator options are pane-scrape or transcript-JSONL parsing, both fragile.

FIX: make the env-file contract two-way. statusline.sh writes CTX_PCT/CTX_TOKENS/CTX_SIZE (atomic tmp+rename, same discipline as the 067 writer) into its own instance env file on each render; herder list grows a ctx column read from the snapshot dir; docs/status-lines.md contract updated. Staleness: include a CTX_TS so readers can distinguish fresh from last-render-hours-ago.

INVESTIGATE: codex status line — does its hook receive equivalent context data (TASK-063 built it; check its input schema) or does codex need a different source (rollout file tail)? If codex can't publish, herder list must render absence honestly (unknown, not 0%).

WHY HIGH: this is the enforcement mechanism for TASK-075 doctrine — the 200-250k compact band applies to workers, and a standing rule that requires pane-scraping to check will not get checked. Depends on: nothing (067 writer + contract already on main). Cross-file: TASK-075 capture should reference 'statusline snapshot' as the intended read path once this lands.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 20:49
---
Context-measurement sources validated live by task075-zore (owner-commissioned investigation): CLAUDE = transcript JSONL ~/.claude/projects/<cwd-slug>/<session-id>.jsonl, last non-sidechain assistant message .message.usage (input + cache_read + cache_creation tokens); registry v2 already holds session id + cwd to resolve the path. CODEX = rollout JSONL ~/.codex/sessions/.../rollout-<ts>-<uuid>.jsonl, last token_count event, .payload.info.last_token_usage.total_tokens and .model_context_window (validated: 61768/258400 = 23.9%). Both are pure file reads by session id — no pane interaction. This answers this task's INVESTIGATE item and adds an implementation option: a herder ctx column can read these two sources directly per tool kind, with the statusline env snapshot remaining the better eventual source (carries window size + freshness/CTX_TS). Owner: 'not against building something into herder'.
---
<!-- COMMENTS:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 claude statusline.sh writes CTX_PCT/CTX_TOKENS/CTX_SIZE/CTX_TS into its instance env file on each render (atomic tmp+rename, same discipline as the TASK-067 writer)
- [ ] #2 herder list shows a ctx column sourced from the snapshot dir; absence/staleness rendered honestly (unknown, not 0%; CTX_TS drives staleness)
- [ ] #3 codex source resolved: statusline publishes equivalent metrics, or list falls back to the validated rollout-JSONL read (comment 1), or absence is rendered unknown — one of the three, decided and implemented
- [ ] #4 docs/status-lines.md contract updated for the two-way env-file protocol
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged (18b7ed0+832aad4+7238914) after three review rounds by data: hostile-input hardening, collision resurrection closed, R1 limitation documented. Codex publishes honest unknown (no statusline hook upstream).
<!-- SECTION:NOTES:END -->
