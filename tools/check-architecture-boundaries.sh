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
#   12,831  the broker lease was removed         (-254)
#   11,932  the draft/refusal block moved out    (Cut 1b, -1,153: 1,155 lines of draft,
#                                              refusal and worktree lifecycle out, and two
#                                              lines of comment in, saying why the worktree
#                                              queue they enqueue on is no longer private)
#
# The raise to 13,123 was legitimate and reviewed at the time — a landed, green feature's code has
# to live somewhere — and of those 307 lines roughly 250 were registry ownership, store wiring and
# route surface, which is what this file is for, with about sixty being pure `Record` -> dictionary
# projection that then moved out to sit beside the type it projected. The removal takes the 254
# that were still here, which is that raise less the projection that had already left. **It is the
# largest fall this ceiling has had, and it is not a refactor**: the feature is gone, not moved.
#
# **The guard's meaning changed when the ratchet was released, and both halves matter.** It no
# longer promises "this file only shrinks". It promises "every growth is stated by somebody, in a
# diff, on purpose" — a different guarantee, still worth having, and one a reader who assumes the
# first will misread every number above.
#
# Set to the measured value with no headroom, on purpose: a ceiling with room in it is permission
# to grow that nobody reviewed. Anyone raising it again adds the line, the commit and the reason.
#   11,678  rebased onto the lease removal        (the extraction measured itself at 11,932 on a
#                                              base that still had the lease; 254 of those lines
#                                              had already gone by the time it merged, and the
#                                              ceiling briefly carried that stale figure)
#   11,874  the handoff label became durable      (+196: 153 for the record, its codec, the
#                                              rehydration, the suppress/unsuppress pass, the
#                                              first-identity adoption and the reclaim, and 43
#                                              for the correction that split "is it bound?" from
#                                              "is a field missing?" and gave the projection an
#                                              ambiguity refusal. A feature's code arriving;
#                                              nothing left the file)
#   11,925  the delivery push moved in            (0c4c85c7, +51)
#   11,063  the task shape and the child briefing moved out
#                                              (Cut 3, 47740b5c, -862: OrchestratorTaskShape 457
#                                              lines and OrchestratorChildBrief 418, plus the two
#                                              `// MARK:` headings they were named by, whose text
#                                              now lives in the new files' headers. That landing
#                                              added no line here; this one is written from its
#                                              diff so the jump from 11,925 is not unexplained)
#   11,071  claims became mandatory                (+9: the door itself is nine lines — three of
#                                              comment saying why it sits above the live-session
#                                              scans, and six for the call and its two exemptions.
#                                              The refusal, its message and the reasoning behind
#                                              both live in `OrchestratorDraft`, beside the other
#                                              ingress refusals, so what landed here is the call
#                                              site and nothing else)
#   10,584  the root-assignment shapes and the child-identity
#           block moved out                    (Cut 4, -487: OrchestratorRootAssignmentShape 205
#                                              lines and OrchestratorChildIdentity 329, chosen by
#                                              measurement rather than by `// MARK:`. Both blocks
#                                              acquire this file's lock zero times and referenced
#                                              no `private` symbol left behind — the two cheapest
#                                              of the eleven candidates measured, and the second
#                                              of them had no heading of its own: it sat in the
#                                              middle of `Independent feature roots`, which never
#                                              described it. No `// MARK:` moved. 485 of the 487
#                                              are the two blocks; the other 2 are the test reset,
#                                              which reached into the ownership memo cache in
#                                              three lines and now calls
#                                              `resetTranscriptOwnershipCacheForTesting()` in one,
#                                              so the cache stays private to the file that owns it)
#   10,628  task retention became a setting        (4eb97d86, +44: a 40-line block above
#                                              `cleanup()` — 10 for `TaskRetentionCandidate`, 28
#                                              for `taskRetentionSweep`, which answers the two
#                                              limits separately so a caller can say which one
#                                              fired — and 4 net inside `cleanup()` itself, which
#                                              now reads three settings instead of two literals
#                                              and builds its candidates once under the lock. The
#                                              24 hours and the 200 rows were written into this
#                                              file; the count was the binding one, sweeping a
#                                              task record in about five days, and 149 usage rows
#                                              could never be attributed because the record they
#                                              needed was gone. Nothing left the file)
#
# The 11,925 raise is a feature landing rather than a relocation: the notification that used to
# fire when a turn stopped now fires on a root's own delivery receipt, and the push lives where
# that receipt is created. Of the 51 lines, 12 are the sender, 9 the pure wording beside
# `batchMessage`, 4 the test seam and its reset, and the rest are the doc comments that say why
# `smart_notifications` means something narrower on this path than on any other.
#
# 10,585 is the sender contract's relocation, taken rather than left as headroom. `844a4e08` moved
# `HandoffDraft`, `handoffDraft` and the sender verdict out into
# `Sources/OrchestratorHandoffSender.swift` and did not lower this number with them, so the file
# sat 43 lines under a ceiling that says two lines above it that it has none on purpose. The guard
# stayed green because the slack check below only refuses a gap over 200 — which is the shape this
# comment block keeps warning about, found by the reviewer of that very delivery and not by
# anything here. A relocation lowers the ceiling; only a feature raises it.
orchestrator_ceiling=10585
orchestrator_lines=$(line_count Sources/Orchestrator.swift)
[ -n "$orchestrator_lines" ] \
  || architecture_guard_fail "orchestrator_lines came back empty; that is a broken script or a missing file, not a clean tree"
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

