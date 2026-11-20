# Bot conversation inspectors and group identities

The conversation header has one Bot identity button that opens its information inspector
with Details, Library,
Memory and Settings tabs. Visited editors stay mounted across tab changes, so a draft name or
memory note is not lost when checking another tab. Closing the inspector returns focus to its
opener. The tab list supports Left, Right, Home and End; Escape closes the inspector, including
after a save has removed focus from a temporarily disabled control. On narrow screens the
inspector occupies the conversation area and the underlying conversation is hidden from focus.

Details holds the conversation's work: background threads, development servers, the quick
actions and routines, as in Codync's `DetailsPanel`. Settings holds the whole Bot setup as
Codync's `BotSettingsPanel` does: titled groups that are always open — Instructions, Agent
(model, login profile and fallbacks), Name and appearance, Workspace, Permissions and
notifications, Application skills and connectors, Network access, Saved permission rules, the
template and export actions, and Remove from the list. On/off choices are switches. The
Agent row and fallbacks still open in place because they hold sub-editors and model discovery.

One mounted editor still owns the draft. It stays mounted, hidden, while Details is shown, so
switching tabs keeps an unsaved change. Unlike Codync, which autosaves, changes are saved
explicitly: a bar pinned to the bottom of the inspector appears only when the draft differs and
offers Cancel and Save. The save keeps its expected version, stale and unknown-outcome
protection; a successful save resets the editor to the returned version, and Cancel discards
the draft. Autosave would turn every keystroke into a version-checked write whose unknown
outcome locks the form, so it was deliberately not copied.

Library embeds the existing conversation-owned file library without its own title or close
button; the inspector already provides both. Memory follows Codync's `MemoryCard`: compact
scope tabs, then one outlined card with the layer's usage, its actions and its entries. Entries
sharing a source are listed inside the entry being read instead of a separate concept picker.
The person's own tools (scope note, remember a short fact, forget selected sources) are rows of
one card below it. Memory still uses the existing
memory-view, save and forget operations, revision checks and unknown-command retention. The
inspector does not introduce a new store, credential, permission or API version. All of these
surfaces remain bound to the selected account, source, Bot and conversation.

Group conversations retain their history polling, sequence merge, pagination cursor, fixed
pending command and explicit unknown-outcome parking. Member messages now show the existing
BR-06 avatar and their Bot's color and name; person messages align to the right. The member
inspector and composer target picker select the actual `reply_to_author` identity. Replying
focuses the composer. Its compact input grows with the text, sends on Enter, and keeps Shift/Alt+Enter for a newline without acting on an IME composition. The target picker and send button stay in the same row; edit, stop and delete are in the secondary actions menu. Group
selection keys the room component by its complete machine/Bot identity.

The room contract contains author and reply-to-author identities, but no quoted message ID or
quoted message text. The UI therefore identifies the reply recipient without inventing a
quotation. Message-level quotations and direct navigation from a room member to its own chat
need a separately owned contract/navigation change. The latter seam belongs to the roster
owner; this implementation does not fork the roster or its organization state.

Approval cards use a compact 360px maximum reading width and show the action, source computer/Project, full offered detail and file diffs.
The four native option kinds keep their exact IDs and display the adapter's label as well as
the localized action. A summary still only opens a scoped detail read. Answering still repeats
the complete live preflight and checks expiry, the current execution fence and offered option.
The card prevents overlapping answers and displays a local failure when the parent rejects an
answer. Unknown outcomes continue to park through the existing conversation journal. On phone widths the jump capsule is hidden while approval cards are present so it cannot cover a native choice. Full details and all choices remain reachable by scrolling.

