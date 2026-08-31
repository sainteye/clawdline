# Machine resource scheduling

Status: design proposal; not implemented or authorized for rollout.

Clawdline can keep many assistant Sessions useful at once. Most of those Sessions spend most of
their time reading, reasoning, editing or waiting, which is cheap to run concurrently. The trouble
starts when several of them independently enter an expensive machine operation such as compiling,
running a full test suite, indexing a checkout, signing an application or replacing the installed
bundle. Each operation may be correct in isolation while their overlap makes the Mac unresponsive,
extends every build, increases memory pressure, and lets two installers race at the final promotion
boundary.

This document proposes a machine-local resource scheduler for that boundary. It is written as an
open-source design: the protocol, policy and fallback must work without a hosted Clawdline service,
must expose what the machine is doing, and must remain useful to a contributor who runs the scripts
directly rather than through an orchestrated task.

Nothing on this page describes a feature the current build already has. The current mechanisms are
the baseline below; later sections are a possible direction and carry unresolved decisions plainly.

## The current boundary

Clawdline already has three related protections, but none of them is a resource scheduler:

- Task `claims` reserve repository paths. They prevent conflicting roots from being briefed onto
  the same source, not two safe builds from competing for CPU and memory.
- Task `serialize` names a machine-global mutex. It is FIFO and restart-aware, but only a dispatched
  task that declared the token participates, and it holds the token for the task's whole queued,
  spawning and briefed lifetime rather than for the few minutes spent compiling.
- Restart maintenance protects terminal admission while `build.sh` replaces the running app. It
  does not govern the earlier `swiftc`, test or packaging work.

Both `build.sh` and `test.sh` invoke `swiftc` directly. A Root Session, contributor shell, CI job or
third-party tool can therefore start an expensive build without appearing in the orchestrator's
serialize queue. Private `TMPDIR` values prevent fixed test-output collisions, but they do not stop
CPU, RAM and disk contention.

The distinction is important:

| Mechanism | Question it answers |
|---|---|
| path claim | May these two tasks edit the same files? |
| correctness lock | Would these two operations corrupt or overwrite shared state? |
| capacity lease | Can this machine run both operations now without unacceptable contention? |

A complete design needs both of the last two. One global `build` mutex is safe but unnecessarily
serial; unrestricted parallelism is flexible but lets every Session make the same locally rational
decision at once.

## Goals

1. Keep interactive Clawdline, terminal and browser work responsive while background work runs.
2. Prevent destructive overlap at install, restart, signing, device and fixed-output boundaries.
3. Admit expensive work according to measured CPU, memory and I/O capacity rather than Session
   count.
4. Acquire resources only around the operation that consumes them, not around an assistant's whole
   reasoning and editing lifetime.
5. Give local scripts, dispatched tasks, Root Assignments, schedules and CI one interoperable
   protocol.
6. Coalesce identical build requests so the same tree and toolchain are compiled once.
7. Make queue position, holder, pressure, cancellation and failure visible without exporting
   machine telemetry.
8. Survive the Clawdline app being rebuilt or restarted without forgetting a live build or granting
   the same exclusive resource twice.

## Non-goals

- This is not an operating-system security boundary. An arbitrary local process can still launch
  `swiftc` outside Clawdline's wrappers.
- It does not schedule assistant tokens, decide which product feature matters most, or replace the
  task graph.
- It does not make tests parallel-safe. Test-state isolation remains a separate correctness
  requirement.
- It does not promise that two compilers are faster than one. Concurrency is admitted only after a
  benchmark proves the capacity policy on the relevant machine.
- It does not require a hosted coordinator, account, analytics service or proprietary build farm.
- It does not authorize an always-on helper, new API route or build-script change. Those are later
  implementation decisions.

## Principles

### Schedule operations, not Sessions

Ten Sessions that are reading are cheaper than one full Swift compile. Session count is therefore a
poor proxy for pressure. A Session remains free to reason and edit; it requests a lease immediately
before starting the heavy process and releases it immediately after that process has settled.

### Keep correctness and capacity separate

