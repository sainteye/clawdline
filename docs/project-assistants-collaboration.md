# Persistent Bot review exchange

This development source adds the minimal request/reply path between Bots on one machine.
It is not installed-native, production Cloud, full team/group-room or release
acceptance. The child sandbox initially blocked `/bin/ps`; Root reran the actual
ACP/TCP path with authorized process census and repaired the peer's invalid
`id:null` notification rather than relaxing the production protocol guard.

## Intended path and current source

The Host binds optional `list_bots` and `message_bot` tools to its current
Bot key, full Project, conversation, turn, fence, authority and lifetime.
Model arguments contain no sender identity or machine credential. Admission
returns a durable queued exchange and wakes the recipient only after commit.
Directory results contain ID, name and agent, excluding self. Hidden Bots appear only to the Lead during a direct user turn.

Since 2026-11-21 the directory and admission span every Project and personal
workspace on the same machine, for ordinary Bots and routine runs alike; before
that only a designated Lead could reach outside its Project, and only during a
person's own turn. A routine that had to hand work to a Bot in another
workspace found an empty directory and stopped. Each side still keeps its own
fence: the exchange records the sender's Project and the recipient's
`target_project`, the target's Project is resolved again on admission, and the
recipient works with its own workspace, memory and permissions. A forwarded
request still cannot contact another Bot. The Lead can contact a hidden Bot in a direct user turn.

The feature-owned exchange table and both Bot aggregates are changed in one
transaction, locking aggregate keys in stable order. Source admission receipts,
notices, events and exactly one typed recipient turn are committed together.
The recipient uses its own native settings, session, memory and permissions.
The request's final output completes an exchange and trace, without a recipient
user bubble or ordinary assistant answer. Completion queues one typed source
reply; its eventual normal answer is the source chat summary. Delivery does not
assert observation or read acknowledgement.

Request turns refuse chat publication, another Bot request, file/artifact
publication and schedule mutations at the Host/Engine boundary. Request/reply
turns cannot write Bot memory as user facts. Broker dispatch remains a separate
capability and is not implemented using a child as a teammate; since
2026-11-24 a request turn may dispatch into the recipient's own Project
(`docs/project-assistants-owned-mcp.md`).

New turn origins are `user`, `host_notice`, `bot_request`, and `bot_reply`.
Empty legacy origin preserves existing behavior, never deriving authority from
text. Each structured message projection retains its own Bot's fence.
Request/reply turns inherit hops; ordinary submissions reset them to zero.

## Format and restart boundary

Schema 3 protects origins and exchange facts from schema-2 writers. Installer
accepts v1/v2 and upgrades the metadata and additive DDL atomically, retaining
the v2 agent-rest table and existing aggregate, routine and fallback bytes.
Default-off new installations still create no schema. Already-installed,
disabled Hosts migrate before exposing history. Unknown future versions refuse.
Legacy empty fields are omitted. The actual schema-1 archive stays unchanged;
a v2 archive was produced with the baseline's own types/installer/exporter, with
canonical SHA256 `3b20cd14d176ef2295aee7c81a99a9a4811d18ce189ddaca4de8099743df1067`.

Exports retain exchange rows and structured messages. **Restoring an individual
Bot that contains collaboration facts is explicitly unsupported**, returning
`collaboration_transfer` without writing anything. A single Bot archive cannot
grant authority to another Bot or replay a stored request. Recovery requires a
compatible daemon and whole-store recovery; do not downgrade metadata or strip
exchange rows. Historical payloads have no new arbitrary retention ceiling.

