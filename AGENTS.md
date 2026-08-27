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
  commit's scope.
- Read the staged diff itself; a clean stat does not prove that a file contains only your work.
- After hunk-staging, commit the reviewed index with plain `git commit -m <message>` and no
  pathspec. `git commit -- <path>...` takes the named files from the worktree instead of the staged
  hunk selection and can absorb another session's unstaged hunks. A commit pathspec is safe only
  when the entire worktree diff of every named path belongs to that commit.
- A child or worker session does not commit. It hands its changes back to the root session for
  review and commit.

### Root-owned landing closure

- A child task reaching `success`, and a reviewer saying `SAFE TO LAND`, mean **delivered** and
  **reviewed**. Neither means the user's code change is complete. The root session that dispatched
  the graph owns integration until the intended target branch contains the reviewed change.
- Plan code-producing graphs through the root-owned landing step: name the delivery branch, target
  branch, landing owner, independent review, and post-integration verification. The last child may
  be a reviewer; the last step of the work is still the root's landing closure.
- When claimed child work comes back, use its task secret with
  `POST /v1/orchestrator/tasks/:id/landing` to mark the obligation `pending`; a named root that later
  accepts a handoff may use the machine-level orchestrator token instead. This makes the obligation
  visible in `GET /v1/orchestrator/landings` but does not block anyone.
- Before reporting completion, the root must integrate without absorbing another session's dirty
  files, test the exact integrated tree with a private `TMPDIR`, and record the resulting target
  commit. Then mark that same landing record `landed` with the commit. `SAFE TO LAND` is a pending
  state, not a completion phrase.
- **HEAD must compile standing alone, and a commit is the only thing that can break that.** It
  happened twice on 2026-08-26, from two different sessions: a whole-file `git add` carried three
  lines whose type was defined in a file that stayed uncommitted, and a protocol requirement landed
  in `Strings.swift` while its fourteen values stayed in the worktree. Both trees were green at the
  moment of committing. **A green tree says nothing about HEAD while anything is uncommitted** — the
  tree is the union of everybody's work and HEAD is only your slice, so a suite run in the tree is
  answering a question nobody asked.
  A partial commit is therefore not finished until its own slice has compiled on its own. Verify the
  staged tree the way this file describes, and where a change spans files ask what else defines what
  you are taking: a declaration without its values, a call without its function, a case without its
  enum — each of them passes in the tree and fails in HEAD.
  Recovering another session's half-landed commit is legitimate root work: restore the missing half,
  or lift the orphaned lines back into the worktree where their owner can still see them. Say in the
  message that it is not your line's work and why HEAD could not wait for its owner.
- If overlapping uncommitted work makes integration unsafe, do not merge and do not close the task.
  Keep the landing obligation pending while coordinating with the owning session. If this root must
  stop, use a Clawdline handoff that names the delivery branch/base/head, target branch, verdict and
  test evidence, overlapping paths and owner if known, and the one next landing action. Never leave
  integration to an unnamed future session. The original root remains owner until Clawdline's
  handoff receipt confirms that the first line reached the named receiving root.
- File-release coordination goes through Clawdline, never an assistant provider's native message
  mechanism. Address the terminal-neutral session `id`, which an agent reads from
  `GET /v1/orchestrator/sessions` with the local orchestrator credential — `GET /v1/sessions` lists
  the same ids and is the paired-device route, so it answers that credential with `401
  unauthorized`. The durable wait routes deliver request and release messages so Claude and Codex
  participate equally.
- Register a wait with Clawdline's durable coordination-wait route, naming the repository, exact
  paths, owner and waiter Clawdline session ids, reason, and release condition. Clawdline persists
  and deduplicates the relationship, delivers the request, and exposes it on both Session records.
  The owner explicitly releases it through Clawdline after committing or otherwise releasing the
  paths; Clawdline fans the release notice out to every waiter and records partial delivery so a
  retry does not notify successful recipients twice. A notice wakes the waiter; it never replaces
  the waiter's own HEAD/status/diff verification. Never infer release from a clean worktree sample.
- A peer wait is the Session's `coordination.state = waiting_on_session` overlay, not its terminal
  `state`. The latter remains `idle`, `working`, or `waiting`; `waiting` still means the assistant
  needs an answer from the person and is the only form that earns the loud UI and push alert. Native
  and web session rows quietly show the owner and release condition, so a person knows the
  idle-looking session is parked and should stay open. When that UI is unavailable, the fallback
  user-visible message ends with `⏳ [Clawdline waiting] <owner> — <condition>; please keep this
  session open.`
- The living Claude Code Artifact for this protocol is
  `artifacts/2026-08-26-clawdline-communication-protocol.html`. Any change to Clawdline task,
  handoff, landing, claims, file-wait or cross-session communication semantics must update that
  standalone `kind=state` HTML in the same line of work and re-check it against the authoritative
  docs. A protocol change is not closed while its Artifact still teaches the previous behavior.
- Check `GET /v1/orchestrator/assistants` before dispatching, and read a `409
  assistant_exhausted`'s `alternatives` before retrying the same assistant. This closure still
  applies when a child dies mid-task because its assistant ran out of quota: whatever it had not
  committed is root's to recover or discard, exactly as with any other child that never reported.

The working tree is an editing buffer shared with other sessions, not a reproducible build input.
**What you verify depends on which half of this protocol you are in, and the two are not the same
question.**

