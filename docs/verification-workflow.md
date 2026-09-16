# Verification and review workflow

Status: the typed graph frontier, review receipts, per-task receipt store and exact per-run
verification ledger are implemented.
Review verdicts and verification records are kept in the Observability store
(`~/Library/Application Support/Clawdline/Observability/usage.sqlite3`), keyed by task and by the
graph they belong to, and they outlive both clocks that used to delete them. Exact run reservations
and outcomes now live beside them in the same durable SQLite store. Compile artifacts can be reused
when their identity matches; the next optimization is making reuse the ordinary focused path and
measuring its hit rate rather than adding another mandatory verification stage.

## Phase 0–1 repository guards

Project Board adds a durable work-item lifecycle above these attempt-level receipts; it does not
reinterpret a task's verification summary as exact-tree proof. Board mode defaults on and may be
disabled without removing the safeguards below. Root attestations, artifact acceptance and
broker-verified landing remain separate evidence kinds. See [Project Board](project-board.md).

Since 2026-09-15 `test.sh` carries no source-text, count, seal-comparison, document-agreement or
guard-proof gates; [`testing-policy.md`](testing-policy.md) decides what gets a test. What remains
of this layer is the Swift pass determination and the receipt the verification ledger reads.
Executed check totals belong only to the run receipt and are never copied back into source or docs.

- `tools/swift-source-manifest.sh` is the Swift source inventory `test.sh` compiles.
- `Tests/TestGroupManifest.swift` records group titles at runtime and adds a failure on any identity
  or order difference without incrementing `checks`. `test.sh` requires exactly one Swift success
  receipt and one structurally valid Cloud completion receipt; their observed counts are telemetry.

Focused `CLAWDLINE_TEST_GROUPS` execution is implemented and fails closed for missing groups or a
zero-check selection; compile caching in “Runner direction” remains planned. Executed checks may
freely increase or decrease with behavior: the release-candidate run records the observed total and
does not rewrite this repository afterwards.

`test.sh` emits one `CLAWDLINE_TEST_SEAL` JSON tuple only after an unfiltered successful run has
produced exactly one Swift receipt and one Cloud receipt whose declared suite count, unique names
and positive observed counts agree internally. The tuple carries the observed strings plus the
assertion-site count as diagnostic telemetry. It is appended to the internal log **and printed to
stdout before that internal log is removed**, so the verification ledger can retain it. Nothing
writes those numbers back into `test.sh`, README files or generated governance. A focused run never
emits the full tuple and therefore cannot mint full-suite evidence.

The default `CLAWDLINE_TEST_PROFILE=release` runs product and contract checks.
It also compiles the shipped `ClawdlineLinux` SwiftPM graph and drives its protected-input/runtime
contract under the machine lock. `./test.sh --linux-package-focused` is the narrower implementer
proof for that same graph and contract; it does not mint a full-suite receipt.
`CLAWDLINE_TEST_PROFILE=infrastructure` skips that Linux graph and adds the progress helper's own
suite, for changes to `Resources/clawdline-progress.sh`. Both profiles use the same machine-wide
compile lock.

[`agent-instruction-coverage.json`](agent-instruction-coverage.json) remains as an index from root
`AGENTS.md` headings to clause owners; no test checks it.

## One risk-sized delivery graph

```text
coherent implementation batch
  -> one review that runs nothing, reading for design faults
  -> one sealed correction wave, which does not return to the reviewer
  -> the single test run, at the commit or the release build
  -> at most one correction for what that run found, then the same run again
  -> landing
  -> build/restart/smoke
```

[`testing-policy.md`](testing-policy.md) owns this order and wins wherever this page asks for more.

