#!/usr/bin/env bash
# Place one Claude or Codex seat in herdr, then launch it through hcom's
# per-invocation fleet preset. Failed launches leave created placement in place
# and print its coordinates; cleanup is always explicit.

set -euo pipefail

die() {
  printf 'fleet spawn: %s\n' "$*" >&2
  exit 1
}

usage() {
  local rc=${1:-2}
  cat >&2 <<'EOF'
usage: spawn.sh <claude|codex> [--model MODEL] [--effort LEVEL] --tag TAG
                (--workspace ID | --worktree-branch NAME --repo PATH |
                 --pane ID | --split-from PANE_ID|self)
                [--split-direction right|down] [--prompt TEXT]

--split-from self splits beside the caller's own pane (herdr pane current).
--split-direction defaults to right.
EOF
  exit "$rc"
}

[[ $# -ge 1 ]] || usage
tool=$1
shift
[[ $tool == claude || $tool == codex ]] || die "tool must be claude or codex"

model=
effort=
tag=
workspace=
worktree_branch=
repo=
pane=
split_from=
split_direction=
prompt=

while (($# > 0)); do
  case $1 in
    --model)
      [[ $# -ge 2 ]] || usage
      model=$2
      shift 2
      ;;
    --effort)
      [[ $# -ge 2 ]] || usage
      effort=$2
      shift 2
      ;;
    --tag)
      [[ $# -ge 2 ]] || usage
      tag=$2
      shift 2
      ;;
    --workspace)
      [[ $# -ge 2 ]] || usage
      workspace=$2
      shift 2
      ;;
    --worktree-branch)
      [[ $# -ge 2 ]] || usage
      worktree_branch=$2
      shift 2
      ;;
    --repo)
      [[ $# -ge 2 ]] || usage
      repo=$2
      shift 2
      ;;
    --pane)
      [[ $# -ge 2 ]] || usage
      pane=$2
      shift 2
      ;;
    --split-from)
      [[ $# -ge 2 ]] || usage
      split_from=$2
      shift 2
      ;;
    --split-direction)
      [[ $# -ge 2 ]] || usage
      split_direction=$2
      shift 2
      ;;
    --prompt)
      [[ $# -ge 2 ]] || usage
      prompt=$2
      shift 2
      ;;
    -h | --help)
      usage 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n $tag ]] || die "--tag is required"
[[ $tag =~ ^[A-Za-z0-9][A-Za-z0-9_-]*$ ]] || die "--tag must contain only letters, digits, underscore, and hyphen"
if [[ -n $effort ]]; then
  case "$tool:$effort" in
    claude:low | claude:medium | claude:high | claude:xhigh | claude:max | \
      codex:low | codex:medium | codex:high | codex:xhigh) ;;
    claude:*) die "--effort for claude must be one of: low, medium, high, xhigh, max" ;;
    codex:*) die "--effort for codex must be one of: low, medium, high, xhigh" ;;
  esac
fi

placements=0
[[ -n $workspace ]] && ((placements += 1))
[[ -n $worktree_branch || -n $repo ]] && ((placements += 1))
[[ -n $pane ]] && ((placements += 1))
[[ -n $split_from ]] && ((placements += 1))
((placements == 1)) || die "choose exactly one placement: --workspace, --worktree-branch with --repo, --pane, or --split-from"
if [[ -n $worktree_branch || -n $repo ]]; then
  [[ -n $worktree_branch && -n $repo ]] || die "--worktree-branch and --repo must be used together"
fi
if [[ -n $split_direction ]]; then
  [[ -n $split_from ]] || die "--split-direction only applies with --split-from"
  [[ $split_direction == right || $split_direction == down ]] || die "--split-direction must be right or down"
fi

command -v jq >/dev/null || die "jq is required"
command -v hcom >/dev/null || die "hcom is required"
command -v herdr >/dev/null || die "herdr is required"

placement_kind=
placement_detail=
cwd=
pane_id=

if [[ -n $workspace ]]; then
  placement_kind=tab
  create_output=$(herdr tab create --workspace "$workspace" --no-focus) || die "herdr tab create failed for workspace $workspace"
  tab_id=$(jq -r '.result.tab.tab_id // empty' <<<"$create_output")
  placement_detail="workspace=$workspace${tab_id:+ tab=$tab_id}"
  pane_id=$(jq -er '.result.root_pane.pane_id | select(length > 0)' <<<"$create_output") \
    || die "tab create returned no root pane id ($placement_detail left for explicit cleanup)"
  cwd=$(jq -r '.result.root_pane.foreground_cwd // .result.root_pane.cwd // empty' <<<"$create_output")
elif [[ -n $worktree_branch ]]; then
  placement_kind=worktree
  placement_detail="branch=$worktree_branch repo=$repo"
  source_output=$(herdr worktree list --cwd "$repo") || die "cannot resolve source repository from $repo"
  source_repo=$(jq -er '.result.source.source_checkout_path // .result.source.repo_root | select(length > 0)' <<<"$source_output") || die "worktree list returned no source checkout for $repo"
  create_output=$(herdr worktree create --cwd "$source_repo" --branch "$worktree_branch" --no-focus) || die "herdr worktree create failed for branch $worktree_branch"
  pane_id=$(jq -er '.result.root_pane.pane_id | select(length > 0)' <<<"$create_output") \
    || die "worktree create returned no root pane id ($placement_detail left for explicit cleanup)"
  cwd=$(jq -er '.result.workspace.worktree.checkout_path | select(length > 0)' <<<"$create_output") \
    || die "worktree create returned no checkout path ($placement_detail, pane=$pane_id left for explicit cleanup)"
else
  if [[ -n $split_from ]]; then
    if [[ $split_from == self ]]; then
      source_output=$(herdr pane current) || die "cannot resolve own pane (herdr pane current failed; not running inside a herdr pane?)"
      source_pane=$(jq -er '.result.pane.pane_id | select(length > 0)' <<<"$source_output") || die "pane current returned no pane id"
    else
      source_output=$(herdr pane get "$split_from") || die "pane does not exist: $split_from"
      source_pane=$(jq -er '.result.pane.pane_id | select(length > 0)' <<<"$source_output") || die "pane get returned no pane id"
    fi
    # herdr 0.8 requires --direction; without it pane split exits 2 with usage.
    split_output=$(herdr pane split --pane "$source_pane" --direction "${split_direction:-right}" --no-focus) || die "herdr pane split failed for pane $source_pane"
    pane=$(jq -er '.result.pane.pane_id // .result.pane_id | select(length > 0)' <<<"$split_output") \
      || die "pane split returned no pane id (source pane=$source_pane left unchanged; split placement may need explicit cleanup)"
    placement_kind=split-pane
  else
    placement_kind=existing-pane
  fi
  placement_detail="pane=$pane"
  pane_output=$(herdr pane get "$pane") || die "pane does not exist: $pane"
  pane_id=$(jq -er '.result.pane.pane_id | select(length > 0)' <<<"$pane_output") || die "pane get returned no pane id"
  process_output=$(herdr pane process-info --pane "$pane_id") || die "cannot inspect pane process state: $pane_id"
  shell_pid=$(jq -r '.result.process_info.shell_pid // empty' <<<"$process_output")
  [[ -n $shell_pid ]] \
    || die "cannot verify idle shell because process info omitted shell_pid: $pane_id"
  if jq -e '
      .result.process_info as $info
      | any(($info.foreground_processes // [])[];
          . as $process
          | $process.pid != $info.shell_pid
            or (["bash", "dash", "fish", "ksh", "nu", "sh", "xonsh", "zsh"] | index($process.name)) == null)
    ' <<<"$process_output" >/dev/null; then
    die "pane is not at an idle shell: $pane_id"
  fi
  cwd=$(jq -r '.result.pane.foreground_cwd // .result.pane.cwd // empty' <<<"$pane_output")
fi

if [[ -z $cwd ]]; then
  pane_output=$(herdr pane get "$pane_id") \
    || die "cannot resolve cwd from pane $pane_id ($placement_detail left for explicit cleanup)"
  cwd=$(jq -er '.result.pane.foreground_cwd // .result.pane.cwd | select(length > 0)' <<<"$pane_output") \
    || die "pane has no cwd: $pane_id ($placement_detail left for explicit cleanup)"
fi
[[ -d $cwd ]] || die "placement cwd is not a directory: $cwd ($placement_detail, pane=$pane_id left for explicit cleanup)"

launch=(hcom 1 "$tool" --tag "$tag" --dir "$cwd")
[[ -z $model ]] || launch+=(--model "$model")
if [[ -n $effort ]]; then
  if [[ $tool == claude ]]; then
    launch+=(--effort "$effort")
  else
    launch+=(-c "model_reasoning_effort=\"$effort\"")
  fi
fi
[[ -z $prompt ]] || launch+=(--hcom-prompt "$prompt")
if [[ $tool == claude ]]; then
  launch+=(--dangerously-skip-permissions)
else
  launch+=(--dangerously-bypass-approvals-and-sandbox)
fi
launch+=(--go)

set +e
launch_output=$(FLEET_PANE=$pane_id HCOM_TERMINAL=fleet "${launch[@]}" 2>&1)
launch_rc=$?
set -e
printf '%s\n' "$launch_output" >&2

hcom_name=$(sed -n 's/^Names:[[:space:]]*//p' <<<"$launch_output" | head -n 1 | tr -d '[:space:]')
[[ -n $hcom_name && $hcom_name != *,* ]] || die "hcom did not report exactly one launched name (placement left at $pane_id)"

batch_id=$(sed -n 's/^Batch id:[[:space:]]*//p' <<<"$launch_output" | head -n 1 | tr -d '[:space:]')
if [[ -z $batch_id ]]; then
  batch_id=$(hcom events --action batch_launched --last 100 \
    | jq -r --arg name "$hcom_name" 'select((.data.instances // []) | index($name)) | .data.batch_id' \
    | tail -n 1)
fi
[[ -n $batch_id ]] || die "cannot find launch batch for $hcom_name (placement left at $pane_id)"

set +e
ready_output=$(hcom events launch "$batch_id" --timeout 120 2>&1)
ready_rc=$?
set -e
printf '%s\n' "$ready_output" >&2
ready_json=$(sed -n '/^{"batch_id"/p' <<<"$ready_output" | tail -n 1)

if (( (launch_rc != 0 && launch_rc != 2) || ready_rc != 0)); then
  die "launch failed or blocked for $hcom_name (batch=$batch_id, $placement_detail, pane=$pane_id)"
fi
if [[ -z $ready_json ]] || ! jq -e '.status == "ready" and .ready == 1 and .blocked == 0 and .failed == 0' <<<"$ready_json" >/dev/null; then
  die "launch did not become ready for $hcom_name (batch=$batch_id, pane=$pane_id)"
fi

roster_entry=
for ((hooks_attempts = 0; hooks_attempts < 10; hooks_attempts++)); do
  roster_json=$(hcom list --json 2>/dev/null || true)
  roster_entry=$(jq -cer --arg base "$hcom_name" \
    '[.[] | select(.base_name == $base or .name == $base)] | select(length == 1) | .[0]' \
    <<<"$roster_json" 2>/dev/null || true)
  if [[ -n $roster_entry ]] && jq -e '.hooks_bound == true' <<<"$roster_entry" >/dev/null; then
    break
  fi
  sleep 1
done
[[ -n $roster_entry ]] || die "ready launch is missing or ambiguous in hcom list: $hcom_name (batch=$batch_id, $placement_detail, pane=$pane_id left for explicit cleanup)"
full_name=$(jq -er '.name | select(length > 0)' <<<"$roster_entry") \
  || die "ready launch roster row has no name: $hcom_name (batch=$batch_id, $placement_detail, pane=$pane_id)"
if ! jq -e '.hooks_bound == true' <<<"$roster_entry" >/dev/null; then
  if [[ $tool == codex ]]; then
    printf 'fleet spawn: note: ready launch is not hook-bound in hcom roster: %s\n' \
      "$full_name (batch=$batch_id, $placement_detail, pane=$pane_id left for explicit cleanup)" >&2
  else
    die "ready launch is not hook-bound in hcom roster: $full_name (batch=$batch_id, $placement_detail, pane=$pane_id left for explicit cleanup)"
  fi
fi

printf 'name=%s\n' "$full_name"
printf 'pane=%s\n' "$pane_id"
printf 'cwd=%s\n' "$cwd"
printf 'placement=%s\n' "$placement_kind"
