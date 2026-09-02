# Architecture refactor blueprint

Status: Phases 0–1 implemented as a review candidate; Phase 2 remains gated and unauthorized.

Measured baseline: `main` at `04071d8a`, 2026-08-29. The exact candidate tree passed 6,434
checks before the application build. Re-measure file sizes and dependencies before starting any
phase; the symbols and ownership boundaries below are more durable than line numbers.

## Objective

Make the current feature set a stable base for future work by giving mutable state, application
policy and transport one owner each. This is a strangler refactor: public facades remain while
cohesive services are extracted behind them. It is not a rewrite, a module-per-helper exercise, or
permission to change wire, persistence or concurrency semantics while moving code.

The current hotspots are evidence of collision, not automatic split points:

| File | Lines at baseline | Responsibility pressure |
|---|---:|---|
| `Tests/main.swift` | 27,629 | entry point, isolation, fixtures, runners and 432 groups |
| `Sources/Orchestrator.swift` | 11,562 | schedules, registry, dispatch, landing, waits, completion and storage |
| `Sources/RemoteServer.swift` | 5,903 | transport, routing, auth, terminal broker, domain handlers, SSE and assets |
| `Sources/Controller.swift` | 4,092 | AppKit composition, session browser, transcript, stack, voice and snapshots |
| `Sources/Settings.swift` | 3,919 | window lifecycle, persistence and several independent product domains |

## Phase 0–1 candidate sealed at `f32071e5`

The extraction candidate starts from `f32071e5f429dee9b22ab3f32853212c14f747f6` and changes no
production Swift behavior. Baseline A and candidate baseline B both execute the same ordered 432
`group()` identities and 6,434 checks. The guards that prove those facts are check-neutral, so the
act of guarding the baseline does not mint a new check and then call the new number equivalent.

| Surface | Before | Candidate |
|---|---:|---:|
| `Tests/main.swift` | 27,630 lines | 34 lines |
| ordered `group()` identities | 432 | 432 |
| full Swift check count | 6,434 | 6,434 |
| Swift test files | 12 | 40 |
| extracted domain runners, including isolation | 1 implicit stream | 24 explicit runners |
| largest extracted suite | n/a | 1,841 lines |

`tools/swift-source-manifest.sh` is now the single deterministic source inventory used by both
`build.sh` and `test.sh`. Build mode recursively compares the production partition only with
`Sources/**/*.swift`; full test mode separately compares that partition and the test partition with
`Sources/**/*.swift` and `Tests/**/*.swift`. A nested addition/removal or a Sources↔Tests partition
swap therefore fails before compilation, while Tests-only drift cannot block an application build.
The launch integration candidate contains 93 production and 42 test sources. Historically, Phase 0 began
with `RemoteServer.swift` at 5,903 lines and the later Closeability receipt recorded
`Orchestrator.swift` at 12,398, `RemoteServer.swift` at 6,316, and 447 manifest entries. Those are
chronology, not executable limits. The single current receipt is the combined launch receipt below;
later approved features must move it explicitly rather than silently weakening it. The guard also
keeps `Tests/main.swift` below 500 lines and free of domain `group()` calls, requires 25 ordered
runners, and enforces the 2,000-line suite stop-growth boundary.

The combined Root Assignment and Usage Portfolio landing is the last observed full-suite receipt:
`Orchestrator.swift` is sealed at 13,482 lines, `RemoteServer.swift` at 6,384, the ordered manifest
at 456 groups, 25 ordered runners, 36 suite files, and the Swift completion receipt at 6,903 checks.
This is an approved feature addition and a test-suite extraction, not authorization for Phase 2
production relocation. Milestone adds 15 Swift checks without growing either sealed file and moves
the executable candidate guard to 6,921. The merged candidate also carries three Usage Portfolio
groups, so its ordered manifest guard is 459; both values remain expectations until the final exact
candidate-tree suite observes them, and must be corrected rather than asserted if either differs.

The Clawdfather succession candidate adds one Coordinator test group and 75 checks, moving the
executable expectations to 461 groups and 7,185 Swift checks. The Cloud Keychain namespace
correction adds two more checks, moving the exact executable receipt to 7,187. It keeps `RemoteServer.swift` at the
6,385-line dispatch-door receipt by introducing the cohesive `CoordinatorSuccessionService`
boundary: the service owns its independent receipt ledger and lifecycle, while the frozen router
retains only one transport-adapter call. These remain candidate expectations until the exact staged
tree full suite observes them.

