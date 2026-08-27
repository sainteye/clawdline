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

# Keep the small browser-independent renderer contracts beside the Swift suite. The web app's
# scoped package.json marks its shipped files as ESM, matching the browser's module entry.
node Tests/web-schedules.mjs
node Tests/web-coordinator.mjs
node Tests/web-session-resilience.mjs

BIN="${TMPDIR:-/tmp}/clawdline-tests"

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -o "$BIN" \
  $(ls Sources/*.swift | grep -v 'Sources/main.swift') \
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

if out=$(CLAWDLINE_REMOTE_DIR="$STORE" "$BIN" Resources/mascots); then status=0; else status=$?; fi
echo "$out"
[ $status -eq 0 ] || exit $status
