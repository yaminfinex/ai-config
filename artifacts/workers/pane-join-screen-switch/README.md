# Pane join + agent screen evidence

Captured 2026-08-28 from the isolated `pane-join-screen-switch` worktree.
The owner serve on port 4400 was used only for read-only `curl GET` checks.
The branch serve was built from this worktree, bound loopback-only at
`127.0.0.1:4493`, and reported:

```text
event: hello
data: {"buildIdentity":"executable:98bc837cad19fb177e9f926ac6b9c8163fc6859160f4645fd7eb22ed78361a67"}
```

## Pane-less reproduction: before

Disposable native-hcom Claude `pjsbare-kole` had session
`d84ed22c-8af1-4636-aa97-45f638e53ec4`. Its hcom launch context contained
process, environment, git, and tty evidence but no `pane_id`. Herdr exposed the
real terminal as `w4R:p3`, with the agent name only in display title/label and
with no `agent_session` or authoritative agent row. The pre-change projection
therefore showed both sides of the gap:

```json
{
  "pane": {"pane_id":"w4R:p3","agent":"-","tool":"-","bus_status":"-","gap":"no bus row"},
  "unplaced": {"pane_id":"-","agent":"pjsbare-kole","tool":"claude","gap":"no visible pane"}
}
```

The old client called `w4R:p3` a shell even though the live terminal was Claude.
No name/title join is safe here.

A second disposable real-shape probe, `pjsprobe2-kame`, demonstrated the
evidence-bearing variant Herdr can report: pane `wY:p6M` carried
`agent_session:{"agent":"claude","kind":"id","source":"herdr:claude","value":"aa319d4e-5a61-4a93-ad55-6fa205f67598"}`.
The old Go decoder retained only `value` and discarded `agent`, preventing the
existing exact unique tool+session join when hcom had no pane claim. That exact
captured shape is now a parser regression fixture.

## Pane-less reproduction: after

Disposable native-hcom Claude `pjsafter-rava` had session
`4b4390ef-72dd-4093-a92d-1811543ca3a5`, again with no hcom pane claim. Herdr
exposed its real terminal as `w4R:p5` without an `agent_session`, so the branch
correctly refused to guess a join:

```json
{
  "pane": {"pane_id":"w4R:p5","agent":"-","tool":"-","herdr_status":"unknown","bus_status":"-","gap":"no bus row"},
  "unplaced": {"pane_id":"-","agent":"pjsafter-rava","tool":"claude","herdr_status":"-","bus_status":"listening","gap":"no visible pane"}
}
```

The client now calls that pane **Unattributed terminal** on the board and
sidebar. Its read-only screen states: “Herdr cannot attribute this terminal; it
may belong to an unplaced agent.” This preserves both honest gaps without
misrepresenting the actual agent terminal as an agent-free shell.

- `after-unattributed-terminal.png`: board/sidebar terminology.
- `unattributed-terminal-screen-warning.png`: live `w4R:p5` Claude terminal
  with explicit attribution warning.
- `no-pane-screen-disabled.png`: the matching unplaced agent tab, with Screen
  disabled and the plain `No live pane.` reason.

## Agent Transcript / Screen switch

The placed `impl-medo` detail had proven pane `w4R:p1`.

- `agent-transcript-default.png`: Transcript selected by default; clean-view,
  queued dock/composer area, and tab behavior remain in place.
- `agent-screen-opt-in.png`: Screen selected inside the same agent tab, showing
  the ANSI-stripped, read-only live pane; the composer remains the bus-only
  typing surface.

Closing the Screen viewport produced this browser resource history, proving
the screen dimension used the same single multiplexed EventSource:

```text
/api/events?agents=impl-medo&screens=w4R%3Ap1
```

No terminal input control or inject-port route was exercised or added.

## Screenshot checksums

```text
9a9d1229df68662d7c547d45153239710d94a8715b4dc0846c19362cebdbf1b8  after-unattributed-terminal.png
71fa2aa8cd97781db050efa7e24c900e058052a5913f6a3086594eb071b7ab36  agent-screen-opt-in.png
27f3c919fa1f139644a535a403caedde9e05924ab7852dad618dc51912055daf  agent-transcript-default.png
45c5208d95d31b013b37b954a24a16cfe6100aa392721c11f565da37750e0bd9  no-pane-screen-disabled.png
5af14c670dac9ffe8cfcc2e6c5d590fc86269a009c6db850027fd5a31c4453d1  unattributed-terminal-screen-warning.png
```
