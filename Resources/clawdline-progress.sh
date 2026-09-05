#!/bin/bash
# Say how far a long-running command has got, to somebody who is not looking at this terminal.
#
# `./test.sh` is 288 seconds and `./build.sh` is not much quicker, and until the seventh project
# status file landed the person who started one had nothing to look at anywhere but the terminal
# they started it in. `run-<key>.json` fixed that for those two scripts. **This file is what makes
# it a facility rather than a format**: a lint run, a data import, a migration, a video encode all
# fit, because `label` and `phase` are free text and nothing in the record is about tests or builds.
#
# Two forms, and the first is the one to reach for:
#
#     clawdline-progress run --label lint --typical 120 -- ./scripts/lint.sh
#
# **The helper is the parent process there.** The exit status is its own, the signals are its own,
# the caller installs no traps at all, and the whole class of bug below cannot occur.
#
#     . "$CLAWDLINE_PROGRESS"
#     progress_start --label test --typical 288
#     progress_phase compiling
#     # no explicit finish: the traps progress_start installs decide from the exit status
#
# The second form is for a script that wants its own phases. **The traps live here, written once**,
# because a dedicated agent with a full brief got them wrong on its first attempt and so did a
# second agent independently, both on this machine's bash 3.2.57:
#
#   * `EXIT` alone reads a killed run as a clean one, because `$?` is 0 inside that handler.
#   * `set -e` does not fire `ERR` for a failure inside a function, and every deliberate `exit 1`
#     misses it too.
#   * A handler that returns instead of exiting lets a `TERM`ed script carry on and declare success.
#
# Where it lives: `Resources/clawdline-progress.sh` in a checkout, `Contents/Resources/` in the app
# bundle. `test.sh` and `build.sh` source the checkout's copy, so running the suite never depends on
# the app being installed. Point `CLAWDLINE_PROGRESS` at whichever copy you have.
#
# >>> clawdline run file >>>
# What a run is doing, where a person can see it while it happens:
# `~/.claude/statusline-cache/run-<key>.json`.
#
# **The seventh project status file**, beside `ghrun-`, `backlog-`, `health-` and `milestone-`, and
# it reuses their rules on purpose so this project has one set of rules rather than two: every field
# optional except `state`; a state a reader does not recognise means *draw nothing*, never a cross;
# and a producer writes a temporary name and renames it into place, so a reader sees the whole of
# the old file or the whole of the new one and never half of each.
#
# **Keyed by working directory, not by git remote.** `ghrun-` is keyed by the remote, and this
# machine routinely has several worktrees of one repository compiling at once — one remote, several
# trees, and a reader with no way to tell them apart. `<key>` is `$PWD` with every `/` turned into a
# `-` and **nothing truncated**: the same shape `backlog-`, `health-` and `milestone-` already use,
# which is `ProjectStatus.key(forPath:)` in Swift and `path_key()` in claude-bestiary. A truncating
# key was removed from all three implementations on 2026-09-05 because `[-48:]` is lossy.
#
# **The one rule that is new lives in the reader**: a `running` row whose `updated_at` is older than
# `stale_after` is ignored. Nothing polls this file and nothing tidies up after the producer, so a
# `kill -9`'d run would otherwise sit in the bar for ever. That ceiling is also why there is no
# `producer` field — `ghrun-` needs one because two writers compete for it; this file has one writer
# and a staleness rule instead.

