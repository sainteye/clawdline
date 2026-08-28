# Handing work out with Clawdline

Read this **before you dispatch**, and only then. It carries how to decide whether to dispatch at
all, how big one task should be, when small work is batched instead, the graph shapes, `claims` and
serialization, and what to do when the broker refuses.

It was part of `AGENTS.md` until it was measured: across 206 dispatched tasks on this machine, every
child was briefed with the whole of it and none of them ever dispatched anything. Teaching that is
loaded before anybody needs it is paid for by every session and used by almost none, so it lives
here now and `AGENTS.md` points at it. What stayed there is what cannot wait to be looked up:
prohibitions, which apply exactly when you were not going to open a file.

The three rules that must not be lazy: a Clawdline dispatch is never satisfied by a
provider-native subagent, a sandboxed loopback failure is not proof that Clawdline is down, and a
recurring stall is not closed by widening a timeout. Those are still in `AGENTS.md`.

## Clawdfather coordinates before it executes

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

## Dispatch feature-sized work, not fragments

A new terminal tab has a real fixed cost: assistant startup, repository and `CHILD.md` reads,
context reconstruction, snapshot setup, and a completion/landing lifecycle. Do not spend that cost
on a single small finding, one mechanical file edit, one probe, or work whose useful part is smaller
than the briefing needed to explain it. Parallelism is not a goal by itself, and Clawdfather must
not fill every available slot merely because the slots exist.

- Make the ordinary implementation node a coherent, independently reviewable feature slice. It
  should normally carry its production change, red-before-green tests, relevant docs/Artifact,
  mutations or failure injection, and its own verification/report through one sustained session.
- Keep a live implementer on that feature until the whole slice is mature. Add closely related
  discoveries and corrections to the same session instead of opening another tab for each one.
  Tiny work that cannot justify a full briefing remains root work.
- **A slice big enough to be worth dispatching is big enough to lose.** `timeout_minutes` stops at
  240, an assistant can exhaust its quota mid-task, and a context window can fill; a four-hour node
  that dies in its third hour costs everything it never committed. A slice expected to run long, or
  to touch several files, is therefore dispatched with `isolation: "worktree"`, commits each
  milestone on its delivery branch rather than at the end, and says through `/progress` when the
  work stops matching its title. Root can then continue from the branch instead of starting again.
  Cutting work larger without this turns one failure into a total one.
- Dispatch one independent reviewer after the complete feature is delivered, not a sequence of
  reviewers for intermediate fragments. A reviewer inspects the whole feature boundary and returns
  the complete finding set in one pass.
- On `CHANGES REQUIRED`, the reviewer repairs what it found and root reviews the repair. The order
  is not negotiable: **the complete finding set is written down and reported to root before a single
  byte is repaired.** A repair made while the findings are still only in the reviewer's head buries
  the judgement somebody needed to see, and root is left looking at a corrected diff with no record
  of what was wrong with it. Once the reviewer has edited production bytes its verdict is spent and
  it cannot approve its own repair: that repair is a delivery, and root performs the independent
  focused diff, mutation and exact-tree acceptance, opening another reviewer when the risk warrants
  it. The reviewer repairs only findings that do not change the design; a broad or design-changing
  correction goes back to the original implementer's session, where the reasoning behind the code
  still is. Never create one task per finding — one correction round carries the whole set.
- **An interrupted review is handed over, not restarted.** Review is the most expensive node here
  and the one most often thrown away: of 101 review dispatches on this machine 30 never returned a
  verdict, and one re-review spent 6.7M tokens re-reading 1.9M tokens of work somebody had already
  read. A reviewer that dies, times out or is cancelled has usually written part of its finding set
  already; give that partial file to whoever picks it up instead of paying for the reading twice.
- Open a different implementation tab only for a genuinely independent feature, required worktree
  isolation, independent review, an assistant/session that died or exhausted quota, or context that
  is demonstrably no longer safe to continue. Record which exception justified the extra session.

Task planning and review reports should expose the fixed-cost side as well as useful output:
session/tab starts, briefing and repeated-context tokens, elapsed useful work, continuations, and
micro-task warnings.

#### Small work accumulates before it is dispatched

Never open a session for one small change. Small work goes into a pool and is dispatched as one
batch, and the pool empties when any of these is true:

