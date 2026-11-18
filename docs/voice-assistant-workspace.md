# A persistent Session for the voice assistant

The voice assistant needs a durable inbox and a visible work list. A person can
keep speaking while earlier requests are prepared or executed. Opening the
assistant again shows unfinished work, results, and its settings. The workspace
is separate from terminal Sessions and Board items.

## Implementation plan and acceptance

1. Add a dedicated, bounded ledger in the existing SQLite store. Preserve input
   identity, task/spec versions, changes and execution receipts. A repeated input
   is not another task; a stale plan cannot overwrite a newer instruction.
2. Use the existing tool-less Haiku/Codex small-model runner for structured plan
   changes. Receive first, plan asynchronously, and retain unprocessed input on
   quota/provider failure. No model call runs from a GET or daemon startup.
3. Execute known voice tools through their existing permission and identity
   checks. Independent reads may overlap. Mutations require explicit confirmation
   of their current specification. Unknown outcomes remain unknown. An
   interruption stops speech, not the persistent work list.
4. Add a sidebar Voice assistant Session with history, unfinished/completed work,
   text input, call controls and settings. Reuse the existing settings file and
   card, preserving key, voice, speed and instruction choices. Keep dictation
   settings in ordinary Settings.
5. Carry workspace reads and mutations over the existing encrypted Cloud seam,
   with the same machine scope and revocable read/send permissions. Reopening or
   switching machines must not retain another machine's plaintext work.
6. Test persistence, duplicate inputs, stale edits, plan/action overlap, interrupted
   calls, failed providers, confirmation and unknown effects. Exercise desktop
   and phone UI with an isolated test backend; no paid model or production write
   is part of verification. Run the repository commit gate and preserve pending
   native-provider/production acceptance separately.

The initial measurements and controlled promise probe established that the old
list tool already starts transcript reads in parallel but waits for every read.
The new directory path must not await unrelated transcripts. Detailed report
preparation remains scoped to what the person asked about.

## Boundaries

The ledger is authoritative for assistant work, not for terminal/Board completion.
Sending a request and completing the requested outcome remain separate facts.
The model proposes changes; deterministic code validates and commits them.
Every external action captures an immutable specification and idempotency key.
Expired or disconnected execution is not automatically retried as a new action.

Settings move in the interface, not by copying credentials between stores. The
work planner is an explicit setting and uses the person's selected provider; it
does not silently fall back to another account. Existing calls retain their
behavior until the workspace workflow is enabled.

The workspace shows whether background planning is enabled on the selected
machine, including a separate unknown state when that setting cannot be read.
When it is off, the page explains that direct calls do not add persistent work
items, links to Settings, and names the next-call boundary and CLI quota use.
Changing machines discards an outstanding settings reply. Text requests can
still be saved without starting a model. The direct-call instructions must not
claim that an utterance or tool result was queued in the persistent work list.
Entering from the sidebar without a machine key or legacy browser key opens
Settings by default. An explicit Work tab remains accessible without a key;
default routing neither enables planning nor starts a call.
The Work tab also carries the background-planning switch, backed by the same
settings endpoint as the Settings tab. A pending save disables that switch;
only the server answer changes its displayed state. A failed save becomes an
explicit unknown state, and a late answer from a previous machine or hidden
tab cannot replace the current view. Toggling the setting does not submit an
input for planning. The compose example is a separate, associated field hint;
runtime notes and action failures have distinct informational and error styles.

Both direct calls and evidence-driven updates share delivery guidance: lead
with the practical consequence, explain one connected thought at a time, use
natural emphasis and pauses, and rephrase after an interruption or a report of
confusion. This changes speech composition without changing playback speed.
Prompt inspection and synthetic event checks do not establish actual intonation
or listener comprehension; recorded speech and human listening remain required
for that acceptance. Guidance follows the official Realtime prompting guide:
https://developers.openai.com/api/docs/guides/voice-prompting .

## Delivered boundary and rollout

The new sidebar page is a machine-owned assistant workspace. It is not an invented
terminal Session, a Board item or one of the separate Project Bots. Its settings
card reads and writes the same `copilot.json`; no credential migration or copy is
needed. Background planning defaults off. Enabling it selects the existing Haiku
or Codex Luna CLI runner; it does not run a turn until a pending request is
explicitly submitted for planning. Changing this mode applies to the next call.

