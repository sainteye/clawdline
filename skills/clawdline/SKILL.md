---
name: clawdline
description: |
  Use Clawdline to dispatch a bounded child task when the current Root keeps synthesis,
  integration, and landing; to hand off an existing work line for full continuation; or to send a
  message, report, status, finding, or coordination note to another live session. Triggers include
  "dispatch a task", "open a child session", "get Codex to review this", "use Clawdline Handoff",
  and the equivalent Chinese
  requests 「派任務」「開 child」「使用 Clawdline Handoff」「交接給下一個 session」. A handoff transfers
  the sender's REFERENCES, VERIFICATION, and OPEN THREADS. Detached poll-only tasks are unattended
  automation, never Root or Major Feature owners. Root Assignment / Feature Launch opens an
  independent ordinary Root and must not be faked with a child, detached automation, or handoff. Do not use for
  work this conversation can simply do, provider-native subagent research, or session inventory.
  When this session is a Clawdline child, CHILD.md governs instead.
---

# Handing work to a child session

You are **Root**. The Clawdline app is the **broker**: you write a couple of files, make one HTTP
call, and it opens a terminal tab, types the first message into it, watches for the finish, adds
up the tokens, and comes back to tell you. The **child** is the session that gets opened. It does
one thing, and when it is done it writes `result.json`.

There are six steps. Do them in order.

Those six hand out a **task**. Handing over the **line of work itself** — this conversation's state,
to a session that continues it — is a different move with different rules, and it is §7.

---

## 0. Work out whether you are a root or a child — this decides whether you may dispatch at all

**Any one of these means you are a child:**

- The **first message** of this conversation is `You are a Clawdline CHILD agent for task <id>…`
- You have read, or been asked to read, `/tmp/.clawdline/<id>/CHILD.md`
- You are holding a `TASK_SECRET=`

**If you are a child, this skill is not what governs you — `CHILD.md` is.** And every child gets
the same answer: **you are the floor.** `CHILD.md` says so in one sentence — *you are the bottom of
this tree: you cannot dispatch Clawdline tasks of your own, and one you attempt is refused* — so
there is nothing to work out and nothing to look for.

**What to do instead is not "stop".** Work inside your task that wants its own context or wants to
run in parallel goes to **your own assistant's subagents** — Claude Code's Task tool, Codex's
subagents. They open no terminal tab, pass through no broker, and hand their answer back into this
session rather than into a file you have to wait for, which for a piece of one task is the better
tool anyway. §1–§6 below are written for a root; where they and `CHILD.md` disagree, `CHILD.md`
wins.

The tree is one level deep: the user's session opens children, a child opens nothing. Without a
floor, one job becomes five becomes twenty-five and a Mac runs out of terminals. The app does
enforce it — a dispatch from a child answers `depth_exceeded` — but that is the last line of
defence, not the first. **There is no `DISPATCHING.md` any more**, in your task directory or
anywhere else: nothing the app opens may dispatch, so there is no recipe to write, and a copy an
older build left behind is deleted rather than left to be followed.

---

## 1. Find the port and the token

```bash
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
TOKEN=$(cat ~/.config/clawdline/orchestrator-token 2>/dev/null)
[ -n "$TOKEN" ] || echo "NO TOKEN"
curl -s "http://127.0.0.1:$PORT/v1/health"
```

- `NO TOKEN` / no such file → **stop** and tell the user: Clawdline is not running, or it is too
  old to have the orchestrator. Ask them to open Clawdline and turn on
  **Let a browser or your phone see your sessions** in Settings → Remote; the token writes itself
  when the server comes up.
- `curl` cannot connect → same thing, the server is not running.
- health answers but there is no token file → this copy of Clawdline does not have the feature.
  Ask them to update.

**That token is the proof that you are a local process running as them.** Do not write it to a
file, do not hand it to a child, do not put it anywhere under `/tmp`, and do not paste it into a
reply.

---

## 2. Draw the whole graph before deciding who gets what

<!-- clawdline-dispatch-role-contract:v1 -->

- **Owned child.** `POST /v1/orchestrator/tasks` creates a bounded child only when Clawdfather
  retains synthesis, integration, and landing.
- **Handoff.** `POST /v1/orchestrator/handoffs` is continuation or transfer of an existing work
  line; the receiver must walk the sender's complete REFERENCES, answer VERIFICATION, and continue
  from OPEN THREADS.
- **Detached automation.** `root.session_id: null` with `root.poll_only: true` is only unattended
  detached automation. It is never a Root and never a Major Feature owner.
- **Root Assignment / Feature Launch.** `POST /v1/orchestrator/root-assignments` opens an
  ordinary independent Root and briefs only objective, scope, constraints, relevant references,
  and acceptance. Its durable machine-auth record and UI classification carry no child, handoff,
  detached, timeout, secret, result, parent, or landing lineage.

<!-- /clawdline-dispatch-role-contract:v1 -->

Use Root Assignment for a genuinely new independently owned Feature. Keep bounded work under
Clawdfather as a child, and use handoff only to continue an existing line with its full state.

### 2.0 Read the policy, then answer whether this should be dispatched at all

**Before every dispatch, the first thing you do is read this Mac's house rules — and they are two
files, not one:**

```bash
cat ~/.config/clawdline/dispatch-policy.md 2>/dev/null        # the base
cat ~/.config/clawdline/dispatch-policy.local.md 2>/dev/null  # what is true only here
```

The base is shipped with contents in it and **the user keeps editing it**, so it grows along with
what this house knows. The local sibling is optional, holds the facts that are true only of this
machine, and the app never seeds, writes or overwrites it. Clawdline composes the two into every
child's briefing with **the local one last, so where they disagree it wins** — read them the same
way round. Reading only the base is reading half the rules, and the half you skipped is the half
about this Mac. **Together they outrank anything in this skill.** Where they and this file disagree,
follow them and say which rule you followed when you report back. Only when both are missing or
empty do the defaults here apply. The user edits the base under Settings → Remote → "How work is
handed out", and you can edit it for them when they ask.

Then, before anything else, **answer the question the policy opens with: should this be dispatched
at all?**

The test is one sentence: **can this be cut into pieces that do not need to talk to each other,
and joined at the end?** There is a measurement behind it, and it is sharp in both directions: on
work that splits, coordinating several agents beat a single one by **80.9%**; on work where every
step depends on the one before it, *every* multi-agent arrangement tested came out **39–70% worse**
than a single agent, because the handoffs break a chain that needed to stay whole.

**When the answer is "it should not be", that is a recommendation and not a veto.** Say so, give
the reason in one sentence, **ask**, and then do whatever they answer. They have reasons this file
cannot see — they may want Codex to take this one, or their own context left free for something
else, or simply to watch it happen in a tab they can step into. **Their yes settles it**, with no
further argument and no conditions attached; what you owe is the reason, once, before the work
starts rather than after it went badly.

It runs the other way too: do not dispatch just to use the feature. The shapes that look
dispatchable and are not: diagnosis and debugging (every step is chosen because of what the last
one found), dozens of small parallel jobs (every node is a real assistant holding a real terminal;
a hundred of them is slower, dearer and unreadable), anything on a path where somebody is waiting,
anything needing agents to talk back and forth, output that has to be structured data a program
will consume, and **work smaller than its own briefing**.

### 2.0b How big one task is, and when small work goes out

**One task is one coherent, independently reviewable feature slice** — its production change, its
red-before-green tests, its docs, and its own verification, carried through one sustained session.
Keep the implementer there until the slice is mature: a discovery or a correction that belongs to
the same feature goes back into the same session, not into a new tab.

**A slice big enough to dispatch is big enough to lose.** `timeout_minutes` stops at 240, an
assistant can exhaust its quota mid-task, and a context window can fill. So a long or multi-file
slice goes out with `isolation: "worktree"`, is told to commit each milestone on its delivery branch
rather than at the end, and to send a progress note when the work stops matching its title. Then a child
that dies in its third hour costs one hour, not four, because the branch still holds the rest.

**Never open a session for one small change.** Small items accumulate in a pool, and the pool goes
out as one task when any of these is true:

- five items are waiting, or
- they are together worth more than about thirty minutes, or
- one of them blocks a landing, or somebody is waiting on it.

With a ceiling so that "accumulate" does not become "never": **no item sits in the pool for more
than 24 hours.** One task carries the whole batch, `claims` is the union of what the batch writes,
and the instructions list the items separately so the result can report on each one.

**Standing sessions.** A session may stay open between jobs — an **odd-jobs** session for those
batches, a **review** session for each finished feature or batch — holding its tab with
`orchestrator_child_linger: -1` and naming the role in `kind`. But **work reaches a standing session
only as an attached follow-up task**: a full task record with its own id, secret, `claims`,
`timeout_minutes` and `result.json`. `POST /v1/sessions/:id/send` is not that. It is the
paired-device route, it makes no task record, and anything fed through it holds no claims, signals
no completion, shows up in no `inflight` answer and is counted in no usage. So **without a follow-up
task carrying `claims`, a standing session must not write to the shared tree at all** — which a
review session satisfies by construction and an odd-jobs session does not. Until that mechanism has
landed, the honest approximation is one ordinary task per emptied pool and one per review round; a
tab you keep alive by typing into it is not a standing session, and do not call it one.

**A message is not an assignment.** When one live assistant needs to report status, a finding or a
coordination note to another — with no new work or shared-tree ownership attached — use
`POST /v1/orchestrator/messages`. Do not put a hand-written sender prefix through
`POST /v1/sessions/:id/send`: that route speaks for the person or a paired device, so App and Web
correctly draw it as their `user` message. The session-message route requires the machine token, an
`Idempotency-Key`, the source's exact current terminal or conversation id, and the target's exact
terminal-neutral session id; it preserves Markdown and draws the sender as a separate
`Clawdline ↔` card. It must not be used to attach work or bypass `claims`, and it refuses a target
that is showing an option menu with `409 target_busy`. Its `ok` means one typing attempt was
accepted — not that the target transcript observed it or the assistant acknowledged it — so
require an explicit reply when the outcome depends on receipt. Surface any typed refusal; never
fall back to `/v1/sessions/:id/send` or a hand-written prefix. The closed body and wire format are
in `docs/messages.md`.

