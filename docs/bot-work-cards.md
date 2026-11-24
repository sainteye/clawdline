# Bot work cards

Status: proposal, revision 3, awaiting the person's review. Nothing here is implemented. No schema,
wire word or tool name in this page exists yet unless it is cited with a `file:line` as current
behaviour.

The person's summary in Traditional Chinese is [`bot-work-cards.zh-TW.md`](bot-work-cards.zh-TW.md).

Revision 3 is a simplification, on the person's instruction after reading the first draft: do not
over-design, stay flexible, and change things only when a real problem appears. Deployment state is
reported by the Bots, nothing is enforced, and cards can be split and merged like to-dos. What the
earlier drafts specified and this one removes is listed in §13, one line each.

## 1. Problem

The person hands work to persistent Bots: a lead that builds a feature, a reviewer, a QA Bot, and
an integrator Bot that merges, pushes, installs the dev build and deploys. In the Bot team this
month every fact about one request lived in chat prose spread over several Bots:

1. *Where is it now?* Nobody can say who holds a request, whether it was reviewed, or how far it
   has been delivered without reading several Bot chats.
2. *A review verdict left no record.* A reviewer Bot rejected two commits and they still reached
   the remote and Cloud. The rejection was a chat message; nothing showed it beside the work.
3. *Delivery is remembered, not written.* What reached `main`, GitHub, the dev install and Cloud is
   read by hand each time, and telling the Bot that asked is a habit.

What exists today:

- **The chat work result card** (`web/console/src/project-assistants/WorkResultCard.tsx`, shipped
  in `523e059d7`) shows **one dispatched Session's** result in the Bot's chat: its delivery commit,
  target branch and Broker landing, read from the task's work link
  (`WorkLink`, `internal/domain/projectassistant/types.go:466-490`), plus the linked Board item's
  independent verification, and it opens the worker Session
  (`docs/project-assistants-owned-mcp.md:101-110`). It is per task. It has no notion of one request
  crossing several Bots, of who holds it, or of a review verdict by another Bot.
- **Board v2 items** are created and assigned only by a person or a Session relaying a person's run
  (`docs/work-system-v2.md:41-62`); a Bot can *link* an item the person named
  (`internal/adapters/projectassistantacp/mcp.go:1004`, `:803`) but never creates or moves one.
- **Agent proposals** are drafts a person must accept (`docs/work-system-v2.md:918-934`): the wrong
  shape for work the person already authorized by asking.

## 2. Decided (the person's; not reopened here)

- A **new record type**, separate from Board v2 work items, modelled on Agent proposals: its own
  tables and its own Board section, never counted in Board counts. Not a new `kind` on work items.
- **Delivery is self-reported.** Each destination is a checklist entry the holding Bot marks, as
  Board v2's `--deployment` text is today (`internal/domain/work/v2.go:900`). The daemon does not
  compute it.
- **Nothing is enforced.** A verdict is recorded and shown; it blocks no commit, push or deploy.
  Enforcement was considered and deliberately deferred until a problem recurs (§13).
- **Cards split and merge**, like to-dos.
- **Completion notifies the person** as well as the Bot(s) that asked.
- The Board's "Bot work" section is **shown by default**. Cards that need the person do **not**
  appear in the Board's *Needs attention* column: only the Bot work section, the Bot chat and a
  notification.
- The default checklist is **`main` and `GitHub`**; the creating Bot adds further entries.

## 3. What a card is

One card is **one request from the person, across Bots**. It answers:

1. Which message of the person authorized it?
2. Which Bot holds it now, as maker, reviewer or integrator?
3. What did review say, about which commit?
4. What has been delivered where, as reported by whom and when?
5. Is it done, and have the person and the asking Bot(s) been told?

It is not human work: it never becomes a Board item and is never counted in Board totals or
columns. It does not schedule Bots, run CI, push or deploy, and it replaces nothing in the broker.

### 3.1 Relationship to existing records

