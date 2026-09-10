#!/usr/bin/env bash
# Compile and exercise the first deliberately small Linux Core boundary. This is an additive
# probe, not the application's package graph: the root Package.swift and Mac build remain intact.
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
repo_root=$(realpath "$script_dir/..")
package_root="$repo_root/tools/ubuntu-core-probe"
manifest="$package_root/core-sources.txt"
expected_revision=da9d28d69ebe3894b18376c8f2395c2f37b8448f
expected_asn1_revision=d9a5b37470adc940d22c3bcd5ca6953a516b727f
expected_resolved_sha256=2805649d3d2fab421900458b3c615b3a9c3e195b96f7da7abf606c01317eaa95
mode=${1:-run}
jobs=${UBUNTU_CORE_PROBE_JOBS:-2}
scratch_path=${UBUNTU_CORE_PROBE_SCRATCH_PATH:-}

fail() {
  echo "ubuntu-core-probe: $*" >&2
  exit 1
}

require_repo_file() {
  local relative=$1
  local path="$repo_root/$relative"
  local resolved
  [ -f "$path" ] || fail "missing repository input: $relative"
  [ -s "$path" ] || fail "repository input is empty: $relative"
  resolved=$(realpath "$path") || fail "cannot resolve repository input: $relative"
  [ "$resolved" = "$path" ] || fail "repository input must not be a symlink or traverse one: $relative"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

case "$mode" in
  run | --red-proof) ;;
  *) fail "usage: $0 [--red-proof]" ;;
esac
case "$jobs" in
  '' | *[!0-9]* | 0) fail "UBUNTU_CORE_PROBE_JOBS must be a positive integer" ;;
esac

scratch_arguments=()
if [ -n "$scratch_path" ]; then
  mkdir -p "$scratch_path"
  scratch_arguments=(--scratch-path "$scratch_path")
fi

[ "$(uname -s)" = Linux ] || fail "requires Linux; use the pinned Docker command in tools/ubuntu-core-probe/README.md"
[ "$(uname -m)" = x86_64 ] || fail "requires amd64/x86_64, found $(uname -m)"
[ -r /etc/os-release ] || fail "cannot identify the Linux distribution"
# shellcheck disable=SC1091
. /etc/os-release
[ "${ID:-}" = ubuntu ] || fail "requires Ubuntu, found ${ID:-unknown}"
[ "${VERSION_ID:-}" = 24.04 ] || fail "requires Ubuntu 24.04, found ${VERSION_ID:-unknown}"

require_repo_file tools/ubuntu-core-probe/core-sources.txt
require_repo_file tools/ubuntu-core-probe/Package.swift
require_repo_file tools/ubuntu-core-probe/Package.resolved
require_repo_file tools/ubuntu-core-probe/Sources/UbuntuCoreProbe/main.swift

extra_harness_sources=$(find "$package_root/Sources/UbuntuCoreProbe" -name '*.swift' \
  ! -path "$package_root/Sources/UbuntuCoreProbe/main.swift" -print)
[ -z "$extra_harness_sources" ] || fail "unlisted checked-in probe source: $extra_harness_sources"

resolved_sha256=$(sha256_file "$package_root/Package.resolved")
[ "$resolved_sha256" = "$expected_resolved_sha256" ] || \
  fail "Package.resolved bytes do not match the reviewed swift-crypto and swift-asn1 lockfile"

stage=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-ubuntu-core-probe.XXXXXX")
cleanup() {
  rm -rf -- "$stage"
}
trap cleanup EXIT INT TERM

printf '%s\n' \
  Sources/CloudCanonicalJSON.swift \
  Sources/CloudClock.swift > "$stage/expected-core-sources.txt"
cmp -s "$stage/expected-core-sources.txt" "$manifest" || \
  fail "core-sources.txt must list exactly CloudCanonicalJSON.swift and CloudClock.swift in canonical order"

mkdir -p "$stage/Sources/UbuntuCoreProbe"
cp "$package_root/Package.swift" "$stage/Package.swift"
cp "$package_root/Package.resolved" "$stage/Package.resolved"
cp "$package_root/Sources/UbuntuCoreProbe/main.swift" "$stage/Sources/UbuntuCoreProbe/main.swift"

source_count=0
while IFS= read -r source || [ -n "$source" ]; do
  case "$source" in
    Sources/*.swift) ;;
    *) fail "invalid Core source path: $source" ;;
  esac
  case "$source" in
    *..* | /* | *\\*) fail "unsafe Core source path: $source" ;;
  esac
  require_repo_file "$source"
  cp "$repo_root/$source" "$stage/Sources/UbuntuCoreProbe/$(basename "$source")"
  source_count=$((source_count + 1))
done < "$manifest"
[ "$source_count" -eq 2 ] || fail "expected two non-empty Core sources, found $source_count"

staged_source_count=$(find "$stage/Sources/UbuntuCoreProbe" -type f -name '*.swift' | wc -l | tr -d ' ')
[ "$staged_source_count" -eq 3 ] || fail "staged target contains an unlisted Swift source"
cmp -s "$repo_root/Sources/CloudCanonicalJSON.swift" \
  "$stage/Sources/UbuntuCoreProbe/CloudCanonicalJSON.swift" || fail "canonical JSON source copy drifted"
cmp -s "$repo_root/Sources/CloudClock.swift" \
  "$stage/Sources/UbuntuCoreProbe/CloudClock.swift" || fail "clock source copy drifted"

if [ "$mode" = --red-proof ]; then
  clock_source="$stage/Sources/UbuntuCoreProbe/CloudClock.swift"
  needle='    public static let requiredStableDuration: TimeInterval = 60'
  [ "$(grep -Fxc "$needle" "$clock_source" || true)" -eq 1 ] || \
    fail "red proof could not identify the clock stability boundary exactly once"
  sed 's/public static let requiredStableDuration: TimeInterval = 60$/public static let requiredStableDuration: TimeInterval = 61/' \
    "$clock_source" > "$stage/CloudClock.swift.mutated"
  mv "$stage/CloudClock.swift.mutated" "$clock_source"
  echo "ubuntu-core-probe: red proof mutates requiredStableDuration from 60 to 61" >&2
fi

echo "ubuntu-core-probe: image=${UBUNTU_CORE_PROBE_IMAGE:-unreported} os=$PRETTY_NAME arch=$(uname -m)"
swift --version
while IFS= read -r source; do
  digest=$(sha256_file "$repo_root/$source")
  echo "ubuntu-core-probe: source=$source sha256=$digest"
done < "$manifest"
echo "ubuntu-core-probe: swift-crypto=4.5.2 revision=$expected_revision"
echo "ubuntu-core-probe: swift-asn1=1.7.2 revision=$expected_asn1_revision"

log="$stage/probe.log"
if swift run --package-path "$stage" "${scratch_arguments[@]}" \
  --disable-automatic-resolution -c release -j "$jobs" UbuntuCoreProbe \
  2>&1 | tee "$log"; then
  run_status=0
else
  run_status=$?
fi

[ "$run_status" -eq 0 ] || fail "compile/run failed with status $run_status"
check_count=$(grep -c '^ok [0-9][0-9]* - ' "$log" || true)
[ "$check_count" -eq 17 ] || fail "expected 17 explicit checks, observed $check_count"
grep -Fqx '17 checks passed' "$log" || fail "missing exact 17-check completion receipt"
echo "ubuntu-core-probe: PASS — two production Core sources compiled and 17 checks passed"
