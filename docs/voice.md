<!-- clawdline-doc: kind=reference audience=both -->

# Voice input settings

Settings › Voice input has three cards: **Recognition**, **LLM cleanup** and **Personal
vocabulary**. Recognition is the local Whisper setup described in
[recording recovery](voice-recording-recovery.md) and the [Whisper record](whisper.md). This page
covers the other two.

## LLM cleanup

Whisper's Chinese punctuation is sparse and uneven, most of all when someone speaks a long sentence
without pausing. The initial prompt that seeds punctuation does not fix it: on 2026-11-11, seven
synthetic Chinese and mixed-language clips came out with identical punctuation with and without the
seed, and this machine, which always runs with the seed, still showed the inconsistency.

With **LLM cleanup** on, the daemon sends each transcript to a small model for one tool-less turn
after Whisper (`internal/adapters/planner/polish.go`). The model adds punctuation, corrects a
mis-heard word only when the context makes it certain, and, at the default level, removes filler
words. It does not translate, summarize or answer. Chinese stays in the
script the transcript is already in (`whisper.Plan.Script`): Traditional, Simplified, or, when
nothing chose one, whatever characters Whisper wrote.

- **Off unless set** (`voice_polish`). With it off, nothing about a recording leaves the machine.
  With it on, the transcript and the vocabulary go to the chosen assistant's provider, through the
  CLI that is already signed in on this machine. No API key is involved.
- **Assistant** (`voice_polish_assistant`): `claude` runs Claude Haiku; `codex` runs Codex's small
  model. Measured on 2026-11-11 with the product's own flags, Haiku took 3.1–4.3 s for one sentence
  and Codex took 5.1–7.7 s; Settings quotes these. (A first prototype without those flags measured
  2.0–3.1 s and 5.3–10.5 s over twelve transcripts.) A browser-side wait of 120 s covers either, and
  the turn runs after Whisper hands back its queue slot, so a cleanup never makes the next
  recording wait.
- **Level** (`voice_polish_level`): `punctuation` keeps every word; `tidy` (the default) also
  removes filler words.

A cleanup is discarded and Whisper's text is used whenever the turn is slow (15 s), the
assistant is not installed or out of usage, the transcript is very long, or the answer changed the
words. "Changed the words" is `planner.Faithful`: ignoring punctuation, spaces and case, at most
20% of the characters may differ at the `punctuation` level and 35% at `tidy`. In the 2026-11-11
measurement Haiku once dropped a three-word phrase; that kind of deletion stays under the ceiling,
while dropping a clause, answering the dictation or translating it does not.

The answer from `POST /v1/voice` carries `polish.status` (`applied`, `unchanged`, `unavailable`,
`unfaithful`, `too_long`, `quota`, `timeout` or `failed`). When the cleanup replaced the text,
`raw` holds Whisper's own words. The composer shows a tag over the box: **Cleaned up by LLM ·
Show original** swaps the dictated words back and forth while they are still in the box unedited;
a fallback says why the original was used. The daemon log records the status, the duration and
both lengths, never the words.

Dictated text is inert content in the prompt. Both CLIs run without tools, MCP servers, slash
commands or saved sessions, so a sentence such as "commit and push to main" is only punctuated.

## Personal vocabulary

- **Your terms** (`voice_vocabulary`): at most 200 distinct one-line terms of at most 64
  characters. They are repaired in every transcript after Whisper (`whisper.ApplyVocabulary`) and,
  with cleanup on, shown to the model.
- **Project names** (`voice_vocabulary_projects`, off unless ticked): the folder names of the
  forty places the project picker lists. In Clawdline a project's name is its folder's name, which
  is normally the repository's name. The card lists exactly which names a ticked box adds, because
  with cleanup on they are sent too.
- **Built in**: `whisper.AlwaysExpected` (Clawdline, Claude Code and the rest), read-only.

## From a phone

The two cards read and write their own narrow route, `/v1/settings/voice`: the five keys above,
plus `polish_assistants`, `vocabulary_builtin` and `vocabulary_projects`, and refusing every other
setting. A paired Cloud console carries it as `voice-settings` and `voice-settings-update`, so
app.clawdline.com shows the same cards under Settings › Voice input for the machine selected in
its header, and names that machine (`machine` in the answer), because a phone can switch between
several. Switching machines reads the cards again. A recording made in a Session is transcribed
by that Session's machine, so its cleanup follows that machine's settings. Changing them from a remote
device needs Send, as the default models do. The Recognition card (language, model) stays on the
machine's own console. A machine from before API level 43 answers that it needs an update, and the
cards say so instead of drawing controls that cannot save.

The bounds are registered in `internal/domain/capacity` as `voice.vocabulary_terms`,
`voice.vocabulary_term_characters`, `voice.vocabulary_project_terms`, `voice.polish_terms`,
`voice.polish_characters` and `voice.polish_timeout_milliseconds`.

## Voice copilot

The phone button in the Session list's toolbar opens a voice call with OpenAI Realtime that
reports on this machine's Sessions. The call can shrink to a draggable bubble that stays above
every page; its red mark ends it.

