---
id: TASK-063
title: >-
  status lines: codex needs context in status line; claude+codex get
  herdr/hcom/herder status segments
status: In Progress
assignee:
  - vibe
created_date: '2026-07-08 07:19'
updated_date: '2026-07-08 10:03'
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

Delta re-verdict from review-063-magi on 61ab259: all six ruled fixes VERIFIED in sandbox (B1 order-safe pre-arg-loop capture; M1 symlink write-through incl dangling refusal; M2 record/restore round-trip exact; L1 graceful skip on unsafe shapes; L2 small_uint overflow guard; N2 doc drift). ONE NEW MEDIUM blocks merge:

[D1] Default ai-setup aborts whole run under set -euo pipefail when codex step returns non-zero — bin/ai-setup:224 calls codex_config_apply_default bare. Paths: (a) python3 absent (shared_valid logs "skipping" but returns 1 — pre-existing); (b) dangling ~/.codex/config.toml symlink (resolved_file returns 1 — NEW in 61ab259, M1 fix widened blast radius). Both reproduced by reviewer.

hera ruling: fix both. Round-3 dispatched via vibe→taro: default invocation non-fatal (|| return 0 in apply_default), explicit --codex-config keeps error rc, suite case proving failing default path still completes ai-setup. Magi holding for delta scoped to D1 closure.

Round 3: taro fix 9a78841 (single || return 0 guard in apply_default covering both rc=1 sources; explicit path rc preserved; dangling_default suite case). Vibe independent gate green; hera regate green (vet+test both modules + 25/25 bare from worktree). Delta requested from magi scoped to D1 (probes: pre-guard non-zero paths in apply_default; explicit-path rc not swallowed).
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

created: 2026-07-08 09:35
---
[hera 2026-07-08] Opus adversarial verdict (review-063-magi, #10416): NOT CLEAN — 1 BLOCKER + 2 MEDIUM + 2 LOW + 2 NIT, all key findings sandbox-reproduced. B1: plain ai-setup FABRICATES ~/.codex (mkdir -p + fresh config) on machines with no codex — angle-3 ruling FAILS. M1: symlinked config.toml replaced by a plain file (dotfiles diverge silently). M2: remove DELETES managed keys instead of restoring pre-install values (angle-6 ruling: delete-not-restore). L1: multi-line arrays/inline comments/CRLF fail SAFELY (tomllib net aborts, live untouched) but config stays unmanageable with poor messaging. L2: overlong integers bypass the statusline whitelist (display garbage, no injection). Probed clean: subtable/duplicate-section handling, up-front non-TOML rejection, true idempotent no-op, ANSI/control injection blocked, env segments display-only. HERA RULINGS: B1 fix (default path guards existence; explicit install may create; skip flag); M1 write-through-symlink (resolve, tmp+mv in target dir, refuse dangling); M2 RESTORE semantics (record pre-install values in stable state file; restore on remove; delete only when nothing recorded); L1 safe-abort accepted + messaging + pin tests; L2 length-cap. Fix round routed to taro via vibe.
---

created: 2026-07-08 09:47
---
[hera 2026-07-08] Round-2 61ab259 regated green (25/25 bare, mode bit verified). Magi delta requested (B1 order guarantee + three repro re-runs). See TASK-059 comment for the shared pre-compact checkpoint.
---
<!-- COMMENTS:END -->
