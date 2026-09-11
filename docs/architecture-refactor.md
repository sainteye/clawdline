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

### Project Board reading/progress correction (2026-09-08)

The Board root adds a cohesive `ProjectBoardNarrative` worker: it owns a separate timer, bounded
model admission/retry lifetime and injected candidate/generation/persistence ports (criteria 1–4).
It does not take state or evidence authority from `ProjectBoardStore`; the existing naming runner
retains the shared subprocess budget. Its focused test runner is an independent worker-boundary
suite, not a fragment extracted merely to lower a file count.

The same correction needs a narrow eight-line growth exception in `Orchestrator.replaceTask`:
the owner captures a credential-free record only after accepting a state/child-identity change and
notifies the existing Board adapter after releasing its lock. Previously Board saw dispatch and
completion but not live execution. Extracting registry mutation into the Board would invert ownership;
the next extraction remains the registry-owner migration above, not a new Board-specific task owner.

The Board Store and web view remain over their stop-growth guidance during this correctness repair.
The Store owns the atomic graph-node attribution, source chronology, mode fence and durable reading
variants together; moving private state across extension files would not reduce coupling. Its next
cohesive seam is an immutable progress-projection input/output boundary. The web view owns selection,
lazy history/report readers and refresh fencing; its next seam is the completion-report reader with
its own selection and cancellation lifetime. Neither relocation is mixed into this correction.
These exceptions do not authorize continued growth or another unreviewed feature in those files.

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

1. any risk-triggered independent review has sealed every finding as fixed, disproved or deferred
   with an owner; routine localized work records the owner's focused diff review instead;
2. `expectedOrderedTestGroupTitles` equals the runtime order, and the exact candidate emits one
   successful Swift receipt plus one structurally valid Cloud receipt with the ordered suite roster;
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

#### Cut 2's remaining stages, and the number that says whether it worked

Ordered by measured access count, because that is what decides how much of the file each stage
rewrites. Counts are identifier occurrences in `Sources/Orchestrator.swift` at the time of writing.

| Stage | Collections | Accesses | Why here |
|---|---|---:|---|
| 1 (landed) | graphAdmissions, titlesByTerminal, handoffTitlesByTerminal, suppressedRootAssignmentLabels, rolesByTerminal | ~28 | cohesive and cheap; the transaction interface has to be designed somewhere it can be wrong safely |
| 2 | the four rate windows | 27 | not a relocation: one abstraction written four times |
| 3 | secrets, sessionDeliveries, handoffDeliveries, sessionSelfStates | 48 | one shape — a per-session record keyed by id |
| 4 | coordinationWaits, handoffs | 53 | two features, one lifetime: created, delivered, released |
| 5 | rootAssignments | 49 | its own feature, and the newest, so its call sites are the least settled |
| 6 | tasks | 167 | last, alone, and only once the door has survived five stages |
| 7 | close the second door, the held-lock door | — | its ratchet reaches zero, or Cut 2 did not happen; the bare regions it leaves are owned in the W1-4 table |

**Stage 3 is one reversible capability boundary.** The original per-collection rule was written
before the four per-session families were traced through restart and projection. That trace showed
one atomic shape with two deliberate lifetimes: `sessionDeliveries` and `sessionSelfStates` persist,
while plaintext task secrets and in-flight handoff deliveries do not. Splitting the move would
temporarily give one Session two state owners and make those lifetime assertions weaker. Under the
project's large-slice policy, the root therefore moved all four behind one closed
`SessionRecordsTransaction`, kept schema v1 and the existing facade, and retained one-commit
rollback. The focused proof performs a real save / forget / load round trip across both lifetimes.
A mutation removing the self-state removal generation made three closeability checks fail; the
restored candidate passed all 53 selected checks. The held-lock adapter still represents migration
debt, not a new lock: its 48 call sites are counted by the guard, may only fall, and must disappear
with the adapter at Stage 7.

