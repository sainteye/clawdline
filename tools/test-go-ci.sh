#!/bin/sh
# Hosted runners cannot prove the live process identity and iTerm2 behaviours
# below. Keep the exclusions named, checked against their source, and printed
# in every CI run; every fixture-backed test still runs.
#
# Flaky tests are quarantined in tools/go-quarantine.txt with an owner and an
# expiry date; one past its date fails the run (docs/working-rules.md).
# CLAWDLINE_GO_QUARANTINE names another list and CLAWDLINE_TODAY another day,
# and CLAWDLINE_GO_CI_DRY=1 prints the go test command instead of running it;
# tools/test-go-ci-quarantine.sh uses all three.
set -eu
cd "$(dirname "$0")/.."

skip=""

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
  skip="$skip${skip:+|}$name"
}

# quarantine_test file name owner expiry reason
quarantine_test() {
  file=$1 name=$2 owner=$3 expiry=$4 reason=$5
  case "$expiry" in
    [0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]) ;;
    *) echo "Quarantine entry for $name has expiry \"$expiry\"; want YYYY-MM-DD" >&2; exit 1 ;;
  esac
  [ -n "$owner" ] || { echo "Quarantine entry for $name has no owner" >&2; exit 1; }
  if ! grep -Fq "func $name(" "$file" 2>/dev/null; then
    echo "Quarantine entry is stale: $file no longer defines $name" >&2
    exit 1
  fi
  # As numbers, since test's string < is not POSIX; the expiry day itself is
  # still covered.
  if [ "$(printf '%s' "$expiry" | tr -d -)" -lt "$(printf '%s' "$today" | tr -d -)" ]; then
    echo "Quarantine expired on $expiry: $file $name (owner $owner)" >&2
    echo "  Fix the test, or extend the date with a reason; tools/go-quarantine.txt." >&2
    exit 1
  fi
  echo "Quarantined until $expiry: $file $name (owner $owner)"
  echo "Reason: $reason"
  skip="$skip${skip:+|}$name"
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

today=${CLAWDLINE_TODAY:-$(date -u +%Y-%m-%d)}
quarantine=${CLAWDLINE_GO_QUARANTINE:-tools/go-quarantine.txt}
trim() { printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'; }
while IFS='|' read -r q_file q_name q_owner q_expiry q_reason; do
  case "$q_file" in "" | \#*) continue ;; esac
  quarantine_test "$(trim "$q_file")" "$(trim "${q_name:-}")" "$(trim "${q_owner:-}")" \
    "$(trim "${q_expiry:-}")" "$(trim "${q_reason:-}")"
done <"$quarantine"

if [ "${CLAWDLINE_GO_CI_DRY:-}" = 1 ]; then
  echo "go test ./... -skip '^($skip)\$'"
  exit 0
fi
go test ./... -skip "^($skip)\$"
