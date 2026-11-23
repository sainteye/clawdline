# Where the Bot voice-call button belongs

Date: 2026-11-23. Base: `c34517bc4`. Investigation only; no product code changed.

## Question

The Bot chat currently stacks three rows at the bottom on a phone:

1. `.pa-chat-metadata`: context usage on the left, "Model · Agent default" on the right
   (`web/console/src/project-assistants/Chat.tsx:1601`).
2. `BotCallControls`: a row of its own with a right-aligned "Voice call" pill
   (`Chat.tsx:1609`, `BotCall.tsx:69-76`, `bot-call.css` `.pa-bot-call-start { justify-content:flex-end }`).
3. The composer (attach, dictate, message field, send).

Measured in the fixture at 390×844 (CSS pixels, `getBoundingClientRect`): metadata 730–746,
call row 748–780, composer 780–828. The call row ends exactly where the composer begins (0 px
gap), so the pill reads as glued to the input, and it costs a 32 px row that the chat loses on
every Bot screen. At 1280×800 the numbers are the same shape: 696–728 call row, 728 composer.

Is this placement reasonable, and where does Codync put it?

## What Codync does

Source: `github.com/leepokai/Codync`, shallow clone of `main` at
`9dae3e2bec9b91a4a42f85a943eb3983f95d0503` (clone deleted after reading).

Codync has a real voice call (not just dictation). It has **no separate call row and no
header call button**. The entry is the composer's trailing button: while the message box is
empty (and the bot is not working), the send button becomes a wider filled capsule with a
waveform symbol, labeled "Call". Typing turns it back into send; a working bot turns it into
stop; a speaking bot during a call turns it into interrupt. One button morphs between the four
states. The same rule is implemented on every client:

| Client | Entry | Evidence |
|---|---|---|
| iPhone | Composer trailing button, `.call` state while draft and files are empty; waveform, 1.35× width capsule | `apps/ios/Kit/Sources/CodyncUI/Chat/Composer.swift:164-215` (state at `:171`, symbol at `:183`, width at `:210`); wired from `apps/ios/Kit/Sources/CodyncUI/Chat/ChatView.swift:64-67` |
| Desktop (Electron) | Same morphing `composer-send` button | `apps/desktop/src/renderer/views/chat/Composer.tsx:142-147`, `:211-227`; wired from `apps/desktop/src/renderer/views/chat/ChatView.tsx:250` |
| Android | Same `Trailing.Call` state | `apps/android/app/src/main/kotlin/com/codync/android/MessageComposer.kt:129`, `:201`; wired from `ChatScreen.kt:173` |
| Website demo phone | Same rule (46 px wide call capsule vs 34 px send) | `web/app/components/live-phone/screens.tsx:380-427` |

Their UI conventions state it directly: "the call button sits in the composer as a filled
waveform capsule while the box is empty" (`docs/design/ui-conventions.md:5`). The header's
trailing buttons are find and remote screen, not call (same line).

**During a call**, the controls move to the top: a call bar floats over the chat under the
header, and the chat stays readable and usable underneath. Left to right: bot avatar (tap to
interrupt while it speaks), a voice-level dots line, provider name when audio leaves the
device, then Call settings (gear), Mute and End call (red ✕). The composer keeps its place.

- iPhone: `.overlay(alignment: .top)` with `CallView` (`ChatView.swift:73-90`); mute and end at
  `apps/ios/Kit/Sources/CodyncUI/Call/CallView.swift:201-214`.
- Desktop: `.call-overlay { position:absolute; top:16px; … justify-content:center }`
  (`apps/desktop/src/renderer/views/chat/chat.css:405-413`), rendered at `ChatView.tsx:256-259`.
- Android: `CallBar` in a column padded under the header (`ChatScreen.kt:149-154`).
- On the lock screen / Dynamic Island the Live Activity carries mute and end.

Public docs and images:

- Voice docs: https://www.codync.dev/docs/voice ("When the message box is empty and the bot
  isn't working, the send button becomes **Call** (a waveform)… The call bar floats at the top
  of the chat").
- Live Activity / Dynamic Island images: https://www.codync.dev/screens/activity.webp,
  https://www.codync.dev/screens/island.webp (both returned HTTP 200 on 2026-11-23).
- App Store listing: https://apps.apple.com/app/codync/id6760984418. The README's committed
  screenshots (`docs/screenshots/desktop-macos.png`, `android-roster.png`) do not show an open
  chat, so the composer state is evidenced by source, not by a public screenshot.

Dictation in Codync is a separate microphone button beside send (`Composer.tsx:205-209` on
desktop), so a Codync composer with an empty box shows mic + waveform-call side by side.

## Clawdline today

Screenshots were taken from the repository's own Bots fixture
(`web/console/src/project-assistants/fixtures.ts` with `tools/fixtures/project-assistants.html`),
served locally and captured with headless Chrome at device scale 2, zh-Hant.
They live in the task's artifacts directory and are not committed:

- `current-390x844.png`, `current-1280x800.png`: the present layout.
- `protoA-*`, `protoB-*`, `protoC-*`: the prototypes below, each at both sizes.

One environment shim was needed: headless Chrome has no on-device speech recognition pack, so
`botCallAudioAvailable()` / `Recognition.available()` would hide the button. The capture script
stubbed only `SpeechRecognition.available()` to report "available"; the button itself is the
real `BotCallControls` component. Prototypes were made by moving that real button element in the
page with injected script, not by redrawing.