`GET /v1/voice/copilot-work` only reads. POST requires Send and an
Idempotency-Key. `input` saves an utterance, and `plan` supplies the current
Session directory and starts one asynchronous organizer. Additional utterances
are combined in subsequent batches, rather than being lost when a turn is
already running. Quota, missing-provider, invalid-plan and stale-plan failures
retain the unprocessed inbox; GET does not retry a paid turn or change providers.
The organizer is tool-less and returns a strict JSON plan; code validates tool
names, dependencies, versions and targets before committing it atomically.

The browser executes the existing voice tools while this workspace or a call is
open. SQLite claims fence multiple browsers and capture a stable action key;
independent read tasks can overlap, writes run alone, and unfinished instructions
block writes. Every mutation needs explicit confirmation of its current task
specification. Pausing an unconfirmed mutation cannot approve it. A target's
machine, complete id, label and conversation identity are checked again before
execution; model-supplied switch and force-close approvals are discarded.
The new Cloud words always address the selected machine, even when another
machine supplied the voice API secret. Switching machines clears old plaintext,
fences late replies, aborts the old executor and disconnects the old call.

The work survives closing a call/browser or restarting the daemon, but unattended
execution while every browser is closed is not implemented. A running task whose
executor disappears becomes unknown when another executor next reconciles its
expired lease. No unknown mutation is retried automatically. A sent Session
message stays `waiting_result`; automatic matching of its eventual goal/result
is not implemented. The person can inspect that Session and explicitly mark it
complete. These limitations are visible in the workspace rather than hidden
behind a completed checkmark. The initial ledger refuses new input or tasks at
256 retained records per collection; it never silently evicts a person's work.
An archive/retention workflow remains a follow-up.

Managed calls share one speech owner between immediate queries and prepared work
updates. ASR utterances are first saved as deferred conversational inputs, with
the exact Session, machine and conversation captured when speech starts. Switching
the screen while transcription finishes cannot rewrite that identity. Read tools
answer ordinary Session questions immediately, without a CLI planning turn. For
continuing work the voice model calls `queue_voice_work` with original saved input
IDs; a single plain message instead uses the deterministic proposal path below.
It cannot paraphrase the saved request or perform a direct mutation. This
routing still depends on model compliance and needs recorded-call acceptance.

Explicit submission promotes those inputs for the organizer. Each input may carry
bounded earlier read evidence from the same Session, identified as historical,
untrusted context rather than fresh status or approval. A group summary remains
one task with parallel content reads and prepared member updates. Tool results
from the same response share one continuation, after all outstanding tools finish.
The narrator waits for durable intake, generation and WebRTC playback; a
`response.done` event alone does not mean playback finished. Silent pipeline
reads do not advance the browser's spoken-report cursor, so unheard content is
not skipped on the next query. They may therefore repeat content later.

Planning failures retain a safe error code and stage on each affected input.
Fresh reads and explicitly submitted new planning can proceed without retrying
failed inputs. Unresolved inputs, including interrupted conversation, still fence
writes: a failed cancellation must not accidentally release an earlier approved
send. The person must explicitly retry, organize or skip the retained request.
Skipping never completes or approves a task. Retry from a call requires an
explicit retry utterance, with at most one submission for that utterance. GET,
startup and failure polling never retry a provider. If failure metadata cannot be
saved, the worker stops rather than re-entering the provider loop. Logs retain
stages, input IDs and error types without prompts, provider output or secrets.

The existing work JSON stores these optional fields, batch selections and message
receipts without a SQLite schema migration. GET/POST work replies advertise protocol 3. Managed calls check the
selected machine before proceeding and refuse older daemons explicitly. Install
the compatible daemon before rolling out the new hosted client; a console build
alone does not upgrade a paired machine. Direct calls keep their existing path.

## Quiet, evidence-backed call opening

