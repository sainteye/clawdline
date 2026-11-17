# Bot conversation inspectors and group identities

The conversation now has a directly accessible information inspector with Details, Library,
Memory and Settings tabs. Visited editors stay mounted across tab changes, so a draft name or
memory note is not lost when checking another tab. Closing the inspector returns focus to its
opener. The tab list supports Left, Right, Home and End; Escape closes the inspector, including
after a save has removed focus from a temporarily disabled control. On narrow screens the
inspector occupies the conversation area and the underlying conversation is hidden from focus.

Details reads the selected Bot's standing instructions, Agent, Project and source computer.
Its edit and routine controls lead to the existing scoped implementations. Settings keeps the
existing login profiles, native runtime options, extensions, fallbacks, permission modes,
notifications, rules, management and network controls. A successful save resets that editor to
the returned version; a stale or unknown save keeps the form and its existing protection.
Cancelling settings discards that form explicitly. Changing tabs does not cancel it.

Library embeds the existing conversation-owned file library. Memory still uses the existing
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