| Record | Scope | What it is to a card |
|---|---|---|
| **Chat work result card** (`WorkResultCard`, from a `WorkLink`) | one dispatched Session | the card's **execution detail**. Each attached task's existing chat card appears under the card, unchanged. The card defines no delivery or landing facts of its own: it displays the work link's (`delivery_commit`, `landing`, `landing_commit`, `landing_target`, `types.go:474-487`) |
| Broker task | one dispatch | attaches to a card through `bot_work_id` on the task (§8.1) |
| Board v2 item | the person's work | optional `board_item_id` link when the person's message named it; the card never edits or moves the item. The linked item's independent verification keeps showing on the chat work result card as it does now |
| Agent proposal, Quick Session to-do | — | unrelated |
| Bot exchange (`message_bot`) | one Bot-to-Bot request | carries `bot_work_id` so the recipient's work attaches to the same card |

The split in one line: the existing chat card is **one Session's result**; the new record is **one
request's custody** — who holds it, what review said, what has been reported delivered.

## 4. Record shape

Tables (names reserved, not created), in the same store as `work_v2_proposals`
(`internal/adapters/store/work_v2.go:170`) and never joined into Board counts:
`bot_work_cards`, `bot_work_events`, `bot_work_verdicts`, `bot_work_checklist`.

`bot_work_cards`:

| Field | Meaning |
|---|---|
| `id` | lowercase UUID (§7.1, §6) |
| `project_id`, `project_path` | catalog Project and path snapshot |
| `title`, `description` | plain language; bounded (§10) |
| `authority` | `{bot, conversation_id, turn_id, message_id}` of the person's message; a merged card also lists the authorities it absorbed |
| `created_by` | the Bot that caused creation |
| `baton` | `{role, bot}`: `role` ∈ `maker`, `reviewer`, `integrator`; exactly one holder |
| `chain` | the Bots the request reached: the creator and every Bot it was handed to or messaged with this card |
| `board_item_id` | optional, only when the person's message named that item |
| `state` | `open`, `done`, `cancelled`, `merged` |
| `waiting` | empty, or the id of an `ask_person` question the holder is waiting on |
| `split_from`, `merged_into` | set by §6 |
| `version` | optimistic version; every write names the version it read |
| `created_at`, `updated_at`, `closed_at` | timestamps |

There is no phase machine. The stage shown — making, in review, integrating — is the baton's role.

`bot_work_checklist`: `{card_id, key, label, state, note, reported_by, reported_at}` with `state` ∈
`open`, `done`, `not_needed`. `note` is free text, normally a commit or version.

`bot_work_verdicts`: `{id, card_id, commit, verdict, text, reviewer_bot, at}` with `verdict` ∈
`pass`, `conditional_pass` (conditions in `text`, required), `reject` (reason in `text`, required).
`commit` is resolved to a full object id when written; a commit that does not resolve in the
Project is refused. A verdict is never edited; a later verdict is a new row.

`bot_work_events` is append-only, one row per accepted write, with actor, versions and time:
`card.created`, `baton.handed`, `verdict.recorded`, `checklist.added`, `checklist.marked`,
`waiting.set`, `waiting.cleared`, `card.split`, `card.merged`, `card.done`, `notified`,
`card.cancelled`, `card.reopened`.

## 5. Review, checklist and completion

**Verdict.** The reviewer holding the baton records a verdict on a commit. It is shown on the card
with the commit, reviewer and time, and it changes nothing else: it does not move the baton, block
a handoff, stop the card being marked done, or reach any push or deploy. A reject is expected to be
followed by a handoff back to the maker, and that is the Bots' practice, not a rule the record
enforces.

**Checklist.** A new card has two entries, `main` and `github`. The creating Bot may add more when
it creates the card or later (for example `dev`, `cloud`, "checked on the live site"); the holder
or the person may add one too. The holder marks an entry `done` with a note, or `not_needed` with a
reason. Every entry is shown as **reported by** that Bot at that time.

