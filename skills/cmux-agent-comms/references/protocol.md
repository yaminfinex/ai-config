# cmux Agent XML Protocol

## Envelope

Use `cmux-agent-v1` for role-agnostic routing.

```xml
<cmux-message protocol="cmux-agent-v1" conversation="CONVERSATION_ID" id="msg-0001">
  <route>
    <from agent="sender-name" surface="surface:1" pane="pane:1" workspace="workspace:1"/>
    <to agent="receiver-name" surface="surface:4" pane="pane:4" workspace="workspace:1"/>
  </route>
  <kind>bootstrap|ack|task|review|response|status|handoff|done</kind>
  <body>Message text.</body>
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
  <body>
    You are now participating in a cmux agent conversation. Run cmux identify --json, confirm your surface,
    and reply using this XML protocol. Keep role-specific behavior to the role instructions supplied separately.
  </body>
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
  <body>Ready. I identify as surface:4 in pane:4 and will reply with cmux-agent-v1 envelopes.</body>
</cmux-message>
```

## Artifact References

Do not assume artifacts live in the cmux session directory. Use the most precise reference:

- `kind="file"` for a working tree file.
- `kind="diff"` when review should target a change range.
- `kind="commit"` when a completed revision is represented by a commit.
- `kind="url"` when the artifact is outside the local filesystem.

Prefer repo-relative paths for files inside the current repository. Use absolute paths for files outside the current repository.

## Reply Semantics

`noninterrupting` means send the XML back to the target surface without Enter:

```bash
cmux send --workspace workspace:1 --surface surface:1 "$XML"
```

`submit` means send the XML and press Enter:

```bash
cmux send --workspace workspace:1 --surface surface:1 "$XML"
cmux send-key --workspace workspace:1 --surface surface:1 Enter
```

Only use `submit` into the user's active surface when explicitly requested. Use `submit` freely for peer agents when continuing their turn.
