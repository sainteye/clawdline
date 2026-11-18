#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_bin=$(mktemp "${TMPDIR:-/tmp}/rounded-brand-mascot.XXXXXX")
trap 'rm -f "$test_bin"' EXIT
swiftc -o "$test_bin" shell/darwin/RoundedBrandMascot.swift tools/swift-tests/RoundedBrandMascotTest.swift
"$test_bin" "$@"
