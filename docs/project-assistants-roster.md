# Bot roster preferences

The roster mixes Bots and group conversations inside their actual Projects.
Pinned conversations stay above Projects. A group retains its source-owned group
identity; sharing an ID with a Bot never shares its preferences or room routing.

The default row uses its full width for the avatar, name, preview and necessary
state. It has no per-row more button or reserved action column. Hold any Bot or
group row to lift it, then drag among its siblings. A lifted copy follows the
pointer while neighbours slide into the provisional order. Release inside the
roster to save; Escape, pointer/touch cancellation, window blur or release
outside restores the saved order. A press moving before the hold remains a normal
vertical scroll, and a short press still opens the original conversation.
Reduced-motion preferences disable neighbour animation.

The top-level List options menu still opens explicit reorder mode, which exposes
pin buttons and keyboard handles. Done returns to reading. Right-click or
Shift+F10 on a Bot row opens that Bot's context menu; on a group row it opens
reorder mode at the row. Alt+Up/Down (and Alt+Home/End) moves
the focused row without a mouse; focused handles retain plain Up/Down and Home/End.
Direct dragging stays inside the same Project and pin band. Explicit controls can
pin/unpin a row without changing its Project or granting source access. Preferences
are written only on completed drops or explicit keyboard/pin actions. A storage
update from another tab cancels an in-flight drag instead of overwriting it.

The List options menu also chooses an order, saved with these preferences.
`default` keeps the Project sections. `unread` lifts Bots that need the
person's answer, then unread Bots, then working Bots into sections above them.
`machine` replaces the Project sections with one section per machine and turns
off Alt-key reordering outside reorder mode.

Group rows compose actual members' avatars, count members beyond the registered
preview bound, and show the returned last speaker and message. Missing member
metadata remains unknown. The optional list projection supplies `last_entry` and
`room`; the roster never fetches each room's history for a preview. Older sources
show Project/member metadata instead. Only a real message timestamp participates
in recent ordering; a missing timestamp keeps its input slot. Group modification
time and room-local sequence numbers do not impersonate message time.

Selecting a group clears the previous Bot selection and opens that exact
source's room across the chat area. An explicit member action resolves the
member's complete machine/Bot key against readable roster items from that
source. It opens the member's single chat while keeping the room mounted and
inactive. The visible return-to-group control restores that room's draft,
quoted message, reading position and member control. Closing the room restores
the original group row's focus and the list's scroll position. A room preview
or a stored preference never grants read or execution access.

Group create/edit forms use the browser's modal layer. The member panel remains
mounted beneath an edit form, but cannot cover or intercept its controls. Cancel
and Escape close only that form and return focus to its actual opener, including
touch activation. Unsaved form changes are discarded without a group write.

Preferences are stored in this browser's local storage under the authenticated
account namespace, with machine/Bot, typed machine/group and machine/Project identities as keys. They
survive closing Bots, switching Sessions and reloading this browser. Other tabs
observe storage changes. They do not synchronize across devices or browser
profiles, with one exception: when a Bot's source supports roster actions, its
pin is saved to the source and the source-owned `sidebar_pinned` value overrides
the local pin set. Source filtering and pagination retain unseen preferences; later Bots
remain visible. Without customization, Project and Bot order follow the roster's
existing activity order. A real Project named `pinned` has its own identity.

Unreadable or unavailable storage shows a notice and leaves the roster usable.
A failed save retains the current view's changes and does not claim persistence.
Account changes remount the roster with that account's preferences. Source
revocation continues to remove unauthorized rows through the existing host.

## Verification

`roster-conversations.test.ts`, `roster-state.test.ts` and `roster-groups.test.ts`
exercise real-identity mixing, summary fallbacks, timestamp ordering, avatar counts,
round trips, account
and machine isolation, filtered moves, missing identities, storage failures,
Project ordering and the Project-name collision. `roster-order.e2e.mjs` bundles
the real roster with synthetic summaries and serves only on loopback. It checks
mouse dragging, keyboard ordering, pin/unpin, reload, Project order, account
switches, cross-Project refusal and corrupt storage in Chromium and WebKit,
in English at desktop size and Traditional Chinese at phone size. Delayed-frame
injection checks that entering reorder mode cannot steal a subsequently chosen
row focus. Chromium's
phone case also sends actual touch input through CDP. It makes no provider calls. `roster-mixed.e2e.mjs` additionally retains identical
records and CSS viewports for the base and candidate, counts full visible rows,
and checks group entry/return, pin persistence, default controls and absence of
per-row history reads. Its local fixture timings do not measure production or
Codync latency.

