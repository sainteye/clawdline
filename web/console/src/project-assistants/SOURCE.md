# Bots module integration and source record

This removable React module implements the approved BR-01 through BR-06 presentation against `docs/project-assistants-handoff.json` (`project-assistants.v1`). It does not enable the feature, add a router entry, install an adapter, start a model, mount a recorder, or contact upstream services.

## Public entry points

- `Bots(props: BotsProps)` from `Bots.tsx` receives `sources: readonly SourceClient[]`, `onClose: () => void`, and optional `language: string` (English by default), `accountKey: string`, `readerID: string`, `viewState: ViewState`, `returnFocus: HTMLElement | null`, `onOpenWork: (ProjectAssistantWorkLink) => void`, `manageHistory: boolean` (false by default), and `onSelectionChange: (ProjectAssistantBotKey | null) => void`.
- `BotOrb(props: BotOrbProps)` from `BotOrb.tsx` receives `onOpen: (opener?: HTMLElement) => void` and optional `language`, `unread`, and `pendingPermissions`. The host sums counts only from currently readable sources. The global entry always uses the approved blob and `#e39567`; it never derives appearance from a selected Bot.
- `SourceClient` is defined only in `client.ts`, with the exact frozen readonly `machineID`, `authorizationRevision`, seventeen methods, generated request/response types and final `AbortSignal` argument. It is injected. No feature HTTP transport or global active-machine fetch is created here.
- `createViewState(storage?)` from `view-state.ts` retains staged drafts, reply IDs, reading positions and recovery-required scopes. Supply one host-owned instance to preserve these through host remounts. Drafts use account namespace, machine ID and Bot ID, and flush on leaving or hiding instead of per keystroke. Without an explicit account key the source grant namespace is used, so renewed authorization does not reuse another account's drafts. The host should always supply its authenticated account namespace and reader principal.
- `words(language)` and the exported `en` / `zhHant` tables from `words.ts` provide English and Taiwan Traditional Chinese together. Register every key as `projectAssistants.<key>` in both shipped catalogs and register this table with the source catalog checker. No shared catalog was edited by PA-04.

`Bots` always mounts with no selected Bot. Phones show roster, then chat, then roster; desktop shows the roster beside an empty selection prompt until selection. The module can own its nested history with `manageHistory=true`. If the shell owns history instead, leave it false and use the host's route coordination; do not assume `onSelectionChange` itself supplies native Back handling. The host must retain the original Sessions surface, origin route, focus and list scroll, and unmount the feature on `onClose`. Save the actual opener supplied by `BotOrb.onOpen` before hiding Sessions and pass it as `returnFocus`; hiding the origin blurs it before the feature effect can inspect `document.activeElement`. The module restores that exact opener's focus without scrolling. An existing feature instance should be remounted for each orb entry.

Root owns feature admission (default off, hidden if never enabled), Session entry positioning, all-source account lifetime, Cloud permission routes, transport, work destinations, production catalogs, shared capacity registrations, and the existing recorder / call dock. There is no Bots microphone. Root must verify real voice controls remain usable in the production shell; the fixture uses a synthetic control and opens no media stream.

## Reuse and deliberate adaptations

Upstream: Codync `234219cef2d3f991f0f7225ba89e3c99cdd34177`, Apache-2.0. Preserve the full original [LICENSE](../../../../third_party/codync/LICENSE), [NOTICE](../../../../third_party/codync/NOTICE) and [derivation record](../../../../third_party/codync/DERIVATION.md), already included by the foundation. Copyright 2026 Po Kai Lee and Codync contributors.