One entry has evidence that already exists, and the card reuses it instead of asking for a report:
when an attached task's work link says the Broker recorded its landing (`landing` is `landed` or
`incorporated`, `types.go:484-487`), the card shows that task's `landing_commit` and
`landing_target` beside the `main` entry, labelled as the Broker's record. It does not mark the
entry: with several tasks on one card, whether "it is on main" is the holder's call. Nothing else
is read: no fetch, no build stamp, no timer.

**Done.** The holder calls `bot_work_done` once every entry is `done` or `not_needed`; the daemon
refuses it otherwise (`bot_work_checklist_open`). The person can complete, cancel or reopen any
card at any time, with a reason.

**Notification.** `card.done` is written in one transaction with one outbox effect, keyed
`bot-work-done-<card id>-<cycle>` (`cycle` counts reopens), which delivers:

- one notification to the person (assumption: over the daemon's existing notification channel;
  which call carries it was not read for this page), and
- one host notice to each asking Bot — the card's authorities and `created_by`, excluding the Bot
  that marked it done — through the wake path dispatched work already uses (`WakeWorkCompletion`,
  `docs/project-assistants-broker.md:172`), marked as a report and counted against
  `bots.follow_up_chain` (`docs/project-assistants-broker.md:180`).

A retry or a daemon restart delivers neither twice; `notified` records the receipt.

## 6. Split and merge

Who may: the person, the card's authority or creating Bot, and its current holder. For a merge, the
caller must have that standing on **every** card named. All cards must be in one Project.

**Split** — `bot_work_split {card_id, expected_version, new: [{title, description}],
idempotency_key}`.

- Creates one new card per entry. Each inherits the source's authority, chain, Project and
  `board_item_id`, gets the default checklist, sets `split_from`, and starts with the baton on the
  caller as maker.
- New ids are derived from `(source id, idempotency_key, index)`, so a retry returns the same cards.
- The source keeps its own events and gains one `card.split` naming the new cards; each new card
  starts with `card.created`. No event is copied or moved.
- **Attached tasks stay on the source.** A task's `bot_work_id` is fixed when it is dispatched
  (§8.1). The new cards start with none and link back to the source; later dispatches name the card
  they belong to.

**Merge** — `bot_work_merge {into, from: [card ids], expected_versions, idempotency_key}`.

- Each `from` card becomes `merged` with `merged_into` set and is closed; nothing is deleted. Its
  events and verdicts stay on it and are shown on the surviving card under "merged from".
- The surviving card keeps its holder; its chain becomes the union, and it adds the absorbed
  cards' authorities, so everyone who asked is told when it completes.
- Checklist entries are united by key. The survivor's state wins; a key only the absorbed card had
  is added `open`.
- **Attached tasks are not rewritten.** A read of the surviving card includes the tasks of every
  card whose `merged_into` is it. If the survivor is itself merged later, those rows are repointed
  in the same transaction, so the lookup is always one step.
- Repeating a merge with the same key, or into the same card, is a no-op. A card already merged
  elsewhere is refused (`bot_work_already_merged`). There is no unmerge: split the survivor.

Splitting and merging produce no notification; the Board section and chat strip simply show the
new shape.

## 7. Creation and handoff

### 7.1 Automatic creation

The daemon, not the model, creates a card when a Bot's `dispatch_work` is accepted with a
**non-empty `claims` list** (the task will write files) in a turn that roots in a person:

- the turn's origin is `user`, or
- it is a `bot_request` whose exchange chain (`source_turn_id`,
  `internal/domain/projectassistant/collaboration.go:53`) reaches a `user` turn within
  `MaxCollaborationHops` (`collaboration.go:23`).

A dispatch with no claims, a routine, a host notice or a webhook turn creates nothing.

The card id is derived from `(machine, root bot, root conversation, root message id)`, so every
dispatch in every Bot that roots in that message attaches to the same card and a retry creates no
second one. A dispatch that names a `bot_work_id` attaches to that card (following `merged_into`)
after the daemon checks the Bot is in its chain. A Bot may also call `bot_work_create` in the same
circumstances, for work that starts before any writing dispatch; it derives the same id. Several
cards from one message come from a split (§6), not from a second derivation.

