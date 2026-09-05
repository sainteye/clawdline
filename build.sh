#!/bin/bash
# Builds Clawdline.app. No Xcode project, no package manager: a few .swift files and one .js,
# compiled straight by swiftc — one less layer of "what does the build config actually say".
set -euo pipefail
cd "$(dirname "$0")"
. tools/swift-source-manifest.sh
verify_swift_source_manifest production

# What this build is doing, for anybody who is not looking at this terminal. The same block
# `test.sh` carries, byte for byte; only the label and the phases differ, and the label is the
# name of this script.
# >>> clawdline run file >>>
# What this run is doing, where a person can see it while it happens:
# `~/.claude/statusline-cache/run-<key>.json`. `./test.sh` is 288 seconds and `./build.sh` is not
# much quicker, and until now the person who started one had nothing to look at anywhere but the
# terminal they started it in.
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
# `-`: the same shape `backlog-`, `health-` and `milestone-` already use, which is
# `ProjectStatus.key(forPath:)` on the Swift side.
#
# **The one rule that is new lives in the reader**: a `running` row whose `updated_at` is older than
# `stale_after` is ignored. Nothing polls this file and nothing tidies up after this script, so a
# `kill -9`'d run would otherwise sit in the bar for ever. That ceiling is also why there is no
# `producer` field — `ghrun-` needs one because two writers compete for it; this file has one writer
# and a staleness rule instead.
#
# **This block is the same text in `test.sh` and in `build.sh`**, and `Tests/run-file-producer.mjs`
# compares the two byte for byte rather than trusting that it stayed so. What differs between the
# two scripts is the label and the phases; the phases are calls outside the block, and the label is
# derived rather than passed, so that one text can say `test` in one file and `build` in the other.
CLAWDLINE_RUN_DIR="${CLAWDLINE_STATUS_DIR:-${HOME:-}/.claude/statusline-cache}"
# The name of the script that is running: `test` here, `build` there, and whatever a harness calls
# its copy. `label` is producer text drawn verbatim in every language, so this adds no sentence
# anybody has to translate.
CLAWDLINE_RUN_LABEL="${CLAWDLINE_RUN_LABEL:-$(basename "$0" .sh)}"
CLAWDLINE_RUN_FILE="$CLAWDLINE_RUN_DIR/run-$(printf '%s' "$PWD" | tr '/' '-').json"
CLAWDLINE_RUN_STARTED=$(date +%s)
# 900 is what a reader with no field to read uses anyway; it is written out because a file that says
# what it means costs twelve bytes and saves the next reader a trip to the documentation.
CLAWDLINE_RUN_STALE_AFTER="${CLAWDLINE_RUN_STALE_AFTER:-900}"
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
clawdline_run_stale_digits="${CLAWDLINE_RUN_STALE_AFTER#-}"
case "$clawdline_run_stale_digits" in
  0) ;;
  "" | *[!0-9]* | 0*) CLAWDLINE_RUN_STALE_AFTER=900 ;;
esac
unset clawdline_run_stale_digits
# **How long this usually takes — measured, and here is where it was measured.** 288 s is one green
# `./test.sh` on 2026-09-03, receipt `8353 checks passed`, in a detached worktree pinned at
# `d97d0afb`; a second run in the shared tree two changes older read 289.55 s with the same four
# boundaries. Both are in `docs/suite-runtime.md`, which is also where the phase names below come
# from. **Nobody has ever measured `./build.sh`**, so it writes no `typical_seconds` at all: the
# field is optional, and an invented number is indistinguishable from a measured one to every reader
# of this file.
case "$CLAWDLINE_RUN_LABEL" in
  test) CLAWDLINE_RUN_TYPICAL="${CLAWDLINE_RUN_TYPICAL:-288}" ;;
  *) CLAWDLINE_RUN_TYPICAL="${CLAWDLINE_RUN_TYPICAL:-}" ;;
