# Bots: show the Sessions a Bot has dispatched (design direction, 2026-11-23)

Status: design proposal only. Nothing here is implemented or approved.

## The request

The person asked whether the Bots screen could show the Sessions a Bot has dispatched, "a little
like the Sessions list, where a small line says which shells are running". Today such a Session is
visible only in the Sessions list, inside the dispatching Bot's group frame
(`web/console/src/session/bot-dispatch-groups.tsx`, commits `de5a0f082`, `e60dd8ce5`, `7dd56f20c`,
`a52e4e780`), with a state line such as "Delivered; review or integration may still be pending".

## The reference: the running-shell line

`bar.sessionShellOne` / `bar.sessionShellMany` ("{n} shells running") appear in two places:

- `web/console/src/session/List.tsx` `stateLine()` appends a quiet `<span class="shells">` at the end
  of a Session row's state line. It is text only; it has no click target of its own. Tapping the
  row opens the Session.
- `web/console/src/App.tsx` adds the same words as a `part quiet` item of the Sessions header count
  band.

So the reference is a pattern of tone (one quiet grey clause, present only when non-zero), not an
expandable component. The Bots chat already has the closer interactive precedent:
`BackgroundCapsule` in `web/console/src/project-assistants/BackgroundTasks.tsx`, rendered in
`.pa-chat-footer` above the composer (`Chat.tsx`), which shows a Bot's own background threads as
one pill and opens a detail view.

## What the Bots screen can read today

Every broker task dispatched with an `AssistantOwner` is synchronised into the owning Bot as a
`ProjectAssistantWorkLink` (`internal/app/projectassistant/workbridge.go` `Sync`, projected by
`AssistantWorkSnapshotOf` in `internal/app/orchestrator/project_assistant_actor.go`). That is the
same provenance the Sessions grouping uses, so no new inference is needed.

| Need | Available now | Where |
|---|---|---|
| Which tasks this Bot dispatched | yes | `work_links` / `work_updated` events on the Bot read view; `WorkResultCard` timeline rows |
| State | yes, mapped | `accepted` (queued/spawning), `waiting_for_person`, `executing` (briefed), `completed` (delivered, not yet landed), `failed`, `landed`, `unknown` |
| Landing commit | yes | `landing_commit` |
| Count of unfinished work per Bot | partly | roster `active_work` counts `accepted`/`waiting_for_person`/`executing` links **plus** background tasks; `background_work` holds the background part, so dispatched = `active_work` − background total, which is a derivation, not a field |
| Task title | **no** on the Bot side | only on Session rows (`bot_dispatch.task_title`) |
| Destination Project (name, icon) | **no** on the Bot side | only on Session rows (`bot_dispatch.project`, built by `botDispatchProject`) |
| Which Session to open | **no** | the record has `ChildConversationID` / terminal identity; the link carries only `task_id` |
| Started / finished time | **no** | record `SpawnedAt`, `AcceptedAt`, `FinishedAt` |
| `completed` (delivered, awaiting integration) in the roster count | **no** | `active_work` excludes it by design |

### Fields to add (proposal)

