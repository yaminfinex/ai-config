---
id: TASK-063
title: >-
  status lines: codex needs context in status line; claude+codex get
  herdr/hcom/herder status segments
status: In Progress
assignee:
  - vibe
created_date: '2026-07-08 07:19'
updated_date: '2026-07-08 09:31'
labels: []
dependencies: []
ordinal: 63000
---

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Owner request (2026-07-08). Current state: claude has claude/statusline.sh (robbyrussell-style; project/branch, model+effort, context used%/kK, cost, and an env-only herdr segment: HERDR_PANE_ID + HERDER_ROLE when HERDR_ENV=1). Codex has NO status line configured — ~/.codex/config.toml has no status keys and the repo does not manage codex config at all.

Scope:
1. Codex status line: get context usage (used% / tokens) visible, plus model and cwd/branch parity with claude where the surface allows. First step is investigating what codex CLI actually supports (config.toml status keys, custom statusline command hook, or only the built-in status bar) — if the surface is missing, note the upstream gap on the ticket and do the best available (e.g. notify/title hooks).
2. herder/hcom status in BOTH status lines: extend claude's herdr segment beyond pane_id+role — herder label (HERDER_LABEL), hcom bus name, and ideally a bus signal (unread count or last-message age). Mirror on codex if its surface allows.
3. Constraint (already encoded in statusline.sh comment): statusline renders on every turn — NO subprocess calls to herdr/hcom/herder per render. Pure env reads are free; anything live must be cached (e.g. sidecar or hook drops a small state file the statusline reads).
4. Bring the codex config under ai-config management like claude/ (settings.shared + local example pattern) so the statusline ships from this repo.

Notes: HERDER_GUID/HERDER_LABEL/HERDER_ROLE/HCOM env are already injected into spawned agents (see spawncmd); hcom unread state would need a cheap source — check what ~/.hcom exposes as flat files before inventing one.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 09:17
---
[hera, from vibe #10209] Dispatched: codex worker task063-taro, worktree task-063-statusline (base main post-047). Phased brief: Phase 0 = authoritative codex statusline surface investigation (worker IS codex — exact config keys or documented upstream gap + version), gating implementation shape; herder/hcom env segments both CLIs (spawncmd/launchcmd read-only survey); bus signal DESIGN-FIRST, reader-only shipping (no flat state file in ~/.hcom today; hookcmd/sidecarcmd/registry fenced off while A5 live — any writer is a scoped follow-up for hera sequencing); codex config management extends ai-setup with idempotence/backup contracts green.
---

created: 2026-07-08 09:31
---
[hera 2026-07-08] Vibe hand-back (#10333): worker task063-taro, commit 32c4666. HERA GATE: vet/test both modules green; 24/24 pre-existing suites green; NEW suite check-ai-setup-codex-config.sh FAILED bare execution — committed mode 100644 (all others 100755), Permission denied in the bare gate loop (content ALL GREEN when forced through bash). Mode fix routed and landed: 6dddb86 (vibe verified bare execution green; vibe gate template now runs bare per doctrine). Phase 0 answer (codex-cli 0.142.5): NATIVE footer config only ([tui].status_line item ids + terminal_title), no custom command hook — herder/hcom segments cannot render in codex footer until upstream adds one; recorded on 029 ledger. HERA RULING on vibe pointer (c): default-install YES (owner DX intent; backup+idempotence+preservation make it safe) — reviewer probes execution semantics. Adversarial review dispatched: review-063-magi (guid a5a0ae2a, own tab), brief napkins/run-herder-dx/brief-review-063.md — leads with awk-TOML-editing torture of the USER's live ~/.codex/config.toml (subtables, duplicate sections, multi-line arrays, symlinked configs, rollback-vs-backup), statusline reader injection, default-install execution, remove semantics. Bus-snapshot WRITER follow-up filed as TASK-067.
---
<!-- COMMENTS:END -->
