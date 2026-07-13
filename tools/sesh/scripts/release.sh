#!/usr/bin/env bash
# Publish a sesh release to the store's channel (design §7). Two-verb
# discipline: publishing artifacts and deploying/restarting the store are
# separate, deliberate acts.
#
#   1. matrix build (CGO_ENABLED=0, darwin/linux × arm64/amd64, -trimpath,
#      stamped version) into releases/<ver>/ + SHA256SUMS
#   2. rsync releases/<ver>/ to a REMOTE STAGING dir (.staging-<ver>/)
#   3. remotely verify staged SHA256SUMS (sha256sum -c)
#   4. atomic remote mv staging → releases/<ver>/ — REFUSED if <ver> already
#      exists (version dirs are immutable; republishing is an error)
#   5. write `latest` via temp + rename + sync — only after step 4
#
# Staging-then-rename: a crashed publish must never leave a partial tree at
# a FINAL version path that a retry mutates or `latest` might point to.
# Stray .staging-* dirs are cleaned by the next run. A failed publish leaves
# the previous `latest` fully usable. Rollback = republish nothing; rewrite
# `latest` to the previous version string (a deliberate, visible fleet
# downgrade — sesh update's equality-only semantics).
#
# DEST is the channel root: 'host:/var/lib/sesh/releases' (ssh) or a local
# absolute path (used by tests/check-release-publish.sh).
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: release.sh DEST [--version V] [--stub-build] [--fail-after stage|verify]

  DEST          channel root: [user@host:]/abs/path (the store data dir's
                releases/ directory)
  --version V   override the git-describe version (test seam)
  --stub-build  write stub artifacts instead of compiling (test seam)
  --fail-after  exit 1 after the named step (test seam for crash injection)
USAGE
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(dirname "$SCRIPT_DIR")"
DEST="${1:-}"
[ -n "$DEST" ] || { usage >&2; exit 2; }
shift

VERSION=""
STUB_BUILD=0
FAIL_AFTER=""
while [ $# -gt 0 ]; do
  case "$1" in
    --version)    VERSION="${2:?--version needs a value}"; shift 2 ;;
    --stub-build) STUB_BUILD=1; shift ;;
    --fail-after) FAIL_AFTER="${2:?--fail-after needs a value}"; shift 2 ;;
    -h|--help)    usage; exit 0 ;;
    *) echo "release.sh: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

say()  { echo "release: $*"; }
fail() { echo "release: FAIL: $*" >&2; exit 1; }

# run_dest executes a shell command at the channel root (ssh for host:path
# DESTs, local sh otherwise — the local form is what the gate script tests).
case "$DEST" in
  *:*) DEST_HOST="${DEST%%:*}"; DEST_ROOT="${DEST#*:}"
       run_dest() { ssh "$DEST_HOST" "sh -c '$*'"; } ;;
  /*)  DEST_HOST=""; DEST_ROOT="$DEST"
       run_dest() { sh -c "$*"; } ;;
  *)   fail "DEST must be host:/abs/path or a local absolute path (got: $DEST)" ;;
esac

if [ -z "$VERSION" ]; then
  # Monorepo-prefixed release tags only (`just tag` creates sesh-vX.Y.Z);
  # matches the justfile's version derivation.
  VERSION="$(cd "$MODULE_DIR" && git describe --tags --match 'sesh-v*' --always --dirty)"
fi
case "$VERSION" in
  *-dirty) fail "refusing to publish a dirty-tree build ($VERSION): commit first" ;;
  ''|*[!A-Za-z0-9._-]*) fail "unusable version string: '$VERSION'" ;;
esac

OUT_DIR="$MODULE_DIR/releases/$VERSION"
say "building $VERSION into $OUT_DIR"
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"
if [ "$STUB_BUILD" -eq 1 ]; then
  for target in darwin-arm64 darwin-amd64 linux-arm64 linux-amd64; do
    printf 'stub %s %s\n' "$VERSION" "$target" >"$OUT_DIR/sesh-$target"
  done
else
  for os in darwin linux; do
    for arch in arm64 amd64; do
      say "  go build $os/$arch"
      (cd "$MODULE_DIR" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath \
        -ldflags "-s -w -X sesh/internal/buildinfo.Version=$VERSION" \
        -o "$OUT_DIR/sesh-$os-$arch" ./cmd/sesh)
    done
  done
fi
(cd "$OUT_DIR" && sha256sum sesh-* >SHA256SUMS)
say "built and summed: $(basename "$OUT_DIR")/{$(cd "$OUT_DIR" && ls -m | tr -d ' ')}"

# Stray staging dirs from crashed publishes are cleaned by the next run.
run_dest "mkdir -p '$DEST_ROOT' && rm -rf '$DEST_ROOT'/.staging-*"

# Version dirs are immutable: republishing an existing version is an error,
# not an overwrite (checked before staging work, and again at promote).
if run_dest "test -e '$DEST_ROOT/$VERSION'"; then
  fail "$DEST_ROOT/$VERSION already exists — version dirs are immutable; bump the version"
fi

STAGING="$DEST_ROOT/.staging-$VERSION"
say "staging to $DEST${DEST_HOST:+ (ssh)}"
if [ -n "$DEST_HOST" ]; then
  rsync -a "$OUT_DIR/" "$DEST_HOST:$STAGING/"
else
  rsync -a "$OUT_DIR/" "$STAGING/"
fi
[ "$FAIL_AFTER" = "stage" ] && fail "injected failure after stage"

say "verifying staged checksums"
run_dest "cd '$STAGING' && sha256sum -c SHA256SUMS >/dev/null" ||
  fail "staged SHA256SUMS verification failed — staging left for inspection, latest untouched"
[ "$FAIL_AFTER" = "verify" ] && fail "injected failure after verify"

say "promoting $VERSION (atomic mv)"
run_dest "test ! -e '$DEST_ROOT/$VERSION' && mv '$STAGING' '$DEST_ROOT/$VERSION'" ||
  fail "$DEST_ROOT/$VERSION appeared during publish — refusing to overwrite"

say "flipping latest -> $VERSION (temp + rename + sync)"
run_dest "printf '%s\n' '$VERSION' >'$DEST_ROOT/.latest.tmp' && sync '$DEST_ROOT/.latest.tmp' && mv '$DEST_ROOT/.latest.tmp' '$DEST_ROOT/latest' && sync '$DEST_ROOT'"

say "published $VERSION"
