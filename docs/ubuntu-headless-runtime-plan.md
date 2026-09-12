# Clawdline Ubuntu / Google Cloud Headless Runtime Plan

Status: proposed implementation plan  
Plan version: 1  
Date: 2026-09-10  
Board item: CLA-296  
Scope: one shared Clawdline product that can run either as the existing macOS app or as a headless Ubuntu service

## Executive decision

Clawdline should support Ubuntu by extracting a shared application core and adding platform adapters. It should **not** become a copied Linux fork.

The first supported cloud shape should be a single-user daemon on one Google Compute Engine VM, backed by a persistent disk and `systemd`, with `tmux` as the only terminal backend. The existing encrypted Clawdline Cloud relay and hosted web UI remain the remote-access plane. Cloud Run is not the first target because Clawdline owns long-lived terminal processes, local repositories and durable host state; GKE is unnecessary until multi-node scheduling becomes a real requirement.

This work can proceed while other Clawdline features are in development if it is delivered as a staged strangler refactor: preserve the current macOS facades, extract one ownership boundary at a time, and land small exact-tree changes. It must not begin as a wide rewrite of `Orchestrator.swift` and `RemoteServer.swift`.

## What “runs on Ubuntu” means

The MVP is complete when a user can:

1. Provision a supported Ubuntu LTS VM on Google Compute Engine.
2. Install one versioned Clawdline daemon package and its `systemd` unit.
3. Pair that daemon with Clawdline Cloud without placing a machine credential in a URL or source tree.
4. Register one or more project roots on a persistent disk.
5. Create, observe, message, interrupt and close managed Codex or Claude sessions through the existing hosted UI.
6. Run those sessions inside `tmux`, survive daemon restarts, and reconcile durable session/task state afterward.
7. Apply bounded concurrency, backpressure, idempotency, receipts and typed errors across accepted, executed, delivered, observed and acknowledged states.
8. Restore the service and its project/state disk from a documented backup without changing the public protocol.

The MVP explicitly excludes iTerm2, AppKit UI, menu-bar controls, macOS Keychain, Apple Events, speech, local notifications, voice input, macOS/iOS build execution, multi-tenant hosting and automatic horizontal scaling.

## Current feasibility assessment

The current repository cannot compile or run as a normal Linux service without architectural work:

- `Package.swift` declares macOS 13 as its platform and exposes a single executable product.
- `build.sh` links macOS-only frameworks including AppKit, Carbon, ServiceManagement, Speech and AVFoundation.
- The source tree contains direct AppKit, Darwin, Security/CommonCrypto and Network imports.
- Terminal control mixes a portable `tmux` path with iTerm2 and Apple Event behavior.
- The highest-risk state and protocol owners remain large and coupled: `Orchestrator.swift`, `RemoteServer.swift`, `SessionWatch.swift`, `Targets.swift` and `Tmux.swift`.
- The existing architecture notes already identify an `Orchestrator` ↔ `RemoteServer` dependency cycle and define a compatible staged extraction direction.

There is nevertheless a viable seam. `CloudAppBridge.swift` already names transport, headless command/read and command-routing protocols; the `tmux` backend is mostly process- and filesystem-oriented. These are useful extraction points, but their concrete implementations still depend on macOS-bound types and the monolithic server.

The relevant Orca precedent is its headless Linux server mode: a long-running service exposes the product through a browser while using Linux service/process conventions. We should borrow that product shape, not its implementation wholesale. Clawdline’s distinguishing constraints are encrypted relay transport, managed terminal lifecycle, repository ownership, Board/task evidence and safe restart semantics.

## Target architecture

The intended dependency direction is:

```text
Hosted Clawdline Web / Cloud relay
                |
      encrypted protocol envelopes
                |
        ClawdlineApplication
  commands, reads, policy, orchestration
        /                    \
ClawdlineCore             HostPorts
domain/state/protocol     terminal, process, secrets,
evidence/idempotency      storage, networking, notify
        |                    |
        +----------+---------+
                   |
          platform adapters
          /                 \
  macOS AppKit host      Ubuntu daemon host
  iTerm2 + tmux          tmux + procfs + systemd
  Keychain               protected file/secret manager
  Apple APIs             POSIX/Linux APIs
```

### Shared modules

`ClawdlineCore` owns platform-neutral value types and rules:

- Session, task, graph, Board and evidence models.
- State transitions and invariants.
- Idempotency keys, receipts and typed failures.
- Protocol serialization and validation.
- Scheduling/backpressure policy that does not invoke a process itself.

`ClawdlineApplication` owns use cases:

- Create/send/interrupt/close/reconcile session operations.
- Task dispatch and result ingestion.
- Read models for sessions, projects, Board and Cloud publication.
- Restart/recovery coordination through injected ports.

### Host ports

The shared application layer depends on small capabilities rather than platform globals:

- `TerminalHost`: create, attach, send, interrupt, capture, close and enumerate terminals.
- `ProcessInspector`: PID ownership, liveness and resource evidence.
- `DurableStore`: atomic records, snapshots and migrations.
- `SecretStore`: load/rotate/remove machine credentials without exposing bytes to logs.
- `Clock`, `IDGenerator` and `FileSystem`: deterministic testing and bounded paths.
- `LocalHTTPHost` and `CloudTransport`: protocol I/O, backpressure and reconnect behavior.
- `AttentionNotifier`: optional host notification; unsupported capability is explicit.

### Platform adapters

The macOS target retains current behavior behind adapters for AppKit, iTerm2, Keychain, Apple Events, native notifications and launch/restart. Existing public types remain facades until callers have moved.

The Ubuntu target supplies:

- `tmux` terminal control and PTY/process inspection through Linux `/proc` and POSIX APIs.
- A `systemd` service with explicit runtime and state directories.
- File permissions or Google Secret Manager integration for machine credentials.
- Swift Foundation networking or a server library with tested Linux support.
- Structured journal logging with secret redaction.
- A local listener bound to loopback by default; public access continues through Clawdline Cloud.

## Google Cloud deployment baseline

The first deployment profile is deliberately boring:

- One Google Compute Engine VM in a named region and zone.
- A supported Ubuntu LTS image and a pinned Swift toolchain/runtime.
- A separate persistent disk for repositories and Clawdline durable state.
- No inbound public application port; outbound TLS to the Cloud relay and model providers.
- A least-privilege service account only for explicitly enabled Google services.
- OS Login/IAP or another auditable administration path instead of password SSH.
- Google Secret Manager is optional for bootstrap; the daemon still needs a local runtime secret boundary.
- Snapshot/backup policy for the persistent disk, plus a restore drill.
- Cloud Monitoring metrics for queue depth, terminal count, relay state, last durable write and reconciliation age.

Cloud Run remains a future stateless edge option, not the execution host. Its instance lifecycle, ephemeral writable filesystem and bounded WebSocket requests are a poor match for durable local repositories and terminal processes. GKE becomes relevant only if we later require a scheduler spanning multiple isolated executors.

## Security and trust boundary

Moving the executor from a user-owned Mac to GCP changes the product’s trust claim even if relay encryption is unchanged. The VM necessarily sees plaintext repositories, prompts, transcripts and provider credentials while executing work.

Before beta we must therefore define:

- Who controls the GCP project and can inspect disks, snapshots, serial console and instance memory.
- Whether one VM is dedicated to one user/account.
- How provider tokens and the Clawdline machine credential are provisioned, rotated and revoked.
- What is encrypted at rest, which principals can decrypt it, and what backups retain.
- Which logs may contain repository paths or prompt fragments, and the redaction policy.
- How OS/package updates, vulnerability response and incident revocation are handled.
- Whether sensitive projects can require a Mac-only executor through capability-based routing.

The default beta contract should be one user/account per VM, no shared Unix users, no public daemon port, and explicit warnings that GCP administrators are inside the execution trust boundary.

## Delivery plan

### Phase 0 — Contract, baseline and Linux probe

Goal: establish what is portable before moving production ownership.

Deliverables:

- A capability matrix for macOS and Ubuntu, including unsupported operations.
- A compile/import inventory and a dependency map from shared behavior to platform APIs.
- Protocol fixtures proving Cloud command/read envelopes remain byte-compatible.
- A minimal Linux CI job that compiles a deliberately small portability probe.
- A benchmark/behavior baseline for create, send, interrupt, close and restart reconciliation.
- A written acceptance matrix and exact module boundaries for Phase 1.

Gate: approve the capability matrix and trust-boundary statement. No production owner moves before this gate.

Estimated effort: 1–2 engineer-weeks.

### Phase 1 — Finish state ownership and break the server cycle

Goal: give mutable orchestration state one explicit owner while preserving current macOS facades.

Deliverables:

- Move one state family at a time behind registry/application operations.
- Replace direct owner-lock manipulation with closed operations and atomic snapshots.
- Move host side effects outside state-lock critical sections.
- Remove the `Orchestrator` ↔ `RemoteServer` ownership cycle along the direction already defined in `docs/architecture-refactor.md`.
- Add focused concurrency and failure-injection tests for each moved invariant.

Gate: no adapter can mutate orchestration collections or acquire the state-owner lock directly.

Estimated effort: 2–4 engineer-weeks.

### Phase 2 — Introduce host ports and preserve macOS

Goal: separate use cases from AppKit/iTerm2/Keychain/Apple Event implementations without changing user behavior.

Deliverables:

- Define the host-port interfaces listed above.
- Wrap the current macOS behavior in adapters.
- Keep compatibility facades thin and measurable.
- Make unsupported capabilities typed rather than silently absent.
- Run exact-tree macOS acceptance after each ownership slice.

Gate: shared application code can run against in-memory/fake ports with no AppKit import.

