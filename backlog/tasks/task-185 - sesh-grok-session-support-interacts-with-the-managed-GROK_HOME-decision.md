---
id: TASK-185
title: 'sesh: grok session support (interacts with the managed GROK_HOME decision)'
status: Done
assignee:
  - mika
created_date: '2026-07-13 06:21'
labels: []
dependencies: []
priority: medium
ordinal: 184000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner ask (2026-07-13): sesh should support grok sessions (store/resume)
alongside claude/codex. PREMISE REWRITTEN 2026-07-14: the managed-GROK_HOME
tension this task was filed around is GONE — the grok dissolution merged on
main removed the managed home entirely. Post-dissolution reality (verified
against merged main + a real ~/.grok by the herder domain owner):

- ONE home, ONE namespace: all grok sessions (manual + herder-seat) live
  under ~/.grok/sessions/. No home-targeting tension; resume = "session id
  exists under ~/.grok/sessions".
- Layout: sessions/<url-encoded-cwd>/<uuidv7-sid>/ — the first level is the
  percent-encoded working directory (e.g. sessions/%2Fhome%2Fgrace/<sid>/).
  Discovery glob: sessions/*/<sid>. The cwd group signals provenance but is
  NOT a seat-vs-manual discriminator (manual runs in the same cwd share the
  group); if seat-tagging is ever wanted it must cross-reference herder
  lifecycle state, not paths — OUT OF SCOPE here.
- Artifacts per session dir: chat_history.jsonl (transcript), events.jsonl,
  plus prompt_context.json, resources_state.json, rewind_points.jsonl,
  signals.json, recap_requests/. Reference parser with cursor/rotation
  handling already exists: tools/herder/internal/observercmd/grok.go
  (grokSessionDir, observeGrokSession) — reuse its format knowledge, do not
  reinvent line handling.
- MUST NOT ship: everything at the ~/.grok TOP level (config.toml,
  active_sessions.json/.lock, managed_config.lock, agent_id,
  credential/auth files, logs/, downloads/, bin/, completions/,
  marketplace-cache/, models_cache.json, CHANGELOG*) — scan ONLY
  sessions/*/<uuidv7-sid>/. Project-scope .grok/config.toml files in
  worktrees are config, never transcripts; the adapter keys on the
  sessions/ tree and ignores any .grok/config.toml it meets.
- Resume: grok has no native fork (raw tool-fork fallback exists in herder);
  a sesh resume surface targets plain session-id existence.

Scope: a clean adapter task — discover/parse/ship ~/.grok/sessions through
the existing sesh ship pipeline (wire v1 frozen; per-file append semantics
as for claude/codex), with which-files-ship decided explicitly
(chat_history.jsonl at minimum; justify inclusion/exclusion of the
auxiliary jsonl/json files against the index contract).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 sesh discovers and ships grok sessions from ~/.grok/sessions/*/<uuidv7-sid>/ (shipped-file set explicitly decided and documented); top-level ~/.grok config/creds/runtime state provably never ships (test pins the exclusion)
- [x] #2 Grok transcripts index and render on the surface (tool=grok end to end); never-500 holds for grok sessions
- [x] #3 Resume surface: a stored grok session can be located by session id consistent with plain ~/.grok/sessions existence semantics
- [x] #4 Docs current per decision-001 (README tool support matrix; wire/index notes if the adapter needed any)
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Merged to main at 72a63d4 (--no-ff, 9 linear commits, 29 files), pushed;
deployed live as sesh-v0.1.11 (store/release string carries a
git-describe suffix from unrelated same-day main commits — zero touch
tools/sesh, binary equivalent to the tag).

Live verification from this node (real ~/.grok): 10 grok session cursors
shipping, grok sessions rendering on the flat sessions list, a real grok
transcript page served 200 in 0.38s (252KB), census reporting the new
version. Wire Amendment 3 (grok into the closed tool enum) landed as the
FIRST commit per the STOP-gate ruling, protocol otherwise byte-untouched,
with a mixed-fleet compatibility clause.

Shape: chat_history.jsonl only (structural identity: session-dir UUID =
session_id = file_uuid); auxiliaries excluded with reasons; exclusion is
exact-shape admission proven against symlink/traversal/case/depth
evasions with a 4-negative detector. Empty message_uuid = non-participant
in all five dedupe/unify paths (independently probed: identical opening
lines never unify sessions). Grok rewinds ride the existing
byte-conflict -> generation flow; duplicated-prefix degraded floor
documented. Bonus fix: the recovery-GET path previously ABORTED the whole
pass on an unrecognized tool — a mixed-fleet wedge affecting all tools —
now parks per-file and re-ships after store upgrade.

Review: codex reviewer stalled on usage limits mid-review (partials
recorded, one finding closed: boundary-test detector evadable);
continuation on claude-opus with mandatory probe reproduction (same-family
noted), APPROVE with both corruption/security dimensions reproduced from
scratch; hera added her own domain spot-checks at the gate. Fixture
identity question dispositioned as conforms-to-documented-precedent;
corpus-wide owner decision filed separately (fixture identity policy
task). Post-merge battery 58/58 green.

Flagged for post-deploy (accepted): real-Mac discovery verification, live
grok process correlation, real rewind capture.
