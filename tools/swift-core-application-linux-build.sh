#!/usr/bin/env bash
# W3-1: a genuine Linux compile of the real ClawdlineCore/ClawdlineApplication SwiftPM targets —
# not a lexical scan, and not a staged/materialized copy like tools/ubuntu-core-probe.sh's probe.
# Package.swift, Packages/ClawdlineCore/ and Packages/ClawdlineApplication/ are the same checked-in
# files this compiles on macOS; nothing here copies or regenerates them. See Packages/README.md
# for why those directories hold symlinks rather than files, and
# tools/check-architecture-boundaries.sh's "real SwiftPM Core/Application/Mac target graph" block
# for the lexical/graph checks that run before this ever needs to compile anything.
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
repo_root=$(realpath "$script_dir/..")
cd "$repo_root"

fail() {
  echo "swift-core-application-linux-build: $*" >&2
  exit 1
}

[ "$(uname -s)" = Linux ] || fail "requires Linux; use the pinned Docker command in tools/ubuntu-core-probe/README.md's image, e.g.: docker run --rm --platform linux/amd64 -v \"\$PWD:/workspace:ro\" swift:6.1.3-noble@sha256:1d10836ca929943d70b2254e555b17247b132f6eed984654351cb6a8cc531613 bash -lc 'cp -r /workspace /build && cd /build && ./tools/swift-core-application-linux-build.sh'"
[ "$(uname -m)" = x86_64 ] || fail "requires amd64/x86_64, found $(uname -m)"
[ -r /etc/os-release ] || fail "cannot identify the Linux distribution"
# shellcheck disable=SC1091
. /etc/os-release
[ "${ID:-}" = ubuntu ] || fail "requires Ubuntu, found ${ID:-unknown}"
[ "${VERSION_ID:-}" = 24.04 ] || fail "requires Ubuntu 24.04, found ${VERSION_ID:-unknown}"

[ -f Package.swift ] || fail "Package.swift is missing"
for member in Packages/ClawdlineCore/CloudCanonicalJSON.swift Packages/ClawdlineCore/CloudClock.swift \
  Packages/ClawdlineCore/Assistant.swift Packages/ClawdlineApplication/HostPorts.swift; do
  [ -e "$member" ] || fail "expected real target member missing: $member"
done

# A read-only bind mount (the safe default for running an unfamiliar script under Docker) cannot
# hold SwiftPM's .build directory. The caller's own docker invocation should have already copied
# the tree somewhere writable; this is a clear failure rather than a confusing permission error if
# it did not.
touch .swift-core-application-linux-build.write-probe 2>/dev/null \
  || fail "$repo_root is not writable; copy the tree to a writable directory before running this (a read-only bind mount cannot hold .build)"
rm -f .swift-core-application-linux-build.write-probe

echo "swift-core-application-linux-build: os=$PRETTY_NAME arch=$(uname -m)"
swift --version

build_log=$(mktemp)
trap 'rm -f "$build_log"' EXIT
# `-c debug`, not `-c release`, and `-j 1` by default: on a Mac running this image under QEMU
# (`docker run --platform linux/amd64` on Apple Silicon, which is how a Clawdline dev Mac reaches
# Ubuntu/amd64 at all — the CI job `swift-core-application-linux` in .github/workflows/ci.yml does
# not need any of this, because GitHub Actions' `ubuntu-24.04` runners are real amd64 hardware, not
# QEMU, and that job pins its own configuration explicitly:
# `SWIFT_CORE_APPLICATION_LINUX_BUILD_CONFIGURATION=release SWIFT_CORE_APPLICATION_LINUX_BUILD_JOBS=2`)
# a `swift build` of this target has intermittently died `Illegal instruction` from inside
# `qemu: uncaught target signal 4` before this repository's own code ever runs, on this target and
# on an unrelated target in `tools/ubuntu-core-probe.sh`'s already-landed probe under the identical
# image — a QEMU emulation gap on one host shape, not a defect either script found.
#
# W3-1 correction (`runtime-qemu-receipt-counts-conflict`): earlier comments here, in
# `docs/ubuntu-headless-runtime-plan.md`, and in a since-superseded delivery artifact each quoted a
# different attempt-count ratio (a 3/5, a 2/2, a 1/3, an 8-row table) for what was supposed to be
# the same handful of local QEMU tries, and none of them agreed with each other or with the CI job's
# actual, fixed `-j 2`. None of those ratios is reproduced here: a QEMU crash rate from a handful of
# manual tries on one Mac is not a statistic anything can act on, and restating it as one kept
# inviting the next document to restate it differently. What this comment claims instead is typed
# and bounded to what was actually observed, dated, in `artifacts/W3_1_CORRECTION.md`'s attempt
# ledger — a real pass, a real `qemu: uncaught target signal 4` crash before repository code ran,
# or "not yet attempted this build" — never an aggregate rate. `-c debug -j 1` remains the default
# here because it is what most local attempts recorded in that ledger got past; that is a default
# chosen for reliability, not a claim about how often the alternative fails.
swift_build_configuration=${SWIFT_CORE_APPLICATION_LINUX_BUILD_CONFIGURATION:-debug}
swift_build_jobs=${SWIFT_CORE_APPLICATION_LINUX_BUILD_JOBS:-1}
if swift build --target ClawdlineCore --target ClawdlineApplication \
  -c "$swift_build_configuration" -j "$swift_build_jobs" 2>&1 | tee "$build_log"; then
  build_status=0
else
  build_status=$?
fi
[ "$build_status" -eq 0 ] || fail "swift build --target ClawdlineCore --target ClawdlineApplication -c $swift_build_configuration -j $swift_build_jobs failed with status $build_status (see this script's comment above if this is qemu: uncaught target signal 4 under Docker on Apple Silicon — that is a known emulation flake here, not necessarily a real regression; retry, and report the exact pass/fail counts rather than either extreme)"
# A `--target`-scoped build reports each target's own completion ("Build of target: 'X'
# complete!") rather than the single whole-package "Build complete!" a bare `swift build` prints.
grep -q "Build of target: 'ClawdlineApplication' complete!" "$build_log" \
  || fail "missing SwiftPM's own ClawdlineApplication completion line"

echo "swift-core-application-linux-build: PASS — ClawdlineCore and ClawdlineApplication compiled on Ubuntu 24.04 amd64"
