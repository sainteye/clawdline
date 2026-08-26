# The four gates a dispatched session stops at

**A dispatched session's tab has nobody watching it.** So the usual intuition about permissions
inverts here: "ask about everything" is not the careful setting, it is the one where the work
silently does not happen and nobody finds out why. A session that stops for approval on that tab
does not stop for a moment — it stops until the task times out, and afterwards the record says
`timeout` about work that was one keystroke from starting.

[`docs/orchestrator.md`](orchestrator.md) is the protocol: the routes, the file formats, the state
machine. This page is the other half — the four places a child actually stops, what reaches each
one, and which intuitions turned out to be wrong when they were measured. Everything below was read
back off a real terminal on 2026-08-25, against Claude Code v2.1.245.

---

## The four gates

From the inside out. `permission_mode` reaches the first two. **It does not reach the last two** —
those stand in front of the session rather than inside it.

### 1. Reading outside the working directory

A child's working directory is its `project_dir` in shared-tree mode, or its matching private
checkout/subdirectory with `isolation: "worktree"`; its entire task still lives in
`/tmp/.clawdline/<id>/`. So the **first thing it ever does** — open its own `CHILD.md` — is a read
outside the tree it was started in, and it stops to ask about it.

`--add-dir` is the fix, and both CLIs spell it the same way. How much to grant depends on whether
the child may dispatch in turn:

| the child | gets | why |
|---|---|---|
| a leaf | `/tmp/.clawdline/<its own id>` | it only ever touches its own directory |
| one that may dispatch | `/tmp/.clawdline` | its grandchildren's directories are named by a `uuidgen` it has not run yet, so no per-task grant can name them in advance |

### 2. Writing outside the working directory

`--add-dir` opens reading, not writing. A child still stops on its own report:
`Do you want to create result.json.tmp?`

Reached by `--permission-mode acceptEdits` and above.

### 3. Command screening — **no "always allow"**

Two shapes get refused on their form alone, regardless of what they actually do:

| the line | what it is told |
|---|---|
| `jq -n '{…}'` | `Contains brace with quote character (expansion obfuscation)` |
| `… > f.tmp && mv f.tmp f` | `Contains shell syntax (string) that cannot be statically analyzed` |

