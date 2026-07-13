#!/usr/bin/env bash
# Docs self-containment gate: sesh's spec/design docs live INSIDE the module
# (docs/specs/, docs/design/), so a checkout of tools/sesh alone carries the
# product contract, wire contract, and distribution design; module files cite
# them by module-root-relative path (repo-prefixed paths would dangle in a
# module-only checkout); no live file anywhere in the repo still points at
# the retired repo-root paths; and the repo-root shared-corpus docs the spec
# cites as related ground truth still exist.
#
# Deliberate sweep exclusions: backlog task bodies are immutable history;
# `git show <sha>:<path>` cites a path as it existed at that commit; and the
# grok demo report quotes captured command output verbatim.
set -euo pipefail

MODULE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SELF="$(basename "${BASH_SOURCE[0]}")"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "ok: $*"; }
step() { echo "--- $*"; }

# scan CMD... — grep/git-grep wrapper that keeps "no matches" (exit 1, a
# valid empty result) distinct from a scanner failure (exit >1), which must
# fail the gate loudly instead of false-greening an empty sweep.
scan() {
  local out rc
  out=$("$@" 2>&1) && rc=0 || rc=$?
  if [ "$rc" -gt 1 ]; then
    fail "scan failed (exit $rc): $*
$out"
  fi
  printf '%s' "$out"
}

# The three docs the module owns (basenames, regex-escaped).
DOCS='2026-07-12-sesh-store-served-distribution\.md|sesh-wire\.md|session-service-spec\.md'

step "moved docs exist inside the module"
for f in docs/design/2026-07-12-sesh-store-served-distribution.md \
         docs/specs/sesh-wire.md \
         docs/specs/session-service-spec.md; do
  [ -f "$MODULE_DIR/$f" ] || fail "missing module doc: $f"
done
ok "spec + design docs present under the module's docs/"

step "module references are module-root-relative"
BAD=$(scan grep -rn 'tools/sesh/docs/' "$MODULE_DIR" \
        --include='*.go' --include='*.sh' --include='*.md' --include='justfile' \
        --exclude="$SELF")
[ -z "$BAD" ] ||
  fail "repo-prefixed doc paths inside the module (they dangle in a module-only checkout):
$BAD"
ok "no repo-prefixed doc paths inside the module"

REPO_ROOT="$(git -C "$MODULE_DIR" rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$REPO_ROOT" ]; then
  step "repo checks"
  ok "not a git checkout — module-only tree, nothing outside the module to check"
  echo "ALL GREEN"
  exit 0
fi

step "repo sweep: zero dangling references to the retired repo-root paths"
CANDIDATES=$(scan git -C "$REPO_ROOT" grep -nE "docs/(specs|design)/($DOCS)" -- \
  ':(exclude)tools/sesh' ':(exclude)backlog/tasks' ':(exclude)backlog/archive')
# Neutralize the correct module-prefixed form first so a bare
# docs/specs|design path surviving on the line is a genuine dangler,
# however many references the line carries.
NEUTRALIZED=$(printf '%s\n' "$CANDIDATES" | sed 's#tools/sesh/docs/#<module-doc>/#g')
DANGLING=$(scan grep -E "docs/(specs|design)/($DOCS)" <<<"$NEUTRALIZED")
DANGLING=$(scan grep -vE 'git show [0-9a-f]{7,40}:' <<<"$DANGLING")
DANGLING=$(scan grep -v 'grok-demo-report-2026-07-12\.md:' <<<"$DANGLING")
[ -z "$DANGLING" ] ||
  fail "dangling references to retired repo-root doc paths:
$DANGLING"
ok "no live file references the retired repo-root doc paths"

step "shared-corpus docs cited by the spec exist at repo root"
# session-service-spec.md ("Related ground truth") cites these repo-root
# design docs. They are shared sessions/missions corpus — other lanes cite
# them too — so they deliberately do NOT move into the module; this pins
# their existence so a corpus reshuffle cannot silently orphan the spec's
# citations. The spec itself stays byte-identical (frozen content).
for f in docs/design/2026-07-09-sessions-missions-boundaries-v2.md \
         docs/design/2026-07-09-session-shipping-prior-art.md \
         docs/design/2026-07-08-sessions-missions-boundaries.md; do
  [ -f "$REPO_ROOT/$f" ] || fail "spec-cited shared-corpus doc missing from repo root: $f"
done
ok "spec-cited shared-corpus docs present at repo root"

echo "ALL GREEN"
