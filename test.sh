#!/bin/bash
# Compile and run the test binary.
#
# Sources/main.swift is excluded: it is top-level code that starts the app, and two
# entry points cannot live in one binary. Everything else compiles in, so the tests
# exercise the same code the app ships rather than a copy of it.
set -euo pipefail

expected_cloud_receipt='CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=12 suites=CloudEnvelope:66,CloudAccount:105,CloudTransport:67,CloudAppBridge:49,CloudSettings:59,ScheduleResume:12,CloudClock:47,CloudCanonicalJSON:91,CloudCommandLedger:101,CloudOutboundSpool:141,CloudPairing:172,CloudLifecycle:87'
# The signed-release baseline has an observed 6,781-check receipt. Root Assignment adds 82
# executed checks, Usage Portfolio adds 43, Milestone adds 15, inline Codex patches add 15,
# the typed planning graph adds 14, the Cloud bridge lifecycle adds 75, and the Usage mobile,
# schedule-title, Feature-status and Claude-only spending correction adds 3. Splitting owned-child
# dispatch from detached automation adds 8. Clawdfather succession adds 75. Root Assignment
# event-time delivery receipts add 16. Bounded Keychain writes add 22. The consolidated
# Keychain/signing correction adds 43 more: CloudAccount
# 92 -> 105, CloudSettings 40 -> 59 and CloudLifecycle 76 -> 87. Every added check is
# unconditional and outside a loop, so the delta is arithmetic rather than an estimate; the
# paired checks inside do/catch contribute one each.
# Publication-bound Session process evidence and bounded subprocess cleanup add 35 checks; removing
# five obsolete main-crossing sites and replacing the four-check coordinator wait fixture with
# five direct-publication checks makes the net change 31.
# The exact candidate-tree run remains authoritative and must update this guard if its observed
# final count differs.
# Transcript first-paint isolation adds 25 unconditional checks: three route/tier predicates and
# twenty-two admission, completion, drain, interruption and queue-responsiveness checks.
# Browser token adoption adds 7 unconditional checks for the 303/cookie contract and public health.
# Stale-while-revalidate readings add 68 unconditional checks: 24 for the freshness policy
# itself (fresh service, stale-then-refresh ordering, a refusal ageing rather than replacing a
# reading, and the serveFor edge), 21 for single-flight admission and the answer a waiter gets
# when the read it joined was refused, 14 for moving the lane's depth to where it can only refuse
# a request that had nothing to serve, and 9 for the trace that keeps queueing apart from cost.
# Reading the process list in one language adds 7 unconditional checks. The fixture beside them
# was written in English by hand, which is how a Mac running zh_TW.UTF-8 came to find none of its
# seventeen assistants: `ps -o lstart=` renders four fields there instead of five, the parser falls
# back to the no-lstart offset, and every row is dropped. Three of these ask the real `ps` through
# the real helper rather than a fixture, so the guard fails on the machine that would have the bug.
# Coexisting with iTerm2's native tmux control mode adds 40 unconditional checks: 11 for reading
# `#{client_flags}` off `list-clients`, 22 for what becomes of an iTerm2 row that has an identity
# and no pty — including that the marker key is the same word in `iterm.js` and in Swift — and 7
# for whether selecting a tmux pane may also bring iTerm2 forward. 7448 -> 7488.
# Correcting that attribution adds 31 more, in the same three groups: 9 for the window count that
# gives it a ceiling and for a tmux this app cannot find being a question that was never asked,
# 16 for the ceiling itself and for a control-mode client having to be speaking over a pty iTerm2
# named in the same reading, and 6 for that identity deciding activation and for the activation
# tail leaving the caller's thread. 7488 -> 7519.
# tmux read parity adds 56 unconditional checks in four groups, counted by running each group on
# its own rather than by reading the source: 30 for one subprocess answering for every pane — the
# script it sends, the marker that has no `%` for strftime to eat, the parsing of what comes back,
# and ten panes costing one invocation with a dead one among them — 14 for a server Clawdline
# starts detached and types its line into rather than handing to tmux as a command, 8 for the
# screen a decision reads being the screen and not its history, and 4 more in the existing
# terminal-plan group for tmux installed with no server being its own answer. 7542 -> 7598.
# Correcting that slice adds 28, counted the same way — each group run on its own. 17 are a new
# group for what a failed tmux command proves about the server behind it, which is where the
# blocking defect was: a socket that has never existed says `error connecting to <path> (No such
# file or directory)` rather than `no server running on <path>`, so the Mac the detached server
# was written for was the one Mac it did not start on. 8 more in the batched-reading group, for a
# deadline and an unreachable socket no longer reading as a tmux too old for the script, and for
# the per-pane fallback itself, which no test had ever executed. 3 in the detached-server group,
# for the page saying that typing a line is not the shell running it and for a pane tmux would
# not type into being a failure. 7598 -> 7626.
# Extracting the store codec into `OrchestratorStore` adds 399 checks and changes nothing
# else: ten table-driven codec groups, one check per field per record, plus the legacy-shape
# fixtures each type's preserved branch is worth. Every pre-existing count is unmoved, which
# is what makes the relocation behaviour-neutral rather than merely green. The extraction was
# measured against 7519 and has been rebased twice since, each time on somebody else's landing
# rather than on anything it changed: the reader-paced question steps moved the baseline to
# 7542, and tmux read parity moved it to 7626. 7626 + 399 -> 8025.
expected_swift_receipt='8026 checks passed'

count_exact_receipt_lines() {
  local receipt=$1
  local log=$2
  awk -v receipt="$receipt" '$0 == receipt { count++ } END { print count + 0 }' "$log"
}

