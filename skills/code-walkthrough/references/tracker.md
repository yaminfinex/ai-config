# Tracker template

Structure only; statuses and rules live in `SKILL.md` (step 4 and Rulings). Keep the sections and their order, so a cold reader (a reseat, a compaction, a design or build seat) recovers the session from this file alone. Anchors are paths from the repository root with a line number.

```markdown
# Walkthrough: <scope in one line>

Turn <n>, <date>. Read-only walkthrough; the operator posts, the build seat edits.
Session: <in progress | stopped by the operator | done> · next: <the next action> · hand-off: <path or none>

## Scope

<kind: PR, branch, range, or files at HEAD> · head <sha> · base <sha> when there is one · checkout <path>
Design of record: <path and the sections consulted> | none
Files, one per line with its role; a diff scope adds each file's change status ("(deleted, at base <sha>)", "renamed from <old path>").

## Units

Numbered, in walk order, each with its files and visited / current / pending.

## Findings

1. [<status>] <unit> · <heading> · `path/from/repo/root/file.ts:123`
   What: <what is there>. Why: <what it costs the reader or the code>. Resolve: <what would resolve it>.
2. [overruled] <unit> · <heading> · `path/from/repo/root/file.ts:123` · Rule: "<the operator's words verbatim>"

## Parked questions

- Q1 (turn <n>): <question> · <anchor once answered> · open / answered in turn <m>
```
