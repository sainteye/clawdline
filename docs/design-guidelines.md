# Design guidelines

> The user's words (2026-09-18, translated from Chinese): "Take the problems you saw in this round of design and consolidate them into the guidelines we design systems by from now on…
> They should be drawn from the general problems this round of design turned up, and organized into the most concise version possible."
> Added: "Even our guidelines themselves have to be reasonable, and that includes being concise and being drawable. And they cannot grow without limit."

The sources are this round's design documents, child reports and commits (2026-09-16 to 18), and the incidents the old repository (`~/code/clawdline`, read-only)
named itself. **Every rule points back to a failure that really happened**; the evidence is in §3.

**The overall principle: two different things must not look the same.** Nearly every incident this round can be written in that form, and the old repository described its own the same way:
"a list that was never read prints exactly what a list with nothing to keep prints" (`d31a09df`).
So every rule names the pair it keeps apart.

## The list (at most 12 rules, 10 today; §1 says why 12, and what happens when it is full)

1. **DG-1 Speaks up** (stopped ≠ idle): every loop that runs by itself reports every round; when it stops, `/v1/health` turns red within three periods; what it cannot do, it refuses by name.
2. **DG-2 Bounded** (full ≠ normal): everything that accumulates has four cells filled in before it ships: the limit, what happens when it is full, who finds out, and who decides what is evicted.
3. **DG-3 Measure the total** (one fast run ≠ a cheaper total): a performance number carries its denominator and its distribution (cost per unit of useful change, p99/maximum), measured on non-empty input at real scale.
4. **DG-4 Record only facts** (fact ≠ observation, sent ≠ delivered): persist only what cannot be recomputed, one fact per transaction; record the intent first and cause the effect after, and record sending and delivery separately.
5. **DG-5 One spelling** (one thing ≠ two spellings): one concept, one name, one envelope, one id namespace; a change that adds a second spelling schedules the removal of the old one at the same time.
6. **DG-6 The same copy** (what the check reads ≠ what execution uses): decisions about permission, deduplication and state transitions read the copy execution uses, at the moment it uses it; a permission level equals its actual effect.
7. **DG-7 Unknown is not absent** (unknown ≠ none): a failed read and no response are both "unknown"; deleting, declaring dead and closing rest only on positive evidence, and touch only what you have proven you own.
8. **DG-8 A control that can go red** (passed ≠ never tested): every "pass" comes with a control under the same conditions that proves the check can go red; what was not measured is written "not measured".
9. **DG-9 Inherit item by item** (has a reason ≠ merely history): anything inherited is graded item by item as port as is, small change, redo or do not port, with evidence that still holds today; defects are not ported, invariants are not dropped.
10. **DG-10 The person's place** (a person's decision ≠ a machine's input): what needs a person's judgement has a clear decision point, channel and safe default; the machine never presses keys for a person, and never asks a person what it can answer itself.

---

## 1. The limit, and what happens when it is full

**At most 12 rules.** Why 12:

- **One page**: one line per rule, and 12 lines plus a heading fit on one phone screen or one printed page. Beyond that, the list turns from something "remembered" into
  something "looked up", and a list that has to be looked up does not come to mind at the moment of design.
- **Asked in one sitting**: review asks rule by rule (§4), and 12 questions is an amount that can be asked in one go.
- **Two slots left, not filled**: this round distilled 10 classes, each with at least three independent incidents; slots 11 and 12 are kept for the next class that keeps coming back.
  The limit is a ceiling, not a target.

**At most 280 lines in total** = 12 rules × 15 lines per rule + 100 lines of fixed sections. Each rule has at most three failure shapes and each shape at most two incidents,
choosing the hardest ones rather than piling more on. Check: `wc -l docs/design-guidelines.md`.

**To add a new rule:**

1. Name the pair of states that "look the same but are different" that it keeps apart. If you cannot, it is advice, not a rule.
2. Attach at least two independent, real incidents, each saying where its evidence is.
3. First try changing the criterion of an existing rule; add a rule only when the existing criteria cannot stop the failure.
4. **When there are already 12 rules, the same change must name the rule it merges or deletes**, together with that rule's record of being cited in review (§4).

