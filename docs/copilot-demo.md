<!-- clawdline-doc: kind=reference audience=both -->

# The voice copilot demo film

A continuous screen recording of the built React console making a voice copilot call, in English
and in Taiwan Traditional Chinese, about a minute and a half each at 1920 × 1080. It is the
console as it is built, not drawn screens: the call sheet, the cloud orb and its meters, the
captions, the cost and memory rows, the bubble, and the copilot's own tools all run unchanged.

```sh
tools/copilot-demo.sh <output directory> [en] [zh-Hant]
```

The script builds the console through `tools/heavy.sh`, then runs
`tools/copilot-demo/record.mjs` once per language (both when none is named) and writes
`clawdline-voice-copilot-demo-<lang>.mp4`. It needs Google Chrome (or `CHROME=<path>`) and
ffmpeg, and spends nothing. It exits 1 unless the call ran as written: `open_session`,
`get_session_recap`, `open_session`, `send_message_to_session`, and the message reached the
stand-in daemon. Watch both films before publishing them.

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
the copilot in `marin`, the voice copilot's default, the person in `cedar`. They are AI voices,
not recordings of people. Changing a line in `story.mjs` means speaking it again:

```sh
rm tools/copilot-demo/voices/<lang>/<line>.ogg
OPENAI_API_KEY=… node tools/copilot-demo/voices.mjs --lang <lang>
```

That is one paid request per missing line, a fraction of a cent each; `--force` speaks every
line again. The recorder refuses to run while a line has no voice.

## How the recording is made

Chrome runs headless at 1600 × 900 CSS pixels and device scale 1.2 through the DevTools
protocol. The screencast's frames keep their own timestamps and become a constant 30 fps H.264
video; a drawn cursor follows the dispatched mouse. The page logs when each voice starts, and the
same files are laid under the video at those moments, so the sound needs no separate sync. A
frame arrives only when the page changes, so a still page shows as fewer, longer frames.
`--keep <dir>` on `record.mjs` keeps the frames and `take.json`: each voice's start and each
tool's result.