Correctness resources are exclusive: two installed-app promotions, two restart-maintenance owners,
or two writers to one device cannot overlap even on a large machine. Capacity resources are
quantitative: a compile may request CPU units and a memory reservation, and the scheduler may admit
more than one when policy and current pressure allow it.

An operation may require both atomically. It never holds `cpu.compile` while waiting for
`install.clawdline`, because partial acquisition would create resource-order deadlocks and waste
capacity.

### Admission is not execution

The protocol must preserve distinct receipts:

```text
requested -> queued -> granted -> process_started -> process_exited
          -> result_observed -> released
```

`granted` says capacity was reserved. Only a process receipt says the command began. A successful
exit is not proof that the caller observed or accepted the artifact. A crash between any two states
must not produce a second grant or a permanent lease.

### Preserve room for the person

The scheduler does not aim for 100% CPU utilization. Its default policy reserves capacity for the
UI, terminal automation, the browser, filesystem metadata and the person using the Mac. Memory and
thermal pressure can reduce admission below the configured maximum; they never increase it.

### Fail visibly and conservatively

A broker outage, stale holder, impossible resource request, full queue and pressure refusal have
different typed results. A script must not silently bypass coordination because the broker did not
answer. Exclusive promotion fails closed; an ordinary compile may use the documented conservative
file-lock fallback.

## Proposed architecture

### 1. A machine-local lease authority

One authority owns the queue and lease lifecycle for the whole Mac. It should not exist only in the
Clawdline app process, because `build.sh` replaces and restarts that process. Two implementation
directions remain viable:

- a small open-source helper/LaunchAgent with a local socket and durable receipt store; or
- a filesystem-first lease helper whose kernel-held locks survive the app restart and whose queue
  can be reconstructed by the next Clawdline process.

The first supports quantitative scheduling and fair queues more naturally. The second has a much
smaller installation and trust surface. Phase 0 below deliberately measures the problem before
choosing between them.

The authority is local-only. It stores resource names, owner identity, request timing, declared
units, process identity and result receipts. It does not store prompts, transcripts, source code or
credentials, and it sends nothing off the machine.

### 2. Operation-scoped clients

The scripts acquire leases at the point of use rather than relying on the task briefing:

```text
clawdline-resource run \
  --class compile.swift \
  --cpu-units 6 \
  --memory-bytes 8589934592 \
  --priority acceptance \
  -- ./test.sh --compile-only
```

The exact CLI is not settled. Its important properties are:

- the command is an argv, not a shell string;
- the request has an idempotency id and an exact owner;
- the helper launches or observes one exact pid/process-start tuple;
- signals and exit status propagate to the caller;
- cancellation releases queued work immediately and running work only after the process is
  stopped or explicitly detached;
- the lease has a bounded heartbeat/reconciliation contract rather than an arbitrary wall-clock
  timeout that can free resources while the process still runs.

Dispatched tasks may declare expected resources for planning and UI, but the execution wrapper is
the authoritative acquisition. A declaration without an acquired lease cannot be rendered as
running.

### 3. Resource classes

The initial vocabulary should be small and closed:

| Resource | Shape | Initial intent |
|---|---|---|
| `cpu.compile` | quantitative | Swift or another compiler frontend |
| `memory.heavy` | quantitative bytes | compilation, linking and large test processes |
| `io.heavy` | quantitative or single slot | archive, index and large checkout scans |
| `install.clawdline` | exclusive | installed bundle promotion |
| `restart.clawdline` | exclusive | restart-maintenance and reconciliation boundary |
| `signing` | exclusive initially | codesign and interactive Keychain access |
| `device:<stable-id>` | exclusive | one physical device or simulator target |
| `port:<number>` | exclusive | a fixed local listening port |

Arbitrary task-supplied strings are useful for correctness mutexes but unsafe for quantitative
policy. Resource classes that influence CPU or memory admission therefore need a versioned schema.

### 4. Capacity policy

Defaults must be conservative and derived from observable machine facts. A possible starting policy
is one heavy compiler at a time, at least two logical cores and 25% of memory left unreserved, and
no new heavy admission while macOS reports material memory or thermal pressure. Those figures are
hypotheses, not constants for the implementation.

