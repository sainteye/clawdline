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

# This ceiling is a ratchet that has been released, and the history is kept here because the
# current value cannot show that. A number on its own reads as monotone; most of these movements
# were not.
#
#   13,592  before Cut 1
#   12,819  after the store codec left           (a97fb176-era extraction, -773)
#   12,822  a dated /// comment landed           (+3, and main was briefly red for it)
#   12,819  reconciled at integration
#   12,816  after the registry owner left        (Cut 2 stage 1, -3)
#   13,123  the broker lease moved in            (2eef7bb6 / 15924b14, +307)
#   13,085  the lease's projection moved out     (correction round, -38)
#   11,932  the draft/refusal block moved out    (Cut 1b, -1,153: 1,155 lines of draft,
#                                              refusal and worktree lifecycle out, and two
#                                              lines of comment in, saying why the worktree
#                                              queue they enqueue on is no longer private)
#
# The raise to 13,123 was legitimate and reviewed — the lease is a landed, green feature and its
# code has to live somewhere — but of those 307 lines, roughly 250 are registry ownership, store
# wiring and route surface, which is what this file is for, and about sixty were pure
# `Record` -> dictionary projection. Those sixty now sit in `Sources/OrchestratorLease.swift`
# beside `OrchestratorLease.record(_:now:)`, which is the same translation for the same type.
#
# **The guard's meaning changed when the ratchet was released, and both halves matter.** It no
# longer promises "this file only shrinks". It promises "every growth is stated by somebody, in a
# diff, on purpose" — a different guarantee, still worth having, and one a reader who assumes the
# first will misread every number above.
#
# Set to the measured value with no headroom, on purpose: a ceiling with room in it is permission
# to grow that nobody reviewed. Anyone raising it again adds the line, the commit and the reason.
orchestrator_ceiling=11932
orchestrator_lines=$(line_count Sources/Orchestrator.swift)
[ "$orchestrator_lines" -le "$orchestrator_ceiling" ] \
  || architecture_guard_fail "Sources/Orchestrator.swift is $orchestrator_lines lines against a ceiling of $orchestrator_ceiling, and the ceiling is set to the measured value with no headroom on purpose — so one added line lands here. That is the ratchet working, not a mistake: take an equal amount out of the file, or raise the number and add your line to the history above it saying which commit raised it and why."

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

remote_server_ceiling=6463
remote_server_lines=$(line_count Sources/RemoteServer.swift)
[ "$remote_server_lines" -le "$remote_server_ceiling" ] \
  || architecture_guard_fail "Sources/RemoteServer.swift grew beyond the heavy-compile lease receipt ($remote_server_ceiling)"

if grep -q 'group(' Tests/main.swift; then
  architecture_guard_fail "new domain group found in Tests/main.swift"
fi

runner_count=$(grep -Ec '^run[A-Za-z0-9]+Tests\(\)$' Tests/main.swift || true)
[ "$runner_count" -eq 31 ] \
  || architecture_guard_fail "ordered domain runner count is $runner_count; expected 31"

