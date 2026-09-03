#!/bin/bash
# Compile and run the test binary.
#
# Sources/main.swift is excluded: it is top-level code that starts the app, and two
# entry points cannot live in one binary. Everything else compiles in, so the tests
# exercise the same code the app ships rather than a copy of it.
set -euo pipefail

expected_cloud_receipt='CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=12 suites=CloudEnvelope:66,CloudAccount:105,CloudTransport:68,CloudAppBridge:125,CloudSettings:59,ScheduleResume:12,CloudClock:47,CloudCanonicalJSON:91,CloudCommandLedger:101,CloudOutboundSpool:141,CloudPairing:172,CloudLifecycle:87'
# The signed-release baseline has an observed 6,781-check receipt. Root Assignment adds 82
# executed checks, Usage Portfolio adds 43, Milestone adds 15, inline Codex patches add 15,
# the typed planning graph adds 14, the Cloud bridge lifecycle adds 75, and the Usage mobile,
# schedule-title, Feature-status and Claude-only spending correction adds 3. Splitting owned-child
# dispatch from detached automation adds 8. Clawdfather succession adds 75. Root Assignment
# event-time delivery receipts add 16. Bounded Keychain writes add 22. The consolidated
# Keychain/signing correction adds 43 more: CloudAccount
# 92 -> 105, CloudSettings 40 -> 59 and CloudLifecycle 76 -> 87. Every added check is
# unconditional and outside a loop, so the delta is arithmetic rather than an estimate; the
# paired checks inside do/catch contribute one each.
# Publication-bound Session process evidence and bounded subprocess cleanup add 35 checks; removing
# five obsolete main-crossing sites and replacing the four-check coordinator wait fixture with
# five direct-publication checks makes the net change 31.
# The exact candidate-tree run remains authoritative and must update this guard if its observed
# final count differs.
# Transcript first-paint isolation adds 25 unconditional checks: three route/tier predicates and
# twenty-two admission, completion, drain, interruption and queue-responsiveness checks.
# Browser token adoption adds 7 unconditional checks for the 303/cookie contract and public health.
# Stale-while-revalidate readings add 68 unconditional checks: 24 for the freshness policy
# itself (fresh service, stale-then-refresh ordering, a refusal ageing rather than replacing a
# reading, and the serveFor edge), 21 for single-flight admission and the answer a waiter gets
# when the read it joined was refused, 14 for moving the lane's depth to where it can only refuse
# a request that had nothing to serve, and 9 for the trace that keeps queueing apart from cost.
# Reading the process list in one language adds 7 unconditional checks. The fixture beside them
# was written in English by hand, which is how a Mac running zh_TW.UTF-8 came to find none of its
# seventeen assistants: `ps -o lstart=` renders four fields there instead of five, the parser falls
# back to the no-lstart offset, and every row is dropped. Three of these ask the real `ps` through
# the real helper rather than a fixture, so the guard fails on the machine that would have the bug.
# Coexisting with iTerm2's native tmux control mode adds 40 unconditional checks: 11 for reading
# `#{client_flags}` off `list-clients`, 22 for what becomes of an iTerm2 row that has an identity
# and no pty — including that the marker key is the same word in `iterm.js` and in Swift — and 7
# for whether selecting a tmux pane may also bring iTerm2 forward. 7448 -> 7488.
# Correcting that attribution adds 31 more, in the same three groups: 9 for the window count that
# gives it a ceiling and for a tmux this app cannot find being a question that was never asked,
# 16 for the ceiling itself and for a control-mode client having to be speaking over a pty iTerm2
# named in the same reading, and 6 for that identity deciding activation and for the activation
# tail leaving the caller's thread. 7488 -> 7519.
# tmux read parity adds 56 unconditional checks in four groups, counted by running each group on
# its own rather than by reading the source: 30 for one subprocess answering for every pane — the
# script it sends, the marker that has no `%` for strftime to eat, the parsing of what comes back,
# and ten panes costing one invocation with a dead one among them — 14 for a server Clawdline
# starts detached and types its line into rather than handing to tmux as a command, 8 for the
# screen a decision reads being the screen and not its history, and 4 more in the existing
# terminal-plan group for tmux installed with no server being its own answer. 7542 -> 7598.
# Correcting that slice adds 28, counted the same way — each group run on its own. 17 are a new
# group for what a failed tmux command proves about the server behind it, which is where the
# blocking defect was: a socket that has never existed says `error connecting to <path> (No such
# file or directory)` rather than `no server running on <path>`, so the Mac the detached server
# was written for was the one Mac it did not start on. 8 more in the batched-reading group, for a
# deadline and an unreachable socket no longer reading as a tmux too old for the script, and for
# the per-pane fallback itself, which no test had ever executed. 3 in the detached-server group,
# for the page saying that typing a line is not the shell running it and for a pane tmux would
# not type into being a failure. 7598 -> 7626.
# Extracting the store codec into `OrchestratorStore` adds 399 checks and changes nothing
# else: ten table-driven codec groups, one check per field per record, plus the legacy-shape
# fixtures each type's preserved branch is worth. Every pre-existing count is unmoved, which
# is what makes the relocation behaviour-neutral rather than merely green. The extraction was
# measured against 7519 and has been rebased twice since, each time on somebody else's landing
# rather than on anything it changed: the reader-paced question steps moved the baseline to
# 7542, and tmux read parity moved it to 7626. 7626 + 399 -> 8025.
# Giving the terminal a setting of its own adds 26, counted by running the two groups on their own
# rather than by reading the source. 19 of them are the terminal-plan group being rewritten from the
# hotkey scope onto `StartPoints.TerminalChoice`: twelve scope-shaped cases go and thirty-one arrive
# — six for `auto` reproducing exactly the order that shipped, six for the two answers the old
# setting had no words for, six for what a `config.json` written before the key meant by its scope,
# four for every triple that used to reach `plan` reaching the same answer through the derived
# choice, and nine for the file itself, including that an unreadable value and an absent one get the
# same answer and that the derived one is written down on the next save. 5 more are the finishing:
# the three raw values that are the file format, a config that says nothing at all, iTerm2-by-name
# with no tmux to have fallen into, and the two that read every page writing a terminal refusal and
# require it to ask for a name first — `terminal_unsupported` stopped carrying one. The last 2 are
# in the screen-reading group, where `docs/interface.md`'s "200 lines of history" is now compared
# with the depth `Targets.screenWithHistory` actually asks tmux for instead of being a transcription
# nothing could contradict. 8026 -> 8052.
# Cut 2 stage 1 gives the five smallest collections an owner and adds 35 unconditional checks in
# four groups: 15 for a transaction reading back its own writes across all five collections and the
# next transaction still seeing them, 4 for `Orchestrator.lock` being the registry's own instance
# and a reader waiting out the whole of a writer's transaction, 11 for the per-terminal facts
# keeping the replace-whole/merge-one semantics the projection had before, and 5 for a graph
# admission being one reservation whose public release takes the lock for itself. No pre-existing
# count moves, which is what makes the relocation behaviour-neutral rather than merely green.
# The stage was measured against 8025 and has been rebased three times since, each onto
# somebody else's landing rather than anything it changed: 8026 after the reader-paced question
# steps, 8052 after the terminal-choice setting. 8052 + 35 -> 8087.
# The heavy-compile lease adds 190 checks, and the number is the one the suite reported rather
# than one read off the source: 8087 -> 8277, measured on the exact staged tree 90c137d5 in the run
# that this line now guards. Eleven groups: the record three programs share, renewal as the proof of
# life, the backstop that is necessary and never sufficient, pid recycling and the unpinned locale,
# the FIFO queue, release and cancel ownership, restart reconciliation, admission degrading to the
# floor rather than refusing, a holder that is not compiling being reported and still not
# reclaimable, a heartbeat that outlives its work, and the store round trip. The run before it was
# red at 4 of 8277 — a briefing that carried a loopback recipe a codex child cannot use, and a
# route locator that stopped being unique when a second `/release` route existed — so this number
# is from the repair, not from the delivery.
# Closing that delivery's review adds 6, counted the same way and appended rather than folded into
# the paragraph above, because each paragraph says how its own checks were arrived at and a reader
# asking where the final number came from needs both halves. 4 are in the start-sheet words group:
# a language keeping the `{app}` hole in the one refusal sentence that has no name to fill it, the
# three pages no longer asking for a name `terminal_unsupported` never carries, what each of those
# branches actually draws rather than only what it asks — the gap that let the whole delivery ship
# a carefully written refusal nobody could read — and the mock answering that code in the shape the
# server really sends. 2 are in the terminal-plan group, for a hand-typed `terminal` value being
# named as discarded instead of silently replaced. Every one is unconditional and at the group's own
# top level. 8277 -> 8283.
# The second correction round over the compile lease adds 22, counted from the diff rather than
# from memory and every one unconditional at its group's top level. 12 are a new group for a
# refusal being an answer to an ask: the poll clock moving on a refusal and on an effect that
# failed, the place in the line not moving with it, nobody else's clock moving, the later arrival
# that must not take the slot from a waiter that has been refused all along, and the control that
# a head which really has stopped asking is still passed over. 7 are in the store round trip,
# which asserted "every field survives" over a codec that dropped three: the two the holder
# carries, the refusal note, the waiter's poll clock, a refusal row with no request id, an adopted
# holder that pins the provenance fallback, and the fixture's own control that no two of its ten
# clocks coincide. 1 more is that same control for the `holder.txt` round trip beside it, whose
# four clocks were all one instant. 2 are the process readings one decision takes, now that the
# one nothing read is gone. 8331 -> 8353. The exact candidate-tree run remains authoritative.
# Removing the broker heavy-compile lease takes 260 away, and the number is the one the suite
# reported rather than one read off the source: 8353 -> 8093, from the run that this line now
# guards, on this branch's tree with `CLAWDLINE_SUITE_JOBS=1`. Thirteen groups go with
# `Tests/OrchestratorLeaseTests.swift` — the same eleven the lease's own paragraph above lists,
# plus the two its two correction rounds added — and no group outside that file loses a check,
# which is what makes this a removal rather than a change to anything that stayed. **The three
# paragraphs above are kept rather than collapsed into this one**: each says how its own number was
# arrived at, and a reader asking where 8093 came from needs the arithmetic *and* the fact that no
# step of it was arithmetic. `Tests/test-sh-lock.mjs` moves separately and is not in this number:
# 165 -> 150, counted by that file itself.
#
# The landing queue adds 72, and they were counted rather than predicted: its five groups were run
# on their own through CLAWDLINE_TEST_GROUPS against the delivered branch and reported `72 focused
# checks passed`, and against a mutation restoring the pre-change world — membership admitting only
# declared pending landings, the isolated write set handed back and dropped, the old single-line
# claims projection — the same selection reported `47 of 72 focused checks failed`. Both runs held
# this script's own lock; neither was a full suite, so 8,298 is 8,226 plus a measured delta and not
# a reading of the whole tree. `main` has moved since this branch's base, so the landing root
# recomputes the absolute from its own tree and what carries across the merge is the 72.
# The local Feature classifier adds 125: six groups in `Tests/UsageLedgerTests.swift` — the rung
# ladder and its decline reasons, the acceptance policy's threshold, the conflicting-head refusal,
# the backfill dry run, the payload's statement of whether a producer is configured, and the Project
# scope a Feature and the Projects table now resolve by one shared rule.
#
# **8101 -> 8226, and the base moved after this line was first written.** The delivery measured
# 8218 four times, and that number was right about the tree it was measured on and wrong about this
# one: `a4ed9edb` added four checks inside a two-variant loop and did not re-seal, so `main` itself
# read 8101 against a seal of 8093 before this landed. Both numbers here are what a run reported —
# 8226 from the exact candidate tree below, 8101 from `main` — rather than either being 8218 + 8.
# **The arithmetic would have produced the same answer and could not have told anyone the base was
# wrong**, which is the whole reason this line is set from a run.
#
# Then the draft/refusal extraction landed on top of that base:
# The draft/refusal extraction adds 157 unconditional checks in six groups, and moves no
# pre-existing count — which is what makes the relocation behaviour-neutral rather than merely
# green. The arithmetic, by group: 13 for the root-identity refusal (nine table rows plus the four
# identity facts its extra carries), 23 for the dispatch door (four owner rows, eight route rows,
# eight assistant rows and the three messages that each name a different door), 41 for the bodies
# `draft(from:)` refuses (forty table rows plus the one row that proves the filesystem seam is
# consulted), 50 for a fully populated body and a minimal one compared field by field against the
# twenty-five fields a `Draft` has, 18 for the shapes an older root still writes, and 12 for
# `isTaskID` and `isTaskSecret`. 8353 -> 8510.
# Rebased onto the lease removal: that landing took the receipt to 8,093, and this
# extraction's 157 land on top of it. Its 157 land on top of whatever base is current; the value below is what a run reported, and the
# base under it moved from 8093 to 8101 to 8226 while this branch waited.
# The durable handoff label and its correction wave add 39: 26 for the record — the codec, the
# rehydration through a restart, the reused terminal id and the different conversation, the untitled
# handoff that stores nothing — and 13 for the correction that split "is it bound?" from "is a field
# missing?", gave the projection an ambiguity refusal, and made the forget guard able to go red.
# **8383 -> 8422, read off the run, not added up.** The arithmetic agrees this time, which is worth
# nothing on its own: it agreed on the base before this one too, while the base itself was wrong.
# Putting the notification on the delivery receipt adds 23 checks and takes 7 away, counted from
# the diff and confirmed by two mutation runs that made every one of the 23 go red. The 23: 12 in a
# new group for the delivery push — its pure wording with and without `smart_notifications`, one
# push for a new receipt, none for a repeat, none for a report outside its turn, and the preference
# gate — 9 in the fan-out group for which key that push reads and for `push_on_fanout` inheriting
# `push_on_finish`, and 2 in the audience group for the removed machinery being absent from
# `Sources/StateHook.swift`. The 7: the three `.finished` decisions that had a case to test, and the
# four in `a long turn keeps enough time to announce its finish`, whose `FinishTracker` is gone with
# the event it timed. Net +16.
#
# **Both halves found `a4ed9edb`'s missing re-seal on their own.** The classifier line measured
# `main` at 8,101 against a seal of 8,093; this one reached the same eight from the other end, by
# reading that the four new `check(` calls sit inside a loop over English and Traditional Chinese.
# Two roads to one number is worth more than one number asserted twice — and neither road is what
# this line is set from.
#
# **8,438 is what this tree's own run reported**, and the number reached this line from that run
# rather than from 8,422 + 16. The two agree, which is worth stating only because agreeing is not
# what makes it right: the same arithmetic agreed with the seal below it on a base that was eight
# short, and the guard that compares these two records cannot tell a pair that agrees from a pair
# that is correct. This one was re-sealed under `CLAWDLINE_RESEAL=1`, which says out loud that the
# run existed to produce the count, and the receipt check at the end of that run is what settled it.
#
# The landing queue merges on top of that. Its own run reported 8,298 against a base of
# 8,226 — a measured delta of 72 — but that base is not this one, so the delta is a
# prediction and not the seal. The value below is what this tree's run reported.
# Task retention becoming a setting adds 34, in the two groups that line introduced. The 8,563 it
# wrote down was that focused count added to the seal *its own* base carried, and that base is not
# this one: `main` has moved to 8,549 since. The number below is what the merged tree's full run
# reported, measured rather than computed. 8,549 + 34 = 8,583.
expected_swift_receipt='8660 checks passed'
# Which tree that number was measured on: assertion call sites in `Tests/*.swift`, counted by
# `tools/check-architecture-boundaries.sh`. The line above is a record and had nothing to compare
# against, so it was green whatever it said — `main` ran 8,101 against a seal of 8,093 for hours
# with every guard passing. This is the measurement that record is checked against: add a `check`
# or an `expect` anywhere in the test sources and the guard goes red before a compiler starts.
# Set both lines together, from the same run, and never from arithmetic.
expected_swift_receipt_witness=6827

