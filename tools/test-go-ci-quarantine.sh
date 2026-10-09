#!/bin/sh
# Prove that a quarantined Go test is skipped and printed until its expiry
# date, and that an expired, stale, ownerless or undated entry fails the run.
# Nothing is compiled: tools/test-go-ci.sh runs dry.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-quarantine.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

# A real test in a real file, so only the date decides.
entry='internal/adapters/tunnel/supervisor_test.go | TestReclaim | board item 0000 | 2026-10-20 | flaky under load'

# run <want exit> <today> <entry>
run() {
  printf '# a comment line\n\n%s\n' "$3" >"$tmp/list.txt"
  set +e
  CLAWDLINE_GO_CI_DRY=1 CLAWDLINE_TODAY=$2 CLAWDLINE_GO_QUARANTINE="$tmp/list.txt" \
    "$root/tools/test-go-ci.sh" >"$tmp/out.txt" 2>&1
  code=$?
  set -e
  [ "$code" -eq "$1" ] || fail "exit $code on $2, want $1"
}
fail() { echo "quarantine: $1" >&2; cat "$tmp/out.txt" >&2; exit 1; }
expect() { grep -Fq "$1" "$tmp/out.txt" || fail "the output does not say: $1"; }

# 1. Before and on its date: skipped and printed with owner and reason.
run 0 2026-10-09 "$entry"
expect 'Quarantined until 2026-10-20: internal/adapters/tunnel/supervisor_test.go TestReclaim (owner board item 0000)'
expect 'Reason: flaky under load'
expect '|TestReclaim)$'
run 0 2026-10-20 "$entry"

# 2. The day after: the run fails and names it.
run 1 2026-10-21 "$entry"
expect 'Quarantine expired on 2026-10-20: internal/adapters/tunnel/supervisor_test.go TestReclaim (owner board item 0000)'

# 3. A test the file no longer defines fails, as an exclusion does.
run 1 2026-10-09 'internal/adapters/tunnel/supervisor_test.go | TestNoSuchTest | board item 0000 | 2026-10-20 | gone'
expect 'Quarantine entry is stale'

# 4. No owner, or a date that is not one, fails.
run 1 2026-10-09 'internal/adapters/tunnel/supervisor_test.go | TestReclaim |  | 2026-10-20 | no owner'
expect 'has no owner'
run 1 2026-10-09 'internal/adapters/tunnel/supervisor_test.go | TestReclaim | board item 0000 | next week | no date'
expect 'want YYYY-MM-DD'

# 5. The list in the repository is well formed today.
set +e
CLAWDLINE_GO_CI_DRY=1 "$root/tools/test-go-ci.sh" >"$tmp/out.txt" 2>&1 || fail "the repository's quarantine list fails today"
set -e

echo "quarantine: an entry is skipped until its date and fails the run after it; stale, ownerless and undated entries fail"
