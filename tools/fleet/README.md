# Fleet lifecycle wrapper

`tools/fleet` is the surviving Claude/Codex spawn and cull surface over hcom
and herdr. It does not replace either registry and it never makes the `fleet`
terminal preset the box default.

Install or refresh the preset once:

```bash
tools/fleet/preset-install.sh
```

The command changes only `[terminal.presets.fleet]` in
`${HCOM_DIR:-$HOME/.hcom}/config.toml`. `spawn.sh` selects it for one launch by
setting both `HCOM_TERMINAL=fleet` and `FLEET_PANE=<pane-id>`. The open helper
prints that pane id before any other stdout, stamps the label, and runs hcom's
generated script in the pane; the preset retains the id so `hcom kill` can run
`herdr pane close {pane_id}`.

## Lifecycle

Spawn one seat in a new tab of an existing workspace, a new herdr-managed
worktree, or an existing idle shell pane:

```bash
tools/fleet/spawn.sh claude --model haiku --tag review --workspace w1A --prompt 'Review the change'
tools/fleet/spawn.sh codex --tag impl --worktree-branch task-319 --repo /path/to/repo
tools/fleet/spawn.sh codex --tag probe --pane w1X:p1
```

The wrapper always passes the placement cwd with `--dir`, launches with
`--go`, and enables autonomy (`--dangerously-skip-permissions` for Claude,
`--sandbox danger-full-access` for Codex). `--model` is optional so the tool's
configured default can be used. On success it prints the hcom name, pane id,
cwd, and placement kind. On failure it exits nonzero and names any placement
left for explicit cleanup.

Message and compact another seat with hcom directly:

```bash
hcom send @review-vava --intent request -- 'Report your result'
hcom term inject review-vava '/compact retain the review contract' --enter
hcom send @review-vava --intent request -- 'Continue the review'
```

Self-compaction uses two composer injections because a self-addressed bus send
reroutes to the owner. The helper detaches and prints its log filename:

```bash
tools/fleet/selfcompact.sh review-vava 'retain the review contract' 'Continue the review'
```

Cull sends one courtesy notice, kills the hcom process, verifies the managed
pane close, and uses only a unique exact label as its fallback:

```bash
tools/fleet/cull.sh review-vava
```

When the seat occupied the only pane in a worktree workspace, managed close
also removes that workspace. The git checkout remains deliberate state: after
the cull is verified, remove that exact checkout with `git worktree remove
<cwd-reported-by-spawn>` and delete the disposable branch separately when it
is no longer needed.

Resume keeps the hcom name and fork assigns a new one. Create an idle target
pane first, then place either operation through the same preset contract:

```bash
FLEET_PANE=w1X:p1 HCOM_TERMINAL=fleet hcom r review-vava
FLEET_PANE=w1X:p2 HCOM_TERMINAL=fleet hcom f review-vava
```

## Resolving an hcom name to a pane

Resolve at action time and stop on ambiguity, in this order:

1. **Managed preset:** use the exact agent row's
   `.launch_context.pane_id` from `hcom list --json`. Fleet launches populate
   it from the preset open command's first stdout line.
2. **Exact label match:** inspect `herdr pane list` for one label ending in
   ` <full-hcom-name> [<tool>]`. Status changes may alter the leading glyph;
   the name and tool suffix must match exactly, and multiple matches refuse.
3. **PID occupant probe:** when managed metadata and the live label are both
   absent, use the retained `tools/herder/internal/occupant` process-ancestry
   probe to join the hcom-bound tool process to one live pane. A positive
   unique ancestry match is usable; no occupant, unprobeable, or ambiguous
   evidence is a report-only result and never permission to close a pane.

The observer may cache this mapping for display, but lifecycle actions do not
consult that cache.
