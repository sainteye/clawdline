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

## Backfill and retention

Old rows may be assigned when durable evidence exists. They must not be guessed from a directory
basename, root Session, or successful task state. When evidence is incomplete, reports retain an
Unknown/Partial bucket. Event ids make retries idempotent, and the supersession chain preserves the
reason a Feature total changed.