After the local ready cue, a call started from the list automatically requests
current Session summaries. Person-owned waits and unresolved Notes come first;
other Sessions follow one or two at a time, grouped by Project within those
priority groups. Both Realtime and GPT-Live use this path, with background work
planning on or off. Speaking before the read finishes supersedes the opening;
later interruptions pause the retained presentation. No greeting, persistent
input or organizer turn is needed. A call placed inside a Session
reads that exact Session's bounded recap and outstanding notes concurrently,
then requests one short handover containing both results. The call does not
route that implicit opening through the planner, narrate its internal stages,
or ask for an unspecified confirmation. The same policy applies with background
planning on or off, including turning an existing call to another Session.
The opening speech is instructed to give the actual outcome/current work and a
concrete decision, when one exists, in at most two sentences. That instruction
still requires recorded audio and human listening for model compliance.

Speaking, changing the focused Session, hanging up or replacing the call
supersedes an unfinished opening. Late data must not speak into the next call.
The first workspace load primes both task evidence and the planner reply as
history, so reconnecting does not replay an earlier planner explanation. Actual
utterances retain their durable intake and explicit routing into immediate reads
or continuing work. Historical requests are omitted from the opening evidence;
only a live question, unresolved notes or the latest reply may establish a current
decision. Earlier approvals cannot override a newer reply saying no action is due.

A local recorded call reported on 2026-11-13 contained zero transcribed user
utterances but five assistant replies. Read-only inspection found an automatic
handover input, two completed reads (latest summary and notes) and two planning
rounds. This establishes unnecessary startup generation and fragmented reporting;
it does not establish that no reads ran or what the provider billed. Private
call text and identifiers remain outside this public repository.

Regression tests exercise the actual client with held transcript responses,
GET-only transport, suppressed openings after speech/call replacement and one
combined response after recap plus notes. No live speech/provider call is used.

## Performance evidence

Affected action: listing Sessions and preparing the call's opening directory.
A reported long list-tool wait motivated the change. The previous path started
up to six transcript requests in parallel, then awaited all of them. That
structure established a dependency, not which production endpoint caused the
reported tail. The new directory carries status, questions and existing delivery
receipts plus an explicit content-not-read warning. A detailed question still
uses summary/recap tools. The maintenance cost is this changed directory contract
and its model instructions; the correctness risk is guessing content from a name,
covered by the warning and behavioral tests.

Controlled command (Node v24.1.0, same checkout and machine, alternating before /
after order, three warmups and 30 recorded runs for each action):

```sh
node --experimental-strip-types tools/bench-voice-directory.mjs \
  408012240201639dcfa5d66638107b5c9fbb37e0
```

Eight synthetic working Sessions; each requested transcript waits 30 ms and
contains a 50,000-byte assistant reply. This is a local tool/dependency probe,
not an HTTP, model or production Cloud latency measurement.

| Action | Before p50 / p95 (ms) | After p50 / p95 (ms) | Requests before / after | Response bytes before / after |
| --- | --- | --- | --- | --- |
| Session directory | 32.058 / 33.850 | 0.040 / 0.059 | 7 / 1 | 301,022 / 758 |
| Opening snapshot | 31.784 / 33.240 | 0.036 / 0.125 | 7 / 1 | 301,022 / 758 |

The small, nearly flat after values are expected because this fixture responds
to the directory immediately. They establish removal of transcript waiting and
bytes, not a promise that Cloud completes in those times. A separate test makes
all transcript promises permanently pending: both list and snapshot still
return. Another holds one group member pending and observes another member's
prepared result before the group finishes. Real-call p50/p95, live provider
planning quality, audio timing on devices, and production Cloud acceptance remain
pending; no usage-priced provider call was made for these checks.

## Skill and dependencies

The instruction skill was prepared from `addyosmani/agent-skills` at immutable
commit `8bfcc1f9916f2eea55494d8c1fa7f4bde867dd9e`, entry
`skills/performance-optimization/SKILL.md`, SHA-256
`82af71540457cf853893685211f64509cd1e781c0de09d0186f16960f2d6eeba`.
The checkout preserves shared reference and license paths (MIT); its instruction
and required references were read and manually applied in this Session. No
native/global assistant installation or automatic reload is claimed.

