# Closing a delivery: landing, and what it means to be finished

Read this when you are the root session that dispatched code-producing work and it has come back,
or when you are about to commit somebody else's delivery. A child never lands, so a child does not
need this file.

It was part of `AGENTS.md` until every session in this repository was paying to read it at the
start of every conversation, including the ones that would never land anything.

## Root-owned landing closure

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
- The exact integrated-tree run is normally the **only full suite in the graph**. Implementers and
  reviewers use focused proof; confirmation reruns the questions a correction changed. Reuse a
  receipt only when repository, tree SHA, question, command digest and environment match. A second
  full run requires a typed inconclusive first result, not a desire to reconfirm green.
- One independent review seals its complete finding set before correction begins. All fixes from
  that set form one correction wave even when their write sets run in parallel. A third review
  requires `scope_changed`, `new_external_evidence`, or `systemic_pattern`; a recurring class after
  that is an architecture hold, not permission for an unbounded review/correction loop.
  The receipt and verdict shapes are specified in
  [`verification-workflow.md`](verification-workflow.md).
- **HEAD must compile standing alone, and a commit is the only thing that can break that.** It
  happened twice on 2026-08-26, from two different sessions: a whole-file `git add` carried three
  lines whose type was defined in a file that stayed uncommitted, and a protocol requirement landed
  in `Strings.swift` while its fourteen values stayed in the worktree. Both trees were green at the
  moment of committing. **A green tree says nothing about HEAD while anything is uncommitted** — the
  tree is the union of everybody's work and HEAD is only your slice, so a suite run in the tree is
  answering a question nobody asked.
  A partial commit is therefore not finished until its own slice has compiled on its own. Verify the
  staged tree the way [`AGENTS.md`](../AGENTS.md) describes, and where a change spans files ask what else defines what
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
- **Documents split by audience, and the split decides where they live.** `docs/` is what the
  community gets: English, written from the outside, tracked here, linked from both READMEs.
  `artifacts/` is a door into the private `clawdline-cloud` repository — internal working
  documents are read and written there, in whatever language suits, and they are not part of this
  repository at all. Anything worth showing somebody who installed this belongs in `docs/`, in
  English, rewritten for a reader who does not work here; moving an internal page across is a
  rewrite, not a copy.
- The living protocol page is `docs/clawdline-protocol.html`. Any change to Clawdline task,
  handoff, landing, claims, file-wait or cross-session communication semantics must update that
  standalone HTML in the same line of work and re-check it against the authoritative docs. A
  protocol change is not closed while that page still teaches the previous behavior.
- **Nothing in the suite may depend on a path that is not in this repository.** `Tests/main.swift`
  read the protocol Artifact through the `artifacts/` symlink with `try!`, so `./test.sh` could
  only pass on a machine that also had the private repository checked out beside this one — and
  every snapshot built the way [`AGENTS.md`](../AGENTS.md) describes died on it, because `git archive` carries what
  is tracked and that path is ignored. A clone must be able to run the suite green. When a test
  needs a document, that document is in `docs/`.
- Check `GET /v1/orchestrator/assistants` before dispatching, and read a `409
  assistant_exhausted`'s `alternatives` before retrying the same assistant. This closure still
  applies when a child dies mid-task because its assistant ran out of quota: whatever it had not
  committed is root's to recover or discard, exactly as with any other child that never reported.

## The landing queue, when more than one line is waiting

The obligation above is per-root. When several roots are landing into one shared checkout, somebody
also has to say who goes first — and on 2026-09-03 that somebody was a coordinator keeping the
order in messages, over seven lines. **It worked, and it worked because seven lines were unusually
cooperative rather than because it was reliable.** Four things went wrong, all recorded: two roots
working in that checkout were not in the list at all and were found by accident; one slot was filed
under the wrong line and both lines spent time correcting it; a wait's message foregrounded one of
its four correctly-listed `paths`, and the waiting line believed the conflict surface was one file
until it attempted the merge three hours later; and the one real ordering constraint — three lines
changing the same line of the same file, two upward and one downward, where the downward one in the
middle costs an extra re-measure — was discovered by a line that tripped over it.

