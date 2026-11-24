# Bot request cards

Status: design approved by the person on 2026-11-24; phases 1 and 2 (backend) are being implemented.

No schema, wire word or tool name in this page exists yet unless it is cited with a `file:line` as
current behaviour. The names it reserves are listed once, in §4.1.

The person's summary in Traditional Chinese is [`bot-request-cards.zh-TW.md`](bot-request-cards.zh-TW.md).

Revision 3 is a simplification, on the person's instruction after reading the first draft: do not
over-design, stay flexible, and change things only when a real problem appears. Deployment state is
reported by the Bots, nothing is enforced, and cards can be split and merged like to-dos. What the
earlier drafts specified and this one removes is listed in §13, one line each.

Revision 4 records three decisions of 2026-11-24 and adds nothing else: the card also serves tasks
that are not development (§3.3), a card is for a task with a longer flow (§3.2), and the Bot
decides when to open one (§7.1). It removes the automatic creation revision 3 had, and every place
where the record assumed a commit, a repository landing, a verdict or a dispatch.

Revision 5 is the approved design. It records the person's last four decisions of 2026-11-24 and
changes nothing else: the two names (§2, §3.1), the holder marking an entry "not needed" (§5),
merging cards that came from different messages (§6), and mockups before the console phase (§12).
The reserved identifiers follow the new name (§4.1), and this page moved from `bot-work-cards.md`.
There are no open questions left.

Revision 6 records the person's choice of 2026-11-24 to implement with the coordinating Bot's
additions: Design canvas direction A for the console (§9.1), no approve or reject buttons on a card
in v1 (§9.1), a "last updated" time on every card (§4), a batch tick across cards (§5), a handoff
that lets the new holder start work for that card (§7.2), and the checklist as an unordered to-do
list (§5).

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

The same is true of work that is not development — a Bot drafting a post, waiting for the person to
read it, then publishing it. That work does not go on the Board, so today it has no record at all
outside the chat.

What exists today:

- **The work result card／派工結果卡** (`web/console/src/project-assistants/WorkResultCard.tsx`,
  shipped in `523e059d7`, whose subject calls it a "Bot work card") shows **one dispatched
  Session's** result in the Bot's chat: its delivery commit,
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
- The default checklist **for development** is **`main` and `GitHub`**; the creating Bot adds
  further entries.

Decided on 2026-11-24:

- **The card also serves non-development tasks.** They do not become Board items; they use this
  same record. There is one record type, and development is one default checklist among others
  (§3.3).
- **A card is for a task with a longer flow** — in development a feature or a bug fix, in
  publishing draft → review → publish. A question, a lookup or anything finished in one reply gets
  none (§3.2).
- **The Bot decides, by judgment, and asks the person when unsure** (§7.1). The person adjusts the
  habit through the Bot's standing instructions and memory.
- **Names.** The existing chat card for one dispatched Session is the **work result card／
  派工結果卡**. The new record in this page is the **Bot request card／Bot 交辦卡**. The Board section
  that lists Bot request cards keeps its label, **"Bot work"／「Bot 工作」**.
- **The holding Bot may mark a checklist entry "not needed" itself**, with its reason shown (§5).
- **Cards that came from different messages of the person may be merged**, within one Project
  (§6).
- **Design mockups come before the console phase** (§12).

## 3. What a Bot request card is

One Bot request card — "card" from here on, where no other card is meant — is **one request from
the person, across Bots**. It answers:

1. Which message of the person authorized it?
2. Which Bot holds it now, as maker, reviewer or integrator?
3. If it was reviewed, what did review say, and about what?
4. Which steps of its checklist are finished, as reported by whom and when, with what evidence?
5. Is it done, and have the person and the asking Bot(s) been told?

It is not human work: it never becomes a Board item and is never counted in Board totals or
columns. It does not schedule Bots, run CI, push, deploy or publish, and it replaces nothing in the
broker.

**Nothing in the record is required to be about code.** A card can be created, worked and completed
with no dispatch, no commit, no verdict and no repository state: those parts are optional and a
card without them simply does not show them.

### 3.1 Relationship to existing records

| Record | Scope | What it is to a card |
|---|---|---|
| **Work result card** (`WorkResultCard`, from a `WorkLink`) | one dispatched Session | the card's **execution detail**. Each attached task's work result card appears under the Bot request card, unchanged. The card defines no delivery or landing facts of its own: it displays the work link's (`delivery_commit`, `landing`, `landing_commit`, `landing_target`, `types.go:474-487`) |
| Broker task | one dispatch | attaches to a card through `bot_request_id` on the task (§8.1); a card may have none |
| Board v2 item | the person's work | optional `board_item_id` link when the person's message named it; the card never edits or moves the item. The linked item's independent verification keeps showing on the work result card as it does now |
| Agent proposal, Quick Session to-do | — | unrelated |
| Bot exchange (`message_bot`) | one Bot-to-Bot request | carries `bot_request_id` so the recipient's work attaches to the same card |

