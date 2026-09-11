# Working in this repository

This checkout is shared by several live agent sessions, including Claude Code and Codex.
Treat every change already present in the opening `git status` as another session's unfinished work.
Do not stage, revert, rewrite, or commit those changes.
Do not infer ownership from a filename, a recent timestamp, or a new commit; inspect the content and scope.

Project vocabulary is defined without implementation detail in [`CONTEXT.md`](CONTEXT.md). Read it
before changing task, graph, review, landing, handoff, or verification semantics; follow its links
to the narrower operational document for the work in front of you.

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
- There is a `pre-commit` hook that mechanises the two bullets above, and it is **off until
  somebody installs it**: `sh tools/install-git-hooks.sh` points `core.hooksPath` at the tracked
  `tools/git-hooks`, repository-wide. What it refuses, what it cannot see, and how to get past it
  are in [`docs/shared-tree-guard.md`](docs/shared-tree-guard.md).
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

### Post-delivery cleanup is part of delivery

A landing is not finished while its checkout still makes already-delivered bytes look like new
work. After recording the landing, refresh the canonical target and landing receipt, then classify
every remaining staged, modified, untracked and registered-worktree row — **and every scratch path
this line created outside the checkout** — as one of: **landed and byte-identical**, **still
unlanded**, **mixed/conflicted**, **task-owned temporary**, or **prunable metadata**. Missing, stale
or ambiguous evidence is a sixth answer: **unknown**.

- Preserve every unlanded or mixed change as a reviewable patch or branch before cleanup. Never
  stage, reset, delete or rewrite another Session's bytes merely to make status clean.
- Only remove landed-identical residue, task-owned temporary output and proven-prunable worktree
  metadata. Refresh status afterwards and reapply anything intentionally preserved.
- **A root creates scratch only through `tools/scratch.sh`**, so every path it made outside the
  checkout — a landing snapshot, a merged tree, a deploy copy — is an owned entry whose marker names
  its owner, and the inventory can find it and `tools/scratch.sh remove` can take it away. A bare
  `mktemp -d` or hand-named `/tmp` directory has no owner and no deadline, which is how 106 snapshot
  directories piled up in one temporary directory.
- **A credential or secret copy lives only in an owned `0700` entry** from `tools/scratch.sh new`,
  and is removed before the turn that made it ends — never left for a sweep, which is the backstop
  for a session that died rather than the plan for one that finished. The contract is
  [`docs/scratch.md`](docs/scratch.md).
- If permissions, live claims or ambiguous ownership prevent cleanup, leave a typed blocker and a
  named next owner. An unattributed dirty row is an unfinished delivery, not cosmetic debt.
- The human handoff always includes **Fixed but not yet released (awaiting review)**, or explicitly
  says `Nothing`. This is separate from dirty-tree cleanup: committed code may still be undeployed.

