# TASK-045 worker brief — sidecar process_id correlation (codex capture fix)

You are implementing the F1 fix for backlog TASK-045 on branch `task-045-capture`
(this worktree). Root cause and design are settled and recorded on the ticket
(`backlog/tasks/task-045*.md`, latest comment) — do not re-derive or re-litigate.

## Root cause (settled)

herder spawn's name capture has two child-specific signals, BOTH keyed on the hcom
roster's `launch_context.pane_id`: the direct roster match in spawn.go's
childBoundBusOnce, and the sidecar's pane-correlation (sidecar.go `findRowForPane`,
`rows[i].LaunchContext.PaneID == paneID`) that feeds registry enrichment. Under hcom
0.7.23, codex sessions never complete the hook handshake (`hooks_bound:false`,
`session_id:""`) and their `launch_context` carries ONLY `process_id`. So both signals
are structurally dead for codex: capture cannot succeed at any window length even though
the bus name goes live (pty/process-bound registration).

Validated live: the RUNNING codex agent process's environment (`/proc/<pid>/environ`)
carries `HCOM_PROCESS_ID` equal byte-for-byte to its roster row's
`launch_context.process_id`, plus `HERDER_GUID` (exported by spawn) and
`HCOM_INSTANCE_NAME`.

## Scope

**`tools/herder/internal/sidecarcmd/` ONLY.** Do NOT touch spawn.go (a parallel wave-A2
unit owns write-path rerouting and may collide), registry internals, or anything else.
If you believe the fix genuinely needs another package, STOP and report blocked.

Implementation — a second, positive correlation path in the sidecar:
- When (and only while) the existing pane_id correlation has not succeeded, attempt
  process-environment correlation:
  1. Locate candidate agent processes for THIS pane. Suggested approach: scan /proc for
     processes whose environ contains `HERDER_GUID=<the sidecar's own guid>` — that
     equality is the OWNERSHIP proof (only our spawned child carries our guid). Bound
     the scan sensibly (e.g. comm/cmdline prefilter on the tool name) and run it only
     while uncorrelated — never on every tick after success.
  2. From the matched process environ, read `HCOM_PROCESS_ID`.
  3. Find the hcom roster row whose `launch_context.process_id` equals it. That row is
     pane-correlated for all purposes: enrich exactly as the existing pane-correlated
     path does (appendEnrichment, enrichedSessionID, reportAgentSession with
     paneCorrelated=true).
- TASK-033 doctrine holds: this is a POSITIVE child-specific signal (guid-proven process
  → its own declared process_id → roster row). NEVER fall back to name-only or tag+cwd
  from this path. `HCOM_INSTANCE_NAME` from environ may be used only as a cross-check,
  never as the primary key.
- Staleness note for comments: reading the LIVE process's environ is authoritative — this
  is not the TASK-043 inherited-shell-env hazard; say so at the call site.
- Fail-soft everywhere: unreadable /proc entries (races, permissions), missing env vars,
  no roster match → quietly keep polling the existing paths. Non-Linux (no /proc):
  compile/run as a no-op, never an error.
- Keep the launch_context.pane_id path first and unchanged — claude must not regress.

## Tests (hermetic, required)

- Make the process-scan/environ-read injectable (seam), so tests don't need real /proc:
  cases: (1) correlation succeeds via process_id when the roster row has no pane_id;
  (2) refuses when environ HERDER_GUID differs from the sidecar's guid; (3) refuses when
  no roster row carries the process_id; (4) existing pane_id path takes precedence and
  is unchanged (regression); (5) environ read failure is swallowed and polling continues;
  (6) the enrichment produced by process_id correlation triggers reportAgentSession
  exactly like the pane-correlated path.
- Extend the existing sidecar contract/test surface; do not build a parallel harness.
- Go gate (export applies to the WHOLE chain):
  `export PATH="$HOME/.local/share/mise/installs/go/1.26.4/bin:$PATH"; env -u GOROOT GOTOOLCHAIN=local go build ./... && env -u GOROOT GOTOOLCHAIN=local go vet ./... && env -u GOROOT GOTOOLCHAIN=local go test ./...` from `tools/herder`.
- Full shell gate before DONE: `for f in tools/herder/tests/check-*.sh; do bash "$f"; done`
  from this worktree's root; all suites green.

## Protocol

- Commit incrementally on `task-045-capture`. Do NOT merge, push, touch main, backlog/,
  or docs/specs/. Delete this brief in your final commit.
- Report ONCE to @vibe on the bus when done or blocked (intent inform, your own --name):
  what you built, how the process scan is bounded, gate results verbatim, deviations
  with rationale.
