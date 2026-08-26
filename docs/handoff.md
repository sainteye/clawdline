# Handing a line of work to the next session

A conversation ends before its work does. The context window fills, the phase changes from planning
to building, the model runs out of allowance, the day ends, or the next hour of this would be better
done in Codex than here. What happens next is nearly always the same bad thing: somebody re-describes
half of it from memory to a session that starts knowing nothing, and the half nobody remembered to
say is the half that mattered.

A **handoff** is the other move. The session that has the state writes it down — decisions, current
position, the sources, the open threads — and a new session picks the line of work up from that
document. Clawdline's part is small and deliberately so: it opens the tab and types the first
sentence. **The file is the protocol; the app is the postman.**

This is the sister page to [`docs/orchestrator.md`](orchestrator.md), and the two describe opposite
motions. Dispatch sends an *errand* out and waits for it to come back. A handoff sends the *line of
work itself* on and does not expect anything back at all.

---

## A handoff is a continuation, not an errand

Both moves open a terminal tab and type a first message into it. Everything after that differs, and
the differences all come from one fact: **the session a handoff opens is a new root, not a child.**

| | Dispatch (`POST /v1/orchestrator/tasks`) | Handoff (`POST /v1/orchestrator/handoffs`) |
|---|---|---|
| what the new session is | a **child**, in somebody's tree | a **root**, in nobody's |
| what it is given | one task it did not choose | a line of work, and the judgement to continue it |
| credential it holds | a task secret, good for one sentence | none. There is nothing for it to prove |
| deadline | `timeout_minutes`, 1…240, 30 by default | none. It is a session, and sessions end when the person ends them |
| what it owes | `result.json`, and that file *is* the completion signal | nothing back to the sender. It reports to the person in its own tab |
| who watches it | the app, once a beat, until it reports | nobody. It is an ordinary session on the list |
| capacity it spends | a child slot, a descendant slot, and depth | none of the three. There is no tree to be deep in |
| when the sender closes | its live tasks are cancelled and their tabs closed | nothing happens to it. It was never the sender's |
| what the app reads | `task.json`, strictly validated | the *existence* of `handoff.md`, and nothing inside it |

