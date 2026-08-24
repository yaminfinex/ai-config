# hcom upgrade runbook

How to move this machine to a new hcom release without breaking the fleet composition.
Written after the 0.7.22 → 0.7.23 upgrade (2026-07-08), then flipped to the
fleet/hcom/herdr lifecycle on 2026-08-24. Dated herder integration notes are
historical evidence, not live verb guidance.

## Ownership model — read this first

- **mise owns hcom on this machine.** The pin lives in the repo at `lib/mise-path.sh`
  (`mise_hcom_version` — the single source of truth) and is materialized into
  `~/.config/mise/conf.d/ai-config.toml` by `bin/ai-setup --shims install`. The test
  battery derives the version from that file, so a bump is one edit.
- **Never upgrade via `hcom update`, the curl installer, brew, or uv here.** They install a
  second binary (typically `~/.local/bin/hcom`) that either shadows or is shadowed by the
  mise install depending on PATH order — you get a machine where `hcom --version` disagrees
  with what agents actually run. (This exact thing happened: `hcom update` reported success,
  `hcom --version` still said 0.7.22.)
- Agents and hooks resolve the pinned hcom directly. The retired herder PATH
  shims are not part of resolution; the durable binary is the **mise shim**
  (`~/.local/share/mise/shims/hcom`) selected by the conf.d pin.

## Procedure

1. **Audit before bumping.** Spawn a read-only auditor against the new release. Cross-check
   every surviving integration surface:
   receipt query shape (`hcom events --agent X --context deliver:<sender>`, JSONL, monotone
   ids), `hcom list <name> --json` (single object, base name, status values),
   roster `launch_context.pane_id`, `events sub` semantics, the sessionstart bootstrap text,
   send flags, fleet preset behavior, and queue-until-deliverable delivery.
   Verdict: upgrade-now / upgrade-with-changes / hold.
2. **Land required composition changes first.** Update hcom-hook, fleet-wrapper,
   session-context, and live-contract compatibility before touching the machine.
3. **Bump the pin in the repo** — edit `mise_hcom_version` in `lib/mise-path.sh` and
   nothing else (the battery derives the version from it). Run the full Go,
   fleet/composition, and `check-*.sh` gates. Commit.
4. **Apply to the machine:** `bin/ai-setup --shims install`
   (regenerates conf.d + `mise install`s the new version).
5. **Remove every stale/stray install** — this is the step that bites:
   - `mise uninstall github:aannoo/hcom@<old>` (and any lingering `ubi:aannoo/hcom`);
   - `rm ~/.local/bin/hcom` if a curl/installer orphan exists;
   - verify with `mise ls | grep hcom` (exactly one version) and, in a **fresh** shell,
     `which -a hcom` + `hcom --version`.
6. **Live contract tier:** run `bash tools/herder/tests/check-live-contract.sh` from
   the repo root before and after applying the pin. The hcom predicates must pass
   against the installed binary: real SessionStart bootstrap extraction, focused
   `hcom list --json` single-object shape, and roster `launch_context` fields. A
   visible skip is acceptable only when installed hcom is absent or no roster entries
   advertise hcom launch context; once the binary is resolved, command failures are
   hard failures.
7. **Live smoke (the upgrade gate):** create disposable herdr placement, spawn a
   throwaway tagged Claude with `tools/fleet/spawn.sh`, and confirm (a) hooks bind,
   (b) `hcom send` delivers, and (c) the agent's bootstrap carries the tag group
   line. Cull it with `tools/fleet/cull.sh`. This catches bootstrap drift the
   hermetic battery structurally cannot.
8. **Record it:** task notes on the board + the run journal if a run is live.

## Known gotchas

- **0.7.24 fail-closes bare `sessionstart` for unknown actors** (upstream #99: actor-first
  routing; identity rows come from `hcom start`/launch, never inferred from a hook call).
  A bare `hcom sessionstart` with a made-up `HCOM_PROCESS_ID` now exits 0 with NO
  bootstrap output — silently. Fleet launches are safe (launch goes through
  `hcom <tool> --go`, which creates the row), but any fixture or probe that
  fabricated identity via bare sessionstart must prime with `hcom start` first
  (`compactthen_wire_test.go` was the live hit; the step-1 audit must include the
  spawncmd/send wire tests, not just the delivery-driver and grok suites).

- **Running sessions keep their old PATH — and this breaks INBOUND delivery, not just CLI
  calls.** A session started before the upgrade may have the OLD versioned mise install dir
  baked into PATH; after `mise uninstall` that binary is gone and hook invocations can no
  longer resolve hcom. Two distinct symptoms (both live-hit on
  2026-07-08): (a) the session's own `hcom` shell calls go quiet — fixable per
  Bash call by prepending `$HOME/.local/share/mise/shims` to PATH; (b) the session's HOOKS
  (which run in the agent process env, unreachable from a shell export) drop incoming
  message BODIES and delivery receipts — the agent sees empty `<hcom>` wakes repeating
  while the real content sits queued. (b) has no in-session fix: drain/read via
  `hcom listen`/`hcom events` in a PATH-fixed shell, and RESTART the session when
  practical (compaction does NOT help — same process, same env). Newly fleet-spawned agents
  inherit the current pinned path. Upgrade sequencing lesson:
  do step 5's uninstalls when long-lived sessions (orchestrators!) are between runs, or
  accept degraded polling until they restart.
- **`hash -r`** after PATH surgery in a live shell; bash caches lookups.
- **The battery cannot see bootstrap-text drift** — its sessionstart fixtures are canned.
  Only step 6's live smoke proves the pairing. Keep fixtures covering BOTH old and new
  shapes when text changes (TASK-040 pattern: dual-style fixtures).
- The conf.d file is regenerated wholesale by ai-setup — never hand-edit it; change
  `lib/mise-path.sh` and re-run.
