#!/usr/bin/env bash
# check-toolchain-gate.sh — regression coverage for the fail-closed toolchain gates.
#
# Every other suite proves behaviour only once the toolchain has resolved, so all
# of them stay green whether or not the go.mod-derived gates exist. A reverted
# guard is invisible to the battery — the same blind spot that let a two-version
# toolchain drift survive. This suite covers the gates themselves.
#
# The contract each gate owes, for a go.mod it cannot honour: refuse AT THE GATE
# — non-zero, before the suite body runs, naming the toolchain. Never fall back
# to an ambient go, and never surface as a downstream compile error or a golden
# diff. Those silent routes are the whole point, so this suite rejects them
# explicitly instead of accepting any non-zero exit.
#
# It also proves its own discrimination: it removes each guard class from a
# throwaway copy and asserts the matching probe goes RED. A guard test that has
# never been shown to fail proves nothing, so failing to notice a removed guard
# is itself a failure here.
#
# Everything runs against a throwaway copy; the real tree is never written to,
# and nothing copy-specific outlives the run — no record naming a deleted temp
# path is left in the caller's global state. Ordinary Go build and module caches
# from the child builds are not in that class: every suite that builds warms
# them, and they are keyed by content rather than by this run's temp paths.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

for dep in mise awk sed cp cmp mktemp; do
  command -v "$dep" >/dev/null 2>&1 ||
    { printf 'FAIL  harness dependency missing: %s\n' "$dep"; exit 1; }
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
COPY="$WORK/repo"
mkdir -p "$COPY"
# Copy the tracked files only, read from the working tree so local edits to them
# are still under test. Copying the directory wholesale would drag in whatever
# untracked build output the checkout happens to hold — on a working checkout
# that is gigabytes of release artifacts against ~13M of tracked source, a cost
# that grows with local history and buys nothing: the gates read go.mod and the
# scripts, and nothing here needs .git, which this way is never copied at all.
git -C "$REPO_ROOT" ls-files -z >"$WORK/tracked" 2>/dev/null ||
  { printf 'FAIL  cannot list tracked files from %s\n' "$REPO_ROOT"; exit 1; }
[ -s "$WORK/tracked" ] ||
  { printf 'FAIL  no tracked files found under %s\n' "$REPO_ROOT"; exit 1; }
( cd "$REPO_ROOT" && xargs -0 -a "$WORK/tracked" cp -a --parents -t "$COPY" ) ||
  { printf 'FAIL  could not copy the tracked tree into %s\n' "$COPY"; exit 1; }

# Keep every trace of this run inside the trapped temp root. A config outside its
# trusted path makes `mise where` fail for a reason unrelated to the probe, which
# looks exactly like the gate refusing — but `mise trust` would buy that by
# writing a permanent record into the caller's global state naming a path this
# suite is about to delete, and registering a config likewise leaves a per-copy
# entry in the state dir and prunable cache state. Point all three at $WORK and
# nothing copy-specific outlives the run. Exported so the gates under test
# inherit them.
export MISE_TRUSTED_CONFIG_PATHS="$COPY"
export MISE_STATE_DIR="$WORK/mise-state"
export MISE_CACHE_DIR="$WORK/mise-cache"

# Gates under test: every suite that derives its toolchain from a go.mod.
# key|script (repo-relative)|module dir
GATES="
node|tools/herder/tests/check-node-contract.sh|tools/herder
observer|tools/herder/tests/check-observer-contract.sh|tools/herder
grok-doctor|tools/herder/tests/check-grok-doctor.sh|tools/herder
mish|tools/mish/tests/check-nesting.sh|tools/mish
"
gate_script() { printf '%s\n' "$GATES" | awk -F'|' -v k="$1" '$1 == k {print $2; exit}'; }
gate_module() { printf '%s\n' "$GATES" | awk -F'|' -v k="$1" '$1 == k {print $3; exit}'; }

# A version no toolchain manager will resolve, to force the resolve path.
UNRESOLVABLE="9.99.9"
# A toolchain directive that cannot agree with any real pin.
CONFLICTING="go1.99.0"

# --- mutations -------------------------------------------------------------
# Only the `go` directive line is rewritten: replacing the file would drop the
# require block and yield dependency errors instead of toolchain errors.
mutate_gomod() {
  local mod="$COPY/$1/go.mod" mode="$2" pin
  pin="$(awk '$1 == "go" {print $2; exit}' "$mod")"
  case "$mode" in
    unresolvable)     sed -i "s/^go .*/go $UNRESOLVABLE/" "$mod" ;;
    tab-pinned)       sed -i "s/^go .*/go\t$pin/" "$mod" ;;
    no-directive)     sed -i '/^go /d' "$mod" ;;
    conflict)         sed -i "s/^go .*/go $pin\ntoolchain $CONFLICTING/" "$mod" ;;
  esac
}

