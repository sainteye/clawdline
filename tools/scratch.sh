#!/bin/bash
# Scratch that has an owner, and goes away with it.
#
# Every snapshot recipe this repository used to print began `snapshot_dir=$(mktemp -d);
# test_tmp=$(mktemp -d)` and removed neither directory. A root has no `work/`, so each run left a
# whole repository snapshot and a test binary behind: 106 `tmp.*` directories holding 1,871 MB in
# this user's temporary directory on 2026-09-11 at 12:44 UTC, 13 of them carrying `clawdline-tests`.
# Nothing had an owner, so nothing had a deadline.
#
# This is the one place such scratch is made and removed. The contract it implements — the owned
# root, the entry, the marker, the owner and what makes an entry releasable — is written once, in
# docs/scratch.md, because the broker sweeps the same root against the same text.
#
#   tools/scratch.sh snapshot-run --subject worktree|index [--root DIR] [--keep [--ttl-hours N]] -- COMMAND…
#   tools/scratch.sh new PURPOSE [--root DIR] [--ttl-hours N]
#   tools/scratch.sh remove PATH [--root DIR]
#
# /bin/bash 3.2 and the stock macOS userland, nothing to install. Every `ps`, `date` and `kill` whose
# output is parsed runs with LC_ALL=C, and every time is read with TZ=UTC: this Mac runs zh_TW, where
# date formats change their field counts, and a start time read in one zone and compared in another
# is eight hours wrong.
set -u
set -o pipefail

prog=scratch.sh
readonly SCRATCH_DEFAULT_ROOT=/tmp/clawdline-scratch
readonly SCRATCH_MARKER=.clawdline-scratch.json
# The command's own status passes through `snapshot-run` unchanged, so these numbers can collide
# with it. The typed code printed on stderr is the authority; the number is a convenience.
readonly EX_USAGE=64 EX_SNAPSHOT=70 EX_ROOT=73 EX_CLEANUP=74 EX_REFUSED=77
# An entry that `snapshot-run` cannot give a provable owner still has to go away on its own.
readonly SNAPSHOT_UNOWNED_TTL_HOURS=6
readonly KEEP_DEFAULT_TTL_HOURS=4
readonly PURPOSE_RE='^[a-z0-9][a-z0-9-]{0,39}$'
readonly ENTRY_RE='^[a-z0-9][a-z0-9-]{0,39}\.[A-Za-z0-9_]+$'
# Only this tool writes markers, and it writes exactly this shape, so one pattern is the whole
# parser. Anything else — another version, another spelling, a second line — is `unknown`.
readonly MARKER_RE='^\{"clawdline_scratch":1,"created_at":[0-9]+,"purpose":"[a-z0-9][a-z0-9-]{0,39}","owner":(null|\{"pid":[0-9]+,"process_start":[0-9]+,"command":"[A-Za-z0-9._+-]*"\}),"keep_until":(null|[0-9]+)\}$'
readonly OWNER_RE='"owner":\{"pid":([0-9]+),"process_start":([0-9]+),'

usage() {
  cat <<'EOF'
usage:
  tools/scratch.sh snapshot-run --subject worktree|index [--root DIR] [--keep [--ttl-hours N]] -- COMMAND [ARG...]
  tools/scratch.sh new PURPOSE [--root DIR] [--ttl-hours N]
  tools/scratch.sh remove PATH [--root DIR]

Scratch with an owner. An entry is a mode-0700 directory named PURPOSE.RANDOM directly under the
owned root, ${CLAWDLINE_SCRATCH_ROOT:-/tmp/clawdline-scratch}, carrying a marker that names who owns
it and until when. The contract is docs/scratch.md.

snapshot-run  Copy the repository you are standing in into a new entry, run COMMAND at the top of
              that copy with TMPDIR=<entry>/tmp, and remove the entry however the run ends:
              success, failure, INT, TERM or HUP. COMMAND's exit status comes back unchanged —
              75 from ./test.sh still means "the machine was busy", not "the suite was red".
  --subject worktree
              HEAD, plus the tracked diff, plus untracked files: "does what I wrote work?". Reads a
              private copy of the index and writes nothing to the shared index or .git. A child's
              subject.
  --subject index
              The staged index, through git write-tree: "will HEAD still build after this commit?".
              ROOT ONLY. It writes a tree object into the shared .git, and only the session that is
              staging may ask that question.
  --root DIR  Make the entry under DIR instead. DIR is an absolute path, as CLAWDLINE_SCRATCH_ROOT
              is; a relative one is refused. A child passes /tmp/.clawdline/<task-id>/work.
  --keep      Keep the entry after COMMAND exits and print its path. It is then held like an entry
              made by `new`, with keep_until --ttl-hours from now (default 4).

new           Make an entry a session keeps across tool calls — a deploy copy, a credential copy —
              and print its path. Its owner is the nearest claude or codex process above this one;
              when there is none, or the process table cannot be read, --ttl-hours (1-24) is required.

remove        Remove one entry. Refuses anything that is not a directory directly under the root, a
              symbolic link, an entry with no version-1 marker, an entry whose owner is still running
              and is not one of this process's ancestors, and an entry whose owner's liveness cannot
              be read. An owner is gone only when the system answers "No such process" for its pid,
              or the process running under that pid started at another time.

exit status   COMMAND's own for snapshot-run, otherwise 0; a signal ends snapshot-run by that same
              signal once the entry is gone. Refusals, with the typed code on stderr:
  64  scratch_usage, scratch_ttl_required
  70  scratch_not_in_git, scratch_snapshot_failed, scratch_snapshot_incomplete
  73  scratch_root_not_absolute, scratch_root_not_normalized, scratch_root_symlink,
      scratch_root_not_directory, scratch_root_not_owned, scratch_root_uncreatable
  74  scratch_cleanup_failed
  77  scratch_not_under_root, scratch_not_an_entry, scratch_marker_missing, scratch_marker_unknown,
      scratch_owner_live, scratch_owner_unknown
EOF
}

