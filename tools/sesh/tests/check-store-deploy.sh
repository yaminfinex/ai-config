#!/usr/bin/env bash
# Store-deployment gate — everything locally provable about the quick-host
# deployment artifacts (ops/ + the deploy-store/tag/release recipes). The
# halves needing a real VM/tailnet (tsnet join, deny-verify pair, GCS write,
# real restore drill) are the owner runbook in ops/README.md; this script
# proves the artifacts that runbook relies on: bootstrap idempotence via its
# SESH_OPS_ROOT seam (second run is a no-op), the crash-safe binary swap in
# deploy-remote.sh (prev retained, target never missing, junk refused,
# running-image version reported), backup.sh's recoverable ordering (db
# snapshot never newer than the uploaded mirror, live sqlite never copied),
# unit lints, and the justfile recipes against a stubbed gcloud.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

OPS_SRC="$SESH_MODULE_DIR/ops"

preflight
for dep in systemd-analyze just; do
  command -v "$dep" >/dev/null 2>&1 || fail "harness dependency missing: $dep"
done
setup_workspace
build_binaries

step "no repo-path assumptions anywhere in ops/"
if grep -rnE 'ai-config|herdr|/home/[a-z]' "$OPS_SRC"; then
  fail "ops/ references a repo or user path (must run from any copy location)"
fi
ok "ops/ is location-independent"

step "unit lints: required directives"
SERVE_UNIT="$OPS_SRC/systemd/sesh-serve.service"
grep -q '^User=sesh$' "$SERVE_UNIT" || fail "sesh-serve lacks the dedicated user"
grep -qE '^ExecStart=/usr/local/bin/sesh serve --tsnet --tsnet-hostname sesh .*--data-dir /var/lib/sesh$' "$SERVE_UNIT" ||
  fail "sesh-serve ExecStart is not the tsnet serve invocation on /var/lib/sesh"
grep -q '^Restart=always$' "$SERVE_UNIT" || fail "sesh-serve lacks Restart=always"
grep -q '^EnvironmentFile=-/etc/sesh/serve.env$' "$SERVE_UNIT" ||
  fail "sesh-serve lacks the optional TS_AUTHKEY env file"
grep -q '^TimeoutStopSec=' "$SERVE_UNIT" || fail "sesh-serve lacks a SIGTERM drain window"
grep -q '^WantedBy=multi-user.target$' "$SERVE_UNIT" || fail "sesh-serve lacks [Install]"
grep -q '^User=sesh$' "$OPS_SRC/systemd/sesh-backup.service" || fail "backup service not sesh-owned"
grep -q '^Type=oneshot$' "$OPS_SRC/systemd/sesh-backup.service" || fail "backup service not oneshot"
grep -q '^OnCalendar=\*:0/15$' "$OPS_SRC/systemd/sesh-backup.timer" || fail "backup timer is not 15-minute"
grep -q '^Persistent=true$' "$OPS_SRC/systemd/sesh-backup.timer" || fail "backup timer lacks Persistent=true"
ok "dedicated user, tsnet serve line, drain window, 15-min persistent timer"

step "systemd-analyze verify (binary paths and user swapped to local equivalents)"
mkdir -p "$WORK/units"
cp "$OPS_SRC/backup.sh" "$WORK/units/sesh-backup-exec.sh"
chmod 755 "$WORK/units/sesh-backup-exec.sh"
sed -e "s|^ExecStart=/usr/local/bin/sesh |ExecStart=$BIN/sesh |" \
    -e "s|^User=sesh$|User=$(id -un)|" \
  "$SERVE_UNIT" >"$WORK/units/sesh-serve.service"
sed -e "s|^ExecStart=/usr/local/bin/sesh-backup.sh$|ExecStart=$WORK/units/sesh-backup-exec.sh|" \
    -e "s|^User=sesh$|User=$(id -un)|" \
  "$OPS_SRC/systemd/sesh-backup.service" >"$WORK/units/sesh-backup.service"
cp "$OPS_SRC/systemd/sesh-backup.timer" "$WORK/units/sesh-backup.timer"
systemd-analyze verify "$WORK/units/sesh-serve.service" "$WORK/units/sesh-backup.service" \
  "$WORK/units/sesh-backup.timer" 2>"$WORK/verify.err" ||
  fail "systemd-analyze verify: $(cat "$WORK/verify.err")"
[ -s "$WORK/verify.err" ] && fail "systemd-analyze verify warnings: $(cat "$WORK/verify.err")"
ok "all three units verify clean"

