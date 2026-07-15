# Claude sidecar entry classification

Status: implemented from a structural census and a separate audit of the
Claude files that sesh actually admits, on 2026-07-15.

## Corpus census and classification

The initial census recursively inspected JSONL below
`$HOME/.claude/projects/`. It recorded only each object's top-level `type` and
key set and did not emit field values. That snapshot covered 1,126 files,
860.09 MiB, and 308,748 valid objects. The corpus was live while counted, so
these are point-in-time observations rather than durable totals.

That recursive population is broader than sesh input. Claude discovery admits
direct UUID-named session JSONL files; it does not currently admit nested
`subagents/workflows/wf_*/journal.jsonl` files. A second audit restricted to
that admitted population found exactly ten non-conversation types, and only
those ten are eligible for metadata exclusion.

Four established entry types remain transcript-bearing:

| Entry type | Observed rows | Index role |
| --- | ---: | --- |
| `assistant` | 126,962 | nested `message.role` |
| `user` | 65,669 | nested `message.role` |
| `attachment` | 39,645 | existing degraded-visible `unknown` |
| `system` | 12,853 | existing role parsing / degraded-visible fallback |

The ten state types observed and semantically audited in the admitted
population retain their exact `entry_type`, byte range, logical placement, and
empty-UUID non-participation semantics, but the index assigns `role=meta`:

| Entry type | Recursive snapshot rows | Structural purpose |
| --- | ---: | --- |
| `agent-name` | 475 | session agent label |
| `ai-title` | 10,977 | generated session title |
| `bridge-session` | 2 | bridge session cursor |
| `file-history-snapshot` | 1,017 | file-history state snapshot |
| `last-prompt` | 15,075 | cached latest prompt/leaf pointer |
| `mode` | 14,904 | current interaction mode |
| `permission-mode` | 13,176 | current permission mode |
| `pr-link` | 1,098 | linked pull-request state |
| `queue-operation` | 6,402 | queued-prompt state transition |
| `worktree-state` | 22 | worktree session state |

The recursive census also found three types only in nested workflow journals:

| Entry type | Recursive snapshot rows | Classification |
| --- | ---: | --- |
| `fork-context-ref` | 7 | `unknown` / visible; metadata-shaped but not evidenced in admitted input |
| `started` | 232 | `unknown` / visible; lifecycle-shaped but not evidenced in admitted input |
| `result` | 232 | `unknown` / visible; substantive workflow output |

The 232 observed `result` objects are the sole generated analyses in their
workflow journals: those files contain paired `started`/`result` rows and no
assistant, user, or system row. The results include findings, evidence,
recommendations, verdicts, or summaries, reach 24,758 serialized bytes, and
none is duplicated in a message row. A type-only metadata classification
would therefore hide the only copy of an answer. `result` may be excluded in
the future only behind a shape discriminator that proves a metadata-only
variant.

This is an explicit admitted-population allowlist, not an inverse rule. The
three recursive-only types and any unrecognized future type stay
`role=unknown` and remain visible through the transcript's degraded renderer.
The regression fixture makes `result`, `started`, `fork-context-ref`, and an
invented `future-sidecar-probe` visible so an over-broad exclusion fails
loudly.

## Surface behavior

For Claude only, `role=meta` rows are excluded before transcript window
arithmetic. A window therefore contains at most 200 renderable conversation
rows, and the pager's `From`, `To`, and `Total` describe conversation rather
than index rows. Each page badges the known metadata rows excluded from that
page's contiguous sorted-row interval. The index still holds those rows and
the raw route still reads the original mirror bytes, so full fidelity is one
click away.

Unknown types, quarantined rows, and every non-Claude tool retain the prior
never-500 degraded behavior. A Claude session containing only known metadata
uses the existing raw fallback because its index has no renderable
conversation row.

## Reclassification and deployment

Classification happens while parsing a line. Already-indexed rows keep their
old `role=unknown` value until `sesh reindex` rebuilds the disposable index
from mirror bytes. Without a deploy-time reindex, existing sessions improve
only as newly appended rows receive `role=meta`; old sidecars continue to
consume transcript slots. A deploy-time reindex gives immediate, corpus-wide
repair.

The recursive Claude source snapshot (860.09 MiB / 308,748 objects) is scale
context, not the exact reindex input because it includes files discovery does
not admit. The actual input is every byte already mirrored for Claude, Codex,
Grok, and Pi. Reindex cost is linear in those mirror bytes plus the existing
global unification/dedup rebuild. It is a whole-corpus sequential replay,
invoked synchronously during serve startup, and uses repeated per-generation
transactions rather than one atomic transaction. Ingest is therefore down for
the full reindex duration. The established operational record describes a
field-sized migration reindex as startup-blocking and taking minutes; no live
reindex was run for this change because choosing and executing the deploy leg
belongs to operations. The incremental parser cost remains constant per
appended row: one entry-type allowlist switch, with no corpus query or schema
change.
