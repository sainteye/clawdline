# Talk to your Sessions with the voice copilot

The voice copilot is a voice call, on a phone or a computer, that reports on your Sessions, explains
what one is doing, and acts on them when you ask. It uses an OpenAI Realtime model with your own
OpenAI API key, which stays on the machine.

## Before you start

On the machine, open **Settings** → **Voice copilot** and paste an OpenAI API key. The same card
sets the **Voice** (with **Listen** to hear it), the speaking speed, **Short answers**, **Waiting for
you to finish**, **Prepare a briefing when a call starts**, and **Extra instructions**.

## Start and end a call

Press the phone button (**Call the voice copilot**) beside the Session list's actions. The call
dials at once and rings until it connects; press **End the call** while it rings to not start it.
Allow the microphone when the browser asks. **Shrink to a bubble** keeps the call going while you
use the page; drag the bubble aside, press it to open the call again, or press its **×** to hang up.

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
- To speak slower or faster, or to answer shorter or longer.

## What it shows

Each thing the machine confirmed — a message sent, a question answered, a Session closed — shows a
small green notice and plays a short chime. If the copilot says it did something the machine did
not confirm, an amber notice says so and the copilot corrects itself. Under the conversation, small
chips list the tools it used for the answer: **✓** done, **?** asks you to choose, **⋯** reads each
one, **✗** failed.

The call's cost so far is shown in the call. Each call's transcript and tool use are kept on the
machine for 14 days, so a call that went wrong can be looked into.
