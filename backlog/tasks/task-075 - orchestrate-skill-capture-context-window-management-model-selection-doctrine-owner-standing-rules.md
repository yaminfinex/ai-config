---
id: TASK-075
title: >-
  orchestrate skill: capture context-window management + model-selection
  doctrine (owner standing rules)
status: To Do
assignee: []
created_date: '2026-07-08 12:23'
labels: []
dependencies: []
priority: high
ordinal: 75000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-directed (2026-07-08, in-session): fold two standing doctrines into the orchestrate skill (and its playbook templates) so every future run inherits them instead of relearning.

(a) CONTEXT-WINDOW MANAGEMENT. The orchestrator is self-aware of its OWN context and of the context of its SPAWNED agents (status lines now expose both post-TASK-063/067). Past ~200-250k tokens of context, agents become measurably less coherent AND much more expensive — context reduction is critical, not cosmetic. STANDING RULE: compact in the 200k-250k band, every time. Strategies, in preference order: (1) compact in place via herder compact (with --then for a follow-up steer when needed); (2) spawn a replacement session and take over identity — herder rename takeover per the C0-era doctrine, and CULL THE ORIGINAL AS THE FIRST ORDER OF BUSINESS (never leave two claimants). The orchestrator monitors spawned workers' context (statusline/pane read) and applies the same band to them: long-running workers approaching the band get compacted or replaced at a unit boundary.

(b) MODEL SELECTION. Fable is smart but expensive: use for planning, design, architecture, adjudication, and as an advisor — NOT for coding tasks. Codex is great at coding. Opus is great at coding. Adversarial code review is CROSS-FAMILY INTERROGATION: opus reviews codex work; codex reviews opus work (never same-family self-review). At extra-important points, run DOUBLE reviews (both families). Advisory adjudication and voting panels draw on multiple model families AND classes with multiple lenses (cf. the fable-sysreview vs opus-review069 divergence, resolved by spec ruling — the divergence itself was signal).

Notes: current run doctrine (run-log) says "codex implements, opus reviews" — (b) supersedes it with the cross-family generalization (today's runs are codex-implement so opus-review remains correct in practice). Home for the capture: the orchestrate skill's doctrine/menu sections + playbook templates; run-log tail gets the interim copy immediately (done at filing time). Propose-only beyond skill files: nothing ships without owner.
<!-- SECTION:DESCRIPTION:END -->
