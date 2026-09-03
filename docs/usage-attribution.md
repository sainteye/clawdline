# Usage attribution: Project and Feature

Usage accounting and work attribution are deliberately separate. Token intervals are immutable
measurements. Project and Feature labels are knowledge that can improve later, so they live in an
append-only attribution log and never rewrite a token row.

## What the dashboard leads with

Generated `output` is the primary work signal. `inputNew`, `cacheRead`, and `cacheWrite` remain
separate, mutually exclusive token parts; they are context and transport characteristics, not work
produced. Unknown is never rendered as zero. Cost keeps value, unit, basis, price snapshot, and
unavailability reason separate.

This is an operational signal, not a productivity score. A short correct answer can be more
valuable than a long one, and cached context may be the cheapest part of a valuable run.

## Facts to record now

Project identity needs:

- a stable canonical Project key (the repository root, not a disposable worktree path);
- a separately editable display label;
- the observed working directory, kept only on the private forensic surface;
- how the identity was obtained (`explicit`, repository common directory, or nearest Git marker);
- the observation time and producer version.

Task lineage needs the task, schedule, parent task, retry predecessor and attempt, plus graph id,
landing state and accepted disposition whenever their producers exist. Missing lineage stays
missing; root Session or task success is not a substitute.

Feature identity needs:

- stable Feature id and display label, scoped to one Project;
- interval/task/schedule/graph/parent/retry lineage used as evidence;
- assignment source (`explicit`, `inherited`, `manual`, `heuristic`, `llm`, or `policy`);
- decision (`proposed`, `accepted`, or `rejected`) and who or what made it;
- confidence when a classifier participated;
- classifier id and version, evidence SHA-256, assignment time, and superseded event id.

The ledger keeps the evidence digest, not raw prompts or transcripts. Public analytics exposes the
canonical Project's final name, never the filesystem path.

## Small-LLM Feature merging

A local, inexpensive classifier groups related task intervals into a Feature. Its input is the
minimum safe metadata that explains the work: canonical Project id, task title/kind, schedule
identity, explicit plan/Feature hints, and lineage. Task titles can contain sensitive text, so
remote classification requires an explicit product policy; local-only is the default and is what
ships.

**What is stored, exactly.** One display label of at most 120 characters — the `Feature:` hint out
of a plan headline or task title, the declared work-line label, or a schedule title — plus a
SHA-256 digest of the fields that rung consumed. A Feature has to be readable, so the label is
kept and truncated rather than hashed; everything else about the evidence is the digest and
nothing more. No prompt, no instruction body and no transcript is stored, and no working directory
or file path: the Project scope reaches SHA-256 and never a stored column. **The classifier itself
performs no network call and consults no model** — it is arithmetic over that metadata. That is a
statement about the classifier, not about every function the surrounding process calls: the
producer reads the schedule registry through `Orchestrator.usageScheduleLabels()`, whose inventory
can send a browser push about a schedule file it cannot parse, carrying nothing of this
classifier's.

**It is off by default.** `usage_feature_classifier` (Bool, default `false`) turns the producer on;
`usage_feature_acceptance_threshold` (Double, default `0.80`, accepted range `0.5 … 1.0`) is the
confidence the acceptance policy requires. Until somebody turns it on, the dashboard says that
automatic Feature attribution is not configured, because that is the truth. An empty table is never
presented as though classification had run, and a threshold nothing is applying is never reported.

The classifier is a pure batch function over a bounded evidence set, because grouping is a batch
property: one interval cannot know on its own whether its label names a durable work line. It walks
four rungs, and the first match wins.

| rung | fires when | confidence |
|---|---|---|
| `explicit_feature_hint` | the plan headline, else the task title, opens with `Feature:` | 0.95 |
| `schedule_identity` | the row carries a non-empty schedule id | 0.88 |
| `declared_work_line` | a declared label is carried by two or more distinct tasks inside one Project | 0.82 |
| `lineage` | the parent task, else the retry predecessor, was classified by a rung above — one hop, no chains | 0.66 |

"Two or more distinct tasks" is what keeps rung 3 from turning the Feature table into a list of
tasks: a label one task carries is a one-off, and a label two tasks carry is a work line. It counts
tasks and not rows, so one task that happens to span two intervals is still a one-off.

A Feature's **Project scope** is resolved by the rule the Projects table uses and by no other: an
accepted Project head first, then the canonical stored key, and a legacy Clawdline managed-worktree
key resolves to no Project at all rather than to a Project of its own. It was two copies of that
rule once, and they disagreed about the same row: the Projects table suppressed a disposable
worktree path to `Unknown Project` while the Feature table baked that same path's hash into a
Feature id.

