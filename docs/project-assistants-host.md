# Bots in the Session shell

This host connects the approved removable [Bots module](../web/console/src/project-assistants/SOURCE.md) to the local App and the Cloud account lifetime. It uses the frozen `project-assistants.v1` generated contract. It does not enable the source feature, start an adapter, or establish production Cloud/native acceptance.

## Ownership and entry

`SessionsPage.assistantEntry` is one slot outside the local/fleet list branch. `App` supplies the fixed orange blob `BotOrb` only after an authorized source has reported readable Bots access. A never-enabled source reporting `hidden` produces no entry and no list request. A source reporting `history_only` retains history/export access while content mutations remain disabled.

`pages/project-assistants.tsx` registers `drawer: false`. Direct page admission returns Sessions; typing a fragment cannot enable the feature. The normal entry is the Session orb, and the origin fragment remains unchanged throughout the nested Bots journey.

`useProjectAssistantsHost` saves the actual `BotOrb.onOpen(opener)` element, the origin hash, window scroll and Session scroll elements before hiding the origin. The existing `SessionsPage`, Session composer, recorder owner and `CopilotDockView` remain mounted. The host renders a fresh `Bots` instance for each orb entry, so selection always starts empty. Desktop retains roster/chat columns; phones show roster then chat.

Only `Bots.manageHistory=true` owns history: one roster entry and one chat entry. Selecting another Bot replaces a chat entry only when it belongs to the same view instance. Initial effect replay under React StrictMode does not push another roster entry. The native Back handler updates selection and its callback; Back from the roster closes the host. The host's explicit return consumes only those owned entries. App's Session keyboard and phone pop handlers yield while Bots owns the surface. Returning restores the original opener with `preventScroll` and the saved scroll positions.

## Account and source lifetimes

`new ProjectAssistantRegistry(accountKey, readerID)` owns one `createViewState()` instance and the source registry. CloudGate constructs it above the active-machine-keyed App and retains it across same-account token renewal and App remounts. Drafts, quotes and reading positions remain keyed by account, machine and Bot; active-machine selection is not their namespace.

`new ProjectAssistantSourceClient(grant, transport, currentGrant)` implements the sixteen frozen `SourceClient` methods with generated request/response types and caller `AbortSignal`. `AssistantGrant` contains the authenticated account namespace, host reader identity, immutable machine, authorization revision, observed API level, pairing, presence and operation permission. Registry entries also receive a fresh opaque instance revision on reauthorization, so cached metadata from a revoked source cannot reappear under a reused grant string.

Every operation checks its captured grant before transport, after the response, and after JSON decoding. Revocation aborts its lifetime signal immediately; late responses cannot cross into a new source/account. Logout, device revocation, machine access failures and machine forgetting invalidate the owning registry/source. Removing one source drops its badge and permission authority while the other clients and account view state survive. An unchanged roster does not manufacture another registry snapshot or metadata refresh.

Pairing/read access is never treated as permission to send. Content mutations require current machine operation permission plus fresh source `interactive` access and an explicit writable status. A failed status read removes content-write authority until a fresh source answer supplies it. Read cursors are replaced with the host's reader hint, but **the server must derive and validate the principal from authenticated context**; client account strings and `reader_id` are not authorization. Local uses `local`. A trustworthy same-principal mapping across Cloud devices remains a backend integration acceptance requirement.

The local host discovers source IDs from the source status response only after the existing health feed reports API level 56. CloudGate supplies all authorized account machine descriptors, including unavailable machines as typed source states. Each Cloud transport captures its machine; it does not use `install.ts`'s active reader or `captureMachineFetch()`.

## HTTP and encrypted envelopes

All paths below are under `/v1/project-assistants`. Bot path segments are encoded. GET fields use generated snake_case names. `machine_id` is the captured source; it is never substituted from a later active-machine selection.

| Source method | HTTP route | Cloud word suffix | Envelope fields |
| --- | --- | --- | --- |
| status | GET `/status` | status | empty |
| projects | GET `/v1/projects` | `projects` (existing word) | empty |
| list | GET base | list | complete `ProjectAssistantListRequest` |
| create | POST base | create | complete `ProjectAssistantCreateRequest` |
| get | GET `/{bot}` | get | complete Bot key |
| edit | PATCH `/{bot}` | edit | complete `ProjectAssistantEditRequest` |
| history | GET `/{bot}/messages` | history | key, conversation_id, before_seq, page_size |
| send | POST `/{bot}/messages` | send | complete `ProjectAssistantSendRequest` |
| stop | POST `/{bot}/stop` | stop | complete `ProjectAssistantStopRequest` |
| answer | POST `/{bot}/permissions/{offer}/answer` | answer | complete permission answer request |
| markRead | POST `/{bot}/read` | read | complete read request with host reader hint |
| observe | POST `/{bot}/observed` | observed | complete observe request |
| events | GET `/{bot}/events` | events | key, after_seq, page_size |
| forget | POST `/{bot}/memory/forget` | forget | complete forget request |
| saveMemory | POST `/{bot}/memory` | memory | complete memory write request |
| export | GET `/{bot}/export` | export | complete Bot key |