The title comes from the first writing dispatch's `title`. The daemon never parses the person's
message to infer scope.

### 7.2 Handoff

`bot_work_handoff {card_id, expected_version, to_bot, role, note, idempotency_key}` moves the
baton, appends `baton.handed` and queues **one** host notice for the new holder, keyed by card and
version. The note is where a Bot says what it is handing over — the commit to review, "this
replaces that", "landed together with that card". It is prose on the card instead of prose in a
chat, and that is all this version claims for it.

A Bot answering another Bot's request cannot call `message_bot`
(`collaboration.go:125`); the card tools are allowed in such a turn, so a reviewer reached that way
can still record its verdict and hand the baton on.

### 7.3 Authority

| Action | Person | Authority / creating Bot | Holder | Other chain Bot | Anyone else |
|---|---|---|---|---|---|
| Read | yes | yes | yes | yes | yes (machine read authority) |
| Edit title / description, add a checklist entry | yes | yes | yes | no | no |
| Hand off within the chain | yes | yes | yes | no | no |
| Hand off to a Bot outside the chain | yes | no | no | no | no |
| Record a verdict | no | no | only as `reviewer` | no | no |
| Mark a checklist entry | yes | no | yes | no | no |
| Set `waiting` | — | — | yes, with an `ask_person` id; cleared on the answer | — | — |
| Split / merge | yes | yes | yes | no | no |
| Mark done | yes | no | yes, checklist complete | no | no |
| Cancel / reopen | yes | no | no | no | no |

Sessions only read. A dispatched child's work attaches through its task; it cannot write the card.

## 8. Interfaces

### 8.1 Broker task field

`bot_work_id` on the broker task record, beside `work_id`
(`internal/app/orchestrator/draft.go:68`), set only by the Bot dispatch door from the resolved
card. It enters the assistant request hash only when non-empty, like `board_item_id`.
`AssistantWorkSnapshotOf` (`internal/app/orchestrator/project_assistant_actor.go:436`) copies it
into `WorkLink.BotWorkID` (`json:"bot_work_id,omitempty"`), next to the fields `523e059d7` added.

### 8.2 Bot tools

Added to `mcpTools()` (`internal/adapters/projectassistantacp/mcp.go:962`):
`bot_work_read`, `bot_work_create`, `bot_work_handoff`, `bot_work_verdict`, `bot_work_check`
(add or mark a checklist entry), `bot_work_wait_person`, `bot_work_split`, `bot_work_merge`,
`bot_work_done`. Every writing tool takes `idempotency_key` and `expected_version`; a stale version
answers `bot_work_version_conflict` with the current one.

`dispatch_work` and `message_bot` gain an optional `bot_work_id`. For `message_bot` the daemon
checks the sender is in the chain, adds the recipient to it, and names the card in the recipient's
turn.

### 8.3 HTTP routes

```text
GET  /v1/bot-work/cards?bot=&project=&state=&cursor=
GET  /v1/bot-work/cards/<id>
POST /v1/bot-work/cards/<id>/handoff|check|split|merge|complete|cancel|reopen   person
POST /v1/bot-work/agent/...                                                      the tools' backing routes
```

Person routes are device-authenticated; agent routes take the Bot host's credential, the same split
as `/v1/work/v2/` and `/v1/work/v2/agent/`.

## 9. Console

- **Board → "Bot work" section**, shown by default, with its own switch and a Bot filter. Rows:
  title, Project, holder Bot and role, and the lights. Never counted in Board totals or columns,
  and never placed in *Needs attention*.
- **Lights.** *build* and *review* first, then one per checklist entry.
  - *build*: the attached tasks' work-link state and delivery commit — the existing facts, not a
    new one.
  - *review*: the latest verdict — green pass, amber conditional pass, red reject, empty if none —
    with its commit.
  - *checklist entries*: empty open, green done, grey not needed, each with "reported by <Bot>,
    <time>" and the note. `main` also shows the Broker's landing commit and target where a work
    link has one, labelled as the Broker's.
