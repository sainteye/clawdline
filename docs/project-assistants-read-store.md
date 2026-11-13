# SQL Bot read views

`projectassistantstore.NewReadViews(db *sql.DB) (*ViewStore, error)` implements
`app/projectassistant.ReadViews`. The host lends its existing query-only reader;
the adapter neither opens nor closes a database, changes pragmas, installs
schema/indexes, mutates facts, nor invokes a provider. The old Repository,
EventFeed and Export contracts remain separate. See the shared
[read-view contract](project-assistants-read-views.md) for transport and client
responsibilities.

## Ownership and transaction

The constructor retains the borrowed pool and generates a private random HMAC
key and public source epoch. There is no registry, cache, expiry timer or retry.
Trusted composition supplies `AuthorizationBinding`, `ByteLimit` and
`CursorByteLimit`; absent/invalid budgets and ceilings above the existing Cloud
plaintext/cursor bounds are rejected. Domain request validation runs first.

Each `Read` starts one read transaction. Its first SELECT obtains stored Bot
identity/Project, Conversation/current fence, Bot version, memory revision and
the **machine** maximum event sequence. Every later selected fact, count and
reference belongs to that same SQLite snapshot. All rows are closed and the
transaction ends before returning; no SQL lifetime crosses serialization or
other IO. A missing Bot/conversation, malformed required shape, mismatched
identity, missing current binding or broken selected collection relation is an
error, not an authoritative empty result. Legacy null/missing collections and
empty arrays/maps remain valid; null members inside populated collections do
not silently disappear.

The aggregate outer properties are PascalCase; domain properties are
snake_case. Queries explicitly CAST the persisted BLOB to TEXT. JSON functions,
MATERIALIZED CTEs and window functions operate on the existing tables; there is
no Go aggregate unmarshal, `load`, Export, Repository.Bot or Conversation call
on the read path.

Subsequent statements materialize the stored header scalars and collection
types once before iterating members. Fence predicates still compare every
required key, Project, conversation and execution component, but use those
scalar columns rather than extracting each header field from the complete
aggregate for each member. The aggregate and returned member JSON remain TEXT;
raw-byte accounting and complete-object decoding are unchanged. This is a
statement-local SQL projection, not a retained cache or schema change.

## Complete control state and paged facts

Snapshot contains the complete stored Bot, every counted accepted/queued/
running/waiting-permission summary across generations, their user messages and
the last terminal current-fence turn summary. The existing16 counted-turn bound
uses a sentinel and rejects overflow. Unknown turns are an exact separate
cross-generation count:17 unknown plus16 counted turns is valid. Captured turn
instructions and failure narratives never enter a compact turn summary.

Head includes exact current-fence pending-offer and latest-per-(turn,category)
trace counts. Permissions and traces have separate summary pages and signed
detail references. Detail returns one complete stored offer (including every
option) or trace; it does not invent tool/question text or authorize an answer.
Trace references identify the selected latest event; superseded references
expire. Runtime permission/fence checks remain authoritative.

Snapshot/history merge messages and WorkLinks into a single descending
`(anchor_message_seq, kind, stable_ID COLLATE BINARY)` order. Work anchors come
from Turn.MessageID to Message.Seq. There is one total page allowance, with a
sentinel and no skipped ties. History is LIVE across requests, not a retained
snapshot. MarkRead/version changes and new generations do not expire historical
positions; durable old-generation message/work facts remain readable. Later
events reconcile work facts using the observation frontier.

Events check stored source identity without loading the aggregate. They return
at most the existing256 rows plus an internal sentinel, reject ahead/old-epoch
cursors and advance only to the last included event. An empty page preserves
the requested sequence. Nested message/turn/permission/work fences, memory,
Bot/read/source scopes and capabilities are checked independently of the outer
envelope; trace ownership and event binding use scalar same-transaction SQL
checks. Only the initial snapshot head may start client replay. Page heads never
advance an existing replay cursor.

## Two separate byte boundaries

The trusted `ByteLimit` is one shared SQL-to-Go **raw-byte prefix budget**, not
an allowance for each row. The first SELECT CASE-checks the combined metadata
bytes before exposing any metadata JSON to Go. Pending-summary/user-message
bytes and the last terminal summary consume the remaining mandatory budget.
Candidate timeline/event/permission-summary/trace-summary queries use an
ordered SQL `sum(length(...)) OVER (...)` and CASE to expose only the fitting
prefix. Event row IDs are charged too, including a corrupt ID that differs from
its JSON payload. Full detail similarly checks its raw size against the
remaining budget before crossing into Go.

Consequently the sum of returned source JSON texts is at most `ByteLimit`,
bounded by the existing8MiB ceiling, plus small internal failure metadata.
Selected ID columns duplicate IDs already inside their bounded JSON; parsed Go
objects, temporary strings and signed references add overhead. This is not a
claim that total Go allocations or RSS equal8MiB. There are at most100 (events:
256) candidate objects, each continuation/detail token is at most the trusted
cursor budget (never above4096 UTF-8 bytes), and no growing retained history.

