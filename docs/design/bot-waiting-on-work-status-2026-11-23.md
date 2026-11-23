# Bots: a "waiting on its Sessions" state and a dispatched count per row (design direction, 2026-11-23)

Status: design proposal only. Nothing here is implemented or approved. It continues
`bot-dispatched-sessions-strip-2026-11-23.md` (commit `34119b37f`), whose direction D
(A: a quiet strip above the Bot chat composer; B: a grey line in the roster) this page keeps.

## The request

The person, with a Bots roster and a Sessions screenshot: the Sessions screen showed three Sessions
running under Atlas while Atlas's roster row showed nothing. They asked that

1. every Bots roster row shows how many Sessions ("clones", 分身) the Bot has dispatched — this makes
   the earlier part B **required**, not a follow-up;
2. a Bot whose own turn is over but whose dispatched work is still running gets a state of its own,
   distinct from `working` (the Bot itself is running), with its own animation, so the person can
   tell "it is thinking" from "it is waiting for the work it handed out".

## What the code does today

| Concern | Where | Behaviour |
|---|---|---|
| Bot status | `internal/adapters/projectassistantstore/roster_activity.go` `projectRosterActivity` | `idle`, `working`, `needsInput`, `error`, derived from the Bot's own turns and permissions only. The field's comment says it "describes the Bot's own chat". |
| Dispatched count | `active_work.go` `activeWorkLinks` + `addRosterBackground` | `active_work` = work links in `accepted`/`waiting_for_person`/`executing` **plus** background tasks; `completed` (delivered, not landed) is excluded. |
| Wire validation | `web/console/src/project-assistants/roster-stream.ts:50` | An unknown `status` value makes the whole roster frame `pa_invalid`. |
| Row presentation | `working.ts` `botRosterActivity` → `BotRow.tsx` | Lifts background threads: background `needs_input` → `needsInput`; background working/waiting while the Bot is not working → `working` with the caption "N background …". Dispatched work links lift nothing, which is why Atlas looked idle. |
| Avatar motion | `BotAvatar.tsx` (`Mood = idle/working/needsInput/failed`) | Working: stepped 3.2 s glance, 4.7 s blink, gradient turning at 1.4 rad/s. needsInput: centred gaze, a separate contour scaling outward at 5 rad/s. Reduced motion stops all of it. Rendered by `BotRow`, `Chat.tsx` header, `GroupRoom.tsx`, `BotOrb.tsx`, `BotVoicePanel.tsx`. |
| Preview orb | `ThinkingOrb` in `BotRow` | Shown for working (grey) and needsInput (amber). |
| Status-bar entry | `web/console/src/cloud/project-assistants.ts` `assistantBadge` | needsInput beats working beats idle across visible Bots. |
| Tower | `tower/tower-state.ts` `towerMood` / `tower-draw.ts` | `wait`/`work`/`error`/idle moods choose a zone; `work` = `activeWork(item)` already draws helpers at the portal. |
| Unread-first order | `roster-conversations.ts` `orderedConversationSections` | Unread, then `status === "working"`, then everything else. Note it reads raw `status`, not `botRosterActivity`, so a Bot lifted to working by background threads is not lifted in the order. |

## 1. Roster count

### What the number counts

**Running clones = dispatched work links in `accepted`, `executing` or `waiting_for_person`.**
That is "Sessions this Bot handed out that have not finished". It excludes:

- background threads (they are the Bot's own internal workers and already have their caption);
- `completed` (delivered, awaiting integration): the clone has finished; the ball is with whoever
  integrates. It is shown in the chat strip (previous design, item 2), not in the count;
- `failed`, `landed`, `unknown`.

The number comes from a new field, not from `active_work − background`, so the client never derives
it (see section 2, contract).

### Placements (mock: `artifacts/mock-static.png`)

- **B1 — grey clause in the preview line.** "Read notes.md · 1 clone". No new element. Cons: the
  preview line is already the busiest text and truncates first on a phone, so the count is the
  first thing to disappear; it competes with the working caption.
- **B2 — corner badge on the avatar ("×3").** Zero width. Cons: the avatar corners already hold the
  agent badge, the lead star, the unread dot and the "!" attention mark; a number there reads as an
  unread or notification count.
- **B3 — icon and number in the right meta column, under the time (recommended).** A small
  two-outlines icon (one outline behind another, the same metaphor as the animation) plus the
  number in normal ink. The meta column is empty below the time on most rows, so the row does not
  grow and nothing reflows; it stays visible while the Bot itself is working; unread count and clone
  count stack in the same column without touching. Absent at zero, no reserved width.

When the Bot is in the new state, the preview caption also changes to "Waiting for 3 Sessions" /
「等 3 個分身完成」(plus " · 1 awaiting integration" when non-zero), the same way
`botRosterActivity` already replaces the caption for background threads. That gives B3's number a
sentence, and covers screen readers: the row's `aria-label` gains the same clause.

## 2. The new state `waitingOnWork`

### Rule

`waitingOnWork` ⇔ the Bot's own status is `idle` **and** running clones > 0 (the count above).

- `completed` does **not** keep a Bot in this state. Nothing is running; the Bot is not waiting on
  a clone but on an integration decision. Keeping the avatar animated for up to 24 hours after
  delivery would make the state mean "something happened recently" instead of "something is
  running". Delivered work stays visible in the strip and caption.
- `waiting_for_person` clones count as running but lift the row to `needsInput` (below), the same
  way background `needs_input` already does.
- Facts come only from broker task records projected through `AssistantOwner` into work links
  (`workbridge.go` `Sync`, `AssistantWorkSnapshotOf`) — never from what the model says in chat. An
  offline or stale source shows idle, as every other activity does (`liveActivity`).

### Precedence (highest first)

1. **needsInput** — the Bot's own permission, or any clone `waiting_for_person`, or any background
   thread needing input. Caption names which ("A dispatched Session needs you").
2. **error** — the Bot's own last turn failed. (Today background working silently hides an own
   error; this design fixes that ordering in the same function.)
3. **working** — the Bot's own turn is running, or (unchanged) its background threads are.
   **Own working + clones running shows working**: what the Bot is doing now is the more immediate
   fact. The B3 count still shows the clones, so nothing is lost.
4. **waitingOnWork** — the Bot is otherwise idle and clones are running.
5. idle.

### Contract

Do **not** add a fifth value to `ProjectAssistantBotSummary.status`. The validator in
`roster-stream.ts:50` rejects unknown values for the whole frame, so a newer daemon would blank the
roster of any older console — the hosted console at a previous stamp, a phone with a cached bundle,
or a Cloud reader. `status` also documents itself as the Bot's own chat; keep that meaning.

Instead:

1. Add an optional `dispatched_work { running, needs_you, delivered }` to `BotSummary` (Go domain
   type, generated contract, wire mapper in `project_assistants_wire.go`), counted in the same store
   query family as `background_work` from the work-link states above (`running` = accepted +
   executing, `needs_you` = waiting_for_person, `delivered` = completed within the strip's display
   window). This is the field the previous design already proposed; one field serves both the count
   and the state. Absent means none or an older source.
2. The validator accepts the new object (non-negative safe integers, like `background_work`) and
   ignores it when absent.
3. `waitingOnWork` is a **presentation state** computed in one place, `botRosterActivity`
   (`working.ts`), from `status`, `background_work` and `dispatched_work`. Every consumer that today
   reads `botRosterActivity` or `rosterActivity` gets it: `BotRow`, the status-bar badge (where it
   ranks after working and does not animate the entry — the entry should not look busy because
   Sessions are), the Chat header avatar, and the tower.
4. A roster change must be published when a work link changes state, as `noteBackgroundOwner` does
   for background tasks; otherwise the count goes stale until the Bot's own next event. Check
   whether `Sync` already writes a roster change for the owner before adding one.
5. Cloud Bot reader passes the field through unchanged; phones see the same count.

### Surfaces

- **Sessions view**: unaffected. Its Bot group frame already reads `bot_dispatch` on Session rows.
- **Bots tower** (`tower/BotTower.tsx`): add a `TowerMood` `oversee` placed in the `desk` zone but
  standing still (no typing sway), facing the portal where `portalHelpers` already draws its clones.
  It needs `dispatched_work` instead of `active_work` for the helper count so background threads and
  dispatched Sessions are not mixed; floor captions use the same "Waiting for N Sessions" text.
- **Chat header**: avatar uses the new mood; the strip from the previous design explains it.

## 3. Animation

Implementation lives in `BotAvatar.tsx`: add `waitingOnWork` to `Mood` and a branch in the same
33 ms requestAnimationFrame loop (it already pauses off-screen and on hidden documents). Three
candidates, same line art, same gradient, no geometry change
(`artifacts/waiting-on-work-motion.gif`, 12 s at 4 fps; static frames in `mock-static.png`):