verify_test_completion_receipts() {
  local log=$1
  local cloud_receipt_count swift_receipt_count reported_swift_receipts
  cloud_receipt_count=$(count_exact_receipt_lines "$expected_cloud_receipt" "$log")
  if [ "$cloud_receipt_count" -ne 1 ]; then
    echo "Cloud test completion receipt appeared $cloud_receipt_count times, expected exactly once — full output kept at $log" >&2
    return 125
  fi

  swift_receipt_count=$(count_exact_receipt_lines "$expected_swift_receipt" "$log")
  if [ "$swift_receipt_count" -ne 1 ]; then
    reported_swift_receipts=$(awk '/^[0-9]+ checks passed$/ { values = values (values ? ", " : "") $0 } END { print values ? values : "none" }' "$log")
    echo "Swift test completion receipt mismatch: expected exactly one '$expected_swift_receipt'; found $swift_receipt_count exact and reported $reported_swift_receipts — full output kept at $log" >&2
    return 125
  fi
}

# This narrow mode exercises the full-suite completion guard without compiling or running the
# suite. It never emits a completion receipt of its own and cannot be mistaken for a full run.
if [ "${1:-}" = "--verify-completion-receipts" ]; then
  if [ "$#" -ne 2 ]; then
    echo "usage: $0 --verify-completion-receipts <suite-log>" >&2
    exit 2
  fi
  verify_test_completion_receipts "$2"
  exit $?
fi

cd "$(dirname "$0")"
. tools/swift-source-manifest.sh
verify_swift_source_manifest full
bash tools/check-architecture-boundaries.sh

# Trailing commas in an argument list are Swift 6.1 syntax. The toolchain here is usually
# newer than CI's, so code that compiles locally can fail to parse on the runner — and the
# error arrives ten minutes later, in a log, attached to a push that is already public.
offenders=$(awk '
  $0 ~ /,[[:space:]]*\)/            { print FILENAME ":" FNR ": " $0 }
  prev ~ /,[[:space:]]*$/ && $0 ~ /^[[:space:]]*\)/ { print FILENAME ":" FNR-1 ": " prev }
  { prev = $0 }
' "${clawdline_production_sources[@]}" "${clawdline_test_sources[@]}")
if [ -n "$offenders" ]; then
  echo "trailing comma before ) — Swift 6.1 syntax, and CI runs something older:"
  echo "$offenders"
  exit 1
fi

# The compatibility page is generated from the table the app uses, so the two cannot disagree
# — but only if something checks. A release added to Compat.swift and not regenerated here is a
# page claiming support for a version that was never tried.
tools/build-compatibility.py --check
tools/check-web-strings.py
tools/check-web-ids.py
node Tests/docs-ui-labels.mjs
node Tests/agent-attention-principle.mjs

# The checked-in protocol fixture is the cross-runtime byte authority. Generate the expected
# bytes in memory and compare through the generator's read-only mode so hand edits fail closed.
swift tools/generate-protocol-vectors.swift --check Tests/protocol-vectors.json

# Keep the small browser-independent renderer contracts beside the Swift suite. The web app's
# scoped package.json marks its shipped files as ESM, matching the browser's module entry.
browser_contract_suites=(
  Tests/web-schedules.mjs
  Tests/web-coordinator.mjs
  Tests/web-clawdfather.mjs
  Tests/web-optimistic.mjs
  Tests/web-transcript-requests.mjs
  Tests/web-session-resilience.mjs
  Tests/web-viewport.mjs
  Tests/web-layout-diagnostics.mjs
  Tests/web-session-disposition.mjs
  Tests/web-session-closeability.mjs
  Tests/web-title-transport.mjs
  Tests/web-code-copy.mjs
  Tests/web-message-images.mjs
  Tests/web-project-artifacts.mjs
  Tests/web-row-gesture.mjs
)
if [ "${#browser_contract_suites[@]}" -ne 15 ]; then
  echo "browser contract roster changed without updating its sealed count" >&2
  exit 1
fi
for browser_contract_suite in "${browser_contract_suites[@]}"; do
  node "$browser_contract_suite"
done
# The hosted console: which transport it is, the pairing mirror against the checked-in
# vectors, and that the static bundle a person uploads by hand is the same bytes twice.
node Tests/web-cloud-boot.mjs
node Tests/web-cloud-pairing.mjs
node Tests/web-cloud-onboarding.mjs
node Tests/web-app-build.mjs
node Tests/dispatch-role-contract.mjs
node Tests/restart-rollout-contract.mjs
node Tests/remote-response-write-close.mjs
node Tests/release-signing-contract.mjs
# The onboarding policy, compiled out of Sources/Onboarding.swift without its AppKit half: that a
# config switch is not readiness, that an allocated credential is not a connection, and that the
# installer reopens the exact bundle it just wrote. It runs here rather than in the Swift suite
# because the shipped policy has no AppKit dependency and this keeps it a second rather than a
# recompile of everything.
node Tests/app-onboarding-focused.mjs
node Tests/keychain-rebuild-focused.mjs
# Two suites that existed and that nothing ran: neither was in this list, and CI only runs
# this script. A test nobody runs is a test that passes.
node Tests/web-user-messages.mjs
node Resources/web/app/js/net/client.test.mjs
# These two are about this script rather than the app: that a crashed run still leaves its output,
# and that the machine-wide suite lock below serialises the expensive half. Both run before the
# lock is taken, so a machine that is already busy still gets told what is wrong with this checkout
# before it starts queueing.
node Tests/test-sh-streaming.mjs
node Tests/test-sh-lock.mjs

