# Architecture refactor blueprint

Status: design, not an authorization to rewrite the application.

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
Approve only Phases 0–1 initially. Phase 2 begins only after the extracted harness preserves group
order and the sealed Phase 0 baseline B. Later phases need a fresh approval at their gate; this document is a map,
not permission to run six migrations concurrently.
