#!/usr/bin/env bash
# W3-1/W3-2: a genuine Linux compile of the real ClawdlineCore/ClawdlineApplication targets and
# the ClawdlineLinux executable product —
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
  Packages/ClawdlineCore/Assistant.swift Packages/ClawdlineApplication/HostPorts.swift \
  Packages/ClawdlineApplication/ProjectRootPolicy.swift \
  Packages/ClawdlineApplication/ProviderLifecyclePolicy.swift \
  Packages/ClawdlineApplication/SessionLaunchPolicy.swift \
  Packages/ClawdlineApplication/TerminalCommandScheduler.swift \
  Packages/ClawdlineApplication/CloudCommandLedger.swift \
  Packages/ClawdlineApplication/CloudOutboundSpool.swift \
  Packages/ClawdlineApplication/CloudDurableStores.swift \
  Packages/ClawdlineLinux/LinuxComposition.swift \
  Packages/ClawdlineLinux/LinuxContainedFileSystem.swift \
  Packages/ClawdlineLinux/LinuxDaemonIngress.swift \
  Packages/ClawdlineLinux/LinuxDaemonLifecycle.swift \
  Packages/ClawdlineLinux/LinuxDocumentReader.swift \
  Packages/ClawdlineLinux/LinuxLocalIngressServer.swift \
  Packages/ClawdlineLinux/LinuxProviderRuntime.swift \
  Packages/ClawdlineLinux/LinuxRuntimeAdapters.swift Packages/ClawdlineLinux/LinuxSHA256.swift \
  Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift \
  Packages/ClawdlineLinux/main.swift \
  Packages/ClawdlineLinuxTests/LinuxRuntimeContractTests.swift; do
  [ -e "$member" ] || fail "expected real target member missing: $member"
done
command -v tmux >/dev/null 2>&1 || fail "tmux is required for the real Linux provider lifecycle contract"
command -v python3 >/dev/null 2>&1 || fail "python3 is required for the containment and package fixtures"
command -v openssl >/dev/null 2>&1 || fail "openssl is required for signed package provenance"
command -v systemd-analyze >/dev/null 2>&1 || fail "systemd-analyze is required for exact packaged-unit parsing"

# A read-only bind mount (the safe default for running an unfamiliar script under Docker) cannot
# hold SwiftPM's .build directory. The caller's own docker invocation should have already copied
# the tree somewhere writable; this is a clear failure rather than a confusing permission error if
# it did not.
touch .swift-core-application-linux-build.write-probe 2>/dev/null \
  || fail "$repo_root is not writable; copy the tree to a writable directory before running this (a read-only bind mount cannot hold .build)"
rm -f .swift-core-application-linux-build.write-probe

echo "swift-core-application-linux-build: os=$PRETTY_NAME arch=$(uname -m)"
swift --version
dpkg-query -W -f='swift-core-application-linux-build: package=${binary:Package} version=${Version}\n' \
  tmux nodejs python3 openssl systemd
echo "swift-core-application-linux-build: runtime fixtures"
dpkg-query -W -f='${Package}=${Version}\n' tmux nodejs python3 openssl systemd

build_log=$(mktemp)
contract_root=""
cleanup() {
  rm -f "$build_log"
  if [ -n "$contract_root" ]; then rm -rf "$contract_root"; fi
}
trap cleanup EXIT
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
if swift build --product ClawdlineLinux \
  -c "$swift_build_configuration" -j "$swift_build_jobs" 2>&1 | tee "$build_log"; then
  build_status=0
else
  build_status=$?
fi
[ "$build_status" -eq 0 ] || fail "swift build --product ClawdlineLinux -c $swift_build_configuration -j $swift_build_jobs failed with status $build_status (see this script's comment above if this is qemu: uncaught target signal 4 under Docker on Apple Silicon — that is a known emulation flake here, not necessarily a real regression; retry, and report the exact pass/fail counts rather than either extreme)"
grep -q "Build of product 'ClawdlineLinux' complete!" "$build_log" \
  || fail "missing SwiftPM's own ClawdlineLinux product completion line"

linux_bin_dir=$(swift build -c "$swift_build_configuration" --show-bin-path)
linux_binary="$linux_bin_dir/ClawdlineLinux"
[ -x "$linux_binary" ] || fail "compiled ClawdlineLinux executable missing at $linux_binary"

# This test target imports the real executable module and drives one actual tmux PTY lifecycle.
# `Package.swift` omits the AppKit product only while its manifest is evaluated on Linux, so this
# command cannot quietly turn into a lexical or mock-only proof on Ubuntu.
CLAWDLINE_TEST_TMUX=$(command -v tmux) \
CLAWDLINE_TEST_LINUX_EXECUTABLE="$linux_binary" swift test \
  -c "$swift_build_configuration" -j "$swift_build_jobs" \
  --filter LinuxRuntimeContractTests

# Execute the protected-input and not-ready contract on the Linux runtime that CI ships. This is
# POSIX-shell driven so the job does not rely on Node being present in the Swift container.
contract_root=$(mktemp -d)
contract_stdout="$contract_root/stdout"
contract_stderr="$contract_root/stderr"
contract_secret="$contract_root/daemon.secret"
contract_config="$contract_root/daemon.json"
contract_symlink="$contract_root/daemon-link.secret"
contract_fifo="$contract_root/daemon.fifo"
contract_oversized="$contract_root/daemon-oversized.json"
printf '%s\n' 'linux-contract-secret-sentinel' > "$contract_secret"
chmod 0600 "$contract_secret"