# >>> clawdline suite lock >>>
# One machine, one suite run — and this block is the whole of that promise. It is bounded by the two
# marker comments so `Tests/test-sh-lock.mjs` can lift it out and drive it against cheap stand-ins,
# the way `Tests/test-sh-streaming.mjs` lifts out the pipeline. Rename a marker and that guard fails
# loudly rather than quietly scanning nothing.
#
# **Why it exists.** The `swiftc` below compiles every file in `Sources/` together with the files in
# `Tests/` in one invocation, and spawns one `swift-frontend` per job. On 2026-09-03 four of them
# held 46 / 45 / 27 / 8 GB at once on a 24 GB Mac and Jetsam force-rebooted it twice, at 01:24 and
# at 01:45 — see /Library/Logs/DiagnosticReports/JetsamEvent-2026-09-03-014340.ips. Several sessions
# share this checkout, and until now the mitigation was a gentleman's agreement that whoever ran the
# suite would `mkdir /tmp/clawdline-suite.lock` first. Forgetting cost the whole machine, so the
# script holds the lock itself.
#
# **What the lock covers**: the `swiftc` invocation *and* the test-binary run that follows it. The
# compile is the memory, the run is minutes of CPU, and the agreement this replaces covered both.
# Release is on EXIT rather than on a line after the run, because every failure between here and the
# end of the script leaves through `exit`, and EXIT is the one path all of them share.
#
# **Liveness is proved by renewal, not by a pid existing.** A pid is a proxy and proxies outlive the
# work: a holder on this machine recorded a `sleep 14400` sentinel, the work it stood for died,
# launchd adopted the sleep, and under a pid-existence rule that lock would have blocked every other
# session until the sentinel's own four-hour timeout. So the holder refreshes `holder.txt` while it
# works, and a holder that stops refreshing stops proving it is there. The distinction is the whole
# of the rule and is worth saying plainly: **a clock on the work is wrong** — a four-hour compile is
# not stale, and a duration timeout is exactly the draft that was withdrawn here — **a clock on the
# proof of life is right**, because a holder renewing every 20s never trips a 60s renewal deadline
# however long its work runs.
#
# **Admission is fail-closed and the physical backstop is never waived.** The lock is handed on only
# when BOTH (A) the holder has stopped proving it is alive AND (B) no compiler process exists
# anywhere on this machine. (B) on its own would hand the lock to a second run in the gaps *between*
# one study's compiles; (A) on its own would hand it over while an orphaned compile is still
# spending the 46 GB this lock exists to ration. Evidence that is missing, stale or ambiguous reads
# `unknown` and **blocks**; it never reads "dead".
#
# **Nothing here kills anything** except the renewal loop this script starts for itself. Not the
# holder, not an orphaned compiler, not a process group. It queues, it refuses, and it names who to
# ask.

# Everything the tests must vary is an environment variable, so `Tests/test-sh-lock.mjs` can drive
# this block without ever going near the real lock or the real compiler.
CLAWDLINE_SUITE_LOCK_DIR="${CLAWDLINE_SUITE_LOCK_DIR:-/tmp/clawdline-suite.lock}"
# `pgrep -x` matches the executable's own name. Measured against a live compile here rather than
# assumed: a running `swiftc` shows up as `swift-frontend` under `pgrep -x`, while `ps -A -o comm=`
# prints the whole toolchain path and matches no bare name at all.
CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN="${CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN:-swift-frontend}"
CLAWDLINE_SUITE_LOCK_RENEW_SECONDS="${CLAWDLINE_SUITE_LOCK_RENEW_SECONDS:-20}"
# Three renewals of slack. A reader prefers the deadline the holder recorded in its own record, so a
# holder that renews more slowly than this reader expects is never declared dead by that reader.
CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS="${CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS:-60}"
CLAWDLINE_SUITE_LOCK_WAIT_SECONDS="${CLAWDLINE_SUITE_LOCK_WAIT_SECONDS:-3600}"
CLAWDLINE_SUITE_LOCK_POLL_SECONDS="${CLAWDLINE_SUITE_LOCK_POLL_SECONDS:-5}"
CLAWDLINE_SUITE_LOCK_NOTICE_SECONDS="${CLAWDLINE_SUITE_LOCK_NOTICE_SECONDS:-30}"
CLAWDLINE_SUITE_LOCK_DONE_FLAG="${CLAWDLINE_SUITE_LOCK_DONE_FLAG:-$CLAWDLINE_SUITE_LOCK_DIR/done}"
# 75 is EX_TEMPFAIL from sysexits(3) — "temporary failure, the user is invited to retry", which is
# what a busy lock is. It is distinct from every status this script already produces: 0, 1 (a guard
# said no), 2 (usage), 125 (receipt), 126 (tee) and the suite's own, which for a signal death is
# 128+N. Nothing else here returns 75.
CLAWDLINE_SUITE_LOCK_BUSY=75

# Where this run's output goes. Defined here rather than beside the pipeline further down, because
# the lock records it: a blocked run can then watch the run it is waiting for instead of guessing.
LOG="${LOG:-${TMPDIR:-/tmp}/clawdline-tests-$$.log}"

clawdline_suite_lock_token=""
clawdline_suite_lock_pid=""
clawdline_suite_lock_started=""
clawdline_suite_lock_pid_started=""
clawdline_suite_lock_renewer=""
clawdline_suite_lock_compilers=""
clawdline_suite_lock_state=""
clawdline_suite_lock_evidence=""

clawdline_suite_lock_new_token() {
  local minted
  minted=$(od -An -tx1 -N16 /dev/urandom 2>/dev/null | tr -d ' \n') || minted=""
  # `od | tr` succeeds and prints an empty string when /dev/urandom cannot be read, and an empty
  # token would match every lock on release. Fall back to something still unique to this run.
  case "$minted" in
    ????????????????*) : ;;
    *) minted="pid$$-$(date +%s)-${RANDOM}${RANDOM}" ;;
  esac
  printf '%s' "$minted"
}

clawdline_suite_lock_field() {
  # The value of one `key=` line, or nothing. Nothing is a real answer here — a record that has not
  # been written yet — so callers test the value rather than this function's status.
  local key=$1 file=$2
  [ -f "$file" ] || return 0
  awk -v key="$key" 'index($0, key "=") == 1 { print substr($0, length(key) + 2); exit }' "$file" 2>/dev/null
}