# --- stubbed host commands ----------------------------------------------------
STUBS="$WORK/stubs"
STUB_STATE="$WORK/stub-state"
mkdir -p "$STUBS" "$STUB_STATE"
export STUB_STATE
cat >"$STUBS/id" <<'EOF'
#!/usr/bin/env sh
case "${1:-}" in
  sesh) [ -e "$STUB_STATE/user-created" ] ;;
  *) exec /usr/bin/id "$@" ;;
esac
EOF
cat >"$STUBS/useradd" <<'EOF'
#!/usr/bin/env sh
touch "$STUB_STATE/user-created"
echo "useradd $*" >>"$STUB_STATE/useradd.log"
EOF
cat >"$STUBS/systemctl" <<'EOF'
#!/usr/bin/env sh
echo "$*" >>"$STUB_STATE/systemctl.log"
EOF
cat >"$STUBS/gcloud" <<'EOF'
#!/usr/bin/env sh
echo "$*" >>"$STUB_STATE/gcloud.log"
EOF
# sqlite3 CLI shim over python3's bindings (the REAL sqlite engine executes
# the statement — VACUUM INTO included); the gate machine may lack the CLI
# that bootstrap installs on the VM. Single statements only, like backup.sh.
cat >"$STUBS/sqlite3" <<'EOF'
#!/usr/bin/env sh
exec python3 -c '
import sqlite3, sys
con = sqlite3.connect(sys.argv[1], isolation_level=None)
for row in con.execute(sys.argv[2]):
    print("|".join(str(c) for c in row))
