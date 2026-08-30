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
- assignment source (`explicit`, `inherited`, `manual`, `llm`, or `policy`);
- decision (`proposed`, `accepted`, or `rejected`) and who or what made it;
- confidence when a classifier participated;
- classifier id and version, evidence SHA-256, assignment time, and superseded event id.

The ledger keeps the evidence digest, not raw prompts or transcripts. Public analytics exposes the
canonical Project's final name, never the filesystem path.

## Small-LLM Feature merging

A local, inexpensive classifier may group related task intervals into a Feature. Its input should
be the minimum safe metadata that explains the work: canonical Project id, task title/kind,
schedule identity, explicit plan/Feature hints, and lineage. Task titles can contain sensitive
text, so remote classification requires an explicit product policy; local-only is the default.

The classifier emits a `proposed` attribution event with confidence, classifier/prompt version and
an evidence digest. A deterministic policy may append an `accepted` event above a configured
threshold, or leave the proposal for manual review. Manual correction appends another decision
that supersedes the prior one. The accounting row never changes.

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
- Comparable Project cost is available only when every row is priced and all values share one
  unit and basis. `partial_cost_coverage`, `mixed_cost_series`, and `no_cost_series` are reason
  codes, not zero-valued totals.
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

## Backfill and retention

Old rows may be assigned when durable evidence exists. They must not be guessed from a directory
basename, root Session, or successful task state. When evidence is incomplete, reports retain an
Unknown/Partial bucket. Event ids make retries idempotent, and the supersession chain preserves the
reason a Feature total changed.
