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

Managed calls have a single speech owner and no Realtime tool execution. ASR
utterances are first acknowledged by the durable inbox, then organized in the
background. A group summary is one task, with parallel content reads and a
prepared update for each member; it does not invoke a separate summarizer per
Session. Prepared updates wait while the person speaks, a response is being
generated, or its audio is still playing. `response.done` alone cannot release
that queue: WebRTC playback must report stopped/cleared. Interrupting speech
never erases or cancels earlier requested tasks.

## Quiet, evidence-backed call opening

Opening the general assistant waits silently for the person. It creates no
spoken greeting, durable input or planner turn. A call placed inside a Session
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
utterances retain their durable intake, planning and execution workflow.

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
