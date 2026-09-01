#!/bin/bash
# Behaviour checks for Sources/ScreenTail.swift.
#
# **Why this is not in Tests/.** It should be, and `B-SCREEN-TAIL-TESTS-ARE-NOT-IN-THE-SUITE`
# says so. On the day it was written both `Tests/TestGroupManifest.swift` and
# `tools/check-architecture-boundaries.sh` were another session's uncommitted work, and the guard
# seals the suite file count and the group count as literals — so adding one suite file would have
# turned somebody else's tree red. This runs the same checks from a directory the guard does not
# read, until that is no longer true.
#
# Optionally takes a directory of raw screen captures (one file per capture, sorted by name) and
# reconciles them, which is the only check that proves the alignment against real input.
#
#     tools/screen-tail-probe.sh [captures-dir]
set -euo pipefail
cd "$(dirname "$0")/.."
out="${TMPDIR:-/tmp}/clawdline-screen-tail-probe.$$"
trap 'rm -f "$out"' EXIT
swiftc -swift-version 5 -o "$out" \
  Sources/ScreenTail.swift Sources/Ansi.swift tools/screen-tail-probe/main.swift
"$out" "${1:-}"