clawdline_suite_lock_pid_alive() {
  # `ps -p` rather than `kill -0`: `kill` reports a live process owned by another user as an error
  # that cannot be told apart from "no such process", and this lock is read by whoever is waiting.
  local pid=$1
  case "$pid" in "" | *[!0-9]*) return 1 ;; esac
  ps -p "$pid" -o pid= >/dev/null 2>&1
}

clawdline_suite_lock_pid_identity() {
  # A pid's start time, as one normalised line, used to tell a recorded holder from a later process
  # that inherited its number. **Both sides of every comparison come out of this one function**,
  # which is why `LC_ALL=C` is pinned here and not at the call sites. Measured on this Mac: the same
  # process reads `Thu Sep  3 02:18:04 2026` under `LC_ALL=C` and `四  9/ 3 02:18:04 2026` under the
  # machine's own `zh_TW.UTF-8`. Nor is the difference only in the bytes — holding the formatter
  # still and varying the day, `zh_TW` renders five whitespace-separated tokens on 2026-09-03 and
  # four on 2026-08-31, while `LC_ALL=C` renders five on both. So nothing here counts fields; the
  # whole line is normalised and compared as a string, from one formatter on both sides.
  local pid=$1 line
  case "$pid" in "" | *[!0-9]*) printf 'unknown'; return 0 ;; esac
  line=$(LC_ALL=C ps -o lstart= -p "$pid" 2>/dev/null | awk 'NR == 1 { $1 = $1; print; exit }') || line=""
  printf '%s' "${line:-unknown}"
}

clawdline_suite_lock_probe_compilers() {
  # 0 = at least one compiler is running, 1 = none anywhere, 2 = the probe could not answer. `pgrep`
  # exits 1 when nothing matched, so a `||` here would read "no compiler" as a failure and a failure
  # as "no compiler"; the status is read explicitly instead, and only an explicit 1 is ever allowed
  # to mean the machine is clear.
  local found="" probe_status=0
  found=$(LC_ALL=C pgrep -x "$CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN" 2>/dev/null) || probe_status=$?
  clawdline_suite_lock_compilers=$(printf '%s' "$found" | tr '\n' ' ')
  case "$probe_status" in
    0) return 0 ;;
    1) return 1 ;;
    *) return 2 ;;
  esac
}

clawdline_suite_lock_working_pids() {
  # What is actually doing the work right now, refreshed on every renewal. A single pid cannot
  # describe a sequence of compiles, and that gap is precisely how a `sleep` came to be a holder.
  local exclude=$1 children=""
  children=$(LC_ALL=C pgrep -P "$clawdline_suite_lock_pid" 2>/dev/null) || children=""
  printf '%s' "$(printf '%s\n' "$children" | awk -v skip="$exclude" 'NF && $1 != skip { line = line (line ? " " : "") $1 } END { print line }')"
}

clawdline_suite_lock_write_record() {
  # Rewritten whole on acquisition and on every renewal, then moved into place, so a reader sees the
  # previous complete record or the new complete one and never half of either.
  local dir=$1 exclude=${2:-} temp="$1/.holder.$$.$RANDOM"
  clawdline_suite_lock_probe_compilers || true
  {
    printf 'holder=%s\n' "$CLAWDLINE_SUITE_LOCK_HOLDER"
    printf 'pid=%s\n' "$clawdline_suite_lock_pid"
    printf 'started=%s\n' "$clawdline_suite_lock_started"
    printf 'tree=%s\n' "$CLAWDLINE_SUITE_LOCK_TREE"
    printf 'log=%s\n' "$LOG"
    printf 'token=%s\n' "$clawdline_suite_lock_token"
    printf 'pid_started=%s\n' "$clawdline_suite_lock_pid_started"
    printf 'heartbeat=%s\n' "$(date +%s)"
    printf 'heartbeat_deadline=%s\n' "$CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS"
    printf 'working=%s\n' "$(clawdline_suite_lock_working_pids "$exclude")"
    printf 'compilers=%s\n' "${clawdline_suite_lock_compilers:-none}"
    printf 'done_flag=%s\n' "$CLAWDLINE_SUITE_LOCK_DONE_FLAG"
    printf 'note=%s\n' "$CLAWDLINE_SUITE_LOCK_NOTE"
  } > "$temp" 2>/dev/null || return 1
  mv "$temp" "$dir/holder.txt" 2>/dev/null || { rm -f "$temp" 2>/dev/null; return 1; }
  return 0
}

