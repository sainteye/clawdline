# Bot work cards

Status: proposal, awaiting the person's review. Nothing here is implemented. No code, schema, wire
word or tool name in this page exists yet unless it is cited with a `file:line` as current
behaviour.

The person's summary in Traditional Chinese is [`bot-work-cards.zh-TW.md`](bot-work-cards.zh-TW.md).

Revision 2 (after review). The review accepted the overall design and named five gaps; this
revision closes them and says where one cannot be closed by a machine reading: batch integration
(§6.7), carried, cherry-picked and squashed landings (§6.4), a revert after verification (§6.6), the
rejected range capturing other people's commits (§6.2), and destinations that only meant something
in this repository (§6.1). It also adds an optional acceptance step on the live destination (§6.8).

## 1. Problem

The person hands work to persistent Bots: a lead that builds a feature, a reviewer, a QA Bot, and
an integrator Bot that merges to `main`, pushes, installs the dev build on this machine and deploys
the hosted console. In the Bot team this month every fact about that work lived in chat prose:

1. *Where is it now?* Nobody can say whether a feature is built, reviewed, merged, pushed, on this
   machine's dev install or on Cloud without reading several Bot chats.
2. *A rejection did not stop anything.* A reviewer Bot rejected two commits. They still reached
   `origin/main` and Cloud: the rejection was a chat message, and the local reverts that were
   supposed to honour it were lost to a reset. Nothing durable stood between a rejected commit and
   a push or deploy.
3. *Relations are oral.* "Commit X supersedes Y" and "carrier X carries Y" exist only in the
   conversation that said them.
4. *Destinations are checked by hand.* The integrator reads local `main`, `origin/main`, the dev
   version and Cloud `BUILD.json` itself every time, and telling the original author that the work
   is live is a remembered habit.

What exists today and why it does not answer these:

- **Bot work links** are a per-task projection of broker records into the Bot's chat
  (`AssistantWorkSnapshotOf`, `internal/app/orchestrator/project_assistant_actor.go:436`). They
  know one dispatched task's state, its delivery commit and its landing
  (`internal/domain/projectassistant/types.go:466-480`), and the chat strip lists them until their
  landing settles (`docs/bots.md:351-370`). They have no notion of one request spanning several
  Bots, no review verdict, and no destination past the landing.
- **Board v2 items** carry a lifecycle with landing evidence, but only a person, or a Session
  relaying a person's run, creates or assigns one (`docs/work-system-v2.md:41-62`), and a Bot actor
  has no Board authority (`docs/project-assistants-broker.md:79-81`). `dispatch_work` can *link* an
  existing item that the person named (`internal/adapters/projectassistantacp/mcp.go:1004`,
  `:800-806`) but never creates or moves one.
- **Agent proposals** are drafts a person must accept (`docs/work-system-v2.md:918-934`). They are
  the wrong shape for status the person already authorized by giving the task.
- **The pre-push hook** refuses a push to `main` while CI on `main` is red, and refuses when it
  cannot read CI (`tools/git-hooks/pre-push:16-28`). It knows nothing of review verdicts.

## 2. Decided direction (the person's; not reopened here)

- A **new record type**, separate from Board v2 work items, modelled on Agent proposals: its own
  tables, its own Board section, never counted in Board counts. Human-created Board items and how
  Sessions use them stay unchanged. None of the invariants in `docs/work-system-v2.md` §2 is
  amended.
- **Not** a new `kind` on work items: that would need an exception to every person-only rule in
  §2.1, §2.2 and §7.

## 3. Purpose and non-goals

A Bot work card answers, from durable records the system reads, never from Bot prose:

1. Which person request is this, and who authorized it?
2. Which Bot holds it now, in which role (maker, reviewer, integrator, or the optional acceptor)?
3. What did review say about the exact commit, and is a rejected commit anywhere it should not be?
4. Which destinations contain it, read when? The target branch and its remote in every Project,
   plus whatever further destinations the card's Project defines (for Clawdline itself: this
   machine's dev install and Cloud).
5. Is it still there — or was it reverted or superseded after it was verified?
6. Is it done, and has the Bot that asked been told?

Non-goals:

- It is not human work. It never becomes a Board item, is never promoted, and never appears in
  Board counts, Unassigned / In progress / Needs attention columns or a Session's assigned items.
- It does not schedule Bots, wake idle ones beyond the one handoff notice in §8.3, or pick who
  reviews.
- It does not replace the broker. Dispatch, landing, callbacks and receipts stay where they are
  (`docs/work-system-v2.md:894-916`).
- It does not run CI, build, push or deploy. It reads results and refuses guarded steps.

### 3.1 Relationship to existing records

| Record | Who creates it | What it is to a card |
|---|---|---|
| Board v2 item | person, or a Session on the person's run | optional `board_item_id` link; the card never edits, moves or duplicates the item |
| Agent proposal | a Session | unrelated; a card is not a proposal and needs no acceptance |
| Quick Session to-do | person | unrelated |
| Broker task | broker, on `dispatch_work` | execution detail of one card through `bot_work_id`, or of an integration batch through `bot_work_batch_id` (§9.1, §6.7), as `work_id` is for items (`docs/work-system-v2.md:914-916`) |
| Bot work link | WorkBridge, from a task | unchanged; it gains a read-only `bot_work_id` so the chat strip can group by card |
| Bot exchange (`message_bot`) | a Bot | carries `bot_work_id` so the recipient's work attaches to the same card (§9.3) |
| Landing record / `carries` | broker | read as evidence; `carries` stays the broker's task-level fact (`docs/broker.md:139-147`). A card proves containment with the same Git read, not by trusting the declaration (§6.4) |

## 4. Record shape

Tables (names reserved, not created): `bot_work_cards`, `bot_work_events`, `bot_work_verdicts`,
`bot_work_relations`, `bot_work_readings`, `bot_work_batches`, `bot_work_batch_cards` (§6.7) and
`bot_work_destinations` (§6.1). Like `work_v2_proposals`
(`internal/adapters/store/work_v2.go:170`), they live in the same store, beside but never joined
into `work_v2_items` counts.

`bot_work_cards`:

| Field | Meaning |
|---|---|
| `id` | lowercase UUID; deterministic from the authority key (§8.1) |
| `project_id`, `project_path` | catalog Project and path snapshot, as items do (`docs/work-system-v2.md:80-93`) |
| `title` | plain-language result, at most the dispatch title bound (`work.OutcomeTitleLimit`, used at `mcp.go:1000`) |
| `description` | plain-language Markdown, bounded (§11) |
| `authority` | `{kind, bot, conversation_id, turn_id, message_id}`; `kind` is `person_message` (the Bot's own user-origin turn) or `relayed` (a `bot_request` turn whose exchange chain roots in a user-origin turn) |
| `created_by` | the Bot that caused creation |
| `baton` | `{role, bot}`: `role` ∈ `maker`, `reviewer`, `integrator`, and `acceptor` where acceptance is required (§6.8); exactly one holder |
| `chain` | the ordered Bots the authority covers (§7); the creator plus every Bot it handed to or messaged with this card |
| `base_commit`, `candidate_commit` | the range under review; set by the maker's handoff, never by text |
| `target` | the branch the work lands on, a snapshot taken at the first handoff to review: the branch the Project's primary checkout has out, the same line the broker names for a landing (`docs/broker.md:121-126`). The `main` destination means this branch whatever it is called |
| `landed_commit`, `containment` | set by the daemon after integration (§6.4): the commit on `target` that holds the candidate's change, and how that was established — `ancestor`, `patch_equivalent` or `asserted` |
| `batch_id` | the open or landed integration batch the card is in, if any (§6.7) |
| `board_item_id` | optional link, only when the person's message named that item (same rule as `mcp.go:1004`) |
| `deployment` | per-destination policy, a map from destination key to `required` / `not_required`, with the revision of each Project definition it was snapshotted from (§6.1) |
| `phase`, `condition` | §5 |
| `version` | optimistic version; every write names the version it read |
| `created_at`, `updated_at`, `closed_at` | timestamps |

`bot_work_relations`: `{card_id, kind, subject_commit, object_commit, object_card_id?, recorded_by,
at}` where `kind` ∈ `supersedes` (subject commit replaces the object commit's verdict, or — with
`object_card_id` — the subject card replaces that card's change, §6.6), `carries` (subject commit's
landing includes the object card's or commit's change, §6.4) and `reverts` (subject commit removed
the object commit's change from the target, §6.6). A relation is an assertion by a chain Bot and is
labelled as one. No assertion alone turns a light green or lifts a rejection: the guard's use of
`supersedes` (§6.3) requires a pass verdict on the subject commit, an asserted `carries` needs a
reviewer's pass on the carrier (§6.4), and `reverts` only ever turns a light red.

`bot_work_verdicts` and `bot_work_readings` are in §6 and §7.

## 5. Lifecycle

```text
making -> in_review -> integrating -> [accepting] -> done
   ^          |             |              |
   +-- rework +-------------+--------------+   (reject, an integrator returning it, a failed acceptance)
any open phase -> cancelled  (person only)
done / cancelled -> making   (person reopens)
```

`accepting` exists only on a card whose policy asks for acceptance (§6.8); every other card goes
from `integrating` to `done` as before.

| Phase | Entered by | Guard |
|---|---|---|
| `making` | creation; `handoff` back to a maker; reopen | none beyond authority |
| `in_review` | maker's `handoff` to a reviewer | names `candidate_commit` that resolves in the Project and `base_commit` that is its ancestor |
| `integrating` | reviewer's `pass` or `conditional_pass` verdict on the current `candidate_commit`, then `handoff` to an integrator | the verdict's commit equals `candidate_commit` |
| `accepting` | the daemon, when every required destination is verified and the card's policy has `acceptance: required` (§6.8) | the baton moves to the named acceptor Bot |
| `done` | the daemon, when every required destination reading is verified (§6.5) and, where required, acceptance is recorded | readings, not a Bot's claim; a Bot cannot set `done` |
| `cancelled` | person | reason |

Conditions layered on a phase, as in `docs/work-system-v2.md:343-349`: `waiting_person` (a Bot
asked the person through `ask_person`; the card stores the question's id and clears when it is
answered), `rejected_published` (the daemon found a rejected, un-superseded commit on the remote or on a
Project destination; only a person clears it), `reverted` (the card's change was contained in the
target and a later commit removed it, §6.6; only a person clears it, or a new landing of the card),
`superseded` (another card with a pass verdict recorded that it replaces this one, §6.6), `blocked`
(a holder-stated reason), `evidence_unknown` (a required reading is unreadable). A condition never
moves the phase.

Events are append-only, one per accepted mutation, with actor, previous and next version and time
(`docs/work-system-v2.md:238-242`): `card.created`, `baton.handed`, `verdict.recorded`,
`relation.added`, `condition.set`, `condition.cleared`, `batch.joined`, `batch.left`,
`containment.recorded`, `destination.verified` (first verified reading of a destination for the
current landed commit), `acceptance.recorded`, `card.done`, `author.notified`, `card.cancelled`,
`card.reopened`, `verdict.overridden`. Periodic readings refresh
`bot_work_readings` and do not write events unless a destination changes state.

Rework: a `reject` returns the card to `making` with the baton on the maker who handed it in. The
next handoff names a new `candidate_commit`; the earlier verdict stays bound to its old commit.

## 6. Review, guards, destinations and completion

### 6.1 Destinations and deployment policy

The first draft fixed four destinations — `main`, `github`, `dev`, `cloud`. Two of them only mean
something in one repository: `dev` is *this daemon's own* running stamp
(`updatecheck.Running`, `internal/adapters/updatecheck/updatecheck.go:86`), and `cloud` is the
hosted console's `BUILD.json`, whose address is a constant of this product
(`SourceURL`, `updatecheck.go:51-57`). A Bot working in any other Project has neither. Destinations
are therefore two kinds.

**General destinations, in every Project.** They need no definition:

| Key | What it is |
|---|---|
| `main` | the card's `target` branch in the Project's repository (§4). The key is fixed; the branch need not be called `main` |
| `remote` | that branch's remote-tracking ref, after a bounded fetch. It replaces the first draft's `github`: the remote need not be GitHub |

**Project destinations, defined per Project.** `bot_work_destinations`:
`{project_id, key, label, reader, params, revision, defined_by, at}`. `reader` is one of a closed
set of readers the daemon implements; a definition chooses and parameterizes one, and never
supplies code or a shell command to be trusted as a reading:

| Reader | Verified when | Parameters |
|---|---|---|
| `daemon_stamp` | this daemon's running build stamp is the subject commit or a descendant of it in the Project's repository | none. Valid only where the running stamp resolves as a commit in the Project's repository — the same condition dev standing already tests (`internal/transport/http/update_dev_standing.go:141-146`); anywhere else it reads `unknown` |
| `build_stamp_url` | a JSON build stamp fetched from the URL names the subject commit or a descendant, **and**, when `callback_command` is set, a daemon-run callback with that recorded command and that stamp exited 0 (§6.4) | `url`, optional `callback_command` |

Clawdline's own two destinations are the first two definitions, not built-in keys: `dev`
(`daemon_stamp`) and `cloud` (`build_stamp_url` with the hosted console's `BUILD.json` and
`tools/wait-hosted-console.sh` as the callback command). The daemon offers them as a preset when a
Project's repository holds its running stamp; the person confirms once.

Only a person defines, changes or removes a Project destination, because a definition decides what
"done" means for every later card: a Bot that could define one could define an easy one. A change
bumps `revision`; open cards keep the revision they snapshotted and show "definition changed" until
a person re-applies the policy. A definition removed under an open card leaves that reading
`unknown` (`destination_undefined`), never verified.

**A card in a Project with no definitions** has exactly the two general destinations. Its lights are
build, review, main and remote (§10), it completes on those, and nothing about dev or Cloud is shown
or implied. If the target branch has no upstream at creation, `remote` starts `not_required` with
the reason `no_remote`, and the card says so rather than waiting for a ref that will never exist.

**Policy.** `deployment` maps each destination key the card has — the two general ones plus the
Project's definitions at creation — to `required` or `not_required`. The creating Bot proposes it
(for example a docs-only change in Clawdline: `dev` and `cloud` not required). A chain Bot may
tighten a destination to `required`; only a person relaxes one to `not_required` after creation.
*Open question Q4 asks the default.*

### 6.2 Verdicts bound to a commit

`bot_work_verdicts`: `{id, card_id, commit, base_commit, verdict, conditions, reviewer_bot,
rejected_commits[], rejected_patch_ids[], at}`.

- `verdict` ∈ `pass`, `conditional_pass`, `reject`. `conditional_pass` requires non-empty
  `conditions` text; `reject` requires a reason.
- `commit` is resolved to a full object id at write time, as the Board landing check does
  (`internal/transport/http/work_v2_landing.go:39-44`).
- On `reject`, the daemon records `rejected_commits` by the range rule below (bounded, §11) and, if
  Q6 is answered yes, the `git patch-id --stable` of each non-merge commit, so a cherry-pick of a
  rejected change is still recognized.
- Only the reviewer holding the baton (role `reviewer`) records a verdict, and not on a commit it
  made itself (the dispatch of the commit's task is recorded on the card; same Bot ⇒ refused
  `self_review`).
- A verdict is never edited. A later verdict on a different commit can lift an earlier rejection
  only through `supersedes` (below).

**The range rule.** `base..commit` is every commit reachable from the candidate and not from the
base. When the maker merged the target into its branch to stay current, that includes other
people's commits, and a reject would then refuse them everywhere. The rule is instead:

```text
rejected_commits = reachable(commit)
                 - reachable(base_commit)
                 - reachable(target_at_verdict)          # local target branch
                 - reachable(remote_target_at_verdict)   # its remote-tracking ref, if any
```

that is, `git rev-list <commit> --not <base_commit> <target_at_verdict> <remote_target_at_verdict>`.
The two target commit ids are resolved when the verdict is written and stored on it, so the set is
reproducible after the target moves. A commit that had already reached the target was not this
card's to reject. The merge commit that brought the target in *is* in the set — it exists only on
the candidate's line — and it carries no patch id.

Two alternatives were weighed and not chosen:

- `--first-parent` from the candidate would also drop merged-in commits, but it drops the maker's
  own side branch merged into the candidate too: landing that side branch alone would then pass the
  guard. It under-captures; the chosen rule does not.
- Excluding by author or by task cannot be read from Git reliably.

One over-capture remains and is handled at evaluation, not at recording: a commit merged in from
*another card's unlanded branch* is neither on the target nor under the base, so it is in the set.
The guard (§6.3) skips a rejected commit that lies in another card's own recorded range when that
card's latest verdict on a commit containing it is `pass` or `conditional_pass` — it was reviewed
there, and that review governs it. If that other card has no verdict yet, the commit stays refused
with both cards named; that is a commit nobody has passed, inside a candidate somebody rejected.

*Acceptance test* (`TestRejectedRangeExcludesMergedInTargetCommits`, in a throwaway repository):
base `B` on the target; the target gains an unrelated commit `I`; the candidate branch has `R1`,
then a merge `M` of the target, then `R2`. A reject on `R2` must record exactly `{R1, M, R2}`. The
guard must then **allow** a push whose new commits are only `I` and **refuse** one containing `R1`.
The test is first run against the naive `base..commit` and its failure output kept: there `I` is
recorded and the innocent push is refused.

### 6.3 The rejected-commit guard

**Rule.** A step that publishes a tree tip `T` over a previous tip `P` is refused with
`bot_work_rejected_commit` when some reject verdict `V` has a rejected commit `c` (or patch id) such
that `c` is reachable from `T`, `c` is not reachable from `P`, and none of these holds:

- `V` has a person override;
- a `pass` verdict exists on a commit `S` reachable from `T` with a relation `S supersedes V.commit`;
- `c` lies in another card's range and that card passed it (§6.2);
- `c` is **cleanly reverted in `T`**: a commit after `c` and reachable from `T` has the patch id of
  `c`'s reversed diff (§6.6). This is what honouring a rejection by reverting it on the target looks
  like, and the daemon can read it. A revert that needed hand edits has a different patch id and is
  not recognized; it needs `supersedes` with a pass, or the person's override.

The answer names the card, the verdict, the commit and the reviewer. It is computed by one daemon
route, `POST /v1/bot-work/guard` with `{project, tip, previous}`, using the same Git reads as the
Board landing check (`ResolveCommit`, `IsAncestor`,
`internal/transport/http/work_v2_landing.go:39-70`). It answers `allow`, `refuse` or `unknown`;
**unknown refuses**, as the pre-push hook already does for unread CI
(`tools/git-hooks/pre-push:26-28`).

**Where the guard runs.** Today none of these paths passes through the daemon, so each needs a
call added:

| Path | Today | Guard placement |
|---|---|---|
| Landing on local `main` | a merge made in a shell; the broker records it afterwards | **not refused**: local `main` is reversible. Recording a landing whose tip contains a rejected commit sets the card's main light red and tells the integrator Bot. The verdict lives in the store, so a `git reset` that loses local reverts no longer loses the rejection |
| Push to the remote | in this repository, `tools/git-hooks/pre-push` (installed through `core.hooksPath`) | the hook calls the guard for each pushed ref with `local_sha` and `remote_sha`; exit 1 refused, 2 could not check |
| Clawdline's `dev` | `clawdline dev install [--rev]` (`cmd/clawdline/dev.go:22-80`) | preflight: guard with `tip` = the requested revision and `previous` = the running stamp (`updatecheck.Running`, `internal/adapters/updatecheck/updatecheck.go:86`) |
| Clawdline's `cloud` | the callback in `docs/hosted-console.md:103-155`, run from the private Cloud checkout | a new `clawdline bot-work guard --project <id> --tip "$stamp" --previous "$live"` line after the existing ancestry check and before `wrangler pages deploy` |

The first two rows are general; the last two belong to Clawdline's Project destinations. **In any
other Project the daemon installs nothing**: it does not write a repository's hooks or deploy
scripts. The person may add the one line `clawdline bot-work guard --project <id> --tip <sha>
--previous <sha>` to that Project's pre-push hook and to whatever publishes its destinations; until
they do, that Project has detection (the `remote` and Project readings setting
`rejected_published`) and no prevention, and the card says "no push guard installed" beside its
remote light. The daemon knows this from whether a guard call for that Project has ever arrived.

**Override, person only.** `POST /v1/bot-work/verdicts/<id>/override` with a device-authenticated
person and a reason (the same authority split as `/v1/work/v2/` versus `/v1/work/v2/agent/`,
`docs/work-system-v2.md:17-19`). It records `verdict.overridden`; the guard then ignores that
verdict. There is **no environment-variable override** for this guard, unlike
`CLAWDLINE_PUSH_ON_RED` (`tools/git-hooks/pre-push:24`), because any agent can set one.

**What the guard cannot do.** A hook is skipped by `git push --no-verify`, and a deploy run without
the new line is not checked. The guard therefore stops honest mistakes, which is what happened, and
the destination readings (§6.4) detect the rest: a rejected, un-superseded, un-overridden commit
found on the remote or on a Project destination sets `rejected_published` on the card, marks it *needs you* and
notifies the person. Routing every push through a daemon command instead is option B in §12 and
open question Q5.

**Conditional pass** does not block the guard by default; its conditions are shown on the card and
the integrator's handoff names which it met. *Q8 asks whether it should block.*

### 6.4 Containment and destination readings

**What is read.** Before integration a card has only a `candidate_commit`. After integration the
subject of every reading is the card's `landed_commit` `L`: a commit on the target that holds the
candidate's change. Each reading is stored in `bot_work_readings` as `{project_id, destination,
subject_commit, state, observed_commit, verified_at, reason}` with `state` ∈ `verified`, `pending`,
`unknown`, `not_required`, `refused`, `reverted`. Readings are keyed by subject commit, not by card,
so every card of one batch (§6.7) shares one row per destination.

**Containment: what "holds the candidate" means.** The first draft required the landed commit to
*contain* the candidate, meaning ancestry. Integration here is often not a merge: the broker
already has `incorporated` for a delivery that is not an ancestor of the commit that carried it
(`docs/broker.md:88`), and `carries` for a commit that brought other tasks along
(`docs/broker.md:139-147`). The daemon therefore establishes `L` and records **how**, in this
order, stopping at the first that holds:

| `containment` | Daemon read | What it proves | What it does not |
|---|---|---|---|
| `ancestor` | `candidate_commit` is an ancestor of `L` (`IsAncestor`, as `work_v2_landing.go:46-56`) | the exact reviewed commits are in the target's history | that a later commit did not remove them (§6.6) |
| `patch_equivalent` | `git cherry <L> <candidate_commit> <base_commit>` reports no `+` line and at least one `-` line — the read the broker uses for `carries` (`PatchesIn`, `internal/adapters/git/worktree.go:152-167`; `internal/app/orchestrator/landing_carries.go:47-67`) | every non-merge commit of the reviewed range has a commit with the same patch in `L`'s history: a clean cherry-pick or rebase | merge commits are outside the comparison (`docs/broker.md:146-147`); a pick that needed conflict edits has a different patch id and fails this read |
| `asserted` | none. The integrator records `carries` naming a carrier commit `S`; the daemon checks only that `S` resolves and is `L` or an ancestor of `L` | that a Bot said so, about a commit that really is on the target | anything about content |

`L` itself comes from a daemon-verified landing, never from a Bot's text: the integration task's
broker landing commit (`landed`, verified against the target, `docs/broker.md:87`), or — when the
integrator merged without a task — a commit it names, which the daemon checks is on the target with
the same reads as a Board direct landing (`verifyWorkV2DirectLanding`,
`internal/transport/http/work_v2_landing.go:16`).

Unlike the broker's `carries`, the card does not depend on anyone declaring anything for the first
two rows: it knows both ends (`L` and the candidate), so it runs the comparison itself. The cost the
broker avoided — searching history for an undeclared carrier (`docs/broker.md:147-148`) — does not
arise.

**Squashed and conflict-resolved landings cannot be proved, and the page says so.** A squash of
three commits into one, or a cherry-pick with hand-resolved conflicts, matches neither read, and no
Git read will: the broker's own record says a semantic integration makes the two trees differ, and
that tree equality cannot answer whether a behaviour was lost (`docs/broker.md:132-135`). There the assertion is unavoidable. It is handled like this:

- the main light shows *asserted by <integrator>*, in amber, never green;
- the card does not complete on an assertion alone. The **reviewer** who passed the candidate
  records a `pass` verdict on the carrier commit `S` (range: `S`'s first parent to `S`). That is an
  independent judgment bound to a commit the daemon verified is on the target, and with it
  `containment` is stored as `asserted` with `confirmed_by`, the light turns green and keeps the
  word *asserted*;
- a person can confirm instead. *Q9 asks whether the integrator's labelled assertion should be
  enough without the reviewer.*

The rejected-commit guard is separate from containment and gets weaker under the same conditions: a
rejected commit cherry-picked under a new id is seen only by patch id (Q6), and one squashed into
another commit is seen by nothing. The guard catches what Git can identify; the reviewer's pass on
the carrier is the check for the rest.

**Destinations.** For subject commit `L`:

| Destination | Reader reused | Verified when |
|---|---|---|
| `main` | Git reads of the card's target branch in the Project (`work_v2_landing.go:46-56`) | `L` is on the target, containment is `ancestor`, `patch_equivalent` or confirmed `asserted`, and no revert is found (§6.6) |
| `remote` | the target's remote-tracking ref (`work_v2_landing.go:57-68`), after a bounded `git fetch` | `L` is an ancestor of the fetched remote-tracking ref |
| a `daemon_stamp` destination (Clawdline's `dev`) | this daemon's own running stamp, `updatecheck.Running(WebRoot())` (`updatecheck.go:86-99`), and the ancestry read behind dev standing (`update_dev_standing.go:125-160`) | the running stamp is `L` or a descendant in the Project's repository |
| a `build_stamp_url` destination (Clawdline's `cloud`) | `updatecheck.Fetch` of the defined URL (`updatecheck.go:116-144`) **and**, when defined, a broker callback whose recorded command is the definition's `callback_command` with the stamp as its argument and which exited 0 | the stamp names `L` or a descendant, and that callback for that stamp or a descendant succeeded |

Before integration, `main` and `remote` are read for the candidate itself (`pending` unless it is
already an ancestor), and Project destinations read `pending`.

Two corrections to the brief's premise, found while reading:

1. **Board v2 has no system reader for dev or Cloud.** Its `deploying → done` step accepts the
   owning Agent's text `--deployment "<what was deployed, where>"`
   (`internal/domain/work/v2.go:852-858`, `:900`). Only the landing (local and remote-tracking
   target) is read by the daemon. The five delivery status lines in `AGENTS.md` are written by the
   agent by hand. Cards therefore cannot "reuse what Board v2 uses" for dev and Cloud; they reuse
   `updatecheck` and the hosted-console waiter, which Board v2 does not call.
2. **CloudGate has no Go reader.** The served-bundle check is a shell script that pipes the bundle
   through `web/console/tools/check-hosted-build.mjs` (`tools/wait-hosted-console.sh:75-77`,
   `docs/hosted-console.md:173-180`). Porting it to Go would duplicate a checker that is still
   changing. The proposal reads the exit of the existing script as run by the daemon's callback
   runner: the daemon starts it and records its exit (`internal/app/orchestrator/callback.go:22-35`),
   so the result is a daemon observation, not a Bot's assertion. The card checks the recorded
   command, not a description of it.

**Unknown never advances** (`docs/work-system-v2.md:73-74`): an unreadable repository, a failed
fetch, a missing or changed definition, a release install where no dev stamp exists, an unreachable
build stamp or a missing callback leaves the reading `unknown` with its reason, keeps the last verified time visible, and
does not complete the card.

Readings refresh on events that can change them — a landing record, a callback settling, a handoff
to `integrating` — and on a bounded timer while a card is `integrating`, never faster than
`bot_work.reading_refresh_seconds` (§11). The remote fetch and a build-stamp fetch are network reads
with timeouts and run after commit, outside any store transaction
(`docs/work-system-v2.md:1164-1168`).

### 6.5 Completion and notification

When every destination whose policy is `required` reads `verified` for the card's `landed_commit`,
none of `rejected_published`, `reverted` or `superseded` is set, and any required acceptance (§6.8)
is recorded, the daemon writes `card.done` in one transaction with an
outbox effect `bot.work.card_done`. The effect delivers one host notice to the card's
`authority.bot` (and, if different, `created_by`) through the same wake path dispatched work uses
(`WakeWorkCompletion`, `docs/project-assistants-broker.md:160-172`), keyed by
`bot-work-done-<card id>-<cycle>` so retries and restarts never notify twice. It is marked as a
report, not the person's instruction, and counts against `bots.follow_up_chain`
(`docs/project-assistants-broker.md:179-181`). The event `author.notified` records the receipt.

A Bot cannot mark a card done. A person can (`POST /v1/bot-work/cards/<id>/complete`, reason
required), as with Board items (`docs/work-system-v2.md:57-60`).

Cards that complete from the same batch reading are told in one notice per recipient Bot (§6.7).

### 6.6 Ancestry is not presence: reverts and supersession

A change that was merged and later reverted is still an ancestor of the target. With ancestry
alone every light stays green and the card completes — or stays complete — for work that is no
longer there. Git has no read for "is this change still present"; what it can read is narrower, and
the design uses exactly that and labels the rest.

**What the daemon can detect.** When containment is recorded, the daemon stores for the card, within
the bound in §11, the patch id of each range commit's *reversed* diff (`git diff <c> <c>^ | git
patch-id --stable`), and of the landing merge's reversed first-parent diff when `L` is a merge. A
later commit on the target with one of those patch ids is a clean `git revert` of that commit, or
of the whole merge. The scan runs over commits newly added to the target, and only when something
could have added them: each broker landing record in the Project, each guard call, and the reading
refresh of an `integrating` card. It reads the new commits once and compares against the stored ids
of open cards and of the most recently completed ones (`bot_work.revert_watch_cards`, §11), so its
cost follows what was added, not the length of history.

**What it cannot.** A revert that needed conflict edits, a change removed by hand in a later
commit, or a rewrite of the same lines has no identifying patch id. For those a chain Bot or the
person records a `reverts` relation naming the removing commit (which the daemon checks is on the
target after `L`). It is an assertion and is labelled as one; it is accepted without confirmation
because it can only turn a light red.

**What happens.** Either way the daemon sets `reverted` on the card with the removing commit:

- the `main` reading and every destination reading for that card become `reverted` — red, "landed
  in `L`, removed by `<commit>`" — instead of `verified`. A destination that still serves a build
  from before the revert is shown as such: the stamp is read, not assumed;
- an open card cannot complete while `reverted` is set;
- a card already `done` stays `done`, is labelled *done, later reverted*, returns to *needs you*,
  and the Bot(s) notified of completion get one notice that it was reverted. The daemon does not
  reopen it: reopening is the person's (§7), and a revert may be exactly what the person asked for;
- it clears when the person clears it, or when a new landing of the card records containment in a
  commit after the revert.

Readings therefore say what they are: *verified at <time>, no revert found through <target tip>*.
A green light is a dated observation, not a promise about now; the tip it was checked through is on
hover.

**Supersession.** When work is deliberately replaced rather than reverted — the shape the broker
lists as not yet modelled (`docs/broker.md:153`) — a chain Bot of the replacing card records
`supersedes` with `object_card_id`. The daemon accepts it only when the replacing card has a pass
verdict on its current candidate, then sets `superseded` on the older card, naming the newer one.
The older card's lights freeze at their last readings and are drawn grey with *superseded by
<card>*; it stops being read and cannot complete by itself. Closing it stays with the person
(complete or cancel), because a Bot's assertion should not close work a person authorized. It is
listed once under *needs you*.

### 6.7 Batch integration

The review reports that the integrator routinely lands several features in one dispatched Session,
one merge and one deploy. This repository's history shows the shape, though not how often: 4 of the last 400 merge commits reachable from `1f20d0eba` name a batch landing branch
(`git log --merges -400 -i --grep='land-batch' --format=%h 1f20d0eba | wc -l`, run 2026-11-24),
each one a task branch merged into a shared batch before the batch reached `main`. A broker task
with a single `bot_work_id` cannot be the execution of all of them.

**Decision: an integration batch record that cards reference**, not a list of card ids on the task.

`bot_work_batches`: `{id, project_id, target, integrator_bot, task_id?, landed_commit?, state,
version, created_at, landed_at}` with `state` ∈ `open`, `landed`, `dissolved`.
`bot_work_batch_cards`: `{batch_id, card_id, candidate_commit, card_version_at_join}`.

| Option | Gain | Cost |
|---|---|---|
| **A. batch record (chosen)** | one place for the shared facts — the landing commit, the task, the target; a card is in at most one open batch, so two integrators cannot both land it; the task and its request hash carry one id | two more tables **(I)** |
| B. `bot_work_ids[]` on the task | no new table | the shared landing commit has no owner and is copied onto every card; nothing stops a card being listed by two tasks; the task hash covers a list whose order must be canonicalized |
| C. one integration task per card | no change to §9.1 | not how integration is done; N merges and N deploys for one |

Every integration goes through a batch, a batch of one included, so there is one code path. Maker
and reviewer tasks keep `bot_work_id`; an integration task carries `bot_work_batch_id`; a task has
at most one of the two.

**How it runs.**

1. *Open.* The integrator calls `bot_work_batch {card_ids[], expected_versions[]}`. In one
   transaction every card must be `integrating`, hold a pass or conditional pass on its current
   candidate, have the caller as its integrator, share the Project and `target`, and be in no other
   open batch. All join or none do. At most `bot_work.cards_per_batch` cards (16, the number of
   tasks one landing may carry, `orchestrator.result_carries`,
   `internal/domain/capacity/register_broker.go:11`).
2. *Dispatch.* `dispatch_work` with `bot_work_batch_id` (§9.3). The task's work link shows the
   batch and its cards.
3. *One landing.* When the broker records the task `landed` at commit `L`, the batch records
   `landed_commit = L` and becomes `landed`; membership is then frozen. For **each** member the
   daemon computes containment against `L` separately (§6.4) and sets that card's `landed_commit`
   and `containment`. A member whose change cannot be shown in `L` stays `integrating` with its main
   light `unknown` (`not_in_batch_landing`); it does not hold the others back.
4. *One deploy, one callback.* Destination readings are keyed by subject commit (§6.4). All members
   share `L`, so one fetch of the remote, one stamp read and one waiter callback for that stamp
   verify the destination for every member at once. Nothing is run per card.
5. *Completion.* Each card completes by its own policy from the shared readings: a docs-only member
   may be done at `remote` while a sibling waits for `cloud`.

**Handoff and notices for a batch.**

- `bot_work_handoff` accepts up to `bot_work.cards_per_batch` cards in one call (a reviewer handing
  five passed cards to the integrator). It writes one `baton.handed` event per card and **one**
  notice to the receiver listing them, keyed by the call's idempotency key.
- Cards that reach `done` from the same reading of the same batch produce **one** notice per
  recipient Bot, listing its cards, keyed `bot-work-done-batch-<batch>-<recipient>-<n>`; a member
  that completes later is told by itself. `author.notified` is still written per card.
- A guard refusal on a batch tip names every member and verdict that caused it, not the first.
- Before landing, the integrator may return one card (`handoff` back to its maker); it leaves the
  batch (`batch.left`) and the rest proceed. A new candidate on a member also removes it: the batch
  joined a specific commit. An emptied batch becomes `dissolved`.
- A revert of the batch's landing merge sets `reverted` on every member (§6.6); a revert of one
  member's commits sets it on that member only.

### 6.8 Post-deploy acceptance (optional fourth role)

A destination reading says a build containing the commit is being served. It does not say the
feature works there. The team already has a step for that — a QA Bot checking the live destination,
for example by screenshot — and today it too is a chat message.

Proposed as an **optional fourth baton role, `acceptor`, off by default**:

- a card's policy may carry `acceptance: {required, bot, destination}`. Tightening and relaxing
  follow the deployment rule (§6.1);
- when the named destination reads `verified`, the daemon moves the card to `accepting` and hands
  the baton to that Bot with the usual single notice;
- the acceptor records `bot_work_accept {card_id, result: accepted | failed, observed_commit,
  evidence_artifact?, note}`. The daemon checks that `observed_commit` is what the destination's
  latest reading observed, so the acceptance is bound to a build, and refuses an acceptor that is
  the card's maker;
- `accepted` lets the card complete; `failed` returns it to its maker with the note, as a rejection
  does.

The result is **a Bot's assertion and is labelled as one** ("accepted by <Bot>", with the evidence
attached): the daemon cannot read a screenshot. That is why it is a separate, named step and not a
seventh "verified" light. The cost is one more phase name and tool name **(I)**. *Q10 asks whether
it belongs in the first delivery.*

## 7. Authority

The **chain** is the set of Bots that the person's request reached: the authority Bot, Bots it
handed the card to, and Bots it messaged with the card's id. Membership is recorded by the daemon
when a handoff or a `message_bot` with `bot_work_id` is accepted, never by text.

| Action | Person | Creating / authority Bot | Holder Bot | Other chain Bot | Bot outside chain | Session | Broker / daemon |
|---|---|---|---|---|---|---|---|
| Create | via a Bot (§8) | automatic (§8) | — | — | no | no | creates on the Bot's dispatch, idempotently |
| Read | yes | yes | yes | yes | yes (same machine's read authority) | yes | — |
| Edit title / description | yes | yes | yes | no | no | no | — |
| Hand off (within chain) | yes | yes | yes | no | no | no | — |
| Hand off to a Bot outside the chain | yes | no | no | no | no | no | — |
| Record a verdict | no | no | only as `reviewer` holder, not on own work | no | no | no | — |
| Confirm an asserted containment (§6.4) | yes | no | no | only the reviewer whose pass it rests on, by a pass on the carrier | no | no | — |
| Open a batch / add or remove a member (§6.7) | no | no | only as `integrator` holder of every card named | no | no | no | freezes membership on landing |
| Record acceptance (§6.8) | no | no | only as `acceptor` holder, not the card's maker | no | no | no | — |
| Add a relation (`supersedes`, `carries`, `reverts`) | yes | yes | yes | yes | no | no | — |
| Set `reverted` | yes (via `reverts`) | via `reverts` | via `reverts` | via `reverts` | no | no | yes, from a detected clean revert |
| Clear `reverted` / `superseded` | yes | no | no | no | no | no | `reverted` clears on a new proved landing |
| Define, change or remove a Project destination (§6.1) | yes | no | no | no | no | no | offers the Clawdline preset |
| Set / clear `waiting_person` | answer clears | — | yes, with an `ask_person` id | — | — | — | clears on answer |
| Tighten deployment or acceptance policy | yes | yes | yes | no | no | no | — |
| Relax deployment or acceptance policy | yes | no | no | no | no | no | — |
| Override a rejection | yes | no | no | no | no | no | — |
| Complete by hand / cancel / reopen | yes | no | no | no | no | no | — |
| Mark done | yes (by hand) | no | no | no | no | no | yes, from readings |
| Clear `rejected_published` | yes | no | no | no | no | no | — |

Sessions only read. A dispatched child Session's work attaches to the card through its task, and
its result is evidence the Bot hands on; a child cannot write the card. Root Sessions read cards
so a person relaying through a Session sees the same facts; writing on the person's behalf from a
Session is not part of this proposal.

## 8. Creation

### 8.1 The automatic rule

A card is created by the daemon, not by the model, when a Bot's `dispatch_work` is accepted with a
**non-empty `claims` list** (the task will write files and produce commits;
`mcp.go:1004-1005`) in a turn whose authority roots in a person:

- the turn's origin is `user` (`internal/domain/projectassistant/collaboration.go:14`), or
- the turn's origin is `bot_request` and its exchange's `source_turn_id` chain
  (`collaboration.go:48-56`) reaches a `user` turn within `MaxCollaborationHops` (`:24`).

A dispatch with an empty claims list (investigation, review read, question) creates nothing. A
routine, host notice, webhook, compact or network-request turn creates nothing.

**Idempotency.** The card id is a UUID derived from `(machine, root bot, root conversation, root
message_id)`, the same derivation style as `AssistantTaskID`
(`docs/project-assistants-broker.md:83-89`). Every dispatch in every Bot that roots in that person
message attaches to one card; a retried dispatch with the same idempotency key returns the same
task and the same card. A dispatch that already names a `bot_work_id` attaches to that card after
checking the dispatching Bot is in its chain.

The Bot may also call `bot_work_create` explicitly in the same circumstances (for work that starts
with a plan before any writing dispatch); it derives the same id, so the automatic path then
attaches instead of duplicating.

### 8.2 What the card does not get from text

The title comes from the first writing dispatch's `title`; the description from an explicit
`bot_work_create` or, failing that, the dispatch title alone. The daemon never parses the person's
message to infer scope, and never creates a card from a reply, a proposal, a landing or a timer
(`docs/work-system-v2.md:67-68`).

### 8.3 Handoff

`bot_work_handoff {cards: [{card_id, expected_version, candidate_commit?, base_commit?}], to_bot,
role, note, idempotency_key}` moves the baton of one card, or of up to `bot_work.cards_per_batch`
cards at once (§6.7); all move or none do. Accepted, it appends one `baton.handed` per card and
records one outbox effect that queues a **single** host-notice turn for the new holder — "these
cards are yours as reviewer", quoting the note — keyed by the call's idempotency key. The receiving
Bot need not be messaged separately.

This matters because a Bot answering another Bot's request cannot itself call `message_bot`
(`CheckCollaborationRequest` refuses `bot_request_tools`, `collaboration.go:123-126`). The card
tools are allowed in such a turn, so a reviewer reached by `message_bot` can still record its
verdict and hand the baton to the integrator.

## 9. Interfaces

### 9.1 Broker task fields

`bot_work_id` (one card: maker and reviewer work) or `bot_work_batch_id` (an integration batch,
§6.7) on the broker task record — at most one of the two, both beside `work_id` (`internal/app/orchestrator/draft.go:68`),
set only by the Bot dispatch door from the resolved card, never from client JSON on the ordinary
dispatch route. It is included in the assistant request hash only when non-empty, following the
`board_item_id` pattern that keeps old receipts' hashes
(`internal/app/orchestrator/project_assistant_actor.go:240-252`). `AssistantWorkSnapshotOf` copies
them into `WorkLink.BotWorkID` (`json:"bot_work_id,omitempty"`) and `WorkLink.BotWorkBatchID`
(`json:"bot_work_batch_id,omitempty"`).

### 9.2 Bot MCP tools

Added to `mcpTools()` (`internal/adapters/projectassistantacp/mcp.go:962`):

| Tool | Writes | Notes |
|---|---|---|
| `bot_work_read` | no | one card, or the caller's open cards; includes verdicts, relations, readings |
| `bot_work_create` | yes | §8.1; refused outside a person-rooted turn |
| `bot_work_handoff` | yes | §8.3 |
| `bot_work_verdict` | yes | §6.2 |
| `bot_work_relate` | yes | `supersedes` / `carries` / `reverts` |
| `bot_work_batch` | yes | open a batch, add or remove a member before landing, or name a landed commit when no task landed it (§6.7) |
| `bot_work_accept` | yes | §6.8; only if acceptance is delivered (Q10) |
| `bot_work_wait_person` | yes | sets `waiting_person` with an `ask_person` id |

Every writing tool takes `idempotency_key` and `expected_version`; a stale version answers the
typed conflict `bot_work_version_conflict` with the current version, as items do
(`docs/work-system-v2.md:75-76`).

### 9.3 Existing tools

- `dispatch_work` gains optional `bot_work_id` and `bot_work_batch_id` (not both). Both absent and
  person-rooted with claims ⇒ §8.1. With a batch id the daemon checks the caller is that batch's
  integrator and the batch is open; no card is created.
- `message_bot` gains optional `bot_work_id`. The daemon checks the sender is in the card's chain,
  stores the id on the exchange, adds the recipient to the chain, and names the card in the
  recipient's turn prompt. The recipient's dispatches then carry it by default.

### 9.4 HTTP routes

Agent routes under `/v1/bot-work/agent/` (the Bot host's credential), person routes under
`/v1/bot-work/` (device-authenticated, send-capable):

```text
GET  /v1/bot-work/cards?bot=&project=&state=open|closed&cursor=
GET  /v1/bot-work/cards/<id>
POST /v1/bot-work/cards/<id>/handoff        person: any Bot
POST /v1/bot-work/cards/<id>/deployment     person: relax or tighten
POST /v1/bot-work/cards/<id>/complete|cancel|reopen
POST /v1/bot-work/cards/<id>/conditions/<rejected_published|reverted|superseded>/clear
POST /v1/bot-work/cards/<id>/containment/confirm
GET  /v1/bot-work/batches/<id>
GET|PUT|DELETE /v1/bot-work/projects/<project>/destinations[/<key>]   person only
POST /v1/bot-work/verdicts/<id>/override
POST /v1/bot-work/guard                     loopback only; used by hook, dev install, deploy
POST /v1/bot-work/agent/cards[...]          the MCP tools' backing routes
```

`clawdline bot-work guard|show|list` wrap them for scripts.

## 10. Console

- **Board → "Bot work" section.** Its own switch beside *Show Agent proposals*
  (`docs/work-system-v2.md:254-258`), with a Bot filter. Rows: title, Project icon, holder Bot
  avatar and role, phase, and its lights. Cards of one batch can be shown grouped under the batch
  with its landing commit. Never counted in Board totals or columns.
- **Lights: four in every Project, then the Project's own.** *build* (the latest task's recorded
  verification for the candidate; labelled "reported by the worker" because `result.json`
  verification is the child's statement), *review* (the verdict on the current commit), *main* and
  *remote* (§6.4) appear on every card. After them comes one light per destination the card's
  Project defines, in definition order, labelled with the definition's `label` — for Clawdline,
  *dev* and *Cloud*, six lights in all; for a Project with no definitions, four and nothing more.
  With acceptance required, an *accepted* mark follows, drawn differently from the lights because
  it is a Bot's statement.
  Each light is green verified (with commit, time and the target tip it was checked through on
  hover), grey not required or superseded, amber pending or *asserted, unconfirmed*, red refused /
  rejected / rejected-published / reverted, and a question mark for unknown with the reason. A
  light that is green on an asserted containment keeps the word *asserted*.
- **Needs you.** Cards with `waiting_person`, `rejected_published`, `reverted` or `superseded`,
  an asserted containment waiting for confirmation, and override requests, are pinned at the top of the Bot work section and counted on the section's switch. Whether they also
  appear in the Board's *Needs attention* column is Q2.
- **Bot chat strip.** The existing dispatched strip (`docs/bots.md:351-370`) groups its task rows
  under their card when `bot_work_id` is present and shows the card's lights in one line. Without a
  card it is unchanged.
- **Project destinations.** Project settings list the Project's destination definitions, with the
  Clawdline preset offered where it applies; only the person edits them (§6.1).
- **Board item link.** An item with linked cards shows a "Bot work" chip in its detail view only;
  item cards and counts are unchanged.
- **Phone and Cloud.** The phone shows the same section in its vertical layout. Cloud carries new
  advertised read words `bot.work.cards` and `bot.work.card` and a send-capability word
  `bot.work.person` inside the encrypted envelope, following the `work.v2.cards` pattern
  (`internal/app/cloudops/ops_work_reads.go:28`, `docs/work-system-v2.md:1150-1154`); a machine that
  does not advertise them leaves the section hidden. The guard route is never on the wire. Payloads
  stay under the wire bounds (`docs/cloud-wire.md` §2.3); descriptions are previewed at 200 runes as
  in cards-v1.

## 11. Capacity, concurrency, migration, privacy

Bounds to register in `internal/domain/capacity` (a new `register_bot_work.go`) before code, each
refusing at the limit rather than evicting the person's records:

| Name | Proposed limit |
|---|---|
| `bot_work.open_cards` | 500 per machine |
| `bot_work.description_bytes` | 16 KiB |
| `bot_work.events_per_card` | 2,000 |
| `bot_work.verdicts_per_card` | 64 |
| `bot_work.rejected_commits_per_verdict` | 256 (a larger range is refused: review a smaller one) |
| `bot_work.revert_patch_ids_per_card` | 256, the same range bound; a card over it shows revert detection as `unknown`, not as clean |
| `bot_work.revert_watch_cards` | the 200 most recently completed cards per Project stay watched for a revert; older ones are no longer scanned and say so |
| `bot_work.revert_scan_commits` | 512 new target commits per scan; a larger jump reads `unknown` until scanned in later passes |
| `bot_work.cards_per_batch` | 16, matching `orchestrator.result_carries` |
| `bot_work.open_batches` | 32 per machine |
| `bot_work.destinations_per_project` | 6 Project-defined, so at most 8 destination lights |
| `bot_work.destination_param_bytes` | 2 KiB per definition (URL and command) |
| `bot_work.relations_per_card` | 32 |
| `bot_work.chain_bots` | 16 |
| `bot_work.page_rows` | 100 |
| `bot_work.reading_refresh_seconds` | 60 minimum between timer refreshes |
| `bot_work.guard_timeout_seconds` | 10 |
| `bot_work.receipts` | 4,096, like `board.receipts` (`internal/domain/capacity/register_board.go:13-18`) |

Concurrency: every write is one transaction that reads the card, checks actor, chain, baton,
version and transition, writes the next state, the event and any outbox effect; Git and network
I/O run after commit. Two Bots handing off at once: one wins, the other receives
`bot_work_version_conflict`.

Readings are stored per `(project, destination, subject commit)`, so a batch of 16 costs one row and
one network read per destination, not 16.

Migration: none. No existing Board item, proposal, task, work link or exchange is rewritten. Old
tasks have no `bot_work_id` and simply do not appear on cards.

Privacy: this repository is public. Examples in docs, tests and fixtures use invented role names
("lead Bot", "reviewer Bot") and synthetic commits; no Bot name, Project name, message text or path
from the person's machine.

## 12. Options considered

| Decision | Option | Gain | Cost | Reversibility |
|---|---|---|---|---|
| Record type | **A. new tables (chosen, by the person)** | no exception to §2 | a second list in the console | schema is irreversible once shipped |
| | B. Board item kind | one list | breaks §2.1/§2.2/§7 | — |
| Push guard | **A. pre-push hook calls the daemon (proposed)** | uses the hook that already runs; small | `--no-verify` skips it; detection catches the rest | reversible |
| | B. pushes only through `clawdline push` | cannot be skipped by a flag | every Bot's push habit and every brief changes; a raw `git push` still works unless remote credentials move | reversible, large |
| | C. no guard, alarm only | nothing to build in paths | repeats the incident before anyone looks | reversible |
| CloudGate | **A. run the existing waiter as a daemon callback (proposed)** | one checker | needs node on the machine, as now | reversible |
| | B. port to Go | daemon self-contained | two checkers that drift | reversible |
| Creation | **A. daemon creates on first writing dispatch (proposed)** | deterministic, idempotent | a request that never dispatches has no card | reversible |
| | B. model decides when to create | covers plan-first work | depends on the model remembering — the failure being fixed | — |
| Rejected-commit identity | A. commit ids only | cheap | a cherry-pick evades it | reversible |
| | B. ids plus patch ids (Q6) | catches cherry-picks | one `git patch-id` pass per push range | reversible |
| Destinations | A. four fixed keys (first draft) | simplest | `dev` and `cloud` are false or meaningless in every other Project | key names irreversible |
| | **B. two general keys plus per-Project definitions over a closed reader set (proposed)** | true in any Project; Clawdline's two become data | a definitions table and a settings surface | definition shape **(I)**; each definition reversible |
| | C. definitions that run an arbitrary command as the reading | any destination at all | a reading becomes whatever a script prints; the person's machine runs stored commands | — |
| Containment | A. ancestry only (first draft) | one read | wrong for every cherry-picked, carried or squashed landing | reversible |
| | **B. ancestry, then patch equivalence, then a labelled assertion needing a reviewer's pass (proposed)** | proves what Git can, labels what it cannot | a squash costs one more review step | reversible |
| | C. accept the integrator's assertion as proof | no extra step | the incident's shape: prose standing in for evidence | reversible |
| Revert | A. do nothing; lights mean "was an ancestor" | nothing to build | a reverted feature reads as live and done | — |
| | **B. detect clean reverts by reversed patch id; record the rest as a labelled relation (proposed)** | catches `git revert`, costs a pass over new commits only | hand-edited reverts need someone to say so | reversible |
| | C. compare trees or re-run the diff at every reading | closer to "present" | later edits to the same lines read as reverts; cost grows with history | reversible |
| Batch | **A. batch record (proposed)**, B. id list on the task, C. one task per card | see §6.7 | see §6.7 | tables **(I)** |
| Acceptance | **A. optional fourth role, off by default (proposed)** | the QA step becomes a record bound to a build | a phase and a tool name; its evidence is an assertion | names **(I)** |
| | B. non-goal | nothing to build | "is it working there" stays in chat | — |

Do-less: verdicts plus the push guard only, with no lifecycle or lights, would have stopped the
incident. It would not answer "where is it now". Phase 2 below is that do-less step and lands
first among the behaviour changes.

## 13. Delivery plan

Irreversible items are marked **(I)**: table schemas, destination keys and reader names, MCP tool
names, HTTP routes and Cloud words. Everything general lands before anything specific to this
repository: phases 1–4 work in any Project; phase 5 adds Project destinations and Clawdline's two.

1. **Domain and store (I: tables).** State machine, authority checks, capacity rows, tables, events.
   *Acceptance:* `go test ./internal/domain/botwork ./internal/adapters/store -run BotWork` covers
   every row of §7 refusing the wrong actor; `TestEveryBoundIsRegistered` fails before the rows are
   added and passes after; a second create with the same root message id returns the same card.
2. **Creation and handoff (I: tool names `bot_work_read|create|handoff`).** Automatic creation in
   the Bot dispatch door, `bot_work_id` on tasks and work links, handoff notice. *Acceptance:* an
   HTTP-boundary test dispatches twice from one person message through two Bots and gets one card
   with both tasks; an empty-claims dispatch creates none; a retried handoff queues one notice.
3. **Verdicts and the guard, general paths.** `bot_work_verdict` with the range rule (§6.2),
   `/v1/bot-work/guard`, the pre-push call. *Acceptance:* a test repository reproduces the incident
   — reject commit C, merge it, push — and the hook exits 1 naming C; adding a pass on S with
   `S supersedes C` makes it exit 0; a clean `git revert` of C makes it exit 0 and a hand-edited
   revert does not; a person override makes it exit 0; a stopped daemon makes it exit 2;
   `TestRejectedRangeExcludesMergedInTargetCommits` (§6.2) fails against `base..commit` and passes
   against the rule; `tools/test-pre-push-*.sh` style tests keep the red-CI behaviour.
4. **Containment, batches, general readings, reverts and completion (I: batch tables, tool
   `bot_work_batch`).** `main` and `remote` readers, the three containment kinds, the batch record,
   revert detection, the done effect and notify. *Acceptance, each in a throwaway repository:*
   (a) a candidate landed by merge reads `ancestor`, by clean cherry-pick `patch_equivalent`, and by
   squash stays amber `asserted` and does not complete until the reviewer passes the carrier;
   (b) a batch of three cards, one task and one landing gives three cards the same `landed_commit`,
   one reading row per destination, and one done notice per recipient across a daemon restart; a
   fourth member whose commits are not in the landing stays `integrating`;
   (c) after a card is done, `git revert` of its commit on the target sets `reverted`, turns its
   lights red and sends one notice; the same card with ancestry-only logic stays green (the red run
   kept);
   (d) a card in a Project with no destination definitions shows four lights and completes on
   `main` and `remote`; with no upstream, `remote` is `not_required` with `no_remote`.
5. **Project destinations and Clawdline's two (I: `bot_work_destinations`, reader names).** The
   definitions table and person-only routes, the `daemon_stamp` and `build_stamp_url` readers, the
   Clawdline preset, the `dev install` preflight and the deploy-callback line in
   `docs/hosted-console.md`. *Acceptance:* with a fake stamp server (`CLAWDLINE_NEXT_UPDATE_URL`,
   `updatecheck.go:46-57`) and a stub callback, a card stays open while `cloud` reads `unknown`,
   completes only after both checks pass; a Bot's attempt to define or relax a destination is
   refused; a `daemon_stamp` definition in a Project that does not hold the running stamp reads
   `unknown`, never verified; removing a definition under an open card reads
   `destination_undefined`.
6. **Console (desktop and phone).** Section, lights (four plus the Project's), batch grouping,
   needs-you, chat strip grouping, Project destination settings. *Acceptance:* console tests for
   each light state including `asserted` and `reverted`; a browser check that Board counts are
   identical with the section on and off, and that a non-Clawdline Project's card shows no dev or
   Cloud light.
7. **Cloud parity (I: wire words).** *Acceptance:* the phone reads a card through the relay with
   the new words advertised and hides the section when they are not.
8. **Acceptance role, if Q10 says yes (I: phase `accepting`, tool `bot_work_accept`).**
   *Acceptance:* a card with acceptance required does not complete on verified destinations alone;
   an acceptance naming a commit the destination did not observe is refused; the maker cannot
   accept its own card.

## 14. Risks and early signals

- **The guard is skipped.** Signal: `rejected_published` set on any card. Fallback: option B for
  pushes.
- **Readings are slow or flaky (network).** Signal: `evidence_unknown` dwell time on `integrating`
  cards. Fallback: longer refresh, manual refresh button for the person.
- **Authority chain misses relayed requests** (an exchange chain that does not reach a user turn).
  Signal: dispatches with claims and no card, counted in diagnostics. Fallback: explicit
  `bot_work_create` with the relayed exchange id.
- **Patch-id reads are wrong in a way nobody notices.** A false `patch_equivalent` would turn a
  light green on a change that is not there. Signal: none by itself, which is why the acceptance
  tests for §6.4 include a conflict-edited pick that must *fail* the read. Fallback: demote
  `patch_equivalent` to needing the reviewer's confirmation, as `asserted` does.
- **Squashes are common and every one costs a review step.** Signal: the share of cards whose
  containment is `asserted`, and how long they wait for confirmation. Fallback: Q9.
- **Reverts by hand go unrecorded.** Signal: a person finding a green card for missing work.
  Fallback: the `reverts` relation exists; if it is not used, acceptance (§6.8) on the live
  destination is the check that would notice.
- **A Project never installs the guard line.** Signal: "no push guard installed" on its cards.
  Fallback: detection by the `remote` reading still raises `rejected_published`.
- **Notice storms** from handoffs. Signal: `bots.follow_up_chain` notices. Fallback: the existing
  chain limit already stops them.

## 15. Not building

- Cards for requests arriving from another machine's Bot network (`network_request` origin): the
  authority chain crosses machines and Cloud carries only ciphertext; later, if asked.
- Scheduling or auto-assigning reviewers; the person and the Bots' own rules decide who reviews.
- Running CI, builds, pushes or deploys from the card.
- A Go port of the hosted-console checker (§12).
- Public Release and `clawdline.com` as Clawdline destinations: not in the incident; they have
  their own gates (`docs/releasing.md`). The per-Project definition shape (§6.1) leaves room to add
  them later without a new key in the product.
- Destination readers that run a stored command and trust its output, and Bots defining
  destinations: a reading must be something the daemon observed, and what "done" requires is the
  person's (§6.1).
- Installing hooks or editing deploy scripts in other Projects' repositories (§6.3).
- Detecting a hand-edited revert, a manual removal, or a rejected change squashed into another
  commit: Git cannot identify them; they are recorded by relation and labelled (§6.4, §6.6).
- A batch spanning Projects or targets: one landing has one repository and one target.
- Reopening or closing a card automatically on a revert or a supersession (§6.6).
- Editing Board items from cards, or showing cards in Board counts (decided direction).
- Root Sessions writing cards on the person's behalf.

## 16. Open questions for the person

- **Q1.** Should the Board's "Bot work" section be shown by default, or start hidden like
  *Show Agent proposals*?
- **Q2.** Should a Bot work card that needs you also appear in the Board's *Needs attention*
  column (mixing the two lists there only), or only in the Bot work section, the Bot chat and a
  notification?
- **Q3.** When one message to a Bot contains several separate tasks, is one card enough, or may the
  Bot split it into several cards under the same message?
- **Q4.** What is a new card's default deployment policy: every destination the Project has
  required, or local `main` and the remote required with the Project's own destinations (for
  Clawdline: dev and Cloud) chosen by the creating Bot?
- **Q5.** Is a push guard that `git push --no-verify` can skip, backed by an alarm when a rejected
  commit is found published, enough — or should every push go through a Clawdline command?
- **Q6.** Should the guard also recognize a rejected change that was cherry-picked under a new
  commit id, at the cost of a slightly slower push check?
- **Q7.** When a card is done, should only the Bot(s) that asked be told, or you as well?
- **Q8.** Should a conditional pass block push and deploy until the integrator states the
  conditions were met, or only be shown?
- **Q9.** When a feature was squashed or hand-merged so the system cannot prove the reviewed change
  is what landed, should the reviewer have to pass the landed commit before the card can complete
  (one more review step per squash), or is the integrator's statement, shown as "asserted", enough?
- **Q10.** Should the first version include the optional acceptance step — a QA Bot confirming on
  the live destination before the card completes, recorded as that Bot's statement — or should it
  wait until the rest is in use?

Q1–Q8 are the first draft's questions. Only Q4's wording changed, because destinations are now
per-Project (§6.1); Q9 and Q10 are new in revision 2.
