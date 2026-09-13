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
#
# 10,606 is a landing reaching the ledger when it is written instead of at the next launch (+21:
# two call sites of one line each in `updateLanding`, and `recordLandingInLedger` — 3 lines of
# code and 16 of comment saying why all three landing states go over and why this is wiring rather
# than a mechanism). Nine landings sat invisible to every ledger reader on 2026-09-05 until a
# rebuild happened to run the backfill, because finalize — the only other writer — always runs
# before anybody can land the work. A feature, so it raises rather than being absorbed.
#
# 10,618 is the other half of that landing reaching the ledger (+12: two four-line write-backs in
# `updateLanding`, one on the idempotent `landed -> landed` re-send and one on the race that finds
# it landed after the verification subprocesses, plus their four lines of comment). The raise above
# wired the paths that *change* a landing, and a re-send changes nothing by definition — so a
# landing recorded before that wiring existed had no door at all and waited for the next launch,
# which is the wait the feature was built to remove. Its review found it; the ceiling is what makes
# the fix visible rather than absorbed.
#
# The two paragraphs below were written on the 2026-09-05 branch that was never merged, against a
# base of 10,585, so their absolute numbers are that tree's and their reasons are this one's:
#
# +19 for the read-only delivery's landing state. `nothing_to_land` is a feature and not a
# relocation: nineteen lines of route, all of them inside `updateLanding` — the machine-only
# credential it shares with `landed`, the field it refuses, and the evidence gate that reads the
# predicate `Sources/OrchestratorTaskShape.swift` owns. The predicate, the enum case and its
# reasons went to that file rather than here, which is why the raise is nineteen and not ninety.
#
# +7 for that same state reading the write set an isolated task actually declared. Seven lines:
# `claims` is empty by design for a worktree child — the broker drops the lease and hands the list
# back as `claims_ignored_for_worktree` — so the gate was reading the one spelling that positively
# means "writes nothing" off every one of them. `OrchestratorLandingQueue.retainedLandingPaths()`
# is a store with a lock of its own, so it is read before the registry lock rather than inside it.
#
# 10,644 is this merged tree counted with `line_count` above: 10,618 was counted on the tree the
# landing verdict landed on, and the 26 lines are the two features that branch brought. Adding
# would have given 10,644 too; the number here is the measurement rather than the sum, because a
# sum cannot tell a feature that arrived twice from one that arrived once.
#
# 10,742 is every notification that names a session carrying that session's address. A feature:
# five producers wrote `url: "/"` while their titles were session names, so a tap on any of them
# ended on the session list. Measured per hunk, +111 -6:
#   +73  a `// MARK: - Where a notification points` section — three `pushURL` overloads (the
#        unchecked one for an id this Mac already holds, the checked one for an id a phone handed
#        in, and the first-of-these one a fan-out falls back through), `scheduleFailureSessionID`,
#        `isWatchedSession` with its test seam, and the doc comments saying which of them each
#        caller wants and why an address that opens nothing is worse than the list.
#   +7-3 `sendAgentPush` carrying the session it is speaking from instead of a literal, and
#   +1   the task lane handing it `current.childTerminalId`.
#   +9-1 `agentNotify` gaining an optional `sessionID` and the paragraph saying why the machine
#   +1-1 token cannot supply one, and the root lane passing it through.
#   +4-1 the scheduled-failure push asking `scheduleFailureSessionID` which session it may name.
#   +4   `Batch.sessionIDs`, +1 `noteEnded` recording each tab, and +10 in `announce` — the
#   +1   fallback to a task of the batch and the comment saying why that is not a guess.
#   +8+2
#   +1   `forget()` clearing the new seam.
# The encoding it all goes through, `WebPush.sessionURL`, is untouched: that half was already
# right, and this raise is every caller that had nothing to hand it.
# 10,747 is the landing sweep's hook, measured on this merged tree. Five lines, and all five are
# the schedule: `scheduleLandingSweep()` beside `sweepBatches()` in `beat(fromTimer:)`, with the
# comment saying why a pending landing outlives every live task that would otherwise keep that
# walk going. The 469-line feature itself is `Sources/OrchestratorLandingSweep.swift`, which is
# where a reader looking for it should end up — the ceiling moved by the call, not by the work.
#
# 10,826 is a settled landing answering honestly and an isolated child being answered about the
# repository it was cut from. Measured on this tree with `line_count` above, and counted per hunk,
# +97 -18:
#   +43-10 the landed/landed door becoming three answers instead of one. `replay` is the old early
#          return with its comment intact; `conflict` is the refusal a resend aimed at another
#          target now gets, naming both values; `correction` falls through to the verification the
#          first landing passed. Most of the 43 are the paragraph saying why durable is not the
#          same as unamendable, which is the decision this raise carries.
#   +6     `resolvedFields` — what the call is asking for in the spelling the record keeps, so the
#          race below reads the resolved commit rather than whatever text named it.
#   +2-1   the race branch's guard gaining "and it disagrees with what this call started from".
#   +1     its captured snapshot, +14-4 the reading it makes and the refusal it returns when
#   +5     another caller's record says something else, with the comment saying why that is the
#          same question as the door above and not a second policy.
#   +1-1   `landedAt` moving with the evidence, and standing still when only an annotation changed.
#   +13    the `corrected_from` reply field and the `orchestrator.landing.corrected` audit line,
#   +1-1   with the return carrying them.
#   +8-0   `inflightRepository` saying which repository a linked worktree belongs to, and
#   +3-1   resolving it: eight lines of that are the comment naming the three other callers that
#          take a directory from somebody who may be standing in a worktree, which is why the
#          reading moved here rather than into `inflightReply`. The resolution itself is
#          `OrchestratorDraft.mainWorktree(containing:)`, a new function in that file.
# 10,733 moves the read-only closeability projection and its history index into
# `Sources/CloseabilityIndex.swift`. The broker retains only mutation tracking, fingerprint
# settlement and the snapshot door, and no longer writes the same full registry twice per save;
# the 93-line reduction is measured on this tree, not headroom.
# Project Board adds eight broker call-site/metadata lines; domain and adapters
# live in separate owners. Measured 2026-09-08 on this candidate, without headroom.
# Board live-transition correction: replaceTask captures one credential-free source record and
# publishes it after unlocking. Registry state ownership remains here; Board projection stays out.
# Schedule Webhook adds one projection call; binding authority remains in ScheduleWebhook.swift.
# Cut 2 Stage 2 moves the four process-local rate windows behind Registry transactions. The
# 10,723-line ceiling is measured on this candidate; the shared lock and route facades remain.
# W1-3 moves handoff envelopes, handoff labels, Root Assignments and coordination waits behind the
# Registry's coordination-record capability. The transitions went to the owner and the multi-line
# door calls came back, so this file moved by one line while the owner gained the state machine:
# 10,721 is measured on this candidate, without headroom.
# W1-4 moves the task table behind the Registry's task-record capability and closes the held-lock
# doors. Converging the bare regions onto acquiring doors measured 10,822 mid-slice (+101); moving
# the serialize/claims/root-key queries and the terminal index (`reindex`) into the owner, where
# the brief put them, took 103 back, and restoring the one-line `records()` spelling the guard
# below pins took one more. 10,718 is measured on this candidate, without headroom.
# W2-1 closes the nine scheduling and six event-publication bare-lock regions the table below
# names, moving `handledScheduleFires`/`pendingScheduleFires`/`lastMissedScheduleFires`/
# `dispatchingSchedules`/`invalidScheduleFingerprints`/`beatsInFlight` to `ScheduleService` and
# `completionPumpScheduled`/`completionPumpGeneration`/`sessionActivityGenerations`/
# `sessionActivityClasses` to `OrchestratorEventPublisher`, both reached through the same shared
# `OrchestratorRegistry.lock`. Every schedule route, `beat`'s control flow and every audit line
# stay here, now calling the new owners instead of the state directly. Relocation, not a feature,
# so the ceiling falls: 10,661 is measured on this candidate, without headroom.
# 10,684 with W2-2, a raise and not a relocation, so it is named: +23 lines. The two child-session
# filesystem probes that ran inside the task door (the place-resume query and the root-close
# cascade) now take rows in one hold, probe outside it and revalidate the exact row in a second —
# that shape is longer than the one-hold version — plus the probe observer the red proof needs.
# The closeability counter and restart receipt moved out (-2). The next extractable boundary is the
# child-session identity query itself (`provenChildSessionID`, `availableScheduledSessionID`).
# 10,687 with the W3-1 correction (`spec-mac-does-not-consume-application`): +3 lines, a guarded
# `#if canImport(ClawdlineApplication) import ClawdlineApplication #endif` block so this file's
# `Assistant`/`TargetSession`/`Permission` references resolve to the real cross-module types
# instead of a duplicate compiled a second time into `Clawdline` — see `Sources/HostPorts.swift`.
# Not a relocation; the same three lines land in every one of the ~58 files this correction
# touched, and this file's ceiling has no headroom to absorb them silently.
orchestrator_ceiling=10687
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
#   5,759  the snippet corrections           (+33, measured: seven lines went out with the second
#                                          percent-decode on `?session=` and its now-wrong local,
#                                          and forty came in — the comment saying why that value
#                                          is used exactly as the parser handed it over, the
#                                          `republishing` envelope and its counter that put a
#                                          snippet write back on the snapshot the Cloud path
#                                          reads, and the four call sites that now go through it.
#                                          The store's own corrections — bytes rather than
#                                          grapheme clusters, a bound on `project` and on
#                                          `position`, symlinks resolved — are all in
#                                          `Sources/Snippets.swift`.)
#                                          5,786 once a browser could hand its diagnostic over.
#                                          Twenty-seven lines: one `case` for
#                                          `POST /v1/diagnostics/report` — the paired-device check,
#                                          the call into the store and the two audited answers —
#                                          and the comment saying why that route is at read level
#                                          rather than behind the write gate. The write itself, the
#                                          size limit and the rotation are in
#                                          `Sources/DiagnosticReport.swift`.
#                                          5,807 once a dispatch had to carry a receipt from the
#                                          worktree inventory. Twenty-one lines, measured: one
#                                          `case` for `GET /v1/orchestrator/inventory` with the
#                                          comment saying why it sits at read level beside
#                                          `inflight` and `landing-queue`, and four lines each on
#                                          `POST /v1/orchestrator/tasks` and
#                                          `POST /v1/orchestrator/detached-tasks` calling the
#                                          admission before dispatch. The three sections, the
#                                          digest, the `409 stale_inventory` refusal and the
#                                          reasons `handoffs` and `root-assignments` do not call it
#                                          are all in `Sources/OrchestratorInventory.swift`, a new
#                                          file, because a router is not a place to keep a ladder.
#                                          5,851 once a session could show a picture on its own
#                                          card. Forty-four lines, measured on the merged tree
#                                          rather than added to it: one route case for
#                                          `POST /v1/artifacts/images`, the five-line clause that
#                                          lets the machine credential be recognised on a path
#                                          that is deliberately not under `/v1/orchestrator/`, and
#                                          the four-line `answer(_:)` for an image-store refusal.
#                                          It is a smaller number than the route is, because the
#                                          twenty-six lines of `images` validation that used to
#                                          sit inline in the message route went out to
#                                          `SessionImageArtifactStore.paths(inImages:)` — one
#                                          reader for both routes — and the marker wire type and
#                                          its parsing are in `Sources/SessionImageMarker.swift`,
#                                          a new file, because a router is not a place to keep a
#                                          wire format.
#                                          5,834 once two routes could name a session. Twenty-seven
#                                          lines, measured per hunk (+31 -4): +22 -3 in
#                                          `POST /v1/push/test`, which now reads an optional
#                                          `session_id` and asks `Orchestrator.pushURL` for the
#                                          address — with the comment saying why only the address
#                                          moves and the words do not; +4 for the seam that lets a
#                                          test read what that route decided without a push service
#                                          on the other end; and +5 -1 in
#                                          `POST /v1/orchestrator/notify`, one argument and the
#                                          three lines saying why an unauthenticated `session_id`
#                                          is the only shape a machine token can offer. Line-neutral
#                                          was tried first and is not available: the route sent no
#                                          body through the parser at all, and `WebPush.send` takes
#                                          `url` before `tag`, so the value has to exist before the
#                                          call. The decision itself is in
#                                          `Sources/Orchestrator.swift`, because a router is not a
#                                          place to keep a rule about notifications.
#                                          **5,878 is those twenty-seven arriving on a `main` that
#                                          had meanwhile moved to 5,851, and it is measured on the
#                                          merged tree rather than added to it.** Both parents
#                                          wrote a number here and neither was wrong on its own
#                                          tree; the sum is not a measurement. `wc -l` on the
#                                          merged file says 5,878.
#                                          **5,898 with the verification ledger's registration**,
#                                          and twenty lines is the whole of what that route costs
#                                          this file: six for the `case` — parse, refuse, read,
#                                          answer — five for its comment saying why the body is
#                                          not here, six blank/closing, and three inside
#                                          `isUsageAnalyticsReading` putting it on the analytics
#                                          worker beside the two reads of the same store. The
#                                          handler itself is `Sources/VerificationLedgerRoute.swift`,
#                                          which is where a route that grows may grow. Measured:
#                                          `wc -l` on this tree says 5,898.
# Board auth, routing and bounded-read wiring add three net lines to HEAD's 5,882;
# take the previously unused headroom down to this candidate's measured size.
# Managed Board send now lives in ProjectBoardWorkflowHTTP; retain the measured wire-up size.
# Session peer-handoff address-book evidence and the verification-run router add fourteen lines;
# their bounded payload/protocol implementations remain outside this router. Measured on this tree.
# Combined Timeline/Board candidate on 9f115484: measured 5,831 lines. Timeline adds
# only bounded-lane route wiring; Cloud backpressure/resync remains intact.
# W2-1 moves the bounded terminal mutation lane's state and admission logic out into
# `Sources/TerminalCommandScheduler.swift`, leaving every route body and every legacy facade name
# (`enqueueTerminalCommand`, `terminalOutstandingForTesting`, `terminalDrainSnapshot`,
# `setRestartMaintenance`, `terminalMaintenanceRefusal`) as a thin delegating wrapper. A
# relocation, so the ceiling falls with it rather than being absorbed as headroom: 5,741 is
# measured on this candidate.
# 5,758 with W2-2's Project worktree lifecycle routes (+17): the connection lane branch (7), the
# verified-Cloud read branch (1) and the `startWorktreeRequest` adapter (9). The codec, auth and
# bounded lane are `Sources/ProjectWorktreeHTTP.swift`; the router keeps only the registration.
# 5,761 with the W3-1 correction (`spec-mac-does-not-consume-application`): +3 lines, the same
# guarded `import ClawdlineApplication` block described beside `orchestrator_ceiling` above.
remote_server_ceiling=5761
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
# 33 once the local-run status file got a suite of its own. It belongs beside the project-status
# group in `Tests/MarkdownTests.swift`, which stood at 1,950 lines against the 2,000-line limit
# below — the same wall the document route met, and the same answer: a file of its own rather
# than a group trimmed to fit under it.
# 34 once snippets arrived beside it: a store with its own file, its own bounds and its own scope
# rule gets its own runner too. **Both branches wrote 33 and neither was wrong on its own tree**,
# which is precisely why this number is counted on the merged one rather than carried over from
# whichever side merged second.
# 35 once the Projects page's read got a suite of its own — the same wall as the two above and the
# same answer. `Tests/UsageLedgerTests.swift` stood at 1,998 lines against the 2,000-line limit
# below, so the three groups that answer for one route moved out whole, in the position they
# already ran in, rather than being trimmed to fit under it.
# 36 with the landing-currency slice's runner, which is the same wall again from the other side:
# `Tests/UsageLedgerTests.swift` and `Tests/OrchestratorLandingTests.swift` were both within a
# hundred lines of the limit, and its two groups belong beside each other rather than split across
# two files with no room in either. That branch wrote 33 against a base of 32; this is the merged
# tree's own count.
# 37 with the session-image marker's runner. `Tests/TranscriptTests.swift` was at exactly 2,000
# lines against the stop-growth limit below — the same wall four of the runners above met, and the
# same answer — so the three groups for a session showing the user an image on its own card are a
# file of their own, called immediately after the transcript runner they read beside.
# 37 with the notification-address slice's runner — the same wall for the fifth time.
# `Tests/OrchestratorCoordinationTests.swift` was at 1,944 lines and its two new groups are 131,
# which is 2,075 against the 2,000 below. They moved out whole, into the position they already ran
# in, so nothing about the executed order changed with them.
# **38, and both parents said 37.** Each added one runner file — the session-image marker's and
# the notification-address slice's — and each was right about its own tree, so `git` had two
# identical-looking claims and no way to see that the merged tree has both. The number below is
# what the merged tree counts, not what either side agreed on.
# 39 with the verification ledger's runner. `Tests/UsageLedgerTests.swift` stood at 1,914 lines
# against the 2,000-line stop-growth limit below and these four groups are 280, which is 2,194 —
# the same wall six of the runners above met, and the same answer. It is called straight after
# `runUsagePortfolioAndLifecycleTests()`, which is where its groups run, so the executed order and
# `expectedOrderedTestGroupTitles` move together and nothing above it shifts.
# **The number is written once.** Both of the checks below used to carry it twice — once in the
# comparison and once, spelled out, in the sentence the guard says when the comparison fails — and
# the verification ledger's runner moved the first without the second. The guard stayed correct
# and its failure message started naming the count before this one, which is worse than no
# message: the next person to add a runner would have been told to write 38 back over the 39 that
# is right. A guard exists to tell somebody what to do, so the expected count and the sentence
# that reports it read the same variable.
# Project Board domain and integration each have a focused runner.
# The bounded Board narrative worker has its own injected-clock/process-boundary runner.
# Schedule Webhook has one cohesive binding/delivery runner beside scheduled dispatch.
# Personal Board Workflow has one cohesive durable ingress/outbox runner.
# Exact per-run verification receipts and Session work-state projection each have one cohesive
# runner, keeping both tests out of already frozen 2,000-line suites.
# Combined Timeline and workflow-presentation runners, measured from Tests/main.swift.
# 49 with W1-5's cohesive store-health/corruption runner, kept out of the frozen recovery suite.
# 50 with W2-1's owner-uniqueness runner: `ScheduleService`, `OrchestratorEventPublisher` and
# `TerminalCommandScheduler` each get proved directly rather than only through whatever paths the
# suites that already existed happened to exercise.
# 52 with W2-2: `runW2CommandAdmissionTests` (the owner seams) and `runProjectWorktreeLifecycleTests`
# (the lifecycle owner and its codec), two independent boundaries run after W2-1's owner suite.
# 53 with W2-3's `runHostPortsTests`: the safe-close lifecycle on fake host ports and the Mac
# composition held to the same decisions, a boundary independent of W2-2's two runners.
runner_count_expected=53
[ "$runner_count" -eq "$runner_count_expected" ] \
  || architecture_guard_fail "ordered domain runner count is $runner_count; expected $runner_count_expected"
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
# 533 with the local run's one: the seventh project status file, its staleness ceiling at both
# sides of the boundary, and the link row that carries no address. Counted from the manifest with
# the awk above rather than added to 532, which is the mistake three paragraphs up.
# 534 with the allow-list group beside it: an unrecognised state draws nothing for the deploy and
# health rows too, not only for the run row. It sits second in `ProjectRunTests.swift`, so it sits
# second in the manifest — the runtime check compares the two orders, not the two totals, and a
# name in the wrong place fails it exactly as a missing name does. Counted from the manifest with
# the awk above.
# 538 with the snippets branch's four beside them: the store's strictness and its UUID-only
# addressing, the scope rule that follows the mark and then the git common directory, the routes
# sharing the write gate with their typed refusals, and the snapshot carrying a list a session
# read is already filtered from. One branch wrote 536 and the other 534, from the same 532; the
# number here is the awk's answer on the merged manifest, which is the only tree that has both.
# 542 is this merged tree counted with the awk above, not one side plus the other's increment.
# Three branches arrived carrying 539, 540 and 541 tonight and every one of them was right about
# the tree it was measured on. Two groups are the deep link's and the blindness probe's; the rest
# came in with the deliveries this merge carries. Counted, never added.

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
# 46 with Tests/ProjectRunTests.swift; see the runner-count note above for why it is its own file.
# 47 with Tests/SnippetStoreTests.swift, which arrived on another branch with the store it proves.
# Two branches, one number each, both 46: counted here on the tree that holds both files.
# 48 with Tests/UsageProjectWorktreeTests.swift, which is not a new area: it is the three Projects
# groups moved out of Tests/UsageLedgerTests.swift when that file hit the 2,000-line stop-growth
# limit above. The move keeps the executed group order — the new runner is called between the two
# it was cut from — so `expectedOrderedTestGroupTitles` is untouched by it.
# 49 with Tests/LandingCurrencyTests.swift, the 2026-09-05 branch's file. It wrote 46 against a
# base of 45; this is the merged tree's own count, and the two files that branch never saw are the
# difference.
# 50 with Tests/SessionImageMarkerTests.swift; see the runner-count note above for why the marker's
# three groups are their own file rather than three more in a suite already at the limit.
# 50 with Tests/NotificationAddressTests.swift, and it is not a new area either: it is the two
# notification-address groups that would have taken
# Tests/OrchestratorCoordinationTests.swift to 2,075 against the 2,000 above. Its runner is called
# straight after that file's, and the groups run where they were written to run, so
# `expectedOrderedTestGroupTitles` does not move for it.
# **51, for the same reason and by the same arithmetic as the runner count above**: both parents
# added a suite file and both still said 50. Measured on the merged tree.
# 52 with Tests/VerificationLedgerTests.swift; see the runner-count note above for why the four
# groups that answer for one route are their own file rather than four more in a suite eighty-six
# lines from the limit.
# 56 with Tests/ScheduleWebhookTests.swift, one cohesive durable binding/delivery suite beside
# scheduled dispatch. This is feature coverage, not a split made only to move the guard.
# 58 with Tests/VerificationRunLedgerTests.swift, the append-only per-run receipt boundary.
# 59 with Tests/SessionWorkStateTests.swift, the bounded peer-handoff projection boundary.
# One owner for the number, for the reason written above the runner count.
# Combined Timeline and workflow-presentation suite files, measured from Tests/ inventory.
# 62 with W1-5's store-health/corruption suite.
# 63 with Tests/W2ApplicationOwnershipTests.swift, W2-1's owner-uniqueness suite.
# 66 with Tests/CloudCommandRefusalTests.swift closing W2-2's typed Cloud refusal proof.
# 67 with Tests/HostPortsTests.swift, W2-3's fake host-port lifecycle suite.
# 68 with Tests/CloudTransparencyTests.swift, the Cloud error-transparency failure-injection suite;
# it has its own file because Tests/CloudAppBridgeTests.swift is nine lines from the limit above.
suite_count_expected=68
[ "$suite_count" -eq "$suite_count_expected" ] \
  || architecture_guard_fail "suite file count is $suite_count; expected $suite_count_expected"
