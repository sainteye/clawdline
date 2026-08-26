---
name: clawdline
version: 2.1.0
description: |
  Hand a piece of work to another session: open a child session (Claude Code or Codex) through the
  Clawdline app, type the first message into it, wait for it to write result.json, and report back
  when it lands. For work that does not need the conversation it was asked in — draw this, run the
  suite, review a diff, a long tidying job. Also hands over a whole line of work — a Clawdline
  handoff: write this conversation's state to a document and open a session that continues it (§7).
  Triggers on: "dispatch a task", "get codex to do it", "open a child session", "do X in the
  background", "run this in another session", "have codex draw an image", "get another agent to
  review this", or several unrelated things wanted at once; and for the handoff half, "use Clawdline
  Handoff", "hand this over to a fresh session", "pick this up in Codex", "continue this tomorrow in
  a new session".
  Also triggers on the same asks in Chinese: 「派任務」「派給 codex 做」「開一個 child／子 session」
  「背景幫我做 X」「dispatch 一個任務」「另開一個 session 去跑」「用 codex 生一張圖」「叫另一個 agent 去審」
  「使用 Clawdline Handoff」「handoff 給新 session」「交接給下一個 session」「明天用新 session 接著做」.
  Does not trigger on: anything this conversation can simply do (a child costs far more than doing
  it), search and analysis a Task/subagent already covers (that is a subagent, not a Clawdline
  child), or wanting to know which sessions are running (that is the Clawdline panel, or
  GET /v1/sessions). **When this session is itself a child, CHILD.md governs and this file does
  not** — see §0.
user-invocable: true
last-updated: 2026-08-26
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

## 0. Work out which level you are on — this decides whether you may dispatch at all

**Any one of these means you are a child:**

- The **first message** of this conversation is `You are a Clawdline CHILD agent for task <id>…`
- You have read, or been asked to read, `/tmp/.clawdline/<id>/CHILD.md`
- You are holding a `TASK_SECRET=`

**If you are a child, this skill is not what governs you — `CHILD.md` is.** Read its "Handing work
on" section:

- **The section is there** → follow it. It already spells the whole thing out for you, including
  the one field nobody else can fill in: `root.parent_task`, which is the id of your own task.
  §1–§6 below are the long version of the same thing and are fine to consult, but where they
  disagree, `CHILD.md` wins.
- **The section is not there** → you are the floor. **Stop.** Tell the user this session is at the
  bottom of the tree and cannot dispatch any further, then do the work yourself.

The tree is two levels deep: the user's session opens children, those children open one more
level, and that is the end of it. Without a floor, one job becomes five becomes twenty-five and a
Mac runs out of terminals. The app does enforce it (dispatch answers `depth_exceeded`), but that
is the last line of defence, not the first — **the first is you.**

---

## 1. Find the port and the token

```bash
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
TOKEN=$(cat ~/.config/clawdline/orchestrator-token 2>/dev/null)
[ -n "$TOKEN" ] || echo "NO TOKEN"
curl -s "http://127.0.0.1:$PORT/v1/health"
```

- `NO TOKEN` / no such file → **stop** and tell the user: Clawdline is not running, or it is too
  old to have the orchestrator. Ask them to open Clawdline and turn on **Answer over HTTP** in
  Settings → Remote; the token writes itself when the server comes up.
- `curl` cannot connect → same thing, the server is not running.
- health answers but there is no token file → this copy of Clawdline does not have the feature.
  Ask them to update.

**That token is the proof that you are a local process running as them.** Do not write it to a
file, do not hand it to a child, do not put it anywhere under `/tmp`, and do not paste it into a
reply.

---

## 2. Draw the whole graph before deciding who gets what

### 2.0 Read the policy, then answer whether this should be dispatched at all

**Before every dispatch, the first thing you do is read this Mac's policy file:**

```bash
cat ~/.config/clawdline/dispatch-policy.md 2>/dev/null
```

The app ships that file with contents in it and **the user keeps editing it**, so it grows along
with what this house knows. **It outranks anything in this skill.** Where the two disagree, follow
the policy and say which rule you followed when you report back. Only when the file is missing or
empty do the defaults here apply. The user edits it under Settings → Remote → "How work is handed
out", and you can edit it for them when they ask.

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

### 2.0a Decide whether the task needs a private worktree

Use `"isolation":"worktree"` for code changes that can be reviewed and landed as a Git branch.
The broker creates a clean private checkout when the task actually starts; optional
`isolation_base` names its Git revision, otherwise the then-current `HEAD` is used. Do not choose it
for reviewers and other reading-only work, artifact-only output, work that needs untracked local
state, or operations whose real collision is a running app, port, device, database, cache, or fixed
build destination. Use `serialize` for those machine-global collisions.