count_exact_receipt_lines() {
  local receipt=$1
  local log=$2
  awk -v receipt="$receipt" '$0 == receipt { count++ } END { print count + 0 }' "$log"
}

# >>> clawdline receipt direction >>>
# Says which way the total moved, because the two directions mean opposite things and shared one
# sentence until 2026-09-03. A green run with zero failures and all twelve cloud suites present
# exited 125 against a seal eight checks stale, and `receipt mismatch` reads as *your delivery is
# broken* in a case where the delivery was fine; the line that hit it spent a round proving the
# eight were not its own. Short is the other direction and is not cosmetic: a group that aborts
# takes the ones after it, so the count is a coverage number as well as a result.
# Reports only. The exit code belongs to whatever called this — a check should not alter the
# conclusion of the thing it reports on.
report_receipt_direction() {
  local log=$1 sealed attempted ran suite missing=""
  sealed=${expected_swift_receipt%% *}
  case "$sealed" in "" | *[!0-9]*) return 0 ;; esac
  # The total lives on a different line in each case: `N of M checks failed:` when the suite is
  # red, `M checks passed` when it is green. An earlier version read only the first and was
  # therefore silent on the case that actually arrived.
  attempted=$(awk 'match($0, /^[0-9]+ of [0-9]+ checks failed/) { n = $3 }
                   match($0, /^[0-9]+ checks passed$/)         { n = $1 } END { print n }' "$log")
  case "$attempted" in "" | *[!0-9]*) return 0 ;; esac
  if [ "$attempted" -lt "$sealed" ]; then
    echo "The total came out short: $attempted ran, this tree's seal is $sealed, so $((sealed - attempted)) never ran." >&2
    ran=$(grep -aoE '^  . [A-Za-z]+ \([0-9]+ checks\)' "$log" | awk '{ print $2 }')
    for suite in $(printf '%s' "${expected_cloud_receipt#*suites=}" | tr ',' ' '); do
      printf '%s\n' "$ran" | grep -qx "${suite%%:*}" || missing="$missing ${suite%%:*}"
    done
    [ -n "$missing" ] && echo "Cloud suites that never reported:$missing" >&2
    echo "A run that did not finish is not a green, whatever its failure count says." >&2
  elif [ "$attempted" -gt "$sealed" ]; then
    echo "The tree grew and the seal did not follow: $attempted ran, the seal says $sealed, so $((attempted - sealed)) checks were added without re-sealing." >&2
    echo "The suite itself is fine. What needs updating is the seal above and the governance row beside it — from a run, never from arithmetic." >&2
  fi
  return 0
}
# <<< clawdline receipt direction <<<

verify_test_completion_receipts() {
  local log=$1
  local cloud_receipt_count swift_receipt_count reported_swift_receipts
  cloud_receipt_count=$(count_exact_receipt_lines "$expected_cloud_receipt" "$log")
  if [ "$cloud_receipt_count" -ne 1 ]; then
    echo "Cloud test completion receipt appeared $cloud_receipt_count times, expected exactly once — full output kept at $log" >&2
    # An aborted group fails here first, before the Swift seal is ever compared, so the direction
    # has to be reported on this path too or `--verify-completion-receipts` stays silent about the
    # one case it is most often pointed at: a log from a run that stopped early.
    report_receipt_direction "$log"
    return 125
  fi

  swift_receipt_count=$(count_exact_receipt_lines "$expected_swift_receipt" "$log")
  if [ "$swift_receipt_count" -ne 1 ]; then
    reported_swift_receipts=$(awk '/^[0-9]+ checks passed$/ { values = values (values ? ", " : "") $0 } END { print values ? values : "none" }' "$log")
    echo "Swift test completion receipt mismatch: expected exactly one '$expected_swift_receipt'; found $swift_receipt_count exact and reported $reported_swift_receipts — full output kept at $log" >&2
    report_receipt_direction "$log"
    return 125
  fi
}

# This narrow mode exercises the full-suite completion guard without compiling or running the
# suite. It never emits a completion receipt of its own and cannot be mistaken for a full run.
if [ "${1:-}" = "--verify-completion-receipts" ]; then
  if [ "$#" -ne 2 ]; then
    echo "usage: $0 --verify-completion-receipts <suite-log>" >&2
    exit 2
  fi
  verify_test_completion_receipts "$2"
  exit $?
fi

cd "$(dirname "$0")"
. tools/swift-source-manifest.sh
verify_swift_source_manifest full
bash tools/check-architecture-boundaries.sh

# Trailing commas in an argument list are Swift 6.1 syntax. The toolchain here is usually
# newer than CI's, so code that compiles locally can fail to parse on the runner — and the
# error arrives ten minutes later, in a log, attached to a push that is already public.
offenders=$(awk '
  $0 ~ /,[[:space:]]*\)/            { print FILENAME ":" FNR ": " $0 }
  prev ~ /,[[:space:]]*$/ && $0 ~ /^[[:space:]]*\)/ { print FILENAME ":" FNR-1 ": " prev }
  { prev = $0 }
' "${clawdline_production_sources[@]}" "${clawdline_test_sources[@]}")
if [ -n "$offenders" ]; then
  echo "trailing comma before ) — Swift 6.1 syntax, and CI runs something older:"
  echo "$offenders"
  exit 1
fi

# The compatibility page is generated from the table the app uses, so the two cannot disagree
# — but only if something checks. A release added to Compat.swift and not regenerated here is a
# page claiming support for a version that was never tried.
tools/build-compatibility.py --check
tools/check-web-strings.py
tools/check-web-ids.py
node Tests/docs-ui-labels.mjs
node Tests/agent-attention-principle.mjs

# The checked-in protocol fixture is the cross-runtime byte authority. Generate the expected
# bytes in memory and compare through the generator's read-only mode so hand edits fail closed.
swift tools/generate-protocol-vectors.swift --check Tests/protocol-vectors.json

# Keep the small browser-independent renderer contracts beside the Swift suite. The web app's
# scoped package.json marks its shipped files as ESM, matching the browser's module entry.
browser_contract_suites=(
  Tests/web-schedules.mjs
  Tests/web-coordinator.mjs
  Tests/web-clawdfather.mjs
  Tests/web-optimistic.mjs
  Tests/web-transcript-requests.mjs
  Tests/web-session-resilience.mjs
  Tests/web-viewport.mjs
  Tests/web-layout-diagnostics.mjs
  Tests/web-session-disposition.mjs
  Tests/web-session-closeability.mjs
  Tests/web-title-transport.mjs
  Tests/web-code-copy.mjs
  Tests/web-message-images.mjs
  Tests/web-project-artifacts.mjs
  Tests/web-row-gesture.mjs
  Tests/web-detached-attach.mjs
)
if [ "${#browser_contract_suites[@]}" -ne 16 ]; then
  echo "browser contract roster changed without updating its sealed count" >&2
  exit 1
fi
for browser_contract_suite in "${browser_contract_suites[@]}"; do
  node "$browser_contract_suite"
