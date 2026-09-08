# Project Board

Project Board combines durable work items with existing dispatch, Session, worktree, usage and
landing records. It is **enabled by default and currently free**. The entitlement returned by the
API is `free_preview`; this is not a subscription check or a promise about future pricing.

Settings → Enable Project Board changes the persisted machine-wide workflow mode. Turning it off
keeps history readable and restores the worktree-oriented Project view, Usage and Ledger entries.
It does not cancel tasks, close Sessions, delete data or disable the ordinary permission, claims,
independent-review, landing or machine-resource protections. Existing work can finish through its
original task protocol. Re-enabling does not manufacture usage boundaries for the suspended time.
It resumes reconciliation from the available broker facts. A Board-store failure is not a mode
change: the Projects entry uses the connection's available standard Project/worktree readers with
a warning, while retaining the setting. A connection without those readers reports unavailable.

## A work item is not an execution attempt

The existing Projects list is the entry point. Open a Project to see its work overview, visual
progress, unresolved work and collapsed delivery history. There is no browser New button, edit
form or manual status selector: the owning assistant creates the objective through conversation
and records the supporting facts. The six execution types are
Feature, Refactor, Task, Bug, Coordination and Epic. An item may link several tasks, Sessions and
worktrees; task success is delivery, not item closure. The lifecycle is:

`backlog → planning → ready → execution → verified → integrated → closed`

The sidebar contains one Projects entry, not a second directory of every historical board record.
The catalog uses the existing durable Project/start-point identity, pixel icon and accent color;
temporary worktrees and retained task-only paths remain records, not new visible Projects. Opening
a row immediately shows that Project's name, icon and path while its work loads. A loading response
never means zero work. Failed or stale reads retain their last observed data with an explicit warning.

Reads follow the presentation hierarchy: catalog, selected Project summaries, then selected item
detail. Catalog totals belong to the materialized Project summary, not to the subset of item rows
currently loaded by the browser. A truncated item view cannot establish that a Project is empty.
A missing or unavailable selected Project is not an empty Project: the view retains the selected
identity and reports unavailable instead of presenting zero work as a measured result.

Cancellation is separate from completion. New scope or failed evidence reopens affected work.
Lifecycle advancement is automatic when the recorded facts satisfy its gates; it is not a job
for the person reading the board. Work beyond planning needs an
owner. Verification needs evidence, not only checked boxes. Code integration needs a broker
landing record; a URL or an agent's statement that it merged is insufficient. A non-code Task
can instead use an artifact accepted by an administrative user. Open blocking findings prevent
verification; unsettled obligations, milestones and pending handoffs prevent closure. An Epic's
children must be delivered before its integration. A handoff proposes a receiver, and only that
authenticated receiver accepts it; it is not a shortcut to completion.

The durable board record survives task retention and terminal closure. It does not replace
`work_state` (what a Session is doing) or the broker task state (what an attempt reached).
Worktrees remain linked execution records in item detail, not the unit of work. The standalone
worktree-oriented Project view remains available in standard mode.

### Different work, different records

| Execution type | What the reader needs to understand |
| --- | --- |
| Feature | Objective, automatic delivery progress, current checklist and related Sessions/worktrees |
| Refactor | Architecture change, checklist, milestones and delivery progress |
| Task | Measured token cost and concrete output, such as a document, URL or deployment |
| Bug (Debug / Fixed) | Fix and cost, `typeDetails.rootCause`, `typeDetails.lessons`, and reference documents |
| Coordination (Moderator / Coordinate) | Time- or handoff-bounded coordination, `coordinates` relations, `typeDetails.outcomes`, `typeDetails.difficulties` and `typeDetails.improvements` |
| Epic (Creation) | Large objective, child work, checklist, milestones and aggregate delivery |

Coordination has **no delivery lifecycle**. It is excluded from completed/open delivery counts and
has no lifecycle progress bar. A coordination record describes a bounded period or handoff, not an
eternally incomplete Feature. Missing narratives are visible gaps, not reasons to hold a status.
Assistants record specialized fields through the ordinary authorized create/update command. A
Session's work type follows its explicit item association; its token activity phase remains separate.

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

An ungraphed retained attempt without an explicit item receives a stable **Task execution record**,
not a guessed Feature. Its identity is canonical Project plus task id, not its title. Repeated
imports reuse it. A later explicit item binding moves its task-scoped records to that item rather
than leaving duplicate delivery evidence on two cards. The ordinary item limit applies; capacity
refusals remain visible in source coverage instead of silently dropping historical work.

Graph fallback identity is scoped to its Project. The first explicit item binding replaces an
inferred fallback and becomes the stable default for later undeclared attempts. A later task may
still explicitly name another item, but it cannot silently retarget that graph default: the
adapter reports `graph_binding_conflict` and keeps the explicit task's accounting owner separate.
Replaying retained tasks therefore does not alternate the fallback between competing items.

Children receive a mode-aware briefing and report checklist progress, output references, exact
verification subjects and outstanding obligations in their existing progress/result artifacts.
The root attaches those facts to the item, not a sequence of user-facing status changes. The
store reconciles the lifecycle after relevant factual commands and broker observations. A child must not obtain the machine credential merely
to write board state. Disabling the board does not invalidate a child's existing result contract.

Clawdfather records coordination as its own item, relating the work it coordinated through
`coordinates` links. Its handoff should name completed coordination, unresolved problems, proposed
improvements and the receiving owner. Debug work records cause and reusable findings; Features,
Refactors and Epics retain their checklist and milestone obligations. This first release supplies
the common record and evidence gates, not automatic extraction of those narratives from transcripts.

## Completion reports