| Upstream path under `apps/desktop/src/renderer/` | Retained behavior | Adaptation |
| --- | --- | --- |
| `views/thread/composer-keyboard.ts`, `composerEnter` | Enter sends, Shift/Alt Enter newline, repeat/composition/keyCode 229 ignored for send | Adds the explicit composition ref required across browser IME implementations. Pure behavior tests cover both confirmation and subsequent send. |
| `views/thread/Composer.tsx`, `DestinationComposer` | Destination keyed editor, drafts outside editor, quoted reply, wrapping mirror | Drops Electron IPC, attachments, slash/mention, plugins, calls and dictation. CSS grid mirror measures without collapsing the live textarea. No new line-count bound. |
| `views/thread/chat-items.ts`, `buildChat` | Stable row identity, author grouping, one-hour separators | Uses source message IDs and generated message kinds; deduplicates replay by ID and sequence order. Group exchanges, calls and upstream hooks are outside v1. History has no arrival animation, so replay never animates a duplicate. Calendar formatting uses the supplied interface language. |
| `views/thread/queued-messages.ts`, `waitingMessages` / `outsideQueue` | A queued user message appears on exactly one surface | Uses authoritative accepted/queued turn and message IDs. Running transitions restore the message to chat. No unsupported steer/unqueue buttons or upstream host version assumption. |
| `views/thread/PermissionCard.tsx`, `sortedOptions` | Allow before deny, once before persistent options; pending/outcome separation | Generated offer fence is retained. Persistent choices are visibly unavailable in v1. Fresh source and full execution fence are rechecked before answering; expiry is checked again at submission. No permission is selected by focus. |
| `views/thread/ThreadView.tsx`, `Transcript` | Earlier-page scroll-height preservation, follow/latest decision, explicit jump to latest | Uses injected per-source history and stored reading position. No upstream global store, timer constants, group or voice container. |
| `views/thread/trace-turns.ts` | Trace grouped by turn, separate from chat / unread | Typed source trace is shown only when supplied. No invented reasoning or generic narration conversion. |
| `views/BotRoster.tsx`, `views/BotRow.tsx`, `Preview` | Semantic selected row button, composite source identity, permission/activity/latest preview priority | Flat authorized roster, activity sorting and independent opaque source cursors replace upstream store sections, dragging, pins and updater dependencies. |
| `components/Avatar.tsx`, `characterPath` | Eight geometric silhouettes and paired eyes | Exact reviewed BR-06 outline paths and palette from the approved design `build.py`. No canvas cache, dots, provider marks, role icon coupling or arbitrary SVG. Unknown appearance renders a safe fallback without rewriting stored data. |

The approved previews were all visually inspected before implementation. Actual module screenshots and interactions are synthetic fixture evidence, not production API / Cloud acceptance. The fixture intentionally contains same-name / same-ID Bots on two machines.

## Verification and remaining integration

Run the `.test.ts` and `components.test.mjs` tests through the existing console runner. They exercise observable drafts, fences, replay/grouping, queue transitions, composition, source grant invalidation and rendered controls. The e2e harness additionally builds an in-memory bundle, starts one temporary server bound to `127.0.0.1`, rejects all `/v1` calls, exercises Chromium and WebKit at 390 and 1440 pixels, records BR screenshots and requests in both languages, checks bounded600-event entry, frontier continuation, live-history races, offline/disabled history and opener restoration, and closes every page/browser/server in `finally`.

```sh
PLAYWRIGHT_MODULE=/absolute/path/to/existing/playwright/index.mjs \
  BOTS_ARTIFACTS=/absolute/task-owned/evidence-directory \
  node web/console/src/project-assistants/bots.e2e.mjs
```

No dependency is installed by the harness. Its fixture has no real provider, Cloud account, personal Session, microphone or model. Real native read-only enforcement, native memory isolation and resume remain typed unavailable until independently proved by adapter owners. Actual metadata persistence across daemon restart, cross-device monotonic reads, production Cloud grants, physical iPhone keyboard/gestures, runtime work dispatch and production voice coexistence require the integrated candidate and their owning acceptance steps.

Capacity owner must register `chat-items.ts:ChatSeparatorGapMs` (3,600,000 ms) and `Chat.tsx:EventRefreshMs` (3,000 ms). List/history/view request sizes reuse the frozen100/100/256 limits,16 pending turns and4096 cursor bytes. Polling is serialized, only scheduled for a visible online writable source, aborted on hide/destination/grant change/unmount, and stops on failure until manual refresh.

Authenticated persisted messages and work links belong to the exact source machine, Bot, full Project (machine, project ID, canonical ID and kind) and conversation. Their original execution binding and generation remain visible after replacement. Both the event envelope and its nested durable payload must match that scope; initial history, earlier history and recovery snapshots apply the same filter. Permission offers, turn control and traces still require the complete current execution fence, including binding and generation. This UI distinction does not relax native callbacks or Broker work ownership, and does not introduce a model-controlled native trust entry point.

Chat entry now calls the additive API59 `view(snapshot)` directly with the
selected summary's captured conversation. The consistent mandatory snapshot
replaces the window/control state and adopts the source epoch and machine
`head.event_frontier`. It never initializes via GET/history, replays events
from zero or falls back to Export. Refresh uses one bounded `view(events)`
page and advances only to the last included event. History uses opaque live
cursors; work facts merge by identity and observation sequence, including
facts from older executions. Page heads do not move replay past unseen events.

An expired cursor or changed execution fence latches account/Bot recovery,
clears current actions and attempts a fresh bounded snapshot. Snapshot failure
preserves the existing window and draft and stops background recovery. Returning
to the Bot or remounting the same host does not release that latch: only an
explicit retry that publishes a validated bounded snapshot can do so. A fresh
mutation preflight uses this same bounded snapshot port, checks current source,
epoch and full fence after the await, and never advances the displayed replay
cursor. A snapshot with no receipt information cannot resolve an unknown effect.
The existing ViewState holds one unresolved intent/receipt for each account/Bot,
without a new registry, storage format, TTL or automatic effect replay.