The split in one line: the work result card is **one Session's result**; the Bot request card is
**one request's custody** — who holds it, what review said, which steps have been reported finished.

### 3.2 Which tasks get a card

A task with a **longer flow**: several steps, usually over more than one turn, often with a wait
for another Bot or for the person in the middle. The person's examples: in development, a feature
or a bug fix; in publishing, draft → review → publish.

No card for a question, a lookup, or anything finished in one reply. The test is the flow, not the
kind of work and not whether files are written: a one-line fix answered in the same reply needs no
card, and a post that waits a day for the person's review does.

### 3.3 Non-development tasks

Non-development tasks are never Board items. They use this same card, shown in the same "Bot work"
section (§9), with the same holder, checklist, waiting mark and completion notice. What differs is
only the checklist the card starts with:

| Kind of work | Default checklist |
|---|---|
| `development` | `main`, `GitHub` |
| `publishing` | `draft`, `reviewed by the person`, `published` |
| anything else | none: the creating Bot names the entries |

`kind` is a short free-form label on the card. It selects a default checklist when the daemon knows
one and does nothing else: no rule, permission or display depends on it. A Bot may always pass its
own entries instead of the default.

**Worked example: publish a post on a social channel.**

1. The person asks a Bot to write and publish a post about this week's release. This will take a
   draft, the person's read and a publish step, so the Bot opens a card: kind `publishing`, three
   open entries, the Bot as holder.
2. The Bot writes the draft, marks `draft` done with a note saying where the draft is, and asks the
   person to read it with the existing `ask_person` tool
   (`internal/adapters/projectassistantacp/mcp.go:975`). The card's `waiting` is set, so it sits
   under **Waiting for you** at the top of the section (§9) until the person answers.
3. The person answers in the Bot's chat. The Bot marks `reviewed by the person` done, noting the
   answer, and revises first if the answer asked for changes.
4. The Bot publishes the post with whatever means it already has, and marks `published` done with
   the post's link as the note. **The link is the evidence**, shown as "reported by <Bot>, <time>".
5. Every entry is done, so the Bot marks the card done and the person is notified (§5).

No task was dispatched, no commit exists and no verdict was recorded; the card shows no build or
review light. The person's review here is an answer to a question, not a verdict row — a verdict is
for a reviewer Bot.

Not designed, on purpose: channel integrations, an approval mechanism beyond the question the Bot
already can ask, and templates beyond one default checklist per kind of work.

## 4. Record shape

Tables (names reserved in §4.1, not created), in the same store as `work_v2_proposals`
(`internal/adapters/store/work_v2.go:170`) and never joined into Board counts:
`bot_request_cards`, `bot_request_events`, `bot_request_verdicts`, `bot_request_checklist`.

`bot_request_cards`:

| Field | Meaning |
|---|---|
| `id` | lowercase UUID (§7.1, §6) |
| `project_id`, `project_path` | the Bot's Project and path snapshot. A Project need not be a repository (`ProjectScope`, `internal/domain/projectassistant/types.go:44-52`) and the card reads nothing from it |
| `kind` | optional short label, e.g. `development`, `publishing`; selects a default checklist (§3.3) |
| `title`, `description` | plain language; bounded (§10) |
| `authority` | `{bot, conversation_id, turn_id, message_id}` of the person's message; a merged card also lists the authorities it absorbed |
| `created_by` | the Bot that caused creation |
| `baton` | `{role, bot}`: `role` ∈ `maker`, `reviewer`, `integrator`; exactly one holder. A card that never changes hands stays with its creator as `maker` |
| `chain` | the Bots the request reached: the creator and every Bot it was handed to or messaged with this card |
| `board_item_id` | optional, only when the person's message named that item |
| `state` | `open`, `done`, `cancelled`, `merged` |
| `waiting` | empty, or the id of an `ask_person` question the holder is waiting on |
| `split_from`, `merged_into` | set by §6 |
| `version` | optimistic version; every write names the version it read |
| `created_at`, `updated_at`, `closed_at` | timestamps. `updated_at` is the time of the card's last event and every read shows it as "last updated", so a stalled handoff is visible. It is a read-model field, not a timer |

There is no phase machine. The stage shown — making, in review, integrating — is the baton's role.

`bot_request_checklist`: `{card_id, key, label, state, note, reported_by, reported_at}` with `state` ∈
`open`, `done`, `not_needed`. `label` is free-form text chosen by whoever adds the entry; `key` is
derived from it and means nothing to the daemon. `note` is free text and is the entry's evidence: a
commit, a version, a URL, or the name of a published artifact. The daemon does not check it; the
console shows a URL in a note as a link.

