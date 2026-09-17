# Tracker template

Rewrite the whole file at 3c of every turn. Keep the sections and their order; the file is the walkthrough's only durable state, so a cold reader (a reseat, a compaction, the build seat) recovers the session from it alone.

```markdown
# Walkthrough: <scope in one line>

Turn <n>, <date>. Read-only walkthrough; the operator posts, the build seat edits.

## Scope

- Kind: PR #<n> | branch <name> | range <from>..<to> | files at HEAD
- Head: <sha> (<ref>) · Base: <sha> (<ref>) · Checkout: <path>
- Design of record: <path> | none
- Files: <one per line, with a one-clause role each>

## Units

| # | Unit | Files | Status |
|---|------|-------|--------|
| 1 | <name: file, type, or flow> | <files> | visited / current / pending |

## Findings

Status set: open · agreed · overruled (rule quoted) · dismissed · build-seat · posted.
Numbered continuously across the session; a number is never reused.

1. [<status>] <unit> · <heading> · `<file:line>` · <finding in one or two sentences>
2. [overruled] <unit> · <heading> · `<file:line>` · Rule: "<the operator's words verbatim>"

## Parked questions

- Q1 (turn <n>): <question> · <anchor once answered> · open / answered in turn <m>

## Design of record

<path, and the sections consulted> | none
```

A finding's number, unit, heading, and anchor stay fixed once written; only the text and the status change. A ruling replaces the text with the rule and sets `overruled`.