Permissions and traces load one summary page, show the exact source totals and
require manual more. Only a complete current-fence stored permission detail
renders answer options; unsafe and persistent options keep the existing denial.
Trace text is fetched only after selection. Capacity refusals retain source
error codes and never produce truncated successful details. See
[the bounded UI record](../../../../docs/project-assistants-read-ui.md) for
validation, race coverage, rerun commands and the remaining integration gates.
Memory inspection/explicit export remain manual transfer operations, with the
pre-existing complete-archive capacity limit; they are not recovery paths.



Removal consists of deleting this subtree and the explicit fixture HTML, then removing only root-owned imports, page admission, entry slot, route coordination and catalog/table registrations. Do not delete persisted Bot data, Project memory or existing voice owners. No production integration import is introduced by this module commit.


## Source-owned Project configuration catalog

The form retains the approved compact settings and original avatar system.
The pinned upstream `views/bots/NewBotView.tsx` and
`views/bots/BotEditorView.tsx` separate the selected computer, Agent picker and
Project destination; this adaptation uses source-registered IDs instead of a
filesystem picker. No arbitrary directory, provider-ready fixture or global
active-machine lookup is introduced.

The SourceClient retains the API58 catalog method
`projects(ProjectAssistantProjectsRequest, AbortSignal)` and returns the
frozen `ProjectAssistantProjectsResponse`. The host's API58 transport owns the
independent `/v1/project-assistant-targets` route and current captured grant.
`catalog.agents` controls configuration choices only; status capabilities
continue to describe native execution and memory verification separately.
The form does not filter Agent choices through native capability readiness.

The first page is requested at the existing 100-row size. Load more is manual,
keeps Project IDs distinct even when labels match, and verifies the same source,
revision and agent set without repeated IDs or cursors. Initial or later errors
keep creation disabled until an explicit successful retry. Retrying an expired
cursor starts a new first page. Switching source clears only the Project
selection and catalog; name, appearance, instructions and policy remain.
Publication is fenced across initial reads, page reads, source replacement,
revocation, close and create preflight. Unknown create settings and idempotency
remain frozen through manual retry; no effect is automatically replayed.
Roster labels read one Project page only and keep ID fallback for unseen rows.

The only added visible key is `projectAssistants.projectsLoading`, with
`Loading Projects…` / `正在載入 Projects…`. Root owns the two shipped catalogs
and semantic/transparency generation. The module changes neither the original
orb and eight avatar outlines, nor Chat/history/recovery/focus/voice ownership.

Run `BOTS_CATALOG_ONLY=1` with the documented browser harness environment for
Chromium/WebKit at 390/1440 and English/Traditional Chinese. Its synthetic
sources expose unavailable native capabilities, 101 Project IDs, same-name
choices, delayed pages, cursor/revision/machine failures, read-only sources
and unknown create outcomes. The scripted first Send for a newly created Bot
returns `adapter_not_found` and invokes no native provider. These real browser
clicks prove form behavior against injected clients, not source persistence,
Server mounting or production Cloud admission. Launch failures are capability
failures, never passing browser acceptance. All pages and the temporary server
close in `finally`; the harness installs no dependencies or external service.


The create preflight status is itself wrapped in `authorized`, so its await
cannot cross a change to the current source object or authorization revision
and then call create. A second effect-entry check precedes create; the source
transport retains its independent current-grant check. The fixture can hold
status, replace/revoke authority before a host render, then release a captured
fresh interactive response. The browser assertion requires zero create calls;
selected refs and cancellation alone cannot pass that controlled scenario.
For a red control, set `BOTS_CREATE_SOURCE` to the retained pre-fix BotCreate
artifact; the harness substitutes only that local module in its in-memory
bundle and never modifies the checkout. Omit it for the delivered candidate.


The frozen candidate passed the eight normal-environment catalog browser
cases. Both retained pre-effect-fence controls failed as intended with one
create call after authority replacement/revocation; the candidate admitted
zero calls. Full request logs and screenshots were retained by the owning
root. The separate `TestProjectAssistantCatalogBridgeThroughServerAndRegistry`
passes normal and race runs against the actual local Bridge/Router/Server,
device gate and PlaceRegistry, including typed cursor/revocation errors and
zero provider initialization. These results do not establish production Cloud
signatures/networking, persistence across devices or physical phone acceptance.


## Additive bounded read port

`view(ProjectAssistantReadViewRequest, AbortSignal)` is method17. Only this
method requires API59. The existing16 methods, metadata catalog and roster
retain API58 support. Unsupported view is a typed, read-only Chat failure;
there is no old-API fallback. The captured Cloud adapter maps exactly
`GET /v1/project-assistants/{encodedBot}/view` and seven query fields to
`project-assistants.view` (read), retaining empty strings and zeroes.
Account, reader, machine, grant revision and API59 are rechecked after transport
and JSON decoding. Metadata does not upgrade native capability readiness.