clawdline_suite_lock_admission() {
  # Reads somebody else's record and leaves two globals: `clawdline_suite_lock_state`, one of
  # `held` / `unknown` / `orphaned` / `stale`, and `clawdline_suite_lock_evidence`, the sentence
  # that says what was read. It sets globals rather than printing because `$(…)` would run it in a
  # subshell and throw the evidence away — and a refusal that cannot name its evidence is a refusal
  # nobody can act on.
  local file="$1/holder.txt" heartbeat deadline now age probe_status pid pid_started identity done_flag
  heartbeat=$(clawdline_suite_lock_field heartbeat "$file")
  deadline=$(clawdline_suite_lock_field heartbeat_deadline "$file")
  pid=$(clawdline_suite_lock_field pid "$file")
  pid_started=$(clawdline_suite_lock_field pid_started "$file")
  done_flag=$(clawdline_suite_lock_field done_flag "$file")
  case "$deadline" in "" | *[!0-9]* | 0) deadline="$CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS" ;; esac

  # The done flag is a **positive** signal only. Present means the guarded work is over, so with the
  # backstop still satisfied the lock may be handed on at once rather than after a renewal deadline
  # nobody is waiting for. Absent proves nothing — a run killed with SIGKILL never writes one — so
  # absence simply falls through to renewal below. Reading absence as "still running" would rebuild
  # the permanent roadblock somewhere new.
  if [ -n "$done_flag" ] && [ -f "$done_flag" ]; then
    clawdline_suite_lock_probe_compilers; probe_status=$?
    case "$probe_status" in
      1) clawdline_suite_lock_state="stale"
         clawdline_suite_lock_evidence="the holder marked its work finished at $done_flag and no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is running anywhere"
         return 0 ;;
      0) clawdline_suite_lock_state="orphaned"
         clawdline_suite_lock_evidence="the holder marked its work finished, but $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is still running as pid(s) ${clawdline_suite_lock_compilers}— the memory is still being spent, so nobody is admitted and nothing here will kill them"
         return 0 ;;
      *) clawdline_suite_lock_state="unknown"
         clawdline_suite_lock_evidence="the holder marked its work finished, but the $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN probe could not answer — unknown blocks"
         return 0 ;;
    esac
  fi

  case "$heartbeat" in
    "" | *[!0-9]*)
      clawdline_suite_lock_state="unknown"
      clawdline_suite_lock_evidence="its record carries no readable heartbeat, so whether anyone is still there is unknown — and unknown blocks rather than reading as dead"
      return 0 ;;
  esac
  now=$(date +%s)
  age=$(( now - heartbeat ))
  if [ "$age" -lt 0 ]; then
    clawdline_suite_lock_state="unknown"
    clawdline_suite_lock_evidence="its heartbeat is ${age#-}s in the future, so the two clocks disagree and the evidence is ambiguous — ambiguous blocks"
    return 0
  fi
  if [ "$age" -le "$deadline" ]; then
    clawdline_suite_lock_state="held"
    identity=$(clawdline_suite_lock_pid_identity "$pid")
    if [ "$identity" = "$pid_started" ]; then
      clawdline_suite_lock_evidence="it renewed ${age}s ago against a ${deadline}s deadline; working: $(clawdline_suite_lock_field working "$file"), compilers: $(clawdline_suite_lock_field compilers "$file")"
    else
      # A fresh renewal outranks a pid, always. The pid is reported because it is useful to a
      # person, never because it decides anything.
      clawdline_suite_lock_evidence="it renewed ${age}s ago against a ${deadline}s deadline, so something is still proving it is there, though pid $pid no longer looks like the process that took the lock"
    fi
    return 0
  fi
  # The holder has stopped proving it is alive. That admits nobody on its own: the backstop is
  # physical, and it is never waived.
  clawdline_suite_lock_probe_compilers; probe_status=$?
  case "$probe_status" in
    1) clawdline_suite_lock_state="stale"
       clawdline_suite_lock_evidence="its last renewal was ${age}s ago, past its own ${deadline}s deadline, and no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is running anywhere on this machine" ;;
    0) clawdline_suite_lock_state="orphaned"
       clawdline_suite_lock_evidence="its last renewal was ${age}s ago, past its own ${deadline}s deadline, but $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is still running as pid(s) ${clawdline_suite_lock_compilers}— an orphaned compile is still spending the memory this lock rations, so nobody is admitted and nothing here will kill them" ;;
    *) clawdline_suite_lock_state="unknown"
       clawdline_suite_lock_evidence="its last renewal was ${age}s ago, but the $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN probe could not answer, so whether a compile is running is unknown — and unknown blocks" ;;
  esac
  return 0
}

clawdline_suite_lock_release_gate() {
  local gate=$1
  if [ "$(clawdline_suite_lock_field pid "$gate/holder.txt")" = "$$" ]; then
    rm -rf "$gate"
  fi
}

clawdline_suite_lock_take_over() {
  # The compare and the swap.
  #
  # `rename(2)` decides which of several waiters that judged the *same* lock stale gets to remove it:
  # exactly one `mv` can succeed and the losers fail with ENOENT. That alone is not enough, because a
  # waiter's judgement can be older than a whole takeover — B reads a stale record; A takes over,
  # acquires and starts compiling; B then renames A's *fresh* lock away and both are inside. So the
  # judgement is made again here, under a gate directory only one waiter can hold. While the lock
  # directory exists no `mkdir` can create it, and the only thing that removes it is this function,
  # so holding the gate across the re-read means no new holder can appear between the compare and
  # the swap. The token is the compare: the swap happens only if the record is still the one judged.
  local lock=$1 judged_token=$2
  local gate="$lock.takeover" gate_pid stale
  if ! mkdir "$gate" 2>/dev/null; then
    gate_pid=$(clawdline_suite_lock_field pid "$gate/holder.txt")
    if ! clawdline_suite_lock_pid_alive "$gate_pid"; then
      # Its taker died between creating the gate and removing it. Exactly one waiter may clear it,
      # for the same reason and by the same means as above.
      if mv "$gate" "$gate.abandoned.$$" 2>/dev/null; then rm -rf "$gate.abandoned.$$"; fi
    fi
    return 1
  fi
  printf 'pid=%s\n' "$$" > "$gate/holder.txt"
  # And confirm the gate is still this waiter's before acting on it: if another waiter cleared it as
  # abandoned in the window just above, two could be holding it, and whichever no longer reads its
  # own pid backs out rather than swapping.
  gate_pid=$(clawdline_suite_lock_field pid "$gate/holder.txt")
  if [ "$gate_pid" != "$$" ]; then
    return 1
  fi
  clawdline_suite_lock_admission "$lock"
  if [ "$clawdline_suite_lock_state" = "stale" ] &&
     [ "$(clawdline_suite_lock_field token "$lock/holder.txt")" = "$judged_token" ]; then
    stale="$lock.stale.$$"
    if mv "$lock" "$stale" 2>/dev/null; then
      rm -rf "$stale"
      echo "suite lock: took over $lock — $clawdline_suite_lock_evidence"
      clawdline_suite_lock_release_gate "$gate"
      return 0
    fi
  fi
  clawdline_suite_lock_release_gate "$gate"
  return 1
}