clawdline_run_file_json() {
    # Free text into a JSON string. A tree path may hold a quote or a backslash — this machine has
    # directories with spaces in them already — and a file that does not parse is drawn as nothing,
    # which is the failure that looks like no failure. Backslashes first, then quotes: the other order
    # escapes the escapes.
    printf '%s' "$1" | LC_ALL=C tr -d '\000-\037' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

clawdline_run_file_holder() {
    # Which session started it. A terminal identity is worth more here than a username, for the reason
    # the suite lock's holder line gives: it is somebody to go and ask.
    local who where
    who="${USER:-$(id -un 2>/dev/null || echo unknown)}"
    if [ -n "${ITERM_SESSION_ID:-}" ]; then
        where="iTerm2 ${ITERM_SESSION_ID#*:}"
    elif [ -n "${TMUX_PANE:-}" ]; then
        where="tmux ${TMUX_PANE}"
    else
        where="no tty"
    fi
    printf '%s (%s)' "$who" "$where"
}

clawdline_run_file_write() {
    # One state, one optional phase, one whole file. Written to a temporary name **in the same
    # directory** and renamed into place, because a rename within a directory is atomic and a reader
    # racing this write has to see one complete file or the other — the rule `ghrun-` already keeps.
    #
    # It never fails the run it is reporting on. A missing `$HOME`, an unwritable cache directory or a
    # full disk costs the person the bar and nothing else, so every path here ends `return 0`.
    local state=$1 phase=${2:-} temp
    [ -n "${CLAWDLINE_RUN_FILE:-}" ] || return 0
    mkdir -p "$CLAWDLINE_RUN_DIR" 2>/dev/null || return 0
    temp="$CLAWDLINE_RUN_FILE.$$.tmp"
    {
        printf '{"state": "%s"' "$state"
        printf ', "label": "%s"' "$(clawdline_run_file_json "$CLAWDLINE_RUN_LABEL")"
        [ -z "$phase" ] || printf ', "phase": "%s"' "$(clawdline_run_file_json "$phase")"
        printf ', "started_at": %s' "$CLAWDLINE_RUN_STARTED"
        [ -z "$CLAWDLINE_RUN_TYPICAL" ] || printf ', "typical_seconds": %s' "$CLAWDLINE_RUN_TYPICAL"
        printf ', "updated_at": %s' "$(date +%s)"
        printf ', "stale_after": %s' "$CLAWDLINE_RUN_STALE_AFTER"
        [ -z "$CLAWDLINE_RUN_LOG" ] || printf ', "log": "%s"' "$(clawdline_run_file_json "$CLAWDLINE_RUN_LOG")"
        printf ', "holder": "%s"' "$(clawdline_run_file_json "$CLAWDLINE_RUN_HOLDER")"
        printf ', "tree": "%s"' "$(clawdline_run_file_json "$PWD")"
        printf '}\n'
    } > "$temp" 2>/dev/null || { rm -f "$temp" 2>/dev/null || true; return 0; }
    mv -f "$temp" "$CLAWDLINE_RUN_FILE" 2>/dev/null || rm -f "$temp" 2>/dev/null || true
    return 0
}

clawdline_run_file_phase() {
    # A phase boundary: still running, doing something else now. `phase` is drawn verbatim in place of
    # the percentage, so these words are for a person and are never translated.
    clawdline_run_file_write running "$1"
    return 0
}

clawdline_run_file_clear() {
    # Take the row away entirely. Neither script calls it — a finished run leaves `ok` or `fail`
    # behind on purpose, and that is what a reader draws — so it is here for a person with a row from
    # a run that no longer exists, and for the suites that drive this file.
    rm -f "${CLAWDLINE_RUN_FILE:-}" 2>/dev/null || true
    return 0
}

clawdline_run_file_finish() {
    # The last write, and only ever one of them: the signal handlers below exit, an exit runs whatever
    # EXIT handler the script keeps, and that handler calls this too. Without the guard the second
    # call would overwrite a `fail` with an `ok` a moment later.
    [ "${CLAWDLINE_RUN_FINISHED:-0}" = 0 ] || return 0
    CLAWDLINE_RUN_FINISHED=1
    clawdline_run_file_write "$1" ""
    return 0
}

clawdline_run_file_exit() {
    # **Composed into whichever EXIT handler the sourcing script already keeps, rather than left as
    # the trap this file arms: bash keeps exactly one EXIT trap and a second `trap … EXIT` silently
    # replaces the first.** In `test.sh` a naive second trap would throw away the machine lock's
    # release on every run, which is the accident its own comments record; in `build.sh` there are
    # four EXIT traps in sequence. `progress_start` arms the first one so that the window before a
    # script's own handler is closed, and every replacement below it is a superset.
    #
    # And it is the EXIT path rather than the ERR trap that carries most of what goes wrong here.
    # Measured on this Mac on 2026-09-05: under `set -e`, a command failing *inside a function* ends
    # the script without firing ERR at all unless `set -E` is also on — and every deliberate
    # `exit 1` in these two scripts misses ERR by construction.
    local status=${1:-0}
    case "$status" in
        0) clawdline_run_file_finish ok ;;
        *) clawdline_run_file_finish fail ;;
    esac
    return 0
}

