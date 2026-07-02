#!/usr/bin/env bash
# trust-modals.sh — single source of truth for first-run directory-trust modal patterns.
#
# Sourced by:
#   - driver-herdr.sh (send preflight: refuse to send while the trust modal is open)
#   - herder-spawn    (readiness / modal clearing after launch)
# Both used to carry their own copies of this alternation, which drifted whenever
# claude/codex reworded their prompts. When a new agent version phrases the trust
# prompt differently, add the new alternative HERE only.
HERDER_TRUST_MODAL_ERE='Do you trust the contents of this directory|Do you trust the files in this folder|Is this a project you created or one you trust|Yes, I trust this folder'
