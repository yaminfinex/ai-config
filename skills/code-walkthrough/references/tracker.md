# Tracker template

Structure only; statuses and rules live in `SKILL.md` (step 4 and Rulings). Keep the sections and their order, so a cold reader (a reseat, a compaction, the build seat) recovers the session from this file alone.

```markdown
# Walkthrough: <scope in one line>

Turn <n>, <date>. Read-only walkthrough; the operator posts, the build seat edits.
Session: <in progress | stopped by the operator | done> · next: <the next action> · hand-off: <path or none>

## Scope

- Kind: PR #<n> | branch <name> | range <from>..<to> | files at HEAD
- Head: <sha> (<ref>) · Base: <sha> (<ref>) · Checkout: <path>
- Design of record: <path> | none
- Files: <one per line, with a one-clause role each; a diff scope adds the change status, a deleted file reads "(deleted, at base <sha>)", a renamed file carries its old path>

## Units

| # | Unit | Files | Status |
|---|------|-------|--------|
| 1 | <name: file, type, or flow> | <files> | visited / current / pending |

## Findings

1. [<status>] <unit> · <heading> · `<file:line>` · <finding in one or two sentences>
2. [overruled] <unit> · <heading> · `<file:line>` · Rule: "<the operator's words verbatim>"

## Parked questions

- Q1 (turn <n>): <question> · <anchor once answered> · open / answered in turn <m>

## Design of record

<path, and the sections consulted> | none
```