Clawdline's active-call UI is split today:

- Realtime (provider) calls: `BotVoicePanel` already sits at the top, under the header
  (`Chat.tsx:1467`), which matches Codync.
- On-device calls: the active `section.pa-bot-call` (status, Interrupt, Mute, End) renders in the
  same bottom slot as the start pill (`BotCall.tsx:77-83`), directly above the composer.

## Options

### A. Join the metadata row

The call pill moves into `.pa-chat-metadata` beside the model label; its own row disappears.

- Height: saves ~16 px (the merged row grows from 16 to 28 px to fit a touch-sized pill;
  composer top unchanged, the free space comes from removing the row). Prototype measured
  metadata 744–772, composer 780 → an 8 px gap appears.
- Reach: right edge just above the composer, easy for a right thumb.
- Safe area: unaffected (above the composer).
- Active call: still needs a home; leaving the on-device bar above the composer keeps the stacked
  look during a call.
- Codync consistency: low. It keeps a text pill in a metadata line, which Codync never does, and
  mixes an action with passive information. With context usage present the row can wrap on a
  390 px screen, bringing the extra row back.
- Cost: smallest change (one JSX move plus CSS).

### B. Composer trailing button morphs into Call (Codync)

When the box is empty, no files/images are attached, nothing is running and a call is possible,
`.pa-send` becomes a 46×34 waveform capsule labeled "Voice call · <bot>". Typing returns it to
send; a running turn keeps today's stop. The separate row is removed.

- Height: saves the full 32 px row; the metadata line sits 2 px above the composer as before.
- Reach: the send position is the most thumb-reachable control on the screen; no new target.
- Safe area: inherits the composer's existing bottom inset.
- Active call: move the on-device `pa-bot-call` bar to the top slot next to `BotVoicePanel`, so
  both engines show their controls in the same place, under the header, as Codync does. While
  a call is active the trailing button shows Interrupt when the bot speaks (Codync) or stays
  disabled send as today (`callActive` already blocks sending, `Composer.tsx:45`).
- Codync consistency: exact on all three of their clients.
- Trade-offs: the action is only visible with an empty box (Codync accepts this; the
  `voiceShortcut` keyboard path still works). The status/error text that the start row shows
  today ("connecting", realtime start errors, `BotCall.tsx:70-71`) needs a home: render it as a
  status line only when non-empty (in the top call slot), not as a permanent row. Accessibility:
  one button changes role/label; keep `aria-label` switching as `.pa-send` already does for stop.
- Cost: moderate (props through `Composer`, a fourth trailing state, moving the active bar).

### C. Header icon on the right

A circular waveform icon at the right end of the chat header, mirroring the back button.

- Height: saves the full 32 px row.
- Reach: top-right corner is the hardest spot to reach one-handed on a 6-inch phone.
- Safe area: header already handles the top inset.
- Active call: bar under the header sits right next to its trigger, which is coherent.
- Codync consistency: low; Codync's header has find and remote screen, not call. Clawdline's header
  also carries `BotBrowserChip` (`Chat.tsx:1461`), which competes for the same right edge.
- Cost: small to moderate.

### Summary

| | Row saved | Thumb reach | Active-call bar | Codync match | Cost |
|---|---|---|---|---|---|
| A. Metadata row | ~16 px, can wrap back | good | unresolved | low | small |
| B. Composer morph | 32 px | best | top, unified with realtime | exact | moderate |
| C. Header icon | 32 px | poor | top | low | small–moderate |

## Recommendation

**B.** It removes the row the person objected to, puts the call where Codync puts it on every
platform, uses the most reachable spot, and lets both call engines show controls in one place at
the top. A is a cheaper cosmetic fix but keeps an action in a metadata line and does not solve the
active-call stacking; C trades the glued row for an awkward reach and a crowded header.

### Files and approximate diff for B

- `web/console/src/project-assistants/Composer.tsx` (~25 lines): accept `onCall?: () => void` and
  `callLabel`; compute trailing state `stop > call > send` (call when `empty && no images/files &&
  onCall && !callActive`); render the waveform icon and `data-call` on `.pa-send`; stop dictation
  before calling, as Codync does.
- `web/console/src/project-assistants/BotCall.tsx` (~30 lines): split the start logic out of
  `BotCallControls` into a hook (e.g. returns `{canStart, start, label, status}`), keep the active
  bar as its own component; drop the `pa-bot-call-start` pill.
- `web/console/src/project-assistants/Chat.tsx` (~10 lines): pass `onCall` to `Composer` from that
  hook; render the active on-device bar and any start status beside `BotVoicePanel` at line 1467
  instead of above the composer at line 1609.
- `web/console/src/project-assistants/bot-call.css` and `styles.css` (~15 lines): remove
  `.pa-bot-call-start`; add `.pa-send[data-call]` (46×34, `border-radius:17px`, centered icon);
  give the top call bar the same horizontal margin as `.pa-bot-voice-panel`.
- Tests: the existing Bots e2e fixtures that click the "Voice call" pill by role/name keep working if
  the label stays the same; update any screenshot baselines and add a case that typing turns Call
  back into Send.
- Copy: no new strings needed if the button reuses `botCallWords().start`; otherwise add English and
  zh-Hant together.

Not verified: the real phone (Safari, safe-area inset, real on-device recognition), and how the
morph feels during a running turn. These need a check on the actual surface after implementation.