The unit is a rollback-safe user outcome or architecture boundary, not a file, test, checklist row
or finding. A new regression test must be able to fail on the code it is about, checked by reading
or running it once rather than recorded as a proof receipt; see
[`testing-policy.md`](testing-policy.md). An independent reader is required for high-risk boundaries
(security/authentication, durable state, concurrency, migration, destructive/external effects, or
broad cross-component semantics); routine localized/docs/generated/test-only work is read by its
owner. Either way there is exactly one review, it happens before any test run, and it answers the
three named axes and seals the complete finding set before one correction wave. Root groups
compatible slices into one release candidate and alone owns its exact full. A candidate that cannot
affect compiled/runtime behavior stops at relevant static checks.

There is no second review. A defect class that survives the correction wave moves to
`architecture_hold` and to the person who asked for the work; it does not become another review
round or another patch.

The stages above own different questions; they do not inherit one another's test list. The reviewer
owns no test run at all — it reads the exact diff, and a named review question without evidence is
reported as such rather than answered by running something. The implementer's work is proved by the
single run at the commit or release build, not by a receipt written for the reviewer. The landing
root reuses that run and verifies only changed merge seams, correction findings and changed
dependencies. Changing owners is not a reason to repeat a command. A tuple with the same tree,
question and environment is reused, while a changed tree is tested only for the question the change
could invalidate.

When a focused runner ends for a known non-semantic transport condition after producing the
required diagnostics—such as the repository's evidenced `exit 133` case—capture the bytes, tick
count, last complete check and fatal count once. Do not rerun it until green. The remaining
candidate-wide question belongs to the single exact full.

## Machine-readable verdict

```json
{"verdict":"changes_required","axes":[
  {"axis":"specification","status":"pass","findings":[]},
  {"axis":"repository_invariants","status":"findings","findings":[
    {"id":"F1","severity":"blocking","summary":"The candidate violates a shared-tree invariant.","evidence":["named reproduction or source"]}
  ]},
  {"axis":"runtime_failure_behavior","status":"pass","findings":[]}
]}
```

`safe_to_land` is invalid unless all three axes pass with no findings. `changes_required` is
invalid without at least one finding. The graph frontier treats a successful review task without a
valid receipt as failed evidence — a review that returned no verdict at all — while
`changes_required` is a returned verdict and reaches the frontier as its own node state: it admits
the `correction` node that consumes it and nothing else, so the rest of the graph waits rather than
failing. Correction closes each finding as `fixed`, `disproved`, or `deferred` with an owner.

## Durable verification receipt

**What is lost, and on which clock.** A task directory under `/tmp/.clawdline/<id>/` is swept
`orchestrator_task_dir_retention_hours` (24) after the task ends, taking the mutation logs, the
red-before receipts and every finding's evidence file with it. The registry row that holds the
verdict itself is evicted at `orchestrator_task_record_limit` (1,350 rows) or
`orchestrator_task_record_retention_days` (30 days), whichever comes first. So a question asked in
February about what a review caught in January had, until store version 6, no surviving place to
read the answer from.

**What is kept, and where.** `Orchestrator.ledgerRecord(of:)` already hands the whole stored task
to `UsageLedger` at three moments — when a task finalizes, when a landing is recorded, and once
per launch for the entire registry — and that record has always carried `review`, `verification`
and `graph`. Store version 6 stops reading past them. Four tables in `usage.sqlite3`:

| table | one row per | carries |
|---|---|---|
| `task_review_receipts` | reviewing task | `verdict`, `axis_count`, `finding_count`, `graph_id`, `node_id`, `project_key`, `kind_raw`, `task_state` |
| `task_review_axes` | axis of a review | `axis`, `status`, `finding_count`, `ordinal` |
| `task_review_findings` | finding | `finding_id`, `severity`, `summary`, `evidence` (JSON array), `axis`, `graph_id` |
| `task_verification_receipts` | verifying task | `runs`, `seconds`, `last`, `scope`, and the same task columns |

The store is the one the tokens are in, so a feature's findings and what that feature cost are one
join apart, and the three disciplines that store already keeps apply unchanged: append and seal,
coverage marks accumulate rather than replace, and an unknown is never written as `0` — a
verification record missing any of `runs`, `seconds`, `last` or `scope` is not stored at all rather
than stored with a zero in the gap.

