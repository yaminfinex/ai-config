# TASK-046 worker brief — herder liveness reconciliation vs herdr 0.7.x

You are implementing the agreed fix for backlog TASK-046 on branch `task-046-liveness`
(this worktree). Design is APPROVED by hera (board owner) — do not re-litigate scope.
Read the ticket for full context: `backlog/tasks/task-046*.md` (and the comments on it).

## Diagnosis (settled — do not re-derive)

NOT a parse-shape bug. Two mechanisms, both confirmed live on herdr 0.7.3:

1. **Coordinate epoch invalidation.** `herdr update --handoff` reissued terminal ids in a
   new scheme (old: `term_` + 16 hex, e.g. `term_655ac376a1c34395`; new: `term_` + 13 hex)
   and changed the pane-id scheme (`w…-N` → `w…:pN`). Every pre-handoff registry row is
   dead-keyed, so terminal-keyed reconcile reports `gone` even for agents alive AND
   detected in `herdr agent list`.
2. **Detection loss for pre-handoff processes.** Panes whose agent process predates the
   handoff show `agent_status: unknown` / no `agent` field in `pane list` and are ABSENT
   from `agent list`, even while actively working. Their hook reports don't reach the new
   server; only a process restart/re-report recovers status. `herder wait` delegates to
   `herdr wait agent-status`, so these time out with status=unknown.

Useful upstream facts: 0.7.x `agent list` rows expose `name` (== the undecorated spawn
label) and `cwd`. Public ids are stable handles that never recycle, but pane ids ARE
reassigned by `herdr pane move`, and terminal ids reissue across handoffs — `terminal_id`
is durable only within a server generation.

## Scope (a–d) and decisions

**(a) DONE (WIP commit, review it, keep or improve):** `herder list` fallback chain.
`internal/herdrcli/herdrcli.go`: Agent gained `Name`, `CWD`. `internal/listcmd/list.go`:
`liveIndex` (byTerm → byPane → byName-unambiguous), `live_matched_by` field in JSON output.

**(b) DONE in list (same WIP):** liveness tri-state. Agent-list miss + pane-list hit
(terminal or pane) → `live_status: "undetected"`; `gone` only when no pane exists.

**(c) TODO:** `herder wait` — on timeout, if the pane exists in `pane list` but
`herdr agent get` shows unknown/absent detection, print guidance on stderr: the pane is
alive but herdr agent detection is lost (process predates a server handoff); restart the
agent in the pane or relaunch to restore status; a bare timeout line is misleading.
See `internal/waitcmd/wait.go` (timeout branch around `client.Run("wait", ...)`).

**(d) TODO (the core deliverable):** new `herder reconcile` command
(`internal/reconcilecmd/`, register in `internal/cli/cli.go`).
- **Dry-run by default; `--apply` writes.** One auditable migration.
- Per latest-active registry row, resolve against live `agent list` + `pane list`:
  - stored terminal_id live in agent list, live `name` empty or == label → **re-confirm**
    (no write; if live pane_id differs from stored, `--apply` appends a row with the
    refreshed pane_id — report as re-confirm with pane refresh).
  - stored terminal_id live but hosts a DIFFERENT non-empty name → **unseat** (report
    only, never write; suggest enroll/adoption — the coordinates belong to someone else).
  - terminal dead → candidate live agents matched by **name == label AND agent kind ==
    rec.Agent AND cwd match** (row cwd: top-level `cwd` in Raw, else provenance.cwd;
    skip the cwd clause only if the row has no cwd anywhere). Exclude candidates whose
    terminal_id is already held by another active row (never steal).
    - exactly one candidate → **re-bind**: `--apply` appends an updated row
      (registry.UpdateRawObject on rec.Raw: terminal_id + pane_id) — this is the
      migration write.
    - more than one → **ambiguous**: refuse loudly, list candidates, never guess.
    - zero → pane alive (tri-state) → **undetected** (cannot re-bind — say why),
      else **gone**.
- Output: human table of GUID/LABEL/outcome/detail (+ `--json`). Exit 0 when nothing
  ambiguous and no errors; exit 1 if any row was ambiguous or a write failed.
- Vocabulary is spec-8.3-compatible on purpose: re-confirm / re-bind / unseat. Keep it.

## Registry semantics you must respect

Append-only JSONL, latest-row-per-guid wins (`registry.LatestByGUID` reproduces the jq
idiom exactly — read the package doc comment in `internal/registry/registry.go`). Writes
go through `registry.Append` with rows built by `registry.UpdateRawObject` (full-object
replacement rows, preserves unknown fields). NEVER mutate rows in place, never reorder.

## Tests (required, hermetic — never touch the real registry or live herdr)

- `tests/check-list-contract.sh`: extend the embedded mock herdr with a `pane list` case
  and an undetected scenario (pane alive, absent from agent list), plus a name-fallback
  scenario (agent list entry with `name` matching a fixture label, new-epoch terminal).
  Regenerate goldens with `--write` and REVIEW the diff — new JSON field
  `live_matched_by` and tri-state values will appear.
- NEW `tests/check-reconcile-contract.sh` modeled on the list suite: fixtures with
  old-epoch rows; scenarios for re-confirm (incl. pane refresh), re-bind, unseat,
  ambiguous (exit 1), undetected, gone; dry-run vs `--apply` (assert the appended row
  content and that dry-run writes nothing).
- `tests/check-wait-contract.sh`: cover the new timeout guidance path (mock
  `agent get` / `pane list` accordingly).
- Go: `env -u GOROOT GOTOOLCHAIN=local PATH="$HOME/.local/share/mise/installs/go/1.26.4/bin:$PATH" go build ./... && go vet ./... && go test ./...`
  from `tools/herder` (PATH go is 1.22 — too old; use the mise 1.26.4 as shown).
- Full gate before reporting DONE: `for f in tools/herder/tests/check-*.sh; do bash "$f"; done`
  from the repo root of THIS worktree. All suites must pass.

## Protocol

- Commit incrementally on `task-046-liveness` with clear messages. Do NOT merge, do NOT
  touch main, do NOT push, do NOT edit backlog/ (single-writer via hera), do NOT touch
  docs/specs/herder-spec.md.
- Delete this brief file in your final commit.
- When done (or blocked), report over the hcom bus to @vibe (intent request): what you
  built, gate results verbatim (pass/fail counts), and anything you changed from this
  brief with rationale. vibe reviews, then hera runs the full gate + adversarial review.