**Send a generated raster as a local image, not as prose.** When ImageGen or another local tool
has produced a PNG/JPEG/WebP/GIF/TIFF that another live session should see, call
`POST /v1/orchestrator/messages` with `images:[{"path":"/absolute/local/path.png"}]` and the
ordinary source/target ids, token and idempotency key above. Never paste base64, invent or persist
a public URL, or fall back to legacy `/send`: Clawdline reads the local file, normalizes it into
its owned store and sends only an opaque expiring reference. The recipient sees a bounded
thumbnail that opens in a preview; once the reference expires or is unavailable, that same place
stays visible as an explicit **Image expired** tile.

**An interrupted review is handed over, not restarted.** A reviewer that died, timed out or was
cancelled has usually written part of its finding set already; give that file to whoever picks it
up. Review is both the most expensive node here and the one most often thrown away — 30 of 101
review dispatches on this machine never returned a verdict, and one re-review spent 6.7M tokens
re-reading 1.9M tokens of work somebody had already read.

**And when the reviewer comes back with findings:** it writes the complete finding set down and
reports it *before* it repairs anything, then repairs only what does not change the design. Once it
has edited production bytes its verdict is spent — that repair is a delivery, and the focused diff,
the mutation and the exact-tree acceptance are yours, not its. A design-changing correction goes
back to the implementer's session instead. Never one task per finding.

**One independent review per feature or batch. Count the rounds.** A *round* is one review of a
delivery; running two complementary reviewers side by side inside a round is still one round, and
that is not what this limits. What this limits is re-reviewing after a correction, which looks
free — the finding set is right there, the correction is small — and is not. Measured here on one
line today: the implementation cost $30.90 and its four review rounds cost $57.39, **1.9× the
thing being reviewed.**

- **Round two** only when round one found a defect *class* that will recur — the same mistake in
  places the reviewer did not read — and not merely because a finding was fixed and you would like
  it checked. "Did the fix work" is answered by the test that was red.
- **Round three** only with a written reason the coordinator has seen before the dispatch. Two
  mechanical triggers, both decidable from the correction diff alone and neither needing judgement,
  since on that same line both correction rounds introduced new defects the next review caught:
  **(a) the correction touches a code path `main` is also on**; **(b) the correction deleted or
  weakened an existing assertion.** Either one, dispatch it. Neither, write the reason or stop.
- **Beyond three, ask the user.** Blanket multi-round review has been rejected here explicitly.

Cheaper than a round, and usually the right answer: send the finding back to the session that wrote
the code, which still has the context, and read the correction diff yourself.

### 2.0a Decide whether the task needs a private worktree

Use `"isolation":"worktree"` for code changes that can be reviewed and landed as a Git branch.
The broker creates a clean private checkout when the task actually starts; optional
`isolation_base` names its Git revision, otherwise the then-current `HEAD` is used. Do not choose it
for reviewers and other reading-only work, artifact-only output, work that needs untracked local
state, or operations whose real collision is a running app, port, device, database, cache, or fixed
build destination. Use `serialize` for those machine-global collisions.

This changes the child rule narrowly. In a shared checkout a child still never commits. In its own
worktree a **Claude** child should commit early and only on `clawdline/task/<complete-task-id>`; it
must not push, switch branches, merge, rebase, stash, hard-reset, invoke `git worktree`, or run a
machine-global installer such as `build.sh`. **The delivery is that branch, not the checkout
directory and not an artifact diff.** The root reviews it with
`git diff <base>...clawdline/task/<id>` and lands it by merge or cherry-pick. Worktree isolation
protects tracked files only; it does not copy ignored dependencies, caches, or env files.

**A Codex child in a worktree cannot commit, so do not ask it to.** A linked worktree keeps no
`.git` of its own — its `.git` is a one-line pointer at `<main repo>/.git/worktrees/<task-id>/`,
which is *outside* the directory Codex's sandbox grants it write access to. Every commit therefore
dies on `fatal: Unable to create '…/index.lock': Operation not permitted`, and a child that cannot
record its delivery reports `failure` with the work sitting there, finished. Task `0c3853b8` did
exactly that today after completing 4438 checks. So when you dispatch:

- **`assistant: "claude"` with `isolation: "worktree"`** — tell it to commit each milestone on the
  delivery branch. The branch is the delivery, and a death in hour three costs one hour.
- **`assistant: "codex"` with `isolation: "worktree"`** — tell it explicitly to **leave the bytes
  uncommitted and not to attempt a commit**, and that root will commit them from the worktree path
  in the task record. Its delivery is the dirty tree plus its `result.json`, and the receipt shows
  `"dirty": true` with `"commits": 0`, which for a Codex task is success and not an unfinished job.

The generated briefing does not yet know the difference — it tells every worktree child to "commit
early and often" whichever assistant it is — so the instruction that overrides it has to come from
you, in `instructions`. Until that is fixed in the app, saying nothing means saying the wrong
thing.

Follow-up implementation rounds are code changes too. Base the next isolated task on the previous
delivery branch or commit with `isolation_base`; do not turn the delivery into a bundle in an
artifact-only task merely to continue editing it. Keeping every implementation round in a brokered
worktree keeps the branch, base, head, commit count and dirtiness in the task record that root must
later close.

### 2.1 If it is going out, pick a shape

The policy file's "pick a shape" section names a few. **Pick one; do not improvise.** In short:

| Shape | When | How the nodes go |
|---|---|---|
| **Split and join** | one question, several independent pieces | leaves on `haiku` each take a piece, one `sonnet`+ node joins and judges |
| **Build then read** | **output is code, or a decision somebody will act on** | a few nodes build, **plus one separate review node that only reads** |
| **Decide then do** | something important is being changed | one node writes the plan and touches nothing → **a person passes it** → another node (usually codex) implements |
| **Batch with takeover** | the same mechanical change across independent modules | one node per module; a dead tab keeps its state for a person to finish |
| **Candidates** | a design trade-off where what is being compared is taste | several nodes each produce a complete answer, **a person picks, no judge node** |

**New features are always Build then read.** The child graph producing code ends with an independent
review node; the root's work ends only after the landing closure in §6. The rules for the reviewer
are at the end of §2.2.

### 2.2 Draw the graph first; do not improvise as you dispatch

Before sending **any** of it, write the whole graph down:

```
root (you)
├── A  search X           claude/haiku   → artifacts/x.md
├── B  search Y           claude/haiku   → artifacts/y.md
└── C  join A and B       claude/opus    → artifacts/report.md
```

Four things have to be decided: **what each leaf produces, who joins them, which assistant and
model each node runs, and what the top hands back**. If you cannot say all four, do not send it —
in a graph nobody thought through, the mistake surfaces at the deepest level.

**Breadth before depth.** Two children splitting a job beat one child that will hand half of it on:
the second way costs a level of latency and one more round of paraphrase. Go deeper only when the
second level's work genuinely cannot be named until the first level has answered.

**When the output is code, or a decision somebody will act on, the child graph ends in a review
node. The root-owned graph does not.** Its final box is `root: land reviewed delivery on <target>
and verify the integrated tree`. Put that box, the delivery branch, the target branch and the root
landing owner in `plan` before dispatch. The reviewer is not a fifth worker but a reader: it reads
what the others produced, writes down what is wrong, and does not fix anything (fixing belongs to
the next round or to a person, and that holds even when it is sure it knows the fix — a repair
quietly buries the judgement somebody needed to see). Five rules:

1. **It took no part in building the thing.** Self-review is measurably bad: a model judging its
   own output misses about a third of its own semantic drift, and the mechanism is structural
   rather than a capability gap — a judge favours low-perplexity text, and a model's own output is
   low-perplexity to it by construction. **A stronger model does not fix this.**
2. **A different assistant helps and does not solve it.** Codex writes it, Claude reads it — that
   is right, but do not mistake it for independence. A panel of nine frontier models was measured
   to carry only about two votes' worth of independent information, because different models get
   the same items wrong. Where a review really matters, run **several reviewers and take the
   majority**, and pick them for being complementary rather than merely different.
3. **Reviews run on an opus-class model.** Not "no weaker than what it judges" — an absolute floor.
   A review is worth exactly what the reviewer's judgement is worth, and a missed finding travels
   all the way to the end. Measured here: a sonnet reviewer, in the middle of correctly explaining
   that judging is prone to hallucination, invented a specific citation — it claimed a document
   disputed a term that document never mentions.
4. **Name the `/tmp/.clawdline/<id>/artifacts/` paths it may read.** This is how rule 1 gets
   enforced rather than merely stated: without the list, a reviewer can wander into the production
   conversation it was supposed to be kept out of.
5. **A verdict, with its receipts.** "What is wrong, worst first, is this safe to ship" — and every
   finding names the artifact and the passage it rests on. A verdict without sources is the exact
   shape a hallucinating judge produces, and it costs nothing to require.

**Every node gets the whole graph**, not just its own square — that is what the `plan` field in
`task.json` is for. A leaf that knows what its output feeds writes something that connects; one
that does not writes an essay.

### 2.3 How many

A session may have **5 children out at once** by default (`orchestrator_max_children`, 1…10) — and
that number is **counted per session, not per Mac**; the whole Mac stops at twenty, four roots'
worth. If you are yourself a child your allowance is none at all, and that is a constant in the
code rather than a setting. Over either line comes back as `over_capacity`; a child dispatching
anything comes back as `depth_exceeded`.

### 2.4 Which assistant

If the user named one, use it. Otherwise:

| | Give it | Because |
|---|---|---|
| **codex** | writing code, **generating images**, hand-written SVG, running a build until it goes green, mechanical edits across many files | it is good at *making a thing you can then look at*, and it bills against a plan rather than per token |
| **claude** | reviewing a diff, reading code to work out why, searching and weighing what it found, prose somebody will read | it is good at *reading and judging* |

**Codex's sandbox blocks outbound connections by default** — do not give it work that needs the
open web; it will stall on an approval or simply fail. (Image generation is not affected; see
§2.5.)

