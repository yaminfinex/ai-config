# Git view live evidence

Captured from the worktree-built Herder executable on `127.0.0.1:4493` with
`AI_CONFIG_ROOT` unset. The required `/home/ubuntu/Coding/ai-config` and
`/home/ubuntu/Coding/missions` roots were served alongside this linked worktree
and a temporary non-Git evidence root. The existing Herder listener on port
4400 was left untouched.

Build identity from the SSE `hello` event:

`executable:ed6d4dff979c737d584116140d197e40f82bd60a0791982ac55c6f8897d335ed`

Evidence map:

- `diff-uncommitted-linked-worktree-light.png` — tracked uncommitted edit vs HEAD, light theme.
- `diff-branch-linked-worktree-dark.png` — all branch work vs the proved `origin/main` merge-base, dark theme.
- `current-markdown-rendered-light.png` and `current-markdown-source-light.png` — Markdown Rendered/Source views.
- `current-line-73-highlight-light.png` — `:73` opens Current source and selects the requested line.
- `history-50-cap-note-light.png` — exactly 50 rows and the honest older-history-unavailable note; no load-more control.
- `history-as-of-revision-light.png` — immutable historical blob at commit `6876e9f1ac61`.
- `history-commit-diff-light.png` — commit-vs-parent patch labeled “What commit 6876e9f1ac61 changed”.
- `changes-from-agent-strip-dark.png` — Changes opened from impl-zeli's exact-root agent strip.
- `truncated-diff-dark.png` — 1,433,485-byte patch capped at 256 KiB with the truncation banner.
- `renamed-file-facts-dark.png` — rename fact from the prior generated CSS asset path.
- `binary-file-state-dark.png` — honest binary current-file state with byte size and no text body.
- `non-git-root-disabled-modes-dark.png` — Current remains readable while Diff/History are disabled with “not a git repository”.

The temporary tracked edit and temporary non-Git file used to exercise those
states were removed immediately after capture and are absent from the final
branch.