### Root Assignment delivery-observation exception (2026-09-01)

The Root Assignment delivery false-timeout correction moves the executable
`Orchestrator.swift` guard from 13,539 to the exact candidate count of 13,592 lines and the Swift
completion receipt from 7,229 to 7,245 checks. This is a narrow bug-fix exception, not Phase 2
approval. The cohesive lifecycle remains owned by `Orchestrator`: the same step captures terminal
state, resolves the exact transcript, compares the delivery event with its deadline, persists the
assignment transition, updates the registry projection and emits the at-most-once audit receipt.

The next extractable boundary is a pure `RootAssignmentDeliveryPolicy`, after the Phase 2 gate and
fresh human approval. Moving only its value types and helper functions today would leave all state,
terminal and transcript dependencies in `Orchestrator` and produce an extension-shaped file split
that changes no dependency direction; moving the complete lifecycle would be an unauthorized
production relocation mixed into a correctness repair. The owner therefore stays named here, the
guard is resealed at the exact corrected file size, and later growth must either pass the Phase 2
gate or record another explicit exception rather than silently raising the number.

The corresponding Root Assignment coordination group is an already-approved Phase 1 test-harness
boundary, so it moves intact to `RootAssignmentCoordinationTests.swift`: runner and group order stay
unchanged, `OrchestratorCoordinationTests.swift` falls from 2,046 to 1,690 lines, the new cohesive
suite is 361 lines, and the sealed suite-file inventory moves from 37 to 38. No production behavior
or check count is created by the relocation.

Bounded Keychain writes add 22 executed checks and no new file, group, runner or suite: the work
lands inside `CloudAccountTests` (82 → 92) and `CloudSettingsTests` (28 → 40), moving the Swift
completion receipt from the released 7,245-check main baseline to 7,267 and the Cloud receipt's
two named suite counts with it. The ordered group manifest, the 25 runners, the suite-file count
and both source-manifest partitions are
deliberately unchanged, so only the check-count receipts moved and only those were edited. The
count is arithmetic over unconditional checks rather than an observation — this candidate was
verified with focused runners and a full-target typecheck, and the exact staged-tree suite is
still what settles it.

The consolidated Keychain/signing correction adds 43 unconditional checks without adding a test
group, runner or suite: `CloudAccountTests` moves 92 → 105, `CloudSettingsTests` 40 → 59 and
`CloudLifecycleTests` 76 → 87. Their focused candidate-overlay run observed all 251 affected checks
green, so the arithmetic completion receipt moves 7,267 → 7,310 while the ordered manifests remain
unchanged. This is focused pre-integration evidence; the landing root still owns the one exact
candidate-tree full-suite receipt that settles the count.

### Transcript first-paint isolation extraction (2026-09-01)

The first transcript-paint candidate initially failed the combined architecture preflight before
compilation: `RemoteServer.swift` reached 6,504 lines against the sealed 6,385-line dispatch-door
receipt. The correction does not raise that limit. `TranscriptReadCoordinator` owns only the
transport-agnostic foreground/background admission budget, serial worker, retry debt and completion
accounting; its work and result types are generic, with no `RemoteServer.Request`/`Response`
dependency or cross-file `RemoteServer` extension. `RemoteServer` privately retains authentication,
HTTP retry encoding, cache policy, route execution and delivery, and returns to exactly 6,385
lines. The single exposed failure-injection method configures its private test backing state on the
owner queue. The deterministic source manifest moves to 99 production files, and the one new
transcript-worker group moves the ordered group receipt from 462 to 463 without adding a test runner
or suite file. Its 25 unconditional Swift checks move the expected executable receipt from 7,341
to 7,366. These are candidate receipts until the exact staged-tree suite observes them.

### Ordered suite and dependency manifest

`Tests/main.swift` owns process order only. It enters subprocess probes, installs isolation, runs
the following synchronous runners in order, starts the existing 11-suite Cloud registry, and then
enters `dispatchMain()`. All suites depend inward on `TestHarness`; only process probes and
`TestIsolation` may configure process-global seams. Domain suites may call production APIs and
shared high-fan-in fixtures, but do not call another domain runner.