# The registry's held-lock doors are closed. `withTransactionOnHeldLock` and its two adapters,
# `withSessionRecordsOnHeldLock` and `withCoordinationRecordsOnHeldLock`, ran a body without
# acquiring the lock and trusted the caller to hold it — exactly the contract the …Locked() suffix
# carried. They were ratcheted at 12, 48 and 26 call sites and reached zero together in W1-4, when
# the task table moved into OrchestratorRegistry and every region converged on an acquiring door
# (a region atomic across families takes `withTaskRecords` and reaches the narrower capabilities
# from that one hold). The ratchets went with the doors. What replaces them is a zero that stays
# zero: no Swift code may declare or call a door whose name ends in `OnHeldLock`. Comments may
# still name the history, which is why comment lines are excluded.
#
# The scan is calibrated before its zero is believed: the same code-only scan must find the
# acquiring task door, which this tree calls in two figures and more. A pattern that stopped
# recognising `withTaskRecords {` would equally stop recognising `withTransactionOnHeldLock {`,
# and its clean zero would be a statement about the pattern.
held_lock_door_sites=$(cat Sources/*.swift Tests/*.swift | grep -vE '^[[:space:]]*(//|/\*|\*)' \
  | grep -cE 'OnHeldLock[[:space:]]*[<({]' || true)
task_door_control=$(cat Sources/*.swift | grep -vE '^[[:space:]]*(//|/\*|\*)' \
  | grep -cE 'withTaskRecords[[:space:]]*[<({]' || true)
[ "${task_door_control:-0}" -gt 50 ] \
  || architecture_guard_fail "the held-lock door scan found only ${task_door_control:-0} acquiring withTaskRecords sites; it has stopped recognising how this tree enters the registry, so its zero for OnHeldLock doors means nothing"
[ "${held_lock_door_sites:-0}" -eq 0 ] \
  || architecture_guard_fail "a door ending in OnHeldLock is declared or called at ${held_lock_door_sites} code site(s); W1-4 closed every held-lock door — acquire through an OrchestratorRegistry door and pass its capability to helpers instead"

# W1-4's gate says the direct door reaches zero. The door it means is the held-lock door checked
# just above — a body run on the caller's word that it holds the lock — and that is zero. The bare
# `lock.lock()` regions counted next are not a door onto the registry, and this comment used to call
# them one: none reaches a Registry collection, because every collection is `private` to
# OrchestratorRegistry.swift (the compiler enforces it; the task-table check below pins it). Each
# guards state `Orchestrator` still declares itself under the shared lock. They are residual
# owner-lock regions, so the W1 gate ("zero externally held owner locks") is not met, and
# docs/architecture-refactor.md names every region's owner and next boundary (W1-5, W2-1 or W2-2).
# W1-3 took the count from 160 to 123 across the three files that took the registry lock directly
# (Orchestrator 153 -> 116, Planning 4, SessionLanding 3); W1-4 took it to 21, all in
# Orchestrator.swift. By this measurement the count had already fallen to 17 before W2-1 (a prior
# delivery's own reduction this history was not updated for). W2-1 closes the nine scheduling and
# six event-publication regions the docs/architecture-refactor.md table names for it, taking bare
# `lock.lock()` in these three files from 17 to 2 — both remaining sites were
# `closeabilityRegistryReadCountForTesting`, explicitly W2-2's. W2-2 moved that counter into
# OrchestratorRegistry behind task-door transitions, so the ratchet reached zero and became what the
# held-lock check above already is: a zero that stays zero. Its control is the acquiring task door,
# which these files call in two figures; a pattern that stopped recognising `lock.lock()` would
# equally stop recognising `withTaskRecords {`, and its clean zero would be a statement about itself.
direct_registry_lock_sites=$(cat Sources/Orchestrator.swift Sources/OrchestratorPlanning.swift Sources/OrchestratorSessionLanding.swift \
  | grep -vE '^[[:space:]]*(//|/\*|\*)' | grep -c 'lock\.lock()' || true)
direct_lock_control=$(cat Sources/Orchestrator.swift | grep -vE '^[[:space:]]*(//|/\*|\*)' \
  | grep -cE 'withTaskRecords[[:space:]]*[<({]' || true)
[ "${direct_lock_control:-0}" -gt 20 ] \
  || architecture_guard_fail "the direct-lock scan's control found only ${direct_lock_control:-0} withTaskRecords sites in Sources/Orchestrator.swift; it no longer recognises how that file enters the registry"
[ "$direct_registry_lock_sites" -eq 0 ] \
  || architecture_guard_fail "the files that took the registry lock directly have $direct_registry_lock_sites bare lock.lock() site(s); W2-2 closed the last one — reach registry state through an OrchestratorRegistry door"

# The same zero for Coordinator.swift's `extension Orchestrator`, which held nine bare regions for the
# restart-maintenance receipt until W2-2 moved the receipt behind
# `OrchestratorRegistry.withRestartRecords` and the task door's `restartRecords` capability. Counted
# from that extension's first line, because the `Coordinator` store earlier in the file takes a lock
# of its own under the same spelling; its control is that store's own lock, which must still be seen.
coordinator_extension_line=$(grep -n '^extension Orchestrator {' Sources/Coordinator.swift | head -1 | cut -d: -f1)
[ -n "$coordinator_extension_line" ] \
  || architecture_guard_fail "Sources/Coordinator.swift has no 'extension Orchestrator {' line, so the restart-maintenance lock count cannot tell Orchestrator's lock from the Coordinator store's"
coordinator_registry_lock_sites=$(tail -n "+$coordinator_extension_line" Sources/Coordinator.swift \
  | grep -vE '^[[:space:]]*(//|/\*|\*)' | grep -c 'lock\.lock()' || true)
coordinator_store_lock_control=$(head -n "$coordinator_extension_line" Sources/Coordinator.swift \
  | grep -vE '^[[:space:]]*(//|/\*|\*)' | grep -c 'lock\.lock()' || true)
[ "${coordinator_store_lock_control:-0}" -gt 0 ] \
  || architecture_guard_fail "the Coordinator store's own lock was not found above the extension; the scan cannot tell a moved receipt from a pattern that stopped matching"
[ "$coordinator_registry_lock_sites" -eq 0 ] \
  || architecture_guard_fail "Coordinator.swift's extension Orchestrator has $coordinator_registry_lock_sites bare lock.lock() site(s); the restart receipt is OrchestratorRegistry's — use withRestartRecords or the task door's restartRecords"

# The task-collection door, stated so this script checks it instead of a sentence promising it:
# (1) the table is declared once, `private` to OrchestratorRegistry.swift, so no row is reached
# except through `withTaskRecords` — the held-lock-door zero above is the whole of the other way in;
# (2) the capability can be made only in that file; (3) there is no generic upsert — admission
# inserts, and the one fixture upsert, `seedTaskForTesting`, has one production-side caller, the
# `holdScheduleTaskForTesting` seam; (4) whole-table replacement is `load()`'s alone. Each expected
# count includes the declaration it names, so a rename reads as a failure here, not a clean zero.
task_door_code_lines() { cat "$@" | grep -vE '^[[:space:]]*(//|/\*|\*)'; }
task_table_declarations=$(task_door_code_lines Sources/*.swift | grep -cE 'static var tasks[[:space:]]*:' || true)
task_table_private=$(grep -cE '^[[:space:]]*private static var tasks: \[String: Orchestrator\.Task\]' Sources/OrchestratorRegistry.swift || true)
{ [ "$task_table_declarations" -eq 1 ] && [ "$task_table_private" -eq 1 ]; } \
  || architecture_guard_fail "the task table must be declared exactly once, private to OrchestratorRegistry.swift (static tasks declarations=$task_table_declarations, private in the registry=$task_table_private)"
task_capability_private_init=$(grep -A1 -E '^[[:space:]]*struct TaskRecordsTransaction \{' Sources/OrchestratorRegistry.swift \
  | grep -cE '^[[:space:]]*fileprivate init\(\) \{\}' || true)
[ "$task_capability_private_init" -eq 1 ] \
  || architecture_guard_fail "TaskRecordsTransaction must open with a fileprivate init(), so only a door in OrchestratorRegistry.swift can make one"
generic_task_upserts=$(task_door_code_lines Sources/*.swift Tests/*.swift | grep -cE '(^|[^A-Za-z0-9_])recordTask\(' || true)
task_seed_sites=$(task_door_code_lines Sources/*.swift | grep -cE '(^|[^A-Za-z0-9_])seedTaskForTesting\(' || true)
task_table_replacements=$(task_door_code_lines Sources/*.swift | grep -cE '(^|[^A-Za-z0-9_])replaceAllTasks\(' || true)
restart_load_sites=$(task_door_code_lines Sources/*.swift | grep -cE '(^|[^A-Za-z0-9_])replaceForLoad\(' || true)
restart_forget_sites=$(task_door_code_lines Sources/*.swift | grep -cE '(^|[^A-Za-z0-9_])removeForForget\(' || true)
restart_fixture_sites=$(task_door_code_lines Sources/*.swift Tests/*.swift | grep -cE '(^|[^A-Za-z0-9_])installForTesting\(' || true)
[ "$generic_task_upserts" -eq 0 ] \
  || architecture_guard_fail "recordTask( is back at $generic_task_upserts code site(s); a production row is created by admitTask and nothing else"
[ "$task_seed_sites" -eq 2 ] \
  || architecture_guard_fail "seedTaskForTesting( appears at $task_seed_sites Sources code site(s), expected 2 (its declaration and holdScheduleTaskForTesting); production creates rows through admitTask"
[ "$task_table_replacements" -eq 2 ] \
  || architecture_guard_fail "replaceAllTasks( appears at $task_table_replacements Sources code site(s), expected 2 (its declaration and load()); a rollback restores its own fields through a named transition"
[ "$restart_load_sites" -eq 2 ] \
  || architecture_guard_fail "replaceForLoad( appears at $restart_load_sites Sources code site(s), expected its declaration plus load() only"
[ "$restart_forget_sites" -eq 2 ] \
  || architecture_guard_fail "removeForForget( appears at $restart_forget_sites Sources code site(s), expected its declaration plus forget() only"
[ "$restart_fixture_sites" -eq 6 ] \
  || architecture_guard_fail "installForTesting( appears at $restart_fixture_sites code site(s), expected its declaration plus five test fixtures only"

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
lock_acquisition_re='(^|[^A-Za-z0-9_])(lock\.lock\(\)|Orchestrator\.lock|with(Transaction|SessionRecords|CoordinationRecords|TaskRecords|RestartRecords)(OnHeldLock)?[[:space:]]*[({])'
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
# five — it counts three of them and owns two as ratchets — so it
# renders the block itself and compares the committed one against that rendering. Nobody types a
# governance number into the doc any more; `tools/generate-governance-table.sh` writes what
# `--emit-governance-table` prints. Every row is now a value against its own rendering, which is a
# comparison that cannot be satisfied by two people making the same mistake twice.
# W2-3's host boundary, checked rather than promised. `Sources/HostPorts.swift` is the application
# side — the port contracts and the safe-close lifecycle that runs only on them — so it imports
# Foundation and nothing else, and none of its code names a platform effect: iTerm2, tmux, the Mac
# facade, the pasteboard or workspace, the Keychain, a file manager, a subprocess, a signal, a sleep
# or the wall clock. Those spellings belong in `Sources/MacHostAdapters.swift`, and the scan is
# calibrated against that file before its zero for the ports is believed: a pattern that stopped
# matching would find nothing in either file, and the control is what refuses that.
#
# An enum case declaration such as `case kill(pid_t)` names a step, not a call, so declarations are
# skipped; `case .iterm: return ITerm.close(…)` still starts with a dot and is still counted.
#
# The second half pins the migration itself. `Targets.end`, `closeIfAssistantGone` and
# `waitToBeGoneForTesting` delegate to `TerminalSafeClose` on `HostPorts.mac`; the exact-tty
# observation, the signal and the backend close they used to perform inline must not come back to
# `Sources/Targets.swift`, where they would bypass the ports the fake-lifecycle tests prove.
host_ports_file=Sources/HostPorts.swift
mac_host_adapters_file=Sources/MacHostAdapters.swift
{ [ -f "$host_ports_file" ] && [ -f "$mac_host_adapters_file" ]; } \
  || architecture_guard_fail "$host_ports_file or $mac_host_adapters_file is missing; the host boundary cannot be checked in a file that is not there"
host_ports_imports=$(grep -E '^[[:space:]]*(@[A-Za-z_]+[[:space:]]+)*import[[:space:]]' "$host_ports_file" \
  | sed -E 's/^[[:space:]]*(@[A-Za-z_]+[[:space:]]+)*import[[:space:]]+//; s/[[:space:]]+$//' \
  | sort -u | tr '\n' ' ')
# W3-1: this file is now also the sole member of the real ClawdlineApplication SwiftPM target, so
# it carries one more import — ClawdlineCore, where Assistant/Assistant.Running now live — but
# only behind #if canImport(ClawdlineCore), never unconditionally: ./test.sh's compatibility flat,
# single-module swiftc invocation has no ClawdlineCore module to resolve an unconditional import
# against. The line below still refuses any import besides these exact two.
[ "$host_ports_imports" = "ClawdlineCore Foundation " ] \
  || architecture_guard_fail "$host_ports_file imports '${host_ports_imports% }'; the application side of the host boundary imports Foundation unconditionally and ClawdlineCore only behind #if canImport(ClawdlineCore) (W3-1) — put any other platform dependency in $mac_host_adapters_file"
# W3-1 correction (`spec-mac-does-not-consume-application`): the guarded import is now
# `@_exported import ClawdlineCore`, not a plain `import ClawdlineCore` — `@_exported` is what
# lets every one of Clawdline's ~58 real consumers reach `Assistant`/`Permission`/`ReasoningEffort`
# through one `import ClawdlineApplication` instead of each also learning it needs `ClawdlineCore`
# by name. The awk pattern accepts an optional attribute prefix for the same reason
# `host_ports_imports` above already strips one; it still refuses anything but exactly that one
# guarded line, still immediately after the `#if`.
host_ports_guarded_core_import=$(awk '
  /^#if canImport\(ClawdlineCore\)$/ { guard = 1; next }
  guard && /^(@[A-Za-z_]+[[:space:]]+)*import ClawdlineCore$/ { print; guard = 0; next }
  { guard = 0 }
' "$host_ports_file")
[ -n "$host_ports_guarded_core_import" ] \
  || architecture_guard_fail "$host_ports_file's import ClawdlineCore is not the line immediately after #if canImport(ClawdlineCore) — an unconditional cross-module import here would break ./test.sh's compatibility flat compile, which has no ClawdlineCore module to resolve it against"
host_code_lines() {
  grep -vE '^[[:space:]]*(//|/\*|\*)' "$1" | grep -vE '^[[:space:]]*case[[:space:]]+[A-Za-z_][A-Za-z0-9_]*\(' || true
}
host_effect_re='(^|[^A-Za-z0-9_])(ITerm|Tmux|Targets|Thread)\.|(^|[^A-Za-z0-9_.])(kill|usleep|shell|osa|Process)\(|(^|[^A-Za-z0-9_.])Date\(\)|(^|[^A-Za-z0-9_])(NSPasteboard|NSWorkspace|FileManager|CloudKeychainStore|SecItem[A-Za-z]*)([^A-Za-z0-9_]|$)'
host_adapter_effect_lines=$(host_code_lines "$mac_host_adapters_file" | grep -cE "$host_effect_re" || true)
[ "${host_adapter_effect_lines:-0}" -ge 10 ] \
  || architecture_guard_fail "the host-effect scan found only ${host_adapter_effect_lines:-0} effect line(s) in $mac_host_adapters_file, which is where they all live; the pattern has stopped recognising them, so its zero for $host_ports_file would mean nothing"
host_port_effect_lines=$(host_code_lines "$host_ports_file" | grep -cE "$host_effect_re" || true)
[ "${host_port_effect_lines:-0}" -eq 0 ] \
  || architecture_guard_fail "$host_ports_file names a platform effect on ${host_port_effect_lines} code line(s); the ports and the lifecycle on them reach the host only through an injected port — move the effect into $mac_host_adapters_file"
safe_close_effect_re='ITerm\.assistantObservation\(|ITerm\.close\(|Tmux\.close\(|(^|[^A-Za-z0-9_.])kill\('
safe_close_adapter_lines=$(host_code_lines "$mac_host_adapters_file" | grep -cE "$safe_close_effect_re" || true)
[ "${safe_close_adapter_lines:-0}" -ge 4 ] \
  || architecture_guard_fail "the safe-close effect scan found ${safe_close_adapter_lines:-0} of the four Mac leaves (exact-tty observation, kill, iTerm2 close, tmux close) in $mac_host_adapters_file; it no longer recognises them, so its zero for Sources/Targets.swift would mean nothing"
safe_close_facade_effects=$(host_code_lines Sources/Targets.swift | grep -cE "$safe_close_effect_re" || true)
[ "${safe_close_facade_effects:-0}" -eq 0 ] \
  || architecture_guard_fail "Sources/Targets.swift performs a safe-close effect inline on ${safe_close_facade_effects} code line(s); the lifecycle is TerminalSafeClose on HostPorts.mac, and an inline observation, signal or close bypasses the ports its tests prove"
safe_close_delegations=$(host_code_lines Sources/Targets.swift \
  | grep -cE 'TerminalSafeClose\.(end|closeIfAssistantGone|waitToBeGone)\(' || true)
[ "${safe_close_delegations:-0}" -eq 3 ] \
  || architecture_guard_fail "Sources/Targets.swift delegates to TerminalSafeClose at ${safe_close_delegations:-0} site(s), expected 3 (end, closeIfAssistantGone, waitToBeGoneForTesting)"

# The remaining W2-3 terminal crossings are creation and raw answer/interrupt bytes. Admission,
# menu parsing and refusal policy stay in StartPoints/Targets, but an admitted platform effect
# must cross TerminalHost. Calibrate the spellings against the Mac leaf before trusting a zero in
# application/facade code, then pin the three real delegations (two create branches, one answer
# byte channel) so a future direct backend call cannot quietly reopen the boundary.
terminal_migration_effect_re='ITerm\.newTabResult\(|Tmux\.new(Window|Session)Result\(|ITerm\.keystroke\(|Tmux\.keystroke\('
terminal_migration_adapter_lines=$(host_code_lines "$mac_host_adapters_file" | grep -cE "$terminal_migration_effect_re" || true)
[ "${terminal_migration_adapter_lines:-0}" -ge 5 ] \
  || architecture_guard_fail "the terminal create/interrupt effect scan found ${terminal_migration_adapter_lines:-0} Mac leaf line(s), expected at least 5; its zero outside the adapter would not be credible"
terminal_migration_facade_effects=$(cat Sources/StartPoints.swift Sources/Targets.swift \
  | host_code_lines /dev/stdin | grep -cE "$terminal_migration_effect_re" || true)
[ "${terminal_migration_facade_effects:-0}" -eq 0 ] \
  || architecture_guard_fail "StartPoints/Targets perform ${terminal_migration_facade_effects} terminal create/interrupt effect(s) inline; preserve policy there but delegate admitted effects through TerminalHost"
terminal_migration_delegations=$(cat Sources/StartPoints.swift Sources/Targets.swift \
  | grep -cE 'HostPorts\.mac\.terminal\.(create|interrupt)\(' || true)
[ "${terminal_migration_delegations:-0}" -eq 3 ] \
  || architecture_guard_fail "StartPoints/Targets delegate create/interrupt through TerminalHost at ${terminal_migration_delegations:-0} site(s), expected 3"

# W2-3 correction, F2: the block above hard-codes one file. That was real coverage for
# Sources/HostPorts.swift and no ratchet at all for whatever Core/Application candidate is added
# next — nothing here stopped a reintroduced AppKit/Security/ServiceManagement/Speech/AVFoundation/
# Carbon import in a new candidate, because nothing knew it was supposed to be one. The fix is a
# checked-in list this guard walks, with a floor that can only be raised: removing or emptying the
# manifest is exactly as unguarded as never having it, so both fail closed here rather than
# quietly measuring zero files and calling that clean.
#
# This is a lexical ratchet over import statements and the same platform-effect spellings
# Sources/HostPorts.swift is already held to above — it proves nothing about Linux, and does not
# claim to. A real Ubuntu compile receipt is W3's, from a genuine second target or CI, not from
# this script reading source text.
#
# W3-1 raised the floor from 1 to 4: Sources/CloudCanonicalJSON.swift and Sources/CloudClock.swift
# (already proven to compile standalone on Ubuntu 24.04 by tools/ubuntu-core-probe.sh) and
# Sources/Assistant.swift (extracted clean of its one platform effect into
# Sources/AssistantInstallation.swift in the same delivery) joined Sources/HostPorts.swift as real
# members of the ClawdlineCore/ClawdlineApplication SwiftPM targets below, not only lexical
# candidates.
core_candidates_file=tools/core-application-candidates.txt
core_candidate_floor=4
[ -f "$core_candidates_file" ] \
  || architecture_guard_fail "$core_candidates_file is missing; the Core/Application candidate manifest must be checked in for this guard to protect anything"
core_candidate_count=$(grep -vcE '^[[:space:]]*(#|$)' "$core_candidates_file" || true)
core_candidate_count=${core_candidate_count:-0}
[ "$core_candidate_count" -ge "$core_candidate_floor" ] \
  || architecture_guard_fail "$core_candidates_file lists $core_candidate_count candidate(s), below the checked-in floor of $core_candidate_floor; this manifest may only grow — restore the missing entries, or raise the floor in the same change that removes one and say why"
swift_import_modules() {
  python3 - "$@" <<'PY'
import re, sys
pattern = re.compile(
    r'^\s*(?:@[A-Za-z_][A-Za-z0-9_]*(?:\([^)]*\))?\s+)*import\s+'
    r'(?:(?:typealias|struct|class|enum|protocol|let|var|func)\s+)?'
    r'([A-Za-z_][A-Za-z0-9_]*)'
)
block_depth = 0
for path in sys.argv[1:]:
    text = open(path, encoding="utf-8").read()
    code, i = [], 0
    while i < len(text):
        pair = text[i:i + 2]
        if block_depth:
            if pair == "/*": block_depth += 1; i += 2; continue
            if pair == "*/": block_depth -= 1; i += 2; continue
            if text[i] == "\n": code.append("\n")
            i += 1
            continue
        if pair == "/*": block_depth = 1; i += 2; continue
        if pair == "//":
            newline = text.find("\n", i + 2)
            if newline < 0: break
            code.append("\n"); i = newline + 1; continue
        code.append(text[i]); i += 1
    for statement in "".join(code).replace(";", "\n").splitlines():
        match = pattern.match(statement)
        if match: print(match.group(1))
PY
}