con.close()
' "$@"
EOF
chmod 755 "$STUBS"/*

# Bootstrap runs from a COPY of ops/ (proves nothing depends on the repo
# location), against a prefixed fake root via the SESH_OPS_ROOT seam.
OPS="$WORK/ops-copy"
cp -a "$OPS_SRC" "$OPS"
VMROOT="$WORK/vmroot"
run_bootstrap() { # [env VAR=... via caller] -> $WORK/bootstrap.out, global BOOT_EXIT
  set +e
  SESH_OPS_ROOT="$VMROOT" PATH="$STUBS:$PATH" "$@" sh "$OPS/bootstrap.sh" \
    >"$WORK/bootstrap.out" 2>&1
  BOOT_EXIT=$?
  set -e
}
tree_sha() { (cd "$1" && find . -type f -exec sha256sum {} \; | sort | sha256sum); }

step "bootstrap: first run without an auth key refuses with the remedy"
run_bootstrap env -u TS_AUTHKEY
[ "$BOOT_EXIT" -ne 0 ] || fail "keyless first bootstrap exited 0"
grep -q 'TS_AUTHKEY' "$WORK/bootstrap.out" || fail "refusal does not name TS_AUTHKEY: $(cat "$WORK/bootstrap.out")"
[ -e "$VMROOT/etc/systemd/system/sesh-serve.service" ] && fail "refused bootstrap still installed units"
ok "keyless bring-up refused before any install"

step "bootstrap: first run creates user, dirs, key handoff, units; serve stays stopped"
run_bootstrap env TS_AUTHKEY=tskey-auth-test123
[ "$BOOT_EXIT" -eq 0 ] || fail "first bootstrap failed: $(cat "$WORK/bootstrap.out")"
[ "$(wc -l <"$STUB_STATE/useradd.log")" = 1 ] || fail "useradd not invoked exactly once"
[ "$(stat -c %a "$VMROOT/var/lib/sesh")" = 750 ] || fail "data dir mode != 750"
[ "$(stat -c %a "$VMROOT/var/lib/sesh/tsnet")" = 700 ] || fail "tsnet dir mode != 700"
[ "$(stat -c %a "$VMROOT/var/lib/sesh/releases")" = 2775 ] || fail "releases dir not group-writable+setgid"
[ -d "$VMROOT/var/lib/sesh/backup/db" ] || fail "backup dir missing"
[ "$(stat -c %a "$VMROOT/etc/sesh/serve.env")" = 600 ] || fail "serve.env not 0600"
grep -q '^TS_AUTHKEY=tskey-auth-test123$' "$VMROOT/etc/sesh/serve.env" || fail "key not handed off to serve.env"
for f in sesh-serve.service sesh-backup.service sesh-backup.timer; do
  cmp -s "$OPS/systemd/$f" "$VMROOT/etc/systemd/system/$f" || fail "$f not installed verbatim"
done
cmp -s "$OPS/backup.sh" "$VMROOT/usr/local/bin/sesh-backup.sh" || fail "backup script not installed"
grep -q 'daemon-reload' "$STUB_STATE/systemctl.log" || fail "no daemon-reload after unit install"
grep -q 'enable sesh-serve.service sesh-backup.timer' "$STUB_STATE/systemctl.log" || fail "units not enabled"
grep -q 'start sesh-backup.timer' "$STUB_STATE/systemctl.log" || fail "backup timer not started"
grep -q 'restart' "$STUB_STATE/systemctl.log" && fail "bootstrap restarted serve with no binary present"
grep -q 'deploy-store' "$WORK/bootstrap.out" || fail "missing-binary NOTE does not point at deploy-store"
ok "user + dirs + 0600 key file + units enabled; serve deferred to deploy-store"

step "bootstrap: second run is a no-op (no rewrite, no reload, no restart)"
BEFORE=$(tree_sha "$VMROOT")
: >"$STUB_STATE/systemctl.log"
run_bootstrap env -u TS_AUTHKEY
[ "$BOOT_EXIT" -eq 0 ] || fail "second bootstrap failed: $(cat "$WORK/bootstrap.out")"
[ "$(tree_sha "$VMROOT")" = "$BEFORE" ] || fail "second run modified installed files"
grep -qE 'daemon-reload|restart' "$STUB_STATE/systemctl.log" && fail "second run reloaded or restarted"
[ "$(wc -l <"$STUB_STATE/useradd.log")" = 1 ] || fail "second run re-created the user"
grep -q 'bootstrap.sh: installed' "$WORK/bootstrap.out" && fail "second run claims to have installed files"
ok "byte-identical tree, no reload, no restart, no useradd"

step "bootstrap: binary present + nothing changed -> start (not restart)"
cp "$BIN/sesh" "$VMROOT/usr/local/bin/sesh"
: >"$STUB_STATE/systemctl.log"
run_bootstrap env -u TS_AUTHKEY
[ "$BOOT_EXIT" -eq 0 ] || fail "bootstrap with binary failed: $(cat "$WORK/bootstrap.out")"
grep -q 'start sesh-serve.service' "$STUB_STATE/systemctl.log" || fail "serve not started"
grep -q 'restart' "$STUB_STATE/systemctl.log" && fail "unchanged re-run restarted serve"
ok "idempotent start once the binary exists"

step "bootstrap: changed unit -> reinstall + daemon-reload + restart"
printf '# drift marker\n' >>"$OPS/systemd/sesh-serve.service"
: >"$STUB_STATE/systemctl.log"
run_bootstrap env -u TS_AUTHKEY
[ "$BOOT_EXIT" -eq 0 ] || fail "bootstrap after unit change failed: $(cat "$WORK/bootstrap.out")"
cmp -s "$OPS/systemd/sesh-serve.service" "$VMROOT/etc/systemd/system/sesh-serve.service" ||
  fail "changed unit not reinstalled"
grep -q 'daemon-reload' "$STUB_STATE/systemctl.log" || fail "no daemon-reload after unit change"
grep -q 'restart sesh-serve.service' "$STUB_STATE/systemctl.log" || fail "no restart after unit change"
ok "unit drift converged with reload + restart"

step "bootstrap: joined node (tsnet state present) needs no key"
rm -f "$VMROOT/etc/sesh/serve.env"
touch "$VMROOT/var/lib/sesh/tsnet/tailscaled.state"
run_bootstrap env -u TS_AUTHKEY
[ "$BOOT_EXIT" -eq 0 ] || fail "re-run on a joined node demanded a key: $(cat "$WORK/bootstrap.out")"
ok "existing tsnet identity satisfies the key precondition"

step "bootstrap: pre-seeded permissive serve.env converged to 0600, content preserved"
# The documented operator alternative to the TS_AUTHKEY env var is placing
# the key in serve.env beforehand — under a normal umask that file lands
# 0644, world-readable. Accepting it must tighten it before install/start.
SEEDROOT="$WORK/vmroot-seed"
mkdir -p "$SEEDROOT/etc/sesh"
printf 'TS_AUTHKEY=tskey-auth-preseeded\n' >"$SEEDROOT/etc/sesh/serve.env"
chmod 644 "$SEEDROOT/etc/sesh/serve.env"
MAIN_VMROOT="$VMROOT"
VMROOT="$SEEDROOT"
run_bootstrap env -u TS_AUTHKEY
VMROOT="$MAIN_VMROOT"
[ "$BOOT_EXIT" -eq 0 ] || fail "pre-seeded bootstrap failed: $(cat "$WORK/bootstrap.out")"
[ "$(stat -c %a "$SEEDROOT/etc/sesh/serve.env")" = 600 ] ||
  fail "pre-seeded serve.env left $(stat -c %a "$SEEDROOT/etc/sesh/serve.env"), want 600"
grep -q '^TS_AUTHKEY=tskey-auth-preseeded$' "$SEEDROOT/etc/sesh/serve.env" ||
  fail "permission convergence altered the key content"
[ -f "$SEEDROOT/etc/systemd/system/sesh-serve.service" ] || fail "pre-seeded bring-up did not complete"
ok "operator pre-seed accepted; key file tightened to 0600 with content intact"

# --- deploy-remote.sh: crash-safe swap ------------------------------------------
step "deploy-remote: fresh install, upgrade retains sesh.prev, running image reported"
DR_ROOT="$WORK/drroot"
DR_STATE="$WORK/dr-state"
DR_DATA="$WORK/dr-data"
DR_STUBS="$WORK/dr-stubs"
mkdir -p "$DR_ROOT/usr/local/bin" "$DR_STATE" "$DR_DATA" "$DR_STUBS"
export DR_STATE DR_DATA
export DR_TARGET="$DR_ROOT/usr/local/bin/sesh"
export DR_PORT DR_PORT2
DR_PORT=$(free_port)
DR_PORT2=$(free_port)
cat >"$DR_STUBS/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "$*" >>"$DR_STATE/systemctl.log"
case "${1:-}" in
  restart)
    [ "${DR_FAIL_RESTART:-0}" = 1 ] && exit 1
    if [ -f "$DR_STATE/pid" ]; then kill "$(cat "$DR_STATE/pid")" 2>/dev/null || true; fi
    "$DR_TARGET" serve --addr "127.0.0.1:$DR_PORT" --surface-addr "127.0.0.1:$DR_PORT2" \
      --data-dir "$DR_DATA" >/dev/null 2>&1 &
    echo $! >"$DR_STATE/pid"
    ;;
  show) cat "$DR_STATE/pid" ;;
esac
exit 0
EOF
chmod 755 "$DR_STUBS/systemctl"
(cd "$SESH_MODULE_DIR" && go build -ldflags '-X sesh/internal/buildinfo.Version=vDEPLOY-1' \
  -o "$WORK/upload-1" ./cmd/sesh-store) || fail "stamped build 1"
(cd "$SESH_MODULE_DIR" && go build -ldflags '-X sesh/internal/buildinfo.Version=vDEPLOY-2' \
  -o "$WORK/upload-2" ./cmd/sesh-store) || fail "stamped build 2"
run_dr() { SESH_OPS_ROOT="$DR_ROOT" PATH="$DR_STUBS:$PATH" sh "$OPS_SRC/deploy-remote.sh" "$@"; }

run_dr "$WORK/upload-1" >"$WORK/dr1.out" 2>&1 || fail "fresh deploy failed: $(cat "$WORK/dr1.out")"
grep -q 'store now: vDEPLOY-1' "$WORK/dr1.out" || fail "running-image version not reported: $(cat "$WORK/dr1.out")"
[ -e "$DR_TARGET.prev" ] && fail "fresh install invented a sesh.prev"

run_dr "$WORK/upload-2" >"$WORK/dr2.out" 2>&1 || fail "upgrade deploy failed: $(cat "$WORK/dr2.out")"
grep -q 'store now: vDEPLOY-2' "$WORK/dr2.out" || fail "upgrade did not report the new running image"
cmp -s "$DR_TARGET" "$WORK/upload-2" || fail "target is not the uploaded binary"
cmp -s "$DR_TARGET.prev" "$WORK/upload-1" || fail "sesh.prev is not the previous binary"
[ -e "$DR_TARGET.next" ] && fail "staging residue left after success"
ok "fresh install + upgrade; prev retained via hardlink; RUNNING version printed"

step "deploy-remote: junk upload refused, known-good target untouched"
printf 'definitely not an executable\n' >"$WORK/junk"
TARGET_SHA=$(sha256sum "$DR_TARGET" | cut -d' ' -f1)
: >"$DR_STATE/systemctl.log"
if run_dr "$WORK/junk" >"$WORK/dr-junk.out" 2>&1; then
  fail "junk upload was deployed"
fi
grep -q 'target untouched' "$WORK/dr-junk.out" || fail "refusal message wrong: $(cat "$WORK/dr-junk.out")"
[ "$(sha256sum "$DR_TARGET" | cut -d' ' -f1)" = "$TARGET_SHA" ] || fail "junk refusal modified the target"
[ -e "$DR_TARGET.next" ] && fail "junk refusal left staging residue"
grep -q 'restart' "$DR_STATE/systemctl.log" && fail "junk refusal still restarted the service"
ok "non-executing upload refused before the swap; no restart"

step "deploy-remote: restart failure is reported as failure (binary already forward)"
if DR_FAIL_RESTART=1 run_dr "$WORK/upload-1" >"$WORK/dr-fail.out" 2>&1; then
  fail "failed restart exited 0"
fi
grep -q 'sesh.prev' "$WORK/dr-fail.out" || fail "failure does not name the retained previous binary"
cmp -s "$DR_TARGET" "$WORK/upload-1" || fail "swap did not complete before the failed restart"
ok "honest failed-but-forward reporting"
kill "$(cat "$DR_STATE/pid")" 2>/dev/null || true

# --- just recipes ---------------------------------------------------------------
step "just deploy-store: build + IAP ship against a stubbed gcloud"
: >"$STUB_STATE/gcloud.log" 2>/dev/null || true
(cd "$SESH_MODULE_DIR" && PATH="$STUBS:$PATH" just deploy-store >"$WORK/ds.out" 2>&1) ||
  fail "just deploy-store failed: $(cat "$WORK/ds.out")"
[ -f /tmp/sesh-linux-amd64 ] || fail "linux/amd64 build not produced"
SCP_LINE=$(grep '^compute scp' "$STUB_STATE/gcloud.log" || true)
SSH_LINE=$(grep '^compute ssh' "$STUB_STATE/gcloud.log" || true)
[ -n "$SCP_LINE" ] || fail "no gcloud scp invocation recorded"
[ -n "$SSH_LINE" ] || fail "no gcloud ssh invocation recorded"
echo "$SCP_LINE" | grep -q -- '--tunnel-through-iap' || fail "scp does not tunnel through IAP"
echo "$SCP_LINE" | grep -q '/tmp/sesh-linux-amd64' || fail "scp does not ship the built binary"
echo "$SCP_LINE" | grep -q 'deploy-remote.sh' || fail "scp does not ship deploy-remote.sh"
echo "$SCP_LINE" | grep -q 'quick-host:/tmp/' || fail "scp target is not the VM tmp dir"
echo "$SSH_LINE" | grep -q -- '--tunnel-through-iap' || fail "ssh does not tunnel through IAP"
echo "$SSH_LINE" | grep -q 'sudo sh /tmp/deploy-remote.sh /tmp/sesh-linux-amd64' ||
  fail "ssh does not run deploy-remote.sh on the uploaded binary"
ok "deploy-store builds static linux/amd64 and drives deploy-remote.sh over IAP"

step "just tag / release: monorepo prefix, validation, default dest"
TAG_OUT=$(cd "$SESH_MODULE_DIR" && just -n tag 1.2.3 2>&1)
echo "$TAG_OUT" | grep -q "git tag 'sesh-v1.2.3'" || fail "tag recipe does not create sesh-v1.2.3: $TAG_OUT"
if (cd "$SESH_MODULE_DIR" && just tag v1.2.3 >"$WORK/tagbad.out" 2>&1); then
  fail "tag accepted a leading-v argument (would double the prefix)"
fi
grep -q 'sesh-vX.Y.Z' "$WORK/tagbad.out" || fail "tag rejection lacks usage: $(cat "$WORK/tagbad.out")"
REL_OUT=$(cd "$SESH_MODULE_DIR" && just -n release 2>&1)
echo "$REL_OUT" | grep -q 'release.sh sesh-host:/var/lib/sesh/releases' ||
  fail "release default dest missing: $REL_OUT"
VM_OUT=$(cd "$SESH_MODULE_DIR" && SESH_STORE_VM=other-vm just -n deploy-store 2>&1)
echo "$VM_OUT" | grep -q 'other-vm:/tmp/' || fail "SESH_STORE_VM override ignored: $VM_OUT"
ok "sesh-v prefix enforced, bad input refused, default dest + env overrides wired"

# --- backup.sh -------------------------------------------------------------------
step "backup: snapshot-API copy, recoverable upload ordering, live db never shipped"
BK_DATA="$WORK/bk-data"
mkdir -p "$BK_DATA/mirror/claude/s1/u1" "$BK_DATA/tsnet" "$BK_DATA/releases"
printf 'transcript bytes\n' >"$BK_DATA/mirror/claude/s1/u1/generation-1.jsonl"
printf 'node key material\n' >"$BK_DATA/tsnet/tailscaled.state"
printf 'vTEST\n' >"$BK_DATA/releases/latest"
python3 - "$BK_DATA/store.sqlite" <<'PY'
import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
con.execute("CREATE TABLE t(v TEXT)")
con.execute("INSERT INTO t VALUES('snapshot-me')")
con.commit(); con.close()
PY
GLOG="$STUB_STATE/gcloud.log"
: >"$GLOG"
SESH_DATA_DIR="$BK_DATA" SESH_BACKUP_BUCKET="gs://test-bucket/sesh" PATH="$STUBS:$PATH" \
  sh "$OPS_SRC/backup.sh" >"$WORK/bk.out" 2>&1 || fail "backup run failed: $(cat "$WORK/bk.out")"
[ "$(PATH="$STUBS:$PATH" sqlite3 "$BK_DATA/backup/db/store.sqlite" 'SELECT v FROM t;')" = "snapshot-me" ] ||
  fail "db snapshot is not a queryable copy"
grep -q 'store.sqlite' "$GLOG" && fail "a store.sqlite path reached gcloud (live-file copy forbidden)"
MIRROR_LN=$(grep -n "/mirror gs://test-bucket/sesh/mirror" "$GLOG" | cut -d: -f1 | head -1)
DB_LN=$(grep -n "/backup/db gs://test-bucket/sesh/db" "$GLOG" | cut -d: -f1 | head -1)
[ -n "$MIRROR_LN" ] || fail "mirror not uploaded"
[ -n "$DB_LN" ] || fail "db snapshot dir not uploaded"
[ "$MIRROR_LN" -lt "$DB_LN" ] || fail "db snapshot uploaded before the mirror (breaks the restore invariant)"
grep -q "/tsnet gs://test-bucket/sesh/tsnet" "$GLOG" || fail "tsnet identity not in the backup"
grep -q "/releases gs://test-bucket/sesh/releases" "$GLOG" || fail "releases channel not in the backup"
ok "VACUUM INTO snapshot; mirror before db; tsnet + releases included"

step "backup: second run refreshes the snapshot; fresh empty data dir is fine"
PATH="$STUBS:$PATH" sqlite3 "$BK_DATA/store.sqlite" "INSERT INTO t VALUES('second')"
SESH_DATA_DIR="$BK_DATA" SESH_BACKUP_BUCKET="gs://test-bucket/sesh" PATH="$STUBS:$PATH" \
  sh "$OPS_SRC/backup.sh" >/dev/null 2>&1 || fail "second backup run failed"
[ "$(PATH="$STUBS:$PATH" sqlite3 "$BK_DATA/backup/db/store.sqlite" 'SELECT count(*) FROM t;')" = 2 ] ||
  fail "second run did not refresh the snapshot"
EMPTY="$WORK/bk-empty"
mkdir -p "$EMPTY"
SESH_DATA_DIR="$EMPTY" SESH_BACKUP_BUCKET="gs://test-bucket/sesh" PATH="$STUBS:$PATH" \
  sh "$OPS_SRC/backup.sh" >"$WORK/bk-empty.out" 2>&1 || fail "backup on empty dir failed: $(cat "$WORK/bk-empty.out")"
ok "snapshots refresh; first-boot empty dir does not crash the timer"

step "backup: capacity warning fires past the threshold"
SESH_DATA_DIR="$BK_DATA" SESH_BACKUP_BUCKET="gs://test-bucket/sesh" SESH_DISK_WARN_PCT=0 \
  PATH="$STUBS:$PATH" sh "$OPS_SRC/backup.sh" >"$WORK/bk-warn.out" 2>&1 || fail "warn run failed"
grep -q 'WARNING' "$WORK/bk-warn.out" || fail "no capacity warning at threshold 0"
grep -q 'escape triggers' "$WORK/bk-warn.out" || fail "warning does not point at the escape triggers"
ok "disk pressure warns loudly and names the escape triggers"

all_green