Status, 2026-09-11 (W2-3, corrected — see the sealed review and correction wave below):
`TerminalHost`, `ProcessHost` (the `ProcessInspector` role), `FileSystemHost`, `SecretStore`,
`HostClock` and `IdentityHost` (the `IDGenerator` role) exist in `Sources/HostPorts.swift`, which
imports Foundation only; this Mac's leaves are `Sources/MacHostAdapters.swift`, and safe close is
the first lifecycle that runs only on them, proved against fakes in `Tests/HostPortsTests.swift`.
Unsupported capabilities are a typed `capability_unavailable` refusal returned before the first
effect (`Sources/HostPorts.swift`'s `HostCapabilityUnavailable.code` — a real Swift-text hit on
this Mac tree since the original W2-3 delivery, not Linux runtime or Ubuntu MVP support).
`TerminalHost` now also has `create`, `capture`, `reveal` and `interrupt` — the rest of the row
above — each with a real Mac leaf and a fake proof; `send`/`capture`/`reveal` in
`Sources/Targets.swift`, admitted creation in `Sources/StartPoints.swift`, and the allowlisted
answer-byte channel run through them as real production paths. Route admission and menu parsing
remain application policy, while their platform effects are adapter-owned. `SecretStore`'s
`loadOrCreate`/`rotate` close the read-then-write race a caller could otherwise compose out of
`data`/`set`. `ProcessHost.signal` takes a full process identity and is revalidated at the Mac
effect boundary rather than trusted from the caller alone. A checked-in, monotonically expandable
Core/Application candidate manifest (`tools/core-application-candidates.txt`) now backs the
architecture guard instead of one hard-coded file. `DurableStore`, `LocalHTTPHost`,
`CloudTransport` and `AttentionNotifier` are not ports yet; they are separate W3/W4 composition
families, not hidden terminal effects. The full finding set and disposition are in
`/tmp/.clawdline/5f1b0a94-cb1c-44b7-99f3-4684a2155d98/artifacts/W2_3_REVIEW.md`; the boundary and
what remains are recorded in [`architecture-refactor.md`](architecture-refactor.md).

Estimated effort: 2–4 engineer-weeks; overlaps with the latter part of Phase 1 by disjoint ownership slices.

### Phase 3 — Split Swift targets and make the core compile on Linux

Goal: produce an actual Linux-buildable shared core and daemon entry point.

Deliverables:

- Split SwiftPM products/targets into core, application, macOS host and Linux daemon.
- Replace unconditional Darwin imports with portable conditionals or adapter code.
- Replace CommonCrypto/Security dependencies in shared code with a cross-platform audited crypto package or move them behind ports.
- Select and validate the Linux HTTP/WebSocket implementation.
- Add Ubuntu CI with pinned toolchain and dependency lock.

Gate: Ubuntu CI compiles shared modules and passes protocol/state tests; macOS app still passes its acceptance suite.