Preparation uses a task-owned checkout: fetch the specified commit, detach at
that commit, compare `shasum -a 256` for the entry before reading it, and retain
its referenced repository files. The skill adds no runtime dependency. Existing
web dependencies are reused or prepared by `node tools/ensure-web-deps.mjs` from
the unchanged `web/package-lock.json`: Node 24.1.0, TypeScript 5.9.3, Vite 6.4.3,
React 19.3.0. Installed provider CLIs reported Claude Code 2.1.322 and Codex CLI
0.166.0; checking their versions does not establish live provider availability.

## Review corrections and discovery continuation

The corrected executor protocol (version 2) refuses claims from older browsers.
Claims include supported tools and the current call identity. Call-scoped tasks
stay with that call; another browser cannot consume its speed/length changes.
Requests submitted outside a call update the next-call defaults, even if another
browser happens to execute them during a call. The call and workspace share the
same lazy guide loader, so help does not require an active call.

An answer task seals the original menu fingerprint and resolved numbered option
from its input directory. Its confirmation shows the original question and option.
The tool checks the fingerprint again, and the final key POST retains that exact
fingerprint. A replacement question requires a newly planned, confirmed task.

Every active executor reconciles remote pause/cancel requests on state updates.
Before any external mutation it also requests admission from the ledger, which
serializes that decision against cancellation and unprocessed speech. Cancellation
before admission prevents the effect. After admission, cancellation cannot promise
rollback: an unacknowledged external effect stays unknown. A proven stop before an
effect becomes paused/cancelled. Explicit retries get a new attempt key; late
results from an earlier attempt cannot overwrite them. Known refusals are failed;
a transport loss or failure after an accepted effect remains unknown. Unknown
reads have a retry button; unknown writes, including navigation, do not.

Each task attempt retains up to eight prepared updates plus a versioned final
result. A call observes that durable evidence with its own watermark. Results
prepared by another browser are narrated once, and multiple group members remain
available even when a poll skips intermediate revisions. Earlier completed history
is not replayed on connection. Actual WebRTC audio/device acceptance remains a
separate runtime check from these deterministic state-machine tests.

The planner receives the actual tool contracts, original requests and recorded
tool results. It can return `wait_for` read task IDs. The ledger keeps the original
request parked until those reads finish, then a finish receipt wakes one organizer
for the next planning round. A project lookup can therefore produce a subsequent
start proposal using its returned project ID; the start still requires confirmation.
Each original request permits four planning rounds including the initial round.
At that boundary the request remains visible with a typed error and an explicit
retry action. Failed discovery is context for clarification, never an automatic
read retry. Paused or unknown discovery waits for human reconciliation; cancelled
discovery drops its continuation. GET, startup and mere polling never call a model.
The continuation context is in memory; after a daemon restart, explicitly resubmit
pending planning with a fresh directory/contracts before continuing.

Regression evidence uses synthetic menus, two executor identities, a held
operation, persisted progress and an injected tool-less planner. It proves the
protocol transitions, not a production latency improvement, native provider
availability or actual phone audio playback. No paid provider call is needed for
these checks.


## Work ends with its call