**Check quota before picking one.** `curl -s http://127.0.0.1:7717/v1/orchestrator/assistants`
(with `X-Clawdline-Orchestrator`) answers `availability` for both — `ok`, `low`, `exhausted`, or
`unknown` when nobody has looked recently. Dispatching to an `exhausted` one is refused with `409
assistant_exhausted`; its `alternatives` array names who to send instead.

### 2.5 When the deliverable is an image

**Codex has a real image model.** It is not a fallback and it needs no API key: `image_gen` is a
built-in tool, on by default, and it draws through the Codex account the child is already signed
in as. Check it in one line:

```bash
codex features list | grep image_generation      # → image_generation  stable  true
```

Two properties of that tool decide how the briefing has to be written:

- **It cannot be told where to save.** The PNG lands under
  `~/.codex/generated_images/<session-id>/*.png` and nowhere else — Codex's own guidance is not to
  rely on a destination argument. **So the instructions must say: generate it, then copy the file
  into `/tmp/.clawdline/<id>/artifacts/`.** Leave that out and the task ends with a picture nobody
  can reach and an empty `artifacts/` directory.
- **The sandbox does not stand in its way.** The drawing happens on the model's side rather than
  over the child's own network, so the "no outbound connections" limit in §2.4 does not reach it.
  Measured on 2026-08-26 with codex-cli 0.149.1: `codex exec -s workspace-write`, 35 seconds,
  ~14k tokens, a 1254×1254 PNG.

**Raster or vector is a real choice, not a workaround:**

| Ask for | When | Because |
|---|---|---|
| **a PNG from `image_gen`** | illustration, texture, anything photographic, a hero image | it is a drawing, and it looks like one |
| **an SVG codex writes by hand** | diagrams, icons, anything that must stay editable, scale cleanly or be diffed | vectors, small files, and a person can change one path afterwards |

Transparency is something you ask `image_gen` for directly, keeping its alpha — not a reason to
fall back to SVG. There is also a CLI path (`gpt-image-2`, `gpt-image-1.5`) that *does* need
`OPENAI_API_KEY`; a child has no reason to reach for it, and must never quietly drop to it when the
built-in tool is right there.

### 2.6 Which model

