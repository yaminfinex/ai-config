---
name: cmux-agent-comms
description: Establish structured communication between multiple AI agents running in cmux panes or surfaces. Use when Codex needs to identify its own cmux surface, connect to an existing peer agent mid-session, spawn a peer Codex/Claude/terminal agent in a split pane, bootstrap role-agnostic tagged-envelope message routing, pass artifact references by path/URL/git commit/diff, or run multi-pass agent conversations that the user can watch and interrupt.
---

# cmux Agent Comms

## Core Model

Use this skill to create role-agnostic communication between agents in cmux. Keep role behavior in other skills; this skill only handles identity, routing, spawning, message envelopes, and handoff mechanics.

Address agents by `surface:N`. Treat `window:N`, `workspace:N`, and `pane:N` as placement metadata, not message endpoints.

Use cmux for visible prompt delivery and wakeups. Use a tolerant tagged envelope for routing and intent; do not get stuck trying to produce or parse strict XML. Use durable artifact references for work products:

```xml
<artifact kind="file" path="/absolute/or/repo-relative/path.md"/>
<artifact kind="diff" repo="/path/to/repo" base="abc1234" head="def5678"/>
<artifact kind="commit" repo="/path/to/repo" hash="def5678"/>
<artifact kind="url" href="https://example.test/doc"/>
```

## Safety Rules

- Run `cmux identify --json` first and record your own `surface_ref`.
- Prefer clear routing and useful artifact refs over perfect envelope syntax.
- Treat message envelopes as LLM-readable prompts, not strict XML documents.
- Do not press Enter in the user's active surface unless explicitly asked.
- Default replies to the user or orchestrator are noninterrupting: send text to their prompt but do not submit it.
- It is acceptable to submit prompts to peer agents when the task is to spawn, bootstrap, or continue that peer.
- Do not close panes, surfaces, windows, or workspaces unless explicitly asked.
- Prefer `cmux read-screen` before sending follow-up prompts so you do not interrupt a peer that is still working.
- If a message contains substantial tagged-envelope or multiline text, prefer `scripts/cmux-agent-send` to avoid shell quoting mistakes.

## Quick Runbook

1. Identify yourself:

```bash
cmux identify --json
```

2. Inspect topology:

```bash
cmux tree --workspace workspace:1
cmux list-panes --workspace workspace:1
```

3. If the peer already exists, identify its `surface:N` from the tree or ask it to run `cmux identify --json`.

4. If the peer must be spawned, create a terminal split and launch the requested agent:

```bash
cmux new-pane --type terminal --direction right --workspace workspace:1 --focus false
cmux send --workspace workspace:1 --surface surface:NEW 'codex'
cmux send-key --workspace workspace:1 --surface surface:NEW Enter
cmux read-screen --workspace workspace:1 --surface surface:NEW --lines 120
```

Use `claude` instead of `codex` when the requested peer is Claude. If the command is unknown, report that the agent binary is unavailable instead of guessing.

5. Bootstrap the peer with a tagged envelope. Include both surfaces, roles only if the user gave them, reply mode, and artifact refs. See `references/protocol.md`.

6. For multi-pass work, route each turn explicitly:

```xml
<cmux-message protocol="cmux-agent-v1" conversation="doc-review" id="msg-0002" in-reply-to="msg-0001">
  <route>
    <from agent="reviewer" surface="surface:4"/>
    <to agent="author" surface="surface:1"/>
  </route>
  <kind>review</kind>
  <payload format="markdown">...</payload>
</cmux-message>
```

## Helper Script

Use the bundled helper when available:

```bash
skills/cmux-agent-comms/scripts/cmux-agent-send --workspace workspace:1 --to surface:4 --file message.env --submit
skills/cmux-agent-comms/scripts/cmux-agent-send --workspace workspace:1 --to surface:1 --file reply.env
```

The helper sends the payload with `cmux send`; `--submit` also sends Enter. Omit `--submit` for noninterrupting replies.

## Detailed References

- `references/runbook.md`: mid-session connection, spawning Codex/Claude peers, and multi-pass operation.
- `references/protocol.md`: tagged envelope fields, artifact references, acknowledgements, and examples.
