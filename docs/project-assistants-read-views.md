# Bounded Bot read views

This is the additive producer/consumer contract. The port and generated types
do not mount a route, enable the feature or establish a live implementation.
Keep the existing Repository, EventFeed and Export transfer contracts.

## Proven starting point

The read-view feasibility investigation at commit `978fabbdd` used the actual
modernc SQLite driver and old aggregate BLOB format. SQL JSON queries returned
100 messages and a machine event frontier from one read transaction without
unmarshalling the whole aggregate in Go. A second WAL handle committed after
the first SELECT; the old snapshot remained consistent and subsequent events
603/604 resumed after frontier602. Another Bot owned602, so a message sequence
or per-Bot maximum is not the source frontier.

The experiment also restored101 pending offers with101 options each, plus17
unknown and16 counted pending turns. These are legal persisted data: native
client concurrency is not a durable collection bound. JSON null arrays iterate
as one null row; query type/shape explicitly. SQLite still parses and scans
aggregate JSON. No O(page) engine-memory/CPU or end-to-end speedup is claimed.

## Optional read port

`app/projectassistant.ReadViews.Read` accepts a domain request plus trusted
`AuthorizationBinding`, `ByteLimit` and `CursorByteLimit`. These three fields
come from the source service/composition and are never client wire fields.
Borrow the host's existing query-only reader, close all SQL rows and end the
short transaction before serialization or other IO. No new production database,
writer pragma, schema, index or mutation is needed for the first adapter.
The existing single-writer lending port alone does not reproduce the experiment's
concurrent schedule; the production integration must lend its separate reader.

Snapshot/head/selected facts come from one SQL snapshot fixed by the first
SELECT. Required aggregate shapes are checked, including legacy null/empty
collections. Missing Bot is not an authoritative empty view. Oversized raw
objects/mandatory data are detected in SQL before copying huge values to Go,
using the trusted existing Cloud byte budget. SQL engine work is still not
bounded by the output size.

## Read operation and modes

The planned additive route is `GET /v1/project-assistants/{bot}/view`, with one
new read Cloud word `project-assistants.view` and one additional SourceClient
method `view`. Existing16 methods/routes remain unchanged. Only this new read
operation needs API59; the existing metadata surface retains API58 support.
The route/API bump is the consumer's later implementation, not this schema.

The closed generated request contains key, conversation_id, mode, source_epoch,
cursor, after_seq and page_size. The path supplies the captured Bot ID and
the query supplies the other fields. Modes are:

| Mode | Required state | Complete returned payloads |
| --- | --- | --- |
| snapshot | Empty epoch/cursor, after_seq0 | Mandatory snapshot and newest timeline rows |
| history | Epoch and signed history cursor, after_seq0 | Older timeline rows |
| events | Epoch, empty cursor, after_seq>=0 | Durable events after that source cursor |
| permissions | Epoch, empty or signed collection cursor | Current-fence pending-offer summaries |
| traces | Epoch, empty or signed collection cursor | Latest current-fence turn/category summaries |
| detail | Epoch, signed detail reference, page_size1 | One complete stored offer or trace |

Events use existing MaxEventPageRows256; other pages use MaxPageRows100.
Unused fields are rejected. Every collection is non-null. Exactly one payload
matches each ReadItemKind. No native actor or credential appears in the response.
Full stored offers do not invent tool/question text absent from PermissionOffer.
Summary data alone never enables an answer; the UI loads the complete stored
offer and still applies current-fence/fresh-write preflight before answering.

## Complete mandatory state and bounded collections

Head contains stored identity/Project/conversation/current fence, Bot.version,
memory revision, the machine log MAX(seq), an exact unknown-turn count across
generations, current-fence pending-offer count and latest current-fence trace
category count. Unknown barriers cannot disappear because their execution is
old; their count blocks sending without granting old execution authority.

