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

**Reading a live call.** The sheet reserves a stable, scrollable region for the latest exchange.
Incoming captions, tool chips, notices and cost updates do not resize or move it. New captions
follow the bottom while the person is there; scrolling back pauses following. The smaller orb
can be dragged vertically to resize it (or adjusted with arrow keys), giving space back to the
conversation. Cost and remembered-call information sit below as unboxed secondary text; the
pricing explanation opens from the cost line. Tool chips keep their own scrollable space, and
opening the execution log does not displace the conversation.

**Assistant workspace.** The sidebar Voice assistant page owns the persistent inbox, work list and copilot settings. Background planning is opt-in; its execution and verification boundaries are documented in [voice-assistant-workspace.md](voice-assistant-workspace.md). Dictation settings remain in Settings.

**What the machine keeps.** Voice assistant › Settings writes `<state>/copilot.json` (mode 0600)
through `/v1/settings/copilot`: the OpenAI API key, the model (`gpt-realtime-mini` by default,
or `gpt-realtime`), semantic VAD's eagerness (`low` by default, which waits through pauses
mid-sentence), the person's extra instructions, and whether and with which assistant a briefing
is prepared. No answer carries the key, only `key_set` and its last four characters. A paired
device needs Send to change these, as for the voice cards.

**How a call starts.** The browser asks `POST /v1/voice/copilot-session`. The machine mints a
Realtime client secret with its key (`POST /v1/realtime/client_secrets`, 600 seconds) and
answers it with the settings; the browser opens the call with that secret, so the key never
reaches a phone or Cloud. Meanwhile the browser reads the Session list into the instructions,
so the first question needs no tool. The directory does not fetch transcripts. Legacy calls may also ask `POST /v1/voice/copilot-briefing`; managed calls use the work pipeline instead, avoiding a duplicate briefing.

The call button dials at once: the sheet opens ringing (a 440 and 480 Hz ringback, one second
on and two off, on the audio context the press opened) until the call connects, fails or is
hung up. A hang-up while it rings ends that attempt at its next step, so no call starts after
it (`start` in `realtime/realtime-copilot.ts`).

Over Cloud the call pass is asked only of machines that offer it, as dictation is asked only of
a machine that transcribes: the reading machine first, then each current machine whose
descriptor lists `copilot-session`. A machine without the word or without an OpenAI key passes
the request on; the machine that answers also prepares the briefing (`copilotSession` in
`cloud/relay-writer.ts`). The seam's refusals for this word are flat (`{error, detail}`), and the
page reads both spellings (`copilotRefusal`), so a refusal shows its own code. Before this a
phone saw only "HTTP 501".

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
Each briefed Session with Board steps or direct to-dos also carries `progress`, its counts read
when the briefing is prepared, and the briefing takes any count from it rather than from a
delivery.

**What a Session has done.** `get_session_recap` first asks the machine that holds the Session,
`POST /v1/voice/copilot-recap` (Cloud word `copilot-recap`, API level 59). That machine reads the
Session's whole record (at most 40 transcript pages): every request, every reply and every
`clawdline session report` summary, each with when it happened (each line at most 1,200
characters, both ends kept; at most 60,000 characters in all, the oldest lines after the first
request left out first). It also counts the Session's Board steps as the Board does (cancelled
steps are not steps) and its direct to-dos, and asks `gpt-4.1-mini` on the voice copilot key for
one spoken recap whose counts come only from that `progress`. The call says the recap as written.
Without a key, on an older daemon, or after 25 s, the call reads the record itself as before, now
handed the same `progress` (read from `/v1/work/v2/session-todos`), each delivery with its `when`,
and the last reply's `last_reply_when`; `get_session_summary` carries `progress` too.

On 2026-11-13 a person asked what one Session had done and was told 41 of 56 steps were finished
with 15 left; the Board said 46 and 10. The recap then read only the requests, the report lines
and the one last reply, from five pages: the newer count was in an ordinary reply between reports,
and the one report found was an hour old with nothing saying so. Recording progress while a
Session works was not chosen: it costs every turn of every Session, and a record the Session must
remember to write lags exactly as the reports did. The Board's counts are already kept as the
work happens, and the record is read whole only when someone asks.

