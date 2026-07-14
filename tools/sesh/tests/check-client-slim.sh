#!/usr/bin/env bash
# Gate the client/store binary split: the published fleet client artifact
# (./cmd/sesh, what install.sh and `sesh update` distribute) must not link
# the store-side machinery — tsnet (tailscale/wireguard/gvisor) and modernc
# sqlite — and the store-side command names must fail on it with one clear
# line naming the sesh-store binary, never a silent no-op or a flag-parse
# death. The store build (./cmd/sesh-store) is asserted to still CONTAIN the
# heavy modules, which proves the grep itself sees them (a rename or probe
# typo fails the gate instead of silently passing it).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace

# Modules that must never enter the client dependency graph. `go version -m`
# reads the module list actually linked into the artifact, so this holds
# regardless of ldflags stripping or size drift.
HEAVY_RE='tailscale\.com|modernc\.org|wireguard|gvisor\.dev'

step "client artifact links no store-side modules"
(cd "$SESH_MODULE_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' \
  -o "$BIN/sesh-client" ./cmd/sesh) || fail "client build"
go version -m "$BIN/sesh-client" >"$WORK/client-mods.txt" || fail "go version -m on client"
if grep -Eq "$HEAVY_RE" "$WORK/client-mods.txt"; then
  grep -E "$HEAVY_RE" "$WORK/client-mods.txt" >&2
  fail "fleet client artifact links store-side modules (tsnet/sqlite crept back into the client graph)"
fi
ok "client module list is free of tsnet/wireguard/gvisor/modernc"

step "store artifact still links them (probe self-check)"
(cd "$SESH_MODULE_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' \
  -o "$BIN/sesh-store" ./cmd/sesh-store) || fail "store build"
go version -m "$BIN/sesh-store" >"$WORK/store-mods.txt" || fail "go version -m on store"
for mod in 'tailscale\.com' 'modernc\.org/sqlite' 'wireguard'; do
  grep -Eq "$mod" "$WORK/store-mods.txt" ||
    fail "store build lacks $mod — the heavy-module probe no longer matches reality"
done
ok "store module list carries the heavy modules the probe watches"

step "client is materially smaller than the store build"
CLIENT_BYTES=$(stat -c %s "$BIN/sesh-client")
STORE_BYTES=$(stat -c %s "$BIN/sesh-store")
[ "$CLIENT_BYTES" -lt $((STORE_BYTES / 2)) ] ||
  fail "client ($CLIENT_BYTES bytes) is not under half the store build ($STORE_BYTES bytes)"
ok "client $CLIENT_BYTES bytes vs store $STORE_BYTES bytes"

step "store-only subcommands fail on the client with one line naming sesh-store"
for args in "serve" "serve --tsnet" "reindex" "admin drop-file claude s f --yes"; do
  set +e
  # shellcheck disable=SC2086
  OUT=$("$BIN/sesh-client" $args 2>&1)
  RC=$?
  set -e
  [ "$RC" -ne 0 ] || fail "client '$args' exited 0 — store stub must error"
  echo "$OUT" | grep -q 'sesh-store' ||
    fail "client '$args' error does not name the sesh-store binary: $OUT"
  [ "$(echo "$OUT" | grep -c 'sesh-store')" -eq 1 ] && [ "$(echo "$OUT" | wc -l)" -le 2 ] ||
    fail "client '$args' error is not the single clear line: $OUT"
done
ok "store-only commands refuse with the artifact-naming error"

step "store build still serves and the client version string is unchanged in shape"
CLIENT_VER=$("$BIN/sesh-client" version) || fail "client version"
STORE_VER=$("$BIN/sesh-store" version) || fail "store version"
[ "$CLIENT_VER" = "$STORE_VER" ] ||
  fail "version strings diverge between builds: client=$CLIENT_VER store=$STORE_VER"
ok "both builds report the same stamped version ($CLIENT_VER)"

all_green
