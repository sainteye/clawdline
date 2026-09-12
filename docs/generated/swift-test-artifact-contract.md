# Swift test compile artifact contract

W0-B, CLA-296. Schema: `clawdline-swift-test-artifact-v1`.
This describes the recipe in `test.sh`, `tools/swift-source-manifest.sh` and
`tools/swift-test-artifact.sh`. It changes verification tooling, not production behavior.

## Entry points

The original uncached full entry remains `./test.sh`. Reuse is explicit and requires a
caller-owned cache outside the checkout. Each run still needs its own `TMPDIR`.

```bash
# Full execution, with opt-in compile reuse. All existing guards and suites still run.
CLAWDLINE_SWIFT_TEST_ARTIFACT=reuse \
CLAWDLINE_SWIFT_TEST_CACHE_DIR=/absolute/private/cache \
TMPDIR=/absolute/private/run-tmp ./test.sh

# A focused Swift execution; no unrelated node/browser suites or Cloud acceptance.
CLAWDLINE_TEST_GROUPS='palette colours parse' \
CLAWDLINE_SWIFT_TEST_ARTIFACT=reuse \
CLAWDLINE_SWIFT_TEST_CACHE_DIR=/absolute/private/cache \
TMPDIR=/absolute/private/another-run-tmp ./test.sh --swift-focused

# A direct Cloud suite execution; comma-separate multiple exact names when proving aggregation.
CLAWDLINE_SWIFT_TEST_ARTIFACT=reuse \
CLAWDLINE_SWIFT_TEST_CACHE_DIR=/absolute/private/cache \
TMPDIR=/absolute/private/cloud-run-tmp ./test.sh --cloud-focused CloudOutboundSpool
```

Create the cache with permissions `0700`. The helper rejects a cache writable by other users,
a cache inside the checkout, and an output inside the cache. Set
`CLAWDLINE_SWIFT_TEST_ARTIFACT=off`, or leave it unset, to use the original compiler invocation.
Unknown artifact modes and command-line modes are errors. `--swift-focused` and
`--cloud-focused` also work uncached.

`CLAWDLINE_TEST_GROUPS` contains exact group titles separated by newlines. Unset means full
execution unless `--swift-focused` was requested. An explicitly empty or whitespace-only value,
unknown title, or selection with zero executed checks fails. Empty separator rows and duplicate
requests follow the existing Swift split/Set semantics. Preflight reads the ordered manifest's
closed literal/concatenation grammar; an unfamiliar expression is an error. The Swift harness
remains responsible for proving actual matches and nonzero checks at runtime.

The focused entry preserves source-manifest and architecture guards, the compile job ceiling,
the machine lock, test-store isolation, output streaming, exit propagation and receipt direction.
The no-argument full entry preserves its entire existing node/browser roster and twelve Cloud
suites. Supplying groups without `--swift-focused` retains the legacy pre-Swift guard/suite path;
the Swift result still has focused scope.

`--cloud-focused` accepts one or more comma-separated names from the closed twelve-suite Cloud
roster. Empty, unknown, duplicate or mixed Swift/Cloud selections fail before compilation. The
Swift harness skips generic groups, runs the selected Cloud suites in canonical roster order,
continues after a suite failure so later evidence remains reachable, and emits a focused receipt
that cannot be mistaken for the full twelve-suite completion receipt.

## Identity and supported input closure

Every lookup builds canonical JSON and uses its SHA-256 as the cache key. There is no timestamp
fast path or caller-supplied trusted digest. The identity includes:

| Input | Binding |
|---|---|
| Repository and subject | SHA-256 of the resolved Git common directory; resolved working directory; `HEAD^{tree}`; a digest of all tracked and nonignored untracked paths, bytes, executable modes, deletions and link spellings |
| Compiler | Invoked and resolved executable paths and bytes, link destination, complete `--version` and target-info output |
| Toolchain | Full resolved compiler toolchain and invoked tools directory's enclosing toolchain; content, names, modes and symlink targets, including tools and Swift/Clang runtime resources |
| Reported runtime | Apple Swift's root-level `paths` object: its optional `sdkPath` must resolve to the requested SDK; every `runtimeResourcePath`, `runtimeLibraryPaths` and `runtimeLibraryImportPaths` target is sealed; edges into an already inventoried toolchain/SDK reference that closure, external targets are inventoried separately, absent optional search directories have explicit absence records |
| SDK | Resolved SDK path and recursively hashed contents; a version label alone is insufficient |
| Host | Kernel, architecture and macOS build identity |
| Arguments | Complete ordered flags and source arguments, including target, language version, jobs, frameworks, explicit SDK/tools directory and fresh module-cache location |
| Sources | Complete ordered source manifest arguments and each source's bytes/mode/link target; the manifest script's own bytes are also a resource |
| Resources | The complete `Resources` tree, including ignored descendants and linked target bytes; `test.sh`, the source-manifest script and artifact helper |
| Environment | A closed compiler environment: selected toolchain tools plus system tools on `PATH`, C locale, and fresh private HOME/TMPDIR; ambient compiler/search-path injection is removed |