done
# The hosted console: which transport it is, the pairing mirror against the checked-in
# vectors, and that the static bundle a person uploads by hand is the same bytes twice.
node Tests/web-cloud-boot.mjs
node Tests/web-cloud-pairing.mjs
node Tests/web-cloud-onboarding.mjs
node Tests/web-app-build.mjs
node Tests/dispatch-role-contract.mjs
node Tests/restart-rollout-contract.mjs
node Tests/remote-response-write-close.mjs
node Tests/release-signing-contract.mjs
# The shared-tree commit guard: that `tools/git-hooks/pre-commit` refuses a commit carrying a path
# another session is working on, that it lets everything else through, and that it fails open and
# loudly when Clawdline is not answering. Throwaway repositories under `mkdtemp` only — this suite
# never runs git against the checkout it is testing, and proves that containment on the way out.
node Tests/git-hooks.mjs
# The onboarding policy, compiled out of Sources/Onboarding.swift without its AppKit half: that a
# config switch is not readiness, that an allocated credential is not a connection, and that the
# installer reopens the exact bundle it just wrote. It runs here rather than in the Swift suite
# because the shipped policy has no AppKit dependency and this keeps it a second rather than a
# recompile of everything.
node Tests/app-onboarding-focused.mjs
node Tests/keychain-rebuild-focused.mjs
# Two suites that existed and that nothing ran: neither was in this list, and CI only runs
# this script. A test nobody runs is a test that passes.
node Tests/web-user-messages.mjs
node Resources/web/app/js/net/client.test.mjs
# These two are about this script rather than the app: that a crashed run still leaves its output,
# and that the machine-wide suite lock below serialises the expensive half. Both run before the
# lock is taken, so a machine that is already busy still gets told what is wrong with this checkout
# before it starts queueing.
node Tests/test-sh-streaming.mjs
node Tests/test-sh-lock.mjs

# >>> clawdline suite lock >>>
# One machine, one suite run — and this block is the whole of that promise. It is bounded by the two
# marker comments so `Tests/test-sh-lock.mjs` can lift it out and drive it against cheap stand-ins,
# the way `Tests/test-sh-streaming.mjs` lifts out the pipeline. Rename a marker and that guard fails
# loudly rather than quietly scanning nothing.
#
# **Why it exists.** The `swiftc` below compiles every file in `Sources/` together with the files in
# `Tests/` in one invocation, and spawns one `swift-frontend` per job. On 2026-09-03 four of them
# held 46 / 45 / 27 / 8 GB at once on a 24 GB Mac and Jetsam force-rebooted it twice, at 01:24 and
# at 01:45 — see /Library/Logs/DiagnosticReports/JetsamEvent-2026-09-03-014340.ips. Several sessions
# share this checkout, and until now the mitigation was a gentleman's agreement that whoever ran the
# suite would `mkdir /tmp/clawdline-suite.lock` first. Forgetting cost the whole machine, so the
# script holds the lock itself.
#
# **What the lock covers**: the `swiftc` invocation *and* the test-binary run that follows it. The
# compile is the memory, the run is minutes of CPU, and the agreement this replaces covered both.
# Release is on EXIT rather than on a line after the run, because every failure between here and the
# end of the script leaves through `exit`, and EXIT is the one path all of them share.
#
# **And what it does not cover, which somebody should decide about rather than discover.**
# `Tests/app-onboarding-focused.mjs` and `Tests/keychain-rebuild-focused.mjs` run above this line and
# each invoke `swiftc` on a file or two of their own. Those compiles are seconds rather than the
# whole of `Sources/`, so they are outside this boundary as the protocol defines it — but they are
# real compiles happening without the lock, and a machine-wide probe will see them. Moving the lock
# above them would close that at the cost of holding it through a dozen unrelated node suites, which
# every queued run then waits out.
#
# **Liveness is proved by renewal, not by a pid existing.** A pid is a proxy and proxies outlive the
# work: a holder on this machine recorded a `sleep 14400` sentinel, the work it stood for died,
# launchd adopted the sleep, and under a pid-existence rule that lock would have blocked every other
# session until the sentinel's own four-hour timeout. So the holder refreshes `holder.txt` while it
# works, and a holder that stops refreshing stops proving it is there. The distinction is the whole
# of the rule and is worth saying plainly: **a clock on the work is wrong** — a four-hour compile is
# not stale, and a duration timeout is exactly the draft that was withdrawn here — **a clock on the
# proof of life is right**, because a holder renewing every 20s never trips a 60s renewal deadline
# however long its work runs.
#
# **Admission is fail-closed and the physical backstop is never waived.** The lock is handed on only
# when BOTH (A) the holder has stopped proving it is alive AND (B) no compiler process exists
# anywhere on this machine. (B) on its own would hand the lock to a second run in the gaps *between*
# one study's compiles; (A) on its own would hand it over while an orphaned compile is still
# spending the 46 GB this lock exists to ration. Evidence that is missing, stale or ambiguous reads
# `unknown` and **blocks**; it never reads "dead".
#
# **Nothing here kills anything** except the renewal loop this script starts for itself. Not the
# holder, not an orphaned compiler, not a process group. It queues, it refuses, and it names who to
# ask.

# Everything the tests must vary is an environment variable, so `Tests/test-sh-lock.mjs` can drive
# this block without ever going near the real lock or the real compiler.
CLAWDLINE_SUITE_LOCK_DIR="${CLAWDLINE_SUITE_LOCK_DIR:-/tmp/clawdline-suite.lock}"
# `pgrep -x` matches the executable's own name. Measured against a live compile here rather than
# assumed: a running `swiftc` shows up as `swift-frontend` under `pgrep -x`, while `ps -A -o comm=`
# prints the whole toolchain path and matches no bare name at all.
CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN="${CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN:-swift-frontend}"
CLAWDLINE_SUITE_LOCK_RENEW_SECONDS="${CLAWDLINE_SUITE_LOCK_RENEW_SECONDS:-20}"
# Three renewals of slack. A reader prefers the deadline the holder recorded in its own record, so a
# holder that renews more slowly than this reader expects is never declared dead by that reader.
CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS="${CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS:-60}"
CLAWDLINE_SUITE_LOCK_WAIT_SECONDS="${CLAWDLINE_SUITE_LOCK_WAIT_SECONDS:-3600}"
CLAWDLINE_SUITE_LOCK_POLL_SECONDS="${CLAWDLINE_SUITE_LOCK_POLL_SECONDS:-5}"
CLAWDLINE_SUITE_LOCK_NOTICE_SECONDS="${CLAWDLINE_SUITE_LOCK_NOTICE_SECONDS:-30}"
CLAWDLINE_SUITE_LOCK_DONE_FLAG="${CLAWDLINE_SUITE_LOCK_DONE_FLAG:-$CLAWDLINE_SUITE_LOCK_DIR/done}"
# Where the renewal loop leaves the reason it stopped, so the run it was renewing for can find out.
# **Outside the lock directory on purpose**: two of the three reasons the loop stops are that the
# directory has changed hands or is gone, and a note written inside it would go with it.
CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE="${CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE:-${TMPDIR:-/tmp}/clawdline-suite-renewal-stopped.$$}"
# The heartbeat is a file inside the lock, touched on every renewal, and the record points at it.
# Inside the lock on purpose: when the lock directory goes, the beat goes with it, and no orphaned
# heartbeat is left pointing at work that ended.
CLAWDLINE_SUITE_LOCK_BEAT="${CLAWDLINE_SUITE_LOCK_BEAT:-$CLAWDLINE_SUITE_LOCK_DIR/beat}"
# 75 is EX_TEMPFAIL from sysexits(3) — "temporary failure, the user is invited to retry", which is
# what a busy lock is. It is distinct from every status this script already produces: 0, 1 (a guard
# said no), 2 (usage), 125 (receipt), 126 (tee) and the suite's own, which for a signal death is
# 128+N. Nothing else here returns 75.
CLAWDLINE_SUITE_LOCK_BUSY=75

# Where this run's output goes. Defined here rather than beside the pipeline further down, because
# the lock records it: a blocked run can then watch the run it is waiting for instead of guessing.
LOG="${LOG:-${TMPDIR:-/tmp}/clawdline-tests-$$.log}"

clawdline_suite_lock_token=""
clawdline_suite_lock_pid=""
clawdline_suite_lock_started=""
clawdline_suite_lock_pid_started=""
clawdline_suite_lock_renewer=""
clawdline_suite_lock_compilers=""
# What the last compiler probe *answered*, kept apart from what it found: `found`, `clear` or
# `unreadable`. Without it the writer below could not tell "I looked and the machine was clear"
# from "I looked and the machine would not say", and wrote the first for both — the fail-open
# direction, in the one field the record contract singles out for keeping them apart.
clawdline_suite_lock_compilers_verdict="unreadable"
clawdline_suite_lock_working=""
clawdline_suite_lock_state=""
clawdline_suite_lock_evidence=""

clawdline_suite_lock_new_token() {
  local minted
  minted=$(od -An -tx1 -N16 /dev/urandom 2>/dev/null | tr -d ' \n') || minted=""
  # `od | tr` succeeds and prints an empty string when /dev/urandom cannot be read, and an empty
  # token would match every lock on release. Fall back to something still unique to this run.
  case "$minted" in
    ????????????????*) : ;;
    *) minted="pid$$-$(date +%s)-${RANDOM}${RANDOM}" ;;
  esac
  printf '%s' "$minted"
}

clawdline_suite_lock_field() {
  # The value of one `key=` line, or nothing. Nothing is a real answer here — a record that has not
  # been written yet — so callers test the value rather than this function's status.
  local key=$1 file=$2
  [ -f "$file" ] || return 0
  awk -v key="$key" 'index($0, key "=") == 1 { print substr($0, length(key) + 2); exit }' "$file" 2>/dev/null
}

clawdline_suite_lock_pid_identity() {
  # A pid's start time, as one normalised line, used to tell a recorded holder from a later process
  # that inherited its number. **Both sides of every comparison come out of this one function**,
  # which is why `LC_ALL=C` is pinned here and not at the call sites. Measured on this Mac: the same
  # process reads `Thu Sep  3 02:18:04 2026` under `LC_ALL=C` and `四  9/ 3 02:18:04 2026` under the
  # machine's own `zh_TW.UTF-8`. Nor is the difference only in the bytes — holding the formatter
  # still and varying the day, `zh_TW` renders five whitespace-separated tokens on 2026-09-03 and
  # four on 2026-08-31, while `LC_ALL=C` renders five on both. So nothing here counts fields; the
  # whole line is normalised and compared as a string, from one formatter on both sides.
  local pid=$1 line
  case "$pid" in "" | *[!0-9]*) printf 'unknown'; return 0 ;; esac
  line=$(LC_ALL=C ps -o lstart= -p "$pid" 2>/dev/null | awk 'NR == 1 { $1 = $1; print; exit }') || line=""
  printf '%s' "${line:-unknown}"
}

clawdline_suite_lock_pid_verdict() {
  # `alive`, `gone` or `unknown` — and the third one is the whole point of this function.
  #
  # There used to be a two-valued `clawdline_suite_lock_pid_alive` beside this — `ps -p` with its
  # status read as the whole answer — and the collapse it made is fail-open wherever the question
  # is "may I act": a renewer that reads one failed `ps` as "the run is gone" stops proving
  # liveness while the run is still inside the guarded section, and sixty seconds later a second
  # run walks in. That happened, reproducibly, with a `ps` broken for one tick. Its last caller was
  # the takeover gate, which had the same collapse in the other direction, so the two-valued reader
  # is gone rather than left here to be picked up again.
  #
  # So the probe carries its own control. `ps -p <pid> -p 1` asks about the process *and* about
  # pid 1, which exists on every running macOS. If `1` comes back the tool answered, and the
  # absence of `<pid>` is then a fact rather than a silence; if `1` does not come back the reading
  # is `unknown` and the caller must not act on it. One fork, not two, because this runs every
  # twenty seconds for the length of a compile.
  # `seen` rather than `out`: `Tests/test-sh-streaming.mjs` forbids `out=$(` anywhere live in this
  # file, because capturing the suite's whole run into a variable is the shape it exists to keep
  # out, and a guard that is a little over-broad in that direction is the right way round.
  local pid=$1 seen="" control=0 target=0 n
  case "$pid" in "" | *[!0-9]*) printf 'unknown'; return 0 ;; esac
  seen=$(ps -p "$pid" -p 1 -o pid= 2>/dev/null) || seen=""
  for n in $seen; do
    if [ "$n" = "1" ]; then control=1; fi
    if [ "$n" = "$pid" ]; then target=1; fi
  done
  if [ "$control" = 0 ]; then printf 'unknown'; return 0; fi
  if [ "$target" = 1 ]; then printf 'alive'; else printf 'gone'; fi
}