Snapshot adds Bot metadata, ALL counted pending-turn summaries, their queued
user messages, and the last terminal current-turn summary. Pending summaries
exclude captured instructions/failure narratives; their existing16-turn bound
is checked with a sentinel, never silently sliced. Unknown turns are represented
by the exact blocking count, with details still available in history.

Permissions/traces are separate bounded summary pages, with honest has_more
and next_cursor. Load the first page and let the person request more, without
automatically draining the source. Full details are separately fetched by signed
reference. This avoids serializing every pending option/trace into recovery.
Oversized indivisible metadata, offer, trace or row returns typed capacity_full
without a successful partial object. Existing archives stay transfer artifacts;
chat initialization/recovery must never call Export as a snapshot fallback.

## Incarnation, cursors and live history

Each reader instance creates a random private signing key and public source
epoch. Neither is a user/native capability. No permanent snapshot cache or TTL
is added. Fresh human/source authority is required before reading and again
before publication; the epoch alone proves no authority. Rebuilding a reader
expires old references even if restored sequence/version values happen to match.
Raw same-process database-file replacement without rebuilding the reader is
unsupported and is not claimed to be detected.

HMAC domain separation binds epoch, trusted authority-scope digest, stored
Bot/Project/conversation, collection and position. History uses immutable
(message-seq,kind,stable-ID) ordering, with WorkLink anchored through its
Turn.MessageID and BINARY ID order. Current-fence permission/trace/reference
cursors additionally bind the full execution fence. Cursor encoding is limited
to4096 UTF-8 bytes (registered); never truncate it or invent end-of-history.
A too-large intermediate cursor need not reject a later complete fitting prefix.

Do not bind every history cursor to Bot.version: MarkRead advances it. History
pages are LIVE consistent reads, not a frozen cross-request transaction. The
client must reconcile events since the initial snapshot and merge work facts by
observation frontier/identity. A newer work event must not be overwritten by an
older history page. Late work inserted behind a visited position is delivered
through events, including facts belonging to older execution generations.
Permission/trace/action authority still uses the complete CURRENT fence.

Only successful snapshot mode replaces the window/control state, adopts epoch
and starts replay after head.event_frontier. History/summary/detail page heads
NEVER advance an existing event cursor. Events use the last included event
sequence, not a later head frontier. The new events mode checks existence and
shape minimally rather than calling the old full aggregate load. An ahead
cursor or old epoch is cursor_expired, never clamped. Only a new bounded
snapshot releases the existing account/Bot recovery latch; preserve drafts and
unknown intent, and keep failure read-only without Export fallback.

## Exact packing and failure ownership

The reader returns bounded candidates and internal per-prefix AfterCursor,
CursorFailure and Failure data; these do not go on the wire. Transport packs
complete generated objects after the transaction ends. The complete HTTP JSON
and actual Cloud serializer, including the maximum legal read correlation name
(read:+128 HTML-escaped '<' bytes), fit the existing8MiB ceiling.

Encode rows/header once where practical and verify arithmetic against actual
serialization; no guessed escaping multiplier. A valid prefix may be returned
before an oversized next object, with honest continuation. Never skip that
object, split an event or silently drop required state. Source authorization
changes invalidate publication even when SQL fact consistency was correct.

## Ownership and evidence still required

Root owns request/port/projection schema, generator, cursor capacity and shared
diagnostics. Store producer owns the SQL adapter/tests only. Consumer owns
service/host reader lending/HTTP/Cloud/Chat wiring under that frozen contract,
with single ownership of shared files. Producer tests must cover null/empty
legacy data, barriers, cross-Bot frontier, old-generation work, own-read cursor
stability and excess data. Consumer tests must prove no initial600-event replay,
no Export recovery, current-fence detail/answer, full-byte packing and older-page
work not overwriting newer facts. Production Cloud, native, phone and feature
removal/rollback acceptance remain separate. Memory/archive transfer paging is
a separate follow-up; no complete-large-export claim is made here.
