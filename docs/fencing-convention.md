# Fencing convention: plain, status, internal

Every chat turn you write is parsed by the owner's transcript viewer (herder web) into three tiers. The owner reads the transcript in Compact view, where a well-fenced turn shrinks to a chip and a badly fenced one lands as a full reply they have to read. This applies to any agent the owner talks to, not just orchestrators. It applies to chat turns only: hcom messages, files, and commit messages are not parsed.

The examples in this file sit inside code blocks because this is a file. In a chat turn, the tags are always typed bare.

## The three tiers

| Tier | What goes in it | How the owner sees it |
|---|---|---|
| Plain text | What the owner reads now: a decision they must make, a direct answer to their question, a deploy or merge relay they hold a veto on | A reply card in every view |
| `<status>…</status>` | One line of progress between tool calls: what you are doing, what just finished, who you messaged | A small chip. Marker-only turns collapse into the activity strip |
| `<internal>…</internal>` | Reasoning, working notes, gate results, merge minutiae, anything the owner does not need to read now | A collapsed "internal note · N words" toggle. Open only in Full view |

**Default to internal.** Promote a line to status when the owner would glance at it. Promote to plain text only when the owner must act on it or asked for it. A turn where nothing needs the owner is one status line, or one internal note, and nothing else.

## Syntax

Derived from the parser (`tools/herder/web/src/features/transcript/fencingModel.ts`). The parser is the contract.

1. **Four tags exist**, spelled exactly like this, lowercase, no attributes, no spaces inside the brackets: `<status>`, `</status>`, `<internal>`, `</internal>`. Any other spelling is ordinary text.
2. **Tags are typed bare.** The parser reads the raw message and knows nothing about markdown. Backticks or a code fence around a tag do not hide it: they become stray visible characters next to a real fence. See failure mode 1.
3. **Every opening tag has its matching closing tag, in order, with no nesting.** One tag pair at a time. If the message has an unclosed opener, a stray closer, a mismatched pair, or a tag pair inside another, the **whole message fails open**: every tag is shown as literal text and the message renders as a plain reply.
4. **A status body is one line.** Any line break between `<status>` and `</status>` fails the whole message open. The body is shown as plain text, so markdown inside it stays literal. An empty body is allowed and renders as the word "status".
5. **An internal body is any length.** Line breaks, markdown, and code blocks inside it are fine. The body renders as markdown when opened. An empty body is allowed.
6. **Text outside the tags is plain text.** Anything that is not whitespace outside a tag pair counts as visible, and visible text turns the turn into a reply card.
7. **A chip-only turn starts with a tag.** The first non-whitespace characters of the message must be `<status>` or `<internal>`, and the message must contain no visible text outside the tags. Only then does Compact view fold it into the activity strip. Several tags in one turn are fine as long as only whitespace sits between them.

## How each shape renders

| Message shape | Compact view (owner default) | Normal view | Full view |
|---|---|---|---|
| Plain text only | Reply card, markdown | Reply card | Reply card |
| Tags only, first non-whitespace character a tag | Chip in the activity strip. Status text is the chip label; internal shows as "internal note". Identical consecutive turns stack with a count | Chips and collapsed notes, no card header | Same, internal notes forced open |
| Plain text plus tags, in any order | Reply card with the segments in order: text as markdown, status as a chip, internal as a collapsed note | Same | Same, internal notes forced open |
| Malformed (rule 3 or 4 broken), or tag spelled wrong | Reply card showing the raw tags as text | Same | Same |

## Examples

### Failure mode 1: tags inside backticks or code blocks

Bad. The backtick is visible text, so the turn becomes a reply card with a stray backtick, a chip, and another stray backtick. In Compact view the owner sees a reply where they expected nothing.

```
`<status>reading the parser</status>`
```

Bad, same outcome with a fence wrapper. The parser sees three backticks, a status, and three more backticks.

````
```
<status>reading the parser</status>
```
````

Good. One bare line, folds into the activity strip.

```
<status>reading the parser</status>
```

Code *inside* an internal body is fine. Only the tags themselves must be bare.

````
<internal>ran `npm test`
```
ok 41
```
</internal>
````

### Failure mode 2: under-using internal

Bad. Six sentences of narration land as a reply card the owner reads in full.

```
I read the brief and the parser. The parser is a single regex over the raw message with a small state machine, and it fails open on any malformed pair. I ran 18 probe strings through it and they all matched my expectations, including the backtick case. I also checked how cleanRows decides what becomes a chip, which turned out to be a leading-tag regex plus the no-visible-text rule. The doc is drafted with a syntax section and an examples section. Ready for review on the docs-fencing thread; nothing needs your input.
```

Good. Two sentences stay plain because they are the deliverable and the ask. Everything else moves into the note.

```
Fencing doc drafted at docs/fencing-convention.md, uncommitted on docs-fencing. Nothing needs your input.

<internal>Parser: single regex over the raw message plus a small state machine; malformed pairs fail the whole message open. Ran 18 probe strings, all matched expectation including the backtick case. Chip decision in cleanRows is a leading-tag regex plus the no-visible-text rule. Reported on the docs-fencing thread.</internal>
```

Better still, when nothing needs the owner at all, the turn is a chip:

```
<status>fencing doc drafted, reported to ziru</status>
```

### Status is one line

Bad. The line break fails the whole message open and the raw tags show as text.

```
<status>tests pass
merging now</status>
```

Good. Two statuses, or one status plus an internal note.

```
<status>tests pass</status>
<status>merging now</status>
```

### Decision needed: plain text carries only the decision

```
Merge of notes-fix conflicts with main in cleanRows.ts. Take main's version, or hold for the author?

<internal>Conflict is the activity-run split; main added the leading-tag regex after the branch forked. Taking main keeps the 41 passing tests. Author (fani) is idle on hcom.</internal>
```

### Malformed pairs fail the whole message open

Each of these renders as a plain reply with the raw tags visible. Nothing in the message is fenced.

```
Before <internal>unfinished
```

```
<internal>outer <status>inner</status></internal>
```

```
<Status>capitalised</Status>
```

## After compaction or a wake

The first line you write after a compaction, resume, or wake is the danger point. The habit of code-formatting XML-shaped tags reasserts itself, and the first turn after a wake is usually a status. Write the bare tag before anything else.

Bad. Plain text and a backticked tag. Renders as a reply card.

```
Resumed after compaction. `<status>reading the brief</status>`
```

Good.

```
<status>resumed after compaction, reading the brief</status>
```

## Session snippet

Single source for the hcom session-context `notes` field. Copy it verbatim.

```
Chat fencing (owner reads herder web in Compact view): plain text only for decisions the owner must make, direct answers, and deploy relays. One-line progress between tool calls goes in <status>…</status>; reasoning and working notes go in <internal>…</internal>. Default to internal. Tags are typed bare, lowercase, closed in order, never nested; status is one line. Good turn, whole message: <status>tests green, merging</status>. Don't wrap a tag in backticks or a code block: the backticks are visible text and the turn lands as a full reply. Don't narrate in plain text: keep the two sentences the owner needs, move the rest into internal. First line after a wake is a bare tag. Full grammar and examples: docs/fencing-convention.md in ai-config.
```
