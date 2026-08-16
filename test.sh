#!/bin/bash
# Compile and run the test binary.
#
# Sources/main.swift is excluded: it is top-level code that starts the app, and two
# entry points cannot live in one binary. Everything else compiles in, so the tests
# exercise the same code the app ships rather than a copy of it.
set -euo pipefail
cd "$(dirname "$0")"

BIN="${TMPDIR:-/tmp}/clawdline-tests"

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -o "$BIN" \
  $(ls Sources/*.swift | grep -v 'Sources/main.swift') \
  Tests/main.swift \
  -framework AppKit -framework Carbon -framework ServiceManagement

"$BIN" Resources/mascots