- **Needs you.** Cards with `waiting` set are pinned at the top of the section; the question also
  appears in the Bot's chat and as a notification, as `ask_person` does now.
- **Bot chat.** The dispatched strip (`docs/bots.md:325-340`) and the chat work result cards are
  grouped under their card when `bot_work_id` is present, with the card's lights in one line.
  Without a card nothing changes.
- **Phone and Cloud.** The same section in the phone layout. Cloud carries new advertised read
  words `bot.work.cards` and `bot.work.card` and one send-capability word for the person's actions,
  following `work.v2.cards` (`internal/app/cloudops/ops_work_reads.go:28`); a machine that does not
  advertise them leaves the section hidden.

## 10. Capacity, concurrency, migration, privacy

Bounds to register in `internal/domain/capacity` before code, each refusing at its limit:

| Name | Proposed limit |
|---|---|
| `bot_work.open_cards` | 500 per machine |
| `bot_work.description_bytes` | 16 KiB |
| `bot_work.events_per_card` | 2,000 |
| `bot_work.verdicts_per_card` | 64 |
| `bot_work.checklist_entries` | 12 per card |
| `bot_work.note_bytes` | 1 KiB (checklist note, verdict text, handoff note) |
| `bot_work.chain_bots` | 16 |
| `bot_work.split_cards` | 8 per call |
| `bot_work.merged_cards` | 16 absorbed per card |
| `bot_work.page_rows` | 100 |
| `bot_work.receipts` | 4,096, like `board.receipts` (`internal/domain/capacity/register_board.go:13-18`) |

Every write is one transaction: read the card, check actor, chain and version, write the next
state, the event and any outbox effect. No Git or network I/O happens in a card write except
resolving a verdict's commit, which runs before the transaction. Two writers at once: one wins, the
other gets `bot_work_version_conflict`.

Migration: none. Old tasks have no `bot_work_id` and do not appear on cards.

Privacy: this repository is public. Docs, tests and fixtures use invented role names and synthetic
commits.

## 11. Options considered

