# Claude sidecar entry classification

Status: implemented from a structural census of the live Claude Code corpus
on 2026-07-15.

## Corpus census and classification

The census inspected only each JSON object's top-level `type` and key set in
`$HOME/.claude/projects/*/*.jsonl`; it did not emit field values. The snapshot
covered 1,126 JSONL files, 860.09 MiB, and 308,748 valid JSON objects. The
corpus was live while counted, so these are a point-in-time observation rather
than durable totals.

Four established entry types remain transcript-bearing:

| Entry type | Observed rows | Index role |
| --- | ---: | --- |
| `assistant` | 126,962 | nested `message.role` |
| `user` | 65,669 | nested `message.role` |
| `attachment` | 39,645 | existing degraded-visible `unknown` |
| `system` | 12,853 | existing role parsing / degraded-visible fallback |

The other 13 observed types are Claude Code state or sidecar records. They
retain their exact `entry_type`, byte range, logical placement, and empty-UUID
non-participation semantics, but the index assigns `role=meta`:

| Entry type | Observed rows | Structural purpose |
| --- | ---: | --- |
| `agent-name` | 475 | session agent label |
| `ai-title` | 10,977 | generated session title |
| `bridge-session` | 2 | bridge session cursor |
| `file-history-snapshot` | 1,017 | file-history state snapshot |
| `fork-context-ref` | 7 | parent-session context reference |
| `last-prompt` | 15,075 | cached latest prompt/leaf pointer |
| `mode` | 14,904 | current interaction mode |
| `permission-mode` | 13,176 | current permission mode |
| `pr-link` | 1,098 | linked pull-request state |
| `queue-operation` | 6,402 | queued-prompt state transition |
| `result` | 232 | structured side-agent result state |
| `started` | 232 | side-agent start state |
| `worktree-state` | 22 | worktree session state |

This is an explicit allowlist, not an inverse rule. An unrecognized future
type stays `role=unknown` and remains visible through the transcript's
degraded renderer. The regression fixture includes `future-sidecar-probe` to
make an accidental “exclude every non-message type” change fail loudly.

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

The live Claude source snapshot alone is 860.09 MiB / 308,748 lines, so a full
reclassification must at least read and parse that much Claude JSONL in
addition to any Codex, Grok, and Pi mirrors held by the store. Reindex cost is
linear in mirror bytes plus the existing global unification/dedup rebuild.
The established operational record describes a field-sized migration reindex
as startup-blocking and taking minutes; no live reindex was run for this change
because choosing and executing the deploy leg belongs to operations. The
incremental parser cost remains constant per appended row: one entry-type
allowlist switch, with no corpus query or schema change.
