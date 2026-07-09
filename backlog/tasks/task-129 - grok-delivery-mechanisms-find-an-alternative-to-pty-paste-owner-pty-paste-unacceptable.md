---
id: TASK-129
title: >-
  grok delivery mechanisms: find an alternative to pty-paste (owner: pty-paste
  unacceptable)
status: To Do
assignee: []
created_date: '2026-07-09 21:11'
labels: []
dependencies: []
priority: high
ordinal: 129000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Capture (owner directive, 2026-07-09)

UNIT TYPE: investigate. The grok characterization (docs/design/2026-07-09-grok-cli-characterization.md) concluded grok's only mid-session message delivery path is pty-paste, because passive-hook stdout is discarded. Owner: pty-paste is UNACCEPTABLE as the delivery mechanism — investigate whether alternative mechanisms exist before any integration work proceeds. Deliverable is an evidence-backed mechanism matrix + recommendation, NOT code.

## Read first

- docs/design/2026-07-09-grok-cli-characterization.md (what was already tested — do not re-derive)
- docs/design/2026-07-09-new-harness-onboarding-playbook.md (shape taxonomy: hook-injection / launch-arg+redelivery / pty)
- For contrast: how codex delivery actually works today (hcom hooks fire at turn boundaries and hand queued messages to the model; herder polls deliver: receipts).

## Candidate mechanisms to probe (each empirically, in scratch)

1. BLOCKING hook semantics: characterization proved passive-hook STDOUT is discarded — but Claude's hook contract has other channels: exit-code-2 STDERR feedback (blocking error text reaches the model), PreToolUse permission decisions (JSON verdicts), Stop-hook block-with-reason. Does grok's compat mode honor ANY output channel that reaches the model? If Stop-hook exit 2 + stderr lands in context, that is a delivery path.
2. MCP: does grok support MCP servers (config, CLI flags)? If yes: a bus-bridge MCP server could deliver via tool results (model polls a receive_messages tool prompted by bootstrap rules) or MCP notifications/sampling if honored. Characterize what grok's MCP client actually supports.
3. Headless/programmatic mode: does grok have a non-interactive JSON/stream mode (claude -p analogue, stdin-driven turns, --output-format stream-json, serve/API mode)? If herder can drive turns programmatically, terminal delivery is moot for worker seats.
4. Session-file/context-file ingestion: grok ingests ~/.claude/CLAUDE.md and honors --rules; is any context source re-read mid-session (file watch, per-turn re-read)? A re-read file = a mailbox.
5. Kill-and-resume delivery: does grok have resume-with-appended-prompt (session id + resume flag)? Delivery as restart is heavy but proven in-house (compact --then pattern). Cost: loses in-flight turn state.
6. Anything else the docs/help/config surface reveals (env vars, sockets, IPC, notify config with a response channel). Sweep grok --help, config schema, release notes for the installed version.

## Safety rails

Scratch dirs only; never touch the live registry/bus; no account signups — a missing key/entitlement is a documented blocker, not a task. No herder/hcom production code changes. If a mechanism needs an upstream grok change, capture it as an upstream-filing candidate (owner files; TASK-029 ledger).

## Acceptance criteria

1. A mechanism matrix doc exists (docs/design/, dated): every candidate above marked WORKS / PARTIAL / DEAD with the actual command + observed output quoted for each verdict.
2. The blocking-hook channels (exit-2 stderr, PreToolUse decisions, Stop block) are each tested explicitly — these are the cheapest wins and the characterization did not exercise them.
3. MCP support is definitively characterized (supported/version/what reaches the model).
4. A recommendation section: the best non-pty delivery mechanism (or a finding that none exists at this grok version, with the specific upstream asks named), plus what it changes in the playbook's Shape B design.
5. No production code or live state modified; all experiments in scratch.
<!-- SECTION:DESCRIPTION:END -->
