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

## How big one task is

**One task is one coherent, independently reviewable feature slice** — production change, red-before-
green tests, docs and its own verification, through one sustained session. Keep the implementer there
until the slice is mature; new discoveries go to that session, not a new tab.

**A slice big enough to dispatch is big enough to lose.** `timeout_minutes` stops at 240, quota can
run out mid-task and a context window can fill. So a long or multi-file slice goes out with
`isolation: "worktree"`, commits each milestone on its delivery branch, and reports through
`/progress` when the work stops matching its title. A death in hour three then costs one hour.

**Never open a session for one small change.** Small items pool, and the pool empties when any of
these is true: five items are waiting; they are together worth more than about thirty minutes; or
one blocks a landing or somebody is waiting on it. **No item sits longer than 24 hours** — otherwise
"accumulate" quietly becomes "never". One task carries the whole batch, `claims` is the union of what
it writes, and the instructions list the items separately so the result can report on each one.

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

## Which assistant, which model

- **Codex** for *making* something you then look at: code, an image from its built-in image model, a
  hand-written SVG, a build driven to green, mechanical edits across many files. It cannot be told
  where to save a drawing — say: generate it, then copy it into `artifacts/`.
- **Claude** for reading and judging: a diff, why something behaves as it does, prose.

`haiku` for mechanical single-source work where being wrong is obvious; `sonnet` is the default for a
leaf; `opus` for a decision somebody acts on without checking, and for any synthesis of several
children's answers. Name a model only when the default is the wrong size, and say why in the plan.

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
