# hcom upgrade runbook

How to move this machine to a new hcom release without breaking the herder integration.
Written after the 0.7.22 → 0.7.23 upgrade (2026-07-08); shaped by what actually went wrong.

## Ownership model — read this first

- **mise owns hcom on this machine.** The pin lives in the repo at `lib/mise-path.sh`
  (single version string) and is materialized into `~/.config/mise/conf.d/ai-config.toml`
  by `bin/ai-setup --shims install`. A test golden pins the same version:
  `tools/herder/tests/check-mise-path-install.sh`.
- **Never upgrade via `hcom update`, the curl installer, brew, or uv here.** They install a
  second binary (typically `~/.local/bin/hcom`) that either shadows or is shadowed by the
  mise install depending on PATH order — you get a machine where `hcom --version` disagrees
  with what agents actually run. (This exact thing happened: `hcom update` reported success,
  `hcom --version` still said 0.7.22.)
- Agents resolve hcom through the herder path-shim (`tools/herder/shims/hcom`), which walks
  PATH for the first real binary. The durable real binary is the **mise shim**
  (`~/.local/share/mise/shims/hcom`), which resolves per the conf.d pin.

## Procedure

1. **Audit before bumping.** Spawn a read-only auditor against the new release (see
   TASK-028 for the reusable brief shape). Cross-check every herder integration surface:
   receipt query shape (`hcom events --agent X --context deliver:<sender>`, JSONL, monotone
   ids), `hcom list <name> --json` (single object, base name, status values),
   roster `launch_context.pane_id`, `events sub` semantics, the sessionstart bootstrap text
   herder's `extract()` scrapes (`hookcmd/hook.go` regexes — a **quote-style change here
   broke 0.7.23 silently**, see TASK-040), send flags, and queue-until-deliverable delivery.
   Verdict: upgrade-now / upgrade-with-changes / hold.
2. **Land any required herder changes first** (0.7.23 needed the quote-agnostic reTag,
   TASK-040). Merge them to main before touching the machine.
3. **Bump the pin in the repo** — `lib/mise-path.sh` and the golden in
   `check-mise-path-install.sh`. Run the full gate (go vet/test herder+bottle + the
   `check-*.sh` battery). Commit.
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
7. **Live smoke (the upgrade gate):** spawn a throwaway tagged agent
   (`herder spawn --role smoke<ver> --agent claude --prompt 'quote your "You are tagged"
   line back to me'`) and confirm (a) spawn binds + delivers, and (b) the agent's bootstrap
   carries the tag group line. Cull it. This catches bootstrap-scrape drift the hermetic
   battery structurally cannot (canned fixtures).
8. **Record it:** task notes on the board + the run journal if a run is live.

## Known gotchas

- **Running sessions keep their old PATH — and this breaks INBOUND delivery, not just CLI
  calls.** A session started before the upgrade may have the OLD versioned mise install dir
  baked into PATH; after `mise uninstall` that binary is gone and the herder shim finds no
  real hcom — it degrades to silent exit 0. Two distinct symptoms (both live-hit on
  2026-07-08): (a) the session's own `hcom`/`herder` shell calls go quiet — fixable per
  Bash call by prepending `$HOME/.local/share/mise/shims` to PATH; (b) the session's HOOKS
  (which run in the agent process env, unreachable from a shell export) drop incoming
  message BODIES and delivery receipts — the agent sees empty `<hcom>` wakes repeating
  while the real content sits queued. (b) has no in-session fix: drain/read via
  `hcom listen`/`hcom events` in a PATH-fixed shell, and RESTART the session when
  practical (compaction does NOT help — same process, same env). Newly spawned agents are
  immune — spawn's argv prepends the mise shims dir explicitly. Upgrade sequencing lesson:
  do step 5's uninstalls when long-lived sessions (orchestrators!) are between runs, or
  accept degraded polling until they restart.
- **`hash -r`** after PATH surgery in a live shell; bash caches lookups.
- **The battery cannot see bootstrap-text drift** — its sessionstart fixtures are canned.
  Only step 6's live smoke proves the pairing. Keep fixtures covering BOTH old and new
  shapes when text changes (TASK-040 pattern: dual-style fixtures).
- The conf.d file is regenerated wholesale by ai-setup — never hand-edit it; change
  `lib/mise-path.sh` and re-run.