die() {  # $1 exit status, $2 typed code, $3 sentence
  printf '%s: %s: %s\n' "$prog" "$2" "$3" >&2
  exit "$1"
}

trim() {
  local s=$1
  s=${s#"${s%%[! ]*}"}
  s=${s%"${s##*[! ]}"}
  printf '%s' "$s"
}

check_ttl() {
  case $1 in
    ''|*[!0-9]*) die $EX_USAGE scratch_usage "--ttl-hours takes a whole number of hours from 1 to 24, not '$1'" ;;
  esac
  [ "$((10#$1))" -ge 1 ] && [ "$((10#$1))" -le 24 ] \
    || die $EX_USAGE scratch_usage "--ttl-hours must be from 1 to 24, not $1"
}

# ---- The root ------------------------------------------------------------------------------------

# Sets SCRATCH_ROOT (as spelled) and SCRATCH_ROOT_REAL (physical). $2 is `create` or `existing`.
# A root that is relative, has a . or .. component, is a link, is not a directory, or is somebody
# else's is refused — never resolved, never replaced, and never worked around by choosing another
# place, because a sweep of the place that was chosen instead is a sweep nobody agreed to.
open_root() {
  local root=${1-} mode=${2:-create}
  [ -n "$root" ] || die $EX_USAGE scratch_usage "the scratch root is an empty string"
  # Whichever of the default, CLAWDLINE_SCRATCH_ROOT and --root it came from. The broker's sweep
  # refuses a relative root as root_not_absolute, so resolving one against this process's working
  # directory would make entries no sweep ever lists. Refused here, before anything is created.
  case $root in
    /*) ;;
    *) die $EX_ROOT scratch_root_not_absolute \
         "the scratch root '$root' is not an absolute path; refusing it rather than resolving it against $PWD" ;;
  esac
  while [ "$root" != / ] && [ "${root%/}" != "$root" ]; do root=${root%/}; done
  # The sweep also refuses a root with a . or .. component as root_not_normalized, in the same order:
  # trailing slashes trimmed, then absolute, then no dot component. The kernel resolves such a component
  # physically, through whatever link stands before it, so the checks below would judge a place the
  # spelling never named — and entries made there would be ones no sweep ever lists.
  case /$root/ in
    */./*|*/../*) die $EX_ROOT scratch_root_not_normalized \
      "the scratch root '$root' has a . or .. component; refusing it rather than resolving it" ;;
  esac
  if [ ! -e "$root" ] && [ ! -L "$root" ]; then
    [ "$mode" = create ] \
      || die $EX_REFUSED scratch_not_under_root "the scratch root $root does not exist, so nothing is an entry under it"
    # Not `mkdir -p`: inventing a chain of directories to reach a mistyped root is working around
    # the mistake. Losing a race to another creator is fine; the checks below judge what won.
    mkdir -m 0700 -- "$root" 2>/dev/null || [ -e "$root" ] || [ -L "$root" ] \
      || die $EX_ROOT scratch_root_uncreatable "cannot create the scratch root $root"
  fi
  [ ! -L "$root" ] \
    || die $EX_ROOT scratch_root_symlink "$root is a symbolic link; refusing it rather than following it or choosing somewhere else"
  [ -d "$root" ] || die $EX_ROOT scratch_root_not_directory "$root exists and is not a directory"
  [ -O "$root" ] || die $EX_ROOT scratch_root_not_owned "$root is not owned by uid $(id -u)"
  SCRATCH_ROOT=$root
  SCRATCH_ROOT_REAL=$(cd -P -- "$root" 2>/dev/null && pwd -P) \
    || die $EX_ROOT scratch_root_uncreatable "cannot enter the scratch root $root"
}

# ---- Processes -----------------------------------------------------------------------------------

# Seconds since the epoch at which process $1 started, at whole-second resolution; 1 when that cannot
# be read, whatever the reason. A `ps` that prints no row has not said the pid is absent — it may not
# be allowed to run, or to show that one pid — so whether a process exists is asked of the kernel
# (signal_probe) and never read from here.
process_start() {
  local raw
  raw=$(LC_ALL=C TZ=UTC ps -o lstart= -p "$1" 2>/dev/null) || return 1
  set -f
  # shellcheck disable=SC2086
  set -- $raw
  set +f
  [ $# -eq 5 ] || return 1
  raw=$(LC_ALL=C TZ=UTC date -j -f '%b %d %H:%M:%S %Y' "$2 $3 $4 $5" +%s 2>/dev/null) || return 1
  case $raw in ''|*[!0-9]*) return 1 ;; esac
  printf '%s\n' "$raw"
}

# The basename the process was started as, in an alphabet a JSON string needs no escaping for.
process_command() {
  local comm
  comm=$(LC_ALL=C ps -o comm= -p "$1" 2>/dev/null) || return 1
  comm=$(trim "${comm##*/}")
  [ -n "$comm" ] || return 1
  printf '%s' "$comm" | LC_ALL=C tr -c 'A-Za-z0-9._+-' '_' | cut -c 1-64
}