# A receipt, not a ratchet: it is set to the measured value and raising it is a stated act.
#   5,446  before the cloud read door
#   5,508  the cloud read door arrived        (+62, counted per hunk rather than estimated: +33 for
#                                          `routeVerifiedCloudRead`, which sends a verified cloud
#                                          read down the same two bounded lanes a phone on the
#                                          tunnel uses instead of opening a second one; +19 for
#                                          the `Request` initializer that builds those two routes
#                                          from a closed enum so a viewer can never name a third;
#                                          and +11 -1 for splitting `readSlowly` into a socket
#                                          half and a `deliver:` half so both callers share one
#                                          budget. A feature's code arriving; nothing left.)
#   5,532  the other four reads joined them  (+24, counted per hunk: +17 in the `Request`
#                                          initializer — twelve lines of route for `agents`,
#                                          `shells`, `skills` and `git`, and five saying that an
#                                          id goes through the same escaping its direct-path call
#                                          site uses — and +7 of comment on `routeVerifiedCloudRead`
#                                          saying why those four take the shared queue: because
#                                          `isTranscriptReading` and `isSlowReading` refuse them on
#                                          the direct path too, so a lane here would be a second
#                                          policy nobody measured. No new routing code: the
#                                          classification written for the first two already sends
#                                          everything that is neither lane to `dispatch`.)
#   5,537  transcript images cross            (+5, one hunk: the `.image` case of the same
#                                          `Request` initializer, which builds the artifact route
#                                          the direct path's `<img>` already asks for. Three of the
#                                          five lines are the comment saying why the id is encoded
#                                          rather than validated a second time here. The picture's
#                                          own bound, its base64 and its refusal live in
#                                          `CloudAppBridge.swift`, which is where an envelope's
#                                          size limit belongs — this file only names the route.)
#   5,570  schedules and the build stamp      (+33, counted per hunk: +23 for
#                                          `orchestratorSnapshot()` with its comment, one body for
#                                          the two publishers that had each written their own
#                                          literal; +11 for `appStamp()` and the paragraph saying
#                                          why it is narrower than `restartHelloPayload()`; -1
#                                          where `broadcastOrchestrator` stopped spelling its
#                                          dictionary out. A new file does not work here: `queue`,
#                                          `streams` and `enqueueCloudPublication` are all
#                                          `private` to this type, and both callers of the new
#                                          body are methods on it.)
#   5,652  the live screen's two routes       (+87, and the ceiling is the file measured rather
#                                          than 5,570 plus 87: the receipt above had five lines of
#                                          slack in it, and carrying slack forward is how a receipt
#                                          stops being one. The lines are the `LiveScreens` holder,
#                                          `GET /v1/sessions/:id/screen` and `GET /v1/screens`, the
#                                          reclaim on start and the stop that has to be synchronous.
#                                          The mechanism itself is not here — it is
#                                          `Sources/LiveScreen.swift`, a new file, because this one
#                                          is a router and a FIFO lifecycle is not routing.)
#   5,670  the project-worktrees read arrived   (+13, counted per hunk: +10 for the route case —
#                                          the parse, the 400 on a bad query, and the two arms of
#                                          the service's closed answer — and +3 naming that path
#                                          in `isUsageAnalyticsReading`, with the comment saying
#                                          why it takes the analytics worker rather than a lane of
#                                          its own: it is the same bounded scan of the same store.
#                                          The projection, its ladder and its refusals are all in
#                                          `UsageLedger.swift`; what landed here is the door.)
#   5,571  the documents route arrived        (+6, one hunk and no more: the `case` and the three
#                                          lines of comment saying that both roots are computed
#                                          here rather than named by the caller. The route's whole
#                                          body — the boundary, the listing, the refusals and the
#                                          response — is in `Sources/ProjectArtifact.swift`,
#                                          beside `projectArtifactResponse`, whose slot-not-path
#                                          safety argument it is the successor to. A new file was
#                                          not worth it for that: this one already holds the two
#                                          named artifact slots and `linksPayload`, and a document
#                                          route is the same subject.)
#   5,676  the documents route joined it      (the ceiling is the file measured: 5,676. 5,670 plus
#                                          the branch's six is the same number, and that agreement
#                                          is not the reason it is written here — a receipt that
#                                          was added to instead of taken is a receipt about a tree
#                                          nobody looked at.)
#   5,726  the snippet routes arrived       (+50, measured: six route cases, the one-line
#                                          `answer(_ reply: Snippets.Reply)` envelope beside the
#                                          orchestrator's, and `snippets` on the orchestrator
#                                          snapshot. The store, the bounds, the typed refusals,
#                                          the write brake and the rule that decides which project
#                                          a session is in are all in `Sources/Snippets.swift`, a
#                                          new file, because a router is not a place to keep a
#                                          store. What landed here is six doors and the envelope
#                                          they answer through.)
remote_server_ceiling=5726
remote_server_lines=$(line_count Sources/RemoteServer.swift)
[ -n "$remote_server_lines" ] \
  || architecture_guard_fail "remote_server_lines came back empty; that is a broken script or a missing file, not a clean tree"
