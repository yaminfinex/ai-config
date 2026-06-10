# bottle testdata — fixture corpus + U1 spike findings

Sanitized Claude Code session fixtures for the `transcript` package (U3) and
the live-harness smoke test (`../scripts/smoke-decant.sh`). All findings below
were produced empirically on **Claude Code 2.1.170** (`claude --version`,
2026-06-10, Linux).

## The three deferred questions — answers and evidence

### 1. Leaf selection: what does `claude --resume` continue from?

**Answer: the last tree entry in the file. The `last-prompt` trailer's
`leafUuid` is ignored for leaf selection, and a stale/dangling `leafUuid` is
tolerated.**

Experiments (lab session with landmark turns "remember codeword ALPHA /
BRAVO / CHARLIE", copied foreign-style with rewritten `sessionId`s into a
`/tmp` cwd's encoded project dir):

- **Stale trailer:** tree truncated after the ALPHA turn, but the file's final
  `last-prompt` kept verbatim — its `leafUuid` pointed at a uuid that does not
  exist in the file. `claude --resume` exited 0 and, asked which codewords it
  knew, answered only `ALPHA` — it continued from the truncated tree's leaf
  and never noticed the dangling pointer.
- **Diverging trailer:** full tree (ALPHA, BRAVO, CHARLIE turns), final
  `last-prompt.leafUuid` rewritten to the ALPHA turn's assistant leaf. Resume
  answered `ALPHA, BRAVO, CHARLIE` — if the trailer drove leaf selection, the
  context would have ended at ALPHA. It did not.

**Implication for U3:** rewriting the final `last-prompt.leafUuid` after a cut
is hygiene/forward-compat, not load-bearing on 2.1.170. Truncating the tree is
what moves the resume point.

### 2. Dangling tool_use: tolerated or hard failure?

**Answer: tolerated — graceful degradation, no hard failure.**

Experiment: lab session cut so the file's last entry is an assistant message
carrying a Bash `tool_use` with no matching `tool_result`. `claude --resume`
exited 0 with full context recall. Mechanism (observed by diffing the session
file after resume): the harness did **not** synthesize a `tool_result`; it
attached the new user entry's `parentUuid` to the dangling assistant entry's
*parent*, orphaning the dangling `tool_use` as a dead branch.

**Implication for U6:** the self-bottle trim (drop trailing entries with
unmatched tool_use ids) is about decant cleanliness — not waking the agent
mid-action, not carrying dead branches — rather than resume survival.

### 3. Multi-compact truncation: coherent between boundaries?

**Answer: yes — truncating between (or at) compact boundaries leaves a
coherent, resumable file.**

No local session had 2+ `compact_boundary` entries, so one was forced: a
throwaway lab session was driven through `/compact` twice via print mode
(`claude --resume <id> -p "/compact"` — slash commands work in `-p`, and
`-p --resume` appends to the same session file rather than forking), with
landmark turns interleaved: ALPHA/BRAVO/CHARLIE, compact #1, DELTA,
compact #2, ECHO.

- **Cut between boundaries** (after the DELTA turn's completing assistant
  reply, dropping boundary #2 and everything after): resumed exit 0, recalled
  `ALPHA, BRAVO, CHARLIE, DELTA` (pre-compact codewords via summary, DELTA
  from the live region).
- **Cut at boundary #1** (keeping the `compact_boundary` entry, its
  `isCompactSummary` user entry, the `/compact` command-echo entries, and
  trailers): resumed exit 0, recalled `ALPHA, BRAVO, CHARLIE`.

**Implication for U3:** any cut that keeps a complete prefix is safe; when
cutting at a boundary, keep the boundary entry together with its summary
chain.

## Fixture inventory

| File | Lines | Source | Covers |
|---|---|---|---|
| `plain.jsonl` | 31 | lab session (synthetic) | linear session: user/assistant turns, thinking + tool_use/tool_result, attachments, `last-prompt`/`mode` trailers, `queue-operation`, `system/model_refusal_fallback` |
| `branched.jsonl` | 71 | real session, scrubbed | in-session rewind: one uuid with two children; `permission-mode`, `ai-title`, `file-history-snapshot`, `system/away_summary`, `system/turn_duration` |
| `compacted.jsonl` | 242 | real session, scrubbed | single `compact_boundary` (`parentUuid: null` + `logicalParentUuid` + `compactMetadata.preservedSegment`), `isCompactSummary` entry, branches, `stop_hook_summary` |
| `multi-compact.jsonl` | 81 | lab session (synthetic, forced) | two compact boundaries with landmark turns between them; `/compact` command-echo user entries |
| `queued.jsonl` | 48 | real session, scrubbed | genuine queued user messages (`queue-operation` enqueue with content) |
| `dangling-tool-use.jsonl` | 26 | lab session (synthetic) | file ends at an assistant `tool_use` with no `tool_result` (the self-bottle scenario) |

Fixture `sessionId`s are fixed fake UUIDs (`00000000-0000-4000-8000-00000000000N`)
so U3 tests can assert rewrites deterministically.

Smoke-test cut points (line counts pinned by `smoke-decant.sh`):
`plain.jsonl` first **16** lines = tree ends at the BRAVO turn's assistant
reply + its valid trailer; `multi-compact.jsonl` first **44** lines = ends
just after compact boundary #1's summary block.

## Sanitization

Fixtures contain **zero real conversation text**. Structure is preserved
verbatim: entry types, uuid/parentUuid topology, `logicalParentUuid`,
timestamps, tool_use/tool_result ids and tool names, trailers, operational
lines, paths, version/gitBranch metadata. Scrubbed to `[sanitized]`: message
text/thinking blocks (signatures emptied), tool inputs/results, attachment
payloads (hook output, skill content), `queue-operation.content`,
`ai-title.aiTitle`, `last-prompt.lastPrompt` (newer CC writes the prompt text
into the trailer), `file-history-snapshot.trackedFileBackups` values, system
entry content other than `compact_boundary`. Lab fixtures keep their short
synthetic codeword sentences (generated for this corpus; that's what the smoke
test asserts recall against), and lab compact summaries are replaced by a
synthetic summary restating the codewords.

Empirically verified post-sanitization: all six fixtures parse as JSONL and
every one resumes (exit 0) when planted foreign-style — including with
scrubbed thinking signatures and tool inputs.

## Census notes beyond the plan's entry-class list

Observed while building the corpus (useful for U3's "unknown types pass
through" default):

- `attachment` entries are **tree nodes** (uuid/parentUuid), and `last-prompt`
  trailers sometimes point at attachment uuids, not message uuids.
- New `system` subtypes: `away_summary` (carries conversation text — scrub!),
  `model_refusal_fallback`, `stop_hook_summary`, `turn_duration`.
- `last-prompt` trailers may carry the full prompt text (`lastPrompt` field)
  in addition to `leafUuid`.
- `queue-operation` lines appear even in non-interactive `-p` sessions.
- `compactMetadata.preservedSegment` references uuids (`headUuid`,
  `anchorUuid`, `tailUuid`) — a truncation that cuts *into* a preserved
  segment was not tested and U3 should keep boundary+summary blocks atomic.

## Running the smoke test

```sh
tools/bottle/scripts/smoke-decant.sh   # costs a handful of small API calls
```

Re-run after Claude Code version bumps; these findings are pinned to 2.1.170.

### Drift coverage — which case catches which harness behaviour change

The script has two layers. Acceptance cases catch a release that *rejects*
what we write; characterisation cases re-assert the empirical answers above,
so a release that silently changes *mechanism* fails the run instead of
surprising U3/U6 later.

| Behaviour we depend on | Consumer | Case that catches drift |
|---|---|---|
| Foreign-written/truncated/boundary-cut/dangling files resume | decant core | `baseline`, `truncated`, `at-compact-boundary`, `dangling-tool-use` |
| Project-dir encoding (non-alnum → `-`) | materializer | `baseline` (wrong dir ⇒ "No conversation found") |
| Leaf = last tree entry, trailer ignored | U3 truncation contract | `divert-trailer` |
| Stale `leafUuid` tolerated (no validation) | U3 trailer repair | `stale-trailer` |
| Resume appends to the **same** session file, no fork | decants-map key → rebottle parent resolution | post-check on `baseline` |
| Dangling tool_use branched around, no synthetic tool_result | U6 self-bottle trim severity | post-check on `dangling-tool-use` |
| Resume is cwd-scoped (chdir mandatory) | decant launch flow | `cwd-scope` (asserts the refusal) |

A characterisation FAIL means harness drift, not necessarily breakage:
re-derive the finding (the experiment recipes are all above) and update both
this README and the affected package contracts.

Not covered (accepted gaps): JSONL schema additions (unknown entry types pass
through by design and structure has been stable), auto-compaction trigger
behaviour (only explicit `/compact` was exercised), interactive-mode resume
differences (`-p` print mode stands in for the interactive path), and `-p`
output formatting itself (implicitly covered — every assertion greps the
reply).
