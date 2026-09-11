#!/bin/bash
# Compile and run the test binary.
#
# Sources/main.swift is excluded: it is top-level code that starts the app, and two
# entry points cannot live in one binary. Everything else compiles in, so the tests
# exercise the same code the app ships rather than a copy of it.
set -euo pipefail

# Opt in with one stable question id. The wrapper computes the repository, exact subject, command
# and environment digests from one canonical recipe, reserves the run before this script acquires
# the compile lock, and completes the same receipt afterwards. The inner marker prevents recursion.
if [ -n "${CLAWDLINE_VERIFY_QUESTION_ID:-}" ] && [ -z "${CLAWDLINE_VERIFICATION_INNER:-}" ]; then
  exec node tools/verified-test-run.mjs "$0" "$@"
fi

cloud_receipt_prefix='CLAWDLINE_CLOUD_TESTS_COMPLETE'
cloud_suite_roster='CloudEnvelope,CloudAccount,CloudTransport,CloudAppBridge,CloudSettings,ScheduleResume,CloudClock,CloudCanonicalJSON,CloudCommandLedger,CloudOutboundSpool,CloudPairing,CloudLifecycle'
# Completion counts are observations from this run, not source-controlled expectations. Keeping
# the previous tree's totals in this file made every legitimate assertion change require a
# measurement full, a source rewrite, and a second identical full. The runtime receipt still
# records the observed totals; completeness is proved structurally by the ordered Swift group
# manifest and by this Cloud roster validation.
cloud_receipt_lines() {
  awk -v token="$cloud_receipt_prefix " 'substr($0, 1, length(token)) == token' "$1"
}

validate_cloud_completion_receipt() {
  node -e '
    const [line, expectedRoster] = process.argv.slice(1);
    const match = /^CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=([1-9][0-9]*) suites=(.+)$/.exec(line);
    if (!match) process.exit(1);
    const entries = match[2].split(",");
    if (entries.length !== Number(match[1])) process.exit(2);
    const names = new Set();
    for (const entry of entries) {
      const pair = /^([A-Za-z][A-Za-z0-9]*):([1-9][0-9]*)$/.exec(entry);
      if (!pair || names.has(pair[1])) process.exit(3);
      names.add(pair[1]);
    }
    if (entries.map((entry) => entry.split(":")[0]).join(",") !== expectedRoster) process.exit(4);
  ' "$1" "$cloud_suite_roster"
}

report_receipt_direction() {
  local log=$1
  local failed_total
  failed_total=$(awk 'match($0, /^[0-9]+ of [0-9]+ checks failed/) { print $3; exit }' "$log")
  if [ -n "$failed_total" ]; then
    echo "The suite reported failures after attempting $failed_total checks; it is not a complete green receipt." >&2
  else
    echo "The suite ended without one complete Swift success receipt; full output kept at $log" >&2
  fi
}

verify_test_completion_receipts() {
  local log=$1 cloud_lines cloud_count swift_count cloud
  if [ -n "${CLAWDLINE_TEST_GROUPS:-}" ]; then
    echo 'test.sh: focused_run_cannot_verify_full_receipt' >&2
    return 125
  fi
  cloud_lines=$(cloud_receipt_lines "$log")
  cloud_count=$(printf '%s\n' "$cloud_lines" | awk 'NF { c++ } END { print c + 0 }')
  swift_count=$(awk '/^[1-9][0-9]* checks passed$/ { count++ } END { print count + 0 }' "$log")
  if [ "$cloud_count" -ne 1 ]; then
    echo "Cloud test completion receipt appeared $cloud_count times, expected exactly once — full output kept at $log" >&2
    return 125
  fi
  cloud=$(printf '%s\n' "$cloud_lines" | awk 'NF { print; exit }')
  if ! validate_cloud_completion_receipt "$cloud"; then
    echo "Cloud test completion receipt is malformed, internally inconsistent, or names a duplicate suite — full output kept at $log" >&2
    return 125
  fi
  if [ "$swift_count" -ne 1 ]; then
    echo "Swift test completion receipt appeared $swift_count times, expected exactly once — full output kept at $log" >&2
    return 125
  fi
}

