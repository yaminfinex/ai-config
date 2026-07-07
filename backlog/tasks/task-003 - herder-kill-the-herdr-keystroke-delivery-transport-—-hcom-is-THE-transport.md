---
id: TASK-003
title: 'herder: kill the herdr keystroke delivery transport — hcom is THE transport'
status: Done
assignee: []
created_date: '2026-07-07 05:37'
updated_date: '2026-07-07 08:26'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
DECISION (user, locked): herder message delivery has exactly one transport, the hcom bus. The herdr keystroke fallback is removed — "why be half pregnant". A target that cannot resolve to a bus-bound agent gets a clear hard error, not typed keystrokes. Ported from the parked napkin brief (napkins/herder-go-port/parked-keystroke-kill-brief.md on the origin machine — essentials embedded here since napkins do not travel).

PHASE A — map first, no code: every path reaching TransportHerdr (driver/selection.go, driver/herdr.go, send/send.go; callers: spawncmd, lifecyclecmd, waitcmd). What resolves herdr-only today (terminal ids, pane ids, bash agents, unregistered panes) and what each becomes (resolve-to-bus vs refuse). Boot-time initial prompt delivery rides the spawn paste path, NOT the delivery driver — out of scope, verify and state so.

PHASE B — the cut: remove TransportHerdr and keystroke delivery; if Selection collapses to a one-transport shell, delete the abstraction and call hcom directly (keep herdr-side RESOLUTION helpers mapping guid/label/terminal -> registry row -> hcom name). send: bus-only, hard error naming what was tried. Notify: ALREADY PARTLY DONE since the brief was written — notify is bus-native via resolveSpawnerBus, keystroke ring survives only for bus-less spawners; remaining work is removing that ring. Regenerate affected goldens (expect check-hcom-contract bus-less cases -> refusals, send/spawn/wait goldens, help text) reviewing EVERY diff.

STALENESS corrections vs the original brief: skills/herder is DELETED — the two-transport doc now lives at tools/herder/docs/delivery-drivers.md (rewrite single-transport or delete, deletion preferred if nothing unique survives); suites number 16 not 15; send --help HERDER_BUS override wording (auto|herdr|hcom) must lose its herdr arm.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 git grep TransportHerdr returns zero; no keystroke-delivery code remains in tools/herder (resolution helpers exempt)
- [x] #2 herder send at a non-bus target refuses cleanly with correct exit code — live negative smoke, no keystrokes typed
- [x] #3 Notify fully bus-native, no terminal-id ring remnants (grep-gate HERDER_NOTIFY_TO semantics)
- [x] #4 delivery-drivers.md rewritten or deleted; every regenerated golden diffed and justified; 16 suites + go gates green
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
DONE by unit-e-peso (branch unit-e-transport-kill). Commits: 6049955 (transport cut + notify ring removal, code+suites+goldens), 82311fc (merge main: units F/G), 013b9be (docs/skills single-transport rewrite, delivery-drivers.md deleted), + post-review P3 fix (spawn-patterns recipe H tail narrowed to boot-time).

PHASE A FINDINGS + RULINGS: (1) Task premise "spawn has its own paste path" was FALSE — spawn shelled out to `herder send <raw pane>` (spawn.go runSend), so a naive send cut broke spawn --prompt. Ruling (hera): move the paste engine verbatim into spawn-private internal/spawncmd/bootpaste.go, in-process; carve-out physically owned by spawn, unreachable as a transport. Fidelity proof: non-notify spawn delivery goldens passed byte-for-byte WITHOUT regeneration. (2) Steered self-compaction (herder send $HERDR_PANE_ID "/compact ...") dies with the keystroke transport — a bus message cannot type a slash command. Ruling: NO self-pane exception; TASK-022 created (herder compact reusing bootpaste); INTERIM stated in orchestrate skill + README + send --help: at context ceiling, commit + HANDOFF + fresh spawn.

WHAT CHANGED: internal/driver deleted (grep TransportHerdr = 0); hcom engine → internal/send/hcom.go, called directly (no selection abstraction); send resolves guid/short/label via registry.Resolve, term_*/pane_id via NEW registry.ActiveByPaneOrTerminal (active rows only); no row / bus-less row → refusal exit 2 naming all forms tried; HERDER_BUS=herdr → 64, =hcom keeps forced literal debug affordance; --no-enter/--no-verify/--force removed (64); vestigial herdr+jq preflights dropped. Notify: keystroke ring appendix + HERDER_NOTIFY_TO exports removed (grep = 0 over tools/skills/bin); bus-less spawner + --notify → hard error BEFORE pane creation; --notify requires --prompt; --notify-to is a bus-resolution hint only.

GOLDEN REGENS (every line reviewed): goldens/send/ 15 NEW (delivery/queued/notjoined/sendfail incl. golden-pinned hcom argv+HCOM_DIR scoping; term/pane resolution; busless/unknown/closed-pane refusals; dryruns; HERDER_BUS=herdr 64; removed-flag 64) — old 10 keystroke goldens + tests/fixtures/registry.jsonl retired with the transport; both send suites now run with NO herdr on PATH (proof of cut). goldens/spawn/: notify → hard-error block with EMPTY argv/herdr-call sections (no pane created); notify_bus/notify_enrolled lose only HERDER_NOTIFY_TO; bash_*/capture_* one summary line each. check-hcom-contract auto-selection flipped to refusals; forced-hcom cases untouched.

VERIFICATION: go vet+test green (herder+bottle) and 16/16 suites GREEN at baseline, post-cut, post-merge, post-docs; independently replicated by hera. LIVE smokes (real registry, no mocks): refusals exit 2 at real bus-less row (rev-i128div-fc83d996; hera replicated at dd58bd50), unknown label, unknown term_*; HERDER_BUS=herdr + dead flags 64; dry-run positive exit 0 (unit-e-3133824d → @unit-e-peso). Codex review: APPROVE-WITH-NITS (P3 fixed).

DOCS: delivery-drivers.md DELETED, unique survivors (incl. TASK-010 print-bypass note) relocated to README "Delivery" section; README notify/layout/docs-list; spawn-patterns recipes F+H bus-only; herder-delta banner pointer; skills/orchestrate SKILL+state-files+relay → INTERIM fresh-spawn-only; send/spawn --help; cli.go summary. FOLLOW-UPS: TASK-022 (herder compact); wave-3 nice-to-haves: spawn-suite scenarios for deep paste edges, unit tests for ActiveByPaneOrTerminal, hookcmd doctrine phrasing on next regen.
<!-- SECTION:NOTES:END -->
