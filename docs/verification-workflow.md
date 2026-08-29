# Verification and review workflow

Status: process contract now; broker/runner automation described below is planned.

## Phase 0–1 repository guards

The refactor foundation implements three local guards. Phase 0 preserved the historical
6,434-check baseline; approved Local Usage integration moved the current receipt to 6,531:

- `tools/swift-source-manifest.sh` is sourced by both `build.sh` and `test.sh`; production mode
  compares the 89-entry production partition only with recursive `Sources/` inventory, while full
  mode separately compares both the 89-production and 40-test partitions. Partition swaps fail
  closed, and Tests-only drift does not block the application build.
- `tools/check-architecture-boundaries.sh` verifies the entry point remains at most 500 lines,
  24 ordered runners,
  442 current sealed group identities, production net-growth receipts and the 2,000-line suite ceiling.
- `Tests/TestGroupManifest.swift` records group titles at runtime and adds a failure on any identity
  or order difference without incrementing `checks`. `test.sh` separately requires the exact
  `6531 checks passed` line and the existing Cloud receipt exactly once.

The missing-nested-source mutation returned 1 before the fixture was restored; the entry-point
growth mutation returned 1 at 534 lines before the 34-line entry was restored. These are guard
proofs, not extra full-suite runs. Focused `--group` execution and compile caching in “Runner
direction” remain planned; this phase does not pretend they exist.

The four sealed structural/count receipts have different owners. A legitimate check change updates
the `6531 checks passed` expectation in `test.sh` and the recorded baseline. A group identity change
updates `expectedOrderedTestGroupTitles`, the `442` architecture expectation and the baseline. A
runner-boundary change updates `Tests/main.swift`, its documented order and the `24` expectation. A
suite-file change updates the manifest and the `34` `*Tests.swift` expectation. The entry point's
current 34-line size is an observation, not another exact guard; its enforced limit is 500. Change
only the receipts affected by the approved behavior change, record the old guard going red, then
record the updated guard green.

## One feature graph

```text
implementation
  -> focused self-proof
  -> independent review
  -> one sealed correction wave
  -> focused confirmation
  -> one root exact-tree full suite
  -> landing
  -> build/restart/smoke
```

Implementation proves new tests red before green and verifies only the claimed feature. Review
answers a named charter across input/output, read/write, success/failure, time/thread/path,
persistence/restart, projections and foreign hunks. The complete finding set is sealed before
correction. Confirmation reopens only those findings and adjacent regressions. Root alone owns the
normal graph's full suite.

A third review requires `scope_changed`, `new_external_evidence`, or `systemic_pattern`. A repeated
defect class beyond that correction seam moves to `architecture_hold` instead of a fourth patch.

## Machine-readable verdict

```json
{
  "subject_tree": "<tree sha>",
  "verdict": "safe | correction_required",
  "questions": [
    {"id":"coordinator.stale-fail-closed","result":"pass","evidence":["receipt-id"]}
  ],
  "findings": [
    {
      "id":"F1",
      "severity":"high",
      "class":"fail_open",
      "path":"Sources/Coordinator.swift",
      "status":"open",
      "question_ids":["coordinator.stale-fail-closed"]
    }
  ]
}
```

SAFE is invalid when the subject differs from the delivery tree or any question/finding lacks a
disposition. Correction closes each finding as `fixed`, `disproved`, or `deferred` with an owner.

## Durable verification receipt

The broker should append receipts outside task `work/`, keyed by repository identity, exact tree,
question, command digest, environment fingerprint and variant. Only commit-tree receipts may be
reused across tasks.

```json
{
  "version": 1,
  "receipt_id": "uuid",
  "feature_id": "session-coordinator-freshness",
  "task_id": "uuid",
  "phase": "implementation | review | correction | confirmation | integration",
  "question_id": "coordinator.stale-fail-closed",
  "subject": {"kind":"commit_tree","tree_sha":"...","overlay_sha256":null},
  "verification_kind": "static | typecheck | focused | mutation | full | build | smoke",
  "variant": "baseline | mutation",
  "mutation_id": null,
  "command_sha256": "...",
  "environment": {
    "os_build":"...", "arch":"arm64", "swift_version":"...", "node_version":"...",
    "sandbox":"native", "test_script_sha256":"...", "private_tmpdir":true
  },
  "outcome": {
    "exit_status":0, "checks_passed":6434, "checks_failed":0,
    "expected":"pass", "duration_ms":252000, "log_sha256":"..."
  }
}
```

A mutation receipt links to a baseline for the same question. A dirty overlay is explicitly local
self-proof. The same exact tuple is not rerun after green. A second full suite is valid only after a
typed `inconclusive_environment` result.

### Reproducible working-overlay digest

An overlay receipt records its exact base commit separately and computes `overlay_sha256` from a
worktree whose `HEAD` is that base. Hash the bytes from
`git diff --binary --full-index --no-ext-diff HEAD` exactly as emitted. Then, for every untracked
path returned by `git ls-files --others --exclude-standard -z` in bytewise (`LC_ALL=C`) sorted order,
append `NUL`, the path bytes, another `NUL`, and the file bytes. SHA-256 the resulting byte stream.
Tracked changes (including deletions, modes and binary patches) are therefore bound by the diff;
untracked paths and contents are bound explicitly. Do not add a separator after the diff other than
the leading `NUL` of the first untracked record, and do not include ignored files.

One reference implementation is:

```bash
{
  git diff --binary --full-index --no-ext-diff HEAD
  while IFS= read -r -d '' path; do
    printf '\0%s\0' "$path"
    dd if="$path" status=none
  done < <(git ls-files --others --exclude-standard -z | LC_ALL=C sort -z)
} | shasum -a 256
```

The recorded base commit, digest recipe and digest are one subject identifier. A digest from an
earlier report that omitted its byte recipe is superseded rather than treated as comparable.

## Invalid result metadata

Malformed child verification must not silently become absent. Task success remains independent,
but readers receive:

```json
{
  "verification_status":"invalid",
  "verification_errors":[
    {"field":"seconds","code":"expected_non_negative_integer"}
  ]
}
```

Completion notices surface the warning. Legacy omitted data is `not_reported`, not `invalid`.
A machine-authenticated root may append a corrected receipt without erasing the original error.

## Runner direction

Keep one runner and split compile from execution scope:

```text
./test.sh --compile-only
./test.sh --group <stable-group-id>
./test.sh --full
```

Cache compilation by source/tree digest, compiler version and flags. A focused run prints executed
groups and check counts but can never emit the full completion receipt. Unknown group exits 2;
targeted-as-full exits 125; any source/toolchain/flag change misses cache; full mode proves every
registered group ran.

## Metrics

- normal full-suite runs per landed feature: 1;
- duplicate full-suite rate for an exact tuple: 0%;
- ordinary review/correction waves: at most 1/1, high risk 2/2;
- third reviews below 5%, all with typed reason;
- new tests with red receipt: 100%;
- code-task verification valid: 100%;
- exact-tree first-pass rate at least 90%;
- full-suite seconds per landed feature reduced by at least 50%.

Every metric keeps its denominator. `0 malformed` without `N submitted` cannot distinguish success
from an empty scan.