`GET /v1/orchestrator/landing-queue?project=<dir>` is where that answer lives now, and the shape of
the fix is that **nobody writes membership**. A root is in its repository's queue when it has live
work there, a delivery on an unmerged branch, or a declared `landing: pending` — derived on every
read from the registry, through the same rule `GET /v1/orchestrator/inflight` uses. There is no add
call, so there is nothing to forget; and the two roots that vanished from the hand-kept list were
exactly the case a derived queue keeps, because their children were still working.

- **Order is the only thing a coordinator writes.** `POST /v1/orchestrator/landing-queue/order`
  takes root keys already in the queue. It cannot add a member (`409 not_queued`) and it cannot
  remove one: an entry the order does not name keeps its row, at the end, as `placement:
  "unplaced"`. A wrong order therefore puts somebody in the wrong place, which is visible, rather
  than out of the queue, which was not.
- **The contended-path answer is computed, not remembered.** Each entry's write set is its tasks'
  declared `claims` plus what each delivery branch changed against its own base, and
  `contended_paths` names every path more than one entry writes. That constraint is now readable
  before the order is set instead of after somebody has re-measured.
- **The slot hands itself on.** The holder is the first entry still in the queue, so a landing
  recorded above moves it with no second write.
  `POST /v1/orchestrator/landing-queue/advance` is what reaches the next root's session, once per
  holder per order generation, with a broker-composed message that prints the whole write set and
  names the route as the authority over its own prose.

**This does not replace a file wait and must not be described as replacing one.** A wait is
path-level and is registered between two named sessions about specific files; the queue is
slot-level and is about whole lines of work. A queue entry may well register a wait; neither is the
other's substitute.

**And one thing this cannot do, said out loud because the whole point is that it cannot be
incomplete:** a person working in the shared checkout with no Clawdline task at all is invisible to
the broker, so they are invisible here too. That is a boundary of what the broker can see rather
than of this queue, and it is the only way a line can still be absent from it.

**`claims: []` no longer reads as a promise.** For an isolated task the broker discards the
declared paths — correctly, because the child writes its own checkout at a different spelling — and
the dispatch contract says an empty declared set positively declares a task read-only. So a
delivery that went on to write twenty-six files read exactly like a review that wrote nothing. The
discarded list is now kept as that task's landing-time write set and answered back as
`landing_paths`, beside a `claims_declared` that says whether anything was declared at all; both
task projections emit them together, so the empty lease is never printed alone.

## Nothing said so when a landing record was never written

Everything above is a rule somebody follows. The queue derives its membership so nobody can forget
to add a line; the record itself has no such property — **`landed` has exactly one entrance and a
person is standing in it.** `POST /v1/orchestrator/tasks/:id/landing` verifies properly when it is
called (the broker runs `merge-base --is-ancestor <commit> refs/heads/<target>` inside the task's
own repository, and that check is right) and it is called by hand. Nobody calls it, nobody knows.

On the night of 2026-09-05/06 that one gap produced, in one evening: 22 landing records written by
hand at the end, 14 of them for deliveries that had been sitting in `main` for days; 21 delivery
branches nobody had merged; two roots re-running the same suite to rediscover the same red; and two
roots landing the same batch, neither aware of the other. Five of the hand-written records were for
a different repository, which is the part that matters most — **the gap belongs to the machine, not
to this checkout.**

`tools/check-landing-records.py` is the answer, and `./test.sh` runs it in the guards phase. It
reads the machine's task registry rather than this tree, groups every terminal task by the
repository it belongs to, and asks git the same question the broker would have asked — without
waiting to be asked.

**Three answers, kept apart, because collapsing them is how a number stops meaning anything.**

| what it says | what it is | what the count is |
|---|---|---|
| `unrecorded landing` | the delivery's head is an ancestor of the target branch and the record is open | a **lower bound**: a squashed or cherry-picked landing shares no commit with its branch and is invisible |
| `outstanding delivery` | terminal, has commits, head is not an ancestor, record open | an **upper bound** on work that has genuinely not landed, for the same reason from the other side |
| `undecidable` | a shared-checkout task that declared write paths and has no record | neither, and never folded into either — it has no branch, so git cannot be asked |

`git cherry` is deliberately not used. Its patch-id equality reports re-done work as unlanded — 9
of one evening's 45 were exactly that — and the commit-title scan people reach for next cannot
survive a reworded message. Ancestry is what the broker itself trusts.