Private output, home, temporary and module-cache locations are represented by relocation tokens
in the identity. They start fresh on each cold compile; the output basename remains
`clawdline-tests`, and source arguments are still relative to the original checkout. The invocation
records their actual positions in the ordered argument vector. These are outputs, not reused
compile inputs. Runtime group selection does not change a compile artifact.

An archive initialized with Git and staged for manifest scanning may have no commit. The helper
proves that HEAD is an unborn symbolic reference and records `head_state: "unborn"` with
`head_tree: null`, while still binding the entire working-overlay digest. It never labels that
snapshot as a commit-tree receipt or trusts a caller-provided tree name. Other Git failures refuse.

The default compiler comes from `xcrun --find swiftc`; `CLAWDLINE_SWIFT_TEST_SWIFTC` can name a
different trusted local compiler installed beneath a `bin` directory. `SDKROOT`, or the SDK
resolved by `xcrun --sdk macosx --show-sdk-path`, selects the explicitly passed SDK. The artifact
recipe supports the current macOS test target. Linux returns `artifact_platform_unsupported`;
W0-F's separate Core probe must define its Linux runtime/toolchain closure before enabling reuse.

Compiler symlinks may cross toolchains: both the invoked tools and resolved compiler toolchain
are bound, together with the compiler-reported resource closure. The accepted target-info document
requires Apple Swift's `compilerVersion`, `target` and root-level `paths` fields, accepts its
optional string `swiftCompilerTag`, and refuses other root fields. The earlier
`resourcePaths` spelling was synthetic test-fixture behavior, not an observed production Swift
contract, and is refused; mixed or unknown root shapes are refused too. A missing/malformed runtime
resource root, relative resource path, unknown resource-path field or unprovable compiler layout
is a typed refusal. A version string or the text of a resource path never substitutes for its
contents. The compiler remains a trusted local executable, not an untrusted program sandbox.

When `CLAWDLINE_VERIFY_QUESTION_ID` enables the outer durable verification ledger,
`tools/verified-test-run.mjs` computes the artifact environment digest **before reservation**.
It calls the same `compiler_environment` recipe as the inner helper, binding exact compiler,
SDK, target, runtime and host inputs, the runner/manifest/helper/wrapper recipe bytes, and the
entire `Resources` tree (including ignored inputs). The target comes from
`clawdline_swift_test_target` in the source manifest used by the artifact invocation. An
unreadable identity returns `artifact_preflight_identity_unavailable`; no reservation or
reusable answer is possible. There is no caller-provided digest bypass. Existing repository,
subject, command, question, scope, reservation and completion contracts remain authoritative.
Artifact and uncached requests have distinct environment identities; ordinary uncached runs
retain their original compiler invocation.

The accepted flag vocabulary is closed: `-swift-version`, `-target`, `-j`, `-framework`, `-D`,
`-O`, `-Onone`, `-Osize`, `-g` and `-enable-testing`. Values have closed lexical forms. Response
files, additional SDK arguments, plugins, header/library search paths and arbitrary linker flags
are refused until their input closure is explicitly supported. Sources must be unique, existing
relative Swift files under `Sources` or `Tests`; the full entry separately checks manifest parity.
Unmerged Git entries, submodules, unreadable inventories and special files fail closed.

Directory inventories represent ancestor symlinks as graph edges to already enumerated nodes.
Other symlink targets are traversed, including external targets. A dangling target's absence is
recorded, so its later appearance invalidates the key. Unresolvable links are errors.

## Publication and reuse

1. Confirm ownership through the **existing owning shell's** `clawdline_confirm_suite_lock`.
   No helper acquires another compile slot. The compile and execution remain under
   `/tmp/clawdline-suite.lock` and its existing renewal/reclamation rules.
2. Read all identity inputs. For a hit, require exactly `metadata.json` and `clawdline-tests`,
   ordinary files, canonical metadata bytes equal to the current identity, and matching binary
   SHA-256, positive size and executable mode. A filename or metadata assertion alone is not proof.
3. On a miss, atomically create `<key>.publishing` inside the cache. A concurrent or abandoned
   reservation is a typed refusal; the helper never steals it or kills a process. Recheck for a
   completed hit after taking the reservation.
4. Compile in that reservation with fresh output/module-cache/home/temp directories. Confirm the
   lock token immediately before compilation. A failed or missing binary cannot be published.