Except the existing `projects` word, suffixes are prefixed `project-assistants.`. Writes use `Idempotency-Key=command.idempotency_key`, and the encrypted action envelope ref uses that same key. The client leaves `content_hash` for the source to recompute. Reads call the captured connection's `_machineRequest`; actions use its `_machineRequestAs`. Existing transport deadlines remain owned by those APIs; the host adds no timeout, polling loop or automatic retry.

The runtime carried-word table and the source descriptor must both advertise the word. This change does not hand-edit the generated vocabulary. A stale console table returns `cloud_not_carried`, retained as transport evidence and mapped to typed unsupported presentation. API levels below 56, or an unknown API level, do not call nonexistent feature routes. Previously observed unavailable sources retain stale history/badge observations instead of becoming authoritative empty lists.

Generated `ProjectAssistantErrorResponse` bodies cross unchanged. Other transport errors retain their raw response in `AssistantTransportFailure.response`. A send whose transport result is unknown reports `pa_outcome_unknown`; no automatic replay is scheduled. Signal cancellation invalidates browser observation and does not claim to undo an already accepted remote action.

## Opening work results

For a Board-linked result, the registry reads the existing `/v1/work/v2/items/{id}` on its captured source (`work.v2.item`) and validates both the item ID and Project. The result carries a `current()` grant check. Local App opens the existing Board modal after leaving nested Bots history.

CloudGate performs that same captured read, rechecks account/source authority, switches the ordinary machine reader and supplies an `AssistantWorkIntent` to the resulting App. The new App's parent effect runs after the real Board page installed its modal listener. It opens the full item only on the matching reader and acknowledges the token once. An older work request cannot replace a later one. No event is fired into the discarded App, and no Bot is turned into a terminal, run or machine-wide credential.

Task-only results retain their real Task ID, state and landing information, without a fake Open button. A sanitized task-read/detail destination is explicitly pending with the integrating root. This host delivery does not close that PA-05 acceptance item.

## Verification and limits

The focused tests cover old/unknown APIs without requests, same-ID Bots on two machines, pre/post-await revocation, logout during decoding, immutable write keys, reader hints, history-only access, readable-but-not-writable sources, raw error preservation, unknown sends without replay, registry reauthorization, and source/Project-fenced Board reads.

The free browser harness bundles the **actual App, SessionsPage, Board and existing Copilot components**, with only its page-glob build mechanism adapted for esbuild. The HTML fixture injects synthetic transports and rejects every escaping `/v1` request. It uses React StrictMode, two sources with the same Bot IDs, and the existing voice singleton in a clearly synthetic connected state. It never opens a microphone, WebRTC connection, native provider, paid API or live daemon.

```sh
PLAYWRIGHT_MODULE=/path/to/existing/playwright/index.mjs \
BOTS_HOST_ARTIFACTS=/task-owned/evidence-directory \
node web/console/tools/project-assistants-host.e2e.mjs
```

The matrix is Chromium/WebKit × 320/390/1440 × English/Traditional Chinese. Assertions cover roster-first entry, desktop A→B using one chat entry, native/button Back, original focus and scroll, distinct drafts, active-machine App remount, IME without Session sends, unsupported routes without source calls, history-only/offline preservation, revocation, default-hidden access, and opening the correct source's real Board modal after remount. Voice assertions compare the same mounted controls across flips and inspect the real singleton's synthetic connection state; they are not real call acceptance.

No new resource bound is introduced. Metadata uses the existing list page size 100 and follows each opaque source cursor serially, rejecting repeated cursors. It does not poll. Existing module list/history/events sizes 100/100/256, the 3,000 ms event refresh and 3,600,000 ms separator remain the module owner's capacities.

Production Cloud vocabulary/decoders, trusted cross-device reader principals, real native adapter enforcement, physical phone gestures/keyboard, real voice/media coexistence, persistence/restart, task-only navigation, deployment, dev installation and release are separate integration evidence. A synthetic matrix or a bundle build does not prove them.

## Removal

Remove this page host, source/registry transport, App/CloudGate props and hooks, Session entry slot, route helpers/admission, fixture/tests, and the source-catalog domain registration. Remove only the corresponding root-owned catalog/semantic entries. Restore the two small Bots history integration changes with the module if the entire feature is removed. Keep the existing Session pages, recorder, Copilot dock, Board owner, Project memory and persisted Bot data. Feature disablement, source revocation, code removal and data deletion are different operations; this host never deletes data.
