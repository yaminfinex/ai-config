#!/usr/bin/env bash
# hcom-tools.sh — single source of truth for which tools launch through the hcom bus
#                 and how each one's real config dir is pinned before exec.
#
# Sourced by:
#   - herder-spawn — gates which tools it ROUTES through hcom (is_hcom_capable).
#   - hcom-launch  — pins the real per-tool config dir before exec (hcom_pin_config_dir).
# Both used to carry their own copies of these lists, which drifted (herder-spawn once
# routed opencode through hcom while the pin table didn't cover it → isolated-bus
# opencode hit the fresh-config login wall). The capability list and the pin table are
# now ONE table here. Adding a tool to is_hcom_capable REQUIRES adding its pin in
# hcom_pin_config_dir IN THE SAME EDIT — the two must never disagree.

# is_hcom_capable <tool> — 0 if this tool should be routed through hcom, 1 otherwise.
is_hcom_capable() {
  case "$1" in
    claude|codex|gemini) return 0 ;;
    # opencode is deliberately excluded: hcom local mode redirects OPENCODE_CONFIG_DIR
    # too, but opencode's REAL default config location is unverified, so we cannot safely
    # pin it. Until that's verified, opencode spawns stay raw/keystroke-only rather than
    # risking the fresh-config auth bug on an isolated team bus. When verified, add it
    # here AND add its pin in hcom_pin_config_dir below in the same edit.
    *) return 1 ;;
  esac
}

# Keep AUTH + real settings on the REAL config dir when HCOM_DIR points at an isolated bus.
# hcom's "local mode" (any HCOM_DIR != ~/.hcom) otherwise DERIVES per-tool config dirs from the
# PARENT of HCOM_DIR (CLAUDE_CONFIG_DIR=<parent>/.claude, CODEX_HOME=<parent>/.codex, ...; documented
# in `hcom reset --help`). That fresh empty dir has no credentials/onboarding → a login wall + lost
# global permissions/trust/CLAUDE.md, and the tool parks before SessionStart so hooks never bind.
# hcom PASSES THROUGH a pre-set config-dir env (verified: no override), so we pin the real dir here.
# Effect: bus location and config dir become INDEPENDENT axes — an isolated HCOM_DIR ringfences bus
# traffic (hook subprocesses inherit it from the tool's env) while auth + hooks stay on the real dir.
#
# The pin fires ONLY when the effective HCOM_DIR would actually trip hcom local mode (set and
# != ~/.hcom) — the exact condition hcom itself uses. It is NOT unconditional: for claude,
# set-vs-unset moves the json state file (unset → ~/.claude.json, the real 100K+ user state;
# set to $HOME/.claude → ~/.claude/.claude.json, a fresh file) — so pinning on the GLOBAL bus
# regressed every launch into first-run onboarding (theme picker) + hcom launch_blocked. Proven
# live in the W4 smoke (run-log: W4 SMOKE INCIDENT); on the global bus hcom sets nothing and
# neither do we. An already-set override is respected either way.
# KNOWN CAVEAT (team buses): with the pin, claude reads state from ~/.claude/.claude.json, which
# starts fresh → one-time onboarding on the first team-bus launch per machine. Tracked for W5.
#
# hcom_pin_config_dir <tool> — pin <tool>'s real config dir; no-op for tools without a known pin
#                              and on the global bus (HCOM_DIR unset or == ~/.hcom).
hcom_pin_config_dir() {
  [[ -n "${HCOM_DIR:-}" && "${HCOM_DIR}" != "$HOME/.hcom" ]] || return 0
  case "$1" in
    claude) export CLAUDE_CONFIG_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}" ;;
    codex)  export CODEX_HOME="${CODEX_HOME:-$HOME/.codex}" ;;
    gemini) export GEMINI_CLI_HOME="${GEMINI_CLI_HOME:-$HOME/.gemini}" ;;
  esac
}
