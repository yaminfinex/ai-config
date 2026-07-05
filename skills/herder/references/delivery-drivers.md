# Delivery Drivers — herder's pluggable transport abstraction

## Overview

Herder's message delivery and doorbell mechanics are abstracted behind a **driver interface**. Commands like `herder send` and `herder spawn --notify` express delivery *intent* without naming a specific transport; the driver implementation is selected automatically at runtime. This keeps `herdr` (the keystroke-based fallback) always available while allowing a second driver like `hcom` to be adopted when present and capable.

**Implementation:** the drivers live in Go inside the `herder` binary — `tools/herder/internal/driver/` (`herdr.go`, `hcom.go`, selection in `selection.go`), invoked through the `herder send` shim → `bin/herder send`. The original bash drivers (`lib/driver-herdr.sh`, `lib/driver-hcom.sh`, `lib/delivery-driver.sh`) were deleted at the flip; git history @ d4ca54c is the bash reference. The behavior contract carried over byte-for-byte and is pinned by the golden suites (`tools/herder/tests/check-send-contract.sh`, `check-hcom-contract.sh`).

**Why drivers?** The current keystroke transport (`herdr agent send` + `Enter` + sigil-verify) is brittle: delivery into a non-active pane silently fails, and "did it submit?" is inferred from screen-scraping. Drivers let a second transport (`hcom`: hooks + SQLite) remove those failure modes without requiring code coupling to either transport.

## Driver Interface Contract

Each driver implements two operations, `Resolve` and `Send` (methods on the driver structs in `tools/herder/internal/driver/`). `Selection.Select(target)` (`selection.go`) picks the transport per target; the send command then dispatches to the selected driver's implementation.

| Operation | Signature | Purpose | Return Contract |
|-----------|-----------|---------|-----------------|
| `resolve` | `<target>` → pane_id/instance | Map target (guid, label, term_*, or explicit id) to live address | exit 0: resolved (stdout: pane_id); exit 2: refused (target gone or invalid) |
| `send` | `<target> <msg> [opts]` → delivered/queued/refused | Deliver a message to the target | exit 0: delivered or queued (target accepted the message); exit 1: delivery failed (temporary); exit 2: refused (target gone/unusable) |

