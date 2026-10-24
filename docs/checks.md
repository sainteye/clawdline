# Check scope and timing

The development target is ten seconds; the ordinary local commit target is one
minute. These are workflow goals, not timeouts that kill an unfinished test or
turn missing coverage into success. Measure elapsed work separately from waiting
for the compile slot. A large changed package or a conservative fallback can
exceed the target and must remain visible.

## Development

```sh
tools/check.sh --quick ./internal/config --run=TestRetiredClaimScanPreservesLineNumbersAndSentenceRules
tools/check.sh --quick ./internal/adapters/store
```

Run the smallest relevant package/test while editing. `--run=` accepts Go's test
name expression. Quick mode is explicitly a development check, not a commit or
integration receipt. A package with slow tests may need a narrower expression to
meet ten seconds. Ordinary tests run directly without the compile queue. Only application/native builds use `tools/heavy.sh`; opaque native build scripts use `tools/heavy.sh --compile`.

A result must not depend on the developer's machine. `cmd/clawdline` tests run
under a `TestMain` (`cmd/clawdline/hermetic_test.go`) that clears every
`CLAWDLINE_*` variable, points the state directory and home at a temporary
tree, and aims the daemon port at a closed loopback port, so a machine whose
saved `product_language` is not English, or whose daemon answers on 7727,
gets the same result as CI. A test that needs a language, directory or daemon
sets it with `t.Setenv` or starts its own.

## Ordinary local commits

```sh
tools/check.sh --plan       # Explain changed paths, direct tests and callers.
tools/check.sh              # Run the local change gate.
tools/check.sh --callers    # Also compile transitive callers' tests without running them.
```

Scope discovery prepares ignored web dependencies before loading TypeScript,
including on the first run in a new worktree. `--plan` may prepare these links,
but runs no test suite and leaves stdout as JSON. Preparation failure stops with
exit 2 rather than disguising a missing resolver as a reason for full testing.

The plan includes committed changes since the merge base with `main`, staged
changes (even when the working file was reverted), unstaged, deleted, renamed
and untracked files. A known empty change set is different from unreadable Git
state. Unknown/tooling/policy inputs and unreliable discovery fall back to full
verification, with a reason. Platform-constrained inputs are named in the plan; foreign-platform validation
remains an explicit integration/CI requirement.

Go package metadata identifies the changed packages and the transitive callers,
including test imports. API schema and route inputs under `api/v1/` select
contract-package tests and contract generation checks alongside changed packages;
they do not force the full platform suite. The local gate selects tests in files paired with changed Go source files,
or directly edited test files. Ambiguous/shared inputs retain the entire package. HTTP server construction
belongs to the route test module, alongside each changed handler module.
The JSON test report must show actual test results; an empty selector is refused.
This local module selection does not claim cross-file runtime coverage. Caller
compilation can be requested with `--callers`; caller runtime suites wait for
integration and CI. A deleted or unowned package cannot silently disappear.
Selection is by path and tests execute the working tree; partial-hunk commits
need exact-commit CI evidence; pending CI does not block dev installation. Repository-copy and
capacity-registration guards still run, uncached, even for
documentation edits. Contract generation runs when schema/generated-contract inputs change.
Capacity guards run for Go or capacity-copy inputs; CSS and shell guards follow
their inputs. Tool self-tests run for tooling/full checks. Repository-copy,
privacy and source guards remain; the machine-word guard scans changed source
and catalog files, including new files, with whole-tree coverage in CI; full CI retains complete validation.

For the console, TypeScript checks the entire project incrementally. Its build
information stays in the ignored `web/.clawdline-check.tsbuildinfo` of this
worktree and is removed with the checkout. The TypeScript resolver traces import,
export and literal dynamic-import dependencies to affected tests. Filesystem
readers and computed loaders remain conservative readers of changed inputs.
Type-only imports are checked by whole-project TypeScript and do not expand
runtime selection. Generated semantic inventories retain their validators and
filesystem-reader tests; configuration/unknown fixtures run broader web tests. Catalogs
retain their complete localization validators. The bundle build waits for
integration/CI. A test selection is not a claim that unselected tests ran.

Privacy checks on ordinary commits read the changed files through literal Git
pathspecs, including untracked files. They still require the private-word list;
undetermined is not a pass. Whole-tree checks remain mandatory at Release; the existing incoming-commit
history privacy guard still applies to pushes. A local delta check does not
certify old commits or an unchanged tree against a changed private-word list.

