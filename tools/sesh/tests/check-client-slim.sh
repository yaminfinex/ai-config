#!/usr/bin/env bash
# Gate the client/store binary split: the published fleet client artifact
# (./cmd/sesh, what install.sh and `sesh update` distribute) must carry
# EXACTLY the intended module set — an allowlist, so any new dependency
# fails by default rather than hiding behind a stale denylist — and none of
# the store-side internal packages. The store-side command names must fail
# on it with one clear line naming the sesh-store binary, never a silent
# no-op or a flag-parse death. The store build (./cmd/sesh-store) is
# asserted to still CONTAIN the heavy modules, which proves the probes see
# them (a rename or probe typo fails the gate instead of silently passing
# it). Finally, the store build's updater must fail closed against the
# channel: the channel serves only client artifacts, so a store that
# self-updates lobotomizes its serve at the next restart.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace

step "client artifact carries exactly the allowlisted external modules"
(cd "$SESH_MODULE_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' \
  -o "$BIN/sesh-client" ./cmd/sesh) || fail "client build"
go version -m "$BIN/sesh-client" | awk '$1=="dep" || $1=="=>" {print $2}' \
  | sort >"$WORK/client-mods.txt" || fail "go version -m on client"
# The client's whole intended dependency universe. Growing it is a
# deliberate act: add the module here in the same change that imports it.
sort >"$WORK/client-mods.want" <<'ALLOW'
github.com/fsnotify/fsnotify
github.com/spf13/cobra
github.com/spf13/pflag
golang.org/x/sys
ALLOW
diff -u "$WORK/client-mods.want" "$WORK/client-mods.txt" >&2 ||
  fail "fleet client module list drifted from the allowlist (see diff above; a new module in the client graph must be an explicit decision)"
ok "client external modules == fsnotify, cobra, pflag, x/sys"

step "client package graph excludes the store-side internals"
(cd "$SESH_MODULE_DIR" && go list -deps ./cmd/sesh) >"$WORK/client-pkgs.txt" ||
  fail "go list -deps on client"
if grep -E '^sesh/internal/(store|index|surface|storecli|sqlitedsn)$' "$WORK/client-pkgs.txt" >&2; then
  fail "store-side internal packages entered the client graph"
fi
ok "no internal store/index/surface/storecli/sqlitedsn in the client graph"

step "store artifact still links the heavy modules (probe self-check)"
(cd "$SESH_MODULE_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' \
  -o "$BIN/sesh-store" ./cmd/sesh-store) || fail "store build"
go version -m "$BIN/sesh-store" >"$WORK/store-mods.txt" || fail "go version -m on store"
for mod in 'tailscale\.com' 'modernc\.org/sqlite' 'wireguard'; do
  grep -Eq "$mod" "$WORK/store-mods.txt" ||
    fail "store build lacks $mod — the heavy-module probe no longer matches reality"
done
(cd "$SESH_MODULE_DIR" && go list -deps ./cmd/sesh-store) | grep -q '^sesh/internal/store$' ||
  fail "store build lacks internal/store — the package probe no longer matches reality"
ok "store module and package lists carry what the probes watch"

step "client stays materially smaller than the store build, under a hard ceiling"
CLIENT_BYTES=$(stat -c %s "$BIN/sesh-client")
STORE_BYTES=$(stat -c %s "$BIN/sesh-store")
[ "$CLIENT_BYTES" -lt $((STORE_BYTES / 2)) ] ||
  fail "client ($CLIENT_BYTES bytes) is not under half the store build ($STORE_BYTES bytes)"
# Absolute drift alarm: the split landed the client at ~7.3 MB; embedded
# assets or stdlib bloat can grow it without a new module. Raising this
# ceiling is a deliberate act, like growing the allowlist.
CLIENT_CEILING=$((12 * 1024 * 1024))
[ "$CLIENT_BYTES" -le "$CLIENT_CEILING" ] ||
  fail "client ($CLIENT_BYTES bytes) exceeds the $CLIENT_CEILING-byte ceiling"
ok "client $CLIENT_BYTES bytes vs store $STORE_BYTES bytes (ceiling $CLIENT_CEILING)"

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

step "both builds report the same stamped version"
CLIENT_VER=$("$BIN/sesh-client" version) || fail "client version"
STORE_VER=$("$BIN/sesh-store" version) || fail "store version"
[ "$CLIENT_VER" = "$STORE_VER" ] ||
  fail "version strings diverge between builds: client=$CLIENT_VER store=$STORE_VER"
ok "both builds report the same stamped version ($CLIENT_VER)"

step "store build fails closed on update against a client-only channel"
# A channel presenting the store's distribution HTTP shape (what `sesh
# update` consumes): client artifacts + SHA256SUMS under /releases/<ver>/,
# the version bytes at /releases/latest/VERSION — served over real HTTP.
CHANNEL="$WORK/channel"
mkdir -p "$CHANNEL/releases/v99.0.0" "$CHANNEL/releases/latest"
cp "$BIN/sesh-client" "$CHANNEL/releases/v99.0.0/sesh-linux-amd64"
(cd "$CHANNEL/releases/v99.0.0" && sha256sum sesh-linux-amd64 >SHA256SUMS)
printf 'v99.0.0\n' >"$CHANNEL/releases/latest/VERSION"
CHANNEL_PORT=$(free_port)
python3 -m http.server "$CHANNEL_PORT" --bind 127.0.0.1 --directory "$CHANNEL" \
  >"$WORK/channel-http.log" 2>&1 &
CHANNEL_PID=$!
cleanup_channel() {
  kill "$CHANNEL_PID" 2>/dev/null || true
  wait "$CHANNEL_PID" 2>/dev/null || true
  cleanup_workspace
}
trap cleanup_channel EXIT
# Readiness probe on a non-release path (no -f: a 404 with a live TCP
# connect is "up") so the zero-requests-before-refusal grep below only ever
# sees the updater's own traffic on /releases.
channel_up() { curl -s -o /dev/null "http://127.0.0.1:$CHANNEL_PORT/gate-probe"; }
wait_for "channel HTTP server" 10 channel_up

# Run a COPY of the store binary so the byte-compare target is the running
# updater itself (the exact self-replacement the guard must forbid).
UPD_TARGET="$WORK/upd-target/sesh"
mkdir -p "$WORK/upd-target" "$WORK/upd-home"
cp "$BIN/sesh-store" "$UPD_TARGET"
set +e
HOME="$WORK/upd-home" SESH_STORE_URL="" "$UPD_TARGET" update \
  --store-url "http://127.0.0.1:$CHANNEL_PORT" >"$WORK/upd.out" 2>&1
UPD_RC=$?
set -e
[ "$UPD_RC" -eq 1 ] || fail "store-build update exited $UPD_RC, want the refusal exit 1: $(cat "$WORK/upd.out")"
grep -q 'deploy-store' "$WORK/upd.out" ||
  fail "store-build update refusal does not name deploy-store: $(cat "$WORK/upd.out")"
! grep -q 'GET /releases' "$WORK/channel-http.log" ||
  fail "store-build update touched the channel before refusing: $(cat "$WORK/channel-http.log")"
cmp -s "$UPD_TARGET" "$BIN/sesh-store" ||
  fail "store binary changed under a refused update — fail-closed is broken"

# --check stays a read-only skew probe: it must reach the channel, report
# the available client release, and still leave the binary untouched.
set +e
HOME="$WORK/upd-home" SESH_STORE_URL="" "$UPD_TARGET" update --check \
  --store-url "http://127.0.0.1:$CHANNEL_PORT" >"$WORK/upd-check.out" 2>&1
CHECK_RC=$?
set -e
[ "$CHECK_RC" -eq 0 ] || [ "$CHECK_RC" -eq 1 ] ||
  fail "store-build update --check exited $CHECK_RC, want 0 or 1: $(cat "$WORK/upd-check.out")"
grep -q 'GET /releases/latest' "$WORK/channel-http.log" ||
  fail "store-build update --check never queried the channel"
! grep -q 'refusing on the store build' "$WORK/upd-check.out" ||
  fail "--check hit the store-build refusal: $(cat "$WORK/upd-check.out")"
cmp -s "$UPD_TARGET" "$BIN/sesh-store" ||
  fail "store binary changed under --check"
ok "store update refused pre-download (target byte-identical); --check still probes skew"

all_green
