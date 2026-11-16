# Bounded Bot Chat reads

Chat consumes the additive [read-view contract](project-assistants-read-views.md)
without changing the approved roster/thread presentation. The original Session
entry, avatars, composer, quote, queued footer, focus/history ownership and
voice owner remain in their existing modules. This is module implementation;
production Cloud, native execution, physical phones and removal acceptance
have independent owners and gates.

## Captured source port

`SourceClient.view(request: ProjectAssistantReadViewRequest, signal:
AbortSignal): Promise<ProjectAssistantReadViewResponse>` is method17. Only
this method requires API59. The original16 methods remain API58-compatible.
The captured HTTP adapter uses `GET /v1/project-assistants/{encodedBot}/view`
with exactly `machine_id`, `conversation_id`, `mode`, `source_epoch`,
`cursor`, `after_seq` and `page_size`. Empty strings and zeroes survive the
Cloud read envelope `project-assistants.view` unchanged. Extra/duplicate query
fields and unsupported modes are typed refusals.

Raw Source flags are validated as booleans before intersecting writable access
with the captured grant. Mandatory Bot metadata, consumed nested primitives and
closed enums are checked, and each event kind owns exactly its matching payload.
The validator reuses existing generated enum values and adds no external runtime
dependency. Malformed false-like or truthy strings cannot become a valid access
state through coercion. The captured new view transport applies the same checks
before updating its observed source state; the original16 methods are unchanged.

The adapter checks the same account, reader, machine and authorization revision
before the request, after transport and after JSON decoding. API59 is also
checked after decoding, including a downgrade with an otherwise unchanged
grant. Native capability metadata is never promoted into execution evidence.
An API58-only source can still supply roster/catalog metadata, but Chat stays
read-only with `pa_unsupported`; it never falls back to aggregate reads.

## Snapshot, refresh and history

Initial entry directly requests `snapshot` using the selected Bot summary's
captured conversation, empty epoch/cursor, `after_seq=0` and `page_size=100`.
The mandatory Bot/pending-turn/queued-message snapshot and source head are
validated before replacing any window/control state. Only that publication
adopts the epoch, starts replay after the exact machine frontier and releases
the existing account/Bot recovery latch. Draft, quote and held unknown effects
survive. Snapshot receipts are absent by contract, so absence cannot establish
failure or make an unknown effect safe to repeat.

Refresh reads one `events` page of up to256 rows. It uses the included events'
last sequence, including the unchanged `after_seq` on an empty page. A later
head frontier never skips unseen events. There is no automatic history drain
or full-journal replay. A subsequent refresh continues a partial event page.
The page head supplies current execution authority and the exact cross-generation
unknown-turn count. Any positive count blocks sending independently of the
bounded16 pending-turn summaries.

The producer may include user messages for running/waiting-permission turns
as well as accepted/queued turns in the mandatory pending-message collection.
Each message must match both MessageID and TurnID of one pending summary;
accepted/queued messages cannot be omitted. Only accepted/queued states enter
the queued footer; running/waiting messages remain in chat. The conversation's
retained memory revision may be older than the source head. That difference
does not relax the full execution fence or the current source memory barrier.

History sends the source's opaque cursor unchanged. Its live page heads do not
move replay. `mergeTimeline` retains source tuple ordering/IDs and reconciles
work by stable work identity plus `observed_event_seq`. A delayed older history
page cannot downgrade a newer work event. When an event arrives before its
historical anchor, the later source row can supply that anchor without replacing
the newer work fact. Persisted messages/work validate exact machine/Bot/full
Project/conversation scope and can retain an older execution fence.

`ChatRows` renders messages and work cards from that same merged timeline.
A work card stays after its admission message, before subsequent messages;
a live result changes the card in place. It is never collected into a separate
tail after all chat messages or sorted by completion time. Work resets author
grouping without inventing a date. Queued messages still belong only to their
footer, and the local sending bubble follows the persisted timeline.

Message and work IDs have separate source namespaces. Both rows can legitimately
contain the same ID string; duplicate checks use kind plus ID, and the original
IDs remain unchanged. Duplicate rows within either kind still fail. The root's
real Store Restore/Read positive control established this legal producer case;
the consumer regression was retained red before this correction and green after.

Invalid envelope mode, foreign outer/nested scope, old epoch, invalid counts,
non-progressing continuation, repeated cursors and mismatched complete payloads
are rejected as typed failures. No malformed empty response unlocks writes.
The UI reuses existing100/256 row,16 counted-turn and4096 cursor-byte bounds;
it adds no TTL, cache capacity, background retry or payload truncation.