The criteria column records engineering judgments against the anti-over-splitting criteria below;
it is not a measured dependency-distance table. Every boundary has at least two stated judgments,
which should be re-evaluated against current dependencies before a later extraction:

| Order | Runner / file | Owned change pressure | Criteria |
|---:|---|---|---|
| 1 | `runTestIsolationTests` / `TestIsolation.swift` | process stores, caches and cleanup | 1, 2, 3 |
| 2 | `runScheduledDispatchTests` / `ScheduledDispatchTests.swift` | schedule parsing, persistence and routes | 1, 3, 4 |
| 3 | `runMascotTests` / `MascotTests.swift` | mascot schema, validation and rendering | 1, 3, 4 |
| 4 | `runTranscriptTests` / `TranscriptTests.swift` | terminal parsing and transcript ownership/rendering | 1, 3, 4 |
| 5 | `runMarkdownTests` / `MarkdownTests.swift` | Markdown rendering, attachments and presentation | 1, 3, 4 |
| 6 | `runDevStackTests` / `DevStackTests.swift` | DevStack schema, state and log projection | 1, 2, 4 |
| 7 | `runHookTests` / `HookTests.swift` | hooks, tunnel, push and remote seams | 1, 3, 4 |
| 8 | `runSessionLaunchTests` / `SessionLaunchTests.swift` | places, terminal launch and start routes | 1, 3, 4 |
| 9 | `runPlannerTests` / `PlannerTests.swift` | intent planning, menus and authenticated commands | 1, 3, 4 |
| 10 | `runPeerMessageTests` / `PeerMessageTests.swift` | peer-envelope transcript reconciliation | 1, 3, 4 |
| 11 | `runCodexSessionTests` / `CodexSessionTests.swift` | Codex activity, rollout and naming | 1, 3, 4 |
| 12 | `runConversationTests` / `ConversationTests.swift` | conversation identity and resume parsing | 1, 3, 4 |
| 13 | `runOrchestratorDispatchTests` / `OrchestratorDispatchTests.swift` | admission, models, worktrees and claims | 1, 3, 4 |
| 14 | `runOrchestratorLandingTests` / `OrchestratorLandingTests.swift` | landing, visibility and root receipts | 1, 2, 4 |
| 15 | `runOrchestratorLifecycleTests` / `OrchestratorLifecycleTests.swift` | serialization, state transitions and briefing | 1, 2, 4 |
| 16 | `runOrchestratorCoordinationTests` / `OrchestratorCoordinationTests.swift` | handoffs, waits, relay and notifications | 1, 2, 4 |
| 17 | `runSessionCloseAndQuotaTests` / `SessionCloseAndQuotaTests.swift` | close lifecycle, linger and quota | 1, 2, 4 |
| 18 | `runSessionRegistryTests` / `SessionRegistryTests.swift` | Claude registry and subprocess jobs | 1, 2, 4 |
| 19 | `runBackgroundAndStorageTests` / `BackgroundAndStorageTests.swift` | background sessions, owned storage and reclaim | 1, 2, 4 |
| 20 | `runOrchestratorRecoveryTests` / `OrchestratorRecoveryTests.swift` | spawn retry, progress and verification metadata | 1, 2, 4 |
| 21 | `runCoordinatorTests` / `CoordinatorTests.swift` | coordinator identity, rebind and Bearings | 1, 2, 4 |
| 22 | `runOrchestratorCompletionTests` / `OrchestratorCompletionTests.swift` | durable completion ingress, retry and ACK | 1, 2, 4 |
| 23 | `runUsageLedgerTests` / `UsageLedgerTests.swift` | ledger normalization, parsing and range semantics | 1, 2, 4 |
| 24 | `runUsagePortfolioAndLifecycleTests` / `UsagePortfolioAndLifecycleTests.swift` | Project portfolio, attribution, migration and lifecycle | 1, 2, 4 |
| 25 | `runSessionWatchTests` / `SessionWatchTests.swift` | queue crossings, live reads and backpressure | 1, 2, 4 |

Infrastructure has narrower direction: `TestProcessProbes` may enter subprocess-only modes;
`TestIsolation` owns global setup; `TestHarness` owns checks, failures and shared fixtures;
`TestGroupManifest` owns the sealed ordered identity list; `CloudTestRunner` is the only async
completion and receipt path. Existing Cloud suites retain their names, order and counts.