esac
# A `typical_seconds` that is not a number would be a file that does not parse, and a file that does
# not parse is drawn as nothing at all — which looks exactly like no run. Same three patterns the
# compile ceiling uses: a non-digit anywhere, a leading zero, or nothing.
case "$CLAWDLINE_RUN_TYPICAL" in "" | *[!0-9]* | 0*) CLAWDLINE_RUN_TYPICAL="" ;; esac
# Which session started it. A terminal identity is worth more here than a username, for the reason
# the suite lock's holder line gives: it is somebody to go and ask.
clawdline_run_file_holder() {
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
CLAWDLINE_RUN_HOLDER="${CLAWDLINE_RUN_HOLDER:-$(clawdline_run_file_holder)}"
# The log this run is writing, once it has one. `test.sh` sets it beside its own `$LOG`, so the
# early phases carry no `log` and every phase after it does; `build.sh` has no log and never sets it.
CLAWDLINE_RUN_LOG="${CLAWDLINE_RUN_LOG:-}"
CLAWDLINE_RUN_FINISHED=0

clawdline_run_file_json() {
  # Free text into a JSON string. A tree path may hold a quote or a backslash — this machine has
  # directories with spaces in them already — and a file that does not parse is drawn as nothing,
  # which is the failure that looks like no failure. Backslashes first, then quotes: the other order
  # escapes the escapes.
  printf '%s' "$1" | LC_ALL=C tr -d '\000-\037' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
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
  # a run that no longer exists, and for the suite that drives this block.
  rm -f "$CLAWDLINE_RUN_FILE" 2>/dev/null || true
  return 0
}

clawdline_run_file_finish() {
  # The last write, and only ever one of them: the signal handlers below exit, an exit runs whatever
  # EXIT handler the script keeps, and that handler calls this too. Without the guard the second
  # call would overwrite a `fail` with an `ok` a moment later.
  [ "$CLAWDLINE_RUN_FINISHED" = 0 ] || return 0
  CLAWDLINE_RUN_FINISHED=1
  clawdline_run_file_write "$1" ""
  return 0
}

clawdline_run_file_exit() {
  # **Composed into whichever EXIT handler the script already keeps, rather than installed as a
  # trap of its own: bash keeps exactly one EXIT trap and a second `trap … EXIT` silently replaces
  # the first.** In `test.sh` that would throw away the machine lock's release on every run, which
  # is the accident its own comments record; in `build.sh` there are three EXIT traps in sequence.
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

trap 'clawdline_run_file_signal "$?"' ERR
trap 'clawdline_run_file_signal 130' INT
trap 'clawdline_run_file_signal 143' TERM
# <<< clawdline run file <<<

# The run file's own way out, from here rather than from the EXIT handler installed further down.
# **A window before the first EXIT trap is a window in which an `exit` is caught by nothing.** No
# ERR trap ever sees a deliberate `exit`, and both scripts have guards above their real handler
# that end that way — so a run that stopped there left a `running` row behind for the reader's
# staleness ceiling to retire fifteen minutes later, which reads as a run still going.
#
# Every EXIT trap installed below **replaces** this one rather than joining it: bash keeps exactly
# one, so each of them composes `clawdline_run_file_exit` and is a superset of this line.
# `Tests/run-file-producer.mjs` holds all of them to that, in both scripts.
trap 'clawdline_run_file_exit "$?"' EXIT

clawdline_run_file_phase preparing


# --- The machine-level heavy-compile lock ---------------------------------------------------
#
# One full Swift compile is the most expensive thing that happens on this Mac, and four
# `swift-frontend` processes have force-rebooted it. `/tmp/clawdline-suite.lock` is the truth:
# holding it is what `mkdir` says it is, which is what makes it work for a contributor with no
# Clawdline running at all, and what makes it the same lock `test.sh` takes for itself.
#
# **There was a broker lease in front of this directory** — a registry, a queue up to thirty-two
# deep, durable state across an app restart, and a liveness axis for waiters. It was removed on
# 2026-09-03; `docs/machine-resource-scheduling.md` records what it was and why it went. What is
# left is the 80% that was used on the night this was built: the directory, the record, the
# heartbeat, and a wait that names who is holding the slot.
#
# **Fail closed.** No lock means the compile does not run. There is no proceed-anyway, and
# nothing here ever ends anybody else's process.
# **One lock, one set of knob names.** Both scripts write `<lock>/holder.txt` and both read each
# other's, and until now each read its own spelling of the same dial: `CLAWDLINE_LEASE_DIR` here
# against `CLAWDLINE_SUITE_LOCK_DIR` in `test.sh`, `CLAWDLINE_LEASE_DEADLINE_SECONDS` here against
# `CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS` there. The defaults matched, so the ordinary path was
# right and nothing said otherwise — but `heartbeat_deadline` is a *record* field that both writers
# fill in and every reader prefers to its own, so somebody who tuned one spelling got two writers
# putting different numbers in one field, and a reader that believed whichever wrote last.
#
# `CLAWDLINE_SUITE_LOCK_*` is the canonical family, because it is the one `test.sh` already carries
# for the whole block. The `CLAWDLINE_LEASE_*` names keep working as aliases — somebody may have
# them exported — and the canonical name wins when both are set. The *variables* below keep their
# old names on purpose: they are what the body of this file and its harnesses read.
CLAWDLINE_LEASE_DIR="${CLAWDLINE_SUITE_LOCK_DIR:-${CLAWDLINE_LEASE_DIR:-/tmp/clawdline-suite.lock}}"
# `pgrep -x` matches the executable's own name, which is the physical backstop below. Measured in
# `test.sh`'s own comment against a live compile: a running `swiftc` shows up as `swift-frontend`
# under `pgrep -x`, and `pgrep -f` additionally counts anything that merely mentions the word.
CLAWDLINE_LEASE_COMPILER_PATTERN="${CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN:-${CLAWDLINE_LEASE_COMPILER_PATTERN:-swift-frontend}}"
CLAWDLINE_LEASE_POLL_SECONDS="${CLAWDLINE_SUITE_LOCK_POLL_SECONDS:-${CLAWDLINE_LEASE_POLL_SECONDS:-5}}"
CLAWDLINE_LEASE_ID=""
CLAWDLINE_LEASE_MODE=""
CLAWDLINE_LEASE_DONE=""
CLAWDLINE_LEASE_PHASE=""
CLAWDLINE_LEASE_PHASE_SINCE=""
CLAWDLINE_LEASE_LAST_COMPILING="never"
CLAWDLINE_LEASE_OWNER_STARTED=""
CLAWDLINE_LEASE_BEATING=1
CLAWDLINE_SUITE_JOBS="${CLAWDLINE_SUITE_JOBS:-}"
# Where the ceiling came from is decided by the marked compile-ceiling block further down, beside
# the invocation it feeds, because that block is the only thing that knows what it settled on.
# Deciding it up here was how this line came to say "unset" for a ceiling the environment had set:
# it took its answer from whichever branch of the acquire ran rather than from the ceiling itself.
CLAWDLINE_SUITE_JOBS_SOURCE="not settled yet"
CLAWDLINE_LEASE_WAIT_SECONDS="${CLAWDLINE_SUITE_LOCK_WAIT_SECONDS:-${CLAWDLINE_LEASE_WAIT_SECONDS:-1800}}"
# The same number `test.sh` uses. A reader prefers the deadline the holder recorded to its own, so
# a record that does not carry one leaves every reader guessing on this run's behalf — and this is
# the field the two spellings above were able to disagree in.
CLAWDLINE_LEASE_DEADLINE_SECONDS="${CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS:-${CLAWDLINE_LEASE_DEADLINE_SECONDS:-60}}"

clawdline_lease_field() {
  # The value of one `key=` line, or nothing — the same reader `test.sh` uses, because the record
  # is one format and a second parser is a second answer.
  local key=$1 file=$2
  [ -f "$file" ] || return 0
  awk -v key="$key" 'index($0, key "=") == 1 { print substr($0, length(key) + 2); exit }' "$file" 2>/dev/null
}

# --- Reading somebody else's hold, and handing it on -----------------------------------------
#
# **This half is `test.sh`'s, copied rather than reinvented**, because the two scripts take the
# same directory and a second set of rules for when a lock may be handed on is a second answer to
# the one question that matters here. What follows is `clawdline_suite_lock_probe_compilers`,
# `…_pid_identity`, `…_pid_verdict`, `…_identity_verdict`, `…_admission`, `…_take_over` and their
# two phrase helpers, with `clawdline_suite_lock_` renamed to `clawdline_lease_`. The rules do not
# move: admission is fail-closed, `unknown` blocks, the physical backstop is never waived, and
# nothing here ever signals a process it did not start.
#
# **Why it had to come across.** `test.sh` hands a dead holder's lock on after one renewal
# deadline; this script had no takeover path at all and no backstop, so its only two outcomes were
# `mkdir` succeeding and waiting out `CLAWDLINE_LEASE_WAIT_SECONDS` — 1800 seconds by default —
# before refusing. A `./test.sh` killed with SIGKILL, or a Mac force-rebooted by Jetsam mid-suite,
# leaves `/tmp/clawdline-suite.lock` behind; the next `./test.sh` reclaims it in 60 seconds and the
# next `./build.sh` waited half an hour and then declined to build. "The machine crashed and needs
# rebuilding" is the path `docs/machine-resource-scheduling.md` names as the one that has to work.
clawdline_lease_compilers=""
# What the last probe *answered*, kept apart from what it found: `found`, `clear` or `unreadable`.
# An unreadable `pgrep` leaves the pid list empty exactly as a clear machine does, and reading the
# emptiness as "clear" is the fail-open direction in the one axis the backstop rests on.
clawdline_lease_compilers_verdict="unreadable"
# `held` / `unknown` / `orphaned` / `stale`, and the sentence that says what was read. Globals
# rather than printed values because `$(…)` runs a function in a subshell and throws the evidence
# away, and a refusal that cannot name its evidence is a refusal nobody can act on.
clawdline_lease_state=""
clawdline_lease_evidence=""

clawdline_lease_probe_compilers() {
  # **A global count, on purpose, and it includes other people's compilers.** The question is "is
  # anything on this machine burning", not "is my own work running".
  #
  # 0 = at least one compiler is running, 1 = none anywhere, 2 = the probe could not answer.
  # `pgrep` exits 1 when nothing matched, so a `||` here would read "no compiler" as a failure and
  # a failure as "no compiler"; the status is read explicitly and only an explicit 1 is ever
  # allowed to mean the machine is clear.
  local found="" probe_status=0
  found=$(LC_ALL=C pgrep -x "$CLAWDLINE_LEASE_COMPILER_PATTERN" 2>/dev/null) || probe_status=$?
  clawdline_lease_compilers=$(printf '%s' "$found" | tr '\n' ' ')
  case "$probe_status" in
    0) clawdline_lease_compilers_verdict="found"; return 0 ;;
    1) clawdline_lease_compilers_verdict="clear"; return 1 ;;
    *) clawdline_lease_compilers_verdict="unreadable"; return 2 ;;
  esac
}

clawdline_lease_pid_identity() {
  # A pid's start time, as one normalised line, used to tell a recorded holder from a later process
  # that inherited its number. **Both sides of every comparison come out of this one function**,
  # which is why `LC_ALL=C` is pinned here and not at the call sites: this Mac runs zh_TW.UTF-8,
  # where the same instant renders with a different field count depending on the day of the month.
  # Nothing counts fields; the whole line is normalised and compared as a string.
  local pid=$1 line
  case "$pid" in "" | *[!0-9]*) printf 'unknown'; return 0 ;; esac
  line=$(LC_ALL=C ps -o lstart= -p "$pid" 2>/dev/null | awk 'NR == 1 { $1 = $1; print; exit }') || line=""
  printf '%s' "${line:-unknown}"
}

clawdline_lease_pid_verdict() {
  # `alive`, `gone` or `unknown` — and the third one is the point. A two-valued reader collapses
  # "the tool did not answer" into "the process is gone", which is fail-open wherever the question
  # is "may I act". So the probe carries its own control: `ps -p <pid> -p 1` asks about the process
  # *and* about pid 1, which exists on every running macOS. If `1` comes back the tool answered and
  # the absence of `<pid>` is a fact; if `1` does not, the reading is `unknown` and blocks.
  local pid=$1 seen="" control=0 target=0 n
  case "$pid" in "" | *[!0-9]*) printf 'unknown'; return 0 ;; esac
  seen=$(ps -p "$pid" -p 1 -o pid= 2>/dev/null) || seen=""
  for n in $seen; do
    if [ "$n" = "1" ]; then control=1; fi
    if [ "$n" = "$pid" ]; then target=1; fi
  done
  if [ "$control" = 0 ]; then printf 'unknown'; return 0; fi
  if [ "$target" = 1 ]; then printf 'alive'; else printf 'gone'; fi
}

clawdline_lease_identity_verdict() {
  # `same`, `different` or `unknown`. `clawdline_lease_pid_identity` returns the literal string
  # `unknown` when it could not read, and `unknown` never equals a recorded start — so a caller
  # comparing the two directly reads every failed read as "a different process now has that
  # number". That is a reading, not a fact.
  local pid=$1 recorded=$2 observed
  observed=$(clawdline_lease_pid_identity "$pid")
  case "$observed" in unknown) printf 'unknown'; return 0 ;; esac
  case "$recorded" in "" | unknown) printf 'unknown'; return 0 ;; esac
  if [ "$observed" = "$recorded" ]; then printf 'same'; else printf 'different'; fi
}

clawdline_lease_duration() {
  local seconds=$1
  case "$seconds" in "" | *[!0-9]*) printf 'unknown'; return 0 ;; esac
  if [ "$seconds" -ge 60 ]; then printf '%ss (%sm)' "$seconds" "$(( seconds / 60 ))"; else printf '%ss' "$seconds"; fi
}

clawdline_lease_phase_since() {
  local value
  value=$(clawdline_lease_field phase_since "$1")
  case "$value" in "" | *[!0-9]*) date +%s ;; *) printf '%s' "$value" ;; esac
}

clawdline_lease_last_compiling_phrase() {
  local file=$1 now=$2 value
  value=$(clawdline_lease_field last_compiling "$file")
  case "$value" in
    "" | never | *[!0-9]*) printf 'nothing has compiled under this lock yet' ;;
    *) printf 'last compiling %s ago' "$(clawdline_lease_duration "$(( now - value ))")" ;;
  esac
}