[ "$remote_server_lines" -le "$remote_server_ceiling" ] \
  || architecture_guard_fail "Sources/RemoteServer.swift grew beyond its receipt ($remote_server_ceiling)"

if grep -q 'group(' Tests/main.swift; then
  architecture_guard_fail "new domain group found in Tests/main.swift"
fi

runner_count=$(grep -Ec '^run[A-Za-z0-9]+Tests\(\)$' Tests/main.swift || true)
# 32 once the document route got a suite of its own. It was written into
# `Tests/MarkdownTests.swift` first, beside the two named artifact slots it succeeds, and that
# file came out at 2,005 lines against the 2,000-line stop-growth limit below — which is the
# limit doing exactly what it is for, so the group moved into its own file rather than being
# trimmed to fit under a wall it would have left the next person standing at.
# 33 once snippets arrived: a store with its own file, its own bounds and its own scope rule gets
# its own runner rather than a group wedged into somebody else's suite.
[ "$runner_count" -eq 33 ] \
  || architecture_guard_fail "ordered domain runner count is $runner_count; expected 33"

manifest_group_count=$(awk '
  /^let expectedOrderedTestGroupTitles: \[String\] = \[/ { in_manifest = 1; next }
  in_manifest && /^\]/ { in_manifest = 0 }
  in_manifest && /",[[:space:]]*$/ { count++ }
  END { print count + 0 }
' Tests/TestGroupManifest.swift)
# 510 until the compile lease's second correction round, which added the group for a refusal
# counting as a waiter's ask, then 511; 498 once that lease's thirteen groups were removed with it;
# 503 once the local Feature classifier landed its five — the classifier's own rung ladder, its
# acceptance policy, the conflicting-head refusal, the backfill dry run, and the payload's statement
# of whether a producer is configured; 504 when that classifier's correction round added the sixth,
# for the Project scope a Feature and the Projects table now resolve by one shared rule.
# A number that only ever rises silently is not a ratchet, so both directions are named here the
# way the `Orchestrator.swift` ceiling's are.
# 509 once the landing queue landed its five: derived membership, a coordinator setting position
# and only position, the contended-path answer, the broker-made slot handoff, and the
# landing-time write set an isolated dispatch used to hand back and drop.
#
# **This number and the manifest it counts moved apart once, and the guard stayed green.** On
# 2026-09-03 a root took its own manifest edit back out of the shared tree while the lease removal
# merged, and afterwards the manifest held 498 and this line expected 498 — they agreed with each
# other and were wrong together, while `Tests/UsageLedgerTests.swift` still declared five groups
# neither of them listed. Nothing here can see that: what catches it is
# `validateExecutedTestGroupManifest()`, which needs a whole suite run, and
# `verify_swift_source_manifest`, which refuses to start one. **A green light that two edited
# numbers produced by agreeing with each other looks exactly like a correct one.**
# 516 once claims became mandatory: the group that used to prove the undeclared dispatch was
# warned now proves it is refused, and a second group holds the refusal's own four-row rule.
# 517 once a detached tmux start had to say where it went: one group, which takes the success arm
# of the start route for the first time — every earlier test of it stops at a refusal, because
# taking that arm meant opening a real terminal until `StartPoints.Fixture` existed.
# The number in the message below was 515 while the check read 516, which is the failure this
# comment block is about wearing its own costume: a guard whose message names a different number
# from the one it enforces cannot be read to find out what it wants.
# 519 once task retention became a setting: two groups, one for the pure sweep and one for the
# three settings reaching it.
# 520 once a landing node could read the receipt the root wrote on the delivery beside it: one
# group, which is the first time any test in this tree observes a landing node reaching `done`.
# 521 once a Feature row could name its Project: one group, and it is the first test in this tree
# to assert that two of the Portfolio's tables carry the *same* id for the same Project rather
# than each carrying one of its own.
# 526 with the live screen's five: the two tmux commands that attach and take off a pipe, the
# coalescing window as arithmetic, the demand that costs nothing while nothing moves, the lease
# whose expiry takes the pipe with it, and the backend that has to say what it cannot do. The
# number is the manifest counted, not 521 plus five: the delivery was cut when the manifest also
# held 521, and adding its increment to a number that had reached the same value by a different
# road is how two edits agree with each other and are wrong together.
# 527 once a `tmux -CC` reveal had to name the tab: one group, and it is the first test in this
# tree that stands on a measurement nothing in here can take — whether iTerm2 moves its selected
# tab when tmux's active window changes, which is a fact about two running applications on
# somebody's desktop. The group proves what follows from the answer (it does not), and the answer
# itself is written down with its commands in `docs/interface.md`.
# 528 with the update check's one: the checker holding a reading between launches, the file it
# keeps it in, and a refusal reaching disk looking like a refusal rather than like an all-clear.
# The decision that reading rests on is not in this number at all — it is compiled and run by
# `Tests/update-check.mjs` out of a marker-bounded block, which takes a second instead of a module.
#
# **Both of the two entries above were written as 527, each correct about its own tree.** They were
# cut in parallel, each from a manifest holding 526, and each counted rather than added — which is
# the rule three paragraphs up and it did not save them, because the rule guards against adding to a
# stale number and says nothing about two branches measuring the same fresh one. The merge is where
# that shows, and only there: neither side is wrong, and the sum of two right answers is not one.
# 528 was that merged tree counted with the awk above.
# 530 once both arrived together: counted from the manifest, never one total plus another, which
# is the mistake the paragraph above is about.
# 524-in-isolation once a Project could be asked which of its worktrees finished a Feature: three groups — the
# outcome ladder that tells a landed delivery from one nobody landed from debris, the read-time
# join itself, and the pair this read exists to keep apart, an empty list with rows behind it
# against a Project nothing in range mentions. The third is the first test in this tree to assert
# that an empty answer carries the receipt proving the query ran.
# 531 is this merged tree counted with the awk above: main's update-check group and the three this
# branch brought, met by a merge that touched neither manifest. Adding would have given 531 too,
# and that is the point rather than a reprieve — the two numbers agreeing is not what makes either
# right, and the paragraph above is about the day two branches agreed and were wrong together.
# 522 once documents could be read from a phone: one group, and it is the first test in this tree
# that asserts a route refuses a caller-supplied *path* rather than a caller-supplied name — the
# two named artifact slots before it could not be asked for a path at all.
# 532 is this merged tree counted with the awk above: 531 was counted on the tree the Projects
# page landed on, and the documents group is the one this branch brought.
# 536 once snippets landed their four: the store's strictness and its UUID-only addressing, the
# scope rule that follows the mark and then the git common directory, the routes sharing the write
# gate with their typed refusals, and the snapshot carrying a list a session read is already
# filtered from.
[ "$manifest_group_count" -eq 536 ] \
  || architecture_guard_fail "ordered group manifest has $manifest_group_count entries; expected 536"

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
# 46 once Tests/SnippetStoreTests.swift arrived with the store it proves.
[ "$suite_count" -eq 46 ] \
  || architecture_guard_fail "suite file count is $suite_count; expected 46"

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

# Cut 4 chose its two files by measuring, and what it measured was that neither of them touches
# the registry lock. That is the whole reason they were cheap: eleven candidates were scored on
# lines, private symbols crossing the proposed boundary, and lock acquisitions, and the two that
# went are the ones whose lock count was zero. `Cross-session coordination waits` — 357 lines, two
# crossing privates, eight acquisitions — is the candidate that looks clean by the first number and
# is a lock-ownership question by the third, which is the rule this repository already wrote down.
#
# A property that decided a cut and is then never checked again lasts until the next person adds a
# convenience. So it is checked. This is not a ratchet: the number is zero and stays zero, because
# a file here that needs the lock belongs back beside the state the lock protects.
#
# **The scan is calibrated before its zero is believed.** The same pattern is run against
# `Orchestrator.swift`, which is known to take the lock in three figures. If the spelling of taking
# the lock ever changes, that control goes to zero and this fails there — rather than reporting a
# clean zero for the two files because it can no longer recognise what it is looking for. That is
# the failure this repository has shipped before: a guard that stopped matching read exactly like a
# guard that passed.
lock_acquisition_re='(^|[^A-Za-z0-9_])(lock\.lock\(\)|Orchestrator\.lock|withTransaction(OnHeldLock)?[[:space:]]*[({])'
count_lock_sites() {
  grep -vE '^[[:space:]]*(//|/\*|\*)' "$1" | grep -cE "$lock_acquisition_re" || true
}
lock_scan_control=$(count_lock_sites Sources/Orchestrator.swift)
[ "${lock_scan_control:-0}" -gt 100 ] \
  || architecture_guard_fail "the lock-acquisition scan found only ${lock_scan_control:-0} sites in Sources/Orchestrator.swift, which takes the lock in three figures; the pattern has stopped recognising how this tree takes the lock, so the zero it would report for the lock-free files below means nothing"

for lock_free in Sources/OrchestratorRootAssignmentShape.swift Sources/OrchestratorChildIdentity.swift; do
  [ -f "$lock_free" ] \
    || architecture_guard_fail "$lock_free is missing; it was cut out of Orchestrator.swift because it holds no lock, and this check cannot say that about a file that is not there"
  lock_free_sites=$(count_lock_sites "$lock_free")
  [ "${lock_free_sites:-0}" -eq 0 ] \
    || architecture_guard_fail "$lock_free acquires the registry lock at ${lock_free_sites} site(s). It was taken out of Orchestrator.swift precisely because it took the lock zero times; code that needs the lock belongs beside the state the lock protects, not here."
done

# The governance table in docs/architecture-refactor.md drifted three times — 480 when the guard
# held 479, 7,918 when the suite observed 7,941, 490 when it observed 494 — and every time for the
# same reason: a count written in prose has no owner and nothing makes it go red. Reading the table
# back and comparing it row by row fixed that for the five rows this script measures, and left the
# sixth exactly as it was: `Swift checks` compared a number in the doc with a number in test.sh, two
# records, neither of which had touched the tree. On 2026-09-03 a commit added eight checks and
# updated neither, so the two agreed, so this guard was green while `main` ran 8,101 against a seal
# of 8,093. **Two copies agreeing is not evidence, and the comparison that produced that green could
# not have produced anything else.**
#
# So the table stops being a list of numbers and becomes a rendering. This script already holds all
# six — it counts three of them, owns two as ratchets, and reads the sixth out of test.sh — so it
# renders the block itself and compares the committed one against that rendering. Nobody types a
# governance number into the doc any more; `tools/generate-governance-table.sh` writes what
# `--emit-governance-table` prints. Every row is now a value against its own rendering, which is a
# comparison that cannot be satisfied by two people making the same mistake twice.
governance_doc=docs/architecture-refactor.md
governance_marker_open='<!-- clawdline-governance-table:v1 -->'
governance_marker_close='<!-- /clawdline-governance-table:v1 -->'

# The table has always written its larger numbers with thousands separators and the smaller ones
# without, so the rendering does too rather than reformatting a document to suit a script.
with_thousands() {
  awk -v n="$1" 'BEGIN {
    out = ""
    while (length(n) > 3) {
      out = "," substr(n, length(n) - 2) out
      n = substr(n, 1, length(n) - 3)
    }
    print n out
  }'
}

