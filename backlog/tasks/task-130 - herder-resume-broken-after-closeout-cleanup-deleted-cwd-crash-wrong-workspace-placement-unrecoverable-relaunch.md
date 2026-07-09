---
id: TASK-130
title: >-
  herder resume: broken after closeout cleanup (deleted-cwd crash,
  wrong-workspace placement, unrecoverable relaunch)
status: To Do
assignee: []
created_date: '2026-07-09 23:07'
labels: []
dependencies: []
priority: high
ordinal: 130000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Capture (live incident, 2026-07-09, guid ea3332ea / task129-laze)

Owner asked to speak to a culled worker; `herder resume` is the supported path but failed three ways in sequence. Resume-after-closeout (worktree + branch removed, seat culled) is a normal, supported flow — it must work or refuse loudly upfront.

## Observed defects (in order)

1. DELETED-CWD LAUNCH: first `herder resume` relaunched the codex session with its recorded cwd pointing at the removed worktree. The session started anyway; its hooks failed every event with "No such file or directory (os error 2)" (SessionStart, UserPromptSubmit), and the session died shortly after. Resume did not preflight cwd existence. Expected: refuse with a remedy ("cwd gone — recreate it or pass a new cwd"), or remap to a safe cwd explicitly.
2. WRONG-WORKSPACE PLACEMENT: the resumed pane landed in workspace w1B — an UNRELATED workspace belonging to another project's session (quick-sites) — apparently "currently focused workspace" targeting. Violates the workspace-affinity principle (see TASK-124 addendum) and confused the owner. Expected: resume into the guid's own/new workspace like --worktree spawns do, or accept a placement flag.
3. UNRECOVERABLE RELAUNCH LOOP: after the broken seat died, subsequent attempts failed hard even once the cwd was recreated: `herder resume` → "session is retired; run herder reopen"; `herder reopen <guid>` → "reopened ... unseated and unlabelled"; `herder resume` again → "launch failed before lifecycle bind" (lifecyclecmd/lifecycle.go verifyLaunchStayedAlive: pane exits inside the settle window), repeatably. The rollout file was valid the whole time — a raw `codex resume <sid>` in a plain pane worked immediately, proving the session was resumable and the failure is in the herder/hcom relaunch path. Diagnosis needed: why does the relaunched pane exit pre-bind (stale hcom instance state for the bus name? launch script env? pty)? The launch failure surfaces ZERO diagnostic detail — "launch failed before lifecycle bind" names neither the pane's exit output nor the failing layer.

## Acceptance criteria

1. Repro of each defect encoded as a test where feasible (resume-with-missing-cwd must refuse-with-remedy before launching; goldens for the refusal).
2. Resume/reopen placement follows workspace affinity (own workspace/tab), never the focused workspace of an unrelated project.
3. The relaunch-loop root cause is identified and fixed OR the failure surfaces the pane's actual exit output in the error.
4. Full house gate green.
<!-- SECTION:DESCRIPTION:END -->