clawdline_suite_lock_identity_verdict() {
  # `same`, `different` or `unknown`. `clawdline_suite_lock_pid_identity` returns the literal
  # string `unknown` when it could not read, and `unknown` never equals a recorded start — so a
  # caller comparing the two directly reads every failed read as "a different process now has
  # that number". That is a reading, not a fact, and this function refuses to let it become one.
  local pid=$1 recorded=$2 observed
  observed=$(clawdline_suite_lock_pid_identity "$pid")
  case "$observed" in unknown) printf 'unknown'; return 0 ;; esac
  case "$recorded" in "" | unknown) printf 'unknown'; return 0 ;; esac
  if [ "$observed" = "$recorded" ]; then printf 'same'; else printf 'different'; fi
}

clawdline_suite_lock_ownership_verdict() {
  # `mine`, `theirs`, `absent` or `unknown`, read off the record's token.
  #
  # `absent` — no lock directory at all — is positive evidence: this run does not hold a lock that
  # does not exist, and no amount of beating will bring it back. Everything else that is not a
  # readable token belonging to somebody else is `unknown`: a `holder.txt` caught mid-rename, a
  # record a writer could not finish, a filesystem that answered nothing. An empty token is not
  # somebody else's token.
  local lock=$1 mine=$2 found
  [ -d "$lock" ] || { printf 'absent'; return 0; }
  [ -f "$lock/holder.txt" ] || { printf 'unknown'; return 0; }
  found=$(clawdline_suite_lock_field token "$lock/holder.txt")
  case "$found" in "") printf 'unknown'; return 0 ;; esac
  if [ "$found" = "$mine" ]; then printf 'mine'; else printf 'theirs'; fi
}

clawdline_suite_lock_probe_compilers() {
  # **A global count, on purpose, and it includes other people's compilers.** The question this asks is
  # "is anything on this machine burning", not "is my own work running": counting only descendants of
  # this script's own driver would miss the compiler `node Tests/keychain-rebuild-focused.mjs`
  # spawns, which is not under that driver and is still real memory. The reasoning is in
  # docs/machine-resource-scheduling.md, commit 4d98f190.
  #
  # 0 = at least one compiler is running, 1 = none anywhere, 2 = the probe could not answer. `pgrep`
  # exits 1 when nothing matched, so a `||` here would read "no compiler" as a failure and a failure
  # as "no compiler"; the status is read explicitly instead, and only an explicit 1 is ever allowed
  # to mean the machine is clear.
  local found="" probe_status=0
  found=$(LC_ALL=C pgrep -x "$CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN" 2>/dev/null) || probe_status=$?
  clawdline_suite_lock_compilers=$(printf '%s' "$found" | tr '\n' ' ')
  # The verdict, in a global, because the status is lost at most call sites: `|| true` is how a
  # caller under `set -e` asks a probe that legitimately returns 1. A caller that wants the answer
  # reads this instead of guessing from the emptiness of the pid list, which cannot tell "nothing
  # was running" from "nothing was read".
  case "$probe_status" in
    0) clawdline_suite_lock_compilers_verdict="found"; return 0 ;;
    1) clawdline_suite_lock_compilers_verdict="clear"; return 1 ;;
    *) clawdline_suite_lock_compilers_verdict="unreadable"; return 2 ;;
  esac
}

clawdline_suite_lock_working_pids() {
  # What is actually doing the work right now, refreshed on every renewal. A single pid cannot
  # describe a sequence of compiles, and that gap is precisely how a `sleep` came to be a holder.
  #
  # **It leaves its answer in a global rather than printing it**, and that is the fix rather than
  # a style: `working=$(clawdline_suite_lock_working_pids …)` forks a subshell whose parent is the
  # very pid this function asks `pgrep -P` about, so the list — and `pid=`, which is its first
  # entry — could name a shell that existed only for the length of the reading. The probe now runs
  # in the caller's own process, before anything forks to carry its result away.
  local exclude=$1 file="$CLAWDLINE_SUITE_LOCK_DIR/.children"
  clawdline_suite_lock_working=""
  LC_ALL=C pgrep -P "$clawdline_suite_lock_pid" > "$file" 2>/dev/null || true
  [ -f "$file" ] || return 0
  # Commas, not spaces: one record is read by three programs and the Swift side has always parsed
  # `work=` as a comma-separated list. See the record contract above `clawdline_suite_lock_write_record`.
  clawdline_suite_lock_working=$(awk -v skip="$exclude" 'NF && $1 != skip { line = line (line ? "," : "") $1 } END { print line }' "$file" 2>/dev/null) || clawdline_suite_lock_working=""
  rm -f "$file" 2>/dev/null || true
  return 0
}

clawdline_suite_lock_duration() {
  # Seconds, and minutes beside them once there are enough of them to matter to a person waiting.
  local seconds=$1
  case "$seconds" in "" | *[!0-9]*) printf 'unknown'; return 0 ;; esac
  if [ "$seconds" -ge 60 ]; then printf '%ss (%sm)' "$seconds" "$(( seconds / 60 ))"; else printf '%ss' "$seconds"; fi
}

clawdline_suite_lock_phase() {
  # What the holder is doing right now, refreshed on every renewal.
  #
  # Renewal proves *that* somebody is there; this says *what they are doing*, and only the two
  # together answer whether the lock is protecting anything. Without it an honest holder writing its
  # report and a holder that finished and forgot to release look exactly the same from outside —
  # which is what happened here at 02:45 on 2026-09-03: a lock held 36 minutes, zero compilers on
  # the machine, no done flag, and a second line waiting with no safe way to tell the two apart.
  #
  # Three values, and they are the record's rather than this script's, because every writer and
  # every waiter has to read the same word: `compiling` is the reason the lock exists; `analysing`
  # is working but not compiling, which for this script is the test binary running for its several
  # minutes; and
  # `idle-holding` is "this run still needs the lock and nothing expensive is happening under it" —
  # the value that has to be *seen*, because it is the one an outside reader cannot tell from a
  # holder that finished and forgot to let go.
  #
  # **It is reportable, never a takeover condition.** A holder that has not compiled for an hour is
  # something a waiter may say out loud so a person can go and ask. It is not something this script
  # may act on: the only two conditions that hand a lock over are still that renewal stopped and
  # that no compiler is running.
  local phase=$1 dir="${CLAWDLINE_SUITE_LOCK_DIR}"
  # Through a file, not only a variable: the renewal loop is a subshell and cannot see a variable
  # set in this shell after it started.
  printf '%s %s\n' "$phase" "$(date +%s)" > "$dir/.phase" 2>/dev/null || return 0
  clawdline_suite_lock_write_record "$dir" "$clawdline_suite_lock_renewer" || true
  return 0
}

clawdline_suite_lock_write_record() {
  # Rewritten whole on acquisition and on every renewal, then moved into place, so a reader sees the
  # previous complete record or the new complete one and never half of either.
  #
  # **THE RECORD CONTRACT. One record, two writers, and this is the list.**
  #
  # `test.sh` and `build.sh` both write `<lock>/holder.txt` and both read each other's. It was
  # three writers while the broker lease existed, and the field list is the one all three agreed
  # on: nothing in it was the broker's alone, so removing that writer costs the contract nothing.
  # They used to write three different subsets of it: `test.sh` wrote seventeen fields, the other
  # two wrote eleven, only eight overlapped, and the four `test.sh` needs for its compare-and-swap
  # — `token`, `owner_pid`, `owner_started`, `heartbeat_deadline` — were written by nobody else. So
  # against a lock either of them wrote its compare was `"" = ""`, always true, and the re-read
  # beside it was carrying the whole swap alone. The same accident in the other direction:
  # `test.sh` wrote `working=` and the Swift reader read `work=`, so each side showed an empty
  # working list for the other's holder.
  #
  #   holder              who to go and ask. Free text, one line.
  #   pid                 the process actually doing the work at this beat, never a stand-in.
  #   owner_pid           the run itself — what ownership is proved against and what the renewal
  #                       loop supervises. It exists for exactly as long as the run does.
  #   owner_started       `owner_pid`'s start identity as one normalised `LC_ALL=C ps -o lstart=`
  #                       line. **The one field a writer may leave empty**, meaning "this writer
  #                       did not record it" — a shell can read that line, a writer holding only
  #                       epoch seconds cannot. Empty is unknown to every reader and is never a
  #                       mismatch. `started=` carries the same instant as epoch seconds.
  #   token               this hold's unique identity, and the compare in every compare-and-swap.
  #                       A pid is reused within hours on a busy machine; a token is not.
  #   phase               compiling | analysing | idle-holding. Reportable, never a takeover input.
  #   phase_since         epoch seconds, moved only when the phase itself changes.
  #   heartbeat           the file whose mtime is the beat, inside the lock directory.
  #   heartbeat_deadline  seconds without a beat after which this holder has stopped proving it is
  #                       alive. A reader prefers the holder's own number to its own.
  #   started             epoch seconds, when this hold began.
  #   renewed             epoch seconds, this record's own last refresh.
  #   tree                what is being verified or built.
  #   log                 where the output is going, so a blocked run can watch instead of guess.
  #   done_flag           the path this run creates when the guarded work is over. Positive signal
  #                       only: present means finished, absent proves nothing.
  #   work                comma-separated pids doing the work right now.
  #   last_compiling      epoch seconds, or `never`.
  #   compilers           three states: empty means this writer has no answer — it did not probe,
  #                       or its probe could not be read; `none` means it probed and the machine
  #                       was clear; otherwise the pids it found. **Empty and `none` are not
  #                       interchangeable**: `none` is a claim about the machine and empty is the
  #                       absence of one, and a writer that spells an unreadable probe `none`
  #                       fails open in the one field written to keep them apart.
  #   note                what a person about to remove this directory by hand should know.
  local dir=$1 exclude=${2:-} temp="$1/.holder.$$.$RANDOM"
  local phase_line phase phase_since last_compiling working worker compilers_field
  phase_line=$(cat "$dir/.phase" 2>/dev/null) || phase_line=""
  phase=${phase_line%% *}
  phase_since=${phase_line##* }
  case "$phase" in "") phase="idle-holding" ;; esac
  case "$phase_since" in "" | *[!0-9]*) phase_since=$(date +%s) ;; esac
  clawdline_suite_lock_probe_compilers || true
  # The three states of `compilers=`, written from the verdict rather than from the pid list. An
  # unreadable probe leaves that list empty exactly as a clear machine does, so writing
  # `${clawdline_suite_lock_compilers:-none}` recorded "I probed and this Mac was clear" for a
  # `pgrep` that answered nothing at all.
  case "$clawdline_suite_lock_compilers_verdict" in
    found) compilers_field="$clawdline_suite_lock_compilers" ;;
    clear) compilers_field="none" ;;
    *) compilers_field="" ;;
  esac
  # `pid` is the process actually doing the work, not a stand-in for it, and the work here is a
  # sequence: the compiler driver, then the test binary. So it is whichever of this run's children
  # is working at this heartbeat, and it falls back to the run's own shell only in the gaps between
  # them, when there is genuinely nothing else to name. `owner_pid` is the shell itself, which is
  # what ownership is proved against and what the renewal loop supervises — it is not a sentinel:
  # it exists for exactly as long as the run does.
  clawdline_suite_lock_working_pids "$exclude"
  working="$clawdline_suite_lock_working"
  worker=${working%%,*}
  case "$worker" in "") worker="$clawdline_suite_lock_pid" ;; esac
  # The beat, touched before the record that points at it, so a reader that sees the record always
  # finds the file.
  : > "$dir/beat" 2>/dev/null || true
  # When it was last true that something was actually compiling. Carried forward from the previous
  # record when it is not true now, so "36 minutes without entering compiling" is a fact a waiter
  # can read rather than one it has to have been watching for.
  if [ "$phase" = "compiling" ] || [ -n "$clawdline_suite_lock_compilers" ]; then
    last_compiling=$(date +%s)
  else
    last_compiling=$(clawdline_suite_lock_field last_compiling "$dir/holder.txt")
    case "$last_compiling" in "" | *[!0-9]*) last_compiling="never" ;; esac
  fi
  {
    printf 'holder=%s\n' "$CLAWDLINE_SUITE_LOCK_HOLDER"
    printf 'pid=%s\n' "$worker"
    printf 'owner_pid=%s\n' "$clawdline_suite_lock_pid"
    printf 'owner_started=%s\n' "$clawdline_suite_lock_pid_started"
    printf 'token=%s\n' "$clawdline_suite_lock_token"
    printf 'phase=%s\n' "$phase"
    printf 'phase_since=%s\n' "$phase_since"
    printf 'heartbeat=%s\n' "$dir/beat"
    printf 'heartbeat_deadline=%s\n' "$CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS"
    printf 'started=%s\n' "$clawdline_suite_lock_started"
    printf 'renewed=%s\n' "$(date +%s)"
    printf 'tree=%s\n' "$CLAWDLINE_SUITE_LOCK_TREE"
    printf 'log=%s\n' "$LOG"
    printf 'done_flag=%s\n' "$CLAWDLINE_SUITE_LOCK_DONE_FLAG"
    printf 'work=%s\n' "$working"
    printf 'last_compiling=%s\n' "$last_compiling"
    printf 'compilers=%s\n' "$compilers_field"
    printf 'note=%s\n' "$CLAWDLINE_SUITE_LOCK_NOTE"
  } > "$temp" 2>/dev/null || return 1
  mv "$temp" "$dir/holder.txt" 2>/dev/null || { rm -f "$temp" 2>/dev/null; return 1; }
  return 0
}

