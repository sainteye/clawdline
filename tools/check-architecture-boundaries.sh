#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/.."

architecture_guard_fail() {
  echo "architecture boundary guard: $1" >&2
  exit 1
}

line_count() {
  wc -l < "$1" | tr -d '[:space:]'
}

main_lines=$(line_count Tests/main.swift)
[ "$main_lines" -le 500 ] \
  || architecture_guard_fail "Tests/main.swift has $main_lines lines; maximum is 500"

orchestrator_lines=$(line_count Sources/Orchestrator.swift)
[ "$orchestrator_lines" -le 12398 ] \
  || architecture_guard_fail "Sources/Orchestrator.swift grew beyond approved Closeability receipt (12398)"

remote_server_lines=$(line_count Sources/RemoteServer.swift)
[ "$remote_server_lines" -le 6316 ] \
  || architecture_guard_fail "Sources/RemoteServer.swift grew beyond approved Closeability receipt (6316)"

if grep -q 'group(' Tests/main.swift; then
  architecture_guard_fail "new domain group found in Tests/main.swift"
fi

runner_count=$(grep -Ec '^run[A-Za-z0-9]+Tests\(\)$' Tests/main.swift || true)
[ "$runner_count" -eq 24 ] \
  || architecture_guard_fail "ordered domain runner count is $runner_count; expected 24"

manifest_group_count=$(awk '
  /^let expectedOrderedTestGroupTitles: \[String\] = \[/ { in_manifest = 1; next }
  in_manifest && /^\]/ { in_manifest = 0 }
  in_manifest && /",[[:space:]]*$/ { count++ }
  END { print count + 0 }
' Tests/TestGroupManifest.swift)
[ "$manifest_group_count" -eq 447 ] \
  || architecture_guard_fail "ordered group manifest has $manifest_group_count entries; expected 447"

suite_count=0
for suite in Tests/*Tests.swift; do
  [ -e "$suite" ] || continue
  suite_count=$((suite_count + 1))
  suite_lines=$(line_count "$suite")
  [ "$suite_lines" -le 2000 ] \
    || architecture_guard_fail "$suite has $suite_lines lines; suite stop-growth limit is 2000"
done
[ "$suite_count" -eq 35 ] \
  || architecture_guard_fail "suite file count is $suite_count; expected 35"

echo "architecture boundaries: main=$main_lines lines, runners=$runner_count, groups=$manifest_group_count, suite_files=$suite_count"
