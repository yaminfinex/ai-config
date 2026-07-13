#!/usr/bin/env bash
# U12 deploy gate — everything locally provable about the packaging
# artifacts. The field halves (reboot survival, 30-day backfill, shared-node
# uids, store migration) are runbook checklists executed at rollout; this
# script proves the artifacts those checklists rely on: unit lints, plist
# renders and parses, `sesh setup` dry-run writes nothing and renders the
# right drop-in, the DP-4b provenance rules on real writes, no repo-path
# leaks, and the R23 stale-binary refusal (simulated with a real binary
# against a newer-generation registry).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

ETC_DIR="$SESH_MODULE_DIR/etc"
TEMPLATES_DIR="$SESH_MODULE_DIR/internal/setup/templates"
STORE_URL_A="http://sesh.example.ts.net:8765"
STORE_URL_B="http://sesh-elsewhere.example.ts.net:8765"

preflight
command -v systemd-analyze >/dev/null 2>&1 || fail "harness dependency missing: systemd-analyze"
setup_workspace
build_binaries

step "no repo-path assumptions in templates or scripts"
if grep -rnE 'ai-config|herdr|/home/[a-z]' "$ETC_DIR" "$TEMPLATES_DIR"; then
  fail "packaging artifacts reference a repo or user path (must move repos untouched)"
fi
ok "etc/ and embedded templates are location-independent"

step "systemd unit template: required directives present"
UNIT_SRC="$TEMPLATES_DIR/sesh-ship.service"
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

step "launchd template: tokens complete, render parses, digest comment tolerated"
TMPL="$TEMPLATES_DIR/dev.sesh.ship.plist.tmpl"
RENDERED="$WORK/dev.sesh.ship.plist"
sed \
  -e "s|@SESH_BIN@|$BIN/sesh|g" \
  -e "s|@SESH_STORE_URL@|$STORE_URL_A|g" \
  -e "s|@HOME@|$HOME_DIR|g" \
  "$TMPL" >"$RENDERED"
grep -q '@' "$RENDERED" && fail "unrendered @TOKEN@ left in plist: $(grep -o '@[A-Z_]*@' "$RENDERED" | sort -u)"
# sesh setup stamps the plist with a trailing XML comment (DP-4b); prove the
# plist parser tolerates it exactly as written.
printf '<!-- sesh-setup: sha256=%064d -->\n' 0 >>"$RENDERED"
python3 - "$RENDERED" <<'PY' || fail "rendered plist (with digest comment) does not parse"
import plistlib, sys
with open(sys.argv[1], "rb") as f:
    p = plistlib.load(f)
assert p["Label"] == "dev.sesh.ship", p["Label"]
assert p["ProgramArguments"][1] == "ship", p["ProgramArguments"]
assert p["EnvironmentVariables"]["SESH_STORE_URL"].startswith("http://"), p["EnvironmentVariables"]
assert p["KeepAlive"] == {"SuccessfulExit": False}, p["KeepAlive"]
PY
ok "template renders to a valid launchd plist; trailing digest comment parses"

step "sesh setup dry-run: correct unit and drop-in rendered, nothing written"
DRY_OUT="$WORK/dry-run.out"
HOME="$HOME_DIR" "$BIN/sesh" setup --dry-run --store-url "$STORE_URL_A" >"$DRY_OUT" 2>&1 ||
  fail "sesh setup --dry-run exited nonzero: $(cat "$DRY_OUT")"
grep -q "Environment=SESH_STORE_URL=$STORE_URL_A" "$DRY_OUT" ||
  fail "dry-run drop-in lacks the store URL"
grep -q "ExecStart=$BIN/sesh ship" "$DRY_OUT" ||
  fail "dry-run unit lacks the pinned absolute path of the running binary"
grep -q "sesh-setup: sha256=" "$DRY_OUT" ||
  fail "dry-run drop-in lacks the DP-4b provenance digest"
[ -e "$HOME_DIR/.config" ] && fail "dry-run created files under HOME"
ok "dry-run pins os.Executable, renders the URL drop-in + digest, writes nothing"