1. `ProjectAssistantWorkLink` gains, projected from the broker record inside
   `AssistantWorkSnapshotOf` (never from client JSON): `title`, `project` (the same
   `SessionBotDispatchProject` shape, resolved by the same helper as the Sessions list),
   `session` (the identity the Sessions list uses to open the worker's row), `started_at`,
   `finished_at`. Older sources omit them; the UI then falls back to the task ID and no timer.
2. `ProjectAssistantBotSummary` gains `dispatched_work { running, needs_you, delivered }`, counted
   in the same store query family as `background_work`, so the roster stops deriving it.
   `delivered` counts `completed` links finished within the display window (item 3 below).
3. These fields travel through the Cloud Bot reader unchanged so the phone sees the same list; the
   phone opens the Session through the existing fleet Session route.
4. Bounds: the strip lists at most a registered number of rows (proposal 5, then "N more" opens
   the Sessions list filtered to this Bot). Register the bound in `internal/domain/capacity`.

## Directions

Screenshots (static HTML, desktop plus a 390px phone with the safe area drawn) were sent to the
person with `clawdline image send`; the mock source is the task artifact `mock.html`.

### A. A quiet strip above the chat composer

One line in `.pa-chat-footer`: "Dispatched Sessions · **2 running** · 1 awaiting integration".
Tap expands it in place into rows (state mark, task title, destination Project mark and name,
elapsed time, chevron); a row opens that Session. Absent when there is nothing to show.

- Pros: sits where the person is already looking; mirrors the existing background capsule, so the
  pattern is learnt once; no new chrome in the header; works one-handed on a phone.
- Cons: the footer already hosts the background capsule, so both must share one slot (see item 9);
  invisible from the roster.

### B. One grey line under each roster row

"● 2 Sessions running · 1 awaiting integration" under the Bot's preview line, only when non-zero.

- Pros: overview across Bots at a glance; tiny; the unread-first ordering already lifts working
  Bots, so this explains why they are on top.
- Cons: no detail and no way to open a Session from here; adds a third line that makes busy rows
  taller on a phone.

### C. Header icon with a count

A small icon plus number beside the Bot's info button. Desktop opens a popover; phone opens a
bottom sheet that respects the home-indicator inset.

- Pros: zero vertical space in the chat; the sheet has room for many rows.
- Cons: an icon with a number reads as a notification badge; it is easy to miss and needs a learnt
  meaning; one more header control on a phone header that is already crowded.

### D. A plus B (recommended)

Roster rows carry B's grey line; inside the chat A's strip gives the list and the jump.

- Pros: answers both "which Bots are busy" and "what exactly is running"; each part is one quiet
  line and disappears when idle.
- Cons: two surfaces to build; needs the roster count field (fields item 2) before B is honest.

**Recommendation: D, built as A first (it carries all the detail and the jump), then B.** A alone
already satisfies the request; B is a small follow-up once `dispatched_work` exists.

## Details for the person to confirm

1. **States shown.** Show `accepted` and `executing` as "running" (green pulsing mark),
   `waiting_for_person` as "needs you" (amber), and `failed` (red) until dismissed or 24 hours.
   `landed` and `unknown` are not listed (unknown goes to the existing timeline card).
2. **Delivered, awaiting integration** (`completed`). Show it in the strip with a hollow amber
   mark and the text "Delivered, awaiting integration" for 24 hours after `finished_at`, or until it
   lands, whichever is first. It is not counted as running.
3. **Collapsed text.** Running count first, then the other non-zero states:
   "Dispatched Sessions · 2 running · 1 needs you · 1 awaiting integration". Grey text, only the
   running count in normal ink.
4. **Expanded row.** Mark, task title (one line, ellipsis), "state · Project mark Project name",
   elapsed `m:ss`/`h:mm` for running rows only, chevron. Minimum 44px touch height.
5. **Where a tap goes.** A row opens that Session (the same target as the Sessions list row). If
   the Session is gone (closed or another machine offline) it opens the existing work timeline card
   instead and says why, never a dead link.
6. **No work, no space.** Strip, roster line and any icon are absent, not empty, when the counts
   are zero; nothing reserves height.
7. **Cross-Project dispatch.** Every row names its destination Project with its mark; a row whose
   Project differs from the Bot's own Project also carries a small "other Project" tag.
8. **Phone layout.** The strip sits above the composer inside the keyboard-avoiding footer, so it
   stays above the home indicator and moves with the keyboard; expanded height is capped at
   about 40% of the viewport with internal scrolling; "N more" opens the Sessions list filtered to
   this Bot.
9. **Sharing the footer with background threads.** When the Bot also has background threads, the
   two become one strip: "Background 1 · Sessions 2 running"; expanding shows two short sections.
   Never stack two pills above the composer.
10. **Order.** Needs you, then running (longest first), then failed, then awaiting integration.

## Setting toggle

The person's rule is that new behaviour gets a switch and the default keeps today's behaviour.
Recommendation: add one switch, "Show dispatched Sessions in Bots" (Settings → Bots), covering
both the strip and the roster line. Because this display was requested explicitly and occupies no
space when idle, the recommendation is to ship it **on** for this person's request but keep the
switch so it can be turned off; if the person prefers the rule strictly, ship it off and turn it on
once. This is the person's choice and should be confirmed with the details above.

## Not decided here

- Whether stopping a dispatched Session from the strip is wanted (the background capsule has a
  stop button; a dispatched Session already has its own controls in the Sessions view).
- Exact copy in other shipped languages; English and Taiwan Traditional Chinese change together.
