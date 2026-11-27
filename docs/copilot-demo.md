<!-- clawdline-doc: kind=reference audience=both -->

# The voice copilot demo film

The recorder captures the built React console making a voice copilot call, in English and
Taiwan Traditional Chinese. These are running console screens, not drawn mockups. The stand-in
daemon supplies fictional Sessions, and the person's lines are prerecorded AI speech.

## Published films, 20 November 2026

The four published cuts use console commit `e3a2eb645ba3be524af750fa05feed065b505764`.
The full call sheet stays visible for at least 30 seconds, the central orb is resized from
112 to 80 and held for five seconds, and the call is minimized to its floating bubble before
opening Search rebuild and Release 2.4. Offline sync remains in the list but has no separate
switch or report. The recording-only presentation adapter preserves these holds when cutting
silent waits; it does not change the product's sheet and bubble modes.

Desktop films are 1920 × 1080 (English 1:51, Chinese 2:43). Phone films are 1080 × 2338
(English 1:55, Chinese 2:35), with a full-width 390 × 808 CSS viewport at device scale 2,
scaled to 1080 pixels wide and 100 pixels of top safe space. No side or bottom padding is added.
This is browser capture, not a physical iPhone recording.

The published media and posters are immutable content-addressed R2 objects selected in
`web/console/src/realtime/copilot-demo-films.ts`. The homepage, voice guide and both README
languages link to the same cuts. Full public readback hashes, byte ranges and cache hits were
checked. All four calls ended through the copilot, with both requested switches and no detected
10 ms silence holes. Three cuts retain amplitude warnings from the recorder; these are
inspection flags, and automated analysis does not establish subjective listening quality.
On-screen amounts are demonstration examples, not a provider invoice.

The commands below describe the general recorder; the published presentation was applied in a
recording-only adapter and is not the default three-Session tour.

```sh
tools/copilot-demo.sh <output directory> [en] [zh-Hant] [--phone] [--live]
```

The script builds the console through `tools/heavy.sh`, then runs
`tools/copilot-demo/record.mjs` once per language (both when none is named) and writes
`clawdline-voice-copilot-demo-<lang>.mp4`, or `<lang>-phone.mp4` with `--phone`. It needs Google Chrome (or `CHROME=<path>`) and
ffmpeg, and spends nothing. It exits 1 unless the call ran as written: `open_session`,
`get_session_recap`, `open_session`, `send_message_to_session`, and the message reached the
stand-in daemon. Watch both films before publishing them.

## A GPT-Live take

```sh
tools/copilot-demo.sh <output directory> [en] [zh-Hant] [--phone] --live
node tools/copilot-demo/record.mjs --lang zh-Hant --live --phone --script triage --person-voice cedar --dist web/console/dist --out <file.mp4>
```

`--live` records a real GPT-Live call instead of the scripted Realtime one, as
`clawdline-voice-copilot-demo-<lang>-live.mp4`. The stand-in daemon still serves the invented
Sessions, now each with a recap on the list (`recaps` in `story.mjs`, written 20 minutes ago)
and a latest one it writes when the call asks (`/v1/voice/copilot-recap`). It opens the call
itself, as `internal/transport/http/copilot_live.go` does, with `OPENAI_API_KEY` or the key
saved in Settings › Voice copilot; the key goes to OpenAI and never reaches the page. **It is
paid**: about two minutes of `gpt-live-1` at $0.05 a minute plus the backend's tokens, roughly
$0.10–0.20 a film on 2026-11-16.

`live-stand-in.js` replaces only the microphone: the person's committed lines are played into
it, with a constant silent source between them, because GPT-Live closed a session whose input
sent nothing after about 20 s (`context_injection_incomplete`). The copilot's words and voice
are the model's own; its voice is recorded from the remote track and laid under the video
with the person's lines.

Each of the person's lines waits until the copilot has answered the one before: it spoke after
the line, its voice (read from the remote track's level, since the transcript runs seconds ahead
of the sound) has been quiet for 3.5 s twice, 0.8 s apart, and no backend response, pending
continuation or tool call is outstanding. Shorter waits, or waits read from the transcript, cut
the copilot off mid-sentence in five of the first eight takes. "Yes, send it" is said only when
the message is still unsent, since GPT-Live may act on the instruction at once or answer the
Session's question instead of sending it.