## Integration, CI, dev and releases

```sh
tools/check.sh --full
tools/check.sh --full --profile
```

Full suites, platform checks and bundles run in background CI after integration.
Do not block local commits, landing on `main` or dev installation on a full local
gate. Show pending or failed CI explicitly and fix failures. The public Release
gate requires successful complete verification of its exact tree, whole-tree
privacy and release acceptance. `--full` remains available for diagnosis and
manual integration verification; `--web` requests the complete web gate locally.

The release evidence test, `tools/release/supply-chain.test.sh`
(`docs/security/release-evidence.md`), runs in every `--full` check, in an
ordinary check when `tools/release/`, `tools/check.sh` or the release
workflows change, and by name in CI's static (Linux) and darwin jobs, because
CI does not run `tools/check.sh`. It drives the SBOM, provenance, rebuild,
macOS signature and gate checks through their passing case and each failure
on synthetic releases; it proves no real attestation or notarization.

The native shell's pairing test, `tools/test-shell-pairing-permission.sh`,
compiles `shell/darwin/Pairing.swift` with a stub session and drives the flow
that saves the chosen permission before the code is shown. It runs on macOS
only: in every `--full` check, in an ordinary check whenever a path outside the
narrow Go/web/docs inputs changes (any `shell/darwin/` or `tools/` file selects
the full tooling group), and by name in CI's darwin job. It builds for the
host's own architecture with a macOS 13 minimum, so an Intel runner runs what
it compiled. Its request timeout is the registered bound
`console.native_pairing_save_seconds`; `internal/domain/capacity`'s
`TestNativePairingSaveTimeoutMatchesTheShell` fails when the Swift constant and
the row disagree or the row is missing.

Dev installation builds the selected commit without repeating full verification.
After switching, verify the running version and served stamp, `GET /` returning
200 and existing Sessions remaining available. Startup failure rolls back through
the updater. Passing these checks does not certify pending CI or Release coverage.

Profile mode forces uncached Go tests with JSON events and retains logs and
`go-profile.json`. Package times overlap; subtests overlap parents. Neither sum
is wall time. Event time excludes work before the first event. Compare ordinary
warm runs separately because `-count=1` intentionally bypasses test-result reuse.

## Linux cache ownership and disk pressure

Linux heavy commands share a persistent tool-owned cache under
`$XDG_CACHE_HOME/clawdline/heavy-go-v1` (default `$HOME/.cache`). A private owner
marker and user ownership are required. Unmarked or symlinked caches are refused;
a file lock serializes use without a daemon too. Nested wrappers reuse the lock.

The managed-cache budget is 4 GiB, with a 2 GiB free-space reserve. Decimal KiB
constants in `internal/domain/capacity/heavy_cache.go` are read by the wrapper
and recorded in `capacity.ToolRegister`. Both capacity guards validate daemon
and tool bounds. Tool measurements are emitted by the wrapper, not invented as
zero daemon usage or added to the daemon's capacity UI.

Before and after each command, the wrapper measures its owned cache and disk.
Only budget or disk pressure triggers reclamation: oldest recognized compiled-cache entries first,
modules only if needed. Go refreshes entry mtimes on use; metadata sorting spills
to disk instead of retaining an unbounded candidate list. Pressure no longer
clears the entire build/test cache. Unknown files in the cache are retained. A still-low preflight refuses the build. Original command
failure survives cleanup failure. Unknown ownership or measurement never
permits deletion. This is a between-command budget, not an in-flight quota.

Explicit `GOCACHE` and `GOMODCACHE` values stay with their owners, including gate
scratch. The default shared Go caches are not cleared or migrated. The old shared
cache needs the owner/idle-process review in `docs/disk-lifecycle.md`. Persistent
wrapper caches and the per-worktree TypeScript build information follow their
stated lifecycle instead of per-command Session cleanup.

## Seeing a background check

Daemon callbacks appear in their owning Session's existing background-work strip,
including Codex callbacks whose assistant has returned to idle. Open a command
like a provider shell to read its command, working directory, phase and recent
output. Queue placement is distinct from execution. Reads use the existing
Session-shell route and Cloud capability with the current Session identity; they
do not acknowledge completion. Logs use the registered callback tail bound and
remain outside repository source. Closing the panel stops reads. An expired or
unreadable log is reported instead of being treated as successful completion.