owner_json() {  # $1 pid
  local start command
  start=$(process_start "$1") || return 1
  command=$(process_command "$1") || return 1
  printf '{"pid":%s,"process_start":%s,"command":"%s"}' "$1" "$start" "$command"
}

# The nearest ancestor that is the assistant — the one process a session keeps for as long as it
# lives. `comm` is what the process was started as: Claude Code's `claude` is a link to a binary
# named after its version, so `ucomm` alone would read `2.1.268`.
assistant_ancestor() {
  local pid=$PPID depth=0 comm ucomm
  while [ "$pid" -gt 1 ] 2>/dev/null && [ "$depth" -lt 64 ]; do
    comm=$(LC_ALL=C ps -o comm= -p "$pid" 2>/dev/null) || return 1
    ucomm=$(LC_ALL=C ps -o ucomm= -p "$pid" 2>/dev/null) || ucomm=
    case $(trim "${comm##*/}") in claude|codex) printf '%s\n' "$pid"; return 0 ;; esac
    case $(trim "$ucomm") in claude|codex) printf '%s\n' "$pid"; return 0 ;; esac
    pid=$(LC_ALL=C ps -o ppid= -p "$pid" 2>/dev/null) || return 1
    pid=$(trim "$pid")
    depth=$((depth + 1))
  done
  return 1
}

# Whether pid $1 exists, asked of the kernel with signal 0. 0 = it exists, 1 = the system answered
# "No such process", 2 = any other answer. "Operation not permitted" is existence: a process this user
# may not signal is still there.
#
# `kill` is looked up on PATH through `env`, as `ps` and `date` are, and not bash's builtin: a builtin
# is never looked up, so nothing put first on PATH could answer for it, and its message carries this
# script's path and line number. Measured on Darwin 24.6.0, normally and under a sandbox that denies
# process-info, in the C locale and in zh_TW: /bin/kill exits 1 with exactly `kill: <pid>: No such
# process` or `kill: <pid>: Operation not permitted`, and 0 silently for a process it may signal.
# Anything else — a kill that could not be run, another kill's wording, an illegal pid — is not an
# answer about the owner.
signal_probe() {  # $1 pid
  local said
  said=$(LC_ALL=C env kill -0 "$1" 2>&1 >/dev/null)
  case $?:$said in
    0:) return 0 ;;
    "1:kill: $1: Operation not permitted") return 0 ;;
    "1:kill: $1: No such process") return 1 ;;
  esac
  return 2
}

