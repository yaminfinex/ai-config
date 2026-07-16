#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

preflight
setup_workspace
build_mish

SESSION_OWNER_VALUE=owner
new_mission asks-a --authority vile
asks_dir="$(mission_dir asks-a)/asks"

step "asks scaffold and closed surface"
assert_dir "$asks_dir/entities"
assert_contains "$asks_dir/config.yml" "schema: mish.asks/v1"
run_mish "$INVOKE_DIR" "asks-help" asks --help
assert_status 0
assert_contains "$LAST_OUT" "withdraw-with-citation"
run_mish "$INVOKE_DIR" "asks-unknown" asks --mission asks-a reopen
assert_status 2
assert_contains "$LAST_ERR" "unknown subcommand"

step "asks create, status, stale refusal, reply, and settlement"
ask_id="ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ec"
create_input="$WORK/create.json"
printf '%s\n' '{"id":"'"$ask_id"'","kind":"ask","actor":"vile","addressed_to":"owner","expects":"decide","anchor":{"type":"task","ref":"TASK-41"},"links":[{"type":"thread","ref":"task-38-asks-board"}],"framing":{"context":"asks need a contract","question":"which contract?","sub_decisions":[],"options":[{"id":"a","label":"mish","cost":"wait","risk":"sequence","blast_radius":"mish"},{"id":"b","label":"other","cost":"writer","risk":"split","blast_radius":"all"}],"recommendation":{"choice":"a","reason":"one writer"},"do_nothing":"blocked"}}' >"$create_input"
run_mish "$INVOKE_DIR" "asks-create" asks --mission asks-a create --input "$create_input"
assert_status 0
assert_contains "$LAST_OUT" '"verb":"asks.create"'
assert_file "$asks_dir/entities/$ask_id.md"
stamp=$(sed -n 's/.*"updated_at":"\([^"]*\)".*/\1/p' "$LAST_OUT")

run_mish "$INVOKE_DIR" "asks-status" status --mission asks-a
assert_status 0
assert_contains "$LAST_OUT" '"asks":{"available":true'
assert_contains "$LAST_OUT" '"state":"open","count":1'

reply_input="$WORK/reply.json"
printf '%s\n' '{"actor":"owner","if_updated_at":"'"$stamp"'","prose":"considering"}' >"$reply_input"
run_mish "$INVOKE_DIR" "asks-reply" asks --mission asks-a reply "$ask_id" --input "$reply_input"
assert_status 0
assert_contains "$LAST_OUT" '"prose":"considering"'
next_stamp=$(sed -n 's/.*"updated_at":"\([^"]*\)".*/\1/p' "$LAST_OUT")

run_mish "$INVOKE_DIR" "asks-stale" asks --mission asks-a reply "$ask_id" --input "$reply_input"
assert_status 1
assert_contains "$LAST_OUT" '"refusal":"stale_entity"'

settle_input="$WORK/settle.json"
printf '%s\n' '{"actor":"owner","if_updated_at":"'"$next_stamp"'","choice":"a","prose":"use mish"}' >"$settle_input"
run_mish "$INVOKE_DIR" "asks-settle" asks --mission asks-a settle "$ask_id" --input "$settle_input"
assert_status 0
assert_contains "$LAST_OUT" '"kind":"ruling","state":"closed","outcome":"settled"'
assert_contains "$asks_dir/entities/$ask_id.md" "options_as_presented:"

all_green
