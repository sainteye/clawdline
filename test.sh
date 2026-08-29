#!/bin/bash
# Compile and run the test binary.
#
# Sources/main.swift is excluded: it is top-level code that starts the app, and two
# entry points cannot live in one binary. Everything else compiles in, so the tests
# exercise the same code the app ships rather than a copy of it.
set -euo pipefail

expected_cloud_receipt='CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=11 suites=CloudEnvelope:64,CloudAccount:77,CloudTransport:29,CloudAppBridge:49,CloudSettings:28,ScheduleResume:11,CloudClock:47,CloudCanonicalJSON:91,CloudCommandLedger:101,CloudOutboundSpool:141,CloudPairing:166'
expected_swift_receipt='6661 checks passed'

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

# The checked-in protocol fixture is the cross-runtime byte authority. Generate the expected
# bytes in memory and compare through the generator's read-only mode so hand edits fail closed.
swift tools/generate-protocol-vectors.swift --check Tests/protocol-vectors.json

# Keep the small browser-independent renderer contracts beside the Swift suite. The web app's
# scoped package.json marks its shipped files as ESM, matching the browser's module entry.
node Tests/web-schedules.mjs
node Tests/web-coordinator.mjs
node Tests/web-clawdfather.mjs
node Tests/web-optimistic.mjs
node Tests/web-session-resilience.mjs
node Tests/web-viewport.mjs
node Tests/web-layout-diagnostics.mjs
node Tests/web-session-disposition.mjs
node Tests/web-session-closeability.mjs
node Tests/web-title-transport.mjs
node Tests/web-code-copy.mjs
node Tests/web-message-images.mjs
# Two suites that existed and that nothing ran: neither was in this list, and CI only runs
# this script. A test nobody runs is a test that passes.
node Tests/web-user-messages.mjs
node Resources/web/app/js/net/client.test.mjs
# This one is about this script rather than the app: that a crashed run still leaves its output.
node Tests/test-sh-streaming.mjs

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

# `if` rather than a bare assignment: under `set -e` a failing command on the right-hand side
# ends the script right there, before what it captured has been printed — so a red suite exited
# 1 with nothing on screen at all, which is worse than having no guard.
# The suite pairs devices, so point the store somewhere disposable. Without this a test run
# writes into whoever's real ~/.config/clawdline is on the machine — see RemoteAuth.directory.
STORE="${TMPDIR:-/tmp}/clawdline-test-store-$$"
mkdir -p "$STORE"
trap 'rm -rf "$STORE"' EXIT
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
LOG="${TMPDIR:-/tmp}/clawdline-tests-$$.log"
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