`bot_request_verdicts`: `{id, card_id, commit, verdict, text, reviewer_bot, at}` with `verdict` ∈
`pass`, `conditional_pass` (conditions in `text`, required), `reject` (reason in `text`, required).
A card needs no verdict. `commit` is optional: a verdict on a draft or a document names what it
reviewed in `text`. When a commit is given it is resolved to a full object id, and one that does
not resolve in the Project is refused. A verdict is never edited; a later verdict is a new row.

`bot_request_events` is append-only, one row per accepted write, with actor, versions and time:
`card.created`, `baton.handed`, `verdict.recorded`, `checklist.added`, `checklist.marked`,
`waiting.set`, `waiting.cleared`, `card.split`, `card.merged`, `card.done`, `notified`,
`card.cancelled`, `card.reopened`.

### 4.1 Reserved names

Every identifier this design reserves, stated once. None exists yet. Tables, tool names and wire
words are irreversible once shipped (§12).

| What | Reserved names |
|---|---|
| Person-facing name | Bot request card／Bot 交辦卡 |
| Board section label | Bot work／Bot 工作 |
| Tables | `bot_request_cards`, `bot_request_events`, `bot_request_verdicts`, `bot_request_checklist` |
| Field on the broker task, the work link, `dispatch_work` and `message_bot` | `bot_request_id` (Go: `WorkLink.BotRequestID`) |
| Bot tools | `bot_request_read`, `bot_request_create`, `bot_request_handoff`, `bot_request_verdict`, `bot_request_check`, `bot_request_wait_person`, `bot_request_split`, `bot_request_merge`, `bot_request_done` |
| Refusal codes | `bot_request_checklist_open`, `bot_request_checklist_empty`, `bot_request_already_merged`, `bot_request_version_conflict`, `bot_request_handoff_scope` |
| Turn origin | `bot_request_handoff` |
| HTTP routes | `/v1/bot-request/cards`, `/v1/bot-request/cards/<id>`, `/v1/bot-request/agent/...` |
| Cloud wire words | `bot.request.cards`, `bot.request.card` (read); the send-capability word is not named yet |
| Outbox effect key | `bot-request-done-<card id>-<cycle>` |
| Capacity names | `botrequest.open_cards`, `.description_bytes`, `.events_per_card`, `.verdicts_per_card`, `.checklist_entries`, `.note_bytes`, `.label_bytes`, `.chain_bots`, `.split_cards`, `.merged_cards`, `.page_rows`, `.receipts`, `.batch_cards` (§10). The register's names allow no underscore in the area, so the area is `botrequest`, not `bot_request` |

Unchanged by this design: the existing `WorkResultCard` component, `WorkLink`, `work_id` and
`board_item_id`. Event names (`card.created`, `baton.handed`, …) are values inside
`bot_request_events`, not separate identifiers.

## 5. Review, checklist and completion

**Verdict.** Optional. A reviewer Bot holding the baton records a verdict, on a commit when there is
one. It is shown on the card with the reviewer, the time and the commit if any, and it changes
nothing else: it does not move the baton, block
a handoff, stop the card being marked done, or reach any push or deploy. A reject is expected to be
followed by a handoff back to the maker, and that is the Bots' practice, not a rule the record
enforces.

**Checklist.** The checklist is a to-do list: unordered, and the card's progress ("n/m ✓") is
derived from it. A request with several goals has several entries. Goals that are unrelated, or
that different Bots will carry, are separate cards (split, §6), not entries on one.

A new card starts with the default entries for its kind (§3.3), or with the entries
the creating Bot names; it has at least one. The creating Bot may add more when it creates the card
or later (for example `dev`, `cloud`, "checked on the live site"); the holder or the person may add
one too. The holder marks an entry `done` with a note, or `not_needed` with a
reason. **The holding Bot may mark an entry `not_needed` itself** (decided 2026-11-24): it needs
nobody's approval, the reason is required, and it is shown on the card with the Bot and the time,
so the person can see what was dropped and why. Every entry is shown as **reported by** that Bot at
that time.

One entry has evidence that already exists, and the card reuses it instead of asking for a report:
on a card that has a `main` entry, when an attached task's work link says the Broker recorded its landing (`landing` is `landed` or
`incorporated`, `types.go:484-487`), the card shows that task's `landing_commit` and
`landing_target` beside the `main` entry, labelled as the Broker's record. It does not mark the
entry: with several tasks on one card, whether "it is on main" is the holder's call. Nothing else
is read: no fetch, no build stamp, no timer.

**Batch tick.** `bot_request_check` may name several cards in one call with the same entries — for
example seven cards, `main` and `GitHub` done, note "<commit>". Each card is its own transaction,
all-or-nothing on that card; the answer lists one result per card (`ok`, `unchanged`, or a typed
refusal). Marking an entry with the state and note it already has is `unchanged`, so a retried
batch writes nothing. A call names at most `botrequest.batch_cards` cards (§10).