**Who is called, when the root has gone home.** A record is closed as `landed` only with this
machine's orchestrator token, never with a task secret ([`api.md`](api.md)). So the obligation was
never really the root's to carry away: the credential that settles it belongs to the machine, and
whoever runs this suite in this repository is standing in front of the one door there is. The guard
prints the exact `curl` for each row.

**Why it does not fail on everything it finds.** Two limits, both deliberate:

- A **cutoff date**, `CUTOFF` in the guard. Debt older than it is printed in full on every run —
  individually, with dates and root labels — and does not fail the run. This exists to stop the
  *next* silent landing, not to hold a tree hostage to a backlog somebody is already draining.
  `--strict` fails on all of it, which is what a sweep wants. Move the cutoff forward only in a
  commit that says why: a cutoff that creeps silently is a mute button with a date on it.
- A **six-hour grace**. The order written above puts the record *after* the integrated-tree run,
  and this guard runs inside that run. With no grace it would fail the one suite the documented
  order places before the record — not a mechanism, a trap.
- **The runner has to be able to pay.** Reporting stays machine-wide and loud; failing asks two
  more questions and prints both answers. See below.

### Whose build stops, which is not the same question as who is told

On 2026-09-06 this guard cost two runs that never reached a compiler. The first was an isolated
child of another line, which died over `b932fa3a` — **another root's** landing, in the base
repository, in no way part of the child's checkout. It could not have cleared it even if it had
understood the message: a record is closed with this machine's orchestrator token, and the child
briefing written by `Sources/OrchestratorPlanning.swift` forbids a child from calling the landing
route at all. The run was refused, told to run a `curl` it is not permitted to run, and had nothing
left to do but stop. The second was the same row failing the merged tree an hour later.

So a row is fatal only when both of these hold, and the run prints both either way:

- **it is in the repository this suite is running in** — eight repositories are in this registry,
  and a build in one of them does not stop over another's; and
- **this run may close the record** — a run inside a **linked worktree** is a child's. That is
  derived rather than guessed: `git rev-parse --git-common-dir` names `<repository>/.git` from
  every checkout of a repository, so the main worktree is that directory's parent, which is the
  shell half of `OrchestratorDraft.mainWorktree(containing:)`.

`--strict` ignores both, because a root doing a sweep wants every row and holds the credential.

**A run that may not fail says so at length, and its green shares no sentence with the other one.**
It prints the rows, prints under each which of the two conditions it misses, prints who can settle
it, and ends with *these are real and none of them stops this run* rather than *nothing has landed
unrecorded*. The alternative is the defect [`shared-tree-guard.md`](shared-tree-guard.md) already
describes from the other side: a check that goes quiet inside a worktree and is read as a check
that passed. A run that cannot name the repository it is standing in at all exits 2 — the finder,
not the tree — rather than exiting 0 carrying the question.

**What the sweep changes here is the wording, not the set.** `landingSweepCandidates` takes
terminal tasks whose landing record already exists and is `pending` with a target; a row with no
record at all declares no target, so no timer is coming for it, and those rows are most of what
this finds. The ones a timer can take are marked as such in the output and are still fatal: the
sweep runs every 300 s and this guard excuses anything younger than six hours, so a row that
reaches the fatal set has already been offered to about seventy passes and is there because the
sweep declined it or could not see it. Making redness depend on a timer would also make it depend
on whether the app is running, which is a property of neither the tree nor the registry.

**And it has been watched going red.** `Tests/guard-red-proofs/landing-records.sh` builds a
repository with one delivery merged into `main` and a registry with one task, and the only
difference between its two arms is the `landing` key. The mutation is the missing record, in the
guard's own terms — not a deleted file, which is a thing that does not happen.

**And it refuses to be green for having nothing to look at.** A registry it cannot parse, one with
no `tasks` list, one with an empty one, a `--repository` no task in the registry belongs to, and a
registry whose task states this guard no longer recognises — the day `Orchestrator.State` grows a
spelling — all exit `2` and name themselves as the finder rather than the tree. The one case that
is legitimately silent is a machine with no registry at all, which is every clone and every CI run,
and it says so in a sentence. That distinction is not decoration: on 2026-09-06 a cleanup on this
machine removed 25 worktrees, one of them belonging to a task that was still running, because the
script's keep-list was never read — and a list that was never read prints exactly what a list of
nothing-to-keep prints.