**What the copilot can read and do.** `list_sessions`, `get_session_summary`, `get_session_recap`
(the whole conversation, paged back at most 5 pages), `get_session_activity`,
`explain_session_reply`, `get_session_screen`, `open_session`, `answer_session_question`, `get_session_notes` and
`send_message_to_session`. `answer_session_question` presses one option of a waiting Session's
question only after the person names it; the press carries the question's fingerprint, so the
machine refuses it if the screen asks something else by then. Over Cloud it resolves that
Session's current execution as a newly started Session's is. **Starting a Session.** `list_machines` names the machines the page offers (on Cloud, CloudGate
answers with each machine's freshness; the local console has its own only) and carries the
current machine's projects, installed assistants and their default and offered models
(`/v1/places`, `/v1/settings/default-models`); `list_projects` reads another machine's with
`?machine=`. `start_session` posts `/v1/places/<id>/start/<assistant>[/<model>]` only after the
copilot has asked for whatever of machine, project, assistant and model it did not know, one at a
time, and read the whole choice back; the project it names must be that id's. `open_session` waits
up to five seconds for a just-started Session to reach the list. On 2026-11-12, before the
projects came with the machine list, the copilot asked "專案A 還是專案B"; with them it named real
projects and started codex in clawdline.

**Writes go to the Session under discussion.** `send_message_to_session`,
`answer_session_question` and `close_session` accept only a Session's id or its whole name (never
an assistant type or part of a name), and require `session_label`, the
name the copilot said aloud, which must be that Session's. The call remembers the Session it last
read, opened or acted on; a write to another one is refused until the person confirms the switch
(`switch_confirmed`). On 2026-11-12 a message went to the wrong Session; replayed against this
machine's Sessions, the copilot asked to send to an unrelated one when the named Session had been
closed, and after the rule "a Session not in the list is said to be missing, never replaced" it
said so instead. A call placed from a Session on 所有機器 is first told the raw id the page gave
(`%2`), while the tools list that Session as `fleet:<machine>:%252`. In call `eae13140`
(2026-11-13) the copilot sent "push to GitHub and deploy" with `%2` twice, both refused, and
asked the person to confirm three times before a send by whole name went through. Once the
opening handover finds the listed row, the instructions are resent with its id, and a write or
read naming the raw id of the Session under discussion resolves to that row; a raw id never
selects another machine's Session of the same id. `close_session` closes a Session the person named and confirmed, with `force:
false` and that reading's version when its row's closeability is `safe`. When it is not, or the
machine answers 409 with reasons, the copilot says the reasons and asks again whether to close
anyway, as the console's close sheet does (`overlays/action-confirm.ts`); only the person's
explicit yes (`force_confirmed`) sends `force: true`, pinned to the version those reasons came
from, and the machine still decides.

**Smart naming.** Asked which Sessions' names no longer match their work, the copilot reads
`get_sessions_summary`, says which ones it would rename and why, and asks once. On a yes,
`smart_rename_sessions` (with `confirmed: true`) posts each named Session to
`POST /v1/sessions/{id}/smart-title`, the route behind the console's Smart naming, one after
another with its own Idempotency-Key, and says each old name and the title the machine chose; a
refusal (no first request yet, naming assistant missing or out of usage) is said for that Session
and the rest go on. At most 6 Sessions per call (`console.copilot_smart_rename_sessions`), since
each spends one small model turn. A rename joins the actions the copilot may only claim once the
machine confirmed it. Before this tool the copilot had no way to rename and, asked to, did not know
what smart naming was.

In the first call after it shipped (2026-11-13) the copilot still read the guide when it heard
"smart naming", the rule for any question about Clawdline, and then typed "please use smart naming
to rename this Session" into the Session, which tried to drive the app's window. The instructions
now say a request to do something a tool does is done with that tool, and `send_message_to_session`
refuses a message asking a Session to rename itself (`ASKS_A_RENAME`), pointing at
`smart_rename_sessions`. Each call's journal now starts with its bundle and tool count, since that
call could not be told apart from one placed by a page loaded before the tool existed. A Session
asked directly has `clawdline session smart-title`, documented in the agent guide (§7). It posts
to `/v1/orchestrator/sessions/{terminal}/smart-title`, the same naming turn behind the orchestrator
token: the paired-device route answered that token 403, which the first real run of the command
showed and its stand-in test had not.

**How it names and explains Sessions.** A Session's label is often its first request and no
longer says what it is doing. By default the copilot calls each Session by a plain name it gives
it from the work (a few words a non-programmer would follow), keeps that name for the call, and
says the original label only when the person asks or a send, answer, close or rename needs the
target confirmed; tools still take the original label. A status answer gives each Session one
sentence on what it is for and two to four everyday sentences on where it stands, without
counting them off or reading technical words. The person asked for this in a call on 2026-11-13
("don't mind their names; name them as you would, then tell me plainly, with rise and fall") and
wanted it from the start of every call; `copilot-speech.ts` carries the same in English for the
work-list reporter.

**What happened since the last report.** The Session open on screen is the one under discussion:
while a call is on, opening another Session (the address's `#session=`, on the console and on
Cloud alike) makes it the focus and tells the call in a system item that asks for no reply.
`get_session_updates` (no name: that Session) reads the transcript back from the newest page, at
most 3 pages (`console.copilot_update_pages`), to the entry the last report ended on, and hands
over everything after it; with no earlier report it gives the recent part, and further back than
it reads it says so. A report mark is the entry's id (or its time and first words), kept per
Session in this browser (`console.copilot_update_marks`, the newest 100), and a recap told to the
person sets it too. Marks are not cursors: Cloud does not carry `after=`, so the same reading
works on a phone. The person asked for this on 2026-11-13: the copilot could read only a summary
or the last reply, not what came between two reports.