**Done.** The holder calls `bot_request_done` once every entry is `done` or `not_needed`; the daemon
refuses it otherwise (`bot_request_checklist_open`). The person can complete, cancel or reopen any
card at any time, with a reason.

**Notification.** `card.done` is written in one transaction with one outbox effect, keyed
`bot-request-done-<card id>-<cycle>` (`cycle` counts reopens), which delivers:

- one notification to the person (assumption: over the daemon's existing notification channel;
  which call carries it was not read for this page), and
- one host notice to each asking Bot — the card's authorities and `created_by`, excluding the Bot
  that marked it done — through the wake path dispatched work already uses (`WakeWorkCompletion`,
  `docs/project-assistants-broker.md:172`), marked as a report and counted against
  `bots.follow_up_chain` (`docs/project-assistants-broker.md:180`).

A retry or a daemon restart delivers neither twice; `notified` records the receipt.

## 6. Split and merge

Who may: the person, the card's authority or creating Bot, and its current holder. For a merge, the
caller must have that standing on **every** card named. All cards must be in one Project. **Cards
that came from different messages of the person may be merged** (decided 2026-11-24): a merge does
not require a common origin or an earlier split, only the one Project.

**Split** — `bot_request_split {card_id, expected_version, new: [{title, description}],
idempotency_key}`.

- Creates one new card per entry. Each inherits the source's authority, chain, Project, `kind` and
  `board_item_id`, gets a copy of the source's checklist entries with every one `open`, sets
  `split_from`, and starts with the baton on the caller as maker.
- New ids are derived from `(source id, idempotency_key, index)`, so a retry returns the same cards.
- The source keeps its own events and gains one `card.split` naming the new cards; each new card
  starts with `card.created`. No event is copied or moved.
- **Attached tasks stay on the source.** A task's `bot_request_id` is fixed when it is dispatched
  (§8.1). The new cards start with none and link back to the source; later dispatches name the card
  they belong to.

**Merge** — `bot_request_merge {into, from: [card ids], expected_versions, idempotency_key}`.

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
  elsewhere is refused (`bot_request_already_merged`). There is no unmerge: split the survivor.

Splitting and merging produce no notification; the Board section and chat strip simply show the
new shape.

## 7. Creation and handoff

### 7.1 Creation: the Bot decides

**The Bot opens the card itself**, with `bot_request_create {title, description, kind, checklist,
idempotency_key}`, when it takes on a task with a longer flow (§3.2). This is the Bot's judgment,
and it covers tasks with no dispatch at all. The daemon never parses the person's message to infer
scope and never opens a card on its own.

**When the Bot is unsure whether a task deserves a card, it asks the person**: an ordinary short
question in chat, or `ask_person` (`mcp.go:975`) when it cannot continue without the answer. The
person shapes the habit in words — "always open a card for release notes", "never for typo fixes" —
kept in the Bot's standing instructions (`docs/bots.md:90`) or memory (`docs/bots.md:71-75`). There
is no setting, rule table or per-kind switch for it.

**A card needs a person's message behind it.** `bot_request_create` is accepted in a turn that roots
in a person:

- the turn's origin is `user`, or
- it is a `bot_request` whose exchange chain (`source_turn_id`,
  `internal/domain/projectassistant/collaboration.go:53`) reaches a `user` turn within
  `MaxCollaborationHops` (`collaboration.go:23`).

A routine, a host notice or a webhook turn cannot open one.

**Idempotency: one card per request of the person, unless split.** The card id is derived from
`(machine, root bot, root conversation, root message id)`. A second `bot_request_create` that roots in
the same message — a retry, a later turn, or another Bot the request was passed to — returns the
existing card unchanged and adds that Bot to its chain. Several cards from one message come from a
split (§6), never from a second create.

**Work attaches by naming the card.** `dispatch_work` and `message_bot` take an optional
`bot_request_id`; the daemon checks the Bot is in the card's chain and follows `merged_into`. A
dispatch that names no card attaches to none.

**The automatic creation of revision 3 is dropped**, not kept as a safety net. It only fired on a
writing dispatch, so it could not cover a task without one, and keeping it would leave two
creators: the daemon would open cards for dispatches the Bot had judged too small for one,
overriding the judgment the person gave to the Bot. If Bots are seen forgetting cards, §14 names
the first response.

### 7.2 Handoff

`bot_request_handoff {card_id, expected_version, to_bot, role, note, idempotency_key}` moves the
baton, appends `baton.handed` and queues **one** host notice for the new holder, keyed by card and
version. The note is where a Bot says what it is handing over — the commit to review, "this
replaces that", "landed together with that card". It is prose on the card instead of prose in a
chat, and that is all this version claims for it.