The added visible key is `projectAssistants.details`: `View details` /
`查看詳細內容`. Root owns the shipped catalogs and generated registrations.
The independent pure read validator rejects malformed mode/payload, scope,
epoch, counts and cursor progression. Source tuple IDs/anchors remain intact;
an older history observation cannot replace a newer work event.

The v3 child ran32 focused helper/component/ViewState tests successfully.
Its captured-source suite passed26 of27; the existing every-word assertion
correctly awaited the backend-owned generated carry addition. The child did
not copy or weaken that assertion. TypeScript checking passed. Chromium and
WebKit were denied at launch by the restricted child environment; these are
not passing UI results. Root owns normal-environment execution of the eight
real browser groups and the original-Chat red control before module acceptance.
No product model, media, paid external API, native provider, production Cloud
or physical phone acceptance is claimed. All product model requests:0.

The default browser harness now exercises the bounded Chat matrix. Set
`BOTS_CATALOG_ONLY=1` for the preserved catalog matrix. For the original-Chat
red control, set `BOTS_CHAT_SOURCE` to the retained seed Chat source; the
harness overrides only that module in memory and never rewrites the checkout.
The first600-journal-event assertion must reject the original implementation.

The next controlled race revision retains the prior v1 Chat separately.
`BOTS_READ_RACE=permission` verifies that second-detail cursor expiry or a
changed generation latches recovery before answering. `BOTS_READ_RACE=parallel`
holds both Send and settings preflights, releases a same-fence recovery first,
then releases ignored-abort old reads and requires zero effects. It also checks
all controllers on unmount. The implementation tracks every active mutation
until its promise settles and aborts all on recovery/snapshot/unmount. Both
races are in the default eight-group matrix; the existing-engine/width/language
environment selectors are for focused red controls only. Root owns real browser
execution of v1 versus this revision; child launch refusal is still not a pass.


The v3 read boundary validates raw Source booleans/enums/capability metadata
and the consumed mandatory Bot, message, work, permission, trace and event
fields before grant composition or publication. Event kind must own exactly
its declared payload. It reuses existing generated enum values and adds no
external runtime dependency. Truthy-string Source and missing Bot counterexamples
were retained red before the correction. The same new view validation runs in
the captured transport; the original16 methods remain unchanged.

Producer-compatible snapshots may include user messages for every counted
pending turn, including running/waiting-permission states. Messages must match
both summary MessageID and TurnID; accepted/queued messages remain mandatory.
Only accepted/queued states appear in the queued footer. A conversation's
retained memory revision may precede the source head, while execution identity
still requires the complete current fence. Both legal inputs have direct tests.

The v4 identity correction preserves another legal producer shape: message and
work rows may share the same ID string. Duplicate checks use kind plus ID,
without changing the source IDs or relaxing duplicate rejection within a kind.
Root supplied a real Store Restore/Read positive control and an exact consumer
Node counterexample; the child retained its own before/after runs and passed
all14 read-view helper tests. Chat, fixture, harness, transport and ViewState
remained byte-identical to v3 for this correction.

Normal v5 browser execution subsequently exposed that the second permission
detail error still reached display without invoking recovery. The v6 actual
permission attempt is wrapped in `withPermissionRecovery`; a late expiry or
changed generation locks the still-current scope and rethrows the original
error, while a stale account/source cannot lock another scope. A direct delayed
operation test checks latch/effects/error identity and no retry; all9 component
tests pass. The mobile browser route now goes Chat → Bots → Sessions before
the existing opener-focus assertion. These correct the observed failed paths;
they do not turn the failed v5 matrix into a passing UI receipt. Normal eight
groups and the retained red controls remain the root's acceptance gate.

## Human memory controls

`MemoryPanel.tsx` exposes the existing human `saveMemory` and source-aware
`forget` operations through the Bot's Memory panel. People enter literal notes
and select saved text rather than copying provenance identifiers. Server-owned
manual provenance, current Project/reader authority and expected revisions stay
unchanged. The panel lists only active entries and reports the retained forgotten
source count; history and external Project/native memory remain separate.

Each uncertain memory mutation retains its exact command, entry identity and
expected revision for an explicit retry. A different mutation cannot replace an
unresolved one. Confirmed writes clear their intent before a display refresh,
so a failed refresh cannot duplicate a saved note. Closing the panel retains an
unconfirmed note/selection until it is reopened in the same Bot chat. These
metadata operations never construct an ACP provider or run a model.

Navigation uses distinct `pagehide` pause and `pageshow` resume handlers. A page
may still report visible while leaving; that state must never start a new
event read during teardown. Draft flushing and owned read cancellation remain
shared with ordinary visibility changes.