swift_code_without_comments() {
  python3 - "$1" <<'PY'
import sys
text = open(sys.argv[1], encoding="utf-8").read()
code, depth, i = [], 0, 0
while i < len(text):
    pair = text[i:i + 2]
    if depth:
        if pair == "/*": depth += 1; i += 2; continue
        if pair == "*/": depth -= 1; i += 2; continue
        if text[i] == "\n": code.append("\n")
        i += 1; continue
    if pair == "/*": depth = 1; i += 2; continue
    if pair == "//":
        newline = text.find("\n", i + 2)
        if newline < 0: break
        code.append("\n"); i = newline + 1; continue
    code.append(text[i]); i += 1
sys.stdout.write("".join(code))
PY
}

core_forbidden_imports='AppKit|Security|ServiceManagement|Speech|AVFoundation|Carbon|Cocoa|Network|CoreServices|Darwin|IOKit|ObjectiveC'
while IFS= read -r candidate; do
  [ -n "$candidate" ] || continue
  [ -f "$candidate" ] \
    || architecture_guard_fail "$core_candidates_file names $candidate, which does not exist in this tree"
  candidate_imports=$(swift_import_modules "$candidate" || true)
  forbidden_hit=$(printf '%s\n' "$candidate_imports" | grep -E "^($core_forbidden_imports)\$" || true)
  [ -z "$forbidden_hit" ] \
    || architecture_guard_fail "$candidate imports ${forbidden_hit//$'\n'/, }, which the Core/Application candidate manifest forbids; move the platform dependency to a Mac leaf in $mac_host_adapters_file"
  candidate_effect_lines=$(host_code_lines "$candidate" | grep -cE "$host_effect_re" || true)
  [ "${candidate_effect_lines:-0}" -eq 0 ] \
    || architecture_guard_fail "$candidate names a platform effect on ${candidate_effect_lines} code line(s); a Core/Application candidate reaches the host only through an injected port"