A Bot answering another Bot's request cannot call `message_bot`
(`collaboration.go:125`) and has no dispatch (`native_work` is disabled for that turn); the card
tools are allowed in such a turn, so a reviewer reached that way can still record its verdict and
hand the baton on.

**The handoff notice can start work (decided 2026-11-24).** The host notice that wakes the new
holder is a turn of origin `bot_request_handoff` that carries the card id and the handoff event's
version. Its authority roots in the card's authority — the person's original message — not in the
Bot that handed it over. In that turn, and only there, `dispatch_work` and `message_bot` are
accepted when all three hold:

1. the card's state is `open` and it has an authority from a person's message (§7.1);
2. the card's holder is the Bot running the turn (`baton.bot`), at the version the notice named or
   a later one in which that Bot still holds it; and
3. the call names that card in `bot_request_id`.

A call that names no card, or another card, is refused `bot_request_handoff_scope`. The turn
grants nothing outside that card: no Board writes, no settings, no other card's writes, and the
usual hop and follow-up bounds (`MaxCollaborationHops`, `bots.follow_up_chain`) still apply.

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

Creating a card is not in the table because no card exists yet: any Bot may, in a turn that roots
in a person (§7.1), and it becomes the card's creating Bot and first holder.

Sessions only read. A dispatched child's work attaches through its task; it cannot write the card.

## 8. Interfaces

### 8.1 Broker task field

`bot_request_id` on the broker task record, beside `work_id`
(`internal/app/orchestrator/draft.go:68`), set only by the Bot dispatch door from the resolved
card. It enters the assistant request hash only when non-empty, like `board_item_id`.
`AssistantWorkSnapshotOf` (`internal/app/orchestrator/project_assistant_actor.go:436`) copies it
into `WorkLink.BotRequestID` (`json:"bot_request_id,omitempty"`), next to the fields `523e059d7` added.

### 8.2 Bot tools

Added to `mcpTools()` (`internal/adapters/projectassistantacp/mcp.go:962`):
`bot_request_read`, `bot_request_create`, `bot_request_handoff`, `bot_request_verdict`, `bot_request_check`
(add or mark a checklist entry), `bot_request_wait_person`, `bot_request_split`, `bot_request_merge`,
`bot_request_done`. Every writing tool takes `idempotency_key` and `expected_version`; a stale version
answers `bot_request_version_conflict` with the current one.

`bot_request_create` takes `title`, `description`, optional `kind` and optional `checklist` (a list of
labels); with neither a known kind nor a checklist it is refused (`bot_request_checklist_empty`).

`dispatch_work` and `message_bot` gain an optional `bot_request_id` (§7.1). For `message_bot` the
daemon checks the sender is in the chain, adds the recipient to it, and names the card in the
recipient's turn.

### 8.3 HTTP routes

```text
GET  /v1/bot-request/cards?bot=&project=&state=&cursor=
GET  /v1/bot-request/cards/<id>
POST /v1/bot-request/cards/<id>/handoff|check|split|merge|complete|cancel|reopen   person
POST /v1/bot-request/agent/...                                                      the tools' backing routes
```

Person routes are device-authenticated; agent routes take the Bot host's credential, the same split
as `/v1/work/v2/` and `/v1/work/v2/agent/`.

## 9. Console

### 9.1 Chosen design: direction A

Design canvas direction A, chosen by the person on 2026-11-24.

- **Bot chat.** The request card sits in the Bot's chat above its existing work result cards.
- **Board → "Bot work"**, with a show/hide switch, shown by default. Each row shows the holder
  Bot's real avatar and name (no role category as the primary label), the title, a progress-state
  label — 進行中 (in progress), 等你 (waiting for you), 已完成 (done) — "n/m ✓" checklist progress,
  and "last updated".
- **Opening a card** shows the holder, the handoff history one line per handoff, the checklist (✓
  done, an empty box open, struck through with its reason when not needed; each with its note and
  "reported by <Bot>, <time>"), the linked Board item, and the actions 取消 (cancel), 交給其他 Bot
  (hand to another Bot) and 完成 (complete).
- Completed entries are ticks; dots or lights express progress state only.
- **No approve or reject buttons on the card in v1.** "等你" opens the Bot's chat, where the person
  replies.

The list below is the earlier outline; where it differs, §9.1 wins.

- **Board → "Bot work" section**, shown by default, with its own switch and a Bot filter. Rows:
  title, Project, holder Bot and role, and the lights. Never counted in Board totals or columns,
  and never placed in *Needs attention*.