clawdline_run_file_signal() {
    # **The `exit` is the point of this function.** Without it a `TERM` handler returns into the
    # script, which carries on from where it was interrupted and finishes by declaring success. That
    # was measured while claude-bestiary's `docs/producers.md` was being written; it is not to be
    # rediscovered here.
    local status=${1:-1}
    # A handler that exits 0 would report the interruption as a clean finish. `ERR` cannot deliver a
    # zero status, but a failure that reports success is exactly what this function exists to prevent.
    case "$status" in "" | 0) status=1 ;; esac
    clawdline_run_file_finish fail
    exit "$status"
}

clawdline_run_file_start() {
    # Open the row and arm the traps. Everything is optional; what is not passed is not written.
    #
    # **The environment wins over the flag**, for all four knobs, because that is how the block this
    # replaces behaved and it is what lets a harness redirect a run without editing the script.
    local label='' typical='' stale='' log='' digits
    while [ $# -gt 0 ]; do
        case "$1" in
            --label)       [ $# -ge 2 ] || { clawdline_progress_usage "--label needs a value"; return 64; }; label=$2; shift 2 ;;
            --label=*)     label=${1#--label=}; shift ;;
            --typical)     [ $# -ge 2 ] || { clawdline_progress_usage "--typical needs a value"; return 64; }; typical=$2; shift 2 ;;
            --typical=*)   typical=${1#--typical=}; shift ;;
            --stale-after) [ $# -ge 2 ] || { clawdline_progress_usage "--stale-after needs a value"; return 64; }; stale=$2; shift 2 ;;
            --stale-after=*) stale=${1#--stale-after=}; shift ;;
            --log)         [ $# -ge 2 ] || { clawdline_progress_usage "--log needs a value"; return 64; }; log=$2; shift 2 ;;
            --log=*)       log=${1#--log=}; shift ;;
            --)            shift; break ;;
            *)             clawdline_progress_usage "progress_start: unknown option $1"; return 64 ;;
        esac
    done

    CLAWDLINE_RUN_DIR="${CLAWDLINE_STATUS_DIR:-${HOME:-}/.claude/statusline-cache}"
    # What this run is called. `label` is producer text drawn verbatim in every language, so it adds
    # no sentence anybody has to translate. A sourced script that passes none is named after itself.
    CLAWDLINE_RUN_LABEL="${CLAWDLINE_RUN_LABEL:-${label:-$(basename "$0" .sh)}}"
    CLAWDLINE_RUN_FILE="$CLAWDLINE_RUN_DIR/run-$(printf '%s' "$PWD" | tr '/' '-').json"
    CLAWDLINE_RUN_STARTED=$(date +%s)
    # 900 is what a reader with no field to read uses anyway; it is written out because a file that
    # says what it means costs twelve bytes and saves the next reader a trip to the documentation.
    CLAWDLINE_RUN_STALE_AFTER="${CLAWDLINE_RUN_STALE_AFTER:-${stale:-900}}"
    # **Validated for the same reason `typical_seconds` is**: this value is interpolated into the file
    # with `%s`, so anything that is not a JSON number makes the whole row unparseable — and a row that
    # does not parse is drawn as nothing at all, which looks exactly like no run. The contract's rule
    # for the reader is that a malformed value is an absent one, and an absent `stale_after` has a
    # documented default; the producer follows the same rule rather than a second one.
    #
    # The sign is split off so one pattern can judge the digits. What JSON accepts is `-5`, `0` and
    # `900`; what it refuses — and a person would not think twice about — is `+5`, `007` and `5.5`.
    # **`0` is kept, and that is a decision rather than an oversight**: a producer that writes `0` means
    # *expire immediately*, and treating it as missing would be the falsy-therefore-default accident
    # this format has already ruled out once. A negative is a number too, and the reader has a
    # documented answer for it, so it travels rather than being replaced.
    digits="${CLAWDLINE_RUN_STALE_AFTER#-}"
    case "$digits" in
        0) ;;
        "" | *[!0-9]* | 0*) CLAWDLINE_RUN_STALE_AFTER=900 ;;
    esac
    # **How long this usually takes, and it is optional on purpose.** `test.sh` passes `--typical 288`
    # — one green run on 2026-09-03, receipt `8353 checks passed`, in a detached worktree pinned at
    # `d97d0afb`, written up in `docs/suite-runtime.md`. **Nobody has ever measured `./build.sh`**, so
    # it passes none and no `typical_seconds` is written at all: an invented number is
    # indistinguishable from a measured one to every reader of this file, so nothing here invents one.
    CLAWDLINE_RUN_TYPICAL="${CLAWDLINE_RUN_TYPICAL:-$typical}"
    # A `typical_seconds` that is not a number would be a file that does not parse. Same three
    # patterns the staleness ceiling uses: a non-digit anywhere, a leading zero, or nothing.
    case "$CLAWDLINE_RUN_TYPICAL" in "" | *[!0-9]* | 0*) CLAWDLINE_RUN_TYPICAL="" ;; esac
    CLAWDLINE_RUN_HOLDER="${CLAWDLINE_RUN_HOLDER:-$(clawdline_run_file_holder)}"
    # The log this run is writing, once it has one. `test.sh` sets it beside its own `$LOG`, so the
    # early phases carry no `log` and every phase after it does; `build.sh` has no log and sets none.
    CLAWDLINE_RUN_LOG="${CLAWDLINE_RUN_LOG:-$log}"
    CLAWDLINE_RUN_FINISHED=0

    # **All four traps, and the EXIT one is not belt and braces.** Measured on this Mac on
    # 2026-09-05: a killed bash script still runs its EXIT trap and `$?` inside it is **0**, so a
    # finish composed into EXIT alone writes a killed run down as a clean one. And no ERR trap ever
    # sees a deliberate `exit`, so without the EXIT trap a guard that stops on `exit 1` leaves a row
    # saying `running` for the reader's staleness ceiling to retire fifteen minutes later.
    #
    # A sourcing script that keeps its own EXIT handler **replaces** this trap rather than joining it
    # — bash keeps exactly one — so each replacement must call `clawdline_run_file_exit` and be a
    # superset of this line. `Tests/run-file-producer.mjs` holds both scripts to that.
    trap 'clawdline_run_file_signal "$?"' ERR
    trap 'clawdline_run_file_signal 130' INT
    trap 'clawdline_run_file_signal 143' TERM
    trap 'clawdline_run_file_exit "$?"' EXIT

    clawdline_run_file_write running ""
    return 0
}