manifest_group_count=$(awk '
  /^let expectedOrderedTestGroupTitles: \[String\] = \[/ { in_manifest = 1; next }
  in_manifest && /^\]/ { in_manifest = 0 }
  in_manifest && /",[[:space:]]*$/ { count++ }
  END { print count + 0 }
' Tests/TestGroupManifest.swift)
# 510 until the compile lease's second correction round, which adds the group for a refusal
# counting as a waiter's ask. 511 until the draft/refusal extraction, whose six groups are the
# first table-driven coverage the moved admission layer has ever had. A number that only ever
# rises silently is not a ratchet, so raises are named here the way the `Orchestrator.swift`
# ceiling's are.
[ "$manifest_group_count" -eq 517 ] \
  || architecture_guard_fail "ordered group manifest has $manifest_group_count entries; expected 517"

# One async function's suspension-point count is the sharpest cliff this repository has.
# Measured 2026-09-03, three files, kernel-tracked lifetime-max peaks:
#
#   runCloudAccountTests        143 await   47,163 MiB   330.0 s
#   runCloudCommandLedgerTests  131 await      954 MiB     3.1 s
#   runCloudTransportTests       61 await      283 MiB     1.0 s
#
# 143 against 131 is +9.2% suspension points, x49 peak, x106 time. No power law produces that:
# explaining 49x would need an exponent of 44. It is not a curve, it is a cliff somewhere between
# 131 and 143 -- and the 46 GiB frontend in tonight's two JetsamEvent crash reports is the file on
# the far side of it. The mechanism is not "async is expensive": per-phase peaks are typecheck
# 0.070, silgen 0.081, sil 0.090, irgen 0.104 GiB, all under a second. The blowup is in the LLVM
# pass pipeline after IRGen, which is superlinear in function size; async lowering is merely what
# grew one function that large. The rule is "do not let one function get that big".
#
# Correction to 0ae16887's commit message, which said a later run "was sampled at 3.44 GB and
# finished without approaching 46 GiB" and offered that as an open question about the world. Both
# halves are wrong, and the error is the one this repository keeps making: comparing two different
# instruments. 3.44 GB was a sampled RSS; 46.06 GiB is ri_lifetime_max_phys_footprint. RSS counts
# only resident, uncompressed pages, so a process whose pages the compressor has eaten reads low
# and harmless. That run also never finished -- its log ends in `signal 15`, and it never reached
# the file at all. What the machine did while it ran: swap file grown 10,240 -> 21,504 MB in four
# minutes, compressor 0.86 -> 12.36 GB, free pages pinned at 0.07 GB, and within thirty seconds of
# the kill: compressor -10.9 GB, swap used -9.7 GB, free +13 GB. A 3.44 GB process cannot do that.
# The honest open item is narrower: 46.06 GiB has been observed once, in one completed isolated
# compile; the second run that would have tested it was aborted, so there is no second reading.
# Recording it as "the same file only reached 3.44 GB elsewhere" would send the next person hunting
# a condition that does not exist -- the gap between those two numbers is the instrument, not the
# world.
#
# This is a ratchet at today's worst value, not a target. runCloudCommandLedgerTests sits at 131 --
# under the cliff, cheap today at 954 MiB, and about a dozen awaits from being what crashed this
# machine twice. It has to come down; until it does, this stops it climbing and stops anything else
# climbing to meet it. The next largest function in the tree is 61, so nothing else is near.
# This scanner is the one guard here that fails OPEN. The others count something by name, so
# renaming it sends their number to zero and that is red. This one derives a maximum: if its regex
# stops recognising a declaration -- a macro-generated function, a syntax Swift ships next year --
# the awaits inside it are silently attributed elsewhere, the maximum falls, and the ratchet waves
# through a function that is over the line. Nothing about that failure is visible.
#
# So the parse is checked against an independent count before its answer is used. A guard whose
# assumption can be overturned by the code it guards needs to notice when it has been.
scanner_funcs=$(python3 tools/suspension-scan.py --count Sources/*.swift Tests/*.swift)
grep_funcs=$(cat Sources/*.swift Tests/*.swift \
  | grep -cE '^[[:space:]]*(private |fileprivate |public |internal |static |final )*func [A-Za-z0-9_]+')
[ "$scanner_funcs" = "$grep_funcs" ] \
  || architecture_guard_fail "suspension scanner parsed $scanner_funcs function declarations, an independent count found $grep_funcs; the scanner has stopped recognising some declaration form and its maximum can no longer be trusted"

suspension_max=$(python3 tools/suspension-scan.py Sources/*.swift Tests/*.swift \
  | head -1 | awk '{print $1}')
[ -n "$suspension_max" ] \
  || architecture_guard_fail "suspension-point scan produced no output; that is a broken scanner, not a clean tree"
# This was a ratchet at 131 while runCloudCommandLedgerTests sat there — a value with no meaning
# except "today's worst", held only to stop it climbing. That function is now split into its
# twenty-five group blocks and the tree's worst is 61, so the ratchet has done its job and the
# guard can go back to being what it should have been: a threshold with a derivation.
#
# 100, because the cliff was measured between 131 and 143 and that leaves three tenths of margin,
# and because the largest function in the tree is 61 — far enough that ordinary growth does not
# trip it. A ratchet at 61 would be red the first time somebody adds five awaits to a test, which
# teaches people to raise the number rather than to split the function.
[ "$suspension_max" -le 100 ] \
  || architecture_guard_fail "one function owns $suspension_max suspension points; the limit is 100, derived from a cliff measured between 131 and 143 — split it rather than raising this"

suite_count=0
for suite in Tests/*Tests.swift; do
  [ -e "$suite" ] || continue
  suite_count=$((suite_count + 1))
  suite_lines=$(line_count "$suite")
  [ "$suite_lines" -le 2000 ] \
    || architecture_guard_fail "$suite has $suite_lines lines; suite stop-growth limit is 2000"
done
[ "$suite_count" -eq 44 ] \
  || architecture_guard_fail "suite file count is $suite_count; expected 44"

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

echo "architecture boundaries: main=$main_lines lines, runners=$runner_count, groups=$manifest_group_count, suite_files=$suite_count, governance table agrees, held-lock door=$held_lock_door_sites, max suspension=$suspension_max, parsed=$scanner_funcs"