- **Lights.** One per checklist entry, preceded by *build* and *review* only on a card that has
  them.
  - *build*: the attached tasks' work-link state and delivery commit — the existing facts, not a
    new one. Absent on a card with no attached task.
  - *review*: the latest verdict — green pass, amber conditional pass, red reject — with its commit
    if it has one. Absent on a card with no verdict.
  - *checklist entries*: empty open, green done, grey not needed, each with "reported by <Bot>,
    <time>" and the note. `main` also shows the Broker's landing commit and target where a work
    link has one, labelled as the Broker's.
- **Waiting for you.** Cards with `waiting` set are pinned at the top of the section; the question
  also appears in the Bot's chat and as a notification, as `ask_person` does now.
- **Bot chat.** The dispatched strip (`docs/bots.md:325-340`) and the work result cards are
  grouped under their Bot request card when `bot_request_id` is present, with the card's lights in one line.
  Without a card nothing changes.
- **Phone and Cloud.** The same section in the phone layout. Cloud carries new advertised read
  words `bot.request.cards` and `bot.request.card` and one send-capability word for the person's actions,
  following `work.v2.cards` (`internal/app/cloudops/ops_work_reads.go:28`); a machine that does not
  advertise them leaves the section hidden.

## 10. Capacity, concurrency, migration, privacy

Bounds to register in `internal/domain/capacity` before code, each refusing at its limit:

| Name | Proposed limit |
|---|---|
| `botrequest.open_cards` | 500 per machine |
| `botrequest.description_bytes` | 16 KiB |
| `botrequest.events_per_card` | 2,000 |
| `botrequest.verdicts_per_card` | 64 |
| `botrequest.checklist_entries` | 12 per card |
| `botrequest.note_bytes` | 1 KiB (checklist note, verdict text, handoff note) |
| `botrequest.label_bytes` | 120 (title, kind, checklist label) |
| `botrequest.chain_bots` | 16 |
| `botrequest.split_cards` | 8 per call |
| `botrequest.merged_cards` | 16 absorbed per card |
| `botrequest.page_rows` | 100 |
| `botrequest.receipts` | 4,096, like `board.receipts` (`internal/domain/capacity/register_board.go:13-18`) |
| `botrequest.batch_cards` | 16 cards per batch tick |

Every write is one transaction: read the card, check actor, chain and version, write the next
state, the event and any outbox effect. No Git or network I/O happens in a card write except
resolving a verdict's commit when it names one, which runs before the transaction. Two writers at once: one wins, the
other gets `bot_request_version_conflict`.

Migration: none. Old tasks have no `bot_request_id` and do not appear on cards.

Privacy: this repository is public. Docs, tests and fixtures use invented role names and synthetic
commits.

## 11. Options considered