## Recovery and effect fencing

Cursor expiry or a changed full fence clears execution actions, aborts pending
effects and enters recovery. A bounded snapshot can reconstruct the state.
If that attempt fails, the visible window/draft remain and background recovery
stops. Reopening the Bot, remounting the same host or returning to the visible
page makes one fresh bounded read attempt; the refresh action also retries it.
The account/Bot latch continues to block effects until a validated snapshot is
published. A failed read retains its actual error rather than inventing a
read-only refusal. No recovery read replays a message, work or permission.
Other Bots and new account namespaces do not inherit the latch.

Mutation preflight uses a fresh bounded snapshot, then checks the still-current
source, epoch, recovery latch and full fence immediately before the effect.
It does not adopt that snapshot's frontier without replacing the window. A
source that ignores cancellation still cannot publish or perform an effect
after authorization replacement, unmount or recovery. A newly discovered
unknown barrier also blocks Send after preflight.

The existing host-owned ViewState retains one unresolved intent and receipt
per account/Bot. It does not persist them to browser storage or maintain an
operation history. Returning/remounting or publishing a receipt-free snapshot
cannot replay that effect. Known source refusals remain separate from an
uncertain execution outcome.

Every active mutation has its own tracked AbortController until its promise
settles. Recovery, snapshot replacement and unmount abort all of them, except
that unmount no longer cancels a Send request already handed to the transport
(see "A send's receipt after leaving its Chat"); a later
mutation cannot replace the only cancellation handle of an earlier preflight.
The complete permission attempt runs through `withPermissionRecovery`, so
cursor expiry or a changed generation from the second detail await also
latches recovery before the error reaches the display handler. A successful same-fence recovery snapshot cannot
revive either an aborted Send preflight or a parallel settings preflight.

## Permission and trace collections

The head contains complete pending-offer and trace-category totals. Chat reads
one summary page and exposes manual more. It never treats the first100 summaries
as the complete collection or drains them automatically. A permission summary
contains no answer choices. Selecting its detail loads the complete stored
offer under the current full fence; only then do the original safe options
appear. Persistent/unsafe grants retain their prior denial, and answering
still has fresh preflight and expiry checks. No tool/question body is invented
when the stored offer does not contain one.

Trace categories are summaries until selected. An indivisible over-budget
detail preserves `pa_capacity_full` and the existing data/draft, with no
truncated successful text. Old-fence or revoked late details cannot restore
actions. The single added visible key is `projectAssistants.details`:
`View details` / `查看詳細內容`; root registers both shipped catalogs.

Manual memory inspection/export remain explicit archive-transfer operations.
They are never initial, refresh or recovery fallback paths. Their old complete
archive capacity limit is outside this delivery; large memory transfer is not
claimed solved.

## Verification and rerun

Focused Node checks cover the real pure helpers, rendered components, captured
source and existing ViewState. The child retained an original red transport
run (`old.view is not a function`), then the new captured API59 read passed.
The v3 focused UI run passed32 tests. The Cloud run passed26/27: the every-word
assertion correctly remained red until the backend-owned generated carry is
merged. It was not skipped or rewritten to accept a missing word. TypeScript
checking passed. Root must rerun the combined candidate after branch integration.

The v4 row-identity correction additionally passed all14 read-view helper tests
and the root's exact Node counterexample. It did not rerun the unchanged full
UI set or claim a browser/Store run by the child. Same-kind duplicate rejection
remains explicitly covered beside the legal cross-kind collision.

The normal-environment v5 eight-group run reached bounded history/frontier,
summary/detail, recovery and unknown-intent checks, then exposed an unwired
second-detail recovery catch on desktop and an incorrect test navigation step
on mobile. Those were failures, not completed UI acceptance. The v6 permission
boundary has a direct delayed-refusal behavior check: current-scope expiry/
generation locks recovery, revoked scopes do not lock a new scope, capacity
errors remain distinct, and no answer effect or automatic retry occurs. All9
component/fixture tests pass. The browser harness retains its outcome/count/
focus/voice checks and follows mobile Chat → Bots → Sessions before checking
opener focus. Root must rerun all eight normal groups and the original red
controls before claiming UI acceptance.

```sh
node --experimental-strip-types --test \
  web/console/src/project-assistants/read-views.test.ts \
  web/console/src/project-assistants/chat-items.test.ts \
  web/console/src/project-assistants/view-state.test.ts \
  web/console/src/project-assistants/components.test.mjs \
  web/console/src/cloud/project-assistants.test.ts
```