Status, 2026-09-11/12 (W3-1): `Package.swift` declares two real library targets —
`ClawdlineCore` (`Sources/Assistant.swift`, `Sources/CloudCanonicalJSON.swift`,
`Sources/CloudClock.swift`) and `ClawdlineApplication` (`Sources/HostPorts.swift`, depending on
`ClawdlineCore`) — plus the pre-existing `Clawdline` executable target, now depending on
`ClawdlineApplication`, as the Mac composition point. Every member is a symlink into `Sources/`,
never a copy (`Packages/README.md` says why a plain file can't coexist with `./test.sh`'s compatibility flat,
single-module compile); `tools/check-architecture-boundaries.sh`'s "real SwiftPM
Core/Application/Mac target graph" block checks the symlinks, the candidate-manifest membership
and the SwiftPM dependency edges themselves (via `swift package describe --type json`) rather than
trusting Package.swift's own comment, and a red-before-green proof on both the target-edge check
and the symlink/one-source-of-truth check is in `artifacts/W3_1_DELIVERY.md`. `swift build` (all
targets, matching CI's existing smoke-test job) and a flat `swiftc -typecheck` over the unchanged
`./build.sh`/`./test.sh` production source list both pass; `./build.sh` and `./test.sh` themselves
were not run, per this task's own instructions. `tools/swift-core-application-linux-build.sh`
compiles the real targets — not a materialized copy — on the pinned Ubuntu 24.04 amd64 image
`tools/ubuntu-core-probe.sh` already uses, wired into CI as `swift-core-application-linux`; a
local run under Docker/QEMU on Apple Silicon passed 2 of 5 tries (the other 3, and one rerun of
the pre-existing, already-landed W0-F probe under the identical image, died `qemu: uncaught target
signal 4` before any of this repository's code ran — an emulation gap on that one host shape, not
a defect either script found; see the script's own comment). `HostPorts.swift` needed
`Assistant`/`Assistant.Running`'s type, and `Assistant.isInstalled`/`.available` were the one
platform effect (`FileManager`, and transitively `Codex.home`) keeping `Assistant.swift` out of a
real target — moved, unchanged, to `Sources/AssistantInstallation.swift` (a mechanical Swift
extension split, not a behavior change) — and `Assistant`/`Assistant.Running.pid`/`.processStart`/
`Assistant.quitLine` became `public` for the same reason `HostPorts.swift` itself already had to be
Foundation-only: a symbol crossing a real module boundary needs real visibility, not only a lexical
promise. `Sources/HostPorts.swift` gained one `#if canImport(ClawdlineCore)`-guarded import — the
one place the two coexisting build systems (SwiftPM's module boundaries and `./test.sh`'s flat
compile) could not otherwise agree — checked by name in the same guard block, not left to a bare
lexical import count. W3-1 deliberately stopped before the Linux composition and before changing
the Mac bundle builder; W3-2 below owns those two connected graph edges.

**Correction, W3-1 (`artifacts/W3_1_CORRECTION.md`).** The sealed review of the status above found
four defects, all now fixed on the same delivery: (1) `spec-mac-does-not-consume-application` —
the `Clawdline -> ClawdlineApplication` edge existed but nothing in `Clawdline` ever imported it;
`Clawdline`'s own SwiftPM source set still compiled `HostPorts.swift`/`Assistant.swift`/
`CloudCanonicalJSON.swift`/`CloudClock.swift` a second time, so `swift build` held two unrelated
copies of `TargetSession`, `Assistant` and the rest. `Package.swift`'s `exclude:` on the
`Clawdline` target closes that; its ~60 real consumers now reach the vocabulary through a guarded
`import ClawdlineApplication`. (2) `repo-graph-guard-is-not-exact` — the guard's target-membership
check was a `find -maxdepth 1` count against a floor, blind to a source added in a nested
subdirectory, and its Mac-edge check only asked whether `ClawdlineApplication` was present, not
whether it was the *only* dependency; both are now exact comparisons against `swift package
describe --type json`'s own resolved `sources`/`target_dependencies`. (3)
`repo-symlink-git-object-claim-is-false` — `Package.swift` and `Packages/README.md` said a symlink
member and the file it names are "the same blob"; a Git symlink is its own object (mode `120000`,
content = the link text) distinct from the target file's blob, and only the *checked-out working
tree* resolves both paths to the same bytes. Both documents now say that correctly.
(4) `runtime-qemu-receipt-counts-conflict` — this status paragraph's "2 of 5", the delivery
artifact's table and `tools/swift-core-application-linux-build.sh`'s own comment each quoted a
different attempt-count ratio for the same handful of local QEMU tries; none of it is restated
here. What is claimed instead is one fresh, dated, single attempt in the correction's ledger
(`-c debug -j 1`, the script's own default): **PASS**, `Build of target: 'ClawdlineApplication'
complete!`, no crash — plus the CI job's exact, already-pinned configuration
(`SWIFT_CORE_APPLICATION_LINUX_BUILD_CONFIGURATION=release SWIFT_CORE_APPLICATION_LINUX_BUILD_JOBS=2`,
real amd64 hardware, not QEMU). A prior local run's QEMU crash before this repository's code ran is
real and worth keeping as a named failure mode; it is not a rate.

**W3-2 composition and build wrapper.** `Package.swift` now exports exact executable products for
both `Clawdline` and `ClawdlineLinux`. Linux depends only on `ClawdlineApplication`, which depends
only on Core; the architecture guard compares SwiftPM's resolved sources, target dependencies and
product mappings, scans the Linux sources for forbidden Apple imports, and requires the source to
consume Application's typed `capability_unavailable` vocabulary. The pinned Ubuntu job builds the
real `ClawdlineLinux` product, so its two inward dependencies compile in the same invocation.

The executable is an honest W3 skeleton, not a prematurely claimed daemon. `health` returns a
diagnostic service/build/config-schema identity with `ready=false` and
`w4_runtime_not_composed`. `check-config` accepts only an exact versioned JSON shape, loopback
listen address, absolute state/secret paths and descriptor-bound protected inputs: `O_NOFOLLOW`,
`O_NONBLOCK`, `O_CLOEXEC`, `fstat`, owner/mode/type/size validation and bounded reads. The secret
must be a non-empty regular 0600 file; the receipt returns only its byte count, never its contents.
`run` performs the same checks and then fails with the typed
W4 refusal before opening a listener, creating state, touching a terminal or spawning a process.
Its health inventory is generated from `HostCapability.allCases`; all six current host capabilities
are unavailable and owned by W4-1's adapter work. It does not invent HTTP, Cloud or attention wire
capability identifiers before their compatibility contracts exist.

On macOS, `build.sh` keeps the machine compile lock and its existing resources, identity stamp,
signing, staged replacement, restart maintenance and rollback flow, but the guarded compile is now
`swift build --product Clawdline`. The wrapper copies the resolved release product into the staged
bundle and compares SHA-256 before and after; there is no second flat `swiftc` graph to drift from
Package.swift. `./test.sh --linux-package-focused` is the narrow locked proof: it compiles the
Linux product and drives health, protected config/secret refusals and the final W4 startup refusal.
It does not bundle, install, restart or claim the Phase 4 runtime gate.

**Bound W2-3 policy handoff to W4-1.** W4-1 owns extracting `StartPoints.start` route admission
(fixtures, typed refusal codes, and the phone attach affordance) plus the `Targets.answer` menu
parser/allowlist from their Mac facades into `ClawdlineApplication`. Acceptance requires the Mac
and Linux compositions to consume one policy implementation and one fixture set before either
invokes `TerminalHost`; Linux must return `capability_unavailable` for an unavailable adapter and
must not copy the route/menu policy into its composition. This is a named W4-1 dependency, not
residual W2 work or an implied follow-up.

**W4-1 runtime adapters and containment (implementation status, 2026-09-12).** The bound handoff
above is now implemented in `ClawdlineApplication`: `SessionLaunchPolicy` produces one structured
provider plan consumed by Mac and Linux before `TerminalHost.create`, while
`TerminalMenuAnswerPolicy` owns the complete digit/Tab/back-Tab byte allowlist before any key
effect. Linux and the existing Mac facade now consume the same Application-owned
`TerminalCommandScheduler`: it serially executes create, send, observe, interrupt, resize, close and
enumerate, with inventory/pane preflights inside admission and the established nested/maintenance
semantics intact. Its tmux leaf uses one explicit socket, concurrently drains stdout/stderr under one
aggregate ceiling, writes stdin nonblockingly under the same deadline and bounds termination/reap.
Its procfs leaf binds observation and signals to pid, effective uid/gid, complete supplementary
groups, process group and the exact `/proc/<pid>/stat` start token.

Provider processes run non-root under the configured uid/gid, with process umask `0077`, an
isolated durable HOME, and an environment constructed from the exact HOME/PATH/LANG/LC_ALL/TERM/
TMPDIR allowlist. That is no longer treated as containment: the provider launcher requires Landlock
ABI 3+, installs a filesystem allowlist for the admitted projects/HOME/tmp, then installs a seccomp
filter denying AF_UNIX socket creation and same-uid process-inspection syscalls before `execve`.
Daemon secrets/runtime/socket paths have no provider rule. Project roots are outside the control
state, protected-config allowlisted, canonical, owner-bound, symlink/traversal refusing, and rejected
for overlap with any reserved control path in either ancestor direction. File effects walk
descriptors from `/` with `O_DIRECTORY | O_NOFOLLOW`, and atomic writes use a same-directory 0600
temporary file, `fsync` and `renameat`. All operations on one secret account share a process-wide
coordinator across store instances. Secrets never enter provider argv/environment or receipts.

The four lifecycle receipt words are deliberately different: admission begins at `accepted`, a
successful adapter call adds `executed`, PTY delivery adds `delivered`, and only a fresh capture,
dimension readback, inventory or proved absence adds `observed`. Total/per-channel work, input,
inventory, output and timeout bounds produce typed failures. A post-create failure either removes
the exact identity it created or returns an `unknown` reconciliation obligation; paste-before-submit
returns a `partial/text_pasted` receipt. Health is `w4_runtime_not_configured` until protected
configuration proves executable descriptors and kernel containment, then
`w4_provider_authentication_not_proven`: provider lifecycle rows remain unusable and empty until the
named W4-2 real-provider authentication gate supplies a controlled receipt. W4-2 also owns service
supervision, restart/reconciliation and package upgrade/rollback. W4-3 extends that local daemon
with task/result/receipt authority plus Board, Session and document reads; W5 owns Cloud lifecycle.
W4-1 does not open a listener, install, restart or publish anything.

The pinned Ubuntu job now builds the Linux-only manifest graph, runs the SwiftPM contracts as a
non-root service identity with a real tmux binary, and retains the protected-startup shell cases.
The macOS focused question compiles the same Application/Linux/test targets and runs the
platform-neutral policies and failure injections; only Ubuntu executes the `#if os(Linux)` real PTY,
Landlock, secret/socket/outside-write, compensation and partial-send lifecycle. This
focused evidence is not the release-train exact full.

Current Linux-only residuals are explicit: the final procfs-to-`kill(-pgid, …)` gap remains without
pidfd group signalling; Landlock/seccomp support is pinned to Ubuntu amd64; AF_UNIX denial may refuse
provider extensions that require local sockets; apt package versions are recorded but not pinned;
and real Claude/Codex authentication still needs an external controlled credential receipt rather
than being inferred from a shell fixture.

**W4-2 service, durable restart and package candidate (implementation status, 2026-09-12).**
`ClawdlineLinux daemon` composes the W4-1 ports as the dedicated non-root service, completes one
startup reconciliation before opening its authenticated bounded loopback listener, and routes all
create/send/observe/close effects through one serialized durable owner. `clawdline-tmux.service`
alone owns `/run/clawdline`, runs tmux in the foreground as systemd's real MainPID, and is not
`PartOf` the daemon; package installation enables both units idempotently. Exact PID-1 restart,
crash-supervision and reboot behavior still needs the disposable Ubuntu external gate.
Configuration schema 2 names `/var/lib/clawdline` and `/run/clawdline` separately while retaining a
read path for schema 1.

The durable writer uses a same-directory 0600 create, complete write, file `fsync`, atomic rename and
directory `fsync`. Schema 1 records migrate additively without lowering their reader floor. Corrupt,
unreadable, future or semantically contradictory records first create a durable recovery obligation;
quarantine, a later tick and process restart therefore cannot turn their missing canonical pathname
into authoritative empty state. Explicit operator recovery is a separate named action.
A complete tmux inventory can mark a known pane present or absent; an incomplete inventory marks it
unknown. Terminal task results and acknowledged command evidence remain terminal, queued commands
remain queued only with a recoverable sealed payload, accepted-only work becomes interrupted, and
an executed/delivered effect without terminal evidence becomes unknown rather than successful.

`tools/linux-package.sh` builds an amd64 versioned archive with an exact internal file manifest,
source/build/config/protocol/durable-schema identity, archive SHA-256, pinned-public-key identity and
detached RSA/SHA-256 signature. Build metadata is derived from the binary's machine-readable
contract. Install snapshots all four caller paths through no-follow descriptors into root-owned
0700 staging, then uses only those bytes for verify/extract. Exact member type/mode/owner/topology,
existing release payloads and target binary identity are revalidated. Root writes beneath the
service-owned state directory use descriptor-bound no-follow operations. Release files/directories,
`current`/`previous`, journal and receipt are fsynced; stale owner/phase recovery selects a complete
old or new pair. Failed health restores the old image only after a fresh descriptor-pinned authority
check proves that image can still read the latest bytes. Incompatible or disappeared authority
keeps the new selector, records typed `rollback_state_incompatible` operator recovery and refuses an
unsafe old-image restart. Image rollback never rewinds state. Private-root/fake-systemctl tests do
not mutate host systemd.

The health record distinguishes `serviceReady` (authoritative startup reconciliation plus exact
release/schema identity) from provider `ready`. Without a real Claude/Codex credential exercise,
provider rows remain unauthenticated/unusable and readiness stays
`w4_provider_authentication_not_proven`. Local Docker supplies native Ubuntu/arm64 source/package
checks; the pinned Ubuntu/amd64 attempt remains QEMU-inconclusive before product execution. Neither
surface supplies systemd-as-PID-1 production containment or real-provider authentication; those are
typed external VM/auth boundaries, not fixture-promoted evidence.

**W4-3 task, Board, Session and document lifecycle (implementation status, 2026-09-12).** The
existing `LinuxDaemonIngressOwner` remains the only command/effect lane. Its Application-owned
vocabulary now closes task create/read/message/result/ack/close and Board/Session/document reads;
machine ingress authentication still happens before the owner; task message/result additionally
bind and constant-time-check the task-secret digest before command existence or outcome, and missing
task/wrong secret/wrong exact identity share one refusal. Schema-2 terminal commands retain their
original sealed bytes across schema-3 migration. Task identity,
project root, Session, claims, accepted message receipts, result digest/count, publication and
acknowledgement live in `/var/lib/clawdline/tasks/authority.json`. Result bytes live once in the
same task root, are fsynced before authority publication, and are verified on startup plus cached
result replay, acknowledgement and close commit points. Completed result queue payloads are
compacted only after their digest-bound task file is durable, and their completed replay
classification remains stable across restart.

Board is a read-only projection (`canWrite=false`, `canManage=false`) of that authority rather than
a Linux Board fork. Reads require the exact recorded project/Session/task tuple and never enter the
durable mutation ledger. Document scope selects only computed `project/artifacts` or
`tasks/<task>/artifacts`; bounded relative Markdown/text names are opened by descriptor walk with
`O_NOFOLLOW`, then checked as service-owned regular single-link files with a 2 MiB limit. Listing
holds the root descriptor through a deterministic descriptor-relative walk; it sorts a bounded
candidate set and marks 200 entries truncated only when a 201st valid file or the walk bound omits
evidence. The old `records/runtime-state.json` stays readable until one atomic replacement installs
a non-authoritative package rollback fence, removing the former rename-to-fence empty window.
Installer and rollback prefer canonical task authority and consult the old pathname only for a
private, single-link, bounded, service-UID-owned pre-migration record without `recordKind`.

The accumulated runtime contracts exercise create → message → observe → result/receipt →
Board/Session/document reads → restart → exact result replay → continue → ack → close plus
schema-2 succeeded/interrupted replay, existing/missing command authentication indistinguishability,
acknowledged/incomplete-inventory precedence, result tamper/missing/link commit refusal, held-root
replacement, exact 200/201/depth/walk listing, migration cutover and failed-health rollback
injections. They are source/disposable-fixture evidence only. Cloud ledger/spool and pairing remain
W5-1/W5-2; real provider credentials, GCE, and systemd-as-PID-1 restart remain explicit external
gates.

Estimated effort: 2–3 engineer-weeks.

### Phase 4 — Ubuntu terminal and service runtime

Goal: support the complete local managed-session lifecycle on Ubuntu.

Deliverables:

- Linux `tmux` adapter and process ownership checks. **Implemented by W4-1.**
- Runtime/state directory layout and crash-safe contained writes. **Implemented by W4-1; additive
  durable record migration and quarantine are implemented by the W4-2 candidate.**
- `systemd` unit, install/upgrade/rollback scripts and exact health endpoint. **Source and
  private-root behavior are implemented by the W4-2 candidate; a PID-1 VM drill remains external
  acceptance.**
- Startup reconciliation and the local serialized terminal/task/queue/command ingress ledger.
  **Implemented by the W4-2 candidate with consecutive-tick, restart and effect-boundary injection;
  real PID-1 socket/pane continuity remains the gate below.**
- Durable task result/ack authority plus exact Board, Session and descriptor-held document reads.
  **Implemented by the W4-3 candidate on the same ingress owner; Cloud publication and real PID-1
  continuity remain later external gates.**
- Typed terminal capacity/backpressure behavior and failure-injection tests. **Implemented by
  W4-1; transport/Cloud backpressure remains with its owning later slice.**

Gate: create → interact → restart daemon → reconcile → continue → close succeeds on a disposable Ubuntu VM.

Estimated effort: 2–4 engineer-weeks.

### Phase 5 — Cloud relay integration and GCE alpha

Goal: operate the Ubuntu host remotely through the current Cloud product.

Deliverables:

- Headless device login/pairing flow.
- Durable credential provisioning and rotation.
- Cloud publication/reconnect/resume behavior equivalent to the Mac where capabilities match.
- Terraform or reproducible `gcloud` provisioning for the baseline VM, disk, firewall and service account.
- End-to-end test from hosted UI to an Ubuntu-managed terminal.

Gate: a clean GCE VM can be provisioned, paired, exercised and destroyed/recreated while its persistent data restore is verified.

Estimated effort: 2–3 engineer-weeks.

### Phase 6 — Beta hardening

Goal: make unattended operation safe enough for invited beta users.

Deliverables:

- Upgrade/rollback compatibility matrix and schema migration tests.
- Backup/restore runbook and restore evidence.
- Resource quotas, disk-full behavior, log rotation and secret redaction.
- Fault injection for relay loss, daemon kill, VM reboot, stale `tmux`, provider failure and partial disk writes.
- Operator alerts and support diagnostics that distinguish accepted, executed, delivered, observed and acknowledged states.
- Security review and documented residual risks.

Gate: seven-day soak with forced failures, no lost acknowledged command, and successful restore/rollback drills.

Estimated effort: 4–8 engineer-weeks.

## Working alongside current development

This project should use one dedicated integration root and isolated worktrees for implementation tasks. Work is divided into 1–3 day ownership slices with exact path claims. A slice lands only after its old facade delegates to the new owner and the exact candidate tree is verified.

Most product work can continue normally. The following hotspots need short serialized landing windows because they are likely to overlap active work:

- `Package.swift`, `build.sh` and `test.sh`.
- `Sources/Orchestrator.swift` and `Sources/RemoteServer.swift`.
- `Sources/SessionWatch.swift`, `Sources/Targets.swift` and `Sources/Config.swift`.
- Cloud bridge routing and credential persistence.

Phase 0 intentionally writes new documentation, test fixtures and a new CI probe first. It need not modify the Board implementation files that other Sessions are currently editing.

The expected elapsed program is:

- architecture contract: 1–2 weeks;
- internal Linux alpha: roughly 6–10 engineer-weeks;
- GCE/Cloud beta: roughly 10–16 cumulative engineer-weeks;
- hardened beta/production candidate: roughly 16–24 cumulative engineer-weeks.

With other development continuing, a realistic calendar range is 3–5 months. This estimate assumes one primary implementer, periodic review, and small contributions from owners of Cloud, Board and orchestration seams.

## Compatibility and migration rules

- No repository or Board schema fork for Linux.
- No copy of the orchestration core under a Linux-specific directory.
- New interfaces enter behind existing macOS facades before callers move.
- Every state-owner extraction has a red-before-green invariant test.
- Cloud envelopes remain closed and versioned; new host capabilities are additive and explicit.
- Existing macOS sessions, task records and Board evidence must remain readable across target splits.
- A Linux-only shortcut cannot weaken task authorization, path containment, restart safety or delivery receipts.
- Platform capability routing is explicit: a request needing Xcode, iTerm2, Apple Events or another Mac-only tool is rejected or routed to a Mac executor.

## Acceptance matrix

| Capability | macOS target | Ubuntu MVP | Acceptance evidence |
|---|---|---|---|
| Hosted Cloud session UI | Required | Required | Same protocol fixtures and end-to-end reads/writes |
| Codex/Claude managed session | Required | Required | Lifecycle scenario with restart reconciliation |
| `tmux` backend | Required | Required | Shared contract tests plus platform integration tests |
| iTerm2 backend | Required | Not supported | Typed `capability_unavailable` on Ubuntu |
| AppKit/menu bar/settings | Required | Not applicable | macOS acceptance only |
| Voice/speech | Optional/current | Not supported | Capability matrix |
| Project/Board/document reads | Required | Required | Cross-platform fixtures and hosted reader test |
| Task dispatch/results | Required | Required | Idempotency, receipt and failure-injection suite |
| Local secret storage | Keychain | Protected Linux adapter | Rotation/revocation and no-secret-log tests |
| macOS/iOS builds | Required where available | Not supported | Route/reject based on declared host capability |
| Upgrade/rollback | Existing app path | `systemd` package path | Exact-version rollback drill |

## Initial Board structure

The program should be represented as one Epic with these child work items:

1. Phase 0: capability contract and Linux compile probe.
2. Phase 1: orchestration state ownership and server-cycle removal.
3. Phase 2: host-port interfaces and macOS adapters.
4. Phase 3: Swift target split and Linux core CI.
5. Phase 4: Ubuntu `tmux` daemon and `systemd` packaging.
6. Phase 5: Clawdline Cloud integration and GCE alpha.
7. Phase 6: beta security, failure injection, backup and operations.
8. Independent architecture/security review.

Dependencies are sequential at the phase level, but disjoint slices inside Phases 1–3 may overlap after their ownership boundaries are recorded. Phase 6 may begin operational threat modeling earlier, but its acceptance gate depends on the GCE alpha.

## Board gaps exposed by this program

The current Board can hold an Epic, children, `blocks` links, checklist, milestones, obligations and versioned plan references. It does not yet make a long-running architecture program easy or unambiguous:

1. **The durable managed-work binding is difficult to see from the selected Board item.** The exact workflow receipts show that `run-f3816d670b1eeb34eaa0c5898cc1788f` correctly created new Epic CLA-297, while the later `run-08dc7654aa91f440c0b29f5b2e7b47f9` explicitly continued CLA-296. The initial contrary diagnosis was therefore disproved. The useful feature is a visible binding receipt—run, requested classification, effective item, created/reused reason, revision and replay state—not a silent change to version-1 `new_work` semantics.
2. **Repeated begin boundaries duplicate Session links.** Work spans are legitimately separate, but the same canonical Session relation should be a source-aware upsert or render once with its activity intervals. A begin is not automatically a distinct execution attempt.
3. **The semantic workflow helper cannot add a typed planning-document reference.** It supports begin/progress/supplement/deliver, while the existing durable `document_reference` operation is only available through a lower-level Board command. The helper should route a version-negotiated typed operation through the same bounded outbox and Store contract, without creating another document model or promoting narrative to evidence.
4. **There is no atomic plan-structure import.** Creating seven phase children, gates, owners, acceptance criteria and dependency links requires many revision-sensitive calls and can leave a half-created roadmap. A bounded operation must validate the complete draft before one persist/revision and must never delete omitted existing children implicitly.
5. **Dependency links do not produce a clear advisory planning frontier.** A large Epic needs `planning_ready`, `blocked` and `unknown` derived from plan dependencies and versioned gate authority. This must remain distinct from the broker’s execution frontier and must not claim a calculated critical path without a reliable duration model.
6. **Planned write claims and collision zones are not visible at the program level.** They would be useful advisory scheduling data, but cannot reserve files or become execution authority; the broker must still validate real claims at dispatch.
7. **Architecture approval gates are not first-class.** A checklist or obligation can approximate one, but cannot bind a decision, authority and released children to an exact plan revision. Approval of an older plan must not release a newer one.
8. **Cross-host capabilities are narrative only.** A plan needs `supported`, `unsupported` or `unknown` capability requirements with an observation time. Capability-aware dispatch is a later step and must not create a second scheduling truth beside the broker.

The minimum useful Board enhancement is a version-negotiated workflow document operation and a bounded, atomic, revision-checked Program Plan operation. It should upsert the Epic/children by stable logical keys, record advisory dependency/gate/capability/claim metadata, return exact binding receipts, and derive a conflict-aware planning frontier without treating narrative text as evidence or replacing broker authorization.

## Sources and repository evidence

- `Package.swift`, `build.sh`, `Sources/Orchestrator.swift`, `Sources/RemoteServer.swift`, `Sources/Targets.swift`, `Sources/Tmux.swift` and `Sources/CloudAppBridge.swift`, inspected 2026-09-10.
- `docs/architecture-refactor.md`, `docs/cloud.md`, `docs/orchestrator.md`, `docs/project-board.md`, `docs/project-board-contract.md` and `docs/board-workflow.md`, inspected 2026-09-10.
- Orca headless Linux server guide: <https://github.com/stablyai/orca/blob/main/docs/reference/headless-linux-server.md>
- Google Compute Engine instance overview: <https://docs.cloud.google.com/compute/docs/instances/instance-creation-overview>
- Google Compute Engine persistent disks: <https://docs.cloud.google.com/compute/docs/disks/persistent-disks>
- Cloud Run container runtime contract: <https://docs.cloud.google.com/run/docs/container-contract>
- Cloud Run WebSockets guidance: <https://docs.cloud.google.com/run/docs/triggering/websockets>

## Immediate next action

Phase 0 begins with a read-only portability inventory and an isolated Linux compile probe. No production behavior moves until the capability matrix, trust-boundary statement and target dependency direction are reviewed against this plan.
