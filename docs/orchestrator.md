# Handing work to another session

For task templates that dispatch on a clock, including catch-up and tab-close policy, see
[`schedules.md`](schedules.md). Scheduled work enters the ordinary lifecycle described here.

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
| **Orchestrator token** | `~/.config/clawdline/orchestrator-token`, mode `0600`, minted when the server starts | dispatch, cancel, read every task, send a root content notification | it is never served over HTTP, never written under `/tmp`, cannot recover a task secret, and is never accepted from a device that merely holds a device token |
| **Task secret** | 64 hex characters, made by the root, handed to the child inside the injected first message | say "this one task is finished"; send up to five content notifications while live or within 60 seconds afterwards | nothing for another task or after its short notification grace. Its durable identity is a SHA-256; only a serialized waiter has a temporary encrypted copy for restart recovery |
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

### An assistant with no quota left

Capacity and depth say whether *this Mac* has room. This gate answers a different question: does
the *assistant account* the task named have anything left to spend at all. It sits right after the
machine-wide capacity ceiling and before the first git subprocess, for the same reason capacity is
checked before git is: it is a fact the broker already knows, cheaper than any subprocess, so it
answers before asking a repository anything.

The read is [`GET /v1/orchestrator/assistants`](api.md#get-v1orchestratorassistants)'s own
`AssistantQuota.current(for:)` — one 5-second-cached pass over at most a handful of small local
files, no network, no `codex`/`claude` subprocess. **Codex's `app-server` exposes
`account_processor/rate_limit_resets`, which could answer a quota probe directly — v1 deliberately
does not use it.** It needs a running `app-server` daemon, an authenticated request to
`chatgpt.com/backend-api`, and has no published contract; reading the rollout it already writes is
free and needs none of that. It remains the one real upgrade path if a future version needs an
answer this reading cannot give. An `exhausted` reading is `409
assistant_exhausted`, carrying `alternatives`: every other assistant's own `availability` and
`detail`, so the refusal itself says who to send the work to instead. A `low` reading dispatches
and adds an `assistant_low` warning to the same `warnings` array `workspace_overlap` and
`claims_overlap` already use. `unknown` — no signal yet, or none of this Mac's files say so —
dispatches quietly: **absence of a reading is not bad news**, and warning about it would train
whoever reads these responses to ignore the warnings that matter.

**Why a refusal here and a warning everywhere else.** Every other warning in this protocol
describes something that *might* go wrong — a shared directory, a shared claim. `exhausted`
describes a tab that is *certain* to die: the night this was built, dispatching into one cost a
cold start, a `spawn_failed`/timeout wait, and thirteen files frozen on a shared index waiting for
somebody else to commit them, because the assistant inside that tab never had anything to answer
with. A caller that already knows better sets `"ignore_quota": true` in `task.json`; the 409's own
`message` names that field, so a stuck root does not have to already know it exists to get past it.

This reading is a fact about the **account**, not about the task or this Mac's tree — it fires the
same way whether the exhausted session belongs to this Mac's own dispatch graph or not, because the
quota it describes is the provider's, shared by everything running under that login.

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
  "reasoning_effort": "high",
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
| `reasoning_effort` | optional and Codex-only: exactly `high` or `xhigh`. Absent adds no CLI override, preserving Codex's model default and the user's configuration. Empty, non-string, any other value, and use with Claude are refused |
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
| `root.session_id` | the dispatcher's assistant session id: Claude's transcript UUID or Codex's rollout `session_meta.session_id`; `null` when unavailable |
| `root.assistant` | optional `claude` or `codex`. New dispatchers send it; absence or explicit `null` is read as missing, and missing is resolved as `claude` for registries and task writers from before this field existed. Other values, including an empty string, are refused |
| `root.parent_task` | the dispatcher's **own** task id, when the dispatcher is a child. `null` from a root. A value that is not a task id is read as `null` |

`root.session_id` being nullable is deliberate. A root that cannot work out its own id still gets
to dispatch. **A best-effort field must not be a required one**, or the honest answer "I don't
know" becomes a reason to invent something. Codex normally exports `CODEX_THREAD_ID` (with
`CODEX_SESSION_ID` as the compatible spelling) and that value is the rollout session id. Claude
has no direct self-query and uses the transcript nonce procedure in the skill.

Two things follow from leaving it out, and the second one surprises people. The task is not told
when it finishes, so the dispatcher has to poll. And **its row has nothing to sit under**: the
list indents a child beneath the session that asked for it, that session is found through this
id, and a task that named nobody is filed under nobody. The row still says `Child`, because being
somebody's child is a fact about that session whether or not the parent is on screen — but it
floats at whatever position the sort gave it, which reads at a glance like a bug in the grouping
rather than a task that declined to say who asked. If a row belonging under yours matters, send
the id and `root.assistant` together.

The broker does not trust either string on its own. For Claude it resolves the exact current
process's transcript (using the validated process registry when available, otherwise a hook id
whose named transcript postdates that process). With neither source naming a transcript, title
and time may still help the transcript pane choose what to display but may not establish root
identity. For Codex it asks the current pid which user
rollout it holds open and reads that rollout's `session_meta.session_id`. The terminal must also
be running the declared assistant. A stale rollout, a leftover hook on a reused tty, or a tab now
occupied by the other assistant therefore produces no `root.terminalId`; it never mounts a child
under the wrong row.

The same strict conversation resolver is used for list mounting, completion lines, workspace
overlap lines, root-close cancellation and batch-notification deep links. Handoff receipts are the
one named compatibility exception: their free-form `from_session` may be either a conversation id
or a watched terminal id, as the handoff API has always promised.

`model` is the only free-form string a dispatch puts on a command line, and it is shaped so that
saying so is not alarming: not a fragment of a command but a name out of a closed alphabet. No
character it admits is one a shell reads — no space, no quote, no `$`, no `;` — so
`claude --model <name>` stays one command with one argument whatever arrives. It is checked in
two places on purpose: here, where a typo can be answered with `bad_task` while somebody is still
holding the request, and again in `StartPoints.modelName` on the way to the tab, where a name
that fails becomes *no flag* rather than no session. The route a phone can reach passes nothing.

