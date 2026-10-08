#!/usr/bin/env bash

# Print hcom's current seat name when a direct fleet invocation can prove it.
# A serve-provided launcher is already authoritative, and lookup failure is
# deliberately silent so herder register can apply its own best-effort default.
fleet_self_name() {
  local name

  [[ -z ${FLEET_LAUNCHER:-} ]] || return 0
  [[ -n ${HCOM_PROCESS_ID:-} ]] || return 0
  name=$(timeout --foreground 2s hcom list self --json 2>/dev/null \
    | jq -r '.name // empty' 2>/dev/null) || return 0
  printf '%s\n' "$name"
}

# Classify a `herdr pane process-info` JSON document, failing closed. Returns 0
# only for proven idleness: shell_pid is a positive integer and every
# foreground process (objects with a numeric pid and string name, or bare
# pids) is that shell, or foreground_processes is absent/null/empty. Returns 1
# when something else holds the foreground, and 2 for anything unprovable:
# missing or invalid shell_pid, a malformed foreground collection or element,
# or a document jq cannot parse or evaluate.
fleet_idle_shell() {
  local verdict

  verdict=$(jq -r '
    def shells: ["bash", "dash", "fish", "ksh", "nu", "sh", "xonsh", "zsh"];
    .result.process_info as $info
    | if ($info | type) != "object" then "unknown"
      else $info.shell_pid as $shell
      | if ($shell | type) != "number" or $shell <= 0 or ($shell | floor) != $shell then "unknown"
        else $info.foreground_processes as $fg
        | if $fg == null then "idle"
          elif ($fg | type) != "array" then "unknown"
          else [$fg[]
              | if type == "number" then
                  (if . == $shell then "idle" else "busy" end)
                elif type == "object" and (.pid | type) == "number" and (.name | type) == "string" then
                  (. as $process
                   | if $process.pid == $shell and any(shells[]; . == $process.name) then "idle" else "busy" end)
                else "unknown" end]
            | if any(.[]; . == "unknown") then "unknown"
              elif any(.[]; . == "busy") then "busy"
              else "idle" end
          end
        end
      end
  ' <<<"$1" 2>/dev/null) || return 2
  case $verdict in
    idle) return 0 ;;
    busy) return 1 ;;
    *) return 2 ;;
  esac
}

# Print a process-info document's shell_pid, or nothing when it is absent.
fleet_shell_pid() {
  jq -r '.result.process_info.shell_pid // empty' <<<"$1" 2>/dev/null || true
}

