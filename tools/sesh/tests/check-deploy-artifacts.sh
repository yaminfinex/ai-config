#!/usr/bin/env bash
# U12 deploy gate — everything locally provable about the packaging
# artifacts. The field halves (reboot survival, 30-day backfill, shared-node
# uids, store migration) are runbook checklists executed at rollout; this
# script proves the artifacts those checklists rely on: unit lints, plist
# renders and parses, installer dry-run writes nothing and renders the right
# drop-in, no repo-path leaks, and the R23 stale-binary refusal (simulated
# with a real binary against a newer-generation registry).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

ETC_DIR="$SESH_MODULE_DIR/etc"

preflight
command -v systemd-analyze >/dev/null 2>&1 || fail "harness dependency missing: systemd-analyze"
setup_workspace
build_binaries

step "no repo-path assumptions in units or scripts"
if grep -rnE 'ai-config|herdr|/home/[a-z]' "$ETC_DIR"; then
  fail "etc/ artifacts reference a repo or user path (must move repos untouched)"
fi
ok "etc/ artifacts are location-independent"

step "systemd unit: required directives present"
UNIT_SRC="$ETC_DIR/systemd/sesh-ship.service"
grep -q '^Restart=on-failure$' "$UNIT_SRC" || fail "unit lacks Restart=on-failure"
grep -qE '^ExecStart=/[^ ]+ ship$' "$UNIT_SRC" || fail "unit ExecStart is not a pinned absolute path"
grep -q '^WantedBy=default.target$' "$UNIT_SRC" || fail "unit lacks [Install] WantedBy"
grep -q 'SESH_STORE_URL' "$UNIT_SRC" && grep -q '^Environment=' "$UNIT_SRC" &&
  fail "unit bakes a store URL; it must arrive via drop-in"
ok "pinned absolute ExecStart, Restart=on-failure, no baked store URL"

step "systemd unit: systemd-analyze verify (binary path swapped to a real build)"
mkdir -p "$WORK/units"
sed "s|^ExecStart=/usr/local/bin/sesh ship$|ExecStart=$BIN/sesh ship|" \
  "$UNIT_SRC" >"$WORK/units/sesh-ship.service"
systemd-analyze --user verify "$WORK/units/sesh-ship.service" 2>"$WORK/verify.err" ||
  fail "systemd-analyze verify: $(cat "$WORK/verify.err")"
[ -s "$WORK/verify.err" ] && fail "systemd-analyze verify warnings: $(cat "$WORK/verify.err")"
ok "unit verifies clean"

step "launchd template: tokens complete, render parses as a plist"
TMPL="$ETC_DIR/launchd/dev.sesh.ship.plist.tmpl"
RENDERED="$WORK/dev.sesh.ship.plist"
sed \
  -e "s|@SESH_BIN@|$BIN/sesh|g" \
  -e "s|@SESH_STORE_URL@|http://sesh-store.example.ts.net:8765|g" \
  -e "s|@HOME@|$HOME_DIR|g" \
  "$TMPL" >"$RENDERED"
grep -q '@' "$RENDERED" && fail "unrendered @TOKEN@ left in plist: $(grep -o '@[A-Z_]*@' "$RENDERED" | sort -u)"
python3 - "$RENDERED" <<'PY' || fail "rendered plist does not parse"
import plistlib, sys
with open(sys.argv[1], "rb") as f:
    p = plistlib.load(f)
assert p["Label"] == "dev.sesh.ship", p["Label"]
assert p["ProgramArguments"][1] == "ship", p["ProgramArguments"]
assert p["EnvironmentVariables"]["SESH_STORE_URL"].startswith("http://"), p["EnvironmentVariables"]
assert p["KeepAlive"] == {"SuccessfulExit": False}, p["KeepAlive"]
PY
ok "template renders to a valid launchd plist with restart-on-failure semantics"

step "installer dry-run: correct drop-in rendered, nothing written"
DRY_OUT="$WORK/dry-run.out"
HOME="$HOME_DIR" bash "$ETC_DIR/install-ship.sh" --dry-run \
  --store-url http://sesh-store.example.ts.net:8765 \
  --binary "$BIN/sesh" >"$DRY_OUT" 2>&1 ||
  fail "install-ship.sh --dry-run exited nonzero: $(cat "$DRY_OUT")"
grep -q "Environment=SESH_STORE_URL=http://sesh-store.example.ts.net:8765" "$DRY_OUT" ||
  fail "dry-run drop-in lacks the store URL"