# The names a script that sources this file uses. The `clawdline_run_file_` spellings above are the
# ones `test.sh` and `build.sh` compose into their own EXIT handlers, and they stay as they are.
progress_start()  { clawdline_run_file_start "$@"; }
progress_phase()  { clawdline_run_file_phase "$@"; }
progress_finish() { clawdline_run_file_finish "$@"; }
progress_clear()  { clawdline_run_file_clear "$@"; }
# <<< clawdline run file <<<

clawdline_progress_usage() {
    [ $# -eq 0 ] || echo "clawdline-progress: $1" >&2
    cat >&2 <<'USAGE'
usage: clawdline-progress run [--label NAME] [--typical SECONDS]
                              [--stale-after SECONDS] [--log PATH] [--] COMMAND [ARG...]

Runs COMMAND and writes a progress row to $CLAWDLINE_STATUS_DIR (default
~/.claude/statusline-cache) keyed by the working directory, so Clawdline's bar and a terminal
status line can say what is happening while it happens. The row ends `ok` or `fail` from
COMMAND's own exit status, and this command exits on that status too.

Or, inside a script that wants its own phases:

    . "$CLAWDLINE_PROGRESS"
    progress_start --label test --typical 288
    progress_phase compiling

--typical is optional and nothing is invented for it: with no measurement, no typical_seconds is
written at all.
USAGE
    return 0
}

clawdline_progress_run() {
    local -a opts cmd
    local status label_given=0 first
    opts=()
    while [ $# -gt 0 ]; do
        case "$1" in
            --) shift; break ;;
            --label|--label=*) label_given=1; opts[${#opts[@]}]=$1
                case "$1" in --label) [ $# -ge 2 ] || { clawdline_progress_usage "--label needs a value"; return 64; }
                    opts[${#opts[@]}]=$2; shift ;; esac
                shift ;;
            --typical|--stale-after|--log)
                [ $# -ge 2 ] || { clawdline_progress_usage "$1 needs a value"; return 64; }
                opts[${#opts[@]}]=$1; opts[${#opts[@]}]=$2; shift 2 ;;
            --typical=*|--stale-after=*|--log=*) opts[${#opts[@]}]=$1; shift ;;
            -*) clawdline_progress_usage "unknown option: $1"; return 64 ;;
            *) break ;;
        esac
    done
    cmd=("$@")
    [ ${#cmd[@]} -gt 0 ] || { clawdline_progress_usage "run needs a command to run"; return 64; }
    # **The label a wrapped command gets by default is the command's own name**, not this script's.
    # `$0` here is `clawdline-progress.sh`, and a bar row reading `clawdline-progress` would say
    # nothing about what is taking the time.
    if [ "$label_given" = 0 ]; then
        first=${cmd[0]##*/}
        opts[${#opts[@]}]="--label"
        opts[${#opts[@]}]="${first%.sh}"
    fi
    clawdline_run_file_start ${opts[@]+"${opts[@]}"} || return $?

    # **This is the whole of why the wrapper form is the one to reach for.** The command runs in the
    # foreground of this shell, so its status is this shell's `$?` and the signals that reach it
    # reach the traps above; the caller writes no handler and can get none of them wrong. `set +e`
    # is here so that a failing command reaches the `exit` below rather than the errexit path — the
    # row it leaves is the same either way, and this way the status is read once, in one place.
    set +e
    "${cmd[@]}"
    status=$?
    set -e
    # No `clawdline_run_file_finish` call: the EXIT trap `progress_start` armed decides from the
    # status, which is the same single way out a sourced script gets.
    exit "$status"
}

clawdline_progress_main() {
    case "${1:-}" in
        run) shift; clawdline_progress_run "$@" ;;
        -h | --help | help) clawdline_progress_usage; return 0 ;;
        "") clawdline_progress_usage "say what to run"; return 64 ;;
        *) clawdline_progress_usage "unknown command: $1"; return 64 ;;
    esac
}

# Sourced or run? `${BASH_SOURCE[0]}` is this file either way; `$0` is this file only when it was
# executed. Sourcing must define the functions and do nothing else — a script that sources this and
# then finds a row already open for it would be reporting a run that had not started.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    set -uo pipefail
    clawdline_progress_main "$@"
    exit $?
fi
