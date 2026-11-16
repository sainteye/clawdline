# Talk to your Sessions with the voice copilot

The voice copilot is a voice call, on a phone or a computer, that reports on your Sessions, explains
what one is doing, and acts on them when you ask. It uses an OpenAI Realtime model with your own
OpenAI API key, which stays on the machine.

## Before you start

On the machine, open **Settings** → **Voice copilot** and paste an OpenAI API key. The same card
sets the **Voice** (with **Listen** to hear it), the speaking speed, **Short answers**, **Waiting for
you to finish**, **Prepare a briefing when a call starts**, and **Extra instructions**.

A call dials at once on the **Voice system** chosen in Settings. While it rings or runs, the small **GPT-Live | Realtime API** switch at the top of the call places it again on the other system, about the same Session.

**Voice system** chooses how a call talks. **Realtime API** (the default) takes turns and uses
the model you pick. **GPT-Live** listens while it speaks, makes short listening sounds and stops
when you cut in, so it sounds more like a person on the phone. It costs about $0.05 a minute plus
a small amount each time it looks something up, and it does not keep the background work list
yet. The choice applies from the next call. Ask for "the latest" summary and the copilot uses one written within five minutes or has a new one written; otherwise it uses the one written in the background and says how long ago.

No key yet? Press the call button anyway: the call says no key is set and offers **Go to Settings ›
Voice copilot** and **Watch the demo**, a film of a call in the console's language.

## Start and end a call

Press the phone button (**Call the voice copilot**) beside the Session list's actions. The call
dials at once and rings until it connects; press **End the call** while it rings to not start it.
Allow the microphone when the browser asks. **Shrink to a bubble** keeps the call going while you
use the page; drag the bubble aside, press it to open the call again, or press its **×** to hang up.

When a call connects from the list, it begins reporting automatically. Sessions
waiting for your answer or with unresolved Notes come first. It reads their
summaries and current questions, then introduces the other Sessions one or two at
a time. Interrupt whenever you need to; ask it to continue to resume the prepared
batch. A call started inside one Session keeps its focused summary.

## What you can ask

- The state of your Sessions: all of them, the working ones, or the idle and recently finished
  ones, newest first. On **All machines** it reports every machine's Sessions.
- What one Session did and what is left, in your own words for it ("the screenshot one"). Asking
  it to open a Session opens it on the page and tells you its state.
- To answer a Session's question, send it a message, close it, or run one of its Snippets or
  Playbooks. It confirms the Session and the words once before it acts.
- To start a new Session in a Project; it asks for the machine, Project, assistant or model it does
  not know.
- A Project's Dev Stack links, read aloud or shown in a window over the page you are on.
- How to use Clawdline: it answers from this guide, and says so when the guide does not cover it.
- To speak slower or faster, or to answer shorter or longer. Both voice systems use the saved
  speaking-speed choice from the next call. GPT-Live uses four pacing prompts; its speed numbers
  identify settings rather than exact playback multipliers. During a call, ask to change the pace
  and it sends the new instructions and saves the choice for later calls.
- How long the call has lasted and what it has cost so far.
- To end the call: say so ("we're done, you can hang up"). It says a short goodbye and hangs up
  when it has finished. Asking it to close a Session does not end the call.

## Ways to ask

Open **Ways to ask** in the call window, choose a Session, then tap a complete question to send it
into the current call. **Another way to ask** shows an alternative sentence for the same request;
you can say either sentence aloud later. A call must already be connected and ready. Opening the
menu reads only the Session directory, without starting a call, reading transcripts or writing a
new recap. Selecting a question uses the connected call and its normal provider usage.

| Purpose | Example | Source |
|---|---|---|
| Current situation | Give me a quick summary | Existing recap and recent status; an explicit latest-status request uses the freshness policy |
| Progress and history | What happened since the last report? | New entries after the report cursor; whole-history questions use the separate recap path |
| Understand the details | Read the last five replies and explain the insights | Recent assistant replies plus related questions, retaining table formatting |
| My decisions | What do I need to decide? | Current questions and unresolved Notes, checked against current status |

Explicit detail questions go directly to the reply reader, without first generating another
summary. The reader pages back through at most five transcript pages and retains up to five
assistant replies, at most 8,000 JavaScript characters per reply and related question. Tool records
do not count as replies. Longer text keeps both ends, with incomplete coverage exposed to the
assistant; insufficient history and failed older-page reads are also reported. Comparison scores
and prices in a Session are source claims unless the record provides verification.

### Tool responsibilities

`get_session_summary` owns status, and `fresh: true` applies the existing five-minute freshness
policy. `get_session_updates` owns the since-last-report cursor. `explain_session_reply` owns both
single and multiple recent reply explanations (`replies: 1..5`). `get_session_activity` remains a
compact execution chronology for process questions. `get_session_recap` remains the whole-history
overview, and `get_status_briefing` the existing prepared multi-Session briefing. Do not combine
these paths for one question without a missing source that requires it. No startup read is added;
existing opening and recap policies remain.

A menu click verifies machine and conversation identity again; the subsequent read rejects a
replaced Session. A click is a conversational question, not mutation consent. Realtime uses a user
text message and its existing narrator; GPT-Live receives the selected question through application
commentary and delegation. Local transport tests establish neither live model tool selection nor
spoken/audio acceptance or measured startup latency.

### Proposed question-focused analysis

The next design uses the user's question and recent original messages as input to a small text
model. It returns a question-specific answer, source references, unknowns and optional requests for
older sections, which the voice assistant explains. General status uses cached recaps; explicit
explanation requests bypass recap generation. Follow-ups reuse the selected conversation and read
window when valid; new content invalidates cached analysis. A summary-sufficiency decision must not
add an extra model roundtrip before every status question: use the selected menu intent or the
existing voice/backend intent decision. Source text is untrusted data, not tool instructions.

GPT-Live already delegates tool use and reasoning to a text backend, so first evaluate whether that
existing backend can do the focused extraction without another model hop. A separate reusable
analysis service would give Realtime the same behavior but is not implemented by the menu change.
Its acceptance must include question relevance, faithful evidence, bounded older reads, revision
isolation, interruption and latency/cost on both engines. No model/pricing or live-quality claim is
made here. This paragraph is a proposal, not a deployed capability.

## Session recaps

With **Session recaps** on (**Settings** → **Voice copilot**), each Session's own assistant reads its
whole conversation from a throwaway copy and writes, in the console's language, one or two plain
sentences — what it is working on, where it got to, and whose move it is — and a fuller account for
the copilot: what it has finished so far and every decision it needs from you. Ask "what has this
Session finished, and what do I need to decide?" or "report this Session's summary" and the copilot
answers from it. **Session info** shows the short recap under **Status**. The conversation itself is
never changed, and the copy is not kept.

- **Written ahead**: about three minutes after a Session stops, and every ten minutes while it
  works. The copilot answers at once.
- **When the copilot asks**: nothing is spent until you ask; then each Session takes about eight
  seconds, three at a time. A Session that has said nothing since its last recap reuses it.

Each recap uses that Session's own Claude Code or Codex account: Claude answers with Haiku, about
one US cent each. Other assistants are not recapped.

## What it shows

Each thing the machine confirmed — a message sent, a question answered, a Session closed — shows a
small green notice and plays a short chime. If the copilot says it did something the machine did
not confirm, an amber notice says so and the copilot corrects itself. Under the conversation, small
chips list the tools it used for the answer: **✓** done, **?** asks you to choose, **⋯** reads each
one, **✗** failed.

The call's cost so far is shown in the call. Each call's transcript and tool use are kept on the
machine for 14 days, so a call that went wrong can be looked into.
