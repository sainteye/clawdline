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
| **Orchestrator token** | `~/.config/clawdline/orchestrator-token`, mode `0600`, minted when the server starts | dispatch, cancel, read every task | it is never served over HTTP, never written under `/tmp`, cannot recover a task secret, and is never accepted from a device that merely holds a device token |
| **Task secret** | 64 hex characters, made by the root, handed to the child inside the injected first message | say "this one task is finished" | nothing else. It names one task and dies with it. Its durable identity is a SHA-256; only a serialized waiter has a temporary encrypted copy for restart recovery |
| **Device token** | `~/.config/clawdline/remote.json` — the paired phones and browsers from [`docs/remote.md`](remote.md) | read task state; cancel a task, if it has `send` and the write switch is on | **dispatch.** Not with `send`, not with `admin`, not over a tunnel, not ever |

Everything below follows from that table.

There is also one secret that is deliberately **not a credential**:
`~/.config/clawdline/orchestrator-archive-key`, a random 32-byte key in its own mode-`0600` file.
The app creates it lazily and replaces it only when the stored bytes do not parse. It encrypts a
serialized waiter's temporary registry copy at rest so the queue can resume after a restart; it is
not derived from the orchestrator token, is not accepted by any route, and is never named or
copied into a child briefing. A child allowed to dispatch may read the orchestrator token exactly
as its dispatch instructions say, but that ability cannot be exchanged for this at-rest key or a
queued sibling's task secret. That is how the three request credentials remain non-interchangeable
without sacrificing restart recovery.

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
`~/.ssh/id_ed25519`; it can also directly read the mode-`0600` archive key. Same-uid compromise is
outside this threat model. The separation above protects the narrower capability boundary of a
child following its briefing and reading only the orchestrator token, not hostile code already
running with unrestricted access to the account.

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
new task hangs — and one standing on the floor is told plainly not to.
[`skills/clawdline/SKILL.md`](../skills/clawdline/SKILL.md) carries the same rule for a root. A child that follows its instructions never has to find the limit
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
- **Four minutes to be briefed.** A tab that opens but never reaches a state where the first message
  can be typed — or that never records having received it, after five attempts — is `spawn_failed`
  rather than a tab sitting there forever with a task attached to it.
  Claude Code may ask *"Do you trust this folder?"* before it will take a first message. Clawdline
  recognises that one menu, answers option `1` once for the task, and leaves an audit receipt;
  `permission_mode` does not govern that answer.

And two more settings. `orchestrator_enabled`, default true — off, and dispatch is refused at the
door. And `orchestrator_permission`, default `full`: **how far a child may go before it stops and
asks.**

That default is a position, not a convenience. On a tab somebody is watching, "ask about
everything" is the careful setting. On a child's tab nobody is watching, and a session that stops
for approval does not stop for a moment — it stops until the task times out, which reads
afterwards as work that silently did not happen.

**That default was arrived at by trying the narrower ones and watching each of them fail.** A
dispatched session's whole job is running commands and writing files, so every stop short of the
last one stops it somewhere: `ask` stops on the first thing it does, which is reading its own
briefing; `edits` gets it past writing a result but not past `cat`, `mkdir`, `curl` or `sleep`,
which is most of what handing work on consists of. No flag covers those and stops short of `full`.
On Haiku there is not even a middle option to reach for, since the mode that would have been one
is the one that model does not have.

What this does not widen is *who may dispatch*. That is still a `0600` file only a local process
can read, and a child already has a shell — the setting changes how many buttons a person has to
press for work they already authorised, not what that work can reach.

**The three words here are Clawdline's, and each one was read back off a real status line — on
more than one model.** That second half is the interesting part. Claude Code has an `auto` mode,
and `--permission-mode auto` selects it on Sonnet and on Opus. On Haiku the same flag produces
`manual`, the mode where everything is asked, silently.

So `auto` is not a broken value, it is a **model-dependent** one, and that is disqualifying for a
field a task fills in. A word here has to mean the same thing to every session a task can name;
one that quietly becomes the *strictest* setting on the cheapest model is the failure nobody
catches, because what it looks like afterwards is a task that timed out having done nothing.
There is no `auto` here for that reason, and the three that are here were checked on all three
models.

