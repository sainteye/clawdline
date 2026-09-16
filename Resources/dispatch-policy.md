# How work is handed out on this Mac

Clawdline reads this file at every dispatch and copies it into the briefing of every child that may
dispatch in turn. Edit it freely; an empty file means there are no house rules. **It is cut at
16,000 characters, at a paragraph break** — but keep it far under that, because it is pasted beside
the task's own instructions and every line of it competes with them for a child's attention.

This file is only about **handing work out**. Landing, shared-tree discipline, file waits and how a
child verifies its own work belong to the repository's rules and to the child's briefing, not here.

## Before that: read what is already here

`GET /v1/orchestrator/inventory?project=<repo>` answers `live`, `unlanded` and `droppable`, and
every row carries a `do` the server would accept. **It is not optional**: both dispatch routes
refuse a body without its current `inventory_generation` as `409 stale_inventory`, and that
refusal carries the whole inventory back, so recovering is one round trip. A row that looks like
your job means stop and say so — 26 unrecorded landings and 10 deliveries re-done from scratch in
one day were all named correctly by a route nobody was made to read.

## Should this be dispatched at all?

The measurement is sharp both ways: on work that splits into independent pieces, several agents beat
a single one by **80.9%**; where every step depends on the last, *every* multi-agent arrangement
tested was **39–70% worse**, because the handoffs break a chain that needed to stay whole. So: **can
this be cut into pieces that need not talk to each other, and joined at the end?**

When the answer is no, keep the chain in one Session. If the user explicitly requested delegation,
state the dependency once and follow that request; do not turn an internal execution choice into a
new approval prompt. Genuine product/design decisions still go to the user as one explicit options
prompt, separate from technical to-dos.

**Clawdline Agent**, **dispatch**, **new tab**, **independent task**, **派 Agent／派下去** all mean
`POST /v1/orchestrator/tasks`: a broker task id and an ordinary assistant session in its own tab.
Codex `thread_spawn`, Claude sidechains and other provider-native children do not satisfy it, and a
failed dispatch is reported and retried, never silently replaced by one.

Shapes that look dispatchable and are not: diagnosis (every step is chosen by what the last one
found), dozens of small parallel jobs, anything on a path where somebody is waiting, agents that
need to talk back and forth, output a program has to parse, and work smaller than its own briefing —
which does not vanish, it accumulates. See below.

**Clawdfather** is the exception to that single-session bias: it holds the machine-wide picture and
sends substantial diagnosis, implementation, research and review out as separate tasks whenever
capacity allows. It also owns the small-work pool below.

**A major Feature needs a visible independent owner.** The dispatch roles are a closed contract:

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

Provider-native subagents remain useful for short, disposable, normally read-only research,
calculation or focused review with no independent delivery. Announce them honestly and never call
them a Clawdline dispatch. If Clawdline refuses the task or is unavailable, report the typed failure
and wait, retry or ask; do not silently turn the Feature into invisible delegation. For every
bounded child it dispatches, Clawdfather continues to own decomposition, any risk-triggered review,
exact-tree integration and landing closure.

## How big one task is

**One task is the largest coherent, rollback-safe user outcome or architecture boundary that one
owner can carry safely.** A file, test, checklist row, finding or small correction is not a slice.
Carry related production changes, tests and docs in one sustained session, then pay for the one
test run at the commit or the release build. Keep the implementer there until the whole unit is
mature; discoveries and corrections stay in that session.

**A slice big enough to dispatch is big enough to lose.** `timeout_minutes` stops at 240, quota can
run out mid-task and a context window can fill. So a long or multi-file slice goes out with
`isolation: "worktree"`, commits each milestone on its delivery branch, and sends a progress note
when the work stops matching its title. A death in hour three then costs one hour.

**But only Claude can commit in that worktree.** A linked worktree's git metadata lives in the main
repo's `.git/worktrees/<task-id>/`, outside what a Codex sandbox may write, so every commit dies on
`index.lock: Operation not permitted` and the child reports failure holding finished work — again
today, after 4438 passing checks. Tell a **claude** worktree child to commit milestones; tell a
**codex** one to leave the bytes dirty for root. The briefing says "commit early and often" to both,
so the instructions must override it.

**Never open a session for one small change.** Small items pool, and the pool empties when any of
these is true: five items are waiting; they are together worth more than about thirty minutes; or
one blocks a landing or somebody is waiting on it. **No item sits longer than 24 hours** — otherwise
"accumulate" quietly becomes "never". One task carries the whole batch, `claims` is the union of what
it writes, and the instructions list the items separately so the result can report on each one.

**A recurring chore is a batch that repeats.** The pool rule is written for work that turns up once,
and scheduled work slips past it: every run is a fresh session paying the whole fixed cost, daily.
Measured on one machine, five runs of three daily chores cost 4.0% of everything it had ever
dispatched, to produce a rounding error of output. Run them as follow-up tasks in one standing
session, kept as separate tasks so a failed chore does not take the others with it.

**Standing sessions** — one kept open between jobs for odd jobs, one for review — take work **only
as an attached follow-up task** carrying its own id, secret, `claims` and `result.json`.
`POST /v1/sessions/:id/send` is not that: it makes no task record, so what it feeds in holds no
claims, signals no completion and is counted in no usage. **Without such a task a standing session
must not write to the shared tree at all**, which review satisfies by construction and odd jobs does
not.

## Pick a shape

- **Split and join** — independent pieces gathered by leaves (`haiku`), one node joining and judging
  (`sonnet`+). The default for research, audits and surveys.