`TestGroupMemberNavigationBrowser` runs the actual `Bots` parent, roster,
room and single chat over authenticated HTTP, the closed Cloud command decoder
and SQLite. Its in-process provider seeds only synthetic history; it launches
no native assistant or external model. The browser matrix covers local and
closed-Cloud transport on desktop and phone viewports, a read-only source and
an API87 source without the API88 group-context capability. It checks
Bot-to-room-to-member-to-room-to-list navigation, exact source identity,
retained room state, focus, scroll position, no history fanout for previews,
and no frontend prompt sends. The Cloud envelope is simulated; these checks
do not establish production Cloud delivery or physical-device acceptance.
The same matrix opens group settings from the member panel and header menu,
checks actual coordinate hits and accessible-button activation, Tab focus,
Enter, Escape, create-form cancellation and absence of group mutations. The
phone reproducer taps the observed Cancel point `(32, 42)`; in the previous
layout it hit the overlaid member heading instead of the button beneath it.

With already installed Playwright browsers, run it from the repository root:

```sh
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
PLAYWRIGHT_BROWSERS_PATH=/absolute/path/to/installed/browser-cache \
CLAWDLINE_GROUP_NAV_ARTIFACTS=/absolute/path/to/evidence \
go test ./internal/transport/http -run '^TestGroupMemberNavigationBrowser$' -count=1
```

The explicit browser-cache path is necessary because HTTP tests isolate HOME.
`BOTS_NAV_BASELINE=<commit>` substitutes only the previous parent/roster/group
implementation to retain a failing selection reproducer. The normal matrix
uses the current source and does not allow the former narrow room placement.

Run the browser check with an existing Playwright installation:

```sh
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
BOTS_ARTIFACTS=/absolute/path/to/evidence \
node web/console/src/project-assistants/roster-order.e2e.mjs
```

The local `0822b8f19` development build's roster called `botIdentity` without
importing it. Opening a nonempty roster threw `ReferenceError` and blanked the
page. Later source repairs were not present in that installed build. Roster
preferences in that implementation also returned empty arrays, so its section
markup did not establish working pinning or persistence. The corrected source
imports the identity helper and verifies actual operations rather than treating
compilation or a health response as UI acceptance.

### Group edits preserve member order

The candidate Bot list may have a different sort order from a room. Renaming
or editing membership now keeps retained members in their stored order and
appends newly selected members. Removing the new member leaves the original
order intact; temporarily unchecking and rechecking a retained member does
not request a reorder. Selected keys still resolve through currently eligible
Bots in the exact Project.

`TestGroupMemberOrderBrowser` exercises production GroupEditor, room and
roster components over authenticated HTTP and SQLite, with an oppositely
ordered candidate list. It checks rename, add/remove, reopening and cancellation,
rendered avatar order and the source's real round-zero `@all` selection.
Chromium and WebKit cover desktop and touch layouts on local and closed Cloud
command rails. The outer encrypted carrier is simulated. No model or formal
viewer is used. This repair does not reorder previously saved groups.

### Whole-row drag comparison

The interaction reference is Codync commit
[`9b47189739216b405416b334b9ada5b5b28176c1`](https://github.com/leepokai/Codync/tree/9b47189739216b405416b334b9ada5b5b28176c1).
Its desktop `apps/desktop/src/renderer/views/BotRoster.tsx` and
`lib/pointer-drag.ts` keep provisional order, a pointer-following lifted copy,
sliding neighbours, edge scrolling, cancellation and a swallowed release click.
The iOS `apps/ios/Views/BotListView.swift` uses a native list, item providers and
accessible move actions; Android's `ListReorder.kt` commits only completed drops.
The browser implementation follows these observable behaviours, using a hold
for both pointer and touch. It does not import upstream platform or store code.

`roster-direct-drag.e2e.mjs` bundles the actual mixed roster with the same nine
synthetic rows at desktop and phone sizes. It records videos, initial/lifted/reopened
screenshots and observed orders. It checks Bot and group dragging, preview before
save, reload persistence, neighbour animation, Escape/outside cancellation,
keyboard/pin access, short group entry and reduced motion in Chromium and WebKit.
Chromium additionally uses actual touch input for dragging, native vertical panning,
touch cancellation and a following short tap. WebKit's pointer cases do not claim
native touch or physical iPhone acceptance. `BOTS_BASELINE=<revision>` substitutes
the previous roster and reproduces the absent default hold before checking the
candidate. No model, live account, production Cloud or installed app is exercised.