**Who decides: any session may propose (in a review finding or a PR); adding, deleting and merging are the user's call.**
While the user is away, the safe default is **not to add**, and the proposal stays unchanged on the to-review list. A rule that no review has cited for three consecutive waves is a candidate for removal —
it is handed to the user and **never deleted automatically**.

This section is the opposite of the timeline's defect. When the timeline's `entries` filled up, it **silently refused writes**, backfill gave up on that project for good,
and nobody was told (`timeline-design.md` §0). This document also refuses additions when full, but the refusal is public (a proposal has to answer on the spot which rule goes),
it has a named decider and a basis for removal; and nothing is swept automatically — an automatic sweep removes things that are still cited, the pit the old 200-entry limit fell into (DG-2).

---

## 2. One picture

```text
  input                                                        world
    |                                                            ^
    v                                                            |
 +--------+     +--------+     +--------+     +--------+         |
 | ENTRY  |---->| DECIDE |---->| STORE  |---->| EFFECT |---------+
 | DG-5   |     | DG-6   |     | DG-4   |     | DG-4   |
 |        |     | DG-7   |     |        |     |        |----> PERSON
 +--------+     +--------+     +---^----+     +--------+      DG-10
                                   |                            ^
 +---------------------------------+---------+                  |
 | LOOPS   beat, schedule, sweep, stream     |  stopped, full   |
 | DG-1 speaks   DG-2 bounded   DG-3 unit    |------------------+
 +-------------------------------------------+
 +------------------------------------------------------------------+
 | PROOF, over every box above:   DG-8 control   DG-9 inheritance   |
 +------------------------------------------------------------------+
```

- **ENTRY** (contracts, gates, routes): DG-5; everything that comes in has exactly one spelling.
- **DECIDE** (permission, deduplication, state transitions, declaring dead): DG-6 reads the copy execution uses; DG-7 unknown cannot be decided on.
- **STORE**: DG-4 record only facts. **EFFECT** (typing, push, deleting files): DG-4 the intent is persisted first, and sending and delivery are each recorded.
- **PERSON**: DG-10; receives only the decisions the machine cannot answer.
- **LOOPS**, the things that keep running: DG-1, DG-2, DG-3. The line into STORE is the 30 whole-file rewrites under DG-3 in §3; the line to PERSON is
  the channel through which "stopped" and "full" reach a person.
- **PROOF**: how we know each box above is right; DG-8, DG-9.

A rule that finds no place on this picture has no structure in common with the others; the list should be reorganized, not have it squeezed in.

---

## 3. Each rule's failures and criterion

> A path with no prefix is in this repository's `docs/`; "the old repository" is `~/code/clawdline`; a *task report* is a child's report,
> kept at `/tmp/.clawdline/<task>/artifacts/report.md`, and it is reclaimed (see §5).

### DG-1 Speaks up (stopped ≠ idle)

- The old `/v1/health` had no `beat`, `scheduler` or `store` field at all, and still answered `ok: true` after the heartbeat stopped; git history holds four main-thread hangs,
  each fixed by removing the blocking call, and not one of them added detection (`broker-design.md` §2.6, §4 C1).
- The scheduler logged only when it fired, so "swept and found nothing due" and "the scheduler stopped" were the same observation, and whether a fix worked could not be answered (`447fd55`, `plan.md` §3.2).
- On 2026-09-06, 26 landings went unrecorded and 10 deliveries were redone, while `/inflight` listed every one of them correctly all day: "Nothing made anybody look." (`coordination.md` §1)
- **Criterion**: stop the loop, and `/v1/health` answers `ok:false` with a `reason` within 3×tick (the acceptance wording of `broker-design.md` §8 B2).
  Every state that needs a person (a dead letter, full, stopped) can point to the channel that brings it in front of a person, and that channel has actually been walked once.
  Unsupported input gets a named refusal and is not ignored: "a broker that ignores `serialize` starts a task its caller asked to wait" (`5feea4b`).

### DG-2 Bounded (full ≠ normal)