**Stages 4 and 5 moved together as W1-3, one reversible coordination-record capability.** Handoff
envelopes, handoff labels, coordination waits and Root Assignments are four durable families with
one lifetime shape — opened, delivered or settled, released, swept — and they already shared one
store snapshot, one restart settlement and one label projection. Moving them one at a time would
have given a handoff's envelope and its label two owners for a stage, so all four moved behind
`OrchestratorRegistry.CoordinationRecordsTransaction`: immutable value projections plus closed,
named transitions (`openHandoff`, `settleHandoffOpening`, `bindHandoffLabel`,
`adoptHandoffLabelIdentity`, `acceptRootAssignment`, `recordRootAssignmentTransitionReport`,
`joinCoordinationWait`, `withdrawCoordinationWaiter`, …) and no mutable collection accessor.
Schema v1, the JSON keys and codecs, the single `NSLock`, the persistence lifetimes and every
`Orchestrator` facade are unchanged, so rollback is reverting the slice; a store written by either
side is read by the other. Three things are new rather than moved. Obligation invalidation no
longer rides a `didSet` in the facade: every handoff or wait transition advances a Registry
mutation clock that `settleObligationGenerationLocked` consumes, the way session self-states
already did. Effects that ran ahead of persistence now follow a synchronous save attempt: the
`root_assignment.active` and `.briefed` audits, Root Assignment injection, restart handoff
announcement and expired-handoff directory deletion all come after that attempt. These five call
sites remain best-effort until W1-5 decides which effects must be gated on save success. The
existing withdrawals — acceptance, trust answer and transition receipt — are kept; only the trust
answer and transition receipt use compare-before-restore transitions. Separating best-effort from
gated persistence is W1-5's behavioral slice, not this extraction's. Regions that touch coordination
records take the acquiring `withCoordinationRecords` door, which took bare `lock.lock()` sites
from 160 to 123 across the three files that still hold the lock directly. The 26
`withCoordinationRecordsOnHeldLock` sites that must stay atomic with tasks or the terminal
projection are ratcheted beside the other two adapters and leave with tasks. The focused proof ran
26 selected groups on the working overlay — 679 checks, green, 37 seconds. Its red half broke two
transitions in a private copy — a withdrawn waiter no longer advancing the obligation clock, a
failed receipt save no longer restoring the receipt — and the two guarding groups failed on exactly
the six checks that name those behaviors, 6 of 132, while the neighbouring handoff-clock check
stayed green. Each new ratchet was pushed one site past its number and refused. The step paths
that type into a live terminal (activation, briefing, injection) are covered by review, not by
execution, and the full suite on the integrated tree remains the landing root's.

**Stages 6 and 7 moved together as W1-4: the task table, and the held-lock doors with it.** The
task collection was the last Registry-facing state `Orchestrator` still declared, and every
held-lock door existed because some region had to stay atomic with it. It moved behind
`OrchestratorRegistry.TaskRecordsTransaction`, reached only through the acquiring
`withTaskRecords` door; the table is `private` to `OrchestratorRegistry.swift`, and
`tools/check-architecture-boundaries.sh` pins that declaration. The capability exposes value
projections (`task`, `tasksByID`, `taskValues`), the serialize, claims and root-key admission
queries, the terminal index rebuild that used to be `Orchestrator.reindex`
(`rebuildTerminalProjection`), and three kinds of write. The first delivery called all of them
"closed row transitions"; only the first kind is closed.

- **Named transitions** choose their own guard and write only their own fields. `admitTask`
  inserts and never replaces. Finalization commits the terminal state in one hold and then reads
  the result file, the transcript's usage and the claimed paths outside it, so its follow-up facts
  go through `recordUntouchedClaims`, `adoptResultReceipt`, `recordHarvestedUsage` and
  `scheduleReclaim`: each writes only while the row still exists and still carries the committed
  outcome, and finalization adopts the row as written. A step walker's value copy — `replaceTask`
  from activation, briefing, watch, expiry and close settlement — goes through
  `commitStepCandidate`, which takes progress notes, the file note, the notification count,
  released claims, the landing, the completion envelope and finalization's facts from the row as it
  stands rather than from the copy. `releaseClaims` appends inside the hold. The
  restart-reconciliation and completion-recovery rollbacks restore, per row, only the fields their
  own step wrote (`restoreExecutorReceipts`, `restoreCompletionRecovery`), instead of a table taken
  before the save. None of these can re-create a swept row.
