#!/bin/bash
# Compile and run the test binary.
#
# Sources/main.swift is excluded: it is top-level code that starts the app, and two
# entry points cannot live in one binary. Everything else compiles in, so the tests
# exercise the same code the app ships rather than a copy of it.
set -euo pipefail
cd "$(dirname "$0")"

# Trailing commas in an argument list are Swift 6.1 syntax. The toolchain here is usually
# newer than CI's, so code that compiles locally can fail to parse on the runner — and the
# error arrives ten minutes later, in a log, attached to a push that is already public.
offenders=$(awk '
  $0 ~ /,[[:space:]]*\)/            { print FILENAME ":" FNR ": " $0 }
  prev ~ /,[[:space:]]*$/ && $0 ~ /^[[:space:]]*\)/ { print FILENAME ":" FNR-1 ": " prev }
  { prev = $0 }
' Sources/*.swift Tests/*.swift)
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
node Tests/web-title-transport.mjs
# Two suites that existed and that nothing ran: neither was in this list, and CI only runs
# this script. A test nobody runs is a test that passes.
node Tests/web-user-messages.mjs
node Resources/web/app/js/net/client.test.mjs
# This one is about this script rather than the app: that a crashed run still leaves its output.
node Tests/test-sh-streaming.mjs

BIN="${TMPDIR:-/tmp}/clawdline-tests"

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -o "$BIN" \
  $(ls Sources/*.swift | grep -v 'Sources/main.swift') \
  Tests/ScheduleResumeTests.swift \
  Tests/CloudEnvelopeTests.swift \
  Tests/CloudTransportTests.swift \
  Tests/CloudAppBridgeTests.swift \
  Tests/main.swift \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation -framework Network

# `if` rather than a bare assignment: under `set -e` a failing command on the right-hand side
# ends the script right there, before what it captured has been printed — so a red suite exited
# 1 with nothing on screen at all, which is worse than having no guard.
# The suite pairs devices, so point the store somewhere disposable. Without this a test run
# writes into whoever's real ~/.config/clawdline is on the machine — see RemoteAuth.directory.
STORE="${TMPDIR:-/tmp}/clawdline-test-store-$$"
mkdir -p "$STORE"
trap 'rm -rf "$STORE"' EXIT

# Streamed through `tee` rather than captured and echoed at the end. The capture was there so the
# receipt below could be grepped, and it cost exactly the run that needed it most: a crash mid-suite
# — measured here as `exit 133`, SIGTRAP — kills the shell before the `echo`, so everything the
# binary had printed goes with it and the red run is the one that arrives with no output at all.
# Now the lines appear as they are produced and the log survives the process that wrote it, which is
# why its path is outside `$STORE` and is printed when the suite fails.
# **The status has to come from the binary, and `pipefail` will not give it to you.** With
# `set -o pipefail` a pipeline reports its rightmost non-zero member, so a `tee` that cannot write
# — a full disk, a read-only `TMPDIR` — would be reported as the suite's own exit code and a green
# suite would look red, or a red one would exit with the wrong number. `PIPESTATUS` names each
# member, so both are read and neither is inferred. `set +e` around the pipeline rather than an
# `if`, because `PIPESTATUS` must be read from the pipeline itself and any command in between,
# `if` included, is a chance to have replaced it.
LOG="${TMPDIR:-/tmp}/clawdline-tests-$$.log"
set +e
CLAWDLINE_REMOTE_DIR="$STORE" "$BIN" Resources/mascots 2>&1 | tee "$LOG"
# Copied whole, in one assignment. Reading the members one at a time does not work and does not
# look broken: the first assignment is itself a command, so it replaces `PIPESTATUS` with its own
# one-element status, and the second read is of an array that no longer has a second member —
# `unbound variable` under `set -u`, on a green suite, at the very end. Measured here.
pipe=("${PIPESTATUS[@]}")
status=${pipe[0]}
tee_status=${pipe[1]}
set -e
if [ "$tee_status" -ne 0 ]; then
  echo "tee exited $tee_status — $LOG may be short, and the terminal above is the whole record" >&2
fi
if [ "$status" -ne 0 ]; then
  echo "the suite exited $status — full output kept at $LOG" >&2
  exit "$status"
fi

# A zero process status is insufficient: removing dispatchMain() lets top-level code return before
# either async suite or the final result path runs. Require the receipt emitted only by that path,
# with full-suite counts so a targeted-case environment cannot make CI green either.
expected_cloud_receipt='CLAWDLINE_CLOUD_TESTS_COMPLETE CloudEnvelope=64 CloudTransport=29 CloudAppBridge=49'
if ! grep -Fqx "$expected_cloud_receipt" "$LOG"; then
  echo "missing or incomplete Cloud test completion receipt — full output kept at $LOG" >&2
  exit 125
fi
rm -f "$LOG"