This changes the child rule narrowly. In a shared checkout a child still never commits. In its own
worktree it should commit early and only on `clawdline/task/<complete-task-id>`; it must not push,
switch branches, merge, rebase, stash, hard-reset, invoke `git worktree`, or run a machine-global
installer such as `build.sh`. **The delivery is that branch, not the checkout directory and not an
artifact diff.** The root reviews it with `git diff <base>...clawdline/task/<id>` and lands it by
merge or cherry-pick. Worktree isolation protects tracked files only; it does not copy ignored
dependencies, caches, or env files.

### 2.1 If it is going out, pick a shape

The policy file's "pick a shape" section names a few. **Pick one; do not improvise.** In short:

| Shape | When | How the nodes go |
|---|---|---|
| **Split and join** | one question, several independent pieces | leaves on `haiku` each take a piece, one `sonnet`+ node joins and judges |
| **Build then read** | **output is code, or a decision somebody will act on** | a few nodes build, **plus one separate review node that only reads** |
| **Decide then do** | something important is being changed | one node writes the plan and touches nothing → **a person passes it** → another node (usually codex) implements |
| **Batch with takeover** | the same mechanical change across independent modules | one node per module; a dead tab keeps its state for a person to finish |
| **Candidates** | a design trade-off where what is being compared is taste | several nodes each produce a complete answer, **a person picks, no judge node** |

**New features are always Build then read.** Any graph producing code ends with an independent
review node — the rules are at the end of §2.2.

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

**When the output is code, or a decision somebody will act on, the graph ends in a review node.**
Not a fifth worker — a reader: it reads what the others produced, writes down what is wrong, and
does not fix anything (fixing belongs to the next round or to a person, and that holds even when
it is sure it knows the fix — a repair quietly buries the judgement somebody needed to see). Five
rules:

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
that number is **counted per session, not per Mac**. If you are yourself a child, your allowance is
3 (`orchestrator_max_grandchildren`, 0…10; `0` means you may not dispatch at all). Over the line
comes back as `over_capacity`.

### 2.4 Which assistant

If the user named one, use it. Otherwise:

| | Give it | Because |
|---|---|---|
| **codex** | writing code, **generating images**, hand-written SVG, running a build until it goes green, mechanical edits across many files | it is good at *making a thing you can then look at*, and it bills against a plan rather than per token |
| **claude** | reviewing a diff, reading code to work out why, searching and weighing what it found, prose somebody will read | it is good at *reading and judging* |

**Codex's sandbox blocks outbound connections by default** — do not give it work that needs the
open web; it will stall on an approval or simply fail. (Image generation is not affected; see
§2.5.)

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
| `sonnet` | ordinary work with judgement in it; the default choice for a leaf |
| `opus` | a decision somebody will act on without checking, and **any node joining several children's answers** |

**A review runs on a model no weaker than what produced the thing.** Judging a big model's output
with a small one is a rubber stamp with a token cost.

On the Codex side the same field takes its slug (e.g. `gpt-5.1-codex`).

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
is the entirety of handing work on. No flag covers those short of `full`. It does not widen *who*
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
jq -n \
  --arg id "$task_id" \
  --arg kind "image" \
  --arg assistant "codex" \
  --arg dir "$PWD" \
  --arg title "Project portrait" \
  --arg instructions "…the full briefing…" \
  --arg created "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg root_session "$ROOT_SESSION" \
  --arg root_label "clawdline root session" \
  --arg model "haiku" \
  --arg plan "$PLAN" \
  '{clawdline_protocol:1, task_id:$id, kind:$kind, assistant:$assistant, model:$model,
    permission_mode:"full",
    isolation:"none", project_dir:$dir, title:$title, instructions:$instructions, plan:$plan,
    deliverables:["artifacts/out.png"], timeout_minutes:30, created_at:$created,
    root:{session_id:(if $root_session=="" then null else $root_session end),
          assistant:"claude", project_dir:$dir, label:$root_label}}' \
  > "/tmp/.clawdline/$task_id/task.json"
```

`$PLAN` is the graph from §2.1, and **every task in the batch carries the same one** — it is the
whole picture, not this node's description of itself.

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
| `model` | optional. Lowercase letters, digits, `.` `_` `-`, ≤ 64 characters. Absent = that assistant's default |
| `permission_mode` | optional. `ask` / `edits` / `full`. Absent = this Mac's ceiling (default `full`). Anything else, `auto` included, is `bad_task` |
| `isolation` | optional. `none` / `worktree`; absent = `none`. Use `worktree` only after the §2.0a decision |
| `isolation_base` | optional Git revision, legal only with `isolation: "worktree"`; absent means `HEAD` at actual start time |
| `plan` | optional but **strongly recommended**: the whole graph, ≤ 4 KiB. Identical across the batch |
| `timeout_minutes` | 1…240, 30 if absent |
| `root.session_id` | found with the trick below; `null` if you cannot find it — never invented |
| `root.parent_task` | **only when you are yourself a child** — the id of your own task, the one in your first message. Root dispatches leave it out. Getting it wrong bills this task to somebody else or counts it as deeper than it is; there is nothing to gain |

### Finding your own session id (best-effort; `null` if you cannot)

Claude Code has no way to ask "who am I", so leave a nonce and fish it out of your own transcript.
**This has to be split across two tool calls** — a call's command text is only written to the
transcript *after that call ends*, so echoing a nonce and grepping for it in the same call never
finds anything (measured; retries do not help):

```bash
# Call A (the same call as step 3 is fine): leave the nonce in the transcript
echo "clawdline-nonce-$task_id"
```

```bash
# Call B (the next tool call, together with steps 4 and 5): now it is findable
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

