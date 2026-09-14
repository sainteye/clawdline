# `ClawdlineCore` and `ClawdlineApplication`

W3-1 (Plan v4; see `artifacts/2026-09-10-clawdline-platform-refactor-program-v4.md` and
`docs/adr/0001-platform-boundary-and-evidence.md`). These are the first two real product-graph
targets, not editor metadata and not a probe like `tools/ubuntu-core-probe/`.

## Why symlinks, not files

Every `.swift` file under `Packages/ClawdlineCore/` and `Packages/ClawdlineApplication/` is a
symlink into `Sources/`, never a copy. That is a constraint, not a style choice:

- SwiftPM refuses two targets that claim the same source file (`target 'X' has overlapping
  sources`), so `Sources/HostPorts.swift` cannot be a member of both the existing `Clawdline`
  executable target's implicit recursive scan of `Sources/` **and** an explicit-`sources:`
  `ClawdlineApplication` target in the same package.
- The only way to give `ClawdlineApplication` that file without removing it from `Clawdline`'s
  physical layout is a different *path* that resolves, in the checked-out working tree, to the
  same bytes — a symlink — or a copy. A copy is exactly the fork the task this shipped under
  forbids: two files that must be kept in sync by hand and will eventually drift.
- Every caller that names the vocabulary these files declare (`Sources/Targets.swift`,
  `Sources/ITerm.swift`, `Sources/Orchestrator.swift` and about fifty neighbours) needs
  `import ClawdlineApplication` under `swift build`'s module-separated compile, but that import
  line also sits in the same file `./test.sh` compiles directly with `swiftc` as one flat module
  with no other modules to import — that compile has no `ClawdlineApplication.swiftmodule`
  to resolve the import against. `#if canImport(ClawdlineApplication)` around the import is what
  lets one file serve both builds; see the note atop `Sources/HostPorts.swift`.