**What it cannot see**, said out loud because that is the point of the file: a landing that reached
the target by squash, rebase or cherry-pick, which shares no commit with its branch; a repository
whose tasks have left the registry's retention window; and a person landing work with no Clawdline
task at all, which is the same boundary the queue has.

## And now the machine walks through the door it can prove

The passage above — **`landed` has exactly one entrance and a person is standing in it** — is no
longer wholly true, and what changed is which of the guard's three answers a machine acts on.

| the guard's answer | who acts on it now |
|---|---|
| `unrecorded landing` | **the broker.** After proving an isolated branch has complete clean, non-empty delivery evidence, its sweep asks the same `merge-base --is-ancestor` question and closes the record itself through the same verified path the HTTP route takes. |
| `outstanding delivery` | a person. The delivery is not in the target branch; whether that is work still to land or a squash nobody can see is a judgement. |
| `undecidable` | **the broker, but only narrowly.** A shared-checkout task has no branch to ask about, so the sweep asks the one question that *is* answerable — is anything this task was allowed to write still outstanding? — and closes on that, saying so. Everything else here is still a person's. |

**The two arms prove two different propositions and the record says which.**

- **Ancestry.** Terminal task, record open, a named target, and a delivery head supported by its
  own kind of evidence. An isolated task requires a known `worktree.base` and `worktree.head`,
  `worktree.commits > 0`, `worktree.dirty == false`, and a chosen head different from its base; if
  a successful ref scan finds live `refs/heads/clawdline/task/<id>`, it must agree with the
  recorded head. A successful scan that confirms the branch is absent may use the complete stored
  receipt, because merged delivery branches can be deleted. A ref scan that cannot launch, exits
  nonzero, returns malformed text, or exits successfully with stdout that is not valid UTF-8 is
  `unanswerable`: undecodable bytes are not an empty ref list, and may use neither stored head nor
  write-set containment. Missing, unknown, zero, dirty, empty, or
  contradictory worktree evidence likewise stays pending. If the admissible head is contained by
  `refs/heads/<target>`, the record closes as `landed` through
  `OrchestratorDraft.verifyTargetLanding` — so `verification_origin`, `verified_commit`,
  `verified_target_commit` and `landed_at` are all real, and this stays the two-check
  `work_complete`. **This proves the delivery reached the target.**
- **Write-set containment.** Terminal task, record open, a named target, no delivery branch, a
  non-empty `claims`, and every claimed path **resolving to something git can see**, unmodified in
  the task's `project_dir`, and identical to `refs/heads/<target>` — **on two readings at least
  five minutes apart**. The record closes as `landed` with the target's own commit, a note naming
  the predicate and both instants it was measured at, and **no verification field at all** — so
  `isBrokerVerifiedTargetLanding` is false and this can never become `work_complete`. **This proves
  only that nothing of the task's write set was outstanding**, which is a smaller sentence, and the
  smaller sentence must never be dressed as the larger one.

**Arm 2 has two rules arm 1 does not, and both come from the review of this feature.**

- **Every claim must resolve positively.** `git status` and `git diff` both answer *exit 0,
  nothing* for a pathspec that matches no file at all, which is character for character the answer
  they give for a path that is clean. Three of the twelve records below declared, as a single
  claim, three real filenames joined by spaces into one path that exists nowhere — and both dry
  runs closed all three, while the sixteen files those names really point at sat there modified.
  So a claim now has to be listed by `git ls-files` in the checkout **or** by `git ls-tree -r` in
  the target commit; two paths rather than one, so that a delivery whose point was deleting a file
  is not read as undecidable. A claim neither answers for makes the whole write set
  `unanswerable`, and the reason names that claim.
- **The same answer must come back twice, five minutes apart.** What this arm proves is true at an
  instant and the record it writes is permanent. On 2026-09-06 four of the rows below were clean
  and identical to the target at 20:24 and dirty again by 20:39, because somebody was still
  editing exactly those files; a live sweep would have closed all four for ever in between. Both
  readings must see the same target tip and the same claims, or the window restarts rather than
  continuing. The asymmetry is what settles it: every refusal this window can make is *look again
  in five minutes*, and a settled tree stays settled.