# Print the foreground command names of a process-info document, comma-joined,
# "none" when the pane reports no foreground process, or "unknown" when the
# document is malformed.
fleet_foreground_names() {
  jq -r '
    .result.process_info.foreground_processes
    | if . == null or . == [] then "none"
      elif type == "array" then map(if type == "object" then (.name // "?") | tostring else tostring end) | join(",")
      else "unknown" end
  ' <<<"$1" 2>/dev/null || printf 'unknown\n'
}

# Print how many panes a `herdr pane list` document places in one tab. Fails
# when the document carries no panes array, so a malformed list never reads
# as an empty tab.
fleet_tab_pane_count() {
  local panes=$1 tab=$2

  jq -er --arg tab "$tab" '
    if (.result.panes | type) == "array" then [.result.panes[] | select(.tab_id == $tab)] | length
    else error("no panes array") end
  ' <<<"$panes" 2>/dev/null
}

# Worktree-local servers. boomerang gives each checkout its own Postgres
# (<checkout>/data/postgres) and Valkey (<checkout>/data/valkey). cull stops a
# seat's pair and drift names the orphans; both act only on a data dir that is
# exactly <linked worktree>/data/<postgres|valkey>. The shared :5433 server and
# the trace store are protected outright: the paths come from the passwd home,
# not the environment, so no caller can move them.
fleet_db_protected_roots() {
  local home

  home=$(getent passwd "$(id -u)" 2>/dev/null | cut -d: -f6) || home=
  [[ -n $home ]] || home=$HOME
  printf '%s\n' "$home/.local/share/boomerang/postgres18" "$home/.local/state/boomerang-trace-store"
}

# Return 0 when a path is, or is under, a protected store (symlinks resolved
# on both sides as far as they exist).
fleet_db_protected() {
  local path root

  path=$(realpath -m -- "$1")
  while IFS= read -r root; do
    root=$(realpath -m -- "$root")
    [[ $path == "$root" || $path == "$root"/* ]] && return 0
  done < <(fleet_db_protected_roots)
  return 1
}

# Print the top level of the linked git worktree holding a directory. Fails
# for a main checkout (git dir == common dir), a non-repo, or a missing dir.
fleet_linked_worktree() {
  local out top gitdir common

  out=$(cd -- "$1" 2>/dev/null && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_COMMON_DIR GIT_OPTIONAL_LOCKS=0 \
    timeout 10s git rev-parse --path-format=absolute --show-toplevel --git-dir --git-common-dir 2>/dev/null) || return 1
  { read -r top; read -r gitdir; read -r common; } <<<"$out"
  [[ -n $top && -n $gitdir && -n $common && $gitdir != "$common" ]] || return 1
  printf '%s\n' "$top"
}

# Print a worktree's data/<name> as a resolved path, or fail: 1 when it does
# not exist, 2 when it resolves anywhere but <worktree>/data/<name> (a
# symlink out of the checkout) or into a protected store.
fleet_worktree_data_dir() {
  local wt=$1 name=$2 want real

  [[ -e $wt/data/$name || -L $wt/data/$name ]] || return 1
  want=$(realpath -e -- "$wt") || return 1
  want=$want/data/$name
  real=$(realpath -e -- "$wt/data/$name" 2>/dev/null) || return 2
  [[ $real == "$want" ]] || return 2
  ! fleet_db_protected "$real" || return 2
  printf '%s\n' "$real"
}

# Print one TSV row per running Postgres postmaster and Valkey server:
#   kind(postgres|valkey) pid port dir gone(0|1)
# found by process scan. dir is the process cwd (both servers chdir into their
# data dir), else its -D/--dir argument; gone is 1 when that dir was deleted.
# Postgres backends (a postgres whose parent is postgres) are skipped, and so
# is any process whose cwd or argument touches a protected store, and any in
# another mount namespace (a container's paths are not this host's).
fleet_db_servers() {
  local proc pid comm stat ppid pcomm cwd gone darg port kind word next i mntns
  local -a argv words

  mntns=$(readlink /proc/self/ns/mnt 2>/dev/null) || mntns=
  for proc in /proc/[0-9]*; do
    pid=${proc##*/}
    read -r comm 2>/dev/null <"$proc/comm" || continue
    case $comm in
      postgres) kind=postgres ;;
      valkey-server) kind=valkey ;;
      *) continue ;;
    esac
    [[ $(readlink -- "$proc/ns/mnt" 2>/dev/null) == "$mntns" ]] || continue
    stat=$(cat -- "$proc/stat" 2>/dev/null) || continue
    read -r _ ppid _ <<<"${stat##*) }"
    if [[ $kind == postgres ]]; then
      pcomm=
      read -r pcomm 2>/dev/null <"/proc/$ppid/comm" || true
      [[ $pcomm != postgres ]] || continue
    fi
    argv=()
    mapfile -d '' -t argv 2>/dev/null <"$proc/cmdline" || continue
    darg='' port=''
    for ((i = 0; i < ${#argv[@]}; i++)); do
      next=${argv[i + 1]:-}
      case $kind:${argv[i]} in
        postgres:-D | valkey:--dir) darg=$next ;;
        postgres:--pgdata=*) darg=${argv[i]#--pgdata=} ;;
        postgres:-D?*) darg=${argv[i]#-D} ;;
        postgres:-p) port=$next ;;
        postgres:--port=*) port=${argv[i]#--port=} ;;
        valkey:--port) port=$next ;;
      esac
    done
    if [[ $kind == valkey && -z $port ]]; then
      # valkey-server rewrites its title to "valkey-server HOST:PORT [cluster]".
      read -ra words <<<"${argv[*]}"
      for word in "${words[@]}"; do
        [[ $word =~ :([0-9]+)$ ]] && port=${BASH_REMATCH[1]} && break
      done
    fi
    gone=0
    cwd=$(readlink -- "$proc/cwd" 2>/dev/null) || cwd=
    if [[ $cwd == *' (deleted)' ]]; then
      cwd=${cwd% (deleted)}
      gone=1
    fi
    [[ -z $darg ]] || ! fleet_db_protected "$darg" || continue
    [[ -z $cwd ]] || ! fleet_db_protected "$cwd" || continue
    [[ -n $cwd ]] || { [[ $darg == /* ]] && cwd=$darg; } || continue
    [[ -d $cwd ]] || gone=1
    if [[ $kind == postgres && -z $port && $gone == 0 ]]; then
      port=$(sed -n 4p -- "$cwd/postmaster.pid" 2>/dev/null) || port=
    fi
    printf '%s\t%s\t%s\t%s\t%s\n' "$kind" "$pid" "${port:-unknown}" "$cwd" "$gone"
  done
}

# Print the live owners of a worktree, comma-joined (seat:NAME, pane:ID): hcom
# seats whose directory and herdr panes whose cwd or foreground cwd lies
# inside it. skip_seat (and seats it parents) and skip_pane are left out.
# Fails when either document is malformed, so an unreadable roster never
# reads as no owner.
fleet_worktree_owners() {
  local wt=$1 roster=$2 panes=$3 skip_seat=${4:-} skip_pane=${5:-}

  printf '%s\n%s\n' "$roster" "$panes" | jq -ers --arg wt "$wt" --arg seat "$skip_seat" --arg pane "$skip_pane" '
    def inside: type == "string" and (. == $wt or startswith($wt + "/"));
    if length != 2 or (.[0] | type) != "array" or (.[1].result.panes | type) != "array" then error("malformed")
    else
      [(.[0][] | select(($seat == "" or (.name != $seat and .parent_name != $seat)) and (.directory | inside)) | "seat:" + .name),
       (.[1].result.panes[] | select(.pane_id != $pane and ((.cwd | inside) or (.foreground_cwd | inside))) | "pane:" + .pane_id)]
      | join(",")
    end
  ' 2>/dev/null
}
