# How work is handed out on this Mac

Clawdline reads this file at every dispatch and copies it into the briefing of every child that may
dispatch in turn. Edit it freely; an empty file means there are no house rules. **It is cut at
12,000 characters, at a paragraph break** — but keep it far under that, because it is pasted beside
the task's own instructions and every line of it competes with them for a child's attention.

This file is only about **handing work out**. Landing, shared-tree discipline, file waits and how a
child verifies its own work belong to the repository's rules and to the child's briefing, not here.

## Should this be dispatched at all?

The measurement is sharp both ways: on work that splits into independent pieces, several agents beat
a single one by **80.9%**; where every step depends on the last, *every* multi-agent arrangement
tested was **39–70% worse**, because the handoffs break a chain that needed to stay whole. So: **can
this be cut into pieces that need not talk to each other, and joined at the end?**

**When the answer is no, that is a recommendation and not a refusal.** Give the reason in a sentence,
ask, then do whatever they answer — **their yes settles it**, and what is owed is the reason once,
before the work starts.

**And ask it as options, not as prose.** Any decision genuinely the user's — this one, which design,
who adopts an orphaned line — reaches him in his own session as an explicit options prompt: one
question at a time, each option naming what happens if he picks it, your recommendation attached.
Asked inside a paragraph it does not arrive; his words are «我會漏掉，我不知道怎麼回答». **Technical
to-dos and user decisions are two lists, never one** — mixed, it is always his half that is lost.

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
bounded child it dispatches, Clawdfather continues to own decomposition, independent review,
exact-tree integration and landing closure.

## How big one task is

**One task is one coherent, independently reviewable feature slice** — production change, red-before-
green tests, docs and its own verification, through one sustained session. Keep the implementer there
until the slice is mature; new discoveries go to that session, not a new tab.

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
- **Ask every task for one progress note in its first three minutes** — the task-directory file for
  a stock codex sandbox, whose outbound connections are blocked, or either channel when this
  machine's `dispatch-policy.local.md` says network access was opened; its briefing carries the one
  that works. Say what it has decided
  to do now it has read the briefing. One round, and it is the only thing that lets a wrong
  direction be cancelled at minute three rather than minute twenty-six: the two dearest cancelled
  tasks measured on one machine burned 18.5M and 16.5M tokens before anybody could tell.
- **An interrupted review is handed over, not restarted.** A reviewer that died or was cancelled has
  usually written part of its findings; hand that file to whoever picks it up. Review is both the
  most expensive node and the one most often thrown away — 30 of 101 review dispatches on one
  machine never returned a verdict.

## Which assistant, which model

- **Codex** for *making* something you then look at: code, an image from its built-in image model, a
  hand-written SVG, a build driven to green, mechanical edits across many files. It cannot be told
  where to save a drawing — say: generate it, then copy it into `artifacts/`.
- **Claude** for reading and judging: a diff, why something behaves as it does, prose.

The choice has a price as well as a fit. Codex work is billed against a plan and Claude work per
token: on one machine 84% of dispatches ran on Codex for nothing, and the whole bill came from the
16% on Claude. So "Codex makes, Claude reads" is not only about which is better at what — sending
making-shaped work to Claude is the most expensive thing you can do by accident. Where both would do
and nothing has to be weighed, it goes to Codex.

`haiku` for mechanical single-source work where being wrong is obvious; `sonnet` for a leaf with
judgement in it; `opus` for a decision somebody acts on without checking, and for any synthesis of
several children's answers.

**Always name the model on a Claude dispatch — `opus` unless you can say why not.** Omitting it
inherits whatever `/model` is set to on this Mac at that moment: nobody chose it for that task and
nothing records it. Three dispatches ran on `claude-fable-5` that way on 2026-08-28. A named model
can be argued with; an inherited one cannot even be seen.

## Somebody has to check the work

Every graph producing code, or a decision anybody acts on, ends with a node whose only job is to find
what is wrong with it — reading a complete feature or a complete batch, never a fragment, and
returning the whole finding set in one pass.

- **It did not help build the thing.** A model judging its own output misses about a third of its own
  semantic drift, structurally: a judge favours low-perplexity text and its own output is
  low-perplexity to it by construction.
- **A different assistant helps and does not solve it.** Nine frontier models carried about two
  votes' worth of independent information; where review really matters use several, complementary.
- **Opus-class, always** — an absolute floor, not "no weaker than what it judges". Measured here: a
  Sonnet reviewer, mid-way through explaining that judging hallucinates, invented a citation.
- **Name the paths it may read** — the exact `artifacts/` directories, and for a batch every branch
  and head. **A verdict with receipts**: worst first, is it safe to ship, every finding naming the
  passage it rests on. A verdict without sources is the shape a hallucinating judge produces.

**Then the reviewer repairs what it found, and root reviews the repair.** The finding set is written
down and reported **before a single byte is repaired**: a repair made while the findings are still in
the reviewer's head buries the judgement somebody needed to see. Once it has edited production bytes
its verdict is spent — that repair is a delivery, and the focused diff and exact-tree acceptance are
root's. It repairs only what does not change the design; a design-changing correction goes back to
the implementer's session. Never one task per finding.

**One review round per feature or batch.** Parallel complementary reviewers count as one round.
Measured on one line here: the implementation cost $30.90 and its four review rounds cost $57.39 —
**1.9x the thing being reviewed** — and both correction rounds introduced defects the next review
caught, so the rounds were not merely expensive, they were part of what made themselves necessary.
Seal findings before correction; disjoint fixes remain one wave, and confirmation reruns only named
questions. A second round needs a recurring defect class. A third needs `scope_changed`,
`new_external_evidence`, or `systemic_pattern`. If the same class escapes again, stop at
`architecture_hold`; do not dispatch a fourth patch.

**The landing root owns the normal full suite.** Children use focused proof. Until a focused Swift
runner ships, an implementer may run one full suite only when labelled
`focused_runner_unavailable`; reviewers do not repeat it. Root tests the exact target candidate.
Never repeat a green tree/question/environment tuple; a second run needs a typed
`inconclusive_environment` receipt.

**A child may add Swift assertions, and the seal is not the reason it cannot.** Adding a `check(`
or `expect(` to `Tests/*.swift` moves `expected_swift_receipt_witness`, and the architecture guard
refuses to start a compile while the witness names a different tree — which reads like a wall,
because a child cannot run the full suite that would produce a new total. **`CLAWDLINE_RESEAL=1` is
the door built for exactly this**: it downgrades that refusal to a warning and lets the run proceed,
so a child runs its focused groups normally and leaves both `expected_swift_receipt` and the witness
untouched for the root to set from the landing run. Verified 2026-09-03: without the flag the guard
prints "the seal was measured somewhere else"; with it, "Letting the run proceed so it can report
the real total", followed by the ordinary green summary. One delivery that day wrote down that it
had added no assertions **because it believed the seal forbade it**, and lost the coverage to a
limit that does not exist.
