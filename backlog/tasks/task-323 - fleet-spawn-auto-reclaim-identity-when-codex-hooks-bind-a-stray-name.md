---
id: TASK-323
title: 'fleet spawn: auto-reclaim identity when codex hooks bind a stray name'
status: Done
assignee: []
created_date: '2026-08-24 09:20'
labels:
  - fleet
  - teardown
dependencies: []
ordinal: 318900
---

## Closed obsolete — root cause found and already dead (2026-08-24, investigation)

A source-level investigation (hcom source + our launch pipeline + the full
forensic trail of the 11:58 incident) found the root cause: OUR deleted codex
PATH shim (tools/herder/shims/codex), not hcom. hcom resolves the tool via
`which codex` in the launching environment and spawns by bare name through the
runner script's PATH; in herdr panes the shims dir preceded the real binary,
so the shim ran instead of codex. Its re-entrancy guard only checked
HCOM_LAUNCH_INFLIGHT (set only by `herder launch`), so a direct hcom launch
re-entered `herder launch codex` → a second nested `hcom codex` launch that
minted the stray name; the real codex ran under the stray identity while the
intended row stayed unbound. Claude was immune only because its vendorbin dir
preceded the shims dir on PATH. The demolition commit b9f4368 deleted the
shims dir, killing the bug: the first post-teardown fleet codex launch
(codex_zino, 2026-08-24 22:52) resolved codex to ~/.local/bin and bound
hooks_bound=true through the identical spawn path. No upstream hcom issue
warranted — their codex integration behaved correctly. spawn.sh's hooks_bound
gate remains as the tripwire if a wrapper shim ever reappears. (Optional
upstream hardening note, if ever talking to hcom: pty could exec the absolute
path launch already resolved, instead of re-resolving the bare name.)
Residual awareness: pre-demolition worktree checkouts still contain shim
copies — harmless unless a pane PATH points at them; none do.

## Teardown reconciliation — 2026-08-24 (superseded by closure above)

Remains open on the surviving fleet wrapper. The doctrine flip documents the
current fail-closed boundary; automatic reclaim, label restamp, managed pane id,
and PID restoration still require implementation and upstream reporting.

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two of three codex launches through the fleet preset bound their hcom hooks
to a stray fresh name while the launch row stayed unbound (run-log entries
33 and 35); claude launches bind clean. The wrapper's hooks_bound gate
catches it and refuses with the placement named; the documented remedy
(`hcom start --as <name>` inside the session) has worked both times.
Smallest hardening: when the gate trips and the stray hook-bound row shares
the launch pane, spawn.sh injects the reclaim into the pane, re-verifies,
and only refuses if reclaim fails. Also worth reporting upstream to hcom as
a codex binding defect.
<!-- SECTION:DESCRIPTION:END -->

Addendum 2026-08-24: the reclaim path leaves more debris than the name —
observed at unit-two closeout: the pane label keeps the stray pre-reclaim
name (so cull.sh's exact-label fallback correctly refuses), the reclaimed
row loses launch_context.pane_id (managed close impossible), and hcom kill
loses its tracked PID (hcom stop required). Auto-reclaim should restore
all three: re-stamp the label, carry the pane id into the reclaimed row,
and rebind the tracked PID.
