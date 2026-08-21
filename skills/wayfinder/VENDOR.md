# Vendored skill

- Source: https://github.com/mattpocock/skills (`skills/engineering/wayfinder/`)
- Upstream commit: 0ab1b63 (vendored 2026-08-21)
- License: MIT (Copyright (c) 2026 Matt Pocock)
- Local changes: none (body verbatim; upstream `agents/openai.yaml` not carried).
- Known dangling dependencies in this repo: the Skill-tool calls to upstream's `research` and
  `prototype` skills (not vendored), and the tracker doc installed by upstream's
  `/setup-matt-pocock-skills` (absent here — wayfinder then falls back to its local-markdown
  tracker). `grilling` and `domain-modeling` resolve to this repo's versions.