done < <(grep -vE '^[[:space:]]*(#|$)' "$core_candidates_file" || true)

# W3-1 correction (`repo-graph-guard-is-not-exact`), extended by W3-2: the real SwiftPM
# Core/Application/Mac/Linux
# target graph, checked against SwiftPM's own resolved manifest instead of reimplemented with
# `find -maxdepth 1`. The original version of this block counted first-level `.swift` files
# against a floor and asked only whether `Clawdline` "has" the `ClawdlineApplication` dependency —
# both are gameable: a maxdepth-1 count cannot see a `.swift` file added in a nested subdirectory,
# which SwiftPM's implicit recursive target scan compiles anyway; a same-count member swap (drop
# one symlink, add a different candidate) still satisfies a floor; and "has Application" says
# nothing about an *extra* edge such as `Clawdline -> ClawdlineCore` added beside it.
# `swift package describe --type json` does not compile anything — it only evaluates the
# manifest — so it is cheap enough to run outside the compile lock like every other check in this
# script, and its `sources`/`target_dependencies` fields are SwiftPM's own recursively-resolved
# compile set and dependency edges: exactly what a nested, extra, replaced or moved source, or a
# direct/extra Mac edge, would change.
core_application_packages_root=Packages
application_policy_sources='Sources/ProjectRootPolicy.swift
Sources/ProviderLifecyclePolicy.swift
Sources/SessionLaunchPolicy.swift
Sources/TerminalCommandScheduler.swift
Sources/CloudCommandLedger.swift
Sources/CloudOutboundSpool.swift
Sources/CloudPairing.swift'
# W5-1's two Application state machines reach persistence only through their store protocols.
# This one source is the deliberately shared POSIX host leaf behind those protocols; Mac and
# Linux compile the same descriptor/owner/link/fsync implementation instead of drifting copies.
application_host_leaf_sources='Sources/CloudDurableStores.swift
Sources/CloudAccount.swift
Sources/CloudKeys.swift
Sources/CloudTransport.swift
Sources/CloudEnvelope.swift
Sources/CloudAppBridge.swift'
while IFS= read -r candidate; do
  [ -n "$candidate" ] || continue
  [ -f "$candidate" ] \
    || architecture_guard_fail "W4-1 Application policy source $candidate is missing"
  candidate_imports=$(swift_import_modules "$candidate" || true)
  forbidden_hit=$(printf '%s\n' "$candidate_imports" | grep -E "^($core_forbidden_imports)\$" || true)
  [ -z "$forbidden_hit" ] \
    || architecture_guard_fail "$candidate imports ${forbidden_hit//$'\n'/, }, which an Application policy source may not import"
  candidate_effect_lines=$(host_code_lines "$candidate" | grep -cE "$host_effect_re" || true)
  [ "${candidate_effect_lines:-0}" -eq 0 ] \
    || architecture_guard_fail "$candidate names a platform effect on ${candidate_effect_lines} code line(s); Application policy reaches the host only through an injected port"