clawdline_suite_lock_start_renewer() {
  # The proof of life, and it is bound to the work rather than to itself: on every tick it checks
  # that the run's own shell is still there and is still the same process, and that the lock is
  # still this run's, before it refreshes anything. That is what stops the renewer from becoming the
  # next sentinel — orphan it and it stops renewing on its first tick instead of holding the machine
  # for the rest of its natural life.
  local lock=$1
  (
    # A subshell inherits this script's EXIT trap — measured, not assumed — and running the release
    # from here would free the lock the moment the renewer stopped. Drop it first.
    trap - EXIT
    while :; do
      sleep "$CLAWDLINE_SUITE_LOCK_RENEW_SECONDS"
      clawdline_suite_lock_pid_alive "$clawdline_suite_lock_pid" || exit 0
      [ "$(clawdline_suite_lock_pid_identity "$clawdline_suite_lock_pid")" = "$clawdline_suite_lock_pid_started" ] || exit 0
      [ "$(clawdline_suite_lock_field token "$lock/holder.txt")" = "$clawdline_suite_lock_token" ] || exit 0
      # Its own pid comes from the file the shell below writes, because bash 3.2 has no `BASHPID`
      # and every way of asking for it from in here forks something whose pid is not this one.
      clawdline_suite_lock_write_record "$lock" "$(cat "$lock/.renewer" 2>/dev/null || true)" || exit 0
    done
  ) &
  clawdline_suite_lock_renewer=$!
  printf '%s' "$clawdline_suite_lock_renewer" > "$lock/.renewer" 2>/dev/null || true
}

clawdline_acquire_suite_lock() {
  local lock="$CLAWDLINE_SUITE_LOCK_DIR"
  local started_at now waited next_notice holder_name holder_pid holder_started judged_token
  started_at=$(date +%s)
  next_notice=0
  while :; do
    if mkdir "$lock" 2>/dev/null; then
      clawdline_suite_lock_token=$(clawdline_suite_lock_new_token)
      clawdline_suite_lock_pid=$$
      clawdline_suite_lock_started=$(date '+%Y-%m-%d %H:%M:%S')
      clawdline_suite_lock_pid_started=$(clawdline_suite_lock_pid_identity "$$")
      if ! clawdline_suite_lock_write_record "$lock"; then
        echo "suite lock: could not write $lock/holder.txt — refusing to compile behind a lock that says nothing about who holds it" >&2
        rm -rf "$lock"
        return "$CLAWDLINE_SUITE_LOCK_BUSY"
      fi
      clawdline_suite_lock_start_renewer "$lock"
      echo "suite lock: $lock is this run's (pid $$), renewed every ${CLAWDLINE_SUITE_LOCK_RENEW_SECONDS}s against a ${CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS}s deadline"
      return 0
    fi
    now=$(date +%s)
    waited=$(( now - started_at ))
    holder_name=$(clawdline_suite_lock_field holder "$lock/holder.txt")
    holder_pid=$(clawdline_suite_lock_field pid "$lock/holder.txt")
    holder_started=$(clawdline_suite_lock_field started "$lock/holder.txt")
    judged_token=$(clawdline_suite_lock_field token "$lock/holder.txt")
    clawdline_suite_lock_admission "$lock"
    if [ "$clawdline_suite_lock_state" = "stale" ]; then
      if clawdline_suite_lock_take_over "$lock" "$judged_token"; then
        continue
      fi
    fi
    if [ "$waited" -ge "$CLAWDLINE_SUITE_LOCK_WAIT_SECONDS" ]; then
      echo "suite lock: gave up after ${waited}s. $lock is held by ${holder_name:-an unnamed run} (pid ${holder_pid:-unknown}, started ${holder_started:-unknown}) — $clawdline_suite_lock_evidence." >&2
      echo "suite lock: nothing was compiled and nothing was killed. Read $lock/holder.txt and ask that run." >&2
      return "$CLAWDLINE_SUITE_LOCK_BUSY"
    fi
    if [ "$waited" -ge "$next_notice" ]; then
      echo "suite lock: waiting ${waited}s for ${holder_name:-an unnamed run} (pid ${holder_pid:-unknown}, started ${holder_started:-unknown}) — $clawdline_suite_lock_evidence"
      next_notice=$(( waited + CLAWDLINE_SUITE_LOCK_NOTICE_SECONDS ))
    fi
    sleep "$CLAWDLINE_SUITE_LOCK_POLL_SECONDS"
  done
}

clawdline_confirm_suite_lock() {
  # Called once between the compile and the run. The compile is the long unattended stretch, and a
  # run that lost the lock during it must not start a second expensive thing under somebody else's.
  local lock="$CLAWDLINE_SUITE_LOCK_DIR"
  if [ -n "$clawdline_suite_lock_token" ] &&
     [ "$(clawdline_suite_lock_field token "$lock/holder.txt")" = "$clawdline_suite_lock_token" ]; then
    return 0
  fi
  echo "suite lock: $lock is no longer this run's — refusing to start the test binary under somebody else's lock." >&2
  return "$CLAWDLINE_SUITE_LOCK_BUSY"
}

clawdline_suite_lock_work_finished() {
  # The positive half of the protocol: the guarded work is over, said in a way a waiter can read
  # even if this shell never reaches its release. Best effort by construction — a run killed with
  # SIGKILL writes nothing, and the readers know that absence proves nothing.
  : > "$CLAWDLINE_SUITE_LOCK_DONE_FLAG" 2>/dev/null || true
}

