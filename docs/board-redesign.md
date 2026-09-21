> **How the whole work system runs today (board items, session to-dos, the Backlog and GitHub Issues; what is built and what is only designed) is on one page in [`docs/work-system.md`](work-system.md), in Chinese.**

> **The implementation follows [`docs/design-decisions.md`](design-decisions.md) (from 2026-09-18 on); this document is kept as analysis, and where the two disagree, that one wins.**

# Board redesign: three structures — the board, session to-dos and the backlog — and their lifecycles

> **Where the requirement came from (the user's words on 2026-09-18, verbatim in the original Chinese, translated here)**
>
> "The board's biggest problems are these:
> 1. The user doesn't know whether something got created (I'm wondering whether the flow should judge whether this is important enough that the user ought to track it, and if so, ask)
> 2. Items that are too trivial should be for the session to look at
> 3. The lifecycle of board items isn't clearly defined, which leads to
>   a. trivial items nobody manages and nobody cares about
>   b. items nobody closes out once they have started, whose progress can't be seen
>   c. items that are already done, while the board system hasn't been updated at all
> 4. I think our process should let humans take part, understand and give instructions where appropriate; only that makes a complete board system
> 5. Items judged not to need human involvement should be created, tracked and ended fully automatically (like a TODO), and should not be mixed in with the items meant for people"
>
> Added the same day (verbatim in the original):
> "The board exists for two purposes:
> 1. Letting humans understand the situation
> 2. Making sure a session doesn't forget what it has to do"
> "I think things that are planned, but with no plan at all to start in the short term, should simply live in a different data structure, something like a Backlog or under some other name"
>
> Below, **#1, #2, #3a, #3b, #3c, #4, #5** refer to the five points above, **Purpose 1 / Purpose 2** to the two purposes, and **#BK** to the passage about the backlog.
> Every design decision says which of these it addresses.
>
> **This round produces documents only and changes no implementation.** Another child is porting the board page's front end, and this document does not touch its files.

What was measured: a read-only snapshot of the old app's board at **2026-09-18 01:02 UTC (09:02 in Taiwan)** (`revision 6403`, **787 cards** — the 776 in the task brief is an earlier number; over these eleven days it grew by about 40 cards a day on average),
plus `project-board-history/*.jsonl` at the same moment (564 files, 12,900 entries), `project-board-workflow.json` (the workflow log),
the state and landing fields of the 774 tasks in `~/.config/clawdline/orchestrator.json` (only id, state and landing were extracted; `secret_hash` was not read),
and the 14 live sessions from `:7727/v1/sessions`. Each card's progress was computed card by card on the snapshot with the `ProgressOf`/`ListSummaryOf` this repository has already ported
(`internal/adapters/board/progress.go`). The scripts and their output are listed in the appendix.

---

## 0. Five sentences

1. **All five of the user's points hold, and it is worse than they said.** **Not one** of the 787 cards can be shown to have been written by a person (the web UI has no button that changes a card at all);
   of the 564 with history, **52.8% never received another non-machine update after they were created**; of the old lifecycle `… → integrated → closed`,
   over eleven days and 787 cards, **0 cards reached `closed`**.
2. **What is broken is not "facts cannot get in" but "the lifecycle cannot close".** On the 610 cards linked to a broker task, the task state on the card and the broker's real state
   disagree **0 times**; but 345 (43.8%) are finished on the evidence while their stored lifecycle still sits in `execution`/`backlog`.
   The closing gate requires a kind of evidence almost nobody writes (verification records), so it can never be passed.
3. **One structure carrying two purposes is exactly the illness the user named.** The same card is at once a session's memo (checklists, obligations and declared spans are all written by sessions)
   and the progress a person wants to see; so what the person sees is the session's notes, and the session's notes are bound by the person's lifecycle rules.
   And one piece of work often has **two cards that are not linked**: only **12.1%** of dispatches are bound to a board item (**0 of 56** dispatches in the last two days),
   and only 31 of the 192 human cards (16%) connect to any dispatch — **human cards show no progress because the progress was never attached to them.**