clawdline_lease_admission() {
  # Reads somebody else's record and leaves `clawdline_lease_state` and `clawdline_lease_evidence`.
  #
  # **Liveness is proved by renewal, not by a pid existing**, and admission is fail-closed: the
  # lock is handed on only when BOTH (A) the holder has stopped proving it is alive AND (B) no
  # compiler exists anywhere on this machine. (B) alone would hand the lock over in the gaps
  # between one run's compiles; (A) alone would hand it over while an orphaned compile is still
  # spending the memory this lock rations. Missing, stale or ambiguous evidence reads `unknown`
  # and blocks; it never reads "dead".
  #
  # `probe_status=0; … || probe_status=$?` rather than `…; probe_status=$?`: the second form takes
  # the whole script down under `set -e` whenever this function is reached from a caller that is
  # not itself inside a condition, and a probe that legitimately returns 1 is the ordinary case.
  local dir=$1 file="$1/holder.txt" beat heartbeat deadline now age probe_status pid pid_started done_flag
  # `heartbeat` in the record is the *path* of the beat file; the evidence is that file's mtime.
  # The holder says where its heartbeat is, the filesystem says when it last happened.
  beat=$(clawdline_lease_field heartbeat "$file")
  heartbeat=""
  if [ -n "$beat" ] && [ -f "$beat" ]; then
    heartbeat=$(stat -f %m "$beat" 2>/dev/null) || heartbeat=""
  fi
  deadline=$(clawdline_lease_field heartbeat_deadline "$file")
  pid=$(clawdline_lease_field pid "$file")
  pid_started=$(clawdline_lease_field owner_started "$file")
  done_flag=$(clawdline_lease_field done_flag "$file")
  case "$deadline" in "" | *[!0-9]* | 0) deadline="$CLAWDLINE_LEASE_DEADLINE_SECONDS" ;; esac

  # The done flag is a **positive** signal only. Present means the guarded work is over, so with
  # the backstop still satisfied the lock may be handed on at once. Absent proves nothing — a run
  # killed with SIGKILL never writes one — so absence falls through to renewal below.
  if [ -n "$done_flag" ] && [ -f "$done_flag" ]; then
    probe_status=0; clawdline_lease_probe_compilers || probe_status=$?
    case "$probe_status" in
      1) clawdline_lease_state="stale"
         clawdline_lease_evidence="the holder marked its work finished at $done_flag and no $CLAWDLINE_LEASE_COMPILER_PATTERN is running anywhere" ;;
      0) clawdline_lease_state="orphaned"
         clawdline_lease_evidence="the holder marked its work finished, but $CLAWDLINE_LEASE_COMPILER_PATTERN is still running as pid(s) ${clawdline_lease_compilers}— the memory is still being spent, so nobody is admitted and nothing here will kill them" ;;
      *) clawdline_lease_state="unknown"
         clawdline_lease_evidence="the holder marked its work finished, but the $CLAWDLINE_LEASE_COMPILER_PATTERN probe could not answer — unknown blocks" ;;
    esac
    return 0
  fi

  case "$heartbeat" in
    "" | *[!0-9]*)
      # **The abandoned acquisition, and the one way out of `unknown`.** Acquiring is `mkdir`, then
      # a few forks, then the first record. A run that dies in that window leaves a directory with
      # no `holder.txt` and no `beat`, and only positive evidence of that distinct thing clears it:
      # nothing has ever been written here, for longer than any acquisition could take, and the
      # machine is clear. A directory that has a `beat` but no record had a record once and
      # something removed it — a different, unexplained event — and stays `unknown`.
      if [ ! -f "$dir/holder.txt" ] && [ ! -f "$dir/beat" ]; then
        local born born_age
        born=$(stat -f %m "$dir" 2>/dev/null) || born=""
        case "$born" in
          "" | *[!0-9]*) born_age=-1 ;;
          *) born_age=$(( $(date +%s) - born )) ;;
        esac
        if [ "$born_age" -gt "$deadline" ]; then
          probe_status=0; clawdline_lease_probe_compilers || probe_status=$?
          case "$probe_status" in
            1) clawdline_lease_state="stale"
               clawdline_lease_evidence="it was created ${born_age}s ago and has never held a record or a heartbeat, which is what a run killed between mkdir and its first write leaves behind, and no $CLAWDLINE_LEASE_COMPILER_PATTERN is running anywhere"
               return 0 ;;
            0) clawdline_lease_state="orphaned"
               clawdline_lease_evidence="it has never held a record, but $CLAWDLINE_LEASE_COMPILER_PATTERN is still running as pid(s) ${clawdline_lease_compilers}— nobody is admitted and nothing here will kill them"
               return 0 ;;
          esac
        fi
      fi
      clawdline_lease_state="unknown"
      clawdline_lease_evidence="its record names no heartbeat file, or the file it names is not there, so whether anyone is still there is unknown — and unknown blocks rather than reading as dead"
      return 0 ;;
  esac
  now=$(date +%s)
  age=$(( now - heartbeat ))
  if [ "$age" -lt 0 ]; then
    clawdline_lease_state="unknown"
    clawdline_lease_evidence="its heartbeat is ${age#-}s in the future, so the two clocks disagree and the evidence is ambiguous — ambiguous blocks"
    return 0
  fi
  if [ "$age" -le "$deadline" ]; then
    clawdline_lease_state="held"
    # Three answers, not two. A record carrying no `owner_started` is one a writer with no
    # `ps -o lstart=` line to give wrote, and reporting that as "this pid no longer looks like the
    # one that took the lock" is a sentence a person could act on and should not have.
    case "$(clawdline_lease_identity_verdict "$(clawdline_lease_field owner_pid "$file")" "$pid_started")" in
      same)
        clawdline_lease_evidence="it renewed ${age}s ago against a ${deadline}s deadline; phase $(clawdline_lease_field phase "$file") for $(clawdline_lease_duration "$(( now - $(clawdline_lease_phase_since "$file") ))"), $(clawdline_lease_last_compiling_phrase "$file" "$now"); working: $(clawdline_lease_field work "$file"), compilers: $(clawdline_lease_field compilers "$file")" ;;
      different)
        clawdline_lease_evidence="it renewed ${age}s ago against a ${deadline}s deadline, so something is still proving it is there, though pid $pid no longer looks like the process that took the lock" ;;
      *)
        clawdline_lease_evidence="it renewed ${age}s ago against a ${deadline}s deadline; its record carries no readable start identity for pid $pid, so that axis says nothing either way" ;;
    esac
    return 0
  fi
  # The holder has stopped proving it is alive. That admits nobody on its own: the backstop is
  # physical, and it is never waived.
  probe_status=0; clawdline_lease_probe_compilers || probe_status=$?
  case "$probe_status" in
    1) clawdline_lease_state="stale"
       clawdline_lease_evidence="its last renewal was ${age}s ago, past its own ${deadline}s deadline, and no $CLAWDLINE_LEASE_COMPILER_PATTERN is running anywhere on this machine" ;;
    0) clawdline_lease_state="orphaned"
       clawdline_lease_evidence="its last renewal was ${age}s ago, past its own ${deadline}s deadline, but $CLAWDLINE_LEASE_COMPILER_PATTERN is still running as pid(s) ${clawdline_lease_compilers}— an orphaned compile is still spending the memory this lock rations, so nobody is admitted and nothing here will kill them" ;;
    *) clawdline_lease_state="unknown"
       clawdline_lease_evidence="its last renewal was ${age}s ago, but the $CLAWDLINE_LEASE_COMPILER_PATTERN probe could not answer, so whether a compile is running is unknown — and unknown blocks" ;;
  esac
  return 0
}

clawdline_lease_release_gate() {
  local gate=$1
  if [ "$(clawdline_lease_field pid "$gate/holder.txt")" = "$$" ]; then
    rm -rf "$gate"
  fi
}

clawdline_lease_take_over() {
  # The compare and the swap, and it is `test.sh`'s.
  #
  # `rename(2)` decides which of several waiters that judged the *same* lock stale gets to remove
  # it: exactly one `mv` succeeds and the losers fail with ENOENT. That alone is not enough,
  # because a waiter's judgement can be older than a whole takeover — B reads a stale record; A
  # takes over, acquires and starts compiling; B then renames A's *fresh* lock away. So the
  # judgement is made again here, under a gate directory only one waiter can hold, and the pair of
  # lines the gate wraps is what actually closes it: the re-read, which a beating holder cannot
  # satisfy, and the token compare, which requires it to still be the *same* record.
  local lock=$1 judged_token=$2
  local gate="$lock.takeover" gate_pid gate_verdict stale
  if ! mkdir "$gate" 2>/dev/null; then
    # Three answers here too. An *empty or non-numeric* `pid` is a different fact and keeps its own
    # answer: the gate was created and its record never written, so there is nobody to ask about
    # and nobody is holding it. Leaving that uncleared would be a deadlock.
    gate_pid=$(clawdline_lease_field pid "$gate/holder.txt")
    case "$gate_pid" in
      "" | *[!0-9]*) gate_verdict="unowned" ;;
      *) gate_verdict=$(clawdline_lease_pid_verdict "$gate_pid") ;;
    esac
    case "$gate_verdict" in
      gone | unowned)
        if mv "$gate" "$gate.abandoned.$$" 2>/dev/null; then rm -rf "$gate.abandoned.$$"; fi ;;
    esac
    return 1
  fi
  printf 'pid=%s\n' "$$" > "$gate/holder.txt"
  # And confirm the gate is still this waiter's before acting on it: if another waiter cleared it
  # as abandoned in the window just above, two could be holding it, and whichever no longer reads
  # its own pid backs out rather than swapping.
  gate_pid=$(clawdline_lease_field pid "$gate/holder.txt")
  if [ "$gate_pid" != "$$" ]; then
    return 1
  fi
  clawdline_lease_admission "$lock"
  if [ "$clawdline_lease_state" = "stale" ] &&
     [ "$(clawdline_lease_field token "$lock/holder.txt")" = "$judged_token" ]; then
    stale="$lock.stale.$$"
    if mv "$lock" "$stale" 2>/dev/null; then
      rm -rf "$stale"
      echo "→ took over $lock — $clawdline_lease_evidence"
      clawdline_lease_release_gate "$gate"
      return 0
    fi
  fi
  clawdline_lease_release_gate "$gate"
  return 1
}

