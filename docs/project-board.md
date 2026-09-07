# Project Board

Project Board combines durable work items with existing dispatch, Session, worktree, usage and
landing records. It is **enabled by default and currently free**. The entitlement returned by the
API is `free_preview`; this is not a subscription check or a promise about future pricing.

Settings → Enable Project Board changes the persisted machine-wide workflow mode. Turning it off
keeps history readable and restores the standalone Projects/Worktrees, Usage and Ledger entries.
It does not cancel tasks, close Sessions, delete data or disable the ordinary permission, claims,
independent-review, landing or machine-resource protections. Existing work can finish through its
original task protocol. Re-enabling does not manufacture usage boundaries for the suspended time.

## A work item is not an execution attempt

Each Project has a list, optional board columns, and item detail. The six execution types are
Feature, Refactor, Task, Bug, Coordination and Epic. An item may link several tasks, Sessions and
worktrees; task success is delivery, not item closure. The lifecycle is:

`backlog → planning → ready → execution → verified → integrated → closed`

Cancellation is separate from completion. Reopening is explicit. Work beyond planning needs an
owner. Verification needs evidence, not only checked boxes. Code integration needs a broker
landing record; a URL or an agent's statement that it merged is insufficient. A non-code Task
can instead use an artifact accepted by an administrative user. Open blocking findings prevent
verification; unsettled obligations, milestones and pending handoffs prevent closure. An Epic's
children must be delivered before its integration. A handoff proposes a receiver, and only that
authenticated receiver accepts it; it is not a shortcut to completion.

The durable board record survives task retention and terminal closure. It does not replace
`work_state` (what a Session is doing) or the broker task state (what an attempt reached).
Worktrees remain available from the board as an advanced resource view, not the unit of work.

## Clawdfather and agent workflow

The owning root creates or selects a work item before dispatch. Add these optional fields to
the normal task JSON:

```json
{
  "work_item_id": "the opaque board item id",
  "work_phase": "output"
}
```

`work_phase` is one of `planning`, `output`, `review_testing`, `correction`, `integration`.
It describes the **entire attempt**; changing activity within a standing Session is only a
declaration unless a measured boundary exists. The fields are persisted with the task and exposed
as `workItemId` / `workPhase`. They remain optional so standard mode and existing automation work.
A retained task with a planning graph can introduce one graph-backed Feature; title similarity and
unknown task kinds never create guessed Feature attribution.

Graph fallback identity is scoped to its Project. The first explicit item binding replaces an
inferred fallback and becomes the stable default for later undeclared attempts. A later task may
still explicitly name another item, but it cannot silently retarget that graph default: the
adapter reports `graph_binding_conflict` and keeps the explicit task's accounting owner separate.
Replaying retained tasks therefore does not alternate the fallback between competing items.

Children receive a mode-aware briefing and report checklist progress, output references, exact
verification subjects and outstanding obligations in their existing progress/result artifacts.
The root attaches those facts to the item. A child must not obtain the machine credential merely
to write board state. Disabling the board does not invalidate a child's existing result contract.

Clawdfather records coordination as its own item, relating the work it coordinated through
`coordinates` links. Its handoff should name completed coordination, unresolved problems, proposed
improvements and the receiving owner. Debug work records cause and reusable findings; Features,
Refactors and Epics retain their checklist and milestone obligations. This first release supplies
the common record and evidence gates, not automatic extraction of those narratives from transcripts.

## Evidence and accounting

Only trusted broker task links allocate existing UsageLedger intervals. User-created Session/task
links are references, not allocation rules. Interval keys deduplicate the underlying measurements;
unknown parts stay unknown, unqualified partial counts stay lower bounds, and recorded costs retain
their unit and basis. Declared task phases group those same intervals without adding the task's
cumulative total again. Historical intervals without a declaration remain unclassified. Coverage
warnings such as regressed or unresolved sources qualify the observation: the measured counts are
retained, but are not advertised as a complete total or necessarily a lower bound.

The reader is bounded and reports truncation and observation time. It does not import all historical
Sessions, infer Features from titles, calculate phase percentages from elapsed time, or turn missing
costs into zero. Session activity spans are retained as declarations, not falsely precise billing.

The initial store accepts up to 200 Projects, 2,000 items and a 32 MiB file. Item histories retain
the newest 2,000 events and expose eviction counts; evidence capacity refuses new evidence rather
than writing a file that cannot reload. Snapshot summaries and selected detail may omit older
nested records, with explicit retained/omitted counts. Broker accounting links survive summary
projection. A snapshot has a 1,000,000-byte store budget and the enriched HTTP response has a
2 MiB budget, below Cloud's envelope limit. These limits are capacity protections, not paid tiers.
Use Refresh or re-enter the page to read updated broker facts; the initial UI does not stream live
board changes or silently refresh an unfinished form.

Verification summaries from old tasks remain summaries: they are not exact-tree acceptance.
Local roots may submit an attributed verification/finding attestation through `record_evidence`;
its subject and source ID must identify the receipt it reports. This is **root-attested evidence**,
not a claim that Clawdline ran the command independently. Code landing evidence comes only from
the existing broker-verified ancestry record. Independent review and exact-candidate acceptance
still follow [the verification workflow](verification-workflow.md) and [landing](landing.md).

## API and authority

`GET /v1/board?project=<opaque-id>&item=<opaque-id>` returns a versioned snapshot. `POST /v1/board`
accepts the closed commands in [the implementation contract](project-board-contract.md). Every
write carries `requestId` and `expectedRevision`. Conflicting revisions refuse rather than overwrite;
ambiguous delivery retries use the same request ID and body. A changed intent uses a new ID.

Authenticated devices can read. Ordinary writes require `send`; mode changes and artifact acceptance
require local administrative authority. The encrypted Cloud path uses its existing authenticated
write authority for these workspace preferences. Neither path can submit landing evidence. Only
the local machine credential can submit root verification/finding attestations. Cloud carries the
same closed `board` / `board-command` vocabulary into the same local service; no second store exists.
The initial Cloud UI requires one unambiguous connected machine and refuses ambiguous fleet routing.

The setting belongs to the board store, not a duplicate Config flag. A corrupt or future-schema
store is preserved and reported unavailable; it cannot introduce new workflow gates or silently
replace a user's history. No paid entitlement enforcement is active in this release.