The setting is a ceiling as well as a default: `full` — nothing asked at all — is unreachable
unless this Mac has been set to it, because the session doing the asking is not the one that lives
with the consequences. A task asking for more than the ceiling is quietly given the ceiling; the
record says what was actually used, and so does the audit line.

**What still stops even at `full`.** Two doors sit in front of the session rather than inside it,
and no permission setting reaches either: the trust prompt (which Clawdline answers once, with an
audit receipt), and Claude Code's command screening, which refuses a
`jq -n '{…}'` line on the shape of it alone (a brace beside a quote reads as obfuscation) and
offers no "always allow". The second one is why a child that dispatches is the one case that
genuinely needs `full`.

**[`docs/dispatch-permissions.md`](dispatch-permissions.md) is the whole of this subject** — all
four gates in order, the flag-by-model table that shows `--permission-mode auto` quietly meaning
`manual` on Haiku, how a child should write a file so screening does not refuse it, the two ways a
spawn dies, and how to check whether a child was ever actually asked. Everything on that page was
read off a terminal rather than taken from a help text.

### House rules

`~/.config/clawdline/dispatch-policy.md` is what this Mac says about **how** work should be
handed out, as opposed to how much of it. It is read fresh on every dispatch — an edit reaches
the next task, not the next launch — and copied into the briefing of every child that is allowed
to dispatch in turn. A leaf never sees it: rules about choosing a model are noise to a session
with no such choice to make.

It ships with opinions rather than a comment saying "put your rules here", because a file with
defensible rules already in it is one somebody edits and an empty one is a feature nobody finds.
It opens with the two decisions that come before any of the others:

**Whether to dispatch at all.** The measurement is sharp in both directions — work that splits
into independent pieces goes 80.9% better with several agents, work where each step depends on
the last goes 39–70% *worse* — so the file states the test as one sentence: *can this be cut into
pieces that need not talk to each other?* It then lists the shapes that look dispatchable and are
not: diagnosis, dozens of small jobs, anything on a path where somebody is waiting, agents that
must talk back and forth, typed output, and work smaller than its own briefing.

**A "no" there is a recommendation, not a refusal.** The rule is to say it, give the reason in a
sentence, and ask — because the person has reasons this file cannot see, and *"I want Codex to
take this one"* is a complete one. What the check buys is that the reason arrives before the work
rather than after it went badly; it was never meant to be a veto.

**Which shape.** Five named ones, so a graph is chosen rather than improvised: *split and join*
for research, *build then read* for anything producing code, *decide then do* for a change worth
a person's eyes at the handover, *batch with takeover* for mechanical work across modules, and
*candidates* for comparing taste. Anything producing code is always *build then read*.

After that: Codex for making things and Claude for reading them; `haiku` for mechanical
single-source work, `sonnet` for a leaf with judgement in it, `opus` for a decision somebody will
act on; breadth before depth; the mechanics of dispatching; and the reviewing node every
code-producing graph ends with.

Editing it is the point. Delete the lot and the paragraph disappears from every briefing — an
empty file means there are no house rules, which is a position and not a broken setting. The
file is written once, on first use, and never rewritten: a default that grew back after being
deleted would be a setting that does not stay set. Settings → Remote has a card saying whether
there are any and a button that opens the file in whatever you write prose in.

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

