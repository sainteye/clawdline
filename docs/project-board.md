# Project Board

### Optional AI reading summaries

Board mode and AI sharing are separate settings. Board mode defaults on; AI sharing defaults off,
including for existing stores. Settings names the configured provider before an administrator can
consent. The background worker may send bounded stored item titles, descriptions and outcome text
to that provider using the existing model account. It does not send full Session transcripts,
credential files or attachments. Item text itself may contain sensitive information; enable sharing
only when that content may leave the device. Revocation stops new admissions and rejects pending
results. Provider changes require consent for the new provider. Original text remains readable;
generated prose never creates verification or landing evidence.

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
inferred fallback and becomes the stable default for later undeclared attempts. Individual graph
nodes have separate stable item bindings: discovery, implementation and review may belong to
different Features without making the whole graph invalid. Only competing assignments of the same
node report `graph_node_binding_conflict`; replay never silently retargets that node. An Epic can
show related implementation items from other Projects as delivery lanes, while every attempt and
its accounting stay with its owning Project's item. A foreign Epic reference is resolved only when
the owning Project has one unambiguous related implementation item; ambiguity remains explicit.

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

### Planning documents and visible remaining work

Planning and decision documents are typed references beside, not inside, delivery artifacts. A
reference has purpose `plan`, `decision` or `reference`, a stable logical document id, an immutable
positive version and an explicit predecessor when a later version supersedes the current head.
The first version begins at 1. The reader labels current and earlier versions instead of replacing
history. A bounded detail keeps every current logical head before filling the remaining first-page
budget with newest earlier versions. A Load more control reads later retained versions in bounded
pages, so an omitted count never makes a current document unreachable. A generic artifact whose
kind happens to be `document` remains an output artifact; old
rows are never reinterpreted as an approved plan.

Every document reference uses the existing canonical hosted-reader address at
`https://app.clawdline.com/#document=1&…`. The fragment names the explicit Mac, Session, scope and
relative inert-text path without a credential or local filesystem root. Board snapshots carry only
this bounded metadata. The document body crosses the existing encrypted document read only after
the person selects its link. The hosted reader and Store apply the same UTF-8 byte limits (128 for
machine/Session identities and 512 for the relative path) and decode the fragment as form data
exactly once: `+` is a space, `%2B` is a literal plus, and encoded separators are validated only
after that one decode. Safe Unicode remains valid within those byte bounds.

Adding or superseding a reference is narrative maintenance: it does not increment the item's scope
revision, clear verification or landing evidence, reopen a terminal lifecycle, or count as artifact
acceptance. Actual objective/checklist/milestone changes and output artifacts retain their existing
invalidation and acceptance safeguards.

Selected detail includes a bounded `remainingWork` reading assembled from the item's unresolved
checklist, its existing Epic `parentId` children, and unresolved obligations. It is split into
**what we will do next** and **what you need to decide**. Only an obligation explicitly recorded
with `actorKind:"user"` enters the user-decision column; older rows remain visibly `unknown` rather
than guessing from an owner label. Required and optional checklist rows, canceled children,
unknown progress and trusted child progress remain distinct. This is a first-screen reading of
existing facts, not a nested task framework or a manual status control. Blocking work and explicit
user decisions receive first-screen priority. Each column has a bounded Load more reader for all
retained omitted rows; pagination applies only to the selected item and never expands an entire
Project response.

The Store materializer builds its parent-child index and per-item progress cache once per pass.
Selected detail walks only that item's children; it does not rescan the global 2,000-item capacity
for every item while holding the Store owner.

### Session relation and resume boundary

A Session relation selector accepts UUID spelling case-insensitively but canonicalizes it to the
repository's lowercase spelling before both reverse-index admission and lookup. Uppercase and
lowercase spellings therefore read the same stored conversation row, never two inferred rows.

Historical Board resume keeps one bounded pending/unknown fence in browser local storage, keyed by
machine, place, provider, conversation and action, together with the original request UUID. A page
reload reuses that UUID for both local `Idempotency-Key` and the encrypted Cloud request instead of
sending a second action. The fence clears only after unique matching live inventory is observed or
the server returns an explicit refusal; elapsed time, terminal-send success, or successful UI
handoff is not execution evidence and does not clear it. If storage is unavailable, admission fails
closed. This release does not claim protection against a person manually clearing browser storage
or atomic admission across concurrent tabs.

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

The assistant composes the report through its ordinary conversation, result and handoff workflow.
There is no model call in `GET /v1/board`, no new provider configuration, and no claim that existing
items all have generated completion reports. If drafting fails or no report has been written,
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

### AI reading titles and summaries

A separate background worker can prepare a short title, explanation, documented outcome and
explicit next step from an item's stored text. It uses the configured Clawdline language (the
system language when set to automatic) and the existing naming provider configuration. This is a
reading aid, not another completion report or a source of status evidence. The original objective,
report and technical references remain available in detail.

Generated text is persisted with its language, model, authoring time and source fingerprint. Reads
never wait for generation. The browser uses only a current variant in the reader's language;
missing, obsolete or other-language variants fall back to the original text. Board OFF stops new
generation and rejects in-flight results, including results from before an OFF/ON cycle. Updating
reading text never changes work scope, reopens a landed item or invalidates verification. A failed
summary must not hold up work, Session interaction or lifecycle progression.

