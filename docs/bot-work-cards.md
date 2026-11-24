# Bot work cards

Status: proposal, awaiting the person's review. Nothing here is implemented. No code, schema, wire
word or tool name in this page exists yet unless it is cited with a `file:line` as current
behaviour.

The person's summary in Traditional Chinese is [`bot-work-cards.zh-TW.md`](bot-work-cards.zh-TW.md).

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
2. Which Bot holds it now, in which role (maker, reviewer, integrator)?
3. What did review say about the exact commit, and is a rejected commit anywhere it should not be?
4. Which of local `main`, GitHub, this machine's dev and Cloud contain it, read when?
5. Is it done, and has the Bot that asked been told?

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
| Broker task | broker, on `dispatch_work` | execution detail of a card, through `bot_work_id` on the task (§9.1), as `work_id` is for items (`docs/work-system-v2.md:914-916`) |
| Bot work link | WorkBridge, from a task | unchanged; it gains a read-only `bot_work_id` so the chat strip can group by card |
| Bot exchange (`message_bot`) | a Bot | carries `bot_work_id` so the recipient's work attaches to the same card (§9.3) |
| Landing record / `carries` | broker | read as evidence; `carries` stays the broker's task-level fact (`docs/broker.md:139-147`) |

## 4. Record shape

Tables (names reserved, not created): `bot_work_cards`, `bot_work_events`, `bot_work_verdicts`,
`bot_work_relations`, `bot_work_readings`. Like `work_v2_proposals`
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
| `baton` | `{role, bot}`: `role` ∈ `maker`, `reviewer`, `integrator`; exactly one holder |
| `chain` | the ordered Bots the authority covers (§7); the creator plus every Bot it handed to or messaged with this card |
| `base_commit`, `candidate_commit` | the range under review; set by the maker's handoff, never by text |
| `board_item_id` | optional link, only when the person's message named that item (same rule as `mcp.go:1004`) |
| `deployment` | per-destination policy (§6.1) |
| `phase`, `condition` | §5 |
| `version` | optimistic version; every write names the version it read |
| `created_at`, `updated_at`, `closed_at` | timestamps |

