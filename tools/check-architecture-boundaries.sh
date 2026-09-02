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

orchestrator_ceiling=12816
orchestrator_lines=$(line_count Sources/Orchestrator.swift)
[ "$orchestrator_lines" -le "$orchestrator_ceiling" ] \
  || architecture_guard_fail "Sources/Orchestrator.swift grew beyond the registry-owner extraction receipt ($orchestrator_ceiling)"

# Task JSON is built on the main queue after a SessionWatch publication lands. Root-terminal
# projection must therefore consume that publication, not re-enter Transcript/Targets and launch
# lsof/ps while the UI is applying the same generation. Deleting the publication argument or
# restoring the old lookup must fail before a compiler is started.
orchestrator_record_projection=$(awk '
  /static func records\(\) -> \[\[String: Any\]\]/ { capture = 1 }
  capture && !/^[[:space:]]*\/\// { print }
  capture && /private static func shape\(/ { exit }
' Sources/Orchestrator.swift)
[ -n "$orchestrator_record_projection" ] \
  || architecture_guard_fail "Orchestrator task-record projection slice was not found"
printf '%s\n' "$orchestrator_record_projection" | grep -q 'publishedInventory()' \
  || architecture_guard_fail "Orchestrator task records do not consume one SessionWatch publication"
record_publication_reads=$(printf '%s\n' "$orchestrator_record_projection" \
  | grep -Fc 'publishedInventory()' || true)
[ "$record_publication_reads" -eq 2 ] \
  || architecture_guard_fail "Orchestrator records/read-one projection has $record_publication_reads publication reads; expected 2"
printf '%s\n' "$orchestrator_record_projection" \
  | grep -q 'let publication = SessionWatch.shared.publishedInventory();' \
  || architecture_guard_fail "Orchestrator records do not capture one publication before mapping tasks"
printf '%s\n' "$orchestrator_record_projection" | grep -q 'publication: publication' \
  || architecture_guard_fail "Orchestrator records do not reuse their captured publication"
printf '%s\n' "$orchestrator_record_projection" | grep -q 'publication.identities' \
  || architecture_guard_fail "Orchestrator task records do not resolve roots from published identity"
if printf '%s\n' "$orchestrator_record_projection" | grep -q 'Transcript.sessionID'; then
  architecture_guard_fail "Orchestrator task records re-scan Transcript/Targets on the main queue"
fi

remote_server_ceiling=6426
remote_server_lines=$(line_count Sources/RemoteServer.swift)
[ "$remote_server_lines" -le "$remote_server_ceiling" ] \
  || architecture_guard_fail "Sources/RemoteServer.swift grew beyond approved TCP close-reclamation receipt ($remote_server_ceiling)"

if grep -q 'group(' Tests/main.swift; then
  architecture_guard_fail "new domain group found in Tests/main.swift"
fi

runner_count=$(grep -Ec '^run[A-Za-z0-9]+Tests\(\)$' Tests/main.swift || true)
[ "$runner_count" -eq 29 ] \
  || architecture_guard_fail "ordered domain runner count is $runner_count; expected 29"

manifest_group_count=$(awk '
  /^let expectedOrderedTestGroupTitles: \[String\] = \[/ { in_manifest = 1; next }
  in_manifest && /^\]/ { in_manifest = 0 }
  in_manifest && /",[[:space:]]*$/ { count++ }
  END { print count + 0 }
' Tests/TestGroupManifest.swift)
[ "$manifest_group_count" -eq 498 ] \
  || architecture_guard_fail "ordered group manifest has $manifest_group_count entries; expected 498"

suite_count=0
for suite in Tests/*Tests.swift; do
  [ -e "$suite" ] || continue
  suite_count=$((suite_count + 1))
  suite_lines=$(line_count "$suite")
  [ "$suite_lines" -le 2000 ] \
    || architecture_guard_fail "$suite has $suite_lines lines; suite stop-growth limit is 2000"
done
[ "$suite_count" -eq 42 ] \
  || architecture_guard_fail "suite file count is $suite_count; expected 42"

# The registry's second door — withTransactionOnHeldLock — does not acquire the lock; it trusts
# its caller to hold it, which is exactly the contract the …Locked() suffix carried and exactly
# what this refactor exists to abolish. It is defensible only as a migration step, and only if it
# shrinks. Nothing about Swift can check it: NSLock cannot be asked whether this thread holds it,
# and an owner field would fire on the ~160 legitimate bare regions that never go through the
# registry. So the check that can exist is a ratchet on the number of sites. It may fall, never
# rise; when it reaches zero the door is deleted and this block goes with it.
held_lock_door_sites=$(cat Sources/Orchestrator.swift Sources/OrchestratorPlanning.swift \
  | grep -c 'withTransactionOnHeldLock' || true)
[ "$held_lock_door_sites" -le 12 ] \
  || architecture_guard_fail "withTransactionOnHeldLock has $held_lock_door_sites call sites; the migration ratchet is 12 and may only fall"
[ "$held_lock_door_sites" -gt 0 ] \
  || architecture_guard_fail "withTransactionOnHeldLock has no call sites left; delete the door and this ratchet together"

# The governance table in docs/architecture-refactor.md drifted three times — 480 when the guard
# held 479, 7,918 when the suite observed 7,941, 490 when it observed 494 — and every time for the
# same reason: a count written in prose has no owner and nothing makes it go red. test.sh and this
# script hold the same numbers and fail the build when they drift, which is why they were right
# each time the table was wrong. So the table is no longer a copy. It is read back here and
# compared with the values this run just computed; a landing that moves a count must move it there
# too, or this fails before a compiler is started.
governance_doc=docs/architecture-refactor.md

documented_value() {
  awk -F'|' -v want="$1" '
    { gsub(/^[ \t]+|[ \t]+$/, "", $2) }
    $2 == want {
      value = $4
      gsub(/[ \t,]/, "", value)
      print value
      exit
    }
  ' "$governance_doc"
}

compare_documented() {
  local label=$1 actual=$2 documented
  documented=$(documented_value "$label")
  [ -n "$documented" ] \
    || architecture_guard_fail "governance table in $governance_doc has no row named '$label'"
  [ "$documented" = "$actual" ] \
    || architecture_guard_fail "governance table says $label is $documented; this run measured $actual"
}

documented_swift_receipt=$(sed -n "s/^expected_swift_receipt='\([0-9]*\) checks passed'/\1/p" test.sh)
[ -n "$documented_swift_receipt" ] \
  || architecture_guard_fail "could not read expected_swift_receipt from test.sh"

compare_documented "ordered groups" "$manifest_group_count"
compare_documented "ordered runners" "$runner_count"
compare_documented "suite files" "$suite_count"
compare_documented "Swift checks" "$documented_swift_receipt"
compare_documented '`Orchestrator.swift` ceiling' "$orchestrator_ceiling"
compare_documented '`RemoteServer.swift` ceiling' "$remote_server_ceiling"

echo "architecture boundaries: main=$main_lines lines, runners=$runner_count, groups=$manifest_group_count, suite_files=$suite_count, governance table agrees, held-lock door=$held_lock_door_sites"