The last two rows are the ones that catch people. **Closing the session that handed over does not
touch the session that took over** — the cascade in
[orchestrator.md](orchestrator.md#the-lifecycle) follows `parent_task` and `root.session_id` links, a
handoff creates neither, and that is correct: you handed the work on precisely so that it would
outlive this conversation.

And the sender does not have to die. A handoff is a copy of state, not a transfer of ownership: fork
a line off to run in parallel and there are now two equal roots in one working tree, with everything
[that section](#two-roots-in-one-working-tree) says about it applying to both.

There is one deliberate ownership convention on top of that transport fact. A root that must stop
while it holds a named pending obligation — for example, landing a reviewed delivery branch — writes
that obligation and the receiving root's ownership explicitly into `handoff.md`, supplies
`from_session`, and waits for Clawdline's *picked up* receipt before stopping. The receipt proves only
that the first line reached the named root; at that point the named obligation has an owner that can
outlive the sender. It does not prove that the package was understood or that the work was completed.
Without that explicit assignment, handoff remains a copy and both roots retain their own work.
If the obligation owns paths with Clawdline waiters, the package also names every waiter session id,
its exact paths and release condition. The receiving owner assumes the eventual Clawdline fan-out;
handoff must not strand a waiter inside the sending assistant's private message system.

---

## When it is worth doing, and when `/compact` is the answer

The research behind this page — [`artifacts/2026-08-26-handoff-demand-research.html`](../artifacts/2026-08-26-handoff-demand-research.html),
four investigating agents and one reviewer checking 45 load-bearing claims against primary sources —
found the technique widespread and entirely hand-rolled. No tool has become the default answer — the
independent ones sit at single or low double-digit stars — and the word *handoff* does not appear
once in the official best-practices documentation. What the ecosystem does have is a shape people keep
rediscovering, and a disagreement inside it worth inheriting.

The clearest statement of what a handoff buys comes from Matt Pocock, whose `/handoff` is that
ecosystem's most-cited version, and he draws the line himself, verbatim:

> "For anything else (same harness, same directory, you are done grilling and moving to
> implementation), `/compact` is the move." … "Three of the five options at a phase boundary preserve
> different things: `/compact` preserves your intent, `/clear` preserves nothing, `/handoff` preserves
> the work's ability to move." … **"What it buys is portability, not compression."**

So: **hand over when the work has to move**, and compact when it merely has to continue where it is.

Worth a handoff, roughly in the order the evidence supports them:

- **A phase boundary** — planning is finished and building starts, and the session that argued the
  plan is the wrong shape to execute it.
- **The context window is about to make the decision for you.** This is what people actually reach
  for it for, and the official documentation is blunt about what compaction costs: *"It replaces the
  verbatim conversation: full tool outputs and intermediate reasoning are gone."*
- **A parallel fork** — you keep going, and a second session takes a copy of the accumulated state
  and works a different branch of it.
- **Tomorrow** — multi-day work, where the alternative is briefing a fresh session by hand every
  morning.
- **A different harness, model or allowance** — Claude to Codex, or a model swap when the current
  one runs out. Cross-assistant handoff is a real, observed use, and this app runs both.
- **A different machine.**

Not worth one:

- **Same harness, same directory, ordinary transition.** `/compact` is cheaper and loses less than
  the round trip through a document.
- **Work smaller than the document that would describe it.** The same test dispatch uses: if writing
  it down takes longer than finishing it, finish it.
- **A question you could answer in this session.** A handoff is for a *line* of work, not a step.

**One asymmetry is worth knowing because it shapes what you write.** The author who defined the
technique calls it portability; the people using it overwhelmingly reach for it as an antidote to
compaction. Both readings are attested by primary sources, and they pull the design in two directions
that turn out to be compatible: **write the document for portability — it may be read in another
harness, on another machine, next week — but reach for it before the context runs out rather than
after.** A document written the moment before compaction is written by a session that still remembers.

---

## The package

Everything a receiving session needs is one directory, for the same reason a child's task is one
directory: **the receiver is an ordinary session with no client library.** It can read a file.

```
/tmp/.clawdline/handoffs/        # 0700
  <handoff-id>/                  # 0700 — lowercase UUID
    handoff.md                   # the sender writes this. Required, non-empty
    attachments/                 # optional. Files the receiver should read, copied rather than linked
```

Handoffs live in their own named subdirectory rather than beside the task directories, and that is
not tidiness. Every child briefing carries the rule *do not read any directory under `/tmp/.clawdline`
except your own*; a task-id-shaped directory that is not a task would make that rule ambiguous exactly
where it needs to be crisp. A sweep looking for tasks finds tasks.

The package holds **no credential**. There is no handoff secret, because there is nothing a receiving
session has to prove: it reports to the person watching its tab, not to a route.

### Transport is not storage

`/tmp/.clawdline` is swept: a task directory goes 24 hours after the task finished, and a handoff
package goes 24 hours after its envelope was created — the same sweep, on the same clock, keyed on
[the record the app keeps](#what-the-app-remembers) rather than on the directory's own timestamps.
`/tmp` never promised more than that. So the package is **transport** — good for the hours between
one session and the next — and anything the line of work will still need next week has to be
**durable** before it is cited.

This is the rule the one measured trial of this method produced, and it earned its place. The trial's
reference chain held throughout — five references, five of them reachable — and still surfaced one
link that would not have survived the week: a document that lived only in a session scratchpad under
`/private/tmp`, 843 lines of approved design, one copy, in a place that evaporates. The receiving
session's report named it the most fragile link in the chain — not a broken one, and not because the
sender hid it, but because *marking a source as volatile is honesty, not a fix*.

> **References are not duplicated — but a volatile source is the exception. Durably archive it first,
> then cite the durable copy.**

In this repository that means `artifacts/` for a document that records a decision and `docs/` for one
that becomes the standing answer. In the package, put the pointer, not the copy.

---

## The document

Eight headings, in this order. The shape is the trial's — [`artifacts/2026-08-26-cloud-architecture-handoff.md`](../artifacts/2026-08-26-cloud-architecture-handoff.md)
is a worked one: 70 lines, standing on five numbered reference entries and three named commits.

**`OBJECTIVE`** — what this line of work is, and what the receiver's first hour looks like. One
paragraph. The receiver reads this before it knows anything, so it is the only section that may
assume nothing.

**`KEY DECISIONS`** — what is settled, marked plainly as *do not reopen*. Number them, so later
sections and later conversations can name one. **Date each decision, and where you can, say where it
was settled** — the trial's receiver found nothing the sender had failed to pass on, and still
flagged that it had no way to check the boundaries of any of the five settled decisions, because a
decision that lives only in the sender's context has nothing behind the summary of it. A line and a
date is enough to fix that.

**`CURRENT STATE`** — what is done, what is not, what is in flight, and what belongs to a different
line that touches this one. Name commits by hash. This section is the one that goes stale, and
keeping it true is the receiver's job from the moment it takes over.

**`REFERENCES`** — the authoritative sources, in reading order, **with their content not repeated
here**. A handoff document that summarises its own sources is a document that will disagree with them
by Thursday. Two layers, and the second is easy to skip and worth having:

1. *What you can read* — paths in this repository, URLs, the archived copies of volatile sources,
   and **the files in this package's `attachments/`, named one by one, by their path relative to
   `handoff.md`**. An attachment no entry names is a file the receiver will never open: the line the
   app types points at `handoff.md` and at nothing else, so `REFERENCES` is the only door the rest of
   the package has.
2. *What you cannot read, and what depended on it* — the meeting, the PDF on somebody's laptop, the
   private repository an upstream decision was modelled on. Say where to ask. A receiver that digs
   will hit these; finding them unmarked reads as a broken chain rather than a known one.

**`CONSTRAINTS & PRINCIPLES`** — the red lines. Violating one of these means the work went the wrong
way, not that it went slowly. This is where the things nobody would infer from the code belong.

**`OPEN THREADS`** — numbered, each a sentence, each pickup-able cold. This is the section the next
session actually works from.

**`IMMEDIATE NEXT STEP`** — one entrance, naming one open thread. Not a plan; a door.

**`VERIFICATION`** — three to five questions **whose answers are deliberately not in this document**,
each naming where the answer lives.

That last section is the load-bearing one and the least obvious. It is not a quiz for a person: it is
a fidelity check built into the handover. A receiver that can answer them has demonstrably walked the
reference chain; one that cannot has found a break, at the cheapest possible moment to find it. In
the trial the receiving session answered five of five, and still came back with four sources it could
not reach and did not pretend to have read.

**Check the pointers once before you hand over.** The same trial had one question pointing at the
wrong sections of a cited artifact. The answer was still findable and the receiver still found it,
but a wrong pointer is indistinguishable from a broken chain until somebody has spent the time
proving otherwise.

---

## What the receiving session owes

Not a report to the sender — there is no channel back and none is needed. What it owes is to the
person in front of it, and it is a sequence:

1. **Read `handoff.md` first, before touching anything.**
2. **Walk `REFERENCES`.** Actually open them. This is the step that costs ten minutes and saves the
   afternoon.
3. **Answer `VERIFICATION` from those sources**, not from the handoff document. Say the answers out
   loud in the tab; they are the receipt.
4. **Say plainly what could not be reached.** A source that is gone, a URL that fails, a pointer that
   leads nowhere: that is a finding, and reporting it is the job. **Do not fill the gap with a
   plausible answer** — an invented one costs more than a missing one, because it stops anybody
   looking.
5. **Continue from `OPEN THREADS`**, starting at `IMMEDIATE NEXT STEP` unless the person says
   otherwise.
6. **Keep `CURRENT STATE` true.** The document is now this session's, and a handoff that stops being
   updated is a handoff that can only be made once.

A receiving session that also intends to hand on later should keep the document under
`OPEN THREADS` discipline as it works, rather than reconstructing it at the end from a context that
is by then exactly as full as the one that made this necessary.

---

## The route

**`POST /v1/orchestrator/handoffs`** — open a session and give it a package to pick up.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/handoffs \
    -H "X-Clawdline-Orchestrator: $(cat ~/.config/clawdline/orchestrator-token)" \
    -H 'Content-Type: application/json' \
    -d "{\"handoff_id\":\"$ID\",\"project_dir\":\"$PWD\",\"assistant\":\"codex\"}"
{"ok":true,"handoff":{"id":"7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431","state":"opening","projectDir":"/Users/you/code/clawdline","assistant":"codex","dir":"/tmp/.clawdline/handoffs/7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431","opened":{"terminalId":"9A1F…","backend":"iterm"}}}
```

**The tab is made before this answers; the typing happens after.** That line is the whole shape of
the route. Opening a tab is synchronous, so a Mac that cannot provide one refuses here with an HTTP
status. The first reply that opens the tab therefore carries `opened` and `assistant`; an idempotent
replay answers from the envelope alone and carries neither field. Everything after that — waiting
for a composer, typing the line, confirming it landed — runs for up to four minutes after the
connection has closed, so **none of it can ever be an HTTP code**. On that first reply, `state` is
`opening`: a tab exists, and nothing has been typed into it yet. A replay carries whichever state
the stored envelope has. There is nothing to poll afterwards; the audit line and the receipt below
are how the sender finds out the sentence landed.

**It is idempotent by `handoff_id`**, the way a dispatch is by `task_id`: the same id twice answers
with the record that already exists and opens no second tab, so a retry after a dropped connection
cannot leave two sessions holding the same line of work. To genuinely open two — a fork to two
places — make two packages. It is a directory and a copy.

### What the app remembers

One row in the same `0600` orchestrator registry the tasks live in — **the envelope, and never the
contents**:

| field | |
|---|---|
| `handoff_id` | the id, which is also the directory name |
| `project_dir` | where the tab was opened |
| `title`, `from_session` | as given, if they were given at all |
| `created` | when the call was accepted |
| `state` | `opening` while the line is being typed, then `delivered` or `spawn_failed` |

That is the whole of it. No path into `handoff.md`, no summary of it, not a byte of what the two
sessions are saying to each other: **the app remembers the envelope and forgets what was in it.** The
postman rule survives being able to answer *did this one already go?*, which is the one question a
postman is allowed to have an answer to.

The row is what the idempotency above answers from, so it **holds across a restart** — the registry
is a file, the way a dispatch's is. `opening` is in that set for the same reason: a retry arriving
during the four minutes of waiting and typing has to find the first call, not open a second tab.

It is also what the sweep keys on. The directory and the row go together 24 hours after `created`;
delivery normally settles inside four minutes. They use the same rule and the same pass that removes
a terminal task's directory. That is the point of keeping a row at all — without one, a sweep would have to go
reading timestamps off `handoffs/`, and the 24-hour promise in [Transport is not
storage](#transport-is-not-storage) would be a promise nothing implements.

### The body

| field | rule |
|---|---|
| `handoff_id` | required. **A lowercase UUID** — the same rule and the same words `task_id` gets, 36 characters of `[a-f0-9-]` — and **equal to the directory name** under `/tmp/.clawdline/handoffs/` |
| `project_dir` | required. Absolute, exists, and is a directory — checked now, not when the package was written |
| `assistant` | optional. `claude` or `codex`. Absent is `claude`, the same default [`POST /v1/places/:id/start`](api.md#post-v1placesidstart-post-v1placesidstartassistant) has |
| `model` | optional. `[a-z0-9._-]`, at most 64 characters, not starting with `-`. Absent means that assistant's own default |
| `title` | optional, ≤ 200 characters. What the tab is called and what the receipt line says. Absent, the tab is `handoff` and the first eight characters of the id, and the receipt drops its bracket |
| `from_session` | optional, ≤ 200 characters, free-form. Whatever the sending assistant calls the session it is in — it is looked up among the running sessions, not parsed, so no shape is assumed and none is required. Best-effort exactly as `root.session_id` is: absent, unrecognised, or an assistant that has no such id at all, and the handoff runs with nobody getting a typed receipt |

`title` and `from_session` are optional for one reason between them: **the app will not open
`handoff.md` to find out either.** It needs a name for the tab and an address for the receipt, and if
it is not told, it goes without rather than reading the file. That is the whole of the postman rule,
written as two nullable fields.

`from_session` is deliberately not specified beyond a length, and that is the assistant-neutral
answer rather than a lax one. Either assistant may send a handoff, they name their sessions
differently, and a route that insisted on one of those shapes would be a route only one of them could
call. What the app does with the value is one lookup: if it matches a session it is watching, that
session gets the receipt; if it matches nothing, nobody does, which is the same outcome as leaving it
out. A value longer than 200 characters is the one refusal here, and it is a refusal about length
rather than about form.

`model` is the only string here that reaches a command line, and it is shaped so that saying so is
not alarming — a closed alphabet with no character a shell reads. It is checked here and again on the
way to the tab, where a name that fails becomes *no flag* rather than no session.

### What the app checks

Everything it can check without reading the file:

- `handoff_id` is a lowercase UUID and names a directory that exists under `/tmp/.clawdline/handoffs/`.
- `handoff.md` exists inside it, is a regular file, and is not empty — *not empty* meaning it has a
  byte in it, since anything cleverer would be the app reading the document.
- `project_dir` is an absolute path to a directory that exists.
- `assistant`, `model`, `title` and `from_session` are in range — the last two by length only.

**It does not read `handoff.md`, parse it, check its headings, or summarise it.** The document is
between the two sessions. An app that validated its shape would be an app that has an opinion about
what a handoff is allowed to say, and the format above is a convention that ought to be able to
change without a release.

### Authorisation

The **orchestrator token**, `~/.config/clawdline/orchestrator-token`, mode `0600` — the same
credential and the same header as a dispatch, `X-Clawdline-Orchestrator`.

That is not a copy-paste of the dispatch rule, it is the same rule: this route opens a terminal tab,
starts an assistant in it, and types instructions written by somebody else into it. That is remote
code execution with a second step whichever route it arrives on, so it lives behind the file only a
local process running as you can read, and **a device token cannot reach it** — not with `send`, not
with `admin`, not over a tunnel. The reasoning is [the credential table](orchestrator.md#what-it-costs-before-anything-else)
and applies here unchanged.

**That last sentence is a mechanism and not a promise, and the mechanism is the path.** Authorisation
in `RemoteServer` is decided before any route is chosen, on a prefix: a request under
`/v1/orchestrator/` is the only kind that can be answered by the orchestrator token, and one that is
not carrying that token falls through to ordinary device authorisation. So the guarantee above is
bought by the `/v1/orchestrator/` in the URL, and by nothing else. Hanging this route off
`/v1/handoffs` would have meant either a paired phone reaching a tab-opening route, or one more
special case in the ladder that decides who may knock — and a security boundary with an exception
list is a boundary somebody eventually edits. It sits under the prefix, and inherits the whole
arrangement rather than negotiating with it: the credential, the ladder, the rate limiter, the
`orchestrator.*` shape of the audit lines.

It shares the dispatch rate limiter — **ten in ten minutes, or one full tree's worth if that is
more**, exactly as [the dispatch brake](orchestrator.md#caps) is — because a loop that opens tabs is
the same loop whichever route it calls. It spends no child, grandchild or descendant capacity,
because it creates none of those things. **A refused handoff still spends its
ticket**, the way a dispatch refused for a bad `task.json` does: the brake counts calls that reached
the machinery, not sessions that came out of it, and the failing call in a loop is exactly the one
worth counting. The same switch governs both, too — `orchestrator_enabled` off refuses this route,
and it refuses it with the same `orchestrator_disabled` a dispatch gets.

### What the app then does

1. **Opens a tab** in `project_dir` running the requested assistant, through the same start-a-session
   machinery `POST /v1/places/:id/start` and a dispatch both use. A Mac where that works is a Mac
   where this works; a refusal here is `terminal_closed` or `terminal_unsupported`, carrying the
   terminal's name in `app` as those refusals already do.
2. **Waits for somewhere to type**, then **types one line** — the same two-stage wait a briefing uses,
   because it exists for the same reason: an assistant that is still starting has a readable banner
   and no composer, and a sentence typed into that is swallowed silently. Up to five attempts inside
   four minutes; a fresh directory's *"Do you trust this folder?"* prompt is answered exactly as it is
   for a dispatch, and recorded.
3. **Confirms the line landed** by finding that first user turn in the assistant's own record —
   Claude Code's transcript, Codex's rollout — rather than trusting that bytes reached a tty.
4. **Types one receipt line** into the sender's terminal, if `from_session` was given, names a
   session this app is watching, and that session is not showing a menu. One attempt, never retried,
   and `orchestrator_notify_root` switches it off along with the dispatch lines.
5. **Settles the row and writes the audit line** — `delivered` or `spawn_failed`, and then forgets
   the handoff in every other sense. There is no watcher, because after this there is nothing to
   wait for.

#### The line

The line typed into the new session is the contract, and it is one line because Return ends a line.
**This is the canonical text — written here once, implemented word for word, and quoted rather than
rephrased everywhere else it appears, the skill included:**

```
You are picking up a Clawdline handoff. Read /tmp/.clawdline/handoffs/<id>/handoff.md before anything else and follow it: walk its REFERENCES, answer its VERIFICATION questions from those sources, say plainly what you could not reach, then continue from OPEN THREADS.
```

**It is English wherever Clawdline's interface is, and it is not localised, because it is protocol
and not interface.** The precedent is exactly one file over: a child's briefing opens *"You are a
Clawdline CHILD agent for task …"* in English on every Mac, and the only localised part of a dispatch
is the sentence the *child* is asked to say out loud to the person watching. A handoff has no such
sentence — what it has is a receiving session that reads this line and acts on it — and the person
sees a language they read the moment that session answers in it. Translating the line would mean
eight strings that a receiver has to be able to recognise, in place of one that it does.

Two parts of it are load-bearing beyond their meaning. The path is the invariant: everything else the
receiver needs is downstream of that file. And *"You are picking up a Clawdline handoff"* is not
throat-clearing — it is the only literal mark that says how this session began, the counterpart of
the mark a dispatch leaves in a child's first turn, and a session that starts without it is one
nobody can identify a week later.

The receipt is a pointer, not a report:

```
[clawdline] handoff 7c1e9b02 (Cloud planning line) picked up by codex in /Users/you/code/clawdline
[clawdline] handoff 7c1e9b02 opened a tab but the first line never landed — type it in by hand
```

Without a `title` there is no bracket — `handoff 7c1e9b02 picked up by codex in …` — for the same
reason the tab falls back to `handoff 7c1e9b02`: the app was not told, and it will not open the
document to find out. Both lines are English, as the dispatch receipts beside them are.

### The refusals

The codes are the dispatch route's, and so is the order they are decided in — same ladder, same
namespace, one route further along:

| `code` | status | when |
|---|---|---|
| `forbidden` | 403 | `X-Clawdline-Orchestrator` is missing or wrong. Decided at the door, before the body is looked at |
| `bad_request` | 400 | the body is not JSON, or has no `handoff_id` in it. The route's own check that it was handed a request at all |
| `orchestrator_disabled` | 403 | the orchestrator is switched off in Settings. One switch over both routes; its label in Settings speaks of dispatch |
| `bad_task` | 422 | a `handoff_id` that is not a lowercase UUID; a `project_dir` that is absent, relative or not a directory; an `assistant`, `model`, `title` or `from_session` out of range; **or a package directory or `handoff.md` that is not there, or is empty** |
| `rate_limited` | 429 | more than ten calls — dispatches and handoffs together — in ten minutes, or more than one full tree's worth if that is larger |
| `terminal_closed` · `terminal_unsupported` | 409 | there is no terminal to open a tab in, or not one this can drive. `app` names it |
| `not_found` | 404 | this build has no handoff route |
| `internal` | 500, 502 | a tab that would not open |

That is an order and not a list, which is worth two sentences. **The id is checked and a replay
answered before a ticket is taken**, so retrying a call that already landed costs nothing, and
everything past that point spends one, refusals included. And **`terminal_closed`,
`terminal_unsupported` and `internal` are the only three reachable once the package has been read**,
because by then the terminal is the last thing left that can fail — the typing that follows is past
the end of this table altogether, and turns into an audit line instead. `not_found` sits outside the
order: it is what a build with no handoff route answers, and such a build never reaches any of the
rest.

**A missing package is `bad_task` — 422, not 404** — and the split is deliberate: the caller wrote
that directory itself moments ago, so its absence is a bad field rather than a missing resource,
which is the same answer and the same status a dispatch gives when `task.json` is not where its
`task_id` said it would be. That leaves `not_found` free to mean the one thing a client genuinely
cannot recover from, *this Clawdline is too old*. There is no new error code on this page — the
`task` in `bad_task` is the ladder's word, not a claim that a handoff is a task — and that is the
check that this route is not a new subsystem.

### There is no second route

No `GET /v1/orchestrator/handoffs`, no cancel, no state to poll. **The session it opened is a root**,
so the whole existing session surface already addresses it:
[`GET /v1/sessions`](api.md#get-v1sessions) lists it,
[`POST /v1/sessions/:id/send`](api.md#post-v1sessionsidsend) types into it,
[`POST /v1/sessions/:id/end`](api.md#post-v1sessionsidend) closes it. A handoff is an event and not
an object with a lifecycle, which is why [the row it leaves](#what-the-app-remembers) is an envelope
rather than a resource: it answers *did this one already go?* and it is not addressable. What it
leaves besides is five audit lines: `handoff.open` when the call is accepted, `handoff.menu` when a
trust prompt is answered, `handoff.inject` for each typing attempt, and `handoff.delivered` or
`handoff.undelivered` when delivery settles.

---

## Two roots in one working tree

A handoff that forks — sender still alive, receiver now working — makes two roots in one checkout,
and everything [`README.md`](../README.md#several-sessions-one-working-tree) says about that applies
to both of them. Neither is the other's parent, so nothing about the dispatch tree softens it:

- **If the receiver dispatches, it declares `claims` like anybody else.** An intersecting claim from
  the sender's live task is `409 workspace_busy` across two definitely identified roots, and that
  refusal is correct rather than an artifact of the handoff. A handoff copies context; it does not
  transfer any reservation the sender is holding.
- **The pair gets L1's directory warning** on the first dispatch each makes into the shared directory,
  unless both tasks declared claims that rule the collision out.
- **The tree's own rules reach the receiver the way they reach everybody** — through
  [`AGENTS.md`](../AGENTS.md) in the checkout, which is what a session reads on arrival. Say in
  `CONSTRAINTS` which of them bind this line of work particularly; do not restate the file.

If the intent was *"I am done, you continue"* rather than a fork, the sender should say so and be
closed. Two roots that both believe they own the same open thread is the failure worth naming here,
and the mechanism only covers half of it. The accidental half is closed: a retried call finds
[its row](#what-the-app-remembers) and opens no second tab, across a restart too. The deliberate half
is not, and cannot be — **nothing in a package says whether the session that wrote it is still
working**, so a second package written on purpose is indistinguishable from a fork that was meant.
Saying which one it is, out loud, is the sender's job and nobody else's.

---

## The skill

A session does not build this by hand. [`skills/clawdline/`](../skills/clawdline/) — the same skill
that dispatches — carries a **Handoff** section that turns *"use Clawdline Handoff"* into a package
and a call: write the document to the eight headings, durably archive anything volatile it cites,
`uuidgen` and `umask 077` and a directory, then the route. There is an English `SKILL.md` and a
Traditional Chinese `SKILL.zh-TW.md` of the same text; install one, not both.

What lives there and not here is the recipe; what lives here and not there is why each step is shaped
the way it is. Neither repeats the other, which is the same rule the documents themselves are asked
to follow.

---

The dispatch side of this — tasks, children, claims, the state machine, what it costs — is
[`docs/orchestrator.md`](orchestrator.md). Every route in full is [`docs/api.md`](api.md). The trial
that produced the format is [`artifacts/2026-08-26-cloud-architecture-handoff.md`](../artifacts/2026-08-26-cloud-architecture-handoff.md)
and the receiving session's report on it is [`artifacts/2026-08-26-handoff-verification.md`](../artifacts/2026-08-26-handoff-verification.md).