# **The record. One format, two writers — see the contract above `clawdline_suite_lock_write_record`
# in `test.sh`, which is where it is written down.** It was three while the broker wrote the file
# too. This used to write eleven of the eighteen fields, and none of the four the shell's
# compare-and-swap depends on, so a `test.sh` waiting behind a build compared two empty tokens and
# always found them equal.
#
# Three things it now does that it did not:
#
#   * **It checks that this run still holds the lock before writing.** It did not, and the
#     consequence needed no exotic timing: a build whose beat lapses, a `test.sh` that legitimately
#     takes the lock over and starts compiling, and then this loop waking twenty seconds later to
#     truncate the *new* holder's beat and rewrite its record under this run's name. One unchecked
#     write turned "the lock changed hands" into two runs compiling.
#   * **It writes through a temporary file and a rename**, so a reader sees the previous complete
#     record or the new one and never half of either. It was a `>` redirect straight onto the live
#     file, rewritten every twenty seconds for the length of a compile, and every reader fails
#     closed on a partial record — so the lock's state flickered into `unknown` on a schedule,
#     which is the one state operators are told to treat as serious.
#   * **It returns non-zero when it did not write.** The caller needs to know.
clawdline_lease_record() {
  local phase="${1:-analysing}" pid="${2:-$$}" first="${3:-}"
  local temp now
  [ -d "$CLAWDLINE_LEASE_DIR" ] || return 1
  if [ -z "$first" ]; then
    [ "$(clawdline_lease_field token "$CLAWDLINE_LEASE_DIR/holder.txt")" = "$CLAWDLINE_LEASE_ID" ] || return 1
  fi
  now=$(date +%s)
  if [ "$phase" != "$CLAWDLINE_LEASE_PHASE" ]; then
    CLAWDLINE_LEASE_PHASE="$phase"
    CLAWDLINE_LEASE_PHASE_SINCE="$now"
  fi
  case "$phase" in compiling) CLAWDLINE_LEASE_LAST_COMPILING="$now" ;; esac
  # The beat, touched before the record that points at it, so a reader that sees the record always
  # finds the file — and only once this run has proved the lock is still its own.
  : > "$CLAWDLINE_LEASE_DIR/beat" 2>/dev/null || true
  temp="$CLAWDLINE_LEASE_DIR/.holder.$$.$RANDOM"
  {
    printf 'holder=%s\n' "build.sh $(id -un) pid $$"
    printf 'pid=%s\n' "$pid"
    printf 'owner_pid=%s\n' "$$"
    printf 'owner_started=%s\n' "$CLAWDLINE_LEASE_OWNER_STARTED"
    printf 'token=%s\n' "$CLAWDLINE_LEASE_ID"
    printf 'phase=%s\n' "$phase"
    printf 'phase_since=%s\n' "$CLAWDLINE_LEASE_PHASE_SINCE"
    printf 'heartbeat=%s\n' "$CLAWDLINE_LEASE_DIR/beat"
    printf 'heartbeat_deadline=%s\n' "$CLAWDLINE_LEASE_DEADLINE_SECONDS"
    printf 'started=%s\n' "$CLAWDLINE_LEASE_STARTED"
    printf 'renewed=%s\n' "$now"
    printf 'tree=%s\n' "$(pwd)"
    printf 'log=%s\n' ""
    printf 'done_flag=%s\n' "$CLAWDLINE_LEASE_DONE"
    printf 'work=%s\n' "$pid"
    printf 'last_compiling=%s\n' "$CLAWDLINE_LEASE_LAST_COMPILING"
    # Empty, and that is the third state the contract defines: this writer does not probe for
    # compilers, which is not the same claim as `none`.
    printf 'compilers=%s\n' ""
    printf 'note=%s\n' "building Clawdline.app; this lock covers the swiftc invocation only. Ask the run named above rather than removing this directory."
  } > "$temp" 2>/dev/null || return 1
  mv "$temp" "$CLAWDLINE_LEASE_DIR/holder.txt" 2>/dev/null || { rm -f "$temp" 2>/dev/null; return 1; }
  return 0
}

# One beat. It lives inside the lock directory, so `rmdir` takes it with the lock and no orphan
# heartbeat can outlive the work it stood for.
clawdline_lease_beat() {
  local phase="${1:-analysing}" pid="${2:-$$}"
  # Once this run is no longer the recorded holder it says so once and goes quiet, rather than
  # repeating itself every twenty seconds for the length of a compile. The state lives here rather
  # than in the loop below, because that loop's shape is the whole argument that a beat stops when
  # the work does, and it is asserted line by line.
  [ "$CLAWDLINE_LEASE_BEATING" = 1 ] || return 0
  if ! clawdline_lease_record "$phase" "$pid"; then
    CLAWDLINE_LEASE_BEATING=0
    echo "!! $CLAWDLINE_LEASE_DIR no longer records this build's hold; not writing to it." >&2
    return 0
  fi
}

# **The heartbeat is emitted by the loop that supervises the compiler, and by nothing else.**
#
# This is the whole difference between a heartbeat and the `sleep 14400` this design exists to
# fix. A detached timer —
#
#     while true; do : > beat; sleep 60; done &      # a sentinel
#
# — keeps beating after the work it claims to represent has died, which is a sentinel in a new
# coat. Here the loop's own condition is the compiler still being alive, so when `swiftc` exits,
# or when this shell is killed, the beat stops with it. `kill -0` sends no signal; it asks
# whether the process exists, and nothing in this file ever signals a process it did not start.
clawdline_lease_supervise() {
  # A beat this run may no longer write stops the beating, not the compile. `swiftc` is already
  # running: ending it here would orphan a `swift-frontend` holding tens of gigabytes, which is
  # the exact thing this whole mechanism exists to prevent, and nothing in this file ends a
  # process it did not start. So `clawdline_lease_beat` says so once and goes quiet, and this loop
  # keeps its one shape: its condition is the compiler still being alive, and its body is the beat.
  local compiler=$1
  while kill -0 "$compiler" 2>/dev/null; do
    clawdline_lease_beat compiling "$compiler"
    sleep 20
  done
}

# The first record after taking the directory directly — and the check that it was written.
#
# **A first write that half-fails leaves a lock nothing can clear.** `clawdline_lease_record`
# touches the beat before it writes the record that points at it, so a `$temp` create that fails
# where the beat's succeeded — ENOSPC on `/tmp` rather than a permission problem, which fails both
# — leaves a directory with a `beat` and no `holder.txt`. That is the one shape `test.sh`'s escape
# from `unknown` deliberately refuses to clear: a directory that has a beat had a record once and
# lost it, which is a different and unexplained event. And this script runs under `set -e`, so the
# unchecked call this replaces did not even get as far as reporting it — the build ended on the
# failed write and left the machine's compile slot blocked with no automatic way out.
#
# `test.sh` has handled this since the record contract landed, and so did the broker's
# `createDirectory` while it existed; this was the third writer, and the asymmetry was invisible
# because each was reviewed against its own file.
clawdline_lease_first_record() {
  CLAWDLINE_LEASE_MODE=directory
  if clawdline_lease_record analysing "$$" first; then
    return 0
  fi
  echo "!! could not write $CLAWDLINE_LEASE_DIR/holder.txt — refusing to compile behind a lock that says nothing about who holds it" >&2
  # Give the directory back, but only while it is still the record-less one this build made a
  # moment ago. Nobody else can legitimately be in it: the only rule that hands on a directory with
  # no record requires it to be older than a whole renewal deadline, which one created milliseconds
  # ago is not. Written as a guard rather than an unconditional `rm -rf` so that the reasoning is
  # visible rather than remembered — the same shape, and the same argument, as `test.sh`'s.
  if [ ! -f "$CLAWDLINE_LEASE_DIR/holder.txt" ]; then rm -rf "$CLAWDLINE_LEASE_DIR"; fi
  # And this build never held it, so the exit trap must not try to release it.
  CLAWDLINE_LEASE_MODE=""
  return 1
}