render_governance_table() {
  printf '%s\n' \
    '| | value on this tree | the one place it is written |' \
    '|---|---:|---|' \
    "| ordered groups | $(with_thousands "$manifest_group_count") | \`Tests/TestGroupManifest.swift\`, counted by the guard |" \
    "| ordered runners | $(with_thousands "$runner_count") | \`Tests/main.swift\`, counted by the guard |" \
    "| suite files | $(with_thousands "$suite_count") | \`Tests/*Tests.swift\`, counted by the guard |" \
    "| Swift checks | $(with_thousands "$documented_swift_receipt") | \`expected_swift_receipt\` in \`test.sh\`, set from a run |" \
    "| \`Orchestrator.swift\` ceiling | $(with_thousands "$orchestrator_ceiling") | the ratchet in \`tools/check-architecture-boundaries.sh\` |" \
    "| \`RemoteServer.swift\` ceiling | $(with_thousands "$remote_server_ceiling") | the receipt in \`tools/check-architecture-boundaries.sh\` |"
}

documented_swift_receipt=$(sed -n "s/^expected_swift_receipt='\([0-9]*\) checks passed'/\1/p" test.sh)
[ -n "$documented_swift_receipt" ] \
  || architecture_guard_fail "could not read expected_swift_receipt from test.sh"