**No foreign key to `usage_intervals`, on purpose.** A task whose session was never known and
whose record carried no usage produces no interval row, and its verdict is still the most durable
thing about it. A receipt that could exist only beside a token count would go missing exactly where
the accounting is already thinnest.

**Which tasks owe a verdict.** The role, not a spelling. `Orchestrator.requiresTypedReview(_:)`
asks the graph when the task has one — so a `correction` node dispatched as `code-review` closes
findings with owners rather than producing a fresh verdict — and reads the dispatch `kind` as words
when it does not, so `code-review`, `review` and `security-review` all qualify while `custom`,
`test` and `image` do not. The predicate it replaced compared `kind` with the literal `"review"`.
Nothing validates that field — the dispatch route stores the first forty characters of whatever it
is sent — so any closed list of spellings drifts away from what arrives, and the one this list left
out is the spelling published on this page: measured on one machine on 2026-09-06, of 56
`code-review` tasks the 38 dispatched without a graph carried no typed verdict between them, while
15 of the 18 with one did.

### Questions this store can now answer

```sql
-- Every finding of one feature, worst first.
SELECT f.severity, f.finding_id, f.summary, f.task_id
  FROM task_review_findings f
 WHERE f.graph_id = :graph
 ORDER BY CASE f.severity WHEN 'blocking' THEN 0 WHEN 'important' THEN 1 ELSE 2 END;

-- Severity distribution for one feature.
SELECT severity, COUNT(*) FROM task_review_findings
 WHERE graph_id = :graph GROUP BY severity;

-- What a feature's implementation cost against what reviewing it cost.
SELECT CASE WHEN r.task_id IS NULL THEN 'implementation' ELSE 'review' END AS role,
       SUM(u.total) AS tokens, COUNT(*) AS rows
  FROM usage_intervals u
  LEFT JOIN task_review_receipts r ON r.task_id = u.task_id
 WHERE u.graph_id = :graph GROUP BY role;

-- Reviews that landed a verdict, and the ones that did not.
SELECT u.task_id, u.kind_raw, r.verdict
  FROM usage_intervals u LEFT JOIN task_review_receipts r ON r.task_id = u.task_id
 WHERE u.kind_raw LIKE '%review%';

-- Did all three axes get answered, and by whom?
SELECT a.task_id, a.axis, a.status, a.finding_count
  FROM task_review_axes a JOIN task_review_receipts r ON r.task_id = a.task_id
 WHERE r.graph_id = :graph ORDER BY a.task_id, a.ordinal;
```

`graph_id` is the feature key for all of these. It is filled from the nested `graph.id` the stored
task record carries; before store version 6 both collectors asked for a flat `graph_id` key no
writer produced, and the column was NULL on all 1,052 rows one machine had stored. A row written
before that, for a task the registry has since evicted, keeps an honest NULL — available is a
statement about the producer, not about every row.

### The per-run receipt tuple

The broker appends one reservation and at most one outcome per *run* to
`verification_run_reservations` and `verification_run_outcomes` in `usage.sqlite3`. The tuple key
binds a canonical repository-identity digest, exact commit tree or working-overlay subject,
question id, verification kind, baseline/mutation variant, command digest and environment digest.
The reservation transaction is `BEGIN IMMEDIATE`: asking whether an exact producer already exists
and inserting that producer are one atomic operation across Sessions and SQLite connections.

The machine-token-only API is documented in `docs/api.md` under “Verification run reservations”.
Its preflight answers `reusable`, `run_required`, or `active`; mismatched idempotency identities and
unusable mutation baselines are typed conflicts. Outcomes are append-only and completion is
idempotent only when the whole reservation and outcome identities match. A malformed stored row is
reported as malformed and blocks reuse rather than disappearing as absent.

Callers do not invent those three digests. Set one stable question id and run the ordinary suite:

