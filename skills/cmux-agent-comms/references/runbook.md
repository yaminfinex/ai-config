# cmux Agent Comms Runbook

## Establish Comms Mid-Session

Use this when the user says something like: "you are the reviewer; the other agent in the right pane is the coder; establish comms."

1. Identify yourself:

```bash
cmux identify --json
```

2. Inspect visible topology:

```bash
cmux tree --workspace workspace:CURRENT
```

3. Resolve the peer endpoint. Prefer an explicit `surface:N`. If the user says pane/panel/window, map it to a surface with `cmux tree` or `cmux list-pane-surfaces`.

4. Send a bootstrap envelope to the peer surface and submit it:

```bash
skills/cmux-agent-comms/scripts/cmux-agent-send --workspace workspace:CURRENT --to surface:PEER --file bootstrap.env --submit
```

5. Poll the peer, then your own surface:

```bash
cmux read-screen --workspace workspace:CURRENT --surface surface:PEER --lines 160
cmux read-screen --workspace workspace:CURRENT --surface surface:SELF --lines 160
```

6. If the peer replied noninterruptingly to your prompt, read the reply and clear or submit it according to the user's instructions.

## Spawn a Peer Agent

1. Create a terminal split:

```bash
cmux new-pane --type terminal --direction right --workspace workspace:CURRENT --focus false
```

The command prints `OK surface:N pane:N workspace:N`. Use that `surface:N` as the peer endpoint.

2. Launch the requested agent:

```bash
cmux send --workspace workspace:CURRENT --surface surface:PEER 'codex'
cmux send-key --workspace workspace:CURRENT --surface surface:PEER Enter
```

For Claude:

```bash
cmux send --workspace workspace:CURRENT --surface surface:PEER 'claude'
cmux send-key --workspace workspace:CURRENT --surface surface:PEER Enter
```

3. Wait until the peer UI is ready:

```bash
cmux read-screen --workspace workspace:CURRENT --surface surface:PEER --lines 160
```

4. Send the bootstrap envelope with `--submit`.

## Multi-Pass Conversation Loop

Use this loop for document production, review cycles, implementation/review, or any other role-specific workflow:

1. Orchestrator sends a task with artifact refs.
2. Peer replies with a tagged envelope and any artifact refs it produced.
3. Orchestrator reads the reply, decides the next recipient, and sends the next turn.
4. Stop when a `kind="done"` message arrives, the user interrupts, or the round budget is reached.

Set an explicit round budget when the user has not provided one:

```xml
<policy max-rounds="3" stop-on-user-interrupt="true"/>
```

## Practical Notes

- `surface:N` is the address. `pane:N` is only where the surface is displayed.
- The envelope is a prompt convention, not strict XML. Preserve route, kind, artifacts, and payload; do not stall on escaping or parser-perfect output.
- Use `cmux send` without Enter for noninterrupting messages to the user-facing agent.
- Use `cmux send-key ... Enter` only after confirming the destination should start processing.
- For large artifacts, send references rather than content.
- For code review or revision review, prefer git refs when they are clearer than file snapshots.
- Do not rely on the current shell working directory unless the artifact reference is explicitly repo-relative.