The architectural defect is stronger than size: `Orchestrator` calls `RemoteServer`, while
`RemoteServer` calls `Orchestrator`; Settings also uses the HTTP server as a serialization service.
Splitting those types into extensions would preserve the defect.

## Target dependency direction

```text
AppComposition
  -> presentation and transport adapters
  -> application services
  -> domain policies and state transitions
  -> ports
  -> filesystem / SQLite / Git / terminal / SessionWatch / network adapters
```

Rules:

- Domain and application services do not reference UI or `RemoteServer.shared`.
- `RemoteServer` parses a closed typed route, calls one application service, and encodes a typed
  result. Raw paths and `[String: Any]` stop at adapters.
- UI does not use the HTTP server to serialize local mutations.
- One mutable collection has one owner and one synchronization model.
- Concrete wiring lives in `main.swift` or `AppComposition`, not in feature implementations.
- A behavior-neutral refactor does not also change a store schema, route contract or concurrency
  primitive. Any such change becomes its own migration project.

## Phase 0 — freeze and prove the baseline

Add guards before moving behavior:

- Preserve the ordered 432-group manifest and record 6,434 as historical baseline A.
- Preserve Cloud receipt suite names and counts.
- Preserve API route shapes, typed errors and store decode semantics.
- Make build and test source discovery share one deterministic manifest before introducing nested
  source directories. A mutation that removes one nested source must make the manifest guard red.
- Put `Tests/main.swift`, `Orchestrator.swift` and `RemoteServer.swift` under net-growth freeze.
  Bug fixes may enter; a feature must use or introduce a named boundary.

Each guard is an independently revertible commit. No production relocation belongs in Phase 0.

## Phase 1 — extract the test harness first

The test harness is the first dependency of every later refactor. Keep exactly one executable
entry point and approximately 15–24 domain suites, not one file per group.

```text
Tests/main.swift                 process-mode dispatch and ordered manifest only
Tests/TestHarness.swift          TestContext, checks and failures
Tests/TestIsolation.swift        global seams and cleanup
Tests/TestProcessProbes.swift    subprocess modes
Tests/OrchestratorTaskTests.swift
Tests/OrchestratorLandingTests.swift
Tests/OrchestratorCompletionTests.swift
Tests/SessionWorkStateTests.swift
Tests/CoordinatorTests.swift
Tests/RemoteServerTests.swift
...
```

Each suite exposes an explicit runner such as `runCoordinatorTests(_:)`; non-main Swift files do
not gain top-level expressions. Preserve suite order initially: current tests share seams and are
not assumed parallel-safe. Move one cohesive suite per commit and compare group order and check
count. A helper becomes shared only when at least two suites use it or it represents a real system
boundary.

Phase 0 seals a new baseline B after its guards land. Acceptance: `Tests/main.swift` below 500
lines, unchanged ordered groups, manifest and check counts exactly equal to baseline B, and one
phase-end full suite. Until this phase finishes, add no new domain `group()` to `Tests/main.swift`.

## Phase 2 — pure Orchestrator policies

Extract code that does not own the mutable registry, while keeping the `Orchestrator` facade:

- `SessionWorkProjector`
- `TaskAdmissionPolicy`
- `ScheduleDefinition`
- `LandingVerificationPolicy`, which accepts already measured Git facts
- `CompletionRetryPolicy`

Types stay with their owning policy or service. Only a high-fan-in identifier or envelope used by
two or more boundaries earns a shared types file; there is no `OrchestratorModels` catch-all.

Each extraction needs table-driven tests and a narrow input/output contract. An `extension
Orchestrator` in another file does not qualify: the dependency and state boundary must change.
The facade continues to acquire Git facts in this phase; only the pure decision moves. `GitInspecting`
is introduced later with the registry/service boundary rather than smuggled into a policy type.

## Phase 3 — one registry owner and application services

Create one `OrchestratorRegistry` transaction/snapshot owner, then move behavior behind services:

- `TaskLifecycleService`
- `TaskDispatchService`
- `HandoffService`
- `CoordinationWaitService`
- `CompletionOutbox`
- `ScheduleService`
- `LandingService`

Inject ports such as `TerminalWorkScheduling`, `SessionInventoryReading`,
`OrchestratorEventPublishing`, `TaskStore` and `GitInspecting`. Remove the
`Orchestrator <-> RemoteServer` cycle by adapting these ports in composition. Do not combine this
with actor conversion; keep synchronization semantics stable until ownership migration is proven.

