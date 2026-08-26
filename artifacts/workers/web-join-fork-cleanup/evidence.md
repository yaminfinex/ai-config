# web-join-fork-cleanup live evidence — 2026-08-26

Private server: branch binary on `127.0.0.1:4493`, with test-scoped
loopback-only binding. Owner port 4400 was not touched.

## Shared live join (`herder list`)

```text
w1A:p1P  ziru  claude  idle  listening  -
```

There was no second, unplaced `ziru` row.

## Private `/api/fleet`

```json
{
  "pane_id": "w1A:p1P",
  "agent_session": "fffc10e7-21d0-4bd8-9822-44318f0d02bc",
  "agent": "ziru",
  "tool": "claude",
  "herdr_status": "idle",
  "bus_status": "listening",
  "gap": "-"
}
```

`ziru_unplaced` was `[]`. `/api/agents/ziru` also resolved pane `w1A:p1P`
while honestly retaining the empty spawn-time `launch_context.pane_id`.

## Fork removal

`POST /api/agents/ziru/fork` returned the standard unknown-endpoint 404.
The rendered agent page browser probe found `forkButtons: 0`; its header was
`ziru · w1A:p1P · idle · listening · claude`.

The private server was stopped after these checks.
