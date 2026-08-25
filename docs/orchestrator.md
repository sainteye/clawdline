# Handing work to another session

A session you are talking to is a session you are waiting on. Some of what people ask for does not
need the conversation it was asked in — generate the image, run the suite, read this diff and tell
me what is wrong with it — and doing it in-line costs the one thing the conversation is for, which
is your attention on the thing you were actually doing.

Clawdline already knows every terminal on this Mac: which of them is running an assistant, which is
idle, which is showing a menu, and how to open a new one in a directory. That is most of what a
dispatcher needs. This is the rest of it — a way for one session to say *do this over there*, and
be told when it is done.

**Three parties, and the middle one is the app.**

- **Root** — a session you are in. It writes a task down and asks for it to be run. Nothing else.
- **Broker** — Clawdline. It checks the ask, records it, opens a terminal tab, types the first
  message into it, watches for the answer, adds up what it cost, and tells the root.
- **Child** — the session the broker opened. It does exactly one task and writes down what
  happened. It may hand parts of that task to children of its own; those may not.

The root never touches a terminal and never learns the child's id until the broker tells it. The
child never learns who asked. Neither of them can dispatch on the other's behalf, and a child may
dispatch only one level further — [that is a rule with teeth](#depth-stops-at-two-and-the-floor-is-what-has-teeth).

**The transcripts on this page are written to the protocol rather than pasted out of a run.** Every
route, field and code below is the contract; where a reply is shown it is what a correct
implementation answers, marked *example* where it matters. The routes themselves are in
[`docs/api.md`](api.md#post-v1orchestratortasks) with the same shapes.

---

## What it costs, before anything else

**Dispatching is remote code execution with a second step.** `POST /v1/orchestrator/tasks` does not
type a line into an existing session — it opens a *new* terminal tab, starts an assistant in it, and
gives it instructions written by somebody else. Anything that can dispatch can run programs on this
Mac, as you, in a directory it chose, for up to four hours.

That is the same power [`POST /v1/sessions/:id/send`](api.md#post-v1sessionsidsend) has, and it is
deliberately **not** behind the same door. Sending is a thing a *paired device* does — a phone, a
browser, something across a tunnel. Dispatching is a thing a *local process running as you* does,
and the difference in who is asking is large enough that sharing one credential between them would
be a mistake in both directions: a phone that may type into a session should not be able to spawn
three more, and a script on this Mac should not have to be paired to a Mac it is already on.

So there are three credentials in this feature and they are not interchangeable.

| credential | where it lives | what it can do | what it cannot |
|---|---|---|---|
| **Orchestrator token** | `~/.config/clawdline/orchestrator-token`, mode `0600`, minted when the server starts | dispatch, cancel, read every task | it is never served over HTTP, never given to a child, never written under `/tmp`, and never accepted from a device that merely holds a device token |
| **Task secret** | 64 hex characters, made by the root, handed to the child inside the injected first message | say "this one task is finished" | nothing else. It names one task, it dies with that task, and the app keeps only its SHA-256 |
| **Device token** | `~/.config/clawdline/remote.json` — the paired phones and browsers from [`docs/remote.md`](remote.md) | read task state; cancel a task, if it has `send` and the write switch is on | **dispatch.** Not with `send`, not with `admin`, not over a tunnel, not ever |

Everything below follows from that table.

### Source address means nothing here, exactly as it means nothing there

Once a tunnel is running, every request arrives from `127.0.0.1` — `cloudflared` connects to the
local port like any other program, so a request from a phone in another country has the same socket
address as a `curl` in the next window. That is written up in
[the threat model](remote.md#it-only-listens-on-loopback-is-not-a-boundary) and it is why this
feature could not be built on *but it came from this machine*.

What separates "a local process running as you" from "somebody on the other end of the tunnel" is
therefore a file, not an address: `orchestrator-token` is mode `0600` in your home directory, and a
request either presents its contents or it does not. A page on the internet cannot read files. A
phone cannot read files. `curl` in your own terminal can, which is the entire point.

The pre-route checks still apply first, because they apply to everything: a request whose `Host` is
not a name this server answers to is refused before the path is looked at, and so is a cross-site
subresource. An orchestrator route is not a hole in either.

**What this is not a defence against is the same thing nothing at this layer defends against.**
Something already running as your user can read `orchestrator-token` exactly as it can read
`~/.ssh/id_ed25519`. If that has happened, this feature is not your problem.

### Depth stops at two, and the floor is what has teeth

Dispatch without a floor is a fork bomb with a language model in it. One task becomes five becomes
twenty-five, each one a real terminal tab running a real assistant against a real API bill, and the
failure mode is not "it got slow" — it is a Mac with sixty tabs open and no obvious way to tell
which of them started it.

So the tree has a bottom, and it is close to the top. **A root's children may dispatch. Their
children may not.** That is not a number picked to be safe; it is where the arithmetic stops being
something a person can hold in their head. Five and three is twenty at full stretch, which is
already more terminals than anybody wants to audit. Five, three and three would be sixty-five.

The floor is enforced twice, and the two fail differently.

**The briefing says so.** `CHILD.md` tells every child which level it is on: one with room under it
gets the whole recipe for dispatching — including `root.parent_task`, the field that says where the
new task hangs — and one standing on the floor is told plainly not to. `~/.claude/skills/clawdline/SKILL.md`
carries the same rule for a root. A child that follows its instructions never has to find the limit
by hitting it.

**The app refuses.** A dispatch names who is asking — the task it hangs under, the session id, or
both — and the new task lands one level below whatever that turns out to be. Past the floor it is
`409 depth_exceeded`. This is the stop that holds when the first one does not: a child talked into
ignoring its briefing still cannot get a task registered, because the refusal does not depend on its
cooperation.

**The two names are combined by taking the deeper answer.** A caller can lie about either one. It
cannot lie its way *up*: claiming a shallower parent than it has is worth nothing when the other
signal still says otherwise, and claiming somebody else's identity moves the task into that
session's bucket rather than out of everybody's. The ceiling below is what closes the rest.

### Caps

- **Five children at once, per session** — `orchestrator_max_children` in
  `~/.config/clawdline/config.json`, valid 1…10. Counted per dispatcher rather than per Mac: what
  the number bounds is how much work one conversation can have out at a time. A sixth is
  `429 over_capacity`, carrying `retry_after` so a client waits instead of spinning.
- **Three of those, for a child** — `orchestrator_max_grandchildren`, valid 0…10. `0` is the rule
  this app had before the second level existed: a child that tries is refused at the door. It is a
  stop on the same list rather than a switch of its own, because "how many" and "whether" are the
  same question asked twice.
- **One full tree, for the Mac** — `orchestrator_max_children × (1 + orchestrator_max_grandchildren)`,
  twenty by default, and not a setting because it is not a choice separate from the two it is made
  of. The per-session caps are the ones a caller could sidestep by claiming to be somebody else;
  this is the one that still holds when it does.
- **Ten dispatches per ten minutes**, the same rolling window the pairing route uses, or one full
  tree's worth if that is more — a brake on a loop should not refuse the work the caps just
  permitted. `429 rate_limited` after that.
- **Four hours, absolute.** `timeout_minutes` is 1…240 and 30 by default. A child that has not
  reported by then is `timeout`, whatever it is still doing.
- **Two minutes to be briefed.** A tab that opens but never reaches a state where the first message
  can be typed is `spawn_failed` rather than a tab sitting there forever with a task attached to it.

And one switch: `orchestrator_enabled`, default true. Off, and dispatch is refused at the door.

---

## The filesystem is the protocol

Everything a child needs and everything it produces is in one directory. That is not an
implementation detail — it is the interface, and it is a directory rather than a socket for a
specific reason: **a child is an ordinary assistant session with no special client library.** It
can read a file and write a file. Anything that required it to hold a connection, speak a protocol,
or keep a credential alive across a compaction would be a design that only works when the child is
having a good day.

```
/tmp/.clawdline/                 # 0700
  <task-id>/                     # 0700 — lowercase UUID
    task.json                    # the root writes this, before dispatching
    CHILD.md                     # the app writes this, just before injection
    result.json                  # the child writes this, when it is done
    artifacts/                   # whatever the child was asked to produce
```

`0700` on both, and `/tmp` is a shared directory on a Unix machine — so a task directory is readable
by you and by root and by nobody else on the box. It holds instructions and outputs and it never
holds a secret: not the orchestrator token, and not the task secret.

### `task.json` — written by the root, before it asks for anything

```json
{
  "clawdline_protocol": 1,
  "task_id": "3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234",
  "kind": "image",
  "assistant": "codex",
  "project_dir": "/Users/you/code/clawdline",
  "title": "Project portrait, medieval hand-drawn",
  "instructions": "You are in /Users/you/code/clawdline … write the SVG to artifacts/project-portrait.svg",
  "deliverables": ["artifacts/project-portrait.svg"],
  "timeout_minutes": 30,
  "created_at": "2026-08-24T10:14:02Z",
  "root": {
    "session_id": "841cbb8d-58b1-4765-9a71-bcdba19bcfef",
    "assistant": "claude",
    "project_dir": "/Users/you/code/clawdline",
    "label": "clawdline main",
    "parent_task": null
  }
}
```

The file is written **first** and the dispatch carries only the id and the secret. The reason is
worth a sentence: instructions are the part most likely to be long, to contain quotes and newlines
and someone's Chinese, and to be edited and re-sent. Putting them in a file that the root owns and
the app validates keeps all of that out of an HTTP body, and makes a retry the same request rather
than a new one.

Validation is strict and the refusal is `422 bad_task` with a message naming the field:

| field | rule |
|---|---|
| `clawdline_protocol` | exactly `1` |
| `task_id` | `^[a-f0-9-]{36}$`, and **equal to the directory name and to the id in the dispatch body**. Three places, all three checked |
| `kind` | `image` · `code-review` · `test` · `custom` |
| `assistant` | `claude` or `codex` |
| `project_dir` | absolute, exists, and is a directory — checked at dispatch, not at planning time |
| `title` | ≤ 200 characters |
| `instructions` | non-empty, ≤ 16 KiB |
| `timeout_minutes` | 1…240; absent means 30 |
| `root.session_id` | a Claude Code session UUID, or `null` |
| `root.parent_task` | the dispatcher's **own** task id, when the dispatcher is a child. `null` from a root. A value that is not a task id is read as `null` |

`root.session_id` being nullable is deliberate. A root that cannot work out its own id — Claude
Code has no route to ask — still gets to dispatch; it just does not get told when the task finishes
and has to poll instead. **A best-effort field must not be a required one**, or the honest answer
"I don't know" becomes a reason to invent something.

`root.parent_task` is the same field one level down, and it exists because a child knows its own
task id from the first line it was ever sent, long before this app has worked out what the session
in that tab calls itself. For a Codex child it is the only usable answer at all: its session id
lives in a rollout file rather than in the hook notes `root.session_id` is matched against. Naming
it is what gets a task filed under its actual parent on the first try instead of being counted as a
root's. Getting it wrong costs capacity and never buys any — [the two names are combined by taking
the deeper answer](#depth-stops-at-two-and-the-floor-is-what-has-teeth).

### `CHILD.md` — written by the app, read by the child

The child's first message is one line:

```
You are a Clawdline CHILD agent for task 3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234. Read /tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/CHILD.md and follow it exactly. TASK_SECRET=…
```

One line, because it is typed into a terminal and Return ends it. Everything that would not fit is
in `CHILD.md`, which the app writes immediately before injecting: where the task is, where the
outputs go, how long it has, whether it may dispatch and how many, that it must not read other task
directories, and exactly what `result.json` has to look like.

It also says what language to speak. The briefing itself is English so every assistant reads it
the same way, but the person watching the tab is whoever set Clawdline's language — so the file
names that language and asks for `result.json`'s `summary` in it. The one fixed line the child
says before anything else (*收到 Clawdline 派來的任務：專案肖像——開始處理。*, in the app's own
words for that language) rides in the typed message itself, ahead of "read CHILD.md" — an
assistant answers the line before it opens the file, and the first thing on the screen should be
what the tab was sent to do. Once it has read the task, it adds a line of its own about what it is
going to do and where the output will land.

The tab is also *called* by its task. Every Clawdline surface — the panel, the island, the phone —
labels a session the app opened for a task with that task's `title`, whichever assistant is in it,
from the moment it appears; and a Codex child's thread is named the same way through the
app-server, so `codex resume` lists it by what it did rather than by the first line it was handed. A tab that opens and starts working in silence is a tab nobody can tell apart from a
stray one; a tab that says what it was sent to do is a child.

**The secret is in the message and not in the file**, and that asymmetry is the whole design.
`CHILD.md` sits in a directory; the message goes into a terminal's input. A file on disk is a thing
that can be read later, by something else, after the task is over. The plaintext secret exists in
the app's memory from dispatch until injection and is dropped the moment the line lands.

### `result.json` — written by the child, and it *is* the signal

```json
{
  "clawdline_protocol": 1,
  "task_id": "3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234",
  "task_secret": "…the value from the first message…",
  "status": "success",
  "summary": "Wrote a 1024×1024 SVG portrait; border and lettering hand-pathed, no raster.",
  "artifacts": ["artifacts/project-portrait.svg"],
  "finished_at": "2026-08-24T10:41:55Z"
}
```

Written last, and written atomically — to `result.json.tmp` and then `mv`, so the watcher never
sees half of it. The app checks it once a beat for every briefed task, hashes `task_secret`, and
compares against what it stored at dispatch in constant time. A file whose secret does not match is
**ignored** and logged once: a wrong secret in a task directory is either a bug or somebody
poking, and neither is a reason to finalize somebody's task.

There is an HTTP way to say the same thing —
[`POST /v1/orchestrator/tasks/:id/complete`](api.md#post-v1orchestratortasksidcomplete) — and it is
optional on purpose. A child in a sandbox with no outbound network still finishes correctly,
because the file is the protocol and the route is a convenience for a child that would rather not
guess whether the app is watching this second.

---

## The lifecycle

```
queued ──▶ spawning ──▶ briefed ──┬─▶ success
                                  ├─▶ failure
                                  ├─▶ timeout
                                  └─▶ cancelled
   └──────────┴────────────────────▶ spawn_failed
```

**queued** — registered. The task.json validated, the secret's hash stored, the caps checked. A task
is in this state for as long as it takes to ask the terminal for a tab, which is normally not long
enough to observe.

**spawning** — a tab exists. The app asked its normal start-a-session machinery for one, in
`project_dir`, running the requested assistant — the same path `POST /v1/places/:id/start` uses, so
a Mac where that works is a Mac where this works. A refusal here (no terminal running, a terminal
that cannot be driven) is `spawn_failed` with the reason kept.

**briefed** — the first message has been typed and accepted. Getting here is the fiddly part and it
is worth knowing what the app is waiting for, because it explains the delay: the tab has to appear
in the session list, the assistant has to actually be up (a shell that has not finished starting
`codex` is not a session yet), and the session has to be idle rather than showing a menu. A fresh
directory raises Claude Code's trust prompt, and the app answers that one — option `1`, once per
task, written to the audit log — because a task that stalls on a dialog nobody is looking at is a
task that fails at the two-minute deadline for no reason anyone could see.

Once briefed, the plaintext secret is gone from memory and the task is the child's problem.

**success · failure** — from `result.json`, or from the complete route, whichever arrives. Both go
through the same finalize.

**timeout** — `briefedAt + timeout_minutes`. There is a second way to get here that is not a clock:
if the child's terminal disappears from the session list for more than a minute while the task is
briefed, the task is `failure` with *child session ended without reporting*. A closed tab is a
finished task in every sense except the one that would have been written down.

**cancelled** — somebody asked. The child's terminal is ended politely, the way
[`POST /v1/sessions/:id/end`](api.md#post-v1sessionsidend) does it. There are two ways to ask. One
is the cancel route. The other is **closing the root session**: ending a session through
[`POST /v1/sessions/:id/end`](api.md#post-v1sessionsidend) — the Close button on the page — cancels
every live task that session dispatched and closes each child's tab first, and only then closes the
root's own. The order is not cosmetic. A task is matched to its root by the session id in that
session's hook note, and the note is reached through the tty of the tab that is about to disappear:
a root closed first is a root that can no longer be matched to anything, and its children run on
reporting into a conversation that ended. Both ways write `orchestrator.cancel` to the audit log,
and the cascade carries `why=root_ended` — a task cancelled *for* you should not read like one you
cancelled.

A task that had already **finished** keeps its record — `success` stays `success`, the result file
is still there — and loses only the tab. Those tabs are the linger described below: a finished
child is left on screen for a while so somebody can read what it did, and the page draws it
indented under the root that asked for it. When that root leaves, the reader the linger was for
has gone, and a row filed under a session no longer in the list is not a courtesy. Those closures
are `orchestrator.close`, also with `why=root_ended`. The tab is what this is keyed on, not the
linger deadline — that deadline lives only in memory, so every task older than the app's last
restart has none while its tab is plainly still there, and the page decides the same way.

**Only an explicit close cascades.** Closing the tab by hand does not. The app never watches a root
for signs of death, because "not in this reading" is a sentence that is also true of a terminal that
lost its accessibility permission for a moment, and the cost of being wrong there is somebody's work
killed mid-turn. A busy child gets no grace period either; somebody pressed a button that says close.

**The cascade reaches both levels, deepest first.** A child's own children go before it does — the
level below is found through the session id its parent goes by, and that stops being a useful thing
to match on the moment that parent's tab is gone. It is gathered from the *finished* children too,
not only the live ones: a child that reported while the work it handed on is still running would
otherwise leave a grandchild belonging to nobody. Cancelling a single task does the same thing on a
smaller scale — what that task handed on goes with it, since it is work nobody is waiting for any
more.

**spawn_failed** — the tab never happened, or never got briefed inside two minutes, or the app was
restarted while the task was still in `queued`/`spawning`. That last one is not a bug: the plaintext
secret lived only in memory, so a task that had not been briefed before a restart can never be
briefed, and saying so beats leaving a row that will sit at `spawning` forever.

**A briefed task survives a restart.** Its secret is on disk as a hash, `result.json` is on disk as
a file, and the timeout is arithmetic on a stored timestamp. So the app comes back up, reads the
registry, and carries on watching. That is the one restart case that matters, because it is the one
where a child is out there doing work.

**The tab goes away afterwards.** A child that reported — `success` or `failure` — has nothing left
to say, so `orchestrator_child_linger` decides how long its terminal tab hangs around: three minutes
by default, `0` to close it the moment the task finalizes, `-1` to leave it to you. Only a reported
child is closed. A `timeout`, a `spawn_failed`, a child that never came up at all: those tabs stay
exactly where they are, because whatever went wrong is written on that screen and closing it would
throw away the only copy.

### Being told

When a task finalizes, the app looks for the root's terminal — the root declared a session id, and
Clawdline knows which tty each session id belongs to from [its hooks](hooks.md) — and if it finds
one that is not currently showing a menu, it types one line into it:

```
[clawdline] task 3f9a21bc (Project portrait, medieval hand-drawn) finished: success — see /tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/result.json
```

That is the whole notification and it is deliberately small. It is not the summary — the summary can
be a paragraph and this is a line typed into somebody's prompt, possibly while they are mid-sentence
— it is a pointer, and the root reads the file when it gets round to it. One attempt; a failure is
logged and not retried, because the second copy of a notification is worse than none.

`orchestrator_notify_root` turns it off for anybody who would rather poll. Every task change also
goes out on [the event stream](api.md#the-event-stream) as an `orchestrator` frame, which is how the
web interface and anything else watching finds out without asking.

### Cleanup

At start and every six hours: task directories for terminal tasks that finished more than 24 hours
ago are removed, and the registry keeps its most recent 200 records. **Artifacts are in `/tmp` and
they are not yours to keep** — if a child produced something worth having, copy it out. The
directory going away after a day is the same promise `/tmp` always made, made explicitly.

---

## What it cost

Every finalized task carries a `usage` object, and it is best-effort by construction: harvesting it
never blocks a finalize and never fails one. A task that finished is finished whether or not the
accounting worked.

The numbers do not come from anywhere new. A child is an ordinary session and it leaves an ordinary
transcript, so this reads the same files [`GET /v1/sessions/:id/transcript`](api.md#get-v1sessionsidtranscriptlimit200)
reads — Claude Code's `~/.claude/projects/<slug>/<uuid>.jsonl`, or Codex's
`~/.codex/sessions/YYYY/MM/DD/rollout-….jsonl`. Which transcript belongs to which child is answered
by the hooks for Claude and by the rollout's own directory, start time and pid for Codex.

- **Claude** — sum the assistant rows' `message.usage`: input, output, cache reads, cache writes,
  and the model from the last row that named one.
- **Codex** — the last `token_count` event carries the running totals for the whole session, so
  reading from the end and stopping at the first one is both cheaper and more correct than adding
  up turns.

`costUsd` is arithmetic on published per-million prices, prefix-matched on the model id, with cache
reads at a tenth of the input rate and cache writes at 1.25×. **It is `null` when the model is not
one this knows**, and that includes every OpenAI model — Codex is billed against a plan rather than
per token, so a dollar figure there would be a made-up number wearing a currency symbol. A Codex
task shows tokens and no cost, and that is the honest reading rather than a gap.

```json
{"usage":{"input":48210,"output":9330,"cacheRead":412880,"cacheWrite":31200,
          "total":501620,"model":"claude-sonnet-4-5","costUsd":0.4243}}
```

---

## The skill

A root session does not talk to this API by hand. `~/.claude/skills/clawdline/SKILL.md` is a global
Claude Code skill — every project, this machine only — and it is what turns *"get codex to draw
this"* into a dispatch. Six steps:

1. **Find the door.** `remote_port` out of `config.json` (7717 if absent),
   `~/.config/clawdline/orchestrator-token` for the token. Either one missing is a full stop with an
   explanation, not a workaround.
2. **Decide the work.** How many (the cap is 3), and which assistant — the user's word if they gave
   one, otherwise Codex for generating and running things and Claude for reading and judging them.
   Instructions have to stand on their own: the child cannot see the conversation they came from,
   so *"do what we discussed"* is an empty file with extra steps.
3. **Make the directory.** `uuidgen`, `openssl rand -hex 32`, `umask 077`, and a `task.json` built
   with `jq -n` rather than string concatenation.
4. **Find its own session id**, best-effort, with a nonce: `echo clawdline-nonce-<task-id>` puts
   that string into the session's own transcript, and a `grep -l` across
   `~/.claude/projects/<slug>/*.jsonl` finds which file it landed in. The basename is the id. Not
   found is `null` and the task still runs.
5. **Dispatch**, and branch on `code`: `over_capacity` waits or asks for fewer, `bad_task` is a file
   to fix and re-send under the same id, and **`depth_exceeded` means this session is already as
   deep as this Mac goes** — the work is its own to do, not something to find another way to hand
   on.
6. **Report, then get out of the way.** No polling loop. The notification arrives on its own, and
   `GET /v1/orchestrator/tasks/:id` answers when somebody asks.

The one rule stated before any of them: **a child dispatches only if its briefing said it could,
and what it opens opens nothing.** `CHILD.md` is where a child reads that, and it carries the same
six steps in miniature — spelled out rather than pointed at the skill, because half of these
sessions are Codex and Codex has no skills.

---

## Later: the same protocol, somebody else's machines

Everything above is one Mac talking to itself. The shapes were chosen so that it does not have to
stay that way.

`task.json`, `result.json` and the four routes carry no reference to a terminal, a tty, or a local
path that could not be a path somewhere else. The only genuinely machine-local thing in the design
is *how the credential is established* — a `0600` file that proves "a process running as this user"
— and that is one function, not a protocol.

The intended shape of a hosted version is therefore small: `clawdline.com` serves the same JSON at
the same paths, an account key replaces the token file, and `project_dir` becomes a workspace the
account owns rather than a directory on your disk. A root session would not be able to tell the
difference beyond the base URL, and neither would this document — which is the property worth
protecting while it is still cheap to protect, and the reason the local implementation does not take
shortcuts through anything terminal-shaped in the wire format.

Nothing here depends on that happening. It is written down so that the local version does not
accidentally make it impossible.

---

Every route in full, with the shapes and a `curl` for each, is
[`docs/api.md`](api.md#post-v1orchestratortasks). What the app is doing when it watches a terminal,
and why it can tell idle from waiting, is [`docs/interface.md`](interface.md).
