# Fixture corpus — real captured session JSONL

Every fixture is cut from a real session transcript on a real machine (house
ruling: goldens are never synthesized shapes). Captured 2026-07-09 on
grace's linux workstation by the U2 worker. Do not edit fixture bytes; recut
from a real source and update this README instead.

## Scrub checklist (run on every fixture before commit)

1. Pattern scan, case-insensitive: `sk-ant`, `sk-<20+ alnum>`, `api[_-]key`,
   `ghp_/gho_/github_pat/glpat`, `xox[bap]-`, `AKIA/ASIA<16>`,
   `BEGIN * PRIVATE KEY`, `password/passwd/secret`, `bearer `, `authorization`,
   `AIza`, `npm_`, `ssh-rsa/ssh-ed25519`, `eyJ<20+> (JWT)`, `netrc`,
   `credential`.
2. High-entropy token scan: all strings matching `[A-Za-z0-9+/_=-]{28,}` with
   Shannon entropy > 4.2, excluding known-benign fields (`signature`,
   `requestId`, uuid/id fields) and known-benign classes (uuids, hex digests,
   `toolu_*` ids, file paths).
3. Human-authored surface review: every user prompt, every Bash `command`
   string, and codex `user_message` events read by the reviewer.
4. Result for all six fixtures: **no credentials found.** One pattern hit was
   a false positive — a case-insensitive `eyJ` substring inside a
   `message.content[].signature` field (Anthropic's opaque thinking-block
   signature, part of the transcript format, kept intact).

Content note: transcripts reference grace's local projects (paths, prompts,
code excerpts). That is transcript content, not credential material; the repo
is private. A leaked secret in fixtures is a repo incident — when in doubt,
hold the fixture back (playbook rule).

## Fixtures

### claude-normal.jsonl — normal Claude Code session

- Source: `~/.claude/projects/-home-grace-Coding-ai-config/45308169-72e6-4cbe-a05c-2a0025db055e.jsonl` (verbatim copy; source mtime 2026-07-02)
- 25 lines, 38,976 bytes, every line valid JSON, ends with `\n`.
- Entry mix: user/assistant/attachment/mode/permission-mode/ai-title/last-prompt;
  3 tool_use + 3 tool_result blocks; monotonic timestamps.

### claude-resume-original.jsonl + claude-resume-new-file.jsonl — resume-into-new-file pair

- Sources (verbatim copies, same project dir, source mtimes 2026-06-28):
  `~/.claude/projects/-home-grace--herdr-worktrees-synfinex-single-sequencer-phase1-zerocopy/2c387aef-72ac-46bc-8ea5-e3b68690a937.jsonl` (original, 206 lines)
  and `.../e1be75ad-151b-47fa-9d69-46de1c117843.jsonl` (resumed, 269 lines).
- Claude Code v2.1.195 resume wrote the session's history into a NEW file
  uuid: 141 message uuids overlap (resumed file lines 3–202 ≈ the original's
  history; first new entry 39 s after the original's last write; the original
  has one post-fork entry of its own).
- Verified property (reported on thread sesh-u1, blocks the wire freeze): the
  in-content `sessionId` of every line equals its own FILE's uuid — the copied
  history was rewritten with the new id, and no field in the resumed file
  references the original session. Content session ids do NOT unify a resume
  pair; only message-uuid overlap does. This is the S2 dedup case.

### claude-trailing-partial.jsonl — trailing partial line (mid-write read state)

- Source: byte prefix of `claude-normal.jsonl` above — the first 20,000 bytes
  of the real file, cutting line 6 mid-string.
- Rationale (orchestrator-confirmed on thread sesh-u2): a racing reader of an
  append-only file sees exactly a byte prefix, so this is real bytes with a
  chosen read boundary, not a synthesized shape. No file on this machine was
  caught ending mid-line (all 893 checked; live files polled at 50 ms for
  60 s — appends look atomic here), so the read-race state was cut rather
  than staked out. The untruncated original stays in the corpus, giving the
  held-back-then-completed ingest test its "after" state for free.

### claude-interleaved-writers-standin.jsonl — interleaved writers (STAND-IN)

- Source: `~/.claude/projects/-home-grace--herdr-worktrees-synfinex-fable-pass/e4578030-c4a9-493f-82e6-de6156d0179a.jsonl` (verbatim copy; source mtime 2026-07-02)
- HONEST LABEL: this is NOT a genuine two-writer file. No file machine-wide
  has two content sessionIds or duplicate uuids; on this Claude Code version,
  two terminals resuming one session each write their own new file (this
  contradicts the prior-art two-terminal interleave claim — gap recorded by
  the orchestrator for the design record).
- What it IS: a real single-session file with 3 forked parentUuid chains
  (two chains alternating over lines 41–52, queued user messages attached to
  successive assistant nodes) — the closest real artifact exercising the same
  parser property: entries whose chain order is non-linear. OPEN GAP: recut
  from a genuinely affected Claude Code version when one is observed.

### codex-rollout-meta.jsonl — Codex rollout with session_meta header

- Source: `~/.codex/sessions/2026/06/26/rollout-2026-06-26T02-43-06-019f01cf-3d22-7ea0-923e-e463b90ea31e.jsonl` (verbatim copy; source mtime 2026-06-26)
- 11 lines, 40,625 bytes. Line 1 `type: "session_meta"` (payload: id,
  timestamp, cwd, originator, cli_version, source, thread_source,
  model_provider, base_instructions, git). Then event_msg/response_item/
  turn_context lines; all valid JSON, ends with `\n`.

### grok-chat-history.jsonl — Grok CLI session transcript

- Source: `~/.grok/sessions/<url-encoded scratchpad cwd>/71ebdd45-2641-49e8-87f5-b8d9f3706714/chat_history.jsonl`
  (verbatim copy; source mtime 2026-07-09; a grok characterization session
  run on grace's linux workstation — captured 2026-07-14 by the grok adapter
  lane worker). The session directory UUID is v4 here; live grok default is
  uuidv7 — both occur on this machine and discovery accepts any UUID.
- 26 lines, 26,457 bytes, every line valid JSON, ends with `\n`.
- Entry mix: 1 system, 7 user, 6 reasoning, 7 assistant (5 with
  `tool_calls`), 5 tool_result — the full type spread observed live. NO
  timestamps and NO message uuids anywhere (verified property: grok index
  rows never dedup; recency falls back to first-ingest).
- Scrub result: checklist run 2026-07-14; one case-insensitive `authorization`
  pattern hit is prose inside the grok system prompt (false positive);
  high-entropy hits are paths and call ids (known-benign classes). No
  credentials found. User prompts are characterization test prompts; tool
  calls are `echo`/`sleep`/`grep` only.