# --- verdict ---------------------------------------------------------------
# Structural, not message-matching: a refusal is recognised by the suite body
# never starting, so rephrasing a diagnostic cannot silently turn this green.
# Gates refuse with 'FAIL: '; suite assertions print 'PASS  '/'FAIL  ' or 'ok: '.
SUITE_STARTED_RE='^PASS  |^FAIL  |^ok: |^--- '
# The gate did not refuse; something downstream did, after an ambient go was used.
AMBIENT_RE='/bin/go|/usr/bin/go|herder: go on PATH|no satisfying mise-installed go'
# The gate did not refuse; the compiler did — the confusing-error route.
COMPILER_RE='-lang was set to|requires go1\.[0-9]+ or later|\(running go |# command-line-arguments|cannot find package'
# Loudness: a refusal must name the domain it is refusing about.
DOMAIN_RE='go|toolchain|mise'

# gate_pins <gate-key> — 0 if the gate accepted a tab-separated directive and let
# the suite body start. Parser robustness is only observable here: with the
# empty-parse guard in place a weak parser refuses rather than leaking, so the
# refusal path cannot tell the two apart.
#
# This asserts the gate let the body RUN, not that the body passed: whether a
# suite goes green in a copied tree is that suite's own business (and the battery
# already covers it), so conflating the two would make this suite fail for
# reasons that have nothing to do with the toolchain.
gate_pins() {
  local key="$1" script module out pin root run_path
  script="$(gate_script "$key")"; module="$(gate_module "$key")"
  REASON=""
  pin="$(awk '$1 == "go" {print $2; exit}' "$COPY/$module/go.mod")"
  # Put the pinned toolchain on PATH for this probe. One gate enforces a floor
  # against the caller's go rather than resolving the pin itself, so without this
  # the probe would be measuring the ambient shell instead of the parser.
  # Resolve from the copy: it is the path this run trusts, whereas the outer
  # checkout is untrusted under the isolated state above and would fail here,
  # silently dropping the pinned toolchain from PATH.
  root="$(cd "$COPY" && mise where "go@$pin" 2>/dev/null)" || root=""
  run_path="$PATH"
  [ -n "$root" ] && run_path="$root/bin:$PATH"
  cp "$COPY/$module/go.mod" "$WORK/gomod.bak"
  mutate_gomod "$module" tab-pinned
  out="$(cd "$COPY" && PATH="$run_path" GOTOOLCHAIN=local timeout 300 "./$script" 2>&1)"
  cp "$WORK/gomod.bak" "$COPY/$module/go.mod"
  # Body output first: gates refuse before any of it is printed.
  if grep -qE -- "$SUITE_STARTED_RE" <<<"$out" || grep -q 'ALL GREEN' <<<"$out"; then
    return 0
  fi
  # State what was observed, not why: this suite cannot establish the cause of a
  # refusal, and asserting one would be the same misdiagnosis these gates fixed.
  REASON="suite body never started; first failure: $(grep -E -- '^FAIL' <<<"$out" | head -1 | tr -s ' ' | cut -c1-75)"
  return 1
}

# gate_refuses <gate-key> <mutation> — 0 if the gate refused properly; sets REASON if not.
gate_refuses() {
  local key="$1" mode="$2" script module out rc
  script="$(gate_script "$key")"; module="$(gate_module "$key")"
  REASON=""
  cp "$COPY/$module/go.mod" "$WORK/gomod.bak"
  mutate_gomod "$module" "$mode"
  out="$(cd "$COPY" && timeout 300 "./$script" 2>&1)"; rc=$?
  cp "$WORK/gomod.bak" "$COPY/$module/go.mod"

  if grep -q 'ALL GREEN' <<<"$out"; then
    REASON="ran green on a go.mod it cannot honour"; return 1
  fi
  if [ "$rc" -eq 0 ]; then
    REASON="exited 0 on a go.mod it cannot honour"; return 1
  fi
  if grep -qE -- "$SUITE_STARTED_RE" <<<"$out"; then
    REASON="suite body started; the gate did not refuse first"; return 1
  fi
  if grep -qE -- "$AMBIENT_RE" <<<"$out"; then
    REASON="fell back to an ambient go: $(grep -oE -- ".{0,25}($AMBIENT_RE).{0,25}" <<<"$out" | head -1 | tr -s ' ')"; return 1
  fi
  if grep -qE -- "$COMPILER_RE" <<<"$out"; then
    REASON="surfaced as a compile error: $(grep -oE -- "($COMPILER_RE).{0,40}" <<<"$out" | head -1 | tr -s ' ')"; return 1
  fi
  if ! grep -qiE -- "$DOMAIN_RE" <<<"$out"; then
    REASON="failed without naming the toolchain: $(tail -1 <<<"$out" | tr -s ' ' | cut -c1-70)"; return 1
  fi
  return 0
}