- The timeline's `entries` at 2,000/2,000: the limit was implemented as refusing writes, backfill skipped the project for good once it saw `capacity`, the store was frozen for more than a day,
  nobody was told, and the old records never noted that it was full (`timeline-design.md` §0, §3.3).
- `remote-audit.jsonl` at 5.7 MB, 36,010 lines and 31 days without rotation, and taking its last 200 lines read the whole file in; `worktree.kept` held 3,699 entries,
  which were really 158 worktrees that could not be cleaned, recorded again every 6 hours (`broker-design.md` §2.1, §2.6).
- Eviction can be wrong too: a hard-coded 200-entry limit swept away the only evidence for usage classification, and 149 rows can never be attributed; after the limit became configurable, the test for it
  "would have gone on passing while testing nothing" (old repository `4eb97d86`, `794d3a53`; `broker-design.md` §4 F1, F2).
- **Criterion**: the design document has a table `name | limit | when full | who finds out | who decides eviction` with no empty cell. "When full" depends on the class of data (DG-4):
  a fact can only be refused, out loud; an observation that can be recomputed may be evicted — 98.7% of the timeline's events were imports recomputable from git, yet when full it chose to refuse, while the 200-entry limit evicted facts.
  An unresolved state counts as accumulation too, and its eviction is its exit condition. The limit can be injected, and at least one test injects a small value, runs to full, and asserts that "full" is said out loud.

### DG-3 Measure the total (one fast run ≠ a cheaper total)

- One board persist took 42 ms, 93% of it encoding, and on that basis the old version did not split it — that number is right (`board-design.md` §2.2). But `orchestrator.json`
  was rewritten whole 30 times in 315 seconds for a net change of +90 B, about 202 MB spent for 90 bytes, and four writes in five changed only the observation clock (`broker-design.md` §2.2).
  What needed measuring was write amplification, not how fast one write is.
- The board write "1.23 s → 0.20 s" measured one write; after the fix, 32 hours still held 479 materializations over 200 ms and 45 over 1 second,
  and a board read on Cloud queued for as long as 229 seconds (`board-design.md` §2.4, §2.6).
- The timeline's acceptance recorded "1,561 B / 0.017 s, fine", and what it measured was an empty result: the default filter removed 100% of the data (`timeline-design.md` §2.7).
- **Criterion**: every performance number carries four things: a denominator (bytes written against bytes of useful change, times per day), a distribution (p50/p99/maximum and the sample count),
  an input size that is also non-empty, and which end was measured (client, queue, server, disk). Missing any one, it is only a single reading.

### DG-4 Record only facts (fact ≠ observation, sent ≠ delivered)

- Observations were persisted: `executor.observed_at` and `inventory_generation` changed every 10 seconds, and each change rewrote 6.75 MB whole; of the board's 12,872
  history entries, 52.5% were projections the machine recomputed, carrying no new fact (`broker-design.md` §2.2, §6.2, `board-design.md` §2.3).
- One fact without one transaction: the old version wrote five files for one dispatch with no shared transaction; "has it landed" had three copies, and the page said 53 were unlanded,
  git said 24 of those had long been ancestors, and the queue said 17 (`broker-design.md` §2.3, old repository `e924dd9a`).
- Sent taken for delivered: the completion notice was once a single send, lost when the terminal was busy, the app restarted or root changed processes, leaving only a
  `result.json` nobody was told about (old repository `f05ed2b3`); a briefing typed into a tty got `command not found: Your` back from the shell (`9de8527`).
- **Criterion**: for every persistent field ask "can the next reading compute it?"; if it can, it is not persisted; while tasks are briefed but no state changes, the write count stays close to 0
  (`broker-design.md` §8 B2). State, event and receipt share one transaction; every side effect points to the persisted event that triggered it;
  the same request sent twice leaves the event count unchanged (`9de8527`: settled twice, still 13 events).

### DG-5 One spelling (one thing ≠ two spellings)

- Refusals had two envelopes, `{"error":{"code","message"}}` and `{"error":"<code>","detail"}`; a client that read only the first turned
  `session_unknown` into `command_failed` and lost `reasons` entirely (`cloud-wire.md` §10.5).