# Whether the exact process recorded in a marker is still the one running under that pid: the same
# pid and the same start, ±1 s. 0 = alive, 1 = gone, 2 = unknown. The broker's sweep decides the same
# way (scratchOwnerStatus in Sources/OwnedStorage.swift).
#
# An owner is gone for exactly two reasons: the system says there is no such process, or the process
# under that pid started at another time. Nothing `ps` fails to show is either. A Codex sandbox refuses
# `ps` altogether, and a table can hide one pid while it shows another — so reading pid 1 beside a
# missing row, which this once rested on, proved only that pid 1 could be read.
owner_liveness() {  # $1 pid, $2 recorded process_start
  local now drift
  signal_probe "$1"
  case $? in
    0) ;;
    1) return 1 ;;
    *) return 2 ;;
  esac
  now=$(process_start "$1") || return 2
  drift=$((now - $2))
  [ "$drift" -ge -1 ] && [ "$drift" -le 1 ] && return 0
  return 1
}

# 0 = $1 is this process or one above it, 1 = the chain was read to its top without meeting $1,
# 2 = it could not be read that far, so whether the entry is this caller's own cannot be said.
is_ancestor() {  # $1 pid
  local pid=$$ depth=0
  while [ "$depth" -lt 128 ]; do
    [ "$pid" = "$1" ] && return 0
    [ "$pid" -gt 1 ] || return 1
    pid=$(LC_ALL=C ps -o ppid= -p "$pid" 2>/dev/null) || return 2
    pid=$(trim "$pid")
    case $pid in ''|*[!0-9]*) return 2 ;; esac
    depth=$((depth + 1))
  done
  return 2
}

# ---- Entries -------------------------------------------------------------------------------------

make_entry() {  # $1 purpose; sets ENTRY
  ENTRY=$(mktemp -d "$SCRATCH_ROOT_REAL/$1.XXXXXXXX" 2>/dev/null) \
    || die $EX_ROOT scratch_root_uncreatable "cannot create an entry under $SCRATCH_ROOT"
  chmod 0700 "$ENTRY"
}

# A temporary name and a rename, so that a reader finds no marker or a whole one, never half.
write_marker() {  # $1 entry, $2 purpose, $3 created_at, $4 owner JSON, $5 keep_until JSON
  local tmp="$1/$SCRATCH_MARKER.tmp.$$"
  printf '{"clawdline_scratch":1,"created_at":%s,"purpose":"%s","owner":%s,"keep_until":%s}\n' \
    "$3" "$2" "$4" "$5" > "$tmp" 2>/dev/null \
    && mv -f -- "$tmp" "$1/$SCRATCH_MARKER" 2>/dev/null && return 0
  rm -f -- "${tmp:?}"
  return 1
}

read_marker() {  # $1 entry; prints the marker. 1 = missing, 2 = not a version-1 marker.
  local marker="$1/$SCRATCH_MARKER" content
  [ -f "$marker" ] && [ ! -L "$marker" ] || return 1
  content=$(cat -- "$marker" 2>/dev/null) || return 2
  [[ $content =~ $MARKER_RE ]] || return 2
  printf '%s' "$content"
}

