# Handing work to another session

For task templates that dispatch on a clock, including catch-up and tab-close policy, see
[`schedules.md`](schedules.md). Scheduled work enters the ordinary lifecycle described here.
The distinction between task completion, Session work state and proof that a root Session is safe
to close is specified separately in [`session-closeability.md`](session-closeability.md), and its
projection, attestation route and compare-and-swap close gate have shipped. Do not infer
closeability from `idle`, `ready` or a child task's `work_complete` check: read the Session row's
`closeability` block, which says so directly and names why when the answer is no.

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
  happened. It is the bottom of the tree: it dispatches nothing.

The root never touches a terminal and never learns the child's id until the broker tells it. The
child never learns who asked. Neither of them can dispatch on the other's behalf, and a child
cannot dispatch at all — [that is a rule with teeth](#the-tree-is-one-level-deep-and-that-is-structural).

## Root Assignment is the fourth primitive

`POST /v1/orchestrator/root-assignments` launches a new ordinary Feature Root rather than adding a
child to this tree. Its closed request names assistant, model, canonical project, label, and only
five briefing fields: objective, scope, constraints, relevant references, and acceptance. It has no
task secret, timeout, result file, parent, handoff package, detached flag, or broker landing
obligation. Closing the caller does not close it.

The request cannot choose a language. When the broker accepts a new assignment, it resolves the
Mac Clawdline interface language, stores its canonical catalog tag and rendered name on that
durable assignment, and puts an explicit language contract in the briefing. That contract covers
the Root's first response, commentary, questions, progress, final response and every other
user-facing message. Initial injection, retry, restart reconciliation and transcript matching all
rebuild the same bytes from the stored value; changing Settings afterwards cannot change the
accepted briefing or its receipt. A legacy durable row with no language field keeps the historical
briefing bytes so an already delivered turn remains a match and is not resent.

The broker records `accepted` before opening a tab, then `terminal_opened`, `prompt_ready`,
`briefed`, and `active`, with `blocked`, `failed`, and `inactive` as explicit alternatives. The
request UUID plus canonical-body digest makes retries durable: identical content replays one stable
non-failed assignment id, while changed content conflicts. A terminal failed id replays as
`request_terminated` and requires a new request id rather than pretending a launch succeeded. A restart with only `accepted` fails
`launch_receipt_lost` instead of risking a duplicate tab.

After a terminal receipt exists, reconciliation uses one exact assistant, terminal/tty,
PID/process-start, and conversation tuple. A stale inventory waits; two candidates fail ambiguity;
confirmed process loss fails before briefing and becomes inactive afterwards. Workspace trust is
also fail-closed: the current build has no automatic workspace-trust authority, so the picker is
left for a person without consuming the ordinary 240-second terminal-open-to-briefing deadline;
answering it starts a durable fresh 240-second pre-brief window and lets the record continue. Any future positive policy adapter must explicitly justify
acceptance and must persist the one-answer picker receipt before typing. Every blocked, failed or
inactive transition is audit-visible once with its exact available assignment/terminal identity,
so a never-briefed orphan tab stays findable while a genuinely briefed Root is never cascade-closed.
Session and web UI receive the bounded nested
`root_assignment:{id,label,state,ownership,explanation}` projection separate from the child `Role`
index; it does not invent a `role:"root_assignment"` field.

Creation and authenticated list/read use only the mode-0600 orchestrator token. See
[`docs/api.md`](api.md#post-v1orchestratorroot-assignments) for the closed body, bounds and errors.

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

### The tree is one level deep, and that is structural

Dispatch without a floor is a fork bomb with a language model in it. One task becomes five becomes
twenty-five, each one a real terminal tab running a real assistant against a real API bill, and the
failure mode is not "it got slow" — it is a Mac with sixty tabs open and no obvious way to tell
which of them started it.

So the tree has a bottom, and it is directly under the top. **A root opens children. A child opens
nothing.** Work inside one child that wants to run in parallel goes to that assistant's own
subagents — Claude Code's Task tool, Codex's subagents — which open no terminal tab, pass through
no broker, and hand their answers back into the session that asked instead of into a file somebody
has to wait for.

**The floor is a constant in the code, not a setting.** `Orchestrator.depthFloor` is `1` and there
is nothing to edit. It used to be read out of `orchestrator_max_grandchildren`, and that is exactly
the shape this project has a name for: `config.json` is seeded once and never migrated, so every
Mac that had already run the app kept its own copy of the old number and a changed default reached
none of them. A depth that a hand-edit can restore is not a rule; it is a preference. The key is
still accepted in an old file — reading it is not an error — and it is read by nothing.

The floor is enforced twice, and the two fail differently.

**The briefing says so.** `CHILD.md` tells every child, in the same paragraph for all of them, that
it is the bottom of the tree, that a dispatch it attempts is refused, and what to use instead.
[`skills/clawdline/SKILL.md`](../skills/clawdline/SKILL.md) routes a root to the same rule, which
now ships in the guide beside the build rather than in the installed stub. A child that follows its
instructions never has to find the limit by hitting it.

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
- **Nothing at all, for a child.** There is no second cap because there is no second level, and
  no setting either: a child that tries is refused at the door by `409 depth_exceeded`.
- **Twenty dispatched sessions, for the Mac** — `orchestrator_max_children × 4`, four roots'
  worth, and not a setting because it is not a choice separate from the number it is made of.
  Several root sessions share one Mac, so it is deliberately more than one root's cap. The
  per-session cap is the one a caller could sidestep by claiming to be somebody else; this is the
  one that still holds when it does.
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

Two files beside the registry say **how** work should be handed out, and what is true of this
machine. `~/.config/clawdline/dispatch-policy.md` is the editable base; the optional sibling
`dispatch-policy.local.md` holds facts true only here. Both are read fresh on every dispatch — an
edit reaches the next task, not the next launch — and composed into the `CHILD.md` of **every**
child.

Every child, and not only a dispatcher, because the file carries more than rules about handing
work out. The sentence in this Mac's own policy saying a Codex sandbox has no network is what
stops a Codex leaf spending a turn on a `curl` that cannot connect: a leaf reads it and behaves
differently, which is the test of whether a paragraph belongs in a briefing at all. When the tree
lost its second level the section was briefly deleted along with the dispatch recipe it travelled
with, on the reading that house rules are rules about dispatching. They are not, and this Mac
would have lost its only channel for telling a child anything about itself.

It ships with opinions rather than a comment saying "put your rules here", because a file with
defensible rules already in it is one somebody edits and an empty one is a feature nobody finds.
**What ships is `Resources/dispatch-policy.md`** — the file this repository edits, copied into the
app bundle by `build.sh` and used to seed the base once when a machine has none. It used to be a
Swift string literal holding an older draft of the same rules, which is worse than it sounds:
`ensurePolicyFile` writes that copy, so it is exactly what a fresh install receives, and a machine
could start life with rules nobody had read for months. If the resource is missing there are no
base rules and no file is written; `ensurePolicyFile` never overwrites an existing base. The app
never seeds, writes, overwrites, or syncs `dispatch-policy.local.md`. Its absence and an empty file
both mean there are no machine-local additions.

The base is placed first. A visible heading introduces the local file last, so its more specific
rules win when the two disagree. The 12,000-character cut follows the same precedence: when the
pair is too long the base is cut at a paragraph boundary and the cut is announced, while the local
part survives whole. Only a local file that exceeds the limit by itself is cut and announced. The
Settings edit button still opens the base; its status and line count describe the composed rules.

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
  "graph": {
    "id": "7f7b3c1a-8e1b-4f31-9b75-61f6ef881234",
    "destination": "The reviewed portrait is landed on main.",
    "current_node": "portrait",
    "nodes": [
      {"id":"portrait","title":"Draw the portrait","kind":"delivery","depends_on":[],"acceptance":["The SVG matches the brief."]},
      {"id":"review","title":"Review the portrait","kind":"review","depends_on":["portrait"],"acceptance":["All review axes have a verdict."]}
    ],
    "unknowns": [],
    "out_of_scope": ["Changing the mascot format."]
  },
  "claims": ["Sources/Orchestrator.swift", "docs"],
  "serialize": ["build"],
  "attach_session": "B6ADA755-5815-4008-8287-85ED28EFE4F4",
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
| `plan` | optional legacy free-text note, ≤ 4 KiB. New multi-node work uses `graph` |
| `graph` | optional typed decision-and-delivery graph. `id` is a lowercase UUID; `destination` is 1…500 characters; `current_node` names one of 1…32 unique acyclic nodes. Every node has a lower-case id, title, kind (`decision` · `delivery` · `review` · `correction` · `verification` · `landing`), declared dependencies, and 1…8 acceptance strings. `unknowns` and `out_of_scope` each hold 0…8 strings. Unknown keys, duplicate ids, missing dependencies, self-edges and cycles are refused |
| `claims` | optional array of 0…32 unique POSIX paths relative to `project_dir`; each is 1…1024 characters, may not start with `/`, and may not contain a `..` component. A directory claim covers its whole subtree; `[]` explicitly declares read-only work |
| `serialize` | optional array of 0…4 unique operation names. Each uses the `model` token rule: 1…64 characters from `[a-z0-9._-]`, not starting with `-` |
| `isolation` | optional `none` or `worktree`; absent is `none`. Unknown values are refused, never downgraded |
| `isolation_base` | optional Git revision, legal only with `isolation: "worktree"`; 1…200 characters from letters, digits, `._/-~`, not starting with `-` and not containing `..`. Absent means `HEAD`; it must resolve to a commit |
| `attach_session` | optional terminal-neutral Session id of a standing session Clawdline opened with launch-time access to the whole task root. When present, deliver this complete task into that existing assistant session without opening a tab. A user-opened session or a Clawdline leaf launched with access only to its original task directory is `409 attach_not_managed` |
| `project_dir` | absolute, exists, and is a directory — checked at dispatch, not at planning time |
| `title` | ≤ 200 characters |
| `instructions` | non-empty, ≤ 16 KiB |
| `timeout_minutes` | 1…240; absent means 30 |
| `root.session_id` | the dispatcher's process-bound assistant conversation id. Ordinary `/v1/orchestrator/tasks` dispatch requires a live resolvable owner; a terminal-neutral id belongs only on terminal-addressed routes |
| `root.assistant` | required `claude` or `codex` whenever a new ordinary HTTP dispatch has a non-null `root.session_id`. Omission or explicit `null` is `422 root_assistant_required`; no capacity is spent, task registered or terminal opened. Persisted legacy rows remain readable; absence is unknown in ownership decisions, while the old Claude default remains only in non-ownership compatibility readers. Other values, including an empty string, are refused |
| `root.parent_task` | the dispatcher's **own** task id, when the dispatcher is a child. `null` from a root. A value that is not a task id is read as `null` |
| `root.poll_only` | optional boolean, default `false`. Ordinary `/tasks` accepts only `false`; the dedicated `/detached-tasks` route requires `true` with a null `root.session_id`, and its unattended caller must poll |

`root.session_id` is required for ordinary owned-child dispatch. `/v1/orchestrator/tasks` fails
with `422 detached_route_required` when `root.poll_only:true`, and with
`422 root_session_required` when the field is null or empty. A non-null spelling with no live process-bound owner fails with
`422 root_unresolved`; a non-null id without explicit `root.assistant` fails first with
`422 root_assistant_required`; two same-assistant processes proving the same conversation fail with
`409 conversation_ambiguous`. Neither is a warning followed by an orphan-shaped task. Codex
normally exports `CODEX_THREAD_ID` (with `CODEX_SESSION_ID` as the compatible spelling)
and that value is the rollout session id. Claude has no direct self-query and uses the transcript
nonce procedure in the skill. Identity failure is not a mode decision: prove the current Root with
`GET /v1/orchestrator/whoami` and resend the same task id.

Unattended automation that was deliberately designed with no interactive owner uses the separate
`POST /v1/orchestrator/detached-tasks` route. That door accepts only a null session id together with
`root.poll_only:true`, otherwise returning `422 detached_task_required`. It has no completion
notification, owner grouping, per-root capacity, or root-close cascade. It is never a Root,
Major Feature, or fallback for a failed owned-child lookup.

A watched terminal id is **not** a substitute for that conversation id. On a new dispatch the
broker compares `root.session_id` with positive process-bound evidence from the active terminal
inventory and with the durable Coordinator's current binding. When either safely proves that the
supplied value is the physical terminal id, dispatch is refused as
`422 root_identity_is_terminal`; evidence is collected independently of the caller-declared
`root.assistant`, and the error includes both `canonical_root_session_id` and the proved
`canonical_root_assistant`. Correct both values and resend the same task id.
Unknown and offline tuples are not guessed: an HTTP dispatch is refused as `root_unresolved`.
Conflicting same-assistant process owners are also not guessed and are refused as
`conversation_ambiguous`. Existing persisted tasks keep their historical root value, and task
mounting still accepts conversation ids only.

Two things follow from the dedicated poll-only mode, and the second one surprises people. The task is not told
when it finishes, so the dispatcher has to poll. And **its row has nothing to sit under**: the
list indents a child beneath the session that asked for it, that session is found through this
id, and a task that named nobody is filed under nobody. The row still says `Child`, because being
somebody's child is a fact about that session whether or not the parent is on screen — but it
floats at whatever position the sort gave it, which reads at a glance like a bug in the grouping
rather than a task that declined to say who asked. If a row belonging under yours matters, send
the id and `root.assistant` together.

Resolution happens once, before capacity, grouping and the task record are chosen. The supplied
current conversation id becomes the durable root key. Completion
notification, `liveTasks(dispatchedBy:)`, session grouping and root-close cascade all consume that
canonical key. If a non-null spelling matches no one, HTTP dispatch returns `422 root_unresolved`;
two same-assistant processes proving the same conversation return
`409 conversation_ambiguous`. Both happen before registering or opening anything. Dedicated
poll-only automation uses a null id plus the explicit flag through its own route; it cannot disguise
an unresolved owner as a warning.

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
overlap lines, root-close cancellation and batch-notification deep links. Handoff is the one named
exception, and it is a wider namespace rather than a looser one: its `from_session` may be either
the conversation id or the watched terminal id, and since the sender contract landed it is required
and must resolve to exactly one of them — the two are compared whole, and anything but one match is
a typed refusal. See [`docs/handoff.md`](handoff.md#the-route).

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

`graph` is the machine-readable decision and delivery map; `plan` remains its backwards-compatible
free-text note. The complete graph travels with every node, while `current_node` identifies the
assignment in front of this child. At admission, a repeated graph id must carry the same
destination, nodes, unknowns and scope while that graph remains in the bounded registry. A node
with dependencies is dispatchable only when each
dependency has durable completion evidence: ordinary nodes need task `success`, review nodes need
a `safe_to_land` review receipt, verification nodes need `verification.last == pass`, and landing
nodes need a broker-verified `landed` receipt — one whose commit the broker resolved inside the
task's repository and proved contained by the named local target. The caller
cannot send a `ready` flag. The broker derives `ready`, `blocked`, `active`, `done`, `failed`, and
`awaiting_landing`, publishes the current `frontier`, and fails closed with
`graph_frontier_blocked`, `graph_dependency_failed`, `graph_definition_conflict`,
`graph_node_active`, or `graph_node_complete`. A completed node cannot be replayed into `active`;
model a correction as a separate node.

**A landing node's receipt is on the delivery, not on the landing node.** Landing is the root's, and
a root does not dispatch a task to itself, so that node normally holds no task at all — which is why
it read `planned` forever while the landing it was waiting for was already recorded a node away. It
reaches `done` when every `delivery` node it transitively depends on has a task carrying
`landing.state == landed`, and `correction` nodes count the same way once they have a task; a
correction that was never dispatched is a repair no review demanded, not evidence that is missing.
The derivation needs at least one such receipt, so a landing node with nothing under it that
produces bytes stays `blocked` instead of being satisfied by an empty conjunction, and it does not
consult the producing task's own outcome — a root records the receipt after proving the commit is
contained by the named local target, and work has been landed after the task that produced it timed
out. A landing node that does have a task of its own reads that task's `landed` receipt first and
falls back to the same derivation, so `awaiting_landing` now means neither record exists.

Both forms go near the top of `CHILD.md`, above even the language rule, because they are context
for every other line. A child that knows what its output feeds writes something joinable; a child
that knows only its own title tends to write a report.

`root.parent_task` is the same field one level down, and it exists because a child knows its own
task id from the first line it was ever sent, long before this app has worked out what the session
in that tab calls itself. It is the strongest answer for every child because the broker already
owns that task-to-terminal link; it also works before either assistant's transcript identity is
observable. Naming it is what gets a task filed under its actual parent on the first try instead
of being counted as a root's. Getting it wrong costs capacity and never buys any — [the two names
are combined by taking the deeper answer](#the-tree-is-one-level-deep-and-that-is-structural).

### Attached follow-up tasks

`attach_session` turns dispatch into a complete follow-up assignment for a standing assistant
session. The task still has a fresh id and secret, its own task directory and `CHILD.md`, claims,
serialize tokens, timeout, usage, result signal, landing record and inflight visibility. The only
difference is that Clawdline types the ordinary first line into the named existing session instead
of opening a terminal tab. The public task record carries `attached: true` and `attachSession`.

**The session needs a recorded task role and the right launch-time grant.** Clawdline gives the
whole `/tmp/.clawdline` task root to a session it opened for a task at or above the floor; a task
below it opens no tab at all, and a user-opened assistant has no recorded task-root grant. A
session holding only `/tmp/.clawdline/<its-original-task-id>` cannot read a new follow-up's sibling
`CHILD.md`, even though Clawdline opened its tab. Because `--add-dir` cannot be added to a running
process, either shape is `409 attach_not_managed`, refused before anything is typed. This grant is
now what the wider add-dir is *for*: a child dispatches nothing, so the only directory it cannot
name in advance is the one a later follow-up task will be given. The registry persists the actual
grant used at launch rather than inferring it from depth.

The id is resolved against every terminal session Clawdline can see, which is wider than the
assistant-only rows `GET /v1/orchestrator/sessions` publishes — a plain shell resolves and is then
refused by name rather than reported missing. A shell is unsupported, the resident assistant must
match the task's assistant, and **one session runs at most one live Clawdline task**. That
single-flight population excludes the task being resolved for, so a serialized attached task —
registered while it queues, resolved again when the pump promotes it — does not read its own
reservation as somebody else's. The single-flight check is repeated under the registration lock,
so two concurrent requests cannot both pass a stale inventory. A cached `waiting` state triggers
the same narrow `Targets.isChoosing` screen proof as
coordination-wait delivery; a confirmed menu refuses the dispatch before any line is typed or task
record is created.

An attached task keeps the standing session's existing depth — the depth of the task that session
was opened for. Acceptance depends on the persisted task-root grant, not that number: a session
launched with only its own task directory is refused. It opens no tab and therefore spends no
child or machine tab-opening capacity, although it remains a live task, passes through
the dispatch rate limiter and quota gate, and holds its ordinary claims and serialize reservations.
If terminal delivery itself fails, the registered task finalizes as `spawn_failed` and the request
returns `502 attach_delivery_failed`.

The task does not own the tab. Success, failure, timeout, cancellation, root-close cascade and any
`orchestrator_child_linger` value leave the standing session open. Its briefing says that writing
`result.json` completes this task but does not end the session, which can then receive a later
complete follow-up assignment. It also does not own the session's *name*: an attached task
publishes a live role on that session while it runs, so `GET /v1/orchestrator/sessions` shows the
`taskId`, but it never renames the session. When it ends, the role and title of the earlier task
that opened this standing child session are visible again.

**Clawdline never answers a menu on a session it did not open.** On a fresh tab there is one menu
to answer — the trusted-folder dialog — and the root answered it by asking for work in that
directory, so the first row is taken once and audited. An attached task did not open its standing
child session, and a menu there can be a permission prompt, a plan approval or an overwrite
confirmation from the work already resident in that tab. Those are left standing and audited as
`orchestrator.menu.left`; once that first decision is recorded, an unchanged menu does not rewrite
the registry or broadcast every five seconds.

**An attachable standing session already has `/tmp/.clawdline` access.** Clawdline records whether
that exact add-dir grant was used when the process launched. A user-opened assistant and a
Clawdline-opened leaf that received only its own task directory are both refused as
`409 attach_not_managed` before anything is typed; `--add-dir` cannot be added afterwards.

The four-minute `readyLimit` applies only to a new tab that never reaches a prompt. An attached
task's first line was already typed by `spawnAttached`, so waiting longer for the standing
session's owner to answer a menu does not relabel delivered work as `spawn_failed`. The wait is
nevertheless bounded: before transcript acceptance, its ordinary `timeout_minutes` runs from that
delivery. Expiry finalizes the task as `timeout`, releases claims and serialize tokens, and returns
the standing session to its earlier role. Budget for what that means end to end: **an attached
task's total wall-clock can reach twice `timeout_minutes`**, once waiting to be picked up and once
running. A dispatcher choosing that number for a standing session is choosing half of the longest
the work can take.

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
| absent | **refused `422 claims_required`** on an ordinary or detached dispatch; still readable, and still an unknown write set, on a stored schedule's task, a respawn, and every record admitted before the requirement | reserves no lease; L1 keeps its directory warning, and where it is still accepted the dispatch reply carries `claims_missing` |

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

**So the quiet one was answered out loud, and out loud was not enough.** 60.7% of the dispatches
measured on this machine declared nothing at all. Declaring costs the root about twenty output
tokens, and a collision costs a whole task — three to eighteen million on that same record — so an
absent field put a `claims_missing` item in the dispatch reply's `warnings`, on the first request
and on the idempotent retry alike. The share did not move, for the reason every ignorable signal on
this machine is ignored: a warning costs the caller nothing. An absent field is now
`422 claims_required`, with nothing registered, nothing opened and the rate ticket refunded.

**The requirement is on the key, not on a non-empty list**, and that line is what keeps it fillable.
A required non-empty list would make `[]` unsayable, and `[]` is the honest answer for a review, an
audit, or anything else that reads and reports; forcing it to name a path it does not write would
put a false reservation into the lease. So there are always three answers: the files, the
directories containing them when the files are not settled, and `[]`. **`"claims": []` still does
not warn**, and that difference is the whole point — warning about a positive read-only declaration
would teach callers that the field is noise, which is how omission reached 60.7% in the first place.

**Declare what the task decides to write, not what a repository guard writes for it.** A ratcheted
line count, a generated manifest or a sealed check total moves on almost every change, so no
dispatcher can foresee it and the broker does not derive it. Leasing such a path would make every
pair of tasks in the same source tree intersect — `claims_overlap` inside a root, `409
workspace_busy` between two — for contention that is settled when the merged tree re-runs the
guard, not by scheduling. It belongs to whoever lands the change.

**Two paths keep the warning rather than the refusal, and both because no caller is holding the
answer**: a stored schedule's task template, whose editor has no `claims` control on either
surface, and a respawn of a body that was already admitted once. Nothing revalidates work in
flight — a record admitted before the requirement keeps its absent declaration, and its idempotent
retry keeps the warning.

The best evidence for requiring it is not an argument. The root session that specified the warning
dispatched the review of its own delivery without `claims`, and drew the `workspace_overlap` notice
that `claims_missing` exists to prevent — on the day it implemented the guard.

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
pumps it once as well. A queued task already occupies its dispatcher's child slot and the
machine-wide descendants slot; counting registration rather than open tabs prevents
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
`parent_task` links back to the same root, so a task does not warn about its parent or its
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
outputs go, how long it has, the graph it is one node of, that it is the bottom of the tree and
what to use instead of dispatching, that it must not read other task directories, and exactly what
`result.json` has to look like.

**It asks for one progress note before the work starts.** `AGENTS.md`, `docs/dispatching.md` and
the dispatch policy have all required it for a while; the briefing — the only thing a child
actually reads — asked only for a note when the work drifted. So `CHILD.md` now asks for the first
one within about three minutes: one sentence saying what the child has decided to do now that it
has read the briefing and `task.json`, before the work rather than during it. The reason is in the
briefing too, because a child that knows why will actually send it — it is the only thing that lets
a wrong direction be cancelled at minute three instead of minute twenty-six, and the two dearest
cancelled tasks on this machine burned 18.5M and 16.5M tokens before anybody could tell what they
had set off to do.

**And it only asks through channels the child can reach.** The progress ask was originally one
curl for everybody, and for a Codex child that ask was physically impossible: its sandbox sets
`CODEX_SANDBOX_NETWORK_DISABLED=1`, loopback `curl` exits 7 after 0 ms, DNS itself is off, and no
approval prompt ever appears — measured on this machine by task be9a54c0, where 133 codex children
were briefed to send the curl and 0 notes arrived, against 26 of 40 claude children. So the
briefing is honest per assistant. A claude child keeps the HTTP fast path, with the file named as
the fallback; a codex child is told to write `progress.json` in its task directory — the same
whole-file-replace, task-secret-inside shape that has always made `result.json` work
([`docs/api.md`](api.md#post-v1orchestratortasksidprogress) has the collection rules) — and is
told its network is off rather than left to discover the failure by trying. The notify recipe, the
`inflight` self-check and the optional completion announce are loopback calls too, so a codex
briefing replaces each with what is true for it: nothing pushes, the plan it was dispatched with
is what it has, and the file alone is the completion signal.

**How to dispatch is in no briefing at all.** There was a `DISPATCHING.md` beside `CHILD.md`,
holding the credential path, the `root.parent_task` rule and the `curl`, written for the children
that were allowed to hand work on. Nothing is allowed to now, so nothing is written; the app
deletes the file if a re-brief finds one an older build left behind. What the measurement behind
that file said is still the reason the briefing stays short: across 206 dispatches on one machine,
28,323 characters of instructions on how to dispatch went into every direct child's briefing —
about 7,081 tokens each — and not one of those 206 children ever dispatched anything. **A
convenience summary of the dispatch recipe back in `CHILD.md` is the thing not to add**: the
credential path appearing in a briefing is what makes a rule that is otherwise structural look
negotiable.

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
  "verification": {"runs": 2, "seconds": 940, "last": "pass", "scope": "swift suite + web-schedules"},
  "finished_at": "2026-08-24T10:41:55Z"
}
```

Written last, and written atomically — to `result.json.tmp` and then `mv`, so the watcher never
sees half of it. The app checks it once a beat for every briefed task, hashes `task_secret`, and
compares against what it stored at dispatch in constant time. A file whose secret does not match is
**ignored** and logged once: a wrong secret in a task directory is either a bug or somebody
poking, and neither is a reason to finalize somebody's task.

`verification` is optional metadata about the proof the child actually ran. `runs` and `seconds`
are non-negative integers, `last` is `pass`, `fail`, or `skipped`, and `scope` is a short free-text
description. A well-formed object is stored on the task record. Older results without it work
unchanged, and a malformed object is ignored rather than turning an otherwise authenticated
success into failure. The briefing gives verification one third of `timeout_minutes`, while still
requiring one relevant compile-and-test pass and red-before-green for every new test. Until a
focused Swift runner ships, an implementer that cannot exercise its behavior more narrowly may use
one full-suite run labelled `focused_runner_unavailable`; reviewers do not repeat it, and root
still owns the exact integrated-tree acceptance.

A review node's result also carries a closed `review` receipt. It has exactly three independent
axes — `specification`, `repository_invariants`, and `runtime_failure_behavior` — and a verdict of
`safe_to_land` or `changes_required`. A passing axis has no findings. An axis with findings keeps
each finding's id, severity (`blocking`, `important`, or `minor`), summary, and concrete evidence.
`safe_to_land` is valid only when all three axes pass; `changes_required` must contain at least one
finding. A graph review node whose task says `success` but has no valid review receipt fails closed
and does not advance the frontier.

```json
{
  "review": {
    "verdict": "changes_required",
    "axes": [
      {"axis":"specification","status":"pass","findings":[]},
      {"axis":"repository_invariants","status":"findings","findings":[
        {"id":"R1","severity":"blocking","summary":"A shared-tree invariant is violated.","evidence":["named reproduction or source"]}
      ]},
      {"axis":"runtime_failure_behavior","status":"pass","findings":[]}
    ]
  }
}
```

`symbols` names every identifier the child's change introduced: new functions and types, new
fields, new string keys, the names of test groups it added. Names, not descriptions — the portrait
above introduced none, and `[]` says that positively where an absent field only says the child did
not answer. For a code task the list reads like
`["Orchestrator.Landing", "updateLanding(taskID:secret:raw:now:)", "orchestrator.landing"]`. It exists
because a shared working tree makes authorship unreadable from the diff alone — by the time root
comes to commit, the files a child edited may hold two or three sessions' unfinished work, and
vocabulary is the only reliable way to tell one session's hunks from another's. Guessing it has
produced staged trees that would not compile, which is the failure the field is there to prevent.
The child's briefing asks for it; **the broker does not**. `readResult` takes `status`, `summary`,
`artifacts` and the optional `verification` object and ignores every other key, so `symbols` never appears on a task record, in a
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
through the same finalize. `finishedAt` records execution; `resultVerifiedAt` is separate and is
set only after `result.json` passes the task-secret check. A `/complete` outcome therefore does not
pretend that the result file was verified.

**timeout** — `briefedAt + timeout_minutes`. Executor loss is a different, typed failure. One
incomplete scan or one complete absence changes nothing; only two fresh complete observations in
one SessionWatch process epoch, at least a minute apart, may settle `executor_missing` or
`identity_changed`. The task record keeps the inventory generation, epoch, time, provenance and
mismatch count. That lets a person distinguish a closed child from a scanner outage or a terminal
id reused by a replacement process without guessing from a label or cwd.

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

**The cascade goes deepest first, even though the tree is one level deep.** Anything filed under a
child goes before that child does — what is below is found through the session id its parent goes
by, and that stops being a useful thing to match on the moment that parent's tab is gone. It is
gathered from the *finished* children too, not only the live ones. In a live tree this walks one
level and stops, because nothing a child opens exists; the order and the finished-parent sweep are
kept for a stored record from an older build, where a task below a task can still be found.
Cancelling a single task does the same thing on a smaller scale.

**None of that is a decision the app makes for you, and describing it as a mechanism was not
enough.** On 2026-08-27, 23:20:37–:51 — fourteen seconds, one close — four briefed tasks under root
`01a04276` were cancelled together, among them a correction dispatched 75 seconds after the review
that demanded it and 25 minutes into its run. Nobody adopted them, and one line then sat for
fourteen hours behind a landing record reading `pending`, which is also the word for work somebody
is actively doing (`B-PENDING-CANNOT-SEE-ITS-EXECUTOR` in [`backlog.yaml`](backlog.yaml)). So a
close carries two obligations the cascade cannot carry for it — **read the live list before closing,
and name who adopts each orphan afterwards** — and both are written down in
[`AGENTS.md`](../AGENTS.md#closing-a-root-is-an-act-with-victims-look-before-you-do-it). The query
that answers the first is `GET /v1/orchestrator/tasks` filtered on `root.sessionId` and the three
unfinished states.

**spawn_failed** — the tab never happened, or never got briefed inside four minutes, or was typed
into five times without the child ever recording the message, or the app was restarted while the
task was in `spawning`. That last one is not a bug: once a task starts opening, the recoverable
queued secret is gone, so the app fails closed rather than risk opening the same global operation
twice. A serialized task that was still `queued` is recovered and pumped instead.

**A `spawn_failed` task can be retried by the broker rather than by the root.** It was 34 of 206
dispatches on the machine this was measured on, 2026-08-28, 33 of them Codex. That registry keeps
200 rows, so the figure is one rolling window and not a running total: a later reading counts a
different population rather than this one changed, and it must not be quoted as the current rate.
The answer used to be that the root writes the whole `task.json` out again under a fresh id,
because that id is finished and re-sending it just returns the terminal record. That is
thirty-four rewrites by the most context-loaded session in the tree, and every one of them is a
chance to drop a field.

[`POST /v1/orchestrator/tasks/:id/respawn`](api.md#post-v1orchestratortasksidrespawn) copies the
original `task.json` with a fresh `task_id`, mints a fresh secret unless the caller supplies one,
and dispatches it through the ordinary gate — same capacity, depth, claims, quota and
serialization rules, same refusals. `instructions` is why it is a file copy rather than a record
copy: the registry never held it.

Only `spawn_failed` may be retried, because it is the one terminal state that means *nothing ran*;
anything else is `409 not_respawnable`. **At most two respawns descend from one original**, counted
over the whole family below it rather than along any one chain — a retry of a retry cannot launder
the cap by being the first from its own immediate parent, and neither can asking the original
again, which is the shape a caller actually falls into because the id it has in hand is the one
that failed — and the third is `409 respawn_exhausted`. Each new task records
`respawn_of` and `respawn_generation`, so a chain reads as a chain in the registry instead of as
three unrelated tasks with the same title.

**A briefed task survives a restart as one logical task.** Its secret hash, root/task/session ids,
timeout, completion outbox and result ingress are durable. Those logical ids are never replaced by
whatever terminal happens to share its label. Startup compares the persisted child tuple —
terminal id, assistant, tty, pid and process start, plus conversation id — with one fresh complete
SessionWatch inventory. Exact match restores observation; incomplete evidence preserves the prior
receipt; persistent absence becomes `executor_missing`; the same terminal with a different process
becomes `identity_changed`. Neither typed mismatch automatically respawns or relinks anything: its
`mover` is `person`.

**Restart is a durable admission-and-drain protocol, not a live-task count.** Before replacement,
POST [`/v1/orchestrator/maintenance/restart`](api.md#post-v1orchestratormaintenancerestart-get-v1orchestratormaintenancerestart)
with one lowercase UUID. The terminal broker immediately refuses new mutations with typed,
retryable `restart_maintenance`, lets already-admitted cascades finish, and persists global and
per-terminal occupancy. Replacement is safe only when the same receipt says `phase:"ready"`,
`safe_to_replace:true`, and both counts are zero. Duplicate calls with the same id are idempotent;
a competing id cannot steal the gate.

`briefed` is durable and is not a restart blocker. A serialized task still `queued` is also safe
when its sealed secret is recoverable. `spawning` is unsafe because the only plaintext briefing
secret is process-local; maintenance refuses with `restart_blocked_by_task_secret` instead of
knowingly replacing the app. An uncoordinated replacement still fails that interrupted spawn
closed on startup and records `replacement_before_safe` on any active restart receipt.

The replacement listener resumes the persisted receipt as `reconciling` and keeps admission
closed. Exact matches can settle on the first fresh complete inventory. Absence or identity change
needs two different complete generations in the same process epoch at least 60 seconds apart.
Reconciliation is bounded at 120 seconds: after that the receipt becomes `complete`, records
`reconciliation_timed_out:true` plus the exact `unresolved_task_ids`, and reopens admission while
ordinary task watching continues to seek typed evidence. Health and SSE `hello` expose the same
per-process `instance`, and Session inventory exposes its own `epoch`, so a monitor can prove it
observed the replacement rather than a stale snapshot; every further replacement updates
`resumed_instance_id` again. Completion/result files written during the listener outage remain
durable inputs and are collected by the existing startup walk.

`DELETE /v1/orchestrator/maintenance/restart` with the same closed request-id body persists
`phase:"aborted"` and reopens admission. It is the explicit exit when the replacement is cancelled
or the drain cannot converge. A malformed durable receipt instead loads as fail-closed
`phase:"invalid"`, writes an audit row, and requires this explicit abort; corrupt state never turns
into silently open admission.

`build.sh` is the adopter. A current runtime must grant `ready` before it is stopped, and the new
listener must report `complete` before the script succeeds. Only an exact 404 from the installed old
runtime takes the one-time bootstrap path: the script uses its legacy queued/spawning preflight for
the replacement that installs this route, then every later build uses the durable receipt.

### A branch, not a diff

An isolated child's delivery is `clawdline/task/<task-id>`. A Claude child commits early, only on
that branch, and never pushes or switches branches. Finalize reads git itself and adds this object
to the record; `head`, `commits`, and `dirty` are best-effort and may be `null`:

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

**A Codex child in a worktree delivers a dirty tree, not commits, and that is not a failure.** The
worktree's git metadata lives in `<project_dir>/.git/worktrees/<task-id>/`, outside the directory
Codex's sandbox may write, so `git commit` there dies on
`fatal: Unable to create '…/index.lock': Operation not permitted` — after which a child that was
told to commit reports `failure` holding finished work. Dispatch a Codex worktree task with
instructions to leave the bytes uncommitted; its receipt then reads `"commits": 0` with
`"dirty": true`, and root lands it from `worktree.path` instead of from the branch. The generated
briefing does not yet distinguish the two assistants, so the task's own `instructions` must.

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
Every new task in a Git project persists the canonical common Git directory, regardless of
isolation: a legacy `.none` task may still have been dispatched from somebody else's disposable
worktree. Readable project/worktree evidence must agree with the receipt. A stale stored path may
fall back only to independently derived evidence. For legacy missing paths, derivation is bounded
to an exact broker-worktree-root/task-id shape whose repository slug matches one unique readable
worktree receipt in the private registry; arbitrary paths, conflicts and basename searches fail
closed. Arbitrary text, a
remote-only ref, or an unrelated commit is refused; a pending edit racing the git
checks is rejected by CAS. `landed_at` records when that proof was captured. Both `landed` and
`abandoned` require a terminal task and are final: neither
can return to another state, and genuinely reopened work gets a new task. `abandoned` is an
explicit decision not to land the delivery, not a temporary pause.

`GET /v1/orchestrator/landings` lists every current pending obligation with its title, owner root,
original claims, target, note, non-negative age, and a closed owner/executor observation. The
status distinguishes exact observed work, an observed ready/holding session, a still-live task,
absence from a complete inventory, and unknown/stale evidence. Only a complete timestamped
SessionWatch inventory can produce `not_observed`; missing assistant identity, incomplete or ambiguous evidence is
`unknown`, never dead/offline. Every row retains stable task/root ids and source times,
provenance/freshness without transcript, token, tty, pid or filesystem-secret data. The same rows
appear in Coordinator Bearings beside `pending_landing_count`, from the same registry snapshot.
The route uses a single-flight bounded SessionWatch observation that refreshes a cache; repeated
reads cannot accumulate main-queue work, and a wedged main queue
degrades the evidence to stale/missing and every owner to `unknown` while preserving registry rows.
Pending obligations are exempt from the
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
worktrees and compiler indexes in their own `/tmp/.clawdline/<task-id>/work/` directory. That
directory is reclaimed as part of task finalization: immediately for `success`, and after
`orchestrator_work_grace_minutes` for every other terminal state. The setting defaults to 60
minutes, accepts `0` for immediate reclaim and `-1` to leave `work/` to the ordinary 24-hour sweep.
A child copies any diagnostic log or diff worth keeping to `artifacts/` before it writes
`result.json`. The existing 24-hour task-root cleanup exempts `landing.state == pending`, because a
root that has not landed may still need the child's receipts.

Immediate cleanup happens inside `finalize`: success always goes immediately, and zero grace
reclaims every terminal outcome inside `finalize`. With a positive grace, other endings carry the
registry-internal `work_cleanup_at` deadline to the five-second beat, which advances a terminal
task for exactly as long as it still owes one of these directories.

**An isolated checkout's build output is reclaimed the same way, on its own deadline.**
`orchestrator_build_grace_minutes` is shaped exactly like the `work/` setting — default 60, range
`-1…1440`, `0` immediate, `-1` deferred — and it names `<worktree.cwd>/.build`, recorded as
`build_cleanup_at`. It is set only for a task that has a worktree of its own: a task working in a
shared tree must never be handed the `.build` of the checkout somebody else is using. It waits for
neither the 24-hour cutoff nor whole-worktree disposal, and a **pending landing does not exempt
it** — that was the gap this closed. Disposing a checkout requires `landing.state != pending`, so
five open landings on this machine were holding 814 MB of object files that no landing has ever
needed: what a landing needs is the source and the delivery branch, and both are left untouched.
Removal is `.build` and nothing else; a directory already absent settles the deadline, a refusal
keeps it for the next beat.

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
enough to pick an owner, and nothing off the session's screen. A session resolves its *own* id by
sending its exact process-bound conversation id to `GET /v1/orchestrator/whoami`; the broker returns
the currently bound terminal-neutral id from the live registry. The UUID cached in
`$ITERM_SESSION_ID` can survive an iTerm restart/resume and name a terminal which no longer exists,
so it is a hint, never identity. `GET /v1/orchestrator/waits` names only ids already inside a wait,
which is no help to the first session that needs to wait on somebody.

If the provider conversation id genuinely cannot be established, the authenticated session index
is the explicit fallback for terminal-addressed operations: select the live assistant row there,
never substitute `$ITERM_SESSION_ID`. It does not create a conversation identity and therefore
cannot be used as `root.session_id`; an interactive Root without a proved conversation owner stops
rather than silently switching to detached automation.

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

### The heavy-compile slot is locked, not agreed

Two force-reboots on 2026-09-03 came from four `swift-frontend` processes on a 24 GB Mac holding
lifetime-max footprints of about 46, 45, 27 and 8 GB. Several sessions share one checkout and each
ran `./test.sh`, whose one `swiftc` call compiles 134,863 lines. The stopgap was a gentleman's
agreement to create `/tmp/clawdline-suite.lock` by hand; queueing is now the system's behaviour
instead of a convention people remember.

**One primitive and no second one.** The lock directory is the truth: holding it is what `mkdir`
says it is, atomic and kernel-backed, and correct for a contributor who runs the scripts with no
Clawdline at all. Inside it:

```text
/tmp/clawdline-suite.lock/          (path overridable)
  holder.txt
    holder=      a readable identity carrying a terminal id, so a blocked session knows who to ask
    pid=         the process doing the work, never a sentinel; refreshed at each beat when the
                 work is a sequence of processes
    phase=       compiling | analysing | idle-holding, refreshed at each beat
    heartbeat=   the beat file's path
    started=     when it began
    tree=        the commit
    log=         where its output goes
    done_flag=   a path the run creates when its work has finished
  beat           the heartbeat file itself, inside the lock so `rmdir` takes it too
```

`note=` and `work=` are additive and optional; the full eighteen-field contract every writer
shares is written out above `clawdline_suite_lock_write_record` in `test.sh`.

**A broker lease sat in front of this directory for one day and was removed.** It added a durable
registry, a FIFO queue with its depth and position, and the reconciliation state after an app
restart — real things a directory cannot hold, and none of them needed on the night two sessions
collided. `Sources/OrchestratorLease.swift`, the five `/v1/orchestrator/leases` routes, the
Bearings block and the session overlay went on 2026-09-03;
[`docs/machine-resource-scheduling.md`](machine-resource-scheduling.md) records what was measured
and why the half that was not used came out. **There is still no second lock**, because a second
authority that can disagree with the directory is a race rather than a safety net.

**Liveness is proved by heartbeat, not by a pid existing.** This is the rule the first
implementation of the stopgap got wrong, in both directions from one cause: a holder recorded
`pid=72929` and that pid was a `sleep 14400` sentinel adopted by launchd. A sentinel outlives the
work, so under a pid-existence rule the lock becomes a four-hour roadblock nobody can clear; and
the obvious patch — "no `swift-frontend` running means stale" — makes the lock reclaimable in the
gaps *between* the compiles of one study, which is the collision back again. Both are the same
cause: the liveness signal was bound to a proxy process instead of to the work.

So the holder beats while it is working, and a `sleep` cannot beat. **A clock on the work is
wrong** — a four-hour compile is not stale, and a duration timeout on the work was withdrawn for
exactly that reason — **a clock on the proof of life is right**, because a holder beating every
twenty seconds never trips a sixty-second threshold however long the work runs. Measured on this
machine, a 30-second sampler drifted at most 1 second over 56 samples in an hour, including a
sample taken with 0.06 GB free and a compiler running; that sample came from a small-footprint
shell path rather than from a 20 GB compile's supervisor, so it is why the threshold is 60 seconds
and not 90, not a proof that a supervisor never stalls.

**The heartbeat must come from something that stops when the work stops**, or it is the same bug
in a new coat:

```bash
while true; do : > beat; sleep 60; done &     # a sentinel — it beats after the work has died

swiftc … & compiler=$!                        # a supervisor loop — the beat stops with the work
while kill -0 "$compiler" 2>/dev/null; do : > beat; sleep 20; done
wait "$compiler"
```

What is being proved is that somebody is still supervising this work, not that a timer is still
running on this machine. `build.sh` is written that way, and the child briefing hands out that
shape rather than the other one. `kill -0` sends no signal; nothing in this delivery signals a
process it did not start.

Admission needs both halves and the physical backstop is never waived: **(A)** the holder's
heartbeat has lapsed, **and (B)** no `swift-frontend` exists anywhere on the machine.

**heartbeat is what the holder says; `swift-frontend` is what the machine is doing. A
machine-level resource guard fails closed on the fact, not on the self-report.** (B) is a
necessary condition, never a sufficient one. Used alone it collides in the gaps between the
compiles of one study; dropped altogether, a holder that is wedged or heavily swapped out can miss
a beat *while still burning 46 GB* — which is precisely what this machine was doing at 01:24 and
01:45 — and taking over on a lapsed heartbeat alone starts a second driver beside it. On a healthy
machine (B) passes instantly at no cost; it blocks only when the holder is stuck, which is the one
case it exists for. The scan matches the executable's own name, `pgrep -x` semantics rather than
`pgrep -f`, so a sampler or a `/usr/bin/time swift-frontend …` wrapper is correctly not a compiler.
It counts **globally**, including compilers nobody in the queue started: the question is whether
anything on this machine is burning, not whether the holder's own compiler is running, and
`test.sh` demonstrably produces a driver outside its own `swiftc` line.

Evidence that is missing, stale or ambiguous reads `unknown` and blocks; it never reads "dead".
**Nothing kills or suspends anything**: a refusal names the holder, the orphan pids and the
evidence, and a person decides.

**`phase` is observable and reportable, and is never a takeover condition.** `idle-holding` means
"I still need the slot and nothing is running right now", which from outside is the same picture
as a holder that finished and forgot to let go. At 02:45 that picture — held 36 minutes, zero
compilers, no `done_flag`, sentinel pid alive, holder actually alive and writing a report — had to
be resolved by a person reading a `work_state` by hand, because the query could not say it. So the
query now says who holds it, how long since it was last `compiling`, and how old the heartbeat is,
and it still refuses to decide: the waiter learns **who to ask**, not what to seize.

**Exclusion and admission are two questions and only the first one is implemented.** Exclusion is
whose turn it is, and it fails closed: no lock, no compile. Admission — whether the holder may
start now and *at what size* — was the broker's half and went with it; what remains of it is the
ceiling. **The ceiling is a ceiling, not a throttle**: its job is to stop somebody passing `-j 8`
and multiplying the peak by eight, not to make a compile that will not fit fit. `swiftc` with no
`-j` was measured on this Mac to run one frontend already, so `-j 1` asks for what already
happens; a second reading saw two concurrent frontends during a full compile, reconcilable with the
first only if one was the stray driver `node Tests/keychain-rebuild-focused.mjs` starts outside
`test.sh`'s own `swiftc` line. What the ceiling is relied on for is the direction that is certain:
it can only lower the number of concurrent frontends, never raise it. `build.sh` and `test.sh` both
read it from `CLAWDLINE_SUITE_JOBS` and both print where the number came from.

The evidence behind the policy, and the failure list it has to survive, are in
[`docs/machine-resource-scheduling.md`](machine-resource-scheduling.md) rather than repeated here.
The reason the distinction is load-bearing is that mutual exclusion alone cannot fix this
machine. Measured at 02:16 with nothing compiling: 10.78 GB anonymous, 3.54 GB wired, 1.24 GB
compressor, 2.04 GB free, and `vm.swapusage` at 10,870 MB used with 1,417 MB free. There are
states on that baseline in which no suite can run at all. Two readings that look like evidence
are deliberately not used: summed RSS across processes double-counts every shared page — 622
processes summed to 22.33 GB on a 24 GB machine while `memory_pressure` reported 81% free — and
physical free percentage looks excellent right after Jetsam kills something. Swap free alone is
not a budget either: over forty minutes with nothing compiling, `vm.swapusage` total went 9,216 →
10,240 → 12,288 MB and free swung from 353 MB to 1,417 MB. **A fixed floor on a quantity whose
denominator moves is a deadlock wearing a threshold's clothes** — which is why the pressure gate
that would have carried one was never relied on, and why nothing that remains here refuses on a
number nobody has taken.

**Nothing in Clawdline projects the slot.** `GET /v1/sessions` carried a `lease` overlay and
Bearings a `heavy_compile_lease` block; both went with the broker lease. Who is holding the slot is
in `/tmp/clawdline-suite.lock/holder.txt`, which is what a waiting run prints, and asking that run
is what a waiter is told to do.

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

An absent store is written only from a complete SessionWatch inventory carrying its accepted-scan
time. Stale, missing, untimestamped or clock-inconsistent evidence returns
`409 coordinator_liveness_unknown` before candidate absence is considered, so the shape of a stale
cache cannot manufacture `session_not_found`. This freshness gate applies only to construction:
under the same lock an existing exact binding stays idempotent and every different identity stays
`coordinator_exists`, without rewriting the durable owner.

Restart continuity is identity continuity, not conversation resurrection. After the app reloads,
the durable record projects `session.coordinator` only when all private binding facts still match
the one current process. That exact row receives the web renderer's closed record—`label`, online
`status`, and `commands`—on both Session JSON surfaces. Every ordinary row is unchanged. A missing
or reused process absent from a complete timestamped inventory leaves the durable coordinator
`offline` and decorates no row; degraded evidence instead reports `unknown`. The role never
migrates to a matching label or the most recently active assistant.

`GET /v1/orchestrator/coordinator`, also orchestrator-token-only, returns safe durable presence and
**Bearings**: one deterministic snapshot over existing Session metadata, active task records,
pending landing records and open coordination-wait groups. It always counts the eight closed
`work_state` values and names safe metadata for `unknown`, human/peer `waiting`, and owners
`blocking` peers. Beside that tally it carries a second, independent one — `closeability_counts`
over the four closed [closeability](session-closeability.md) values plus `not_projected` — and
each named session row carries its own `closeability_state`. The two tallies are never derived from each
other: a row can be `ready` and not closeable, or quiet and closeable, and one number read as the
other is exactly the collapse that projection exists to undo. A session whose closeability was not
projected is counted under `not_projected` rather than folded into `unknown`, because absent and
doubtful are different facts. It also carries the ordered `pending_landings` rows exposed by the landing GET,
so the count and each owner/executor status share one task/landing registry observation. Named
lists are independent filters and may honestly overlap when a session is
both owner and waiter. RemoteServer takes one bounded SessionWatch observation and then one Orchestrator
snapshot; every Orchestrator-derived row flag plus task, landing and open-wait total is computed in
that single registry lock window. SessionWatch and the registry are not cross-source atomic. Each
source therefore carries its own observation time, provenance and freshness; an incomplete Session
inventory is `stale`, never silently complete. If the main queue is unavailable, a cached inventory
is explicitly stale (or missing before the first successful read), landing rows remain present and
ownership is `unknown`. The device projector allowlists each pending row, ownership record,
evidence source and evidence field independently; it never copies an opaque nested dictionary.
Coordinator liveness follows the same boundary: a complete timestamped observation may say exact
`online` or, when no older than the binding, exact `offline`; stale, missing, untimestamped or
pre-binding evidence says `status:unknown, lifecycle:unknown` and cannot authorize reconnect.
The route returns only the
opaque coordinator UUID and terminal-neutral session id, assistant, cwd/label/work-state. It never
returns transcript text/path, assistant conversation id, tty, pid or process start.

Four commands—status report, duplicate/conflict/ownership inspection, landing closure and
scope/permissions—are connected read-only views over the device-safe Bearings projection.
`deep_status_audit` is the one connected user-attributed send: only while the exact Clawdfather is
online and the current browser has write/send capability, a first press displays a high-token,
multi-session preview and a second explicit press sends one stable instruction through the existing
Session `/send` route. Since-away, cross-session coordination judgement, ask/quiet-watch, dispatch,
stop and reconnect remain explicitly disabled with their specific reason codes. There is still no
device dispatch, task/landing/wait mutation, autonomous close, Build, or machine-token grant.

Every command row also carries closed `token_effort` and `token_effort_basis` fields. The badge is
an expected relative Token workload (`low|medium|high|unknown`), not actual usage and never dollars;
it remains visible on disabled rows and does not imply availability. Reads are low, a single
Session question is medium, spawning or multi-Session fan-out is high, and work whose unbuilt shape
is not known stays unknown. A missing or invented effort value is rendered unknown rather than
quietly cheap.

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
old tuple is present in that current timestamped scan, `coordinator_online` refuses live takeover.
If the scan is incomplete or untimestamped,
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

### Clawdfather succession: Handoff plus durable lifecycle receipts

The Clawdfather-specific succession routes compose Phase A2 with ordinary Handoff without changing
either primitive's meaning. An ordinary Handoff still opens an independent root and proves only
that its canonical first line reached that root. The separate succession ledger records the
ownership observations and side-effect boundaries that Handoff deliberately does not: accepted,
receiver open requested/opened, package delivered, sender observed, receiver pickup, sender drain,
sender close requested, old exact binding offline, rebind committed, receiver online and receiver
completion observed.

Each transition is persisted under its own machine-local `flock` before the next irreversible
effect. A duplicate request id is either the same durable operation or a typed conflict. A restart
after an open request cannot open another receiver without a terminal receipt; a restart after a
successful terminal close still waits for fresh exact-offline evidence; and a restart after the
Coordinator compare-and-swap may reconcile the new exact online binding only when the offline
receipt already exists. Store failure is never treated as progress. Sender close consumes the
same versioned closeability projection as the close API, with no lost descendants or obligations,
and recomputes it immediately before ending the terminal.

This automation is intentionally narrower than Handoff. It is machine-token-only, operates on the
one configured stable Coordinator UUID and expected generation, and never elects from labels,
recency or transcript prose. It is not a general transfer-of-ownership switch and does not make a
receiver's first-line `picked_up` receipt mean understanding or completion. The caller still writes
the named obligations, REFERENCES, VERIFICATION and OPEN THREADS into `handoff.md`; the receiver
still answers those questions; its final acknowledgement is a distinct succession receipt. The
closed request bodies, retry rules, public record and typed refusals are in
[`docs/api.md`](api.md#coordinator-succession).

**The web app's new-Session *Name the new session Clawdfather* choice is not another coordinator
route.** None of the coordinator lifecycle endpoints above became reachable from a paired device,
and none of them types into anything. When the creation sheet opens it reads the device-safe
Bearings projection.
Only the exact closed state `registration.state === "available"` enables the choice. `configured`
closes it for every configured durable owner (`online`, `offline` or `unknown`); `blocked`, a missing field, or an unknown
value also closes it rather than guessing that the store is empty. That projection contains no
machine credential or durable compare-and-swap fields.

After the new tab appears in `/v1/sessions`, the page waits until its `assistant` field proves that
Claude or Codex is ready rather than typing a paragraph into the newborn shell. It then reads
Bearings once more to close the start-up race and sends one instruction over the existing
`POST /v1/sessions/:id/send`, which the page could already reach. The session that receives it is a
local process running as the owner of this Mac, so it reads the orchestrator token and performs the
register itself — the same act as a person typing the curl by hand, and the same trust boundary
every Clawdline dispatch already stands on. The browser therefore never holds the machine
credential and cannot take over a configured coordinator. What it *does* know is the target's
terminal-neutral id, because that is the `id` on the Session row it drew, so it hands that over in
the instruction. Whether the role was taken is read back through the authenticated
`session.coordinator` projection: the exact bound row and its SessionChat header wear the crown.
There is no Clawdfather assignment item in an existing Session's action menu. The recipe the new
Session is asked to follow is the next section.

### Deep status audit: the agent-driven first slice

The Clawdfather panel's **Deep status audit** item is another narrow use of the same ordinary
user-attributed Session send. It does not start a broker audit run. Its stable instruction tells
Clawdfather to first snapshot sessions, tasks, landings and waits; contact every relevant idle/root
Session; require exactly four separate reply sections—unfinished work with owner, blocker and one
next action; completed but not landed; landed with commit/target evidence; and user decisions—and
wait with a bounded deadline. Clawdfather then rereads all registries, compares the same
task/Session/commit across surfaces, and verifies Git ancestry only when a delivery commit exists.

The report must distinguish `unreachable`, `timeout`, `stale snapshot`, `contradiction`,
`missing delivery commit`, and `already-integrated-but-unclosed`. It must not auto-dispatch,
auto-land, auto-close, start technical work, or accept a title, path, or commit message as proof.
The `/send` receipt proves only that this instruction reached the terminal handoff. Persistent run
records, broker-owned probes and durable timeouts belong to a future protocol and are not shipped
by this slice.

### Becoming Clawdfather: the recipe a session runs on itself

There is no route by which anything but a local process holding the orchestrator token can register
the machine coordinator, and there is deliberately not going to be one. So "make that session
Clawdfather" is never carried out *for* a session; it is carried out *by* it. A person types the
curl, or the web app's new-Session **Name the new session Clawdfather** choice types a
registration-only instruction into the newly created session through the ordinary
`POST /v1/sessions/:id/send`; the session does exactly what is written below. This section is the
authoritative copy of it, so that a session which has just been asked has something to follow
rather than Swift source to reverse-engineer.

For the web creation helper, **registration-only** is a hard boundary: after step 2 it follows
step 3 only when the Mac says `registration.state` is `available`. If any record is configured,
whether online, offline or unknown, it reports that owner and stops. Steps 4 and 5 remain a manual local
repair procedure; the
creation sheet never asks a new Session to rebind or replace an offline owner.

**And it never asks over a store it cannot read.** `coordinator.configured` is `false` for an
absent record, a corrupt one and one from an unknown version alike, so a browser gating on that
field alone would type the instruction into a session and learn from the resulting
`409 coordinator_store_invalid` — in that session's transcript, where the person who pressed the
switch never sees it. `registration.state` (`available` / `configured` / `blocked`, documented
under `GET /v1/orchestrator/coordinator/bearings` in `docs/api.md`) is the field the switch reads,
and `blocked` is refused in the browser before anything is sent. A session that arrives here on
its own should read it the same way: a store you cannot parse is not an unregistered machine.

**The two local facts.** The token is `~/.config/clawdline/orchestrator-token`, mode `0600` and
readable only by a process running as its owner; the port is `remote_port` in
`~/.config/clawdline/config.json`, and `7717` when that file says nothing.

```bash
ORCH=$(cat ~/.config/clawdline/orchestrator-token)
PORT=$(python3 -c 'import json,os;print(json.load(open(os.path.expanduser("~/.config/clawdline/config.json"))).get("remote_port",7717))' 2>/dev/null || echo 7717)
```

**1. Your own terminal-neutral id.** It is not your conversation id and not your transcript's name.
It is the id of the *terminal pane currently bound to this conversation* — the value
`GET /v1/sessions` and `GET /v1/orchestrator/sessions` both call `id`. Resolve it from the exact
process-bound conversation id:

```bash
CONVERSATION_ID='<this assistant process-bound conversation id>'
SESSION_ID=$(curl -fsSG "http://127.0.0.1:$PORT/v1/orchestrator/whoami" \
  -H "X-Clawdline-Orchestrator: $ORCH" \
  --data-urlencode "conversation_id=$CONVERSATION_ID" | jq -er .terminal_id)
```

For Codex, `CODEX_THREAD_ID` (or compatible `CODEX_SESSION_ID`) is the conversation input to that
request, not its terminal result. Claude obtains its transcript UUID through the nonce procedure in
the Clawdline skill. The response's provenance says `live_session_registry` and
`single_snapshot_revalidated`; behind it are two uncached process-evidence passes, each shared by
every row in the frozen SessionWatch target population. A detach or rebind during resolution
returns a typed retry instead of an id. Never substitute `$ITERM_SESSION_ID`: measured after
restart/resume, that cached value can name a terminal absent from the live registry. Under tmux the
same route works unchanged. If the conversation id itself cannot be established, read the
authenticated `GET /v1/orchestrator/sessions` index and deliberately select a row only for a
terminal-addressed operation; labels/cwd/state are not proof for `root.session_id`.

**2. Read the current state before deciding anything.** One request answers all three cases, and
the two fields a reconnect needs come only from here:

```console
$ curl -s "http://127.0.0.1:$PORT/v1/orchestrator/coordinator" \
    -H "X-Clawdline-Orchestrator: $ORCH"
{"version":1,"observed_at":1787884000,"store":{"status":"ready"},
 "coordinator":{"configured":true,"id":"5ac9c093-f483-4606-87eb-2278b34436fe",
   "scope":"machine","label":"Clawdfather","registered_at":1787821469,"generation":3,
   "rebound_at":1787882803,"status":"online","lifecycle":"standby",
   "session":{"id":"509F54A8-356E-420D-9EAC-73D676C9580E","assistant":"claude",
              "label":"Clawdfather 新增介面","cwd":"/Users/you/code/clawdline",
              "work_state":"unknown"}},
 "bearings":{…}}
```

`coordinator.configured` is `false` when nobody has ever registered. When it is `true`, a complete
timestamped inventory reports `coordinator.status` as `online` for the exact bound process or
`offline` only when a scan no older than that binding proves it absent. `unknown` means the
inventory is stale, missing, untimestamped or predates the binding: wait and repeat this read; do
not claim the process is offline or attempt reconnect. `coordinator.id` and
`coordinator.generation` are the pair a later exact-offline reconnect must quote back.

**3. Nobody is configured — register.** One field, and it is your id from step 1:

```console
$ curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/coordinator/register" \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d "{\"session_id\":\"$SESSION_ID\"}"
{"ok":true,"created":true,"coordinator":{…}}
```

`created:false` with a `200` means you were already it and nothing changed.

**4. Configured and exactly `offline` — reconnect.** Registration is never a takeover, so reconnecting is a
separate, guarded operation, and its three fields are closed: the `id` and `generation` you just
read, plus your own id.

```console
$ curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/coordinator/rebind" \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d "{\"expected_coordinator_id\":\"$COORD_ID\",\"expected_generation\":$GENERATION,\
         \"session_id\":\"$SESSION_ID\"}"
{"ok":true,"rebound":true,"coordinator":{…}}
```

The stable UUID survives a reconnect on purpose, so `expected_generation` is the compare-and-swap
value, not the UUID. Both are checked under the store's `flock`; a mismatch means the record moved
after you read it, and the answer is to go back to step 2 rather than to retry with the old numbers.

**5. Configured and `online` on a different session — stop.** Do not attempt it. The API refuses
this by design — `409 coordinator_exists` from register, `409 coordinator_online` from rebind — and
the refusal is the point rather than an obstacle: a live coordinator is somebody's running work, and
there is no unconditional replace, stop or delete operation anywhere in this protocol. Report which
session holds the role (`coordinator.session.id`, `label`, `cwd`) and leave it alone.

**The refusals worth recognising.** `403 forbidden` means the request reached the handler without
the orchestrator token — a paired device or a task secret, neither of which may do this.
`404 session_not_found` almost always means step 1 sent a conversation or rollout id instead of a
terminal id. `409 session_unbound` means the row exists but its process-bound identity could not be
completed, so nothing was decided. `409 coordinator_liveness_unknown` means the Session scan is
stale or older than the current binding, and absence of evidence is refused as proof of death; wait
for a fresh scan and read step 2 again. `409 coordinator_store_invalid` and
`500 coordinator_store_failed` are about the durable record itself and are never fixed by retrying
harder.

**Say what happened.** Registration is machine-wide state, and the person who asked for it is
usually watching a different window. Whether you registered, reconnected, or found somebody else
online and left them there, that sentence is the deliverable.

### Session work-state projection: one answer, separate evidence axes

Every live Session row carries exactly one closed `work_state`: `ready`, `working`, `holding`,
`waiting_you`, `waiting_session`, `unknown`, `milestone_complete`, or `work_complete` — plus the
independent `owed` overlay, the second axis. **The per-state contract — what a person should do
on seeing each state, the icons, the declaration route, and the holding/waiting_session
boundary — is [`docs/session-states.md`](session-states.md); that page governs where the two
disagree.** (`waiting_you` was `waiting_human`; `unknown` was `needs_triage`, renamed because the
fail-closed default means "the broker has no positive evidence" — an absence, never the reader's
to-do.)

This is not another free-form truth store. The broker deterministically projects it from the
terminal presence reading, the task registry's authenticated result, a root's process-bound
session-delivery receipt, the matching landing record, durable handoff state, coordination
waits, and the session's own bounded self-declaration (`ready`/`holding` claims and the `owed`
debt, `POST /v1/orchestrator/sessions/:id/state`, provenance `self`). Those sources remain
separate, and top-level terminal `state` is unchanged. A missing or unknown projected value
fails closed in the web client as `unknown`, never as blank idle and never as a check.

The precedence is `waiting_you` (terminal question) > waiting-on/owed coordination file wait >
unreadable or missing evidence (`unknown`) > current `working` > an idle dispatcher's live
child (`waiting_session`) > the finished task receipt > delivered milestone > the self claim >
`ready` for an assistant-free prompt. Current work
intentionally outranks an older receipt: during the
child's linger somebody can resume using the terminal, and the earlier assignment's success cannot claim
that new activity is finished. `waiting_you` remains the only state that requests a person's
attention or drives the loud row/push. `waiting_session` stays the quiet `⏳` relationship, and
`holding` has exactly one entrance — a self claim carrying a declared next step and a non-person
mover — so it can never become the new default exit.

An active child is already typed broker evidence that its dispatcher has an outstanding Session
obligation. When that exact root process is idle, the projection is therefore `waiting_session`,
not `unknown` and deliberately not `holding` — a child can wedge, so waiting on one is waiting
on a session; when it works in parallel, current `working` wins. The browser validates the
same fact against the live task's resolved `root.terminalId` and names the child task beside `⏳`.
The child finishing removes this wait evidence; it does not itself mark the root delivered.

A successful task with `finishedAt` is `milestone_complete` (one check) only when the task receipt
is bound to the process occupying the Session now: exact assistant, terminal and tty, pid plus
process start, process-bound rollout/conversation id, and the transcript's task-marker proof must
all agree. A terminal id reused by a later ordinary or assistant process cannot borrow the settled
task. Missing legacy identity fields fail closed. An open handoff's `from_session` is compared in
two strict namespaces—exact terminal id and exact process-bound conversation id—with no prefix,
title, tty, or fallback guessing.

An ordinary root has a second, deliberately narrow route to that same one-check milestone:
`POST /v1/orchestrator/sessions/:terminal-id/complete` while its current turn is observably
working. The broker resolves and stores the same exact process tuple itself; the body supplies only
a bounded summary. Its disposition is `scope:session` with
`evidence:authenticated_session_delivery`. The first subsequent idle settles the receipt, and the
same terminal's next working or waiting transition consumes it. Thus it says only “this root
delivered the turn now awaiting approval,” survives an app restart, and cannot be borrowed by a
reused process or reappear after newer unreported work.

That one check is authenticated, durable reported evidence that the current assignment/phase
delivered; review, landing, handoff, waits, or later graph nodes may remain. It becomes
`work_complete` (two checks) only when the same task also has the new machine-authenticated,
git-verified target landing fields above and the terminal has no unresolved coordination wait or
handoff. Legacy landed rows without those fields remain a milestone. Neither unstructured
assistant prose, progress notes nor Clawdfather advisory can write either check. Clawdfather
explains which receipt
is missing, coordinates its owner, and prioritizes `unknown` rows; it is not a status-truth writer.

The existing task result remains a child's typed, durable Session report: `success` maps to
delivered milestone evidence, while `failure`, `timeout`, cancellation, or a missing finish receipt
map to triage rather than completion. Natural-language `/progress` notes remain display-only
context. A child may intend that all work is complete, but that intent is still only its `success`
receipt; it cannot call the root route or directly produce `work_complete`.

Completion metadata therefore has two narrow scopes. `scope:task` names an authenticated child
delivery and is the only scope that can advance to a broker-verified target landing.
`scope:session` names one root-reported turn and is consumed on the next turn; it can never advance
to two checks. The registry still does not model one authoritative set of every descendant,
review, landing, and handoff obligation belonging to a human root's whole graph. Claiming that
broader completion would be invented global truth, so neither scope calls a root graph complete.
The typed double-check evidence remains `broker_verified_target_landing`, not “task closure”: the
broker verified local git containment, not the root's complete test/review graph. `ready` is
likewise never inferred for an idle assistant: without positive evidence, its stopped state is
`unknown` — an absence that asks nothing of the reader. The positive evidence can now also be the
session's own authenticated declaration (`POST /v1/orchestrator/sessions/:id/state`, provenance
`self`, docs/session-states.md), which is how an idle assistant honestly reaches `ready`. Plain
non-assistant prompts can be `ready` because their absence of an assistant assignment
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

When a task finalizes, its terminal outcome and a completion outbox envelope are committed in the
same atomic `orchestrator.json` replacement **before** terminal automation begins. The envelope has
a stable `notice_id`, delivery state, attempt count, typed last error and bounded `next_retry_at`.
A serial utility queue then resolves the root and types one compact semantic message; iTerm
automation never holds the RemoteServer queue, main thread or SSE broadcaster. On the wire that
message is one exact line: a wrapper around one JSON object with no LF or CR bytes:

```
<clawdline-notice>{"ack_path":"/v1/orchestrator/tasks/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/completion/ack","audience":"root","body":"[clawdline] task 3f9a21bc (Project portrait) finished: success — read /tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/result.json — after observing, ACK notice 6b1d46cb-… at /v1/orchestrator/tasks/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/completion/ack","child_may_still_write":false,"claims_released":false,"kind":"task_finished","notice_id":"6b1d46cb-1111-4222-8333-444444444444","outstanding":0,"protocol":"clawdline.notice","result_path":"/tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234/result.json","state":"success","task":{"id":"3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234","title":"Project portrait"},"version":2}</clawdline-notice>
```

`protocol` and `version` identify the envelope. Version 1 remains a closed legacy schema with two
`kind` values, `task_finished` and `workspace_overlap`; readers continue to accept literal version-1
rows already stored in transcripts. New writers emit version 2, whose closed set is those two plus
`file_wait_request`, `file_wait_release`, and `handoff_receipt`. A completion has a closed terminal
`state`, its task, audience, result path, outstanding-child count, the two timeout/claim flags,
`notice_id`, and `ack_path`. Readers also accept the original version-2 completion shape without
the last two fields so transcript history remains readable, but only the current shape is
idempotently ACKable. An
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

Delivery and consumption are intentionally different. `root_missing`, `conversation_ambiguous`,
`root_choosing`, `iterm_modal`, `terminal_timeout`, `identity_stale`, and other transport failures retry with capped
exponential delay; a transport success also retries until the root explicitly ACKs the same
`notice_id`. The stable id makes duplicate terminal lines one consumption, and ACK is idempotent.
Eight delivery attempts is the bound; exhaustion becomes `dead_letter`, visible through
`GET /v1/orchestrator/completions`, and a machine-authenticated reconciliation may rearm it.
For a grandchild, the parent terminal is eligible only when terminal id, assistant, tty, PID,
process start, transcript-marker proof and conversation id all match the parent Task. Reuse of the
same terminal/TTY by another same-assistant process is `identity_stale` and sends no bytes.

`accepted_at`, `executed_at`, `result_verified_at`, `transport_delivered_at`, `observed_at`, and
`acknowledged_at` are independent ledger facts. An HTTP response, SSE frame or successful Apple
Event never fills `observed_at`; the explicit ACK fills observation and acknowledgement together.
If persisting that ACK fails, rollback compare-and-swaps only the matching completion-delivery
transition; concurrently refreshed worktree, landing, close and other Task fields are preserved.
The task/result GET routes remain the fallback. Startup and the reconciliation API create at most
25 outboxes per pass for identifiable terminal tasks from the last seven days; null-root and older
history remain poll-only. Historical root ids are never rewritten. A Coordinator rebind may route
an old conversation to its current one only through the persisted process-bound alias interval.
The task's historical `root.assistant` authenticates that alias; delivery then selects the current
binding's conversation id **and assistant**, so a proved Codex-to-Claude or Claude-to-Codex role
move cannot send to the stale assistant process.
The machine ledger accepts only no query or exactly one `pending=true|1|false|0`; `1` is exactly
true and `0` exactly false. Repeated `pending`, unknown queries, malformed/non-object bodies, extra
keys and wrong types are typed `400 bad_request`. Reconciliation requires one JSON object
containing only optional string `task_id` and JSON-boolean `include_dead_letter`.

This is separate from a live session reporting to another live session. That uses machine-token
`POST /v1/orchestrator/messages` and transcript role `message`; App and Web name the resolved
source instead of drawing the words as the person's user turn. Text-only reports retain the strict
version-1 `<clawdline-message>` envelope. An optional closed list of local image paths advances
only that message to version 2: Clawdline bounds, decodes and re-encodes each file into its owned
expiring cache, while the envelope and transcript carry only opaque id, PNG type, byte count,
dimensions and absolute expiry. Authenticated clients read bytes from
`GET /v1/artifacts/images/:id`; expiry/deletion is `410 artifact_expired`, not the
`404 artifact_not_found` used for unknown ids. No URL, source path or base64 enters a transcript.
The closed inventory, including which first lines intentionally remain ordinary `user` prompts,
is [`messages.md`](messages.md).

`orchestrator_notify_root` turns it off for anybody who would rather poll. Every task change also
goes out on [the event stream](api.md#the-event-stream) as an `orchestrator` frame, which is how the
web interface and anything else watching finds out without asking.

### Cleanup

At start and every six hours: task directories for terminal tasks that finished more than 24 hours
ago are removed; terminal handoff envelopes and packages are removed 24 hours after their `created`
time. The registry keeps its most recent 200 task records. **Artifacts are in `/tmp` and
they are not yours to keep** — if a child produced something worth having, copy it out. The
directory going away after a day is the same promise `/tmp` always made, made explicitly.

Heavyweight `work/` storage has a shorter, separate life. It is removed during a successful
finalize, or when the non-success grace deadline expires; `artifacts/`, `task.json`, `CHILD.md` and
`result.json` remain untouched until the whole task-root sweep above. Reclaiming a missing `work/`
is success, and a filesystem refusal never delays or reverses the terminal task state.

Session-message image artifacts are not task artifacts and do not live under `/tmp/.clawdline`.
Their Clawdline-owned cache has its own deterministic bounds: 24-hour TTL, 64 live files, 64 MiB
total, six per message, bounded input/decoded size and bounded tombstone metadata. Each write and
read prunes only opaque-id files inside that store. `CLAWDLINE_SESSION_IMAGE_DIR` moves the entire
deleting store for test isolation; it never changes which source paths a request is allowed to
name.

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

## The usage ledger

**What it cost above is a fact about one task record, and that record is about to be deleted.**
The registry keeps 200 rows and evicts the oldest; a task directory is swept 24 hours after the
task finishes. So "what did last month cost, by model" was answerable exactly once, by a one-off
export and a session dedicated to producing it, and could not be repeated. Every day without a
durable copy is evidence that cannot be recovered later.

The ledger is that copy. It lives in
`~/Library/Application Support/Clawdline/Observability/usage.sqlite3` — `0600` in a `0700`
directory, WAL, schema migrations owned by the app — deliberately away from `orchestrator.json`,
from `/tmp/.clawdline` and from the transcript archive, so neither the 200-row cap nor the
24-hour sweep can reach it. About 2 KB a row, which is roughly 115 MB a year at this machine's
pace, so detail is kept and nothing is deleted to save space.

**Two version numbers, and they are not interchangeable.** `schema_version` is part of every
interval key, so bumping it to add a column would orphan every row ever written from the key its
own collector computes next time; the store's `user_version` is the migration counter, and adding
a column, an index or a column *name* moves that one alone. Each migration statement is
independent of the ones before it, so a crash part-way through leaves the next launch re-running a
statement that fails harmlessly and then finishing — which is deliberate rather than lucky, and is
covered by a test that builds a version-1 store rather than starting, as every other test does,
from an empty file that runs the creation path instead of the upgrade.

The legacy forensic aggregate and CSV remain on `GET /v1/orchestrator/usage` and `.csv`. Read the
privacy-safe public contract through
[`GET /v1/orchestrator/usage/analytics`](api.md#get-v1orchestratorusageanalytics-analyticscsv-analyticsjson),
its safe `.csv` and its lossless public `.json`. The web app's **Usage** button provides Overview and Agent
Work over that same contract: local-day range and timezone, breakdown and exclusive token trend,
cost series separated by unit+basis, coverage/correction panels and paginated drill-down. Every
visual has a table or textual summary, and the contract sends no prompt, session id or raw path.

### One row

A row is **one assistant session's usage delta inside one attributable work boundary** — not a
task, and not a whole standing session that never closes. Its identity is
`SHA256(assistant, session_id, boundary_kind, boundary_id, segment_no, schema_version)`, unique.

A new segment is cut when the work boundary changes (a task begins or ends inside a session), when
the model changes, at local midnight, and after a seal. That keeps a standing session's startup
cost in its first segment, so a follow-up attached to it is charged only its own increment, and it
makes per-month and per-model figures reproducible.

### Two collectors, one door

Both dispatched tasks and hand-opened sessions are recorded, so the surface can honestly claim to
be what Clawdline sees rather than only what it started.

- **Task finalize** hands the whole task record over as the task reaches a terminal state. This
  happens on **executed, not delivered**: it does not wait for, read, or depend on whether the
  root was successfully told, because "the task ended and this is its usage" and "somebody was
  notified" are two events with different failure modes.
- **`SessionWatch`** checkpoints every assistant session in the reading it already takes — the
  transcripts are on disk, so it costs no extra terminal round trip — at most once every five
  minutes per session, and seals a session's row when its process disappears.
- **Startup** imports whatever the registry still holds, up to its 200 rows. Running it again is
  free, so it runs on every launch rather than once and picks up whatever finished while the app
  was not running.

**"Free" is a promise with three parts, and it is the ledger's to keep rather than the caller's.**
`./build.sh` closes and reopens the app, so anything the backfill does twice it does once per
launch, for as long as that row exists.

- A record already attributed contributes a delta of zero, which is the cursor doing its job.
- **A record that has never had a session produces no row at all.** Nothing was observed — no
  transcript, no counters, not even empty ones — so a row would describe work that was
  *anticipated*, and the real row would join it the moment the task actually ran. It is not
  harmless to write one anyway: the queued half of the registry became permanent unmeasured rows,
  and five of those beside one genuinely unreadable session read as six coverage gaps, which
  buries the only real one.
- **An import whose content equals what is already recorded writes nothing**, corrections
  included. A correction is compared against both the sealed row's own object *and* the
  corrections already standing against it, because a sealed row's `usage_raw` is deliberately
  frozen — rewriting it would destroy the evidence of what was sealed — so a comparison against
  that column alone can never converge. Without the second half, four identical imports wrote
  four corrections and `corrections` became a count of launches rather than of disagreements.

**Neither collector can double-count the other.** Every measurement any of them takes is a
*session cumulative* — both transcript readers sum a file from the top, and a task's stored
`usage` is the whole session rather than that task's slice — so the store attributes the
difference against a per-session cursor and never the number it was handed. Reading the same
transcript twice attributes zero the second time; a task that ran inside a watched session is
counted once, by whichever collector reached it first.

### Five invariants, and why each one is there

- **Re-reading a source can never double-count.** The cursor above; the deterministic interval key
  and its unique constraint are the second belt, not the first.
- **A row's usage is attributable to one work boundary**, so per-month and per-model figures are
  reproducible.
- **A source that cannot be read is a state, never a zero and never a missing row.** Unknown token
  counts are SQL NULL, the row is sealed `source_missing`, and neither the route nor the export
  may render it as `0`. Rows with unknown usage sort to the top of every view — any part unknown,
  not only all four — because the sessions most likely to go missing are the long ones, which
  biases every total downward. A cumulative counter that goes **backwards** is the same kind of
  event: the reading is never subtracted, the cursor re-anchors to the replacement rather than
  measuring for ever against a high-water mark that source will not reach again, and the row is
  marked `source_regressed` so that every reader knows the number was measured across a seam.
- **Every number carries what kind of number it is.** Not one `costUsd` but `cost_value`,
  `cost_unit`, `cost_basis` and `price_snapshot_id`, and where there is no cost a `missing_reason`
  naming which kind of unavailable it is.
- **A row's coverage marks accumulate; they do not overwrite.** If two things are true about a
  row's coverage, both reach every reader. `coverage_reasons` is a **set** and every writer unions
  into it. It was one slot with a last writer, and the two facts that collide are reachable: a
  task filed under an invented identity whose transcript is then replaced, and a watched session
  that rotated and was unreadable by the time it closed. The mark that lost was always the earlier
  one — `source_regressed` — so the rows whose number was measured across a seam were exactly the
  rows that stopped saying so. This is the invariant above wearing the writer's clothes: a mark
  that never reaches a reader is a mark that was never made.

### Money, and the failure this exists to stop

31 of this machine's 165 rows with usage carry a cost; 134 do not, and the side without one is
`gpt-5.6` — 58% of every token here. **A cost is copied through as the recorded fact it is, and
never recomputed; where the source has none, none is invented** — not from a price table, not
`0`, not borrowed from another row of the same model.

The failure has already happened: a first snapshot summed the absent values as zero and produced
**1137M tokens, $0.00**, a month-end that looks entirely normal and is not a bug in any way a
reader could see.

`price_snapshot_id` is not protection against a historical month being re-priced — recorded costs
are recorded and do not move. It is so the rows that carry *no* cost can be priced later with a
permanent record of which rates were used, which is what makes that decision reversible.

### Spelling, and why the object is copied rather than picked apart

The registry on disk spells `cache_read` and `cost_usd`. The same values over HTTP spell
`cacheRead` and `costUsd`. Claude's transcript spells `cache_read_input_tokens`; Codex's rollout
spells `cached_input_tokens`. **A collector written against one spelling silently reads `0` from
another, and the field most likely to be dropped is the cache read — 96.6% of every token on this
machine.** So the source's object is stored whole beside the normalized parts, and the normalizer
reads every spelling; a key absent under all of them stays NULL.

Whether `input` already includes the cache read is decided by **arithmetic rather than by the
assistant's name**: Codex's cumulative `input_tokens` includes its cached input and its total is
`input + output`, Claude's does not and its total is the sum of four, so both readings are
computed and the one that reconciles with the source's own stated total wins.

**That decision is recorded, in `input_basis`, every time it is made** — including the benign case.
It reshapes the stored parts, and without the column the only way to tell "the cache was taken out
of input" from "input never included it" was to re-derive the arithmetic from `usage_raw`; a
determination nobody can audit later is the same shape as the unknowns this store exists to keep
visible. The words are `excludes_cache`, `includes_cache`, `readings_agree` (both readings are the
same number, which happens exactly when there is no cache read to move), `includes_cache_assumed`
(no stated total, decided on the assistant's known shape — the weakest of them, and it says so),
`unstated`, `unreconciled`, and `includes_cache_unreconciled`. The last exists because a Codex row
can be reshaped *and* fail to reconcile, and `parts_do_not_sum` on its own reads as "these parts
are wrong" rather than "they still do not sum after I removed the cache". `reconciliation` keeps
its own meaning — something did not add up — and stays NULL when everything did.

Checking any of this is **per row, on a fixed task id, never aggregate against aggregate**. The
same task's usage must match on all three surfaces — the registry file, the HTTP record, the
ledger. Comparing totals, or comparing "the first row with usage" from two places, is how a
wrong conclusion about these two spellings was reached and then corrected in one afternoon: an
unaligned comparison looks like an experiment and is not one.

### One seam where a row becomes a number

**Every reader of this store goes through `UsageLedger.Row.measurement`, and none of them reads
the token columns directly.** The aggregate, the wire payload and the CSV export all ask it — and
so do the two that used to be exceptions to that sentence, because "true today and nothing enforces
it" is the state this seam replaces. The range's **ordering** asks `Measurement.incomplete` rather
than re-deciding "incomplete" in an `ORDER BY`, so the day the seam widens what counts as
incomplete the sort cannot quietly disagree with it; and the `was` snapshot a correction records —
the object somebody compares a disputed month against — is taken through the seam like everything
else.

That is a structural answer to a defect that was found three times in one review, in three
different readers: the store marks a row — a part it never measured, a coverage reason, a session
identity it had to invent — and a reader turns the mark back into an ordinary number. An aggregate
that coalesced a NULL part to `0` and then dropped that row's *measured* tokens out of its own
total. A wire payload with no field a coverage reason could travel in, so `session_unresolved` and
`source_regressed` reached the route looking like healthy sessions. Three guards would have fixed
three symptoms; one seam is what stops the fourth reader from doing it again.

What that means for a number the route hands back:

- A part **no** row in the group measured is `null`, never `0`.
- A part **some** row could not measure is summed over the rows that did, and
  `tokenPartsUnknown` says which column is short and on how many rows.
- `total` is the sum of what was measured, so a row three-quarters known contributes its three
  quarters rather than vanishing; `tokenRowsUnknown` counts every row that could not measure
  *something*, not only the rows that measured nothing at all.
- `coverageReasons` carries the store's own words for why rows are marked. `coverage` says how
  much of a source was read; it says nothing about a session filed under an invented identity or
  a number measured across a replaced transcript.
- **A row can carry more than one of those marks, and it is counted under each.** So these counts
  can sum to more than `rows`, deliberately: they answer *how many rows carry this mark*, never
  *how many rows are marked*. In the CSV they arrive in one `coverage_reasons` field,
  space-separated — space rather than comma because the export is what a month gets audited from
  and an embedded comma is exactly what a hand-rolled splitter gets wrong.
- **`total` and the CSV's `measured` are the same quantity, and the CSV's `total` is not.** The
  route's `total` is the sum of what was measured; the export's `total` column is strict and
  empty the moment one part of a row is unknown. Adding up an export therefore has to use
  `measured`, which is why that column exists: with only the strict one, a range holding a single
  partly measured row could no longer be reconciled against the number the route had just given
  for the same range, and a figure that cannot be checked against the figure beside it is what
  this store is for. `measured` is empty rather than `0` for a row that measured nothing at all,
  the same rule the aggregate follows.

### Lineage and attribution, without guessing

`parent_task_id`, `retry_of`, `attempt`, and `landing_state` are copied from durable task records
when present. `project_key` is canonicalized to the repository root, so worktree UUID directories
do not fragment one Project. `graph_id` and accepted `disposition` remain NULL until their own
producer exists; neither is inferred from a root Session or a successful terminal state.

Feature is mutable knowledge rather than mutable accounting. Project/Feature decisions therefore
live in an append-only attribution event table with source, decision, confidence,
classifier/version, evidence digest, time and supersession. A local deterministic classifier appends
the proposal — source `heuristic`, no model and no network — and a deterministic policy appends the
acceptance above a configured threshold; only
one unambiguous active accepted head enters analytics. The token interval never changes. See
[`usage-attribution.md`](usage-attribution.md) for the recording and privacy contract.

Four known gaps, written down rather than left to be discovered:

- A task record whose child session was never identified is filed under a synthetic
  `unresolved-session:<task-id>` and marked `session_unresolved`. Whichever collector saw the task
  first defines its session identity, so this only happens when neither ever knew one — but say
  what it costs when it does happen, because the earlier wording did not: the invented identity
  gets a **cursor of its own**, so a session that was already being watched has its cumulative
  counters attributed a second time and that group's total doubles. The row says so
  (`coverageReasons.session_unresolved` on the route, `coverage_reasons` in the CSV); nothing
  repairs it, and a later reading under the real session id does not merge the two.
- A Claude session whose transcript cannot be named by Claude Code's own session registry is not
  checkpointed at all. Usage is accounting, and a ranked guess at which transcript belongs to a
  tab is how a sibling's tokens get charged to the wrong session.
- **A terminal the inventory cannot enumerate is never checkpointed either**, for the same reason
  and with a different cause: `SessionWatch` can only offer what the backend reports, so a session
  running in a terminal this app cannot read contributes nothing until it is dispatched work,
  which files it through the other collector. It is absent rather than wrong, and absence has no
  row to carry a reason.
- **A checkpoint is throttled to one per session per five minutes**, so up to five minutes of a
  standing session's spend can be attributed to whichever side of a boundary the next reading
  lands on — the increment is filed against the boundary the reading describes, and the cadence
  is what bounds how much can be misfiled either way. Making it finer costs a transcript read
  every 1.2 seconds, which is the rate the panel reads at.

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

### And one optional line in your global `CLAUDE.md`

The stub reaches every project, but a skill description is something a session reads **once**, near
the start, and decides whether it applies. On 2026-09-05 a session that had just finished a turn
read this one, saw three clauses about handing work *out*, correctly concluded none of them was it,
and never looked again — so it never learned that a finished turn is worth reporting. It said
afterwards where it would have seen it: the file it reads every single time.

That file is yours and this project does not write it. If you want the delivery receipt to reach
sessions that are not dispatched by Clawdline and are not picking up a handoff, paste this into
`~/.claude/CLAUDE.md`:

```markdown
- When a turn is genuinely finished — integration, verification and any commit included — send one
  **delivery receipt** to Clawdline, which puts the *done* check on that session. It applies in
  **any** repository, not only Clawdline's own. How, in the clawdline skill guide's
  `Report the root's completed turn` section — load the guide rather than typing a route from
  memory. Not for partial work, a diagnosis, a blocker, or a question back to the user; a Clawdline
  **child** reports through its own `result.json` instead.
```

It is optional, it is per-machine, and nothing here breaks without it — a handoff receiver is
reached through its package instead, which is why the guide has the sender write that line into
`OBJECTIVE`. Ranked by what actually reaches a reader at the moment it matters, the order is: the
handoff package, then this file, then the stub's trigger list.

**What that copies is a discovery stub, and that is the point.** A file installed into a skills
directory is a copy, and a copy never updates, so the routes and fields it must not go stale on are
not in it. They ship with the app instead, and the stub says how to read them from the build that
will actually broker the dispatch:

```bash
/Applications/Clawdline.app/Contents/Resources/clawdline-skill.sh get clawdline
```

That is a local file read. It needs no running app, so it answers the same over SSH and while
Clawdline is closed — which is when an assistant most needs to know what this build supports.

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
   to fix and re-send under the same id, and **`depth_exceeded` means this session is a child and
   children do not dispatch** — the work is its own to do, with its assistant's own subagents if it
   needs several at once, not something to find another way to hand on.
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

The one rule stated before any of them: **only a root dispatches, and what it opens opens
nothing.** `CHILD.md` is where a child reads that, in the same words for every child, together
with what to use instead — its own assistant's subagents, which need no broker and no skill, which
matters because half of these sessions are Codex and Codex has no skills.

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