- five items are waiting, or
- the items together are worth more than about thirty minutes of work, or
- one of them blocks a landing, or somebody is waiting on it.

And a ceiling, so that "accumulate" does not quietly become "never": **no item sits in the pool for
more than 24 hours.** One task carries the whole batch, its `claims` are the union of what the batch
writes, and its briefing lists the items separately so the result can report on each one. A batch is
a legitimate review target in its own right: a reviewer reads the whole batch boundary in one pass,
exactly as it reads one feature.

**A recurring chore is a batch that repeats.** The pool rule was written for work that turns up
once, and scheduled work slips straight past it: every run is a fresh dispatch paying the whole
fixed cost of a session, every day. Measured here: five runs of three daily chores — a weather push,
a publishing check, a post — cost 9.4M tokens and $8.52 between them, 4.0% of everything this
machine has ever dispatched, to produce 51k tokens of output. Run them as follow-up tasks inside one
standing session rather than one session each, and keep them separate tasks inside it so a chore
that fails does not take the others with it.

`docs/backlog.yaml` is where an item worth keeping is written down — its `severity` x `cost` lane is
already this repository's only ranking of work, and nothing here lets anybody type a priority
directly. The pool belongs to the root, or to Clawdfather where one is registered, and it is named
in the report: how many items the batch carried, and what is still waiting.

#### Say what you are about to do, in the first three minutes

Every dispatched task sends one `/progress` note as soon as it has read its briefing and decided how
to proceed — before the work rather than during it. It costs one round.

That note is the only thing that makes an early cancellation possible. Measured here: the two most
expensive cancelled tasks on this machine burned 18.5M and 16.5M tokens and ran twenty-six minutes
each before anybody could see they were going the wrong way, because the protocol asked for a note
only when the work stopped matching its title — a signal that arrives after the divergence rather
than at it. A wrong first sentence is visible at minute three, and it is the cheapest thing in this
system to correct.

#### Standing sessions

A session may stay open between jobs rather than being closed once it reports. Two roles are worth
keeping alive: an **odd-jobs** session that takes the small batches above, and a **review** session
that takes each finished feature or batch. Both hold their tab through
`orchestrator_child_linger: -1`, and both carry the role in the task's `kind` (`odd-jobs`,
`review`), so a reader can tell what a tab is for.

**Work reaches a standing session only as an attached follow-up task** — a complete task record with
its own id, secret, `claims`, `timeout_minutes` and `result.json`, dispatched into the existing
session instead of into a new tab. `POST /v1/sessions/:id/send` is not that. It is the paired-device
route, it creates no task record, and work fed through it holds no claims, produces no completion
signal, appears in no `inflight` answer and is counted in no usage. A standing session fed that way
is invisible to every safety mechanism in this file, which is worse than a session that costs a tab.

That is also the boundary of what a standing session may touch: **with no follow-up task carrying
`claims`, it must not write to the shared tree at all.** A review session satisfies that by
construction, because a reviewer produces findings and not bytes. An odd-jobs session does not,
which is the whole reason the attached-task mechanism has to exist before one is kept alive.

Until that mechanism is in `HEAD`, the honest approximation is the batch above: one ordinary task
per emptied pool, one ordinary task per review round. Do not describe a session kept alive by hand
as a standing session, and do not claim Clawdline can reattach work to an existing session before
the field that does it has landed.

Declare every path a task may write in `claims`, relative to `project_dir`:

```json
"claims": ["Sources/Feature.swift", "Tests/FeatureTests.swift"]
```

Claims reserve equal, ancestor, and descendant paths across separately identified root trees.
A conflict is rejected before a child starts with `409 workspace_busy`, including the blocking
task, root context, conflicting absolute paths, and retry advice.

**Declaring `claims` is the cheapest thing in this protocol, and most dispatches skip it.** It
costs the root about twenty output tokens. Skipping it costs the broker its only way to prove two
tasks are disjoint, and a collision costs a whole task — between three and eighteen million tokens
on this machine's record. Measured: 60.7% of dispatches here declared nothing at all, and the price
was paid in full at least once, when two roots corrected the same delivery six seconds apart and
neither could be refused, because repository-relative claims are discarded for worktree-isolated
tasks and `/inflight` was still empty for both.

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

## A `workspace_busy` refusal is not a scheduling verdict

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
