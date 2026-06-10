---
name: bottling
description: Pin, name, and re-enter agent contexts with the `bottle` CLI. Use when the user says "bottle this as X", "rebottle this", "decant X into a pane", "pin this context", "pin this session", "save this conversation as a bottle", or wants to snapshot the current session and resume it later from exactly this point.
---

# bottling

`bottle` snapshots an agent conversation into an immutable, named, versioned
**bottle**, then **decants** it — materializes a fresh, resumable session seeded
from that frozen context. The CLI's own help is the real surface; this skill is a
thin pointer to it.

## Start here

```bash
bottle            # the agent-tuned skill: concept model + command table
bottle <cmd> --help   # usage + examples + pitfalls for one command
```

Read those first — they document the *landed* behavior. Don't guess flags.

## Self-bottling (the live session)

To bottle the conversation you are *in*, pass your own session id:

```bash
bottle create <name> --session "$CLAUDE_CODE_SESSION_ID"
```

`bottle create <name>` with no `--session` already defaults to the live session
when `$CLAUDE_CODE_SESSION_ID` is set, so the bare form usually suffices.

**Self-bottle cut rule:** a bottle of your own live session is cut at the last
*completed* turn — the in-flight turn holding the running `bottle create` call is
trimmed. That is correct, not a bug: it keeps every decant from waking mid-action.

## v1 is Claude-only

Any agent (codex included) can `bottle list`, `bottle show`, `bottle log`, and
`bottle decant ... --pane` to inspect bottles and open them into a herdr split.
But self-bottling needs a Claude session id; from another harness pass
`--session ID` or `--last` to `bottle create` instead.

## Syncing across machines

`bottle sync` converges the store with a **private** git remote (first run:
`bottle sync --remote <url>`) — see `bottle sync --help` for the pitfalls.