# --- phase 1: every gate refuses every go.mod it cannot honour --------------
printf -- '--- gate refusals\n'
for key in node observer grok-doctor mish; do
  for mode in unresolvable no-directive conflict; do
    if gate_refuses "$key" "$mode"; then
      ok "$key: refuses at the gate [$mode]"
    else
      bad "$key: $mode" "$REASON"
    fi
  done
  if gate_pins "$key"; then
    ok "$key: reads a tab-separated directive and pins correctly"
  else
    bad "$key: tab-pinned" "$REASON"
  fi
done

# --- phase 2: this suite can see a guard disappear --------------------------
# Each class is removed from a copy of a gate, then the probe that exists to
# catch that removal must go RED. If it still passes, the probe is not testing
# what it claims and this suite says so.
#
# The gates are defence in depth, which shapes what a class means here: the
# GOVERSION readback backstops the parser, the empty-parse guard and the mise
# resolve, so removing any single one of those still refuses (verified). A class
# is therefore the smallest removal whose contract observably breaks, and where
# the readback is the thing that would mask the leak it is removed too. Asserting
# a leak the code correctly prevents would be a lie about the code.
#
# The mish gate has no resolution class: its preflight enforces a floor
# (go >= the pin) rather than resolving an exact pin. That is its documented
# contract, not a missing guard.
printf -- '--- discrimination self-check\n'

degrade() { # degrade <class> <gate-script-path>
  local class="$1" f="$2"
  case "$class" in
    parser)
      # Weak parser: a tab-separated directive reads empty.
      sed -i 's|\$1 == "go" {print \$2; exit}|/^go /{print $2; exit}|' "$f" ;;
    empty-parse)
      # Without the readback to mask it, an empty pin resolves off the ambient
      # toolchain config instead of go.mod — the mute wrong-pin route.
      sed -i '/^\[ -n "\$GO_VERSION" \]/d' "$f"
      sed -i '/^\[ "\$GO_HAVE" = "\$GO_VERSION" \]/,+1d' "$f" ;;
    toolchain-conflict)
      sed -i '/^\[ -z "\$TOOLCHAIN" \]/,+1d' "$f" ;;
    exact-pin-resolution)
      sed -i 's|^GO_ROOT=.*|GO_ROOT="$(mise where "go@$GO_VERSION" 2>/dev/null)"|' "$f"
      sed -i '/^  toolchain_fail "could not resolve go /d' "$f"
      sed -i '/^\[ "\$GO_HAVE" = "\$GO_VERSION" \]/,+1d' "$f" ;;
  esac
}

# self_check <class> <label> <gate-key> <probe> [mutation]
# probe is `refuses <mutation>` or `pins`.
self_check() {
  local class="$1" label="$2" key="$3" probe="$4" mode="${5:-}" f detected=1
  f="$COPY/$(gate_script "$key")"
  cp "$f" "$WORK/gate.bak"
  degrade "$class" "$f"
  if cmp -s "$f" "$WORK/gate.bak"; then
    cp "$WORK/gate.bak" "$f"
    bad "self-check: $label" "removal did not apply — the gate was rewritten; update this suite"
    return
  fi
  if [ "$probe" = pins ]; then
    gate_pins "$key" && detected=0
  else
    gate_refuses "$key" "$mode" && detected=0
  fi
  cp "$WORK/gate.bak" "$f"
  if [ "$detected" -eq 0 ]; then
    bad "self-check: $label" "the probe still passed with the $label removed — this suite is not discriminating"
  else
    ok "self-check: removing the $label is caught"
  fi
}

self_check parser               "whitespace-robust parser"  node pins
self_check empty-parse          "empty-parse guard"         node refuses no-directive
self_check toolchain-conflict   "toolchain-conflict check"  node refuses conflict
self_check exact-pin-resolution "exact-pin resolution"      node refuses unresolvable

if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - toolchain gates fail closed, and this suite can see them disappear.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