The evidence and fail-closed procedure are in
[`docs/landing.md`](docs/landing.md#post-delivery-worktree-reconciliation).

#### Closing a root is an act with victims; look before you do it

Ending a session cancels every live task it dispatched — the ones it opened, live and the finished
ones still holding a tab, deepest first. [`docs/orchestrator.md`](docs/orchestrator.md)
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

### A peer handoff is a status, not only a message

Before a Session becomes idle because another Session must move the work, it records that handoff
through `POST /v1/orchestrator/sessions/:terminal-id/state` with
`state:"waiting_session"`, a one-line `note`, the exact mover in `moved_by`, and
`person_needed:false`. This is the honest status for “finished my part; waiting for Clawdfather to
integrate” and “waiting for another root to release its candidate”. It renders as self-reported;
it is not broker proof that the work is complete.

If the Session's whole assigned turn is genuinely delivered, use the root completion receipt
instead: its one check is the stronger and more precise “delivered, awaiting approval”. If that
receipt later receives broker-verified landing, it becomes two checks. Do not bury either handoff
only in a prose message—the Session list cannot derive a durable state from transcript wording.

A user decision is neither kind of peer wait. Record it in the persistent `owed` overlay and
follow the notification rule below before waiting.

### A status audit has a technical handoff and a human ending

When Clawdfather asks several Sessions what they have done, the evidence reply and the last message
the person reads are two different products. The **technical handoff** may name paths, commits,
trees, test receipts, broker rows and closeability versions. Keep it complete so Clawdfather can
verify and combine the work. Do not paste that handoff back to the user as the Session's ending.

Before a Session says it is ready to close, its final user-facing message summarizes its whole
assigned line of work in plain language. Use short bullets and these headings, with Emoji as scan
markers rather than decoration:

- **✅ What was completed** — outcomes the person can recognize; mention implementation versus
  release only when that distinction matters.
- **🧭 What remains** — unfinished, planned, blocked, awaiting integration and awaiting release are
  different states. Name the current owner. Write `Nothing` when there is none.
- **🙋 What you need to decide** — decisions or physical actions only the person can supply. Write
  `Nothing` when there is none; never hide a question in another section.
- **📌 Project Board** — say whether the work and remaining items are registered, name the visible
  item when useful, and call out a stale Board row instead of treating it as product truth.
- **🚀 Release status** — say plainly whether the result is only written, committed, installed on
  the Mac, published to Cloud, or actually checked there. Do not let `landed` read as `deployed`.
- **⚙️ Process feedback** — name any rule, approval, wait, repeated verification or coordination
  round that blocked or slowed the work; estimate the lost time when possible and suggest one
  concrete improvement. Write `Nothing` when the flow was proportionate.
- **🔒 Can this Session close?** — yes or no, and the one human reason. Clawdfather still verifies
  broker closeability before closing; this sentence is not authority by itself.

Put exact SHAs, paths, command lines, counts and log locations in a clearly separated technical
appendix only when the user asked for them or when one short receipt is needed to make the claim
auditable. The summary is written for the person, not for Clawdfather. It must be understandable
without opening every Session, reading a screenshot of terminal output, or knowing the difference
between a tree hash and a build stamp. Clawdfather synthesizes these human endings across Sessions;
it does not forward the raw audit replies unchanged.

Name each line by its **Session title**, because that is what the person can recognize in the UI.
An internal terminal ID such as `%123` is not a title and is not currently a usable navigation
link; keep the ID in the technical appendix when correlation is necessary. If the product cannot
turn a referenced Session into a clickable jump, register that as a UI follow-up instead of making
the person hunt through tabs by ID.

### Notify before waiting for the user

When an agent can already tell that the next blocking step requires the user to return to a Mac or
phone, approve a permission, enter a credential, or make the final confirmation in a browser, it
sends a Clawdline attention request **before** it settles into waiting. A root uses
`POST /v1/orchestrator/notify`; a child uses `POST /v1/orchestrator/tasks/:id/notify` with its task
secret. The notification names the project or task, the concrete action, and why it is needed.

This is an interruption budget, not a progress feed. Do not notify for routine status, background
work, or a request the user is already actively answering in the same surface. Never put a secret,
token, password, or one-time code in notification content. Delivery does not authorize the action
and does not replace the explicit prompt in the owning Session.

If notification is disabled, no device is subscribed, delivery fails, or the limiter refuses the
request, do not loop or route around the user's preference. Keep the actionable instruction in the
Session and report the typed refusal. The exact routes and refusal codes are in
[`docs/api.md`](docs/api.md#post-v1orchestratortasksidnotify-post-v1orchestratornotify).

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
through it. Snapshot it *without touching the shared index*, inside the task's own `work/`:

```sh
tools/scratch.sh snapshot-run --subject worktree --root /tmp/.clawdline/<task-id>/work -- ./test.sh
```

That is three steps and a cleanup. It unpacks `git archive HEAD` into a private copy; replays
`git diff --binary --full-index --no-ext-diff HEAD` onto it with `git apply`, which carries staged
and unstaged edits, deletions, modes and binary changes without writing a stash object under the
sandbox-protected `.git`; and overlays the untracked files, which no diff can carry. Then it stages
the copy in a repository of its own, runs `./test.sh` at its top with a private `TMPDIR`, and removes
all of it however the run ends — success, failure, `INT`, `TERM` or `HUP` — with the suite's exit
status passed back unchanged. What it replaced was a copy-paste recipe that made two `mktemp -d`
directories and removed neither: on 2026-09-11 this user's temporary directory held 106 of them,
1,871 MB. The contract behind the tool, and each choice it makes, is
[`docs/scratch.md`](docs/scratch.md).

The untracked-file overlay is not optional, and it was found the hard way: the first child told to
follow the old recipe hit `test.sh` requiring `Tests/web-schedules.mjs`, which existed only as an
untracked file from another session. It reported the gap instead of quietly working around it,
which is the right thing to do with a rule that does not fit — the rule was wrong, not the
situation.

The archive starts from `HEAD`; `git diff HEAD` then describes the shared tracked worktree without
creating an object in `.git`, and `git apply` reconstructs that state in the private copy. **It must
not change the shared index, and a plain `git diff HEAD` does:** measured on 2026-09-11 with git
2.38.1, when a tracked file's stat information is stale — the same bytes with a new mtime — it writes
the refreshed index back, with and without `GIT_OPTIONAL_LOCKS=0`. So the tool reads a private copy
of the index. **A child must not use `git write-tree` for this.** That reads the *index*, so it
requires staging first — and the index is shared. A child staging its own files sweeps up whatever
another session left in there, and then a root commits it. That has happened here.

**A root asks "will HEAD still build after this commit?"** The working tree cannot answer that, and
neither can a green suite run inside it. Root is staging anyway, so the index is the right subject:

```sh
tools/scratch.sh snapshot-run --subject index -- ./test.sh
```

That is `git archive "$(git write-tree)"` into an owned entry under `/tmp/clawdline-scratch`, removed
the same way. `--subject index` belongs to a root alone, because `write-tree` writes a tree object
into the shared `.git`.

Both subjects stage the copy in a repository of its own (`git init -q && git add -A`), and the need
for that was found twice on 2026-09-05 by two sessions that did not know about each other: a child
following the old recipe and a root building a landing snapshot both watched the whole suite abort
on `version_scan_no_files` before a single check ran. `tools/check-version-strings.py` asks git for
the files it scans, and it is right to fail closed — a version scan that silently finds no files is
the failure it was written to prevent — so the recipe was what was wrong. Staging is enough; nothing
reads a commit.

Use a new, private `TMPDIR` for every `./test.sh` run.
The script writes its test binary to the fixed path `${TMPDIR}/clawdline-tests`; shared `TMPDIR`
values race and overwrite it. `snapshot-run` gives each run its own, inside the entry it removes.

**Verification is budgeted, and it does not leave its output behind.** A child that keeps re-running
the suite is paying this repository's largest fixed cost over and over: `./test.sh` compiles every
file in `Sources/` together with the test files in one `swiftc` invocation, with no cache between
runs.

- A child does not run `./build.sh` (that is a rule of its own, further down), does not use an app
  restart or the real UI as acceptance, does not re-run a full suite after every small edit, does
  not run a suite unrelated to the paths it claimed, and does not repeat a green run to see whether
  it was flaky.
- It runs the cheapest verification pass that materially reduces the risk of the complete delivery
  unit. Accumulate related edits first, then compile and run the relevant groups once near the end;
  do not pay a Swift compile for each assertion, file, finding or small correction. Use one
  representative red-before-green or failure-injection proof for each materially new failure class
  when the test could otherwise pass without the behavior. Pure prose, generated-count
  transcription, mechanical moves and test-fixture-only corrections do not need a synthetic red
  mutation. **Handing back code that does not compile costs root far more than one honest run costs
  anybody, but repeated proof of the same boundary is waste rather than safety.**
- Until the repository ships a focused Swift runner, an implementer whose behavior cannot be
  exercised any narrower may use **one** full-suite run and record
  `focused_runner_unavailable` in its receipt. Reviewers do not repeat that run. The time budget is
  still one third of `timeout_minutes`; on reaching it the child stops verifying and reports the
  state reached rather than spending the rest of its time trying to get to green.
- `result.json` reports what verification cost: `"verification": {"runs": 2, "seconds": 940, "last":
  "pass", "scope": "..."}`. Without a number, "it kept re-verifying" is an impression rather than a
  finding.
- Heavy temporary output — repository snapshots, build products, compiler indexes — goes in the
  task's own `work/` directory. A child passes `--root /tmp/.clawdline/<task-id>/work` to
  `tools/scratch.sh`, so the snapshot and its private `TMPDIR`, test binary included, live in an entry
  under `work/` that the tool removes when the run ends and that Clawdline's `work/` reclaim would
  take in any case. A child does not use the owned root `/tmp/clawdline-scratch`; that is for
  sessions that have no `work/`. Anything worth keeping is copied into `artifacts/` first.

**`./test.sh` takes a machine-wide lock, and that is not politeness.** On 2026-09-03 this Mac was
force-rebooted twice inside half an hour because several sessions each started the suite: four
`swift-frontend` processes, peaks of 46, 45, 27 and 8 GB on a 24 GiB machine. The script now
acquires `/tmp/clawdline-suite.lock` itself before it compiles and gives it back after the binary
has run, so forgetting is no longer possible. What that means for you:

- **A run that waits is queueing, not stuck.** The wait names who holds the lock, both pids, what
  phase they are in and how long since anything actually compiled, so *why am I waiting* always has
  an answer you can go and ask rather than a spinner.
- **`exit 75` means the machine was busy, not that the tests failed.** A run that cannot get the
  lock waits up to an hour (`CLAWDLINE_SUITE_LOCK_WAIT_SECONDS`) and then gives up with 75, which
  is `EX_TEMPFAIL` — *temporary failure, the caller is invited to retry*. Nothing else in the script
  returns it. Report it as a blocked verification and say who was holding the lock; reporting it as
  a red suite sends somebody hunting a defect that is not there.
- **Nothing is ever killed.** A lock may be waited for, refused, or reported. It is taken over only
  when the holder has stopped renewing **and** no compiler is running anywhere on the machine —
  both, because either alone admits a collision: no-compiler-alone reclaims the lock in the gaps
  between one study's compiles, and stopped-renewing-alone reclaims it from a holder that was
  merely swapped out while its compile still holds twenty gigabytes. Missing or ambiguous evidence
  blocks; it never reads as *dead*.
- **Do not compile around a wait.** Compiling outside the lock is the thing that rebooted the
  machine. `CLAWDLINE_SUITE_JOBS=<n>` is the supported way to ask for fewer compiler jobs; unset
  adds no flag and the command line stays what it always was. If your work genuinely cannot be
  verified without going around the queue, report that instead of doing it.
- **An isolated worktree is a separate checkout, not a second Mac.** The lock is machine-wide on
  purpose, and `build.sh` takes the same slot because it is the same capacity.

`docs/machine-resource-scheduling.md` carries the measurements, the instruments that lied on the
way, and the design that came out of them.

**One release candidate normally pays for one final full suite, and the landing root owns it.**
Several compatible Feature slices should share that candidate instead of each buying the same
machine-wide compile. Implementation and correction use one accumulated focused pass; they do not
compile once per small edit. A docs-only or mechanically generated candidate that cannot affect
compiled/runtime behavior may stop at its relevant static checks. A second full run is allowed only
when the first is typed `inconclusive_environment` (crash, timeout, or a named sandbox capability),
never merely to see whether a green result was flaky. Record the reason beside the second receipt.

**Verification is divided by question, not repeated by role.** The implementer proves the changed
behavior once with an accumulated focused receipt. The reviewer reads that receipt and the diff;
it does not rerun tests unless its review question needs runtime evidence that the receipt does not
contain, and then it names that missing question. The integrator consumes both receipts and tests
only new merge seams, correction findings, or changed dependencies before the release candidate's
single exact full. Never rerun the same tree/question/environment tuple because ownership moved,
because another Session cannot see the old stdout, or merely to obtain a fresh green. A non-semantic
runner failure such as a fully evidenced `exit 133` is recorded once and left for the one final exact
gate; repeated retries are not verification.

**Review is risk-triggered, not ceremonial.** Require one independent sealed review for security,
authentication, durable state, concurrency/backpressure, migrations, destructive/external actions,
or a broad cross-component change. Routine localized code, docs, generated data and test-only
repairs use the owner's focused diff review unless the user asks for more. When independent review
is warranted, it reads the complete Feature or batch once and seals the full finding set before one
correction wave; confirmation reopens only those findings and adjacent regressions. Do not create a
review or verification task per finding. The staged extraction rules and anti-over-splitting gate
are in [`docs/architecture-refactor.md`](docs/architecture-refactor.md).

**Coordination is event-driven.** A single owner with a clean index and no declared path conflict
proceeds without asking for a landing slot, sending heartbeats, or requesting approval at each
internal step. Coordinate only on an observed path/index conflict, an active machine lock, an
external dependency, a changed scope boundary, or a decision only the user can make. Ordinary
updates collapse to: boundary claimed, blocker changed, and delivery landed.

Let the broker's durable `task_finished`, progress, notification and landing events wake the Root;
do not keep an assistant alive in a fixed polling loop. The Root observes the delivery, performs
its integration duty, and only then starts dependent work. A worker does not autonomously start a
peer or downstream task, because that can race the Root's exact-tree integration or start the same
dependency twice.

Polling is a bounded watchdog, not the normal control flow. Check a newly queued or spawning task
once after 90 seconds. Check healthy briefed/working code only after 15 minutes with no task,
progress or worktree activity. For a known compile or test, wait its expected duration plus three
minutes. A schema-valid `result.json.tmp` is recovered by the broker's 30-second stable-result
finalizer and must not be polled by an LLM. Every watchdog first reads only compact task state and
timestamps; fetch a transcript only after that evidence names a stale or blocked condition. Do not
send a timed user update when no state changed.

**A verification receipt names its subject and question.** At minimum keep the repository, exact
tree SHA (or an explicit working-overlay digest), question id, command/variant, environment,
duration, exit status and check counts. Only an exact commit-tree receipt may be reused across
tasks. A dirty-overlay green is the child's self-proof, not root acceptance. Until the broker owns
the durable receipt ledger, put these fields in the task artifact and do not rerun the same
tree/question/environment tuple.

**`./test.sh` still exits 133 occasionally, and one measurement would settle it.** Whoever hits it
next records five things, each with its unit and how it was taken: `wc -c` bytes; `wc -l` lines;
`grep -a -c '✓'` ticks — **not** the line count, because the last line may be half a tick; **the
full text of the last complete `✓` line**, meaning the one before any truncated remnant; and
`grep -a -c 'Fatal error:'` together with whether the tree contains `e8bf1532`. The deciding field
is the fourth: if it converges across different trees the cause is a *place*, and the code around
that test is what to read; if it scatters while the byte counts converge, it is a buffer. The two
cases measured so far converge on one test. An earlier reading that called the cut a buffer
boundary was withdrawn once those two were compared with each other instead of against a run that
died of something else — the sister of the rule about holding the observed thing still.

A materially new failure class needs one representative proof that would have failed without the
behavior. That can be a baseline red, a focused mutation, or failure injection. Do not manufacture
one red run per assertion: several tests may protect the same class, and prose, generated values,
mechanical moves and fixture-only corrections do not become safer through a synthetic failure.

**Assert on the check count, not on the exit code.** The reason was measured on this repository's
own guard, and the gap it was measured in is now closed. `Tests/test-sh-streaming.mjs` said it
proved `test.sh` "reports the binary's status", but the block it lifted out ended at `set -e`, and
the line that actually reports that status — `exit "$status"` — sat on the far side of it: deleting
that line left all five of its checks green, while the `test.sh` they had just approved would
report a red suite as `exit 0`. **`9487dce8` repaired it** — the extraction now walks forward to
the last `fi` of the status branches, `exit "$status"` is inside the block it runs, and a sixth
check reads the run's own status — so read that paragraph as history, not as a live hole in the
guard. **The rule it bought is not history.** What a day's green lights actually rest on is the
check counts in the logs, not exit codes: an empty snapshot produces no check count and a trapped
log produces no check count, which is exactly the difference a status of 0 cannot show you.

Do not build from the live working tree, because it may contain another session's partial edits or
untracked files.

### A live task is not by itself a build/restart blocker

Build from a clean exact candidate tree, but do not turn that source-tree rule into a second,
unrelated rule that says the machine must have no live task. Clawdline compiles the replacement
beside the installed app while the old app is still serving; the observable interruption begins
only when the finished bundle replaces the old one and ends when the new listener is healthy.

Task state decides restart risk:

- `queued` or `spawning` is unsafe. Before briefing, the plaintext task secret exists only in the
  running broker, so replacing that process can strand the opened child and make the task
  `spawn_failed`. Wait for this state to clear; `build.sh` performs the same last-moment check.
- `briefed` is durable and is **not a restart blocker**. Its terminal and assistant are independent
  processes, the briefing has reached the child, and the broker record survives restart. Progress
  or `result.json` written during the short listener outage is collected after the app returns.
- a finished task, open coordination wait, pending landing, ordinary Claude/Codex Session, or
  registered coordinator is durable broker/session state. Restarting Clawdline does not authorize
  closing those terminals, cancelling their work, resolving their obligations, or rebinding the
  coordinator.

The other real risk is an in-flight terminal mutation: an Apple Event already admitted to the
terminal broker is process-local. `build.sh` obtains the durable restart-maintenance receipt, waits
for global and per-terminal queues to reach zero, replaces only at `phase:"ready"`, and waits for
the new process to reconcile and reopen admission. Only the first rollout to an older runtime may
receive an exact 404; that documented bootstrap uses the legacy queued/spawning preflight and a
genuinely quiet terminal-mutation window. A guard that only counts live tasks cannot prove this and
must not use `briefed > 0` as a proxy for it.

After restart, verify the new binary/build identity, `/v1/health`, a fresh Session inventory and the
existing coordinator id/generation. A temporarily stale or unknown projection is a reason to wait
for the new scan, not to rebind or close anything.

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
### To compare two surfaces, hold the observed thing still

Two sessions asked the same question: is a task's cost a stored fact, or something computed at the
moment it is served? One compared *the first record on disk that has a `usage` block* against *the
first record in the HTTP response that has one* — *two different tasks* — and read the difference
between two rows as a difference between two surfaces, then wrote an acceptance condition on top
of a mechanism that does not exist. The other compared **one task across both surfaces** and had
the answer immediately: when the cost is there it is a recorded fact and the exit merely spells it
in camel case; when it is not, neither surface has it and nothing was computed anywhere.

So: **name the single subject first, then look at it through each surface.** Whenever you are
about to say "the API differs from the registry", "the child sees something different from what
the root sees", or "this field is computed rather than stored", check that both readings are of
the *same row, same task, same id*. Different rows are not evidence about surfaces.

This is the sister of the rule about saying what you actually read before claiming to have
confirmed something, and it is the harder of the two to catch: that one stops an assertion with no
source, and this one stops an assertion whose sources are real but not aligned. The second kind
arrives wearing the shape of an experiment, so nobody thinks to question it.

### A rendering collapses distinctions, and then we reason about the rendering

Three times in one day, on three unrelated questions, somebody printed a summary and then argued
from the summary rather than from what it was made of.

A script printed `(未宣告)` for both a missing `claims` field and an empty list, and the count built
on it — "twelve tasks in flight, none declaring claims" — was read as a fact about discipline. It
was not: the broker discards claims for worktree-isolated tasks, so `[]` there means *declared and
dropped*. The evidence was in the dispatcher's own response, which had returned
`claims_ignored_for_worktree` with all eight paths listed, and it still counted its own task as
undeclared. The instruction that followed — "declare harder" — could not have moved a single cell,
because for those tasks the declaration had already happened. Earlier the same day, a cost display
rendered *absent*, *recorded* and *a deliberate zero* identically, and a session concluded the
registry stores no cost at all.

The shape is always the same: **a rendering is lossy, the loss is silent, and the conclusion is
about the rendering rather than the world.** It is not carelessness — the summary is what you can
see, so it is what you think with.

Two habits catch it. **Before counting, ask what different states this cell could be in and whether
the rendering can tell them apart** — missing, empty, zero, refused, not-yet-known are five states
that love to share one spelling. And **when a count implies a fix, check that the fix can move the
count**; if the metric cannot see the discipline it is asking for, the metric is the defect, not
the people.

Where a field genuinely has to carry several meanings, the repair is to stop overloading it — keep
the declaration on the record next to a state that says how it was treated — not to remember the
ambiguity.

### A defect on the phone is read from a file, never from a paste

Some faults only exist in the browser on somebody's phone, where you have no console, no inspector
and no address bar. **Do not ask for the report.** On 2026-09-06 the trace was long enough that
pasting it into the conversation hung the program, Universal Clipboard had not synced it, and the
fallback was a person reading a string off a screen out loud — which is an engineering problem
turned into his afternoon.

The standing arrangement is three lines: **add an observation point** with `Diagnostics.note()`,
**ask for one press** — Settings, five taps on the version line, `LAYOUT DEBUG`, **Send to Mac** —
and **read the file**, always at

```
~/Library/Logs/Clawdline/diagnostics/report.json
```

The path is a constant, so **check `written_at` before you read anything else** — the file is
always there once anybody has ever pressed the button, and the one way this goes wrong is reasoning
about last Tuesday's press. The format is JSON, and the report states its own gaps rather than
leaving you to infer them: how many trace entries were dropped, and whether each recorder that lives
outside the page is `unread`, `merged`, `unavailable` or `failed`. **The most you may ask of the
person holding the phone is one press.** All of it, including how to add a recorder of your own, is
in [`docs/diagnostics.md`](docs/diagnostics.md).

### A guard must be able to go red

Seven times in one day, on unrelated questions, a command reported success and nothing had
happened. The root is the same every time: **succeeding and producing a result were treated as one
fact.**

- `sed` at the end of a pipeline swallowed the status of the command before it, so the `||` never
  fired. A pipeline's exit code is its *last* command's, not the one you care about.
- A guard's glob matched zero files in the directory it is actually invoked from, printed `ok`, and
  exited 0.
- `2>&1 | wc -l` counted a five-line error message as five resources.
- `git stash create` succeeded and printed an empty string, so `|| echo HEAD` never fired, the
  snapshot held nothing, and `./test.sh` exited 127 with zero failures reported.
- `git ls-files | grep | sed`, judged with `|| echo none`: the status belonged to `sed`, and was
  always 0.
- `grep -c` **exits 1 when the count is legitimately zero**, so `grep -c … || echo 0` prints two
  zeros — one the real count, one the fallback.
- `grep` printed *nothing at all* — not `0` — on a log it had decided was binary, while its exit
  code looked ordinary. `-a` produced the real `0`.

Four questions catch all seven, and they are cheap to ask before relying on a command:

1. **Can this succeed and produce nothing?**
2. **Is this exit code the one I think it is?** `a | b` reports `b`'s.
3. **Does this tool change behaviour silently on the shape of its input?** `grep` on something it
   decides is binary; `wc -l` counting the stderr that `2>&1` handed it.
4. **Does this exit code carry a result or a status?** `grep` is found/not-found, `diff` is
   differs/same, `test` likewise. There `||` does not mean *it failed*, it means *the answer is
   no* — and a perfectly correct "no" fires your fallback.

The prescription is one line: **prove your guard can go red.** Break what it guards, in the
directory it is really run from, and watch it fail. A check nobody has seen fail is not a weaker
check, it is a claim with nothing behind it. The one guard written that way on the day the other
seven were found is `clawdline-cloud`'s `infra/check-aws-descriptions.py`: its author ran it where
it is actually invoked, made *scanned zero files* return exit `2` with
`aws_description_scan_empty` on stderr, and had the success line print how many files it read
(`ok: N .tf file(s) scanned`). **A count in the output is what lets the next reader tell "clean"
from "never looked".**

Two habits belong under this rule, because they are how a false reading travels.

**Identify by content, and hand the content over.** Searching by content does not save you if what
you pass on is a line number. Somebody located a passage correctly by matching its text and
reported it as *line 74*; by the time that was read the line held something else, a grep against
code that no longer existed came back empty, and the emptiness was broadcast as a fact. On this
tree line numbers are the half that expires — quote the line itself.

**A number keeps its unit.** `lines: 283` was passed on as *283 ticks*. The tick count was 282, and
the conclusion built on the coincidence had to be withdrawn. A number that changes units in transit
is indistinguishable from a measurement, and it is the same failure as a count read off a lossy
rendering.

One caution about the seventh instance, because the prescription is cheap and the diagnosis was
wrong — it was written from three logs nobody had held still beside each other, and it said *three
truncated logs were all cut mid-character and only one of them silenced grep*, which cannot be true
of any three files: a prefix ending mid-character is invalid UTF-8, so all three were. What decides
is not where the cut is but **whether the invalid byte sits in a line that has already ended.** Four
control files, every one of them cut after the second byte of a `✓`:

| the log | PATH `grep -c '✓'` | `/usr/bin/grep -c` |
|---|---|---|
| invalid bytes at EOF, no trailing newline | `1`, rc 0 — speaks | `1` |
| invalid bytes then `\n` | nothing, rc 1 — **silent** | `1` |
| invalid bytes mid-line with more after them | nothing, rc 1 — **silent** | `1` |
| cut on a character boundary, mid-line | `1`, rc 0 | `1` |

The same rule held on two real crash logs from one run: the `tee` copy ended `promoted\n  e2 9c` —
a `✓` two bytes in, stopped at EOF — and grep answered `282`; the redirected copy had a shell error
message after its invalid byte and grep said nothing at all, while `/usr/bin/grep` said `287`. **A
log cut cleanly at EOF, which is what a crashed run usually leaves, is the case that does not go
silent** — so silence is not a truncation detector in either direction.

**And the missing premise, without which none of this reproduces.** `grep` on this Mac is not BSD
grep. Claude Code's shell snapshot defines it as a function wrapping **ugrep 7.8.4**, which is what
goes quiet; `/usr/bin/grep` is BSD grep 2.6.0-FreeBSD and answered correctly in all six readings
above. Written as a fact about `grep` it is a fact about one shell on one machine, and whoever
checks it from another shell will report that it does not reproduce.

The prescription's first half is unchanged and still worth following: always pass `-a` when grepping
a log that may be truncated, which costs nothing; and **never read grep's silence as evidence that a
log was truncated.**

**Its second half — *for that, use `file` or the byte count* — had never been measured, and neither
tool detects truncation on its own.** `file -b --mime-encoding` answers `unknown-8bit` only when
something follows the invalid bytes: a newline behind them, or more of the line after them, which is
rows two and three of the table above. Row one — the bytes simply stop there, which is the shape a
crashed run usually leaves, and the row this section already calls the common case — comes back
`utf-8`, the same answer it gives for a log that was never cut at all. And a byte count is not
readable without the destination stdout had: measured on this Mac, `st_blksize` is **4096 for a
regular file and 16384 for a pipe**, so one number is a filled buffer on one destination and a
fraction of one on the other. (That is the stdio buffer for a destination, not the page size two
sections down; the two are separate constants that happen to share a value on pipes here.) So read
the last bytes themselves — `tail -c` — and if a size is going to carry an argument, record where
stdout was pointed when it was taken.

### Before moving a file, list what names it — that costs nothing, and testing costs four minutes

Moving code out of a large file breaks things that named the old file by a spelling: a tool that
pinned a full function signature, a test that pinned `Sources/X.swift`, a guard that pinned one way
of writing a call. **None of them appears in the diff**, and the compiler sees only the first kind,
so the rest surface one at a time — each costing a full suite run to find and another to confirm.

On 2026-09-03, extracting 1,003 lines out of `Sources/RemoteServer.swift` took five suite runs.
Four were red, each on a different pinned spelling, each found by running rather than by looking.
After the third the line stopped repairing and enumerated instead: seven files read that file's
contents, four of them compared its payload against a fallback, and all four were already fixed.
**The enumeration cost no machine time. The four runs it replaced cost sixteen minutes of a
machine that serialises compiles behind a lock.**

So, before the first run of an extraction:

- **List every referrer by name**, not by memory: `grep -rn '<old file path>' Tests tools Sources`,
  plus the symbols the extraction renames.
- **Calibrate the pattern against a known positive before believing a zero.** That day's first
  enumeration reported "11 sites, none affected" while one of them was red at that moment: the
  pattern matched `contains("literal")` and the code wrote `contains("\(key):")`. A count of zero
  from an uncalibrated pattern is a statement about the pattern.
- **Prepare the negatives too.** A pattern widened to catch what it missed will catch things it
  should not; the only way that shows up before it wastes a run is a list of sites that must *not*
  match. That day's negatives caught a return type and a commented-out example.

**And a delivery's own count is a claim you can check.** One landing that day said "six adapter
lines"; five were repaired, the sixth was in a test file, and the two numbers sat on the same screen
without being subtracted. The compiler charged four minutes for that.

### A sample taken along one path measures that path

The same shape turned up four times in one day at three different levels, and it is the reason the
two rules above keep needing to be rewritten rather than merely obeyed.

| what was sampled | what it measured | what it missed |
|---|---|---|
| one block sent to the main queue, asking `Thread.isMainThread` | `true` | after `dispatchMain()` the main thread is parked, and every later block answers `false` |
| a `dispatchPrecondition` probe entered from main, synchronously | it passed | the off-main path traps |
| **a review round asking one question** — *is `Row.measurement` the only exit?* | yes, and thoroughly proved | the defect came in through the **write** side: `coverage_reason` overwriting itself |
| one row's keys, to decide whether a registry stores cost at all | that row does not | other rows do |

So the rule is not about probes:

> **A question sampled along a single path measures that path** — whether the sample is one block,
> one row, or the scope of one review.

**A scope you set yourself is the dangerous kind, because it looks like care.** The clearest
statement of it came from the session that caught itself doing it: *the scope I wrote was narrow in
exactly the way I had spent the afternoon warning other people about.*

The prescription: **send samples across three axes — time, thread, path** — and when that is not
possible, write *this measured the fast path only* in the report rather than *it does not happen*.
The fast path is usually the one without the bug in it; the bug is on the slow path, in the race,
after initialisation has finished.

### A single exit constrains its readers, not what reaches it

> **"This is the only way out" can be true and still not be enough, because the way *in* overwrites
> itself.**

Measured: `coverage_reason` is a single last-writer-wins slot. `apply()` writes `source_regressed`
and then, **in the same call**, overwrites it with `sample.coverageReason`; `departed()` overwrites
it again. Two reachable paths each downgrade the same row, and every reader is told about exactly
one of them. The exit really is single, and it helps not at all.

So when checking an invariant, ask **whether it covers the exit or the whole journey.** And fix it
by stating the invariant rather than the mechanism — *coverage marks accumulate rather than
overwrite; a row that is two things at once reaches every reader as both* — with a standing refusal
to add a second single slot, which is the same defect in a larger room.

### Measure a constant here; do not recall it

`getconf PAGESIZE` on this Mac is **16384**. 4096 is Intel's number.

One line described its own 16,385-byte truncation point as *a 4 × 4096 block boundary* — the
arithmetic is right and the unit is wrong: that is not this machine's block, it is exactly one page
plus one byte. Reasoning from 4096, a second line concluded that *one unflushed block hides 68
groups*; the real figure is about 269, which is **68% of the whole suite**.

Every "it crashed within N groups of the last tick" interval computed that day is therefore void —
not to be recomputed, but discarded, because the interval the true value produces is *almost
anywhere*.

> **That 2^14 was announcing itself as one page, and we read it as four blocks.**

## Dispatching substantial work with Clawdline

Use Clawdline for work that can be split into self-contained tasks and joined later.
Create `/tmp/.clawdline/<task-id>/task.json`, then register it with `POST /v1/orchestrator/tasks`.
Put the complete instructions in `task.json`; the POST body carries only `task_id` and the task
secret under the field name `secret` (the child's terminal `result.json` uses `task_secret`).

### A major Feature has a visible independent owner

<!-- clawdline-dispatch-role-contract:v1 -->

- **Owned child.** `POST /v1/orchestrator/tasks` creates a bounded child only when Clawdfather
  retains synthesis, integration, and landing.
- **Handoff.** `POST /v1/orchestrator/handoffs` is continuation or transfer of an existing work
  line; the receiver must walk the sender's complete REFERENCES, answer VERIFICATION, and continue
  from OPEN THREADS.
- **Detached automation.** `POST /v1/orchestrator/detached-tasks` is the only public route that
  accepts `root.session_id: null` with `root.poll_only: true`; ordinary
  `POST /v1/orchestrator/tasks` refuses poll-only. It is only unattended automation, never a Root
  or Major Feature owner.
- **Root Assignment / Feature Launch.** `POST /v1/orchestrator/root-assignments` opens an
  ordinary independent Root and briefs only objective, scope, constraints, relevant references,
  and acceptance. Its durable machine-auth record and UI classification carry no child, handoff,
  detached, timeout, secret, result, parent, or landing lineage.

<!-- /clawdline-dispatch-role-contract:v1 -->

Use Root Assignment only for a genuinely new independently owned Feature. Keep bounded work under
Clawdfather as a child, and use handoff only to continue an existing line with its full state.

Provider-native subagents remain appropriate for short, disposable, normally read-only research,
calculation or focused review that creates no independent delivery or landing ownership. Announce
that use honestly; never present it as a Clawdline dispatch. If the broker refuses or Clawdline is
unavailable, report the typed failure and wait, retry or ask—the Feature does not silently change
into an invisible delegation. For every bounded child it does dispatch, Clawdfather still owns
decomposition, independent review, target-tree integration and landing closure after delivery.

### Cross-session assistant communication uses the message route

When one live assistant sends a message, report, status, finding or coordination note to another,
use `POST /v1/orchestrator/messages`. If that route refuses the message, surface its typed failure;
never fall back to `POST /v1/sessions/:id/send` or a hand-written sender prefix, because those are
ordinary `user` turns. A message is not an assignment: it cannot attach new work, transfer
shared-tree ownership or bypass `claims`. See [`docs/messages.md`](docs/messages.md) for the closed
envelope and [`docs/api.md`](docs/api.md) for the request contract.

### Prove a localhost failure before calling Clawdline offline

An agent execution sandbox may refuse or isolate loopback even while the installed Clawdline app
is healthy. A failed request to `127.0.0.1:7717` from the restricted environment is therefore not
evidence that Clawdline stopped. Retry the same minimal, read-only health request with the required
localhost permission (and check the current configured port) before declaring the service offline,
relaunching it, or falling back from dispatch. Report the two cases differently: an inaccessible
loopback path is an execution-environment limitation; a permitted health request that still cannot
connect is a Clawdline service failure. Never substitute a provider-native child session merely
because the first sandboxed request could not reach localhost.

### Name a session to a person, address it by id

`%88` is what `to_session` takes and what every route matches on exactly; it is also unreadable to
the person reading your report, who cannot tell which of their tabs it is — and it does not last,
because a terminal id is reissued whenever the app restarts while the label outlives it. The wire
format already pairs them: a `<clawdline-message>` envelope carries `source.label` beside
`source.id`, and every row from `GET /v1/orchestrator/sessions` carries `label`, `cwd` and
`assistant`. Prose is the only place the name is dropped. So the first time a session appears in
something a person reads, write **its label, then its id and project** — 「MongoDB 現代化與平台收尾」
(`%88`, `~/code/foodblogs`) — and the short form after that. Nothing about machine addressing
changes: no route has ever matched a title, and none should start.

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

**This whole rule is addressed to a root.** The dispatch tree is one level deep: a root opens
children and a child opens nothing, so a child asked to hand part of its work on does the opposite
of the above — it uses its own assistant's subagents (Claude Code's Task tool, Codex's subagents),
because a Clawdline dispatch from a child is refused with `409 depth_exceeded`. See
[`docs/orchestrator.md`](docs/orchestrator.md#the-tree-is-one-level-deep-and-that-is-structural).

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
**You are the bottom of the tree: you cannot dispatch Clawdline tasks of your own, and one you
attempt is refused.** When part of the task wants to run in parallel or deserves a context of its
own, use your assistant's built-in subagents — no tab, no broker, and the answer comes back to you
rather than to a file you have to wait for.

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
- **An unqualified Clawdline deploy includes the hosted console.** The user works from the Cloud
  version, so installing/restarting the Mac app alone is not a completed deploy. Unless the request
  explicitly limits the target to one surface, deploy the exact accepted commit both as the native
  app and as the `app.clawdline.com` bundle: build the latter with `tools/build-web-app.py`, upload
  it to the Cloudflare Pages project `clawdline-app`, then verify the deployment-specific
  `*.pages.dev` hostname and `https://app.clawdline.com/BUILD.json` name the expected build stamp.
  This default adds the hosted console, not the unrelated marketing, API, or relay deployments.
- **One named operational finalizer owns the side effects.** When a workflow explicitly names a
  downstream root as the finalizer for build, restart, or deploy, the landing root stops after the
  commit and its exact-tree verification receipt. It sends that exact commit and receipt to the
  named owner and must not build, restart, or deploy the same change itself. The ordinary landing
  root remains the operational finalizer only when no downstream finalizer was named. Transport
  acceptance is not observation; it proves only that the handoff was sent. Obtain an explicit
  acknowledgement from the named owner, or re-read the named owner's session and see it acknowledge
  the handoff, before treating the handoff as live.
- Never alter, stage, discard, or claim another session's pre-existing uncommitted work.