Restart interrupts queued/running requests rather than replaying them as chat.
As in Codync, whose message_bot promises "its final reply (or why it couldn't
finish)", an interrupted (unknown) or cancelled request comes back to its
sender as a reply turn that says it did not finish and must not be resent
without asking the person; restart recovery wakes that sender once the
recipient's actor is released. Before 2026-11-21 only completed or failed
requests replied, so a restart left the sender promising a summary that never
came. Replies that already came back survive a restart, queued or waiting for
queue space, because running them repeats no other Bot's work; an explicit
Stop still drops unstarted waiting replies, retaining completed payloads.
Sender Stop does not cancel admitted outgoing requests. Recipient Stop records
cancelled or unknown, replies to the sender, and preserves the existing
process owner barrier. Logical outstanding
capacity is independent of process cleanup ownership.

## Bounds and upstream navigation

The inspected read-only upstream HEAD was
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`.

* `host/src/chat/team.rs:23-28`: global 64 outstanding requests, request 32000
  bytes and stored notice reply 4000 bytes. This increment retains the complete
  request and therefore adds no separate request-quote truncation bound.
  Clawdline raised its stored reply to 32000 bytes on 2026-11-24 and marks a
  truncated reply with its original length
  ([project-assistants-owned-mcp.md](project-assistants-owned-mcp.md)).
* `host/src/chat/team/requests.rs:10-48`: hops 8, per-sender outstanding 16,
  and request-turn recursion refusal.
* `host/src/chat/team/directory.rs:12-13,42-58,135-149`: visibility, paging and
  minimal directory behavior. Clawdline uses its existing registered page bound;
  it does not adopt upstream's cross-computer directory authority.
* `host/src/agent/bot/queue.rs:242-264`: distinct queued requests/replies.
* `host/src/agent/bot/turn.rs:107`: the sent projection.
* `host/src/store/entries.rs`: durable entry representations (navigation only;
  this source does not import their storage format).

The actual capacity guard failed on all five new constants before registration.
Logical active counts come from durable outstanding rows; the largest sender
occupancy is measured separately. Generic message, pending-turn and process
limits remain distinct.

## Local acceptance and remaining scope

Root exercised the authenticated TCP Server/Host/Engine/SQLite flow using an
actual owned MCP endpoint and Go ACP subprocess. Both one-actor and ordinary
actor capacity complete A input / B request / A reply once each. B's own live
permission is answered through HTTP; forbidden request-side publications and
cross-Project/source/self/forged identities refuse. A has one user input and one
summary, B has neither a user bubble nor a normal assistant answer, and Broker
launches remain zero. Closing and recreating the actual Server, Host and SQLite
owner retains the delivered exchange through new HTTP export/read-view calls
without another ACP invocation. Installed vendor/model/API calls are zero.

A deterministic SQLite interleaving first made a source final result become
unknown, or lost its explicit message, when exchange admission moved the source
version. The repair rebuilds only the uncommitted local terminal/event mutation
from current authorized facts. Event/command identities remain fixed; no native
Open, Send, permission answer or Broker effect is repeated.

Actor pressure originally accepted a request and silently missed its wake. One
runtime waiter now waits on owner/capacity changes, without raising the bound or
creating a waiter per request. A full source chat queue originally rejected the
recipient's completed result. Completion now commits its payload and a waiting
delivery atomically; the source worker promotes one typed reply when existing
queue space is available. Only promotion places the completed result in the
registered chat queue. Promotion is idempotent. Explicit
Stop drops unstarted waiting replies while retaining completed text. A planned
shutdown preserves queued and waiting replies. Startup wakes their senders
through the normal queue so even a status created during shutdown can be
delivered without repeating the recipient's request.

SQLite controls cover admission/completion rollback, same-key replay/conflict,
hidden recipients, stale fences, Stop, request restart without replay, surviving
history after sender deletion, per-sender 16 and global 64 pressure, and full
source queue completion/promotion. A schema-2 database SQL fixture was produced
by the actual fd9a962 types, Installer and Store, with fallback settings/rest,
forgotten memory, routines, queued work, receipts, events and opaque aggregate
bytes. Migration preserves all old values and rolls back new exchange DDL when
the metadata update aborts or is ignored. Actual v1/v2 archive fixtures retain
their original canonical hashes. These controls do not qualify installed native
adapters or additional destinations.

The ordinary integration gate is the next acceptance boundary. Pending
integration work must not be described as a green gate or a main-line landing.
Managers, team/group rooms, coordinator behavior and networking remain in
their existing Board steps. Broader delete/revoke and late-result combinations
remain additional coverage; the whole step is not complete.

## Original chat exchange detail construction

`ChatRows` offers a separate exchange action for messages carrying structured
exchange metadata in the captured Bot's chat. Existing browser, quote, copy,
input and output actions are retained. `ExchangePanel` opens a native modal
dialog with initial focus on Close, Escape handling, focus return, explicit
retry/refresh and independently scrolling content. Phone widths use a single
column of labels and values. Request, reply and diagnostic text is escaped by
React and preserves line breaks and Markdown source; it cannot run HTML.

`loadExchange` uses only the captured `SourceClient.export` and `status` reads.
It validates the exported Bot and conversation, the full Project (including
Project ID and canonical identity), both exchange participants and their
conversations, immutable request turn identities and nondecreasing revision.
The export supplies no source-status envelope, so an authenticated status read
must confirm fresh, online, readable access before publication. History-only
access remains readable. Missing, duplicate, stale, malformed, foreign and
revoked results are explicit typed failures. Abort and captured authorization
changes are checked after asynchronous reads; a closed or replaced panel cannot
publish an old response. The mounted panel follows the existing output viewer's
three-second lifetime check and discards content when its source is revoked.
There is no request admission, model call, reply replay or read acknowledgement.

The panel identifies requester and responder by their actual Bot IDs, adding
the exported name for its own Bot. It does not fetch or invent a deleted peer's
name. Request outcomes and reply delivery are separate labels. Unknown/future
states remain unknown even when a reply looks final. Completed request text
never proves delivery or observation. The panel shows the complete selected
exchange, not an aggregated peer thread; no new retention limit is added.

The current panel reads the captured chat's byte-bounded read view through
snapshot and history cursors. Every page validates the source and conversation.
The store joins notices to matching exchange records before packing each page,
so request outcomes and reply delivery reflect the durable revision. Participant
names come from Bot profiles matched to their own Project scopes, with a captured
profile read when a Bot is missing from the roster. Earlier notices remain
readable across a fresh conversation. The paragraphs above record the original
export-based version.

The read-only upstream comparison was `BotConversation.tsx` and
`bot-exchange.ts` at `9f67eb09084db61b3cca9c84a11bc03df9de29b7`, together
with `host/src/chat/team.rs` and `team/requests.rs`. Upstream opens peer history
from a compact chat row and shows authored request/reply rows with status.
Clawdline keeps its existing single-machine exchange contract and captured
transport rather than importing cross-computer directory authority.

Focused Node controls cover both participants, foreign scope, missing/duplicate
rows, stale revisions, read-only access, abort/revocation during export and
status, explicit retry, distinct outcomes/delivery and actual chat/loading
markup in English and Traditional Chinese. `TestProjectAssistantExchangeCapturedReadSQLite`
seeds a synthetic SQLite exchange, then reads it through a real authenticated
TCP handler and the production paired `ProjectAssistantSourceClient`. It checks
exact bytes, history-only access, GET-only transport, unchanged durable data,
revocation refusal and zero provider calls. Disabling the fixture before
seeding matters: disabling correctly drops pending reply delivery; that is a
lifecycle mutation, not a read effect.

This is construction evidence. Root must integrate the `botExchange` bilingual
catalog fragments and source-catalog registration before landing. Console test
discovery already includes `exchanges.test.ts`; no package script change is
needed. Root also owns the ordinary integration gate, main-line receipt,
transparency refresh and the separate visual/native/Cloud acceptance card.
No deployment, installed-native acceptance or production browser acceptance is
claimed here.
