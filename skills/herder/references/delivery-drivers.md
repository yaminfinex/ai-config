# Delivery Drivers — herder's pluggable transport abstraction

## Overview

Herder's message delivery and doorbell mechanics are abstracted behind a **driver interface**. Commands like `herder-send` and `herder-spawn --notify` express delivery *intent* without naming a specific transport; the driver implementation is selected automatically at runtime. This keeps `herdr` (the keystroke-based fallback) always available while allowing a second driver like `hcom` to be adopted when present and capable.

**Why drivers?** The current keystroke transport (`herdr agent send` + `Enter` + sigil-verify) is brittle: delivery into a non-active pane silently fails, and "did it submit?" is inferred from screen-scraping. Drivers let a second transport (`hcom`: hooks + SQLite) remove those failure modes without requiring code coupling to either transport.

## Driver Interface Contract

Each driver implements four operations. The operations are called via `driver_dispatch <op> <target> [msg]`, which selects the driver and dispatches to its implementation.

| Operation | Signature | Purpose | Return Contract |
|-----------|-----------|---------|-----------------|
| `resolve` | `<target>` → pane_id/instance | Map target (guid, label, term_*, or explicit id) to live address | exit 0: resolved (stdout: pane_id); exit 2: refused (target gone or invalid) |
| `send` | `<target> <msg> [opts]` → delivered/queued/refused | Deliver a message to the target | exit 0: delivered or queued (target accepted the message); exit 1: delivery failed (temporary); exit 2: refused (target gone/unusable) |
| `ring` | `<target> <msg>` → ok/refused | Best-effort doorbell (one-line message, e.g., orchestrator wake) | exit 0: accepted; exit 2: refused |
| `join` | `<agent>` → ok | Attach a spawned agent to the active bus (transport-specific; herdr = no-op) | exit 0: joined (or no-op for herdr); non-zero: join failed (non-fatal) |

**Exit codes (KTD2 — reuse `herder-send`'s contract):**
- `0` — success: resolved / delivered / queued (target accepted message) / joined
- `1` — transient failure: send attempt failed (retry-able)
- `2` — refused: target gone, invalid, or in an unsafe state (do not retry)
- `64` — usage error: unknown op, missing arg, etc.

**JSON output:** The `send` operation emits a JSON record on stdout (when `--json` is passed) preserving the shape of the current `herder-send` output: `{pane_id, agent, target, resolved_via, submitted, verify, message_preview}`. The `resolve` operation (used by `--dry-run`) emits: `{pane_id, target, resolved_via, drifted, dry_run}`.

### Selection & Fallback (KTD3)

A command's `select_driver()` logic is:

1. If `HERDER_BUS` env is set (values: `auto`, `herdr`, `hcom`), honor it:
   - `auto` (default): automatic capability detection (below)
   - `herdr`: force the herdr keystroke driver, regardless of hcom presence
   - `hcom`: force hcom; error if target cannot be resolved via hcom (debugging/testing only)
2. Else if `HERDER_BUS` is not set, use `auto` (default):
   - If hcom is on PATH AND `hcom_resolve <target>` succeeds (target joined and usable) → `hcom`
   - Else → `herdr` (fallback; always available)
3. The `herdr` driver is never required; a machine with no hcom behaves identically to today.

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

**Location:** `skills/herder/scripts/lib/driver-herdr.sh`

Moves the current `herder-send` keystroke logic into the driver interface. This is the fallback and the baseline implementation.

**Resolution:** registry (guid/label) → terminal_id → live pane_id (drift-proof). Bare pane_id or term_* handled directly.

**Send:** `herdr agent send <pane_id> <text>`, then `herdr pane send-keys Enter` if `--no-enter` not set. Verifies delivery via sigil heuristic and status detection.

**Ring:** one-line `herder-send` call through the driver (same verification logic).

**Join:** no-op (keystroke transport has no per-agent bus membership).

**Supported on:** any system with herdr installed.

### `hcom` — Hook-based SQLite driver

**Location:** `skills/herder/scripts/lib/driver-hcom.sh`

Delivers messages via hcom's hook-injection and SQLite backend. Removes the silent pane-drop failure mode and enables mid-turn injection / idle-wake without keystroke brittleness. **Both Claude and Codex are first-class hcom targets** — the live U5 probe proved Codex hook delivery reliable (mid-turn injection ~8.4s, a burst of sends coalesced into a single atomic injection, ordered, zero drops), so Codex is not special-cased to herdr.

**Resolution:** target label → hcom instance name via `hcom list <label>`. Present ⇒ usable (joined) → exit 0; `Not found` ⇒ not joined → exit 2, forcing herdr fallback. This clean "not usable → fall back" signal is what `select_driver`/`hcom_resolve` key on.

**Send:** `hcom send --from <sender> @<instance> -- <msg>` (the sender is an external identity and need not itself be joined). The driver polls the event stream for a `deliver:` ack — a recorded delivery receipt — mapping it to `delivered`; batched/idle sends keep `queued` semantics. Matches herdr's exit-code contract (0 delivered/queued, 1 transient, 2 refused).

**Ring:** `hcom send` for the doorbell (best-effort; falls back on not-found).

**Join:** attaches a spawned child to the worktree bus with its **name pinned to the herder label** — a bare `hcom start` auto-assigns a random name (e.g. `tuna`), which `hcom_resolve(label)` could never find. `herder-spawn --bus auto` pins it by injecting `HCOM_INSTANCE_NAME=<label>` (and `HCOM_DIR`, `HCOM_TOOL`) into the child's **process env at launch** (a Bash-tool `export` can't reach the hooks); the post-settle `join` registers the instance under the label. Non-blocking; join failure is non-fatal (agent stays reachable via herdr).

