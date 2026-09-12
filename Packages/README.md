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