- **Build then read** — for code or a decision somebody acts on. Never the same node or session.
- **Decide then do** — one node plans touching nothing, a person passes it, a second implements. The
  value is the gap in the middle, where a person can still say no cheaply.
- **Batch with takeover** — one mechanical change across independent modules, one node each; a dead
  node leaves its state on screen for a person to finish by typing.
- **Candidates** — one problem, several differently-instructed nodes, a person picks. No judging
  node: what is compared is taste.

Plan the whole graph before dispatching any of it. **Breadth before depth** — two children splitting
a job beat one that hands half of it on. **Every node is told the whole graph**, which is what `plan`
is for: a leaf that knows what its output feeds writes a usable output, one that does not writes an
essay, and leaves are narrow enough to state in a sentence. **Stagger dispatches 30–45 seconds** or
they compete, and a tab that has not reached a prompt in four minutes is `spawn_failed`, whose retry
needs a fresh id and secret. **Say when you did it yourself.**
- **Do not ask a task to echo a clear briefing.** Use progress only for a material boundary change —
  the task-directory file for
  a stock codex sandbox, whose outbound connections are blocked, or either channel when this
  machine's `dispatch-policy.local.md` says network access was opened; its briefing carries the one
  that works. Send one short note only when the discovered write set, approach, dependency, risk
  or blocker materially differs from the briefing, or a long-running task needs an early root
  choice. Do not send periodic heartbeat notes.
- **An interrupted review is handed over, not restarted.** A reviewer that died or was cancelled has
  usually written part of its findings; hand that file to whoever picks it up. Review is both the
  most expensive node and the one most often thrown away — 30 of 101 review dispatches on one
  machine never returned a verdict.

## Which assistant, which model

Choose the assistant and model for the complete delivery unit, not by a fixed provider-role rule.
Use the strongest available reasoning where a high-risk design or review genuinely needs it and an
efficient capable model for routine work. Do not split a Feature merely to route different stages
to different models. Record an explicit model only when the dispatch intentionally overrides the
Session default.

## Check in proportion to risk

Use an independent review node for security/authentication, durable state,
concurrency/backpressure, migrations, destructive/external effects, or a broad cross-component
change. Routine localized code, docs, generated data and test-only corrections are read by their
owner. When independent review is warranted, it reads the complete feature or batch, never a
fragment, and returns the whole finding set in one pass.

**The review comes first and runs nothing.** It reads for design faults before any test run exists,
does not wait for a suite and is owed no test receipt. One correction pass answers the whole finding
set and does not go back to it.

- **It did not help build the thing.** A model judging its own output misses about a third of its own
  semantic drift, structurally: a judge favours low-perplexity text and its own output is
  low-perplexity to it by construction.
- **A different assistant can help and does not solve it.** Choose one capable independent reader
  for the whole risk boundary; multiple reviewers are exceptional, not a default.
- **Name the paths it may read** — the exact `artifacts/` directories, and for a batch every branch
  and head. **A verdict with receipts**: worst first, is it safe to ship, every finding naming the
  passage it rests on. A verdict without sources is the shape a hallucinating judge produces.

**Then the original implementer fixes the complete finding set in the same sustained Session.**
The reviewer writes the findings before any correction, but does not switch roles and implement
them. Root reads the correction itself. Never one task per finding.

**There is one review per delivery, and that was it.** Parallel complementary reviewers are still
that one review; what is refused is re-reviewing after a correction, which looks free and is not.
Measured on one line here: the implementation cost $30.90 and its four review rounds cost $57.39 —
**1.9x the thing being reviewed** — and both correction rounds introduced defects the next review
caught, so the rounds were not merely expensive, they were part of what made themselves necessary.
Seal findings before correction; disjoint fixes remain one wave. "Did the fix work" is answered by
the single test run, not by a second reader. If the same defect class escapes that wave, stop at
`architecture_hold` and tell the person who asked for the work; do not dispatch another patch.

**A brief for work that deletes, overwrites or releases states what the decision may rest on**, not
the shape somebody caught. Naming the shape invites the next patch to close that one spelling and
leave the class standing. Measured on 2026-09-11: a review named one symlink escape, the correction
closed it, and the confirmation found the same class inside the helper that correction introduced;
separately, "the process is gone" was fixed for a process table that cannot be read at all and the
same reading survived for a table that hides one pid. Two extra correction rounds, about two hours.
So write the basis — *a path is checked from its root as spelled before any other spelling of it*;
*a process is gone only when the system answers that there is no such process*; *unknown never
authorises a removal* — and ask, in the same task, for an audit of every site that decides the same
question.

**The landing root owns the release candidate's full suite.** Children accumulate related changes
and pay for one run at their commit; they do not compile once per assertion, file or finding.
Several compatible Feature slices share one exact release candidate rather than each paying for a
full suite. Docs-only or mechanically generated candidates that cannot affect compiled/runtime
behavior use their relevant static checks. Until a focused Swift runner ships, an implementer may
run one full suite only when labelled `focused_runner_unavailable`; reviewers run nothing at all.
Root tests the exact target candidate. A red run buys one correction and then the same run again;
if two corrections have not cleared it, stop and say so to the person waiting.
Never repeat a green tree/question/environment tuple; a second run needs a typed
`inconclusive_environment` receipt.

**A child may add Swift assertions without asking anyone to maintain a total.** That single run
records what it executed; the release-candidate run records its own observed Swift and Cloud counts.
No count is copied back into `test.sh`, README files or governance docs, and no RESEAL measurement
run exists. Completeness comes from the ordered group/runner/suite rosters and a unique runtime
receipt, not from comparing this tree with the previous tree's assertion number.
