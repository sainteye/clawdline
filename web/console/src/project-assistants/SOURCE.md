# Bots module integration and source record

This removable React module implements the approved BR-01 through BR-06 presentation against `docs/project-assistants-handoff.json` (`project-assistants.v1`). It does not enable the feature, add a router entry, install an adapter, start a model, mount a recorder, or contact upstream services.

## Public entry points

- `Bots(props: BotsProps)` from `Bots.tsx` receives `sources: readonly SourceClient[]`, `onClose: () => void`, and optional `language: string` (English by default), `accountKey: string`, `readerID: string`, `viewState: ViewState`, `returnFocus: HTMLElement | null`, `onOpenWork: (ProjectAssistantWorkLink) => void`, `manageHistory: boolean` (false by default), and `onSelectionChange: (ProjectAssistantBotKey | null) => void`.
- `BotOrb(props: BotOrbProps)` from `BotOrb.tsx` receives `onOpen: (opener?: HTMLElement) => void` and optional `language`, `unread`, and `pendingPermissions`. The host sums counts only from currently readable sources. The global entry always uses the approved blob and `#e39567`; it never derives appearance from a selected Bot.
- `SourceClient` is defined only in `client.ts`, with the exact frozen readonly `machineID`, `authorizationRevision`, sixteen methods, generated request/response types and final `AbortSignal` argument. It is injected. No feature HTTP transport or global active-machine fetch is created here.
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

Run the `.test.ts` and `components.test.mjs` tests through the existing console runner. They exercise observable drafts, fences, replay/grouping, queue transitions, composition, source grant invalidation and rendered controls. The e2e harness additionally builds an in-memory bundle, starts one temporary server bound to `127.0.0.1`, rejects all `/v1` calls, exercises Chromium and WebKit at 320, 390 and 1440 pixels, records BR screenshots and requests in both languages, checks 150-message pagination and replay, offline/disabled history and opener restoration, and closes every page/browser/server in `finally`.

```sh
PLAYWRIGHT_MODULE=/absolute/path/to/existing/playwright/index.mjs \
  BOTS_ARTIFACTS=/absolute/task-owned/evidence-directory \
  node web/console/src/project-assistants/bots.e2e.mjs
```

No dependency is installed by the harness. Its fixture has no real provider, Cloud account, personal Session, microphone or model. Real native read-only enforcement, native memory isolation and resume remain typed unavailable until independently proved by adapter owners. Actual metadata persistence across daemon restart, cross-device monotonic reads, production Cloud grants, physical iPhone keyboard/gestures, runtime work dispatch and production voice coexistence require the integrated candidate and their owning acceptance steps.

Capacity owner must register `chat-items.ts:ChatSeparatorGapMs` (3,600,000 ms) and `Chat.tsx:EventRefreshMs` (3,000 ms). List/history/event request sizes reuse the frozen 100/100/256 contract limits. Polling is serialized, only scheduled for a visible online writable source, aborted on hide/destination/grant change/unmount, and stops on failure until manual refresh.

Authenticated persisted messages and work links belong to the exact source machine, Bot, full Project (machine, project ID, canonical ID and kind) and conversation. Their original execution binding and generation remain visible after replacement. Both the event envelope and its nested durable payload must match that scope; initial history, earlier history and recovery snapshots apply the same filter. Permission offers, turn control and traces still require the complete current execution fence, including binding and generation. This UI distinction does not relax native callbacks or Broker work ownership, and does not introduce a model-controlled native trust entry point.

An expired event cursor latches a separate recovery-required state, aborts in-flight mutations and obtains the existing authenticated transactional `export` snapshot. The latch belongs to the existing host-owned ViewState under the account/Bot draft identity: leaving Chat, switching Bots, or remounting the host with that same ViewState cannot clear it. It is not a new registry or cross-account persistent storage. A later fresh GET, history page or successful event page cannot unlock the UI or mutation handlers. Mutations recheck the latch after their own GET, including when a source ignores cancellation. Only successful publication of a verified same-scope snapshot clears the latch. Snapshot failures preserve history, drafts and unknown receipts, prohibit writes, and stop polling and visibility-triggered recovery until explicit retry. The snapshot's conversation/binding/generation and own event frontier remain authoritative; no later GET is substituted into that snapshot. Root verified the store's aggregate/events/receipts share one SQLite read transaction; integrated concurrent-write acceptance remains with the store/root. Unknown send outcomes retain draft and receipt and are never automatically retried.

Two known limits remain outside this bounded UI correction: initialization still reads events from zero after its first 100-message history page, so journal replay can expand the initial window; recovery still requires the full Export, which can exceed the Cloud plaintext capacity. A capacity failure remains locked and does not manufacture a cursor, truncate facts, enlarge a cap or replay a send. The root owns a subsequent bounded, consistent read projection. Synthetic Chromium/WebKit acceptance at 390 and 1440 pixels proves the classified durable facts and recovery lock; it does not establish complete PA-04/PA-05, production Cloud or large-archive recovery acceptance. Use `BOTS_TARGETED=1` for this four-case, English-only regression subset of the existing browser harness.


Removal consists of deleting this subtree and the explicit fixture HTML, then removing only root-owned imports, page admission, entry slot, route coordination and catalog/table registrations. Do not delete persisted Bot data, Project memory or existing voice owners. No production integration import is introduced by this module commit.


## Source-owned Project configuration catalog

The form retains the approved compact settings and original avatar system.
The pinned upstream `views/bots/NewBotView.tsx` and
`views/bots/BotEditorView.tsx` separate the selected computer, Agent picker and
Project destination; this adaptation uses source-registered IDs instead of a
filesystem picker. No arbitrary directory, provider-ready fixture or global
active-machine lookup is introduced.

The sixteen-method SourceClient now takes
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
