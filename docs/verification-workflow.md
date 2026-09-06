# Verification and review workflow

Status: the typed graph frontier, review receipts and the durable receipt store are implemented.
Review verdicts and verification records are kept in the Observability store
(`~/Library/Application Support/Clawdline/Observability/usage.sqlite3`), keyed by task and by the
graph they belong to, and they outlive both clocks that used to delete them. The per-run receipt
tuple sketched under "Durable verification receipt" — one row per command, environment and variant
— is not what shipped; what shipped is described there instead, with the sketch kept below it as
the direction the tuple would extend in.

## Phase 0–1 repository guards

The refactor foundation implements three local guards. **This section deliberately names no
counts.** It carried five of them — a check target, a runner count, a group count, a suite-file
count and two source-manifest partition sizes — and every one had drifted by the time anybody read
them, because each is a receipt that moves with the tree while a paragraph does not. The current
values are in the generated table in
[`architecture-refactor.md`](architecture-refactor.md), written by
`tools/generate-governance-table.sh` from the tree itself; the guard refuses a tree whose table is
not that run's own rendering, which is why retyping a number into it is never the fix.

- `tools/swift-source-manifest.sh` is sourced by both `build.sh` and `test.sh`; production mode
  compares the production partition only with recursive `Sources/` inventory, while full mode
  separately compares both the production and test partitions. Partition swaps fail closed, and
  Tests-only drift does not block the application build.
- `tools/check-architecture-boundaries.sh` verifies the entry point stays within its line limit,
  the ordered runner count, the current sealed group identities, production stop-growth receipts
  and the 2,000-line suite ceiling.
- `Tests/TestGroupManifest.swift` records group titles at runtime and adds a failure on any identity
  or order difference without incrementing `checks`. `test.sh` separately requires the exact
  `expected_swift_receipt` line and the existing Cloud receipt exactly once.

The missing-nested-source mutation returned 1 before the fixture was restored; the entry-point
growth mutation returned 1 at 534 lines before the 34-line entry was restored. These are guard
proofs, not extra full-suite runs. Focused `CLAWDLINE_TEST_GROUPS` execution is implemented and
fails closed for missing groups or a zero-check selection; compile caching in “Runner direction”
remains planned.

The sealed structural/count receipts have different owners. A legitimate check change updates
`expected_swift_receipt` in `test.sh` from a run, never from arithmetic, and moves
`expected_swift_receipt_witness` to the assertion-site count that run was taken on. A group identity
change updates `expectedOrderedTestGroupTitles`. A runner-boundary change updates `Tests/main.swift`
and the runner-count expectation in the guard. A suite-file change updates the manifest and the
suite-file expectation. The entry point's size is an observation, not another exact guard; only its
limit is enforced. Change only the receipts affected by the approved behavior change, record the old
guard going red, then record the updated guard green — and regenerate the governance table
afterwards, because it is the one place all of them are written down at once.

**A child adding assertions does not reseal.** Adding a `check(` or `expect(` moves
`expected_swift_receipt_witness`, and the architecture guard refuses to start a compile while the
witness names a different tree. `CLAWDLINE_RESEAL=1` downgrades that refusal to a warning so a
focused run can proceed; both seal values stay as they are, for the landing root to set from the
exact-tree run.

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
answers three named, independent axes: `specification`, `repository_invariants`, and
`runtime_failure_behavior`. The complete finding set is sealed before correction. Confirmation
reopens only those findings and adjacent regressions. Root alone owns the normal graph's full suite.

A third review requires `scope_changed`, `new_external_evidence`, or `systemic_pattern`. A repeated
defect class beyond that correction seam moves to `architecture_hold` instead of a fourth patch.

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

### The per-run receipt tuple, not yet built

The broker should also append one receipt per *run*, outside task `work/`, keyed by repository
identity, exact tree, question, command digest, environment fingerprint and variant. Only
commit-tree receipts may be reused across tasks. That is the direction the tables above extend in;
what they hold today is one receipt per task, which is what the task record carries.

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