clawdline_release_suite_lock() {
  # Ownership-checked, and this is the answer to the first of the two `nohup` mistakes: an outer
  # shell that writes `trap 'rmdir "$LOCK"' EXIT`, backgrounds the suite and returns fires that trap
  # immediately, while the run it started is still compiling. Its pid is not the pid in the record
  # and it never held the token, so its release is a no-op that says so instead of freeing a lock
  # somebody is working behind.
  local lock="$CLAWDLINE_SUITE_LOCK_DIR" recorded_pid recorded_token
  case "$lock" in "" | "/") return 0 ;; esac
  [ -d "$lock" ] || return 0
  recorded_pid=$(clawdline_suite_lock_field pid "$lock/holder.txt")
  recorded_token=$(clawdline_suite_lock_field token "$lock/holder.txt")
  if [ -n "$clawdline_suite_lock_token" ] &&
     [ "$recorded_pid" = "$$" ] &&
     [ "$recorded_token" = "$clawdline_suite_lock_token" ]; then
    rm -rf "$lock"
    echo "suite lock: released $lock"
  else
    echo "suite lock: $lock is held by pid ${recorded_pid:-unknown}, not by this shell (pid $$) — left alone" >&2
  fi
  # Always 0. This runs from the EXIT trap under `set -e`, where a non-zero return would replace the
  # status the run was actually exiting with.
  return 0
}

clawdline_suite_exit_cleanup() {
  local status=$?
  # The only process this script ever signals is the renewal loop it started for itself. The lock
  # signals nobody else's, ever.
  # `wait` as well as `kill`, and not for tidiness: without it bash reports the job asynchronously
  # as `Terminated: 15` on stderr of every single run, and waiting also guarantees the renewer is
  # reaped before the release below rather than possibly writing a record after it.
  if [ -n "$clawdline_suite_lock_renewer" ]; then
    kill "$clawdline_suite_lock_renewer" 2>/dev/null || true
    wait "$clawdline_suite_lock_renewer" 2>/dev/null || true
    clawdline_suite_lock_renewer=""
  fi
  clawdline_release_suite_lock
  # Bash keeps exactly one EXIT trap, so a second `trap … EXIT` further down would silently replace
  # this one and leave the lock behind on every run. The `$STORE` cleanup that used to have a trap
  # of its own is composed here instead. `${STORE:-}` because the store is created after this trap
  # is installed: a run that dies in the compile has none to remove.
  if [ -n "${STORE:-}" ]; then rm -rf "$STORE"; fi
  return "$status"
}

clawdline_suite_lock_default_holder() {
  # Who to ask, not just what to blame. A terminal identity is worth more here than a username.
  local who where
  who="${USER:-$(id -un 2>/dev/null || echo unknown)}"
  where=$(tty 2>/dev/null) || where=""
  case "$where" in "" | "not a tty") where="no tty" ;; esac
  if [ -n "${ITERM_SESSION_ID:-}" ]; then
    where="iTerm2 ${ITERM_SESSION_ID#*:}"
  elif [ -n "${TMUX_PANE:-}" ]; then
    where="tmux pane ${TMUX_PANE}"
  fi
  printf '%s running ./test.sh (%s)' "$who" "$where"
}

clawdline_suite_lock_tree() {
  # The exact tree being verified. `--no-optional-locks` because this checkout is shared with other
  # sessions and an ordinary `git status` refreshes the index they are also using.
  local tree dirty
  tree=$(git rev-parse 'HEAD^{tree}' 2>/dev/null) || tree=""
  if [ -z "$tree" ]; then printf 'unknown (not a git checkout)'; return 0; fi
  dirty=$(git --no-optional-locks status --porcelain 2>/dev/null | head -1) || dirty=""
  if [ -n "$dirty" ]; then printf '%s (working tree dirty)' "$tree"; else printf '%s' "$tree"; fi
}

CLAWDLINE_SUITE_LOCK_HOLDER="${CLAWDLINE_SUITE_LOCK_HOLDER:-$(clawdline_suite_lock_default_holder)}"
CLAWDLINE_SUITE_LOCK_TREE="${CLAWDLINE_SUITE_LOCK_TREE:-$(clawdline_suite_lock_tree)}"
# The note says out loud the thing the mechanism above already enforces, because whoever reads this
# file is usually reading it at the moment they are tempted to remove the lock by hand. A gap with
# no compiler running does **not** mean nobody holds it: one run is a sequence of expensive steps
# and the gaps between them are part of the hold.
CLAWDLINE_SUITE_LOCK_NOTE="${CLAWDLINE_SUITE_LOCK_NOTE:-running ./test.sh; output is going to $LOG. This lock covers a whole sequence of expensive steps, so a moment with no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN running does not mean it is free. It is handed on only when the heartbeat above has expired, or when $CLAWDLINE_SUITE_LOCK_DONE_FLAG exists, and in both cases only while no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is running anywhere. If you think it is stuck, ask the run named above rather than removing this directory.}"

trap clawdline_suite_exit_cleanup EXIT
clawdline_acquire_suite_lock || exit $?
# <<< clawdline suite lock <<<

BIN="${TMPDIR:-/tmp}/clawdline-tests"

required_cloud_test_files=(
  Tests/CloudEnvelopeTests.swift
  Tests/CloudAccountTests.swift
  Tests/CloudTransportTests.swift
  Tests/CloudAppBridgeTests.swift
  Tests/CloudSettingsTests.swift
  Tests/ScheduleResumeTests.swift
  Tests/CloudClockTests.swift
  Tests/CloudCanonicalJSONTests.swift
  Tests/CloudCommandLedgerTests.swift
  Tests/CloudOutboundSpoolTests.swift
  Tests/CloudPairingTests.swift
  Tests/CloudLifecycleTests.swift
)
for required_cloud_test_file in "${required_cloud_test_files[@]}"; do
  if ! test -f "$required_cloud_test_file"; then
    echo "required Cloud test suite is missing: $required_cloud_test_file" >&2
    exit 1
  fi
