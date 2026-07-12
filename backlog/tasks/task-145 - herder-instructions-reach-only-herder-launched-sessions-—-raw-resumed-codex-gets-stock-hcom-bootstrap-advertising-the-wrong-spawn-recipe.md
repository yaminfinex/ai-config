---
id: TASK-145
title: >-
  herder instructions reach only herder-launched sessions — raw/resumed codex
  gets stock hcom bootstrap advertising the wrong spawn recipe
status: Done
assignee: []
created_date: '2026-07-10 01:41'
updated_date: '2026-07-12 07:23'
labels: []
dependencies: []
priority: high
ordinal: 145000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
FOUND LIVE (2026-07-10, owner-hit): a raw `codex resume` of session laze had NO herder operating instructions in context — only the stock hcom [HCOM SESSION] bootstrap, which actively advertises `Spawn agents: hcom <n> <tool>` and an `hcom start` recovery hint. laze followed it and launched unmanaged reviewer agents (no registry rows, wrong panes/permissions/bootstrap); owner had to kill them.

MECHANICS (verified): the herder-aware block ("## AGENTS (herder lifecycle)" + an explicit SUPERSEDED paragraph overriding hcom spawn/kill/resume recipes) is injected ONLY by herder spawn generated launch scripts (~/.hcom/.tmp/launch/codex_*.sh). hcom hooks in ~/.codex/config.toml fire for ANY codex session, so raw launches/resumes get the stock template with the competing recipe. The unmanaged path is the only one advertising itself to unmanaged sessions.

DIRECTIONS TO EVALUATE: (a) machine-wide hcom template override so the stock bootstrap on this box points at herder (does hcom config support template customization?); (b) herder observer/sidecar adoption injects or delivers the overlay when it recognizes an unmanaged-but-seated session; (c) at minimum, herder resume re-injects the overlay (managed resume may already — verify) and docs tell operators to prefer herder resume over raw codex resume. Coordinate with the spawn/resume placement work.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Verified statement of which launch/resume paths carry the herder overlay today (spawn, herder resume, fork, raw claude, raw codex, raw codex resume)
- [x] #2 Chosen mechanism implemented so raw-launched/resumed sessions on this machine no longer see the bare hcom spawn recipe without the herder supersede
- [x] #3 Regression check covering the injection (script-level test or documented manual verification)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Research leg dispatched 2026-07-10 to gpt-5.6-sol (@researcher-todo, branch task-145-injection-research), brief napkins/run-herder-dx/task-145-research-brief.md; deliverable = committed design memo + matrix + recommendation. Implement leg is a separate unit after the direction is picked.

Research leg DONE + merged (fbb378b --no-ff, docs-only): memo docs/design/2026-07-10-herder-instruction-injection.md. VERIFIED MATRIX: herder spawn/resume/fork inject the overlay for BOTH families (claude via SessionStart rewrite; codex via launch-time developer_instructions or post-bind verified bus re-delivery — the codex resume path warns, not fail-closed, on timeout). Raw claude: NOT covered (vendor binary shadows the herder shim; global hook gives only an availability hint). Raw codex fresh: NOT covered, and does NOT get the competing stock recipe either. Raw codex RESUME: the dangerous one — retains the historical stock hcom recipe from thread history while getting no overlay. hcom stock template is compiled into upstream src/bootstrap.rs — NO override mechanism; notes/codex_system_prompt cannot fix resume (preprocessing strips user developer_instructions). RECOMMENDATION (implement leg, awaiting owner ruling): observer delivers the existing CodexResumeAddendum as informational bus doctrine ONLY on authoritative correlation (live pane/process + tool session id + joined hcom row), no auto-enroll, no guessing on ambiguity; plus shim PATH repair and herder-resume-not-raw docs as defense in depth. Alternative: wait for upstream hcom seam (leaves the known path unsafe). Upstream draft candidate goes to the ledger with the implement leg.

OWNER RULING (2026-07-10, chat): mechanism (a) chosen — observer delivers CodexResumeAddendum as informational bus doctrine ONLY on authoritative correlation (live pane/process + tool session id + joined hcom row), no auto-enroll, no guessing on ambiguity; plus shim PATH repair and herder-resume-not-raw docs as defense in depth. ADDED SCOPE (owner): capture the operating rule in the new-harness runbook (docs/new-harness-onboarding.md) — always herder resume, never raw codex resume, plus a pointer to the injection matrix memo. SEQUENCING: implement leg dispatches after the TASK-146 synthetic exercise + autostart flip (the mechanism rides a running observer).

Implement leg dispatched 2026-07-12 (worker razu, branch task-145-injection-implement, gpt-5.6-sol high reasoning), brief napkins/run-herder-dx/task-145-implement-brief.md. TASK-156 docs fix bundled into the same unit.

Implement leg shipped in merge 22def8f (commits 8e81e87+a0afeda+38cccb2). Mechanism (a) as ruled: observer delivers CodexResumeAddendum informational-only on four-leg authoritative correlation, fail-closed silent on any missing leg, one-shot per incarnation via status receipts (positive-evidence retention, 24h TTL bound), zero registry writes. Docs: herder-resume-not-raw in new-harness-onboarding + memo pointer. Opus review: APPROVE r1 (3 non-blocking, hardened by choice), REQUEST-CHANGES delta r1 (sub-transport prune violated one-shot), APPROVE delta r2. Gates 53/53 every round + post-merge. NOTE: feature activates for real when the observer next restarts (live pid 2876552 untouched, owner controls rollout).
<!-- SECTION:NOTES:END -->
