---
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
execution: code
title: "feat: port herder substrate from bash to Go (tools/herder)"
date: 2026-07-02
plan_depth: deep
---

# feat: herder → Go — 1:1 port, goldens as the spec

> Historical port plan. Its per-team bus compatibility requirement is superseded: herder now
> spawns only on the node's global bus. The old contract below remains as port provenance, not
> current CLI or operational guidance.

## Why

~2,200 lines of bash whose core is no longer orchestration: send/verify state machine,
resolution structs faked as `pane|via|drifted|note` strings, globals-as-args, `set -e`
dodging, python3/awk escapes. Growth direction (more drivers, teams, concurrency) is
where bash costs compound. Repo already pays Go's entry costs: bottle ships via
`bin/bottle` hash-keyed build-cache launcher — distribution solved, agent-patchability
preserved (edit .go → next invocation rebuilds).

## Non-goals (hard)

**Zero behavior change.** Same stderr summaries byte-for-byte, same `--json` stdout
shapes, same exit codes (0/1/2/64), same herdr/hcom argv patterns (mocks assert them).
No new features, no locking improvements, no opencode. Improvements come AFTER flip.

## Decisions

- **D1** One binary `herder` with subcommands `spawn|send|list|wait|cull`; module
  `ai-config/tools/herder`, go 1.26, layout mirrors `tools/bottle` (cmd/ + internal/).
- **D2** `bin/herder` launcher = bottle pattern (source-hash cache key, mise fallback).
- **D3** Existing hermetic suites + goldens are the acceptance spec. Suites gain a bin
  indirection (generalize `HERDER_SEND_BIN` → per-tool `HERDER_*_BIN`, default = bash
  scripts) so the SAME suite runs against either implementation; port gate = green
  against Go with zero golden edits.
- **D4** Drivers become a Go interface (`Resolve`, `Send`) with herdr + hcom impls;
  registry-driven auto-selection ported as-is. Trust-modal ERE becomes a shared Go
  const (keep `trust-modals.sh` until bash deleted).
- **D5** Historical port boundary: the old hcom launch relay and PATH shims originally stayed bash as
  exec-into-hcom env wrappers. Post-port cleanup removed the vestigial relay
  relays; spawn, lifecycle, and PATH shims now exec the Go `herder launch` path with
  sidecar startup directly. PATH shims still remain shell wrappers.
  `lib/hcom-hooks.sh`/ai-setup remain out of scope.
- **D6** Path compatibility on flip: `skills/herder/scripts/herder-*` become 2-line
  exec shims to `bin/herder <subcommand>` — SKILL.md paths and agent muscle memory
  survive; bash implementations (scripts bodies + lib/driver-*.sh etc.) deleted same
  commit (git history = reference; pinned at d4ca54c).
- **D7** Spawn/list/wait have NO hermetic suites today (live-validated only). Rule:
  **characterize bash first** (new mock-herdr scenarios + goldens generated from the
  bash implementation), THEN port. Never write the golden from the Go side.