step "sesh setup argument validation and no-sudo"
HOME="$HOME_DIR" "$BIN/sesh" setup --dry-run >/dev/null 2>&1 &&
  fail "setup accepted a missing --store-url"
grep -rnE '(^|[^a-z])sudo([^a-z]|$)' "$SESH_MODULE_DIR/internal/setup"/*.go &&
  fail "setup invokes sudo"
ok "setup refuses a missing store URL and uses no sudo"

step "DP-4b on real writes (stubbed systemctl): intact digest migrates, edits refuse"
STUB_BIN="$WORK/stub-bin"
mkdir -p "$STUB_BIN"
printf '#!/usr/bin/env sh\nexit 0\n' >"$STUB_BIN/systemctl"
printf '#!/usr/bin/env sh\necho yes\n' >"$STUB_BIN/loginctl"
chmod +x "$STUB_BIN/systemctl" "$STUB_BIN/loginctl"
DROPIN="$HOME_DIR/.config/systemd/user/sesh-ship.service.d/10-local.conf"

HOME="$HOME_DIR" PATH="$STUB_BIN:$PATH" "$BIN/sesh" setup --store-url "$STORE_URL_A" \
  >"$WORK/setup-a.out" 2>&1 || fail "first real setup failed: $(cat "$WORK/setup-a.out")"
grep -q "SESH_STORE_URL=$STORE_URL_A" "$DROPIN" || fail "drop-in missing after install"
grep -q "sesh-setup: sha256=" "$DROPIN" || fail "written drop-in lacks provenance digest"

# One-command URL migration: digest intact → new explicit URL replaces, no --force.
HOME="$HOME_DIR" PATH="$STUB_BIN:$PATH" "$BIN/sesh" setup --store-url "$STORE_URL_B" \
  >"$WORK/setup-b.out" 2>&1 || fail "digest-intact URL migration refused: $(cat "$WORK/setup-b.out")"
grep -q "SESH_STORE_URL=$STORE_URL_B" "$DROPIN" || fail "URL migration did not rewrite the drop-in"

# Operator edit (URL-only — shape equality could not catch this) → refuse untouched.
sed -i "s|$STORE_URL_B|$STORE_URL_A|" "$DROPIN"
DROPIN_SHA=$(sha256sum "$DROPIN" | cut -d' ' -f1)
if HOME="$HOME_DIR" PATH="$STUB_BIN:$PATH" "$BIN/sesh" setup --store-url "$STORE_URL_B" \
  >"$WORK/clobber.out" 2>&1; then
  fail "setup did not refuse an operator-edited drop-in without --force"
fi
grep -q "refusing to overwrite" "$WORK/clobber.out" || fail "refusal message missing: $(cat "$WORK/clobber.out")"
[ "$(sha256sum "$DROPIN" | cut -d' ' -f1)" = "$DROPIN_SHA" ] ||
  fail "refusal path modified the operator drop-in"
HOME="$HOME_DIR" PATH="$STUB_BIN:$PATH" "$BIN/sesh" setup --force --store-url "$STORE_URL_B" \
  >/dev/null 2>&1 || fail "--force did not override the drop-in refusal"
grep -q "SESH_STORE_URL=$STORE_URL_B" "$DROPIN" || fail "--force did not rewrite the URL"
rm -rf "$HOME_DIR/.config"
ok "digest-intact drop-in migrates URL without --force; operator edit refused untouched; --force overrides"

step "install-ship.sh is a deprecation pointer"
if HOME="$HOME_DIR" bash "$ETC_DIR/install-ship.sh" --store-url "$STORE_URL_A" \
  >"$WORK/deprecated.out" 2>&1; then
  fail "deprecated install-ship.sh still exits 0"
fi
grep -q "sesh setup" "$WORK/deprecated.out" || fail "deprecation message does not name sesh setup"
[ -e "$HOME_DIR/.config" ] && fail "deprecated installer wrote files"
ok "install-ship.sh refuses and points at sesh setup"

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
