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
- Once a root session has completed the task it was originally assigned, reviewed its own diff,
  and verified the exact commit in isolation, it should commit its completed work without waiting
  for a separate user request to commit. This permission applies only when the root can isolate its
  own changes from every pre-existing or unrelated worktree change; otherwise it must keep the work
  uncommitted and coordinate with the owner of the overlapping paths.

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

A new test must be seen red before the change that makes it green. A test born green proves
nothing: reviews here have repeatedly found suites that stayed green after the guarded logic was
replaced with a stub — or deleted outright. Break the thing once, watch the test catch it, then fix it.
Do not build from the live working tree, because it may contain another session's partial edits or
untracked files.

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

### Clawdfather coordinates before it executes

The registered Clawdfather is the machine-wide context owner. Its scarce resource is attention
across sessions, tasks, waits, landings, failures, and user decisions—not keystrokes in one
implementation. It should personally do quick inventory reads, decomposition, synthesis,
conflict resolution, landing review, and final verification. It should dispatch substantial
diagnosis, implementation, research, and independent review as separate Clawdline tasks whenever
capacity and authority allow, even when the delegated work is sequential internally.

Do not apply the ordinary "diagnosis is often faster in one session" heuristic to make
Clawdfather absorb a long investigation. Give one diagnostic task the evidence and a self-contained
question, let that task preserve its own reasoning chain in its own tab, and keep Clawdfather free
to understand the rest of the machine. Work smaller than a clear briefing remains reasonable for
Clawdfather to do directly. Clawdfather still owns the result: compare it with live evidence,
choose follow-up work, integrate safely, and never equate delegation with completion.

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
- `workspace_busy`: do not start **that shared-tree dispatch**. Treat the refusal as a choice of
  execution topology, not as proof that the work itself must stop. Apply the decision order below;
  when a shared-tree wait is genuinely required, coordinate with the named root or ask it to give
  up completed paths early with `POST .../claims/release` (see
  `docs/orchestrator.md#releasing-claims-early`) — the only way to break a circular wait where two
  roots each hold what the other one needs.
- `bad_task`: correct the invalid `task.json` field and resend the same task id.

### A `workspace_busy` refusal is not a scheduling verdict

Clawdfather and every dispatching root optimize for the fastest **safe completion**, not for the
fewest concurrent tasks. After `409 workspace_busy`, decide in this order:

1. **Narrow or split first.** If the task does not truly write every conflicting path, correct its
   claims or split independent discovery, implementation, and review nodes. Never narrow a claim
   while leaving instructions that still authorize the child to write the path.
2. **Isolate work that can start from a stable commit.** If the implementation does not need the
   blocking root's uncommitted bytes, resend it with `"isolation":"worktree"` and an explicit
   `isolation_base`. The broker will discard repository-relative claims because the child edits a
   separate checkout; keep the intended write set in the plan and instructions so root review
   still has an exact scope. A dirty-base warning means the child does not see those working-tree
   changes, not that isolation failed.
3. **Wait only for a real data dependency.** Register a durable Clawdline file wait when correctness
   depends on the blocking root's unfinished version, or when the eventual integration cannot be
   reviewed without it. Name the exact path and release condition; do not infer release from a
   transient clean status.
4. **Keep implementation and integration separate.** Worktree isolation can finish implementation
   while a shared path is owned, but it never authorizes an early merge. The root still waits for
   release, reviews the blocker and isolated delta together, resolves conflicts, tests the exact
   integrated tree, and completes the landing receipt.

Do not use worktree isolation to evade `serialize` for builds or another machine-global resource.
Isolation changes the checkout; it does not create a second Mac.

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
- A root may run `./build.sh` when the user explicitly asks to build or install. A child or worker
  session must not run it: the script replaces and restarts the user's running app and can
  interrupt work owned by another session. Before a root runs it, re-check `git status` and make
  sure its wildcard source and resource inputs will not absorb another session's uncommitted work;
  if they would, build the exact intended tree in an isolated snapshot or coordinate first.
- Never alter, stage, discard, or claim another session's pre-existing uncommitted work.