Policy inputs may include:

- performance/logical core count;
- physical memory and current pressure class;
- recent peak RSS for the same job class;
- thermal state;
- whether an interactive foreground operation is waiting;
- measured throughput from controlled one-versus-two-job experiments.

The scheduler must record why a request was admitted or held. `queued` without a reason is not an
explanation.

### 5. Priority and fairness

Priority is derived from lifecycle rather than chosen freely by each assistant:

1. a user-blocking interactive operation;
2. the landing graph's one exact-tree acceptance;
3. focused compile or focused test;
4. ordinary feature implementation;
5. review, background research and scheduled maintenance.

Within a class, requests are FIFO with aging. Aging prevents background work from starving, while a
bounded interactive reserve prevents a long background queue from making the app appear frozen.
Priority changes queue order, not correctness: it never preempts an exclusive promotion already in
its irreversible phase.

### 6. Split build, test and promotion

`build.sh` currently presents one command, but the resource boundary should distinguish:

```text
compile -> package -> verify -> promote/install -> restart/reconcile
```

The expensive compile may run before an exclusive install lease exists. Promotion remains short,
atomic and exclusive. Test compilation and test execution also become distinct: a focused test can
reuse a compiled runner, while only a full execution may mint the full-suite receipt.

This follows the runner direction in `docs/verification-workflow.md`: compile once, execute focused
groups many times, and reserve one exact candidate-tree full run for the landing Root.

### 7. Build identity, caching and coalescing

A build key must include every input that can change the artifact:

```text
source/tree digest
+ compiler executable and version
+ target triple
+ flags and frameworks
+ deterministic source manifest
+ generated inputs and relevant tool versions
```

When several callers request the same key, one becomes the producer and the others become followers.
Followers observe the producer's receipts and reuse only a completed, verified artifact. A failed or
cancelled producer does not become a green cache entry. A different key never shares mutable build
output.

Caches are content-addressed and read-only to consumers. This is especially important for isolated
worktrees: sharing a writable build directory would reintroduce the interference that isolation was
created to remove.

### 8. Ownership and recovery

Every request identifies its origin when available:

- terminal-neutral Session and process-bound conversation;
- task or Root Assignment id;
- repository and exact tree;
- launched pid and process start;
- nullable graph node and landing owner.

The authority periodically reconciles the process tuple. A missing process after a complete local
observation may release capacity; stale or incomplete observation produces `unknown`, not a release.
After a restart, a durable running receipt plus a still-live exact process reconstructs the lease.
A durable grant with no process-start receipt is reconciled separately and cannot be treated as
running or silently granted again.

### 9. User-visible state

Clawdline should show, for each queued or running operation:

- resource class and units;
- queue position and wait age;
- holder and owning Feature when known;
- admission/hold reason;
- current pressure class;
- whether the request may be cancelled;
- producer/follower relationship for a coalesced build;
- last durable receipt.

The page must keep `missing`, `zero`, `unknown`, `refused` and `not yet measured` distinct. A single
spinner would collapse the very states the scheduler exists to make visible.

## Open-source and trust boundary

The scheduler is infrastructure on a contributor's computer, so its trust story must be smaller
than its convenience story:

- The complete protocol, store schema, policy defaults and CLI remain in this repository.
- The default installation works locally without a Clawdline Cloud account.
- No command text, path, pressure sample or usage history is uploaded.
- The broker accepts only local callers under an explicit authentication or same-user boundary.
- It executes only the argv supplied by the caller; it does not fetch code or interpret a shell
  program.
- Configuration is inspectable and versioned. Unknown versions fail closed rather than being
  replaced.
- A contributor can disable the optional daemon and use the conservative documented lock path.
- The feature cannot claim to control processes that bypass its wrapper. Detection may warn, but it
  must not present advisory observation as enforcement.
- macOS can be the first implementation because Clawdline is currently a macOS app, while resource
  names and lifecycle receipts should avoid unnecessary Apple-only semantics.

## Failure behavior to design before implementation

The acceptance suite needs failure injection for at least:

- authority restart after request, grant, process start, process exit and result observation;
- caller crash while queued and while running;
- command starts but the process receipt cannot be persisted;
- lease persisted but process launch fails;
- incomplete or stale process inventory;
- duplicate request ids with same and different content;
- one request needing several resources atomically;
- priority inversion and starvation under a continuous interactive workload;
- memory pressure rising between queue and grant;
- two identical build keys and a producer failure;
- cache metadata present with a missing or corrupt artifact;
- two concurrent install attempts;
- broker unavailable during an ordinary compile and during promotion;
- a direct uncoordinated compiler observed outside the wrapper;
- Clawdline rebuilding and restarting while another repository's compile remains live.

Every refusal is typed. No test should prove only that a timeout exists; it must prove unrelated
health, Session and event-stream work remains responsive while a heavy dependency is blocked.

## Phased delivery

### Phase 0 — measure and freeze the question

- Record single-build and concurrent-build wall time, peak RSS, CPU, I/O, thermal state and
  foreground Chat latency on the same exact trees.
- Identify every repository entry point that compiles, tests, signs, installs or restarts.
- Add no scheduler yet. The output is the capacity baseline and the minimum resource vocabulary.

Exit: the project can say whether two builds improve throughput, only increase contention, or vary
by job class on the supported Mac.

### Phase 1 — correctness locks at the scripts

- Add a machine-local exclusive fallback around install/restart/signing.
- Add a conservative compile lock to `build.sh` and `test.sh`.
- Use unique output and private `TMPDIR` everywhere.
- Print the holder and typed reason instead of hanging silently.

Exit: two direct shells cannot race promotion or accidentally overlap full compiles, even when no
orchestrated task exists.

### Phase 2 — operation leases and queue visibility

- Introduce the local lease authority and wrapper.
- Acquire only around compile, test execution and promotion phases.
- Add FIFO, cancellation, durable receipts and crash/restart reconciliation.
- Project queue/holder state into Clawdline.

Exit: task `serialize` is no longer the only coordination path, and direct project scripts use the
same machine queue.

### Phase 3 — quantitative policy

- Add CPU/memory/I/O units, pressure-aware admission, derived priority and aging.
- Prove interactive responsiveness and starvation bounds with failure injection.
- Retain a one-heavy-job policy on machines where the benchmark does not justify wider admission.

Exit: concurrency is a measured policy rather than a constant or a Session count.

### Phase 4 — compile cache and request coalescing

- Split compile from execution scope.
- Add content-addressed immutable artifacts.
- Coalesce identical build keys and preserve producer/follower receipts.
- Reuse exact receipts only for the same tree, question and environment tuple.

Exit: parallel callers asking the same question pay for one compile without weakening exact-tree
acceptance.

## Acceptance metrics

Measurements need a fixed machine, exact tree, toolchain, command and output destination. At
minimum report:

- peak simultaneous heavy compilers;
- queue wait and execution p50/p95 by class;
- total wall time for a fixed batch of builds;
- peak memory and time under pressure;
- foreground Chat open and event-stream heartbeat p50/p95 during builds;
- duplicate exact build requests and coalescing rate;
- stale/unknown lease count and recovery time;
- exclusive-operation overlap count, which must remain zero;
- cache hits tied to a complete build identity;
- starvation bound for the lowest admitted priority.

The success condition is not merely lower compile time. The system succeeds when a fixed batch
finishes no slower in aggregate, interactive work remains responsive, unsafe overlaps stay at zero,
and every wait has an observable reason.

## Decisions deliberately left open

1. Whether the durable authority is a LaunchAgent or a filesystem-first helper.
2. Whether a running low-priority process may ever be suspended, or only future work held.
3. Which macOS pressure APIs are stable enough to affect admission rather than telemetry alone.
4. How much resource declaration belongs in task JSON versus the execution wrapper.
5. Whether compile cache storage belongs to Clawdline or to a general per-language adapter.
6. What controlled benchmark justifies more than one heavy compiler on a machine.

These decisions should be made from Phase 0 evidence. Until then, the narrow safe improvement is a
script-level exclusive lock; the general design remains this proposal rather than an implied current
capability.