**A child asks "does what I wrote work?"** The working tree is the right subject — other sessions'
half-finished edits included, because a child does not commit and their mess cannot reach HEAD
through it. Snapshot it *without touching the shared index*:

```sh
snapshot_dir=$(mktemp -d); test_tmp=$(mktemp -d)
git archive "$(git stash create)" | tar -x -C "$snapshot_dir"
# stash create holds tracked files only. Untracked ones the suite needs — a new test file another
# session has written but not committed — are absent, and the run fails on something nobody broke.
git ls-files --others --exclude-standard -z \
  | tar --null -T - -cf - | tar -xf - -C "$snapshot_dir"
(cd "$snapshot_dir" && TMPDIR="$test_tmp" ./test.sh)
```

That second line is not optional and was found the hard way: the first child told to follow this
recipe hit `test.sh` requiring `Tests/web-schedules.mjs`, which existed only as an untracked file
from another session. It reported the gap instead of quietly working around it, which is the right
thing to do with a rule that does not fit — the rule was wrong, not the situation.

`git stash create` writes a commit object holding the working tree and leaves the index exactly as
it found it; it prints nothing when the tree is clean, so fall back to `HEAD`. **A child must not
use `git write-tree` for this.** That reads the *index*, so it requires staging first — and the
index is shared. A child staging its own files sweeps up whatever another session left in there,
and then a root commits it. That has happened here.

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

A new test must be seen red before the change that makes it green. A test born green proves
nothing: reviews here have repeatedly found suites that stayed green after the guarded logic was
replaced with a stub — or deleted outright. Break the thing once, watch the test catch it, then fix it.
Do not build from the live working tree, because it may contain another session's partial edits or
untracked files.

## Dispatching substantial work with Clawdline

Use Clawdline for work that can be split into self-contained tasks and joined later.
Create `/tmp/.clawdline/<task-id>/task.json`, then register it with `POST /v1/orchestrator/tasks`.
Put the complete instructions in `task.json`; the POST body carries only `task_id` and the task secret.

Declare every path a task may write in `claims`, relative to `project_dir`:

```json
"claims": ["Sources/Feature.swift", "Tests/FeatureTests.swift"]
```

Claims reserve equal, ancestor, and descendant paths across separately identified root trees.
A conflict is rejected before a child starts with `409 workspace_busy`, including the blocking
task, root context, conflicting absolute paths, and retry advice.

**There are two ways to get `claims` wrong and only one of them is loud.** Leaving it out is the
quiet one: the broker cannot prove two tasks are disjoint, so it falls back to warning about every
pair sharing a directory — on 2026-08-26 that was a dozen notices in an evening, not one of which
described a real conflict, while the single genuine collision that night was caught by
`workspace_busy` in the same breath as the dispatch. **Dispatching without `claims` is not the
cautious choice; it is the one that produces noise instead of an answer.**
Claiming too widely is the other, and the broker now names it: a task that finishes without ever
touching a claimed path is reported as such. A path claimed and unused blocks other trees for
nothing, so take the report seriously and narrow next time — this is the same error as the first
one, pointing the other way.
Claims are a dispatch gate, not filesystem enforcement; instructions must still restrict the child
to its declared scope.

Use `serialize` for machine-global operations rather than files:

```json
"serialize": ["build"]
```

Tasks sharing a serialization token queue in creation order and acquire all requested tokens together.
Use it for operations such as builds whose fixed outputs can collide even when source claims do not.

Branch on the orchestrator's typed error `code`:

- `over_capacity`: wait for `retry_after`, reduce the batch, or send it in stages.
- `depth_exceeded`: stop dispatching; this session is at the tree's depth limit.
- `workspace_busy`: do not start; wait, coordinate with the named root, narrow the claims honestly,
  or ask the side that's blocking you to give up the conflicting paths early with
  `POST .../claims/release` (see `docs/orchestrator.md#releasing-claims-early`) — the only way to
  break a circular wait where two roots each hold what the other one needs.
- `bad_task`: correct the invalid `task.json` field and resend the same task id.

The complete task schema, claim and serialization semantics, lifecycle, credentials, and result
protocol are in [`docs/orchestrator.md`](docs/orchestrator.md).
Children may use their task secret to push rare, time-sensitive content with `POST /v1/orchestrator/tasks/:id/notify`.
Routine output stays in `result.json`; notification-shaped work such as a daily forecast should say explicitly to notify.
The user can turn agent notifications off; on `409 agent_notify_disabled`, do not retry, leave the content in `result.json`, and report the refusal honestly.
HTTP request and response shapes are in [`docs/api.md`](docs/api.md#post-v1orchestratortasks).

## When Clawdline dispatched you

If the first message names you as a Clawdline child, read the task's `CHILD.md` before doing anything else.
`CHILD.md` is authoritative for that task and overrides this file where they differ.
Stay inside the named project paths and your own task directory; do not inspect other `/tmp/.clawdline` tasks.
Write the requested artifacts and write `result.json` last, exactly as the briefing specifies.

## Never

- Never run `git commit`, `git reset`, `git checkout`, or `git stash` from a child or worker
  session; integration belongs to the root.
- Never run `./build.sh`; it replaces and restarts the user's running app and can interrupt other
  dispatched sessions.
- Never alter, stage, discard, or claim another session's pre-existing uncommitted work.
