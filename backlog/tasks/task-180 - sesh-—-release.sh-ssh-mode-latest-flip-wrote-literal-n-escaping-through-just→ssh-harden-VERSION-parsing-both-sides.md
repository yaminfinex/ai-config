---
id: TASK-180
title: >-
  sesh — release.sh ssh-mode latest flip wrote literal 'n' (escaping through
  just→ssh); harden VERSION parsing both sides
status: To Do
assignee: []
created_date: '2026-07-13 06:03'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 179000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
FIELD BUG, first live publish 2026-07-13: the remote latest flip's printf lost its backslash through the just→sh→ssh quoting chain, format became '%sn', and the store served VERSION 'sesh-v0.1.0n' → installer requested a nonexistent version dir → 404. Live data repaired by hand (echo > tmp && mv, verified cat -A). This was builder-vame's flagged-untested path #5 (ssh mode never run against a real remote) — the flag was accurate. Fix: (1) make the remote flip escaping-proof (avoid backslash sequences in the ssh command string entirely); (2) harden install.sh AND sesh update to trim whitespace and validate fetched VERSION against the version-shape regex, failing loudly on mismatch instead of building a 404 URL; (3) extend the release gate to exercise the ssh path via an ssh-shim seam so local-mode green can never again mask ssh-mode breakage.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Remote latest flip produces byte-exact version + newline, proven by a gate that runs release.sh through an ssh shim
- [ ] #2 install.sh and sesh update reject malformed VERSION loudly (tested with the literal 'sesh-v0.1.0n' regression bytes)
- [ ] #3 Docs current: ops/README publishing section notes the quoting hazard class
<!-- AC:END -->