What a take does is a script in `story.mjs` (`LIVE_SCRIPTS`, `--script <name>`), so it can be
revised without touching the recorder. `tour`, the default since 2026-11-17, lets the call report by itself and then opens
three Sessions that report did not cover, each in another state: Search rebuild (finished, its
story told while its conversation is read down), Offline sync (working, with news) and Release
2.4 (a long run of tests, fixes and tool output that has just stopped on a decision with
trade-offs, too much to read on a phone, so the summary is worth hearing). Opening one Session
that wanted a decision at once showed none of that. `auto` was the default once a call from the list began
reporting by itself, until `tour` replaced it: the person lets it tell the Sessions, waiting ones first and then what the
working ones are doing, and the take goes on from there as `triage` does. It surfaced a defect:
the report went to GPT-Live as one append of over 500 tokens, OpenAI refused it, and the call
said nothing; the rules and evidence now go as thinking pieces (`liveContextPieces`, at most
`LIVE_APPEND_TOKENS` = 400 estimated tokens) and the spoken note names the batch. `triage`, the first default (2026-11-16), is what a person on
a phone wants from a call, one selling point a step: "Anything need me right now?" (the copilot
picks out what waits on them), the finger opens Release 2.4 from the list (it tells that
Session's state unasked, its kept recap and then the latest, which brings the iPad failures),
"Tell it to fix the iPad tests first" (a decision made from the call), "How long have we been on,
and what has it cost so far?" (`get_call_status`), and "That's it for today. You can hang up"
(`end_call`: the copilot says goodbye and the call hangs up by itself). The take fails unless the
call ended by voice. A phone take of it ran 72-99 s after the edit.

In a GPT-Live take Release 2.4's UI suite is still running when the call starts: the
briefing, the list and its kept recap all say so. It fails on iPad at the moment the call asks for
its latest state, and the list shows its question then. The news is news: an earlier take's
briefing already held the failures, and the copilot told them twice.

`--person-voice <voice>` plays the person's lines in another voice, from
`voices/<lang>@<voice>/` (`voices.mjs --lang <lang> --person-voice <voice>` speaks them, with the
language's own instructions for the person). On 2026-11-16 the Chinese phone take was made twice,
with `ash` and with the English film's `cedar`, to compare them.

`switching`, the first desktop takes' script, is the overview and then two Sessions opened with
the mouse: after the overview the person opens Search
rebuild and then Release 2.4 from the list with the mouse, and the copilot tells each one's
state unasked, its kept recap first and then the latest. Release 2.4's kept recap is from before
its UI suite finished, so the latest one brings the iPad failures. Switching by voice
("open Search rebuild") is not shown: it took a backend round trip per switch and made the film
longer without showing more.

Every take is then edited (`tighten.mjs`, also runnable alone on a film): each stretch in
which neither voice is heard and that is longer than 1.2 s becomes 0.6 s, the opening keeps
its first 2.6 s and the close 1.6 s after the last word, and each click, and the moment a call
hangs up by itself, keeps 1.2 s before and 0.6 s after it, so the pointer is seen moving there and
the bubble is seen going away. `take.json` keeps those moments (`actions`). Picture and sound are cut together
(`trim`/`atrim` and `concat`; `select`/`aselect` cut only the picture). On 2026-11-16 this
took 134 s to 80 s in English and 184 s to 123 s in Chinese. `--no-tighten` keeps the whole
take, and `--keep` keeps it as `untrimmed.mp4`.

The copilot's voice arrives over WebRTC and can break up when the machine is busy: on
2026-11-16 a Chinese take made at a load average near 10 broke up in one answer, and the
person heard it. Every live take is checked (`audioGlitches`): a 10 ms frame gone silent between
loud ones, or a sample-to-sample jump no voice makes, is a break, and the take exits 1 naming
where (`--allow-glitches` keeps it; `take.json` lists them as `glitches`). The take that broke up
had eight; the clean takes had none to two. Record again when the load is lower.

The tour's first tap waits for both named Sessions in the first batch's transcript,
a sentence ending, and 800 ms of quiet on the actual remote audio track and transcript.
A mid-sentence pause or the end of only one summary is insufficient. Once a latest-state
note is sent, the recorder requires speech after that note and a finished turn; it no longer
skips an unspoken note after eight seconds. A turn timeout fails the take instead of advancing.
These are recording heuristics; review `take.json` and listen before accepting a film.

After a Session is opened, the person waits for its latest state too (`latestNoteAt`): the
call sends it only once the kept summary has been said, and the live model takes a few seconds to
start on it, so a phone take on 2026-11-16 played the next line over it. With nothing new the call
sends none, and the wait ends after 25 s.

The recorder closes its Chrome and removes the profile however it ends, a failure, a timeout's
SIGTERM or Ctrl-C included. Before 2026-11-16 it did so only after a take that succeeded; another
Session found 155 headless Chromes from failed takes, orphaned for up to four days and loading the
machine, which made later takes break up.

Chrome's screencast sends frames in CSS pixels whatever the emulated device scale, so the phone
film was 390 x 844 frames enlarged to 1080 x 2338 and its text broke into blocks. Chrome is now
started at the device scale (`--force-device-scale-factor`): the phone at scale 2 (780 x 1688) and
the desktop at 1600 x 900 CSS pixels and scale 1.5 (2400 x 1350), every other frame, scaled to
1080 x 2338 and 1920 x 1080, JPEG at quality 95. Scale 3 and 2 at every frame were sharper but
loaded the Chrome that also receives the copilot's voice, and take after take broke up. Frames
go to disk as they arrive (held in memory as base64, high-resolution takes hit the heap limit),
and a recorder first stops any demo Chrome a dead recorder left (a heap abort runs no handler).

A real model chooses its own tools and words, so takes differ. The recorder exits 1 unless
Release 2.4 received a message or an answer and no error was reported; it prints what the
copilot said, and `--keep <dir>` keeps `take.json` with the backend's answers, the call
journal, each line's timing and how long the copilot had been quiet before it. Watch every take.

## What is real and what stands in

| Real | Stands in |
|---|---|
| The built console in the film's language (`ui_language`), its list, Session pages and bubble | The daemon: a stand-in serves the invented story in `tools/copilot-demo/story.mjs` |
| `RealtimeCopilotClient`, its states, captions, cost and memory | OpenAI's Realtime peer: `call-stand-in.js` replaces `RTCPeerConnection` and speaks the data channel's events |
| The orb, moved by `CallMeter` from the voices it hears | The microphone: recorded voices are played into the stream `getUserMedia` returns |
| The tools, against the stand-in's `/v1/sessions`, `/v1/transcript` and `/v1/sessions/<id>/send` | The copilot's words: written in `story.mjs`, not produced by a model |

The cost row prices usage figures the stand-in makes up, so its dollars are not a real call's.
The recording browser reports no Web Push, so the sidebar's offer to set it up stays hidden,
as it is for a person who has already turned notifications on.

## The story

Six invented Sessions in two projects, written separately in each language so neither reads as
a translation: two wait on the person, two are working, two have delivered. The call:

1. The person opens the call from the Session list and asks how things are; the copilot answers
   from the machine's briefing without a tool.
2. The sheet shrinks to its bubble.
3. "Open Search rebuild and walk me through what it did": the copilot opens the finished
   Session, reads its whole conversation with `get_session_recap`, and tells it as one story
   while the conversation is on screen.
4. "Open Release 2.4": the page switches to the Session waiting on an iPad question.
5. The person tells it to fix the tests first; the copilot reads the message back, the person
   says yes, and the message appears in Release 2.4's conversation as it changes to working.
6. Goodbye, and the bubble's red mark ends the call.

## The voices

`tools/copilot-demo/voices/<lang>/<line>.ogg` are OpenAI text-to-speech (`gpt-4o-mini-tts`):
the copilot in `marin`, the voice copilot's default, and the person in `cedar` in English and
`ash` in Chinese. They are AI voices,
not recordings of people. Each language's speaking instructions and `tempo` are in `story.mjs`:
Chinese is played 1.15 times faster, because it came out at about 4.1 characters a second and
asking the model for a quicker pace made it slower. Changing a line means speaking it again:

```sh
rm tools/copilot-demo/voices/<lang>/<line>.ogg
OPENAI_API_KEY=… node tools/copilot-demo/voices.mjs --lang <lang>
```

That is one paid request per missing line, a fraction of a cent each; `--force` speaks every
line again. The recorder refuses to run while a line has no voice.

## How the recording is made

`--phone` records a 390 × 844 phone at device scale 2 with touch, as a portrait 1080 × 2338
film. A tap shows a ring where it lands instead of a pointer. The page reports itself as opened
from the Home Screen, as the console is used on an iPhone, so the sidebar does not ask to be
added there. After the sheet shrinks, a finger drags the bubble down beside the composer, and
the finished Session's conversation is scrolled by the page rather than a wheel.

Chrome runs headless at 1600 × 900 CSS pixels and device scale 1.5 through the DevTools
protocol. The screencast's frames keep their own timestamps and become a constant 30 fps H.264
video; a drawn cursor follows the dispatched mouse. The page logs when each voice starts, and the
same files are laid under the video at those moments, so the sound needs no separate sync. A
frame arrives only when the page changes, so a still page shows as fewer, longer frames.
`--keep <dir>` on `record.mjs` keeps the frames and `take.json`: each voice's start and each
tool's result.