clawdline_lease_acquire() {
  CLAWDLINE_LEASE_ID="build-$$-$(date +%s)"
  # The owner's start identity, in the one shape the record contract defines for it: a normalised
  # `LC_ALL=C ps -o lstart=` line, from one formatter, compared whole. `LC_ALL=C` because this Mac
  # runs zh_TW.UTF-8, where the same instant renders with a different field count depending on the
  # day of the month.
  CLAWDLINE_LEASE_OWNER_STARTED=$(LC_ALL=C ps -o lstart= -p $$ 2>/dev/null | awk 'NR == 1 { $1 = $1; print; exit }') || CLAWDLINE_LEASE_OWNER_STARTED=""
  # **`started=` is this process's start, not the moment it got here**, and it is derived from the
  # `ps` line above rather than taken separately so that the record's two start fields cannot
  # disagree. `build.sh` reaches this line only after the Keychain helper has run, so `date +%s`
  # here was minutes late — a reader working out how long this hold has lasted was reading the wait
  # rather than the run. `LC_ALL=C` on both halves for the same reason it is on the reading. If the
  # conversion cannot be made, fall back to now and be late rather than absent: a missing field
  # reads as unknown, which blocks.
  CLAWDLINE_LEASE_STARTED=$(LC_ALL=C date -j -f '%a %b %e %T %Y' "$CLAWDLINE_LEASE_OWNER_STARTED" +%s 2>/dev/null) \
    || CLAWDLINE_LEASE_STARTED=""
  [ -n "$CLAWDLINE_LEASE_STARTED" ] || CLAWDLINE_LEASE_STARTED=$(date +%s)
  CLAWDLINE_LEASE_DONE="${TMPDIR:-/tmp}/clawdline-build-done-$$"
  rm -f "$CLAWDLINE_LEASE_DONE"
  local deadline=$(( $(date +%s) + CLAWDLINE_LEASE_WAIT_SECONDS ))
  local announced=0 judged_token=""
  # **`mkdir` is the whole of the exclusion, `rename` is the whole of the takeover, and the wait is
  # the whole of the queue.** There is no broker in front of this any more, so there is no
  # position, no budget and no visibility beyond the record in the directory — and none of those
  # three was what stopped two compiles colliding. What is here is what was used: take it, or hand
  # a dead holder's on under the two conditions below, or say who has it. Never proceed without it.
  while :; do
    if mkdir "$CLAWDLINE_LEASE_DIR" 2>/dev/null; then
      if ! clawdline_lease_first_record; then
        return 1
      fi
      echo "→ heavy-compile lock taken ($CLAWDLINE_LEASE_ID)"
      return 0
    fi
    # Somebody has it. Read what the record says before deciding anything, because the answer is
    # what both the takeover and the sentence below are made of.
    judged_token=$(clawdline_lease_field token "$CLAWDLINE_LEASE_DIR/holder.txt")
    clawdline_lease_admission "$CLAWDLINE_LEASE_DIR"
    if [ "$clawdline_lease_state" = "stale" ]; then
      if clawdline_lease_take_over "$CLAWDLINE_LEASE_DIR" "$judged_token"; then
        # The directory is gone; go round and take it with `mkdir` like anybody else. Losing that
        # race to another waiter is fine — it means the slot is held by a run that is alive.
        announced=0
        continue
      fi
    fi
    if [ "$announced" = 0 ]; then
      echo "→ waiting for $CLAWDLINE_LEASE_DIR — $clawdline_lease_evidence"
      echo "  held by:"
      sed 's/^/    /' "$CLAWDLINE_LEASE_DIR/holder.txt" 2>/dev/null | head -4
      announced=1
    fi
    if [ "$(date +%s)" -ge "$deadline" ]; then
      echo "!! gave up waiting ${CLAWDLINE_LEASE_WAIT_SECONDS}s for the heavy-compile slot." >&2
      echo "   $clawdline_lease_evidence" >&2
      echo "   Nothing was killed and nothing was compiled. Look at $CLAWDLINE_LEASE_DIR/holder.txt." >&2
      return 1
    fi
    sleep "$CLAWDLINE_LEASE_POLL_SECONDS"
  done
}

# Release is idempotent and removes the directory only while it still names this process — the
# one rule a release path may never break is removing a lock somebody else owns.
clawdline_lease_release() {
  [ -n "$CLAWDLINE_LEASE_MODE" ] || return 0
  # The token, not the holder line: a pid is reused within hours on a busy machine, so
  # `holder=build.sh <user> pid <n>` can match a record this run did not write. The token cannot.
  if [ "$(clawdline_lease_field token "$CLAWDLINE_LEASE_DIR/holder.txt")" = "$CLAWDLINE_LEASE_ID" ]; then
    rm -rf "$CLAWDLINE_LEASE_DIR"
  fi
  CLAWDLINE_LEASE_MODE=""
}

APP="${CLAWDLINE_APP:-$HOME/Applications/Clawdline.app}"
APP_PARENT="$(dirname "$APP")"
APP_NAME="$(basename "$APP")"
BUNDLE_ID="com.tsunamiworks.clawdline"
LOCAL_SIGN_IDENTITY_NAME="Clawdline Local Development"
LOCAL_SIGNING=0
# BEGIN keychain-rebuild-focused: bounded signing commands
# Every Keychain-touching command below can reach a system dialog — "unlock the login keychain",
# "codesign wants to access key" — that waits for a person who may not be at the Mac. macOS ships
# no timeout(1) and /bin/bash here is 3.2, so `wait -n` is out too: a watchdog subshell it is.
#
# The marker file is what tells a timeout apart from an ordinary non-zero exit. Deriving it from
# the signal number cannot: 143 is also what a command killed by anything else reports.
CLAWDLINE_SIGN_QUERY_TIMEOUT="${CLAWDLINE_SIGN_QUERY_TIMEOUT:-30}"
CLAWDLINE_CODESIGN_TIMEOUT="${CLAWDLINE_CODESIGN_TIMEOUT:-120}"
clawdline_require_positive_integer() {
  local name=$1 value=$2
  case "$value" in
    ''|*[!0-9]*|0)
      echo "!! $name must be a positive integer, got: $value" >&2
      return 2
      ;;
  esac
}
clawdline_require_positive_integer CLAWDLINE_SIGN_QUERY_TIMEOUT "$CLAWDLINE_SIGN_QUERY_TIMEOUT" || exit $?
clawdline_require_positive_integer CLAWDLINE_CODESIGN_TIMEOUT "$CLAWDLINE_CODESIGN_TIMEOUT" || exit $?

# `CLAWDLINE_BOUNDED_OUTCOME` is the typed side channel. Exit 124 alone is ambiguous because the
# child itself is allowed to exit 124; only `timeout` means the watchdog killed a live process.
CLAWDLINE_BOUNDED_OUTCOME=not_run
clawdline_bounded() {
  local seconds=$1 outfile=$2
  shift 2
  local marker="$outfile.timed-out"
  rm -f "$marker"
  "$@" >"$outfile" 2>&1 &
  local pid=$!
  (
    sleep "$seconds"
    kill -TERM "$pid" 2>/dev/null && : > "$marker"
    sleep 2
    kill -KILL "$pid" 2>/dev/null
  ) >/dev/null 2>&1 &
  local watchdog=$!
  local status=0
  wait "$pid" 2>/dev/null || status=$?
  kill -TERM "$watchdog" 2>/dev/null || true
  wait "$watchdog" 2>/dev/null || true
  if [ -e "$marker" ]; then
    rm -f "$marker"
    CLAWDLINE_BOUNDED_OUTCOME=timeout
    return 124
  fi
  CLAWDLINE_BOUNDED_OUTCOME=exit
  return "$status"
}
# END keychain-rebuild-focused: bounded signing commands
# The phase, and the reason it is on this side of the marker: everything between
# `# BEGIN keychain-rebuild-focused: …` and its `END` is lifted out of this file and run by
# `Tests/keychain-rebuild-focused.mjs` in a shell that has none of these functions, so a call
# inside the region would end that harness at 127 instead of testing the signing branches.
clawdline_run_file_phase 'checking signing'
# BEGIN keychain-rebuild-focused: signing identity selection
# The login keychain is only ever *probed*, never unlocked: a build that could unlock somebody's
# Keychain would be a build that could be asked to.
LOCAL_SIGN_KEYCHAIN="${CLAWDLINE_LOCAL_SIGN_KEYCHAIN:-$HOME/Library/Keychains/login.keychain-db}"
if [ -n "${CLAWDLINE_KEYCHAIN_STATUS_HELPER:-}" ]; then
  keychain_status_command=("$CLAWDLINE_KEYCHAIN_STATUS_HELPER")
else
  keychain_status_command=(xcrun swift tools/keychain-status.swift)
