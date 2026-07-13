#!/usr/bin/env bash
# Release-channel publish gate (design §7): a failed or
# interrupted publish leaves the previous `latest` fully usable and no
# partial tree at a final version path; republishing an existing version is
# refused; the staged tree is checksum-verified before promotion; and the
# darwin cross-build compiles (matrix smoke — the full matrix is exercised
# by real publishes, the mechanics here run on stub artifacts).
#
# The ssh path is exercised through an ssh shim (one remote-shell parse,
# run locally), so local-mode green can never mask ssh-mode breakage; the
# malformed-VERSION regression pins the consumers' loud refusal.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

RELEASE="$SESH_MODULE_DIR/scripts/release.sh"

preflight
command -v rsync >/dev/null 2>&1 || fail "harness dependency missing: rsync"
setup_workspace
build_binaries

CHANNEL="$WORK/channel"

step "matrix smoke: darwin/arm64 cross-build compiles (CGO_ENABLED=0)"
(cd "$SESH_MODULE_DIR" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
  go build -trimpath -o "$WORK/sesh-darwin-arm64-smoke" ./cmd/sesh) ||
  fail "darwin/arm64 cross-build failed"
rm -f "$WORK/sesh-darwin-arm64-smoke"
ok "darwin/arm64 cross-build compiles"

step "publish v1: full happy path (stub artifacts, local DEST)"
bash "$RELEASE" "$CHANNEL" --version v99.0.1 --stub-build >"$WORK/pub1.out" 2>&1 ||
  fail "publish v99.0.1: $(cat "$WORK/pub1.out")"
[ -f "$CHANNEL/v99.0.1/sesh-linux-amd64" ] || fail "published tree missing artifacts"
[ -f "$CHANNEL/v99.0.1/SHA256SUMS" ] || fail "published tree missing SHA256SUMS"
[ "$(cat "$CHANNEL/latest")" = "v99.0.1" ] || fail "latest != v99.0.1: $(cat "$CHANNEL/latest")"
(cd "$CHANNEL/v99.0.1" && sha256sum -c SHA256SUMS >/dev/null) || fail "published checksums invalid"
ls "$CHANNEL" | grep -q '^\.staging-' && fail "staging dir left after success"
ok "version dir + SHA256SUMS + latest, no staging residue"

step "republish of an existing version is refused, tree untouched"
TREE_SHA=$(cd "$CHANNEL" && find v99.0.1 -type f -exec sha256sum {} \; | sort | sha256sum)
if bash "$RELEASE" "$CHANNEL" --version v99.0.1 --stub-build >"$WORK/repub.out" 2>&1; then
  fail "republish of v99.0.1 did not fail"
fi
grep -q "immutable" "$WORK/repub.out" || fail "refusal does not explain immutability: $(cat "$WORK/repub.out")"
[ "$(cd "$CHANNEL" && find v99.0.1 -type f -exec sha256sum {} \; | sort | sha256sum)" = "$TREE_SHA" ] ||
  fail "refused republish modified the published tree"
[ "$(cat "$CHANNEL/latest")" = "v99.0.1" ] || fail "refused republish moved latest"
ok "immutable version dirs: republish refused, bytes and latest untouched"

step "interrupted publish (crash after stage): previous latest usable, no final tree"
if bash "$RELEASE" "$CHANNEL" --version v99.0.2 --stub-build --fail-after stage \
  >"$WORK/crash-stage.out" 2>&1; then
  fail "--fail-after stage did not fail"
fi
[ -e "$CHANNEL/v99.0.2" ] && fail "crashed publish left a FINAL version path"
[ "$(cat "$CHANNEL/latest")" = "v99.0.1" ] || fail "crashed publish disturbed latest"
[ -d "$CHANNEL/.staging-v99.0.2" ] || fail "expected staging residue from the crash"
ok "crash after stage: latest usable, partial tree only under .staging-*"

step "retry after crash: staging residue cleaned, publish completes"
bash "$RELEASE" "$CHANNEL" --version v99.0.2 --stub-build >"$WORK/pub2.out" 2>&1 ||
  fail "retry publish v99.0.2: $(cat "$WORK/pub2.out")"
ls "$CHANNEL" | grep -q '^\.staging-' && fail "retry left staging residue"
[ "$(cat "$CHANNEL/latest")" = "v99.0.2" ] || fail "latest != v99.0.2 after retry"
ok "retry cleans stray staging and promotes atomically"

step "crash between verify and promote: no final tree, latest untouched"
# (The staged-checksum verification itself runs and passes in the happy-path
# publishes above; this injects the crash window just after it.)
if bash "$RELEASE" "$CHANNEL" --version v99.0.3 --stub-build --fail-after verify \
  >"$WORK/verify.out" 2>&1; then
  fail "--fail-after verify did not fail"
fi
[ -e "$CHANNEL/v99.0.3" ] && fail "verify-crash left a final tree"
[ "$(cat "$CHANNEL/latest")" = "v99.0.2" ] || fail "verify-crash disturbed latest"
rm -rf "$SESH_MODULE_DIR/releases/v99.0.3"
ok "injected post-verify crash: no promotion, latest untouched"