**What the machine keeps.** Settings › Voice copilot writes `<state>/copilot.json` (mode 0600)
through `/v1/settings/copilot`: the OpenAI API key, the model (`gpt-realtime-mini` by default,
or `gpt-realtime`), semantic VAD's eagerness (`low` by default, which waits through pauses
mid-sentence), the person's extra instructions, and whether and with which assistant a briefing
is prepared. No answer carries the key, only `key_set` and its last four characters. A paired
device needs Send to change these, as for the voice cards.

**How a call starts.** The browser asks `POST /v1/voice/copilot-session`. The machine mints a
Realtime client secret with its key (`POST /v1/realtime/client_secrets`, 600 seconds) and
answers it with the settings; the browser opens the call with that secret, so the key never
reaches a phone or Cloud. Meanwhile the browser reads the Session list into the instructions,
so the first question needs no tool, and asks `POST /v1/voice/copilot-briefing`.

**The briefing.** The machine reads its own Sessions through the handlers `/v1/sessions` and
`/v1/transcript` answer with (at most 10, most urgent first: waiting, working, delivered, idle),
keeps each one's waiting question, the person's recent asks, the summaries it recorded with
`clawdline session report --summary` and its last reply (at most 1,500 characters, keeping its
beginning and its end, where a question for the person usually is), and has the chosen assistant write one
spoken briefing in order of urgency in a single tool-less turn (`planner.Briefing`, the same CLI
invocation as LLM cleanup). On 2026-11-12, over three Sessions, Codex took 7.9 s and Claude Haiku
14.4 s. The first request starts it in the background and waits at most 10 s; later requests are
answered `pending` until it is ready, and a briefing is handed out again for 60 s. When it
arrives, the browser sends it to the call in a second `session.update`; until then the
`get_status_briefing` tool waits for it, and without it the copilot reads the Sessions itself.
A last reply that ends by asking the person something counts as needing them even when the
Session is idle: such a question is prose, not a menu, so the Session's state does not show it.

**What the copilot can read and do.** `list_sessions`, `get_session_summary`, `get_session_recap`
(the whole conversation, paged back at most 5 pages), `get_session_activity`,
`explain_session_reply`, `get_session_screen`, `open_session`, `answer_session_question`, `get_session_notes` and
`send_message_to_session`. `answer_session_question` presses one option of a waiting Session's
question only after the person names it; the press carries the question's fingerprint, so the
machine refuses it if the screen asks something else by then. Over Cloud it resolves that
Session's current execution as a newly started Session's is. `get_session_notes` reads the unresolved
notes agents left for the person (secret handoffs excepted) and does not reply to them. `open_session` opens the
Session on the page the call runs in, whichever browser that is: the local console opens it as a
press in the list would, and on Cloud the reading machine's current execution of it is resolved
first, as for a started Session (`realtime/copilot-open.ts`).

The other reads keep 600 characters of a reply, which loses the reasoning of a long diagnosis.
`explain_session_reply` hands over the last reply whole (at most 8,000 characters, keeping its
beginning and end) with the person's ask it answers, and the instructions have the copilot
explain it at length: the problem, each cause in plain words, the proposed fix, and what the
person is asked to decide with each choice's cost.

**Speed and length.** `audio.output.speed` is the machine's choice of 0.8, 0.9 (the default),
1.0 or 1.1; it slows the generated audio, and the instructions also ask for a pause between
sentences. Ordinary answers stay at three sentences while **Short answers** is on (the default; off lets them
run in full); a report on Sessions goes through each one in
two or three (its goal, what it delivered recently, what it is doing now), from each busy row's
`goal` (the person's newest ask), `recent` (its last two delivery summaries) and `latest`. On
2026-11-12 "how are the working Sessions" was answered "four Sessions are working" and nothing
else until that exception was stated first.

**The voice.** One of Realtime's ten voices, `marin` unless chosen otherwise; a call cannot change
its voice once it has spoken, so a new choice applies from the next call. **Listen** plays one line
in it over a receive-only connection, with no microphone. Over Cloud the copilot is opened inside one
Session's page, which pins Session content to that Session; its reads carry
`X-Clawdline-Unopened-Read: 1`, and the seam resolves and verifies the named Session's current
execution instead (`cloud/unopened-read.ts`). Settings › Voice copilot shows the instructions and
every tool's description exactly as a call sends them.

**What the browser keeps.** The lines both sides spoke in earlier calls (at most 24, three days),
for the next call's instructions, and this call's cost, priced from each `response.done` usage at
OpenAI's listed rates; Whisper captions are billed per minute and are not included.

The bounds are registered as `voice.copilot_key_bytes`,
`voice.copilot_extra_instructions_characters`, `voice.copilot_briefing_timeout_milliseconds`,
`voice.copilot_briefing_input_characters`, `voice.copilot_briefing_characters`,
`voice.copilot_briefing_reply_characters`, `console.copilot_reply_characters`,
`console.copilot_memory_turns`, `console.copilot_list_transcript_reads`,
`console.copilot_recap_pages` and `console.copilot_screen_reads`.
