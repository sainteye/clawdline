# Bots attention while in a Session — design preview

Status: **design preview for review, not an implementation.** Nothing in the product changes
with this page. `bots-attention-preview.html` beside it is a self-contained, simulated
interaction; it contacts no Bot and delivers no notification.

## The problem

While a person works in a Session, the Bots entry (the floating orb or its docked icon) shows
one combined number. When that number rises, the orb wiggles for a moment and nothing else
happens, so a reply, a permission request or a failure is easy to miss. The person asked for a
message-like tip and/or a sound.

## What the preview proposes

1. **One small tip per meaningful event**, anchored just above the Bots entry, not a page-wide
   toast. It shows the Bot's original line avatar, its name, what happened (replied / needs your
   permission / could not finish), how many items wait for that Bot and how many wait elsewhere,
   and the actions **Open Bot**, **Quiet tips** and a close button.
2. **Visible tips are on by default. Sound is off by default.** The sound is a short, quiet,
   synthesized two-note chime (sine, peak gain 0.05, about 0.3 s). The preview creates its
   `AudioContext` only after an explicit click on the Sound switch or **Test sound**; nothing
   plays on load.
3. **Kinds follow Codync's three alert kinds**: done (reply), needs input (permission) and
   failed. A reply tip fades after 5.2 s unless focus is inside it; permission and failure tips
   stay until dismissed or opened, mirroring Codync's "done fades, failed stays".
4. **Only the newest tip is shown**; other waiting items are summarized as "+N more waiting"
   instead of stacking.

## Rules the preview demonstrates

- **Alert on an event ID, never on a count change.** The badge count is a sum of unread
  replies and pending permissions across Bots, so a rising count cannot tell which Bot, which
  kind or whether the item is new. A production version must key alerts on the item that
  arrived (message, permission entry or failed transition) and remember the IDs already
  alerted. "Repeat the last event (same ID)" shows a duplicate being skipped.
- **Existing unread never alerts.** On page load, reload or reconnect, the snapshot's items are
  recorded as seen without a tip or a sound. "Reload / reconnect" demonstrates this. Codync does
  the same: its island compares against the previous statuses and produces nothing for the
  first snapshot.
- **No tip while Bots is open.** The count updates inside Bots instead. Today the host already
  stops polling the badge while Bots is open.
- **Dismissing a tip removes only the tip.** Opening and reading the Bot clears its unread
  reply and failure attention, while permission counts remain pending until answered. The
  preview does not answer permission requests. "Quiet tips" pauses tips for the rest of the preview without
  clearing anything; sound obeys the same pause.
- **Sound is throttled per Bot and kind for 5 s**, as Codync throttles push alerts.
- **No focus steal and no audio routing changes.** Tips announce through a polite live region;
  focus moves only when the person activates a control. The preview does not touch
  `navigator.audioSession`.
- **Reduced motion** (system setting or the preview switch) removes the orb wiggle, the tip
  entrance and the waiting-avatar pulse.
- Every control is a button reachable by keyboard; Escape closes a focused tip or the simulated
  Bots window.

## Evidence from source

Clawdline at the task's merge of `f74ef215c`:

- `web/console/src/project-assistants/BotOrb.tsx:15` sums `unread+pendingPermissions` into one
  count; `:22` and `:88-98` (`useRinging`) set a 1.2 s ringing state whenever that number rises.
  That is the only attention signal today.
- `web/console/src/pages/project-assistants.tsx:15` sets `BADGE_REFRESH_MS = 15_000`; `:40-45`
  refresh the badge on that interval only while Bots is closed and the page is visible.
- `web/console/src/cloud/project-assistants.ts:455-458` (`assistantBadge`) folds every readable,
  non-hidden Bot's `unread_count` and `pending_permissions` into the two totals, losing item
  identity.
- `web/console/src/realtime/chime.ts:71-80` deliberately switches `navigator.audioSession` to
  `playback` for the voice-send chime. The preview does **not** adopt this: an unsolicited Bot
  chime must not override the ringer or audio session.
- `web/console/src/project-assistants/BotAvatar.tsx:54-56` and
  `web/console/src/project-assistants/avatar-paths.ts` are the line avatars the preview copies
  (outline stroke 4.4, eyes 5.5). No new artwork was produced.

Codync at `9f67eb09084db61b3cca9c84a11bc03df9de29b7` (read-only reference):

- `host/src/remote/push.rs:22-24` defines `Done`, `NeedsInput` and `Failed`; `:108-115` skip
  alerts when `bot.notify == Some(false)`, when the Bot is hidden or while the phone app is
  connected, and throttle the same Bot and kind to 5 s; `:117-122` send only a generic line to
  the relay while the real title and body are sealed per device.
- `apps/desktop/src/renderer/views/island/island-model.ts:226-246` derives notices from status
  transitions (none on the first snapshot) and fades only `finished`; `apps/desktop/src/shared/island.ts:95`
  sets `FINISHED_MS = 5200` and `:136-143` (`transitions`) emits a notice only for a Bot that left
  `working`/`needsInput` for idle or error.
- `apps/desktop/src/renderer/views/island/IslandViews.tsx:158-187` (`Notice`) renders the alert
  card with "Open conversation" and "OK".
- A search for `AudioContext`, `new Audio(`, `NSSound` and notification sounds found audio only
  in call and voice-message playback. **No foreground alert chime or in-app toast for Bot events
  was found in Codync**; the optional chime here is a Clawdline proposal, not parity.

## What the preview does not decide

- Where the tip and sound settings live in the product, and whether they are per device,
  per person or per Bot.
- How the client learns event IDs (a new field on the summary, an event stream, or the existing
  15 s poll with per-item cursors). The rule — alert on a new ID, never on a count — is what the
  preview fixes.
- Whether Cloud or phone push should change; this concerns the open Session view only.
- Subjective loudness: the chime was played in a browser and its `AudioContext` reached
  `running`, but its volume has not been judged by ear.

## Needs the person's review

- Tip position and size next to the orb on desktop and phone.
- The copy in English and Taiwan Traditional Chinese.
- Whether replies should alert at all, or only permission requests and failures.
- Whether the chime should be offered, and whether its default stays off.

## Cloud delivery

The hosted document viewer removes scripts, so the delivered Cloud document uses rendered desktop and phone-size screenshots rather than claiming that the prototype runs there. The same synthesized chime is supplied in a short H.264/AAC MP4 that the Cloud document player supports. The original self-contained HTML remains the locally verified interaction prototype. Opening a Bot in that prototype clears unread reply/failure attention while leaving its permission count pending.