- **Same-hold read-modify-write** — `updateTask` and `commitTask` — remains a row-scoped
  capability, not a closed transition: the caller computes the change, and it loses no concurrent
  write only because the change comes from the row read in the same hold. Review keeps that, not
  the compiler. Neither creates a row.
- **Whole-table writes** are `load()`'s `replaceAllTasks` and `forget()`'s `removeAllTasks`;
  `seedTaskForTesting` is the one upsert, for fixtures. The guard counts the production callers of
  the upsert and of the table replacement.

What these do not close is named rather than implied. Among the step walkers of one state, the
walkers' own fields — state, identity, inject counters, deadlines, executor receipts, interventions
— are still last-writer-wins. Landing, completion-delivery and progress transitions are still
same-hold read-modify-write in `Orchestrator`, correct by construction but not named Registry
operations; moving them behind expected-state named operations is a follow-on node, not W1-4.

The same hold derives `registry`, `sessionRecords` and `coordinationRecords`, and that derivation
is what let the second door close: a region atomic across families acquires once and hands its
capability down. The registry helpers that used to say "caller holds the lock" now take the
capability as a parameter, so a caller must have one — except `noteActivityLocked`, which touches
only `Orchestrator`'s own activity counters and still carries the unchecked convention. No call
site returns, stores or captures a capability; Swift 5 cannot forbid it, so that is review's to
keep. `withTransactionOnHeldLock`, `withSessionRecordsOnHeldLock` and
`withCoordinationRecordsOnHeldLock` are deleted, and their ratchets (12, 48 and 26 sites) are
replaced by a calibrated check that no code declares or calls a door ending in `OnHeldLock`. **That
held-lock door is what the W1-4 gate means by "direct door reaches zero", and it is zero.** The
`Task` mutation `didSet` became a Registry clock consumed at settlement, the way the session and
coordination clocks already were.

Six files outside the trio read or wrote the table and now enter the task door:
`OrchestratorInventory`, `OrchestratorLandingQueue` and `OrchestratorLandingSweep`, which took
`Orchestrator.lock`; `Coordinator`, whose restart reconciliation and its rollback write rows; and
`UsageFeatureAttribution` (three snapshot reads) and `UsageLedger` (one). The slice's path set was
extended twice during implementation, the second time by progress note only; the correction wave
ratifies the whole 18-path union, including `Coordinator.swift`, `UsageFeatureAttribution.swift`
and `UsageLedger.swift`.

**W1-4 does not complete W1.** The W1 gate is *zero externally held owner locks*, and the shared
lock is still taken bare in 30 regions guarding state `Orchestrator` declares itself: 21 in
`Orchestrator.swift` (bare `lock.lock()` in the three files that took the registry lock directly
fell from 123 to 21) and 9 in `Coordinator.swift`'s `extension Orchestrator`, which the three-file
count never saw and which now has a ratchet of its own. None reaches a Registry collection. Two
task-door reads also probe the filesystem inside the hold. Each is owned by the boundary that will
move it; the counts may only fall.

