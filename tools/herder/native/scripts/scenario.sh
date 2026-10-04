# Sourced by the justfile's check-* recipes: `. scripts/scenario.sh RECIPE`. Builds with `shots`
# (screenshots land in shots/), makes a throwaway HOME (gone on exit) and defines `scenario` and `seen`;
# a recipe keeps only its scripts and its assertions. Nothing reaches the real serve: every run points
# HERDER_URL at testdata/fake_serve.py on a free loopback port of its own (H1), so checks run side by side.
set -euo pipefail
recipe=$1
cargo build --release --features shots
mkdir -p shots
home=$(mktemp -d) fake=
trap '[ -z "$fake" ] || kill $fake 2>/dev/null; rm -r "$home"' EXIT

# scenario NAME "FAKE-SERVE ARGS" SCRIPT [RELAUNCH-SCRIPT]: run the app once per script on its own HOME
# ($home/NAME) against one fake serve; env set on the call (HERDER_NATIVE_FRONT) reaches the app. Each run
# must exit 0 having reached `quit`. The app's log is $home/NAME.log, the fake serve's $home/NAME.fake.
scenario() {
    local name=$1 args=$2 script status port
    shift 2
    # shellcheck disable=SC2086 # the fake serve's arguments are words
    python3 testdata/fake_serve.py 0 $args >"$home/$name.port" 2>"$home/$name.fake" & fake=$!
    # It prints the free port it took once it listens.
    for _ in $(seq 100); do [ -s "$home/$name.port" ] && break; sleep 0.05; done
    port=$(head -1 "$home/$name.port")
    [ -n "$port" ] || { echo "$recipe: $name: the fake serve did not start"; exit 1; }
    for script in "$@"; do
        status=0
        HOME="$home/$name" HERDER_URL=http://127.0.0.1:$port HERDER_NATIVE_SHOT_DIR=shots \
            HERDER_NATIVE_SCRIPT="$script" ./target/release/herder-native >"$home/$name.log" 2>&1 || status=$?
        grep -E "platform|harness|expect|box|says|has|header|notes|list|said|rows|parts|tools|paths|wheel|point|drag|fast|jump|tap|select|click|summon|dock" "$home/$name.log" || true
        [ $status = 0 ] && grep -q '\] quit$' "$home/$name.log" || { echo "$recipe: $name failed (exit $status)"; exit 1; }
    done
    { kill $fake && wait $fake; } 2>/dev/null || true
    fake=
}

# seen NAME PATTERN N: the fake serve logged PATTERN exactly N times in NAME.
seen() {
    local n
    n=$(grep -c "$2" "$home/$1.fake" || true)
    [ "$n" = "$3" ] || { echo "$recipe: $1: the fake serve saw '$2' $n times, expected $3"; exit 1; }
}
