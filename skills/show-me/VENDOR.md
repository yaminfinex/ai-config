# Vendored skill

- Source: https://github.com/humanlayer/skills (`plugins/show-me/skills/show-me/`)
- Upstream commit: 3c26291 (path last changed 6ab9013; vendored 2026-09-17)
- License: MIT (Copyright (c) 2026 HumanLayer)
- Local changes (2026-09-25): the HTML step's hardcoded `Bash(open …)` block is replaced by the
  host's opener (`open` on macOS, `xdg-open` on Linux), falling back to handing over the absolute
  path when neither exists. Everything else is verbatim.
- Pre-existing installer copy: `~/.agents/skills/show-me` (recorded in `~/.agents/.skill-lock.json`); `bin/ai-setup` will supersede it with a link into this repo.