# The one place anything is deleted. The path has to be a direct child of the root this run
# checked, named like an entry, a real directory and not a link to one — and the root itself must
# still be that same real directory, because `rm` resolves every component before the last.
# Every expansion that reaches `rm` is `${var:?}`, so an empty or unset name cannot widen it.
remove_entry() {  # 0 removed or already gone, 1 could not remove, 2 not an entry
  local root=${SCRATCH_ROOT_REAL:?} path=${1:?} name attempt
  name=${path##*/}
  [ "$path" = "$root/$name" ] || return 2
  [[ $name =~ $ENTRY_RE ]] || return 2
  [ ! -L "$root" ] && [ "$(cd -P -- "$root" 2>/dev/null && pwd -P)" = "$root" ] || return 2
  [ ! -L "$root/$name" ] || return 2
  [ -e "$root/$name" ] || return 0
  [ -d "$root/$name" ] || return 2
  for attempt in 1 2 3; do
    rm -rf -- "${root:?}/${name:?}" 2>/dev/null
    [ -e "$root/$name" ] || [ -L "$root/$name" ] || return 0
    # A read-only directory inside — a module cache, a fixture — stops rm. Give the owner write
    # permission back, following no link (-P), and try again.
    chmod -R -P u+w "${root:?}/${name:?}" 2>/dev/null
    [ "$attempt" = 3 ] || sleep 1
  done
  return 1
}

# ---- snapshot-run --------------------------------------------------------------------------------

# Runs "$@" inside the snapshot with no repository environment inherited from the caller. Inside a
# hook, GIT_DIR and GIT_WORK_TREE would otherwise point `git apply` at the real checkout.
in_tree() {
  (
    cd -- "$ENTRY/tree" || exit 1
    unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES \
      GIT_COMMON_DIR GIT_NAMESPACE GIT_PREFIX
    GIT_CEILING_DIRECTORIES=$ENTRY
    export GIT_CEILING_DIRECTORIES
    "$@"
  )
}

# Paths read NUL-separated from standard input, printed the same way, minus the ones the snapshot
# does not have. `-L` beside `-e`, because a link whose target is missing is still a path the copy
# holds; a path the overlay deleted is one it does not, and a deletion is part of the subject.
present_in_tree() {
  local path
  while IFS= read -r -d '' path; do
    [ -e "$ENTRY/tree/$path" ] || [ -L "$ENTRY/tree/$path" ] || continue
    printf '%s\0' "$path"
  done
}

# How many paths a NUL-separated list holds. Counting the separators rather than the lines, because
# a path may contain a newline and `wc -l` would then count one file twice.
count_nul() {  # reads the list on standard input
  local n
  n=$(tr -dc '\0' | wc -c) || return 1
  printf '%s' "$((n))"
}

# The recipe AGENTS.md used to print, step for step, with one correction measured on 2026-09-11:
# `git diff HEAD` refreshes the stat cache of the index it reads and writes that index back — with
# and without GIT_OPTIONAL_LOCKS=0, on git 2.38.1 — so the worktree subject reads a private copy of
# the index, and the shared index is never written.
materialise() {  # $1 subject, $2 repository top level
  local subject=$1 top=$2 index tree_id snapshot_tree have both
  mkdir -- "$ENTRY/tree" "$ENTRY/tmp" || return 1
  cd -- "$top" || return 1
  case $subject in
    worktree)
      index=$(git rev-parse --git-path index) || return 1
      case $index in /*) ;; *) index="$top/$index" ;; esac
      cp -- "$index" "$ENTRY/index" || return 1
      git archive HEAD | tar -x -C "$ENTRY/tree" || return 1
      # Staged and unstaged edits, deletions, modes and binary changes, without writing a stash
      # object under a .git that a Codex sandbox may not be allowed to write at all.
      GIT_INDEX_FILE="$ENTRY/index" git diff --binary --full-index --no-ext-diff HEAD \
        | in_tree git apply --allow-empty --whitespace=nowarn || return 1
      # The diff cannot carry untracked files, and a test another session wrote but has not
      # committed may be one the suite needs.
      GIT_INDEX_FILE="$ENTRY/index" git ls-files --others --exclude-standard -z \
        | tar --null -T - -cf - | tar -xf - -C "$ENTRY/tree" || return 1
      ;;
    index)
      tree_id=$(git write-tree) || return 1
      [ -n "$tree_id" ] || return 1
      git archive "$tree_id" | tar -x -C "$ENTRY/tree" || return 1
      ;;
  esac
  # `tools/check-version-strings.py` asks git for the files it scans, so a snapshot that is not a
  # repository fails closed with `version_scan_no_files` before a single check runs. Staging is
  # enough; nothing reads a commit.
  in_tree git init -q && in_tree git add -A || return 1
  # `git add -A` obeys the copy's own `.gitignore`, and a repository may track a file that matches
  # it. Measured on 2026-09-11 while landing a82f062d: the snapshot's index held 724 files where the
  # commit has 725, and the missing one was `tools/ubuntu-core-probe/Package.resolved`, tracked and
  # matching `.gitignore:17`. It was on disk the whole time — everything that asks git for the file
  # list, `git ls-files` and `tools/check-version-strings.py` among them, simply ran on a tree one
  # file short and went green. So the paths the subject is known to hold are added by name, and the
  # snapshot then has to prove it is the tree it was taken from before the command runs in it.
  case $subject in
    index)
      git ls-tree -r -z --name-only "$tree_id" \
        | in_tree git add -f --pathspec-from-file=- --pathspec-file-nul || return 1
      snapshot_tree=$(in_tree git write-tree) || return 1
      [ "$snapshot_tree" = "$tree_id" ] || die $EX_SNAPSHOT scratch_snapshot_incomplete \
        "the snapshot in $ENTRY/tree is tree $snapshot_tree, not the $tree_id it was taken from"
      ;;
    worktree)
      # There is no tree to compare against — the subject is an overlay nobody has written down —
      # so the weaker true thing is proved instead: every path the source repository tracks and the
      # copy has is in the copy's index. The list comes from the private index copy, so reading it
      # still writes nothing shared.
      GIT_INDEX_FILE="$ENTRY/index" git ls-files -z | present_in_tree > "$ENTRY/tracked" || return 1
      if [ -s "$ENTRY/tracked" ]; then
        in_tree git add -f --pathspec-from-file=- --pathspec-file-nul < "$ENTRY/tracked" || return 1
      fi
      in_tree git ls-files -z > "$ENTRY/staged" || return 1
      # A subset test that does not care what is in a path: the union is no larger than the
      # snapshot's own list exactly when the snapshot already holds every tracked path.
      have=$(sort -z -u < "$ENTRY/staged" | count_nul) || return 1
      both=$(cat -- "$ENTRY/tracked" "$ENTRY/staged" | sort -z -u | count_nul) || return 1
      [ "$both" = "$have" ] || die $EX_SNAPSHOT scratch_snapshot_incomplete \
        "the snapshot in $ENTRY/tree is missing $((both - have)) of the files $top tracks"
      ;;
  esac
}

signal_number() {
  case $1 in HUP) echo 1 ;; INT) echo 2 ;; *) echo 15 ;; esac
}

# A signal is forwarded to the command as TERM — a job started with `&` by a non-interactive shell
# ignores INT, measured on bash 3.2 even after `trap - INT` — and the tool waits for it, because
# removing a directory a live command is still writing into leaves half of it behind. A second
# signal while waiting ends the command with KILL.
scratch_signal() {
  RUN_SIGNAL=$1
  trap 'scratch_escalate' INT TERM HUP
  if [ -n "${CHILD_PID:-}" ]; then
    kill -TERM "$CHILD_PID" 2>/dev/null
    while kill -0 "$CHILD_PID" 2>/dev/null; do wait "$CHILD_PID" 2>/dev/null; done
    CHILD_PID=
  fi
  exit $((128 + $(signal_number "$1")))
}

scratch_escalate() {
  [ -z "${CHILD_PID:-}" ] || kill -KILL "$CHILD_PID" 2>/dev/null
}

# The EXIT trap, and so every way out of a snapshot run: the command's own exit, a refusal while
# materialising, and the `exit` a signal handler ends with.
scratch_cleanup() {
  local status=$?
  trap '' INT TERM HUP
  if [ -n "${ENTRY:-}" ]; then
    if [ "${RUN_KEEP:-0}" = 1 ] && [ "${RUN_FINISHED:-0}" = 1 ] && [ -z "${RUN_SIGNAL:-}" ]; then
      keep_entry || status=$EX_CLEANUP
    elif ! remove_entry "$ENTRY"; then
      printf '%s: scratch_cleanup_failed: could not remove %s (the run itself exited %s)\n' \
        "$prog" "$ENTRY" "$status" >&2
      status=$EX_CLEANUP
    fi
  fi
  if [ -n "${RUN_SIGNAL:-}" ] && [ "$status" != "$EX_CLEANUP" ]; then
    # Die of the same signal, so a calling script sees an interrupted child rather than one that
    # happened to exit 130 and carries on to its next line.
    trap - "$RUN_SIGNAL" EXIT
    kill -"$RUN_SIGNAL" $$
  fi
  exit "$status"
}

# A kept snapshot becomes a directory a session keeps across tool calls, so it is owned the way an
# entry from `new` is: by the assistant above this process, or by nobody with a deadline.
keep_entry() {
  local pid owner keep_until
  keep_until=$(( $(date +%s) + RUN_TTL * 3600 ))
  if pid=$(assistant_ancestor) && owner=$(owner_json "$pid"); then :; else owner=null; fi
  if ! write_marker "$ENTRY" "$RUN_PURPOSE" "$CREATED_AT" "$owner" "$keep_until"; then
    printf '%s: scratch_cleanup_failed: could not record keep_until in %s, so it is removed instead\n' \
      "$prog" "$ENTRY" >&2
    remove_entry "$ENTRY"
    return 1
  fi
  printf '%s: kept %s until %s\n' "$prog" "$ENTRY" \
    "$(LC_ALL=C TZ=UTC date -r "$keep_until" '+%Y-%m-%dT%H:%M:%SZ')" >&2
}

cmd_snapshot_run() {
  local subject= root_arg= keep=0 ttl= top owner keep_until status
  while [ $# -gt 0 ]; do
    case $1 in
      --subject) [ $# -ge 2 ] || die $EX_USAGE scratch_usage "--subject needs worktree or index"
                 subject=$2; shift 2 ;;
      --root) [ $# -ge 2 ] || die $EX_USAGE scratch_usage "--root needs a directory"
              root_arg=$2; shift 2 ;;
      --keep) keep=1; shift ;;
      --ttl-hours) [ $# -ge 2 ] || die $EX_USAGE scratch_usage "--ttl-hours needs a number"
                   ttl=$2; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      --) shift; break ;;
      *) die $EX_USAGE scratch_usage "unexpected '$1' before --" ;;
    esac
  done
  case $subject in
    worktree|index) ;;
    '') die $EX_USAGE scratch_usage "--subject is required: worktree (anyone) or index (root only)" ;;
    *) die $EX_USAGE scratch_usage "--subject is worktree or index, not '$subject'" ;;
  esac
  [ $# -gt 0 ] || die $EX_USAGE scratch_usage "no command after --"
  if [ -n "$ttl" ]; then
    [ "$keep" = 1 ] || die $EX_USAGE scratch_usage "--ttl-hours only means something with --keep"
    check_ttl "$ttl"
    ttl=$((10#$ttl))
  fi
  top=$(git rev-parse --show-toplevel 2>/dev/null) && [ -n "$top" ] \
    || die $EX_SNAPSHOT scratch_not_in_git "not inside a git working tree, so there is nothing to snapshot"
  git rev-parse --verify -q HEAD >/dev/null \
    || die $EX_SNAPSHOT scratch_not_in_git "$top has no HEAD commit to snapshot"
  open_root "${root_arg:-${CLAWDLINE_SCRATCH_ROOT:-$SCRATCH_DEFAULT_ROOT}}" create

  RUN_KEEP=$keep
  RUN_TTL=${ttl:-$KEEP_DEFAULT_TTL_HOURS}
  RUN_PURPOSE="snapshot-$subject"
  RUN_FINISHED=0
  RUN_SIGNAL=
  CHILD_PID=
  ENTRY=
  CREATED_AT=$(date +%s)
  # The owner of a snapshot run is this process: it removes the entry when it exits, and if it is
  # killed outright the marker is what tells the broker the owner is gone.
  if owner=$(owner_json $$); then
    keep_until=null
  else
    owner=null
    keep_until=$((CREATED_AT + SNAPSHOT_UNOWNED_TTL_HOURS * 3600))
  fi

  trap scratch_cleanup EXIT
  trap 'scratch_signal INT' INT
  trap 'scratch_signal TERM' TERM
  trap 'scratch_signal HUP' HUP

  make_entry "$RUN_PURPOSE"
  write_marker "$ENTRY" "$RUN_PURPOSE" "$CREATED_AT" "$owner" "$keep_until" \
    || die $EX_ROOT scratch_root_uncreatable "cannot write the marker in $ENTRY"
  printf '%s: snapshot of the %s at %s/tree, removed when this run ends\n' "$prog" "$subject" "$ENTRY" >&2
  materialise "$subject" "$top" \
    || die $EX_SNAPSHOT scratch_snapshot_failed "could not copy the $subject of $top into $ENTRY"

  # In the background so a signal is handled while the command runs rather than after it; `<&0`
  # because a background job's standard input is otherwise /dev/null.
  (
    cd -- "$ENTRY/tree" || exit $EX_SNAPSHOT
    TMPDIR="$ENTRY/tmp"
    export TMPDIR
    exec "$@"
  ) <&0 &
  CHILD_PID=$!
  wait "$CHILD_PID"
  status=$?
  CHILD_PID=
  RUN_FINISHED=1
  exit "$status"
}

# ---- new, remove ---------------------------------------------------------------------------------

new_abandoned() {
  local status=$?
  [ -z "${ENTRY:-}" ] || [ "${NEW_DONE:-0}" = 1 ] || remove_entry "$ENTRY"
  exit "$status"
}

cmd_new() {
  local purpose= root_arg= ttl= pid owner keep_until created
  while [ $# -gt 0 ]; do
    case $1 in
      --root) [ $# -ge 2 ] || die $EX_USAGE scratch_usage "--root needs a directory"
              root_arg=$2; shift 2 ;;
      --ttl-hours) [ $# -ge 2 ] || die $EX_USAGE scratch_usage "--ttl-hours needs a number"
                   ttl=$2; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      -*) die $EX_USAGE scratch_usage "unexpected '$1'" ;;
      *) [ -z "$purpose" ] || die $EX_USAGE scratch_usage "new takes one purpose, and got '$purpose' and '$1'"
         purpose=$1; shift ;;
    esac
  done
  [[ $purpose =~ $PURPOSE_RE ]] \
    || die $EX_USAGE scratch_usage "the purpose '$purpose' must match [a-z0-9][a-z0-9-]{0,39}"
  if [ -n "$ttl" ]; then
    check_ttl "$ttl"
    ttl=$((10#$ttl))
  fi
  created=$(date +%s)
  if pid=$(assistant_ancestor) && owner=$(owner_json "$pid"); then
    keep_until=null
    [ -z "$ttl" ] || keep_until=$((created + ttl * 3600))
  else
    [ -n "$ttl" ] || die $EX_USAGE scratch_ttl_required \
      "no claude or codex process is above this one, so nothing can own the entry; pass --ttl-hours 1-24"
    owner=null
    keep_until=$((created + ttl * 3600))
  fi
  open_root "${root_arg:-${CLAWDLINE_SCRATCH_ROOT:-$SCRATCH_DEFAULT_ROOT}}" create
  NEW_DONE=0
  ENTRY=
  trap new_abandoned EXIT
  make_entry "$purpose"
  write_marker "$ENTRY" "$purpose" "$created" "$owner" "$keep_until" \
    || die $EX_ROOT scratch_root_uncreatable "cannot write the marker in $ENTRY"
  NEW_DONE=1
  printf '%s\n' "$ENTRY"
}

cmd_remove() {
  local target= root_arg= name parent parent_real content status
  while [ $# -gt 0 ]; do
    case $1 in
      --root) [ $# -ge 2 ] || die $EX_USAGE scratch_usage "--root needs a directory"
              root_arg=$2; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) [ -z "$target" ] || die $EX_USAGE scratch_usage "remove takes one path, and got '$target' and '$1'"
         target=$1; shift ;;
    esac
  done
  [ -n "$target" ] || die $EX_USAGE scratch_usage "remove needs the path of an entry"
  open_root "${root_arg:-${CLAWDLINE_SCRATCH_ROOT:-$SCRATCH_DEFAULT_ROOT}}" existing
  case $target in /*) ;; *) target="$PWD/$target" ;; esac
  while [ "$target" != / ] && [ "${target%/}" != "$target" ]; do target=${target%/}; done
  name=${target##*/}
  parent=${target%/*}
  [ -n "$parent" ] || parent=/
  parent_real=$(cd -P -- "$parent" 2>/dev/null && pwd -P) && [ "$parent_real" = "$SCRATCH_ROOT_REAL" ] \
    || die $EX_REFUSED scratch_not_under_root "$target is not directly under $SCRATCH_ROOT"
  [[ $name =~ $ENTRY_RE ]] \
    || die $EX_REFUSED scratch_not_an_entry "$name is not named <purpose>.<random>"
  [ ! -L "$SCRATCH_ROOT_REAL/$name" ] \
    || die $EX_REFUSED scratch_not_an_entry "$target is a symbolic link, and an entry is a directory"
  [ -d "$SCRATCH_ROOT_REAL/$name" ] || die $EX_REFUSED scratch_not_an_entry "$target is not a directory"
  [ -O "$SCRATCH_ROOT_REAL/$name" ] \
    || die $EX_REFUSED scratch_not_an_entry "$target is not owned by uid $(id -u)"
  content=$(read_marker "$SCRATCH_ROOT_REAL/$name")
  case $? in
    0) ;;
    1) die $EX_REFUSED scratch_marker_missing "$target has no $SCRATCH_MARKER; an entry without one is unknown, and unknown is not removed" ;;
    *) die $EX_REFUSED scratch_marker_unknown "$target has a marker that is not version 1; unknown is not removed" ;;
  esac
  if [[ $content =~ $OWNER_RE ]]; then
    local owner_pid=${BASH_REMATCH[1]} owner_start=${BASH_REMATCH[2]}
    owner_liveness "$owner_pid" "$owner_start"
    case $? in
      1) ;;
      0) is_ancestor "$owner_pid"
         case $? in
           0) ;;
           1) die $EX_REFUSED scratch_owner_live \
                "$target belongs to process $owner_pid, which is still running and is not above this one" ;;
           *) die $EX_REFUSED scratch_owner_unknown \
                "$target belongs to process $owner_pid, which is still running, and the processes above this one cannot be read to say whether it is one of them; unknown is not removed" ;;
         esac ;;
      *) die $EX_REFUSED scratch_owner_unknown \
           "$target belongs to process $owner_pid, and whether it is still running cannot be read: the system has not said there is no such process, nor shown when a process under that pid started; unknown is not removed" ;;
    esac
  fi
  remove_entry "$SCRATCH_ROOT_REAL/$name"
  status=$?
  [ "$status" = 0 ] || die $EX_CLEANUP scratch_cleanup_failed "could not remove $target"
}

case ${1-} in
  snapshot-run) shift; cmd_snapshot_run ${1+"$@"} ;;
  new) shift; cmd_new ${1+"$@"} ;;
  remove) shift; cmd_remove ${1+"$@"} ;;
  -h|--help|help) usage ;;
  '') usage >&2; exit $EX_USAGE ;;
  *) usage >&2; die $EX_USAGE scratch_usage "unknown subcommand '$1'" ;;
esac
