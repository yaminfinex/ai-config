---
id: TASK-108
title: >-
  Investigate sustained ~34% CPU in herder observer daemon (busy-loop since
  start, empty log)
status: Done
assignee: []
created_date: '2026-07-09 06:59'
updated_date: '2026-07-09 07:14'
labels: []
dependencies: []
priority: high
ordinal: 108000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
UNIT TYPE: investigate. Deliverable is a root-cause diagnosis with evidence — NOT a merged fix. If the cause is confirmed, propose the fix shape in the report; the fix itself is a separate implement task.

SYMPTOM (measured 2026-07-09 ~07:00 by hera): the herder observer daemon (`herder observer run`, pid 2561955, started 01:06 as a setsid-detached process) shows SUSTAINED ~34% CPU — `ps` reports 120 CPU-minutes over 5h52m elapsed, i.e. ~34% averaged over its entire lifetime, so this is a continuous busy-loop, not a spike. RSS is tiny (~14MB, no leak signature). Its log at ~/.local/state/herder/observer.log is 0 bytes — nothing logged since start.

CONTEXT: the observer daemon is in an owner-sanctioned bake period (first long-running production run after the herdr-eye fix merged 6f80e26). Code lives in tools/herder/internal/observercmd/ (observer.go is the core; the daemon mode is the `observer run` persistent path, which subscribes to pane events — see the T-11d cases in tools/herder/tests/check-observer-contract.sh). Sweep cadence should be minutes-scale, and defaultReconfirmInterval is time.Hour (observer.go:26) — nothing in the intended design justifies constant CPU.

SUSPECTS TO CHECK (not exhaustive): a poll/select loop with zero or missing timeout (event subscription channel closed or erroring and the loop spinning on it — note the EMPTY log despite hours of spinning suggests a silent tight retry loop rather than logged errors); a ticker leak; a reconnect loop against the herdr socket that fails fast and retries without backoff; busy-wait on a closed pane-event stream after herdr restarted (herdr restarts DID happen during the bake window and reconnect/generation behavior across restarts is an explicit bake watch item).

HARD SAFETY RAILS: the running daemon is live bake evidence — do NOT kill, restart, or signal it destructively, and do NOT modify production code or live registry/bus state in this task. Non-destructive observation is allowed and expected: read /proc/2561955 (stat, status, wchan, task/*/stack if readable, fd listing), `strace -c -f -p 2561955` for a bounded sample (attach briefly, detach cleanly), perf sampling if available, plus code reading. Reproducing the loop with a scratch-state observer instance (separate HERDER_STATE/HOME pointing at scratch dirs) is encouraged if feasible — that instance you may kill freely.

DELIVERABLE: report (napkin under napkins/run-herder-dx/, dated) with: (1) what the process is actually doing (syscall profile or stack evidence); (2) the specific code path (file:line) responsible; (3) whether it affects correctness of the bake evidence or only burns CPU; (4) proposed fix shape; (5) whether the daemon should be restarted now or can safely finish the bake as-is.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Syscall/stack evidence from the LIVE process (bounded strace -c sample, /proc wchan/stack reads, or perf) is quoted in the report and identifies what the spin loop does
- [ ] #2 The responsible code path is named at file:line in tools/herder/internal/observercmd/ with an explanation of why it spins (which condition makes the loop hot and why it never settles)
- [ ] #3 The report states with evidence whether the busy loop corrupts or biases bake evidence (missed events, wrong unseats) or is purely a CPU burn
- [ ] #4 A reproduction exists (scratch-state observer instance exhibiting the same spin) OR the report explains concretely why reproduction was not feasible
- [ ] #5 A proposed fix shape is stated (what changes, expected CPU after) plus a restart-now vs finish-bake recommendation
- [ ] #6 The live daemon pid 2561955 was not killed/restarted/signal-disrupted; no production code or live registry/bus state modified
<!-- AC:END -->