```bash
CLAWDLINE_VERIFY_QUESTION_ID=landing.exact-tree CLAWDLINE_SUITE_JOBS=1 ./test.sh
```

`test.sh` delegates once to `tools/verified-test-run.mjs`, which reserves before entering the
machine-wide compile lock, streams and retains the exact output, and completes the same receipt.
`reusable` exits without compiling; `active` exits 75; only `run_required` starts the suite. A dirty
overlay additionally requires `CLAWDLINE_VERIFICATION_TASK_ID`. An exported tree snapshot without
its original Git remote may carry `CLAWDLINE_VERIFICATION_REPOSITORY_ID`; an index snapshot carries
its exact `CLAWDLINE_VERIFICATION_TREE_SHA` from `git write-tree`.

The canonical digest recipe is versioned length framing: for each field, append its unsigned
eight-byte big-endian UTF-8 byte length and then its bytes, and SHA-256 the complete stream.
Repository fields are `clawdline-verification-repository-v1` and the exact `remote.origin.url`
(falling back to the real Git common-directory path). Command fields are
`clawdline-verification-command-v1` followed by each argv element, preserving argument boundaries.
Environment fields are `clawdline-verification-environment-v1`, then sorted key/value pairs for
architecture, Node version, platform, Swift version, `CLAWDLINE_SUITE_JOBS`, and
`CLAWDLINE_TEST_GROUPS`; absent is the literal `<absent>` and differs
from empty. `tools/verified-test-run.mjs` is the reference implementation.

These values are **machine-authenticated caller attestations**. The broker validates their shape,
canonical tuple relationship, exclusivity and append-only history; it does not independently run
Git, inspect the command, fingerprint the environment, or re-hash the log. A receipt therefore
proves what the local orchestrator-token holder attested and what the ledger preserved, not an
independent observation by the broker.

```json
{
  "schema_version": 1,
  "receipt_id": "uuid",
  "task_id": "uuid",
  "repository_sha256": "...",
  "question_id": "coordinator.stale-fail-closed",
  "subject": {"kind":"commit_tree","tree_sha":"..."},
  "verification_kind": "static | typecheck | focused | mutation | full | build | smoke",
  "variant": "baseline | mutation",
  "mutation_id": null,
  "command_sha256": "...",
  "baseline_receipt_id": null,
  "environment_sha256": "...",
  "outcome": {
    "state":"passed", "exit_status":0, "checks_passed":6434, "checks_failed":0,
    "duration_ms":252000, "log_sha256":"...", "full_suite_receipt_sha256":"..."
  }
}
```

A mutation receipt links to a passed baseline for the same repository, exact subject, question and
environment; a working-overlay mutation additionally stays inside the baseline's task scope. A
dirty overlay reservation requires a non-null task id, and that task id is part of its producer
identity; active and passing overlay evidence cannot cross task boundaries. Only an exact
commit-tree pass is reusable across tasks. Failed and typed `inconclusive_environment` outcomes
permit a new reservation. Full runs require a commit-tree subject, and a passing full outcome
additionally binds the SHA-256 of the complete `CLAWDLINE_TEST_SEAL` tuple. Focused evidence cannot
be completed as full evidence because verification kind is part of the immutable reservation and
focused runs cannot produce that seal.

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

- normal full-suite runs per release candidate: at most 1;
- duplicate full-suite rate for an exact tuple: 0%;
- routine localized/docs/generated/test-only review tasks: 0;
- risk-triggered review/correction waves: at most 1/1;
- second reviews of one delivery: 0;
- test runs scheduled before the review: 0;
- regression tests whose ability to fail on the old code was read or run once: 100%;
- coordination messages without an observed collision, dependency, boundary change or user decision: 0;
- code-task verification valid: 100%;
- exact-tree first-pass rate at least 90%;
- full-suite seconds per landed feature reduced by at least 50%.

Every metric keeps its denominator. `0 malformed` without `N submitted` cannot distinguish success
from an empty scan.