done

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -o "$BIN" \
  "${clawdline_library_sources[@]}" \
  "${clawdline_test_sources[@]}" \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation -framework Network

# Between the two halves of the guarded section. The compile is the long unattended stretch, so this
# is where a lock that changed hands underneath the run has to be noticed — before the second
# expensive thing starts.
clawdline_confirm_suite_lock || exit $?

# `if` rather than a bare assignment: under `set -e` a failing command on the right-hand side
# ends the script right there, before what it captured has been printed — so a red suite exited
# 1 with nothing on screen at all, which is worse than having no guard.
# The suite pairs devices, so point the store somewhere disposable. Without this a test run
# writes into whoever's real ~/.config/clawdline is on the machine — see RemoteAuth.directory.
STORE="${TMPDIR:-/tmp}/clawdline-test-store-$$"
mkdir -p "$STORE"
# No `trap 'rm -rf "$STORE"' EXIT` here any more, and that is not an omission. Bash keeps exactly
# one EXIT trap, so this line silently replaced the suite lock's — installing it left the machine
# lock behind on every single run. The removal is composed into `clawdline_suite_exit_cleanup`
# above instead, which reads `${STORE:-}` and so does the right thing whether or not this line has
# been reached.
# The drop cache goes inside it, and that is the same problem with teeth: the suite writes real
# image files through `Drop.store`, and every write prunes the oldest entries away. Unisolated,
# running the tests deletes pictures the person dropped into the bar — see Drop.directory. Spelled
# out at the invocation below rather than held in a variable of its own, because `test-sh-streaming`
# re-runs that block with only `$BIN`, `$STORE` and `$LOG` defined. The binary sets the same
# boundary for itself, so narrowing a failure by running it directly is safe too.

# Streamed through `tee` rather than captured into a variable and echoed at the end.
#
# **Not for the reason it first looked like.** The story here used to be that a crashing binary took
# the captured output with it; that is false and was measured to be false. `if out=$(…)` survives a
# `SIGTRAP` in the binary perfectly well — the `if` keeps the assignment out of `errexit`, the
# substitution reads to EOF, the `echo` runs — and against a Swift binary that prints 500 lines and
# then calls `fatalError`, capture and `tee` left the *same* 439 lines on disk.
#
# Two other things were eating the output, and only one of them is this pipe's business:
#
#   * **stdout was block buffered** at 16384 — the binary's fd 1 is this pipe, and stays this pipe
#     however the caller redirects, because a caller's `> run.log` lands on `tee`'s stdout and not
#     on the binary's. So a crash could swallow most of the suite's own output, by the same amount
#     for everybody. That is fixed in `Tests/TestIsolation.swift`, which now asks for line
#     buffering; both forms lost those lines equally.
#   * **The shell itself gets killed from outside** — an agent harness timeout, a cancelled CI job,
#     Ctrl-C, the OOM killer. There is no `echo` in that story at all. Measured: killed at 0.45s,
#     `tee` had 219 lines on disk and the captured form had none. On a machine where half a dozen
#     sessions run this suite under harnesses that impose timeouts, that is the common case.
#
# So the log survives the process that wrote it, which is why its path is outside `$STORE` and is
# printed when the suite fails. Copy it somewhere before the temporary directory goes.
# **The status has to come from the binary, and `pipefail` will not give it to you.** With
# `set -o pipefail` a pipeline reports its rightmost non-zero member, so a `tee` that cannot write
# — a full disk, a read-only `TMPDIR` — would be reported as the suite's own exit code and a green
# suite would look red, or a red one would exit with the wrong number. `PIPESTATUS` names each
# member, so both are read and neither is inferred. `set +e` around the pipeline rather than an
# `if`, because `PIPESTATUS` must be read from the pipeline itself and any command in between,
# `if` included, is a chance to have replaced it.
# `$LOG` is set with the suite lock above, which records the path so a blocked run can watch this
# one; there is one definition and it is that one.
set +e
CLAWDLINE_REMOTE_DIR="$STORE" CLAWDLINE_DROPS_DIR="$STORE/drops" "$BIN" Resources/mascots 2>&1 | tee "$LOG"
# Copied whole, in one assignment. Reading the members one at a time does not work and does not
# look broken: the first assignment is itself a command, so it replaces `PIPESTATUS` with its own
# one-element status, and the second read is of an array that no longer has a second member —
# `unbound variable` under `set -u`, on a green suite, at the very end. Measured here.
pipe=("${PIPESTATUS[@]}")
status=${pipe[0]}
tee_status=${pipe[1]}
set -e
# The guarded section is over here, pass or fail: the compile and the run are both behind us and
# only receipt checking is left. Saying so is what lets the next run in immediately instead of
# waiting out a renewal deadline nobody is renewing against.
clawdline_suite_lock_work_finished
if [ "$status" -ne 0 ]; then
  echo "the suite exited $status — full output kept at $LOG" >&2
  exit "$status"
fi
# A `tee` that could not write has to end the run on its own number, and it has to do it *here*.
# Warning and carrying on was worse than it looked: the receipt check below reads `$LOG`, which is
# the file tee just failed to write, so a **green** suite ended as `exit 125, missing receipt`
# pointing at a path that does not exist — a false red, wearing the costume of the thing this whole
# change exists to remove. 126 rather than 125 so the two are told apart on sight.
if [ "$tee_status" -ne 0 ]; then
  echo "tee exited $tee_status writing $LOG, so the receipt below cannot be checked." >&2
  echo "The suite itself passed; the terminal above is the whole record." >&2
  exit 126
fi

# A zero process status is insufficient: removing dispatchMain() lets top-level code return before
# either async suite or the final result path runs. Require the receipt emitted only by that path,
# with full-suite counts so a targeted-case environment cannot make CI green either.
verify_test_completion_receipts "$LOG"
rm -f "$LOG"