| Decision | Option | Gain | Cost | Reversibility |
|---|---|---|---|---|
| Record type | **A. new tables (the person's)** | no exception to Board rules | a second list | schema irreversible once shipped |
| | B. Board item kind | one list | breaks the person-only rules | — |
| | C. extend the chat work result card only | nothing new | it is per Session; custody across Bots has nowhere to live | — |
| Delivery state | **A. self-reported checklist (the person's)** | small; works in any Project; flexible entries | a wrong report looks like a right one, apart from its label | reversible |
| | B. daemon-computed readings | cannot be misreported | readers, timers and per-Project definitions for a problem not yet shown to need them | — |
| Verdict | **A. recorded and shown (the person's)** | the rejection is visible beside the work | does not stop a rejected commit shipping | reversible |
| | B. guard on push and deploy | would have stopped the incident | hooks, deploy-script lines, overrides, range rules | — |
| Lifecycle | **A. state plus baton role** | nothing to get stuck in | no rule that review happens before integration | reversible |
| | B. phase machine with guards | order is enforced | rigid against split, merge and ad-hoc routes | — |
| Tasks on split / merge | **A. never rewrite a task; redirect on read** | task records and hashes untouched | a split card starts without its history | reversible |
| | B. move tasks between cards | tidy cards | rewrites broker records | — |

## 12. Delivery plan

Irreversible items are marked **(I)**.

1. **Record and custody (I: tables, tool names).** Domain rules, store, capacity rows; automatic
   creation at the Bot dispatch door; `bot_work_id` on tasks and work links; create, read, handoff.
   *Acceptance:* `go test` covers each row of §7.3 refusing the wrong actor;
   `TestEveryBoundIsRegistered` fails before the rows are added and passes after; two dispatches
   from one person message through two Bots yield one card with both tasks; an empty-claims
   dispatch yields none; a retried handoff queues one notice.
2. **Verdict, checklist, done, notification.** *Acceptance:* a reject is stored with its resolved
   commit and blocks neither a handoff nor done; done is refused while an entry is open; the person
   and each asking Bot receive exactly one notice across a daemon restart, and again after a
   reopen and second completion.
3. **Split and merge.** *Acceptance:* a retried split returns the same cards; tasks remain on the
   source; after a merge the survivor's read lists the absorbed card's tasks and verdicts, its done
   notice reaches both authorities, and no broker task record changed (compared byte for byte).
4. **Console, desktop and phone.** *Acceptance:* console tests for each light state; a browser
   check that Board counts and *Needs attention* are identical with the section on and off, and
   that existing chat work result cards render unchanged under a card.
5. **Cloud parity (I: wire words).** *Acceptance:* the phone reads a card through the relay with
   the words advertised and hides the section without them.

## 13. Deferred until needed

Each was specified in an earlier draft (this page's history) and removed on purpose. Enforcement
in particular was considered and deferred until the problem recurs.

- Daemon-computed destination readings (`bot_work_readings`) and their refresh timer.
- The hosted-console (`CloudGate`) callback as a required proof of a Cloud deploy.
- Detecting a rejected commit already published (`rejected_published`).
- The rejected-commit guard: the pre-push call, the dev-install preflight, the Cloud deploy line.
- Patch-id matching of cherry-picked rejected changes.
- Person override routes for a rejected commit.
- A conditional pass blocking anything.
- A phase machine with guarded transitions.
- `supersedes` / `carries` as structured relations; for now they are handoff notes.
- An integration-batch record for one landing serving many cards; for now, merge the cards or
  mark each.
- Proof that a cherry-picked or squashed landing contains the reviewed change.
- Detecting a revert after delivery.
- A rejected-range rule that excludes merged-in commits (needed only by the guard).
- Per-Project destination definitions; a checklist entry is the flexible form of one.
- A separate acceptance role; for now, a checklist entry such as "checked on the live site".

## 14. Risks and early signals

- **A wrong report reads as delivered.** Signal: the person finds a card marked `cloud` done that
  is not live, or a rejected commit shipped again. Response: the matching line in §13.
- **Bots do not keep cards current**, so the section goes stale. Signal: open cards whose attached
  tasks all landed long ago. Response: a reminder in the handoff or completion notice before any
  enforcement.
- **Split and merge make history hard to follow.** Signal: questions about which card a task
  belongs to. Response: show the lineage (`split_from`, `merged_into`) on the card.
- **Notification noise** from many small cards completing. Signal: the person muting them.
  Response: a digest.

## 15. Not building

- Cards for requests from another machine's Bot network.
- Scheduling or choosing reviewers.
- Running CI, builds, pushes or deploys from a card.
- Editing Board items from cards, or showing cards in Board counts or *Needs attention*.
- Root Sessions writing cards for the person.
- **A Board-item "hand to Bot" action.** Suggested by the owner of the Session that shipped the
  chat work result card; a possible later step, not part of this proposal. It would coexist
  without change here: the person's action on the item would be the card's authority in place of a
  chat message and would set `board_item_id`; the item stays the person's and is never moved by
  the Bot, while the card tracks the Bots' custody.

## 16. Open questions for the person

- **Q1. Names.** Commit `523e059d7` already calls the chat card for one dispatched Session a "Bot
  work card". Two things need two names. Recommended: keep the chat card as the **work result
  card／派工結果卡** (its component is already `WorkResultCard`), and call the new record a **Bot
  request card／Bot 交辦卡**. Is that right, or do you prefer other names?
Q2 and Q3 ask about defaults this page already wrote down (§5: the holder may; §6: any cards in one
Project may), so either answer is a small change.

- **Q2.** May the Bot holding a card mark a checklist entry "not needed" itself, with its reason
  shown — or should dropping an entry be yours only?
- **Q3.** May cards that came from different messages of yours be merged into one, or only cards
  that were split from the same message?