fi
signing_probe_out=$(mktemp "${TMPDIR:-/tmp}/clawdline-signing-probe.XXXXXX")
clawdline_signing_probe_exit() {
  # The probe's own cleanup **and** the run file's way out, in one handler, because bash keeps
  # exactly one EXIT trap and this one replaces the trap installed with the run-file block above.
  # Four of the deliberate `exit 1`s in the selection below — a locked Keychain, two identities,
  # no identity at all — are the most common way a build stops, and an `exit` reaches no ERR trap;
  # this used to be the one window the run file was knowingly left open in, and the cost of that
  # was a `running` row nobody would retire for fifteen minutes. The temporary file goes first: it
  # is this handler's own business, and the run file never fails the run it reports on.
  local status=${1:-0}
  rm -f "$signing_probe_out" "$signing_probe_out.timed-out"
  # `declare -F`, because this handler is inside the region between the
  # `# BEGIN keychain-rebuild-focused: signing identity selection` markers, and that region is
  # lifted out and run by `Tests/keychain-rebuild-focused.mjs` in a shell where the run-file block
  # does not exist. A bare call there would print `command not found` from inside an EXIT trap.
  # Same guard and same reason as the one call inside test.sh's suite-lock block.
  if declare -F clawdline_run_file_exit >/dev/null 2>&1; then clawdline_run_file_exit "$status" || true; fi
}
trap 'clawdline_signing_probe_exit "$?"' EXIT
if [ "${CLAWDLINE_SIGN_IDENTITY+x}" = x ]; then
  # An explicit value keeps its historical meaning, including an empty value becoming ad-hoc.
  SIGN_IDENTITY="${CLAWDLINE_SIGN_IDENTITY:--}"
elif [ "${CLAWDLINE_SIGN_ADHOC:-0}" = 1 ]; then
  # The documented ad-hoc contract: chosen, not fallen into. Nothing below is consulted, so it
  # is also the way past a locked Keychain without unlocking anything.
  SIGN_IDENTITY=-
  echo "→ CLAWDLINE_SIGN_ADHOC=1; signing ad-hoc by explicit request"
  echo "  Ad-hoc means a new code identity every rebuild: macOS re-asks to authorise iTerm2"
  echo "  automation, and the Cloud Keychain items are re-authorised on first use."
else
  identity_status=0
  clawdline_bounded "$CLAWDLINE_SIGN_QUERY_TIMEOUT" "$signing_probe_out" \
    security find-identity -v -p codesigning "$LOCAL_SIGN_KEYCHAIN" || identity_status=$?
  if [ "$identity_status" -eq 0 ]; then
    identity_output=$(cat "$signing_probe_out")
    identity_hashes=$(printf '%s\n' "$identity_output" \
      | awk -v name="$LOCAL_SIGN_IDENTITY_NAME" \
          'index($0, "\"" name "\"") { print $2 }')
    identity_count=$(printf '%s\n' "$identity_hashes" \
      | awk 'NF { count++ } END { print count + 0 }')
    if [ "$identity_count" -eq 1 ]; then
      # An identity that exists in a locked Keychain is worse than one that does not: codesign
      # finds it, then stops on an unlock dialog. Ask first, and say so instead of hanging.
      keychain_status=0
      clawdline_bounded "$CLAWDLINE_SIGN_QUERY_TIMEOUT" "$signing_probe_out" \
        "${keychain_status_command[@]}" "$LOCAL_SIGN_KEYCHAIN" || keychain_status=$?
      if [ "$keychain_status" -eq 0 ]; then
        SIGN_IDENTITY=$(printf '%s\n' "$identity_hashes" | awk 'NF { print; exit }')
        LOCAL_SIGNING=1
      else
        if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
          echo "!! the login Keychain did not answer within ${CLAWDLINE_SIGN_QUERY_TIMEOUT}s" >&2
        else
          echo "!! the login Keychain is locked or unreadable: $LOCAL_SIGN_KEYCHAIN (status $keychain_status)" >&2
        fi
        echo "   $LOCAL_SIGN_IDENTITY_NAME exists there, so signing would stop on an unlock" >&2
        echo "   dialog. Clawdline will not unlock a Keychain for you." >&2
        echo "   Unlock it yourself:  security unlock-keychain $LOCAL_SIGN_KEYCHAIN" >&2
        echo "   Or build ad-hoc:     CLAWDLINE_SIGN_ADHOC=1 ./build.sh" >&2
        exit 1
      fi
    elif [ "$identity_count" -gt 1 ]; then
      echo "!! multiple valid code-signing identities are named $LOCAL_SIGN_IDENTITY_NAME" >&2
      printf '   %s\n' $identity_hashes >&2
      echo "   Remove or rename the extra identity; Clawdline will not choose by Keychain order." >&2
      exit 1
    else
      echo "!! no valid $LOCAL_SIGN_IDENTITY_NAME identity exists in $LOCAL_SIGN_KEYCHAIN" >&2
      echo "   Run tools/setup-local-signing-identity.sh, or explicitly choose ad-hoc:" >&2
      echo "     CLAWDLINE_SIGN_ADHOC=1 ./build.sh" >&2
      exit 1
    fi
  else
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      echo "!! code-signing identity lookup did not answer within ${CLAWDLINE_SIGN_QUERY_TIMEOUT}s" >&2
    else
      echo "!! code-signing identity lookup failed with status $identity_status" >&2
    fi
    echo "   Refusing an implicit ad-hoc fallback. Inspect $LOCAL_SIGN_KEYCHAIN, or explicitly choose:" >&2
    echo "     CLAWDLINE_SIGN_ADHOC=1 ./build.sh" >&2
    exit 1
  fi
fi
# END keychain-rebuild-focused: signing identity selection
# The probe's own trap is replaced by cleanup_build further down, so retire it here rather than
# leaving the file behind for whoever empties TMPDIR next.
rm -f "$signing_probe_out" "$signing_probe_out.timed-out"
# **Back to the run file's own handler, not to no handler at all.** A bare `trap - EXIT` here left
# everything between this line and `cleanup_build` — the staging directory, the backup, the lease —
# with no EXIT trap of any kind, which is the same window this block closed at the top of the file.
trap 'clawdline_run_file_exit "$?"' EXIT

mkdir -p "$APP_PARENT"
# Build beside the installed app, on the same filesystem. The final rename is then quick and
# cannot turn into a slow copy just when the running app has been stopped.
STAGE_ROOT="$(mktemp -d "$APP_PARENT/.clawdline-build.XXXXXX")"
STAGED_APP="$STAGE_ROOT/$APP_NAME"
BIN="$STAGED_APP/Contents/MacOS/Clawdline"
RES="$STAGED_APP/Contents/Resources"
BACKUP="$STAGE_ROOT.previous"
cleanup_build() {
  # First, so it reads the status the build is actually leaving with. The run file is composed into
  # this handler rather than trapped separately because bash keeps exactly one EXIT trap, and this
  # file installs three in sequence.
  local clawdline_build_status=$?
  clawdline_run_file_exit "$clawdline_build_status" || true
  if [ "${MAINTENANCE_ACTIVE:-0}" = 1 ] && [ -n "${MAINTENANCE_REQUEST_ID:-}" ] \
      && [ -r "${TOKEN_FILE:-}" ] && [ -n "${PORT:-}" ]; then
    curl -sS --max-time 5 -X DELETE \
      "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
      -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
      -H 'Content-Type: application/json' \
      -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null 2>&1 || true
  fi
  clawdline_lease_release
  rm -rf "$STAGE_ROOT"
}
trap cleanup_build EXIT

echo "→ building staged app for $APP"
mkdir -p "$(dirname "$BIN")" "$RES"

clawdline_lease_acquire || exit 1
clawdline_run_file_phase compiling

# >>> clawdline compile ceiling >>>
# How many compiler jobs this build may have, and where that number came from.
#
# **The same rule `test.sh` uses, deliberately duplicated rather than shared.** Both marked blocks
# are lifted and driven against the same fake `sysctl` readings by `Tests/test-sh-lock.mjs`, which
# asserts they answer with the same numbers — a behavioural identity, which survives the two
# scripts printing different sentences, where a textual one would not. Sourcing one file from both
# was the other option and it was rejected: the guard machinery here runs a lifted block from a
# temporary directory, where a relative `.` would find nothing, and a block that cannot be run on
# its own is a block nothing checks.
#
# **This compile is not `test.sh`'s and has its own readings.** 103 production sources with `-O`,
# which is where the LLVM pass pipeline runs — the phase that reached 46 GiB on the old
# `CloudAccountTests`. Measured 2026-09-03 on a detached worktree, machine lock held, footprint
# from `proc_pid_rusage(RUSAGE_INFO_V4)`:
#
#     -j  1   169 s    one frontend's peak 0.430 GiB    most alive together 0.445 GiB
#     -j  4    54 s                        0.408                            0.837
#     -j  8    37 s                        0.400                            1.336
#     -j 14    33 s                        0.410                            2.064
#
# Every frontend here is about half of what one costs in `test.sh`, and that is not a property of
# `-O`: the expensive files are the test suites, and this compile has none of them. Fourteen buys
# four seconds over eight and spends every core to do it.
compile_jobs=()
case "${CLAWDLINE_SUITE_JOBS:-}" in
  "")
    # `sysctl` failing, or answering something that is not a count, reads as one job rather than as
    # no ceiling. The floor is the safe direction and it is also what this line did before.
    clawdline_compile_jobs=$(sysctl -n hw.ncpu 2>/dev/null) || clawdline_compile_jobs=""
    case "$clawdline_compile_jobs" in
      "" | *[!0-9]* | 0*) clawdline_compile_jobs=1 ;;
    esac
    if [ "$clawdline_compile_jobs" -gt 8 ]; then clawdline_compile_jobs=8; fi
    CLAWDLINE_SUITE_JOBS_SOURCE="min(8, hw.ncpu); CLAWDLINE_SUITE_JOBS unset" ;;
  # `0*` and not just `0`: `00` is all digits, so it would slip past `*[!0-9]*` and reach `swiftc`
  # as `-j 00`. The contract is "a positive whole number", and `00` and `007` are not that however
  # they behave downstream.
  *[!0-9]* | 0*)
    echo "build.sh: CLAWDLINE_SUITE_JOBS='${CLAWDLINE_SUITE_JOBS}' is not a positive whole number of jobs." >&2
    exit 2 ;;
  *)
    clawdline_compile_jobs=$CLAWDLINE_SUITE_JOBS
    CLAWDLINE_SUITE_JOBS_SOURCE="CLAWDLINE_SUITE_JOBS" ;;