step "dirty-tree version strings are refused"
if bash "$RELEASE" "$CHANNEL" --version v1.2.3-dirty --stub-build >"$WORK/dirty.out" 2>&1; then
  fail "dirty version was accepted"
fi
grep -q "dirty" "$WORK/dirty.out" || fail "dirty refusal unexplained: $(cat "$WORK/dirty.out")"
ok "dirty-tree publish refused"

step "nonconforming version strings are refused at publish"
# Consumers (install.sh, sesh update) validate against the release shape;
# the publisher must refuse anything they would reject.
if bash "$RELEASE" "$CHANNEL" --version vBOGUS-1 --stub-build >"$WORK/shape.out" 2>&1; then
  fail "nonconforming version vBOGUS-1 was accepted"
fi
grep -q "unusable version" "$WORK/shape.out" || fail "shape refusal unexplained: $(cat "$WORK/shape.out")"
ok "publish refuses versions the fleet's consumers would reject"

step "ssh-mode publish through the ssh shim: byte-exact latest"
# The shim reproduces exactly ONE remote-shell parse over the command string
# — what real ssh does — so quoting/escaping regressions in release.sh's ssh
# path (the first live publish served latest as 'sesh-v0.1.0n' when a second
# quoting layer unquoted a printf backslash) fail here without a real
# remote. rsync rides the same shim as its transport.
SSH_SHIM="$WORK/ssh-shim"
mkdir -p "$SSH_SHIM"
cat >"$SSH_SHIM/ssh" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"$WORK/ssh-shim.log"
shift            # drop the host argument
exec sh -c "\$*"
EOF
chmod +x "$SSH_SHIM/ssh"
CHANNEL_SSH="$WORK/channel-ssh"
PATH="$SSH_SHIM:$PATH" bash "$RELEASE" "seshhost:$CHANNEL_SSH" --version v99.0.4 --stub-build \
  >"$WORK/pub-ssh.out" 2>&1 || fail "ssh-mode publish: $(cat "$WORK/pub-ssh.out")"
[ -s "$WORK/ssh-shim.log" ] || fail "publish never went through the ssh shim"
printf 'v99.0.4\n' | cmp -s - "$CHANNEL_SSH/latest" ||
  fail "ssh-mode latest is not byte-exact version+newline: [$(od -c "$CHANNEL_SSH/latest" | head -2)]"
(cd "$CHANNEL_SSH/v99.0.4" && sha256sum -c SHA256SUMS >/dev/null) ||
  fail "ssh-mode published checksums invalid"
ls "$CHANNEL_SSH" | grep -q '^\.staging-' && fail "ssh-mode publish left staging residue"
rm -rf "$SESH_MODULE_DIR/releases/v99.0.4"
ok "ssh path: byte-exact latest (version + newline), verified artifacts, no residue"

step "end-to-end: the real store serves the published channel"
rm -rf "$SESH_MODULE_DIR/releases/v99.0.1" "$SESH_MODULE_DIR/releases/v99.0.2"
cp -a "$CHANNEL" "$STORE_DIR/releases"
start_store
BASE="$STORE_URL"
[ "$(curl -fsS "$BASE/releases/latest/VERSION")" = "v99.0.2" ] ||
  fail "served latest VERSION != v99.0.2"
curl -fsS "$BASE/install.sh" >"$WORK/install-served.sh"
grep -q "BASE='$BASE'" "$WORK/install-served.sh" ||
  fail "served install.sh did not interpolate the request base"
sh -n "$WORK/install-served.sh" || fail "served install.sh does not parse as sh"
curl -fsS "$BASE/releases/v99.0.2/SHA256SUMS" >/dev/null || fail "SHA256SUMS not served"
CODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/releases/latest/sesh-linux-amd64")
[ "$CODE" = 404 ] || fail "latest asset route answered $CODE, want 404 (immutable-paths-only)"
ok "store serves VERSION/install.sh/assets from the published channel"

step "one-curl onboarding: fresh node, no repo, no toolchain (stubbed systemctl)"
# Republish the channel with a REAL linux binary so the installed node runs.
rm -rf "$STORE_DIR/releases"
mkdir -p "$STORE_DIR/releases/v99.1.1"
cp "$BIN/sesh" "$STORE_DIR/releases/v99.1.1/sesh-linux-amd64"
(cd "$STORE_DIR/releases/v99.1.1" && sha256sum sesh-linux-amd64 >SHA256SUMS)
printf 'v99.1.1\n' >"$STORE_DIR/releases/latest"

STUB_BIN="$WORK/stub-bin"
mkdir -p "$STUB_BIN"
printf '#!/usr/bin/env sh\nexit 0\n' >"$STUB_BIN/systemctl"
printf '#!/usr/bin/env sh\necho yes\n' >"$STUB_BIN/loginctl"
chmod +x "$STUB_BIN/systemctl" "$STUB_BIN/loginctl"