# The compile-job ceiling must not exist before ./test.sh holds the machine lock. b8dfd0ff moved
# this block above the acquisition and an eight-way 104-file typecheck then ran outside the lock for
# weeks; on 2026-09-03 a sampler caught eight frontends alive with the lock free, and the chain's
# top was another line's own landing script, queueing for a machine it had just heated up. The
# repair deletes the coupling rather than setting it to one, so this anchors on the marked block and
# the lock's call site — an anchor on `export CLAWDLINE_COMPILE_JOBS` would pass afterwards for the
# reason that its subject no longer exists anywhere, and one on the name alone matches the function
# definition and a comment, putting the lock 343 lines early. Proved four ways against a4ed9edb,
# which still carries the defect.
# Both anchors are counted before either is used. Taking the first hit and moving on was wrong in
# two ways that were only found by mutating this file. `head -1` on the lock matched an assignment —
# `acq=clawdline_acquire_suite_lock` — and the guard then measured against wherever the name first
# appeared rather than where the lock is taken, so a ceiling placed between the two read as green;
# the `()` filter had excluded the definition, which is what its author thought of, and nothing had
# excluded an assignment. And a second stray ceiling marker made the guard report the first one's
# line while the real block sat correctly below the lock, sending a reader to a line that is not the
# problem. A count of both, reported when it is not one, costs two greps and closes both.
ceiling_block_lines=$(grep -c '^# >>> clawdline compile ceiling >>>' test.sh || true)
ceiling_block_line=$(grep -n '^# >>> clawdline compile ceiling >>>' test.sh | head -1 | cut -d: -f1)
suite_lock_hits=$(grep -n 'clawdline_acquire_suite_lock' test.sh \
  | grep -v '^[0-9]*:[[:space:]]*#' | grep -v 'clawdline_acquire_suite_lock()' \
  | grep -v '^[0-9]*:[[:space:]]*[A-Za-z_][A-Za-z0-9_]*=')