**A group of Sessions.** `get_sessions_summary` reads the working ones (with those waiting on the
person), the idle and lately finished ones, or all, newest activity first, each with its goal,
what it finished lately and what it is doing; only the newest `LIST_LATEST_READS` are read and
the rest are counted. A sentence about several Sessions at once ("最近做完的告訴我他們做的事情")
is marked `many` by the Decision API and sent there, never put back as "which one?". On
2026-11-12 such a question was answered with three candidates to choose from; live, five group
questions then chose the right group each time.

**The microphone is asked for in the tap.** iOS Safari refused `getUserMedia` (NotAllowedError)
when the call asked only after its pass came back over Cloud, seconds after the press; it is
now asked before the first await, beside the pass, and released when the call does not start.
A refusal is said by its reason (`microphoneProblem`), not by the browser's sentence.

**How Clawdline is used, and a Project's links.** `clawdline_help` answers from the user guide
(`docs/user/*.md`, bundled as its own chunk and loaded the first time a call asks): it first hands
the copilot the guide's contents, one id per section, and the copilot picks up to three sections
by meaning, then reads them (`copilot-help.ts`). On 2026-11-12 a word match of a Chinese question
against the mostly English guide picked unrelated sections, and offered beside the contents it was
picked over the right one; it is not offered. The guide then gained the sections it was asked for
and lacked: archiving a Session, every machine's Sessions, and the voice copilot itself.
`get_devstack_links` reads a Project's Dev Stack links by its whole name and, with `open`, opens
that Project's Dev Stack links in a modal over the page the person is on (`DevStackModal`, held at
the page's root with the copilot's bubble, `COPILOT_DEVSTACK`). It first switched to the Projects
page and its setup dialog; the person asked to see the links without leaving their page.