The real browser harness mounts the actual Chat and uses synthetic bounded
sources. It runs Chromium/WebKit ×390/1440 ×English/Traditional Chinese, writes
DOM/request evidence and screenshots, blocks live `/v1` traffic, and closes all
owned pages/browsers/temporary servers. It installs no dependency and invokes
no model or media. Set the two paths to an existing Playwright module and a
task-owned artifact directory:

```sh
PLAYWRIGHT_MODULE=<existing-playwright-index.mjs> \
BOTS_ARTIFACTS=<task-evidence-directory> \
node web/console/src/project-assistants/bots.e2e.mjs
```

The eight groups exercise600 genuine journal events with initial100 rows,
frontier602 followed by603/604, opaque history/scroll anchors, delayed old work,
101 permission/trace groups, manual detail/answer, oversized detail, failure
retention/manual recovery, account/Bot/remount isolation, queued16/unknown17,
ignored-abort reads/preflight, API58-only metadata, unknown intent, IME, quote,
focus and voice ownership. `BOTS_CHAT_SOURCE=<retained-seed-Chat.tsx>` runs the
same input against the original Chat without modifying the checkout; the
initial600-event assertion must go red. `BOTS_CATALOG_ONLY=1` retains the
existing catalog matrix.

`BOTS_READ_RACE=order` selects the work-card chronology regression: send a new
instruction, complete older work, then release delayed history with the older
work state. It checks visible row order and quoting through the real Chat in
Chromium/WebKit at both widths and languages. The pre-fix renderer fails because
the old card follows the new instruction. These fixtures make no provider calls.

The retained v1 Chat is a separate red control for two narrower races. Set
`BOTS_READ_RACE=permission` to test second-detail cursor/generation failure,
or `BOTS_READ_RACE=parallel` to hold Send/settings preflights, release a
same-fence recovery snapshot first, then release the ignored-abort old reads.
The latter also verifies cancellation on unmount. The default eight groups
include both races. For a representative red control only, `BOTS_ENGINE`,
`BOTS_WIDTH` and `BOTS_LANGUAGE` can select one existing group; an empty
selection is a failure. Root must record actual red and green browser outcomes.

The restricted child could not launch either engine: Chromium reported Mach
bootstrap permission denial; WebKit aborted during launch. These are capability
failures, not browser passes. Root owns normal-environment execution and the
red control before acceptance, plus real Server/Bridge/Cloud route integration.
No production Cloud networking, native provider, physical phone, release or
large-memory-transfer acceptance follows from these module checks. Product
model requests0; paid external API requests0; external API cost0.

## Removal boundary

All UI validation/merge helpers remain in the removable project-assistants
subtree. Remove its host imports, admission, entry slot, route coordination,
catalog registrations and the captured Cloud view route only with their owners.
Keep persisted Bot/Project memory and the original voice owner. Backend/store
read ports and shared generated contracts have their own ownership; deleting
this module alone does not prove full feature removal or rollback.


## Browser-driven Project label correction

A browser run against the real Go Server, gate, Host and Store showed that the
roster resolved the registered Project label while the chat header displayed
its internal Project ID. The host now passes the same source-authorized label,
keyed by machine and Project ID, into Chat. Missing catalog metadata retains
the existing ID fallback. This changes presentation only; the captured scope,
fences and request identities still use their original IDs.

## Connected source acceptance

`TestProjectAssistantJointCapturedSourceAndRealReadViews` runs the actual
TypeScript SourceClient, captured relay and strict readView helper against the
real in-process Go Bridge, Router, Server, Host and SQL Store. The six View
modes use real source bodies. Only the original sixteen API58 compatibility
methods use the explicitly separate request-capture router; View cannot enter
that router.

The targeted run retained 43 hashed Bridge replies: 27 actual source responses
and 16 compatibility captures. It covered all six successful modes, twelve
single-field rejection controls derived from valid Go bodies, machine event
frontiers, late old-generation work, signed summary/detail cursors, typed
cursor/scope/capacity failures, post-pack revocation, delayed grant changes,
read-only and disabled history, API59 admission and API58 compatibility.
A missing conversation retains the Store's existing `pa_not_found` contract;
a separate mismatched stored identity proves `pa_scope_denied` crosses the
client unchanged. Source content is never repaired into a successful page, and
View performs no Export fallback or provider initialization.

Run this connected check with Node24 available:

```sh
go test -count=1 -v \
  -run=^TestProjectAssistantJointCapturedSourceAndRealReadViews$ \
  ./internal/transport/http
```

`CLAWDLINE_PA_JOINT_ARTIFACTS` optionally retains raw Go response digests and the
Node report in an empty task-owned evidence directory. The test installs no
runtime or dependency. Its synthetic data and in-process transport do not
establish production Cloud encryption, physical phone behavior, native provider
admission, full feature removal or Codync parity. Those acceptance boundaries
remain independent.


## One stop action while its receipt is pending

Stopping active and queued work owns its waiting state for the complete fresh
snapshot preflight and source request. A synchronous latch also rejects a second
activation before React renders the disabled button. The action uses the current
turn from that preflight, retains one command identity for its attempt, presents
the actual stop receipt and refreshes the bounded conversation. It never retries
the stop automatically or turns an accepted stop into a claim that an external
effect is known. Reading and quoting stored history do not start a provider;
only an explicit message does.


## Live local roster at API71

Local sources that expose `canRoster` and `subscribeRoster` now use the existing
source-owned SSE protocol. The initial snapshot becomes visible only when
complete. Each fully validated frame replaces absolute facts by machine/Bot
identity and durable revision; neither hello nor EOF acknowledges progress.
Disconnect keeps the last complete rows visibly stale, and reconnect uses their
applied cursor. Incomplete snapshots restart fresh. The existing 3-second
interval schedules serial reconnection, never a List poll. Source revocation,
scope denial or unpairing clears retained rows and stops the subscription.

The registered 1,000-row client budget includes removal revisions. Overflow
requests a fresh snapshot, reclaiming removal history; an oversized fresh
snapshot stops with a capacity error. Hidden Bots retain their authorized
metadata for the existing Unhide control. Deleted Bots keep key-only removal
markers, so an older replay cannot restore them. Capacity diagnostics report
browser occupancy as unknown rather than inventing a daemon measurement.