Cut one service at a time through the facade. Verify store semantic equality, idempotency,
restart recovery and exactly one event publication per committed transition.

## Phase 4 — typed HTTP route families

Keep connection lifecycle, parsing, response writing and SSE ownership in `RemoteServer`. Extract
a `RemoteRouter`, authentication middleware, the terminal mutation broker, payload builders and
cohesive route handlers for Sessions, Orchestrator, schedules, media and pairing.

Do not create one file per endpoint. A route family belongs together when it shares authorization,
state owner and transaction boundary. Unknown method/path combinations stop at the router.
Delegate one family per commit so the old dispatch facade remains a rollback point.

## Phase 5 — Settings features

Keep one `SettingsWindow` for window and tab lifecycle. Extract state-owning features for schedules,
Remote/pairing, Cloud and app scope; keep small reusable AppKit primitives together. A view does not
earn a file merely for being a view. Settings calls application services directly rather than
using `RemoteServer.serialized`.

## Phase 6 — PromptController

Do this last because AppKit lifecycle is the most coupled boundary. Extract low-risk rendering and
formatting first, then converge on at most 4–6 state-owning child controllers:

- prompt window
- session browser
- transcript pane
- DevStack pane
- prompt input/voice
- snapshot renderer

A child controller must own its views, state, timers and cancellation. If extraction requires
exposing many parent properties, withdraw the proposed boundary instead of replacing cohesion with
indirection.

## What not to split now

- The Web UI already has an effective ES-module boundary.
- Cloud production units are mostly cohesive and small.
- Translation files are parallel implementations of one schema; line count alone is not a reason
  to fragment them.
- `UsageLedger.swift` has a single data journey. Revisit it only after Orchestrator stabilizes.

## Anti-over-splitting test

Extract only when at least two are true:

1. independent reason to change;
2. independent state or lifecycle;
3. distinct dependency set;
4. narrow independently testable contract;
5. demonstrated claim/merge collision;
6. removal of a dependency cycle or cross-layer reference.

Do not extract merely for line count, for a single private helper, when both sides always change and
ship together, or when the new file needs broad access to its parent's state. A new file below about
100 lines should normally be a high-fan-in value, protocol or adapter boundary.

Guardrails:

| Kind | Expected | Warning | Stop net growth |
|---|---:|---:|---:|
| production Swift | 250–900 | 1,200 | 2,500 |
| UI feature/controller | 400–1,200 | 1,500 | 2,500 |
| test suite | 300–1,000 | 1,200 | 2,000 |
| entry/facade | 100–500 | 800 | 1,200 |
| JS feature module | 150–600 | 800 | 1,200 |

An exception names the cohesive lifecycle, owner, next extractable boundary and why splitting now
would increase coupling.

## Phase gate

Every phase records before/after commits, keeps wire/store contracts stable, does focused proof and
one phase-end exact-tree suite, adds no dependency cycle, and can be reverted as whole commits.
Approve only Phases 0–1 initially. Phase 2 begins only when all of these are true on one exact
candidate commit tree:

1. independent review has sealed every finding as fixed, disproved or deferred with an owner;
2. `expectedOrderedTestGroupTitles` equals the runtime order, the exact current Swift completion
   receipt named above passes, and the existing Cloud receipt appears exactly once with the
   existing eleven suite names and counts;
3. `tools/swift-source-manifest.sh` reports the exact on-disk recursive inventory, including a
   recorded red mutation for one missing nested source;
4. the architecture guard reports `Tests/main.swift <= 500`, 24 ordered runners, no domain group in
   the entry point, no extracted suite above 2,000 lines, and no net growth in the two frozen
   production hotspots;
5. the diff contains no production relocation, wire/store/concurrency change or new dependency
   cycle, and HEAD compiles standing alone; and
6. the Phase 2 policy list receives fresh human approval after the evidence above is reviewed.

Failure of any item leaves `architecture_hold`; it is not permission to weaken a receipt or begin a
partial production move. Later phases need the same fresh approval at their own gate. This document
is a map, not permission to run six migrations concurrently.

## Next refactor: the registry owner (measured 2026-09-02)