esac
compile_jobs=(-j "$clawdline_compile_jobs")
echo "→ compiling with -j $clawdline_compile_jobs, from $CLAWDLINE_SUITE_JOBS_SOURCE"
# <<< clawdline compile ceiling <<<

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -O \
  ${compile_jobs[@]+"${compile_jobs[@]}"} \
  -o "$BIN" \
  "${clawdline_production_sources[@]}" \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation -framework Network &
CLAWDLINE_COMPILER=$!
clawdline_lease_supervise "$CLAWDLINE_COMPILER"
wait "$CLAWDLINE_COMPILER"

# The work is over: say so positively, then give the slot back. `done_flag` existing is what lets
# another line take the lock at once instead of waiting out a heartbeat threshold. Packaging,
# signing and installing are not what this lock protects.
: > "$CLAWDLINE_LEASE_DONE" 2>/dev/null || true
clawdline_lease_release
clawdline_run_file_phase packaging

cp Resources/iterm.js "$RES/"
cp Resources/Clawdline.icns "$RES/"
cp Resources/clawdline-hook.sh "$RES/"
# The default dispatch policy. It is a document people read and edit, so it ships as a file rather
# than as a string literal in the source; Orchestrator writes it out once if the machine has none.
cp Resources/dispatch-policy.md "$RES/"
# The agent guide, and the reader the installed SKILL.md stub points at. The stub is a copy in
# somebody's skills directory and never updates, so the routes and fields it must not go stale on
# ship here instead, beside the build that serves them. Reading one is a local file read: it needs
# no running app, which is why it still answers over SSH and while Clawdline is closed.
cp Resources/clawdline-skill.sh "$RES/"
chmod +x "$RES/clawdline-skill.sh"
cp -R Resources/skill-guides "$RES/"
cp -R Resources/mascots "$RES/"
# The web interface, served by RemoteServer when it is switched on.
[ -d Resources/web ] && cp -R Resources/web "$RES/"

cat > "$STAGED_APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Clawdline</string>
  <key>CFBundleDisplayName</key><string>Clawdline</string>
  <key>CFBundleIdentifier</key><string>com.tsunamiworks.clawdline</string>
  <key>CFBundleExecutable</key><string>Clawdline</string>
  <key>CFBundleIconFile</key><string>Clawdline</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.7.0</string>
  <key>CFBundleVersion</key><string>0.7.0</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHumanReadableCopyright</key><string>Copyright © 2026 TsunamiWorks Co., Ltd.</string>
  <!-- Without this line macOS kills the app the first time it talks to iTerm2 -->
  <key>NSAppleEventsUsageDescription</key>
  <string>Clawdline needs to control iTerm2 so it can put what you type into Claude Code.</string>
  <!-- Asked for only when the microphone button is pressed. Recognition runs on this Mac when
       the dictation language is installed, and goes to Apple when it is not — the bar says
       which, while it is listening. -->
  <key>NSMicrophoneUsageDescription</key>
  <string>Clawdline uses the microphone only while you hold a dictation session open, so you can talk into the prompt instead of typing.</string>
  <key>NSSpeechRecognitionUsageDescription</key>
  <string>Clawdline turns your speech into text. It uses this Mac when the dictation language is installed, and Apple's service when it is not.</string>
  <!-- clawdline://open so any tool can summon it, not just the built-in hotkey -->
  <key>CFBundleURLTypes</key>
  <array><dict>
    <key>CFBundleURLName</key><string>com.tsunamiworks.clawdline</string>
    <key>CFBundleURLSchemes</key><array><string>clawdline</string></array>
  </dict></array>
</dict>
</plist>
PLIST

# A stable local certificate keeps the Keychain ACL's designated requirement unchanged across
# rebuilds. A release still supplies the company Developer ID identity and receives Hardened
# Runtime, a trusted timestamp, and only the two resource entitlements the app actually uses.
# The private key for either identity never enters this repository.
clawdline_run_file_phase signing
# BEGIN keychain-rebuild-focused: signing branches
codesign_out=$(mktemp "${TMPDIR:-/tmp}/clawdline-codesign.XXXXXX")
if [ "$SIGN_IDENTITY" = - ]; then
  adhoc_sign_status=0
  clawdline_bounded "$CLAWDLINE_CODESIGN_TIMEOUT" "$codesign_out" \
    codesign --force --sign - --identifier "$BUNDLE_ID" "$STAGED_APP" \
    || adhoc_sign_status=$?
  if [ "$adhoc_sign_status" -ne 0 ]; then
    cat "$codesign_out" >&2
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      echo "!! ad-hoc codesign timed out after ${CLAWDLINE_CODESIGN_TIMEOUT}s; staged bundle state is unknown" >&2
    else
      echo "!! ad-hoc codesign failed with status $adhoc_sign_status" >&2
    fi
    exit "$adhoc_sign_status"
  fi
elif [ "$LOCAL_SIGNING" = 1 ]; then
  echo "→ using stable local signing"
  local_sign_status=0
  clawdline_bounded "$CLAWDLINE_CODESIGN_TIMEOUT" "$codesign_out" \
    codesign --force --sign "$SIGN_IDENTITY" --keychain "$LOCAL_SIGN_KEYCHAIN" \
    --identifier "$BUNDLE_ID" "$STAGED_APP" \
    || local_sign_status=$?
  if [ "$local_sign_status" -ne 0 ]; then
    cat "$codesign_out" >&2
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      # The one failure the person cannot see, because the dialog it is waiting on may be
      # behind another window or on another Space.
      echo "!! codesign did not finish within ${CLAWDLINE_CODESIGN_TIMEOUT}s" >&2
      echo "   That is what an unanswered Keychain access dialog looks like from here." >&2
      echo "   Configure the key's code-signing access yourself in Keychain Access or with" >&2
      echo "   SecurityTool. Its set-key-partition-list command requires '-k password'," >&2
      echo "   which Clawdline does not accept or pass." >&2
    else
      echo "!! signing with $LOCAL_SIGN_IDENTITY_NAME failed (exit $local_sign_status)" >&2
    fi
    echo "   Or build ad-hoc:    CLAWDLINE_SIGN_ADHOC=1 ./build.sh" >&2
    rm -f "$codesign_out" "$codesign_out.timed-out"
    exit "$local_sign_status"
  fi
  echo "✓ signed with stable local identity $LOCAL_SIGN_IDENTITY_NAME"
  echo "  After changing signing identity, first use may show up to three Keychain prompts (machine credential and two Cloud keys); approve each item you use."
else
  signed=0
  release_sign_status=0
  for attempt in 1 2 3; do
    if clawdline_bounded "$CLAWDLINE_CODESIGN_TIMEOUT" "$codesign_out" \
        codesign --force --sign "$SIGN_IDENTITY" --identifier "$BUNDLE_ID" \
        --options runtime --timestamp --entitlements Resources/Clawdline.entitlements \
        "$STAGED_APP"; then
      signed=1
      break
    else
      release_sign_status=$?
    fi
    cat "$codesign_out" >&2
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      echo "!! Developer ID codesign timed out after ${CLAWDLINE_CODESIGN_TIMEOUT}s; staged bundle state is unknown" >&2
      exit 124
    fi
    [ "$attempt" = 3 ] && break
    echo "  Apple timestamp service did not answer; retrying Developer ID signing ($attempt/3)"
    sleep $((attempt * 5))
  done
  [ "$signed" = 1 ] || {
    echo "!! Developer ID signing failed after 3 attempts"
    rm -f "$codesign_out" "$codesign_out.timed-out"
    exit "$release_sign_status"
  }
fi
rm -f "$codesign_out" "$codesign_out.timed-out"
# END keychain-rebuild-focused: signing branches

# Packaging and CI build beside the installed app but must never inspect, stop, replace, or reopen
# the live Clawdline process. Their caller owns the fresh output path and receives only the bundle.
if [ "${CLAWDLINE_BUILD_ONLY:-0}" = 1 ]; then
  [ ! -e "$APP" ] || { echo "!! build-only destination already exists: $APP"; exit 1; }
  mv "$STAGED_APP" "$APP"
  echo "✓ built $APP"
  exit 0
fi

# Nothing installed or running has been touched until here. Remember the state at the instant
# of replacement — somebody who deliberately quit while a long build was running should not
# have the app reopened against their wishes.
clawdline_run_file_phase installing
echo "→ installing finished build"
WAS_RUNNING=0
pgrep -x Clawdline >/dev/null 2>&1 && WAS_RUNNING=1