suite_lock_line=$(printf '%s\n' "$suite_lock_hits" | head -1 | cut -d: -f1)
[ "${ceiling_block_lines:-0}" -le 1 ] \
  || architecture_guard_fail "test.sh carries $ceiling_block_lines compile-ceiling markers; this check reads the first and cannot say which one rations the compile, so fix the duplicate rather than trusting the line it names"

{ [ -n "$ceiling_block_line" ] && [ -n "$suite_lock_line" ]; } \
  || architecture_guard_fail "cannot locate the compile-ceiling block or the suite lock in test.sh (block=${ceiling_block_line:-missing} lock=${suite_lock_line:-missing}); if either was renamed, update this check rather than deleting it"
[ "$ceiling_block_line" -gt "$suite_lock_line" ] \
  || architecture_guard_fail "test.sh settles its compile ceiling at line $ceiling_block_line but does not hold the machine lock until $suite_lock_line, so everything it runs above the lock can compile wide with nothing rationing it"

# Rendering the table from the seal removes the second copy of the Swift-checks number and does not
# make that number true. `expected_swift_receipt` is a record of what one run reported, and a record
# with nothing to compare against is green whatever it says — which is how eight added checks stayed
# invisible until somebody else's suite run hours later. No guard can count checks without running
# them, so what stands in for the count is a witness of the tree the count was taken on: the number
# of assertion call sites in the sealed test sources. It is a record against a measurement, the
# shape the other five rows already have, and it goes red on the thing that actually happened —
# an assertion added to a test file with the seal left alone.
#
# It is deliberately not a model of the total. Two of a4ed9edb's four checks came from one call site
# inside a two-variant loop; the witness would have caught that, because the sites moved. Widening a
# loop around an existing check moves the total and no site, and nothing before the run sees it.
# `report_receipt_direction` in test.sh still catches that at the end, and the run is still what
# settles the number. This closes the case that has bitten twice, not the general one.
#
# The scan fails closed in every way it can fail. If `\b` stops being honoured, or the names change,
# the count moves or falls to zero, and both are red.
sealed_assertion_sites=$(sed -n 's/^expected_swift_receipt_witness=\([0-9]*\).*$/\1/p' test.sh)
[ -n "$sealed_assertion_sites" ] \
  || architecture_guard_fail "could not read expected_swift_receipt_witness from test.sh; it says which tree expected_swift_receipt was measured on, and without it the seal is a number nothing can check"