- **D8** Live smoke is a mandatory gate twice (P5, P6): mocks provably miss real-timing
  bugs (this run's W1 unconditional-pin bug was caught only live).

## Units

| Unit | Work | Gate |
|------|------|------|
| P0 | Suite bin-indirection across all 5 suites; characterization suites + goldens for send-path of `list`, `wait`, `spawn` (mock-herdr scenarios: readiness/modal, new-tab root-close, name capture, brief staging, notify) — generated FROM bash | all suites green vs bash, both default + explicit `HERDER_*_BIN` |
| P1 | Scaffold: `tools/herder` module, subcommand dispatch, `bin/herder` launcher; `internal/registry` (JSONL, latest-by-guid), `internal/herdrcli` (exec herdr, typed payloads) | `go test ./...` + `go vet`; launcher builds/caches/execs |
| P2 | Port resolve + herdr send driver (state machine: land/re-paste, submit, verify, paste-blob, queued, extra-Enter) as `internal/driver` + `herder send` | `check-send-contract.sh` green vs Go, zero golden edits |
| P3 | Port hcom driver + registry-driven selection (`HERDER_BUS` auto/override) | `check-hcom-contract.sh` green vs Go |
| P4 | Port `list`, `wait`, `cull` (incl. D8 bus-drop sweep) | `check-cull-busdrop.sh` + P0 list/wait suites green vs Go |
| P5 | Port `spawn` (readiness/modal machine, new-tab dance, hcom name capture, codex brief staging, notify wiring, perm injection, login-shell argv) | P0 spawn suite green vs Go + live spawn/send/cull smoke |
| P6 | Flip: scripts → exec shims (D6), bash bodies deleted, `bin/` symlinks → launcher-backed | ALL suites green vs Go + full-chain live smoke (spawn both agents, duplex bus msg, wait, cull, buses empty) |
| P7 | Docs: SKILL.md + delivery-drivers.md + this plan to shipped reality | doc review vs source |

Sequencing: P0 → P1 → {P2→P3, P4 after P2} → P5 → P6 → P7. P2 is the risk spike —
if byte-parity proves impractical there, stop and renegotiate the contract before
porting more.

## Risks

- **Byte-parity of stderr/JSON** — goldens catch; keep bash formatting quirks verbatim
  (printf widths, `// empty` fallbacks → Go zero-values must serialize identically).
- **Mock fidelity** — mocks assert bash's herdr argv patterns; Go must issue identical
  argv. Treat divergence as a Go bug, never a mock edit.
- **Spawn characterization gaps** (P0) — mock can't reproduce every timing path; the
  P5/P6 live smokes are the backstop.
- **jq-in-registry semantics** — `group_by(.guid)|map(.[-1])|last` tie-breaking must be
  reproduced exactly (ordering, duplicate handling).

## Unresolved questions — provisionally resolved (user AFK; defaults taken, renegotiable)

1. **D6 delete-on-flip**: YES — delete bash bodies on flip; git history (d4ca54c) is
   the reference. (Alternative was legacy/ quarantine.)
2. **Live smokes unattended**: YES — orchestrated agents run them (same shape as this
   run's SMOKE-OK gate; ~2 short-lived workers per smoke).
3. **wait suite depth**: shallow (resolution/args/output via mock-herdr, no real-sleep
   timeout paths); live smoke covers actual waiting.

## SHIPPED — P7 addendum (2026-07-02, post-run)

The port shipped on `feat/herder-go-port`: P0 9db5e3c → P6 fd448d6. All 8 hermetic suites
green on **default paths** (i.e. Go, through the flipped script shims); zero golden edits
across P2–P6; P2's byte-parity risk spike passed; full-chain live smoke (claude + codex,
duplex bus messaging, completing wait leg, cull, zero smoke residue) passed on the global bus.
`skills/herder/scripts/herder-*` are 2-line exec shims to `bin/herder <sub>`;
`lib/{delivery-driver,driver-herdr,driver-hcom}.sh` deleted (−2028 lines). Later substrate
work removed the vestigial hcom launch relays, routed spawn/lifecycle/PATH shims directly
through `bin/herder launch`, single-sourced hcom-capability/config pinning in `launchcmd`,
and deleted the remaining `skills/herder/scripts/lib/` helpers. PATH shims remain shell wrappers.

Deviations from plan, all recorded in the run-log:

- **P3/P4 serialized** (plan allowed parallel): one worktree, one writer.
- **D3 indirection landed per-tool**: `HERDER_{SEND,LIST,WAIT,CULL,SPAWN}_BIN` (P0 decision),
  not a single `HERDER_BIN_DIR`.
- **Launcher grew portability beyond the bottle clone** (`bin/herder`): hash tool fallback
  `sha256sum` → `/sbin/sha256sum` → `shasum -a 256`; writable-cache candidates
  `$XDG_CACHE_HOME` → `$HOME/.cache` → `$TMPDIR`, with `GOCACHE` exported under the selected
  cache when absent/unwritable; exports `AI_CONFIG_ROOT` so the cached binary resolves
  `skills/herder/scripts` sibling helpers from any cwd.
- **Teams demoted to optional coded capability** (USER decision, P6 sliding door): the global
  bus is the default operating posture; `--team` stays ported + golden-locked
  (zero-behavior-change holds) but no protocol may require it. Cause: the documented
  one-time claude onboarding on first team-bus launch per machine (`lib/hcom-tools.sh`
  KNOWN CAVEAT; W5 state-file seeding would lift it).
- **Substrate backlog, not port scope**: herdr reports hcom-backed panes as
  `agent_status=unknown` (bash and Go alike — parity holds), so `herder-wait --status idle`
  can time out on an idle peer; hcom `listening` / `--status unknown` are the workarounds.

Post-flip improvement backlog (out of port scope, per Non-goals) starts from this baseline.
