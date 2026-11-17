# Bot roster preferences

The roster keeps pinned Bots above their Projects. A star pins or unpins a Bot;
its Project remains unchanged. Handles reorder Bots within their current section
or reorder Project sections. Mouse, touch and pen input use pointer capture;
Up/Down and Home/End on a focused handle perform the same reorder. Dropping a Bot
into the pinned section pins it. Dropping it back into its own Project unpins it.
A presentation drag cannot move a Bot into another Project or grant source access.

Preferences are stored in this browser's local storage under the authenticated
account namespace, with machine/Bot and machine/Project identities as keys. They
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

`roster-state.test.ts` and `roster-groups.test.ts` exercise round trips, account
and machine isolation, filtered moves, missing identities, storage failures,
Project ordering and the Project-name collision. `roster-order.e2e.mjs` bundles
the real roster with synthetic summaries and serves only on loopback. It checks
mouse dragging, keyboard ordering, pin/unpin, reload, Project order, account
switches, cross-Project refusal and corrupt storage in Chromium and WebKit,
in English at desktop size and Traditional Chinese at phone size. Chromium's
phone case also sends actual touch input through CDP. It makes no provider calls.

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