assertion_sites=$(cat Tests/*.swift \
  | grep -oE '\b(check|expect)[A-Za-z0-9_]*\(' | wc -l | tr -d '[:space:]' || true)
[ "${assertion_sites:-0}" -gt 0 ] \
  || architecture_guard_fail "the assertion-site scan of Tests/*.swift found nothing; that is a broken scan, not a tree without tests"

# The two ceilings above are the only rows here whose left-hand side is also a record: both are
# constants in this script, compared against constants in a document. Everything else on that list
# is derived from the thing it describes on every run, so it cannot agree with a stale copy.
#
# That matters because the check which reads the real file is `-le`, a bound, and a bound cannot
# see slack. A ceiling set too high passes it, passes the table row if both records were edited
# together, and silently licenses the growth it was supposed to stop. This line shipped that
# mistake once: an extraction measured 11,932 on a base that still had the lease, the merged tree
# was 11,678, and 254 lines of headroom nobody granted survived every check but one — and that one
# only fired because the script and the table had been edited separately.
#
# So the ceilings are pinned to the file as well. A ceiling more than 200 lines above what it
# guards is not a ceiling, it is a budget nobody approved.
orchestrator_slack=$(( orchestrator_ceiling - orchestrator_lines ))
[ "$orchestrator_slack" -le 200 ] \
  || architecture_guard_fail "Sources/Orchestrator.swift is $orchestrator_lines lines under a ceiling of $orchestrator_ceiling; $orchestrator_slack lines of unearned headroom. Lower the ceiling to what the tree measures."
remote_server_slack=$(( remote_server_ceiling - remote_server_lines ))
[ "$remote_server_slack" -le 200 ] \
  || architecture_guard_fail "Sources/RemoteServer.swift is $remote_server_lines lines under a ceiling of $remote_server_ceiling; $remote_server_slack lines of unearned headroom. Lower the ceiling to what the tree measures."

# A `Task { … }` started inside the ledger tests races the statements after it. `Task.yield()`
# does not order it: yield hands the executor one turn, it does not wait for the task to reach the
# point the next statements assume. Five of the six sites were written with a real barrier and the
# sixth was not, and that one difference produced a red that four sessions spent two hours
# attributing to three innocent commits.
#
# `waitUntilTransactionCount` waits for an event, so it absorbs a delay of any size; `yield`
# absorbs zero. Measured on one binary: with 50ms injected inside the duplicate, yield went red
# 3 of 3 and the barrier stayed green 3 of 3.
#
# The assertion is about shape, not about the barrier's argument. All six sites pass `3` today,
# and a `waitUntilTransactionCount(2)` would satisfy this check and still race.
#
# Extend this list when a genuinely-ordering primitive is added. A new, correct barrier missing
# from here turns a correct line red, and the repair is to add its name — never to go back to
# `Task.yield()`.
ledger_sync_points='waitUntilTransactionCount|waitUntilPersisted|waitUntilRowCount'
ledger_tests=Tests/CloudCommandLedgerTests.swift

# `Task` written any of the ways Swift allows, not just `= Task {`. The first version of this
# pinned that one spelling, and `Task { … }` unbound, `Task<V, E> { … }` and `Task.detached`
# went past it in silence — a guard that misses reads exactly like a guard that passes.
# awk has no `\b`, so the boundary is spelled out; `->` skips return types and the comment
# skip keeps a commented-out example from counting.
ledger_task_re='(^|[^A-Za-z0-9_])Task([^A-Za-z0-9_]|$)'
ledger_task_sites=$(awk -v re="$ledger_task_re" '
  /^[[:space:]]*\/\// { next }
  $0 ~ "->[[:space:]]*Task" { next }
  $0 ~ re && /\{/ { c++ }
  END { print c+0 }' "$ledger_tests")
[ "$ledger_task_sites" -gt 0 ] \
  || architecture_guard_fail "found no Task sites in $ledger_tests; the scanner is broken, not the tree"

unordered_ledger_tasks=$(awk -v ok="$ledger_sync_points" -v re="$ledger_task_re" '
  /^[[:space:]]*\/\// && pending != 1 { next }
  $0 ~ "->[[:space:]]*Task" { next }
  $0 ~ re && /\{/ { site = NR; pending = 1; next }
  pending == 1 && ($0 ~ /^[[:space:]]*$/ || $0 ~ /^[[:space:]]*\/\//) { next }
  pending == 1 { pending = 0; if ($0 !~ ok) printf "%d:%s\n", site, $0 }
' "$ledger_tests")

[ -z "$unordered_ledger_tasks" ] \
  || architecture_guard_fail "$(printf '%s\n' \
       "a Task in $ledger_tests is followed by something that does not order it:" \
       "$unordered_ledger_tasks" \
       "Wait for the task to arrive (store.waitUntilTransactionCount), not for one executor turn." \
       "If you added a new ordering primitive, add its name to ledger_sync_points in this script.")"

# Printing the table is the only thing this mode does, and it happens before the comparison below so
# that a doc which has fallen behind can still be regenerated. Everything above has already run, so
# a tree that fails a ratchet cannot render itself a table saying otherwise.
if [ "${1:-}" = "--emit-governance-table" ]; then
  render_governance_table
  exit 0
fi

# Re-sealing is a cycle without this door, and rendering the table did not close it — it moved it
# one step earlier, which is where it belongs. The seal's witness now goes red the moment a check is
# added, before a compiler starts; the true total is only known once the suite has finished. So the
# door is what lets the run that produces the number start at all. `CLAWDLINE_RESEAL=1` says out
# loud that this run exists to produce it, and downgrades the witness to a warning. Nothing else
# relaxes — including the table, which never needs to: it is rendered from the seal, so it is still
# in agreement with the seal that has not been changed yet. The suite still ends on
# `verify_test_completion_receipts`, which still fails and says which way the total moved.
if [ "$assertion_sites" != "$sealed_assertion_sites" ]; then
  if [ "${CLAWDLINE_RESEAL:-}" = "1" ]; then
    echo "architecture boundaries: CLAWDLINE_RESEAL=1 — the seal was measured on a tree with $sealed_assertion_sites assertion call sites and this one has $assertion_sites." >&2
    echo "Letting the run proceed so it can report the real total. Set expected_swift_receipt from what it reports and expected_swift_receipt_witness to $assertion_sites, then run tools/generate-governance-table.sh; this run's own receipt check still has to pass." >&2
  else
    architecture_guard_fail "test.sh seals $documented_swift_receipt checks against a tree with $sealed_assertion_sites assertion call sites, and this tree has $assertion_sites — so the seal was measured somewhere else and no run has produced a total for what is here. Re-run with CLAWDLINE_RESEAL=1 to let the suite report the real total, then set expected_swift_receipt from it and expected_swift_receipt_witness to $assertion_sites."
  fi
fi

# One comparison for all six rows, against the rendering above rather than against six hand-typed
# cells. It catches a row edited, and also a row renamed, reordered or deleted, which six per-row
# lookups could not: a lookup for a row that is gone reports a missing row, and a lookup nobody
# wrote reports nothing at all.
documented_governance_table=$(awk -v opener="$governance_marker_open" -v closer="$governance_marker_close" '
  $0 == opener { inside = 1; found = 1; next }
  $0 == closer { inside = 0 }
  inside      { print }
  END         { exit found ? 0 : 3 }
' "$governance_doc" | sed '/^[[:space:]]*$/d') \
  || architecture_guard_fail "governance table markers are missing from $governance_doc; the table is generated by tools/generate-governance-table.sh and the markers are how it finds where to write"
[ -n "$documented_governance_table" ] \
  || architecture_guard_fail "the governance table in $governance_doc is empty between its markers; run tools/generate-governance-table.sh"
if [ "$documented_governance_table" != "$(render_governance_table)" ]; then
  echo "governance table in $governance_doc is not what this tree renders. Committed:" >&2
  printf '%s\n' "$documented_governance_table" >&2
  echo "Rendered:" >&2
  render_governance_table >&2
  architecture_guard_fail "run tools/generate-governance-table.sh — the table is generated, so the fix is never to retype a number into it"
fi

echo "architecture boundaries: main=$main_lines lines, ceiling after lock ($ceiling_block_line>$suite_lock_line), runners=$runner_count, groups=$manifest_group_count, suite_files=$suite_count, governance table is this run's own rendering, seal witness=$assertion_sites sites, held-lock door=$held_lock_door_sites, max suspension=$suspension_max, parsed=$scanner_funcs"