| Region (function) | State it guards | Next boundary |
|---|---|---|
| schedule removal, schedule inventory ×2, manual run, terminal-admission retry, stale-fire skip, dispatch settlement, `handledScheduleFireForTesting` — 8 | `handledScheduleFires`, `pendingScheduleFires`, `lastMissedScheduleFires`, `dispatchingSchedules`, `invalidScheduleFingerprints` | W2-1 scheduling (Cut 3's schedule owner) |
| `scheduleCompletionPump`, `completionPump` ×3, `completionAttempt`'s generation check — 5 | `completionPumpScheduled`, `completionPumpGeneration` | W2-1 event publication |
| `beat`'s in-flight counter — 1 | `beatsInFlight` | W2-1 scheduling |
| `activityGeneration(ofTerminal:)` — 1 | `sessionActivityGenerations` (also written by `noteActivityLocked` inside other holds) | W2-1 event publication |
| closeability read counter ×2 — 2 | `closeabilityRegistryReadCountForTesting` | W2-2 command admission (the closeability query) |
| `readResult`'s bad-secret record — 1 | `badResults` | W2-2 command admission (result intake) |
| `dispatchToken()`, `archiveKey()` — 2, **reading and writing their files inside the hold** | the orchestrator token and archive-key files | W1-5 persistence health: an unreadable file is replaced by a fresh mint today |
| `load()`'s flag — 1 | `loaded` | W1-5 persistence health: the flag is set before the read, so an unreadable store leaves empty tables authoritative |
| `Coordinator.swift` restart maintenance: begin ×2, advance ×2, abort ×2, current record, admission check, resume — 9 | `restartReceipt` | W2-2 command admission (restart maintenance) |
| `scheduledResumeTitle` → `availableScheduledSessionID`: `fileExists`, transcript ownership, `Codex.head` — **inside the task door** | task rows | W2-2 command admission (the place-resume query): take rows in the door, probe outside, revalidate |
| `tasksUnder` (root-close cascade) → `provenChildSessionID` — **inside the task door** | task rows | W2-2 command admission (session close): the same shape |

Schema v1, the `tasks` key and its codec, the single `NSLock`, the rate and capacity limits,
idempotent replays and every `Orchestrator` facade are unchanged, so rollback is reverting the
slice, and a store written by either side is read by the other. What the correction changed is
behaviour under a race only: a finalization stage or a step commit no longer erases a fact
committed after its copy, a swept row is no longer re-created, a second admission of one id is
answered as the idempotent replay it would have been a moment earlier, and a failed-save rollback
no longer puts back rows or fields another writer changed meanwhile.

Effects follow the door, but **persistence does not precede every terminal send, and the first
delivery said it did.** The one audit that ran with the lock held — `replaceTask`'s stale-write
refusal — now runs after the door returns, and no effect moved into a hold. The ordering on this
tree, read from the code:

- queued activation (`startQueuedTaskIfEligible`): commit `spawning` → `save()` → `spawn`;
- direct dispatch: admit in memory → `spawn` → `save()`;
- briefing injection (`brief`): `Targets.send` → in-memory `replaceTask` → `orchestrator.brief.inject`
  audit → the step's `save()` on the main queue after `brief` returns;
- startup-menu answer: in-memory `replaceTask` → `Targets.answer` → audit → that same `save()`;
- briefed acceptance: in-memory `replaceTask`, secret discarded → `orchestrator.brief` audit → that
  same `save()`.

A crash between a send or an acceptance and that save reloads the row as `spawning`, and startup
recovery records `spawn_failed` — "The app restarted before the child was briefed." — even when the
child has the line. The ordering predates W1-4. Its owner is **W2-1**, which owns the terminal step
lane these sends run on; which effects must further wait for a *successful* save is W1-5's
decision. The mutation proof below exercised two orderings and no terminal send: a progress note's
save runs after the task door released the lock, and a session delivery's save precedes its push.

The focused proof was one `--swift-focused` selection of 239 groups — every group in the suites
that exercise a converted region (dispatch, completion, landing, landing queue, recovery,
coordination, Root Assignment, scheduled dispatch, background and storage, landing currency,
closeability, close and quota, store, lifecycle, work state, Coordinator, usage portfolio and the
registry itself) plus the delivery-push group — run on the working overlay: 4,373 focused checks,
green, 74 seconds including the compile. Two new registry groups carry the new failure classes.
Their red half injected two defects into a private copy — a row update that no longer ticks the
task mutation clock, and the save interceptor called inside the task door — and the two groups
failed on exactly the three checks that name those behaviors, 3 of 15. The zero-door guard was
shown red on a copy carrying one `withTransactionOnHeldLock` call. The step paths that type into a
live terminal remain covered by review rather than execution, and the full suite on the integrated
tree remains the landing root's.

The correction wave that followed the sealed review was proved the same way, once: one
`--swift-focused` selection over the same suites plus the new seam group, green on the corrected
working overlay. Its red half ran the seam group alone on the pre-correction overlay, carrying only
the test hook and the group, and failed on exactly the three checks that name the defect: a later
finalization stage reset a notification count committed after the terminal commit, a swept task
was re-created, and a step walker's older copy erased a notification count and a progress note
committed after it. The task-door guard was shown red on a private copy twice — a production caller
of `seedTaskForTesting`, and a tenth bare lock in `Coordinator.swift`'s extension. The reliability
baseline was resealed for `Orchestrator.swift` and `Coordinator.swift` only. The full suite on the
integrated tree remains the landing root's.

**W1-5 makes broker persistence health authoritative instead of treating every read failure as an
empty registry.** `OrchestratorPersistence` owns the byte boundary. An absent store is a valid
first run; a readable schema-v1 object with a complete, uniquely keyed row set is ready. Malformed,
partial, duplicate-row, unreadable and future-version stores are typed non-authoritative states.
Readable rejected bytes stay at the canonical path and also receive a content-addressed `0600`
quarantine copy. Neither `load()` nor `save()` replaces them with an empty schema. SSE and Cloud
snapshots omit task/schedule authority while unhealthy, and orchestrator routes fail with the same
typed `orchestrator_store_unavailable` state; the storage diagnostic remains readable. A repaired
canonical file is adopted only on a fresh process start, not by a mid-process implicit reload.

The token and queued-secret key use the same absence-only rule: creation may win only when the
path is absent, while malformed, unreadable, non-regular and symlink paths are preserved and make
authentication or queued-secret crypto fail closed. Store schema remains version 1 and valid old
rows round-trip unchanged, so rollback is still a source revert rather than a data migration.

W1-4's five named best-effort effects now cross a successful save boundary. Root Assignment
activation and briefing audits roll back their owned fields when the save is refused; injection
rolls back the unsent attempt before any terminal bytes; restart recovery publishes no handoff or
assignment announcement until its recovered state is durable; cleanup persists the registry
transition before removing task/worktree or expired-handoff directories. A rejected compound
recovery or cleanup mutation reloads the last authoritative image so a later unrelated save cannot
smuggle it to disk and the next pass can retry it. Direct dispatch and the terminal briefing lane
remain explicitly owned by W2-1; W1-5 does not claim those pre-existing orderings are closed.

**Stage 2 is the one that is not a relocation.** `dispatchTimes`, `notifyTimes`,
`notifyCredentialFailureTimes` and `scheduleWriteTimes` are four `[Date]` arrays carrying the same
five operations verbatim — expire by window, check room, take one, give one back (two of the four),
clear on cleanup — differing only in `window` and `limit`. `notifyTimes` carries the
filter/guard/append triple **twice**, at two separate call sites, which is what four copies of an
abstraction cost. One `RateWindow` with `take()` and `giveBack(_:)` replaces all of it, and "give
back" acquires a name for the first time. It changes call sites in a way the pure moves do not, so
it needs red-before-green tests per window rather than one shared test.

**Stage 7 is the acceptance test for the whole cut, and it is now mechanical.** Stage 1 opened a
second door, `withTransactionOnHeldLock`, which does not acquire the lock and therefore trusts its
caller exactly the way the `…Locked()` suffix did. That is defensible as a migration step and
indefensible as a destination. The guard now ratchets its call-site count: 12, may only fall, and
**fails when it reaches zero** so that the door and the ratchet are deleted in the same commit.

So the honest measure of Cut 2 is not how many lines left `Orchestrator.swift`. **It is whether
`withTransactionOnHeldLock` still exists.** A cut that stalls with both doors open has renamed the
convention rather than removed it, and left the file worse than it found it: two ways to reach the
same state where there was one.

**Cut 3 — `ScheduleService.swift` (~1,119 lines).** The five schedule-only collections
(`handledScheduleFires`, `pendingScheduleFires`, `lastMissedScheduleFires`, `dispatchingSchedules`,
`invalidScheduleFingerprints`, plus `scheduleWriteTimes`) and the 34 schedule functions move
together; the single crossing to `tasks` goes through a registry read port. This section already
has its own suite, its own persistence and its own routes, so it satisfies criteria 1–4 once the
owner from Cut 2 exists.

Expected end state: `Orchestrator.swift` near 11,000 lines, and — more important than the number —
schedules, handoffs and coordination waits each have somewhere to go, so the next feature stops
paying rent in the frozen file.

### Splittability is a property of variable scope, not of size

`tools/` gained a mechanical splitter during the CloudAccountTests work: it takes a run of
consecutive top-level statements and lifts it into a nested `async throws` section, but only when
no declaration made inside that run is referenced after it. On `runCloudAccountTests` it produced
twenty-eight sections and took the file's codegen peak from 46.06 GiB to 0.83.

Pointed at `runCloudCommandLedgerTests`, which had the same symptom — 131 suspension points in one
coroutine, second worst in the tree — **the same tool produced one section of 635 lines and no
improvement at all.**

The difference is not size. That function's top level is two declarations and twenty-five
`if checks.includes("group")` blocks, each carrying its own fixture and referencing nothing from
its neighbours. There is no *run of statements whose declarations die with them* to find, because
every statement is inside a block. The tool looked for the wrong shape and correctly reported that
it was absent.

What the file needed was already there: twenty-five named groups, each a natural boundary. Wrapping
each block's body in a nested `async throws` took the largest coroutine from 131 to 18 and moved
nothing — same statements, same order, same 84 assertions, byte-identical string literals.

**So: before reaching for the splitter, look at where the declarations live.** A function whose
top level is a flat sequence of statements splits mechanically. A function whose top level is a
list of self-contained blocks is already split and only needs each block given its own coroutine.
A function whose long-lived values thread through everything splits neither way, and that is the
one to leave alone until its state has an owner.

### A ratchet and a threshold are not the same guard

The suspension-point guard was a ratchet at 131 for as long as `runCloudCommandLedgerTests` sat
there: a number with no meaning except *today's worst*, held only to stop it climbing, and
justified only because that worst value was itself under the measured cliff. With the tree's worst
now 61, it is a threshold with a derivation instead — 100, three tenths of margin below a cliff
measured between 131 and 143, and far enough above 61 that ordinary test growth does not trip it.

The distinction is worth stating because the two look identical in the script and behave opposite
in practice. **A ratchet at today's value is right for a quantity that can only fall by deliberate
work** — the held-lock door count is one, because every one of those call sites has to be moved by
hand. **It is wrong for a quantity that grows in the ordinary course of the work**: a ratchet at 61
goes red the first time somebody adds five awaits to a test, and the lesson it teaches is to raise
the number, not to split the function. Zero headroom is correct for the first kind and a trap for
the second.

### Governance correction, landed with Cut 1

This document had drifted from the executable guard. The correction landed with Cut 1; on 2026-09-03
the repair went further than restating the numbers, and the heading is left naming the first half so
that the two are not read as one event: **the table below is not written by hand at all.**
`tools/check-architecture-boundaries.sh --emit-governance-table` renders it from the values that run
just computed, `tools/generate-governance-table.sh` writes it between the markers, and the guard
compares the committed block against the same rendering before a compiler is started. The six values
this table once duplicated were, in order, 463 ordered groups, 25 ordered runners, 38 suite files, no
Swift-checks row at all, a 13,592-line `Orchestrator.swift` ceiling and a 6,385-line `RemoteServer`
one — every one of them wrong about the tree, and none of them attached to anything that could say
so. The third column below is the point of the exercise: each row now names the one place its number
is written, and this document is not that place for any of them.

<!-- clawdline-governance-table:v1 -->

| | value on this tree | the one place it is written |
|---|---:|---|
| ordered groups | 629 | `Tests/TestGroupManifest.swift`, counted by the guard |
| ordered runners | 49 | `Tests/main.swift`, counted by the guard |
| suite files | 62 | `Tests/*Tests.swift`, counted by the guard |
| `Orchestrator.swift` ceiling | 10,718 | the ratchet in `tools/check-architecture-boundaries.sh` |
| `RemoteServer.swift` ceiling | 5,831 | the receipt in `tools/check-architecture-boundaries.sh` |

<!-- /clawdline-governance-table:v1 -->

The `Orchestrator.swift` ceiling has moved in both directions and the guard now carries that
history beside the number: 12,816 before the heavy-compile lease, 13,123 when `2eef7bb6` landed it,
13,085 once about sixty lines of pure projection moved to `Sources/OrchestratorLease.swift` where
the type they project already lives, and **12,831 when that lease was removed whole** — the largest
fall the ceiling has had, and the one place in this history where the number fell because a feature
went rather than because code moved. A ratchet that reads as "this only falls" is worth less than
one whose raises are visible; one that cannot tell a removal from a refactor is worth less again,
which is why the guard's history block names which of the two each line was.

Executed Swift and Cloud check totals are intentionally absent from this table. They are facts
about one run, so the canonical run receipt records them together with the exact tree, command,
environment and log digest. Copying the previous tree's totals into `test.sh` and contributor docs
created a self-invalidating loop: the first successful full run changed source metadata and forced
a second identical full. Structural completeness remains enforced by the ordered group, runner,
suite-file and Cloud-suite rosters; observed assertion totals remain receipt telemetry.

### What stage 1 proved, and what it declined to do

**The bypass proof, kept here because it lived only in a task report that is not in version
control.** Two deliberate probes were compiled against the integrated tree; both failed to build,
which is the claim the whole boundary rests on:

```
'titlesByTerminal' is inaccessible due to 'private' protection level
'OrchestratorRegistry.Transaction' initializer is inaccessible due to 'fileprivate' protection level
```

A third probe — legitimate use through `withTransaction` — is the control, and the full suite is
what runs it.

**"The five smallest collections" is not literally true, and the choice still stands.** `badResults`
has 2 access sites and `batches` 5, so both are at or below the five that moved. They stayed because
smallness was the tie-break, not the criterion: what stage 1 needed was collections whose accesses
are *cohesive* — the four per-terminal facts are one projection rebuilt together, and
`graphAdmissions` is one reservation with a public release. A stage assembled purely by ascending
access count would have mixed unrelated lifetimes and taught the transaction interface nothing.

**The plan asked for one collection per revertible commit; this stage moved five in one.** That is a
deviation from a stated acceptance condition, and it went the useful direction — the five are one
cohesive group and splitting them would have produced four commits that do not compile standing
alone, which `docs/landing.md` forbids. Later stages carrying independent collections should hold to
the original rule.

**The heading used to carry a provenance, and that is the shape this section keeps re-learning.**
It said `observed after Cut 1`, and for the Swift-checks row that was not true when it was written:
no full suite had run on the tree it described. Not a wrong arithmetic under a right label — a right
number under a wrong one. So the heading now names what every cell in it actually is, and where a
number came from is said per row, in prose, rather than by one heading that would have to be true of
six different things at once. **A column heading is the smallest place in this document that can
lie, because it is the one part nobody re-reads when a cell changes.**

Historical landing runs recorded their own observed check totals. Those receipts remain evidence
for their exact trees, but the totals are no longer copied into this document or `test.sh`: ordered
rosters prove completeness, while each run records its own changing assertion count.

**Closing that delivery's review moves it by six more, to 8,058.** Four are in *the page is given the
words it draws the start sheet with* (9 → 13): a language keeping a `{app}` hole in the one sentence
that has no name to fill it, the three pages no longer asking for a name this refusal never carries,
what each of them actually draws rather than only what it asks, and the mock offering the shape the
server really sends. Two are in *which terminal a session is started in, and when none of them will
do* (34 → 36), for a hand-typed `terminal` value being named as discarded rather than silently
replaced. No new group, runner or suite file. **8,058 is a reading of this correction's own branch
and not a prediction about `main`**, which gains another line's 8,087 in between: the landing root
recomputes the absolute from its own tree, and what carries across the merge is the delta of six.

**How that 26 was measured, since no full suite could be run the night it landed.** One file,
`Tests/CloudAccountTests.swift`, was measured at a ≥23.65 GiB lifetime maximum on a 24 GB Mac, so
whole-tree codegen was refused machine-wide. Each of the three groups was instead run **on its own**
through `CLAWDLINE_TEST_GROUPS`, twice — once as delivered and once with this slice's additions
patched back out — against a binary whose only difference from the shipped one is that expensive
file, stubbed to its two signatures:

| group | on `main` | after `47925577` | delivered |
|---|---:|---:|---:|
| which terminal a session is started in… | 12 | 31 | **34** |
| the page is given the words it draws the start sheet with | 7 | 7 | **9** |
| a screen read to decide something is the screen… | 8 | 8 | **10** |

`8,026 − 12 + 34 + 2 + 2 = 8,052`. The middle column is measured; the `main` column for the first
row is a static count of `git show 47925577^`, whose twelve calls are all unconditional and outside
any loop. **The first full suite to run this tree is still the authority** — if it disagrees, the
arithmetic above is what was wrong, not the code, and both places move together.

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

### MARK boundaries are narrative, dependency boundaries are what compiles

Cut 3 moved two blocks out by their `// MARK:` spans and the first compile failed:

```
Sources/OrchestratorChildBrief.swift:111:26: error: 'policySection' is inaccessible
                                                   due to 'private' protection level
```

Swift's `private` is scoped to a file *and* a type, so a `private static func` that the
whole `enum Orchestrator` could call becomes invisible the moment the caller moves to an
extension in another file. `policySection()` had exactly one caller — the briefing that was
being moved — and sat at lines 9,551–9,579, outside the 9,849–10,230 MARK span. Cutting on
the MARK alone could not have kept them together.

Three things were checked before the cut and all three passed: indentation depth (nested,
not top level), stored-instance-property count (an extension cannot hold them; there were
none), and `swiftc -parse` on the new files. None of them can see this. **What was missing
was not a check but a criterion: which `private` members declared in the original file does
the moving block call?**

That is answerable with grep before cutting, and two lines wrote it independently. Both
first versions matched the bare name and both reported four or five hits on a block that is
mostly briefing prose — `record` and `shape` are ordinary English words. Stripping string
literals (including `"""` bodies) and requiring a call position (`name(` or `.name`) took
both down to the single real one. The two implementations disagreed about which false
positives they produced, which is what makes their agreement on the answer worth something.

Its limit travels with it: it sees direct calls only. A private member reached through
`Self.`, through a typealias, or passed as a function value is invisible to it. **It is a
cheap pass before cutting, not a promise that the cut compiles — the compiler is still the
subject.** This cut bought that answer with a full suite compile; the next one need not.

### `extension` and a new `enum` are both correct, and they cost different people

The draft extraction moved 37 static symbols into `enum OrchestratorDraft`, so
`Orchestrator.isTaskID` became `OrchestratorDraft.isTaskID`. Every branch written before it
landed still spells the old name, and git says nothing: the two sides touch different lines,
so the merge is clean and the compiler is the first thing to object —

```
Sources/OrchestratorStore.swift:285: error: type 'Orchestrator' has no member 'isTaskID'
```

The hard part is that it does not read as suspicious. The other six calls in that same file
were already rewritten by the extraction, so the surviving one looks like its neighbours;
the only difference is the type name, and a type name is not what anyone diffs for.

This cut moved 875 lines into `extension Orchestrator` instead, so no symbol left the
namespace and nothing downstream can break this way. That was not foresight — the blocks
hold eighteen nested types and renaming them would have touched every call site, so the
extension was the only cheap option. The benefit is worth naming anyway, because the next
cut has to choose:

```
enum NewThing { … }          names the dependency in the type system;
                             every queued branch pays one post-merge compile
extension Existing { … }     invisible to queued branches;
                             the grouping lives in the filename, not in the types
```

Neither is the right answer in general. The cost of the first one falls on people who are
not in the room when the choice is made, which is the part that is easy to leave out of it.

A one-minute check for anyone merging onto a tree that has had an `enum`-style extraction:
list the moved symbols out of the new file, then grep your own diff for `Existing.<name>`
against that list. Calibrate it first — a known-moved symbol must appear in the list, and
the count of such calls in the post-extraction tree must be zero.