If call B still comes back empty, try once more a call later; if it is still empty, put `null` and
move on rather than getting stuck here.

**This is worth two things, and the second one is easy to forget:** one, the app needs to know
which terminal to notify when the task finishes; two, **the child's row in the list is indented
under you because of this id**. With `null` the task still runs, but you have to poll for the
finish yourself, and that row floats in the middle of the list marked `Child` with nobody above it,
looking like the grouping is broken. Use `null` when you cannot find it — never a guess — but when
you can find it, fill it in. `ROOT_SESSION` has to be in the same bash call as step 4's `jq`, or
the variable will not survive; alternatively paste the string straight into `--arg`.

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
| `forbidden` | wrong token or none | re-read the token file; still failing means the app regenerated it, so ask the user to restart Clawdline |
| `rate_limited` | more than 10 dispatches in 10 minutes | wait for the window to roll |
| `not_found` | no such route | this Clawdline has no orchestrator; ask the user to update |

**Resending the same `task_id` is safe** — you get the record that already exists, not a second
tab. So a retry after a timeout is just a resend; there is no need for an `Idempotency-Key`.

---

## 6. Report, then wait

As soon as it is out, tell the user: **how many, what each is, who is doing it, and where the
output will be.** One line each; the first 8 characters of the `task_id` is enough.

Completion arrives one of two ways, and you do not have to choose:

1. **You get told** — the app types a line into your terminal:
   ```
   [clawdline] task 3f9a21bc (Project portrait) finished: success — see /tmp/.clawdline/<id>/result.json
   ```
   When you see it, read `result.json` and `artifacts/`, then tell the user what came back.
2. **You poll** — when `root.session_id` came back empty, or the user wants to know now:

```bash
curl -s "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$task_id" \
  -H "X-Clawdline-Orchestrator: $TOKEN" | jq '.task | {state, summary, artifacts, usage}'
```

`state` runs `queued → spawning → briefed → success | failure | timeout | cancelled |
spawn_failed`. **`briefed` means the child is working**; it can sit there a long while and that is
not a stall.

**Do not open a while loop and wait in it.** Check now and then, and when the user asks; children
routinely run for ten or twenty minutes, and tying the root session to a poll is the most expensive
way to use this.

To end one early:

```bash
curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$task_id/cancel" \
  -H "X-Clawdline-Orchestrator: $TOKEN"
```

Reading the result:

```bash
cat "/tmp/.clawdline/$task_id/result.json"
ls -la "/tmp/.clawdline/$task_id/artifacts/"
```

`summary` in `result.json` is a sentence the child wrote itself, and `artifacts` is what it
*claims* it produced — **a claim is a claim; `ls` the directory yourself**. Task directories are
cleared 24 hours after they finish, so anything the user wants to keep has to be copied out.

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

slug=$(printf '%s' "$PWD" | sed 's/[^a-zA-Z0-9]/-/g')
f=$(grep -l "clawdline-nonce-$task_id" "$HOME/.claude/projects/$slug/"*.jsonl 2>/dev/null | head -1)
ROOT_SESSION=$(if [ -n "$f" ]; then basename "$f" .jsonl; fi)

jq -n --arg id "$task_id" --arg dir "$PWD" --arg created "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      --arg rs "$ROOT_SESSION" \
  '{clawdline_protocol:1, task_id:$id, kind:"image", assistant:"codex",
    project_dir:$dir, title:"Project portrait, medieval manuscript style",
    instructions:"You are in /Users/you/code/clawdline, a macOS menu bar app that watches the Claude Code and Codex sessions in the terminal and draws their state in the menu bar, the notch and a floating panel. Read README.md and docs/interface.md first to understand what it does, then draw one image that stands for this project: medieval illuminated manuscript style, decorative border, hand-drawn strokes, highly artistic. Use your built-in image_gen tool, landscape, high quality. image_gen writes to ~/.codex/generated_images/<session>/ and cannot be told a destination, so when it is done copy that PNG to /tmp/.clawdline/<TASK_ID>/artifacts/project-portrait.png and confirm the file is there with ls -la. Then write result.json as CHILD.md describes.",
    deliverables:["artifacts/project-portrait.png"], timeout_minutes:30, created_at:$created,
    root:{session_id:(if $rs=="" then null else $rs end), assistant:"claude",
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
- **If you are a child that dispatched further, wait for those before writing your own
  `result.json`.** The moment your task ends, everything you sent out is collected with it — your
  finish is their deadline.
- The full protocol — state machine, file formats, API, how cost is counted — is in
  [`docs/orchestrator.md`](../../docs/orchestrator.md).