5. Re-read input identity and confirm the lock token. Flush binary and metadata, then publish the
   complete artifact directory into `<key>` with macOS `renameatx_np(RENAME_EXCL)`, using opened
   parent directory descriptors. This atomically refuses every existing destination, including
   an empty directory, file or symlink that appeared during compilation, preserving its inode.
   A platform/filesystem without exclusive rename is refused; there is no replacing-rename
   fallback. Validate the published pair through the same hit
   verifier. Ordinary failures remove only this invocation's unpublished reservation.
6. Copy verified binary bytes to a fresh private inode next to the caller's output, verify the
   copy, re-read input identity, reconfirm the lock, then atomically replace the caller's binary.
   Existing ancestor device/inode identities enforce cache/repository/output containment even
   through case-folded and symlink aliases. The output parent identity is captured before compile
   and revalidated immediately before replacement; staging, replacement and cleanup use the
   pinned parent descriptor. The delivered regular file must belong to the caller, have one link,
   and have a different device/inode pair from the shared binary; the cache binary must also have
   one link. Existing unrelated output hardlinks retain their old bytes/inode after replacement.
   The test process never executes the shared cache inode. The original shell reconfirms its lock
   before running it, as on the uncached path.

Persistent input changes during compilation or hit delivery are refused. Root acceptance should
still use an isolated exact candidate: pre/post hashing is not a transactional filesystem snapshot
and cannot prove against an adversary changing and restoring inputs between reads. This is a
local optimization for a trusted compiler and caller-owned files, not a signature or a trust
boundary against another process with the same user's write authority.

A corrupt or incomplete final directory is refused, not silently repaired. An interrupted
publication can leave `<key>.publishing`; its existence never counts as success. Inspect the
owner's run before removing such a reservation. A caller can select a new private cache or use
the uncached path without altering the machine compile lock.

## Receipts and refusals

The helper prints `CLAWDLINE_SWIFT_TEST_ARTIFACT` followed by JSON containing `version: 1`,
`kind: "compile_only"`, `reused`, `identity_sha256` and `binary_sha256`. This is compile provenance.
It asserts no passing tests and contains no full-suite seal fields.

A focused run must have exactly one positive `N focused checks passed` line and the selected
successful group titles in manifest order. Missing, duplicate, zero, failed or mismatched evidence
is refused. A focused log containing a full Swift result, a Cloud completion line or
`CLAWDLINE_TEST_SEAL` is refused. Both the full verifier and the full seal emitter independently
reject focused context, in addition to their existing outer scope gates.

The full completion flow records its observed Swift count, assertion-site telemetry and Cloud
receipt in one runtime tuple. Nothing copies those values back into `test.sh` or docs. The
permanent artifact tests check full/focused separation, receipt uniqueness and structural Cloud
roster validity rather than freezing a previous tree's counts. Compile reuse does not waive any
full receipt gate.

A compiler failure prints `CLAWDLINE_SWIFT_TEST_COMPILE_FAILURE` JSON with `version: 1`,
`kind: "exit" | "signal"`, the original subprocess `returncode`, and the returned `exit_status`.
Positive exit codes are preserved, including 126–255. A negative signal returncode `-N` becomes
the shell-compatible status `128 + N` and includes `signal: N`; thus explicit exit 137 and a
SIGKILL both return 137 but remain distinguishable in the typed evidence. No success receipt is
printed. Normal failure cleanup removes the unpublished reservation before returning the status.

| Status | Meaning |
|---|---|
| `2` | Invalid/unsupported mode, selection, flags or input contract |
| `73` | Artifact publication busy, or absent/lost artifact lock ownership |
| `74` | Unreadable/corrupt inputs, changed inputs/output parent, nonprivate binary/copy, publication conflict, unavailable exclusive publication or publication I/O failure |
| `75` | Existing machine-lock admission/confirmation refusal; no second compile lane |
| `125` | Focused receipt invalid, or attempted full receipt verification/emission in focused context |
| Other nonzero | Original compiler or test-process failure; stderr names the failing stage |

## Focused proof and acceptance limits

`node Tests/swift-test-artifact.mjs` runs the fake-compiler matrix in private temporary directories.
`--case '<exact case name>'` selects one case and refuses a zero-match selection. The matrix covers
cold/warm use, identity dimensions, ignored/linked resources, metadata/binary tamper, interrupted
and concurrent publication, input/lock changes, group selection, full-receipt non-forgeability and
the original compile dispatch. It also exercises cross-toolchain links, external runtime closure,
outer ledger inequality, case/symlink/hardlink aliases, parent replacement, publication-time
competitors, legitimate reseal and compiler exits/signals. It starts no real Swift compiler and
takes no real machine lock. Case-folding coverage requires a case-insensitive volume; on a
case-sensitive volume the test explicitly reports a symlink-only alias control.

Root still owns independent review, a real cold compile from the exact candidate, a warm focused
execution of that artifact, and the one full exact-tree acceptance run. The uncached entry is the
rollback seam. This tooling neither installs the Mac app nor deploys anything to Cloud.