clawdline_suite_lock_phase_since() {
  local value
  value=$(clawdline_suite_lock_field phase_since "$1")
  case "$value" in "" | *[!0-9]*) date +%s ;; *) printf '%s' "$value" ;; esac
}

clawdline_suite_lock_last_compiling_phrase() {
  # "36 minutes without entering compiling" is the sentence that was missing at 02:45. It is said,
  # and it decides nothing.
  local file=$1 now=$2 value
  value=$(clawdline_suite_lock_field last_compiling "$file")
  case "$value" in
    "" | never | *[!0-9]*) printf 'nothing has compiled under this lock yet' ;;
    *) printf 'last compiling %s ago' "$(clawdline_suite_lock_duration "$(( now - value ))")" ;;
  esac
}

clawdline_suite_lock_admission() {
  # Reads somebody else's record and leaves two globals: `clawdline_suite_lock_state`, one of
  # `held` / `unknown` / `orphaned` / `stale`, and `clawdline_suite_lock_evidence`, the sentence
  # that says what was read. It sets globals rather than printing because `$(…)` would run it in a
  # subshell and throw the evidence away — and a refusal that cannot name its evidence is a refusal
  # nobody can act on.
  local file="$1/holder.txt" beat heartbeat deadline now age probe_status pid pid_started identity done_flag
  # `heartbeat` in the record is the *path* of the beat file, and the evidence is that file's
  # modification time. The holder says where its heartbeat is; the filesystem says when it last
  # happened.
  beat=$(clawdline_suite_lock_field heartbeat "$file")
  heartbeat=""
  if [ -n "$beat" ] && [ -f "$beat" ]; then
    heartbeat=$(stat -f %m "$beat" 2>/dev/null) || heartbeat=""
  fi
  deadline=$(clawdline_suite_lock_field heartbeat_deadline "$file")
  pid=$(clawdline_suite_lock_field pid "$file")
  pid_started=$(clawdline_suite_lock_field owner_started "$file")
  done_flag=$(clawdline_suite_lock_field done_flag "$file")
  case "$deadline" in "" | *[!0-9]* | 0) deadline="$CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS" ;; esac

  # The done flag is a **positive** signal only. Present means the guarded work is over, so with the
  # backstop still satisfied the lock may be handed on at once rather than after a renewal deadline
  # nobody is waiting for. Absent proves nothing — a run killed with SIGKILL never writes one — so
  # absence simply falls through to renewal below. Reading absence as "still running" would rebuild
  # the permanent roadblock somewhere new.
  if [ -n "$done_flag" ] && [ -f "$done_flag" ]; then
    clawdline_suite_lock_probe_compilers; probe_status=$?
    case "$probe_status" in
      1) clawdline_suite_lock_state="stale"
         clawdline_suite_lock_evidence="the holder marked its work finished at $done_flag and no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is running anywhere"
         return 0 ;;
      0) clawdline_suite_lock_state="orphaned"
         clawdline_suite_lock_evidence="the holder marked its work finished, but $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is still running as pid(s) ${clawdline_suite_lock_compilers}— the memory is still being spent, so nobody is admitted and nothing here will kill them"
         return 0 ;;
      *) clawdline_suite_lock_state="unknown"
         clawdline_suite_lock_evidence="the holder marked its work finished, but the $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN probe could not answer — unknown blocks"
         return 0 ;;
    esac
  fi

  case "$heartbeat" in
    "" | *[!0-9]*)
      # **The abandoned acquisition, and the one way out of `unknown`.**
      #
      # Acquiring is `mkdir`, then a few forks, then the first record. A run that dies in that
      # window — a harness timeout, a Ctrl-C — leaves a directory with no `holder.txt` and no
      # `beat` in it, and `unknown` blocks on it correctly. What was missing is that nothing could
      # ever clear it: only `stale` reaches the takeover, a record that will never be written can
      # never become stale, and the note in this very file tells the next person not to remove the
      # directory by hand. One ordinary Ctrl-C therefore turned the machine's compile slot into a
      # permanent roadblock.
      #
      # So this is not "absence read as death" — it is positive evidence of a distinct thing:
      # *nothing has ever been written here*, for longer than any acquisition could take, and the
      # machine is clear. All four must hold. A directory that has a `beat` but no record had a
      # record once and something removed it, which is a different and unexplained event: that
      # stays `unknown`. And (B) is never waived here either.
      if [ ! -f "$1/holder.txt" ] && [ ! -f "$1/beat" ]; then
        local born born_age
        born=$(stat -f %m "$1" 2>/dev/null) || born=""
        case "$born" in
          "" | *[!0-9]*) born_age=-1 ;;
          *) born_age=$(( $(date +%s) - born )) ;;
        esac
        if [ "$born_age" -gt "$deadline" ]; then
          clawdline_suite_lock_probe_compilers; probe_status=$?
          case "$probe_status" in
            1) clawdline_suite_lock_state="stale"
               clawdline_suite_lock_evidence="it was created ${born_age}s ago and has never held a record or a heartbeat, which is what a run killed between mkdir and its first write leaves behind, and no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is running anywhere"
               return 0 ;;
            0) clawdline_suite_lock_state="orphaned"
               clawdline_suite_lock_evidence="it has never held a record, but $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is still running as pid(s) ${clawdline_suite_lock_compilers}— nobody is admitted and nothing here will kill them"
               return 0 ;;
          esac
        fi
      fi
      clawdline_suite_lock_state="unknown"
      clawdline_suite_lock_evidence="its record names no heartbeat file, or the file it names is not there, so whether anyone is still there is unknown — and unknown blocks rather than reading as dead"
      return 0 ;;
  esac
  now=$(date +%s)
  age=$(( now - heartbeat ))
  if [ "$age" -lt 0 ]; then
    clawdline_suite_lock_state="unknown"
    clawdline_suite_lock_evidence="its heartbeat is ${age#-}s in the future, so the two clocks disagree and the evidence is ambiguous — ambiguous blocks"
    return 0
  fi
  if [ "$age" -le "$deadline" ]; then
    clawdline_suite_lock_state="held"
    # Three answers, not two. A record that carries no `owner_started` is one a writer that has no
    # `ps -o lstart=` line to give wrote — it has only epoch seconds, in `started=` — and reporting
    # that as "this pid no longer looks like the one that took the lock" is a sentence about every
    # lock such a writer holds that a person could act on and should not have.
    case "$(clawdline_suite_lock_identity_verdict "$(clawdline_suite_lock_field owner_pid "$file")" "$pid_started")" in
      same)
      # Everything a person needs to decide whether to go and ask: who, how long, what they say they
      # are doing, and when anything last actually compiled. None of it moves the lock.
        clawdline_suite_lock_evidence="it renewed ${age}s ago against a ${deadline}s deadline; phase $(clawdline_suite_lock_field phase "$file") for $(clawdline_suite_lock_duration "$(( now - $(clawdline_suite_lock_phase_since "$file") ))"), $(clawdline_suite_lock_last_compiling_phrase "$file" "$now"); working: $(clawdline_suite_lock_field work "$file"), compilers: $(clawdline_suite_lock_field compilers "$file")" ;;
      different)
      # A fresh renewal outranks a pid, always. The pid is reported because it is useful to a
      # person, never because it decides anything.
        clawdline_suite_lock_evidence="it renewed ${age}s ago against a ${deadline}s deadline, so something is still proving it is there, though pid $pid no longer looks like the process that took the lock" ;;
      *)
        clawdline_suite_lock_evidence="it renewed ${age}s ago against a ${deadline}s deadline; its record carries no readable start identity for pid $pid, so that axis says nothing either way" ;;
    esac
    return 0
  fi
  # The holder has stopped proving it is alive. That admits nobody on its own: the backstop is
  # physical, and it is never waived.
  clawdline_suite_lock_probe_compilers; probe_status=$?
  case "$probe_status" in
    1) clawdline_suite_lock_state="stale"
       clawdline_suite_lock_evidence="its last renewal was ${age}s ago, past its own ${deadline}s deadline, and no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is running anywhere on this machine" ;;
    0) clawdline_suite_lock_state="orphaned"
       clawdline_suite_lock_evidence="its last renewal was ${age}s ago, past its own ${deadline}s deadline, but $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is still running as pid(s) ${clawdline_suite_lock_compilers}— an orphaned compile is still spending the memory this lock rations, so nobody is admitted and nothing here will kill them" ;;
    *) clawdline_suite_lock_state="unknown"
       clawdline_suite_lock_evidence="its last renewal was ${age}s ago, but the $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN probe could not answer, so whether a compile is running is unknown — and unknown blocks" ;;
  esac
  return 0
}

clawdline_suite_lock_release_gate() {
  local gate=$1
  if [ "$(clawdline_suite_lock_field pid "$gate/holder.txt")" = "$$" ]; then
    rm -rf "$gate"
  fi
}