grep -q "ExecStart=$BIN/sesh ship" "$DRY_OUT" ||
  fail "dry-run drop-in lacks the ExecStart override for a non-default binary path"
[ -e "$HOME_DIR/.config" ] && fail "dry-run created files under HOME"
ok "dry-run renders drop-in (URL + ExecStart override) and writes nothing"

step "installer argument validation"
HOME="$HOME_DIR" bash "$ETC_DIR/install-ship.sh" --dry-run --binary relative/sesh \
  --store-url https://x >/dev/null 2>&1 && fail "installer accepted a relative --binary"
HOME="$HOME_DIR" bash "$ETC_DIR/install-ship.sh" --dry-run >/dev/null 2>&1 &&
  fail "installer accepted a missing --store-url"
ok "installer refuses relative binary paths and missing store URL"

step "runbook: no https against the tsnet ingest listener, no malformed DENY probe"
# tsnet mode is plain HTTP over WireGuard; an https:// store URL fails at
# transport and a non-UUID probe path 400s before the grant check runs.
if grep -n ':8765' "$SESH_MODULE_DIR/README.md" | grep 'https://'; then
  fail "README prescribes https:// against the tsnet ingest listener"
fi
if grep -nE '/v1/files/[a-z]+/[^$][^/]*/[^$"]*"?$' "$SESH_MODULE_DIR/README.md" |
  grep -vE '\$[A-Z]+|[0-9a-f]{8}-[0-9a-f]{4}'; then
  fail "README DENY probe uses non-UUID path segments (400s before the grant check)"
fi
ok "runbook URLs are tsnet-correct and the DENY probe reaches the grant check"

step "installer preserves operator-edited drop-ins (refuse without --force)"
DROPIN_DIR="$HOME_DIR/.config/systemd/user/sesh-ship.service.d"
mkdir -p "$DROPIN_DIR"
printf '# operator-tuned\n[Service]\nEnvironment=SESH_STORE_URL=http://operator.example:1\n' \
  >"$DROPIN_DIR/10-local.conf"
DROPIN_SHA=$(sha256sum "$DROPIN_DIR/10-local.conf" | cut -d' ' -f1)
if HOME="$HOME_DIR" bash "$ETC_DIR/install-ship.sh" --dry-run \
  --store-url http://sesh-store.example.ts.net:8765 --binary "$BIN/sesh" \
  >"$WORK/clobber.out" 2>&1; then
  fail "installer did not refuse a differing existing drop-in without --force"
fi
grep -q "refusing to overwrite" "$WORK/clobber.out" || fail "refusal message missing: $(cat "$WORK/clobber.out")"
[ "$(sha256sum "$DROPIN_DIR/10-local.conf" | cut -d' ' -f1)" = "$DROPIN_SHA" ] ||
  fail "refusal path modified the operator drop-in"
HOME="$HOME_DIR" bash "$ETC_DIR/install-ship.sh" --dry-run --force \
  --store-url http://sesh-store.example.ts.net:8765 --binary "$BIN/sesh" \
  >/dev/null 2>&1 || fail "--force did not override the drop-in refusal"
rm -rf "$HOME_DIR/.config"
ok "differing drop-in refused untouched; --force overrides"

step "R23: stale binary vs newer registry refuses cleanly (simulated in the field shape)"
R23_STATE="$WORK/r23-state"
mkdir -p "$R23_STATE"
printf '{"schema_generation": 99, "cursors": {}}\n' >"$R23_STATE/cursors.json"
REG_SHA=$(sha256sum "$R23_STATE/cursors.json" | cut -d' ' -f1)
set +e
HOME="$HOME_DIR" SESH_STATE_DIR="$R23_STATE" SESH_STORE_URL="http://127.0.0.1:9" \
  "$BIN/sesh" ship >"$WORK/r23.out" 2>&1
R23_EXIT=$?
set -e
[ "$R23_EXIT" -ne 0 ] || fail "stale-binary shipper exited 0 against a newer registry"
grep -q "carries schema generation 99" "$WORK/r23.out" || fail "refusal does not name the generations: $(cat "$WORK/r23.out")"
grep -q "left untouched" "$WORK/r23.out" || fail "refusal does not promise the registry is untouched"
grep -q "Remedy" "$WORK/r23.out" || fail "refusal does not carry the operator remedy"
[ "$(sha256sum "$R23_STATE/cursors.json" | cut -d' ' -f1)" = "$REG_SHA" ] ||
  fail "refusal modified the registry file"
ok "clean refusal: nonzero exit, remedy named, registry byte-identical (matches runbook signature)"

all_green