**Bus isolation:** `HCOM_DIR=<worktree>/.hcom` scopes the entire bus (DB + hooks + logs) to a worktree, so the bus boundary == the worktree boundary — this is how one hcom transport composes with orchestrate Inv 7 (one writer per worktree). `.hcom/` is gitignored.

**Supported on:** systems with hcom installed and on PATH; the target must have joined the bus (via `herder-spawn --bus auto`, or its own `hcom start`).

---

## Selection in Action

**Scenario 1: hcom absent**
```
$ HERDER_BUS=auto ./herder-send term_xyz 'msg'
→ select_driver: hcom not on PATH → herdr driver
→ sends via herdr keystroke path (identical to today)
```

**Scenario 2: hcom present, target joined**
```
$ HERDER_BUS=auto ./herder-send @claude-worker 'msg'
→ select_driver: hcom on PATH, hcom_resolve('claude-worker') succeeds → hcom driver
→ sends via hcom hooks; target wakes / injects mid-turn
```

**Scenario 3: hcom present, target NOT joined**
```
$ HERDER_BUS=auto ./herder-send @cli-task 'msg'
→ select_driver: hcom_resolve('cli-task') fails (not joined) → herdr driver
→ sends via herdr keystroke path (fallback)
```

**Scenario 4: hcom present, forced herdr**
```
$ HERDER_BUS=herdr ./herder-send @claude-worker 'msg'
→ select_driver: HERDER_BUS=herdr override → herdr driver
→ sends via herdr keystroke path (testing/debugging the old path)
```

**Scenario 5: hcom present, forced hcom (debugging only)**
```
$ HERDER_BUS=hcom ./herder-send @not-joined 'msg'
→ select_driver: HERDER_BUS=hcom override → hcom driver
→ hcom_resolve('not-joined') fails → exit 2 (hard error for debugging)
```

---

## Implementing a New Driver

To add a third driver (e.g., agmsg SQLite or h5i git-log):

1. Create `skills/herder/scripts/lib/driver-<name>.sh`.
2. Implement four functions: `<name>_resolve`, `<name>_send`, `<name>_ring`, `<name>_join`.
   - Each should honor the exit-code contract (0/1/2/64).
   - `send` must return JSON on stdout (preserve the shape from `driver_json_send`).
3. Source it in `delivery-driver.sh` after the herdr driver.
4. (Optional) Add a capability-detection clause to `select_driver()` to pick the new driver when appropriate.
5. Update this doc.

---

## Testing

**Golden fixtures:** Before major refactors (e.g., U7's herdr extraction), capture `herder-send --dry-run --json` and `herder-send` outputs across key cases:
- Resolution via term_*, guid, label, explicit pane_id
- Delivered, queued, and refused outcomes
- Codex large-paste collapse handling

Re-run the same tests after the refactor and verify JSON and exit codes are byte-identical. This proves behavior preservation.

**Driver-specific tests:** Each driver can test its mechanics in isolation via `--dry-run` and unit-level scenarios (e.g., hcom_send without full delivery verification).

---

## References

- `herder-send` script header: public contract (target forms, options, exit codes, `--json`)
- `orchestrate/SKILL.md` Invariants 8–9: how delivery drivers fulfill the invariants
- `herder/SKILL.md`: user-facing command docs (transport-agnostic)
- `skills/herder/references/spike-findings-hcom.md`: Phase 0 findings (hcom capability / reliability verdict)
