# Working in this repository

This checkout is shared by several live agent sessions, including Claude Code and Codex.
Treat every change already present in the opening `git status` as another session's unfinished work.
Do not stage, revert, rewrite, or commit those changes.
Do not infer ownership from a filename, a recent timestamp, or a new commit; inspect the content and scope.

## Shared-tree discipline

- Record `git status --short` before editing so you know the pre-existing state.
- Edit only paths assigned to your task.
- Use `git add -- <file>...` with every path named explicitly.
- Never use `git add -A`, `git add .`, or another broad staging command.
- Before any root-owned commit, run `git diff --cached --stat` and remove anything outside that
  commit's scope. **What you are looking for is a path you did not stage yourself.** The index is
  shared: another session's `git add` is already sitting in it, and a plain `git commit` with no
  pathspec commits the whole index, theirs included. That happened twice on 2026-08-28. Read the
  stat as *"is every one of these lines mine?"* rather than *"does this look about right"*, and
  take a stranger back out with `git reset -- <path>`, which unstages without touching their bytes.
- Read the staged diff itself; a clean stat does not prove that a file contains only your work.
- After hunk-staging, commit the reviewed index with plain `git commit -m <message>` and no
  pathspec. `git commit -- <path>...` takes the named files from the worktree instead of the staged
  hunk selection and can absorb another session's unstaged hunks. A commit pathspec is safe only
  when the entire worktree diff of every named path belongs to that commit.
- **The two commit traps point in opposite directions**, which is why neither bullet above replaces
  the other: no pathspec sweeps in what somebody else *staged*, a pathspec sweeps in what somebody
  else left *unstaged*. `git diff --cached --stat` catches the first, reading the staged diff
  catches the second, and a commit that has had neither run on it is guarded against neither.
- A child or worker session does not commit. It hands its changes back to the root session for
  review and commit.
- Once a root session has completed the task it was originally assigned, reviewed its own diff,
  and verified the exact commit in isolation, it should commit its completed work without waiting
  for a separate user request to commit. This permission applies only when the root can isolate its
  own changes from every pre-existing or unrelated worktree change; otherwise it must keep the work
  uncommitted and coordinate with the owner of the overlapping paths.
### Root-owned landing closure

A child task reaching `success`, and a reviewer saying `SAFE TO LAND`, mean **delivered** and
**reviewed**. Neither means the change is complete: the root session that dispatched the graph owns
integration until the target branch contains the reviewed change, and `SAFE TO LAND` is a pending
state rather than a completion phrase.

**The whole obligation — landing records, the exact-tree acceptance, HEAD compiling on its own,
overlapping dirty work, file-release waits and handing an unfinished landing to a named root — is
in [`docs/landing.md`](docs/landing.md). Read it when a delivery comes back.** A child does not
land and does not need it.

#### Closing a root is an act with victims; look before you do it

Ending a session cancels every live task it dispatched — grandchildren first, then children, live
ones and the finished ones still holding a tab. [`docs/orchestrator.md`](docs/orchestrator.md)
describes that mechanism; what follows is the obligation, which the mechanism does not carry.

**Before closing a root, look at what the close takes with it.** One command, and it is short:

```sh
curl -s "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
  -H "X-Clawdline-Orchestrator: $TOKEN" \
  | jq --arg s "$MY_SESSION_ID" '[.tasks[]
      | select(.root.sessionId == $s and (.state | IN("queued","spawning","briefed")))
      | {id, title, state, assistant}]'
```

Nothing there means close freely. Anything there is work in progress that the close will end, so
either let it finish, or say in your closing message what you are killing and why. On 2026-08-27
at 23:20:37–:51 — a fourteen-second window, one close, not four decisions — four in-flight tasks
died together. One of them was a correction dispatched 75 seconds after the review that demanded
it, 25 minutes into its run.

