# Bot roster preferences

The roster mixes Bots and group conversations inside their actual Projects.
Pinned conversations stay above Projects. A group retains its source-owned group
identity; sharing an ID with a Bot never shares its preferences or room routing.

The default row shows its avatar, name, one-line preview and necessary state.
Repeated Agent labels are omitted. Less frequent actions are in the row's menu,
reachable by hover or keyboard focus on desktop and a visible touch target on
phones. List options opens the explicit reorder mode; Done returns to reading.
Only reorder mode shows pin buttons and handles. An empty pin drop target appears
only during a row drag. A presentation drag cannot move a conversation into another
Project or grant source access. Mouse, touch and pen use pointer capture;
Up/Down and Home/End on a focused handle perform the same reorder.

Group rows compose actual members' avatars, count members beyond the registered
preview bound, and show the returned last speaker and message. Missing member
metadata remains unknown. The optional list projection supplies `last_entry` and
`room`; the roster never fetches each room's history for a preview. Older sources
show Project/member metadata instead. Only a real message timestamp participates
in recent ordering; a missing timestamp keeps its input slot. Group modification
time and room-local sequence numbers do not impersonate message time.

Selecting a group opens that exact source's room. Returning restores the original
row's focus and the list's scroll position. Room layout and member-to-Bot
navigation are integrated with the group-room implementation separately.

Preferences are stored in this browser's local storage under the authenticated
account namespace, with machine/Bot, typed machine/group and machine/Project identities as keys. They
survive closing Bots, switching Sessions and reloading this browser. Other tabs
observe storage changes. They do not synchronize across devices or browser
profiles. Source filtering and pagination retain unseen preferences; later Bots
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
in English at desktop size and Traditional Chinese at phone size. Chromium's
phone case also sends actual touch input through CDP. It makes no provider calls. `roster-mixed.e2e.mjs` additionally retains identical
records and CSS viewports for the base and candidate, counts full visible rows,
and checks group entry/return, pin persistence, default controls and absence of
per-row history reads. Its local fixture timings do not measure production or
Codync latency.

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
