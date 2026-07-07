---
id: TASK-017
title: 'codex resume/fork: deliver herder doctrine post-boot (seam cannot)'
status: Done
assignee: []
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 20:27'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit C finding (TASK-014): hcom strips ALL user developer_instructions on codex resume/fork and re-adds only its own bootstrap — structurally unfixable at the launch-args seam, so resumed/forked codex sessions see only hcom stock guidance. Wave-2 candidate: resume/fork cmds deliver the herder addendum post-boot via herder send / hcom message once the session is up. Needs design (timing, dedup, idempotence).

SCOPE ADDITION (wave-2 findings): also close the launch --help documentation gap — it says nothing about the codex bootstrap addendum (TASK-014) or its resume/fork strip behavior (Unit F finding #2, Unit G nice-to-have).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Design ratified on thread unit-l BEFORE build: bus-message transport, registry-bind readiness poll, no-dedup idempotence, warn-never-block failure mode
- [x] #2 herder resume of a codex target delivers a resume-variant herder addendum to the session's newly bound bus name post-boot, delivery verified (delivered/queued reported on stderr)
- [x] #3 herder fork of a codex target (native path) delivers the same addendum to the child; claude resume/fork paths are byte-identical in behavior (no addendum send, no added wait)
- [x] #4 Bind timeout or delivery failure warns on stderr and resume/fork still exit 0 — doctrine delivery never blocks or fails the lifecycle command
- [x] #5 Resume addendum shares the AGENTS doctrine + codex SUBAGENTS section byte-identically with CodexBootstrapBlock (drift guard extended); preamble reworded for mid-conversation message delivery incl. harmless-repeat framing and do-not-reply
- [x] #6 check-resume-contract + check-fork-contract gain codex addendum cases (delivered path, bind-timeout warn path); every golden diff line justified; pinned gate 17/17 + go vet/test green
- [x] #7 launch --help documents the codex addendum, the resume/fork strip, and post-boot re-delivery (prose only); README resume/fork Session Bootstrap gap paragraph updated to the new behavior
- [x] #8 LIVE smoke: a real codex session resumed via herder resume receives the addendum on its bus (transcript/pane evidence in DONE report)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 5cc551e (unit-l-codex-resume). Root cause: hcom strips ALL user developer_instructions on codex resume/fork and re-adds only its own bootstrap (TASK-014), so the launch-args seam cannot carry the herder addendum there. Fix: post-boot bus delivery from lifecyclecmd — resume()/fork() of a codex target poll the registry until the sidecar binds the new session's hcom_name (HERDER_ADDENDUM_SETTLE_MS, default 60000ms, <=0 skips), then deliver hookcmd.CodexResumeAddendum via the in-process verified-delivery engine (send.Run — sliding door ratified by hera over the BinHerder subprocess; codex review APPROVE zero findings, #2737). Design points as blessed: bus message not system prompt (preamble self-identifies, do-NOT-reply, repeat-is-a-no-op); NO dedup state (dedup false-skips exactly when compaction ate the prior copy); bind timeout / send failure WARN with manual remedy and NEVER fail the lifecycle command; claude paths byte-identical (sessionstart rewrite covers claude resume). CodexResumeAddendum shares the doctrine tail (herderAgentsSection + new codexSubagentsSection) with CodexBootstrapBlock; drift guard extended (verbatim embed x3 + HasSuffix shared-tail pin); launch-block byte-identity proven by unregenerated launch/hook goldens. Suites: 3 new goldens (resume delivered w/ full 2526-char addendum pinned verbatim, resume bind-timeout warn, fork native-codex delivered); pre-existing goldens ZERO churn (conditional codex seed + conditional HCOM SEND ARGV section); mock sidecar-bind subshell must detach stdio or it holds the exec pipe (found+fixed). Verification: go vet/test green, battery 17/17, suites flake-checked x2; LIVE smoke: real codex spawned/culled/resumed via worktree binary — verify=queued, hcom deliver receipt event 2606, addendum present in resumed rollout jsonl, codex correctly gave no reply. launch --help codex gap closed (prose only per fence) + resume/fork --help + README Session Bootstrap updated. Residual: codex fork --self fallback rides spawn, no post-boot path — TASK-027 (filed by hera). Nice-to-haves: TASK-027 could parse spawn --json from forkSelfFallback; gemini has no doctrine block at all; live smoke showed hcom reuses the same bus name on resume (registry bind makes this irrelevant).
<!-- SECTION:NOTES:END -->