clawdline_suite_lock_take_over() {
  # The compare and the swap.
  #
  # `rename(2)` decides which of several waiters that judged the *same* lock stale gets to remove it:
  # exactly one `mv` can succeed and the losers fail with ENOENT. That alone is not enough, because a
  # waiter's judgement can be older than a whole takeover — B reads a stale record; A takes over,
  # acquires and starts compiling; B then renames A's *fresh* lock away and both are inside. So the
  # judgement is made again here, under a gate directory only one waiter can hold.
  #
  # **What the gate does and does not do, corrected.** It used to say here that no new holder can
  # appear between the compare and the swap, because the only thing that removes the lock directory
  # is this function. That is false and the next person to edit this would have trusted it:
  # `clawdline_release_suite_lock` also removes it, and the `mkdir` in `clawdline_acquire_suite_lock`
  # is gated by nothing — so while a waiter holds the gate, the judged holder can release, a fresh
  # run can acquire and write its own record, and this `mv` can rename that fresh lock away.
  #
  # What actually closes it is the pair of lines the gate wraps: the re-read, which requires the
  # record to still be `stale` and so cannot be satisfied by a holder that is beating, and the token
  # compare, which requires it to still be the *same* record. Between reading the token and the `mv`
  # there is a residual window no shell construct can remove; it is milliseconds wide and needs a
  # release plus a whole acquisition inside it. The gate's real job is narrower and still worth
  # having: it stops several waiters running the re-read and the swap over each other.
  #
  # The token compare is only a compare when both sides have a token. Against a record written by
  # `build.sh`, or by the broker lease while it existed, it used to be `"" = ""` — always true, with
  # the re-read carrying the whole swap alone. Every writer now writes one, which is what the record
  # contract above `clawdline_suite_lock_write_record` is for.
  local lock=$1 judged_token=$2
  local gate="$lock.takeover" gate_pid gate_verdict stale
  if ! mkdir "$gate" 2>/dev/null; then
    gate_pid=$(clawdline_suite_lock_field pid "$gate/holder.txt")
    # **Three answers here too, and this was the last two-valued reading in the block.**
    # The two-valued reader this used to call could not tell a dead process from a `ps` that
    # would not answer, and under the machine state this lock exists for — load in the sixties, swap full —
    # that is the reading most likely to fail. Reading a failed `ps` as "its taker died" let a
    # second waiter clear a gate whose holder was alive and about to swap.
    #
    # An *empty or non-numeric* `pid` is a different fact and keeps its old answer: the gate was
    # created and its record never written, so there is no process to ask about and nobody is
    # holding it. Leaving that one uncleared would be a deadlock — no waiter could ever take over
    # again — which is why it is named rather than folded into `unknown`.
    case "$gate_pid" in
      "" | *[!0-9]*) gate_verdict="unowned" ;;
      *) gate_verdict=$(clawdline_suite_lock_pid_verdict "$gate_pid") ;;
    esac
    case "$gate_verdict" in
      gone | unowned)
        # Its taker died between creating the gate and removing it. Exactly one waiter may clear
        # it, for the same reason and by the same means as above.
        if mv "$gate" "$gate.abandoned.$$" 2>/dev/null; then rm -rf "$gate.abandoned.$$"; fi ;;
    esac
    return 1
  fi
  printf 'pid=%s\n' "$$" > "$gate/holder.txt"
  # And confirm the gate is still this waiter's before acting on it: if another waiter cleared it as
  # abandoned in the window just above, two could be holding it, and whichever no longer reads its
  # own pid backs out rather than swapping.
  gate_pid=$(clawdline_suite_lock_field pid "$gate/holder.txt")
  if [ "$gate_pid" != "$$" ]; then
    return 1
  fi
  clawdline_suite_lock_admission "$lock"
  if [ "$clawdline_suite_lock_state" = "stale" ] &&
     [ "$(clawdline_suite_lock_field token "$lock/holder.txt")" = "$judged_token" ]; then
    stale="$lock.stale.$$"
    if mv "$lock" "$stale" 2>/dev/null; then
      rm -rf "$stale"
      echo "suite lock: took over $lock — $clawdline_suite_lock_evidence"
      clawdline_suite_lock_release_gate "$gate"
      return 0
    fi
  fi
  clawdline_suite_lock_release_gate "$gate"
  return 1
}

clawdline_suite_lock_start_renewer() {
  # The proof of life, and it is bound to the work rather than to itself: on every tick it checks
  # that the run's own shell is still there and is still the same process, and that the lock is
  # still this run's, before it refreshes anything. That is what stops the renewer from becoming the
  # next sentinel — orphan it and it stops renewing on its first tick instead of holding the machine
  # for the rest of its natural life.
  local lock=$1
  (
    # **A supervisor, not a timer.** `while :; do touch beat; sleep 60; done` would be the same
    # defect in new clothes: it keeps beating after the work it stood for has died, which is what a
    # `sleep 14400` recorded as a holder did tonight. Every tick below re-checks the run it is
    # supervising — the shell is still there, it is still the same process, the lock is still that
    # run's — and exits the moment any of those stops being true. The heartbeat therefore says
    # "somebody is still watching this work", not "a timer is still running on this machine".
    #
    # A subshell inherits this script's EXIT trap — measured, not assumed — and running the release
    # from here would free the lock the moment the renewer stopped. Drop it first.
    trap - EXIT
    #
    # **What may stop this loop, and it is a short list.** Only *positive* evidence that this run
    # no longer owns the lock: the run's own shell is provably gone, its pid provably belongs to a
    # different process now, the record provably carries somebody else's token, or there is no
    # lock directory at all. A probe that could not answer is `unknown`, and unknown costs a tick,
    # never the lock — the same rule the readers above already follow, applied here at last.
    #
    # It was the other way round and it was reproduced: three of the four conditions were
    # *readings*, each `|| exit 0` on a single unretried sample, and one `ps` broken for two
    # seconds ended the beat permanently while the run went on compiling. Sixty seconds later a
    # second run took the lock over and both were inside the guarded section. The machine state
    # that makes `ps` fail — load in the sixties, swap full, Jetsam active — is the exact state
    # this lock exists for, so that sample is least reliable precisely when it matters most.
    #
    # And when it does stop, it says so on stderr. A silent renewer leaves the run holding a lock
    # it has stopped defending with nothing in the log to say when that began.
    local verdict stop_reason="" unwritten=0 unreadable=""
    while :; do
      sleep "$CLAWDLINE_SUITE_LOCK_RENEW_SECONDS"
      unreadable=""
      verdict=$(clawdline_suite_lock_pid_verdict "$clawdline_suite_lock_pid")
      case "$verdict" in
        gone) stop_reason="the run it renews for (pid $clawdline_suite_lock_pid) is gone" ;;
        unknown) unreadable="this machine could not say whether pid $clawdline_suite_lock_pid is still there" ;;
        alive)
          verdict=$(clawdline_suite_lock_identity_verdict "$clawdline_suite_lock_pid" "$clawdline_suite_lock_pid_started")
          case "$verdict" in
            different) stop_reason="pid $clawdline_suite_lock_pid is a different process now, so this run has ended" ;;
            unknown) unreadable="this machine could not read the start time of pid $clawdline_suite_lock_pid" ;;
          esac ;;
      esac
      if [ -z "$stop_reason" ]; then
        verdict=$(clawdline_suite_lock_ownership_verdict "$lock" "$clawdline_suite_lock_token")
        case "$verdict" in
          theirs) stop_reason="$lock now records somebody else's token, so it has changed hands" ;;
          absent) stop_reason="$lock is gone, so there is no longer a lock for this run to hold" ;;
          unknown) unreadable="${unreadable:+$unreadable; }$lock/holder.txt carries no readable token, which proves nothing about who holds it" ;;
        esac
      fi
      if [ -n "$unreadable" ] && [ -z "$stop_reason" ]; then
        echo "suite lock: renewal evidence unreadable this tick — $unreadable. Unknown blocks: still holding, still beating." >&2
      fi
      if [ -n "$stop_reason" ]; then
        echo "suite lock: renewal stopped — $stop_reason" >&2
        # And leave it where the run can find it. Saying it on stderr is not telling the run: the
        # run is inside `swiftc` or the test binary and reads nothing, and the one ownership
        # confirmation it makes is between the two. A note it can pick up there is the difference
        # between a proof of life that stopped and a proof of life that stopped unnoticed.
        printf '%s\n' "$stop_reason" > "$CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE" 2>/dev/null || true
        exit 0
      fi
      # Its own pid comes from the file the shell below writes, because bash 3.2 has no `BASHPID`
      # and every way of asking for it from in here forks something whose pid is not this one.
      if clawdline_suite_lock_write_record "$lock" "$(cat "$lock/.renewer" 2>/dev/null || true)"; then
        unwritten=0
      else
        # A record this loop could not write is a tick lost, not a lock given up. It keeps trying,
        # and it says so once the silence is long enough that a waiter could act on it: past the
        # deadline the record itself declares, another run may legitimately judge this one stale.
        unwritten=$(( unwritten + CLAWDLINE_SUITE_LOCK_RENEW_SECONDS ))
        echo "suite lock: could not refresh $lock/holder.txt (${unwritten}s of this run's proof of life is missing); still holding, still trying" >&2
      fi
    done
  ) &
  clawdline_suite_lock_renewer=$!
  printf '%s' "$clawdline_suite_lock_renewer" > "$lock/.renewer" 2>/dev/null || true
}

clawdline_acquire_suite_lock() {
  local lock="$CLAWDLINE_SUITE_LOCK_DIR"
  local started_at now waited next_notice holder_name holder_pid holder_worker holder_started judged_token
  started_at=$(date +%s)
  next_notice=0
  while :; do
    if mkdir "$lock" 2>/dev/null; then
      clawdline_suite_lock_token=$(clawdline_suite_lock_new_token)
      clawdline_suite_lock_pid=$$
      clawdline_suite_lock_started=$(date '+%Y-%m-%d %H:%M:%S')
      clawdline_suite_lock_pid_started=$(clawdline_suite_lock_pid_identity "$$")
      # Written before the first record rather than through `clawdline_suite_lock_phase`, which
      # would rewrite a record that does not exist yet.
      printf 'idle-holding %s\n' "$(date +%s)" > "$lock/.phase" 2>/dev/null || true
      if ! clawdline_suite_lock_write_record "$lock"; then
        echo "suite lock: could not write $lock/holder.txt — refusing to compile behind a lock that says nothing about who holds it" >&2
        # Give the directory back, but only while it is still the record-less one this run made a
        # moment ago. Nobody else can legitimately be in it: the only rule that hands on a
        # directory with no record requires it to be older than a whole renewal deadline, which
        # one created milliseconds ago is not. The guard is here so that the reasoning is visible
        # rather than remembered — an unconditional `rm -rf` on this path removes whatever is
        # there, and what is there is only this run's by argument.
        if [ ! -f "$lock/holder.txt" ]; then rm -rf "$lock"; fi
        return "$CLAWDLINE_SUITE_LOCK_BUSY"
      fi
      clawdline_suite_lock_start_renewer "$lock"
      echo "suite lock: $lock is this run's (pid $$), renewed every ${CLAWDLINE_SUITE_LOCK_RENEW_SECONDS}s against a ${CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS}s deadline"
      return 0
    fi
    now=$(date +%s)
    waited=$(( now - started_at ))
    holder_name=$(clawdline_suite_lock_field holder "$lock/holder.txt")
    # Both numbers, because they answer different questions: `owner_pid` is the run, which is who to
    # go and ask, and `pid` is whatever of its processes is working at this heartbeat.
    holder_pid=$(clawdline_suite_lock_field owner_pid "$lock/holder.txt")
    holder_worker=$(clawdline_suite_lock_field pid "$lock/holder.txt")
    holder_started=$(clawdline_suite_lock_field started "$lock/holder.txt")
    judged_token=$(clawdline_suite_lock_field token "$lock/holder.txt")
    clawdline_suite_lock_admission "$lock"
    if [ "$clawdline_suite_lock_state" = "stale" ]; then
      if clawdline_suite_lock_take_over "$lock" "$judged_token"; then
        continue
      fi
    fi
    if [ "$waited" -ge "$CLAWDLINE_SUITE_LOCK_WAIT_SECONDS" ]; then
      echo "suite lock: gave up after ${waited}s. $lock is held by ${holder_name:-an unnamed run} (run pid ${holder_pid:-unknown}, working pid ${holder_worker:-none}, started ${holder_started:-unknown}) — $clawdline_suite_lock_evidence." >&2
      echo "suite lock: nothing was compiled and nothing was killed. Read $lock/holder.txt and ask that run." >&2
      return "$CLAWDLINE_SUITE_LOCK_BUSY"
    fi
    if [ "$waited" -ge "$next_notice" ]; then
      echo "suite lock: waiting ${waited}s for ${holder_name:-an unnamed run} (run pid ${holder_pid:-unknown}, working pid ${holder_worker:-none}, started ${holder_started:-unknown}) — $clawdline_suite_lock_evidence"
      next_notice=$(( waited + CLAWDLINE_SUITE_LOCK_NOTICE_SECONDS ))
    fi
    sleep "$CLAWDLINE_SUITE_LOCK_POLL_SECONDS"
  done
}