**What it refuses, always.** A settled record — a settled state may never move to another one. A
task that has not finished. A record with no named target. `abandoned`, which is a sentence about
somebody giving up and not a thing a machine can observe. `nothing_to_land`, which
`nothingToLandAdmission` refuses for these tasks anyway. And a repository git will not answer for is
**skipped**, never read as *nothing landed* — the same refusal `contained_commits` makes above.

**What it costs.** A pass runs at most every five minutes, **on a serial queue of its own**, never
while another pass is running, never from a read path, and never inside the registry lock. It looks
at at most twenty pending records, and it saves and broadcasts once for the whole pass rather than
once per closure.

The subprocess bound is worth stating properly, because the first version of this paragraph stated
it an order of magnitude low. Per *(repository, target)* pair: one `check-ref-format` and one
`rev-list`. Per git directory: one `for-each-ref`. Per repository identity — which for a worktree
task is *per task*, since its identity key is the task id — up to three `rev-parse` calls. Then per
row: an ancestry row that is actually landing pays four more in `verifyTargetLanding`, and a
write-set row pays three, or four when the checkout could not answer for every claim. Twenty
ancestry rows in twenty worktrees is therefore something like 140 subprocesses at a 15-second
timeout each, which is why the queue is the sweep's own: on `worktreeQueue` that worst case would
have been a dispatch waiting behind it.

The write happens under the lock behind a compare-and-swap on the exact record and task state the
git answers were about, so a record that settled while git was running is left alone.

**It compares against the ref the record names, and never against a better one.** Three of the
twelve records measured below name `blog-reread-2026-09-06` as their target while their work
actually reached `main`; that branch still existed and was eleven commits behind `main` when this
was written on 2026-09-06 — a live measurement, so read it as *behind, and drifting further* rather
than as the number eleven. Their claimed paths
are clean and identical to `main`, so a sweep willing to substitute the repository's default branch
would close all three — and it would then hold a record saying `target: blog-reread-2026-09-06,
state: landed` about a branch that does not contain the work. **Which branch a record should have
named is a person's decision**, it is settled by editing the record rather than by reading the tree,
and a settled state may never move afterwards. So the sweep reports what it found — *N claimed paths
differ from `refs/heads/blog-reread-2026-09-06`*, naming the ref it compared against — and leaves the
row. That sentence is what tells a person the record names the wrong branch, which is the thing they
can act on.

**Measured, on this machine's own registry, before any of it shipped — and the number has two
subjects, not one.** A dry run over the twelve pending records the user was looking at on
2026-09-06 answers about *(that registry snapshot × the shared checkout at the moment of the run)*,
and only the first half holds still. Arm 1 closes none of the twelve at any hour — not one of them
has a delivery branch — and that part is a property of the snapshot. How many arm 2 closes is not:

| when the checkout was read | arm 2 closed | left |
|---|---|---|
| 2026-09-06 20:24 CST, before the F1 rule | 8 | 4 |
| 2026-09-06 20:5x CST, the reviewer's independent recomputation | 4 | 8 |
| 2026-09-06 22:2x CST, with F1 in and the window counted | see `artifacts/dryrun-corrected.txt` | |

The four that moved between the first two readings are the four somebody was still editing; three
of the eight in the first reading were the bogus-pathspec rows F1 now refuses. **So do not quote a
closure count as a fact about the snapshot.** What is a fact about the snapshot: twelve candidates,
zero with a delivery branch, three whose claims resolve to nothing, and one — `dae845fb` — whose
claims were tracked and clean in every reading taken.

**And the twelve were closed by hand while this was being built, which is the cost rather than a
counter-example.** Between 11:55:26 and 11:55:40 UTC on 2026-09-06 — fourteen seconds — a person
settled all twelve with twelve separate `curl` calls, and every one of the resulting records is a
genuine broker verification: `verification_origin: local_target_branch`, four distinct commits
(`1527591f`, `9c3ca799`, `d9a0cf78`, `a52e42e5`), each confirmed contained by `main` at `64793579`.
Nobody cut a corner. The door simply admits one record at a time and only when somebody remembers to
walk through it, and three of the twelve needed their `target` changed on the way — which is exactly
the half a machine must not do. What the sweep removes is the remembering, not the judgement.

`tools/check-landing-records.py` and this sweep have to keep asking the same question. The guard
still runs in `./test.sh` and still prints the `curl` for every row a person must settle; what it
should now find, in the ordinary case, is that `unrecorded landing` is empty because the sweep got
there first.