An item's short `summary` remains its objective or description. Its owning root can add a separate
completion report after ordinary work reaches `closed` or its current scope has an authoritative
landed projection. This includes retained historical work whose later broker landing is sound even
when older process records are missing; it excludes genuinely active, unlanded delivery. The report
answers five reader questions: the objective, delivered outcomes, what verification and landing do
(and do not) establish, remaining work, and lessons.

Coordination has no closure lifecycle. Its period / handoff report instead requires one typed,
named boundary: either a finite increasing `time_interval`, or a `handoff` id that resolves to a
handoff source on that same item. Recording or failing to record either kind of report never changes,
holds or advances item status.

The assistant composes the prose through its ordinary conversation, result and handoff workflow.
There is no model call in `GET /v1/board`, no new provider configuration, and no claim that existing
items already have autonomously generated reports. If drafting fails or no report has been written,
the item still advances from its independent evidence and the reader says that the report is absent.
Turning Board mode off keeps earlier reports readable and starts no report workflow.

The owning local root records one consolidated report with the same CAS and idempotency rules as
every Board command. A useful template is:

```json
{
  "operation": "record_report",
  "requestId": "report-<stable-intent-id>",
  "expectedRevision": 42,
  "itemId": "<opaque-item-id>",
  "objective": "What this item set out to accomplish, without copying the card summary.",
  "deliveredOutcomes": "Concrete user-visible or operational results; distinguish child delivery from integration.",
  "verificationLanding": "Exact receipt subjects and limits. Say plainly when work is delivered-only or landing is unknown.",
  "remainingWork": "Unknown facts, residual obligations, deferred scope and their named owners.",
  "lessons": "Reusable decisions, pitfalls and what a future attempt should know.",
  "authorship": "assistant",
  "model": "<model-used-to-compose-this-prose>",
  "sourceReferences": [
    {"kind":"task", "targetId":"<task-id>", "label":"delivery result"},
    {"kind":"artifact", "targetId":"<item-artifact-id>", "label":"report artifact"},
    {"kind":"evidence", "targetId":"<receipt-source-id>", "label":"exact-tree acceptance"},
    {"kind":"external", "targetId":"<missing-or-legacy-source>", "label":"unresolved historical source"}
  ]
}
```

For a Coordination period, the same command additionally carries:

```json
{"reportBoundary":{"kind":"time_interval","label":"September integration","startedAt":1788796800,"endedAt":1789401600}}
```

The alternative is
`{"kind":"handoff","label":"Root to receiver","handoffId":"<same-item-handoff-id>"}`
and its `sourceReferences` must include that handoff id.

The server supplies actor, authored time, report version, current scope revision, source resolution
and `authority:"narrative_only"`; callers cannot submit those fields. Source relationship is frozen
authoring-time provenance, not recomputed current truth: wire rows expose an explicit relationship
such as `same_item_at_authorship` plus `resolvedAt`. Older stored rows without that optional epoch
use the report's `authoredAt`, so a later inferred-to-explicit rebind can never relabel old provenance
as a present-day same-item fact. Task, artifact, evidence and
handoff references resolve only inside the same item, item references only inside the same Project,
and everything else remains explicitly `unresolved`. URLs accept only HTTP(S). These references
help a reader retrace the narrative but cannot mint verification or landing authority.

Replacing a report appends a version rather than erasing prose. Up to 16 versions are retained; a
seventeenth is refused without discarding the first. Catalog and ordinary detail reads keep
superseded versions metadata-only. Expanding one version performs the bounded lazy read
`GET /v1/board?project=<id>&item=<id>&report=<report-id>` through the same authenticated read lane;
unknown selectors and item/Project mismatches are typed refusals. A failed or late expansion stays
inside that version reader and cannot clear the current item or current report.

Reopened or newly scoped work keeps its latest report but labels it `historical_needs_update` until
that exact scope is again report-eligible and receives a new report. Rebinding also preserves the
old report body while qualifying its authoring-time relationships. This same command and provenance
model is the intended seam for a later, separately measured low-cost historical pilot; the pilot
does not receive a second store or lifecycle shortcut.

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
The visible board refreshes every 15 seconds after its previous read finishes. Reads do not overlap;
hidden documents and pages do not poll. A failed refresh retains the last observed records with an
explicit stale warning. Expanded record sections and reading position survive refresh. Refresh is
also available on demand. Disabling the board stops its automatic refresh and workflow advancement.

### Progress is a projection of evidence, not a completion guess

Each item exposes a bounded `progress` object beside its durable lifecycle. It distinguishes
planning, queued, execution, review/testing, correction, verified, landed, delivered-only, blocked,
canceled and unknown. The browser uses that projection for its status marker and four-stage visual
journey. Only the observed stage is highlighted; a historical landing does not manufacture missing
test receipts for earlier stages. Evidence detail retains current versus historical applicability.

Historical delivery and current exact-scope acceptance answer different questions. A broker-verified
historical landing may be displayed as landed without forging exact-tree verification. An old
landing must not hide newer active work, failed proof or reopened scope. Child success alone is
shown as delivered, awaiting confirmation, never as landed. Cancellation is kept distinct and is
excluded from landed totals. Evidence gaps remain visible rather than mass-closing old cards.

Verification summaries from old tasks remain summaries: they are not exact-tree acceptance.
Local roots may submit an attributed verification/finding attestation through `record_evidence`;
its subject and source ID must identify the receipt it reports. This is **root-attested evidence**,
not a claim that Clawdline ran the command independently. Code landing evidence comes only from
the existing broker-verified ancestry record. Independent review and exact-candidate acceptance
still follow [the verification workflow](verification-workflow.md) and [landing](landing.md).

## API and authority

`GET /v1/board?project=<opaque-id>&item=<opaque-id>` returns a versioned snapshot; adding the opaque
`report=<report-id>` selector retrieves one retained version body. `POST /v1/board`
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
