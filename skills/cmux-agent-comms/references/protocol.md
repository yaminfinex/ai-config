# cmux Agent Tagged Envelope Protocol

## Envelope

Use `cmux-agent-v1` for role-agnostic routing. This is an LLM-readable tagged envelope, not a strict XML contract. Agents should preserve the important tags and attributes, but should not spend time repairing harmless syntax issues when the route, kind, artifacts, and payload are clear.

```xml
<cmux-message protocol="cmux-agent-v1" conversation="CONVERSATION_ID" id="msg-0001">
  <route>
    <from agent="sender-name" surface="surface:1" pane="pane:1" workspace="workspace:1"/>
    <to agent="receiver-name" surface="surface:4" pane="pane:4" workspace="workspace:1"/>
  </route>
  <kind>bootstrap|ack|task|review|response|status|handoff|done</kind>
  <payload format="markdown">
Message text. Prefer artifact references for large content, diffs, or documents.
  </payload>
  <artifacts>
    <artifact kind="file" path="relative/or/absolute/path"/>
    <artifact kind="diff" repo="/path/to/repo" base="abc1234" head="def5678"/>
    <artifact kind="commit" repo="/path/to/repo" hash="def5678"/>
    <artifact kind="url" href="https://example.test/item"/>
  </artifacts>
  <reply>
    <mode>noninterrupting|submit</mode>
    <to surface="surface:1" workspace="workspace:1"/>
  </reply>
</cmux-message>
```

Use `in-reply-to="msg-0001"` after the first turn.

## Bootstrap Existing Peer

Use when the user says a peer agent already exists in a pane/surface and assigns roles mid-session.

```xml
<cmux-message protocol="cmux-agent-v1" conversation="CONVERSATION_ID" id="bootstrap-0001">
  <route>
    <from agent="orchestrator" surface="surface:1" pane="pane:1" workspace="workspace:1"/>
    <to agent="peer" surface="surface:4" pane="pane:4" workspace="workspace:1"/>
  </route>
  <kind>bootstrap</kind>
  <payload format="markdown">
    You are now participating in a cmux agent conversation. Run cmux identify --json, confirm your surface,
    and reply using this tagged-envelope protocol. Keep role-specific behavior to the role instructions supplied separately.
  </payload>
  <reply>
    <mode>noninterrupting</mode>
    <to surface="surface:1" workspace="workspace:1"/>
  </reply>
</cmux-message>
```

## Acknowledgement

```xml
<cmux-message protocol="cmux-agent-v1" conversation="CONVERSATION_ID" id="ack-0001" in-reply-to="bootstrap-0001">
  <route>
    <from agent="peer" surface="surface:4" pane="pane:4" workspace="workspace:1"/>
    <to agent="orchestrator" surface="surface:1" pane="pane:1" workspace="workspace:1"/>
  </route>
  <kind>ack</kind>
  <payload format="markdown">Ready. I identify as surface:4 in pane:4 and will reply with cmux-agent-v1 envelopes.</payload>
</cmux-message>
```

## What Matters

Prioritize these in order:

1. Correct destination `surface:N`.
2. Clear `kind`.
3. Useful artifact references.
4. Enough payload for the next agent to act.
5. Syntactic tidiness.

Do not block on strict XML validity. If arbitrary text would require escaping, put it in an artifact file and reference it, or place a concise Markdown summary in `<payload format="markdown">`.

## Artifact References

Do not assume artifacts live in the cmux session directory. Use the most precise reference:

- `kind="file"` for a working tree file.
- `kind="diff"` when review should target a change range.
- `kind="commit"` when a completed revision is represented by a commit.
- `kind="url"` when the artifact is outside the local filesystem.

Prefer repo-relative paths for files inside the current repository. Use absolute paths for files outside the current repository.

## Reply Semantics

`noninterrupting` means send the envelope back to the target surface without Enter:

```bash
cmux send --workspace workspace:1 --surface surface:1 "$MESSAGE"
```

`submit` means send the envelope and press Enter:

```bash
cmux send --workspace workspace:1 --surface surface:1 "$MESSAGE"
cmux send-key --workspace workspace:1 --surface surface:1 Enter
```

Only use `submit` into the user's active surface when explicitly requested. Use `submit` freely for peer agents when continuing their turn.