Approved direction: **extract the state owner**. Phase 2's pure-policy list is deliberately
skipped, and the ordering proposed before measurement is corrected below. Measurements were taken
on `main` at `61696746` with the working tree busy; re-measure before each cut.

### Why the freeze did not hold, and why pure policy would not help

`Orchestrator.swift` was put under net-growth freeze on 2026-08-29 at 11,562 lines. Three days
later it is 13,592 — **+2,030 lines, +17.6%** — through seven individually legitimate exceptions:

```
11,562 → 12,337 → 12,398 → 12,431 → 13,482 → 13,502 → 13,539 → 13,592
```

The guard's ceiling is whatever the last delivery measured, so it can prove nobody grew the file
quietly; it cannot make the file smaller, and an exception clause is always available to the next
feature. Meanwhile the four features that opened their own cohesive file —
`SessionClosePolicy` (118), `CoordinatorSuccession` (519), `OrchestratorPlanning` (552),
`TranscriptReadCoordinator` (66) — held `RemoteServer` to +41 lines over the same three days. The
pattern works on new behavior and does nothing for work that must change the existing state
machine, because that work has nowhere else to go.

Phase 2's pure policies would not change this. This document already concedes the point for
`RootAssignmentDeliveryPolicy`: moving value types and helpers leaves every state, terminal and
transcript dependency behind and produces an extension-shaped split. The growth curve would be
unchanged.

### The ownership defect, measured

| Fact | Measurement |
|---|---|
| `Orchestrator.lock` | one `NSLock`, **160** `lock.lock()` sites across the file |
| collections it protects | **19+** `static var` dictionaries, sets and arrays |
| additional independent locks | `ownershipLock`, `storeSaveLock`, `coordinationDeliveryLock`, `closingTasksLock` |
| implicit in-lock contracts | **10** `…Locked()` functions callable only under the lock |

There is no owner. There is one lock and a convention, and the convention is enforced by a
function-name suffix. Every new feature that touches task state extends the convention.

The "The store" section (941 lines) is **not** that owner: it is a serializer. Only `load()` and
`save()` touch the shared collections (4 lock sites between them); the remaining ~780 lines are 21
pure `stored(_:)` / `…(from:)` codec functions with no state and no lock.

### Corrected cut order

The pre-measurement proposal was to warm up on "Scheduled dispatches" and then take the store.
Measurement reverses it: the schedule section enters the **same** `lock` in 20+ places and
`hasActiveScheduleTaskLocked` reads the task registry inside it. Schedules cannot leave until an
owner exists.

**Cut 1 — `OrchestratorStore.swift`, the codec (~780 lines).** The 21 `stored(_:)` / `…(from:)`
functions move as pure translation between domain values and `[String: Any]`. `load()` and
`save()` stay in `Orchestrator` because they assign the shared collections. Zero lock sites, zero
state, no dependency-direction change, table-driven tests per record type. Acceptance: store decode
semantics identical for every record type including the legacy-shape branches, unchanged group and
check counts apart from the new codec groups, `Orchestrator.swift` down ~780 lines.

**Landed 2026-09-02 (`integrate/cut1`).** Twenty functions moved, not the twenty-one counted
above. The fog-of-war unknown is answered: no codec function reaches shared state through a helper,
and all twenty-three helpers they call were read. `Orchestrator.swift` is 13,592 -> 12,819 lines and
the guard ceiling drops with it. `load()` and `save()` stayed. So did the pure closure-attestation
and restart/executor codecs, which pass the same mechanical test but sit outside the section beside
the owners they belong to — the next cut to touch those owners should take them. Neutrality is
proved mechanically rather than asserted: reversing the boundary spelling and the six
`private` -> internal widenings makes the moved body byte-identical to the original under `diff`.

Two more pure codecs stayed and the delivery report did not say why, which independent review
caught: `ledgerRecord(of:)` and `completionRecord(_:)` both pass the same mechanical test — no
shared collection, no lock — but sit outside the section, against the ledger and the completion
outbox that own them. Leaving them is right for the same reason the closure-attestation pair stayed;
the cut that takes those owners should take these with it. A report that claims a mechanically
re-derived list owes the reader every function the criterion selected, including the ones it then
declined on other grounds.