- Two fields both called session id wanted opposite values: `root.session_id` was given a terminal id, dispatch accepted it, the child did the work, and `notifyRoot` found no one and logged nothing,
  so four tasks were orphaned (old repository `8dffbe49`); the landing slot's holder was named by conversation id and looked up by terminal id, so `advance` was never once delivered (`broker-design.md` §5 O2).
- One name for two things: `pending` meant both "someone is working on it" and "the executor is dead", and a line waited behind it for 14 hours (`broker-design.md` §5 O1);
  the phone's `data-view` and the desktop's `data-pane` were merged into one attribute, and at 760 wide clicking a row showed no conversation (`replica.md`, reviewer round two, N1).
- **Criterion**: the concept register in `plan.md` §10: every concept has one owner and one spelling, and a guard goes red when it sees a second. In review ask:
  does this change give a concept that already exists a second name, envelope or id? If so, where is the date for removing the old spelling written down?

### DG-6 The same copy (what the check reads ≠ what execution uses)

- The gate read the decoded `r.URL.Path` while the mux matched `cleanPath(EscapedPath())`: `/v1/sessions/..%2F..%2Fv1%2Fauth%2Fx/git` was, to the gate,
  the public `/v1/auth/…` and, to the mux, a session id, so without a token it reached the handler and inventoried the whole machine; on the way to the fix a second spelling turned up,
  `…/documents/../../../../v1/health` (`15ff376`; `replica.md`, "the copy of the path the gate reads").
