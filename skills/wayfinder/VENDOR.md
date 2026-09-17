# Vendored skill

- Source: https://github.com/mattpocock/skills (`skills/engineering/wayfinder/`)
- Upstream commit: 959a8e9 (verified 2026-09-17; no upstream change to this path since 0ab1b63)
- License: MIT (Copyright (c) 2026 Matt Pocock)
- Local changes: none (body verbatim).
- Known dangling dependencies in this repo: the Skill-tool calls to upstream's `research` and
  `prototype` skills (not vendored), and the tracker doc installed by upstream's
  `/setup-matt-pocock-skills` (absent here — wayfinder then falls back to its local-markdown
  tracker). `grilling` and `domain-modeling` resolve to this repo's versions.