The worker starts once with the app, on a utility queue. It admits one generation at a time,
at most 60 attempts in a rolling hour, through the existing naming process lane at very low
priority. Each tick asks for at most eight candidates. Failed item IDs are temporarily excluded
from that bounded snapshot so later history can progress; exclusions expire rather than abandoning
an item permanently. Session auto-naming's separate switch does not enable or disable Board prose.
Board AI sharing has its own provider-scoped, durable `board-reading-v1` consent, OFF for new
and migrated stores. The Settings control names OpenAI/Codex or Anthropic/Claude and the source
fields before accepting consent: stored title, description, type and documented outcome, not
transcript, credential files or attachments. Board ON alone never grants sharing; Board OFF,
revocation or a provider change stops admission. Revocation advances the presentation epoch so
an older in-flight result cannot commit even if permission is subsequently restored.
The configured naming assistant is retained, with its existing configured Codex model or Claude
Haiku model. Missing model output remains a readable original title, not a blocking spinner.
Structured turns use a bounded stdin file, capped in-memory stdout, launch-to-exit deadline and
an owned subprocess group; no model output file can grow without limit. Codex currently accepts
only the probed CLI 0.153.4 with a frozen `code_mode_only` model catalog and disabled code-mode
host. Its API may still advertise wrapper tools; forced execution is rejected by the handler,
and direct shell calls are unsupported. Unknown versions/catalog modes fall back to original
text, rather than assuming that a feature-list flag proves isolation.

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
journey where a lifecycle stage is known. Delivered-only and unknown use a labeled checkpoint,
not an unexplained row of unlit stage markers. Only the observed stage is highlighted; a historical landing does not manufacture missing
test receipts for earlier stages. Evidence detail retains current versus historical applicability.

`progress.group` separates active work, waiting work, unresolved history, completed records,
cancellations and coordination. Historical uncertainty is not a count of work still being done.
A successful read-only broker attempt explicitly settled as `nothing_to_land` may finish its
retained Task execution record without claiming that a Feature was merged. Such an attempt after
a real landing does not reopen that landed work. A current observed attempt or declared Session
span takes precedence over older discovery output; declared activity is labeled as a declaration.
Epic delivery lanes expose each related item's own Project, owner and progress without turning
aggregate activity into aggregate verification or landing.

Large Epic and Refactor items also expose a transparent percentage model. The recorded percentage
uses only required checklist rows and milestones from a fixed leaf scope. An Epic with child or
Program Plan nodes excludes its own summary rows and every in-scope container summary, so a parent
and its descendants are never counted twice; optional and `not_applicable` rows do not inflate the denominator. If any leaf has no declared
acceptance scope, the overall percentage is unavailable rather than guessed. Planning, implementation,
acceptance and release remain four separate measurements: beginning work does not claim acceptance,
and acceptance does not claim release. Until Board has an authoritative deployment receipt, release
is shown as unknown (`—`), even when every leaf is landed or settled; a Git landing is not a rollout.
An existing nested Epic is retained as a scope member, so an unscoped nested program makes the total
unavailable instead of silently disappearing from a 100% result.

That strict answer is no longer the only orientation on the card. Every non-canceled Epic and
Refactor with a recognizable Board lifecycle also carries a low-confidence `lifecycleEstimate`.
It maps the already-derived planning, queued, execution, correction, delivery, review, verification
and landing states into a bounded range; an Epic averages its current Program Plan nodes or explicit
members. Unknown members widen the range instead of disappearing. The browser labels this as a
stage estimate and may show it beside a separate exact acceptance percentage. It does not create
scope, pass a checklist row, verify work, prove landing or claim release. This fallback is available
without enabling an external AI provider, so a Board whose acceptance rows are still being authored
does not reduce every large card to the same “scope needed” placeholder.

When Board reading AI is explicitly enabled, its current presentation variant may include a separate
AI estimate with a bounded range, confidence label, named scope, basis, model and timestamp. A
missing exact acceptance denominator requires low confidence and an honestly partial scope; it no
longer forces the AI field to be null when lifecycle orientation exists. The
estimate is `narrative_only`: it never changes lifecycle, verification, landing, release, checklist or
milestone state. The browser labels recorded evidence and AI estimation separately and hides stale AI
variants after the underlying progress fingerprint changes.

Project cards use compact materialized summaries; selected item details are resolved only when
opened. Successful models do not expire merely because ten seconds elapsed. Durable changes,
source events and changes in the already-published Session Project inventory trigger bounded
background updates. Failed updates retain the last model and distinguish failure from a refresh
that is actually running. Collapsed history initially creates no card DOM; expansion renders it
in batches of 30, without changing authoritative Project totals.

The default Board is a human work view, not a transcript of every agent step. Explicitly created
Features, Epics, Tasks and Bugs remain the primary cards; an exact `parentId` renders a smaller
subtask card linked to its parent. Broker-created fallback rows for review, testing, correction,
coordination attempts and transferred provenance remain durable and searchable, but appear only in
the collapsed **Agent execution details** section. The distinction comes from stored source and
parent identities, never words in a title. “Removing” these rows from the ordinary Board therefore
means removing distraction from the default presentation, not erasing evidence needed for audits,
reconciliation or replay.

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