Both prompts offer only *yes* and *no* — there is no "and always allow this", so a child hits the
same wall on every attempt. Either write the lines differently ([below](#how-a-child-should-write-a-file))
or run at `bypassPermissions`.

### 4. The trust prompt — **no setting reaches it; Clawdline answers it once**

A `project_dir` this Mac has never run that assistant in gets *"Do you trust this folder?"* before
the session will take a first message. It is asked before the session exists in any sense a
permission mode could apply to. Clawdline recognises this specific menu, sends option `1` once per
task, and writes `orchestrator.menu` to the audit log. A probe in a genuinely new checkout reached
its briefing without a person touching the dialog. Any different menu remains unanswered.

### What follows from the four

`orchestrator_permission` defaults to `full` — `bypassPermissions` — and that is a measurement
rather than a convenience. A dispatched session's whole job *is* running commands and writing
files, so every stop short of the last one stops it somewhere: `ask` on the first thing it does,
`edits` past writing a result but not past `cat`, `mkdir`, `curl` or `sleep`, which is most of
what handing work on consists of.

**What this does not widen is who may dispatch.** That is still a `0600` file only a local process
can read, and a child already has a shell. The setting changes how many buttons a person has to
press for work they already authorised, not what that work can reach.

---

## The flag table

Read off the status line, both with and without the flag, in the same directory.
**The values `--help` lists are not the values that work.**

| `--permission-mode` | Haiku 4.5 | Sonnet 5 / Opus 5 |
|---|---|---|
| *(absent)* | `manual` | `manual` |
| `auto` | **`manual`** | `⏵⏵ auto mode on` |
| `acceptEdits` | `⏵⏵ accept edits on` | `⏵⏵ accept edits on` |
| `bypassPermissions` | `⏵⏵ bypass permissions on` | `⏵⏵ bypass permissions on` |
| `dontAsk` | `⏵⏵ don't ask on` | `⏵⏵ don't ask on` |

**`auto` is the expensive square.** On Haiku it silently becomes `manual` — everything asked —
with no error and no warning. It is not a broken value, it is a **model-dependent** one, and that
is disqualifying for a field a task fills in: a word that quietly becomes the *strictest* setting
on the cheapest model produces a task that runs its full timeout having done nothing. Clawdline's
`Permission` therefore has three values and no `auto`; each of the three behaves identically on
every model tried.

Two things that will mislead you while checking this:

- **A test with no control proves nothing.** This bug survived one round of verification because
  the check was run in a different directory, which was *remembering its own mode* — the status
  line said `auto mode on` and the flag had nothing to do with it. Start two sessions side by
  side, same directory, one with the flag and one without.
- **The transcript does not record the effective mode.** A session started with
  `--permission-mode acceptEdits` still writes
  `{"type":"permission-mode","permissionMode":"default"}`. Only the status line is the answer.

---

## How a child should write a file

The direct consequence of gate 3. These are the two most load-bearing instructions in a briefing —
how to report, and how to hand work on — which makes them the two most likely places for a child to
stop dead one step from finished.

```bash
# Refused on shape, every time, with no way to allow it permanently:
jq -n --arg id "$sub" '{clawdline_protocol:1, task_id:$id}' > task.json
jq -n '{…}' > result.json.tmp && mv result.json.tmp result.json

# A heredoc is not refused:
cat > /tmp/.clawdline/$sub/task.json <<JSON
{"clawdline_protocol": 1,
 "task_id": "$sub",
 "assistant": "claude",
 "root": {"parent_task": "<the child's own task id>"}}
JSON
```

And `result.json` is written with the assistant's **file-writing tool**, never a shell line.
Atomicity is not the child's problem to solve: a half-written file fails to parse and is read
again on the next beat, which is what the beat is for.

---

## The two ways a spawn dies

Both answer `spawn_failed`, and **neither can be retried under the same task id** — that id is
terminal, and re-sending it returns the record rather than opening anything. Use a fresh uuid and
a fresh secret.

| the message | what happened | what to do |
|---|---|---|
| `the app restarted before the child was briefed` | Somebody restarted Clawdline in the second between the tab opening and the first message being typed. The plaintext secret lives only in the old process's memory. **A briefed task survives a restart; one still opening its tab does not.** | Re-dispatch. `build.sh` now prints any task caught in that window before it restarts anything. |
| `did not reach a prompt within 4 minutes` | The tab opened but never painted a composer. Usually not a fault — **several sessions starting at once, competing for one Mac**, which is what a two-level dispatch does by definition. A status line that shells out to git or a CI API before painting makes it slower still. | Re-dispatch. The window was two minutes, which was enough for one session and not for four. |

---

## When a dispatch fails, three things follow

**A failed spawn used to leave a live assistant behind.** The tab was kept — the rule being that
whatever went wrong is written on the screen — but a spawn that never reached briefing has nothing
of the task on its screen at all. Meanwhile it holds a slot, and *the usual reason a tab fails to
reach a prompt is that too many sessions were starting at once*. So the failure fed itself: four
dead tabs still running, and the next two spawns failing for the reason the first four had. Those
tabs now close immediately. A `timeout` still keeps its screen, which is the case where something
is actually written on it.

**A rebuild has to wait for a tab that is mid-spawn.** `build.sh` asks the running app whether any
task is `queued` or `spawning` and waits up to ninety seconds before restarting. Printing a warning
was tried first and does not work on a shared machine: whoever runs `./build.sh` there is usually
another agent, and it does not stop to read a line it did not ask for. Twice in one afternoon a
rebuild landed in the second between a tab opening and its first message being typed.

**A child that gave up on dispatching and did the work itself has to say so.** One two-level task
came back `success` saying "the European group and the Asian group each completed their review".
Both grandchildren were `spawn_failed` and had never run a turn — the parent had done all of it
alone, and done it correctly. Falling back like that is the right call; the answer is what was
asked for, not who produced it. But whoever reads the summary is deciding how much to trust the
result, and *both halves reported* and *both halves failed and I did it myself* are different
amounts of evidence behind the same answer. `CHILD.md` now asks for that sentence.

---

## Verifying that a child was never asked

**Only by watching the screen while it runs.** Match `Do you want to proceed?` or `Allow command`
against a live capture.

- **The transcript cannot tell you.** Asked-then-approved and never-asked are written down
  identically; only a *refusal* leaves a mark.
- **The child's own report cannot tell you either.** It does not know somebody pressed a key on its
  behalf — from inside, the tool simply ran.
- **Do not match on `❯ 1.`** An ordinary options menu looks exactly the same. A real permission gate
  always either says `Do you want to proceed?` or names the command it wants to run.

---

## Sharing a working tree

Several sessions on this Mac work in the same checkout, and they are also editing files and making
commits.

**After dispatching, do not assume that new files or new commits are the child's.** The test is
whether the *subject* of a change is the task you handed out — read the content, not the filenames.
A child sent to do A does not incidentally produce a B.

```bash
git log --format='%h %ad %s' --date=format:'%H:%M' -5   # does the time line up with the dispatch?
git diff --stat                                          # which files moved
git diff -- <file> | grep '^+' | head -20                # is this the subject you asked for?
```

Getting this wrong is expensive in both directions: another session's 376-line feature was once
read as a child's scope creep, which would have produced a completely wrong review; and a child's
half-finished edits can be swept into somebody else's commit.

So, in the shared checkout: **children do not commit, roots do.** Instructions should forbid
`git commit`, `stash`, `reset` and `checkout` outright — and `./build.sh`, which restarts the app
and kills whatever child somebody else is mid-spawn on.

`isolation: "worktree"` is the narrow exception. That child owns one checkout and one branch named
`clawdline/task/<task-id>`; it commits early and only there, and the root reviews and lands the
branch. It still must not push, switch branches, rebase, merge, stash, hard-reset, invoke
`git worktree`, point git at the base repository, or run `./build.sh`. This is a briefing rule,
not shell enforcement. The separate cwd also reduces Codex rollout candidates, making its
filesystem-based identity match less ambiguous.

Before dispatching to Codex, read its weekly quota off its status line (`weekly N% left`). At zero
it stops mid-task and cannot even write the handover saying what it finished.

---

## Before you implement against this

- If this is a fresh directory, did `orchestrator.menu` record the one automatic trust answer?
- Is `--add-dir` on the command line, and does a dispatching node get the *parent* directory?
- Was the `--permission-mode` value tested on **the model you are actually going to use**?
- Did that test have a control — with and without the flag, in the same directory?
- Does the briefing tell the child to write files with its file tool rather than `jq` and a redirect?
- Does a task ending take what it handed on with it? (Without that, a timeout leaves orphan
  grandchildren under a parent that no longer exists.)
- When something new appears in the working tree, has it been checked against `git log` before being
  attributed to a child?
- Is "was it ever asked?" being answered by watching the screen, not by reading the transcript
  afterwards?
- Does a failed spawn close its tab, or is it leaving a live assistant to slow down the next one?
- Does the summary distinguish "my children reported" from "my children failed and I did it myself"?