**Cut 2 — `OrchestratorRegistry.swift`, the owner.** One type owns the lock and the collections and
exposes a transaction interface; the 160 bare lock sites converge on it and the 10 `…Locked()`
contracts become methods that cannot be called outside a transaction. Migrate one collection at a
time, each its own revertible commit, `Orchestrator` keeping its facade. Do not convert to an actor
in the same project — synchronization semantics stay stable while ownership moves. Acceptance per
collection: identical mutation order, restart recovery unchanged, exactly one event publication per
committed transition, and a red mutation proving the transaction boundary is enforced.

**Cut 3 — `ScheduleService.swift` (~1,119 lines).** The five schedule-only collections
(`handledScheduleFires`, `pendingScheduleFires`, `lastMissedScheduleFires`, `dispatchingSchedules`,
`invalidScheduleFingerprints`, plus `scheduleWriteTimes`) and the 34 schedule functions move
together; the single crossing to `tasks` goes through a registry read port. This section already
has its own suite, its own persistence and its own routes, so it satisfies criteria 1–4 once the
owner from Cut 2 exists.

Expected end state: `Orchestrator.swift` near 11,000 lines, and — more important than the number —
schedules, handoffs and coordination waits each have somewhere to go, so the next feature stops
paying rent in the frozen file.

### Governance correction, landed with Cut 1

This document had drifted from the executable guard. The guard is authoritative, and these are its
values on the integrated tree, read out of `tools/check-architecture-boundaries.sh` and observed by
a full suite on 2026-09-02:

| | this document said | observed after Cut 1 |
|---|---:|---:|
| ordered groups | 463 | 504 |
| ordered runners | 25 | 29 |
| suite files | 38 | 42 |
| Swift checks | — | 8,026 |
| `Orchestrator.swift` ceiling | 13,592 | 13,149 |
| `RemoteServer.swift` ceiling | 6,385 | 6,463 |

That row reads 8,026 rather than the 8,025 observed after Cut 1: the multi-question picker's
confirmation guard added one check on 2026-09-02 (`4273990a`). It is written here because the guard
now asserts this table, which is the mechanism the paragraph below asked for — the first landing to
move a count after that change is the one that proves it works, and this is that landing.

**The correction itself needed correcting three times, always the same way, and the third time an
independent reviewer had to catch it.** The section first said the guard held 480 ordered groups; at
`13bc9a10` it held **479**, because the reading came from a working tree carrying another session's
uncommitted edits rather than from HEAD. Then `main` moved to 480 and the extraction added ten, so
the figure became 490 — correct for about an hour, until tmux read parity landed 484 and the answer
became 494. The check receipt drifted along the same path: 7,918 computed, 7,941 observed, 8,025
observed again after the rebase.

Every one of those was a true reading of a tree that had stopped existing. **The defect is not
carelessness about arithmetic; it is that a count in prose has no owner and nothing makes it go
red.** `tools/check-architecture-boundaries.sh` holds the same numbers and fails the build when they
drift, which is why the guard was right three times while this table was wrong three times. The
reviewer found the third instance by re-running `git merge-tree` and noticing that this file merges
*cleanly* — the counts in `test.sh` and the guard conflict loudly and get fixed, and the table beside
them updates silently and does not. So: when a landing moves a count, the guard is the record and
this table is a copy; re-read it from the guard on the exact tree being landed, or do not write it
down at all.

The 8,025 is an observation. The implementer computed 7,918 as 7,519 plus a focused run of 399 and
never ran a full suite after its own change; that number was already stale when it was written,
because `main` had moved the baseline underneath it twice. This document's own rule applies to its
own receipts: arithmetic over unconditional checks is not an observation, and the landing root owns
the run that settles it.

Any Phase gate decision taken against the stale numbers was taken against the wrong baseline.
Replace the exception clause with a budget the guard can execute: a feature may add lines to a
frozen file only when it also names a new boundary that absorbs them, so "somewhere else to go"
becomes a check rather than a habit.

### Precondition: the tree is busy

This working copy is shared. Within one 15-minute window on 2026-09-02 the uncommitted set turned
over completely — `ReadingFreshness.swift` and `Orchestrator.swift` edits gave way to
`QuestionSteps.swift`, `SessionState.swift` and `RemoteServer.swift` edits. A cut that rewrites
13,592 lines is an enormous claim. Each cut therefore declares its claims at dispatch, starts only
when `Orchestrator.swift` is clean at HEAD, and lands as whole revertible commits before the next
cut begins.