**After a cascade, the orphans are somebody's, and that somebody has a name.** A cancelled task
leaves a landing record reading `pending`, which is also what actively progressing work reads —
the record cannot tell "being worked on" from "its worker was killed hours ago"
(`B-PENDING-CANNOT-SEE-ITS-EXECUTOR` in [`docs/backlog.yaml`](docs/backlog.yaml)). So the outgoing
root writes down, for each orphan, **which named root or which named person picks it up**, in the
same place it records the cancellation. "Somebody will notice" is not an owner: the safe-close
correction was written down as *cancelled*, not as *cancelled together with three siblings by one
close, and now unowned*, and it sat for fourteen hours looking like a hard problem rather than an
absent one. If nobody can be named, say that out loud to the user before closing — an orphan whose
owner is unknown is a decision for them, in the shape [below](#decisions-that-are-the-users-go-to-the-user-as-options).

### Root completion receipt

After a root has genuinely completed its assigned turn—including required integration,
verification and the root-owned commit—and immediately before its final completion response, it
reports one session-scoped delivery receipt through
`POST /v1/orchestrator/sessions/:terminal-id/complete`. Follow the exact command and refusal rules
in [`docs/api.md`](docs/api.md#post-v1orchestratorsessionsidcomplete). This produces only
**delivered, awaiting approval**; it never claims independent review or broker-verified landing.

Do not report this receipt for a partial result, diagnosis-only answer, blocked task, clarifying
question, or child task. A child still finishes only through its authenticated `result.json`.
Clawdline consumes a root receipt when that same terminal begins its next observed turn, so an old
check cannot reappear after newer unreported work.

## Decisions that are the user's go to the user, as options

A decision only the user can make — which of two designs, whether to spend the quota, what happens
to an orphaned line of work — does not travel in prose. Asked in a paragraph it does not arrive:
the user said it plainly on 2026-08-28, «我會漏掉，我不知道怎麼回答» — *I miss it, and I do not
know how to answer.* A question buried in a status report is a question that was not asked.

So it goes to him **in his own session, through an explicit options prompt** (`AskUserQuestion`, or
whatever that assistant calls its option cards), and:

- **One question at a time.** Four decisions are four prompts, not one prompt with four parts.
- **Each option names what happens if he picks it** — the cost, what it gives up, who then does
  what. An option that is only a label makes him do the reasoning you already did.
- **The asker's recommendation is attached**, first in the list and marked as the recommendation.
  You have read the evidence; withholding your view is not neutrality, it is extra work for him.

**Technical to-dos and user decisions are two lists, never one.** They get mixed in a single
"next steps" paragraph, and it is always the user's half that is lost — a to-do can wait for
whoever picks the work up, but a decision has no owner but him, so a decision filed under to-dos
simply stops existing. Report them separately and label them: *what we will do next*, and
*what we need you to decide*. When a line of work resumes after a pause, ask for both lists from
each line — its next steps, and its decisions — as two questions.

## Verifying your work

The working tree is an editing buffer shared with other sessions, not a reproducible build input.
**What you verify depends on which half of this protocol you are in, and the two are not the same
question.**

**A child asks "does what I wrote work?"** The working tree is the right subject — other sessions'
half-finished edits included, because a child does not commit and their mess cannot reach HEAD
through it. Snapshot it *without touching the shared index*:

```sh
snapshot_dir=$(mktemp -d); test_tmp=$(mktemp -d)
git archive HEAD | tar -x -C "$snapshot_dir"
# Rebuild the tracked working tree without writing a stash object under the sandbox-protected
# .git directory. This carries staged and unstaged edits, deletions, modes and binary changes.
git diff --binary --full-index --no-ext-diff HEAD \
  | (cd "$snapshot_dir" && git apply --allow-empty --whitespace=nowarn)
# The diff still cannot carry untracked files. A new test another session has written but not
# committed may be one the suite needs, and leaving it out makes the run fail on something nobody
# broke.
git ls-files --others --exclude-standard -z \
  | tar --null -T - -cf - | tar -xf - -C "$snapshot_dir"
(cd "$snapshot_dir" && TMPDIR="$test_tmp" ./test.sh)
```

That untracked-file overlay is not optional and was found the hard way: the first child told to
follow this recipe hit `test.sh` requiring `Tests/web-schedules.mjs`, which existed only as an
untracked file from another session. It reported the gap instead of quietly working around it,
which is the right thing to do with a rule that does not fit — the rule was wrong, not the
situation.

The archive starts from `HEAD`; `git diff HEAD` then describes the shared tracked worktree without
creating an object in `.git`, and `git apply` reconstructs that state in the private snapshot. It
reads the shared index but does not change it. **A child must not use `git write-tree` for this.**
That reads the *index*, so it requires staging first — and the index is shared. A child staging its
own files sweeps up whatever another session left in there, and then a root commits it. That has
happened here.

**A root asks "will HEAD still build after this commit?"** The working tree cannot answer that, and
neither can a green suite run inside it. Root is staging anyway, so the index is the right subject:

```sh
snapshot_dir=$(mktemp -d); test_tmp=$(mktemp -d)
git archive "$(git write-tree)" | tar -x -C "$snapshot_dir"
(cd "$snapshot_dir" && TMPDIR="$test_tmp" ./test.sh)
```

Use a new, private `TMPDIR` for every `./test.sh` run.
The script writes its test binary to the fixed path `${TMPDIR}/clawdline-tests`; shared `TMPDIR`
values race and overwrite it.

**Verification is budgeted, and it does not leave its output behind.** A child that keeps re-running
the suite is paying this repository's largest fixed cost over and over: `./test.sh` compiles every
file in `Sources/` together with the test files in one `swiftc` invocation, with no cache between
runs.

- A child does not run `./build.sh` (that is a rule of its own, further down), does not use an app
  restart or the real UI as acceptance, does not re-run a full suite after every small edit, does
  not run a suite unrelated to the paths it claimed, and does not repeat a green run to see whether
  it was flaky.
- It does run one verification that actually proves its change: compile, the tests covering what it
  touched, and one red-before-green run for each test it added. Iterating until something first
  compiles and passes is ordinary work and is not what this rule is about. **Handing back code that
  does not compile costs root far more than one honest run costs anybody.**
- The budget is one third of `timeout_minutes`, or three full-suite runs, whichever comes first. On
  reaching it the child stops verifying and says in `result.json` what state the work was in, rather
  than spending the rest of its time trying to get to green.
- `result.json` reports what verification cost: `"verification": {"runs": 2, "seconds": 940, "last":
  "pass", "scope": "..."}`. Without a number, "it kept re-verifying" is an impression rather than a
  finding.
- Heavy temporary output — repository snapshots, build products, compiler indexes — goes in the
  task's own `work/` directory, and the private `TMPDIR` for a verification run points at
  `work/tmp`, so the test binary lands there too. Clawdline reclaims `work/` when the task ends;
  until that reclaim has landed, delete it yourself before writing `result.json`. Anything worth
  keeping is copied into `artifacts/` first.

A new test must be seen red before the change that makes it green. A test born green proves
nothing: reviews here have repeatedly found suites that stayed green after the guarded logic was
replaced with a stub — or deleted outright. Break the thing once, watch the test catch it, then fix it.
Do not build from the live working tree, because it may contain another session's partial edits or
untracked files.

### A confirmation is worth what it names

"I checked, and you are right" is not a check. On 2026-08-28 one root read the route table at
`docs/api.md:117` — *task secret for pending/abandoned; orchestrator token only for landed* — and
told another root that marking a landing `abandoned` required that task's secret. The second root
replied that it had read `docs/api.md` and confirmed it. Neither had read `docs/api.md:2209`, where
the prose says `pending` and `abandoned` accept **either** the task secret **or** the machine
token, and only `landed` is restricted; the comment at `Sources/RemoteServer.swift:622` says the
same, and so does the behaviour. The page contradicts itself, and the wrong half is the one shaped
like a summary — which is the half people read.

The misreading was cheap. The confirmation was not. Believing the claim had been independently
checked, the first root went looking for seven task secrets it never needed — matching dispatch
bodies across a 52.9 MB rollout — and wrote the false rule into a handover document for the next
session to inherit. Someone else later closed all seven rows with the orchestrator token, in one
call, because the rule had never been true.

So **when you confirm a claim, name what you read**: a file and a line, or the command and its
output. "I confirmed" carries no information and cannot itself be checked, while "`docs/api.md:2209`
contradicts the table at `:117`" is something the next reader can test in ten seconds. It is the
standard findings are already held to here, for the same reason — a claim that names its source can
be wrong out loud, and one that does not can only be believed.

**And name when you read it.** Five sessions edit this tree at once, so a reading has a shelf life
measured in minutes. The same afternoon produced three of these: a root quoted a section of this
file it had read that morning, hours after `d67d2229` moved 254 lines of it into
`docs/dispatching.md`; and two roots gave each other a rebase distance of fourteen and fifteen
commits for the same branch, both correct when measured and both wrong by the time they were read,
because `main` moved between them. **On this tree a rebase map is a measurement that expires, not a
fact** — and so is `git status`, `git log`, and the file you opened before lunch. Say when you
looked, or look again before you quote it.

The same expiry applies to a claim about *people*, and that one is easier to miss because it does
not look like a reading at all. "I have given that to X" is true only for as long as X is still
open: on 2026-08-28 a session deliberately left a paragraph for another task to write, correctly
refusing to duplicate it — but that task had finished and landed forty minutes earlier, so the
paragraph became an orphan. It is the same shape as the work a close cascade strands, with a
different cause. The sentence that was missing is when you last confirmed X was still open.

## Dispatching substantial work with Clawdline

Use Clawdline for work that can be split into self-contained tasks and joined later.
Create `/tmp/.clawdline/<task-id>/task.json`, then register it with `POST /v1/orchestrator/tasks`.
Put the complete instructions in `task.json`; the POST body carries only `task_id` and the task
secret under the field name `secret` (the child's terminal `result.json` uses `task_secret`).

### Prove a localhost failure before calling Clawdline offline

An agent execution sandbox may refuse or isolate loopback even while the installed Clawdline app
is healthy. A failed request to `127.0.0.1:7717` from the restricted environment is therefore not
evidence that Clawdline stopped. Retry the same minimal, read-only health request with the required
localhost permission (and check the current configured port) before declaring the service offline,
relaunching it, or falling back from dispatch. Report the two cases differently: an inaccessible
loopback path is an execution-environment limitation; a permitted health request that still cannot
connect is a Clawdline service failure. Never substitute a provider-native child session merely
because the first sandboxed request could not reach localhost.

### Repeated communication stalls require a capacity and protocol audit

When request latency, a loading state, a pending message, a dropped event, or a terminal-automation
failure recurs, do not close the incident by patching only the visible timeout or spinner. Trace the
whole path and record its capacity and protocol contract: connection and queue ownership, bounded
concurrency, queue limits and backpressure, synchronous external calls, retry amplification,
idempotency and delivery receipts, revision/resume behavior, stale snapshots, and failure isolation.
Distinguish accepted, executed, delivered, observed, and acknowledged states instead of treating one
HTTP response as all five. Require typed errors and observable counters for overload and degraded
dependencies, plus failure-injection tests for a blocked terminal bridge, slow transcript reader,
lost SSE/reconnect, unreachable loopback, and duplicate retry. A recurring cross-layer symptom is
not complete until the root has said which limit was reached (or proved no limit was reached), why
unrelated routes did or did not remain responsive, and what prevents the same bottleneck from
reappearing elsewhere.

### "Dispatch" means a Clawdline task, not a provider-native subagent

When the person says **Clawdline Agent**, **dispatch**, **open a new tab**, **independent task**, or
uses the local shorthand **派 Agent／派下去**, satisfy that request through
`POST /v1/orchestrator/tasks`. The accepted task must have its own task id and ordinary assistant
session in a separate terminal tab. Codex `thread_spawn`, Claude sidechains, and any other
assistant-provider-native child session do **not** satisfy a Clawdline dispatch request and must not
be described as one.

Use a provider-native subagent only when the person explicitly asks for that kind of subagent, or
when no Clawdline-dispatch language was used and the task merely benefits from internal delegation.
If a Clawdline dispatch is refused or fails to reach its prompt, report that typed failure and apply
the retry/topology rules below; do not silently replace it with a provider-native child session.

### Everything else about dispatching is in `docs/dispatching.md`

**Before you dispatch, read [`docs/dispatching.md`](docs/dispatching.md).** Whether the work should
be dispatched at all, how large one task is, when small work accumulates into a batch instead,
standing sessions, the graph shapes, declaring `claims`, `serialize`, and how to branch on the
broker's typed refusals — all of it is there, and none of it is here, because a session that is not
about to dispatch was paying to read it every time.

## When Clawdline dispatched you

If the first message names you as a Clawdline child, read the task's `CHILD.md` before doing anything else.
`CHILD.md` is authoritative for that task and overrides this file where they differ.
Stay inside the named project paths and your own task directory; do not inspect other `/tmp/.clawdline` tasks.
Write the requested artifacts and write `result.json` last, exactly as the briefing specifies.

## Never

- Never run `git commit`, `git reset`, `git checkout`, or `git stash` from a child or worker
  session; integration belongs to the root. The one exception is a **Claude** child in its own
  brokered worktree, whose briefing tells it to commit milestones on `clawdline/task/<id>` — that
  branch is the delivery. A **Codex** child leaves the bytes dirty even in a worktree, because it
  cannot commit there; see `skills/clawdline/SKILL.md` §2.0a for why.
- A root may run `./build.sh` when the user explicitly asks to build or install. A child or worker
  session must not run it: the script replaces and restarts the user's running app and can
  interrupt work owned by another session. Before a root runs it, re-check `git status` and make
  sure its wildcard source and resource inputs will not absorb another session's uncommitted work;
  if they would, build the exact intended tree in an isolated snapshot or coordinate first.
- Never alter, stage, discard, or claim another session's pre-existing uncommitted work.