The `model` field in `task.json` (optional; leave it out for that assistant's default). **Lowercase
letters, digits, `.`, `_` and `-` only** — anything else comes back as `bad_task`.

| Model | When |
|---|---|
| `haiku` | mechanical, single-source work: fetch a page, pull three facts, reformat. The kind where being wrong is obvious |
| `sonnet` | ordinary work with judgement in it, when you can say why not `opus` |
| `opus` | a decision somebody will act on without checking, and **any node joining several children's answers** |

**A review runs on an opus-class model** — an absolute floor, not a comparison with whatever
produced the thing (§2.2 rule 3 has the measurement behind that). "No weaker than what it judges"
would let a haiku leaf be reviewed by haiku, and a review is worth exactly what the reviewer's
judgement is worth: a missed finding travels all the way to the end.

On the Codex side the same field takes its slug (e.g. `gpt-5.1-codex`).

**Always name the model on a Claude dispatch — `opus` unless you can say why not — and on Codex
name one only when the default is the wrong size.** The asymmetry is not style: an omitted Claude
model inherits whatever `/model` is set to on this Mac right now, which nobody chose for that task
and no record keeps. Three dispatches ran on `claude-fable-5` that way on 2026-08-28. **A pin the
account cannot honour fails where you will not see it**, which is the reason the Codex half stays
conservative: Clawdline validates the spelling, not the entitlement, so the task
reaches `briefed` normally and then dies inside the assistant's own CLI with a 400 that appears
only in that assistant's rollout, not in the task record, not in `warnings`, and not in
`result.json`. `gpt-5.1-codex` did this today on a ChatGPT-authenticated account. Omitting `model`
cannot fail this way.

For a Codex task only, optional `reasoning_effort` is exactly `high` or `xhigh`. Use `high` for
coding and `xhigh` for planning. Leave it out to inherit the Codex/model and user defaults:
omission adds no command-line override. Empty values, non-strings, every other name (including
`max` and `ultra`), and using the field with `assistant: "claude"` are `bad_task`. Hand-written
schedule templates may carry it and Codex schedule edits preserve it even though there is no UI
control; explicitly switching that schedule to Claude removes the incompatible hidden override.

### 2.7 Will the child stall on a permission prompt

**Nobody is watching the child's tab.** A session that stops to ask you to approve something stops
until it times out — and afterwards it just looks like the work never happened, with no way to see
why.

`permission_mode` in `task.json` takes three values (**there is no `auto`** — see the warning):

| Value | Maps to | When |
|---|---|---|
| `ask` | no flag (Claude Code's manual) | only if you intend to sit and watch that tab |
| `edits` | `--permission-mode acceptEdits` | leaves that only read and write files and run nothing |
| `full` | `--permission-mode bypassPermissions` | **the default**, and in practice the only one that finishes |

**Why `full` is the default.** A dispatched session's job *is* running commands and writing files,
and every narrower setting stops somewhere: `ask` stops on the first thing it does (reading its own
CHILD.md), `edits` gets past writing files but not past `cat` / `mkdir` / `curl` / `sleep` — which
is most of what a task consists of. No flag covers those short of `full`. It does not widen *who*
may dispatch; that is still the `0600` token file.

**⚠️ `auto` is model-dependent, which is why Clawdline does not offer it.** Measured:
`--permission-mode auto` gives you auto mode **on Sonnet and Opus, and `manual` on Haiku** — a
prompt at every step, worse than passing no flag at all, and with no error message anywhere. A
value that quietly becomes the strictest setting on the cheapest model has no business in a
dispatch field. The wider lesson: **when you verify a flag, the model is one of the variables.**
Testing on one model gets you the wrong answer.

**Four doors, innermost first. Clawdline handles the first two for you:**

1. Reading **across directories** (the child's task lives in `/tmp/.clawdline/` while its working
   directory does not) → the app adds `--add-dir` for you.
2. Writing **across directories** (writing its own result.json) → needs `edits` or better.
3. **Command screening** — `jq -n` with a single-quoted filter is read as "a brace against a quote,
   therefore obfuscation", and `... > f.tmp && mv` as "shell syntax that cannot be analysed
   statically", **and neither prompt offers "always allow"**. Only `full` gets through. So: **write
   JSON with a heredoc, and write files with the Write tool.**
4. **Trusting the folder** — a directory nobody has opened before asks "Do you trust this folder?"
   first, **no permission setting reaches it**, and the task turns into `spawn_failed` two minutes
   later. Before dispatching into a new directory, somebody has to have opened it by hand once.

**This Mac's ceiling** is under Settings → Remote → "How far a child may go on its own"
(`orchestrator_permission` in config.json). A task asking for more than the ceiling is quietly
lowered to it; what actually took effect is in the task record's `permission` field, which you can
read back.

**The only way to check whether a child was ever prompted is to watch that tab live.** The
transcript cannot tell you afterwards — asked-and-approved and never-asked are written down
identically — and a child reporting "nothing blocked me" does not count either.

### 2.8 The instructions have to stand on their own

The child cannot see this conversation. All it has is `instructions` and `plan` from `task.json`,
so "do what we just discussed" says nothing there. Write absolute paths, and name the file each
output goes into. **A leaf's instructions should be narrow enough to state in one sentence** — if
it takes three paragraphs to say what "done" means, that is two children.

**Ask for one progress note in the first three minutes**, saying what it has decided to do now
that it has read the briefing — before the work, not during it. One line in the instructions buys
it, and it costs the child one round. **Do not name the channel** — the two assistants do not share
one. A codex child has no network at all (`CODEX_SANDBOX_NETWORK_DISABLED=1`; a `curl` to
`127.0.0.1` ends in exit 7 and even DNS is off), so it writes `progress.json` in its own task
directory and the broker collects it the way it collects `result.json`; a claude child can use
either that file or the HTTP route. Each child's own briefing carries the one that works for it.

That note is the only thing that makes an early cancellation possible, and the difference is not
small: the two dearest cancelled tasks measured on this machine burned 18.5M and 16.5M tokens and
ran twenty-six minutes each before anybody could tell they were going the wrong way. The protocol
asked for a note only when the work stopped matching its title, which is a signal that arrives
after the divergence rather than at it. A wrong first sentence is visible at minute three, and it
is the cheapest thing in this system to correct.

---

## 3. Make the directory, the id and the secret

```bash
task_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
secret=$(openssl rand -hex 32)
umask 077 && mkdir -p "/tmp/.clawdline/$task_id/artifacts" && chmod 700 /tmp/.clawdline "/tmp/.clawdline/$task_id"
echo "$task_id"
```

`umask` and `mkdir` have to be in the **same** bash call — every tool call is a fresh shell, and a
umask set in an earlier one is gone.

`secret` is the credential the child reports completion with: 64 hex characters. **It travels one
route only** — you hand it to the app in the dispatch body, and the app puts it in the first
message typed into the child. The app keeps only its SHA-256. **Do not write it into `task.json`,
and do not write it into any file under `/tmp`.**

## 4. Write task.json

Build it with `jq -n` rather than by hand — string-pasting breaks the moment `instructions`
contains a quote or a newline.

```bash
graph_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
GRAPH=$(jq -nc --arg id "$graph_id" --arg current "portrait" \
  '{id:$id,destination:"Reviewed portrait landed",current_node:$current,
    nodes:[{id:"portrait",title:"Draw",kind:"delivery",depends_on:[],acceptance:["Matches the brief"]},
           {id:"review",title:"Review",kind:"review",depends_on:["portrait"],acceptance:["Three-axis verdict"]}],
    unknowns:[],out_of_scope:[]}')
jq -n \
  --arg id "$task_id" \
  --arg kind "image" \
  --arg assistant "codex" \
  --arg dir "$PWD" \
  --arg title "Project portrait" \
  --arg instructions "…the full briefing…" \
  --arg created "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg root_session "$ROOT_SESSION" \
  --arg root_assistant "$ROOT_ASSISTANT" \
  --arg root_label "clawdline root session" \
  --argjson poll_only "${POLL_ONLY:-false}" \
  --arg model "haiku" \
  --arg plan "$PLAN" \
  --argjson graph "$GRAPH" \
  '{clawdline_protocol:1, task_id:$id, kind:$kind, assistant:$assistant, model:$model,
    permission_mode:"full",
    isolation:"none", project_dir:$dir, title:$title, instructions:$instructions,
    plan:$plan, graph:$graph,
    deliverables:["artifacts/out.png"], timeout_minutes:30, created_at:$created,
    root:{session_id:(if $root_session=="" then null else $root_session end),
          assistant:$root_assistant, project_dir:$dir, label:$root_label,
          poll_only:$poll_only}}' \
  > "/tmp/.clawdline/$task_id/task.json"
```

`$GRAPH` is a JSON object for the complete graph from §2.1. Every task carries the same graph id,
destination, nodes, unknowns and scope; only `current_node` changes. `$PLAN` is the optional legacy
free-text note. The broker, not the caller, derives the live frontier from task, review and landing
receipts.

Field rules (breaking one is `422 bad_task`; the app will not fill anything in for you):

| Field | Rule |
|---|---|
| `clawdline_protocol` | always `1` |
| `task_id` | lowercase UUID, **the same in the directory name, the file and the dispatch body** |
| `kind` | `image` · `code-review` · `test` · `custom` |
| `assistant` | `claude` or `codex` |
| `project_dir` | absolute path, and the directory has to exist now |
| `title` | ≤ 200 characters, one line a person can read |
| `instructions` | non-empty, ≤ 16 KiB |
| `deliverables` | paths relative to the task directory; `artifacts/…` by convention |
| `claims` | optional, and **declare it anyway**: 0…32 unique relative paths this task may write, each 1…1024 characters, no leading `/` and no `..`. `[]` positively declares the task read-only. See below |
| `model` | optional. Lowercase letters, digits, `.` `_` `-`, ≤ 64 characters. Absent = that assistant's default |
| `reasoning_effort` | optional, Codex-only. Exactly `high` (coding) or `xhigh` (planning). Absent = inherit Codex/user defaults with no CLI override; `max` and `ultra` are not accepted |
| `permission_mode` | optional. `ask` / `edits` / `full`. Absent = this Mac's ceiling (default `full`). Anything else, `auto` included, is `bad_task` |
| `isolation` | optional. `none` / `worktree`; absent = `none`. Use `worktree` only after the §2.0a decision |
| `isolation_base` | optional Git revision, legal only with `isolation: "worktree"`; absent means `HEAD` at actual start time |
| `graph` | optional typed graph; use it for every multi-node feature. Lowercase UUID id, destination, `current_node`, 1…32 unique acyclic nodes, dependencies, 1…8 acceptance strings per node, and bounded `unknowns` / `out_of_scope`. Node kinds: `decision` · `delivery` · `review` · `correction` · `verification` · `landing`. One retained graph id accepts one definition |
| `plan` | optional legacy free-text note, ≤ 4 KiB; new coordination flows use `graph` |
| `timeout_minutes` | 1…240, 30 if absent |
| `root.session_id` | this assistant's current process-bound conversation id. A terminal-neutral id belongs only on terminal-addressed routes; an ordinary dispatch must resolve to a live owner |
| `root.assistant` | **required for every ordinary dispatch with a non-null owner**: the assistant dispatching this task, `claude` or `codex`; it is not the child named by top-level `assistant`. Omission/null is `root_assistant_required`; a legacy missing assistant is unknown for ownership (the old Claude fallback survives only in non-ownership compatibility readers) |
| `root.parent_task` | **leave it out.** It existed for a dispatching child, and a child cannot dispatch; every dispatch this app now accepts comes from a root, where the field is `null`. It is still validated and still read, because a stored record from an older build carries it |
| `root.poll_only` | default `false`. Set `true` only for deliberate detached automation with a null session id; it will not receive completion notification or own a child row, so the caller must poll |

**Declaring `claims` costs about twenty output tokens, and most dispatches skip it anyway.**
Measured across 206 dispatches on this machine: 60.7% declared nothing at all. A collision costs a
whole task, which on the same record is between three and eighteen million tokens thrown away.

And know the one case `claims` cannot save you from: **repository-relative claims are discarded for
a worktree-isolated task**, because the child edits a separate checkout. On 2026-08-28 two roots
dispatched a correction of the same delivery six seconds apart, both isolated, and nothing refused
either of them — `/inflight` was still empty for both. When you isolate, `/inflight` is the only
check there is, so read it, and keep the intended write set in the plan so review still has a scope.

**There are two ways to get `claims` wrong and only one of them is loud.** Leaving it out is the
quiet one: the broker cannot prove your task is disjoint from anybody else's, so it falls back to
warning about every pair that shares a directory. On 2026-08-26 that was a dozen notices in one
evening, not one of which described a real conflict, while the single genuine collision that night
was refused at dispatch with `409 workspace_busy` in the same breath. **Omitting `claims` is not
the cautious choice; it is the one that produces noise instead of an answer.** Claiming too widely
is the other one, and it announces itself: a claimed path blocks other trees whether or not you
ever touch it, and at terminal state the broker names every claim the task never touched. Same
error, pointing the other way — take that report seriously and declare narrower next time.

### Finding your own assistant and session id (required unless deliberately poll-only)

> `root.session_id` is the assistant's own conversation id — the Claude transcript uuid or
> Codex rollout id — never the watched terminal id. At dispatch it resolves that spelling against
> `root.assistant` and stores one process-bound conversation key for
> completion notification, grouping, capacity and close cascade. Always inspect `warnings`: a
> non-null spelling that matches no live owner is refused as `root_unresolved`. If there truly is
> no interactive owner, set `POLL_ONLY=true` deliberately and accept that polling is the only
> completion path; never let an empty lookup silently choose that mode.

Two same-assistant processes proving the same conversation fail as `conversation_ambiguous`; they
are never deduplicated into one owner. If this assistant genuinely cannot establish its provider
conversation id, use authenticated `GET /v1/orchestrator/sessions` as the explicit fallback only
for terminal-addressed operations. Select the live row deliberately and never substitute
`$ITERM_SESSION_ID`. The index does not manufacture `root.session_id`; dispatch without a
conversation owner is explicit poll-only automation.


**Codex:** its current rollout id is exported directly. Do this in the same shell call that writes
`task.json` so both variables reach `jq`:

```bash
ROOT_ASSISTANT=codex
ROOT_SESSION="${CODEX_THREAD_ID:-${CODEX_SESSION_ID:-}}"
echo "root session = ${ROOT_SESSION:-null} ($ROOT_ASSISTANT)"
```

`CODEX_THREAD_ID` and the compatible `CODEX_SESSION_ID` spelling name the
`session_meta.session_id` in Codex's rollout. The broker accepts it only when the declared Codex
terminal's **current pid** holds that same user rollout open; an id copied from an older rollout
does not mount a child under a reused terminal.

**Claude:** set the assistant, then leave a nonce and fish the id out of your own transcript.

Claude Code has no way to ask "who am I", so leave a nonce and fish it out of your own transcript.
**This has to be split across two tool calls** — a call's command text is only written to the
transcript *after that call ends*, so echoing a nonce and grepping for it in the same call never
finds anything (measured; retries do not help):

```bash
# Call A (the same call as step 3 is fine): leave the nonce in the transcript
ROOT_ASSISTANT=claude
echo "clawdline-nonce-$task_id"
```

```bash
# Call B (the next tool call, together with steps 4 and 5): now it is findable
ROOT_ASSISTANT=claude
slug=$(printf '%s' "$PWD" | sed 's/[^a-zA-Z0-9]/-/g')
f=$(grep -l "clawdline-nonce-<task_id>" "$HOME/.claude/projects/$slug/"*.jsonl 2>/dev/null | head -1)
ROOT_SESSION=$(if [ -n "$f" ]; then basename "$f" .jsonl; fi)
echo "root session = ${ROOT_SESSION:-null}"
```

How it works: the nonce lands in the transcript along with the record of call A, and the filename
`grep -l` finds in call B, minus `.jsonl`, is this session's id. The slug is `$PWD` with every
non-alphanumeric character replaced by `-`. Remember to substitute the real id into `<task_id>` —
call B is a new shell and call A's variables are gone.

**Do this on the main thread, not in a subagent.** A subagent's transcript lives at
`~/.claude/projects/<slug>/<session-id>/subagents/agent-*.jsonl`, which the `*.jsonl` glob does not
reach (measured), and you get an empty string. If you really are running inside one, this recovers
it — **the session id is the directory two levels above that file**:

```bash
p=$(grep -rl "clawdline-nonce-$task_id" "$HOME/.claude/projects/$slug/" 2>/dev/null | head -1)
case "$p" in
  */subagents/*) ROOT_SESSION=$(basename "$(dirname "$(dirname "$p")")") ;;   # two levels up = session id
  *.jsonl)       ROOT_SESSION=$(basename "$p" .jsonl) ;;
esac
echo "root session = ${ROOT_SESSION:-null}"
```

If call B still comes back empty, try once more a call later. If it is still empty, do not dispatch:
report that the current root identity could not be proved. Only automation deliberately designed
to be detached may set `ROOT_SESSION=""` and `POLL_ONLY=true`, and that caller must keep polling.

**This is worth two things, and the second one is easy to forget:** one, the app needs to know
which terminal to notify when the task finishes; two, **the child's row in the list is indented
under you because of this id**. A null id is refused unless the task explicitly declares
`root.poll_only:true`; that mode is for detached automation, not a fallback for a failed lookup.
Never guess an id. `ROOT_SESSION` and `ROOT_ASSISTANT` have to be in the same bash call
as step 4's `jq`, or the variables will not survive; alternatively paste both strings straight
into `--arg`.

Do not put the physical iTerm/tmux/Clawdline row id in `root.session_id`. For a new dispatch the
broker positively recognizes an active physical id or the durable Coordinator's physical binding
and returns `422 root_identity_is_terminal` with `canonical_root_session_id` and
`canonical_root_assistant`. Evidence is independent of what you put in `root.assistant`, so replace
both values with that actual tuple. Unknown or offline identities are not guessed and are refused
as `root_unresolved`; multiple same-assistant process owners are refused as
`conversation_ambiguous`. Detached automation uses null plus `root.poll_only:true`.

---

## 5. Dispatch

```bash
curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
  -H "X-Clawdline-Orchestrator: $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"task_id\":\"$task_id\",\"secret\":\"$secret\"}"
```

Success looks like this (`state` will be `queued` or `spawning`; the tab is not open yet):

```json
{"ok":true,"task":{"id":"…","state":"spawning","kind":"image","title":"Project portrait",
 "assistant":"codex","projectDir":"/Users/you/code/clawdline","created":1787100000,
 "spawnedAt":1787100002,"dir":"/tmp/.clawdline/…","child":{"terminalId":"…","backend":"iterm"}}}
```

Failure is always `{"error":{"code":…,"message":…,"request_id":…}}`. **Branch on `code`:**

| `code` | Means | Do |
|---|---|---|
| `depth_exceeded` | **you are already at the bottom of the tree** | stop now, tell the user as in §0, and do this one yourself. Do not route around it |
| `over_capacity` | the allowance is full | `message` says whether it is your session's allowance or the whole Mac's. The error carries `retry_after` in seconds. Wait and resend, or send fewer / in batches. **Do not hammer it** |
| `bad_task` | `task.json` does not validate | read `message`, fix the file, resend the same `task_id` (same id is idempotent). A bad `model` lands here too |
| `root_session_required` | `root.session_id` is empty and the task did not explicitly opt into polling | find this assistant's current conversation id. Use `root.poll_only:true` only for intentionally detached automation |
| `root_assistant_required` | a non-null owner omitted `root.assistant` | set the actual dispatching assistant to `claude` or `codex`; do not rely on the legacy Claude read fallback |
| `root_unresolved` | the supplied root id matches no live process-bound owner | refresh the current conversation id; do not let the child open under a stale or guessed id |
| `conversation_ambiguous` | multiple live processes of the declared assistant prove the same conversation id | stop and resolve the duplicate ownership; do not select an arbitrary terminal |
| `root_identity_is_terminal` | `root.session_id` is positively proved to be a physical terminal id | replace it with `canonical_root_session_id` from the error; never broaden task resolution to terminal ids |
| `forbidden` | wrong token or none | re-read the token file; still failing means the app regenerated it, so ask the user to restart Clawdline |
| `rate_limited` | more than 10 dispatches in 10 minutes | wait for the window to roll |
| `not_found` | no such route | this Clawdline has no orchestrator; ask the user to update |

**Resending the same `task_id` is safe** — you get the record that already exists, not a second
tab. So a retry after a timeout is just a resend; there is no need for an `Idempotency-Key`.

---

## 6. Report, wait, then close the root obligation

As soon as it is out, tell the user: **how many, what each is, who is doing it, and where the
output will be.** One line each; the first 8 characters of the `task_id` is enough.

Completion arrives one of two ways, and you do not have to choose:

1. **You get told** — the app types one durable `task_finished` notice into your terminal:
   ```
   [clawdline] task 3f9a21bc (Project portrait) finished: success — read /tmp/.clawdline/<id>/result.json — after observing, ACK notice <notice-id> at /v1/orchestrator/tasks/<id>/completion/ack
   ```
   When you see it, read `result.json` and `artifacts/`, then ACK the exact `notice_id` with the
   machine token. Repeating the same ACK is safe:

   ```bash
   PORT="${CLAWDLINE_PORT:-7717}"
   TOKEN=$(cat ~/.config/clawdline/orchestrator-token)
   TASK_ID='<task-id from the completion line>'
   NOTICE_ID='<notice-id from the completion line>'
   curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/completion/ack" \
     -H "X-Clawdline-Orchestrator: $TOKEN" -H 'Content-Type: application/json' \
     -d "{\"notice_id\":\"$NOTICE_ID\"}"
   ```

   Retries carry the same id, so a duplicate line is the same completion, not another result to
   consume. ACK only after you have observed the notice; an HTTP response, SSE frame or terminal
   send success does not count for you.
2. **You poll** — when `root.session_id` came back empty, or the user wants to know now:

```bash
curl -s "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$task_id" \
  -H "X-Clawdline-Orchestrator: $TOKEN" | jq '.task | {state, summary, artifacts, usage}'
```

`state` runs `queued → spawning → briefed → success | failure | timeout | cancelled |
spawn_failed`. **`briefed` means the child is working**; it can sit there a long while and that is
not a stall.

For delivery diagnosis, `GET /v1/orchestrator/completions?pending=true` exposes accepted,
executed, result-verified, transport-delivered, observed and acknowledged separately. The broker
retries root missing/chooser/modal/timeout/stale identity on a background queue with a bounded
budget and keeps polling as the fallback; do not treat `transport_delivered_at` as proof the root
consumed it. Eight unsuccessful attempts become a typed `dead_letter`, never silent loss. Inspect
that state in the machine-authenticated ledger; rearm it explicitly with
`POST /v1/orchestrator/completions/reconcile` and JSON boolean `include_dead_letter: true` after
correcting the cause. Reconciliation is bounded and never rewrites a historical root identity.

**Do not open a while loop and wait in it.** Check now and then, and when the user asks; children
routinely run for ten or twenty minutes, and tying the root session to a poll is the most expensive
way to use this.

To end one early:

```bash
curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$task_id/cancel" \
  -H "X-Clawdline-Orchestrator: $TOKEN"
```

**And closing your own session cancels all of them at once, which is the part people forget.**
Ending a root — the Close button, `POST /v1/sessions/:id/end` — ends every live task it dispatched,
deepest first. That is documented as a mechanism; here it is an obligation with two halves.

*Before* you close, look at what the close takes with it:

```bash
curl -s "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
  -H "X-Clawdline-Orchestrator: $TOKEN" \
  | jq --arg s "$ROOT_SESSION" '[.tasks[]
      | select(.root.sessionId == $s and (.state | IN("queued","spawning","briefed")))
      | {id, title, state, assistant}]'
```

An empty list means close freely. A non-empty one is work in progress that your close will end, so
either wait for it or say in your last message what is being killed. On 2026-08-27, 23:20:37–:51 —
fourteen seconds, one close — four in-flight tasks died together, one of them a correction
dispatched 75 seconds after the review that demanded it and 25 minutes into its run.

*After* a cascade, **name who adopts each orphan.** Their landing records say `pending`, which is
also what live work says; the record cannot tell a task being worked on from one whose worker was
killed hours ago. So write down, per orphan, the named root or named person picking it up — and if
there is nobody, put that to the user as a decision rather than leaving it in the list. The
safe-close correction was recorded as *cancelled* and not as *cancelled by a cascade and now
unowned*, so it sat for fourteen hours reading as a hard problem instead of an absent one.

Reading the result:

```bash
cat "/tmp/.clawdline/$task_id/result.json"
ls -la "/tmp/.clawdline/$task_id/artifacts/"
```

`summary` in `result.json` is a sentence the child wrote itself, and `artifacts` is what it
*claims* it produced — **a claim is a claim; `ls` the directory yourself**. Task directories are
cleared 24 hours after they finish, so anything the user wants to keep has to be copied out.

`symbols` is the field you cannot reconstruct afterwards: every name the child's change introduced
— new functions and types, new fields, new string keys, the names of test groups it added. Names,
not descriptions. This tree is shared, so by the time you come to commit, the files that child
edited may hold two or three sessions' unfinished work, and that vocabulary is how you tell one
session's hunks from another's. Guessing it has produced staged trees that would not compile. A
child that reports nothing there has not told you it changed nothing; ask, or read the diff for the
names yourself before staging.

For a typed review node, read the `review` object too. It must keep `specification`,
`repository_invariants`, and `runtime_failure_behavior` as three separate axes. `safe_to_land`
requires all three to pass with no findings; `changes_required` requires at least one finding with
an id, severity, summary, and concrete evidence. Task `success` without that valid receipt does not
advance the graph frontier.

**When you pass what came back to the user, split it in two.** What the results imply for *you* to
do next is one list; what only *he* can decide is a separate list, labelled as such — never mixed
into a single "next steps" paragraph, where the decisions reliably disappear. And a decision goes
to him as an explicit options prompt in his own session, one question at a time, each option naming
what happens if he picks it, with your recommendation attached: asked in prose he misses it and
does not know how to answer, which is his own account of it. Full rule:
[`AGENTS.md`](../../AGENTS.md#decisions-that-are-the-users-go-to-the-user-as-options).

### Child completion is not code completion

The orchestrator states above describe the **child task**. For a code-producing graph, keep a
separate root-owned obligation with these meanings:

```
delivered -> reviewed -> pending landing -> landed
```

- `success` means the child delivered what it claims.
- `SAFE TO LAND` means an independent reader found no blocker. It moves the root obligation to
  **pending landing**; it does not mean merged, shipped, complete, or done.
- `landed` means the intended target ref contains the reviewed change and the exact integrated tree
  passed its required verification.

The dispatching root is the landing owner unless a named root accepts a Clawdline handoff. A vague
"someone later" is not an owner. Do not give the user a completion answer while the obligation is
`delivered`, `reviewed`, or `pending landing`.

### Report the root's completed turn

When your own root turn is genuinely complete—including required integration, verification and
commit—make its last tool action an authenticated session delivery report, then give the user the
final answer. This is the root equivalent of a child's `result.json`, but deliberately weaker: it
draws one check, **delivered; awaiting approval**, and can never claim independent review or a
broker-verified landing.

```bash
ROOT_CONVERSATION='<this assistant process-bound conversation id>'
ROOT_TERMINAL=$(curl -fsSG "http://127.0.0.1:$PORT/v1/orchestrator/whoami" \
  -H "X-Clawdline-Orchestrator: $TOKEN" \
  --data-urlencode "conversation_id=$ROOT_CONVERSATION" | jq -er .terminal_id)
jq -n --arg summary "$SUMMARY" '{summary:$summary}' \
  | curl -sS -X POST \
      "http://127.0.0.1:$PORT/v1/orchestrator/sessions/$ROOT_TERMINAL/complete" \
      -H "X-Clawdline-Orchestrator: $TOKEN" \
      -H 'Content-Type: application/json' --data-binary @-
```

Set `SUMMARY` to one concrete sentence of at most 500 characters before that block. Call it only
while the final turn is still working. Do not call it for partial work, a diagnosis-only answer, a
blocker, a question, or from a child. Branch on typed refusals and report one honestly; prose is
not a substitute for a missing receipt. Clawdline consumes the receipt when this terminal begins
its next observed turn, so an old check cannot return after newer unreported work.

While a root is idle with a live Clawdline child, the broker projects `waiting_session` and the row
names the child beside a quiet `⏳`; that is waiting, not triage and not delivery. Root activity
still reads `working`, and a child finishing removes the wait without completing the root's own
integration turn.

When claimed child work comes back, root records the open obligation on that task with its task
secret: `POST /v1/orchestrator/tasks/:id/landing` and `{"state":"pending","target":"<ref>"}`.
A named root that accepted a handoff may use the machine-level orchestrator token instead, like
cancel and claims release. By convention children do not call this route, although they necessarily
hold their own task secret. `GET /v1/orchestrator/landings` is the shared signpost; reading it does
not retain claim locks or block another dispatch.

Read each pending row's closed `ownership.status` before deciding what to do. Exact observed work,
observed ready/holding, a still-live task, absence from a complete inventory and unknown/stale
evidence are different answers. Incomplete, missing, or assistant-less legacy SessionWatch evidence is always `unknown`,
never proof that an owner is dead/offline; compare the stable task/root ids with the same row in
Coordinator Bearings. This read is bounded and cached, so a wedged live observer still returns
durable rows with stale/missing evidence and unknown ownership. Landing target verification uses
the task's durable Git common-directory receipt for every Git task regardless of isolation. A
legacy deleted broker-worktree path is derived only from one uniquely matching retained repository
slug; missing, ambiguous, arbitrary, or mismatched evidence fails closed without guessing a checkout.

### Close a code delivery

After the terminal review verdict, root does all of this:

1. Read the broker's worktree receipt and the review evidence. Confirm the delivery branch, base,
   head, commit count and clean/dirty fact are the ones reviewed.
2. Read the target repository's current branch, head and status. Integrate by merge, cherry-pick or
   rebase without staging, rewriting or absorbing another session's pre-existing changes. In a
   shared tree, the child's `symbols` list is what tells its hunks from the ones already there.
3. If overlapping uncommitted work makes that unsafe, stop the merge attempt but **keep the
   obligation pending**. Use Clawdline's session/task view to identify and coordinate with the
   owner, then retry when the tree is safe. Shared-tree safety is a reason to wait, not a reason to
   declare the work complete.
4. Test the exact integrated tree according to the repository's rules, using private temporary
   paths. You are staging anyway, so the index is the right subject: test an archive of
   `git write-tree`, never the live working tree, which is the union of everybody's work. Then
   verify the target ref contains the intended delivery and record the target commit.
5. Mark the task's landing record `landed` with the verified commit, using the task secret or the
   machine token after an accepted handoff. Only now report `landed` or `complete` to the user. Say
   which target and commit received it.

**HEAD has to compile standing alone, and committing is the only act that can break that.** It
happened twice on 2026-08-26 in this repository, from two different sessions: a whole-file `git add`
carried three lines whose type was defined in a file that stayed uncommitted, and a protocol
requirement landed while its fourteen values stayed in the worktree. Both trees were green at the
moment of committing, and that is the trap — **a green tree says nothing about HEAD while anything
is uncommitted**, because the tree is the union of everybody's work and HEAD is only your slice. So
a partial commit is not finished until its own slice has compiled on its own, and before taking one
ask what else defines what you are taking: a declaration without its values, a call without its
function, a case without its enum. Each of them passes in the tree and fails in HEAD.

### Close a whole batch, when you are the coordinator

Several lines of work finish around the same time, all wanting the same tree. The section above
lands **one** delivery; this is the six steps that land **all of them**, and it is what a
registered Clawdfather does when it stops dispatching and starts closing. Run them in order — the
ordering step is the one that is skipped, and it is the one that prevents the merge conflicts.

1. **Freeze.** Tell every live line to stop at a clean boundary and report — not to abandon what it
   is doing, and not to start anything new. Give them the report shape in the same message, because
   a freeze that returns six differently-shaped answers has to be asked twice.
2. **Inventory.** From each line, in a fixed shape, six fields and no prose:
   - **delivery branch, base, head** — what exists, and what it was built on;
   - **shared-tree paths it must write** — the ordering input for step 3;
   - **landing task id** — what gets marked `landed` at the end;
   - **which commit its verification ran against** — a green run against a stale base is not
     evidence about the tree you are about to make;
   - **who blocks it, and who it blocks.**

   Read the broker's own view beside those answers rather than instead of them: `GET
   /v1/orchestrator/tasks` for state, worktree receipt and dirtiness, `GET
   /v1/orchestrator/inflight?project=<dir>` for what else is claiming those paths.
3. **Order by contention, not by seniority and not by who waited longest.** The input is which
   files each delivery writes: two deliveries on disjoint paths need no ordering at all, and two
   that overlap need the one they both build on to go first. Where they genuinely tie, the
   tie-break that worked is **the one already rebased onto, and verified against, the newest base
   goes first** — its green run is the only one still about the tree everybody will inherit, and
   landing it makes every other line's rebase cheaper rather than dearer.
4. **Land one at a time, and verify each on the exact staged tree yourself.** Not the deliverer's
   run — yours, on the index you are about to commit, by the "Close a code delivery" steps above.
   One at a time is not caution for its own sake: it is what makes a failure attributable, because
   the only thing that changed since the last green tree is the delivery you just staged.
5. **Build**, once, at the end — after the last landing, never between them. It replaces and
   restarts the user's running app, so say so before you do it and do it from HEAD, not from the
   working tree.
6. **Resume, and ask each line for two lists.** Before anything restarts, ask every line —
   separately — for **(a) its technical next steps** and **(b) the decisions only the user can
   make.** Two questions, two lists, always. Asked as one they come back as one, and it is reliably
   the user's half that is lost inside a paragraph of to-dos. Those decisions then go to the user
   as options, one at a time, each carrying its consequence and your recommendation — the shape is
   in [`AGENTS.md`](../../AGENTS.md#decisions-that-are-the-users-go-to-the-user-as-options).

Then close: only the lines that actually landed are `landed`, everything else keeps its obligation
with a named owner, and closing your own session is the act described above under "To end one
early" — look at what it takes with it first.

### Wait for files through Clawdline

Do not coordinate a shared-tree wait with Claude Code's native messages, a Codex-specific channel,
or an assistant conversation id. The broker boundary is Clawdline:

1. Find the owning root with `GET /v1/orchestrator/sessions`, using the local orchestrator
   credential. It lists each session's terminal-neutral `id`, `assistant`, `cwd`, `label`, `state`
   and — for tabs Clawdline opened — `taskId`. Address sessions by that `id`, never a
   provider-specific `sessionId`. **`GET /v1/sessions` and `GET /v1/sessions/:id/git` are the
   paired-device API and answer the orchestrator credential with `401 unauthorized`**, so read the
   repository's own state by running `git` in the checkout instead. Resolve your *own* id by
   calling `GET /v1/orchestrator/whoami` with this assistant's exact process-bound conversation id.
   `$ITERM_SESSION_ID` is only a cached hint and can be stale after restart/resume.
   `GET /v1/orchestrator/waits` names only ids already inside a wait — it cannot reach a session
   nobody is waiting on yet, which is why the index exists. If the provider conversation id cannot
   be established, use the authenticated sessions index as a deliberate fallback for a
   terminal-addressed operation; it is not authority for `root.session_id`.
2. Register the relationship with `POST /v1/orchestrator/waits`, using the local orchestrator
   credential. Name `repository`, exact `paths`, `owner_session_id`, `waiter_session_id`, `reason`
   and `release_condition`. Clawdline canonicalizes paths, deduplicates the waiter, persists the
   group and delivers the request to the owner. Never print or copy either credential into a task.
3. After committing or explicitly releasing the paths, the owner calls
   `POST /v1/orchestrator/waits/:id/release` with its session id and the commit when one exists.
   Clawdline fans out to every waiter and receipts each successful delivery; retrying a partial
   release sends only to the recipients still pending. An abandoning waiter may cancel only itself.
4. A release notice is a wake-up signal, not proof. Every waiter re-reads target HEAD, status and
   diff before staging or integrating. The broker never infers release from a clean status sample.

An unresolved wait survives app restart and remains visible when its owner disappears.
`GET /v1/sessions` publishes it to the app's own UI as a `coordination` overlay:
`waiting_on_session`/`waitingOn` for the blocked session and `waitedOnBy` for its owner — an agent
holding the orchestrator credential reads the same relationships from `GET /v1/orchestrator/waits`.
This does **not** set the terminal state to `waiting`; that word still means a person must answer
and alone triggers the loud row and push notification.
Native and web rows quietly show `⏳ owner · release condition`. Use a final-line
`[Clawdline waiting]` marker only as fallback when that UI is unavailable.

### Becoming Clawdfather, when you are the one asked

Nothing can register the machine coordinator for you. The three coordinator routes take the
orchestrator token only, so the app's **Make this session Clawdfather** item does not call them —
it types this procedure's instruction into your session over the ordinary send route, and you carry
it out. The full text with every refusal is in [`docs/orchestrator.md`](../../docs/orchestrator.md);
this is the short form.

1. **Resolve your own terminal-neutral id** through
   `GET /v1/orchestrator/whoami?conversation_id=…`, using this assistant's exact process-bound
   conversation id. For Codex that input is `CODEX_THREAD_ID` / `CODEX_SESSION_ID`; Claude uses
   the transcript UUID from the nonce procedure above. The response's `terminal_id` is the pane
   address these routes take. Never substitute `$ITERM_SESSION_ID`: after iTerm restart/resume it
   may still name the vanished old terminal. The same resolver works under tmux; labels, cwd and
   state are not identity fallbacks. If the provider conversation id genuinely cannot be
   established, read authenticated `GET /v1/orchestrator/sessions` and deliberately select the
   live row for terminal-addressed work only; ownerless dispatch remains explicit poll-only.
2. **Read the state first** — `GET /v1/orchestrator/coordinator`, with `$TOKEN` from step 1 of this
   skill. `coordinator.configured` false means nobody holds it; otherwise `coordinator.status` is
   `online`, `offline`, or `unknown`, and `coordinator.id` and `coordinator.generation` are the pair
   a later exact-offline reconnect has to quote.
3. **Nobody configured** → `POST /v1/orchestrator/coordinator/register` with `{"session_id": …}`,
   one field and no more. `created:false` means you already were it.
4. **Configured and `unknown`** → wait, then repeat step 2. Stale, missing, untimestamped, or
   pre-binding Session evidence is not proof that the previous process is offline; do not register
   or rebind from it.
5. **Configured and exactly `offline`** → `POST /v1/orchestrator/coordinator/rebind` with
   `expected_coordinator_id`, `expected_generation` and `session_id`. The UUID survives a
   reconnect on purpose, so the generation is the compare-and-swap value. A mismatch means the
   record moved while you were reading it: go back to step 2 rather than retrying the old numbers.
6. **Configured, `online`, somebody else** → stop. Registration is never a takeover and the API
   refuses it (`coordinator_exists`, `coordinator_online`). Report who holds it and leave it there.

Then say what happened. Whoever asked is usually watching another window, and "already held by the
session in that other checkout" is as much of an answer as "registered".

### Keep the protocol page current, and know which audience you are writing for

**Documents split by audience, and the split decides where they live.** A repository's `docs/` is
what the community gets: English, written from the outside, tracked in git, safe for a test to
depend on. A private working document — an audit, a research page, a plan in the person's own
language — belongs wherever that project keeps internal material, and nothing in a public suite may
depend on it. Moving one across is a rewrite, not a copy.

In this repository the living page is `docs/clawdline-protocol.html`. Any change to task dispatch,
handoff, claims, landing closure, file waits, ownership transfer, structured notices or other
cross-session communication updates it before the protocol work closes. It is not a dated audit and
there is no dated audit beside it: it must equal now.

Have a Claude Code session read the complete authoritative protocol rather than paraphrasing the
current conversation. It updates the diagrams, state labels, source links and the checklist, then
root inspects the standalone HTML and verifies every claimed transition against `AGENTS.md`,
`docs/dispatching.md`, `docs/landing.md`, `docs/orchestrator.md`, `docs/api.md`, `docs/handoff.md`,
this skill and the machine's dispatch policy. If those sources changed and the page did not, the
landing obligation remains pending.

If this root must stop before step 5, make a Clawdline handoff rather than dropping the obligation.
Its `CURRENT STATE`, `OPEN THREADS` and `IMMEDIATE NEXT STEP` must name the delivery branch, base and
head; target branch and current head; review verdict and test receipts; overlapping paths and their
owner when known; and exactly one next landing action. A handoff is not itself landing, and the
original root remains responsible until Clawdline's receipt confirms the first line reached the
named receiving root. That receipt is the ownership-transfer point for this explicitly named
obligation; it is not evidence that the code landed.

---

## 7. Handoff — handing this line of work to the next session

The six steps above hand out a **task**. This one hands over the **line of work itself**: you write
down what this conversation knows, and a new session picks it up and carries on. The session it opens
is a new **root**, not a child — no secret, no timeout, no `result.json`, and closing this session
does not touch it.

**Triggers:** "use Clawdline Handoff", "hand this over to a fresh session", "pick this up in Codex",
"continue this tomorrow in a new session", 「使用 Clawdline Handoff」「handoff 給新 session」
「交接給下一個 session」「明天用新 session 接著做」.

**Not this, if `/compact` would do.** Same harness, same directory, an ordinary transition — compact
it. A handoff buys portability, not compression: reach for it when the work has to *move* (a phase
boundary, another harness or model, tomorrow, a parallel fork, another machine), or when the context
window is about to make the decision for you. And not for a line of work smaller than the document
that would describe it.

**If you are a child (§0), this is not your move either.** Handing a line of work on is a decision
about a root's conversation, and a child that opens a fresh root steps outside the tree it was placed
in. Report to your task and let the root decide.

Four steps.

**1. Write the document, to the eight headings.** `OBJECTIVE` · `KEY DECISIONS` (marked *do not
reopen*, each dated) · `CURRENT STATE` · `REFERENCES` · `CONSTRAINTS & PRINCIPLES` · `OPEN THREADS`
(numbered) · `IMMEDIATE NEXT STEP` (one door) · `VERIFICATION`.

Two rules do most of the work. **Do not repeat what a reference says** — point at it; a handoff that
summarises its own sources will disagree with them by Thursday. And **`VERIFICATION` is three to five
questions whose answers are deliberately not in the document**, each naming where the answer does
live: that is what makes the receiver walk the chain, and a question it cannot answer is a break found
at the cheapest possible moment. Check those pointers once before handing over — a wrong pointer looks
exactly like a broken chain.

**2. Durably archive anything volatile you cite, before citing it.** A design document living in a
session scratchpad, an artifact that exists only as a URL, a file under `/tmp`: copy it into the
repository — `artifacts/` for a record, `docs/` for a standing answer — and cite the copy. References
are not duplicated; a volatile source is the exception. This is the step people skip and the one that
breaks the chain a week later.

**3. Build the package.**

```bash
hid=$(uuidgen | tr '[:upper:]' '[:lower:]')
umask 077 && mkdir -p "/tmp/.clawdline/handoffs/$hid" && chmod 700 /tmp/.clawdline/handoffs "/tmp/.clawdline/handoffs/$hid"
echo "$hid"
```

Write `handoff.md` into that directory with your file-writing tool, and put anything the receiver
should read beside it under `attachments/` — **naming each attachment in `REFERENCES`**, by its path
relative to `handoff.md`. An attachment no reference names is one the receiver never opens: the line
it is given points at `handoff.md` and at nothing else. No secret, no token, nothing else — there is
no credential in a handoff.

**4. Open the session.**

```bash
curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/handoffs" \
  -H "X-Clawdline-Orchestrator: $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"handoff_id\":\"$hid\",\"project_dir\":\"$PWD\",\"title\":\"Cloud planning line\",\"from_session\":\"$ROOT_SESSION\"}"
```

`assistant` (`claude` / `codex`; absent is `claude`) and `model` are optional. `title` names the tab
— without it the tab is `handoff` and the first eight characters of the id — and `from_session` is
where the receipt line goes: whatever this session's own id is, ≤ 200 characters, unrecognised is the
same as absent. Both are best-effort, because **the app will not open `handoff.md` to work either of
them out**. Branch on `code` as in §5: `forbidden`, `orchestrator_disabled` (the switch in Settings
covers handoffs too), `bad_request`, `bad_task` (a bad field, or a package directory or `handoff.md`
that is not there), `rate_limited` — the same brake dispatch uses, and a refusal spends a slot of it
— and `not_found`, meaning this build has no handoff route.

On `not_found`, finish steps 1–3 and give the user the canonical sentence from
[`docs/handoff.md` § “The line”](../../docs/handoff.md#the-line) verbatim to paste themselves:
`You are picking up a Clawdline handoff. Read /tmp/.clawdline/handoffs/<id>/handoff.md before anything else and follow it: walk its REFERENCES, answer its VERIFICATION questions from those sources, say plainly what you could not reach, then continue from OPEN THREADS.`

Then tell the user what went where in two lines — what the handoff covers, and the path. If you are
carrying on yourself rather than stopping, say so: **a handoff is a copy, not an ending.** Two roots
in one working tree means `claims` on every dispatch either of you makes, and the tree's own rules
([`AGENTS.md`](../../AGENTS.md)) reach the new session on arrival.

The protocol in full — the package layout, why the app never reads the document, what the receiving
session owes, the route's validation and refusals — is [`docs/handoff.md`](../../docs/handoff.md).

---

## 8. Schedule — dispatching a task template on a clock

Use this only when the user wants Clawdline itself to dispatch recurring work. Write one strict
JSON file at `~/.config/clawdline/schedules/<lower-case-uuid>.json`; start from the complete schema
in [`docs/schedules.md`](../../docs/schedules.md), and do not invent fields. `when.at` is the Mac's
local `HH:MM`; `days` is `daily` or weekday names. Choose `close_tab` deliberately: `on_success`
keeps a failed tab for takeover, `always` closes every outcome, and `never` keeps the existing
orchestrator linger behavior.

After writing the file, validate it through the read route: find its `id`, or stop and report the
row whose `state` is `invalid` and read its `error`. Then manually run a valid schedule once:

```bash
curl -s "http://127.0.0.1:$PORT/v1/orchestrator/schedules" \
  -H "X-Clawdline-Orchestrator: $TOKEN"
curl -s -X POST \
  "http://127.0.0.1:$PORT/v1/orchestrator/schedules/<schedule-id>/run" \
  -H "X-Clawdline-Orchestrator: $TOKEN"
```

Read the dispatch response and then the task record; do not call installation verified merely
because the file exists. Tell the user the honest boundary: if the app is closed it cannot fire,
and restart catches up only inside `catch_up_hours`.

For a task whose useful output **is one timely sentence** — for example, a daily weather
forecast — say explicitly in its instructions to use the task-secret `/notify` route that
`CHILD.md` provides. Routine results still belong in `result.json`; push is for the rare answer
the user is waiting for. A local root script may instead `POST /v1/orchestrator/notify` with the
orchestrator token and `{"title":"…","body":"…"}`. Titles are at most 80 characters, bodies
500; one task gets 5 messages and the whole Mac gets 30 per hour. A task secret may notify while
the task is live and for only 60 seconds after it finishes, so notification-shaped work sends the
content before writing its final `result.json` whenever practical.
The user may turn agent notifications off in Settings → Remote. A
`409 agent_notify_disabled` response means the content was not delivered and no allowance was
spent: do not retry, keep the content in `result.json`, report the refusal honestly, and let the
scheduled task otherwise finish normally.

---

## A worked example — asking codex to draw this project

The user says: "get codex to draw me a picture of this project, medieval manuscript style."

**Call A** — check the environment, make the directory, leave the nonce (the secret is not needed
yet):

```bash
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
TOKEN=$(cat ~/.config/clawdline/orchestrator-token)
task_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
umask 077 && mkdir -p "/tmp/.clawdline/$task_id/artifacts" && chmod 700 /tmp/.clawdline "/tmp/.clawdline/$task_id"
echo "clawdline-nonce-$task_id"
echo "task_id=$task_id"
```

**Call B** — fish out the session id, make the secret, write task.json, dispatch (substituting the
`task_id` call A printed):

```bash
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
TOKEN=$(cat ~/.config/clawdline/orchestrator-token)
task_id=<the id call A printed>
secret=$(openssl rand -hex 32)

if [ -n "${CODEX_THREAD_ID:-${CODEX_SESSION_ID:-}}" ]; then
  ROOT_ASSISTANT=codex
  ROOT_SESSION="${CODEX_THREAD_ID:-${CODEX_SESSION_ID:-}}"
else
  ROOT_ASSISTANT=claude
  slug=$(printf '%s' "$PWD" | sed 's/[^a-zA-Z0-9]/-/g')
  f=$(grep -l "clawdline-nonce-$task_id" "$HOME/.claude/projects/$slug/"*.jsonl 2>/dev/null | head -1)
  ROOT_SESSION=$(if [ -n "$f" ]; then basename "$f" .jsonl; fi)
fi

if [ -z "$ROOT_SESSION" ]; then
  echo "refusing detached dispatch: current root conversation id was not found" >&2
  exit 2
fi

jq -n --arg id "$task_id" --arg dir "$PWD" --arg created "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      --arg rs "$ROOT_SESSION" --arg ra "$ROOT_ASSISTANT" \
  '{clawdline_protocol:1, task_id:$id, kind:"image", assistant:"codex",
    project_dir:$dir, title:"Project portrait, medieval manuscript style",
    instructions:"You are in /Users/you/code/clawdline, a macOS menu bar app that watches the Claude Code and Codex sessions in the terminal and draws their state in the menu bar, the notch and a floating panel. Read README.md and docs/interface.md first to understand what it does, then draw one image that stands for this project: medieval illuminated manuscript style, decorative border, hand-drawn strokes, highly artistic. Use your built-in image_gen tool, landscape, high quality. image_gen writes to ~/.codex/generated_images/<session>/ and cannot be told a destination, so when it is done copy that PNG to /tmp/.clawdline/<TASK_ID>/artifacts/project-portrait.png and confirm the file is there with ls -la. Then write result.json as CHILD.md describes.",
    deliverables:["artifacts/project-portrait.png"], timeout_minutes:30, created_at:$created,
    root:{session_id:(if $rs=="" then null else $rs end), assistant:$ra,
          project_dir:$dir, label:"clawdline root"}}' \
  > "/tmp/.clawdline/$task_id/task.json"

curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
  -H "X-Clawdline-Orchestrator: $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"task_id\":\"$task_id\",\"secret\":\"$secret\"}"
```

(Remember to replace `<TASK_ID>` inside `instructions` with the real id — the child reads it
literally. Measured: split across two calls like this, the session id is found first time; crammed
into one call it is always empty.)

Then say to the user:

> Sent: **Project portrait, medieval manuscript style** (`3f9a21bc`, codex, 30 minute limit).
> It will draw with codex's built-in image model and copy the PNG to
> `/tmp/.clawdline/3f9a21bc-…/artifacts/project-portrait.png`.
> A line will come in when it finishes and I will look it over then.

If the ask had been a diagram or an icon rather than an illustration, the same task would ask for a
hand-written SVG instead — see §2.5.

---

## Things that catch people out

- **The secret appears once, in the dispatch body.** It stays in your transcript (`0600`, the same
  trust boundary as the token file), but it must **never** reach `task.json`, `CHILD.md`, or
  anywhere else a child can read.
- **`/tmp/.clawdline` is not your shared workspace.** Touch your own task's directory and nothing
  else; do not read other tasks.
- **One thing per child.** "And run the tests while you're at it" in the same `instructions` gives
  one child two goals, and when it fails you cannot tell which half broke.
- **Leaves finishing is not the graph finishing.** If the graph has a joining node, it can only be
  dispatched once every leaf's `result.json` is there, and its `instructions` must name the exact
  `/tmp/.clawdline/<id>/artifacts/` directories to read. That ordering is root's job; the app does
  not sequence anything for you.
- **The reviewer finishing is not the code finishing.** `SAFE TO LAND` creates a root-owned pending
  landing obligation. Close it on the named target and verify the integrated tree, or hand that
  obligation to a named root; never translate it into "done" by itself.
- **A hunk-reviewed index must be committed without pathspecs.** After `git add -p` and staged-tree
  verification, use plain `git commit -m <message>`. `git commit -- <path>...` takes the named files
  from the worktree and can absorb foreign unstaged hunks that the reviewed index excluded. Use a
  pathspec only when every worktree hunk in every named file belongs to the delivery.
- **File waiters are Clawdline relationships.** Request and release through Clawdline session ids,
  notify every waiter when paths are released, and revalidate after notification. Never make this
  safety protocol depend on Claude Code or Codex messaging.
- **The protocol page is part of the protocol.** Any communication-semantics change updates the
  repository's public protocol page — `docs/clawdline-protocol.html` here — and verifies it against
  every authoritative source before completion. A private working document is not a substitute: it
  is the tracked, English one that anybody can check.
- **Assume anything new in the working tree is not the child's.** Several sessions usually share
  one checkout on this Mac, and they are editing and committing too. If `git status` grows a few
  files after you dispatch, or `git log` grows an entry, **that is not the child's report card**.
  Before crediting anything to a child, do these three:

  ```bash
  git log --format='%h %ad %s' --date=format:'%H:%M' -5   # do the times line up with your dispatch?
  git diff --stat                                          # which files moved
  git diff -- <a file> | grep '^+' | head -20              # is the subject of the change your task?
  ```

  **Read the content, not the filenames.** The test is whether what this change is about is the
  task you handed out. A child sent to build feature A does not casually produce feature B — if you
  are looking at B, it is almost certainly somebody else's.

  Getting this wrong is expensive: crediting somebody else's work to a child gives you a wrong
  review, may have you `git checkout` away half an hour of a colleague's work, and leaves you with
  a completely wrong impression of what that child can do. It cuts the other way too — a child's
  half-finished edits can get swept into another session's commit.
- **A child does not commit; root does.** Say so in the instructions: no `git commit` / `stash` /
  `reset` / `checkout`. They run in a shared working tree, and one `git reset --hard` takes
  everybody else's work with it.
- **A child and a root are asking different questions, so they verify differently.** A child asks
  "does what I wrote work?" — the working tree is the right subject, other sessions' half-finished
  edits included, because a child does not commit and their mess cannot reach HEAD through it. Put
  this in the instructions verbatim when the child will run tests:

  ```bash
  snapshot_dir=$(mktemp -d); test_tmp=$(mktemp -d)
  git archive HEAD | tar -x -C "$snapshot_dir"
  git diff --binary --full-index --no-ext-diff HEAD \
    | (cd "$snapshot_dir" && git apply --allow-empty --whitespace=nowarn)
  git ls-files --others --exclude-standard -z \
    | tar --null -T - -cf - | tar -xf - -C "$snapshot_dir"
  (cd "$snapshot_dir" && TMPDIR="$test_tmp" ./test.sh)
  ```

  **Three commands and not one, because the one-liner was wrong twice.** `git archive "$(git stash
  create)"` reads correctly and fails silently: on a **clean tree** `git stash create` exits 0 and
  prints an **empty string**, so a `|| echo HEAD` fallback never fires, `git archive ""` unpacks
  nothing, `./test.sh` exits 127 and the run ends with zero failures. That green ran nothing, and it
  aims straight at read-only reviewers, whose tree is always clean. Second, a stash object carries
  only *tracked* changes, so a test another session has added but not committed is missing from the
  snapshot while the `test.sh` that calls it is not: the suite stays green and that test never ran.
  Replaying `git diff HEAD` onto a `HEAD` archive and overlaying the untracked files covers a clean
  tree, a dirty one and a new file, and writes no object into `.git` — which matters in a linked
  worktree, where a Codex sandbox may not write there at all. **Never tell a child to use `git
  write-tree`** — that reads the
  *index*, so it has to stage first, and the index is shared: the child sweeps up whatever another
  session left in there, and then a root commits it. That has happened here. `write-tree` is root's
  tool, because root is staging anyway and "will HEAD still build after this commit?" is a question
  only the index can answer.
- **The house rules are the user's, not yours.** Where `~/.config/clawdline/dispatch-policy.md`
  disagrees with your judgement, follow it, and say which rule you followed when you report back.
- **A child opens a real terminal tab and runs real commands.** Dispatching is authorising it to
  act inside that `project_dir`. When a task could touch something that matters, confirm with the
  user once first.
- **`project_dir` has to be a directory this Mac already trusts.** The first time Claude Code
  starts in a folder it asks "Do you trust this folder?" — that is not a permission prompt, it is a
  door before startup, and `permission_mode` does not reach it. The child sits on that screen, the
  app cannot type into it, and two minutes later it is `spawn_failed` with nothing on screen but a
  menu that makes no sense. To dispatch into a new directory, ask the user to open claude there by
  hand once first.
- **Do not end your own session while a task you dispatched is still out.** The moment your
  session ends, everything you sent out is collected with it — your finish is their deadline. A
  child has nothing out to wait for: it dispatches nothing, which is what §0 is for.
- The full protocol — state machine, file formats, API, how cost is counted — is in
  [`docs/orchestrator.md`](../../docs/orchestrator.md).
