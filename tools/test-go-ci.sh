#!/bin/sh
# Hosted runners cannot prove the live process identity and iTerm2 behaviours
# below. Keep the exclusions named, checked against their source, and printed
# in every CI run; every fixture-backed test still runs.
set -eu
cd "$(dirname "$0")/.."

check_test() {
  file=$1
  name=$2
  reason=$3
  grep -Fq "func $name(" "$file" || {
    echo "CI exclusion is stale: $file no longer defines $name" >&2
    exit 1
  }
  echo "Excluded: $file $name"
  echo "Reason: $reason"
}

check_test cmd/clawdline/portheld_test.go \
  TestADaemonThatCannotBindNamesWhoHasThePort \
  "requires the host process table to identify and timestamp a live listener"
check_test internal/adapters/process/holder_unix_test.go \
  TestTheSystemNamesThisProcessAsTheListener \
  "requires the host process table to identify and timestamp the test process"
check_test internal/adapters/tunnel/supervisor_test.go \
  TestReclaim \
  "requires the host process table to prove a live child command before signalling it"
check_test internal/adapters/terminal/iterm_seal_darwin_test.go \
  TestTheRealListingCarriesWhateverItCouldNotRead \
  "requires both the live process table and an iTerm2 Apple Event"

go test ./... -skip '^(TestADaemonThatCannotBindNamesWhoHasThePort|TestTheSystemNamesThisProcessAsTheListener|TestReclaim|TestTheRealListingCarriesWhateverItCouldNotRead)$'
