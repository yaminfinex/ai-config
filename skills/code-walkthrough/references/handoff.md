# Hand-off formats

Written beside the tracker in the closing turn, in the form the operator asks for. Both draw only from the tracker: every item keeps its number, so the operator can trace a comment or a brief line back to the ruling that produced it.

## PR review comments

File: `<tracker-stem>-review-comments.md`. One block per item with status `agreed`, `overruled`, or `build-seat`; `dismissed` items stay out. Ready to paste: each block is one paragraph in the operator's voice, with the anchor as its heading.

```markdown
### `path/file.rs:123` (item 7, agreed)

<one paragraph: what the reader sees, why it is a problem, what would resolve it>
```

An `overruled` item's paragraph is the operator's rule, so the comment states the operator's position.

## Build brief

File: `<tracker-stem>-build-brief.md`. For the build seat: the settled decisions first, then the work.

```markdown
# Build brief: <scope in one line>

Head <sha>. Source: <tracker path>.

## Settled decisions (do not reopen)

- <item n>: <the operator's rule or the agreed finding, one sentence>

## Build-seat items

- <item n> · `path/file.rs:123` · <what to change> · Done when: <checkable criterion>

## Out of scope

- <dismissed items by number, one clause each>
```