A symlink has neither problem: `git` tracks the link itself (a small blob holding the *link
text* — the relative path it points at — not a second copy of the target's content), so there is
exactly one place the target file's bytes live; the two SwiftPM targets see the symlink as a path
distinct from `Sources/HostPorts.swift`, so the overlap check does not fire.

### Correction: the symlink and its target are not the same Git object

An earlier version of this document, and of `Package.swift`'s own comment, said a symlink and the
file it names are "the same blob" and that `git show` on either produces identical bytes. That
conflates two different things `git` tracks. `git ls-tree HEAD -- Packages/ClawdlineCore/Assistant.swift`
reports it as **mode `120000`** (a symlink), and its blob's content is the sixteen-odd bytes of
link text, `../../Sources/Assistant.swift` — not the several hundred lines of Swift in
`Sources/Assistant.swift`, which is a wholly different blob under mode `100644`. `git show
HEAD:Packages/ClawdlineCore/Assistant.swift` prints that link text, not the source. What *is*
true, and is the actual guarantee this layout gives: the **checked-out working tree** resolves
the symlink through the filesystem, so any tool that reads the path (the Swift compiler included)
sees the same bytes as `Sources/Assistant.swift` — one production source, reached by two paths,
with no second copy for either `git` or a person to let drift. That is a working-tree-resolution
property, not a Git-object-identity one, and `tools/check-architecture-boundaries.sh` checks the
former (that the link still resolves to the right, real, non-dangling file) rather than the
latter, which was never true and is not what this layout needs.

## What is really proven here, and what is not

`tools/swift-core-application-linux-build.sh` on the pinned Ubuntu 24.04 image compiles the Linux
executable and its inward `ClawdlineApplication -> ClawdlineCore` dependencies as the real product
graph, executes the Linux XCTest runtime contracts (including a real tmux PTY lifecycle on
Ubuntu), then executes nine health/config/secret/composition contract cases. The Mac
`Clawdline` product also depends inward through Application, and `build.sh` now bundles that exact
SwiftPM product rather than compiling a second flat source graph. Resolved sources, dependency
sets and executable-product mappings are checked mechanically in
`tools/check-architecture-boundaries.sh`, not only asserted here.

This package graph alone does not prove a working Ubuntu daemon. W4-1 composes real tmux, procfs,
contained-file and protected-secret leaves behind the Application ports; `run` validates protected
inputs and returns a non-root runtime-composition receipt. Its diagnostic health identity remains
`ready=false`, with `w4_runtime_not_configured` before protected configuration and
`w4_provider_authentication_not_proven` after executable/kernel validation: real-provider auth,
systemd-as-PID-1 and Cloud lifecycle remain external gates even after the W4-2/W4-3 source
candidates. Capability rows distinguish compiled, configured and usable; fixture success is not
provider readiness. The wider candidate manifest remains a lexical ratchet, not proof that every
candidate has moved into one of the real library targets.

W4-3 extends that same daemon owner with a closed Application task/read vocabulary, durable
`/var/lib/clawdline/tasks/authority.json`, per-task result bytes and task/project document reads.
Task publication is authenticated by a stored secret digest before command existence is inspected;
schema-2 legacy command seals retain byte-for-byte replay identity across migration. Result/ack
receipts are durable before response, completed result queue classification survives restart, and
replay/ack/close revalidate the descriptor-pinned result bytes before committing. Board is a
read-only projection of that task authority, not a second Board store. Document selection is
limited to a computed project or task artifact root; both content and deterministic bounded listing
hold descriptor-relative no-follow walks and accept only owned regular single-link files. Package
upgrade/rollback prefers canonical task authority, permits only genuine pre-migration legacy state,
checks the configured service UID and refuses fence/recordKind/link/mode/size mismatches. Failed
health does not restore an old selector until the latest authority is revalidated. This remains a
source/disposable-fixture candidate: Cloud relay/pairing, real provider credentials, GCE and a real
PID-1 restart are not proven here.

W5-4 adds one daemon-lifetime Relay supervisor around exactly one `LinuxRelayRuntimeOwner`. The
supervisor strongly retains the shared Application ledger/spool owner, projects typed
starting/running/unauthorized/failed/stopped readiness, reports start failure, and waits for the
owner's stop path when the local service loop exits. Explicit 401/403/revocation responses stop
transport retry until a new owner is deliberately composed; transient transport failures retain
the bounded reconnect policy. The signed daemon template carries `cloudCommandsEnabled`, defaults
it to `false`, and package installation refuses to silently change an existing explicit value.
Enabling it is therefore an authenticated release input (`linux-package.sh install
--cloud-commands-enabled true`), not an inferred consequence of installing W5-4 source.

The Ubuntu release build uses `--static-swift-stdlib`, and both the build gate and package builder
reject an ELF that still names a Swift/Foundation shared object or any unresolved dependency.
The archive therefore remains runnable on a fresh Ubuntu 24.04 host without installing a separate
Swift toolchain; ordinary distribution libraries such as libcurl remain host dependencies.

A fresh host is enrolled before the daemon starts by running `ClawdlineLinux cloud-login
--config /etc/clawdline/daemon.json` as the configured non-root service user. The command opens
only the protected Linux state and secret stores, prints a JSON-line invitation containing the
one-time user code and verification URLs, waits for authorization, and atomically stores the
machine credential plus executor key material before printing its completion receipt. It never
prints the opaque device code, credential, private key, or master secret. A matching enrolled
identity is idempotent; partial, future, corrupt, or mismatched protected state fails closed rather
than creating a second machine identity.

W5-1 adds `CloudCommandLedger.swift`, `CloudOutboundSpool.swift` and
`CloudDurableStores.swift` to the same Application target. Mac production opens the shared
ledger/spool before attaching its bridge; the Ubuntu daemon opens and retains the same stores
before local admission. The host file leaf is single-writer, versioned and descriptor-checked;
candidate snapshots become visible to the actors only after file fsync, rename and directory
fsync complete. Invalid state is preserved or quarantined and startup fails rather than switching
to memory. Mac startup descriptor-checks the former `cloud-sequence.json` and durably raises the
new spool counter to that sender's reserved ceiling before publication; its live schema-compatible
rollback fence raises the predecessor ceiling before every old-image block boundary, and invalid
legacy bytes never become a zero floor. Durable logical rows retain digest/size metadata rather
than plaintext-equivalent payloads. `CloudTransport` owns no outbound queue or sequence: the Application spool persists a
global sequence and exact frame, persists `sent`, then performs the async socket write and settles
only an exact-channel/sequence correlated authenticated receipt. Attempt deadlines wake without
new traffic; receipt ingestion is bounded and observable. W5-4 composes the public Linux relay
owner from that same already-open spool and ledger plus the protected identity authority. It
refreshes identity on every ready generation, replays exact sent frames, settles authenticated
ACK/`publish_error`, and routes admitted commands through `LinuxDaemonIngressOwner`; write effects
are separately gated by explicit configuration and rechecked roster/revocation policy immediately
before the effect. Never-sent replaced or stale rows are removed in their normalization commit,
while sent tombstones remain durable for late receipt correlation. W0-E is pinned as `authority=candidate` with
`cutover_required=true`; it authorizes no version emission, cutover, or client-floor raise.

W5-2 moves `CloudAccount.swift`, `CloudKeys.swift`, `CloudPairing.swift` and the portable half of
`CloudTransport.swift` into that same Application target. macOS supplies the login-Keychain leaf;
Ubuntu supplies its existing descriptor-checked protected-file `SecretStore`. Both enter one
`CloudExecutorIdentityAuthority`, whose canonical bounded record owns account/machine/device,
Ed25519 and content keys, paired and revoked viewers, generation/key/revocation epochs, and the
single pending/completed handover. The normative public client uses identity `start`, four
role-bound phase writes/polls, and machine identity rotation routes; legacy three-call methods are
compatibility only. A prepared grant is durable before delivery, confirm receipt precedes pin, and
exact reply-loss retries return `duplicate`; a second claimant, stale phase or mismatched
fingerprint cannot replace it. Readiness never creates or falls back to memory. Every initial or
reconnect signature accepts only the exact current identity/epochs after the W5-1 ledger and spool
open. `swift-crypto` is
an exact Linux-only Application dependency at 4.5.2; macOS continues to use CryptoKit. The accepted
eight-member v1 handover wire and W0-E candidate authority are unchanged. First Mac enrollment
atomically renames the existing JSON roster out of the pathname a
pre-W5 binary understands, imports that fence into protected state, and removes the fence only after
the protected commit. A crash resumes from the fence, while rollback cannot revive its stale
authorization. The byte-identical public/private 843-byte route-role vector SHA-256 is
`2f79369d4ee866976c6da2a41358c1aab5321ab0e6054bf8a3afe16e215f69a9`; the public lifecycle
extension that pins exact request members, duplicate replay and confirm-receipt timing is
`79504ce608fd278cecdb2e26f62d6d1c7e915ef66f94a79cc12ff240a95e410e`.

W4-1 also adds four Application-owned policy/ownership members — `ProjectRootPolicy.swift`,
`ProviderLifecyclePolicy.swift`, `SessionLaunchPolicy.swift` and `TerminalCommandScheduler.swift`.
The last is the existing Mac serial/nested/maintenance owner moved behind the real Application
boundary, not a second Linux counter. Mac `StartPoints.start` and
`Targets.answer`, plus Linux lifecycle entry points, consume those policies before a terminal
effect. Their symlinks follow the same one-source-of-truth rule above.

### Correction: the edge is now consumed, not only declared

The original delivery proved the target-dependency edge above and stopped there: `Clawdline`'s
own SwiftPM source set *also* still compiled `HostPorts.swift`, `Assistant.swift`,
`CloudCanonicalJSON.swift` and `CloudClock.swift` a second time as ordinary members of the
`Clawdline` module, so `swift build` produced two unrelated `TargetSession` types, two unrelated
`Assistant` types, and so on — the dependency existed and nothing in `Clawdline` ever imported
`ClawdlineApplication` to reach the real ones. `Package.swift`'s `exclude:` list on the `Clawdline`
target closes that: those four files are no longer part of `Clawdline`'s own compile, so under
`swift build` there is exactly one SwiftPM identity for each type they declare, and `Clawdline`'s
remaining consumers reach it through the guarded `import ClawdlineApplication` described above.
`artifacts/W3_1_CORRECTION.md` carries the full before/after and what verified it.

The operator-facing Ubuntu/AWS build, signed-package, enrollment and acceptance procedure lives in
[`docs/aws-linux-install.md`](../docs/aws-linux-install.md). In particular, the official Ubuntu
Swift image may already own UID 1000; reuse its existing non-root identity rather than blindly
creating another user before compilation.
