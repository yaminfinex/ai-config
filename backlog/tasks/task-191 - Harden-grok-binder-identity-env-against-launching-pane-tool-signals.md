---
id: TASK-191
title: Harden grok binder identity env against launching-pane tool signals
status: In Progress
assignee: []
created_date: '2026-07-13 13:01'
updated_date: '2026-07-13 22:01'
labels: []
dependencies: []
ordinal: 190000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up from the activation unit's review chain (reviewer direction (b), deferred to keep activation lean). The grok bridge binder's identity invocation (runHcomSeatIdentity) builds its env from os.Environ(), scrubbing only HCOM_PROCESS_ID/CODEX_THREAD_ID/HCOM_TAG — it inherits the launching pane's tool signals (CLAUDE*/CLAUDECODE). hcom start keys claude-hook-install-and-exit-1 off those vars (suppressed only by HCOM_LAUNCHED/adhoc signals), so a binder started from a Claude-pane context without a launched signal can hit hcom's hook-install path instead of binding. Reachability today is narrow (herder-spawned panes carry HCOM_LAUNCHED; bare terminals carry no CLAUDE*), but the binder should present a DETERMINISTIC identity env regardless of who launched it: allowlist-build the identity-invocation env (house rule: allowlists on security boundaries) rather than scrub-listing os.Environ(). Includes: pin that a binder launched with hostile/foreign tool signals still binds adhoc and never triggers hook installation. Evidence and bisection: activation review thread, reviewer round-2/round-3 findings.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
LIVE EVIDENCE 2026-07-14 (first production grok spawn, calibration reviewer): bridge 'hcom start' resolved hcom via ambient PATH -> mise shim, which is CWD-SENSITIVE — with cwd inside a task worktree mise errored 'no tasks defined' and hcom start exited 1; bridge retried to the 8s deadline; seam hard-fail cleaned up correctly (row gone, pane torn down, launch-error written at <state>/grok/<guid>/). Workaround that worked: HERDER_REAL_HCOM=<abs mise install path> on the spawn. This task's allowlist-env scope should INCLUDE deterministic hcom resolution (pin the resolved absolute hcom at plan time, not PATH-walk at bind time) alongside the CLAUDE*/tool-signal neutralization. Priority effectively raised: without it, grok spawns fail from any cwd where the mise shim misbehaves.

ROOT CAUSE REFINED 2026-07-14 (mika, owner-hit from a napkin dir): resolveRealHcom (tools/herder/internal/launchcmd/grok.go) DOES walk PATH and skip herder shims, but canonicalFile RESOLVES THE SYMLINK of the mise shim (~/.local/share/mise/shims/hcom -> ~/.local/bin/mise); mise dispatches on argv[0], so the recorded hcom-bin is the mise binary itself and the bridge execs mise with hcom args -> "no tasks defined in <cwd>" -> 8s ready deadline -> launch fail. Evidence: <state>/grok/<seat>/bridge.log + hcom-bin containing the mise path. cwd-dependence masked it. Fix space per report: do not canonicalize through argv0-dispatch shims (skip canonicalization when symlink target basename != hcom), or resolve via the shim manager, or keep walking past mise shims like herder shims. OWNER IS BLOCKED without HERDER_REAL_HCOM.
<!-- SECTION:NOTES:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Deterministic hcom resolution: the bridge never records an argv0-dispatch shim target as hcom — canonicalization must not resolve through a symlink whose target basename is not hcom (mise-shim class); the recorded hcom-bin execs successfully from ANY cwd including non-project dirs, no HERDER_REAL_HCOM needed
- [ ] #2 Binder identity-invocation env is allowlist-BUILT (not scrub-listed from os.Environ()); hostile/foreign tool signals (CLAUDE*/CLAUDECODE/HCOM_*) present at launch never alter binding and never reach hcom hook-install
- [ ] #3 Regression tests pin both classes: (a) fake argv0-dispatch shim on PATH is resolved past to the real hcom, cwd-independent; (b) identity-env allowlist mutation test in the env class that exposed the original failure
- [ ] #4 HERDER_REAL_HCOM override still honored and documented as the escape hatch
<!-- AC:END -->
