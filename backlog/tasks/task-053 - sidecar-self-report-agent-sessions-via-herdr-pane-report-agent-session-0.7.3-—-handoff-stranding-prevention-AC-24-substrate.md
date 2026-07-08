---
id: TASK-053
title: >-
  sidecar: self-report agent sessions via herdr pane report-agent-session
  (0.7.3) — handoff-stranding prevention + AC-24 substrate
status: In Progress
assignee:
  - codex-f07b1274
created_date: '2026-07-08 05:25'
updated_date: '2026-07-08 06:19'
labels: []
dependencies: []
priority: high
ordinal: 53000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed by hera on behalf of vibe + spec-ravu coordination (bus #5982; ravu memo: napkins/herder-spec/memo-sid-exposure.md in the herder-spec worktree).

Sidecar self-reports agent sessions via `herdr pane report-agent-session` (new 0.7.3 CLI). sidecar.go:438 already holds the SessionID from hcom list but never tells herdr; ONE extra call makes herder self-sufficient (no third-party integration installs needed for sid exposure), and reported sids ride PaneAgentSessionSnapshot in the HandoffManifest — meaning the NEXT `update --handoff` stops stranding registry rows entirely. This is the PREVENTION half of the handoff problem; `herder reconcile` (TASK-046, in flight) is the one-time migration half. It also makes the herder-spec AC-24 probe substrate real (per-pane sid exposure becomes herder-fed rather than assumed — spec amendment in flight on the herder-spec branch). Ordering note for TASK-046: sid-based matching stays OUT of the current reconcile fallback ladder because sids are empty by default until this lands. Implementation: codex worker per owner model policy. x-ref TASK-046, herder-spec AC-24.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:30
---
Memo path update (spec-ravu #6065): canonical copy now napkins/run-herder-dx/spec-memo-sid-exposure.md (main checkout, run napkin) — worktree copy is disposable. Wave-A sizing companion: spec-plan-wave-a.md beside it.
---

created: 2026-07-08 06:04
---
Dispatched (vibe #6530): codex worker task053-nida (f07b1274), worktree /home/grace/Coding/ai-config-task053, branch task-053-sid-reporting, brief via the 045 workaround (spawn capture missed again — same signature, another datapoint for TASK-045; bus delivery verified, worker acked). Scope fenced to sidecarcmd only with explicit stop-and-report-blocked if registry internals seem needed — no collision with wave-A1. Ground truth: ratified spec D11/AC-24 + spec-memo-sid-exposure.md. Hand-back: worker DONE -> vibe review -> hera gate (opus adversarial review per policy).
---

created: 2026-07-08 06:15
---
HAND-BACK (vibe #6674): 3 commits, real diff dfe3bec (sidecar.go +25, sidecar_test.go +131 — scope fence respected exactly, verified by hera diff --stat). vibe APPROVED, no fix round. hera gate re-run: vet+test clean (10+5 pkgs), 20/20 suites green (branch predates A1s 21st suite — expected). Opus adversarial review dispatched: @review-053-kana (wrong-guid report poisoning, dedup self-heal, fail-soft bound, 0.7.3 flag semantics, test-vs-mock drift). DEPLOYMENT NOTE recorded: running sidecars keep old binary — sid reporting effective for NEW spawns; fleet coverage arrives with natural turnover (consistent with D11 re-seat-via-observation). Third consecutive spawn-capture miss datapoint -> TASK-045. Merge gates on verdict.
---

created: 2026-07-08 06:19
---
Adversarial verdict (opus @review-053-kana #6759): FINDINGS, 1 medium. Invariants 1/3/4/5 held (incl. flags/exit-codes/stderr verified against real herdr 0.7.3). F1: failed report NOT self-healed for a stable sid — enrichedSessionID advances UNCONDITIONALLY (sidecar.go:142/174) while lastReportedSID only sets on success; after one failed report the outer guard (keyed on enrichedSessionID) never fires again for the same sid, so one transient herdr failure (500ms deadline under load, mid-handoff — precisely when it matters) silently loses sid delivery for the whole session. Test gap: failure case never issues a second poll with the same sid. Fix: key the report on lastReportedSID != row.SessionID (reportAgentSession already self-dedups), not on enrichment change; add second-poll-after-failure test. Non-blocking note: reportAgentSession trusts caller for pane-correlation — consider passing paneCorrelated explicitly. Fix round routed to vibe/worker; reviewer held for delta re-verdict.
---
<!-- COMMENTS:END -->