| Decision | Option | Gain | Cost | Reversibility |
|---|---|---|---|---|
| Record type | **A. new tables (the person's)** | no exception to Board rules | a second list | schema irreversible once shipped |
| | B. Board item kind | one list | breaks the person-only rules | — |
| | C. extend the work result card only | nothing new | it is per Session; custody across Bots has nowhere to live | — |
| Non-development tasks | **A. the same card, another default checklist (the person's)** | one record, one place to look | role and light names read as development words on a publishing card | reversible |
| | B. a second record type per kind of work | each fits its work exactly | two lists and two sets of tools for the same custody question | — |
| Who opens a card | **A. the Bot, by judgment; asks when unsure (the person's)** | covers work with no dispatch; small tasks get no card | a Bot can forget, and then nothing records it | reversible |
| | B. the daemon, on the first writing dispatch | cannot be forgotten for dispatched work | misses every task without a dispatch; opens cards the Bot judged unnecessary | — |
| | C. both, the daemon as a safety net | fewer missing cards | two creators to reconcile, and the daemon overrides the Bot's judgment | — |
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

**Gate: mockups before the console (decided 2026-11-24).** Met: the person chose Design canvas
direction A on 2026-11-24 (§9.1). Phases 1 to 3 touch no console surface.

**Suggested order.** Phases 1 and 2 first, together with a minimal list view (phase 4a), so the
record is useful as soon as Bots can write it. Split and merge (phase 3), the rest of the console
(phase 4b) and Cloud (phase 5) come later. The numbers below are identifiers, not the order. The
minimal list view is a console surface, so the gate applies to it as well.

1. **Record and custody (I: tables, tool names).** Domain rules, store, capacity rows; create,
   read, handoff; `bot_request_id` on tasks and work links.
   *Acceptance:* `go test` covers each row of §7.3 refusing the wrong actor;
   `TestEveryBoundIsRegistered` fails before the rows are added and passes after; two creates that
   root in one person message through two Bots yield one card, and two dispatches naming it both
   attach; a dispatch that names no card creates none; a create from a routine turn is refused; a
   retried handoff queues one notice.
2. **Verdict, checklist, done, notification.** *Acceptance:* a `publishing` card with no task, no
   commit and no verdict goes from created to done with a URL as its last note; a verdict without a
   commit is stored; a reject is stored with its resolved
   commit and blocks neither a handoff nor done; done is refused while an entry is open; the person
   and each asking Bot receive exactly one notice across a daemon restart, and again after a
   reopen and second completion.
3. **Split and merge.** *Acceptance:* a retried split returns the same cards; tasks remain on the
   source; after a merge the survivor's read lists the absorbed card's tasks and verdicts, its done
   notice reaches both authorities, and no broker task record changed (compared byte for byte).
4. **Console, desktop and phone.** Gated on the person's choice from the mockups. In two parts:
   - **4a. Minimal list view.** The "Bot work" section as a read-only list in the chosen design:
     title, holder and lights, with *Waiting for you* on top.
     *Acceptance:* console tests for each light state, including a card with no build or review
     light; a browser check that Board counts and *Needs attention* are identical with the section
     on and off.
   - **4b. The rest of §9.** The person's actions, the Bot filter, split and merge lineage,
     grouping in the Bot chat, and the phone layout.
     *Acceptance:* a browser check that existing work result cards render unchanged under a Bot
     request card, on desktop and at phone width.
5. **Cloud parity (I: wire words).** *Acceptance:* the phone reads a card through the relay with
   the words advertised and hides the section without them.

**Built so far (2026-11-24).** The record, store and person routes arrive at API 118 (moved from
117, which Bot settings saves took on `main`). The Bot tools `bot_request_read`, `_create`,
`_handoff`, `_check` (one card, or `cards` for a batch), `_verdict`, `_wait_person` and `_done` are
in `mcpBotRequestTools()` (`internal/adapters/projectassistantacp/mcp.go`) and run in
`internal/adapters/projectassistanthost/bot_request.go` against the daemon store. A card refusal
reaches the Bot as `{code, field, version}`. Creation finds the person's message by walking a
`bot_request` turn's exchange chain back to a `user` turn (at most `MaxCollaborationHops`); a turn
from a host notice, a routine run, a room, the network, a compaction or a Bot reply is refused
`bot_request_no_person_authority`. The card tools are offered in Bot-to-Bot turns too (§7.2).

Built on 2026-11-24 (second delivery, same API level 118):

- **`bot_request_id` on tasks and links (§8.1): built.** `Record.BotRequestID`
  (`internal/app/orchestrator/record.go`) is set only by `DispatchAssistant`, after
  `botRequestCard` (`internal/app/orchestrator/bot_request_effects.go`) followed `merged_into` (at most
  `botrequest.MaxMergeHops`) and found the card open with the dispatching Bot in its chain; a task file
  that names `bot_request_id` is refused (`draft.go`). It enters `AssistantRequestHash` and the host's
  payload hash only when non-empty, and `WorkLink.BotRequestID` is `omitempty`, so old receipts keep
  their hash. `dispatch_work` and `message_bot` take `bot_request_id`; `message_bot` adds the recipient
  to the card's chain first (`Store.JoinBotRequestChain`).
- **Handoff notice delivery: built.** The `bot_request.handoff` row is drained by
  `runBotRequestHandoff` into one host-notice turn for the holder through
  `Engine.WakeBotRequest` (`internal/app/projectassistant/bot_request_notice.go`), keyed
  `pa.BotRequestHandoffKey(card, version)`: the runtime receipt for that key makes a retry, a restart
  or a second drain queue nothing. A full Bot is retried until `pa.CompletionWakeRetryWindow`, as work
  completions are; a handoff that was superseded or a closed card ends the row.
- **Handoff can start work: built for the handoff-notice turn.** In a turn whose key names a card
  (`pa.BotRequestHandoffCard`), `dispatch_work` and `message_bot` are accepted only when they name that
  card and `br.HandoffDispatch` holds (open, person authority, this Bot holds it); anything else is
  `bot_request_handoff_scope`. Other turns keep today's refusals.
- **Completion delivery: built.** A completion queues two rows under the cycle's key:
  `bot_request.done` pushes the person once (not idempotent, so an interrupted push is recorded unknown
  and never resent) and `bot_request.done.notice` wakes each asking Bot once (idempotent through the
  runtime receipt). A reopen starts a new cycle and so a new pair.
- **Contract types: built.** `api/v1/bot-request.schema.json` generates `BotRequestView`,
  `BotRequestPage`, `BotRequestEntry`, `BotRequestEvent`, `BotRequestVerdict`, `BotRequestBaton`,
  `BotRequestAuthority`, `BotRequestPersonWrite`, `BotRequestMark`, `BotRequestWriteResult` and
  `BotRequestRefusal` for Go and TypeScript; the HTTP handlers write those shapes.

Built on 2026-11-24 (third delivery, console, phase 4, local daemon only):

- **Board → "Bot work": built.** `BotWorkSection` (`web/console/src/pages/work/BotWorkSection.tsx`)
  sits above the Board's columns and outside every count. A row shows the holder Bot's avatar and
  name, the title, the state in words with a dot, "n/m ✓" and the last update; cards waiting for the
  person come first (`sortBotRequests`). Its switch is shown by default and remembered per browser
  (`clawdline.board.bot-work`); the existing Board switches are not persisted, so this one follows
  detail mode's storage instead. A read that fails says so and offers a retry; it is never drawn as
  an empty list.
- **Card detail: built.** `BotRequestDetail` (`web/console/src/project-assistants/BotRequestCard.tsx`)
  is a dialog on desktop and a bottom sheet at phone width. It rereads the card for its history and
  after every write. Cancel asks first, hand to another Bot takes the Bot, what it should do and a
  note, and complete asks for a reason while an entry is open. A refusal is shown in plain words by
  its code; a version conflict or an unanswered write rereads the card. A waiting card has one
  extra action, which opens the holder's chat. There is no approve or reject.
- **Bot chat: built.** A card's one line (title, state, "n/m ✓") sits above the first work result
  card that carries its `bot_request_id`; an open card with no work result in the chat is listed
  at the chat's end, above the dispatched strip. A chat whose Bot has no card renders as before.
- **Where the section is hidden.** The console reads cards from the local daemon only. A daemon
  without the routes answers 404 and the section is not rendered. **Cloud parity (phase 5) is not
  built:** a machine reached through Cloud has no wire words for cards, so the Board section and
  the chat line are hidden there, on the phone as well.
- **Known gaps in what the routes tell the console.** A `baton.handed` event records who handed
  the card on and the note, not the receiver; the detail names the receiver only when the chain
  settles it (every Bot after the first arrived by exactly one handoff) and otherwise says only who
  handed it on. The card read does not list its attached tasks, so the detail lists dispatched
  Sessions only when opened from a chat that has loaded those work links, never from the Board.
  The generated `ProjectAssistantWorkLink` does not name `bot_request_id` yet although the daemon
  sends it; the console reads it as an optional string.
- **Checks.** `bot-request.test.ts` (read model, routes and refusals), `bot-request-components.test.mjs`
  (each card state) and `web/console/tools/bot-request-cards.e2e.mjs`, which drives the real Board
  page in headless Chrome at 1280 and 390 against a fixture that refuses every `/v1` route it does
  not serve, and asserts the Board's counts are identical without the routes, with the section on
  and with it off. The chat part of that script renders the card line beside the real work result
  card in a harness, not inside the whole chat.

Not built in the console: the Bot filter, split and merge lineage, reopening a closed card, and
ticking entries as the person.

Not built yet: a Bot-to-Bot request turn whose exchange names a card (the recipient of a
`message_bot` with `bot_request_id`) still cannot dispatch for it: the exchange does not record the
card, and such turns keep their `peer_dispatch` / `bot_request_tools` refusals. Split and merge.

**Relation to the wider Bot-to-Bot tool loss.** A separate report says Bots lose their tools in
every Bot-to-Bot turn (`pa_disabled` / `bot_request_tools`). The handoff rule in §7.2 deliberately
grants only card-scoped dispatch in a handoff turn and does not fix that wider defect; if the
wider fix lands first, the card scope still applies on top of it rather than being replaced.

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
- The daemon opening a card on a Bot's first writing dispatch (revision 3, §7.1 there).
- Attaching a dispatch to a card it did not name.
- Channel integrations, approval machinery and per-kind templates for non-development work.
- Role names per kind of work; a publishing card uses `maker` like any other.

## 14. Risks and early signals

- **A wrong report reads as delivered.** Signal: the person finds a card marked `cloud` done that
  is not live, or a rejected commit shipped again. Response: the matching line in §13.
- **A Bot forgets to open a card, or to name it when dispatching.** Signal: the person asks where a
  feature is and there is no card, or a card whose work was dispatched shows no task. Response: a
  line in the Bot's standing instructions first; then a reminder in the `dispatch_work` reply when
  a writing dispatch names no card; the daemon opening cards itself (§13) only after both.
- **Bots open cards for everything.** Signal: the section fills with one-step cards. Response: the
  person tells the Bot; §3.2 is the rule it is held to.
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
- Running CI, builds, pushes, deploys or publications from a card.
- Posting to any channel, or checking that a link in a note is live.
- Editing Board items from cards, or showing cards in Board counts or *Needs attention*.
- Root Sessions writing cards for the person.
- **A Board-item "hand to Bot" action.** Suggested by the owner of the Session that shipped the
  work result card; a possible later step, not part of this proposal. It would coexist
  without change here: the person's action on the item would be the card's authority in place of a
  chat message and would set `board_item_id`; the item stays the person's and is never moved by
  the Bot, while the card tracks the Bots' custody.