4. **The design: three structures, each with one reader, one state machine and one table.** The board = what a person needs to know now; session to-dos = what a session must not forget,
   created, tracked and ended fully automatically; the backlog = what is planned but carries no "commitment to start". The three are linked by one work identity (`work_id`),
   and a move is a transaction with a trigger and an actor, written to the `moves` log. The line between the board and the backlog is **whether there is a commitment** (a dispatch, someone who took it on,
   a start date within seven days, a wait for a person's decision, or a delivery awaiting closure), which can be computed from facts; it is not an adjective.
5. **Sorting today's 787 cards by these rules gives session to-dos 602 (76.5%), board 108 (13.7%), backlog 77 (9.8%).**
   Of just the 185 meant for people: **backlog 77 (41.6%), awaiting a person to close 66 (35.7%), really happening now 31 (16.8%), finished 11 (5.9%)**.
   In other words, **fewer than one card in five on the human board is "now"**. The smallest acceptable first step is to build this sorting as a read-only projection,
   so the user can check the rules against their own data before anything is written (§9).

---

## 1. Is the user right? Answered point by point, in numbers

### 1.1 How it was measured, and definitions

| Term | Definition | Why it is defined this way |
|---|---|---|
| **Written by a person** | The history entry's `actor` is a device a person operates, and the write has a matching operation in the web UI | The old actor rule (`ProjectBoardHTTP.swift:260-268`): the local credential is always recorded as `machine`, a paired device as the device id |
| **Non-machine update** | Excludes five machine reconciliations: `item_created`, `automatic_state_reconciled`, `catalog_reconciled`, `task_reattributed`, `session_relation_confirmed` | These five are recomputation, not new facts (`board-design.md` §2.3 already measured them at 52.5%) |
| **Update after creation** | A non-machine event **more than 60 seconds** after `item_created` | Writes in the same batch as creation (linking a session, declaring a span, binding a task) are part of creating it, not progress |
| **Idle** | The snapshot time minus the last non-machine event | `updatedAt` keeps being pushed later by machine reconciliation (see §1.4), so it cannot stand for "someone is working on it" |
| **Has started** | Has a broker dispatch, a session delivery record, evidence, or a ticked checklist row | **A declared span does not count**: the workflow's `begin_template` defaults `phase` to `"output"`, so registering an item also declares "producing output" (see §1.7) |

### 1.2 #1 "The user doesn't know whether something got created" — **holds, and it is structural**

- **A person has no way to create one.** The old web page's only writes to the board are `set_enabled` and `set_ai_consent` (`input/board-settings.js:85,102`),
  and the product documentation says so outright: "There is no browser New button, edit form or manual status selector" (old repository `docs/project-board.md:30-31`).
- **Who wrote the 12,900 history entries:**

  | actor | Entries | Share | What it is |
  |---|---:|---:|---|
  | `board` | 4,771 | 37.0% | The store itself (creation, automatic reconciliation) |
  | `broker` | 2,944 | 22.8% | Dispatch facts |
  | `workflow:<provider>:<session>` | 2,870 | 22.2% | Written by sessions in conversation (codex 2,136, claude 734) |
  | `machine` | 1,851 | 14.3% | The local credential, that is, root or an agent writing with curl |
  | `root_attestation`/`root_report` | 268 | 2.1% | Root's verification statements and completion reports |
  | Paired device | 196 | 1.5% | **One device only, touching only 7 cards**, written in an agent's voice (for example: "The user has explicitly approved the specified payload + destination in the Session; S3 put-object succeeded…"), and the web page has no matching button — judged to be an agent writing through a device token |

  **Provably written by a person: 0/787.**
- **Who created them:** execution records the broker derived automatically from dispatches, **514 cards (65.3%)**; created explicitly by an agent, 273, of which 99 can be traced to a session conversation,
  59 were root using the machine credential or bound by the broker in the same second, 25 have only a creation event, 89 had no history file yet when they were created, and 1 is unknown. **Created by a person: 0.**
- **How agents open cards without people knowing:** in the old version every message sent through Clawdline carried a workflow envelope that asked the session to first classify the message as
  `existing_item|new_work|question|clarification`. Of the 256 retained runs, 236 had a `begin`, and in **47 of them (19.9%) the agent judged it `new_work` itself
  and opened a new card on the spot, without asking anyone**.
- **Was anyone asked:** of 360 obligations, only **7 (1.9%), on 4 cards,** are recorded as `actorKind: "user"` (a person must decide), and 5 of those are still open;
  277 recorded no actorKind at all. Workflow deliveries had `waiting_user` 15 times in 256.

> **Verdict: holds.** Severity: high. It is not just that the person "doesn't know whether it was created"; the person **takes no part in creation at all**; the only way of asking (`actorKind:user`) was used 7 times in ten days.

### 1.3 #2 "Items that are too trivial should be for the session" — **holds**

| Measure | Number |
|---|---|
| Single-task execution records derived automatically by the broker, with no checklist | **496/787 (63.0%)** |
| **No** non-machine update at all after creation (after 60 seconds) | **298/564 (52.8%)**; add the 124 with exactly one, and **422 have ≤1 (74.8%)** |
| Time from creation to the last non-machine event | **<10 minutes: 312 (55.3%)**, 10 minutes–1 hour 135 (23.9%), 1–6 hours 45, 6–24 hours 45, 1–3 days 25, 3–7 days 2, ≥7 days **0** |
| Percentiles of the same distribution | p50 **0** (everything written at the moment of creation), p75 0.76 hours, p90 14.9 hours, p95 23.6 hours |
| ≤1 update and finished within an hour | 107, **all of them agent cards** |
| Added per day | 379 in the initial import on 09-08; 408 over the ten days after, **about 41 a day on average** |

The old version already ran a row-by-row audit on 09-15 (carried out by a codex session; 465 cards then, 474 carrying `catalogDisposition` now),
sorting cards into `human`/`agent`/`archive` and folding the agent rows into an "Agent execution details" section collapsed by default. But that was only **a filter on presentation**: they were still in the same document, the same lifecycle and the same 2,000-card capacity,
and the counting and "nobody closes it" still happened to them (the 260 cards in §1.4).

> **Verdict: holds.** Severity: high. Two thirds of the board is the broker's own bookkeeping, and half of it never moved again after the moment it was written.

### 1.4 #3 "The lifecycle isn't clearly defined" — **all three failure modes can be measured**

**#3a Trivial, managed by nobody, cared about by nobody:**

- 298 cards with zero updates after creation (§1.3): agent 240, archive 42, human 16.
- By the rules in §7, **260 cards not meant for people (agent/archive 253, coordination records 7) are unfinished and will never be touched again**: delivered 186, blocked 54,
  unknown 17, execution 3. In the old version they would stay "not finished" forever.
- **The machine pretends they are moving**: the median of `updatedAt − createdAt` is **108 hours**, but the median of the real last event is the very second of creation.
  **362 cards have an `updatedAt` more than a day later than their last real event** — that is machine reconciliation touching them.
  Of 4,124 `automatic_state_reconciled` entries, **3,115 (75.5%) swing back and forth between `planning ⇄ execution`**, concentrated on 209 cards
  (208 of them automatically created task records) and in 143 distinct seconds (at most 88 cards in one second): noise from batch reconciliation.

**#3b Nobody closes it once started, and its progress cannot be seen:**

- **Sessions start and don't close**: the workflow log holds 528 runs (256 retained + 272 retired). **158 (29.9%) have not even a `begin`,
  and 230 (43.6%) have a `begin` but no `deliver`.**
- **Progress cannot be seen**: only **94/774 (12.1%)** dispatches carry a `work_item_id` binding them to a board item; after 09-15, 6/99 (6.1%); **on 09-17 and 09-18, 0/56**.
  Of the 192 human cards, only **31 (16.1%)** connect to any broker task. The rest of the progress happens on a separate agent card, and the two are not linked (§1.7).
- **Of the 181 unfinished human cards, 98 (54.1%) have had no non-machine update for more than 3 days**, and 26 for more than 7 days.
- **"In progress" is false**: of the 15 cards projected as active, **7 (46.7%) rest solely on a declared span, and the session that declared it no longer exists**
  (5 of them human cards). Of the 14 unfinished spans, 11 belong to sessions that no longer exist.

**#3c Done, but the board was not updated:**

| Measure | Number | Note |
|---|---:|---|
| Stored lifecycle reached `closed` | **0/787** | `integrated` 1, `verified` 8 |
| Projection finished (landed/settled/verified/canceled) but stored state still `execution`/`backlog` | **345 (43.8%)** | The mark a person sees and the lifecycle the system stores say two different things |
| The broker says every task has landed (`landed`/`nothing_to_land`), yet the card shows unfinished | **16** | blocked 12, delivered 2, correction 1, execution 1 |
| Cards projected `delivered` | 262 | Of these, **207 (79.0%) idle ≥3 days**; the tasks' landing fields: **169 have no landing record at all**, 64 have only the session's own claim of delivery, 25 abandoned |
| Task state on the card vs the broker's real state | **0 disagreements** (610 cards have a task link, and all 610 tasks can be found) | **The facts get in; what is broken is closing** |

Why it can never close: the old automatic reconciliation (`ProjectBoardStore.swift:3440-3505`) requires, for `closed`, all at once: an owner, no unresolved blocking finding,
**evidence on every row of the current checklist, verification evidence**, delivery (landing) evidence, and passing `closureIsSafe`. Verification evidence can only be written by root with `record_evidence`,
and in eleven days only 194 were written. **The gate is the right shape, but the evidence it wants almost never arises in the real flow, so in effect there is no exit.**

> **Verdict: all three failure modes hold.** Severity: high. Especially #3c: the person looks at the progress projection, the system stores a different state that never moves forward,
> and nothing between the two makes them converge.

### 1.5 #4, #5 "People should take part where appropriate; what needs no person should be fully automatic and not mixed in" — **today, neither half is done**

- **Instructions a person can give:** 0 on the board (only switches). A person's intent can get in only by "talking to the session and having the agent write it down", and that path fails to close 43.6% of the time.
- **The automatic part does not end automatically:** 260 agent cards are stuck unfinished (§1.4), because their end conditions are the same as those of human cards.
- **Mixed together:** the same document, the same capacity, the same lifecycle; the only separation is the UI's default collapse.

### 1.6 Summary table

| What the user said | Verdict | The number that says it best |
|---|---|---|
| #1 doesn't know whether it was created | **Holds** | Created by a person 0/787; agents judged `new_work` themselves and opened cards in 19.9% of begins; obligations asking a person, 7 in ten days |
| #2 trivial items belong to the session | **Holds** | Automatic execution records 63.0%; zero updates after creation 52.8%; 55.3% written within ten minutes |
| #3a managed by nobody | **Holds** | 260 cards not meant for people never finish; 362 "updated" by the machine with no real event |
| #3b nobody closes it, progress cannot be seen | **Holds** | 43.6% of runs never deliver; dispatch binding 12.1% (0% in the last two days); 46.7% of active cards are ghosts |
| #3c done but not updated | **Holds** | `closed` 0 cards; 345 finished in projection with the lifecycle unmoved; task state on cards, 0 disagreements |
| #4 people take part where appropriate | **Holds (missing)** | Instructions a person can give on the board: 0 |
| #5 a separate automatic track | **Holds (missing)** | Agent and human cards share one table, one lifecycle, one capacity |

### 1.7 Found along the way, and not mentioned in the old records

1. **Two cards for one piece of work.** For example, `CLA-495`/`496`/`497` ("design analysis of the broker/board/timeline") are in the `clawdline` project, human cards, **shown as never started**;
   the execution records actually dispatched (such as `CLA-48`, "broker design analysis") are in the `clawdline-go` project, agent cards, **already landed**.
   The task that wrote this document is one too: `CLA-498` (human card, `clawdline`) and `CLA-53` (execution record, `clawdline-go`). Pairs in the same project with similar titles within six hours: at least 9
   (cross-project pairs were not counted, so this is a lower bound).
2. **Project ownership follows the session's cwd, not the work.** A root opened in `~/code/clawdline` doing work for `clawdline-go` records the cards it opens under `clawdline`.
3. **`begin_template` defaults `phase` to `"output"`**, so "registering an epic nobody has started on" also declares "producing output" (`CLA-493`/`494` are both like this),
   and so a span cannot count as evidence that something "started".
4. **The workflow envelope on every message pollutes screen parsing.** The old app's log has 62 lines like `choosing (unnumbered): options=17 — please use Chinese ⏐ <clawdline-workflow …` (the message text, Chinese in the log, is given here in English),
   where the screen's menu parser took the envelope's content as options.
5. **The assistants' built-in to-do tools are almost unused on this machine**: of 400 Claude transcripts from the last 10 days, 0 used `TodoWrite`;
   of 300 Codex sessions, 16 (5%) used `update_plan`. So session to-dos **cannot** rely on projecting them (the decision in §3.1 comes from this number).
6. **Item keys are not unique**: `CLA-53` is three cards at once, in three projects: `clawdline`, the cloud service and `clawdline-go`;
   80 keys are duplicated in all, involving 224 cards. The new design's identity is `work.id`; the key is only for display.

### 1.8 What could not be measured (this does not count as passing)

- **Whether a card was ever opened by a person: cannot be measured.** The old version does not record reads of individual cards; `remote-audit.jsonl` does not record board reads, and Cloud's
  `kind=board` reads in the log record only "read the board", not which card.
- **Whether a person was really present for the 112 cards with traces of a session conversation in the workflow:** `/send` may also be root forwarding, so 112 is only an upper bound on "a person may have been present".
- **The stall threshold rests on only ten days**: "after more than 3 days of silence it never resumed" was observed within these ten days (§5.5); longer silences had no chance to be observed.
- **Progress was computed with the Go port**, not the Swift original; whether the two agree card by card was not re-checked here (`board-design.md` B1 says it was ported rule by rule).
- `orchestrator.json` and the session list were read within minutes after the snapshot; `:7727/v1/sessions` may see fewer sessions than the old app, so "the session no longer exists" may be slightly overstated.

---

## 2. Design principles (each points back to a problem)

| # | Principle | Addresses |
|---|---|---|
| D1 | **One structure, one reader.** The board's reader is a person, the session to-do's reader is a session, the backlog's reader is the person doing the planning. No record is written for two kinds of reader at once | Purpose 1 vs Purpose 2, #2, #5 |
| D2 | **What is meant for a person is known to that person before it appears.** There are only three ways onto the board: the person says so, the person confirms a proposal, or an automatic move that meets the "commitment" rule — and an automatic move always appears in the daily digest | #1, #4 |
| D3 | **Everything unfinished has an owner and a clock.** When the clock runs out it takes a written transition; it never "quietly stays put" | #3a, #3b |
| D4 | **Close on evidence that is actually produced.** For code, the closing evidence is the broker's landing record (proven reliable: 0 disagreements); "verified" is a badge, not a gate | #3c |
| D5 | **A person is pulled in at only a few points, and on a budget.** Only a decision that blocks work is pushed; everything else goes into the daily digest or the to-confirm area | #4 |
| D6 | **Facts are persisted, observations are not** (following `broker-design.md` §6.2). The phase a session declares is an observation and cannot make a card "in progress" | #3b (ghost in-progress), #3a (machine oscillation) |
| D7 | **The rules decide whether to ask, the agent supplies only facts, and the person decides whether to track.** An agent's self-assessment is not enough to put something on the board | #1 |
| D8 | **A dispatch must carry a work identity.** Without one, the broker binds it by rule or opens a session to-do, and never opens a separate human card in another project | #3b (progress cannot be seen), the twins in §1.7 |

---

## 3. Three structures

### 3.1 What each one is

| | **Board** | **Session to-do** (Todo) | **Backlog** |
|---|---|---|---|
| Purpose | **Purpose 1**: a person understands the situation now | **Purpose 2**: a session does not forget what it has to do | Planned, but with no commitment to start |
| Reader | A person | A session (itself, its root, the session that takes it over) | The person doing the planning |
| The question it answers | "What is happening now, and what is waiting on me?" | "What do I (this session) still owe?" | "What comes later, and in what order?" |
| Who creates | A person; or a proposal a person confirmed; or a move in from the backlog or a to-do by rule (always in the digest) | **Automatic**: broker dispatch, a child's `result.json`, the `remaining` in a session's delivery, obligations | A person; or a proposal answered "later"; or a stalled board item moved back |
| Who ends | Evidence (landing) ends it automatically; or a person "accepts / drops" it | **Automatic**: its task ends and lands, the obligation is resolved, a delivery covers it; when the session disappears it is handed to root | Only a person (discard), or it is moved onto the board |
| Required | Owner, goal, clock (the deadline for the next evidence) | Owner session, origin (which task or delivery) | Nothing (rank and planned start date optional) |
| Magnitude (today's data converted by §7) | In progress 31, awaiting closure 66 | 602 (5 of them live) | 77 |

**Where session to-dos grow from automatically:** not the assistants' `TodoWrite`/`update_plan` (§1.7-5: almost nobody uses them), but **facts the broker already produces reliably**:
a dispatch = root's to-do "collect the result and land it"; the `remaining` in a child's `result.json` and in a session's `deliver` = to-dos for their respective owners;
obligations split by `actorKind` (`user` ones become "Waiting on you" on the board, the rest become to-dos).
This path needs no extra API call from a session — measured, 43.6% of runs miss even `deliver`, so **any design of the form "ask the agent to remember to call" will miss the same share**.

### 3.2 How the data is separated: three tables + one work identity + one log of moves

Three approaches were compared:

| Approach | Upside | Downside | Judgement |
|---|---|---|---|
| **One table, different `kind`** (= the old `audience`) | A move changes only one field | Every query must remember to filter; capacity, counting and lifecycle are shared; **the old version was exactly this, and the numbers in §1.3–1.4 are the result** | No |
| **One table, different state machines** | The state machines are separate | Invariants (a board item must have an owner and a clock, a backlog item need not) rest only on code convention, and the schema cannot enforce them; they still get counted together | No |
| **Separate tables** | Invariants are guaranteed by the schema (`NOT NULL`, `CHECK`); a query **cannot** mix in another kind; capacity and retention are set per table | A move takes a transaction; an identity across tables is needed | **Adopted** |

**Decision (D1, #5, #BK):**

```sql
-- The identity of the work: whichever structure it is in now, the id does not change; links (tasks, documents, reports) hang here
CREATE TABLE work (
  id          TEXT PRIMARY KEY,           -- uuid
  project_id  TEXT NOT NULL,              -- the work's project, not the session's cwd (§1.7-2)
  title       TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  created_by  TEXT NOT NULL               -- user | user_via_session:<run> | root:<session> | broker
);

CREATE TABLE board_items (                -- board: what a person needs to know now
  work_id          TEXT PRIMARY KEY REFERENCES work(id),
  state            TEXT NOT NULL CHECK (state IN ('active','awaiting_closure','done','dropped')),
  owner            TEXT NOT NULL,         -- a root session or "user"
  commitment       TEXT NOT NULL,         -- dispatch | assigned | scheduled | decision | delivered
  evidence_due_at  INTEGER,               -- the clock: required for active/awaiting_closure
  acceptance       TEXT,                  -- acceptance conditions a person reads (a few), not the session's step list
  closed_reason    TEXT                   -- landed | accepted | unconfirmed | dropped
);

CREATE TABLE backlog (                    -- backlog: planned, with no commitment to start
  work_id      TEXT PRIMARY KEY REFERENCES work(id),
  state        TEXT NOT NULL CHECK (state IN ('planned','dropped')),
  rank         INTEGER,
  start_on     TEXT,                      -- planned start date; coming within seven days triggers a move onto the board
  reviewed_at  INTEGER                    -- the last time a person looked at it
);

CREATE TABLE todos (                      -- session to-do: what a session must not forget
  id            TEXT PRIMARY KEY,
  work_id       TEXT REFERENCES work(id), -- nullable: a session's pure chores have no human work
  owner_session TEXT NOT NULL,
  origin        TEXT NOT NULL,            -- dispatch | result_remaining | deliver_remaining | obligation
  task_id       TEXT,
  state         TEXT NOT NULL CHECK (state IN ('open','done','dropped','handed_off')),
  escalated_at  INTEGER
);

CREATE TABLE moves (                      -- every move between the three structures, append-only
  seq      INTEGER PRIMARY KEY,
  work_id  TEXT NOT NULL,
  from_s   TEXT NOT NULL,                 -- todo | proposal | board | backlog | none
  to_s     TEXT NOT NULL,
  trigger  TEXT NOT NULL,                 -- a trigger code from §6
  actor    TEXT NOT NULL,
  evidence TEXT NOT NULL,                 -- JSON: which fact triggered it
  at       INTEGER NOT NULL
);
```

Two more small tables: `proposals` (the proposals of §4 and their answers) and `decisions` (things waiting for a person to decide).

**Where it lives:** in the same SQLite as the broker (the direction of `broker-design.md` §6.1, §6.9). The reason is #3c: the old board was a separate document, updated by the broker pushing
`observe(...)`, with yet another set of closing conditions — between two stores, one place that fails to converge means it never converges. Inside one DB,
"task landed" → "to-do ended" → "board item ended" can be **one transaction** (the rule of `broker-design.md` §6.1: one externally visible fact is one transaction).
This differs from `board-design.md` C1, "port the whole-document rewrite as is": C1 was a judgement about **porting the old model**; this document changes the model, and once the model changes, the old reasons (42 ms, not worth splitting)
are no longer decisive. The read-only reading of the old version's 787 cards (`Legacy` in `internal/adapters/board/source.go`) **does not change**.

**Derived display states are not stored.** The "queued / in progress / in review / delivered / landed" on a card is still derived from evidence by `ProgressOf` (`board-design.md` B1, B5, ported as is),
but **not written back to the store**, so there will be no more 4,124 reconciliation entries and 75.5% oscillation (D6, #3a). Only the four lifecycle states and a person's decisions are stored.

### 3.3 How the screen is divided

- **The board page** shows only `board_items`, from top to bottom:
  1. **Waiting on you**: `decisions` and `awaiting_closure` (the closure queue). Each card has only one to three buttons.
  2. **In progress**: `active`. A card shows the goal, the owner, the derived state, **the count of session to-dos** ("3 to-dos, 1 stuck", without listing them),
     the time of the last evidence, and the clock ("back to the backlog if there is no new evidence within 3 days").
  3. **Scheduled this week**: backlog items whose `start_on` is within seven days but not yet dispatched (they already count as a commitment; see §6).
  4. **Recently done**: `done` within seven days, collapsed by default.
- Beside the board's title, a **To confirm** badge (the number of `proposals`); opening it shows the list of proposals.
- **The backlog** is another tab of the board page: a ranked list, filterable by project, with "Schedule" and "Start" on every row.
- **Session to-dos do not appear on the board page.** They are in a panel of the session's details, and are provided for the session itself to read (API and briefing; see §5.2).
  This is the literal realization of #5, "should not be mixed in with the items meant for people": **not collapsed, but not on the same page**.

> The current board page front end is the port of the old version that another child is building, as a read-only view of the 787 old cards. This design's screens are the new app's own board, scheduled after that port lands (§9).

### 3.4 The two purposes do not fight: who writes what

One old card held all of these at once: the goal (the person's), the checklist (the session's plan, yet treated as the person's acceptance conditions), obligations (some for a person to decide, most the session's own),
spans (the session's declaration, yet they made the card "in progress"), and deliveries (the session's own account, yet the completion mark a person reads). **Every kind of field was shared by two readers, and each side was bound by the other.**

| Information | Old version kept it in | New design keeps it in | Who writes | Who reads |
|---|---|---|---|---|
| Goal, owner, acceptance conditions (a few) | The card | `board_items` | A person (or a proposal a person confirmed) | The person; a session reads it, read-only, to know "why" |
| Step list, remaining work | The card's checklist/obligations | `todos` | Automatic (broker facts) | The session |
| Things a person must decide | The card's obligations (`actorKind:user`, 7 of them) | `decisions` | A session raises it, a person answers | The person |
| "I am working on X" | The card's span | **Not persisted** (a live field in memory and SSE) | The session | The person (live screen) |
| Delivery, landing | The card's evidence/sessionDeliveries | The broker's tasks and landing records | The broker | Both read it and derive their own states |

**The rule: a session never writes a board item's state; a person never needs to edit a to-do.** A board item's state is derived from the facts of the to-dos and tasks it is linked to, plus a person's decisions;
a to-do's state is derived from broker facts. The two are linked only through `work_id`, and **no record exists for two readers at once** (D1).

### 3.5 The name: just call it **Backlog**

The API uses `backlog`, and the Chinese interface writes **Backlog** too, with the subtitle "planned, not yet scheduled". The reasons:

1. **It is the user's own word**, and its meaning in the industry fits exactly: ranked, planned, not in progress.
2. **Every Chinese word we could coin collides with a word this app already uses**: the word for "to-do" (*dàibàn*) is the session track; the word for "planning" (*guīhuà*) is progress's `planning` phase;
   "awaiting scheduling" (*dàipái*) differs from *dàibàn* by one character, and on a phone the two cannot be told apart at a glance.
3. **The old `backlog` was a state of a board item** (151 cards); the new design **removes** it, so the word has only one meaning: a structure, not a state.

The other two names: the **board** (`board`) and the **session to-do** (`todo`).

---

## 4. Creation: what is worth a person's tracking, who judges, when to ask, and how to fool-proof it (#1, #4, D2, D7)

### 4.1 The default: everything goes into session to-dos, without asking

Everything a session does starts as a to-do. **No asking, no board.** This rule alone stops the 47 cases in §1.2 of "the agent judged `new_work` itself and opened a card".

### 4.2 When it is "worth asking": judged by rules, with the agent supplying only facts

| Signal | Where it comes from (a fact, not a self-assessment) | Why the line is here |
|---|---|---|
| **I1 crosses sessions**: this line of work dispatched a child, or did a handoff | The broker | Once a thing needs a second session, it is no longer one turn's chore |
| **I2 lives long**: a to-do open for more than **24 hours** | Timestamps | The measured p95 is 23.6 hours (§1.3): only the longest 5% exceed it |
| **I3 has an external effect**: deploying, releasing, pushing to the default branch, spending money, sending mail outside | Declared by the session in a **closed vocabulary**, or the broker observes a landing on the default branch | These are the things a person later asks about: "did that go live?" |
| I4 needs a person's decision | Obligation `actorKind:user` | **Does not go through a proposal**; it becomes "Waiting on you" directly (§5.1) |
| I5 the person says to track it ("track this", "put it on the board") | A message the person sent (the workflow run proves a person sent it) | **No asking**; created directly, with the actor recorded as `user_via_session:<run>` |

**The rule:** meets any of I1, I2, I3, and not yet bound to a board item → generate a **proposal**. I4 and I5 generate no proposal (one becomes a decision directly, the other is created directly).

**Never proposed:** a child's own task (its root owns that line of work); auxiliary attempts such as review/test/correction (they are steps of something else);
`question`/`clarification`; scheduled routine work (the 61 tasks with a `schedule_id`).

**Who judges: the rules decide "whether it may ask", the agent supplies only the facts for I1–I3, and the person decides "whether to track".** An agent's self-assessment is not enough to go on the board —
the 47 cards in §1.2 are what agent self-assessment produced.

### 4.3 When to ask, and through which channel

| Situation | Channel | What it looks like |
|---|---|---|
| The person is present (this root session received a message the person sent within the last 30 minutes) | **In the session**, appended to the end of the agent's delivery message for this turn, **non-blocking** | "Put 'X' on the board to track? Track / Later (backlog) / No" — the person replies, or presses it on the board |
| The person is not present | **The board's to-confirm area**, not pushed | The To confirm badge + one line in the daily digest, "N proposals" |
| A person's decision is needed and it blocks work (I4) | **Push** (the existing notify, within its limits of 5 per task and 30 per hour) | The only case that pushes |

Estimated magnitude: grouping dispatches by "root × project × day", since 09-08 the daily median is 5 lines of work, at most 17. A line of work already bound is not asked about again, so
the actual number of asks will be below this ceiling (**not measured**; it is an upper-bound estimate). "Needs a person's decision" was 7 in ten days, about 0.7 a day, well within the push budget.

### 4.4 Fool-proofing: never ask when it should not

The fool-proofing sits at **the only entry**: a proposal can only be created through `POST /v1/orchestrator/proposals`, and **whether to ask in the session is the server's answer, not the agent's decision**.
The agent's briefing says one sentence: "Ask in the conversation only when the response is `ask: true`, using the sentence in the response."

| Refusal code | Condition |
|---|---|
| `proposal_below_threshold` | Meets none of I1–I3 |
| `proposal_from_child` | The caller is a child; a child's proposal is recorded in its root's to-confirm area instead |
| `proposal_duplicate` | The same `work_id` has already been proposed; a rejected one may be proposed again only **when there is a new signal**, and only once |
| `proposal_already_tracked` | Already bound to a board item: bind it directly, don't ask |
| `proposal_budget_exhausted` | This session has already asked once this turn, or has asked 3 times today → goes to the to-confirm area instead |
| (not a refusal) `ask: false, reason: human_absent` | The person is not present → goes to the to-confirm area |

**A proposal nobody answers expires after 7 days, and the result is "stays a session to-do"** (not tracked). This is the safe default: no answer means nothing is pushed onto the person's board;
it is still tracked and ended automatically as a to-do, and the expiry appears in the digest once.

Every proposal and its answer is written to `proposals`; "the server said `ask:false` but the conversation asked anyway" cannot be fully prevented (an agent can type anything),
so **it is measured**: `/v1/diagnostics` records `proposals.asked_inline` and `proposals.ask_true`, and when the two disagree, the briefing was not followed.

---

## 5. Lifecycles

### 5.1 Board items

```
   a person says so / a person confirms a proposal / the backlog moves it in by rule
                         │
                         ▼
   ┌────────────────── active ───────────────────┐
   │   (owner and clock: evidence_due_at)        │
   │                                             │
   │ all bound executions finished, at least one │  a new dispatch / a new blocking finding
   │ delivery, but no automatic-closure evidence │◄──────────────────────┐
   ▼                                             │                       │
awaiting_closure ──(landing evidence arrives)──► done ─────(reopen)──────┘
   │   │                                         ▲
   │   └──(person: accept)───────────────────────┤
   │                                             │
   └──(asked once, no answer in 7 days)──► done (closed_reason = unconfirmed, marked "delivery unconfirmed")

active ──(3 days with no new evidence and no delivery)──► back to the backlog (writes moves, goes in the digest)
active / awaiting_closure ──(person: drop)──► dropped
```

| Transition | Trigger | By whom | Evidence |
|---|---|---|---|
| →`active` | A person creates it, a person confirms a proposal, the backlog moves it in per §6 | Person / rule | One `moves` entry |
| `active`→`done` | Every bound task `landed`, `incorporated` or `nothing_to_land`, and no unresolved blocking obligation/finding | **Rule (automatic)** | The broker's landing record (D4) |
| `active`→`awaiting_closure` | Every bound execution has finished, at least one delivery, but no evidence for the row above (not code, the session's own claim of delivery, landing has not happened yet) | Rule | Task terminal state + delivery |
| `awaiting_closure`→`done` | Landing evidence arrives | Rule | The broker |
| `awaiting_closure`→`done` | The person presses "Accept" (= a generalization of the old `accept_artifact`, recorded as the person's decision; a fact, not an opinion) | **Person** | One `decisions` entry |
| `awaiting_closure`→`done(unconfirmed)` | Asked once in the digest 3 days after entering, then 7 more days without an answer | Rule | The clock; the card is marked "delivery unconfirmed" for good |
| `awaiting_closure`→`active` | The person presses "Needs changes" | Person | A new to-do opens for the owner |
| `active`→backlog | **3 days** with no new non-machine evidence, no delivery, and no decision waiting on a person | Rule | The clock; `moves` records `stalled_3d`, and it goes in the digest |
| `done`→`active` | A new dispatch is bound to it, or a new blocking finding (following the old "reopen for a new scope") | Rule | Broker / finding |
| →`dropped` | The person presses "Drop" | **Only a person** | `decisions` |
| The owner is gone | The owner session no longer exists and no one has taken over | Rule | The card is marked "no one responsible" and goes in the digest; the clock keeps running |

**What "evidence" the clock looks at:** any fact bound to this `work_id` — a to-do created or ended, a dispatch and its terminal state, a landing, a session's delivery report,
a person's decision. A session's declaration ("I am working on it") is an observation and does not count (D6). Work root is doing directly, with no dispatch, moves the clock with its delivery report each round;
if a report is missed and the item moves back to the backlog, the cost is one action: the next fact bound to it (or the person pressing "Start") moves it back onto the board (§6).
**Moving back to the backlog never deletes anything; it only tells the truth about the "commitment".**

**Why `done(unconfirmed)` can safely end automatically:** it only clears the person's board; it does **not** claim a landing. An unlanded delivery remains an obligation in the broker's own inventory
(`unlanded`), which has an owner and a landing queue; the board does not need "never close" to remember it. The card keeps the "delivery unconfirmed" mark, and any new evidence reopens it.

### 5.2 Session to-dos

```
open ──(its task ends and lands / obligation resolved / a delivery covers it)──► done
open ──(owner session ends)──► handed_off (to root; if root is gone too → dropped after 24 hours, one line in the digest)
open ──(meets the escalation conditions of §6)──► stays open, and a decision or proposal is created on the board
```

| Transition | Trigger | By whom |
|---|---|---|
| Created | A dispatch is accepted (root's "collect the result and land it" to-do); the `remaining` of `result.json`/`deliver`; an obligation not for the user | The broker (automatic) |
| →`done` | Task terminal state + landing (`landed`/`incorporated`/`nothing_to_land`); obligation resolved; covered by a later `deliver` | The broker (automatic) |
| →`done` (unlanded) | Task terminal but `abandoned`, or the broker's landing obligation closed by a person or root | The broker (automatic) |
| →`handed_off` | The owner session ends | Rule: moved to its root; with a `handoff`, moved to the session taking over |
| →`dropped` | Still no owner 24 hours after being handed off | Rule, in the digest |

**How a session "doesn't forget" (Purpose 2):** to-dos are brought back to a session at three moments when it tends to forget:
(1) a dispatch's briefing and a handoff's OPEN THREADS carry its unfinished to-dos; (2) a child's completion notice (the existing durable notice) carries root's "collect the result and land it" to-do;
(3) `GET /v1/orchestrator/sessions/:id/todos` (machine credential) can be read at any time. **No more envelope on every message a person sends** (§1.7-4; it is also where the 43.6% missing `deliver` came from).

### 5.3 Backlog

```
planned ──(any move-in trigger of §6)──► moved onto the board
planned ──(person: discard)──► dropped
planned, not looked at for 30 days ──► the weekly digest asks once, "keep it?"; kept by default
```

**The backlog is never deleted automatically.** A person's planning being cleared by a machine is the worst mistake there is (the same reason as `board-design.md` §6-5 and `timeline-design.md` §3.3).

### 5.4 Which rule stops each of 3a, 3b and 3c

| Failure | The mechanism today | The rule that stops it |
|---|---|---|
| **#3a trivial, managed by nobody** | Automatic execution records share a table and a lifecycle with human cards, with the human end conditions, so they never end; machine reconciliation keeps "updating" them | (1) Trivial things are not board items at all but to-dos (§3); (2) to-dos **end automatically** on broker facts, and are handed off or dropped when their owner disappears (§5.2); (3) derived states are not persisted, so the machine no longer "touches" them (§3.2) |
| **#3b nobody closes it once started, progress cannot be seen** | Dispatches are not bound to board items (12.1%); sessions' begins don't close (43.6%); declared spans make dead sessions look like they are still working | (1) **D8**: a dispatch must carry a `work_id`; without one the broker binds it or opens a to-do, and never opens a separate human card in another project; (2) every board item has an owner and a **clock**: 3 days without evidence sends it back to the backlog, and a vanished owner marks it "no one responsible"; (3) declarations are not persisted (D6), and "in progress" is derived only from the broker's live attempts |
| **#3c done but the board was not updated** | The closing gate requires verification records, which are almost never produced; `closed` 0 cards | (1) **D4**: landing evidence is enough to end it automatically; verification is a badge; (2) deliveries that are not code or have not landed go to the closure queue: **asked once, with a deadline and a default**; (3) task landed → to-do ended → board item ended in the same DB, in one transaction (§3.2) |

### 5.5 Where the thresholds come from

| Threshold | Value | Basis |
|---|---|---|
| Board stall, back to the backlog | **3 days** | Of all 12,900 events, activity resumed after more than 1 day of silence only 11 times, and **0 times after more than 3 days**; for the 281 finished cards, the p99 of the longest silence along the way is 30.8 hours. In this data, three days without movement means it will not move again (limits in §1.8) |
| Worth asking (I2) | **24 hours** | The longest-lived 5% (p95 23.6 hours) |
| The board's "short term" | **7 days** | `start_on` within seven days = scheduled this week; matches the weekly backlog digest |
| Default outcome of the closure queue | Asked once 3 days after entering, then 7 more days | Gives a person one whole weekend; after that it ends, marked "unconfirmed" |
| Proposal expiry | 7 days | Same as above |

For threshold sensitivity see §7.3: **changing the threshold only moves cards between "board: in progress" and "board: awaiting closure"; it does not change session to-dos or the "never started" backlog.**

---

## 6. Rules for moving between the three

Every move is a transaction that writes one `moves` entry (`trigger`, `actor`, `evidence`), and **an automatic move always appears in the daily digest** (D2).

| From → to | Trigger | By whom | Evidence |
|---|---|---|---|
| **Backlog → board** | Someone dispatches carrying this `work_id` | Root (when dispatching) | The broker task is accepted |
| | The person says "start on X" in a session, and the session's begin is bound to this existing item | The person (through the session) | The run the person sent + the binding |
| | The person presses "Start" on the board (assigning it to a root) or "Schedule" and sets `start_on` | Person | `decisions` |
| | `start_on` comes within seven days | Rule | The date |
| | A session accepted an assignment (`assignment_decision: accepted`) | Session | Workflow receipt |
| **Board → backlog** | `active` and 3 days with no new non-machine evidence, no delivery, and no decision waiting on a person | Rule | The clock, `stalled_3d` |
| | `start_on` passed 1 day ago and still no dispatch | Rule | The date, `schedule_missed` |
| | The person presses "Back to backlog" | Person | `decisions` |
| | **Never**: what has been delivered (that goes to the closure queue, not back to planning) | — | — |
| **Session to-do → board (escalation)** | A person's decision is needed and it blocks work (obligation `actorKind:user`, `waiting_user`) | The session raises it, the rule places it | Creates a "Waiting on you" on its board item; with no board item, it is only pushed and stays in the session |
| | The same to-do's attempt fails a second time (two `failure`/`timeout`/`spawn_failed`) | Rule | Broker terminal state; with a board item, a "Stuck: retry / change approach / drop" decision is attached; without one, a proposal is created |
| | Meets I1–I3 of §4.2 | Rule | Creates a **proposal** (not straight onto the board) |
| | The owner session ended and no one has taken over 24 hours after hand-off | Rule | One digest line: "N to-dos have no one: hand to whom / discard" |
| **Board → session to-do (demotion)** | The person presses "Don't track" on a proposal or an item | Person | `decisions`; the work goes back to being only to-dos |
| **Proposal → board / backlog / to-do** | The person chooses "Track" / "Later" / "No"; no answer for 7 days → to-do | Person / rule | `proposals` |

---

## 7. Where today's 787 cards land

### 7.1 Rules (in order; the first match decides)

```
Input: audience (the old catalogDisposition; where absent, inferred from inferredSourceKey), type,
       progress (Go ProgressOf), unresolved user decisions, the last non-machine event, has started (§1.1)

S  audience ≠ human, or type = coordination               → session to-do
     finished (completed/canceled)                        → S.done
     active and not a ghost (§1.4)                        → S.live
     everything else                                      → S.autoclose (ended automatically at migration)
B  audience = human and finished                          → board: finished (B.done)
B  an unresolved user decision, or active and not a ghost → board: in progress (B.now)
K  never started (no dispatch, delivery, evidence or ticked checklist) → backlog (K.never_started)
B  started, idle ≤ 3 days                                 → board: in progress (B.now)
B  started, idle > 3 days, projection delivered/verified/
     blocked/correction/review_testing                    → board: awaiting closure (B.closure)
K  everything else (started, idle > 3 days, no delivery)  → backlog (K.stalled)
```

coordination goes into session to-dos because these are coordination records between sessions (for example: "Notify Clawdfather: /git and other routes are occupying RemoteServer's shared queue"),
and the old version itself specified that they have no delivery lifecycle.

### 7.2 Result (threshold 3 days)

| Structure | Breakdown | Cards | Share of all | Projected states |
|---|---|---:|---:|---|
| **Session to-do** | S.done (finished) | 337 | 42.8% | landed 304, canceled 19, settled 14 |
| | S.autoclose (ended automatically at migration) | 260 | 33.0% | delivered 186, blocked 54, unknown 17, execution 3 |
| | S.live (in progress) | 5 | 0.6% | The execution records of the few children running now |
| | **Subtotal** | **602** | **76.5%** | |
| **Board** | B.now (really happening now) | 31 | 3.9% | delivered 24, execution 3, unknown 2, blocked 1, correction 1 |
| | B.closure (awaiting a person to close) | 66 | 8.4% | delivered 52, verified 7, correction 3, blocked 2, review_testing 2 |
| | B.done (finished) | 11 | 1.4% | landed 8, canceled 3 |
| | **Subtotal** | **108** | **13.7%** | |
| **Backlog** | K.never_started | 60 | 7.6% | planning 53, unknown 4, execution 3 |
| | K.stalled | 17 | 2.2% | planning 12, unknown 5 |
| | **Subtotal** | **77** | **9.8%** | |
| | **Total** | **787** | 100% | |

**Only the cards meant for people (the old version's 192 `audience=human` cards minus 7 coordination = 185):**

| | Cards | Share of human cards |
|---|---:|---:|
| Really **backlog** | **77** | **41.6%** |
| Awaiting a person to close | 66 | 35.7% |
| Really happening now | 31 | 16.8% |
| Finished | 11 | 5.9% |

**41.6% of the board today is really backlog; "now" is only 16.8%.** The 66 awaiting closure are the concrete list for #3c (for example: "Automatic tracking list goes live",
"Bug-fix proposal management page: commit, push, deploy" — each time the session said it was delivered, and the board never closed it).

### 7.3 Threshold sensitivity

| Stall threshold | B.now | B.closure | K.stalled | K.never_started | S (total) | B.done |
|---|---:|---:|---:|---:|---:|---:|
| 1 day | 14 | 82 | 18 | 60 | 602 | 11 |
| **3 days** | **31** | **66** | **17** | **60** | **602** | **11** |
| 7 days | 97 | 17 | 0 | 60 | 602 | 11 |

**Session to-dos at 602 and "never started" at 60 do not move at all under the three thresholds**; the threshold only decides how human cards split between "in progress" and "awaiting closure / stalled".

### 7.4 Who judges, and how to recover from a wrong call

- **Who judges: the rules (§7.1), approved once by a person.** The classifier runs read-only on the old data (not a single byte is written to the old store, `plan.md` §4), and produces each row's placement and
  **reason code** (for example `audience_agent`, `never_started`, `idle_gt_3d+delivered`). The migration is one **batch proposal**: on one screen the user sees
  "board 31 / awaiting closure 66 / backlog 77 / auto-ended 260 / archived 348", can change any row, and presses "Apply" once. **Until it is approved, the new board is empty, and the old cards stay visible read-only.**
- **Look first at what is most likely wrong:** the list is sorted by confidence, lowest first —
  1. **Twins**: a human card shows "never started", but the execution record for the same thing has landed (§1.7-1, at least 9 pairs, cross-project ones on top of that). By the rules these would go to the backlog, when really they should be
     "finished". Before migrating, run a binding match once: same root session, within six hours, the task's `work_item_id` or graph matches — list them as "possibly the same thing" for a person to confirm.
     **Do not merge by title automatically** (the old version's rule, for the same reason: a title is not an identity).
  2. **Those decided by the threshold**: `K.stalled` and `B.closure`, because they move with the threshold (§7.3).
  3. **Those whose span the template defaulted to output**: already excluded by the strict definition of "has started" (§1.1), and listed for a person to see.
- **Recovering from a wrong call:** every migrated row writes `moves` (`trigger: migration`, with the rule version and the reason code). Any row can be moved to another structure with one action;
  the old card can always be read (`migrated_from` points back to the old id). The classifier is a pure function over read-only input, so **rerunning the whole batch is idempotent**: rerun it after changing the rules, and it only moves rows a person has not changed by hand
  (where a person changed a row, the person's choice wins, `moves.actor = user`).
- **The 260 S.autoclose ended automatically by the migration**: this only takes them out of "unfinished"; the broker's records of unlanded deliveries are unaffected.

---

## 8. Where people take part: few points, well chosen (#4, D5)

| Stage | When a person is pulled in | What the person sees | Instructions the person can give | Channel | Budget |
|---|---|---|---|---|---|
| **Creation** | A proposal meets I1–I3, and the person is present or it is digest time | A one-sentence goal + why it was proposed (I1/I2/I3) | Track / Later (backlog) / No | End of the conversation (present) or the to-confirm area | ≤1 per session per turn; ≤3 a day put in front of the person |
| **Dispatch** | No one is pulled in (dispatch is root's business); when a backlog item is dispatched, one line in the digest says so | — | — (the person can press "Start" on the backlog beforehand) | Digest | — |
| **In progress** | **Only decisions that block work** | The question, the options, who is waiting | Answer | **Push** + "Waiting on you" on the board | The existing notify limits (5 per task, 30 per hour) |
| **Delivery** | A delivery with no automatic closing evidence, after 3 days | The delivery summary, output links, what is still missing | Accept / Needs changes / Back to backlog / Drop | The board's closure queue + one digest line | In batches (one screen at a time) |
| **Landing** | No one is pulled in | One digest line, "N landed" | — | Digest | — |
| **Periodic** | Once a day | What finished, what stalled back to the backlog, how many proposals, how many await closure, whether anything has no one responsible | Jump from the digest straight to the matching list | Daily digest (one message) | 1 a day |
| **Planning** | Once a week | Backlog items no one has looked at for 30 days | Keep / Discard / Schedule | Weekly digest | 1 a week |

**Instructions a person can give on the board (the complete list):** Start, Schedule, Back to backlog, Accept, Needs changes, Drop, Hand over, Track this, Don't track, Answer a decision.
What is said in natural language in a session is turned by root into the same set of instructions, with the actor recorded as `user_via_session:<run>` (the run proves the message was sent by a person).

This **deliberately overturns one of the old version's principles**: the old `project-board.md:30-34` was "no manual status menu; all status is derived from evidence". This design keeps "status is derived from evidence",
but adds that **a person's decision is also evidence** (accept, drop, schedule), because what was measured is that with automatic evidence alone, closing never happens (#3c).
A person's decision cannot override the broker's facts (it cannot, for example, mark something unlanded as landed); it can only decide "I accept this result" or "we are not doing this".

---

## 9. Implementation order, and the smallest acceptable first step

Precondition: the port of the old board page (the other child's work noted at the top) lands first, giving the old cards a read-only, 1:1 view. This design is the new app's own board, scheduled after it.

| Step | Content | Done when | Depends on |
|---|---|---|---|
| **1 (smallest acceptable)** | **A read-only three-track projection**: `GET /v1/board/tracks?project=`, sorting the old 787 cards into three tracks per §7.1, each row with a reason code; a CLI with the same rules, `clawdline board tracks` | Against the snapshot in this document's appendix, the numbers are exactly those of §7.2 (602/108/77 and every breakdown); every rule has a fixture test that can fail; **zero writes** | None |
| 2 | Session to-dos: a `todos` table, created, ended and handed off automatically from the Go broker's facts | Failure injection: a child dies, root dies, the landing arrives before the result, `result.json` is sent twice → the to-dos all converge, with exactly one entry each | Broker B3 (notifications) |
| 3 | Board and backlog: `work`, `board_items`, `backlog`, `moves`; the commitment rule, the 3-day clock, automatic ending on landing, the closure queue | Fixtures reproduce the three failures of §5.4, each handled by its rule | Broker B5 (landing) |
| 4 | Where people take part: `proposals`, `decisions`, the to-confirm area, daily/weekly digests, the refusal codes of §4.4 | One test per refusal code; `asked_inline` and `ask_true` agree in diagnostics | 3 |
| 5 | Migration: the batch proposal screen, approved once by a person, recorded in `moves` | Reruns are idempotent; rows a person changed are not overwritten | 1, 3 |
| 6 | Front end: the board page's three areas, the backlog tab, the to-do panel in session details | A reviewer checks it against §3.3 of this document | The board-page port landed, 3, 4 |

**Why the first step is a read-only projection:** it is the cheapest step, yet it lets the user judge whether the rules are right **against their own data** ("are these 77 really backlog?"),
before any write or any change to the screens. If the rules are wrong, what changes is a pure function, not migrated data.

**Explicitly not ported:** the workflow envelope on every message and the `begin` classification (§1.7-3, 4, the source of #1); writing `automatic_state_reconciled` into durable history (D6);
deciding a card's project by the session's cwd (§1.7-2).

---

## 10. What needs the user's decision (each already proceeds on a safe default)

| # | Question | Default | Why this is the safe side |
|---|---|---|---|
| 1 | The backlog's name | **Backlog** (the same word in Chinese and English, subtitled "planned, not yet scheduled") | It is your own word; every Chinese candidate collides with an existing term (§3.5) |
| 2 | The stall threshold for moving back to the backlog | **3 days** | In the data, 0 resumptions after 3 days of silence; changing the threshold only affects how human cards split (§7.3) |
| 3 | How long "short term" is | **7 days** | Matches the weekly backlog digest |
| 4 | Proposals nobody answers | **After 7 days, they stay session to-dos (not tracked)** | No answer means nothing is pushed onto your board; it is still tracked and ended automatically |
| 5 | Closures nobody answers | **Ask once, then end after 7 more days, marked "delivery unconfirmed"** | Clears the board without claiming a landing; the broker's unlanded records are unaffected; any new evidence reopens it |
| 6 | How to migrate the old 787 cards | **A batch proposal, applied only once you approve it**; until then the new board is empty | The first move of data should not be decided by the machine alone |
| 7 | Whether what is said in a session counts as a person's instruction | **It counts**, with the actor recorded as `user_via_session:<run>` | Most of your instructions are already in conversation; the run proves a person sent the message |
| 8 | The workflow envelope on every message | **Not ported** | It is where agents opening cards on their own came from, and it is the path behind the 43.6% that never closed |
| 9 | A card's project | **Specified explicitly at creation**; only when unspecified does it use the session's cwd, marked `project_inferred` | Stops root opening cards for project B from inside project A's directory (§1.7-2) |
| 10 | Overturning the old "no manual status" principle | **Overturn it**: a person's "accept / drop / schedule" is evidence | With automatic evidence alone, `closed` was 0 cards in eleven days (§8) |

---

## 11. What this document did not do

- **No implementation was changed, and the repository's build and tests were not run.** To compute progress card by card, `internal/adapters/board` was copied into the task's temporary directory,
  and a 30-line measuring program was added, compiled and run; the repository itself was not touched.
- **Card reads were not measured** (the old version does not record them), so "never opened by anyone" **cannot be measured**.
- The definition of "the person is present" (received a message the person sent within 30 minutes) and the daily number of proposals **have not been tried on the real flow**; the numbers in §4.3 are upper-bound estimates.
- All numbers come from one machine over eleven days. The basis of the stall threshold is limited by that window (§1.8).
- Twins were measured only within a project (at least 9 pairs); cross-project ones were only illustrated by examples, not counted in full.
- No token or secret was read; from `orchestrator.json` only the tasks' id, state and landing fields were extracted, and `secret_hash` was not read. The only credential read was the new version's own
  `local-token`, used for read-only GETs against `:7727`.

---

## Appendix: where the numbers come from

The scripts and output are all in the measuring task's `artifacts/` (`measure.py`, `classify3.py`, `boardmeasure.go`, `metrics.json`, `metrics2.json`,
`classify3.json`, `twins.json`), and the snapshot and intermediate files in `snapshot.tar.gz` (0600). To reproduce: unpack the tarball, add the two scripts, and run
`python3 measure.py && python3 classify3.py` — the numbers of §7.2 and §7.3 come out exactly (confirmed by actually rerunning once). The acceptance of step 1 in §9 checks these numbers against the same snapshot.

| Number | How it was measured |
|---|---|
| 787 cards, revision 6403 | At 2026-09-18 01:02:40 UTC, `project-board.json`, `project-board-history/` and `project-board-workflow.json` were copied with `cp` into a temporary directory; after that only the copies were read |
| Each card's progress, group, audience | `boardmeasure.go`: calls `ProgressOf`/`ListSummaryOf` (this repository's `internal/adapters/board/progress.go`) on the copies |
| actor distribution, creators, updates after creation | Parsed the 564 history files line by line; the actor rule is from `ProjectBoardHTTP.swift:260-268`, `:419-432` |
| The web page has no operation that changes a card | `grep` over the old `Resources/web/app/js`: `boardCommand` is used only by `input/board-settings.js`, for `set_enabled`/`set_ai_consent` |
| The closing gate | Read `ProjectBoardStore.swift:3440-3505` |
| 4,124 automatic reconciliations, 3,115 oscillations | Parsed "from X to Y" from the summaries of `automatic_state_reconciled` in history |
| Task state, landing, `work_item_id` | Extracted the id/state/landing/work_item_id of 774 tasks from `~/.config/clawdline/orchestrator.json` (without `secret_hash`) |
| Task state on cards vs real state, 0 disagreements | The card's `links[kind=task, source=broker].attemptState` against the task's `state` |
| Live sessions | `GET :7727/v1/sessions` (local token, read-only), 14 |
| 528 workflow runs, missing begin/deliver | `runs[].missingFollowUp` in `project-board-workflow.json` (256 retained) + `retiredGaps` (272 retired) |
| Resumptions after silence | The gap between each card's consecutive non-machine events |
| `TodoWrite`/`update_plan` usage | `grep -l` over the last 10 days of `~/.claude/projects/**/*.jsonl` (first 400) and `~/.codex/sessions/**/*.jsonl` (first 300) |
| Number of lines of work | Tasks in `orchestrator.json` grouped by (root session, project_dir, day), excluding 61 scheduled tasks |
| Three-track classification and sensitivity | `classify3.py` (the rules of §7.1), run once each with thresholds of 1, 3 and 7 days |