clawdline_confirm_suite_lock() {
  # Called once between the compile and the run. The compile is the long unattended stretch, and a
  # run that lost the lock during it must not start a second expensive thing under somebody else's.
  #
  # **It confirms two things, not one.** "The record still carries this run's token" is the lock
  # not having changed hands. It is not the same as this run still *proving* it holds it: the
  # renewer is one background subshell, and if it stops — killed with the process group, or ended
  # by a stop condition — the token sits there unchanged while the beat goes still. The run then
  # spends the whole test binary inside the guarded section with nothing renewing, and after one
  # deadline another run may legitimately judge this lock stale and take it. That is the same two
  # runs in the guarded section the renewer's own three-answer rule was written to prevent,
  # arrived at from the other end, so this is the second thing it asks.
  local lock="$CLAWDLINE_SUITE_LOCK_DIR" stopped renewer_pid
  if [ -f "$CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE" ]; then
    stopped=$(cat "$CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE" 2>/dev/null) || stopped=""
    echo "suite lock: the renewal loop stopped during the guarded section — ${stopped:-no reason recorded}" >&2
    rm -f "$CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE" 2>/dev/null || true
  fi
  if [ -z "$clawdline_suite_lock_token" ] ||
     [ "$(clawdline_suite_lock_field token "$lock/holder.txt")" != "$clawdline_suite_lock_token" ]; then
    echo "suite lock: $lock is no longer this run's — refusing to start the test binary under somebody else's lock." >&2
    return "$CLAWDLINE_SUITE_LOCK_BUSY"
  fi
  # Still this run's lock. Now: is anything still saying so? `jobs -p` rather than `ps`, for the
  # same reason the exit trap uses it — a renewer that exited is reaped and its number is reusable,
  # so asking the machine about the number answers about whoever has it now.
  if [ -n "$clawdline_suite_lock_renewer" ] &&
     jobs -p 2>/dev/null | grep -qx "$clawdline_suite_lock_renewer"; then
    return 0
  fi
  # The lock is this run's and nothing is renewing it. Nothing is killed and nothing is given up:
  # a fresh renewer is started, because the alternative — throwing away a compile that has already
  # been paid for — is worse than resuming the beat under a lock this run demonstrably still holds.
  renewer_pid="${clawdline_suite_lock_renewer:-none}"
  echo "suite lock: $lock is still this run's but its renewer (${renewer_pid}) is gone — restarting the proof of life before the test binary starts." >&2
  clawdline_suite_lock_start_renewer "$lock"
  return 0
}

clawdline_suite_lock_work_finished() {
  # The positive half of the protocol: the guarded work is over, said in a way a waiter can read
  # even if this shell never reaches its release. Best effort by construction — a run killed with
  # SIGKILL writes nothing, and the readers know that absence proves nothing.
  : > "$CLAWDLINE_SUITE_LOCK_DONE_FLAG" 2>/dev/null || true
}

clawdline_release_suite_lock() {
  # Ownership-checked, and this is the answer to the first of the two `nohup` mistakes: an outer
  # shell that writes `trap 'rmdir "$LOCK"' EXIT`, backgrounds the suite and returns fires that trap
  # immediately, while the run it started is still compiling. Its pid is not the pid in the record
  # and it never held the token, so its release is a no-op that says so instead of freeing a lock
  # somebody is working behind.
  local lock="$CLAWDLINE_SUITE_LOCK_DIR" recorded_pid recorded_token
  case "$lock" in "" | "/") return 0 ;; esac
  [ -d "$lock" ] || return 0
  recorded_pid=$(clawdline_suite_lock_field owner_pid "$lock/holder.txt")
  recorded_token=$(clawdline_suite_lock_field token "$lock/holder.txt")
  if [ -n "$clawdline_suite_lock_token" ] &&
     [ "$recorded_pid" = "$$" ] &&
     [ "$recorded_token" = "$clawdline_suite_lock_token" ]; then
    rm -rf "$lock"
    echo "suite lock: released $lock"
  elif [ "$recorded_pid" = "$$" ]; then
    # The pid matches and the token does not, which is the case the token exists for: this lock is
    # not the one this run acquired, whatever the number in it says. A pid is reused within hours on
    # a busy machine, and the record is rewritten by whoever holds it now.
    echo "suite lock: $lock records pid $$ but not this run's token — it has changed hands since this run took it, so it is left alone" >&2
  else
    echo "suite lock: $lock is held by pid ${recorded_pid:-unknown}, not by this shell (pid $$) — left alone" >&2
  fi
  # Always 0. This runs from the EXIT trap under `set -e`, where a non-zero return would replace the
  # status the run was actually exiting with.
  return 0
}

clawdline_suite_exit_cleanup() {
  local status=$?
  # The only process this script ever signals is the renewal loop it started for itself. The lock
  # signals nobody else's, ever — **and that is checked at the moment of signalling rather than
  # asserted here.** The renewer can exit long before this trap runs; bash then reaps it and the
  # number becomes reusable, so a bare `kill "$pid"` at the end of a long run can send SIGTERM to
  # a stranger who happens to have inherited it. `jobs -p` lists only this shell's own live jobs:
  # a reaped renewer is not in it, and neither is whoever took its number.
  # `wait` as well as `kill`, and not for tidiness: without it bash reports the job asynchronously
  # as `Terminated: 15` on stderr of every single run, and waiting also guarantees the renewer is
  # reaped before the release below rather than possibly writing a record after it.
  if [ -n "$clawdline_suite_lock_renewer" ]; then
    if jobs -p 2>/dev/null | grep -qx "$clawdline_suite_lock_renewer"; then
      kill "$clawdline_suite_lock_renewer" 2>/dev/null || true
      wait "$clawdline_suite_lock_renewer" 2>/dev/null || true
    fi
    clawdline_suite_lock_renewer=""
  fi
  rm -f "$CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE" 2>/dev/null || true
  clawdline_release_suite_lock
  # Bash keeps exactly one EXIT trap, so a second `trap … EXIT` further down would silently replace
  # this one and leave the lock behind on every run. The `$STORE` cleanup that used to have a trap
  # of its own is composed here instead. `${STORE:-}` because the store is created after this trap
  # is installed: a run that dies in the compile has none to remove.
  if [ -n "${STORE:-}" ]; then rm -rf "$STORE"; fi
  return "$status"
}

clawdline_suite_lock_default_holder() {
  # Who to ask, not just what to blame. A terminal identity is worth more here than a username.
  local who where
  who="${USER:-$(id -un 2>/dev/null || echo unknown)}"
  where=$(tty 2>/dev/null) || where=""
  case "$where" in "" | "not a tty") where="no tty" ;; esac
  if [ -n "${ITERM_SESSION_ID:-}" ]; then
    where="iTerm2 ${ITERM_SESSION_ID#*:}"
  elif [ -n "${TMUX_PANE:-}" ]; then
    where="tmux pane ${TMUX_PANE}"
  fi
  printf '%s running ./test.sh (%s)' "$who" "$where"
}

clawdline_suite_lock_tree() {
  # The exact tree being verified. `--no-optional-locks` because this checkout is shared with other
  # sessions and an ordinary `git status` refreshes the index they are also using.
  local tree dirty
  tree=$(git rev-parse 'HEAD^{tree}' 2>/dev/null) || tree=""
  if [ -z "$tree" ]; then printf 'unknown (not a git checkout)'; return 0; fi
  dirty=$(git --no-optional-locks status --porcelain 2>/dev/null | head -1) || dirty=""
  if [ -n "$dirty" ]; then printf '%s (working tree dirty)' "$tree"; else printf '%s' "$tree"; fi
}

CLAWDLINE_SUITE_LOCK_HOLDER="${CLAWDLINE_SUITE_LOCK_HOLDER:-$(clawdline_suite_lock_default_holder)}"
CLAWDLINE_SUITE_LOCK_TREE="${CLAWDLINE_SUITE_LOCK_TREE:-$(clawdline_suite_lock_tree)}"
# The note says out loud the thing the mechanism above already enforces, because whoever reads this
# file is usually reading it at the moment they are tempted to remove the lock by hand. A gap with
# no compiler running does **not** mean nobody holds it: one run is a sequence of expensive steps
# and the gaps between them are part of the hold.
CLAWDLINE_SUITE_LOCK_NOTE="${CLAWDLINE_SUITE_LOCK_NOTE:-running ./test.sh; output is going to $LOG. This lock covers a whole sequence of expensive steps, so a moment with no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN running does not mean it is free. It is handed on only when the heartbeat above has expired, or when $CLAWDLINE_SUITE_LOCK_DONE_FLAG exists, and in both cases only while no $CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN is running anywhere. If you think it is stuck, ask the run named above rather than removing this directory.}"

trap clawdline_suite_exit_cleanup EXIT
clawdline_acquire_suite_lock || exit $?
# <<< clawdline suite lock <<<

BIN="${TMPDIR:-/tmp}/clawdline-tests"

required_cloud_test_files=(
  Tests/CloudEnvelopeTests.swift
  Tests/CloudAccountTests.swift
  Tests/CloudTransportTests.swift
  Tests/CloudAppBridgeTests.swift
  Tests/CloudSettingsTests.swift
  Tests/ScheduleResumeTests.swift
  Tests/CloudClockTests.swift
  Tests/CloudCanonicalJSONTests.swift
  Tests/CloudCommandLedgerTests.swift
  Tests/CloudOutboundSpoolTests.swift
  Tests/CloudPairingTests.swift
  Tests/CloudLifecycleTests.swift
)
for required_cloud_test_file in "${required_cloud_test_files[@]}"; do
  if ! test -f "$required_cloud_test_file"; then
    echo "required Cloud test suite is missing: $required_cloud_test_file" >&2
    exit 1
  fi
done