# A dispatched task lives through a restart once it has been briefed: its durable record is on disk,
# its secret has reached the child, and its child is a terminal tab this script does not touch. A
# `briefed` task is therefore evidence of live work, not a restart blocker. In the seconds
# *before* that,
# it does not — the plaintext secret is only in the old process's memory, so a child whose tab has
# opened but whose first message has not been typed can never be briefed, and comes back as
# `spawn_failed: the app restarted before the child was briefed`.
#
# That window is about a second wide per task, which is small until several people share this
# working copy: one session runs ./build.sh, another one's grandchildren die, and the message it
# gets back blames the app rather than the person who rebuilt it. So look, and say so. Not a
# refusal — this is somebody's own machine and their own build — just the one fact that turns a
# baffling failure into an obvious one.
if [ "$WAS_RUNNING" = 1 ] && command -v curl >/dev/null 2>&1; then
  PORT=$(/usr/bin/python3 -c 'import json,os;print(json.load(open(os.path.expanduser("~/.config/clawdline/config.json"))).get("remote_port",7717))' 2>/dev/null || echo 7717)
  TOKEN_FILE="$HOME/.config/clawdline/orchestrator-token"
  if [ -r "$TOKEN_FILE" ]; then
    if ! TASK_SNAPSHOT=$(curl -s --max-time 2 \
        "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" 2>/dev/null); then
      TASK_SNAPSHOT=
    fi
    MIDFLIGHT=$(printf '%s' "$TASK_SNAPSHOT" | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
for x in t:
    if x.get("state") in ("queued", "spawning"):
        print("  %s  %s" % (x.get("id","")[:8], x.get("title","")[:48]))' 2>/dev/null)
    BRIEFED=$(printf '%s' "$TASK_SNAPSHOT" | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
for x in t:
    if x.get("state") == "briefed":
        print("  %s  %s" % (x.get("id","")[:8], x.get("title","")[:48]))' 2>/dev/null)
    if [ -n "$BRIEFED" ]; then
      echo "→ briefed task(s) are durable and do not block this restart"
      echo "$BRIEFED"
    fi
    if [ -n "$MIDFLIGHT" ]; then
      # Wait rather than warn. The window is seconds wide and closes on its own, while the thing
      # on the other side of it is somebody's dispatched task dying with a message that blames
      # the app. A printed warning is the right shape for a person and the wrong one here: on a
      # machine where several sessions share a checkout, the one running this is usually another
      # agent, and it will not stop to read a line it did not ask for.
      echo "→ a dispatched task is mid-spawn; waiting for it to be briefed (up to 90s)"
      echo "$MIDFLIGHT"
      for _ in $(seq 1 90); do
        sleep 1
        STILL=$(curl -s --max-time 2 "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
            -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" 2>/dev/null \
          | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
print("".join("x" for x in t if x.get("state") in ("queued", "spawning")))' 2>/dev/null)
        [ -z "$STILL" ] && { echo "   clear — carrying on"; break; }
      done
      # Ninety seconds is the whole of the patience. Past that the task is not mid-spawn any
      # more, it is stuck, and holding a build hostage to it helps nobody.
      [ -n "$STILL" ] && echo "   still spawning after 90s; restarting anyway"
    fi
  fi
fi

# New runtimes own the replacement boundary: close admission, let already-admitted Apple Events
# drain, persist `ready`, and keep admission closed until the replacement's own complete Session
# inventory reconciles every briefed executor. The first build that installs this protocol is
# necessarily talking to an older runtime; only an exact 404 takes the documented bootstrap path
# above. Every other refusal is typed and stops before the running app is touched.
MAINTENANCE_REQUEST_ID=
MAINTENANCE_ACTIVE=0
if [ "$WAS_RUNNING" = 1 ] && command -v curl >/dev/null 2>&1 && [ -r "${TOKEN_FILE:-}" ]; then
  MAINTENANCE_REQUEST_ID=$(/usr/bin/uuidgen | tr '[:upper:]' '[:lower:]')
  MAINTENANCE_REPLY=$(mktemp "$STAGE_ROOT/maintenance.XXXXXX")
  MAINTENANCE_STATUS=$(curl -sS --max-time 5 -o "$MAINTENANCE_REPLY" -w '%{http_code}' \
      -X POST "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
      -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
      -H 'Content-Type: application/json' \
      -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" || true)
  if [ "$MAINTENANCE_STATUS" = 404 ]; then
    echo "→ installed runtime has no restart-maintenance route; using one-time bootstrap preflight"
    MAINTENANCE_REQUEST_ID=
  elif [ "$MAINTENANCE_STATUS" != 200 ]; then
    echo "!! restart maintenance was refused (HTTP ${MAINTENANCE_STATUS:-unreachable})"
    /usr/bin/python3 -c 'import json,sys
try:
    e=json.load(open(sys.argv[1])).get("error",{})
    print("   %s: %s" % (e.get("code","unknown"),e.get("message","no message")))
except Exception: pass' "$MAINTENANCE_REPLY"
    exit 1
  else
    MAINTENANCE_ACTIVE=1
    echo "→ restart maintenance admitted; waiting for the terminal broker to drain"
    for _ in $(seq 1 120); do
      PHASE=$(/usr/bin/python3 -c 'import json,sys
try: print(json.load(open(sys.argv[1])).get("restart",{}).get("phase",""))
except Exception: pass' "$MAINTENANCE_REPLY")
      [ "$PHASE" = ready ] && break
      curl -sS --max-time 5 -o "$MAINTENANCE_REPLY" \
        "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" || true
      PHASE=$(/usr/bin/python3 -c 'import json,sys
try: print(json.load(open(sys.argv[1])).get("restart",{}).get("phase",""))
except Exception: pass' "$MAINTENANCE_REPLY")
      [ "$PHASE" = ready ] && break
      sleep 1
    done
    if [ "${PHASE:-}" != ready ]; then
      curl -sS --max-time 5 -X DELETE \
        "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
        -H 'Content-Type: application/json' \
        -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null || true
      echo "!! terminal broker did not drain in 120s; maintenance was aborted and nothing was replaced"
      exit 1
    fi
  fi
fi

pkill -x Clawdline 2>/dev/null || true
# **`pkill` asks; it does not wait.** Moving the bundle while a process on its way out is still
# reading it can make AppKit teardown ask CoreFoundation for files that have just moved away.
# Wait for the old process to be genuinely gone before the two quick renames below.
for _ in $(seq 1 60); do
  pgrep -x Clawdline >/dev/null 2>&1 || break
  sleep 0.1
done

# Keep the old bundle recoverable until the staged one is in its final name. Both moves are on
# the same filesystem; if the second one fails, put the first one back before reporting failure.
HAD_OLD=0
if [ -e "$APP" ]; then
  if ! mv "$APP" "$BACKUP"; then
    echo "!! could not move the existing app aside; it has not been changed"
    [ "$WAS_RUNNING" = "1" ] && open "$APP"
    exit 1
  fi
  HAD_OLD=1
fi
if ! mv "$STAGED_APP" "$APP"; then
  echo "!! could not install the finished build"
  if [ "$HAD_OLD" = "1" ]; then
    if mv "$BACKUP" "$APP"; then
      echo "   restored the previous app"
    else
      echo "   previous app is still recoverable at: $BACKUP"
    fi
  fi
  [ "$WAS_RUNNING" = "1" ] && [ -d "$APP" ] && open "$APP"
  exit 1
fi
[ "$HAD_OLD" = "1" ] && rm -rf "$BACKUP"

if [ "$WAS_RUNNING" = "1" ]; then
  open "$APP"
  # **Say it came back only if it came back.** This used to print "relaunched" the instant
  # `open` returned, which says nothing: `open` hands the request to Launch Services and exits.
  # A build that killed the app and failed to start it again reported success, and the person
  # watching saw their bar vanish with no reason given — which reads as a crash, not as a build.
  for _ in $(seq 1 50); do
    pgrep -x Clawdline >/dev/null 2>&1 && break
    sleep 0.1
  done
  if pgrep -x Clawdline >/dev/null 2>&1; then
    if [ "$MAINTENANCE_ACTIVE" = 1 ]; then
      echo "→ replacement is listening; waiting for fresh executor reconciliation"
      RECONCILED=0
      for _ in $(seq 1 180); do
        RESTART_REPLY=$(curl -sS --max-time 5 \
          "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
          -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" || true)
        PHASE=$(printf '%s' "$RESTART_REPLY" | /usr/bin/python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("restart",{}).get("phase",""))
except Exception: pass')
        if [ "$PHASE" = complete ]; then RECONCILED=1; break; fi
        sleep 1
      done
      if [ "$RECONCILED" != 1 ]; then
        curl -sS --max-time 5 -X DELETE \
          "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
          -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
          -H 'Content-Type: application/json' \
          -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null || true
        echo "!! replacement did not reconcile in 180s; maintenance was explicitly aborted"
        exit 1
      fi
      MAINTENANCE_ACTIVE=0
    fi
    echo "✓ done (relaunched, since it was running before)"
  else
    echo "!! it was running before and did not come back — start it with:"
    echo "   open \"$APP\""
    exit 1
  fi
else
  echo "✓ done (it was not running, so it was left closed)"
fi
echo "  run:    open \"$APP\""
echo "  config: ~/.config/clawdline/config.json"