SQLite still parses/scans aggregate JSON and may materialize/order rows or
construct temporary query structures. Engine CPU/memory is **not O(page)** and
is not bounded by the returned JSON budget. No latency, peak-RSS or production
contention improvement is inferred from these tests.

The source budget does not estimate HTTP/Cloud escaping or envelope overhead.
After the transaction, the transport measures actual complete generated JSON
and its actual Cloud correlation envelope against the existing ceiling. The
adapter supplies `AfterCursor`, `CursorFailure` and `Failure` per candidate;
none are wire fields. An oversized next object yields a payload-free failure
sentinel, so a legal earlier prefix can be published with honest continuation.
The oversized object is not skipped. A first oversized object has no publishable
prefix and must become typed capacity failure.

Each cursor prefix is judged independently. An unencodable intermediate position
does not invalidate a later fitting prefix. If packing really must stop at a
position with no legal continuation, it must refuse instead of pretending the
page ended. Mandatory data is never sliced to make a success response fit.

## Cursor isolation

HMAC domain separation binds the reader epoch, trusted authorization digest,
stored Bot/Project/conversation, collection and position. Permission/trace
collection cursors and detail references additionally bind the complete current
execution fence. History deliberately does not bind Bot.Version. Tokens are
strictly decoded, checked in UTF-8 bytes and never truncated. Rebuilding a
reader expires old references even when a fresh restore has identical version
and sequence values. Same-process replacement of a DB file without rebuilding
the reader is unsupported and is not detected by an invented heuristic.

## Verification boundary

The adapter tests use synthetic identities and `t.TempDir` databases, actual
Store Commit/Restore and the existing modernc SQLite driver. They cover legacy
null/empty data, malformed required metadata and members, missing relationships,
source/authority/epoch/cursor isolation, UTF-8/lowered budgets, counted/unknown
control separation, all-once600-message/101-work traversal,101 offers each with
101 options, current-fence details, MarkRead and generation changes, shared raw
prefix budgeting, giant trace/captured-instruction data, nested event scopes and
borrowed-pool ownership. A test-only SQL driver barrier pauses the actual first
SELECT after its first row: a second WAL handle commits old-generation work and
message events603/604 while the reader still reports frontier602 owned by a
different Bot. Event resume reads exactly those later facts.

These are module tests, not Host/HTTP/Cloud/native/phone/removal acceptance.
The integrating root owns exact-tree full-gate and deployed-surface evidence.
There are zero product-model or paid external API requests in this module work.
Removal consists of detaching the optional ReadViews composition and deleting
the three adapter sources/two tests/this document; no schema or stored-data
rollback is necessary.

### Long-history source deadline

Initial/recovery snapshot reads exposed a failure on a legal long conversation.
The independent Store regression fixture writes 9,001 contiguous messages
(5,209,366 JSON bytes), 600 durable source events and another Bot's creation
event, which owns machine frontier 602. It uses real Store Commit and ViewStore,
synthetic fixed-length scopes, the existing 100-row page and the original
60-second read context. Fixture creation took 130ms in the failing run; the
head took 76ms, then the first fact-shape SQL statement exhausted the context.
Snapshot returned `context deadline exceeded` after 60.010s. This is separate
from the transport fixtures with different scope lengths and JSON byte counts.

The saved CPU profile contains 53.33s sampled CPU, including 52.83s cumulative
under SQLite's JSON extraction, 52.73s under its JSON cache search and 51.82s
flat in the underlying byte comparison. EXPLAIN QUERY PLAN and the captured SQL
retain the aggregate lookup and member scans; materializing scalars removes
the repeated large-TEXT header lookups from the per-member predicates. The
profile does not establish a bound on SQLite CPU, memory or parse work.

With those statement-local scalars, the same fixture and deadline returned a
100-row snapshot in 2.414s, its 100-row history continuation in 2.121s and an
empty events page in 344ms. Every returned message equals the complete stored
object, the continuation starts at message 8,901, frontier remains 602, the
initial events are not replayed, an ahead cursor is still typed `cursor_expired`,
and no transaction remains borrowed after Read. The regression also checks a
100-row events prefix against complete stored source-event objects. A separate
SQL probe records each statement and its duration; its snapshot shape/relation checks took
382/376ms. These are local synthetic source measurements with substantial
margin inside the existing deadline, not complete Cloud latency or a production
ceiling. Real usage frequency has not been measured for this unreleased feature.

The retained implementation adds one scalar-projection boundary and a behavior
regression test; it keeps the existing shape, fence, relationship and byte-budget
checks. Malformed facts, nested foreign events, missing event bindings and
Trace-to-Turn ownership remain refusals. The bounded transport/Bridge deadline
guard and exact response-envelope verification belong to the integration owner.