Because a Feature is scoped to one Project, **a work line that genuinely crosses two Projects is two
Features**, and the table shows the same label twice with the runs split. Measured on 2026-09-03,
every duplicate label in that ledger was of that kind — one repository and another, or a repository
and a disposable directory that is not a managed worktree. That is the contract working, not the
scope rule failing; merging them would need a decision that a Feature may span Projects, which this
document does not make.

Within one Feature the stored label is the **lexicographically smallest** spelling in the group,
not whichever row was written first: two spellings of one work line share an id and therefore share
an event id, and the ledger keeps the first event forever. A rung-1 hint written in quotation marks
loses both halves of the quotation rather than only the opening one.

**Rung 4 recorded nothing on the data measured on 2026-09-03**, and that is a reading of one ledger
copy rather than a property of the producer. The rung has three inputs — the row's
`usage_intervals.parent_task_id`, the durable broker record's own `parentTaskID`, and the retry
predecessor (`usage_intervals.retry_of`, else the record's `retryOf`) — and what was measured is
that `parent_task_id` was NULL in all 605 rows, that none of the 201 durable records carried a
parent, that three carried a retry predecessor, and that no row reached the rung all the same. It
ships because lineage is a named input of this contract, and at 0.66 it is below the 0.80 default in
any case: it can add proposals, never mint an accepted Feature at default settings.

Where no rung fires, the classifier says why: `no_task_identity`, `no_durable_task_record`,
`solitary_declared_label`, `no_grouping_evidence`. Those are the classifier's own words, carried by
a run receipt. They are not written into the ledger, and they are not the Portfolio's
`no_unambiguous_accepted_head`, which is a different statement about a different thing.

A match appends a `proposed` attribution event whose source is `heuristic` — not `llm`, because no
model participates, and not `policy`, because a policy decides where this only observes evidence.
It carries the confidence, the classifier id, the classifier/prompt version and a SHA-256 evidence
digest taken over exactly the fields that rung consumed. A deterministic policy then appends an
`accepted` event, source `policy`, superseding that proposal and carrying the same value, for every
proposal at or above the threshold; the rest are left for manual review. Manual correction appends
another decision that supersedes the prior one. The accounting row never changes.

Any change to a rung, a confidence, a normalization rule or the digest recipe increments the
classifier version, because event ids are derived from it. Those ids are deterministic, so a repeat
pass inserts nothing and reports what was already present instead.

**A pass never appends an acceptance beside an accepted head it did not supersede.** A version
bump, or a durable record that has since gained a plan headline, moves the event id seed; the new
acceptance would supersede only its own new proposal, leaving two active accepted heads on one
interval — which resolves to none, so the interval would silently leave its Feature for `Unknown`.
Where an active accepted head is already there, the pass appends its `proposed` event, holds the
acceptance for manual review and counts it as `heldExistingAcceptedHead`, and the existing head
keeps the interval in its Feature meanwhile. Superseding the old head instead is not available: an
accepted policy event must carry its predecessor's value, which refuses exactly when the Feature id
changed.

A production pass reads a bounded window: thirty days, at most 5,000 rows, newest first. A window
that comes back holding its whole cap says so in the receipt (`windowTruncated`), because past the
cap the batch loses its oldest rows — and rung 3 asks whether a label is carried by two tasks *in
this batch*, so a truncated window can under-report a genuine work line as
`solitary_declared_label`.

The run receipt separates a refusal from a duplicate. A proposal or acceptance the store would not
take — an event the validator refuses, a database that will not open — is counted as refused, never
as already present, because those two readings are otherwise byte-identical and only one of them
means the pass did its job.

Analytics uses only one unambiguous active accepted head. Proposal-only, rejected, or conflicting
heads remain `Unknown Feature`. This lets the grouping improve without making historical token
totals move or hiding classifier disagreement.

## Portfolio projection contract

`/v1/orchestrator/usage/analytics` carries a versioned `portfolio` projection built from the same
bounded rows as the range totals. It never reclassifies ledger evidence in the browser.

- Projects group by the stored canonical Project key. The public id is a deterministic digest and
  the public label is the canonical Project's display name; a missing key is `Unknown Project`.
  `working_dir` and its basename are not Portfolio identity fallbacks.
- Project rank uses generated output. A partly measured group keeps its measured output beside an
  `unknownOutputRuns` count; a group with no measured output carries JSON `null` and ranks after
  measured groups.
- A run is a distinct task id when present, then the stable stored boundary kind+id, then the
  session id; only a row missing all three falls back to its interval. Scheduled
  contribution requires recorded scheduled origin, and the Scheduled Work table requires an
  explicit `schedule_id`; missing identity stays `Unknown Schedule`.
- A stored Session boundary is root/main work. A stored task boundary at the broker's only
  production depth (`1`) is child work. Scheduled origin stays a separate scheduled class rather
  than being guessed as either; missing evidence is `unavailable` or `partial`. Parent labels and
  terminal state do not fill the gap, and impossible depth-2 fixtures are not evidence.
- Estimated Project spending is currently scoped to Claude Code. Codex plan-billed rows are left
  outside that estimate rather than treated as zero or allowed to hide a valid Claude estimate.
  Within the Claude-only scope every row must still be priced and all values must share one unit
  and basis. `partial_cost_coverage`, `mixed_cost_series`, and `no_cost_series` are reason codes,
  not zero-valued totals.
- Change compares generated output against the immediately preceding range with the same number
  of local calendar days. Both `from` and `to` are required, both matching reads must stay below
  the scan ceiling, and every output value in both subjects must be known. Otherwise the response
  names `closed_range_required`, `range_truncated`, `no_previous_data`, or `incomplete_output`.
- The Feature table consumes only the ledger's single active accepted head. Every other row is
  grouped under `Unknown Feature` with `no_unambiguous_accepted_head`.

Insights are bounded operational clues over those fields. They describe a soundly comparable top
mover, context-to-output ratio, coverage change, or concentration inside one fully comparable cost
series. They do not call token volume quality, impact, velocity, or productivity.

## Legacy managed-worktree Project migration

Rows written before canonical Project keys may carry a Clawdline-managed worktree path ending in a
task UUID. The Portfolio suppresses that label to `Unknown Project`; it never treats the UUID or
basename as repository evidence. The selected policy is to migrate when proof exists, through
append-only accepted Project attribution events—not by updating token rows.

Migration is a maintenance operation with this closed sequence:

1. Take a SQLite backup that includes WAL state, hash the backup with SHA-256, and keep it outside
   the live Observability directory. Apply refuses a missing or malformed backup digest.
2. Run `planLegacyProjectMigration` as a dry run over a read-only ledger copy. For each interval it
   emits either a deterministic proposal followed by a `project-migration-v1-*` accepted event,
   or an unresolved audit entry. The accepted policy decision supersedes its same-value proposal;
   save the complete versioned manifest before apply.
3. Evidence may be only one of:
   - a still-existing exact worktree for which Git returns a common directory whose non-bare
     repository root is independently verified; or
   - a durable task record whose id equals the worktree task id, whose `worktree.path` and
     `worktree.cwd` exactly equal the legacy key, and whose `worktree.repository` equals the
     original `project_dir` and resolves to a live Git common directory.
   Missing, conflicting, basename-only and UUID-only evidence stays unresolved.
4. Apply the saved plan with its backup digest. Event ids are deterministic and the attribution
   table is insert-only, so rerunning the same plan reports `alreadyPresent` and writes no second
   decision. The audit manifest records interval, legacy key, repository root, evidence source and
   digest, event id, status and reason for every candidate row.
5. Logical rollback appends one deterministic rejection superseding each accepted migration head,
   which returns the row to `Unknown Project` without changing token measurement. Byte-for-byte restore
   stops Clawdline, preserves the failed database for forensics, restores the previously hashed
   SQLite backup, and then restarts; a backup digest mismatch is a refusal, not a warning.

An accepted Project event changes only the analytics identity/label. The original `project_key`,
token parts, costs, timestamps and coverage marks remain immutable and auditable.

**The migration is not what merges two Features that share a label across two worktrees.** It is
append-only on the `project` dimension and never rewrites `project_key`, so a reading that resolves
Project scope from the raw column would still see two Projects after it ran. What merges them is
the shared scope rule above, which suppresses both disposable keys to the same unknown scope. What
the migration adds on top is the accepted head that moves such an interval out of that unknown
scope and into its repository's — the Feature id changes with it, as a scope change should.

## Backfill and retention

Old rows may be assigned when durable evidence exists. They must not be guessed from a directory
basename, root Session, or successful task state. When evidence is incomplete, reports retain an
Unknown/Partial bucket. Event ids make retries idempotent, and the supersession chain preserves the
reason a Feature total changed.