done <<< "$application_policy_sources"
core_application_package_graph=$(swift package --disable-sandbox describe --type json 2>/dev/null) \
  || architecture_guard_fail "swift package describe failed; the real Core/Application/Mac/Linux target graph cannot be checked"
. tools/swift-source-manifest.sh

# Every symlink under Packages/ClawdlineCore/ and Packages/ClawdlineApplication/ must still be a
# real, correctly-named, candidate-listed link into Sources/ — the "one source of truth"
# guarantee from Packages/README.md. This is a different invariant from "which files are
# members" (checked below against SwiftPM's own resolution), so it stays its own filesystem scan.
for package_target in ClawdlineCore ClawdlineApplication; do
  package_dir="$core_application_packages_root/$package_target"
  [ -d "$package_dir" ] \
    || architecture_guard_fail "$package_dir is missing; the real $package_target SwiftPM target has no sources"
  package_target_member_count=0
  while IFS= read -r member; do
    [ -n "$member" ] || continue
    package_target_member_count=$((package_target_member_count + 1))
    [ -L "$member" ] \
      || architecture_guard_fail "$member is a real file, not a symlink; every $core_application_packages_root member must be a symlink into Sources/ so the bytes have one source of truth (Packages/README.md) — a copy here is exactly the fork this boundary forbids"
    link_target=$(readlink "$member") \
      || architecture_guard_fail "$member is a symlink readlink could not resolve"
    case "$link_target" in
      ../../Sources/*.swift) ;;
      *) architecture_guard_fail "$member points at '$link_target', not a ../../Sources/*.swift path — repointing this symlink away from Sources/ breaks the one-source-of-truth guarantee" ;;
    esac
    resolved_basename=$(basename "$link_target")
    [ "$resolved_basename" = "$(basename "$member")" ] \
      || architecture_guard_fail "$member's filename does not match its symlink target's basename ($resolved_basename); a real package member must be named after the source file it mirrors"
    [ -f "$member" ] \
      || architecture_guard_fail "$member's symlink target does not resolve to a regular file; it is dangling"
    if ! grep -qxF "Sources/$resolved_basename" "$core_candidates_file" \
        && ! printf '%s\n%s\n' "$application_policy_sources" "$application_host_leaf_sources" \
          | grep -qxF "Sources/$resolved_basename"; then
      architecture_guard_fail "$member mirrors Sources/$resolved_basename, which is neither in $core_candidates_file nor W4-1's exact Application policy set"
    fi
  done < <(find "$package_dir" -maxdepth 1 -name '*.swift' | LC_ALL=C sort)
  [ "$package_target_member_count" -gt 0 ] \
    || architecture_guard_fail "$package_dir has no .swift members; an empty target is not the boundary this checks"
done

# The exact expected membership of each real target, and of the Mac target's dependency set — not
# a floor or a contains-check, a pinned set. This is what W3-1 shipped (`Packages/README.md`):
# three files in ClawdlineCore, fourteen in ClawdlineApplication, and exactly one Mac-to-Application
# edge. Growing real membership stays possible and stays deliberate — `core-application-candidates.txt`
# above may only grow on its own — but *this* pinned list may only be edited in the same change
# that adds the matching symlink(s), never as a side effect of something else moving a file
# around.
core_expected_members='Assistant.swift
CloudCanonicalJSON.swift
CloudClock.swift'
application_expected_members='HostPorts.swift
ProjectRootPolicy.swift
ProviderLifecyclePolicy.swift
SessionLaunchPolicy.swift
TerminalCommandScheduler.swift
CloudCommandLedger.swift
CloudOutboundSpool.swift
CloudDurableStores.swift
CloudAccount.swift
CloudKeys.swift
CloudPairing.swift
CloudTransport.swift
CloudEnvelope.swift
CloudAppBridge.swift'
mac_expected_dependencies='ClawdlineApplication'
linux_expected_members='LinuxComposition.swift
LinuxContainedFileSystem.swift
LinuxDaemonIngress.swift
LinuxDaemonLifecycle.swift
LinuxDocumentReader.swift
LinuxLocalIngressServer.swift
LinuxProviderRuntime.swift
LinuxRuntimeAdapters.swift
LinuxSHA256.swift
LinuxDurableCloudRuntime.swift
main.swift'
linux_expected_dependencies='ClawdlineApplication'
linux_tests_expected_members='LinuxRuntimeContractTests.swift'
linux_tests_expected_dependencies='ClawdlineApplication
ClawdlineLinux'

core_application_exactness=$(printf '%s' "$core_application_package_graph" | python3 -c '
import json, sys
data = json.load(sys.stdin)
targets = {t["name"]: t for t in data.get("targets", [])}
products = {p["name"]: p for p in data.get("products", [])}
for name in ("ClawdlineCore", "ClawdlineApplication", "Clawdline", "ClawdlineLinux", "ClawdlineLinuxTests"):
    if name not in targets:
        print("missing:" + name)
        sys.exit(0)
def sources(name):
    return sorted(targets[name].get("sources") or [])
def deps(name):
    return sorted(targets[name].get("target_dependencies") or [])
print("package.external-dependencies=" + str(len(data.get("dependencies") or [])))
for name in ("ClawdlineCore", "ClawdlineApplication", "Clawdline", "ClawdlineLinux", "ClawdlineLinuxTests"):
    print(name + ".sources=" + ",".join(sources(name)))
    print(name + ".deps=" + ",".join(deps(name)))
    print(name + ".external-products=" + str(len(targets[name].get("product_dependencies") or [])))
for name in ("Clawdline", "ClawdlineLinux"):
    if name not in products:
        print("missing-product:" + name)
        sys.exit(0)
    print(name + ".product-targets=" + ",".join(sorted(products[name].get("targets") or [])))
') || architecture_guard_fail "the Package.swift target graph could not be parsed"
case "$core_application_exactness" in
  missing:*)
    architecture_guard_fail "${core_application_exactness#missing:} is not a declared Package.swift target; the real Core/Application/Mac/Linux composition graph is incomplete"
    ;;
  missing-product:*)
    architecture_guard_fail "${core_application_exactness#missing-product:} is not a declared Package.swift product; build and CI would have no exact executable to request"
    ;;
esac
graph_field() {
  printf '%s\n' "$core_application_exactness" | sed -n "s/^$1=//p"
}
sorted_csv() {
  printf '%s\n' "$1" | LC_ALL=C sort | paste -sd, -
}

expected_core_sources=$(sorted_csv "$core_expected_members")
actual_core_sources=$(graph_field 'ClawdlineCore\.sources')
[ "$actual_core_sources" = "$expected_core_sources" ] \
  || architecture_guard_fail "ClawdlineCore's SwiftPM-resolved sources are [$actual_core_sources], not the pinned [$expected_core_sources] — a nested, extra, replaced or moved source under Packages/ClawdlineCore/ changes what actually compiles even when the symlink scan above looks fine"

expected_application_sources=$(sorted_csv "$application_expected_members")
actual_application_sources=$(graph_field 'ClawdlineApplication\.sources')
[ "$actual_application_sources" = "$expected_application_sources" ] \
  || architecture_guard_fail "ClawdlineApplication's SwiftPM-resolved sources are [$actual_application_sources], not the pinned [$expected_application_sources] — a nested, extra, replaced or moved source under Packages/ClawdlineApplication/ changes what actually compiles even when the symlink scan above looks fine"

actual_core_deps=$(graph_field 'ClawdlineCore\.deps')
[ -z "$actual_core_deps" ] \
  || architecture_guard_fail "ClawdlineCore has a target dependency ($actual_core_deps); it must have none — Core is the root of the graph"

actual_application_deps=$(graph_field 'ClawdlineApplication\.deps')
[ "$actual_application_deps" = "ClawdlineCore" ] \
  || architecture_guard_fail "ClawdlineApplication's target dependencies are [$actual_application_deps], not exactly [ClawdlineCore]"

expected_mac_deps=$(sorted_csv "$mac_expected_dependencies")
actual_mac_deps=$(graph_field 'Clawdline\.deps')
[ "$actual_mac_deps" = "$expected_mac_deps" ] \
  || architecture_guard_fail "Clawdline's (Mac composition) target dependencies are [$actual_mac_deps], not exactly [$expected_mac_deps] — an extra or a direct Clawdline -> ClawdlineCore edge passes a 'has Application' check but is not the single Mac -> Application -> Core edge this gate exists to hold"

expected_linux_sources=$(sorted_csv "$linux_expected_members")
actual_linux_sources=$(graph_field 'ClawdlineLinux\.sources')
[ "$actual_linux_sources" = "$expected_linux_sources" ] \
  || architecture_guard_fail "ClawdlineLinux's SwiftPM-resolved sources are [$actual_linux_sources], not the pinned [$expected_linux_sources] — the Linux composition source set must change explicitly with this exact guard"

expected_linux_deps=$(sorted_csv "$linux_expected_dependencies")
actual_linux_deps=$(graph_field 'ClawdlineLinux\.deps')
[ "$actual_linux_deps" = "$expected_linux_deps" ] \
  || architecture_guard_fail "ClawdlineLinux's target dependencies are [$actual_linux_deps], not exactly [$expected_linux_deps] — Linux composition must depend inward through Application, never directly on Core or Mac"

expected_linux_test_sources=$(sorted_csv "$linux_tests_expected_members")
actual_linux_test_sources=$(graph_field 'ClawdlineLinuxTests\.sources')
[ "$actual_linux_test_sources" = "$expected_linux_test_sources" ] \
  || architecture_guard_fail "ClawdlineLinuxTests resolves [$actual_linux_test_sources], not the pinned [$expected_linux_test_sources]"
expected_linux_test_deps=$(sorted_csv "$linux_tests_expected_dependencies")
actual_linux_test_deps=$(graph_field 'ClawdlineLinuxTests\.deps')
[ "$actual_linux_test_deps" = "$expected_linux_test_deps" ] \
  || architecture_guard_fail "ClawdlineLinuxTests depends on [$actual_linux_test_deps], not exactly [$expected_linux_test_deps]"

actual_package_dependencies=$(graph_field 'package\.external-dependencies')
[ "$actual_package_dependencies" = 1 ] \
  || architecture_guard_fail "Package.swift declares $actual_package_dependencies external package dependency/dependencies, expected exactly the reviewed swift-crypto 4.5.2 dependency"
package_dump=$(swift package --disable-sandbox dump-package 2>/dev/null) \
  || architecture_guard_fail "swift package dump-package failed; the exact swift-crypto pin cannot be checked"
printf '%s' "$package_dump" | python3 -c '
import json, sys
d = json.load(sys.stdin)
deps = d.get("dependencies") or []
targets = {t["name"]: t for t in d.get("targets") or []}
try:
    source = deps[0]["sourceControl"][0]
    exact = source["requirement"]["exact"][0]
    product = [x["product"] for x in targets["ClawdlineApplication"]["dependencies"] if "product" in x]
except (IndexError, KeyError, TypeError):
    raise SystemExit(1)
expected = ["Crypto", "swift-crypto", None, {"platformNames": ["linux"]}]
if len(deps) != 1 or source.get("identity") != "swift-crypto" or exact != "4.5.2" or product != [expected]:
    raise SystemExit(1)
' || architecture_guard_fail "the sole external dependency is not exactly swift-crypto 4.5.2 / Crypto for Linux ClawdlineApplication"
for graph_target in ClawdlineCore Clawdline ClawdlineLinux ClawdlineLinuxTests; do
  actual_external_products=$(graph_field "$graph_target\.external-products")
  [ "$actual_external_products" = 0 ] \
    || architecture_guard_fail "$graph_target has $actual_external_products external product dependency/dependencies; the production graph is closed and requires an explicit reviewed allowlist before adding one"
done
actual_application_external_products=$(graph_field 'ClawdlineApplication\.external-products')
[ "$actual_application_external_products" = 1 ] \
  || architecture_guard_fail "ClawdlineApplication has $actual_application_external_products external product dependency/dependencies, expected exactly Linux Crypto"

for executable_product in Clawdline ClawdlineLinux; do
  actual_product_targets=$(graph_field "$executable_product\.product-targets")
  [ "$actual_product_targets" = "$executable_product" ] \
    || architecture_guard_fail "$executable_product product resolves to [$actual_product_targets], not exactly its same-named executable target"
done

# The Linux target may use portable Foundation and the Application boundary. It must never gain an
# Apple host import, nor become a second Mac composition hidden behind a conditional. The import is
# also required: a manifest edge that no source consumes is the same inert graph defect W3-1's Mac
# correction closed.
linux_imports=$(swift_import_modules Packages/ClawdlineLinux/*.swift || true)
linux_disallowed_imports=$(printf '%s\n' "$linux_imports" | grep -Ev '^(Foundation|ClawdlineApplication)$' || true)
[ -z "$linux_disallowed_imports" ] \
  || architecture_guard_fail "ClawdlineLinux imports ${linux_disallowed_imports//$'\n'/, }; its closed import allowlist is Foundation and ClawdlineApplication"
linux_application_imports=$(printf '%s\n' "$linux_imports" | grep -cx 'ClawdlineApplication' || true)
[ "${linux_application_imports:-0}" -eq 8 ] \
  || architecture_guard_fail "ClawdlineLinux imports ClawdlineApplication ${linux_application_imports:-0} times, expected once in each of its eight policy-consuming runtime/composition/lifecycle/document/cloud source files"
swift_code_without_comments Packages/ClawdlineLinux/LinuxComposition.swift | grep -q 'HostCapabilityUnavailable\.code' \
  || architecture_guard_fail "ClawdlineLinux does not consume the Application target's typed capability-unavailable vocabulary; a declared edge alone is inert"

# W4-3 adds one closed Application vocabulary and one Linux read adapter. These checks are a
# lexical ratchet, not runtime proof: they make removal of the held descriptor-relative walker,
# read-only Board projection, or canonical package authority predicate turn this cheap guard red.
for operation in taskCreate taskRead taskMessage taskResult taskAcknowledge taskClose \
  boardRead sessionRead documentsRead documentRead; do
  swift_code_without_comments Sources/ProjectRootPolicy.swift | grep -q "case $operation" \
    || architecture_guard_fail "HeadlessApplicationOperation is missing its closed $operation case"
done
swift_code_without_comments Packages/ClawdlineLinux/LinuxDocumentReader.swift \
  | grep -q 'O_NOFOLLOW' \
  || architecture_guard_fail "Linux document reads no longer hold a no-follow descriptor walk"
swift_code_without_comments Packages/ClawdlineLinux/LinuxDocumentReader.swift \
  | grep -q 'st_nlink == 1' \
  || architecture_guard_fail "Linux document reads no longer refuse multiply-linked files"
swift_code_without_comments Packages/ClawdlineLinux/LinuxDocumentReader.swift \
  | grep -q 'fdopendir' \
  || architecture_guard_fail "Linux document listing no longer walks a held root descriptor"
swift_code_without_comments Packages/ClawdlineLinux/LinuxDaemonIngress.swift \
  | grep -q 'canWrite: false, canManage: false' \
  || architecture_guard_fail "the Linux Board projection gained a write/manage capability"
linux_package_task_authority=$(grep -c '/var/lib/clawdline/tasks/authority\.json' \
  tools/linux-package.sh || true)
[ "${linux_package_task_authority:-0}" -eq 2 ] \
  || architecture_guard_fail "Linux install and rollback do not both inspect the canonical task authority"
linux_package_legacy_fallback=$(grep -c '/var/lib/clawdline/records/runtime-state\.json' \
  tools/linux-package.sh || true)
[ "${linux_package_legacy_fallback:-0}" -eq 2 ] \
  || architecture_guard_fail "Linux install and rollback lost their bounded pre-W4-3 migration fallback"
grep -q 'compatible.add_argument("--legacy-state")' tools/linux-package-helper.py \
  || architecture_guard_fail "Linux package compatibility cannot fall back to the legacy migration source when canonical authority does not yet exist"
grep -q 'compatible.add_argument("--expected-uid"' tools/linux-package-helper.py \
  || architecture_guard_fail "Linux package compatibility no longer binds authority to the configured service uid"
grep -q 'package rollback fence is not task authority' tools/linux-package-helper.py \
  || architecture_guard_fail "Linux package compatibility no longer refuses the migration rollback fence"
grep -q 'rollback_state_incompatible' tools/linux-package.sh \
  || architecture_guard_fail "failed Linux health can no longer record typed refusal before unsafe selector rollback"

# `spec-mac-does-not-consume-application`'s other half: Clawdline's own SwiftPM source set must
# not include a Core/Application-owned file. If it did, `swift build` would compile that file a
# second time as an unrelated `Clawdline.*` type, silently defeating every check above — the
# dependency edge would be exact and the two library targets' membership would be exact, and the
# Mac target would still hold a duplicate, unconsumed copy of the same vocabulary.
# CloudTransport and CloudAppBridge have explicitly conditional Application bodies so the flat
# compatibility suite and SwiftPM hosts compile the same production bytes. CloudEnvelope remains
# visible to the Mac composition until its vocabulary can be promoted in a separately claimed
# slice; all three exceptions are pinned here rather than silently widening duplicate ownership.
grep -q '^#if !SWIFT_PACKAGE || CLAWDLINE_APPLICATION_TARGET$' Sources/CloudTransport.swift \
  || architecture_guard_fail "CloudTransport lost its Application-only shared identity boundary"
grep -q '^#if !SWIFT_PACKAGE || CLAWDLINE_APPLICATION_TARGET$' Sources/CloudAppBridge.swift \
  || architecture_guard_fail "CloudAppBridge lost its Application-only durable outbound boundary"
grep -q '^#if !SWIFT_PACKAGE || !CLAWDLINE_APPLICATION_TARGET$' Sources/CloudAppBridge.swift \
  || architecture_guard_fail "CloudAppBridge lost its Mac/flat compatibility bridge boundary"
mac_forbidden_sources=$(printf '%s\n%s\n' "$core_expected_members" "$application_expected_members" \
  | grep -Ev '^(CloudTransport|CloudEnvelope|CloudAppBridge)\.swift$' | LC_ALL=C sort)
actual_mac_sources=$(graph_field 'Clawdline\.sources')
mac_duplicated_sources=$(comm -12 \
  <(printf '%s\n' "$mac_forbidden_sources") \
  <(printf '%s\n' "$actual_mac_sources" | tr ',' '\n' | LC_ALL=C sort))
[ -z "$mac_duplicated_sources" ] \
  || architecture_guard_fail "Clawdline's own SwiftPM sources still include ${mac_duplicated_sources//$'\n'/, }, which ClawdlineCore/ClawdlineApplication already own — exclude it in Package.swift's Clawdline target so it is compiled exactly once, as part of the real target that owns it"

# The shipped Mac product is the union of the Mac composition and its two inward library targets.
# That compiler-owned union must equal the production manifest used by the compatibility flat suite;
# checking only for duplicate ownership lets an `exclude:` silently remove a dynamically used file.
manifest_production_basenames=$(printf '%s\n' "${clawdline_production_sources[@]}" \
  | sed 's#^Sources/##' | LC_ALL=C sort -u)
swiftpm_mac_union=$(printf '%s\n%s\n%s\n' "$actual_mac_sources" "$actual_core_sources" "$actual_application_sources" \
  | tr ',' '\n' | sed '/^$/d' | LC_ALL=C sort -u)
[ "$swiftpm_mac_union" = "$manifest_production_basenames" ] \
  || architecture_guard_fail "the shipped SwiftPM Mac source union differs from tools/swift-source-manifest.sh's production manifest; Package.swift exclude/source membership and the release-candidate source inventory must describe the same files"

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
    "| \`Orchestrator.swift\` ceiling | $(with_thousands "$orchestrator_ceiling") | the ratchet in \`tools/check-architecture-boundaries.sh\` |" \
    "| \`RemoteServer.swift\` ceiling | $(with_thousands "$remote_server_ceiling") | the receipt in \`tools/check-architecture-boundaries.sh\` |"
}

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

echo "architecture boundaries: main=$main_lines lines, ceiling after lock ($ceiling_block_line>$suite_lock_line), runners=$runner_count, groups=$manifest_group_count, suite_files=$suite_count, governance table is this run's own rendering, held-lock doors=$held_lock_door_sites (task-door control=$task_door_control), direct lock=$direct_registry_lock_sites, max suspension=$suspension_max, parsed=$scanner_funcs"