An isolated task's checkout is deliberately elsewhere:
`~/Library/Application Support/Clawdline/worktrees/<repo-slug>/<task-id>/`, under a `0700` root.
It is not task-protocol storage, not subject to `/tmp` cleanup, and never appears in the remote
start-place list. Keeping it outside both `/tmp` and the repository prevents OS cleanup from
leaving stale git administration records and prevents another session's `git status` from seeing
the checkout. `CHILD.md`, the secret flow, `result.json`, and artifacts stay in the task directory.

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
  "model": "gpt-5.1-codex",
  "project_dir": "/Users/you/code/clawdline",
  "title": "Project portrait, medieval hand-drawn",
  "instructions": "You are in /Users/you/code/clawdline … write the SVG to artifacts/project-portrait.svg",
  "deliverables": ["artifacts/project-portrait.svg"],
  "plan": "root → 3 searchers (haiku) → this one joins them up (opus) → report.md",
  "claims": ["Sources/Orchestrator.swift", "docs"],
  "serialize": ["build"],
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
| `model` | optional. `[a-z0-9._-]`, at most 64 characters, not starting with `-`. Absent means that assistant's own default |
| `permission_mode` | optional. `ask` · `edits` · `full`. Absent takes `orchestrator_permission`, which is also the ceiling — asking for more than it gives you it instead |
| `plan` | optional, ≤ 4 KiB. The whole graph this task is one node of |
| `claims` | optional array of 0…32 unique POSIX paths relative to `project_dir`; each is 1…1024 characters, may not start with `/`, and may not contain a `..` component. A directory claim covers its whole subtree; `[]` explicitly declares read-only work |
| `serialize` | optional array of 0…4 unique operation names. Each uses the `model` token rule: 1…64 characters from `[a-z0-9._-]`, not starting with `-` |
| `isolation` | optional `none` or `worktree`; absent is `none`. Unknown values are refused, never downgraded |
| `isolation_base` | optional Git revision, legal only with `isolation: "worktree"`; 1…200 characters from letters, digits, `._/-~`, not starting with `-` and not containing `..`. Absent means `HEAD`; it must resolve to a commit |
| `project_dir` | absolute, exists, and is a directory — checked at dispatch, not at planning time |
| `title` | ≤ 200 characters |
| `instructions` | non-empty, ≤ 16 KiB |
| `timeout_minutes` | 1…240; absent means 30 |
| `root.session_id` | a Claude Code session UUID, or `null` |
| `root.parent_task` | the dispatcher's **own** task id, when the dispatcher is a child. `null` from a root. A value that is not a task id is read as `null` |

`root.session_id` being nullable is deliberate. A root that cannot work out its own id — Claude
Code has no route to ask — still gets to dispatch. **A best-effort field must not be a required
one**, or the honest answer "I don't know" becomes a reason to invent something.

Two things follow from leaving it out, and the second one surprises people. The task is not told
when it finishes, so the dispatcher has to poll. And **its row has nothing to sit under**: the
list indents a child beneath the session that asked for it, that session is found through this
id, and a task that named nobody is filed under nobody. The row still says `Child`, because being
somebody's child is a fact about that session whether or not the parent is on screen — but it
floats at whatever position the sort gave it, which reads at a glance like a bug in the grouping
rather than a task that declined to say who asked. If a row belonging under yours matters, send
the id.

`model` is the **only** string a dispatch puts on a command line, and it is shaped so that
saying so is not alarming: not a fragment of a command but a name out of a closed alphabet. No
character it admits is one a shell reads — no space, no quote, no `$`, no `;` — so
`claude --model <name>` stays one command with one argument whatever arrives. It is checked in
two places on purpose: here, where a typo can be answered with `bad_task` while somebody is still
holding the request, and again in `StartPoints.modelName` on the way to the tab, where a name
that fails becomes *no flag* rather than no session. The route a phone can reach passes nothing.

`plan` is the graph, not this task's job — the same text in every task of one dispatch. It goes
near the top of `CHILD.md`, above even the language rule, because it is the context every other
line is read in: a child that knows its answer is one of four being joined together writes
something joinable, and one that does not writes a report. Leaves get it too, and that is the
half that matters — a leaf that knows what its output feeds is the difference between an answer
and an essay.

