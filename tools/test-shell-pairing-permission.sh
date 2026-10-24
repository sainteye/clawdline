#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-shell-pairing.XXXXXX")
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM
# The host's own architecture, so an Intel runner builds what it can run; the
# shell's sources still require macOS 13.
swiftc -target "$(uname -m)-apple-macos13.0" -o "$test_dir/test" \
  shell/darwin/Pairing.swift shell/darwin/NextConfig.swift tools/test-shell-pairing-permission.swift
# An empty directory of its own: nothing here may read the person's token.
CLAWDLINE_NEXT_DIR="$test_dir/next" "$test_dir/test"