`bot_work_relations`: `{card_id, kind, subject_commit, object_commit, object_card_id?, recorded_by,
at}` where `kind` ∈ `supersedes` (subject commit replaces the object commit's verdict) and `carries`
(subject commit's landing includes the object card's or commit's change). A relation is an
assertion by a chain Bot and is labelled as one; only the guard's use of `supersedes` (§6.3)
requires a pass verdict on the subject commit, so a relation alone never lifts a rejection.

`bot_work_verdicts` and `bot_work_readings` are in §6 and §7.

## 5. Lifecycle

```text
making -> in_review -> integrating -> done
   ^          |             |
   +-- rework +-------------+   (reject, or an integrator returning it)
any open phase -> cancelled  (person only)
done / cancelled -> making   (person reopens)
```

| Phase | Entered by | Guard |
|---|---|---|
| `making` | creation; `handoff` back to a maker; reopen | none beyond authority |
| `in_review` | maker's `handoff` to a reviewer | names `candidate_commit` that resolves in the Project and `base_commit` that is its ancestor |
| `integrating` | reviewer's `pass` or `conditional_pass` verdict on the current `candidate_commit`, then `handoff` to an integrator | the verdict's commit equals `candidate_commit` |
| `done` | the daemon, when every required destination reading is verified (§6.2) | readings, not a Bot's claim; a Bot cannot set `done` |
| `cancelled` | person | reason |

Conditions layered on a phase, as in `docs/work-system-v2.md:343-349`: `waiting_person` (a Bot
asked the person through `ask_person`; the card stores the question's id and clears when it is
answered), `rejected_published` (the daemon found a rejected, un-superseded commit on `origin/main`
or Cloud; only a person clears it), `blocked` (a holder-stated reason), `evidence_unknown` (a
required reading is unreadable). A condition never moves the phase.

Events are append-only, one per accepted mutation, with actor, previous and next version and time
(`docs/work-system-v2.md:238-242`): `card.created`, `baton.handed`, `verdict.recorded`,
`relation.added`, `condition.set`, `condition.cleared`, `destination.verified` (first verified
reading of a destination for the current candidate), `card.done`, `author.notified`,
`card.cancelled`, `card.reopened`, `verdict.overridden`. Periodic readings refresh
`bot_work_readings` and do not write events unless a destination changes state.

Rework: a `reject` returns the card to `making` with the baton on the maker who handed it in. The
next handoff names a new `candidate_commit`; the earlier verdict stays bound to its old commit.

## 6. Review, guards, destinations and completion

### 6.1 Deployment policy

`deployment` is four values, one per destination — `main`, `github`, `dev`, `cloud` — each
`required` or `not_required`. The creating Bot proposes the policy at creation (for example a
docs-only change: `dev` and `cloud` not required). A chain Bot may tighten a destination to
`required`; only a person relaxes one to `not_required` after creation. *Open question Q4 asks the
default.*

### 6.2 Verdicts bound to a commit

`bot_work_verdicts`: `{id, card_id, commit, base_commit, verdict, conditions, reviewer_bot,
rejected_commits[], rejected_patch_ids[], at}`.

- `verdict` ∈ `pass`, `conditional_pass`, `reject`. `conditional_pass` requires non-empty
  `conditions` text; `reject` requires a reason.
- `commit` is resolved to a full object id at write time, as the Board landing check does
  (`internal/transport/http/work_v2_landing.go:39-44`).
- On `reject`, the daemon records `rejected_commits` = `git rev-list base_commit..commit` (bounded,
  §11) and, if Q6 is answered yes, the `git patch-id --stable` of each, so a cherry-pick of a
  rejected change is still recognized.
- Only the reviewer holding the baton (role `reviewer`) records a verdict, and not on a commit it
  made itself (the dispatch of the commit's task is recorded on the card; same Bot ⇒ refused
  `self_review`).
- A verdict is never edited. A later verdict on a different commit can lift an earlier rejection
  only through `supersedes` (below).

### 6.3 The rejected-commit guard

**Rule.** A step that publishes a tree tip `T` over a previous tip `P` is refused with
`bot_work_rejected_commit` when some reject verdict `V` has a rejected commit `c` (or patch id) such
that `c` is reachable from `T`, `c` is not reachable from `P`, `V` has no person override, and there
is no `pass` verdict on a commit `S` reachable from `T` with a relation `S supersedes V.commit`.

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
| Push to `origin/main` | `tools/git-hooks/pre-push` (installed through `core.hooksPath`) | the hook calls the guard for each pushed ref with `local_sha` and `remote_sha`; exit 1 refused, 2 could not check |
| Dev install | `clawdline dev install [--rev]` (`cmd/clawdline/dev.go:22-80`) | preflight: guard with `tip` = the requested revision and `previous` = the running stamp (`updatecheck.Running`, `internal/adapters/updatecheck/updatecheck.go:86`) |
| Cloud deploy | the callback in `docs/hosted-console.md:103-155`, run from the private Cloud checkout | a new `clawdline bot-work guard --project <id> --tip "$stamp" --previous "$live"` line after the existing ancestry check and before `wrangler pages deploy` |

**Override, person only.** `POST /v1/bot-work/verdicts/<id>/override` with a device-authenticated
person and a reason (the same authority split as `/v1/work/v2/` versus `/v1/work/v2/agent/`,
`docs/work-system-v2.md:17-19`). It records `verdict.overridden`; the guard then ignores that
verdict. There is **no environment-variable override** for this guard, unlike
`CLAWDLINE_PUSH_ON_RED` (`tools/git-hooks/pre-push:24`), because any agent can set one.

**What the guard cannot do.** A hook is skipped by `git push --no-verify`, and a deploy run without
the new line is not checked. The guard therefore stops honest mistakes, which is what happened, and
the destination readings (§6.4) detect the rest: a rejected, un-superseded, un-overridden commit
found on `origin/main` or Cloud sets `rejected_published` on the card, marks it *needs you* and
notifies the person. Routing every push through a daemon command instead is option B in §12 and
open question Q5.

**Conditional pass** does not block the guard by default; its conditions are shown on the card and
the integrator's handoff names which it met. *Q8 asks whether it should block.*

### 6.4 Destination readings

Four readings, each computed by the daemon for the card's `candidate_commit` (after integration:
the commit the integrator's handoff names as landed, which must contain the candidate), each stored
in `bot_work_readings` as `{destination, state, observed_commit, verified_at, reason}` with
`state` ∈ `verified`, `pending`, `unknown`, `not_required`, `refused`.

| Destination | Reader reused | Verified when |
|---|---|---|
| `main` | Git reads of `refs/heads/main` in the Project (`work_v2_landing.go:46-56`) | candidate is an ancestor of local `main` |
| `github` | `refs/remotes/origin/main` (`work_v2_landing.go:57-68`), after a bounded `git fetch` | candidate is an ancestor of the fetched remote-tracking ref |
| `dev` | this daemon's own running stamp, `updatecheck.Running(WebRoot())` (`updatecheck.go:86-99`), and the ancestry read behind dev standing (`internal/transport/http/update_dev_standing.go:125-160`) | the running stamp is the candidate or a descendant in the Project's repository |
| `cloud` | `updatecheck.Fetch(SourceURL())` for `BUILD.json` (`updatecheck.go:51-56`, `:116-144`) **and** a broker callback whose command was `tools/wait-hosted-console.sh <stamp>` and exited 0 | `BUILD.json` names the candidate or a descendant, and that callback for that stamp or a descendant succeeded |

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
fetch, a release install where no dev stamp exists, an unreachable `BUILD.json` or a missing
callback leaves the reading `unknown` with its reason, keeps the last verified time visible, and
does not complete the card.

Readings refresh on events that can change them — a landing record, a callback settling, a handoff
to `integrating` — and on a bounded timer while a card is `integrating`, never faster than
`bot_work.reading_refresh_seconds` (§11). The GitHub fetch and the Cloud fetch are network reads
with timeouts and run after commit, outside any store transaction
(`docs/work-system-v2.md:1164-1168`).

### 6.5 Completion and notification

When every destination whose policy is `required` reads `verified` for the current commit and no
`rejected_published` condition is set, the daemon writes `card.done` in one transaction with an
outbox effect `bot.work.card_done`. The effect delivers one host notice to the card's
`authority.bot` (and, if different, `created_by`) through the same wake path dispatched work uses
(`WakeWorkCompletion`, `docs/project-assistants-broker.md:160-172`), keyed by
`bot-work-done-<card id>-<cycle>` so retries and restarts never notify twice. It is marked as a
report, not the person's instruction, and counts against `bots.follow_up_chain`
(`docs/project-assistants-broker.md:179-181`). The event `author.notified` records the receipt.

A Bot cannot mark a card done. A person can (`POST /v1/bot-work/cards/<id>/complete`, reason
required), as with Board items (`docs/work-system-v2.md:57-60`).

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
| Add a relation | yes | yes | yes | yes | no | no | — |
| Set / clear `waiting_person` | answer clears | — | yes, with an `ask_person` id | — | — | — | clears on answer |
| Tighten deployment policy | yes | yes | yes | no | no | no | — |
| Relax deployment policy | yes | no | no | no | no | no | — |
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

`bot_work_handoff {card_id, expected_version, to_bot, role, candidate_commit?, base_commit?,
note, idempotency_key}` moves the baton. Accepted, it appends `baton.handed` and records one outbox
effect that queues a single host-notice turn for the new holder — "card X is yours as reviewer",
quoting the note — keyed by `bot-work-baton-<card>-<version>`. The receiving Bot need not be
messaged separately.

This matters because a Bot answering another Bot's request cannot itself call `message_bot`
(`CheckCollaborationRequest` refuses `bot_request_tools`, `collaboration.go:123-126`). The card
tools are allowed in such a turn, so a reviewer reached by `message_bot` can still record its
verdict and hand the baton to the integrator.

## 9. Interfaces

### 9.1 Broker task field

`bot_work_id` on the broker task record, beside `work_id` (`internal/app/orchestrator/draft.go:68`),
set only by the Bot dispatch door from the resolved card, never from client JSON on the ordinary
dispatch route. It is included in the assistant request hash only when non-empty, following the
`board_item_id` pattern that keeps old receipts' hashes
(`internal/app/orchestrator/project_assistant_actor.go:240-252`). `AssistantWorkSnapshotOf` copies
it into `WorkLink.BotWorkID` (`json:"bot_work_id,omitempty"`).

### 9.2 Bot MCP tools

Added to `mcpTools()` (`internal/adapters/projectassistantacp/mcp.go:962`):

| Tool | Writes | Notes |
|---|---|---|
| `bot_work_read` | no | one card, or the caller's open cards; includes verdicts, relations, readings |
| `bot_work_create` | yes | §8.1; refused outside a person-rooted turn |
| `bot_work_handoff` | yes | §8.3 |
| `bot_work_verdict` | yes | §6.2 |
| `bot_work_relate` | yes | `supersedes` / `carries` |
| `bot_work_wait_person` | yes | sets `waiting_person` with an `ask_person` id |

Every writing tool takes `idempotency_key` and `expected_version`; a stale version answers the
typed conflict `bot_work_version_conflict` with the current version, as items do
(`docs/work-system-v2.md:75-76`).

### 9.3 Existing tools

- `dispatch_work` gains optional `bot_work_id`. Absent and person-rooted with claims ⇒ §8.1.
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
POST /v1/bot-work/cards/<id>/conditions/rejected_published/clear
POST /v1/bot-work/verdicts/<id>/override
POST /v1/bot-work/guard                     loopback only; used by hook, dev install, deploy
POST /v1/bot-work/agent/cards[...]          the MCP tools' backing routes
```

`clawdline bot-work guard|show|list` wrap them for scripts.

## 10. Console

- **Board → "Bot work" section.** Its own switch beside *Show Agent proposals*
  (`docs/work-system-v2.md:254-258`), with a Bot filter. Rows: title, Project icon, holder Bot
  avatar and role, phase, and six lights. Never counted in Board totals or columns.
- **Six lights.** *build* (the latest task's recorded verification for the candidate; labelled
  "reported by the worker" because `result.json` verification is the child's statement),
  *review* (the verdict on the current commit), *main*, *GitHub*, *dev*, *Cloud* (§6.4). Each light
  is green verified (with commit and time on hover), grey not required, amber pending, red refused /
  rejected / rejected-published, and a question mark for unknown with the reason.
- **Needs you.** Cards with `waiting_person` or `rejected_published`, and override requests, are
  pinned at the top of the Bot work section and counted on the section's switch. Whether they also
  appear in the Board's *Needs attention* column is Q2.
- **Bot chat strip.** The existing dispatched strip (`docs/bots.md:351-370`) groups its task rows
  under their card when `bot_work_id` is present and shows the card's lights in one line. Without a
  card it is unchanged.
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

Do-less: verdicts plus the push guard only, with no lifecycle or lights, would have stopped the
incident. It would not answer "where is it now". Phase 2 below is that do-less step and lands
first among the behaviour changes.

## 13. Delivery plan

Irreversible items are marked **(I)**: table schemas, MCP tool names, HTTP routes and Cloud words.

1. **Domain and store (I: tables).** State machine, authority checks, capacity rows, tables, events.
   *Acceptance:* `go test ./internal/domain/botwork ./internal/adapters/store -run BotWork` covers
   every row of §7 refusing the wrong actor; `TestEveryBoundIsRegistered` fails before the rows are
   added and passes after; a second create with the same root message id returns the same card.
2. **Creation and handoff (I: tool names `bot_work_read|create|handoff`).** Automatic creation in
   the Bot dispatch door, `bot_work_id` on tasks and work links, handoff notice. *Acceptance:* an
   HTTP-boundary test dispatches twice from one person message through two Bots and gets one card
   with both tasks; an empty-claims dispatch creates none; a retried handoff queues one notice.
3. **Verdicts and the guard.** `bot_work_verdict`, `/v1/bot-work/guard`, the pre-push call, the
   `dev install` preflight, the deploy-callback line in `docs/hosted-console.md`. *Acceptance:* a
   test repository reproduces the incident — reject commit C, merge it, push — and the hook exits
   1 naming C; adding a pass on S with `S supersedes C` makes it exit 0; a person override makes it
   exit 0; a stopped daemon makes it exit 2; `tools/test-pre-push-*.sh` style tests keep the red-CI
   behaviour.
4. **Readings and completion.** Four readers, `bot_work_readings`, done effect and notify.
   *Acceptance:* with a fake `BUILD.json` server (`CLAWDLINE_NEXT_UPDATE_URL`,
   `updatecheck.go:46-57`) and a stub callback, the card stays open while Cloud reads `unknown`,
   becomes `done` only after both Cloud checks pass, and the author Bot receives exactly one notice
   across a daemon restart.
5. **Console (desktop and phone).** Section, lights, needs-you, chat strip grouping. *Acceptance:*
   console tests for each light state; a browser check that Board counts are identical with the
   section on and off.
6. **Cloud parity (I: wire words).** *Acceptance:* the phone reads a card through the relay with
   the new words advertised and hides the section when they are not.

## 14. Risks and early signals

- **The guard is skipped.** Signal: `rejected_published` set on any card. Fallback: option B for
  pushes.
- **Readings are slow or flaky (network).** Signal: `evidence_unknown` dwell time on `integrating`
  cards. Fallback: longer refresh, manual refresh button for the person.
- **Authority chain misses relayed requests** (an exchange chain that does not reach a user turn).
  Signal: dispatches with claims and no card, counted in diagnostics. Fallback: explicit
  `bot_work_create` with the relayed exchange id.
- **Notice storms** from handoffs. Signal: `bots.follow_up_chain` notices. Fallback: the existing
  chain limit already stops them.

## 15. Not building

- Cards for requests arriving from another machine's Bot network (`network_request` origin): the
  authority chain crosses machines and Cloud carries only ciphertext; later, if asked.
- Scheduling or auto-assigning reviewers; the person and the Bots' own rules decide who reviews.
- Running CI, builds, pushes or deploys from the card.
- A Go port of the hosted-console checker (§12).
- Public Release and `clawdline.com` destinations: not in the incident; they have their own gates
  (`docs/releasing.md`).
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
- **Q4.** What is a new card's default deployment policy: all four destinations required, or
  local `main` and GitHub required with dev and Cloud chosen by the creating Bot?
- **Q5.** Is a push guard that `git push --no-verify` can skip, backed by an alarm when a rejected
  commit is found published, enough — or should every push go through a Clawdline command?
- **Q6.** Should the guard also recognize a rejected change that was cherry-picked under a new
  commit id, at the cost of a slightly slower push check?
- **Q7.** When a card is done, should only the Bot(s) that asked be told, or you as well?
- **Q8.** Should a conditional pass block push and deploy until the integrator states the
  conditions were met, or only be shown?