# From here to the end of the test-binary run is what the lock is for, and the record says so while
# it happens: a waiter reading `phase=compiling` knows the lock is protecting something, and one
# reading `phase=idle-holding` knows to go and ask rather than to guess.
# >>> clawdline compile ceiling >>>
# How many compiler jobs this run may have, and where that number came from.
#
# **This block sits below `clawdline_acquire_suite_lock` on purpose, and that placement is the
# rule rather than a habit: a ceiling above the lock is a ceiling nothing rations.** It governs one
# invocation — the `swiftc` a few lines down, inside the lock — and the code above the lock cannot
# read it, because up there it does not exist yet.
#
# It briefly did. `b8dfd0ff` moved this block to the top of the script and exported the number so
# that the whole-`Sources/` typecheck inside `Tests/keychain-rebuild-focused.mjs` could share it,
# which took that typecheck from 34 s to 7 s. **That typecheck runs outside the lock**, and the
# comment beside the lock markers explains why the two focused suites were left there: their
# compiles are seconds long, and moving the lock above them would make every queued run wait out a
# dozen unrelated node suites. Seconds-long is what made that trade defensible, and eight-way
# parallel is not seconds-long in the only sense that matters here — **it is eight `swift-frontend`
# processes competing with whoever currently holds the lock.** The boundary was written down as a
# fact and not as an intent, so it read as an opportunity to the next person past it. That was me.
#
# So: `CLAWDLINE_SUITE_JOBS` is the injection point for the locked compile and for nothing else —
# a ceiling, floor of one, so low headroom means a slower compile rather than a slot that never
# comes. Anything compiling above the lock passes `-j 1` explicitly, in its own file, next to the
# reason. `build.sh` already had this shape: its ceiling block sits below `clawdline_lease_acquire`.
#
# **Unset used to add no flag at all, and that was deliberate** — the driver's own default here is
# one job, measured twice against this exact invocation: 7,479 samples at 54 ms from a
# `proc_listpids` walker over the driver's own descendants, and 426 independent
# `ps -Ao pid=,ppid=,ucomm=` samples at 250 ms. The same instruments read 8 when `-j 8` was
# passed, so they can count above one. That measurement still holds. What made one job the right
# *default* was a memory limit that has since gone: `Tests/CloudAccountTests.swift` reached
# 46.06 GiB in a single frontend, multiplying that by N was not survivable on a 24 GB machine, and
# Jetsam force-rebooted this Mac twice on 2026-09-03 proving it. `a97fb176` split that function
# into twenty-eight coroutines and took the file to 0.83 GiB. The limit went and the decision it
# justified stayed, which is the only reason this line was still one job.
#
# Re-measured on 2026-09-03 after the split. All five values on one detached worktree pinned at
# `d97d0afb` so every N compiled identical bytes, `/tmp/clawdline-suite.lock` held across the whole
# sweep, 152 files, footprint from `proc_pid_rusage(RUSAGE_INFO_V4)`'s
# `ri_lifetime_max_phys_footprint` with `/usr/bin/time -l` beside it as a second reading:
#
#     -j  1   102 s    one frontend's peak 0.845 GiB    most alive together 0.861 GiB
#     -j  2    53 s                        0.822                            0.949
#     -j  4    34 s                        0.852                            1.028
#     -j  8    24 s                        0.835                            1.181
#     -j 14    18 s                        0.833                            2.012
#
# **The per-frontend peak does not move with N.** It is one file and it costs what it costs
# whoever compiles it. What grows is how many are alive at once, and it grows far slower than N,
# because the driver hands each frontend a batch and the expensive file is only ever in one of
# them: eight jobs cost 1.18 GiB together, not eight times 0.85.
#
# So the default is derived rather than absent: `min(8, hw.ncpu)`, floor one. **Both terms carry
# weight, and both stand on the table above rather than on anything elsewhere in the tree.**
#
# 8 rather than 14, for two reasons and neither is taste. Fourteen buys six seconds over eight and
# spends every core on the machine to do it, which is the difference between a compile somebody can
# work through and one they cannot. And the safety margin is not the same: headroom here with
# nothing compiling — `vm_stat` free plus file-backed — was 4,485 MB, against which eight jobs'
# worst case of 2.52 GiB clears by 1.9 GiB and fourteen's 3.53 GiB clears by under one. That worst
# case is the N largest lifetime maxima summed as though they had all been alive at the same
# instant, which the batching makes impossible; it is the number to plan against precisely because
# it is the one that does not depend on the batching continuing to behave.
#
# `hw.ncpu` rather than a constant, because CI is a `macos-14` runner with a fraction of this Mac's
# cores, and a number measured on one machine is not a constant. This file has been wrong about
# that before: `getconf PAGESIZE` here is 16,384, and reasoning from Intel's 4,096 turned one page
# into four blocks and voided a day of arithmetic.
clawdline_suite_jobs_flags=()
case "${CLAWDLINE_SUITE_JOBS:-}" in
  "")
    # `sysctl` failing, or answering something that is not a count, reads as one job rather than
    # as no ceiling. The floor is the safe direction and it is also the old behaviour.
    clawdline_compile_jobs=$(sysctl -n hw.ncpu 2>/dev/null) || clawdline_compile_jobs=""
    case "$clawdline_compile_jobs" in
      "" | *[!0-9]* | 0*) clawdline_compile_jobs=1 ;;
    esac
    if [ "$clawdline_compile_jobs" -gt 8 ]; then clawdline_compile_jobs=8; fi
    clawdline_compile_jobs_source="min(8, hw.ncpu); CLAWDLINE_SUITE_JOBS unset" ;;
  # `0*` and not just `0`: `00` is all digits, so it slipped past `*[!0-9]*` and reached `swiftc` as
  # `-j 00`. The contract this guard states is "a positive whole number", and `00` and `007` are not
  # that however they behave downstream. The three patterns are: anything with a non-digit in it, a
  # leading zero of any length, and nothing at all.
  *[!0-9]* | 0*)
    echo "test.sh: CLAWDLINE_SUITE_JOBS='${CLAWDLINE_SUITE_JOBS}' is not a positive whole number of jobs." >&2
    exit 2 ;;
  *)
    clawdline_compile_jobs=$CLAWDLINE_SUITE_JOBS
    clawdline_compile_jobs_source="from CLAWDLINE_SUITE_JOBS" ;;
esac
clawdline_suite_jobs_flags=(-j "$clawdline_compile_jobs")
# Set, deliberately not exported. A child that inherited this number would be a child compiling at
# this width outside the lock, which is the whole of what went wrong.
echo "test.sh: compile job ceiling: ${clawdline_compile_jobs}, ${clawdline_compile_jobs_source}"
# <<< clawdline compile ceiling <<<

clawdline_suite_lock_phase compiling
swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  ${clawdline_suite_jobs_flags[@]+"${clawdline_suite_jobs_flags[@]}"} \
  -o "$BIN" \
  "${clawdline_library_sources[@]}" \
  "${clawdline_test_sources[@]}" \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation -framework Network

# Between the two halves of the guarded section. The compile is the long unattended stretch, so this
# is where a lock that changed hands underneath the run has to be noticed — before the second
# expensive thing starts.
clawdline_confirm_suite_lock || exit $?
clawdline_suite_lock_phase analysing

# `if` rather than a bare assignment: under `set -e` a failing command on the right-hand side
# ends the script right there, before what it captured has been printed — so a red suite exited
# 1 with nothing on screen at all, which is worse than having no guard.
# The suite pairs devices, so point the store somewhere disposable. Without this a test run
# writes into whoever's real ~/.config/clawdline is on the machine — see RemoteAuth.directory.
STORE="${TMPDIR:-/tmp}/clawdline-test-store-$$"
mkdir -p "$STORE"
# No `trap 'rm -rf "$STORE"' EXIT` here any more, and that is not an omission. Bash keeps exactly
# one EXIT trap, so this line silently replaced the suite lock's — installing it left the machine
# lock behind on every single run. The removal is composed into `clawdline_suite_exit_cleanup`
# above instead, which reads `${STORE:-}` and so does the right thing whether or not this line has
# been reached.
# The drop cache goes inside it, and that is the same problem with teeth: the suite writes real
# image files through `Drop.store`, and every write prunes the oldest entries away. Unisolated,
# running the tests deletes pictures the person dropped into the bar — see Drop.directory. Spelled
# out at the invocation below rather than held in a variable of its own, because `test-sh-streaming`
# re-runs that block with only `$BIN`, `$STORE` and `$LOG` defined. The binary sets the same
# boundary for itself, so narrowing a failure by running it directly is safe too.

# Streamed through `tee` rather than captured into a variable and echoed at the end.
#
# **Not for the reason it first looked like.** The story here used to be that a crashing binary took
# the captured output with it; that is false and was measured to be false. `if out=$(…)` survives a
# `SIGTRAP` in the binary perfectly well — the `if` keeps the assignment out of `errexit`, the
# substitution reads to EOF, the `echo` runs — and against a Swift binary that prints 500 lines and
# then calls `fatalError`, capture and `tee` left the *same* 439 lines on disk.
#
# Two other things were eating the output, and only one of them is this pipe's business:
#
#   * **stdout was block buffered** at 16384 — the binary's fd 1 is this pipe, and stays this pipe
#     however the caller redirects, because a caller's `> run.log` lands on `tee`'s stdout and not
#     on the binary's. So a crash could swallow most of the suite's own output, by the same amount
#     for everybody. That is fixed in `Tests/TestIsolation.swift`, which now asks for line
#     buffering; both forms lost those lines equally.
#   * **The shell itself gets killed from outside** — an agent harness timeout, a cancelled CI job,
#     Ctrl-C, the OOM killer. There is no `echo` in that story at all. Measured: killed at 0.45s,
#     `tee` had 219 lines on disk and the captured form had none. On a machine where half a dozen
#     sessions run this suite under harnesses that impose timeouts, that is the common case.
#
# So the log survives the process that wrote it, which is why its path is outside `$STORE` and is
# printed when the suite fails. Copy it somewhere before the temporary directory goes.
# **The status has to come from the binary, and `pipefail` will not give it to you.** With
# `set -o pipefail` a pipeline reports its rightmost non-zero member, so a `tee` that cannot write
# — a full disk, a read-only `TMPDIR` — would be reported as the suite's own exit code and a green
# suite would look red, or a red one would exit with the wrong number. `PIPESTATUS` names each
# member, so both are read and neither is inferred. `set +e` around the pipeline rather than an
# `if`, because `PIPESTATUS` must be read from the pipeline itself and any command in between,
# `if` included, is a chance to have replaced it.
# `$LOG` is set with the suite lock above, which records the path so a blocked run can watch this
# one; there is one definition and it is that one.
set +e
CLAWDLINE_REMOTE_DIR="$STORE" CLAWDLINE_DROPS_DIR="$STORE/drops" "$BIN" Resources/mascots 2>&1 | tee "$LOG"
# Copied whole, in one assignment. Reading the members one at a time does not work and does not
# look broken: the first assignment is itself a command, so it replaces `PIPESTATUS` with its own
# one-element status, and the second read is of an array that no longer has a second member —
# `unbound variable` under `set -u`, on a green suite, at the very end. Measured here.
pipe=("${PIPESTATUS[@]}")
status=${pipe[0]}
tee_status=${pipe[1]}
set -e
if [ "$status" -ne 0 ]; then
  echo "the suite exited $status — full output kept at $LOG" >&2
  # The receipt check below is never reached on a red run, so the one thing that would notice a
  # shrunken total is unreachable exactly when it would help. This says it here instead.
  report_receipt_direction "$LOG"
  exit "$status"
fi
# A `tee` that could not write has to end the run on its own number, and it has to do it *here*.
# Warning and carrying on was worse than it looked: the receipt check below reads `$LOG`, which is
# the file tee just failed to write, so a **green** suite ended as `exit 125, missing receipt`
# pointing at a path that does not exist — a false red, wearing the costume of the thing this whole
# change exists to remove. 126 rather than 125 so the two are told apart on sight.
if [ "$tee_status" -ne 0 ]; then
  echo "tee exited $tee_status writing $LOG, so the receipt below cannot be checked." >&2
  echo "The suite itself passed; the terminal above is the whole record." >&2
  exit 126
fi

# The guarded section is over: the compile and the run are both behind us and only receipt checking
# is left, so the next run may come in without waiting out a renewal deadline nobody is renewing
# against. It sits below the two `exit` branches rather than above them, because those branches
# leave through the EXIT trap, which releases the lock properly — and because everything between
# `set +e` and the last `fi` above is lifted out and executed by `Tests/test-sh-streaming.mjs`,
# where this function does not exist.
clawdline_suite_lock_phase idle-holding
clawdline_suite_lock_work_finished

# A zero process status is insufficient: removing dispatchMain() lets top-level code return before
# either async suite or the final result path runs. Require the receipt emitted only by that path,
# with full-suite counts so a targeted-case environment cannot make CI green either.
verify_test_completion_receipts "$LOG"
rm -f "$LOG"