- **W1 Orbit.** One small gradient dot per running clone (cap 3) walks the outline, a 6 s lap;
  the eyes slowly follow the leading dot. Reads as "things in motion around it". Cons: at the
  roster's 44 px the dots are 2 px and look like dust; a count-driven drawing changes when a clone
  finishes, which is noisy.
- **W2 Echo clone (recommended).** A second, thinner outline (2.4 vs 4.4 stroke, 28–50 % opacity)
  sits behind the Bot, offset up-right, drifting out and back on a 4 s cosine; the eyes glance
  toward it at the far point and blink on the existing 4.7 s cadence. It literally draws a 分身,
  is legible at 44 px, and differs from both existing moods: working moves the *light* and the
  *eyes*; needsInput *scales* a contour outward and fades it; W2 *translates* a constant second body
  and never scales. The gradient does not turn, so the Bot itself looks at rest.
- **W3 Patient dash.** The outline becomes dashed and the dashes crawl (8 s per lap); eyes
  half-lidded with a slow 6.4 s blink. Calm, but a dashed outline already means "pending / not
  connected" elsewhere in the console, and it alters the approved outline itself, which the person
  asked to keep.

**Reduced motion** (`useReduceMotion`): no movement. W2 keeps a static, dashed second outline at
the mid offset — the state remains distinguishable without motion (`mock-reduced.png`). W1 keeps
its dots fixed; W3 keeps a still dashed outline. Stale sources stop moving, as today.

The roster preview shows no ThinkingOrb in this state (it means "the Bot is thinking"); the caption
and B3 number carry it.

## 4. Unread-first ordering

Recommendation: **merge into the working section**, ordered own-working first, then
waitingOnWork, each keeping its existing relative order. A separate section would add a header for
what is usually one or two rows, and from the person's side both are "busy, something will arrive".
While there, make the section read the presentation state (`botRosterActivity`) rather than raw
`status`, so a Bot shown as working because of background threads is also lifted — today the row
and the order disagree.

## 5. Complete detail list (integrates direction D)

1. **Count (B3, required).** Icon + running clones in the row's meta column; absent at zero.
   Counts accepted, executing and waiting_for_person; not delivered, failed, landed or background.
2. **State `waitingOnWork`.** Own status idle and running clones > 0; delivered does not count.
   Precedence needsInput > error > working > waitingOnWork > idle.
3. **Caption.** "Waiting for N Sessions" / 「等 N 個分身完成」, plus " · M awaiting integration";
   a clone waiting for the person lifts the row to needsInput with "A dispatched Session needs you".
4. **Animation W2 Echo clone**, reduced-motion static dashed echo; no ThinkingOrb.
5. **Contract.** New optional `dispatched_work {running, needs_you, delivered}`; `status` keeps its
   four values; state computed in `botRosterActivity`; roster change on every work-link transition.
6. **Order.** Merged into the unread-first working section, after own-working Bots.
7. **Chat strip (previous A).** Unchanged from the previous items 1–5 and 8–10: states, collapsed
   text, expanded rows, tap opens the Session, phone layout, ordering.
8. **Footer sharing (previous 9).** One strip for background threads and Sessions; never two pills.
9. **Tower and status bar.** Tower `oversee` mood at the desk facing its portal clones; status-bar
   entry ranks waitingOnWork after working and does not animate for it.
10. **Bounds.** The strip's row limit (proposal 5) is registered in `internal/domain/capacity`.

**Recommendation:** build 5 → 1–3 → 4 → 6 in one construction item (the count and state need the
field before anything is honest), then 7–8 (the strip), then 9.

## 6. Setting

One switch, "Show dispatched Sessions in Bots" (Settings → Bots), covering the count, the
`waitingOnWork` state and animation, and the chat strip — they are one fact shown three ways, and
half of it on would leave the row and the avatar contradicting each other.

Default: **on**, consistent with the previous design. Reasons: the person explicitly asked for the
count and the state; every element is absent when no Session is dispatched; and nothing that is
shown today changes meaning — background-thread behaviour (`working`) is deliberately left as it is
in this design. With the switch off, rows, avatars, ordering and tower behave exactly as today.
If the person wants their rule applied strictly, ship it off and turn it on once; this is theirs to
confirm.

## Not decided here

- Whether background threads should also move from `working` to `waitingOnWork` (semantically
  they are handed-out work too). Left unchanged so the default changes nothing visible today.
- Exact copy in other shipped languages; English and Taiwan Traditional Chinese change together.
- Whether the B3 number should be tappable (open the strip directly) on desktop.