`reasoning_effort` is different: it is a closed Codex-only enum, not a command fragment. `high`
is the recommended coding setting and `xhigh` the recommended planning setting. The command
converges in one place as `codex [--model <name>] --config model_reasoning_effort=<value>
[--add-dir …] [permission flags]`. When the field is absent, the complete `--config` pair is
absent too; Clawdline does not replace Codex's model or user default. `max` and `ultra` are not
protocol values and are refused rather than passed through. The selected value is persisted,
shown in the public task record, and written into `orchestrator.dispatch` audit metadata.

A hand-written schedule task template may carry the same field. Schedule parsing applies the
ordinary Codex-only validation, and an edit carries it forward because the schedule UI has no
reasoning control. If the edit explicitly switches the assistant to Claude, the Mac removes that
now-incompatible hidden override; putting it directly on a Claude template remains invalid.

`plan` is the graph, not this task's job — the same text in every task of one dispatch. It goes
near the top of `CHILD.md`, above even the language rule, because it is the context every other
line is read in: a child that knows its answer is one of four being joined together writes
something joinable, and one that does not writes a report. Leaves get it too, and that is the
half that matters — a leaf that knows what its output feeds is the difference between an answer
and an essay.

`root.parent_task` is the same field one level down, and it exists because a child knows its own
task id from the first line it was ever sent, long before this app has worked out what the session
in that tab calls itself. It is the strongest answer for every child because the broker already
owns that task-to-terminal link; it also works before either assistant's transcript identity is
observable. Naming it is what gets a task filed under its actual parent on the first try instead
of being counted as a root's. Getting it wrong costs capacity and never buys any — [the two names
are combined by taking the deeper answer](#depth-stops-at-two-and-the-floor-is-what-has-teeth).

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

**There are two ways to get `claims` wrong, and only one of them is loud.** Omitting it is the
quiet one, and it reads as caution while behaving as noise: with no declaration the broker cannot
prove two tasks are disjoint, so it falls back to L1's directory-level warning about every pair
sharing a `project_dir`. On 2026-08-26 that produced a dozen notices in one evening, none of which
described a real conflict, while the single genuine collision that night was refused at dispatch
with `409 workspace_busy` in the same breath — the mechanism that actually catches collisions is
the one that needs the field. Claiming too widely is the other direction of the same error, and it
does announce itself: an over-wide claim blocks other trees whether or not the task touches it, and
[the terminal audit](#the-terminal-claims-audit) names every claimed path the task never touched.
One failure mode is reported after the fact and the other is not reported at all, which is why the
absent field is the more expensive of the two to leave alone.

The check and registration happen atomically as soon as the dispatch has validated. A serialized
task reserves its claims for its entire time in `queued`; promotion is not a second gap where
another root can enter. A live claim from a different root refuses the new dispatch immediately
with `409 workspace_busy`, before serialization and before L1 warnings or a terminal spawn. The
error names the blocking task, its title and root label, when it was created, every conflicting
absolute path, and advisory `retry_after: 60`. The rejected task is not registered, and the audit
log records `orchestrator.claims.blocked`. It also does not consume an entry in the ten-minute
dispatch rate limiter, so following the retry advice cannot turn repeated `workspace_busy`
answers into `rate_limited` by itself.

The error, and every `claims_overlap`/`claims_overlap_unknown_root` warning below, is also
context-sufficient without a follow-up GET: both carry `age_seconds` (`now` minus the blocking
task's `created`, an integer, computed against the answering request's own clock) and `root_key`.
**`root_label` is self-reported prose and can be stale, or shared by two unrelated roots — two
different dispatch trees both calling themselves "clawdline schedules" is a real case this app has
hit — while `root_key` is the same tree's stable identity every time.** It is
`Orchestrator.rootKeyDigest` of the canonical root key already used for identity below (a live
root's session id, or `task:<id>` for a task resolved back to itself): SHA-256, truncated to its
first 8 hex characters. `root_key` is present whenever the blocking task's own root resolves —
including inside an otherwise-`_unknown_root` pair, where only the *new* task's side failed to
resolve — and absent only when that specific task's own root could not be determined.

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
remains on its branch until the root explicitly integrates it. That safety property also means a
terminal child state cannot be the completion state of a code-producing root graph: the delivery is
durable, but still pending.

A serialized task holds claims from dispatch throughout its entire `queued` wait. That wait has no
independent timeout: it is bounded by its serialize blockers finishing, timing out, or being
cancelled, and by cancellation of the queued task itself. `timeout_minutes` still starts only at
`briefedAt`.

### Releasing claims early

Terminal state is not the only way a lease ends. A task that has finished editing some (or all) of
what it declared can hand those paths back through
[`POST .../claims/release`](api.md#post-v1orchestratortasksidclaimsrelease) while it is still
`briefed` or `spawning`, and a `409 workspace_busy` blocked on exactly those paths can retry
immediately rather than waiting for the whole task to end. This is the only way to break a
circular wait where two roots each hold a path the other one needs — the retry advice on `409`
says "wait", but nothing releases on its own until a side chooses to.

`paths` in the request body names the same relative declarations `claims` used, and may not
contain a `..` component; empty or omitted releases everything the task still holds. Release is
compared exactly the way arbitration compares claims (`sharedClaimPath`) — ancestor and
descendant, not exact string equality: `Sources` released frees every declared claim key under it,
and naming a path *inside* a directory-shaped claim frees that whole claim key, because a
directory claim is one atomic reservation rather than a set of the files under it. Release is
idempotent: a path already released, or one this task never declared, is silently a no-op, so a
retried release call can never fail on its own earlier success. It is refused for a task that
cannot be found (`404 not_found`), one already in a terminal state (`409 already_done`, since a
terminal task's claims are already released in full), one still `queued` (`409 not_started` — it
has not started writing yet, so giving up its lease would leave `instructions` free to write those
paths with no reservation behind them once it is promoted; cancel it instead), or a malformed
`paths` entry (`400 bad_request`).

Release is a machine-level permission, like cancel: the request carries only the orchestrator
token, and that token does not identify which root is calling. Any root holding it may release any
task's claims, including one that is currently blocking it — there is no ownership check, and the
audit log's `orchestrator.claims.released` entry names the task and the paths freed, not a caller,
because there is no caller identity to record.

Internally, a task's frozen `claimKeys` — the historical declaration — never change from release;
what changes is `releasedClaims`, a list of freed keys with when each was freed. Every place that
decides whether a claim is *live* — dispatch-time arbitration and L1's disjoint-claims silence
rule — compares `activeClaimKeys` (`claimKeys` minus `releasedClaims`) rather than `claimKeys`
itself, so a released path stops blocking or warning against every other task immediately, under
the same lock dispatch uses. `claims` and the GET record's frozen reservation are otherwise
unchanged; `released_claims` is a separate, additive record of what was given back and when. A
task that has released everything it declared has an empty `activeClaimKeys`, which reads to
`declaredClaimsAreDisjoint` exactly like a positive `claims: []` declaration — so once every claim
is given back, that task's directory-overlap warnings against other tasks fall silent too, the
same silence an explicitly read-only task gets.

### The terminal claims audit

Purely observational, and never a gate: when a task reaches a terminal state, the broker checks
each of its declared claims against the filesystem — mtime of the path at `project_dir` plus that
relative claim, compared against `spawnedAt`. A path whose mtime is at or after `spawnedAt` was
touched; one whose mtime is earlier, or that does not exist at all, was not. A directory-shaped
claim (the common spelling: `Sources`, `docs`) is judged recursively rather than by its own mtime
alone: a directory's mtime only moves when an entry is added, removed, or renamed directly inside
it, so a file edited in place further down would otherwise read as untouched. The walk skips
`.git` and `node_modules` and stops after 2,000 scanned entries; a claim whose subtree is larger
than that is left out of the audit entirely — not reported as untouched, and no advice given for
it — rather than judged from a partial walk.

Untouched paths are written into the task record's `untouched_claims`, and one typed line is
appended to the same completion notification a root already gets, naming the count and up to three
of the paths (`…, and N more` past that — the complete list is always in `untouched_claims`), with
a reminder to claim narrower next time. Only `success` and `failure` are judged: a task that never
spawned has no baseline to judge against, and `timeout`, `spawn_failed`, and `cancelled` leave the
child's actual ending ambiguous — a `timeout`'s tab may still be writing, a `spawn_failed` or
`cancelled` task may never have opened at all — so none of the three produce `untouched_claims` or
the "claim narrower" advice; saying both "it may still be writing" and "it never touched this,
claim narrower" about the same task on the same line was the bug this excludes.

This exists because an over-wide declaration costs exactly as much as a real conflict: it still
returns `409 workspace_busy` to somebody else, or still earns a `claims_overlap` warning, whether
or not the task ever touches most of what it claimed. The audit does not narrow anything and does
not retroactively unblock a task that already hit `409` — it only shows up after the fact, so the
next dispatch in that line of work can declare what it actually needed.

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
the same best-effort version-1 `workspace_overlap` notice at promotion rather than retroactively
added to that response.

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

The new task's root also gets one aggregate `workspace_overlap` notice for all overlaps, while
every other root that can be found gets the notice concerning its task. The fallback `body` keeps
the former concise `[clawdline] workspace overlap: …` sentence. A root with a null session id
cannot be found and is quietly skipped. Delivery runs outside the request queue; as with
completion notification, a root showing a menu or a failed terminal send does not affect dispatch.
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

### Agent notifications — content, not state

Clawdline's ordinary push notifications describe **state**: a session needs an answer, a schedule
failed, a batch ended. An agent notification describes **content**: today's forecast, the URL the
user was waiting for, the one result whose value is arriving on time rather than merely saying
that work stopped. The second kind is opt-in and scarce. Routine work still reports through
`result.json`; a dispatcher asks for notification-shaped output explicitly, and every generated
`CHILD.md` teaches the child the task-secret route without requiring a skill to be installed.

A live child, or one that finished no more than 60 seconds ago, may call
[`POST /v1/orchestrator/tasks/:id/notify`](api.md#post-v1orchestratortasksidnotify-post-v1orchestratornotify).
A local root or script may call `POST /v1/orchestrator/notify` with the orchestrator token. The
first prefixes the visible title with the task title (a scheduled task therefore uses the schedule
name); the second prefixes it with `Clawdline`. Both enter the existing RFC 8291 fan-out. The opt-in
has hard bounds — 80 title characters, 500 body characters, five messages per task and 30 per Mac
in a sliding hour — and every attempt leaves an `orchestrator.notify` audit row. The per-task count
survives restarts; the process-memory hourly window restarts empty. With no subscription the route
returns `409 not_subscribed` without consuming either allowance. Push-service failures return
`502 push_failed` with sent/failed subscription counts instead of reporting a silent success.
Task and root content use stable WebPush topics. Settings → Remote has a separate agent-notification
preference, on by default and stored as `orchestrator_agent_notify`; turning it off leaves finish,
deploy and other notifications alone. While it is off both content routes return
`409 agent_notify_disabled` without attempting delivery or spending an allowance. An agent that
receives that refusal does not retry: it leaves the content in `result.json`, reports `failure`
honestly, and explains that the user disabled agent notifications.

### `result.json` — written by the child, and it *is* the signal

```json
{
  "clawdline_protocol": 1,
  "task_id": "3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234",
  "task_secret": "…the value from the first message…",
  "status": "success",
  "summary": "Wrote a 1024×1024 SVG portrait; border and lettering hand-pathed, no raster.",
  "symbols": [],
  "artifacts": ["artifacts/project-portrait.svg"],
  "finished_at": "2026-08-24T10:41:55Z"
}
```

Written last, and written atomically — to `result.json.tmp` and then `mv`, so the watcher never
sees half of it. The app checks it once a beat for every briefed task, hashes `task_secret`, and
compares against what it stored at dispatch in constant time. A file whose secret does not match is
**ignored** and logged once: a wrong secret in a task directory is either a bug or somebody
poking, and neither is a reason to finalize somebody's task.

`symbols` names every identifier the child's change introduced: new functions and types, new
fields, new string keys, the names of test groups it added. Names, not descriptions — the portrait
above introduced none, and `[]` says that positively where an absent field only says the child did
not answer. For a code task the list reads like
`["Orchestrator.Landing", "updateLanding(taskID:secret:raw:now:)", "orchestrator.landing"]`. It exists
because a shared working tree makes authorship unreadable from the diff alone — by the time root
comes to commit, the files a child edited may hold two or three sessions' unfinished work, and
vocabulary is the only reliable way to tell one session's hunks from another's. Guessing it has
produced staged trees that would not compile, which is the failure the field is there to prevent.
The child's briefing asks for it; **the broker does not**. `readResult` takes `status`, `summary`
and `artifacts` and ignores every other key, so `symbols` never appears on a task record, in a
notification, or in any API answer — it is written for whichever root opens the file, and root has
to open the file to get it.

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

### Landing is a root obligation, not a child state

The lifecycle above answers whether a child ran and reported. It deliberately does not claim that
a code delivery reached `main` (or any other target ref). For a code-producing graph, root carries
one additional protocol obligation:

```
delivered -> reviewed -> pending landing -> landed
```

`success` supplies **delivered**. An independent `SAFE TO LAND` verdict supplies **reviewed** and
opens **pending landing**. Neither is `landed`. The root that dispatched the graph owns the pending
landing unless a named root accepts it through a Clawdline handoff; an unspecified future session
is not an owner.

The graph's `plan` must therefore name the delivery branch, target branch, landing owner, review
node and root-owned landing closure before the first child is dispatched. Follow-up implementation
rounds use another isolated worktree based on the previous delivery branch or commit. Turning a
code branch into an artifact bundle for later editing drops the broker's base/head/dirty receipt and
is not a substitute for worktree delivery.

That obligation is now part of the task registry rather than prose alone. After claimed work comes
back, root uses the task secret with `POST /v1/orchestrator/tasks/:id/landing` to record
`{"state":"pending","target":"main"}` (plus an optional delivery, review conclusion, or note).
A named root that later accepts a handoff can use the machine-level orchestrator token instead;
this is the same credential family as cancel and claims release. A task secret can maintain
`pending`, but **cannot write `landed`**: each child holds its own secret, so that would make target
landing self-reported. Only the machine/root orchestrator credential reaches the landed transition.
The broker derives `owner_root_key` from its existing root identity,
preserves the original `since`, and exposes the object on the task record. Repeating `pending` may
fill in `target`, `delivery`, or `note` without restarting `since`. After the target contains the
verified delivery, root posts `{"state":"landed","target":"main","commit":"<sha>"}` with the
machine credential. The broker resolves both commit and `refs/heads/main` inside the task's project
repository, proves the former is an ancestor of or equal to the latter, and persists canonical
`verified_commit`, `verified_target_commit`, and `verification_origin = local_target_branch`.
Arbitrary text, a remote-only ref, or an unrelated commit is refused; a pending edit racing the git
checks is rejected by CAS. `landed_at` records when that proof was captured. Both `landed` and
`abandoned` require a terminal task and are final: neither
can return to another state, and genuinely reopened work gets a new task. `abandoned` is an
explicit decision not to land the delivery, not a temporary pause.

`GET /v1/orchestrator/landings` lists every current pending obligation with its title, owner root,
original claims, target, note, and non-negative age. Pending obligations are exempt from the
registry's ordinary newest-200 cleanup cap, so age alone cannot erase an unresolved row. When a
claimed task reaches terminal state without any landing record, its one completion line reminds
root to record one; the reminder is suppressed when every claim was judged untouched.

**This is a signpost, not a gate.** Landing state does not retain claims, extend their lifetime,
refuse a dispatch, freeze a file, or stop a commit. It makes the unfinished obligation queryable so
another root can decide whether to wait; claims arbitration and pre-commit discipline remain
separate mechanisms for actually preventing the other two classes of collision.

After review, root closes the obligation by checking that receipt and verdict, reading the target
repository's current head and status, integrating without absorbing another session's uncommitted
files, testing the exact integrated tree under the repository's rules, and recording the target
commit that contains the delivery. Only then may it report the user's code change complete.

**“Testing the exact integrated tree” is a different act for root than for a child, because the two
are asking different questions.** A child asks whether what it wrote works; the working tree is the
right subject, other sessions' half-finished edits included, because a child does not commit and
their mess cannot reach HEAD through it. It snapshots that tree with `git archive "$(git stash
create)"`, which writes a commit object for the working tree and leaves the shared index exactly as
it found it. A child must not use `git write-tree` for this: that reads the *index*, so it has to
stage first, and one shared index means a child staging its own files sweeps up whatever another
session left in there — after which a root commits it. Root's question is the opposite one, *will
HEAD still build after this commit?*, which the working tree cannot answer at all. Root is staging
anyway, so the index is exactly the right subject and `git archive "$(git write-tree)"` is the
snapshot to test.

That question has a corollary the protocol now states outright: **HEAD must compile standing alone,
and committing is the only act that can break it.** Twice on 2026-08-26, from two different
sessions, a partial commit took a slice that did not stand up — three lines whose type stayed
uncommitted in another file, and a protocol requirement whose fourteen values stayed in the
worktree. Both trees were green when those commits were made, because a green tree is the union of
everybody's work while HEAD is only the committing session's slice. So a suite passing in the tree
is not evidence about HEAD while anything is uncommitted, and before taking a partial commit root
asks what else defines what it is taking: a declaration without its values, a call without its
function, a case without its enum. Recovering another session's half-landed commit is legitimate
root work — restore the missing half, or lift the orphaned lines back into the worktree where their
owner can still see them, and say in the message that it is not your line's work and why HEAD could
not wait.

When integration used hunk staging, the commit must be created from that reviewed index with plain
`git commit -m <message>` and no pathspec. `git commit -- <path>...` does not mean “commit the staged
hunks of these paths”: it takes those named files from the worktree and can absorb foreign unstaged
hunks that the staged-tree review deliberately excluded. A pathspec commit is safe only when every
worktree hunk in every named path belongs to the same delivery. After committing, compare the commit
tree to the tested `git write-tree`; a matching file list is not enough.

An overlapping dirty shared tree is a landing delay, not a completed outcome. Root leaves the
obligation pending, identifies and coordinates with the owning session through Clawdline's session
and task views, and retries when integration is safe. If root must stop first, its handoff package
names the delivery branch/base/head, target branch/current head, verdict and test receipts,
overlapping paths and known owner, and exactly one next landing action. The original root remains
responsible until Clawdline's handoff receipt confirms that the first line reached the named
receiving root. For this explicitly assigned obligation, that receipt transfers ownership; it does
not say that the delivery landed.

### Owned storage is an allowlist, not a scratch-directory sweep

Clawdline records Claude child scratchpads in
`~/.config/clawdline/owned-storage.jsonl` only after the transcript's first user turn proves the
exact `Clawdline CHILD agent for task <uuid>` marker. Each append-only `own` row carries the task,
assistant, session, reconstructed canonical path, proof method, project directory, and timestamp.
The ledger is independent of `orchestrator.json`: the ordinary task registry keeps only its newest
200 settled rows, while the ownership fact must survive that turnover. Old rows whose paths have
been absent for more than 30 days may be compacted by atomic replacement; an unreadable ledger or
malformed line is never compacted away.

The safety boundary is what the ledger can name. Clawdline does not enumerate `/tmp/claude-*` and
then apply gates to what it finds. Interactive Claude or Codex sessions, directories with no
ledger receipt, and other agents' files are outside the candidate set altogether. In particular,
the approximately 0.95 GB of historical child scratchpads whose task rows were evicted before this
ledger existed are intentionally left unowned and unreclaimed. Recovering them would require the
namespace-wide transcript inference that this design rejects; preserving data is the chosen side
of that ambiguity.

`GET /v1/orchestrator/storage` is the read-only inventory and dry run. It lists each ledger-owned
path, byte size, task, decision (`held`, `releasable`, or `unknown`), reason, age and eligibility
time, plus totals. Releasable requires every fact: terminal task, no pending landing, the applicable
floor (one hour after `landed`/`abandoned`, twelve hours without a landing, or twenty-four hours
when process-start identity is missing), no matching live child session, a dead or demonstrably
reused child pid, and an exact canonical nonsymlink path. `pending` has no timeout. Any unreadable
or malformed source produces `unknown`, and **unknown is held**; an empty live-session set and an
unreadable registry are different values in the type system.

This phase does not quarantine, purge, delete, or expose a mutating storage route. The collector is
a separate, default-off phase. Children are also told to put repo copies, build output, mutation
worktrees and compiler indexes in their own `/tmp/.clawdline/<task-id>/work/` directory so those
large temporary files live inside storage Clawdline already owns. The existing 24-hour task-root
cleanup now exempts `landing.state == pending`, because a root that has not landed may still need
the child's receipts.

### File release waits belong to Clawdline

A root waiting for paths in a shared tree must not create a relationship that exists only inside
Claude Code or Codex. It resolves both parties through Clawdline's session and Git readings, then
registers the relationship with `POST /v1/orchestrator/waits`. The request names repository,
exact paths, owner and waiter terminal-neutral Clawdline session ids, reason and release condition.
Clawdline canonicalizes the paths, deduplicates the same waiter, persists the group in its
orchestrator store, and delivers the file-wait message to the owner through Clawdline's terminal
transport as a version-2 `file_wait_request` notice. Its typed payload carries the wait id,
repository, canonical paths, waiter session id, reason and release condition; its `body` keeps the
complete operational sentence the owner acts on.

**Where a root reads those two ids.**
[`GET /v1/orchestrator/sessions`](api.md#get-v1orchestratorsessions) is the one session listing the
orchestrator credential can open: `GET /v1/sessions` lists the
same ids and is the paired-device route, so it answers that credential with `401 unauthorized`. The
index gives an `id`, `assistant`, `cwd`, `label`, `state`, and a `taskId` for tabs this app opened —
enough to pick an owner, and nothing off the session's screen. Two older ways still work and neither
is complete: a session's *own* id is the UUID after the colon in `$ITERM_SESSION_ID`, and
`GET /v1/orchestrator/waits` names the ids already inside a wait, which is no help to the first
session that needs to wait on somebody.

**Two different namespaces, and only one of them is what these routes list.** A root
identifies itself in `task.json` with whatever id its own assistant gave it — Claude Code's own
`sessionId`, or a Codex thread id — and that is *not* the terminal-neutral session `id` this app
assigns and `GET /v1/sessions` lists as `id`. Comparing a root's self-reported id against that `id`
column is comparing two different namespaces; a miss there proves nothing about whether the other
session is still alive, only that the two ids do not name the same thing.

**`sessions[].sessionId` is verified but optional.** It is emitted only when the current process
can be bound to its exact Claude transcript or Codex user rollout. That makes a value which is
present useful identity evidence, but absence still proves nothing: a brand-new rollout may not
exist yet, a process reading may fail, or a Claude session may have neither a registry entry nor a
hook naming its transcript. A stale hook, stale rollout or reused tty is omitted rather than used.
The broker performs this same process-bound resolution when it writes `tasks[].root.terminalId`;
that field, not a client-side comparison against `root.sessionId`, is the grouping contract.
**Querying for a session by an id and getting nothing back is never proof that session is gone** —
it may only mean this app could not prove the id at that moment. To judge whether a particular root
is still around, use what a session listing can always promise instead: the `cwd`, `label` and
`state` of a session sitting where the root said it would be — from
`GET /v1/orchestrator/sessions` with the orchestrator credential, or `GET /v1/sessions` with a
device token — or sending the root a message and reading whether anything answers.

The owner uses `POST /v1/orchestrator/waits/:id/release` only after committing or explicitly
releasing the paths, naming its own Clawdline session id and the commit when one exists. Clawdline
fans a version-2 `file_wait_release` notice out to every waiter. Its typed payload carries the wait
id, repository, canonical paths and optional commit/note, while its `body` still ends with the
safety instruction to re-check HEAD, status and diff. Each successful delivery is receipted before
the route answers; if another waiter cannot be reached, the group stays registered and a retry
addresses only the recipients still pending. A waiter abandoning the work may remove only itself
with `POST /v1/orchestrator/waits/:id/cancel`. Release is always explicit: the broker never guesses
from one transient clean `git status`.

This composes the assistant-neutral session surface: a Claude root may wait on a Codex owner or the
other way round. The unresolved relationship survives an app restart and remains visible if the
owner disappears. A release notice only wakes a waiter; the waiter still reads HEAD, status and diff
before acting. Provider-native messages are never a fallback because they would divide one safety
rule into incompatible Claude and Codex halves.

`GET /v1/sessions` exposes the relationship as a `coordination` overlay. A blocked session carries
`coordination.state = waiting_on_session` and `waitingOn`; an owner carries `waitedOnBy`. This does
not change the session's terminal `state`: `waiting` still means a person must answer and alone
drives the loud row and push notification. Native and web rows draw peer waits quietly as
`⏳ owner · release condition`, making an idle-looking but parked session safe for a person to leave
open. A final-line `[Clawdline waiting]` sentence is only a fallback when that UI is unavailable.

### Clawdfather Phase A1: durable identity and read-only Bearings

Clawdfather is now a broker-authenticated role, not presentation fiction. Construction is explicit:
a local caller reads a terminal-neutral assistant id from `GET /v1/orchestrator/sessions` and sends
that exact id to `POST /v1/orchestrator/coordinator/register` with the orchestrator token. The
broker accepts only a live Claude or Codex row whose complete process-bound identity is proved by
the same seam used for task mounting and Session completion receipts: terminal id, assistant, tty,
pid/start and current transcript/rollout conversation id. Label, cwd, title, task ancestry, root
depth, words such as “father”, and recency are not evidence.

The durable version-1 record lives at `~/.config/clawdline/coordinator.json`. It contains a stable
opaque coordinator UUID, fixed `scope: machine` and `label: Clawdfather`, plus the private binding
tuple and last safe session label/cwd. The private parent is repaired toward `0700`; the regular,
non-symlink lock and store files are `0600`. Registration holds a machine-local exclusive `flock`,
force-reloads after it acquires the lock, keeps it through atomic creation, and verifies the bytes
it wrote before succeeding. Cache fingerprints notice another process's atomic create/replacement.
A first registration creates the UUID; the same tuple is idempotent. Process start uses the shared
`SessionRegistry.startTolerance` to absorb subsecond `Date() - etime` reconstruction drift while
terminal, assistant, tty, pid and process-proved conversation remain exact. Any other tuple gets
`409 coordinator_exists` and safe metadata about the existing identity. There is deliberately no
takeover through registration. A2 adds the separately guarded offline reconnect below; there is
still no unconditional replacement, deletion or stop path. Corrupt and unknown-version records
fail closed and are preserved for diagnosis rather than treated as absence.

Restart continuity is identity continuity, not conversation resurrection. After the app reloads,
the durable record projects `session.coordinator` only when all private binding facts still match
the one current process. That exact row receives the web renderer's closed record—`label`, online
`status`, and `commands`—on both Session JSON surfaces. Every ordinary row is unchanged. A missing
or reused process leaves the durable coordinator `offline` and decorates no row; the role never
migrates to a matching label or the most recently active assistant.

`GET /v1/orchestrator/coordinator`, also orchestrator-token-only, returns safe durable presence and
**Bearings**: one deterministic snapshot over existing Session metadata, active task records,
pending landing records and open coordination-wait groups. It always counts the seven closed
`work_state` values and names safe metadata for `needs_triage`, human/peer `waiting`, and owners
`blocking` peers. Named lists are independent filters and may honestly overlap when a session is
both owner and waiter. RemoteServer takes one SessionWatch observation and then one Orchestrator
snapshot; every Orchestrator-derived row flag plus task, landing and open-wait total is computed in
that single registry lock window. SessionWatch and the registry are not cross-source atomic. Each
source therefore carries its own observation time, provenance and freshness; an incomplete Session
inventory is `stale`, never silently complete. The route returns only the
opaque coordinator UUID and terminal-neutral session id, assistant, cwd/label/work-state. It never
returns transcript text/path, assistant conversation id, tty, pid or process start.

All commands are disabled in A1. The four vocabulary entries for status report,
duplicate/conflict/ownership inspection, landing closure advice and scope/permissions explain that
Bearings exists at authenticated `GET /v1/orchestrator/coordinator`, but their web actions are
preview-only and not connected. The renderer must say Disabled/Preview, never Available.
Since-away, cross-session coordination judgement, ask/quiet-watch, dispatch, stop and reconnect are
also explicitly disabled with a specific Phase A1 reason. There is no command execution route,
no transcript grant/read, no typing into sessions, no dispatch or task/landing/wait mutation, no
Build, no model wake and no inference.

Registering this optional role does not enter the task registry. It cannot change parent links,
depth or caps, root keys, claim ownership, wait/landing writers, terminal `state`, `work_state`, or
the evidence that writes `milestone_complete`/`work_complete`. Bearings explains those receipts;
it never authors them. The exact request, response and error schemas are in
[`docs/api.md`](api.md#machine-coordinator-identity-and-bearings).

**Proposed restart boundary, not a current power.** A sandbox-loopback refusal is only
`observer_unreachable`, never `app_down`. Before any future restart, Clawdfather must corroborate it
with host listener/process proof and a host-network health probe, and record observer domain and
provenance such as `sandbox_loopback`, `host_listener`, and `host_health`. One failed observer cannot authorize restart. Phase A1 implements no restart or other health-driven mutation capability.

### Clawdfather Phase A2: guarded offline reconnect

Phase A2 preserves the A1 coordinator UUID across a dead assistant process without weakening the
process-bound singleton. A machine-token caller first reads Bearings, then sends that observed
opaque `expected_coordinator_id` and one terminal-neutral live `session_id` to
`POST /v1/orchestrator/coordinator/rebind`, together with the observed positive
`expected_generation`. The three-field request is closed. It is not accepted from a paired device
or task secret, and registration still never implies takeover.

The candidate must be exactly one current Claude or Codex row with the full terminal id, assistant,
tty, pid/start and process-proved conversation tuple. Reconnect enters the same process gate and
regular non-symlink `flock` as registration and force-reloads durable state before deciding. It
fails closed for absent, corrupt and unsupported records. The expected UUID identifies the stable
role, while the generation is the lifecycle compare-and-swap value because the UUID intentionally
survives reconnect. Both are checked under the `flock`; either mismatch returns safe current
coordinator metadata and requires the caller to refresh before any mutation or idempotent answer.

An exact current binding is idempotent. Any different candidate is accepted only when one complete,
current SessionWatch scan contains the candidate and does not contain the old exact tuple. If the
old tuple is present, `coordinator_online` refuses live takeover. If the scan is incomplete,
`coordinator_liveness_unknown` refuses to convert absence of evidence into offline proof. This is
also the answer when the scan predates the current binding's construction or last reconnect: a
pre-binding snapshot cannot disprove a newer tuple. This is the same observer lesson as host
loopback: one constrained or stale observer being unable to see something is not proof that it died.
The timestamp comes from `SessionWatch.scanObservedAt`, set when `apply` accepts the completed scan,
and crosses RemoteServer unchanged. HTTP handling never stamps cached targets with `Date()`. A
missing scan timestamp fails closed. Process-local `scanGeneration` remains useful provenance but
is never sole offline proof, because it resets when the app restarts.

Success atomically replaces and reads back only the private process binding. It preserves the
stable UUID, scope, label and original `registered_at`; advances a monotonic `generation`; and sets
`rebound_at`. An A1 record with neither lifecycle field remains valid generation 1. Bearings exposes
the generation and reconnect timestamp, but never tty, pid, process start or conversation id. The
old process immediately loses its optional `session.coordinator` projection and only the exact new
process receives it. The new reconnect time must be no earlier than both the previous construction
or reconnect barrier and the completed scan consumed by the decision. A backward wall clock fails
closed rather than making later pre-binding evidence appear fresh.

This phase is intentionally a lifecycle substrate, not autonomous operation. It does not create a
tab, start or wake an assistant, resume a transcript, grant project transcript access, type into a
Session, dispatch a task, mutate waits/landings/git, Build, stop/delete a process, or execute the web
menu's `reconnect` command. The menu remains disabled; an authenticated local operator chooses an
already-live replacement. Future start/restore or health-driven behavior must keep the separate
observer provenance and explicit policy boundaries rather than widening this endpoint.

### Session work-state projection: one answer, separate evidence axes

Every live Session row carries exactly one closed `work_state`: `ready`, `working`,
`waiting_human`, `waiting_session`, `needs_triage`, `milestone_complete`, or `work_complete`.
This is not another truth store. The broker deterministically projects it from the terminal
presence reading, the task registry's authenticated result, the matching landing record, durable
handoff state, and coordination waits. Those sources remain separate, and top-level terminal
`state` is unchanged. A missing or unknown projected value fails closed in the web client as
`needs_triage`, never as blank idle and never as a check.

The precedence is `waiting_human` (terminal question) > `waiting_session` (waiting-on or owed wait)
> unreadable or missing evidence (`needs_triage`) > current `working` > delivered milestone >
broker-verified target landing. Current work intentionally outranks an older receipt: during the
child's linger somebody can resume using the terminal, and the earlier assignment's success cannot claim
that new activity is finished. `waiting_human` remains the only state that requests a person's
attention or drives the loud row/push. `waiting_session` stays the quiet `⏳` relationship.

A successful task with `finishedAt` is `milestone_complete` (one check) only when the task receipt
is bound to the process occupying the Session now: exact assistant, terminal and tty, pid plus
process start, process-bound rollout/conversation id, and the transcript's task-marker proof must
all agree. A terminal id reused by a later ordinary or assistant process cannot borrow the settled
task. Missing legacy identity fields fail closed. An open handoff's `from_session` is compared in
two strict namespaces—exact terminal id and exact process-bound conversation id—with no prefix,
title, tty, or fallback guessing.

That one check is authenticated, durable reported evidence that the current assignment/phase
delivered; review, landing, handoff, waits, or later graph nodes may remain. It becomes
`work_complete` (two checks) only when the same task also has the new machine-authenticated,
git-verified target landing fields above and the terminal has no unresolved coordination wait or
handoff. Legacy landed rows without those fields remain a milestone. Neither child prose nor
progress notes nor Clawdfather advisory can write either check. Clawdfather explains which receipt
is missing, coordinates its owner, and prioritizes `needs_triage`; it is not a status-truth writer.

The existing task result is the typed, durable Session report: `success` maps to delivered
milestone evidence, while `failure`, `timeout`, cancellation, or a missing finish receipt map to
triage rather than completion. Natural-language `/progress` notes remain display-only context.
This deliberately reuses the authenticated, versioned task/result registry instead of adding a
second session-status API that could drift. A child may intend that all work is complete, but that
intent is still only its `success` receipt; it cannot directly produce `work_complete`.

The completion scope is deliberately `task`, recorded in the Session's optional `disposition`
metadata. The registry does not yet model one authoritative set of every descendant, review,
landing, and handoff obligation belonging to a human root's whole graph. Claiming that broader
completion would be invented global truth, so the projection fails closed and never calls a root
graph complete. The typed evidence name is `broker_verified_target_landing`, not “task closure”:
the broker verified local git containment, not the root's complete test/review graph. `ready` is
likewise not inferred for an idle assistant: without positive evidence
that no assignment exists, its stopped state is `needs_triage` (the health target for this queue is
zero). Plain non-assistant prompts can be `ready` because their absence of an assistant assignment
is directly observable.

### The protocol has a living Claude Code Artifact

`artifacts/2026-08-26-clawdline-communication-protocol.html` is the human-readable, visual surface of
this protocol. It is a standalone HTML Artifact with `artifact:kind=state`, so it is not a historical
audit: it must describe the current dispatch, review, landing, handoff, claim, file-wait and
cross-assistant notification behavior.

Every change to those semantics includes an update to that same Artifact in its root-owned landing
closure. A Claude Code session reads the full authoritative sources — `AGENTS.md`, this document,
`docs/api.md`, `docs/handoff.md`, both language versions of the Clawdline skill and the machine's
dispatch policy — and updates the Artifact's diagrams, state words, source pointers and operator
checklist. Root then verifies the standalone file against those sources. A green implementation or
review verdict does not close a protocol change while the Artifact still describes the old rules.

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

The deadline is not permission to force-close a busy child. Linger cleanup and explicit root close
share one bounded terminal broker, one per-task closing guard, and one success-only deadline clear.
Inventory, screen classification and the irreversible decision all execute inside that broker.
Activity is `busy`, `idle` or `unknown`; capture failure is `unknown` and can never authorize close.
Immediately before every explicit or linger close Clawdline takes a fresh complete iTerm/tmux
inventory and requires the same terminal id, backend and tty. After `/quit` or `/exit`, it re-scans
the exact tty; every TERM/KILL rung is bound to the same `(pid, process start)` identity, so a
different PID or the same PID reused by a later process fails closed. Only complete inventory plus
an exact-tty observation proving the assistant absent permits the tab close. A failed scan or a process still present after TERM/KILL
leaves the tab open and records `terminal_intervention`. There is no ten-minute force-close. An
empty whole-machine reading may forget a missing tab only when SessionWatch marks that empty
inventory authoritative. Only an actual iTerm automation timeout/malformed list reports
`answer_dialog`; process-scan failures and a still-running tty report `inspect_terminal` instead.
That distinction is persisted as typed state. A modal gets one automatic retry after a fresh
well-formed iTerm list closes the circuit; `inspect_terminal` never repeats `/exit`, TERM or KILL
on the five-second beat.

The same global admission domain also owns manual and timer schedule fire, serialized task
promotion, background shell kill, focus/start/resume, root cascades and every terminal-bearing
task/handoff/wait delivery. It admits eight operations globally and two per real recipient session;
nested cascades execute inline to preserve ordering, while inheriting or adding recipient
accounting. tmux subprocesses have a real deadline and TERM/SIGKILL cleanup, so a hung tmux process
settles as a typed timeout rather than occupying the serial lane forever.

**A `spawn_failed` that never reached briefing is the exception, and it closes at once.** Nothing
of the task is on that screen: the session was opened and never spoken to, so what is there is a
fresh prompt that explains less than the summary does. And keeping it is not free — each one is a
live assistant holding a slot, while the usual reason for failing to reach a prompt in the first
place is *too many sessions starting at once*. Left alone they compound: four dead tabs still
running, and the next two spawns failing for the reason the first four did.

### Being told

When a task finalizes, the app looks for the root's terminal — the root declared a session id, and
Clawdline knows which tty each session id belongs to from [its hooks](hooks.md) — and if it finds
one that is not currently showing a menu, it types one compact semantic message into it. On the
wire that message is one exact line: a wrapper around one JSON object with no LF or CR bytes, so
both iTerm and tmux preserve it byte-for-byte:

```
<clawdline-notice>{"audience":"root","body":"[clawdline] task 3f9a21bc (Project portrait) finished: success — read /tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/result.json","child_may_still_write":false,"claims_released":false,"kind":"task_finished","outstanding":0,"protocol":"clawdline.notice","result_path":"/tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/result.json","state":"success","task":{"id":"3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234","title":"Project portrait"},"version":2}</clawdline-notice>
```

`protocol` and `version` identify the envelope. Version 1 remains a closed legacy schema with two
`kind` values, `task_finished` and `workspace_overlap`; readers continue to accept literal version-1
rows already stored in transcripts. New writers emit version 2, whose closed set is those two plus
`file_wait_request`, `file_wait_release`, and `handoff_receipt`. A completion has a closed terminal
`state`, its task, audience, result path, outstanding-child count and the two timeout/claim flags. An
overlap has the new task plus one or more `{task,path}` rows. A file-wait request carries `wait_id`,
`repository`, non-empty `paths`, `waiter_session_id`, `reason`, and `release_condition`; a release
carries the same wait/repository/paths identity plus optional `commit` and `note`. Both handoff
outcomes are one `handoff_receipt` kind because they report the result of the same delivery attempt:
its closed `state` is `picked_up` or `first_line_failed`, beside `handoff_id`, optional `title`,
`assistant`, and `project_dir`. `body` is the concise fallback the assistant can read;
it still says what finished, where `result.json` is, whether sibling work remains, whether a
timed-out tab may still write, and — as prose, with no field of its own — any declared claims the
child never touched. It is not the child's summary.

Recognition is strict and whole-message only. Any LF or CR, extra keys, unknown kinds or versions,
malformed JSON, missing wrapper bytes, text before or after the wrapper, and quoted lookalikes are
not partly interpreted. They remain visible with their full text, as whatever the row they arrived
in already was — an ordinary user turn, or a peer message when the lookalike was quoted inside a
cross-session envelope. Claude transcripts and Codex rollouts decode the same envelope to a
dedicated notice entry; no reader reconstructs semantics from `body`.

One delivery attempt; a failure is logged and not retried, because the second copy of a
notification is worse than none.

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
   of the batch. For code, that graph names the delivery branch, target, review node, landing owner
   and final root-owned landing-and-verification step.
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
6. **Report, wait, then close the root obligation.** No polling loop. The notification arrives on
   its own, and `GET /v1/orchestrator/tasks/:id` answers when somebody asks. A child `success` and
   `SAFE TO LAND` remain delivered/reviewed facts; for code, root integrates on the named target,
   verifies the exact integrated tree and records its commit before reporting completion. An unsafe
   dirty tree keeps that obligation pending or sends it through a named, receipted handoff.
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

The local implementation of the cloud blueprint's Phase 6 is [scheduled tasks](schedules.md): its
clock calculation runs separately, but every actual scheduled dispatch returns to the same serial
server queue, capacity buckets and lifecycle described above. A paired read-only device can inspect
the schedule list through the same orchestrator GET route; only manual run needs the orchestrator
token. Its clock is on this Mac today, while its generated task uses this protocol unchanged.

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
