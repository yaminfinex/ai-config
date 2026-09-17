# Hand-off: feedback

Written beside the tracker in the closing turn, file `<tracker-stem>-feedback.md`. It is feedback for whoever comes next, a design, planning or build seat, and rarely buildable as it stands. It draws only from the tracker: every item keeps its number, so a reader can trace a line back to the ruling that produced it. `posted` items are already delivered and stay out.

Items are grouped by unit. Each carries its number, anchor, status, and the three parts (what is there, why it costs, what would resolve it). An `overruled` item carries the operator's rule verbatim in place of the three parts, so the feedback states the operator's position.

When the operator stops with findings still `open`, the file ends with a section headed "Unresolved" that lists every open item, its text unchanged.

```markdown
# Feedback: <scope in one line>

Source: <tracker path>. Head <sha>. Read-only walkthrough; nothing here is built. A design, planning or build seat may take it from here.

## <unit>

- <item n> · `path/from/repo/root/file.ts:123` · <status> · What: <what is there>. Why: <what it costs>. Resolve: <what would resolve it>.
- <item m> · `path/from/repo/root/file.ts:123` · overruled · Rule: "<the operator's words verbatim>"

## Unresolved

- <item n> · `path/from/repo/root/file.ts:123` · open · What: … Why: … Resolve: …
```
