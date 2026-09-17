# Hand-off formats

Written beside the tracker in the closing turn, in the form the operator asks for. Both begin with a line naming the source tracker path and draw only from it: every item keeps its number, so the operator can trace a comment or a brief line back to the ruling that produced it. `posted` items are already delivered and stay out of both.

When the operator stops with findings still `open`, each format ends with a section headed "Unresolved" that lists every open item by number with its text unchanged. An open item keeps its own words there; the settled sections carry only rulings.

## PR review comments

File: `<tracker-stem>-review-comments.md`. One block per item with status `agreed`, `overruled`, or `build-seat`; `dismissed` items stay out. Ready to paste: each block is one paragraph in the operator's voice, with the anchor as its heading.

```markdown
Source: <tracker path>

### `path/file.rs:123` (item 7, agreed)

<one paragraph: what the reader sees, why it is a problem, what would resolve it>

## Unresolved

- <item n> · `path/file.rs:123` · <the finding's text, unchanged>
```

An `overruled` item's paragraph is the operator's rule, so the comment states the operator's position.

## Build brief

File: `<tracker-stem>-build-brief.md`. For the build seat: the settled decisions first, then the work, then what is unresolved.

```markdown
# Build brief: <scope in one line>

Source: <tracker path>. Head <sha>.

## Settled decisions

- <item n>: <the operator's rule or the agreed finding, one sentence>

## Build-seat items

- <item n> · `path/file.rs:123` · <what to change> · Done when: <checkable criterion>

## Unresolved

- <item n> · `path/file.rs:123` · <the finding's text, unchanged>

## Out of scope

- <dismissed items by number, one clause each>
```
