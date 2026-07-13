---
id: TASK-184
title: >-
  sesh — setup bootstraps launchd into gui/0: Options.UID default never applied
  (Darwin onboarding blocked)
status: Done
assignee: []
created_date: '2026-07-13 06:15'
updated_date: '2026-07-13 06:41'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 183000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
FIELD BUG, first real Darwin onboarding 2026-07-13: install.sh → sesh setup rendered the plist fine, then ran launchctl bootstrap gui/0 (exit 125). internal/setup/setup.go documents UID 'default: os.Getuid()' but no defaulting exists; internal/cli/setup.go builds Options without UID (zero value 0); internal/cli/update.go:64 sets it explicitly, and unit tests inject 501 explicitly — so only the real-Mac setup path hit gui/0. Fix: apply the documented default inside setup.Run (UID==0 → os.Getuid()) so no caller can forget; on Darwin refuse a real uid of 0 outright (root has no per-user gui domain and LaunchAgents are per-user by design); regression test builds Options WITHOUT UID and asserts the rendered gui/<uid> domain.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 setup.Run defaults UID from os.Getuid() when unset; caller-omission regression test asserts gui/<real-uid>, not gui/0
- [x] #2 Darwin path refuses uid 0 with a clear message
- [x] #3 launchctl invocation lines in tests derive from the same domain helper the code uses (no fixture drift)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Done commit 251684c. UID default inside setup.Run, Darwin refuses uid 0 pre-write, shared LaunchdDomain helper at all launchctl sites incl. tests. Reviewer-novu: CLEAN first pass (#56443). Field workaround used on owner Mac pre-fix: manual launchctl bootstrap gui/$(id -u).
<!-- SECTION:NOTES:END -->
