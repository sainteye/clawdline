<!-- clawdline-doc: kind=reference audience=both -->

# The voice copilot demo film

A continuous screen recording of the built React console making a voice copilot call, in English
and in Taiwan Traditional Chinese, about a minute and a half each at 1920 × 1080. It is the
console as it is built, not drawn screens: the call sheet, the cloud orb and its meters, the
captions, the cost and memory rows, the bubble, and the copilot's own tools all run unchanged.

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

The take shows a call that follows the screen: after the overview the person opens Search
rebuild and then Release 2.4 from the list with the mouse, and the copilot tells each one's
state unasked, its kept recap first and then the latest. Release 2.4's kept recap is from before
its UI suite finished, so the latest one brings the iPad failures. Switching by voice
("open Search rebuild") is not shown: it took a backend round trip per switch and made the film
longer without showing more.

Every take is then edited (`tighten.mjs`, also runnable alone on a film): each stretch in
which neither voice is heard and that is longer than 1.2 s becomes 0.6 s, the opening keeps
its first 2.6 s and the close 1.6 s after the last word, and each click keeps 1.2 s before and
0.6 s after it so the pointer is seen moving there. Picture and sound are cut together
(`trim`/`atrim` and `concat`; `select`/`aselect` cut only the picture). On 2026-11-16 this
took 134 s to 80 s in English and 184 s to 123 s in Chinese. `--no-tighten` keeps the whole
take, and `--keep` keeps it as `untrimmed.mp4`.

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

`--phone` records a 390 × 844 phone at device scale 3 with touch, as a portrait 1080 × 2338
film. A tap shows a ring where it lands instead of a pointer. The page reports itself as opened
from the Home Screen, as the console is used on an iPhone, so the sidebar does not ask to be
added there. After the sheet shrinks, a finger drags the bubble down beside the composer, and
the finished Session's conversation is scrolled by the page rather than a wheel.

Chrome runs headless at 1600 × 900 CSS pixels and device scale 1.2 through the DevTools
protocol. The screencast's frames keep their own timestamps and become a constant 30 fps H.264
video; a drawn cursor follows the dispatched mouse. The page logs when each voice starts, and the
same files are laid under the video at those moments, so the sound needs no separate sync. A
frame arrives only when the page changes, so a still page shows as fewer, longer frames.
`--keep <dir>` on `record.mjs` keeps the frames and `take.json`: each voice's start and each
tool's result.
