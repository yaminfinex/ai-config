# TASK-053 worker brief — sidecar self-reports agent sessions to herdr

You are implementing backlog TASK-053 on branch `task-053-sid-reporting` (this worktree).
Read the ticket first: `backlog/tasks/task-053*.md`. Ground truth documents, both on main:
- `docs/specs/herder-spec.md` — RATIFIED; cite D11 / AC-24 where relevant.
- `napkins/run-herder-dx/spec-memo-sid-exposure.md` — spec-ravu's probe memo; the "cheapest
  fix is herder-side" section is the origin of this task.

## Why (settled — do not re-derive)

herdr 0.7.3 exposes per-pane agent sessions only via reports (`herdr pane
report-agent-session`); with no integration installed, `agent_session` is empty on every
pane. The sidecar already learns each session's id from `hcom list` (see the enrichment
seam in `tools/herder/internal/sidecarcmd/sidecar.go` around lines 138–171:
`enrichedSessionID`) but never tells herdr. One reporting call makes herder self-sufficient:
sid exposure works with zero third-party config, and reported sids ride
PaneAgentSessionSnapshot in the HandoffManifest — so the NEXT `herdr update --handoff`
stops stranding registry rows (the prevention half; `herder reconcile`, already shipped,
is the migration half).

## Scope

**`tools/herder/internal/sidecarcmd/` ONLY.** Hard constraint: wave-A1 (TASK-055,
registry v2 record types) is being implemented in parallel by another worker — do NOT
touch `internal/registry/` or any registry row shapes. If you believe the fix genuinely
needs a registry change, STOP and report blocked instead.

Implementation:
- When the sidecar observes a session id for its pane (first enrichment AND any change,
  e.g. after /clear or /resume mints a new id), report it upstream:
  `herdr pane report-agent-session <pane_id> --source ID --agent LABEL [--seq N]
  [--agent-session-id ID] [--agent-session-path PATH] [--session-start-source SOURCE]`
  (that usage line is from the live 0.7.3 binary). Resolve the exact flag semantics from
  `herdr pane report-agent-session` errors/upstream docs and the ravu memo — in
  particular what `--source` identifies (the reporter) and whether `--seq` must increase
  across re-reports. Record what you establish as comments at the call site.
- Fail-soft: a failed or missing `herdr` report call must NEVER break or delay sidecar
  operation — best-effort, swallow errors (the sidecar's existing posture toward herdr).
- Report only when the sid is non-empty and the pane id is known. Never invent or
  re-report a stale sid after the row's sid goes empty.
- Respect the sidecar's existing correlation discipline: only report a sid the sidecar
  has positively attributed to ITS OWN pane (the pane-correlated path); never from the
  ambiguous tag+cwd fallback (see the refuse-to-guess comment near sidecar.go:420).

## Tests (hermetic, required)

- Extend the sidecar test seams / mock-herdr with `pane report-agent-session` handling:
  assert the call fires on first enrichment, fires again on sid CHANGE, does not fire on
  no-change ticks, does not fire from the ambiguous fallback, and that herdr failure is
  swallowed (sidecar continues).
- Go gate: `env -u GOROOT GOTOOLCHAIN=local PATH="$HOME/.local/share/mise/installs/go/1.26.4/bin:$PATH" go build ./... && go vet ./... && go test ./...` from `tools/herder`.
- Full shell gate before reporting DONE: `for f in tools/herder/tests/check-*.sh; do bash "$f"; done`
  from this worktree's root; all suites green. If an existing sidecar contract suite
  exists, extend it rather than adding a parallel one.

## Protocol

- Commit incrementally on `task-053-sid-reporting`; clear messages. Do NOT merge, push,
  touch main, edit `backlog/` (single-writer via hera), or edit `docs/specs/`.
- Delete this brief file in your final commit.
- When done or blocked, report ONCE to @vibe on the hcom bus (intent inform, with your
  own --name): what you built, what you established about the report-agent-session flag
  semantics, gate results verbatim, any deviations with rationale. Then end your turn.