`root.parent_task` is the same field one level down, and it exists because a child knows its own
task id from the first line it was ever sent, long before this app has worked out what the session
in that tab calls itself. For a Codex child it is the only usable answer at all: its session id
lives in a rollout file rather than in the hook notes `root.session_id` is matched against. Naming
it is what gets a task filed under its actual parent on the first try instead of being counted as a
root's. Getting it wrong costs capacity and never buys any — [the two names are combined by taking
the deeper answer](#depth-stops-at-two-and-the-floor-is-what-has-teeth).

For `worktree`, the broker resolves the base to a commit SHA and records that immutable value.
Branch names and `HEAD` can move while other sessions commit; the SHA is the receipt for what the
child actually started from. A serialized task therefore omits the `worktree` object until its tab
exists; its eventual base is resolved only as it leaves the queue. A dirty base is admitted with a warning:
uncommitted files do not cross into the clean checkout. The branch is always
`clawdline/task/<complete-task-id>`.

### Reserving declared write paths at dispatch

`claims` is a dispatch-time reservation for paths a task may write. Each entry is relative to
`project_dir`. At registration the broker standardises and resolves that existing directory once,
then normalises `.` and empty components in each relative claim and joins them literally. The
resulting absolute strings are frozen in the lease and compared with exact, case-sensitive
spelling; the possibly nonexistent target is never resolved, so creating it cannot change its
key, and `/tmp` and `/private/tmp` project-root spellings converge. Equal paths conflict, as do
ancestors and descendants: `Sources` covers
`Sources/Orchestrator.swift`, while `a/b` and `a/bc` are unrelated. Absolutising first is important
when two tasks use nested project directories — `project_dir=/repo` plus `packages/app/Sources`
is the same reservation as `project_dir=/repo/packages/app` plus `Sources`.

The field has three states, and the registry and every GET record preserve the distinction:

| `claims` in `task.json` | Meaning | Lease and L1 behavior |
|---|---|---|
| one or more paths | the task declares exactly these write scopes | reserves their frozen keys; disjoint declarations can silence L1 |
| `[]` | the task positively declares that it is read-only | reserves no lease, never conflicts or receives `409 workspace_busy`, and can silence L1 |
| absent | the task's write set is unknown | reserves no lease; L1 keeps its directory warning |

An empty array gives a read-only task an active, harmless declaration. Silence therefore has only
one meaning: both tasks supplied enough scope information to prove their frozen claim sets do not
intersect. Merely omitting the field never makes that assertion.

The check and registration happen atomically as soon as the dispatch has validated. A serialized
task reserves its claims for its entire time in `queued`; promotion is not a second gap where
another root can enter. A live claim from a different root refuses the new dispatch immediately
with `409 workspace_busy`, before serialization and before L1 warnings or a terminal spawn. The
error names the blocking task, its title and root label, when it was created, every conflicting
absolute path, and advisory `retry_after: 60`. The rejected task is not registered, and the audit
log records `orchestrator.claims.blocked`. It also does not consume an entry in the ten-minute
dispatch rate limiter, so following the retry advice cannot turn repeated `workspace_busy`
answers into `rate_limited` by itself.

Tasks in the same root tree may overlap because that root owns the graph and may have ordered the
work itself. Their dispatch succeeds and adds a `claims_overlap` item to `warnings`, naming the
other task and the conflicting absolute paths. Root identity is the same `parent_task` walk L1
uses, not merely the immediate `root.session_id` spelling. Only two successfully resolved,
different roots create a hard refusal. If either side is unknown, dispatch succeeds with the same
warning shape under `claims_overlap_unknown_root`; a null root cannot hard-block somebody else.

The task record is the lease: every GET record shows its declared `claims`, and every terminal
state — `success`, `failure`, `timeout`, `cancelled`, or `spawn_failed` — stops holding them. That
includes a queued task whose pump cannot open a child, because pump failures use the same finalizer.
Timeout deliberately leaves the child tab open for inspection, so the tab may still be writing
after its claims are released; the root receives a typed line that says both facts explicitly.
Cleanup work is work too: a separately dispatched cancel, revert, rollback, or removal task must
declare every path it may change just as an entering task does. That is what lets it collect only
the state its own declared scope covers instead of quietly damaging another root during exit.

Claims have an intentionally honest boundary. Non-empty declarations protect declaring tasks from
one another. An absent `claims` field keeps the old behavior and receives L1's directory-level
visibility; the broker does not infer a write set from instructions. Claims are a dispatch gate,
not filesystem enforcement, so a child can still write outside what it declared. Dispatch-time
refusal is valuable precisely because it removes the human negotiation window in which both
already-briefed tasks would otherwise keep editing while their roots decide what to do.

With worktree isolation, relative claims under `project_dir` are discarded with a warning because
the child edits the separate checkout, not those shared-tree paths. External machine resources
still belong in `serialize`. This also removes the shared-tree timing mismatch where a claim is
released at terminal state but work is not landed until a later commit: the isolated delivery
remains on its branch until the root explicitly integrates it.

A serialized task holds claims from dispatch throughout its entire `queued` wait. That wait has no
independent timeout: it is bounded by its serialize blockers finishing, timing out, or being
cancelled, and by cancellation of the queued task itself. `timeout_minutes` still starts only at
`briefedAt`.

### Serializing a machine-global operation

`serialize` names scarce operations rather than directories. For example, two tasks that both run
the build can use `"serialize":["build"]` when the build writes a fixed binary under `TMPDIR`.
Without that mutex, two otherwise unrelated roots can overwrite the same output — `test.sh`'s
fixed `${TMPDIR}/clawdline-tests` path is a real example.

The namespace is machine-global and deliberately ignores roots. A task acquires all of its names
together as it leaves `queued`, and holds them through `spawning` and `briefed` until any terminal
state. If any name is held, it stays queued. Waiters sharing a name are FIFO by creation time (task
id breaks an equal timestamp), including waiters from another root. A multi-name request never
holds a subset: `['build','database']` waits until both are free, so crossed token orders cannot
deadlock. Tasks with no `serialize` field take the old path unchanged.

A queued response and every GET record include `"waiting_on":["<task-id>",…]` when blockers
exist; the field is absent when there are none. It can name a current holder or an older queued
waiter entitled to a shared name first. Cancelling a queued task is immediate, opens no tab, and
pumps the next eligible waiter. Every other terminal outcome pumps the same queue, and startup
pumps it once as well. A queued task already occupies its dispatcher's children/grandchildren
slot and the machine-wide descendants slot; counting registration rather than open tabs prevents
an unbounded queue from bypassing the capacity limits.

To make that startup handoff possible, only a serialized task still waiting has an encrypted copy
of its task secret in the app's `0600` orchestrator registry. The independent random at-rest key
described in the credential section persists across restarts; neither the orchestrator token nor
any device or task credential can derive it. The sealed copy is removed before the task starts
opening; a spawning task still follows the existing fail-closed restart rule. Waiting does not
consume the work timeout: as for every task, `timeout_minutes` begins at `briefedAt`.

Queued tasks are excluded from workspace-overlap scans because they have opened no tab and touched
no file. When the background pump promotes one to `spawning`, it scans the then-current active
tasks. The original dispatch response has already returned, so any overlap warning is delivered as
the same best-effort typed `[clawdline]` line at promotion rather than retroactively added to that
response.

### When two roots share a workspace

A successful dispatch also looks at every task in `spawning` or `briefed`. If one
from a different root is working in the same `project_dir`, or one directory is an ancestor of the
other, the reply carries a `warnings` array beside `task`:

```json
{"ok":true,"task":{"id":"3f9a21bc-…","state":"spawning"},
 "warnings":[{"code":"workspace_overlap","task":"a70c5e11-3b28-4d6f-8e10-2c94b7f0d3aa",
              "dir":"/Users/you/code/clawdline",
              "message":"Task 3f9a21bc-… overlaps active task a70c5e11-… at /Users/you/code/clawdline."}]}
```

This is visibility, not arbitration. The task is still registered and opened, no state changes,
and when there is no overlap the `warnings` field is absent rather than an empty array; idempotent
retries recompute the same field from the tasks currently active. Paths are resolved and compared
by component, so `/a/b` contains `/a/b/c` but has no relationship to
`/a/bc`; spelling deliberately remains case-sensitive, as it is in `StartPoints.isDurablePlace`,
even on the usual case-insensitive APFS volume. The `dir` field and the path in `message` both name
the shared writable descendant: if an active task uses `/Users/you/code` and the new task uses
`/Users/you/code/clawdline`, both say `/Users/you/code/clawdline`. Tree identity follows
`parent_task` links back to the same root, so a depth-two task does not warn about its parent or
siblings. Without such a link, a null root session id is unknown and the overlap is reported.

There is one deliberate silence rule for a directory-overlapping pair: when both tasks have a
`claims` field (including `[]`) and their frozen claim scopes do not intersect, neither the
dispatch response nor either root's typed line reports that pair. If either field is absent, L1
warns as before. Intersecting non-empty declarations still go through claims arbitration first,
including `409 workspace_busy` across two definitely identified roots.

The new task's root also gets one aggregate `[clawdline]` line for all overlaps, while every other
root that can be found gets the line concerning its task. A root with a null session id cannot be
found and is quietly skipped. Delivery runs outside the request queue; as with completion
notification, a root showing a menu or a failed terminal send does not affect dispatch.
`orchestrator_notify_root` turns these typed lines off too.

### `CHILD.md` — written by the app, read by the child

The child's first message is one line:

```
You are a Clawdline CHILD agent for task 3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234. Read /tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/CHILD.md and follow it exactly. TASK_SECRET=…
```

One line, because it is typed into a terminal and Return ends it. Everything that would not fit is
in `CHILD.md`, which the app writes immediately before injecting: where the task is, where the
outputs go, how long it has, the graph it is one node of, whether it may dispatch and how many,
this Mac's house rules if it may, that it must not read other task directories, and exactly what
`result.json` has to look like.

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

**The secret is in the message and not in the task directory**, and that asymmetry is the whole
design. `CHILD.md` sits in a directory; the message goes into a terminal's input. A file there is a
thing that can be read later, by something else, after the task is over. The plaintext secret stays
in app memory until the child's own transcript proves the line landed. A serialized waiter also
has the temporary encrypted registry copy described above; it is deleted before spawning.

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
without `serialize` is here only until the terminal is asked for a tab. A serialized task remains
here until every requested operation is free; `waiting_on` says which older tasks stand ahead.

**spawning** — all serialized operations, if any, have been acquired and the app is opening a tab.
For an isolated task this is when the broker runs `git worktree add`, immediately before opening;
queued tasks hold no idle checkout. Failure becomes `spawn_failed` with at most 500 characters of
git's diagnostic, and the next attempt needs a fresh task id (therefore a fresh branch).
Once it exists, `spawnedAt` and `child.terminalId` appear. The app uses its normal start-a-session
machinery in the effective cwd — `project_dir` normally, or the worktree's matching monorepo
subdirectory — running the requested assistant. This is the same path
`POST /v1/places/:id/start` uses, so a Mac where that works is a Mac where this works. A refusal
here (no terminal running, a terminal that cannot be driven) is `spawn_failed` with the reason kept.

**briefed** — the child's own record shows the first message as a turn it received. Getting here is
the fiddly part, and it is worth knowing what the app is waiting for, because it explains the delay.

It waits for **somewhere to type**. A tab in the session list is not that, and neither is a process
with the right name: an assistant still starting its MCP servers has a readable banner and no
composer, and a briefing typed into that is swallowed without anything reporting a failure. So the
app waits until it can see the assistant's own empty composer, which is the first moment there is
anywhere for a sentence to go.

Then it waits for **evidence the sentence arrived**. Typing into a terminal proves that bytes
reached a tty and nothing more, so the task stays in `spawning` and keeps its secret until the
assistant's own record — Claude Code's transcript, Codex's rollout — carries that task's first user
turn. Only that moves it to `briefed`.

If the record still has no such turn once the receipt window has passed and the empty composer is
back, the app types it again, up to five attempts in total, then gives up as `spawn_failed`. It
will not send a second copy on the strength of a missing file alone: both assistants write the user
turn before they begin it, so a first message that was accepted closes the gate before it can be
run twice.

A fresh directory raises Claude Code's trust prompt, and the app answers that one — option `1`,
once per task, written to the audit log — because a task that stalls on a dialog nobody is looking
at is a task that fails at the two-minute deadline for no reason anyone could see.

This was exercised with a genuinely new checkout: the probe reached its briefing without a person
touching the trust menu. Worktree isolation also improves Codex identity matching because rollout
candidates are searched under the child's distinct cwd rather than among every session in the
shared checkout.

Once briefed, the plaintext secret is gone from memory and the task is the child's problem.

The walk that does all of this runs on the main thread and shells out to a terminal on the way,
which is a combination that has already put two walks on the stack at once.
[docs/waiting.md](waiting.md) is why, and the rule that came out of it.

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
linger deadline: a task can have a tab and no deadline — one dispatched before the linger existed,
one on a Mac that set `orchestrator_child_linger` to `-1` — and closing the root should still take
the row somebody is looking at. The page decides the same way, so what a close takes is what a
reader sees.

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

**spawn_failed** — the tab never happened, or never got briefed inside four minutes, or was typed
into five times without the child ever recording the message, or the app was restarted while the
task was in `spawning`. That last one is not a bug: once a task starts opening, the recoverable
queued secret is gone, so the app fails closed rather than risk opening the same global operation
twice. A serialized task that was still `queued` is recovered and pumped instead.

**A briefed task survives a restart.** Its secret is on disk as a hash, `result.json` is on disk as
a file, and the timeout is arithmetic on a stored timestamp. So the app comes back up, reads the
registry, and carries on watching. That is the one restart case that matters, because it is the one
where a child is out there doing work.

Replacing the app still has one unavoidable window: a task in `spawning` is failed closed on
restart. A worktree preserves files and branch state, but task lifecycle belongs to the app process;
isolation does not turn an interrupted spawn into a resumable dispatch.

### A branch, not a diff

An isolated child's delivery is `clawdline/task/<task-id>`. The child commits early, only on that
branch, and never pushes or switches branches. Finalize reads git itself and adds this object to the
record; `head`, `commits`, and `dirty` are best-effort and may be `null`:

```jsonc
"worktree": {
  "path": "/Users/you/Library/Application Support/Clawdline/worktrees/repo-a1b2c3d4/<task-id>",
  "branch": "clawdline/task/<task-id>",
  "base": "b7363e94f9d899d3f3903db7dbad075ce270494f",
  "head": "2655757a…",
  "commits": 3,
  "dirty": false
}
```

Review and land it from the repository (three dots show the child's work since the fork; two dots
also include unrelated movement at the other tip):

```bash
git -C <project_dir> log --oneline <base>..clawdline/task/<id>
git -C <project_dir> diff <base>...clawdline/task/<id>
git -C <project_dir> merge --no-ff clawdline/task/<id>   # or cherry-pick <sha>
# alternatively: git -C <project_dir> rebase --onto main <base> clawdline/task/<id>
git -C <project_dir> branch -d clawdline/task/<id>       # only after landing
```

Conflicts are the visible cost of parallel work and should be resolved during integration. Review
the branch before landing it; a child commit has not become trusted merely by being isolated.

**The tab goes away afterwards.** A child that reported — `success` or `failure` — has nothing left
to say, so `orchestrator_child_linger` decides how long its terminal tab hangs around: three minutes
by default, `0` to close it the moment the task finalizes, `-1` to leave it to you. A `timeout`
keeps its tab regardless, because whatever went wrong is written on that screen and closing it
would throw away the only copy.

**The deadline survives the restart that lands in the middle of it.** Three minutes is longer than
this app stays running while it is being worked on, and a deadline that lived only in memory was
lost every time — the tab then stood open for the rest of the day, because nothing sets a second
one. It is written down now, and read back at startup. What crosses is the deadline and nothing
else: whether that tab is still the child's is asked again, of a reading *this* process took,
matching the terminal id, the tty, and the assistant in it. A deadline that ran out while the app
was away gets twenty seconds first, so the first reading has landed before anything is judged
missing — and a reading with no terminals in it at all decides nothing, since that is also what
the first second after launch looks like, and what iTerm2 not answering looks like.

**A `spawn_failed` that never reached briefing is the exception, and it closes at once.** Nothing
of the task is on that screen: the session was opened and never spoken to, so what is there is a
fresh prompt that explains less than the summary does. And keeping it is not free — each one is a
live assistant holding a slot, while the usual reason for failing to reach a prompt in the first
place is *too many sessions starting at once*. Left alone they compound: four dead tabs still
running, and the next two spawns failing for the reason the first four did.

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
ago are removed; terminal handoff envelopes and packages are removed 24 hours after their `created`
time. The registry keeps its most recent 200 task records. **Artifacts are in `/tmp` and
they are not yours to keep** — if a child produced something worth having, copy it out. The
directory going away after a day is the same promise `/tmp` always made, made explicitly.

Worktrees follow a separate, fail-safe policy: an empty clean checkout whose `HEAD` remains on its
task branch is removed with that empty branch when the child tab closes; after 24 hours a clean
checkout with commits is removed while its branch is retained indefinitely; any dirty checkout,
moved `HEAD`, missing branch, or unreadable git fact is kept. Removal always uses
`git worktree remove` followed by `git worktree prune`, never filesystem deletion. The directory
shape is also scanned for records evicted by the 200-row cap: the repository comes from the linked
checkout's git metadata and the branch base from the oldest reflog entry, then the same disposal
rules are applied. If either fact is unreadable, the checkout is kept and audited as `unreadable`;
`remove_failed` is reserved for a removal that git actually rejected. **The `/tmp` 24-hour promise
does not extend to branches**: Clawdline never automatically deletes the only copy of committed work.

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

A root session does not talk to this API by hand. [`skills/clawdline/`](../skills/clawdline/) in
this repository is a Claude Code skill — copy it to `~/.claude/skills/clawdline/` and it covers
every project on that machine and no other — and it is what turns *"get codex to draw this"* into a
dispatch:

```bash
mkdir -p ~/.claude/skills/clawdline
cp skills/clawdline/SKILL.md ~/.claude/skills/clawdline/SKILL.md      # or SKILL.zh-TW.md
```

There is an English `SKILL.md` and a Traditional Chinese `SKILL.zh-TW.md` of the same text. Install
one of them, not both — they declare the same `name:`. Seven sections:

1. **Find the door.** `remote_port` out of `config.json` (7717 if absent),
   `~/.config/clawdline/orchestrator-token` for the token. Either one missing is a full stop with an
   explanation, not a workaround.
2. **Draw the graph, then decide the work.** The house rules first — `dispatch-policy.md`, which
   outranks every default below it. Then the whole graph before any of it is sent: what each leaf
   produces, who joins those answers together, what the top hands back. Then per node, which
   assistant (the user's word if they gave one, otherwise Codex for generating and running things
   and Claude for reading and judging them) and which model. Breadth before depth: two children
   splitting a job beat one child that will hand half of it on. Instructions have to stand on
   their own — the child cannot see the conversation they came from, so *"do what we discussed"*
   is an empty file with extra steps — and the graph goes in `plan`, the same text in every task
   of the batch.
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
7. **Hand a whole line of work on when dispatch is the wrong motion.** The same skill writes the
   continuation package and calls `POST /v1/orchestrator/handoffs`; the receiver is a new root,
   not a child. The complete package and receiving contract are in
   [`docs/handoff.md`](handoff.md).

**Drawing is a real branch inside step 2.** Codex has an image model built in — `image_gen`, on by
default, no API key, billed to the account the child is already signed in as — so *"draw this"*
comes back as a PNG rather than a hand-written SVG whenever what was asked for is an illustration.
It cannot be told where to save: the file lands under `~/.codex/generated_images/<session>/`, so the
briefing has to tell the child to copy it into the task's `artifacts/` afterwards, or the task
finishes with a picture nobody can reach. Vector is still the right ask for diagrams, icons, and
anything that has to stay editable.

The one rule stated before any of them: **a child dispatches only if its briefing said it could,
and what it opens opens nothing.** `CHILD.md` is where a child reads that, and it carries the same
dispatch steps in miniature — spelled out rather than pointed at the skill, because half of these
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

`isolation: "worktree"` is a workspace-shape contract — a clean independent Git checkout whose
delivery is a branch — not a demand that a hosted broker invoke one particular command. A hosted
implementation may use a clone, container, or snapshot. `task.json` says what is requested; the
record's local `worktree.path` says what this broker actually did, just as `dir` and terminal ids do.

Nothing here depends on that happening. It is written down so that the local version does not
accidentally make it impossible.

---

Every route in full, with the shapes and a `curl` for each, is
[`docs/api.md`](api.md#post-v1orchestratortasks). What the app is doing when it watches a terminal,
and why it can tell idle from waiting, is [`docs/interface.md`](interface.md).