Work does not cross calls (decided on 2026-11-13). A request one call never
carried out was taken up by the next call and aimed at the Session that call
was placed from. Hanging up now ends the call on the machine (`park_call`,
kept as the action's name so an older machine still parks): every utterance it
left unplanned is dismissed with its words kept, and every task it produced
that was not confirmed is cancelled. A write the person confirmed during the
call, and anything already running or finished, completes and reports as
before. This replaces parking an interrupted conversation for a later call.

## Confirmed messages, connection cues and visible waiting

Message confirmation is conversational in Live, Realtime and managed mode.
The assistant must read the full recipient and assistant-composed content, ask
whether to send, and wait. The assistant interprets natural agreement from the
conversation and invokes the message tool once, without asking again for the
same approved recipient and content. Changed drafts require a revised read-back.

The user's 2026-11-18 decision replaces the application-owned draft gate. There
is no transcript equality, playback-silence qualification, consent phrase list,
or durable prepare-message round trip in conversational message sending. The
send tool is an effect, not a drafting tool; its description says so. Routing
still resolves the exact recipient and handles live questions, transport
idempotency and actual receipts. A changed call cannot execute an old admitted
request. Background work-list approval contracts for other mutations remain.
Prompt compliance is not a programmatic proof that the assistant obtained
consent. Local transport tests do not establish actual model compliance or
what someone heard on their phone; those remain separate acceptance.

Opening a Session or its snippets on screen changes only this browser, so a
managed call runs open_session and open_snippets at once; on 2026-11-13 opening
a Session was queued for the work list twice and never happened. The existing work-list button remains available for other exact-spec actions.
Direct conversational messages do not create a pending-proposal card or a second
confirmation task. Ordinary acknowledgements do not automatically execute work.
Delivery questions read the work ledger rather than inferring from recipient
activity, and each generated response receives current action evidence at flush.

Connection is acknowledged by local audio after the voice service confirms the
expected tools. Transport connection alone is not readiness. A configuration
acknowledgement uses the existing workspace-control request deadline and fails
explicitly if it never arrives. No generated greeting is required. The ringback survives call-history initialization and continues until readiness,
failure or hangup. Each top-level tool start, including automatic background
queries, plays one soft local note. Active tool calls retain a 330 Hz sine note;
automatic background queries and durable background work use a single 220 Hz
triangle note with the same duration and loudness envelope. Nested reads share
their parent start. A
running durable task has one start cue per task version; ordinary state refreshes
and long waits never create repeated audio. Entering another Session uses a
three-note 660/880/660 Hz melody, distinct from readiness and hangup. Resuming
one Session does not replay that switch cue. Realtime follows narrated name
anchors; Live cues each single-item handover, and explicit successful navigation
uses the same identity. These cues carry no model generation. Tool and Session
cues follow the existing per-browser Processing sounds setting; ringback remains
independent.
Active saving, tool waiting,
planning, execution and response preparation have visible state and elapsed time.
Confirmation and failure are stopped states, not an animation claiming progress.
The sheet and minimized bubble both expose state. Local tones use no model call;
real device playback, accessibility and listener comprehension still need actual
browser/audio acceptance. Mock audio and event tests cannot establish those.

Recent idle reports use a 30-minute window, expanding to one hour only if empty.
They include only currently idle Sessions, newest first, and report approximate
elapsed idle time. The current directory supplies confirmed activity/delivery
timestamps rather than an exact transition-to-idle timestamp; this estimate and
unknown times are explicit in the tool result. Older transcripts are not fetched.
An explicit all-idle request bypasses the recent window.


## Automatic continuation allowance

The machine defaults to three automatically continued durable read tasks per
batch. This is neither the two-read concurrency limit nor the retained inbox
capacity. A grouped Session summary is one work item; direct conversational
queries remain immediate reads. The organizer retains overflow, merges duplicate
requests and preserves real dependencies. It does not gain execution authority.
Important writes still require exact-spec confirmation, and ambiguous targets or
missing facts still require a question; model confidence is not authorization.

The ledger stores the batch and exact task versions, shared across executors and
surviving reloads. A completed, failed, cancelled or superseded slot consumes the
allowance without implying success. Once the batch settles, queued ready read
candidates are shown and announced in order. The work-page button or a fresh
explicit spoken continuation after those titles finish playing admits only that
selection. A newer task cannot steal an unused reviewed slot. Changed versions,
selection or settings require a fresh offer. Approval never releases unconfirmed
writes. The setting applies to the next batch. Explicit retry of a failed read is
a separate person action, not silent automatic retry. Old machines omit the
capability and controls; update the machine before relying on batch enforcement.


## Spoken Session references and presentation batches

The realtime conversation includes the assistant's previous responses. That
alone does not give a separate selector model the earlier speaking order. A
recorded call showed the selector replacing even a correct Session ID when
it received only the latest relative phrase. Exact names in the actual
utterance and verified spoken references now take precedence over classification.
Descriptive words can still use the existing selector; writes retain their
separate identity and confirmation gates.

A call-local ledger matches known Session names against a completed spoken
transcript, records their actual text order only after playback finishes, and
binds each to machine, conversation, label and ID. A fresh directory must still
match that identity. Generated or interrupted speech, duplicate names and
unrecognized aliases cannot prove order: ask for a name rather than guessing.
A later detail keeps the previous multi-Session list available for ordinals.
On 所有機器 a label ends in 「（machine）」, which speech leaves out; the bare
name anchors it only while no other Session read in the call shares that name.
On 2026-11-13 (call f91a9eb8) both gaps showed together: three idle Sessions
were read without their machine suffix, so none was recorded, and
「跟我講你最後的那一個」 was not recognized as a position, so the classifier
replaced the copilot's correct third Session with the second. 「最後（的）那個」
is now a position, and the call's progress line is text without the mascot.
The reference window holds 24 recent identities and clears on call end or
machine switch. The journal stores text and tool evidence, not replayable audio.

A group report goes one Project at a time (asked for on 2026-11-13). Sessions
of the same Project, named by their repository folder (`project_key`, else
`cwd`), are kept together; Projects are ordered by their most recently moved
Session, and a batch never crosses into another Project. Each batch carries its
`project`, and the voice says which Project it is before its Sessions.
On 所有機器 the page handed the copilot its rows without `cwd` or
`project_key`, so every batch arrived with `project: ""` and Sessions of
different Projects were read together unnamed (calls of 2026-11-14 01:04 and
01:16); those rows now carry both (`copilotFleetRows`).
A call's group report reads at most two Sessions for its current response. Only one next batch is prefetched while
that response is spoken. It is not sent to the reporter until the current
batch's matched names finish playback. Interruption pauses subsequent speech;
explicit presentation continuation resumes the retained batch. Unheard words
are not marked as covered. Remaining-only requests exclude recently presented
IDs. Every later read rechecks the captured identity instead of reading a pane's
replacement. Each report covers at most 24 Sessions and explicitly discloses
any unread overflow; it does not claim complete coverage beyond that window.
This presentation queue is separate from the durable task allowance and grants
no write approval. Reference tracking starts no model. Summary prefetch respects the existing recap setting and may request a fresh fork when that setting permits it.
Separate spoken responses still consume the existing voice service normally.

Synthetic held-read and event tests exercise sequencing, interruption and
identity preservation in direct and managed calls. They do not establish actual
prosody, listener understanding or production latency. If the latest reply is
only implementation detail, delivery guidance asks for the bounded recap of
original requests instead of repeating the same jargon or inventing a goal.


## Whole-call tool history

The area below speech and above usage keeps this call's tool history. New
utterances, interrupted reports, minimized sheets and re-opened sheets do not
clear it. Hanging up marks pending observations as result-unconfirmed rather than claiming
the underlying action was cancelled; the last call's
history remains available until a new call starts. Reloading the page does not
restore the in-memory history, and cross-device historical browsing is outside
this view's contract.

One call-owned ledger supplies both this area and the expanded activity log.
A unique operation ID is created at start and updated at completion; callbacks
from an older call cannot enter a replacement call. Each entry carries its call,
parent, source, target, wall-clock start, monotonic offset, duration and outcome.
Model-invoked tools and automatic background operations are counted separately.
Automatic opening and read-ahead use the same observation hooks, including
per-Session transcript, recap, progress, current question, Notes, reference
resolution and follow-up analysis reads. Existing summaries are marked reused;
a local cache lookup is not described as a fresh provider generation. A read
that falls back after an HTTP failure remains a failed observation even when
the original function can answer from older evidence. Observation never runs a
tool a second time, changes its authority or starts an execution sound.

The original green-outline capsules remain below speech, with one capsule per
invocation showing its target, outcome and duration, newest start first. Completion
updates its capsule in place rather than moving an older invocation above a new
one. Counts remain visible above
them; there is no disclosure triangle or grouped list. The existing activity log
shows each observation with its start time and source. The processing-coverage tooltip
counts overlapping top-level intervals once and excludes their nested read
intervals. Summed durations can exceed that total. The scroll area keeps every
observation rather than applying the mixed diagnostic log's fifty-line limit;
capsules wrap within the original scroll area. Completed observations enter
the existing private call journal as `call_tool_operation` metadata through the
normal journal flush, without adding model requests or another write per read.

The synthetic browser fixture at
`/tools/fixtures/copilot-tool-history.html?engine=live&lang=en` (or
`engine=realtime`) uses the actual client and modal with provider-free transport
substitutes. Start background reads, release them, request the list, speak again,
minimize/reopen and validate. It checks three recap reads, three transcript reads,
cache labeling, unique IDs, journal agreement and placement. This proves the
fixture behavior, not an actual paid call or real phone audio acceptance.


### Interrupted report navigation

An explicit request for other or next Sessions passes the interrupted current
item and presents the retained following item. Continuing the report resumes the
unfinished item instead. Complete polite requests use the same local routing as
short next commands, in both engines. One input turn can advance only once even
when both the local continuation timer and a model tool callback handle it.
Skipping records `session_report_skipped`; it does not record playback completion,
create ordinal-reference evidence, approve work or reread already prepared data.
Live saves each completed item's transcript before clearing the buffer for the
next item, so the private journal no longer loses the first item of a batch.

A persisted call investigation exposed repeated resumption of one interrupted
item after requests for other Sessions. Synthetic regression inputs exercise the
same presentation transition without retaining personal call content in this
repository. Provider speech comprehension and actual device playback still need
live-call acceptance; these tests establish local routing and queue behavior.


### Short-screen tool-history layout

The tool-history wrapper keeps its intrinsic height instead of shrinking below
its capsules. Usage grows with its content rather than overflowing a fixed-height footer. The call body scrolls separately above fixed-in-flow hangup controls, so sticky
controls cannot overlay its content. The obsolete application-gated
waiting-proposal card is removed; draft review
now happens in conversation. The call body scrolls when a small screen needs
more room; the capsule trail retains its own
compact scroll area. A provider-free fixture with long transcript text, usage and
both engines checks child containment as well as sibling geometry.

## Compact call sheet and fresh calls

The call sheet shows the engine selector, orb, current exchange, persistent green tool
capsules, duration, amount and call controls. Quick commands sit at the right of the
history heading, including before the first tool starts. A paused report can still be
continued from that picker. The subtitle, connected-state/progress copy, automatic-report
explanation and diagnostic-log entry are removed; connection failures and action notices
remain available. Unpriced totals retain their lower-bound wording.

The browser no longer saves conversation turns for a later call. Realtime configuration
and Live backend updates contain the current Session snapshot, settings and briefing,
with no prior-call section. Initialization removes only the retired voice-memory storage
key without reading it. Current-call provider context, captions and operation journals
are separate from this removed feature.


## Read-only Session message-board queries

The call's existing board target registry supplies `list_session_boards` and
`get_session_board`. Both call engines expose them as immediate reads, including
managed calls; they do not create a durable work task or call the mutation endpoint.
Each invocation reads fresh board indexes through the target's existing local or
Cloud source, alongside the Session directory for current Lead labels. Only active
boards are returned. Posts are not fetched for these directory questions.

Membership joins use machine identity and the stable assistant/conversation actor,
never a Session label. A transferred Lead is read again, a no-longer-listed Lead
retains its actor identity without an invented Session ID, and partial source failure
is reported explicitly. Exact IDs or exact normalized names identify a board;
duplicate names return choices. Unavailable sources cannot establish a unique match
or a known empty membership. Nested index reads use the existing call-history observer.

Local and fleet Session rows receive the same current index affiliation as their
existing board groups. Known unassigned Sessions have an explicit label; absent
identity, absent member metadata or failed reads suppress that claim. Labels wrap
onto a separate line and truncate long names inside the row, retaining the full
name in a tooltip. English and Taiwan Traditional Chinese copy ship together.

Provider-free tests cover fresh Lead transfer, machine-scoped identities, duplicate
names, archived boards, partial failures, unavailable membership and read-only direct
and managed call paths. The browser fixture at
`/tools/fixtures/session-board-affiliation.html?mode=local&lang=zh-Hant` also supports
`mode=fleet` and `lang=en`. It checks actual Session rows at phone and desktop sizes,
Lead refresh, long names and unavailable-state rendering. It establishes these local
surfaces without claiming real voice-model comprehension or device audio acceptance.