The upstream reference remains Codync
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`: subscribe before catch-up, durable
replacement and independent working/permission/error/unread facts. The approved
BR-06 SVG outlines and palette remain; animation and reduced-motion behavior
are unchanged by this connection work. Cloud and older sources keep snapshot
listing. The encrypted Cloud recorder still refuses the local SSE URL.

`TestProjectAssistantRosterLiveHTTP` composes the writable Store, its Service
wake-up bus and query-only ViewStore, and exercises a persistent real HTTP SSE
connection. Its opt-in Chromium harness mounts the actual SourceClient and Bots
component. English desktop (1280×900) and Traditional Chinese mobile dimensions
(390×844) verify all five states, activity/writing/completion, permission priority,
exact duplicate replay, reader-specific unread, cursor reconnection with stale
rows, actual Unhide, deletion and revocation. Both runs record one navigation,
zero List calls, at most one subscription per source and zero page errors.
No provider or model runs. These are local fixture and browser proofs, not
production Cloud or physical-phone acceptance.

```sh
CLAWDLINE_ROSTER_LIVE_E2E_DIR=/absolute/task-owned/evidence-directory \
PLAYWRIGHT_MODULE=/absolute/path/to/existing/playwright/index.mjs \
PLAYWRIGHT_BROWSERS_PATH=/absolute/path/to/existing/browser-cache \
go test ./internal/transport/http -run '^TestProjectAssistantRosterLiveHTTP$' -count=1
```

## Roster state after a restart continuation

A restart continuation is admitted by a notice message with its own turn and message anchor. The roster previously chose only user-message admissions, while the bounded chat read view already selected terminal turns by their actual admission anchors. A completed continuation could therefore appear completed in chat while the roster continued to show the acknowledged unknown failure of the interrupted turn.

The roster now selects the most recent user or notice admission whose ID matches its turn's `MessageID`, excluding joined turns. Active execution still takes priority. Later log notices from an old turn do not become new admissions. A current unknown continuation remains an error; selecting a later completed continuation does not alter earlier turns, unknown receipts, permissions, execution policy or retry behavior. For restart continuations, this follows Codync's current-status roster semantics while retaining Clawdline's explicit uncertainty records. Routine notice admissions follow Clawdline's existing chat projection; Codync routine-lane parity has not been established.

Focused counterexamples exercise the original failed/acknowledged turn followed by a successful notice continuation, later log notices, a current unknown continuation and a newer human turn. A real SQLite commit/read-view/roster case confirms the two projections agree and the original unknown receipt stays unknown. Production-state replay evidence is retained privately; no conversation content is included in this document.

## Cloud snapshot roster refresh

Nonlocal sources retain their existing status and List transport. Returning from
chat reads the roster immediately. While the roster is visible, the existing
conversation refresh cadence schedules the next read after completion. Refresh
requests are serialized and coalesced; a slow read is allowed to finish instead
of being repeatedly aborted. Backgrounding or pagehide stops the timer, and
foreground/pageshow catches up immediately. Unmount and query or authorization
changes cancel the old reader; terminal authority refusals clear retained rows.
Read-only paired sources use the same read loop. Routine refreshes keep the
existing rows visible without a loading message.

A refresh starts with a fresh keyset cursor and sequentially rereads only the
number of pages already loaded for that source and query. It replaces those
rows atomically, including deletion, and retains the new continuation cursor.
This follows ListForReader's machine/Bot ordering and avoids retaining deleted
rows through a merge or losing loaded later pages through a first-page refresh.
Selection, reader identity, draft storage and scroll containers are unchanged.
Local API71 SSE continues to own its own connection without List polling.

`snapshot-roster.test.ts` exercises loaded coverage, deletion, slow single-flight
reads, late cancellation/authority changes and foreground/pagehide teardown.
`TestProjectAssistantRosterCloudHTTP` optionally runs the actual Bots component
and nonlocal ProjectAssistantSourceClient through real Go Host HTTP and SQLite,
with synthetic Store mutations and no provider invocation. Its browser harness
uses the configured Clawdline browser and records response bodies and operations.
This is a snapshot transport fixture: it does not establish encrypted production
Cloud relay behavior, physical-phone acceptance or native model execution.

```sh
CLAWDLINE_ROSTER_CLOUD_E2E_DIR=/absolute/task-owned/evidence-directory \
BOTS_BROWSER_CLAWDLINE_DIR=/absolute/configured/browser-broker-directory \
go test ./internal/transport/http -run '^TestProjectAssistantRosterCloudHTTP$' -count=1
```

## A send's receipt after leaving its Chat

On 2026-11-16 a production Cloud Bot accepted, ran and answered one message
while the person had already returned to the roster. Reopening the chat still
held that message as unknown with a disabled composer: unmount had aborted the
Send request, so the client lost a receipt the source had already written.

The Send request now belongs to the host ViewState lifetime once its bytes may
cross. Chat journals the intent, opens the request and passes it a signal that
recovery, snapshot replacement and unwritable-source events still abort while
the Chat is shown; leaving the Chat does not. Preflight reads and uploads are
before the effect and are still cancelled by leaving, returning the draft.
`HeldOutcomes.settle` applies the answer by the command's idempotency key only:
a newer held send, a set-aside send's place, and the person's new draft and
quote are never replaced. `endTransport` then tells any Chat for that
account/Bot, including one opened after leaving, which shows the send as
sending until then and adopts the journal's outcome once. The SourceClient
still turns a revoked or changed grant into outcome unknown, so a late answer
after revocation never reattaches. The open-request record is memory only: a
full reload or a lost connection keeps the journaled send genuinely unknown,
with the explicit check and set-aside; nothing is resent.

This follows Codync's ownership, where the send task and its local
`clientNonce` entry live in the per-computer `BotStore`
(`apps/ios/Kit/Sources/CodyncUI/Store/BotStore+Sending.swift`, pinned tree
9f67eb09) and "an eventual acknowledgement must not erase a newer draft". It
reuses Clawdline's existing receipt and journal instead of Codync's nonce
protocol.

`TestProjectAssistantSendLifetimeBrowser` runs the actual Bots UI and Cloud
SourceClient in a real browser against a real TCP Go Host, SQLite and a
scripted provider. It holds the send's HTTP answer after admission, leaves and
reopens the chat, then releases it; it also leaves during preflight, reloads
during a held answer, and revokes after leaving. Before the fix the reopened
chat stayed held with a locked composer; after it, four messages produce four
provider sends and replies and nothing is resent.
`TestProjectAssistantSendLifetimeSourceKeepsAdmittedTurnWhenClientLeaves`
covers the source half without a browser.

```sh
CLAWDLINE_SEND_LIFETIME_E2E_DIR=/absolute/task-owned/evidence-directory \
BOTS_BROWSER_CLAWDLINE_DIR=/absolute/configured/browser-broker-directory \
go test ./internal/transport/http -run '^TestProjectAssistantSendLifetime' -count=1
```