The layout and interaction study used the [Codync homepage](https://www.codync.dev/) and
[public source](https://github.com/leepokai/Codync/tree/9f67eb09084db61b3cca9c84a11bc03df9de29b7),
including `DetailsPanel.tsx`, `ChatSidePanel.tsx`, `PermissionCard.tsx` and the room model.
The pinned source has an additional Computer tab; the observed homepage demo had four tabs.
Its Library explicitly reported that it was unavailable in the web demo. These observations
are not evidence about its production backend. Clawdline retains its own approved BR-06
line avatars and existing data ownership. Existing Codync adaptations and licensing remain
documented in `third_party/codync`.

## Verification

### Roster search and partial source failures

The roster searches Bot names and the displayed Project label. Snapshot APIs filter Bot names;
when a Project label matches, the viewer reads that source's ordinary unfiltered pages and
filters by the Project label locally. Load more retains the matching request's cursor and page
coverage. Name searches continue to use the source's name filter, including results beyond the
initial page. Project labels remain scoped to their source machine.

An empty search on a readable machine is an empty result, even when another machine refuses
access. The failed machine and its typed refusal remain in the expandable connection details.
Selecting only the failed machine still shows its source failure. The search-clear control has
its own localized name rather than the reply-cancellation label.

`roster-search.e2e.mjs` runs the real Bots components against paginated, synthetic snapshot
sources with an unpaired peer. It checks Project and Bot names, pagination, empty results,
selected-machine failures and all-source failures in English desktop and Traditional Chinese
mobile viewports. Set `BOTS_ARTIFACTS` to an owned directory and run it with Node; it uses an
existing headless Chromium and installs nothing. It makes no live API or model requests.

`TestProjectAssistantUIAlignmentBrowser` runs the production components and the production
SourceClient against authenticated, task-owned loopback HTTP services and SQLite stores. Its
native provider is synthetic and makes no model or vendor API calls. Set
`CLAWDLINE_UI_ALIGNMENT_BROWSER_DIR` to an artifact directory and `CHROME` to an existing test
browser, then run `go test ./internal/transport/http -run '^TestProjectAssistantUIAlignmentBrowser$' -count=1`.
It does not install a browser, start the person's daemon or bind its production port.

`BOTS_BASELINE` may name an extracted baseline checkout for before/after captures. The optional
baseline bundle resolves unchanged dependencies through the task's prepared workspace. Fault
injection at the fixture proxy supplies expired/stale detail and unknown answers; successful
settings, memory, room sends and native answers cross the actual HTTP/Host/SQLite boundary.
Local browser evidence does not establish Cloud deployment, installed-native behavior, public
release acceptance or human visual approval.

## Conversation presentation and message actions

Single-chat text uses separate user and assistant bubbles. One More button opens the
message's quick reactions, Reply and Copy in a viewport-clamped portal. The same entry is
visible on touch layouts, appears on desktop hover or keyboard focus, and opens from
Enter/Space on the message or its context menu. It does not add an open-state background
to the message. Popup focus uses preventScroll; Escape returns to the trigger. Inspector
navigation dismisses only the transient menu, retaining any unresolved reaction intent.

Reaction summaries have reserved row space, so opening a picker, adding or removing one
reaction does not change the bubble or scroll geometry. A selected emoji closes the
picker immediately and shows a marked pending preview. The existing typed, same-ID desired
state command still owns persistence. A validated delivered receipt confirms that preview
at the submitted actor revision plus one, until the same or newer actor revision arrives
from the source. Older poll results cannot flash back over a confirmed local operation.
A known refusal restores the previous state; unknown results retain the original intent,
remain visibly uncertain and require explicit same-ID recovery. No background replay,
new API operation, source permission or model request is introduced.

Codync 2.47 source at `9f67eb09084db61b3cca9c84a11bc03df9de29b7` separates the summary and
message actions in `ChatRows.tsx` (lines258-324), anchors a portal in `Menu.tsx` (lines183-227),
and replaces its immediate local reaction with the Host response in `bot-store.ts`
(lines242-259). These are source observations. Existing native screenshots do not establish
its CSS viewport; untested Codync touch or keyboard behavior and cross-product response
speed are not claimed. Clawdline retains its explicit pending/unknown receipt distinction.

`TestProjectAssistantMessageActionsBrowser` uses production Chat and SourceClient with real
HTTP/Host/SQLite, synthetic long history and scripted provider work. Its desktop,390px and
320px cases record bubble/scroll coordinates, per-frame pending/confirmed feedback, the
source transaction response and delayed HTTP acknowledgement separately. The proxy can
withhold event visibility to test receipt projection without changing the polling cadence.
It records original WebM video and checks keyboard focus, touch input and a known refusal.
`BOTS_ACTIONS_BASELINE` loads the exact four old presentation sources for the same reproducer.
The existing reaction browser test separately covers persistence, same-ID unknown recovery
and revoked late replies. These isolated checks do not replace formal Cloud or physical
phone acceptance, installed-native qualification or human visual review.

### The message menu must cross the actual window layer

The standalone Chat geometry fixture did not cover the production window's
stacking context. In the real `useProjectAssistantsHost`, `.pa-window` is fixed
at layer 1000. A body-mounted menu at layer 60 existed in the accessibility tree
but was painted behind that window. The exact production CSS reproduced eight
occluded controls and zero reaction writes in an isolated HTTP/Host/SQLite run.
The popup now sits above the window and below the existing output overlay.

`TestProjectAssistantMessageMenuHostBrowser` mounts the real host, registry,
Bots and Chat against a synthetic authenticated source. It checks the actual
`elementFromPoint` result at all six emoji and both text actions, not just DOM
presence or layout visibility. Its Chromium/WebKit desktop and narrow-phone
matrix also exercises one reaction add/remove, unchanged chat geometry, Escape
focus, outside-pointer passage and dismissal when entering details. The host's
position and layer are not replaced by fixture CSS. Source and synthetic browser
evidence remain separate from production Cloud visual acceptance; no external
model request is made by this regression.