- Read, do something slow, save the copy back: the notification pump typed for a few seconds and then saved back its copy from before typing, overwriting an ACK that had arrived in between; dispatch saved `spawning` back,
  overwriting a `briefed` that had just been proven. Four kinds of lost update, one shape (`5feea4b`; `broker.md`, "three things the measurements caught", #3).
- Read-level permission, write-level effect: the read-only Git panel would run the `core.fsmonitor` a malicious repository names (`TestChangesRunsNothingTheRepositoryNames`
  in `internal/adapters/git/changes_test.go`); the read-only list starts a `codex app-server` on every read,
  and so far only a limit has been added: "This route is a GET that starts a program." (`codexServers` in `internal/adapters/projects/past.go`)
- **Criterion**: permission, dispatch and every place that decides by path read the key one function produces (`routePath`). Every "read → slow step → write"
  re-reads inside the lock (`mutate`), and a failure-injection test puts a second writer inside the slow step (`race_test.go`).
  In review ask: is the variable this decision reads the same one the next line executes with?

### DG-7 Unknown is not absent (unknown ≠ none)

- The keep list was never read, and it printed exactly like "nothing to keep", so 25 worktrees were removed, one of them with someone still working in it
  (old repository `d31a09df`); `git status` on a path that does not exist returns exit 0 and no output, the same as clean, and that closed three landing records (`broker-design.md` §4 G1).
- Four minutes without a progress note was recorded as `spawn_failed`, although the briefing said plainly not to send heartbeats, so a working child was recorded as never having started (`eefa318`);
  the old version, in the same shape, deleted the checkout and delivery branch of a child that was still working (old repository `95f6a30b`, `broker-design.md` §4 D1).
- Touching what is not yours: `pkill -f 'bin/clawdline serve'` killed a sibling child's daemon (two task reports);
  a test session opened inside the user's tmux, because `$TMUX` overrode `TMUX_TMPDIR` (a task report).
- **Criterion**: three values in the type: known present, known absent, unknown (`Complete:false` is not an empty list, `cross-platform.md` §5). Every destructive action
  lists the readings and the proof of ownership it rests on, and refuses if any of them is unknown. Tests break the read source and assert the result is "unknown", not empty or no (the control group in `b8f5258`).

### DG-8 A control that can go red (passed ≠ never tested)

- A reading with no power to say no: six minutes of watching without a dispatch after the scheduler fix proved nothing (`447fd55`); the old repository's service worker was fixed over eleven rounds, and
  "A reading that has no power to disagree with you looks exactly like a reading that agrees." (old repository `docs/hard-problems.md`)
- Unequal conditions: Chrome zooms `localhost` to 110% by default, so the two sides had dpr 2.2 against 2.0 (a task report); Cloud's "end to end" test
  used devtools to plant keys in IndexedDB and skip pairing, which proved the transport, not the pairing (`cutover.md` §4.2).
- Green that never ran: "1 of 7716 checks failed" against a tree with 8,218 checks, more than five hundred of which never ran (old repository
  `docs/machine-resource-scheduling.md`); the native shell had not compiled since the voice wave, until another child ran into it and it was fixed (introduced in `8ede06e`, fixed in `5e9115f`).
- **Criterion**: every row of an acceptance record says "control: …, result: red"; a regression test comes with "ran on the code before the fix, and failed"
  (`15ff376`: 8 of the 12 old spellings returned 200); a comparison of two sides lists width, dpr, zoom and data set, and proves they are the same.

### DG-9 Inherit item by item (has a reason ≠ merely history)

- Defects were ported faithfully: the pairing wrong-guess count resetting with each new pairing, and Origin compared by hostname alone while cookies ignore the port, both came over unchanged from Swift
  (`remote.md`, "Order", point 2; task report findings F-02 and F-03, fixed in `f2caa6a`); the 11 defects the old version still has open today had to be named one by one as not ported
  (`broker-design.md` §5).
- The shape was ported and the invariant dropped: the broker's first wave let landing records be silently overwritten, let `claims: []` read like a promise, and treated unreadable as absent,
  reproducing the old incidents exactly (`broker-design-challenge.md` §4).
- The reasons that "port it together with its reason" relied on did not exist: five retypes, two observations 30 seconds apart, read 4 / write 8 — no commit anywhere says why; when two of them were graded port as is,
  a commit from 12 minutes earlier (`eefa318`) had already overturned them by measurement (`broker-design-challenge.md` §2.2, §2.3).
- **Criterion**: a porting document has a table grading each item port as is, small change, redo or do not port, with an evidence column on every row; "an earlier analysis said so" and "to match the old version" are not evidence.
  For every shape not ported, say where the invariant it guarded went.

### DG-10 The person's place (a person's decision ≠ a machine's input)

- The machine pressed a key for a person: the broker saw an assistant on the tty and typed, then Enter; that Enter answered the workspace trust dialog,
  the cursor was on "No, exit", and the child exited without reading anything (`eefa318`; `broker.md`, "three things the measurements caught", #1).
- What should reach a person went where nobody looks: a child asked root a question directly in a progress note, root received neither of the two, and the child had to decide by itself;
  both guides said at the time that progress would wake root (old repository `825fc32e`).
- A person standing in the machine's place: `landed` had one entry point and had to be recorded by hand, 22 were written by hand in one night, and 14 of them had been in `main` for days
  (old repository `docs/landing.md`); 17 "are you stuck?" messages got 17 "I have not stopped" replies, when the answer was in the same row's `mover` (`coordination.md` §4.2).
- **Criterion**: the design document lists every point that needs a person: who, through which channel they see it, the safe default when nobody answers, and whether the machine could trigger it by mistake. Any program that types into a tty
  proves before typing that the other side is a composer and not a dialog (`TestADialogIsNeverTypedInto`). A question the machine can answer is not sent to a person.

---

## 4. How this document is used

**When designing a new feature**, the design document or PR description carries a "guidelines answer": one question per rule, one sentence each. What you cannot answer is written "unknown", never left blank (DG-7);
a rule broken on purpose gets its reason and goes to the user to decide (DG-10).

1. When it stops, who finds out, and how soon? (DG-1)
2. What does it accumulate? The limit, what happens when full, who finds out, who evicts? (DG-2)
3. What is the denominator of its cost? On how large a non-empty input was it measured? (DG-3)
4. Which fields can be recomputed? Where is the transaction boundary of each outward action? Where are sending and delivery each recorded? (DG-4)
5. Which names, ids and envelopes does it add? Does the register already hold the same concept? (DG-5)
6. Does every decision read the copy execution uses? Is there a slow step between the read and the write? (DG-6)
7. Which reads can fail? When they do, which action would delete something or declare something dead because of it? (DG-7)
8. What is the acceptance control? How is it proven that it can go red? (DG-8)
9. What was inherited, and from where? What is today's evidence for each item? (DG-9)
10. Where does a person decide? Through which channel, with what default? Could the machine trigger it by mistake? (DG-10)

**In review**, choose the rules by what the change touched. The table below is the minimum:

| This change touched | Look at least at |
|---|---|
| Routes, gates, permissions, wire contracts | DG-5, DG-6 |
| Storage, schemas, events, receipts | DG-4, DG-2 |
| Background loops, queues, connections, schedules | DG-1, DG-2, DG-3 |
| Deleting, sweeping, declaring dead, closing | DG-7 |
| Porting from the old version or another document | DG-9 |
| Typing into a terminal, notifications, asking a person to decide | DG-10 |
| Any claim that something "passes" or is "faster" | DG-8, DG-3 |

Every review finding is tagged with its rule number, for example `[DG-6]`; this is the only data source for the removal basis in §1. A finding that fits no rule is tagged `[DG-?]`;
once two independent ones accumulate, that is a proposal for a new rule, and it follows the procedure in §1.

---

## 5. Checking this document by its own rules

| Question | Answer |
|---|---|
| Is it observed? (DG-1) | **No observation that actually runs.** §4 asks reviews to tag `[DG-n]`, but nothing collects the tags; when a rule goes stale, nothing speaks up. **Broken.** |
| What is its limit? (DG-2) | 12 rules, 280 lines, three failure shapes per rule. Now 10 rules, 273 lines. **Kept.** |
| When full, who decides what goes? (DG-2, DG-10) | The user; while they are away, the default is not to add. **Kept**, but the content of this version has not been through them yet (see point 6 below). |
| Can it be drawn? | Yes, §2. Each of the ten has a place; a rule that finds no place is a rule that should be reorganized. |

**Which rules it breaks, stated plainly:**

1. **DG-1**: as in the table. The citation record is only a convention.
2. **DG-4**: evidence cited as a task report exists only in a report under `/tmp`, reclaimed when the task ends; evidence that points to commits and to documents in the repository is not.
   What could be moved to a durable source has been; 5 task reports remain, in DG-7, DG-8 and DG-9.
3. **DG-6**: the list, the picture and §3 describe the same set of rules in three places, and no guard keeps them consistent, only "change them in the same commit".
   The manual check follows; every row must read `1 1 1` (this version ran it, and all ten do):

   ```sh
   f=docs/design-guidelines.md; for n in $(seq 1 12); do echo "DG-$n $(grep -cE "^[0-9]+\. \*\*DG-$n " $f) $(grep -cE "^### DG-$n " $f) $(sed -n '/^```text/,/^```$/p' $f | grep -cE "DG-$n([^0-9]|$)")"; done
   ```
4. **DG-8**: there is no control. It has not been used to review a good design to see whether it raises false alarms, nor applied to a new design to see whether it catches problems in advance.
   Only a retrospective was done: each of the 11 unfixed old-version defects in `broker-design.md` §5 maps to at least one rule (O1, O2, O11→DG-5; O3→DG-2;
   O4, O8, O9→DG-7; O5, O6→DG-4; O7→DG-1; O10→DG-6). The rules were written from these very incidents, so mapping back is only a necessary condition.
5. **DG-9**: the numbers are the ones each document and report measured for itself, not re-measured today. Every quotation was checked against its source, except `4eb97d86` and `e924dd9a`, which are quoted by way of
   `broker-design.md`. `broker-design-challenge.md` had not landed when this was written; the version cited is the one its authoring task finished with.
6. **DG-10**: written while the user was asleep. The limit of 12, the `DG-` numbering and the choice of these ten are all safe defaults I chose, awaiting their decision.
7. **DG-3**: what this document is really for — whether the next design has fewer incidents because of it — has not been measured at all; the line count is only a proxy.