write_contract_config() {
  local host=${1:-127.0.0.1} secret=${2:-$contract_secret}
  printf '{"version":1,"listen":{"host":"%s","port":7718},"stateDirectory":"%s/state","secretFile":"%s"}\n' \
    "$host" "$contract_root" "$secret" > "$contract_config"
  chmod 0644 "$contract_config"
}

expect_contract_status() {
  local expected=$1 label=$2 actual
  shift 2
  if "$@" >"$contract_stdout" 2>"$contract_stderr"; then actual=0; else actual=$?; fi
  [ "$actual" -eq "$expected" ] \
    || fail "$label returned $actual, expected $expected; stderr: $(tr '\n' ' ' < "$contract_stderr")"
}

expect_contract_error() {
  local code=$1 label=$2
  grep -q "\"code\":\"$code\"" "$contract_stderr" \
    || fail "$label did not return typed error $code"
}

expect_contract_status 0 health env CLAWDLINE_BUILD_IDENTITY="${GITHUB_SHA:-unknown}" "$linux_binary" health
grep -q '"ready":false' "$contract_stdout" || fail "health claimed the later daemon service gate"
grep -q '"readinessCode":"w4_runtime_not_configured"' "$contract_stdout" \
  || fail "health did not separate compiled adapters from configured capability"
grep -q '"service":"clawdline-daemon"' "$contract_stdout" || fail "health service identity is not clawdline-daemon"

write_contract_config
expect_contract_status 0 valid-config "$linux_binary" check-config --config "$contract_config"
grep -q '"configuration":"accepted_not_started"' "$contract_stdout" \
  || fail "valid protected config produced no acceptance receipt"
if grep -q 'linux-contract-secret-sentinel' "$contract_stdout"; then
  fail "valid config receipt exposed secret content"
fi

chmod 0644 "$contract_secret"
expect_contract_status 78 readable-secret "$linux_binary" check-config --config "$contract_config"
expect_contract_error invalid_secret_file readable-secret
chmod 0600 "$contract_secret"

ln -s "$contract_secret" "$contract_symlink"
write_contract_config 127.0.0.1 "$contract_symlink"
expect_contract_status 78 symlink-secret "$linux_binary" check-config --config "$contract_config"
expect_contract_error invalid_secret_file symlink-secret

write_contract_config 0.0.0.0
expect_contract_status 78 public-listener "$linux_binary" check-config --config "$contract_config"
expect_contract_error invalid_configuration public-listener

mkfifo "$contract_fifo"
expect_contract_status 78 fifo-config timeout 5 "$linux_binary" check-config --config "$contract_fifo"
expect_contract_error invalid_configuration fifo-config

dd if=/dev/zero of="$contract_oversized" bs=65537 count=1 status=none
chmod 0644 "$contract_oversized"
expect_contract_status 78 oversized-config "$linux_binary" check-config --config "$contract_oversized"
expect_contract_error invalid_configuration oversized-config

write_contract_config
chmod 0666 "$contract_config"
expect_contract_status 78 writable-config "$linux_binary" check-config --config "$contract_config"
expect_contract_error invalid_configuration writable-config

write_contract_config
expect_contract_status 69 run-unconfigured "$linux_binary" run --config "$contract_config"
expect_contract_error capability_unavailable run-unconfigured

contract_project="$contract_root/project"
mkdir -m 0700 "$contract_project"
printf '{"version":1,"listen":{"host":"127.0.0.1","port":7718},"stateDirectory":"%s/state","secretFile":"%s","runtime":{"uid":%s,"gid":%s,"projectRoots":["%s"],"tmuxExecutable":"%s","providers":{"claude":"%s","codex":"%s"}}}\n' \
  "$contract_root" "$contract_secret" "$(id -u)" "$(id -g)" "$contract_project" \
  "$(command -v tmux)" "$linux_binary" "$linux_binary" > "$contract_config"
chmod 0644 "$contract_config"
expect_contract_status 0 run-configured "$linux_binary" run --config "$contract_config"
grep -q '"configuration":"runtime_adapters_configured_provider_auth_pending"' "$contract_stdout" \
  || fail "run did not return the truthful configured/auth-pending receipt"
grep -q '"readinessCode":"w4_provider_authentication_not_proven"' "$contract_stdout" \
  || fail "configured runtime claimed provider authentication readiness"
grep -q '"umask":"0077"' "$contract_stdout" || fail "run did not report the closed service umask"
if grep -q 'linux-contract-secret-sentinel' "$contract_stdout"; then
  fail "runtime composition receipt exposed secret content"
fi

# W4-2's package/systemd slice is part of this same focused Linux acceptance: it uses only private
# roots and a fake systemctl, so it cannot mutate the runner's host service.
./tools/linux-systemd-contract.sh

echo "swift-core-application-linux-build: PASS — ClawdlineLinux graph compiled; focused runtime, restart-state, package and protected-startup contracts passed on Ubuntu 24.04 amd64"