emit_complete_test_seal_receipt() {
  local log=$1 cloud swift witness cloud_count swift_count
  if [ -n "${CLAWDLINE_TEST_GROUPS:-}" ]; then
    echo 'test.sh: focused_run_cannot_emit_full_receipt' >&2
    return 125
  fi
  cloud=$(cloud_receipt_lines "$log")
  cloud_count=$(printf '%s\n' "$cloud" | awk 'NF { c++ } END { print c + 0 }')
  swift=$(awk '/^[1-9][0-9]* checks passed$/ { line=$0; count++ } END { if (count == 1) print line }' "$log")
  swift_count=$(awk '/^[1-9][0-9]* checks passed$/ { count++ } END { print count + 0 }' "$log")
  witness=$(cat Tests/*.swift \
    | grep -oE '\b(check|expect)[A-Za-z0-9_]*\(' | wc -l | tr -d '[:space:]' || true)
  if [ "$cloud_count" -ne 1 ] || ! validate_cloud_completion_receipt "$cloud" \
     || [ "$swift_count" -ne 1 ] || [ -z "$witness" ] || [ "$witness" -le 0 ]; then
    echo "test.sh: cannot emit a complete test receipt (cloud=$cloud_count swift=$swift_count witness=${witness:-missing})" >&2
    return 125
  fi
  local receipt
  receipt=$(node -e '
    const [swift, witness, cloud] = process.argv.slice(1);
    process.stdout.write("CLAWDLINE_TEST_SEAL " + JSON.stringify({
      version: 1, outcome: "passed", swift_receipt: swift,
      assertion_sites: Number(witness), cloud_receipt: cloud
    }) + "\n");
  ' "$swift" "$witness" "$cloud")
  printf '%s\n' "$receipt" >> "$log"
  printf '%s\n' "$receipt"
}

is_unfiltered_test_run() {
  [ -z "${CLAWDLINE_TEST_GROUPS:-}" ]
}

# >>> clawdline suite roster >>>
# Every `.mjs` suite in this checkout is either named somewhere below or written down here as a
# deliberate exception. Nothing else compares the roster with the directory, and that gap is not
# hypothetical: `Tests/web-usage-analytics.mjs` and `Tests/web-close-confirm-explanation.mjs` were
# committed, looked tested, and had never once been run when they were found on 2026-09-04.
#
# Naming each suite stays deliberate — a glob would run whatever happened to be in `Tests/` in an
# order nobody chose, and would miss `Resources/web/app/js/net/client.test.mjs`, which lives beside
# the source it tests. So the list stays hand-written and this check is what makes a hand-written
# list answerable to the filesystem.
#
# The allowlist is empty today, which is a fact and not a placeholder: every `.mjs` in this
# checkout runs. Add a path here only with the reason on the line above it, because an exception
# with no reason is indistinguishable from the oversight this guard exists to catch.
suite_roster_allowlist=''

verify_suite_roster() {
  local script on_disk registered allowed unregistered stale
  script=$(basename "$0")
  on_disk=$( { ls Tests/*.mjs 2>/dev/null; find Resources -name '*.test.mjs' 2>/dev/null; } \
    | grep -v '^[[:space:]]*$' | sort -u)
  # Registered means a whole line of this script that *is* the path, optionally preceded by `node`:
  # an invocation, or an entry in the `browser_contract_suites` array. Matching the path anywhere in
  # the file would count a mention in a comment as a registration — and the comment directly above
  # names two suites, so that weaker pattern would report this checkout as fully registered on the
  # day both of them were unregistered.
  registered=$(grep -Eo '^[[:space:]]*(node[[:space:]]+)?"?[A-Za-z0-9._/-]+\.mjs"?[[:space:]]*$' "$script" \
    | sed -E 's/^[[:space:]]*(node[[:space:]]+)?"?//; s/"?[[:space:]]*$//' | sort -u)
  allowed=$(printf '%s\n' "$suite_roster_allowlist" | grep -v '^[[:space:]]*$' | sort -u || true)

  unregistered=$(comm -23 <(printf '%s\n' "$on_disk") \
    <(printf '%s\n%s\n' "$registered" "$allowed" | grep -v '^[[:space:]]*$' | sort -u))
  if [ -n "$unregistered" ]; then
    echo "These .mjs suites are in the checkout and nothing in $script runs them:" >&2
    printf '  %s\n' $unregistered >&2
    echo "Add a \`node <path>\` line for each, or name it in suite_roster_allowlist with its reason." >&2
    return 1
  fi

  # An allowlist entry for a file that no longer exists is the same rot in the other direction: it
  # reads as a considered exception and guards nothing.
  stale=$(comm -13 <(printf '%s\n' "$on_disk") <(printf '%s\n' "$allowed" | grep -v '^[[:space:]]*$' | sort -u))
  if [ -n "$stale" ]; then
    echo "suite_roster_allowlist names files that are not in this checkout:" >&2
    printf '  %s\n' $stale >&2
    return 1
  fi
  return 0
}
# <<< clawdline suite roster <<<

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

# The roster check on its own, without compiling or running anything. It exists so the guard can be
# proved to go red — a check nobody has seen fail is worth less than no check, because it makes
# people believe somebody is looking.
if [ "${1:-}" = "--verify-suite-roster" ]; then
  verify_suite_roster
  exit $?
fi

# >>> clawdline focused entry >>>
# Opt-in skips unrelated node/browser suites; the original no-argument full entry is unchanged.
# Validate before guards or compilation, including explicitly empty/newline-only selections.
. tools/swift-test-artifact.sh
clawdline_swift_focused_only=0
clawdline_linux_package_focused_only=0
case "${1:-}" in
  "") [ "$#" -eq 0 ] || exit 2 ;;
  --swift-focused)
    [ "$#" -eq 1 ] || exit 2
    clawdline_swift_focused_only=1
    clawdline_validate_swift_test_selection required ;;
  --linux-package-focused)
    [ "$#" -eq 1 ] || exit 2
    clawdline_swift_focused_only=1
    clawdline_linux_package_focused_only=1 ;;
  *) echo 'test.sh: unknown_test_mode' >&2; exit 2 ;;
esac
clawdline_validate_swift_test_selection
case "${CLAWDLINE_SWIFT_TEST_ARTIFACT:-off}" in
  off|reuse) ;;
  *) echo 'test.sh: unknown_swift_artifact_mode' >&2; exit 2 ;;
esac
clawdline_test_profile=${CLAWDLINE_TEST_PROFILE:-release}
case "$clawdline_test_profile" in
  release|infrastructure) ;;
  *) echo 'test.sh: unknown_test_profile' >&2; exit 2 ;;
esac
# <<< clawdline focused entry <<<

# Everything above this line is either a definition or one of the two narrow modes, which run
# nothing and must therefore say nothing. From here down this is a run, and the two lines below are
# how it says so. They sit after the `cd` because the file is keyed by the working directory.
#
# **The traps live in the helper, not here.** They used to be a marked block copied byte for byte
# into `build.sh`, which is how a duplicated construct usually starts; `Resources/clawdline-progress.sh`
# is that block with an interface on it, and anything else with a slow command to run can have the
# same bar for one line. What is left here is what is actually this script's: its name and its
# measured duration.
#
# **Sourced from the checkout, never from the installed app.** The bundle ships the same file, and
# a suite that could only run on a Mac with Clawdline installed would be a suite CI cannot run.
#
# `progress_start` arms the ERR, INT, TERM **and** EXIT traps. That last one is what closes the
# window every guard below this line used to fall through: no ERR trap ever sees a deliberate
# `exit`, so a guard that stops on `exit 1` above the suite lock's own handler used to leave a
# `running` row for the reader's staleness ceiling to retire fifteen minutes later. Every EXIT trap
# installed further down **replaces** this one rather than joining it — bash keeps exactly one — so
# each of them composes `clawdline_run_file_exit` and is a superset of it, and
# `Tests/run-file-producer.mjs` holds all of them to that in both scripts.
#
# Recent runs no longer fit the old 288-second estimate, and a stale progress estimate is worse than
# no estimate. Per-phase durable receipts will supply a rolling value; until then neither test nor
# build invents one.
. ./Resources/clawdline-progress.sh
progress_start --label test

# (d) in `docs/suite-runtime.md`: the manifest, the architecture guard, the trailing-comma scan, the
# three Python guards and the protocol vectors. Historically this phase took a few seconds; it exists so
# that a run which dies in them is not drawn as a run that died in the compile.
progress_phase guards
. tools/swift-source-manifest.sh
verify_swift_source_manifest full
bash tools/check-architecture-boundaries.sh

if [ "$clawdline_swift_focused_only" -eq 0 ]; then
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
# The version this app calls itself. One line of Sources/CodexNaming.swift told `codex app-server` a
# release the app had left behind two releases earlier, and nothing could see it, because a literal
# that is never compared with anything cannot go stale loudly. This reads the app's own versions out
# of build.sh and the release table and refuses a third state — neither derived nor allowed with a
# written reason — anywhere in the tracked tree. It also holds the two sources against each other,
# because the fallback every bundle-less process takes is the table's newest row.
tools/check-version-strings.py
tools/check-web-strings.py
tools/check-web-ids.py
# `curl` exits 0 for `409`, `401` and `503` alike, which is how a refused restart came to be
# announced as an accepted one and a five-second client timeout came to be written down as the
# server refusing. Every `curl` this repository runs now either asks curl to fail on an HTTP error
# or takes the code out and compares it. The self-test runs first because it is the scanner's own
# control: twenty-nine shapes, calls and mentions, and a scan that has stopped telling them apart
# would otherwise pass the tree in silence. docs/curl-status.md has the rule.
tools/check-curl-status.py --self-test
tools/check-curl-status.py
# `landed` has exactly one entrance and a person is standing in it. The broker verifies a landing
# properly — the same `merge-base --is-ancestor` this guard runs — but only when somebody calls the
# route, so a delivery that reaches `main` while nobody writes its record is finished and silent.
# On 2026-09-05/06 that produced 22 hand-written records in one evening, 14 of them for work that
# had been in `main` for days, five of them for a different repository; this reads the machine's
# task registry rather than this tree, which is how it sees those five. It fails only on landings
# after its own cutoff and outside a six-hour grace, because `docs/landing.md` writes the record
# *after* this suite; everything older is printed in full on every run. docs/landing.md has the
# rule. Measured standalone at 1.2 s over 298 terminal tasks in 8 repositories.
#
# **Reporting is machine-wide; failing is not, and that is a third limit rather than a restatement
# of the two above.** On 2026-09-06 this line killed two runs that never reached a compiler, one of
# them an isolated child that died over another root's landing in the base repository — debt it
# could not have settled, because the route takes this machine's orchestrator token and `CHILD.md`
# forbids a child from calling it at all. A row now stops the run only when it is in the repository
# this suite is running in *and* this checkout is not a linked worktree, and the run says both
# conditions out loud whichever way they fall. `Tests/landing-records-scope.mjs` holds the half a
# red proof structurally cannot: that the narrowed green never reads as the green of a machine with
# nothing on it.
tools/check-landing-records.py
# The guard above ends by printing the `curl` that closes a record, and that snippet named a header
# the server does not read — `X-Clawdline-Orchestrator-Token` against the `x-clawdline-orchestrator`
# in `Sources/RemoteServer.swift`. Following it to the letter answers `403 forbidden`, which reads
# as "my token is wrong" rather than as "this instruction is wrong", so the one person standing in
# front of the only entrance is turned away by the sentence telling them to go through it. Found on
# 2026-09-06 while draining 26 unrecorded landings by hand. This is not two documents agreeing:
# one side is the route's own credential read, so the pair cannot drift silently in either
# direction. `remediation_header` is what the guard prints; the second grep is what the server
# accepts.
remediation_header=$(sed -n 's/.*-H \\"\([a-zA-Z-]*\): \$(cat .*/\1/p' tools/check-landing-records.py | head -1)
if [ -z "$remediation_header" ]; then
  echo "test.sh: cannot find the orchestrator header in check-landing-records.py's remediation curl" >&2
  exit 1
fi
if ! grep -q "\"$remediation_header\"" Sources/RemoteServer.swift; then
  echo "test.sh: check-landing-records.py tells the reader to send '$remediation_header', and" >&2
  echo "         Sources/RemoteServer.swift does not read a header by that name. Following the" >&2
  echo "         printed curl would answer 403." >&2
  exit 1
fi
unset remediation_header
# And every guard above has to have been seen to fail. Two checks that could not go red arrived on
# 2026-09-05 — a claims comparison that is identically true inside a linked worktree, and a
# `stale > worst` that was an identity — and both were green the way a working guard is green. This
# puts one defect in front of each `tools/check-*` and requires it to say so, and refuses a guard
# that no proof names. It matches `tools/check-*` itself, so it is on its own list.
# docs/guard-red-proofs.md has the shape of a proof. Measured standalone at 3.9 s with eight
# proofs, and 5.3 s once the landing-records proof — which builds a repository and merges in it —
# became the ninth. The tenth, `landing-records-scope.sh`, builds another repository, another merge
# and a linked worktree of it, and two readings taken with it in are 5.27 s and 5.23 s: inside this
# machine's noise rather than free, and measured rather than reasoned about.
if [ "$clawdline_test_profile" = infrastructure ]; then
  bash tools/check-guards-go-red.sh
else
  bash tools/check-guards-go-red.sh --meta
fi
verify_suite_roster
# (c) in `docs/suite-runtime.md` records how this pre-compile phase once dominated a run. The
# infrastructure profile keeps those expensive self-tests without charging every release candidate.
progress_phase 'node suites'
node Tests/docs-ui-labels.mjs
# The two READMEs are one document in two languages, and the file above pins eleven strings in
# them by hand. That catches a pinned sentence disappearing and nothing else: on 2026-09-04 a
# section deleted from either side, and a section added to one side only, were all green. This
# compares their heading sequence instead — count and order — which is the most that can be
# compared when the heading text is in two different languages.
node Tests/docs-readme-parity.mjs
# Keep the three contributor quick starts honest about cost without copying a volatile check total.
# Executed counts belong to run receipts; the dated wall-time measurement has one documented home.
node Tests/docs-suite-facts.mjs
node Tests/agent-instruction-topology.mjs
# And nothing at all watched `CHANGELOG.md`, the document that becomes the release notes. On
# 2026-09-04 four of its forty entries still described `orchestrator_max_grandchildren` and a
# dispatch tree two levels deep, long after the second level came out — the same shape as the 0.5.0
# cut that `tools/release.sh`'s header exists because of. This asserts every HTTP route the
# `## Unreleased` block names is still one the server answers. It cannot tell an entry that names a
# dead key to bury it from one that names it as a live setting; that half stays a person's job.
node Tests/changelog-facts.mjs
node Tests/agent-attention-principle.mjs
# A child's final file is irreversible broker input. Validate the temporary receipt first,
# including the closed review schema, and prove the briefing carries the validator into projects
# that do not contain Clawdline's own tools directory.
node Tests/task-result-validator.mjs
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
  Tests/web-fast-mode.mjs
  Tests/web-session-closeability.mjs
  Tests/web-session-selection.mjs
  Tests/web-module-boundaries.mjs
  Tests/web-title-transport.mjs
  Tests/web-code-copy.mjs
  Tests/web-markdown-lists.mjs
  Tests/web-message-images.mjs
  Tests/web-project-artifacts.mjs
  Tests/web-row-gesture.mjs
  Tests/web-detached-attach.mjs
  Tests/web-terminal.mjs
  Tests/web-pages.mjs
  Tests/web-projects.mjs
  Tests/web-documents.mjs
)
if [ "${#browser_contract_suites[@]}" -ne 24 ]; then
  echo "browser contract roster changed without updating its sealed count" >&2
  exit 1
fi
for browser_contract_suite in "${browser_contract_suites[@]}"; do
  node "$browser_contract_suite"
done
# The test push and the button that fires it, joined: `net/live.js` driven for real and read off
# the wire, and the real `Settings.test` driven against a stand-in transport and read off its
# argument. Registered on a line of its own rather than in the roster above, because that array
# carries a sealed count and a second session is adding to this file in the same window.
node Tests/web-push-test-session.mjs
# The web app's half of the run file this script now writes: the footer that draws a run in flight.
# It arrives on another branch — the producer and the two readers were built at the same time — so
# in a checkout that has only one of them this line is what says the other is missing, rather than
# the roster check quietly passing over a suite nobody runs.
node Tests/web-run-progress.mjs
# The hosted console: which transport it is, the pairing mirror against the checked-in
# vectors, and that the static bundle a person uploads by hand is the same bytes twice.
node Tests/web-cloud-boot.mjs
node Tests/web-cloud-pairing.mjs
node Tests/web-cloud-onboarding.mjs
node Tests/web-app-build.mjs
# The Plan page: the only screen in this product that can start a payment, and the four ways it
# fails without throwing — a checkout that was never created, a webhook that has not arrived, an
# account that already pays, and a tier the control plane does not sell. A standalone line rather
# than a member of `browser_contract_suites` above, so this adds nothing to that roster's sealed
# count and nothing to the Swift receipt.
node Tests/web-billing.mjs
node Tests/dispatch-role-contract.mjs
# The three surfaces that tell a root how to reach a standing session, held against the closed set
# of `attach_*` refusals scanned out of Sources/*.swift rather than against each other: they spent
# seven days telling every root to approximate a mechanism that had already landed, and comparing
# two copies of a wrong sentence produces agreement, not a red.
node Tests/attached-follow-up-contract.mjs
# The same shape, one route further along and for the same reason. `POST /v1/orchestrator/handoffs`
# grew nine typed refusals when it started requiring a sender, four surfaces were written to
# describe them, and nothing compared any of the four with the code — so within one review three of
# them were false, including both shipped guides still teaching that an unrecognised sender is the
# same as an absent one. That is the sentence the sender of the 2026-09-04 handoff read.
node Tests/handoff-sender-contract.mjs
node Tests/linux-package-graph.mjs
node Tests/restart-rollout-contract.mjs
node Tests/remote-response-write-close.mjs
node Tests/terminal-current-and-browser-open.mjs
# `GET /sw.js`, which was the one of RemotePage's five entry points with no route test — the gap
# `B-SERVICE-WORKER-HAS-NO-ROUTE-TEST` names. The script is a response body inside a Swift raw
# string, so this lifts it out and runs it in a node:vm with stand-in worker globals rather than
# grepping it: install, activate, fetch, push and notificationclick are each driven. What it is
# guarding is the only lever that reaches a browser already holding a stale copy of the page, and
# every step of that lever is one line.
node Tests/web-service-worker.mjs
# The press that carries the layout trace onto this Mac, and what the panel says afterwards. Driven
# rather than read: the panel's own click handler runs against a stand-in fetch, so what is checked
# is the text on the screen containing the server's path and the server's typed code — a button
# that posts perfectly and says nothing is the failure this file exists for.
node Tests/web-diagnostics-send.mjs
node Tests/release-signing-contract.mjs
# The shared-tree commit guard: that `tools/git-hooks/pre-commit` refuses a commit carrying a path
# another session is working on, that it lets everything else through, and that it fails open and
# loudly when Clawdline is not answering. Throwaway repositories under `mkdtemp` only — this suite
# never runs git against the checkout it is testing, and proves that containment on the way out.
node Tests/git-hooks.mjs
# The other half of `tools/check-landing-records.py`, which the guards phase above runs: that it
# reports the whole machine and fails only on debt this run can settle — the row is in the
# repository the suite is running in, and the checkout is not a linked worktree, because a linked
# worktree is a child's and `CHILD.md` forbids a child from calling the landing route at all. The
# red proofs hold the red side; what they structurally cannot hold is what the *green* side says,
# and an exit 0 that goes quiet inside a worktree is the exact defect the hook above carries a
# paragraph about. So this drives both arms of one fixture and requires the narrowed green to name
# the checkout, the repository, the derivation, the row and who can settle it. Throwaway
# repositories under `mkdtemp` and a registry of its own: this machine's real
# `~/.config/clawdline/orchestrator.json` is never opened.
node Tests/landing-records-scope.mjs
# The scratch tool that replaced the two snapshot recipes AGENTS.md used to print, which made
# `mktemp -d` directories and removed none of them. What it promises is an absence, so every run here
# is followed by a look at the root: success, a failing command whose status comes back unchanged,
# INT, TERM and HUP leave nothing; `--keep` leaves one entry with a valid marker; a symlinked or
# foreign root and a path outside the root are refused. Throwaway repositories and a scratch root
# under `mkdtemp` only — this machine's `/tmp/clawdline-scratch` and `/tmp/.clawdline` are never
# pointed at, and the refusal that keeps them out is itself counted. docs/scratch.md is the contract.
node Tests/scratch-tool.mjs
# The onboarding policy, compiled out of Sources/Onboarding.swift without its AppKit half: that a
# config switch is not readiness, that an allocated credential is not a connection, and that the
# installer reopens the exact bundle it just wrote. It runs here rather than in the Swift suite
# because the shipped policy has no AppKit dependency and this keeps it a second rather than a
# recompile of everything.
node Tests/app-onboarding-focused.mjs
node Tests/keychain-rebuild-focused.mjs
# The write at the other end of that press, on a real disk. `Sources/DiagnosticReport.swift` needs
# Foundation and nothing else, so it compiles here in a couple of seconds beside a harness instead
# of costing a whole-module rebuild under the machine lock — which matters because the three ways
# this can lie are all about what is on the disk afterwards: accepted and nothing written, written
# and the wrong path named, over the limit and success returned.
node Tests/diagnostic-report-focused.mjs
# What Clawdline tells `codex app-server` it is, on the same terms: the identity block is lifted out
# of Sources/CodexNaming.swift by its marker comments and compiled against the real Sources/Compat.swift,
# because the rest of that file names half the app and compiling it means compiling the module. The
# version in that payload was a typed string that went on naming an old release for two releases
# after it, with nothing in this suite comparing it to anything.
node Tests/codex-client-identity.mjs
node Tests/codex-naming-order.mjs
# Whether there is a newer Clawdline, on exactly the same terms and for a sharper reason: a check
# that answers "nothing newer" when it was in fact rate-limited is a silence that reads as an
# all-clear, and the person on the old build never finds out. The decision block is lifted out of
# Sources/UpdateCheck.swift by its markers and compiled against the real Sources/Compat.swift, so
# every way of failing is exercised without a single request leaving this Mac.
node Tests/update-check.mjs
# The install path the website and both READMEs recommend, which runs on a Mac this project has
# never seen and had nothing exercising it: that the release reply is read without an interpreter
# that is really an xcselect shim, that a reply this script cannot read says so instead of ending
# the install in silence, and that the ad-hoc legacy branch verifies the bundle before it takes
# macOS's last guard off it. Stand-ins on PATH for every command it reaches out with, and a
# temporary DEST — nothing here downloads a release or touches an installed app.
node Tests/install-focused.mjs
# Two suites that existed and that nothing ran: neither was in this list, and CI only runs
# this script. A test nobody runs is a test that passes.
node Tests/web-user-messages.mjs
# The sheet next door: what a snippet press does — insert through the composer's own `appendMsg`,
# never send — the grouping the Mac resolved, and the guard that leaves a control undrawn on a
# transport whose route is missing. Registered here rather than in `browser_contract_suites` above
# so the sealed count of that roster stays the landing root's to move.
node Tests/web-snippets.mjs
# The waiting card, which had no test of any kind until 2026-09-06 — the loudest thing the phone
# draws and the one whose contents are least certain, because every word on it comes from a menu
# parsed off the Mac's own screen. What is pinned here is the live-screen button that does not:
# that it is outside the read/unread branch and so survives a parse that failed. Standalone rather
# than a member of `browser_contract_suites` above, so that roster's sealed count stays root's.
node Tests/web-waiting-card.mjs
# The verification ledger page, and the reason it has a suite rather than a share of the roster
# above: what it guards is that three states stay three different things on screen. `present` is a
# figure, `absent` is the words *no record*, `unknown` is the words *not measurable*, and one
# `|| 0` anywhere in that module turns all three into the same grey rectangle — which is the
# defect the whole feature exists to end, arriving through the front door. Registered on a line of
# its own so `browser_contract_suites`' sealed count stays the landing root's to move.
node Tests/web-ledger.mjs
node Tests/web-board.mjs
node Tests/web-session-board.mjs
node Tests/web-board-session.mjs
node Tests/web-board-workflow.mjs
node Tests/web-board-transport.mjs
node Tests/web-timeline.mjs
node Tests/board-request-capacity-focused.mjs
node Resources/web/app/js/net/client.test.mjs
# The lightbox's own zoom, beside the module it tests for the same reason `client.test.mjs` is:
# what it holds is arithmetic rather than a page. Four screenshots reached a phone on 2026-09-05
# and none of them could be enlarged — `index.html` turns the browser's pinch off page-wide — so
# the anchor maths, the two bounds and the recentre are the lightbox's own and are checked here.
node Resources/web/app/js/view/transcript-images.test.mjs
# Two more of exactly the same, found the same way and registered on 2026-09-04. Neither had run
# once since the day it was committed — `Tests/web-usage-analytics.mjs` at f0eedc18 and
# `Tests/web-close-confirm-explanation.mjs` at 58386b07 — because nothing compares this hand-written
# roster with the directory. The first guards the Usage Portfolio front end, which is the one panel
# in the web app that does not go through the transport seam; the second guards the wording a person
# reads before closing a session. Both were green the first time they were run, which is the less
# useful of the two possible answers: a red would have been noticed, a green was never missed.
node Tests/web-usage-analytics.mjs
node Tests/web-close-confirm-explanation.mjs
# These two are about this script rather than the app: that a crashed run still leaves its output,
# and that the machine-wide suite lock below serialises the expensive half. Both run before the
# lock is taken, so a machine that is already busy still gets told what is wrong with this checkout
# before it starts queueing.
node Tests/cloud-contract-v1.mjs
if [ "$clawdline_test_profile" = infrastructure ]; then
  # These suites prove the test/lock/cache/measurement infrastructure itself. Product release
  # candidates do not spend minutes re-proving them when none of those inputs changed.
  node Tests/test-sh-streaming.mjs
  node Tests/test-sh-lock.mjs
  node Tests/platform-architecture-inventory.mjs
  node Tests/platform-reliability-characterization.mjs
  node Tests/swift-test-artifact.mjs
  node Tests/progress-helper.mjs
fi
# And that these two scripts are the helper's first callers rather than its documentation: that they
# source it from the checkout, name themselves, arm it before anything that could exit, and that
# every EXIT trap either of them installs further down is a superset of the one it armed. Both run
# before the lock is taken, so a checkout whose producer is broken is told so before it starts
# queueing for a compiler.
node Tests/run-file-producer.mjs
fi

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
# `Tests/app-onboarding-focused.mjs`, `Tests/keychain-rebuild-focused.mjs` and
# `Tests/codex-client-identity.mjs` run above this line and
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
# **The dials are shared, because the lock is.** `build.sh` used to read its own spelling of these
# — `CLAWDLINE_LEASE_DIR`, `CLAWDLINE_LEASE_DEADLINE_SECONDS`, `CLAWDLINE_LEASE_WAIT_SECONDS` — and
# the defaults agreed, so the ordinary path was right and nothing ever said otherwise. What the
# ordinary path hides is `heartbeat_deadline`: a *record* field both writers fill in and every
# reader prefers to its own, so tuning one spelling put two different numbers in one field. The
# `CLAWDLINE_SUITE_LOCK_*` name is canonical and wins when both are set; the `CLAWDLINE_LEASE_*`
# one keeps working here as well as there, so tuning either spelling reaches both writers.
CLAWDLINE_SUITE_LOCK_DIR="${CLAWDLINE_SUITE_LOCK_DIR:-${CLAWDLINE_LEASE_DIR:-/tmp/clawdline-suite.lock}}"
# `pgrep -x` matches the executable's own name. Measured against a live compile here rather than
# assumed: a running `swiftc` shows up as `swift-frontend` under `pgrep -x`, while `ps -A -o comm=`
# prints the whole toolchain path and matches no bare name at all.
CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN="${CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN:-swift-frontend}"
CLAWDLINE_SUITE_LOCK_RENEW_SECONDS="${CLAWDLINE_SUITE_LOCK_RENEW_SECONDS:-20}"
# Three renewals of slack. A reader prefers the deadline the holder recorded in its own record, so a
# holder that renews more slowly than this reader expects is never declared dead by that reader.
CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS="${CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS:-${CLAWDLINE_LEASE_DEADLINE_SECONDS:-60}}"
CLAWDLINE_SUITE_LOCK_WAIT_SECONDS="${CLAWDLINE_SUITE_LOCK_WAIT_SECONDS:-${CLAWDLINE_LEASE_WAIT_SECONDS:-3600}}"
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
  # The run file's own way out, composed here for the same reason the `$STORE` removal below is
  # composed here: bash keeps exactly one EXIT trap and a second one silently replaces this. The
  # EXIT path is also the only one that sees a deliberate `exit 1`, which no ERR trap ever does.
  # `declare -F` because `Tests/test-sh-lock.mjs` lifts this block out and runs it on its own, where
  # the run-file block above does not exist and a missing function would end that harness at 127
  # inside its own cleanup.
  if declare -F clawdline_run_file_exit >/dev/null 2>&1; then clawdline_run_file_exit "$status" || true; fi
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
  if [ -n "${linux_package_scratch:-}" ]; then rm -rf "$linux_package_scratch"; fi
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

# The log the run is streaming into, now that the block above has named it. The phases before this
# line carry no `log` and every phase after it does, which is honest: there is nothing in it yet.
CLAWDLINE_RUN_LOG="$LOG"

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

# >>> clawdline swift artifact invocation >>>
progress_phase compiling
clawdline_suite_lock_phase compiling
linux_package_scratch=""
linux_package_bin_dir=""
build_linux_package_contract() {
  linux_package_scratch="${TMPDIR:-/tmp}/clawdline-linux-package-$$"
  mkdir -p "$linux_package_scratch"
  linux_package_bin_dir=$(swift build --disable-sandbox \
    --scratch-path "$linux_package_scratch" -c debug --show-bin-path)
  swift build --disable-sandbox \
    --scratch-path "$linux_package_scratch" -c debug --product ClawdlineLinux \
    ${clawdline_suite_jobs_flags[@]+"${clawdline_suite_jobs_flags[@]}"} 2>&1 | tee "$LOG"
}

if [ "$clawdline_linux_package_focused_only" -eq 1 ]; then
  # W3-2's narrow compiler/behavior proof. It uses SwiftPM's real product graph under the same
  # machine-wide lock as the full suite, then drives the resulting executable's fail-closed
  # startup contract. No Mac app is bundled, installed, signed, or restarted.
  build_linux_package_contract
else
  # The release candidate always compiles and executes the shipped Linux graph too. The flat
  # compiler below remains as a compatibility test graph; it cannot cover Packages/ClawdlineLinux.
  if [ "$clawdline_test_profile" = release ]; then build_linux_package_contract; fi
if [ "${CLAWDLINE_SWIFT_TEST_ARTIFACT:-off}" = "reuse" ]; then
  clawdline_swift_test_artifact "$BIN" \
    -swift-version 5 -target "$clawdline_swift_test_target" \
    ${clawdline_suite_jobs_flags[@]+"${clawdline_suite_jobs_flags[@]}"} \
    -framework AppKit -framework Carbon -framework ServiceManagement \
    -framework Speech -framework AVFoundation -framework Network \
    -- "${clawdline_library_sources[@]}" "${clawdline_test_sources[@]}"
else
swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  ${clawdline_suite_jobs_flags[@]+"${clawdline_suite_jobs_flags[@]}"} \
  -o "$BIN" \
  "${clawdline_library_sources[@]}" \
  "${clawdline_test_sources[@]}" \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation -framework Network
fi
fi
# <<< clawdline swift artifact invocation <<<

# Between the two halves of the guarded section. The compile is the long unattended stretch, so this
# is where a lock that changed hands underneath the run has to be noticed — before the second
# expensive thing starts.
clawdline_confirm_suite_lock || exit $?
clawdline_suite_lock_phase analysing
progress_phase analysing

if [ "$clawdline_linux_package_focused_only" -eq 1 ]; then
  CLAWDLINE_LINUX_BINARY="$linux_package_bin_dir/ClawdlineLinux" \
    node Tests/linux-package-graph.mjs 2>&1 | tee -a "$LOG"
  linux_package_receipts=$(grep -Ec '^linux package graph: [1-9][0-9]* checks passed$' "$LOG" || true)
  if [ "$linux_package_receipts" -ne 1 ]; then
    echo "test.sh: expected one Linux package receipt, found $linux_package_receipts — full output kept at $LOG" >&2
    exit 125
  fi
  clawdline_suite_lock_phase idle-holding
  clawdline_suite_lock_work_finished
  rm -rf "$linux_package_scratch"
  linux_package_scratch=""
  rm -f "$LOG"
  exit 0
fi

if [ "$clawdline_test_profile" = release ]; then
  CLAWDLINE_LINUX_RUNTIME_ONLY=1 CLAWDLINE_LINUX_BINARY="$linux_package_bin_dir/ClawdlineLinux" \
    node Tests/linux-package-graph.mjs 2>&1 | tee -a "$LOG"
  rm -rf "$linux_package_scratch"
  linux_package_scratch=""
fi

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
# **`set +e` turns off errexit; it does not turn off the ERR trap, and the two have to move
# together.** The trap installed with the run file ends in `exit "$status"`, so with it left armed
# the pipeline below fired it on every red suite and left through the handler: no line naming
# `$LOG`, no `report_receipt_direction`, and the `exit 126` branch for a `tee` that could not write
# unreachable — all of it only on a red run, which is when they exist. Measured, then guarded by
# `Tests/test-sh-streaming.mjs`, which now runs this block under the same traps.
#
# **Disarmed here rather than repaired in the handler**, and the two rejected directions are worth
# naming. The handler is shared with `INT` and `TERM`, where the `exit` is the whole point — a
# `TERM` handler that returns carries on from where it was interrupted and finishes by declaring
# success, which was measured. And a handler that recorded `fail` without exiting would latch it:
# inside a `set +e` window a command is *expected* to fail and be read, so a green suite whose
# `tee` happened to return non-zero would have ended as a `fail` row nothing could overwrite.
# What the ERR trap is for is "errexit is about to end this run, write the row before it does".
# `set +e` withdraws exactly that premise, so `trap - ERR` belongs to it the way `set -e` below
# belongs to the rearm. What it costs is stated rather than hidden: between these two lines a
# failure nobody reads reaches no trap at all, and it is the EXIT trap installed above — which
# every deliberate `exit` in this file also goes through — that still writes the row.
set +e
trap - ERR
CLAWDLINE_REMOTE_DIR="$STORE" CLAWDLINE_DROPS_DIR="$STORE/drops" "$BIN" Resources/mascots 2>&1 | tee "$LOG"
# Copied whole, in one assignment. Reading the members one at a time does not work and does not
# look broken: the first assignment is itself a command, so it replaces `PIPESTATUS` with its own
# one-element status, and the second read is of an array that no longer has a second member —
# `unbound variable` under `set -u`, on a green suite, at the very end. Measured here.
pipe=("${PIPESTATUS[@]}")
status=${pipe[0]}
tee_status=${pipe[1]}
set -e
trap 'clawdline_run_file_signal "$?"' ERR
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

# A focused selection ends before the Cloud roster by design and therefore cannot mint the
# complete full-suite tuple. Its own `N focused checks passed` receipt is the only one it emits.
if is_unfiltered_test_run; then
  emit_complete_test_seal_receipt "$LOG" || exit $?
else
  clawdline_verify_focused_test_receipt "$LOG" || exit $?
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
if is_unfiltered_test_run; then
  verify_test_completion_receipts "$LOG"
fi
rm -f "$LOG"