**A demo for a person without a key.** When the call start answers `copilot_key_missing`, the
sheet offers **Watch the demo** beside Settings: a modal film of a call from `media.clawdline.com`
(R2 behind its CDN, published by the site's `scripts/publish-media.py`), in the console's language,
with a phone's portrait cut on a narrow screen; only the poster loads when it opens, and the film
when play is pressed (`CopilotDemoFilm.tsx`, `copilot-demo-films.ts`).

**Every machine.** On a page showing every machine (所有機器) the call reads the page's own list
(`COPILOT_FLEET`, CloudGate): each machine's Sessions, with tool IDs made from separately
URI-encoded stable machine and Session IDs (`fleet:machine-a:%252` for `%2` on `machine-a`).
Machine names remain in display labels; duplicate names and renames do not change identity.
Selectors refuse repeated IDs instead of choosing the first row. Every read and
write of one Session names its machine (`X-Clawdline-Unopened-Machine`), so the seam resolves its
current execution there; the Decision API is handed that list; and the one-machine briefing is
skipped. On 2026-11-12, read from the journal, a phone on 所有機器 heard about one Linux Session
only, because `/v1/sessions` over Cloud is the reading machine's list, and the briefing it
waited for was this Mac's. A pending briefing reaches a phone as 200 with `pending`, and is now
waited for rather than called failed; the remembered call's greeting no longer brings up an
earlier topic, which it had invented ("上次提到的會議議程"). Snippets and Notes of a Session on
another machine are still read from the reading machine.

**The call journal.** What a call heard (the transcribed sentence), said, and did (each tool, its
arguments, outcome, time and the start of its answer), with its errors and notices, is handed a
turn at a time to the machine that gave the call its pass (`POST /v1/voice/copilot-journal`,
Cloud word `copilot-journal`, API level 47). The machine appends it to
`<state>/copilot-calls/<date>.jsonl` (mode 0600) and keeps 14 days. On 2026-11-12 the person
said a call "did not understand anything"; the turns were only in the phone's browser, and the
machine's log showed only that no tool had been called.

**What the person sees the copilot do.** Every send, answer, close, Snippet or Playbook run and
new Session the machine confirmed shows a small notice in the sheet and above the bubble for five
seconds, with a short two-note chime (`actionNotice`, `CopilotNotice.tsx`); a refusal never does.
When the copilot says it did one of these ("已經幫你送過去了") and the machine confirmed none since
the person last spoke, an amber notice says so and the copilot is told to correct itself and ask
again (`unbackedClaims`). On 2026-11-12 it told the person a message was sent without calling the
send; only the second ask sent it, and nothing on screen told the two apart. A Session's own work
reported in the third person ("它已經發布了") is not a claim.

**Which Session the person means is the copilot's call.** The read tools take an id or a whole
name; anything else answers with every Session (id, name, state) and the copilot picks the id
from what the person said. On 2026-11-12 the person asked for "Task 或 TimeTree 相關的" Session;
the label-substring match found nothing for "[Task] Foyer 每日 TimeTree 日程整理" and the copilot
said it did not exist. Matching what was meant is left to the model rather than to a looser rule
in this code.

The voice model alone still read the wrong Session, or none, too often: across eleven spoken
descriptions of this machine's Sessions on 2026-11-12, gpt-realtime-mini read the right one for 7
(gpt-realtime, about four times the price, for 8). So a read that names a Session by neither id
nor whole name, and every read the person described in words, also asks the **Decision API**:
`POST /v1/voice/copilot-decide` with the person's transcribed sentence (`heard`, never the voice
model's paraphrase, which handed over its own pick of a label). The machine that gave the call its
pass reads its own Session list and asks `gpt-4.1-mini` on its key for one id, the two or three it
cannot tell apart, or neither (`copilot_decide.go`). Only an id from that list is used; a lone
candidate is the Session; a sentence that points nowhere ("它"), a refusal or a slow answer leave
the voice model's own choice standing (`resolveRead`). Writes never take a decision. With it, 10
of the 11 descriptions read the right Session, at a median of 0.7 s per decision. The sentence is
bounded (`voice.copilot_decision_said_characters`) and so is the wait
(`voice.copilot_decision_timeout_milliseconds`). Over Cloud the close resolves that
Session's current execution as `answer_session_question` does. Times reach the copilot already
said as a person says them (剛剛, 12 分鐘前, 3 小時前, 昨天 18:20, 11/9 14:30; 1 小時 20 分 for a
duration), in the briefing's input too, never as a count of minutes. `get_session_notes` reads the unresolved
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
sentences. The Session summary names only the kinds it has (no "0 個已完成"). Ordinary answers stay at three sentences while **Short answers** is on (the default; off lets them
run in full); a report on Sessions goes through each one in
two or three (its goal, what it delivered recently, what it is doing now), using the
requested summaries and actual delivery evidence. Directory listing does not read
every busy Session's transcript or infer its contents from its name. On
2026-11-12 "how are the working Sessions" was answered "four Sessions are working" and nothing
else until that exception was stated first.

**Changing them in the call.** `set_speaking_speed` (one step slower or faster, normal, or a rate)
and `set_answer_length` apply at once with a `session.update` carrying only the rebuilt
instructions and `audio.output.speed`, and save the choice on the machine for the next calls. On
2026-11-12 a call that had already spoken accepted speed 0.8 and the long-answer instructions, and a
resent unchanged voice was accepted too; the briefing's later update now also sends only the
instructions and speed.

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
`console.copilot_smart_rename_sessions`,
`console.copilot_memory_turns`, `console.copilot_list_transcript_reads`,
`console.copilot_recap_pages` and `console.copilot_screen_reads`.

A demo film of a call, recorded from the built console in English and Traditional Chinese, is
[the voice copilot demo](copilot-demo.md).


### Dispatched children

A Session list carries dispatched children beside their roots. The copilot's tools tell them apart the way the console's list nests them: from the machine's dispatch records (`/v1/orchestrator/tasks`, `child.terminalId` and `root.terminalId`, read through `ToolDeps.tasks`) and from `parent_session_id` on a Cloud status row. `list_sessions` counts children separately (`children`, with the main Session each helps, and `children_note`), every group report leaves them out, and the copilot reads one only when the person asks about it. Without readable records every row is a Session, as before.

### GPT-Live calls

Voice assistant › Settings › **Voice system** (`engine` in `copilot.json`: `realtime` by default,
or `live`) chooses the engine for the next call. A GPT-Live call (`realtime/copilot-live.ts`)
differs from a Realtime call in four ways; its tools, Session targeting and confirmation rules
are the same `runCopilotTool` a Realtime call runs.

- **Who opens it.** GPT-Live has no client secret: the server holding the key creates the
  session. The browser gathers a complete WebRTC offer and posts it with the live model's short
  instructions to `POST /v1/voice/copilot-live` (Cloud word `copilot-live`, API level 67); the
  machine posts `{session, transport: {type: "webrtc", sdp}}` to OpenAI's `/v1/live/sessions`
  with its key and answers only the SDP answer and the session id (`copilot_live.go`). The key
  never leaves the machine. Bounds: `voice.copilot_live_offer_bytes`,
  `voice.copilot_live_instructions_bytes`, `voice.copilot_live_body_bytes`.
- **Two models.** `gpt-live-1` speaks and listens at once with a persona, backchannel,
  interruption and delegation policy (`liveInstructions`). It hands every Session question and
  action to a Responses backend, `gpt-6-luna`, whose instructions are the Realtime call's whole
  tool rules plus the snapshot, memory, briefing and focus, and whose tools are `COPILOT_TOOLS`
  (`liveBackendUpdate`, sent by `session.update` after `session.started` and again when the
  focus, briefing or answer length changes). Tools run one at a time (`parallel_tool_calls: false`).
- **Tool results.** A finished backend `function_call` arrives inside a `response.event`
  envelope; the browser runs it and returns `response.item.create`, then `response.create` once
  nothing is pending.
- **Automatic list reports.** After configuration, a call from the list reads
  current Session summaries, questions and unresolved Notes. Person-owned waits
  come first; later batches contain one or two Sessions. Realtime uses playback
  receipts; GPT-Live sends session-wide commentary and uses the existing transcript
  quiet window. Missing named speech pauses continuation. Interruption and an
  explicit screen switch supersede old queued reports. This is separate from the
  background work planner, which remains a Realtime feature.

**Speaking pace.** The machine’s saved speed also applies to GPT-Live. The four existing
settings select slow (0.8), unhurried (0.9), normal (1.0) or quick (1.1) pacing instructions;
these numbers are not measured Live playback multipliers. `livePacingInstructions` adds one
common rule and one pace at startup, in Taiwan Traditional Chinese for Chinese calls and
English otherwise. It changes articulation and pauses while preserving content, answer length,
language, accent and interruption policy. An in-call `set_speaking_speed` also sends
`session.instructions.append` to the speaking model, with a unique event id and
`delegation_id: null`; updating the Responses backend alone cannot change its speech. Each
new pacing instruction explicitly supersedes earlier pacing directions. The setting is saved
for the next call. Instruction transport tests do not establish audible speed or compliance;
listening acceptance remains separate. The protocol and prompting guidance are documented in
[Managing GPT-Live sessions](https://developers.openai.com/api/docs/guides/live-conversations)
and [Prompting GPT-Live](https://developers.openai.com/api/docs/guides/live-prompting#pace).

**Switching per call.** A call button dials at once on the settings' engine, as before. While the call rings or runs, the sheet's header shows a `GPT-Live | Realtime API` toggle; pressing the other side calls `switchEngine(engine)`, which hangs up and places the call again on that system from the same Session, inside the tap iOS needs for the microphone. (A pick-before-dialing sheet shipped briefly on 2026-11-15 and was replaced at the person's request.)

**Origin and cue.** At `session.started` a Live call resolves the Session it was placed from to the row the tools list (`fleet:<machine>:…` on every machine's list) and uses it as the focus, as a Realtime call's opening handover does; unresolved, every send was refused as "not the Session under discussion". Each backend tool call joins `pendingTools` while it runs, so the progress cue plays when it starts and every `PROGRESS_CUE_REPEAT_MS` until it answers.

**Recaps.** With background recaps on, a tool reads a recap the list already carries when it is at most `KEPT_RECAP_SECONDS` (1800) old, with its age; `fresh: true`, set only when the person asks for the latest, lowers that to `FRESH_RECAP_SECONDS` (300) and has an older one written now. Before this a report of four working Sessions waited 20-22 s for four forks written one after another.

**Opening on its Session.** A Live call placed from one Session does not greet: at `session.started` it says the recap the list already carries for that Session (`liveOpeningNote`, with its age). A recap at most `FRESH_RECAP_SECONDS` old is the latest, and nothing more is read. An older one is followed, while it is spoken, by two reads together: the latest recap (`forkedRecap` with `fresh`) and the Session's newest reports (`get_session_updates`, its last two assistant messages, at most `LIVE_UPDATE_CHARS` = 150 characters). The latest state is sent only once the opening has been said (`untilOpeningSaid`: the live model spoke and then fell silent for `liveQuietMs`, the person not speaking; at most `LIVE_OPENING_WAIT_MS` = 60 s), and it tells the live model to go on with only what is newer. Nothing new, no note. On 2026-11-15 a note sent while the opening was spoken was blended into it, and a fresh recap clipped to its first 400 characters recounted the Session's history; `clipRecap` keeps a recap's first sentence and its end, where it stands now, for every spoken note.

**Appends fit.** OpenAI refuses a GPT-Live append over 500 tokens. A report batch from a call
on the list went as one (rules, evidence and the rules again) and the call was silent on
2026-11-16; the rules and evidence now go as `session.thinking.append` pieces of at most
`LIVE_APPEND_TOKENS` (400) estimated tokens (`liveTokens`: CJK characters 1.3, others a third),
and the spoken `session.commentary.append` is one short note naming the batch (`liveReportNote`).

**Not news twice.** The latest state is said only when it adds something the call has not.
On 2026-11-16 the overview said Release 2.4's three iPad failures and the latest recap of it,
opened a minute later, said them again as "I just got the latest". The note now tells the live
model to compare it with everything it has told the person in this call and to say nothing when
nothing is newer, and the client does not send it at all when under `LIVE_NEWS_SHARE` (a quarter)
of its words are new against the kept recap and everything the live model has said
(`newShare`: Latin words and numbers, pairs of CJK characters; journaled as `no_news`).

**Switching Session.** When the person opens another Session on screen (`followScreen`) or turns the call to one (`turnTo`), a Live call does the same for it at once, even after the person has spoken: its kept summary is said (`liveOpeningNote` with `switched`), the latest recap and newest reports are read together, and what is newer follows once that has been said. A later switch supersedes an earlier one's pending reads (`liveFocusRevision`). Until 2026-11-16 the live model was told only the new Session's name and, asked about it, spoke in generalities.

**Speaking while reading.** The backend's instructions end with `LIVE_BACKEND_FLOW`, which overrides the Realtime rule that every Session question needs a read: an overview is answered from the snapshot's recaps (with their age), and a read is made only for details, the latest state or a Session without a recap. Each Session a read returns is handed to the live model at once as a session-wide `session.commentary.append` (`delegation_id: null`; `liveReadNotes`, at most `LIVE_NOTE_CHARS` = 400 characters of recap), so it speaks about one Session while the backend reads the next; the backend's final answer adds only what was not said. Tool calls stay sequential (`parallel_tool_calls: false`), because a continuation must follow every pending result of a response. Live accepts a specific `delegation_id` only for a client delegation: a call on 2026-11-15 that passed the Responses delegation's id was refused with "Unknown client or Decision delegation", and the fresh summary the person asked for was never spoken. A request for a Session's latest state is one `get_session_summary` with `fresh: true`; the same call first read `get_session_updates`, which is not spoken, and the person waited over 40 s.

**The call itself.** Both systems have two tools about the call, added on 2026-11-16 at the
person's request. `get_call_status` reads the call's own state (`callStatus` in `ToolDeps`): the
system, seconds since it connected (`connectedAt`) and the dollars on its cost line, so the copilot
answers from the figures instead of guessing. `end_call` hangs up after a goodbye, and only when
the person's own transcribed words ask to end the call (`asksToEndCall`): hanging up, closing, or
ending with the call or conversation named; closing a Session never counts, and a refusal is
`voice_end_not_asked`. The client then waits until the copilot has spoken since the request and
its voice has been quiet (`endQuietMs`: Realtime's end of playback, 0.8 s; GPT-Live's transcript,
2.5 s, since it has no end-of-speech event) with no tool running, and disconnects; a goodbye
that never comes ends the call after 6 s, and none waits past `endGoodbyeMaxMs` (15 s). In a
managed call both are immediate: they change nothing on the machine.

**Cost.** OpenAI bills a Live session per second ($0.05 a minute on 2026-11-15) and the backend
per token ($0.10 in, $0.01 cached, $0.50 out per 1M for `gpt-6-luna`). The call's cost line adds
`session.usage.updated` seconds and each forwarded `response.completed` usage at those rates.

**Not yet verified.** The event names and shapes follow OpenAI's GPT-Live guides
(`live-delegation`, `live-conversations`, `voice-webrtc`) as read on 2026-11-15; the tests drive
them with a stand-in. Real calls since then have corrected the delegation type, the origin
and the note's delegation id; the call journals are the evidence.

### Connection and processing acknowledgement

The call does not rely on generated speech to prove connection. The service must
acknowledge the expected tool configuration; then the browser gives one local
connection tone and the sheet says it is ready for speech. The Session handover
still waits for its actual evidence. A separate soft processing note (at the start of work and of each lookup, then
every four seconds of silent waiting; switched in Voice assistant → Settings) and a visible phase distinguish tool waiting from confirmation or
failure. No tone implies a message was delivered. The message proposal, explicit
spoken confirmation and typed-input receipt are described in
[the persistent workspace](voice-assistant-workspace.md#confirmed-messages-connection-cues-and-visible-waiting).


### Remembering the spoken order

The assistant's conversation history and the identity used by a tool are distinct.
The call now records verified speaking order after playback, so “the last one”
or “the second Session” can bind to a real identity before a fresh read. It does
not ask a separate text model to guess that order. Unknown or changed identities
need clarification. Group presentation uses one or two Sessions per response
and one next batch can read in parallel; an interruption pauses the presentation.
These controls are independent of durable work approval and automatic task
allowance. See [the workspace's presentation contract](voice-assistant-workspace.md#spoken-session-references-and-presentation-batches)
for limits and evidence gaps.

**Pending decisions and spoken answers.** The directory treats `waiting_you`,
`work_person_needed`, and unresolved attention Notes as reasons to check with the
person, even if the terminal is idle or a milestone was delivered. Notes and the
current reply still need reading; an old recap does not establish a new question.
The fleet's presentation list deliberately omits menu content. A targeted question
read uses that Session's machine, conversation and execution, with the existing
bounded screen-read budget. It does not open a different Session or ask a paid
model. Background planning hydrates waiting menus before sealing an answer.

A normal send refused as `session_asking` returns the current question, choices
and fingerprint where available, or names the missing question read. It never
retries the write or presses a choice automatically. The copilot can map an
explicit decision expressed by meaning to one current option; different approval
scopes require clarification. `/key` still checks the exact question fingerprint.
A spoken decision to reply is not permission for the voice copilot itself to
perform the target Session's filesystem operation. Managed calls retain their
existing exact-spec confirmation boundary.

Verification uses synthetic local and multi-machine transports, including a menu
appearing between directory read and send. The tests do not establish live model
comprehension, transcription accuracy or real audio acceptance. No provider call
is needed for this regression suite.