**Exit codes (KTD2 — reuse `herder send`'s contract):**
- `0` — success: resolved / delivered / queued (target accepted message)
- `1` — transient failure: send attempt failed (retry-able)
- `2` — refused: target gone, invalid, or in an unsafe state (do not retry)
- `64` — usage error: unknown op, missing arg, etc.

**JSON output:** The `send` operation emits a JSON record on stdout (when `--json` is passed) preserving the shape of the current `herder send` output: `{pane_id, agent, target, resolved_via, submitted, verify, message_preview, extra_enter_sent, paste_collapsed}` — the last two are herdr keystroke diagnostics; dropping them was tail-review B4 and the send goldens assert them. The hcom driver extends the base with `{hcom_name, hcom_dir}` in their place. The `resolve` operation (used by `--dry-run`) emits: `{pane_id, target, resolved_via, drifted, dry_run}` on the herdr path; an hcom-routed dry-run emits `{target, transport, hcom_name, hcom_dir, team, dry_run}` (or `{target, transport, would: "refuse", dry_run}` for a bus-less row under forced hcom).

### Selection & Fallback (KTD3, W3)

The selection logic (`Selection.Select` in `tools/herder/internal/driver/selection.go`) is:

1. If `HERDER_BUS` env is set (values: `auto`, `herdr`, `hcom`), honor it:
   - `auto` (default): registry-driven selection (below)
   - `herdr`: force the herdr keystroke driver, regardless of hcom presence
   - `hcom`: force hcom for debugging/testing; a registry row with empty `hcom_name` refuses with exit 2, while no registry row is treated as a literal hcom bus name on the caller's ambient `HCOM_DIR`
2. Else if `HERDER_BUS` is not set, use `auto` (default):
   - Resolve `<target>` against the spawn registry's latest record.
   - If that record has a non-empty (and non-`"null"`) `hcom_name` and `hcom` is on PATH → `hcom`
   - Else → `herdr` (fallback; always available)
3. The registry record is the source of truth. Auto selection no longer asks hcom whether the raw target string exists as an instance name, which avoids the dual namespace where a herder label and an hcom instance name can differ.

Dry-run reports the same transport a real send would take. On the hcom path it prints the recorded `@hcom_name`, `team`, and `HCOM_DIR`; a recorded peer with an empty `hcom_name` reports `would REFUSE (exit 2)`; an unregistered forced-hcom target is shown as a literal `@target` on the ambient bus.

### Behavior Preservation (R2, R5)

The `herdr` driver re-implements the current keystroke logic *unchanged*:

- **Resolution:** target → pane_id via registry (guid/label/short-guid) or terminal_id (term_*) with drift-proof terminal re-lookup. A raw pane_id is honoured verbatim.
- **Send:** `herdr agent send` (write text) + optionally `herdr pane send-keys Enter` (submit).
- **Delivery verification:** sigil heuristic (message text present but no longer in input buffer) + status detection (agent transitioned to `working`) + codex paste-blob detection.
- **Return values:** `delivered`, `queued` (target was busy; message accepted to run next), `placed` (--no-enter only).
- **Sharp edges:** pane-id drift via terminal_id, codex large-paste collapse, "Conversation interrupted" recovery, pre-flight safety checks, timeouts.

All exit codes, `--json` shapes, `--dry-run` output, and flag behavior remain byte-for-byte identical.

---

## Available Drivers

### `herdr` — Keystroke driver (always available)

**Location:** `tools/herder/internal/driver/herdr.go` (bash original deleted at the flip; reference @ d4ca54c)

Carries the `herder send` keystroke logic behind the driver interface. This is the fallback and the baseline implementation.

**Resolution:** registry (guid/label) → terminal_id → live pane_id (drift-proof). Bare pane_id or term_* handled directly.

**Send:** `herdr agent send <pane_id> <text>`, then `herdr pane send-keys Enter` if `--no-enter` not set. Verifies delivery via sigil heuristic and status detection.

**Supported on:** any system with herdr installed.

### `hcom` — Hook-based SQLite driver

**Location:** `tools/herder/internal/driver/hcom.go` (bash original deleted at the flip; reference @ d4ca54c)

Delivers messages via hcom's hook-injection and SQLite backend. Removes the silent pane-drop failure mode and enables mid-turn injection / idle-wake without keystroke brittleness. **Both Claude and Codex are first-class hcom targets** — the live U5 probe proved Codex hook delivery reliable (mid-turn injection ~8.4s, a burst of sends coalesced into a single atomic injection, ordered, zero drops), so Codex is not special-cased to herdr.

**Resolution:** guid/short-guid/label resolves through the herder registry to the latest row. A row with `hcom_name` resolves to that hcom-assigned name; a row with empty `hcom_name` exits 2 so the caller can refuse or fall back; no row means forced-hcom literal addressing and resolves to the target string. Registry load/parse errors are treated as "no row" on this path, matching the bash `2>/dev/null || true` behavior.

**Send:** `hcom send --from <sender> @<hcom_name> -- <msg>` (the sender is an external identity and need not itself be joined). When the registry provides `hcom_dir`, the driver scopes `HCOM_DIR` to the recorded value for its hcom preflight/send/events calls only — the caller's environment is untouched. Empty `hcom_dir` inherits the caller's ambient bus. The driver preflights `hcom list <hcom_name>` on the scoped bus, sends, then polls `hcom events --context deliver:<hcom_name> --after <timestamp>` for a recorded delivery receipt. Ack maps to `delivered`; no ack inside the window maps to `queued`. Matches herdr's exit-code contract (0 delivered/queued, 1 transient failure, 2 refused).

**Bus membership:** hcom agents are launched *through* hcom and are bus-bound from birth — `herder spawn` execs `bin/herder launch <tool> --tag <role>` (`hcom <tool> --run-here --tag <role>`), so there is no separate bus attach after spawn. The old name-pinning design was retired with W2: pinning was proven not to work because hcom owns identity at launch. hcom assigns the instance name `<role>-<random>`; `herder spawn` captures it into the registry (`team`/`hcom_dir`/`hcom_name`/`hcom_tag`), which is the coordinate W3 send/list/wait/cull resolve through.

**Bus isolation (teams, D7) — optional capability, not the default posture:** the bus is scoped by `HCOM_DIR`, pinned into the child's process env at spawn: `--team <name>` (else `$HERDER_TEAM`, else the global `~/.hcom` bus) → `HCOM_DIR=$HERDER_TEAMS_ROOT/<name>` (default root `~/.hcom/teams`). The **global bus is the default** and the normal operating mode — registry-driven addressing (guid/label → `hcom_name`) already gives per-agent targeting without a team. When a team is used, `herder send` crosses into the recorded team bus by scoping its hcom calls to the recorded `hcom_dir`. See the config-dir pin caveat below before requiring teams anywhere.

**Config-dir pin condition:** `PinConfigDir` in `tools/herder/internal/launchcmd` pins `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, or `GEMINI_CLI_HOME` only when `HCOM_DIR` is set and not equal to `~/.hcom`. That is hcom's local-mode condition; pinning on the global bus is intentionally avoided because set-vs-unset moves Claude's JSON state and caused first-run onboarding. **Known caveat (team buses):** with the pin, claude reads state from `~/.claude/.claude.json`, which starts fresh — the first team-bus claude launch per machine hits one-time onboarding (login/theme picker); it persists machine-wide once completed. This is the main reason teams are an opt-in ringfence rather than the default (tracked for W5: state-file seeding would remove the wall).

**Shell shims (W4):** interactive and herder spawn launches reach hcom through PATH shims:
`claude`/`codex` → `tools/herder/shims/<tool>` → `bin/herder launch` → `hcom <tool> --run-here`.
`herder launch` exports `HCOM_LAUNCH_INFLIGHT=1` before execing hcom; if hcom resolves the tool name
through PATH and lands back on the shim, the shim skips itself and execs the real binary. The shims
honor optional `HERDER_SHIM_ARGS_<TOOL>` env vars when a user deliberately sets them, but installer
activation only puts the shim directory on PATH; permission defaults such as Claude skip-permissions
stay a manual machine choice rather than repo-written config. The shims are repo-prepared; machine
PATH activation is handled by `ai-setup --shims install`.

**Supported on:** systems with hcom installed and on PATH; the target must be a bus-bound hcom instance (herder spawn does this automatically for hcom-capable agents).

---

## Selection in Action

**Scenario 1: hcom absent or target has no bus name**
```
$ HERDER_BUS=auto ./herder send term_xyz 'msg'
→ select: no registry hcom_name → herdr driver
→ sends via herdr keystroke path (identical to today)
```

**Scenario 2: registry row has a bus coordinate**
```
$ HERDER_BUS=auto ./herder send review-1234abcd 'msg'
→ select: registry hcom_name=review-rive + hcom available → hcom driver
→ HCOM_DIR=<recorded hcom_dir> hcom send --from <sender> @review-rive -- 'msg'
```

**Scenario 3: registry row is not bus-bound**
```
$ HERDER_BUS=auto ./herder send bash-1234abcd 'msg'
→ select: registry hcom_name empty → herdr driver
→ sends via herdr keystroke path (fallback)
```

**Scenario 4: hcom present, forced herdr**
```
$ HERDER_BUS=herdr ./herder send @claude-worker 'msg'
→ select: HERDER_BUS=herdr override → herdr driver
→ sends via herdr keystroke path (testing/debugging the old path)
```

**Scenario 5: hcom present, forced hcom (debugging only)**
```
$ HERDER_BUS=hcom ./herder send @not-joined 'msg'
→ select: HERDER_BUS=hcom override → hcom driver
→ no registry row: target is treated as literal @not-joined on the ambient bus
→ hcom list preflight fails if absent → exit 2
```

---

## Implementing a New Driver

To add a third driver (e.g., agmsg SQLite or h5i git-log):

1. Create `tools/herder/internal/driver/<name>.go` with a driver struct implementing `Resolve` and `Send`.
   - Honor the exit-code contract (0/1/2/64).
   - `Send` must emit JSON on stdout under `--json` (preserve the base shape `{pane_id, agent, target, resolved_via, submitted, verify, message_preview}`; transport-specific diagnostics ride alongside — herdr adds `extra_enter_sent`/`paste_collapsed` (golden-asserted, see B4), hcom adds `hcom_name`/`hcom_dir`).
2. Wire the driver into `Selection` and add a registry/capability clause to `Selection.Select` (`selection.go`) to pick it when appropriate; dispatch it from `internal/send/send.go` (real send and `--dry-run`).
3. Characterize before you rely on it: add a hermetic contract suite + goldens under `tools/herder/tests/` (mock the transport CLI like `mock-hcom`), so the contract is pinned. Goldens are immutable once written — a driver that can't match its goldens is a driver bug.
4. Update this doc.

---

## Testing

**Golden suites are the contract.** `tools/herder/tests/check-send-contract.sh` (herdr driver: resolution via term_*/guid/label/pane_id; delivered, queued, and refused outcomes; paste-collapse handling) and `check-hcom-contract.sh` (selection routing, hcom delivery/queued/refused, bus scoping, forced-`HERDER_BUS` dry-runs) pin stderr, `--json`, and exit codes byte-for-byte against goldens that were generated from the bash implementation before the Go port. They run against the shipped Go binary on default paths; `HERDER_CMD_SEND_BIN` (and the other per-tool `HERDER_{SPAWN,LIST,WAIT,CULL}_BIN` vars) can point a suite at any alternative executable honouring the CLI.

**Goldens are immutable during implementation work.** If an implementation can't match a golden, fix the implementation or stop with findings — never edit the golden to make it pass.

**Driver-specific tests:** each driver can be probed in isolation via `--dry-run` and unit tests under `tools/herder/internal/` (`go test ./...` from `tools/herder`).

---

## References

- `herder send --help` + the send goldens (`tools/herder/tests/goldens/`): public contract (target forms, options, exit codes, `--json`)
- `orchestrate/SKILL.md` Invariants 8–9: how delivery drivers fulfill the invariants
- `herder/SKILL.md`: user-facing command docs (transport-agnostic)