HOME="$HOME_DIR" PATH="$STUB_BIN:$PATH" sh -c "curl -fsSL '$BASE/install.sh' | sh" \
  >"$WORK/onboard.out" 2>&1 || fail "one-curl onboarding failed: $(cat "$WORK/onboard.out")"
INSTALLED="$HOME_DIR/.local/bin/sesh"
[ -x "$INSTALLED" ] || fail "installer did not place the binary"
cmp -s "$INSTALLED" "$BIN/sesh" || fail "installed binary differs from the published artifact"
DROPIN="$HOME_DIR/.config/systemd/user/sesh-ship.service.d/10-local.conf"
grep -q "SESH_STORE_URL=$BASE" "$DROPIN" || fail "onboarding drop-in lacks the fetched base URL"
grep -q "sesh-setup: sha256=" "$DROPIN" || fail "onboarding drop-in lacks the provenance digest"
grep -q "ExecStart=$INSTALLED ship" "$HOME_DIR/.config/systemd/user/sesh-ship.service" ||
  fail "onboarding unit does not pin the installed binary"
ok "curl | sh produced binary + pinned unit + digested drop-in with the store URL"

step "sesh update: real HTTP convergence and stable --check exit codes"
(cd "$SESH_MODULE_DIR" && go build -ldflags '-X sesh/internal/buildinfo.Version=v99.1.2' \
  -o "$STORE_DIR/releases-next-sesh" ./cmd/sesh) || fail "stamped build failed"
mkdir -p "$STORE_DIR/releases/v99.1.2"
mv "$STORE_DIR/releases-next-sesh" "$STORE_DIR/releases/v99.1.2/sesh-linux-amd64"
(cd "$STORE_DIR/releases/v99.1.2" && sha256sum sesh-linux-amd64 >SHA256SUMS)
printf 'v99.1.2\n' >"$STORE_DIR/releases/latest"

SOLO="$WORK/solo"
mkdir -p "$SOLO"
cp "$INSTALLED" "$SOLO/sesh"
set +e
HOME="$WORK/home-solo" "$SOLO/sesh" update --check --store-url "$BASE" >"$WORK/check.out" 2>&1
CHECK_EXIT=$?
set -e
[ "$CHECK_EXIT" -eq 1 ] || fail "--check with update available exited $CHECK_EXIT, want 1"
grep -q " -> v99.1.2" "$WORK/check.out" || fail "--check did not print from -> to: $(cat "$WORK/check.out")"

HOME="$WORK/home-solo" "$SOLO/sesh" update --store-url "$BASE" >"$WORK/update.out" 2>&1 ||
  fail "sesh update failed: $(cat "$WORK/update.out")"
[ "$("$SOLO/sesh" version)" = "v99.1.2" ] || fail "updated binary reports $("$SOLO/sesh" version)"
[ -f "$SOLO/sesh.prev" ] || fail "previous binary not retained as sesh.prev"
HOME="$WORK/home-solo" "$SOLO/sesh" update --check --store-url "$BASE" >"$WORK/check2.out" 2>&1 ||
  fail "--check after convergence exited nonzero: $(cat "$WORK/check2.out")"
grep -q "already up to date: v99.1.2" "$WORK/check2.out" || fail "up-to-date not reported: $(cat "$WORK/check2.out")"
ok "update converged dev -> v99.1.2 over the wire; --check exits 0/1 as documented"

step "regression: malformed latest (literal field bytes) refused by install.sh and sesh update"
# First live publish served these exact bytes (a printf format mangled in
# transit): valid charset, nonexistent version. Both consumers must fail
# loudly at the VERSION read instead of building a 404 download URL.
printf 'sesh-v0.1.0n' >"$STORE_DIR/releases/latest"
if HOME="$WORK/home-malformed" PATH="$STUB_BIN:$PATH" sh -c "curl -fsSL '$BASE/install.sh' | sh" \
  >"$WORK/mal-install.out" 2>&1; then
  fail "install.sh accepted the malformed version"
fi
grep -q "malformed version.*sesh-v0.1.0n" "$WORK/mal-install.out" ||
  fail "install.sh refusal not loud/naming the bytes: $(cat "$WORK/mal-install.out")"
grep -q "installing sesh" "$WORK/mal-install.out" &&
  fail "install.sh proceeded past the version check: $(cat "$WORK/mal-install.out")"
set +e
HOME="$WORK/home-solo" "$SOLO/sesh" update --check --store-url "$BASE" >"$WORK/mal-check.out" 2>&1
MAL_EXIT=$?
set -e
[ "$MAL_EXIT" -eq 2 ] || fail "update --check against malformed latest exited $MAL_EXIT, want 2"
grep -q "invalid version string.*sesh-v0.1.0n" "$WORK/mal-check.out" ||
  fail "update refusal not loud/naming the bytes: $(cat "$WORK/mal-check.out")"
ok "regression bytes refused loudly by both consumers; no 404 URL built"

all_green
